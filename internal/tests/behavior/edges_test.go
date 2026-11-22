package behavior_test

import (
	"bytes"
	cryptorand "crypto/rand"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/big"
	"math/rand"
	"os"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/jellevandenhooff/gosim"
)

// Constructs that translation once got wrong.

// varNilMap is a blank global that is nil.
var _ map[string]int = nil

type hidden map[string]int

type holder struct{ h hidden }

// hide's parameter hides the type hidden, which the translated assignment
// of a map to h names.
func hide(hidden []string) holder {
	m := make(map[string]int, 0)
	for _, k := range hidden {
		m[k]++
	}
	return holder{h: m}
}

func TestShadowedMapType(t *testing.T) {
	if got := hide([]string{"a", "a", "b"}).h; got["a"] != 2 || got["b"] != 1 {
		t.Errorf("got %v", got)
	}
}

func TestMakeSizes(t *testing.T) {
	n := uint16(3)
	ch := make(chan int, n)
	ch <- 1
	ch <- 2
	ch <- 3
	if len(ch) != 3 || cap(ch) != 3 {
		t.Errorf("len %d, cap %d", len(ch), cap(ch))
	}
	size := 10
	m := make(map[int]int, size)
	m[1] = 1
	if len(m) != 1 {
		t.Errorf("got %v", m)
	}
}

func TestBigIntRand(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	n := big.NewInt(1000)
	seen := make(map[int64]bool)
	for range 200 {
		v := new(big.Int).Rand(r, n)
		if v.Sign() < 0 || v.Cmp(n) >= 0 {
			t.Fatalf("got %v, not in [0, 1000)", v)
		}
		seen[v.Int64()] = true
	}
	if len(seen) < 100 {
		t.Errorf("only %d values in 200 draws", len(seen))
	}
	if v := new(big.Int).Rand(r, big.NewInt(0)); v.Sign() != 0 {
		t.Errorf("Rand with n = 0 got %v", v)
	}
	// The receiver is the bound.
	m := big.NewInt(1000)
	m.Rand(r, m)
	if m.Sign() < 0 || m.Cmp(n) >= 0 {
		t.Errorf("m.Rand(r, m) with m = 1000 got %v", m)
	}
}

func TestGoPrintln(t *testing.T) {
	done := make(chan struct{})
	go println("from a goroutine")
	go func() { close(done) }()
	<-done
}

var builders map[string]*strings.Builder

// setBuilders's parameter hides the package strings in the body, where the
// translated nil names *strings.Builder.
func setBuilders(strings map[string]*strings.Builder) func() {
	builders = strings
	return func() { builders = nil }
}

func TestShadowedPackage(t *testing.T) {
	reset := setBuilders(map[string]*strings.Builder{"a": {}})
	if len(builders) != 1 {
		t.Errorf("got %v", builders)
	}
	reset()
	if builders != nil {
		t.Errorf("got %v after the reset", builders)
	}
}

// chanSet is a set of channels, as in a struct field of CockroachDB.
type chanSet struct {
	mu struct {
		chans map[chan<- struct{}]struct{}
	}
}

func TestMapOfChannels(t *testing.T) {
	var s chanSet
	s.mu.chans = make(map[chan<- struct{}]struct{})
	ch := make(chan struct{}, 1)
	s.mu.chans[ch] = struct{}{}
	for c := range s.mu.chans {
		select {
		case c <- struct{}{}:
		default:
			t.Error("the channel is full")
		}
	}
	delete(s.mu.chans, ch)
	if len(s.mu.chans) != 0 || len(ch) != 1 {
		t.Errorf("got %d channels, %d in the channel", len(s.mu.chans), len(ch))
	}
}

type optMap map[string]int

func takesOpt(m optMap) int { return len(m) }

var globalOpts = map[string]int{"a": 1, "b": 2}

func TestGlobalMapConversion(t *testing.T) {
	if n := takesOpt(globalOpts); n != 2 {
		t.Errorf("got %d", n)
	}
}

//go:embed testdata/embed.txt
var embedded embed.FS

