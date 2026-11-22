package gosimruntime

import (
	"runtime"
	"runtime/metrics"
	"unsafe"

	"github.com/jellevandenhooff/gosim/internal/race"
)

// The functions in this file stand in for the parts of package runtime and
// runtime/metrics that report the state of the real Go runtime: the
// garbage collector, the heap, the real goroutines and the host's CPUs. That
// state differs between two runs of the same seed, and a program that reads
// it would take different paths. The simulation reports fixed values
// instead, or values it derives from its own state.

// SimCPUs is the number of CPUs a simulated program sees, whatever the host
// has, so that a seed runs the same on any host.
const SimCPUs = 2

// NumCPU stands in for runtime.NumCPU.
func NumCPU() int {
	return SimCPUs
}

// GOMAXPROCS stands in for runtime.GOMAXPROCS. The simulation runs one
// goroutine at a time, so a setting changes nothing; it always reports
// SimCPUs.
func GOMAXPROCS(n int) int {
	return SimCPUs
}

// NumGoroutine stands in for runtime.NumGoroutine: the number of goroutines
// of the calling goroutine's machine.
func NumGoroutine() int {
	g := getg()
	n := 0
	// The scheduler counts in the upcall; the race detector learns that
	// the count passes from this goroutine to the scheduler and back.
	race.Release(unsafe.Pointer(&n))
	g.upcall(func() {
		race.Acquire(unsafe.Pointer(&n))
		for _, other := range gs.get().goroutines {
			if other.machine == g.machine {
				n++
			}
		}
		race.Release(unsafe.Pointer(&n))
	})
	race.Acquire(unsafe.Pointer(&n))
	return n
}

// ReadMemStats stands in for runtime.ReadMemStats. It reports a heap that
// is empty and a collector that never ran.
func ReadMemStats(m *runtime.MemStats) {
	*m = runtime.MemStats{}
}

// metricsValue has the layout of runtime/metrics.Value, whose fields are
// not exported. TestReadMetrics checks that the two agree.
type metricsValue struct {
	kind    metrics.ValueKind
	scalar  uint64
	pointer unsafe.Pointer
}

// ReadMetrics stands in for runtime/metrics.Read. Every metric reads as
// zero, of the kind the real runtime gives it: a histogram keeps the real
// runtime's buckets, which are fixed for a Go version, with every count
// zero. A metric the runtime does not know reads as metrics.KindBad, as it
// does on the real runtime.
func ReadMetrics(samples []metrics.Sample) {
	metrics.Read(samples)
	for i := range samples {
		v := (*metricsValue)(unsafe.Pointer(&samples[i].Value))
		switch v.kind {
		case metrics.KindUint64, metrics.KindFloat64:
			v.scalar = 0
		case metrics.KindFloat64Histogram:
			h := samples[i].Value.Float64Histogram()
			v.pointer = unsafe.Pointer(&metrics.Float64Histogram{
				Counts:  make([]uint64, len(h.Counts)),
				Buckets: append([]float64(nil), h.Buckets...),
			})
		}
	}
}
