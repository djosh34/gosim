//go:build sim

package behavior

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"hash/maphash"
	"io"
	"io/fs"
	mathrand "math/rand"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"runtime/pprof"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"weak"

	"github.com/jellevandenhooff/gosim"
	"github.com/jellevandenhooff/gosim/gosimruntime"
	"github.com/jellevandenhooff/gosim/internal/tests/behavior/linkname"
)

// Tests for what the Go 1.27 port added or changed.

func TestTempDirAndSetenv(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOSIM_TEST_VAR", "set")
	if got := os.Getenv("GOSIM_TEST_VAR"); got != "set" {
		t.Errorf("got %q, want set", got)
	}
}

func TestSyncMapRange(t *testing.T) {
	var m sync.Map
	keys := []any{"a", 1, new(int), "b"}
	for i, k := range keys {
		m.Store(k, i)
	}
	var seen []int
	m.Range(func(k, v any) bool {
		seen = append(seen, v.(int))
		return true
	})
	// Every key hashes to 0, so Range visits the newest key first.
	if !slices.Equal(seen, []int{3, 2, 1, 0}) {
		t.Errorf("got %v", seen)
	}
	if v, ok := m.Load(keys[2]); !ok || v != 2 {
		t.Errorf("got %v %v", v, ok)
	}
	if !m.CompareAndSwap("a", 0, 10) {
		t.Error("CompareAndSwap failed")
	}
}

func TestWeakPointer(t *testing.T) {
	p := new(int)
	w := weak.Make(p)
	runtime.GC()
	if w.Value() != p || weak.Make(p) != w {
		t.Error("weak pointer lost its value")
	}
	runtime.AddCleanup(p, func(int) { t.Error("cleanup ran") }, 0)
}

func TestPprofLabels(t *testing.T) {
	ctx := pprof.WithLabels(context.Background(), pprof.Labels("b", "2", "a", "1"))
	ctx = pprof.WithLabels(ctx, pprof.Labels("a", "3"))
	var got []string
	pprof.ForLabels(ctx, func(k, v string) bool {
		got = append(got, k+"="+v)
		return true
	})
	if !slices.Equal(got, []string{"a=3", "b=2"}) {
		t.Errorf("got %v", got)
	}
	pprof.Do(ctx, pprof.Labels("c", "4"), func(ctx context.Context) {
		if v, ok := pprof.Label(ctx, "c"); !ok || v != "4" {
			t.Errorf("got %q %v", v, ok)
		}
	})
}

func TestMapLiteralAddress(t *testing.T) {
	m := &map[string]int{"a": 1}
	(*m)["b"] = 2
	if len(*m) != 2 {
		t.Errorf("got %v", *m)
	}
}

type namedMap map[string]int

func TestMapsOnNamedMap(t *testing.T) {
	m := namedMap{"a": 1}
	c := make(namedMap)
	for k, v := range m {
		c[k] = v
	}
	if !reflect.DeepEqual(m, c) {
		t.Errorf("got %v", c)
	}
	if v, ok := reflect.TypeAssert[namedMap](reflect.ValueOf(m)); !ok || len(v) != 1 {
		t.Errorf("got %v %v", v, ok)
	}
}

//go:embed testdata/embed.txt
var embedded string

func TestEmbed(t *testing.T) {
	if embedded != "embedded by TestEmbed\n" {
		t.Errorf("got %q", embedded)
	}
}

func TestTestFuncAsValue(t *testing.T) {
	t.Run("embed", TestEmbed)
}

func TestWriteArtifact(t *testing.T) {
	gosim.WriteArtifact("artifact.txt", []byte("hello"))
}

// TestCPUTime reads the process's CPU time from inside a run.
func TestCPUTime(t *testing.T) {
	before, err := gosim.CPUTime()
	if err != nil {
		t.Fatal(err)
	}
	for {
		now, err := gosim.CPUTime()
		if err != nil {
			t.Fatal(err)
		}
		if now > before {
			return
		}
	}
}

