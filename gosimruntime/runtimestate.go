package gosimruntime

import (
	"bytes"
	"fmt"
	"runtime"
	"runtime/metrics"
	"slices"
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

// Stack stands in for runtime.Stack, and so for runtime/debug.Stack. It
// formats the calling goroutine's stack as the real runtime does, with what
// differs between two runs of a seed taken out: the goroutine's number is
// its number in the simulation, a frame's arguments read "...", and a
// "created by" line does not name the creating goroutine. With all set,
// the stacks of the other goroutines of the caller's machine follow, by
// number, each frame a function and its file and line.
//
//go:noinline
func Stack(buf []byte, all bool) int {
	tmp := make([]byte, 64<<10)
	for {
		n := runtime.Stack(tmp, false)
		if n < len(tmp) {
			tmp = tmp[:n]
			break
		}
		tmp = make([]byte, 2*len(tmp))
	}
	g := getg()
	out := simStack(tmp, goroutineID(g))
	if all {
		for _, peer := range machineGoroutines(g) {
			out = append(out, '\n')
			out = appendGoroutineStack(out, peer)
		}
	}
	return copy(buf, out)
}

// peerGoroutine is a goroutine of the caller's machine, as Stack lists it:
// its number, and runnable or waiting, as the simulation's scheduler has it.
type peerGoroutine struct {
	id    int
	state string
}

// machineGoroutines returns the goroutines of g's machine other than g, by
// number.
func machineGoroutines(g *goroutine) []peerGoroutine {
	var peers []peerGoroutine
	race.Release(unsafe.Pointer(&peers))
	g.upcall(func() {
		race.Acquire(unsafe.Pointer(&peers))
		for _, other := range gs.get().goroutines {
			if other.machine == g.machine && other != g {
				state := "waiting"
				if other.runnableIdx.idx != -1 {
					state = "runnable"
				}
				peers = append(peers, peerGoroutine{id: other.ID, state: state})
			}
		}
		race.Release(unsafe.Pointer(&peers))
	})
	race.Acquire(unsafe.Pointer(&peers))
	slices.SortFunc(peers, func(a, b peerGoroutine) int { return a.id - b.id })
	return peers
}

// appendGoroutineStack appends the stack of a peer goroutine, as
// GetStacktraceFor captures it.
func appendGoroutineStack(out []byte, peer peerGoroutine) []byte {
	pcs := make([]uintptr, 128)
	n := GetStacktraceFor(peer.id, pcs)
	out = fmt.Appendf(out, "goroutine %d [%s]:\n", peer.id, peer.state)
	frames := runtime.CallersFrames(pcs[:n])
	for {
		f, more := frames.Next()
		if f.Function != "" {
			out = fmt.Appendf(out, "%s(...)\n\t%s:%d\n", f.Function, f.File, f.Line)
		}
		if !more {
			return out
		}
	}
}

// goroutineID returns g's number in the simulation. The scheduler sets it
// before g first runs, on a real goroutine that may have run others before;
// the race detector does not see that order.
//
//go:norace
func goroutineID(g *goroutine) int {
	return g.ID
}

// simStack rewrites a stack that runtime.Stack formatted, called from
// Stack, as Stack describes.
func simStack(stack []byte, id int) []byte {
	var out bytes.Buffer
	lines := bytes.Split(stack, []byte("\n"))
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		switch {
		case bytes.HasPrefix(line, []byte("goroutine ")):
			if j := bytes.IndexByte(line, '['); j >= 0 {
				line = fmt.Appendf(nil, "goroutine %d %s", id, line[j:])
			}
			out.Write(line)
			out.WriteByte('\n')
			// The frame of Stack itself, and its file line.
			if i+1 < len(lines) && bytes.HasPrefix(lines[i+1], []byte("github.com/jellevandenhooff/gosim/gosimruntime.Stack(")) {
				i += 2
			}
			continue
		case bytes.HasPrefix(line, []byte("created by ")):
			if j := bytes.Index(line, []byte(" in goroutine ")); j >= 0 {
				line = line[:j]
			}
		case len(line) > 0 && line[0] != '\t':
			// A frame: the arguments are in the last parentheses, which
			// a note such as " [recovered]" may follow.
			if j := bytes.LastIndexByte(line, '('); j >= 0 {
				if k := bytes.IndexByte(line[j:], ')'); k >= 0 {
					line = slices.Concat(line[:j], []byte("(...)"), line[j+k+1:])
				}
			}
		}
		out.Write(line)
		if i < len(lines)-1 {
			out.WriteByte('\n')
		}
	}
	return out.Bytes()
}
