package gosimruntime

import (
	"sync"
	"time"
)

// A run started from metatesting can also have a limit of CPU time
// (RunConfig.CPUKill): the CPU time the process uses from the start of the
// run, which, as runs in a test binary do not overlap, is the run's own. A
// run that spins uses CPU time and passes the limit; a run whose process is
// starved, as on a stalled machine, uses little and does not, so that only
// its limit of real time can end it. A run that passes CPUKill ends as one
// that passes WallKill does.

// cpuCheckInterval is how often a run's CPU time is checked.
var cpuCheckInterval = time.Second

// cpuTime reads the CPU time of the process, in nanoseconds.
var cpuTime = CPUTime

// startCPULimit sets the limit of CPU time of a run that starts now. When
// the process has used kill of CPU time since, it calls ended, which must
// not return, as the run goes on in another goroutine. The returned stop
// clears the limit once the run ended; it waits for an ended that already
// started, so that no later run starts before the binary ends.
func startCPULimit(kill time.Duration, ended func()) (stop func()) {
	if kill <= 0 {
		return func() {}
	}
	read, interval := cpuTime, cpuCheckInterval
	start, err := read()
	if err != nil {
		panic(err)
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
			if now, err := read(); err != nil || time.Duration(now-start) < kill {
				continue
			}
			mu.Lock()
			if !stopped {
				ended()
			}
			mu.Unlock()
			return
		}
	}()
	return func() {
		mu.Lock()
		stopped = true
		mu.Unlock()
		close(done)
	}
}