// TestEmbedFSAsFS passes an embed.FS where an fs.FS is expected.
func TestEmbedFSAsFS(t *testing.T) {
	data, err := fs.ReadFile(embedded, "testdata/embed.txt")
	if err != nil || len(data) == 0 {
		t.Fatalf("got %q, %v", data, err)
	}
	var names []string
	if err := fs.WalkDir(embedded, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		names = append(names, p)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if want := []string{".", "testdata", "testdata/embed.txt"}; !slices.Equal(names, want) {
		t.Errorf("got %q, want %q", names, want)
	}
	info, err := fs.Stat(embedded, "testdata/embed.txt")
	if err != nil || info.Size() != int64(len(data)) || info.IsDir() {
		t.Errorf("got %v, %v", info, err)
	}
	if _, err := fs.Stat(embedded, "missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a missing file: got %v", err)
	}
}

// TestStatStdio stats the standard files, as cockroachdb/ttycolor does
// when it starts. In a simulation they are character devices.
func TestStatStdio(t *testing.T) {
	for _, f := range []*os.File{os.Stdin, os.Stdout, os.Stderr} {
		info, err := f.Stat()
		if err != nil {
			t.Fatalf("%s: %v", f.Name(), err)
		}
		// Natively, go test's output may be a pipe.
		if gosim.IsSim() && info.Mode()&os.ModeCharDevice == 0 {
			t.Errorf("%s: mode %v, not a character device", f.Name(), info.Mode())
		}
	}
}

// stdoutMode is set by an initializer that, as cockroachdb/ttycolor's,
// panics when os.Stdout cannot be stat'ed. gosim runs every initializer
// once outside any machine, before the machines run them.
var stdoutMode = mustStat(os.Stdout)

func mustStat(f *os.File) os.FileMode {
	info, err := f.Stat()
	if err != nil {
		panic(err)
	}
	return info.Mode()
}

func TestInitializerStatsStdout(t *testing.T) {
	if gosim.IsSim() && stdoutMode&os.ModeCharDevice == 0 {
		t.Errorf("os.Stdout has mode %v", stdoutMode)
	}
}

// randomKey is made by an initializer from crypto/rand, which gosim also
// runs outside any machine.
var randomKey = mustRandom()

func mustRandom() []byte {
	b := make([]byte, 32)
	if _, err := cryptorand.Read(b); err != nil {
		panic(err)
	}
	return b
}

func TestInitializerReadsRandom(t *testing.T) {
	if bytes.Equal(randomKey, make([]byte, 32)) {
		t.Error("the key is all zeros")
	}
}

