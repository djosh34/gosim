package gosimruntime

import (
	"os"
	"slices"
	"testing"
)

// TestTimersShareAGoroutine checks that the calls of a machine's timers
// that fired run in the order they fired, all on one goroutine, which
// waits between them: a firing starts no goroutine of its own.
func TestTimersShareAGoroutine(t *testing.T) {
	const timers = 50
	var fired, order []int
	ids := map[int]bool{}
	f := func() {
		m := CurrentMachine()
		start := gs.get().clock.now
		for i := range timers {
			// Each pair of timers fires at the same instant.
			when := start + int64(1+i/2)*1000
			NewTimer(func(*Timer) {
				fired = append(fired, i)
				RunFromTimerHandler(func() {
					order = append(order, i)
					ids[getg().ID] = true
				}, m)
			}, nil, m, when)
		}
		Sleep(int64(timers) * 1000)
	}
	run(f, 1, false, false, "INFO", makeConsoleLogger(os.Stderr), nil)

	if len(order) != timers {
		t.Fatalf("%d of %d timer calls ran", len(order), timers)
	}
	if !slices.Equal(order, fired) {
		t.Fatalf("timers fired in the order %v, and their calls ran in the order %v", fired, order)
	}
	if len(ids) != 1 {
		t.Errorf("the timer calls ran on %d goroutines, want 1", len(ids))
	}
	expectNumGoroutines(t, "end")
}
