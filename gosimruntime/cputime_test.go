package gosimruntime

import (
	"testing"
	"time"
)

// TestCPUTimeCounts spins until CPUTime counted 10 ms, and checks that
// that took at least 5 ms of real time and at most 5 s: one goroutine
// spins, so CPU time cannot run much ahead of real time. CPU time read in
// the wrong unit, 1000 times too much or too little, fails one of them.
// No share of the CPU is needed, so a starved process passes too.
func TestCPUTimeCounts(t *testing.T) {
	before, err := CPUTime()
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	for time.Since(start) < 5*time.Second {
		now, err := CPUTime()
		if err != nil {
			t.Fatal(err)
		}
		if time.Duration(now-before) >= 10*time.Millisecond {
			if took := time.Since(start); took < 5*time.Millisecond {
				t.Errorf("10 ms of CPU time took %v of real time", took)
			}
			return
		}
	}
	t.Error("5 s of spinning did not count 10 ms of CPU time")
}
