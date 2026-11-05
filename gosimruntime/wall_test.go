package gosimruntime

import (
	"testing"
	"time"
)

// TestWallStopWaitsForKill checks that stop does not return while the
// kill of the run it stops is under way, so that no later run starts
// before the binary ends.
func TestWallStopWaitsForKill(t *testing.T) {
	started, release, stopped := make(chan struct{}), make(chan struct{}), make(chan struct{})
	stop := startWallLimit(0, time.Millisecond, func() {
		close(started)
		<-release
	})
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

// TestWallStopBeforeKill checks that a kill whose run was stopped first
// does nothing.
func TestWallStopBeforeKill(t *testing.T) {
	killed := make(chan struct{})
	stop := startWallLimit(time.Hour, 10*time.Millisecond, func() { close(killed) })
	stop()
	if WallLimitPassed() {
		t.Error("the limit holds after stop")
	}
	select {
	case <-killed:
		t.Fatal("a stopped run was killed")
	case <-time.After(50 * time.Millisecond):
	}
}
