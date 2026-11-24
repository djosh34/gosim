package gosimruntime

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeCPU makes the limit read its CPU time from the returned counter, in
// nanoseconds, and check it every millisecond, until the test ends.
func fakeCPU(t *testing.T) *atomic.Int64 {
	var ns atomic.Int64
	oldTime, oldInterval := cpuTime, cpuCheckInterval
	cpuTime = func() (int64, error) { return ns.Load(), nil }
	cpuCheckInterval = time.Millisecond
	t.Cleanup(func() { cpuTime, cpuCheckInterval = oldTime, oldInterval })
	return &ns
}

// TestCPULimitPassed checks that a run that uses its limit of CPU time is
// killed, and not before.
func TestCPULimitPassed(t *testing.T) {
	ns := fakeCPU(t)
	killed := make(chan string, 1)
	stop := startCPULimit(time.Second, func(why string) { killed <- why })
	defer stop()
	ns.Store(int64(999 * time.Millisecond))
	select {
	case <-killed:
		t.Fatal("killed before the limit")
	case <-time.After(50 * time.Millisecond):
	}
	ns.Store(int64(time.Second))
	select {
	case why := <-killed:
		if want := "passed its limit of 1s of CPU time"; why != want {
			t.Errorf("killed because %q, want %q", why, want)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("not killed after the limit")
	}
}

// TestCPULimitStarved checks that a run whose process uses no CPU time,
// as on a stalled machine, is not killed however long it takes.
func TestCPULimitStarved(t *testing.T) {
	ns := fakeCPU(t)
	ns.Store(int64(time.Hour))
	killed := make(chan struct{})
	stop := startCPULimit(time.Second, func(string) { close(killed) })
	select {
	case <-killed:
		t.Fatal("a starved run was killed")
	case <-time.After(200 * time.Millisecond):
	}
	if past := stop(); past != "" {
		t.Errorf("a starved run is past its limit: %s", past)
	}
}

// TestCPULimitSleepingRun reads the real CPU time of the process: a run that
// takes ten times its limit of CPU time in real time, but uses little CPU,
// is neither killed nor past its limit when it ends. A limit of real time
// would have killed it.
func TestCPULimitSleepingRun(t *testing.T) {
	old := cpuCheckInterval
	cpuCheckInterval = 10 * time.Millisecond
	defer func() { cpuCheckInterval = old }()
	killed := make(chan struct{})
	stop := startCPULimit(100*time.Millisecond, func(string) { close(killed) })
	select {
	case <-killed:
		t.Fatal("a sleeping run was killed")
	case <-time.After(time.Second):
	}
	if past := stop(); past != "" {
		t.Errorf("a sleeping run is past its limit: %s", past)
	}
}

// TestCPULimitPastAtStop checks that a run that passes its limit between
// two checks is past it when it ends.
func TestCPULimitPastAtStop(t *testing.T) {
	ns := fakeCPU(t)
	cpuCheckInterval = time.Hour
	stop := startCPULimit(time.Second, func(string) { t.Error("killed between checks") })
	ns.Store(int64(time.Second))
	if past, want := stop(), "passed its limit of 1s of CPU time"; past != want {
		t.Errorf("past its limit: %q, want %q", past, want)
	}
}

// TestCPULimitReadError checks that a run whose CPU time cannot be read is
// killed with the error rather than left without a limit.
func TestCPULimitReadError(t *testing.T) {
	fakeCPU(t)
	var fail atomic.Bool
	cpuTime = func() (int64, error) {
		if fail.Load() {
			return 0, errors.New("no rusage")
		}
		return 0, nil
	}
	killed := make(chan string, 1)
	stop := startCPULimit(time.Second, func(why string) { killed <- why })
	defer stop()
	fail.Store(true)
	select {
	case why := <-killed:
		if !strings.Contains(why, "its CPU time could not be read: no rusage") {
			t.Errorf("killed because %q", why)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("not killed when the CPU time could not be read")
	}
}

// TestCPUStopWaitsForKill checks that stop does not return while the kill
// of the run it stops is under way, so that no later run starts before the
// binary ends.
func TestCPUStopWaitsForKill(t *testing.T) {
	ns := fakeCPU(t)
	started, release, stopped := make(chan struct{}), make(chan struct{}), make(chan struct{})
	stop := startCPULimit(time.Second, func(string) {
		close(started)
		<-release
	})
	ns.Store(int64(time.Second))
	<-started
	go func() {
		stop()
		close(stopped)
	}()
	select {
	case <-stopped:
		t.Fatal("stop returned while the kill was under way")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	<-stopped
}

// TestCPUStopBeforeKill checks that a run stopped before its limit passed
// is not killed afterwards.
func TestCPUStopBeforeKill(t *testing.T) {
	ns := fakeCPU(t)
	killed := make(chan struct{})
	stop := startCPULimit(time.Second, func(string) { close(killed) })
	if past := stop(); past != "" {
		t.Errorf("past its limit at stop: %s", past)
	}
	ns.Store(int64(time.Hour))
	select {
	case <-killed:
		t.Fatal("a stopped run was killed")
	case <-time.After(50 * time.Millisecond):
	}
}
