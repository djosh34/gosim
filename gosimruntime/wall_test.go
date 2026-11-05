package gosimruntime

import (
	"fmt"
	"sync"
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

// TestSoFarWhileWriting takes what a run saved and logged so far while the
// run goes on saving and logging, as a kill does.
func TestSoFarWhileWriting(t *testing.T) {
	s := &scheduler{logBuffer: &lockedBuffer{}}
	defer gs.set(gs.get())
	gs.set(s)
	stop, writing := make(chan struct{}), make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := 0; ; i++ {
			if i == 1 {
				close(writing)
			}
			select {
			case <-stop:
				return
			default:
			}
			s.setArtifact(fmt.Sprint(i%64), []byte("x"))
			if _, err := fmt.Fprintf(s.logBuffer, "line %d\n", i); err != nil {
				t.Error(err)
				return
			}
		}
	})
	<-writing
	for range 1000 {
		soFar()
	}
	close(stop)
	wg.Wait()
	artifacts, log := soFar()
	if len(artifacts) == 0 || len(log) == 0 {
		t.Errorf("got %d artifacts and %d bytes of log", len(artifacts), len(log))
	}
}
