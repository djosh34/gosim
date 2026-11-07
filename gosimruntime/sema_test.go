package gosimruntime

import (
	"io"
	"runtime"
	"testing"
	"weak"
)

// TestSemaphoreLeavesNoRunBehind makes a semaphore on memory of a run and
// leaves it without waiters, as a crashed machine leaves one. Once the run
// ends, its memory must be garbage: the entry and the entries free for
// reuse belong to the run (tigerstore #531).
func TestSemaphoreLeavesNoRunBehind(t *testing.T) {
	var ref weak.Pointer[[1 << 20]uint32]
	result := run(func() {
		globals := new([1 << 20]uint32)
		ref = weak.Make(globals)
		getsema(&globals[0], true)
		// One more entry, freed to the pool, then taken again.
		getsema(&globals[1], true)
		freesema(&globals[1])
		getsema(&globals[2], true)
	}, 1, false, false, "INFO", io.Discard, nil)
	if result.Failed {
		t.Fatalf("run failed: %v", result.Err)
	}
	for range 3 {
		runtime.GC()
	}
	if ref.Value() != nil {
		t.Fatal("a semaphore of the run kept its memory after the run")
	}
}
