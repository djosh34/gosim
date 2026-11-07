package gosimruntime

import (
	"io"
	"runtime"
	"testing"
	"weak"
)

// Aborting a waiter empties its queue, but its semaphore is only lazily
// removed. Its address must not keep the machine's globals alive across runs.
func TestSemaphoreDoesNotRetainRun(t *testing.T) {
	// Force pool reuse too: a popped slot in its backing array must not
	// keep the reused entry (and its new address) alive after this run.
	semaPool = append(semaPool, &sema{})
	var ref weak.Pointer[[1 << 20]uint32]
	result := run(func() {
		globals := new([1 << 20]uint32)
		ref = weak.Make(globals)
		getsema(&globals[0], true)
	}, 1, false, false, "INFO", io.Discard, nil)
	if result.Failed {
		t.Fatalf("run failed: %v", result.Err)
	}
	for range 3 {
		runtime.GC()
	}
	if ref.Value() != nil {
		t.Fatal("an empty semaphore retained the previous run's allocation")
	}
}
