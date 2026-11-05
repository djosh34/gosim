package gosimruntime

import (
	"fmt"
	"os"
	"runtime"
	"sync/atomic"
	"time"
)

// A run started from metatesting can have a limit of real time
// (RunConfig.WallLimit). The simulation reads it with WallLimitPassed, at
// points of its own choosing, and stops itself, as with any failure it
// reports: real time never changes what a run does before its limit. A run
// whose goroutine never yields gets to no such point, so a second limit
// (RunConfig.WallKill) ends the whole process, after it wrote every
// goroutine's stack to stderr.

// wallDeadline is when the current run's limit of real time passes, as
// Unix nanoseconds, or 0 for no limit.
var wallDeadline atomic.Int64

// WallLimitPassed reports whether the current run has a limit of real time
// that has passed.
func WallLimitPassed() bool {
	d := wallDeadline.Load()
	return d != 0 && time.Now().UnixNano() > d
}

// Stacks returns the stack of every goroutine of the process, the
// simulation's included.
func Stacks() []byte {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return buf[:n]
		}
		buf = make([]byte, 2*len(buf))
	}
}

// startWallLimit sets the limits of real time of a run that starts now. It
// returns a function that clears them once the run ended.
func startWallLimit(limit, kill time.Duration) (stop func()) {
	now := time.Now()
	if limit > 0 {
		wallDeadline.Store(now.Add(limit).UnixNano())
	}
	var timer *time.Timer
	if kill > 0 {
		timer = time.AfterFunc(kill, func() {
			fmt.Fprintf(os.Stderr, "gosim: the run passed its limit of %v of real time; the stack of every goroutine:\n%s\n", kill, Stacks())
			os.Exit(2)
		})
	}
	return func() {
		wallDeadline.Store(0)
		if timer != nil {
			timer.Stop()
		}
	}
}