// TestTimerFirings counts the timers that fire in a run.
func TestTimerFirings(t *testing.T) {
	before := gosim.TimerFirings()
	time.Sleep(time.Second)
	tk := time.NewTicker(time.Second)
	for range 3 {
		<-tk.C
	}
	tk.Stop()
	fired := make(chan struct{})
	time.AfterFunc(time.Second, func() { close(fired) })
	<-fired
	if got := gosim.TimerFirings() - before; got != 5 {
		t.Errorf("%d timers fired, want 5: one sleep, three ticks, one AfterFunc", got)
	}
	// A tick nobody reads still fires: an unread ticker fires 3 times in
	// 3.5 s, beside the sleep's end.
	before = gosim.TimerFirings()
	unread := time.NewTicker(time.Second)
	time.Sleep(3500 * time.Millisecond)
	unread.Stop()
	if got := gosim.TimerFirings() - before; got != 4 {
		t.Errorf("%d timers fired, want 4: three ticks nobody read and the end of a sleep", got)
	}
}

// TestHostFile writes bytes to a file on the host and reads them back.
func TestHostFile(t *testing.T) {
	f, err := gosim.CreateHostFile()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte("hello, host"), 3); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 11)
	if _, err := f.ReadAt(b, 3); err != nil {
		t.Fatal(err)
	}
	if string(b) != "hello, host" {
		t.Errorf("read %q back", b)
	}
	if _, err := f.ReadAt(b, 20); !errors.Is(err, io.EOF) {
		t.Errorf("a read past the end gave %v, want io.EOF", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ReadAt(b, 0); !errors.Is(err, fs.ErrClosed) {
		t.Errorf("a read of a closed host file gave %v, want fs.ErrClosed", err)
	}
	if err := f.Close(); !errors.Is(err, fs.ErrClosed) {
		t.Errorf("a second close gave %v, want fs.ErrClosed", err)
	}
}

// TestHostFileSlots checks that a simulation's live owners get slots of
// their own in a host file, which keep what is written to them.
func TestHostFileSlots(t *testing.T) {
	f, err := gosim.CreateHostFile()
	if err != nil {
		t.Fatal(err)
	}
	type owner struct{ name string }
	owners := []*owner{{"a"}, {"b"}, {"c"}}
	offs := map[int64]*owner{}
	for _, o := range owners {
		off, err := f.Slot(o, 16)
		if err != nil {
			t.Fatal(err)
		}
		if offs[off] != nil {
			t.Fatalf("%s and %s got the slot at %d", offs[off].name, o.name, off)
		}
		offs[off] = o
		if _, err := f.WriteAt([]byte(fmt.Sprintf("%-16s", o.name)), off); err != nil {
			t.Fatal(err)
		}
	}
	for off, o := range offs {
		b := make([]byte, 16)
		if _, err := f.ReadAt(b, off); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(string(b)); got != o.name {
			t.Errorf("the slot of %s holds %q", o.name, got)
		}
	}
	if _, err := f.Slot(&owner{}, 32); !errors.Is(err, gosimruntime.ErrSlotSize) {
		t.Errorf("a slot of another size than the file's first: %v, want %v", err, gosimruntime.ErrSlotSize)
	}
	// Once its owner is garbage, a slot comes back for another.
	gone := &owner{"gone"}
	at, err := f.Slot(gone, 16)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte(fmt.Sprintf("%-16s", gone.name)), at); err != nil {
		t.Fatal(err)
	}
	gone = nil
	reused := false
	for range 50 {
		runtime.GC()
		next := &owner{"next"}
		off, err := f.Slot(next, 16)
		if err != nil {
			t.Fatal(err)
		}
		owners = append(owners, next)
		if off == at {
			reused = true
			break
		}
	}
	if !reused {
		t.Error("the slot of an owner that is garbage never came back")
	}
	runtime.KeepAlive(owners)
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Slot(&owner{}, 16); !errors.Is(err, fs.ErrClosed) {
		t.Errorf("a slot of a closed host file gave %v, want fs.ErrClosed", err)
	}
}

// TestWallLimit waits, on the simulated clock, until its limit of real
// time passed, and saves every goroutine's stack.
func TestWallLimit(t *testing.T) {
	if os.Getenv("TESTINGWALL") != "1" {
		t.Skip()
	}
	for !gosim.WallLimitPassed() {
		time.Sleep(time.Millisecond)
	}
	gosim.WriteArtifact("stacks", gosim.Stacks())
}

// TestWallKill saves an artifact, then never yields, so only its second
// limit of real time ends it.
func TestWallKill(t *testing.T) {
	if os.Getenv("TESTINGWALL") != "1" {
		t.Skip()
	}
	gosim.WriteArtifact("before.txt", []byte("saved"))
	for n := 0; ; n++ {
		_ = n
	}
}

