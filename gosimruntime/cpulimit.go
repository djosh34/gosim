package gosimruntime

import (
	"fmt"
	"sync"
	"time"
)

// A run started from metatesting can also have a limit of CPU time
// (RunConfig.CPUKill): the CPU time the process uses from the start of the
// run, which, as runs in a test binary do not overlap, is the run's own. A
// run that spins uses CPU time and passes the limit; a run whose process is
// starved, as on a stalled machine, uses little and does not, so that only
// its limit of real time can end it. A run that passes CPUKill while it
// runs ends as one that passes WallKill does; one that passes it in its
// last moments, between two checks, fails when it ends.

// cpuCheckInterval is how often a run's CPU time is checked.
var cpuCheckInterval = time.Second

// cpuTime reads the CPU time of the process, in nanoseconds.
var cpuTime = CPUTime

// startCPULimit sets the limit of CPU time of a run that starts now. When
// the process has used kill of CPU time since, or its CPU time cannot be
// read, it calls ended with why, which must not return, as the run goes on
// in another goroutine. The returned stop clears the limit once the run
// ended and reports why the run is past its limit, or ""; it waits for an
// ended that already started, so that no later run starts before the
// binary ends.
func startCPULimit(kill time.Duration, ended func(why string)) (stop func() (past string)) {
	if kill <= 0 {
		return func() string { return "" }
	}
	read, interval := cpuTime, cpuCheckInterval
	start, startErr := read()
	// past checks the run's CPU time now.
	past := func() string {
		if startErr != nil {
			return fmt.Sprintf("its CPU time could not be read: %v", startErr)
		}
		now, err := read()
		if err != nil {
			return fmt.Sprintf("its CPU time could not be read: %v", err)
		}
		if time.Duration(now-start) >= kill {
			return fmt.Sprintf("passed its limit of %v of CPU time", kill)
		}
		return ""
	}
	var mu sync.Mutex
	stopped := false
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
			}
			why := past()
			if why == "" {
				continue
			}
			mu.Lock()
			if !stopped {
				ended(why)
			}
			mu.Unlock()
			return
		}
	}()
	return func() string {
		mu.Lock()
		stopped = true
		mu.Unlock()
		close(done)
		return past()
	}
}
