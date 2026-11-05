package gosimruntime

import (
	"bytes"
	"maps"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/jellevandenhooff/gosim/internal/race"
)

// A run started from metatesting can have a limit of real time
// (RunConfig.WallLimit). The simulation reads it with WallLimitPassed, at
// points of its own choosing, and stops itself, as with any failure it
// reports: real time never changes what a run does before its limit. A run
// whose goroutine never yields gets to no such point, so a second limit
// (RunConfig.WallKill) ends the whole test binary, after it reported the
// run with what it saved and logged so far and every goroutine's stack.

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

// startWallLimit sets the limits of real time of a run that starts now.
// When kill passes, kill calls ended, which must not return, as the run
// goes on in another goroutine. The returned stop clears the limits once
// the run ended; it waits for an ended that already started, so that no
// later run starts before the binary ends.
func startWallLimit(limit, kill time.Duration, ended func()) (stop func()) {
	if limit > 0 {
		wallDeadline.Store(time.Now().Add(limit).UnixNano())
	}
	var mu sync.Mutex
	stopped := false
	var timer *time.Timer
	if kill > 0 {
		timer = time.AfterFunc(kill, func() {
			mu.Lock()
			if !stopped {
				ended()
			}
			mu.Unlock()
		})
	}
	return func() {
		mu.Lock()
		stopped = true
		mu.Unlock()
		wallDeadline.Store(0)
		if timer != nil {
			timer.Stop()
		}
	}
}

// soFarMu guards the artifacts and the log of the current run, which a
// kill reads while the run may still write them, and killable, the
// scheduler of the current run, which run sets under it so that a kill
// sees the scheduler run made.
var (
	soFarMu  sync.Mutex
	killable *scheduler
	// killEdge orders the copies WriteArtifact makes before a kill's
	// read of them, for the race detector.
	killEdge byte
)

// setKillable makes s the run whose artifacts and log a kill reports.
func setKillable(s *scheduler) {
	soFarMu.Lock()
	defer soFarMu.Unlock()
	killable = s
}

// lockedBuffer is a run's log, written under soFarMu.
type lockedBuffer struct {
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	soFarMu.Lock()
	defer soFarMu.Unlock()
	return b.buf.Write(p)
}

// bytes returns a copy of the log so far.
func (b *lockedBuffer) bytes() []byte {
	soFarMu.Lock()
	defer soFarMu.Unlock()
	return bytes.Clone(b.buf.Bytes())
}

// soFar returns what the current run saved with WriteArtifact and logged
// so far, for the report of a run that never yields.
func soFar() (map[string][]byte, []byte) {
	soFarMu.Lock()
	defer soFarMu.Unlock()
	// Under the lock: every artifact the lock lets us see was copied and
	// released before it was stored, so this acquire orders each copy
	// before the read.
	race.Acquire(unsafe.Pointer(&killEdge))
	s := killable
	if s == nil {
		return nil, nil
	}
	var log []byte
	if s.logBuffer != nil {
		log = bytes.Clone(s.logBuffer.buf.Bytes())
	}
	return maps.Clone(s.artifacts), log
}