func TestUnimplementedHook(t *testing.T) {
	if os.Getenv("TESTINGFAIL") != "1" {
		t.Skip()
	}
	defer func() {
		recover()
		t.Error("recovered from an unimplemented hook")
	}()
	reflect.MapOf(reflect.TypeFor[string](), reflect.TypeFor[int]())
}

// TestExitStopsMachine checks that os.Exit on a machine stops it, that
// nothing after os.Exit runs, and that what it wrote stays on its disk.
func TestExitStopsMachine(t *testing.T) {
	m := gosim.NewMachine(gosim.MachineConfig{
		Label: "exit",
		Addr:  netip.MustParseAddr("10.0.0.1"),
		MainFunc: func() {
			if err := os.WriteFile("before", []byte("kept"), 0o644); err != nil {
				return
			}
			os.Exit(3)
			_ = os.WriteFile("after", []byte("not kept"), 0o644)
		},
	})
	m.Wait()
	results := make(chan string, 2)
	m.SetMainFunc(func() {
		before, _ := os.ReadFile("before")
		_, err := os.Stat("after")
		results <- string(before)
		results <- fmt.Sprint(os.IsNotExist(err))
	})
	m.Restart()
	if got := <-results; got != "kept" {
		t.Errorf("the file written before os.Exit holds %q", got)
	}
	if got := <-results; got != "true" {
		t.Error("code after os.Exit ran")
	}
}

func TestReflectLinkname(t *testing.T) {
	if os.Getenv("TESTINGFAIL") != "1" {
		t.Skip()
	}
	linkname.New()
	t.Error("the linkname into reflect returned")
}

func TestFatalUnlock(t *testing.T) {
	if os.Getenv("TESTINGFAIL") != "1" {
		t.Skip()
	}
	defer func() {
		recover()
		t.Error("recovered from a fatal error")
	}()
	var mu sync.Mutex
	mu.Unlock()
}

func TestOpenRoot(t *testing.T) {
	f, err := os.Open("/")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Sync(); err != nil {
		t.Error(err)
	}
	if err := f.Close(); err != nil {
		t.Error(err)
	}
	if info, err := os.Stat("/"); err != nil || !info.IsDir() {
		t.Errorf("got %v %v", info, err)
	}
}

func TestLoopbackListener(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		c, err := l.Accept()
		if err != nil {
			t.Error(err)
			return
		}
		c.Write([]byte("hi"))
		c.Close()
	}()
	c, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(c)
	if err != nil || string(b) != "hi" {
		t.Errorf("got %q %v", b, err)
	}
}

//go:embed testdata/embed.txt
var embeddedBytes []byte

type namedBytes []byte

//go:embed testdata/embed.txt
var embeddedNamedBytes namedBytes

//go:embed testdata/embed.txt
var embeddedFS embed.FS

// TestEmbedBytesPerRun changes embedded variables; the meta test checks that
// the next run sees their embedded values again.
func TestEmbedBytesPerRun(t *testing.T) {
	if string(embeddedBytes) != "embedded by TestEmbed\n" || string(embeddedNamedBytes) != "embedded by TestEmbed\n" || embedded != "embedded by TestEmbed\n" {
		t.Errorf("got %q, %q, %q", embeddedBytes, embeddedNamedBytes, embedded)
	}
	if _, err := embeddedFS.ReadFile("testdata/embed.txt"); err != nil {
		t.Error(err)
	}
	embeddedBytes[0] = 'X'
	embeddedNamedBytes[0] = 'X'
	embedded = "changed"
	embeddedFS = embed.FS{}
}

func TestContextInCleanup(t *testing.T) {
	t.Cleanup(func() {
		select {
		case <-t.Context().Done():
		default:
			t.Error("the context is not canceled during cleanup")
		}
	})
}

