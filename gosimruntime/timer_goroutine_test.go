package gosimruntime

import (
	"os"
	"slices"
	"testing"
)

// TestTimersShareAGoroutine checks that the calls of a machine's timers
// that fired all run, on one goroutine, which waits between them: a
// firing starts no goroutine of its own. The calls of timers that fire at
// the same instant run in an order the run picks, so some pairs run in
// the other order than they fired.
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
					ids[GetGoroutine()] = true
				}, m)
			}, nil, m, when)
		}
		Sleep(int64(timers) * 1000)
	}
	run(f, 1, false, false, "INFO", makeConsoleLogger(os.Stderr), nil)

	if len(order) != timers {
		t.Fatalf("%d of %d timer calls ran", len(order), timers)
	}
	if !slices.Equal(slices.Sorted(slices.Values(order)), slices.Sorted(slices.Values(fired))) {
		t.Fatalf("timers fired in the order %v, and their calls ran in the order %v", fired, order)
	}
	// A pair's calls run after the pair fired and before the next pair's.
	swapped := false
	for i := 0; i < timers; i += 2 {
		a, b := order[i]/2, order[i+1]/2
		if a != i/2 || b != i/2 {
			t.Fatalf("the calls of the timers of instant %d ran in the order %v", i/2, order)
		}
		swapped = swapped || order[i] != fired[i]
	}
	if !swapped {
		t.Errorf("every pair's calls ran in the order the pair fired: %v", order)
	}
	if len(ids) != 1 {
		t.Errorf("the timer calls ran on %d goroutines, want 1", len(ids))
	}
	expectNumGoroutines(t, "end")
}
