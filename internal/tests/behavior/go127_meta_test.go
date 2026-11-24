//go:build !sim

package behavior_test

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jellevandenhooff/gosim/internal/gosimtool"
	"github.com/jellevandenhooff/gosim/metatesting"
)

func TestWriteArtifact(t *testing.T) {
	mt := metatesting.ForCurrentPackage(t)
	run, err := mt.RunQuiet(&metatesting.RunConfig{Test: "TestWriteArtifact", Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if run.Failed || string(run.Artifacts["artifact.txt"]) != "hello" {
		t.Errorf("got failed %v, artifacts %q", run.Failed, run.Artifacts)
	}
}

func TestWallLimit(t *testing.T) {
	mt := metatesting.ForCurrentPackage(t)
	run, err := mt.RunQuiet(&metatesting.RunConfig{Test: "TestWallLimit", Seed: 1, ExtraEnv: []string{"TESTINGWALL=1"}, WallLimit: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if run.Failed || !strings.Contains(string(run.Artifacts["stacks"]), "TestWallLimit") {
		t.Errorf("got failed %v, stacks %q", run.Failed, run.Artifacts["stacks"])
	}
	// The limit ends with its run.
	run, err = mt.RunQuiet(&metatesting.RunConfig{Test: "TestWriteArtifact", Seed: 1})
	if err != nil || run.Failed {
		t.Errorf("the next run: %v, failed %v", err, run != nil && run.Failed)
	}
}

func TestWallKill(t *testing.T) {
	mt := metatesting.ForCurrentPackage(t)
	run, err := mt.RunQuiet(&metatesting.RunConfig{Test: "TestWallKill", Seed: 1, ExtraEnv: []string{"TESTINGWALL=1"}, WallKill: 200 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if !run.Failed || !run.Ended || string(run.Artifacts["before.txt"]) != "saved" || !strings.Contains(string(run.Artifacts["gosim-stacks.txt"]), "TestWallKill") || !strings.Contains(run.Err, "TestWallKill with seed 1") {
		t.Errorf("got failed %v, ended %v, err %q, artifacts %q", run.Failed, run.Ended, run.Err, slices.Sorted(maps.Keys(run.Artifacts)))
	}
	// The next run starts a new test binary.
	run, err = metatesting.ForCurrentPackage(t).RunQuiet(&metatesting.RunConfig{Test: "TestWriteArtifact", Seed: 1})
	if err != nil || run.Failed || string(run.Artifacts["artifact.txt"]) != "hello" {
		t.Errorf("the next run: %v, %+v", err, run)
	}
}

// TestCPUKill runs the spinning TestWallKill with a limit of CPU time
// well before its limit of real time: the CPU limit ends it as WallKill
// would, with what it saved and every goroutine's stack.
func TestCPUKill(t *testing.T) {
	mt := metatesting.ForCurrentPackage(t)
	run, err := mt.RunQuiet(&metatesting.RunConfig{Test: "TestWallKill", Seed: 1, ExtraEnv: []string{"TESTINGWALL=1"}, CPUKill: 2 * time.Second, WallKill: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if !run.Failed || !run.Ended || string(run.Artifacts["before.txt"]) != "saved" || !strings.Contains(string(run.Artifacts["gosim-stacks.txt"]), "TestWallKill") || !strings.Contains(run.Err, "TestWallKill with seed 1 passed its limit of 2s of CPU time;") {
		t.Errorf("got failed %v, ended %v, err %q, artifacts %q", run.Failed, run.Ended, run.Err, slices.Sorted(maps.Keys(run.Artifacts)))
	}
	// A run that ends well within its limit of CPU time passes, and a new
	// test binary runs it.
	mt = metatesting.ForCurrentPackage(t)
	run, err = mt.RunQuiet(&metatesting.RunConfig{Test: "TestWriteArtifact", Seed: 1, CPUKill: time.Hour})
	if err != nil || run.Failed || string(run.Artifacts["artifact.txt"]) != "hello" {
		t.Errorf("the next run: %v, %+v", err, run)
	}
	// TestCPUTime uses some CPU time and ends before the limit is checked:
	// it fails as it ends, past its limit, and the binary goes on.
	run, err = mt.RunQuiet(&metatesting.RunConfig{Test: "TestCPUTime", Seed: 1, CPUKill: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	if !run.Failed || run.Ended || !strings.Contains(run.Err, "TestCPUTime with seed 1 passed its limit of 1ns of CPU time") {
		t.Errorf("a run past its limit as it ends: failed %v, ended %v, err %q", run.Failed, run.Ended, run.Err)
	}
}

// TestCPUKillStarved stops the process of a run that uses 2 s of CPU time
// for 6 s of real time, past its limit of 4 s of CPU time. A starved run
// uses no CPU time while it waits, so it is not killed, as a limit of real
// time would kill it, and it passes.
func TestCPUKillStarved(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("finds and stops the test binary through /proc")
	}
	mt := metatesting.ForCurrentPackage(t)
	var run *metatesting.RunResult
	var err error
	done := make(chan struct{})
	go func() {
		defer close(done)
		run, err = mt.RunQuiet(&metatesting.RunConfig{Test: "TestCPUFor", Seed: 1, ExtraEnv: []string{"TESTINGCPUFOR=1"}, CPUKill: 4 * time.Second, WallKill: time.Hour})
	}()
	// Stop the test binary whose run spins.
	pid := spinningChild(t, done)
	if err := syscall.Kill(pid, syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	time.Sleep(6 * time.Second)
	select {
	case <-done:
		t.Fatal("the run ended before its process was stopped")
	default:
	}
	if err := syscall.Kill(pid, syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}
	<-done
	if err != nil || run.Failed || run.Ended {
		t.Errorf("a starved run: %v, %+v", err, run)
	}
}

// procStat returns the fields of /proc/<pid>/stat after the command, which
// is in parentheses: the state, the parent's pid, and so on.
func procStat(pid int) []string {
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return nil
	}
	return strings.Fields(string(stat[bytes.LastIndexByte(stat, ')')+1:]))
}

// procCPU returns the CPU time the process pid used so far.
func procCPU(t *testing.T, pid int) time.Duration {
	f := procStat(pid)
	if len(f) < 13 {
		t.Fatalf("no process %d", pid)
	}
	// utime and stime, in clock ticks of 1/100 s.
	utime, _ := strconv.ParseInt(f[11], 10, 64)
	stime, _ := strconv.ParseInt(f[12], 10, 64)
	return time.Duration(utime+stime) * 10 * time.Millisecond
}

// spinningChild waits for a child of this process that is a metatest
// binary and has used 500 ms of CPU time since the wait began: the one
// whose run spins, not an idle binary another test left. It fails the test
// if done closes first.
func spinningChild(t *testing.T, done <-chan struct{}) int {
	self := strconv.Itoa(os.Getpid())
	start := make(map[int]time.Duration)
	for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		select {
		case <-done:
			t.Fatal("the run ended before its process was stopped")
		default:
		}
		procs, err := os.ReadDir("/proc")
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range procs {
			pid, err := strconv.Atoi(p.Name())
			if err != nil {
				continue
			}
			f := procStat(pid)
			if len(f) < 2 || f[0] == "Z" || f[1] != self {
				continue
			}
			if cmd, err := os.ReadFile(filepath.Join("/proc", p.Name(), "cmdline")); err != nil || !bytes.Contains(cmd, []byte("-metatest")) {
				continue
			}
			cpu := procCPU(t, pid)
			if s, ok := start[pid]; !ok {
				start[pid] = cpu
			} else if cpu-s >= 500*time.Millisecond {
				return pid
			}
		}
	}
	t.Fatal("no metatest binary under this process spins")
	return 0
}

func TestReflectLinkname(t *testing.T) {
	mt := metatesting.ForCurrentPackage(t)
	run, err := mt.RunQuiet(&metatesting.RunConfig{Test: "TestReflectLinkname", Seed: 1, ExtraEnv: []string{"TESTINGFAIL=1"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "unimplemented hook: github.com/jellevandenhooff/gosim/internal/tests/behavior/linkname.unsafeNew"; !run.Failed || run.Err != want {
		t.Errorf("got failed %v, err %q; want err %q", run.Failed, run.Err, want)
	}
}

func TestUnimplementedHook(t *testing.T) {
	mt := metatesting.ForCurrentPackage(t)
	run, err := mt.RunQuiet(&metatesting.RunConfig{Test: "TestUnimplementedHook", Seed: 1, ExtraEnv: []string{"TESTINGFAIL=1"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "unimplemented hook: reflect.MapOf"; !run.Failed || run.Err != want {
		t.Errorf("got failed %v, err %q; want err %q", run.Failed, run.Err, want)
	}
}

func TestFatalUnlock(t *testing.T) {
	mt := metatesting.ForCurrentPackage(t)
	run, err := mt.RunQuiet(&metatesting.RunConfig{Test: "TestFatalUnlock", Seed: 1, ExtraEnv: []string{"TESTINGFAIL=1"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "fatal error: sync: unlock of unlocked mutex"; !run.Failed || run.Err != want {
		t.Errorf("got failed %v, err %q; want err %q", run.Failed, run.Err, want)
	}
}

func TestEmbedBytesPerRun(t *testing.T) {
	mt := metatesting.ForCurrentPackage(t)
	for range 2 {
		run, err := mt.RunQuiet(&metatesting.RunConfig{Test: "TestEmbedBytesPerRun", Seed: 1})
		if err != nil {
			t.Fatal(err)
		}
		if run.Failed {
			t.Errorf("a run failed: %s", run.LogOutput)
		}
	}
}

func TestMaphashComparable(t *testing.T) {
	mt := metatesting.ForCurrentPackage(t)
	run, err := mt.RunQuiet(&metatesting.RunConfig{Test: "TestMaphashComparable", Seed: 1, ExtraEnv: []string{"TESTINGFAIL=1"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "unimplemented hook: hash/maphash.comparableHashUnimplemented"; !run.Failed || run.Err != want {
		t.Errorf("got failed %v, err %q; want err %q\n%s", run.Failed, run.Err, want, run.LogOutput)
	}
}

func TestForTestBinary(t *testing.T) {
	// A copy of the binary elsewhere, as a campaign's jobs download it.
	built := gosimtool.GetPathForPrecompiledTestBinary(t, "github.com/jellevandenhooff/gosim/internal/tests/behavior", nil)
	data, err := os.ReadFile(built)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "behavior.test")
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatal(err)
	}
	run, err := metatesting.ForTestBinary(t, path).RunQuiet(&metatesting.RunConfig{Test: "TestWriteArtifact", Seed: 1})
	if err != nil || run.Failed || string(run.Artifacts["artifact.txt"]) != "hello" {
		t.Errorf("got %v, %+v", err, run)
	}
}

func TestExitNonzero(t *testing.T) {
	run, err := metatesting.ForCurrentPackage(t).RunQuiet(&metatesting.RunConfig{Test: "TestExitNonzero", Seed: 1, ExtraEnv: []string{"TESTINGFAIL=1"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "os.Exit(3) on the main machine"; !run.Failed || !strings.Contains(run.Err, want) {
		t.Errorf("got failed %v, err %q; want err with %q", run.Failed, run.Err, want)
	}
}

func TestPanicLog(t *testing.T) {
	mt := metatesting.ForCurrentPackage(t)
	var checksums []string
	for range 2 {
		run, err := mt.RunQuiet(&metatesting.RunConfig{Test: "TestPanicLog", Seed: 1, ExtraEnv: []string{"TESTINGFAIL=1"}})
		if err != nil {
			t.Fatal(err)
		}
		if !run.Failed || !strings.Contains(string(run.LogOutput), "a panic for the log") {
			t.Fatalf("got failed %v, log %s", run.Failed, run.LogOutput)
		}
		checksums = append(checksums, string(run.Checksum))
	}
	if checksums[0] != checksums[1] {
		t.Errorf("two runs of a panic gave different checksums")
	}
}