func TestRemovedTempDir(t *testing.T) {
	if err := os.Remove(t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

func TestCloseUnacceptedConnection(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestMaphashComparable(t *testing.T) {
	if os.Getenv("TESTINGFAIL") != "1" {
		t.Skip()
	}
	maphash.Comparable(maphash.MakeSeed(), "key")
}

// TestReseed draws the same values after each Reseed with one seed,
// whatever ran before it, and other values after another seed. Its
// goroutines wake at one instant from timers set after the Reseed, while
// the timer heap holds a different past each time.
func TestReseed(t *testing.T) {
	round := func(seed int64, pending int) []uint64 {
		// Timers that outlast the round give the heap another past.
		for i := range pending {
			tm := time.NewTimer(time.Hour + time.Duration(i))
			defer tm.Stop()
		}
		// The clock moves only once no goroutine can run: the last
		// round's goroutines have ended, as Reseed asks.
		time.Sleep(time.Millisecond)
		gosim.Reseed(seed)
		at := time.Now().Add(time.Second)
		var out []uint64
		var wg sync.WaitGroup
		var mu sync.Mutex
		for i := range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				time.Sleep(time.Until(at))
				mu.Lock()
				out = append(out, uint64(i)<<32|uint64(mathrand.Uint32()))
				mu.Unlock()
			}()
		}
		wg.Wait()
		return out
	}
	first := round(7, 0)
	if again := round(7, 5); !slices.Equal(first, again) {
		t.Errorf("after the same seed: %v, then %v", first, again)
	}
	if other := round(8, 3); slices.Equal(first, other) {
		t.Errorf("seeds 7 and 8 drew the same: %v", other)
	}
}

func TestSometimesCrashOnSyncOff(t *testing.T) {
	// The machine turns crashes on and off again, then syncs 200 times.
	var done bool
	// The token orders the machine's write of done before the read here.
	gosimruntime.TestRaceToken.Release()
	m := gosim.NewSimpleMachine(func() {
		gosimruntime.TestRaceToken.Acquire()
		defer gosimruntime.TestRaceToken.Release()
		gosim.CurrentMachine().SetSometimesCrashOnSync(true)
		gosim.CurrentMachine().SetSometimesCrashOnSync(false)
		f, err := os.OpenFile("f", os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			t.Error(err)
			return
		}
		defer f.Close()
		for i := 0; i < 200; i++ {
			if err := f.Sync(); err != nil {
				t.Error(err)
				return
			}
		}
		done = true
	})
	m.Wait()
	gosimruntime.TestRaceToken.Acquire()
	if !done {
		t.Error("the machine crashed in fsync after SetSometimesCrashOnSync(false)")
	}
}

func TestSometimesCrashOnSyncOn(t *testing.T) {
	var done bool
	// The token orders the machine's write of done before the read here.
	gosimruntime.TestRaceToken.Release()
	m := gosim.NewSimpleMachine(func() {
		gosimruntime.TestRaceToken.Acquire()
		defer gosimruntime.TestRaceToken.Release()
		gosim.CurrentMachine().SetSometimesCrashOnSync(true)
		f, err := os.OpenFile("f", os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			t.Error(err)
			return
		}
		defer f.Close()
		for i := 0; i < 200; i++ {
			if err := f.Sync(); err != nil {
				t.Error(err)
				return
			}
		}
		done = true
	})
	m.Wait()
	gosimruntime.TestRaceToken.Acquire()
	if done {
		t.Error("200 fsyncs with SetSometimesCrashOnSync(true) and no crash")
	}
}

// TestRuntimeState checks that the reads of the real runtime's state give
// the simulation's fixed values.
func TestRuntimeState(t *testing.T) {
	if n := runtime.NumCPU(); n != gosimruntime.SimCPUs {
		t.Errorf("NumCPU: %d, want %d", n, gosimruntime.SimCPUs)
	}
	if n := runtime.GOMAXPROCS(16); n != gosimruntime.SimCPUs {
		t.Errorf("GOMAXPROCS(16): %d, want %d", n, gosimruntime.SimCPUs)
	}
	if n := runtime.GOMAXPROCS(0); n != gosimruntime.SimCPUs {
		t.Errorf("GOMAXPROCS(0) after GOMAXPROCS(16): %d, want %d", n, gosimruntime.SimCPUs)
	}

	before := runtime.NumGoroutine()
	stop := make(chan struct{})
	go func() { <-stop }()
	// A goroutine on another machine does not count.
	gosimruntime.TestRaceToken.Release()
	m := gosim.NewSimpleMachine(func() {
		gosimruntime.TestRaceToken.Acquire()
		ch := stop
		gosimruntime.TestRaceToken.Release()
		<-ch
	})
	if n := runtime.NumGoroutine(); n != before+1 {
		t.Errorf("NumGoroutine: %d, want %d", n, before+1)
	}
	close(stop)
	m.Wait()
	gosimruntime.TestRaceToken.Acquire()

	ms := runtime.MemStats{Alloc: 1}
	runtime.ReadMemStats(&ms)
	if ms != (runtime.MemStats{}) {
		t.Errorf("ReadMemStats: %+v, want zeros", ms)
	}

	samples := []metrics.Sample{{Name: "/sched/goroutines:goroutines"}, {Name: "/sched/latencies:seconds"}}
	metrics.Read(samples)
	if v := samples[0].Value; v.Kind() != metrics.KindUint64 || v.Uint64() != 0 {
		t.Errorf("%s: %v %v, want KindUint64 0", samples[0].Name, v.Kind(), v.Uint64())
	}
	if v := samples[1].Value; v.Kind() != metrics.KindFloat64Histogram || len(v.Float64Histogram().Counts) == 0 {
		t.Errorf("%s: %v, want a histogram", samples[1].Name, v.Kind())
	} else {
		for _, c := range v.Float64Histogram().Counts {
			if c != 0 {
				t.Errorf("%s: count %d, want 0", samples[1].Name, c)
			}
		}
	}
}

// TestStack checks that runtime.Stack and debug.Stack give the same text on
// every run of a seed: TestMetaDeterministic compares the logged stacks.
func TestStack(t *testing.T) {
	buf := make([]byte, 1<<16)
	stacks := []string{string(buf[:runtime.Stack(buf, false)]), string(debug.Stack())}
	done := make(chan struct{})
	go func() {
		defer close(done)
		stacks = append(stacks, string(debug.Stack()))
	}()
	<-done
	for _, s := range stacks {
		t.Log(s)
		if !strings.HasPrefix(s, "goroutine ") || !strings.Contains(s, "TestStack") {
			t.Errorf("not a stack of TestStack:\n%s", s)
		}
		if strings.Contains(s, " in goroutine ") || strings.Contains(s, "gosimruntime.Stack(") {
			t.Errorf("a stack names a creating goroutine or Stack itself:\n%s", s)
		}
		for _, line := range strings.Split(s, "\n") {
			if !strings.HasPrefix(line, "\t") && strings.Contains(line, "0x") {
				t.Errorf("a frame shows its arguments: %s", line)
			}
		}
	}
}

// TestExitNonzero calls os.Exit(3) on the main machine; the meta test
// checks that the run fails.
func TestExitNonzero(t *testing.T) {
	if os.Getenv("TESTINGFAIL") != "1" {
		t.Skip()
	}
	os.Exit(3)
}

// TestPanicLog panics on a goroutine; the meta test checks that two runs
// give the same checksum.
func TestPanicLog(t *testing.T) {
	if os.Getenv("TESTINGFAIL") != "1" {
		t.Skip()
	}
	go panic("a panic for the log")
	select {}
}

// TestRawHandledSyscall makes a syscall the simulation handles through
// RawSyscall6, bypassing package syscall: it fails, and the run goes on.
func TestRawHandledSyscall(t *testing.T) {
	if _, _, errno := syscall.RawSyscall6(syscall.SYS_FCHMOD, ^uintptr(0), 0o644, 0, 0, 0, 0); errno == 0 {
		t.Error("fchmod of no file succeeded")
	}
}

func stackAllBlocked(ch chan struct{}) { <-ch }

// TestStackAll checks that runtime.Stack with all lists the goroutines of
// the caller's machine, and only those.
func TestStackAll(t *testing.T) {
	ch := make(chan struct{})
	defer close(ch)
	go stackAllBlocked(ch)
	gosimruntime.TestRaceToken.Release()
	m := gosim.NewSimpleMachine(func() {
		gosimruntime.TestRaceToken.Acquire()
		c := ch
		gosimruntime.TestRaceToken.Release()
		otherMachineBlocked(c)
	})
	time.Sleep(time.Millisecond)
	buf := make([]byte, 1<<16)
	s := string(buf[:runtime.Stack(buf, true)])
	t.Log(s)
	if !strings.Contains(s, "stackAllBlocked") || !strings.Contains(s, "TestStackAll") {
		t.Errorf("not the stacks of this machine's goroutines:\n%s", s)
	}
	if strings.Contains(s, "otherMachineBlocked") {
		t.Errorf("a goroutine of another machine:\n%s", s)
	}
	_ = m
}

func otherMachineBlocked(ch chan struct{}) { <-ch }
