package gosimruntime

import (
	"testing"
	"time"
)

// TestCPUTimeCounts spins for 20 ms of real time and checks that CPUTime
// counted at least 10 ms of it, in nanoseconds.
func TestCPUTimeCounts(t *testing.T) {
	before, err := CPUTime()
	if err != nil {
		t.Fatal(err)
	}
	for start := time.Now(); time.Since(start) < 20*time.Millisecond; {
	}
	after, err := CPUTime()
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Duration(after - before); d < 10*time.Millisecond {
		t.Errorf("20 ms of spinning took %v of CPU time", d)
	}
}
