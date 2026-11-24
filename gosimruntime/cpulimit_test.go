package gosimruntime

import (
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
// killed.
func TestCPULimitPassed(t *testing.T) {
	ns := fakeCPU(t)
	killed := make(chan struct{})
	stop := startCPULimit(time.Second, func() { close(killed) })
	defer stop()
	ns.Store(int64(999 * time.Millisecond))
	select {
	case <-killed:
		t.Fatal("killed before the limit")
	case <-time.After(50 * time.Millisecond):
	}
	ns.Store(int64(time.Second))
	select {
	case <-killed:
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
	stop := startCPULimit(time.Second, func() { close(killed) })
	defer stop()
	select {
	case <-killed:
		t.Fatal("a starved run was killed")
	case <-time.After(200 * time.Millisecond):
	}
}

// TestCPUStopWaitsForKill checks that stop does not return while the kill
// of the run it stops is under way, so that no later run starts before the
// binary ends.
func TestCPUStopWaitsForKill(t *testing.T) {
	ns := fakeCPU(t)
	started, release, stopped := make(chan struct{}), make(chan struct{}), make(chan struct{})
	stop := startCPULimit(time.Second, func() {
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
	stop := startCPULimit(time.Second, func() { close(killed) })
	stop()
	ns.Store(int64(time.Hour))
	select {
	case <-killed:
		t.Fatal("a stopped run was killed")
	case <-time.After(50 * time.Millisecond):
	}
}