// TestCopyOfStderr copies stderr with fcntl, as CockroachDB's logging does
// in an initializer.
func TestCopyOfStderr(t *testing.T) {
	fd, err := unix.FcntlInt(os.Stderr.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	origStderr := os.NewFile(uintptr(fd), os.Stderr.Name())
	defer origStderr.Close()
	if _, err := origStderr.Write([]byte("written to the copy of stderr\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := origStderr.Stat(); err != nil {
		t.Fatal(err)
	}
}

// TestStructOf builds a struct type at run time, as CockroachDB's
// schemachanger/rel does in its initializers.
func TestStructOf(t *testing.T) {
	typ := reflect.StructOf([]reflect.StructField{
		{Name: "A", Type: reflect.TypeFor[int](), Tag: `json:"a"`},
		{Name: "B", Type: reflect.TypeFor[map[string]int]()},
	})
	if typ.NumField() != 2 || typ.Field(0).Tag.Get("json") != "a" || typ.Field(1).Type.Kind() != reflect.Map {
		t.Fatalf("got %v", typ)
	}
	v := reflect.New(typ).Elem()
	v.Field(0).SetInt(7)
	if v.Field(0).Int() != 7 {
		t.Errorf("got %v", v.Field(0).Int())
	}
}

// TestMapIndexMissing checks that MapIndex of a missing key is not valid,
// which YAML decoders use to find duplicate keys.
func TestMapIndexMissing(t *testing.T) {
	m := reflect.ValueOf(map[string]int{"a": 0})
	if !m.MapIndex(reflect.ValueOf("a")).IsValid() {
		t.Error("MapIndex of a key with a zero value is not valid")
	}
	if m.MapIndex(reflect.ValueOf("b")).IsValid() {
		t.Error("MapIndex of a missing key is valid")
	}
}

// TestSetMapIndexDeletes checks that SetMapIndex with the zero Value
// deletes the key.
func TestSetMapIndexDeletes(t *testing.T) {
	m := map[string]int{"a": 1, "b": 2}
	v := reflect.ValueOf(m)
	v.SetMapIndex(reflect.ValueOf("a"), reflect.Value{})
	v.SetMapIndex(reflect.ValueOf("c"), reflect.ValueOf(3))
	if len(m) != 2 || m["b"] != 2 || m["c"] != 3 {
		t.Errorf("got %v", m)
	}
}

// TestChmod changes modes, as os.MkdirTemp's callers do; the simulated
// file system keeps no modes, so only errors are checked.
func TestChmod(t *testing.T) {
	setupRealDisk(t)
	dir, err := os.MkdirTemp(".", "chmod")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(dir + "/f")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod("missing", 0o700); !os.IsNotExist(err) {
		t.Errorf("chmod of a missing file: got %v", err)
	}
}

// TestAppend opens a file with O_APPEND twice, as CockroachDB records its
// temp directories: each write goes to the end.
func TestAppend(t *testing.T) {
	setupRealDisk(t)
	for _, line := range []string{"one\n", "two\n"} {
		f, err := os.OpenFile("record", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString(line); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := os.ReadFile("record"); err != nil || string(got) != "one\ntwo\n" {
		t.Errorf("got %q, %v", got, err)
	}
}

// TestReadDirFile reads an open directory, which fails with EISDIR, and
// lstats a directory, as CockroachDB does on its store.
func TestReadDirFile(t *testing.T) {
	setupRealDisk(t)
	if err := os.Mkdir("dir", 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open("dir")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Read(make([]byte, 10)); !errors.Is(err, syscall.EISDIR) {
		t.Errorf("read of a directory: got %v, want EISDIR", err)
	}
	if _, err := f.ReadAt(make([]byte, 10), 0); !errors.Is(err, syscall.EISDIR) {
		t.Errorf("pread of a directory: got %v, want EISDIR", err)
	}
	info, err := os.Lstat("dir")
	if err != nil || !info.IsDir() {
		t.Errorf("lstat: got %v, %v", info, err)
	}
}

// TestNilChanLen takes len and cap of a nil channel, as the AWS SDK's S3
// upload manager does.
func TestNilChanLen(t *testing.T) {
	var ch chan int
	if len(ch) != 0 || cap(ch) != 0 {
		t.Errorf("len %d, cap %d", len(ch), cap(ch))
	}
}

// TestReadDirInfo lists a directory with the files' details, which stats
// each entry relative to the open directory, as CockroachDB does with
// ioutil.ReadDir.
func TestReadDirInfo(t *testing.T) {
	setupRealDisk(t)
	if err := os.MkdirAll("d/sub", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("d/file", []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open("d")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	infos, err := f.Readdir(-1)
	if err != nil {
		t.Fatal(err)
	}
	sizes := map[string]int64{}
	for _, info := range infos {
		if info.IsDir() {
			sizes[info.Name()] = -1
		} else {
			sizes[info.Name()] = info.Size()
		}
	}
	if len(sizes) != 2 || sizes["file"] != 5 || sizes["sub"] != -1 {
		t.Errorf("got %v", sizes)
	}
	if err := os.RemoveAll("d"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat("d"); !os.IsNotExist(err) {
		t.Errorf("d after RemoveAll: %v", err)
	}
}

var lenSlots [5]int

// lenHalf divides a constant len by a float constant: an int division.
var lenHalf = len(lenSlots) / 2.0

func TestConstantLenIsInt(t *testing.T) {
	if lenHalf != 2 {
		t.Errorf("len(lenSlots) / 2.0 = %v, want 2", lenHalf)
	}
}

// TestEmbedFSReadDir reads an embedded directory an entry at a time.
func TestEmbedFSReadDir(t *testing.T) {
	var fsys fs.FS = embedded
	f, err := fsys.Open("testdata")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dir := f.(fs.ReadDirFile)
	first, err := dir.ReadDir(1)
	if err != nil || len(first) != 1 || first[0].Name() != "embed.txt" {
		t.Fatalf("first ReadDir(1): %v, %v", first, err)
	}
	if rest, err := dir.ReadDir(1); len(rest) != 0 || err != io.EOF {
		t.Errorf("second ReadDir(1): %v, %v; want io.EOF", rest, err)
	}
	if rest, err := dir.ReadDir(-1); len(rest) != 0 || err != nil {
		t.Errorf("ReadDir(-1) at the end: %v, %v", rest, err)
	}
}

// TestEmbedFSAsReadFileFS converts an embed.FS to fs.ReadFileFS.
func TestEmbedFSAsReadFileFS(t *testing.T) {
	var rf fs.ReadFileFS = embedded
	if data, err := rf.ReadFile("testdata/embed.txt"); err != nil || len(data) == 0 {
		t.Errorf("got %q, %v", data, err)
	}
}

func localLen() int {
	var a [4]byte
	return len(a)
}

// lenOfMapLiteral is constant, though translation turns the map literal
// into a call.
const lenOfMapLiteral = len([1]map[int]int{{}})

// lenWithLocal takes len of an array of a global and a local.
func lenWithLocal() int {
	v := 7
	return len([2]int{globalOpts["a"], v})
}

// lenUnderLocalInt takes len of a global array where a local hides int.
func lenUnderLocalInt() int {
	int := 3
	return len(lenSlots) + int
}

// TestLocalConstantLen takes len of local arrays, which stays as written,
// and the cases around it.
func TestLocalConstantLen(t *testing.T) {
	a := [3]int{1, 2, 3}
	var b [len(a)]int
	if localLen() != 4 || len(b) != 3 {
		t.Errorf("got %d and %d", localLen(), len(b))
	}
	if lenOfMapLiteral != 1 || lenWithLocal() != 2 || lenUnderLocalInt() != 8 {
		t.Errorf("got %d, %d, %d", lenOfMapLiteral, lenWithLocal(), lenUnderLocalInt())
	}
}

// TestDupMinimum dups a standard file to a descriptor at or above 100.
func TestDupMinimum(t *testing.T) {
	fd, err := unix.FcntlInt(1, unix.F_DUPFD_CLOEXEC, 100)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if fd < 100 {
		t.Errorf("got descriptor %d, below 100", fd)
	}
	// The lowest free descriptor at or above the minimum, below one
	// handed out before.
	fd2, err := unix.FcntlInt(1, unix.F_DUPFD_CLOEXEC, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd2)
	if fd2 < 10 || fd2 >= 100 || gosim.IsSim() && fd2 != 10 {
		t.Errorf("minimum 10 after 100: got descriptor %d", fd2)
	}
	for _, min := range []int{-1, 1 << 30} {
		if _, err := unix.FcntlInt(1, unix.F_DUPFD_CLOEXEC, min); err != unix.EINVAL {
			t.Errorf("minimum %d: got %v, want EINVAL", min, err)
		}
	}
	if !gosim.IsSim() {
		return
	}
	// In a simulation the machine's descriptors are known: 3 and 4 are
	// free, and the last one below the limit is 2^20-1, once.
	fd3, err := unix.FcntlInt(1, unix.F_DUPFD_CLOEXEC, 3)
	if err != nil || fd3 != 3 {
		t.Errorf("minimum 3: got %d, %v", fd3, err)
	}
	defer unix.Close(fd3)
	last, err := unix.FcntlInt(1, unix.F_DUPFD_CLOEXEC, 1<<20-1)
	if err != nil || last != 1<<20-1 {
		t.Errorf("minimum 2^20-1: got %d, %v", last, err)
	}
	defer unix.Close(last)
	if fd, err := unix.FcntlInt(1, unix.F_DUPFD_CLOEXEC, 1<<20-1); err != unix.EMFILE {
		t.Errorf("minimum 2^20-1 again: got %d, %v; want EMFILE", fd, err)
	}
	// An open does not get the descriptor of a closed dup.
	dup, err := unix.FcntlInt(1, unix.F_DUPFD_CLOEXEC, 50)
	if err != nil {
		t.Fatal(err)
	}
	unix.Close(dup)
	for range 50 {
		g, err := os.OpenFile("dup2", os.O_CREATE|os.O_RDWR, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		got := int(g.Fd())
		g.Close()
		if got == dup {
			t.Fatalf("an open got %d, a closed dup's descriptor", dup)
		}
	}
	// An open after the high dup gets a low descriptor.
	f, err := os.OpenFile("dup", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.Fd() >= 1<<20 {
		t.Errorf("an open got descriptor %d", f.Fd())
	}
}

var lenSource = [3]int{1, 2, 3}

// lenSized is a global whose array type takes len of another global.
var lenSized [len(lenSource)]int

// lenSizedMap holds an array type sized by len of a global, in a map.
var lenSizedMap = map[string][len(lenSource)]int{"a": {1, 2, 3}}

const lenConst = len(lenSource) * 2

func TestGlobalSizedByLen(t *testing.T) {
	if got := []int{len(lenSized), len(lenSizedMap["a"]), lenConst}; !slices.Equal(got, []int{3, 3, 6}) {
		t.Errorf("got %v", got)
	}
	var local [len(lenSource)]byte
	if cap(local) != 3 {
		t.Errorf("got %d", cap(local))
	}
}

// lenNilDeref takes len of an array literal of a global and a nil
// dereference: Go does not evaluate the operand, so nothing panics.
func lenNilDeref() int {
	var p *int
	return len([2]int{*p, lenSource[0]})
}

// lenConstWithLocal declares a constant from len of a local and a global.
func lenConstWithLocal() int {
	v := 1
	const n = len([2]int{v, lenSource[0]})
	return n
}

// capUnderLocalLen takes cap of a global array where a local hides len.
func capUnderLocalLen() int {
	len := 42
	return cap(lenSource) + len
}

// Constants of len of nil maps and channels, which translation turns into
// calls.
const (
	lenNilMap     = len([1]map[int]int{nil})
	lenNilChan    = len([1]chan int{nil})
	lenNilMapConv = len([1]map[int]int{map[int]int(nil)})
	lenNilField   = len([1]struct{ m map[int]int }{{m: nil}})
	lenNilStruct  = len([1]lenFields{{M: nil, C: nil}})
	lenNilPos     = len([1]lenFields{{nil, nil}})
	lenParamLit   = len([1]func(int) int{func(v int) int { return v + lenSource[0] }})
)

type lenFields struct {
	M map[int]int
	C chan int
}

// lenFuncLitLocal takes len of an array of a function literal, with a
// variable of its own, and a global.
func lenFuncLitLocal() int {
	return len([2]any{func() { x := 1; _ = x }, lenSource[0]})
}

func TestConstantLenOperands(t *testing.T) {
	if got := []int{lenNilDeref(), lenConstWithLocal(), capUnderLocalLen(), lenNilMap, lenNilChan, lenNilMapConv, lenNilField, lenFuncLitLocal(), lenNilStruct, lenNilPos, lenParamLit}; !slices.Equal(got, []int{2, 2, 45, 1, 1, 1, 1, 2, 1, 1, 1}) {
		t.Errorf("got %v", got)
	}
}

// initDups copies stdout and stderr in an initializer, as CockroachDB's
// logging does, closes one copy and copies again, then closes the rest.
// gosim runs initializers once outside any machine too, where a panic here
// ends the run.
var initDups = dupInInit()

func dupInInit() []int {
	a, errA := unix.FcntlInt(1, unix.F_DUPFD_CLOEXEC, 3)
	b, errB := unix.FcntlInt(2, unix.F_DUPFD_CLOEXEC, 3)
	if errA != nil || errB != nil || a == b {
		panic(fmt.Sprintf("dups in an initializer: %d, %v and %d, %v", a, errA, b, errB))
	}
	if err := unix.Close(a); err != nil {
		panic(fmt.Sprintf("closing a dup in an initializer: %v", err))
	}
	c, errC := unix.FcntlInt(1, unix.F_DUPFD_CLOEXEC, 3)
	if errC != nil || c == b {
		panic(fmt.Sprintf("dup after close in an initializer: %d, %v", c, errC))
	}
	// Leave no copy open, for TestDupMinimum.
	unix.Close(b)
	unix.Close(c)
	return []int{a, b, c}
}

func TestDupInInit(t *testing.T) {
	if len(initDups) != 3 {
		t.Errorf("got %v", initDups)
	}
}
