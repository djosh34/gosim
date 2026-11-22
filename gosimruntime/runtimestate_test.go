package gosimruntime

import (
	"runtime/metrics"
	"testing"
)

// TestReadMetrics checks that metricsValue has the layout of
// metrics.Value: ReadMetrics must leave values that the metrics.Value
// methods read as zero, of the kind the runtime gives them.
func TestReadMetrics(t *testing.T) {
	samples := []metrics.Sample{
		{Name: "/sched/goroutines:goroutines"},
		{Name: "/gc/heap/goal:bytes"},
		{Name: "/sched/gomaxprocs:threads"},
		{Name: "/sched/latencies:seconds"},
		{Name: "/gc/pauses:seconds"},
		{Name: "/no/such:metric"},
	}
	want := make([]metrics.Sample, len(samples))
	copy(want, samples)
	metrics.Read(want)
	// Read twice: the second read gets the histograms of the first.
	for range 2 {
		ReadMetrics(samples)
		for i, s := range samples {
			if s.Value.Kind() != want[i].Value.Kind() {
				t.Fatalf("%s: kind %v, want %v", s.Name, s.Value.Kind(), want[i].Value.Kind())
			}
			switch s.Value.Kind() {
			case metrics.KindUint64:
				if v := s.Value.Uint64(); v != 0 {
					t.Errorf("%s: %d, want 0", s.Name, v)
				}
			case metrics.KindFloat64:
				if v := s.Value.Float64(); v != 0 {
					t.Errorf("%s: %v, want 0", s.Name, v)
				}
			case metrics.KindFloat64Histogram:
				h, real := s.Value.Float64Histogram(), want[i].Value.Float64Histogram()
				if len(h.Counts) != len(real.Counts) || len(h.Buckets) != len(real.Buckets) {
					t.Fatalf("%s: %d counts and %d buckets, want %d and %d",
						s.Name, len(h.Counts), len(h.Buckets), len(real.Counts), len(real.Buckets))
				}
				for j, c := range h.Counts {
					if c != 0 {
						t.Errorf("%s: count %d is %d, want 0", s.Name, j, c)
					}
				}
			case metrics.KindBad:
				if s.Name != "/no/such:metric" {
					t.Errorf("%s: KindBad", s.Name)
				}
			}
		}
	}
}

func TestSimStack(t *testing.T) {
	in := "goroutine 3971 [running]:\n" +
		"github.com/jellevandenhooff/gosim/gosimruntime.Stack({0x4c5e0eba9900, 0x10000, 0x10000}, 0x0)\n" +
		"\t/src/gosimruntime/runtimestate.go:120 +0x40\n" +
		"example.com/p.(*T).f(0x4c5e0eba9900, {0xdf17300, 0x4c5e25ab3420}, ...)\n" +
		"\t/src/p/p.go:10 +0xa0\n" +
		"example.com/p.g[...](0xdf17258?)\n" +
		"\t/src/p/p.go:20 +0x18\n" +
		"panic({0x1a2b3c0?, 0x4c5e0eba9900?}) [recovered]\n" +
		"\t/go/src/runtime/panic.go:800 +0x130\n" +
		"created by example.com/p.h in goroutine 205348\n" +
		"\t/src/p/p.go:30 +0x64\n"
	want := "goroutine 7 [running]:\n" +
		"example.com/p.(*T).f(...)\n" +
		"\t/src/p/p.go:10 +0xa0\n" +
		"example.com/p.g[...](...)\n" +
		"\t/src/p/p.go:20 +0x18\n" +
		"panic(...) [recovered]\n" +
		"\t/go/src/runtime/panic.go:800 +0x130\n" +
		"created by example.com/p.h\n" +
		"\t/src/p/p.go:30 +0x64\n"
	if got := string(simStack([]byte(in), 7)); got != want {
		t.Errorf("simStack:\n%s\nwant:\n%s", got, want)
	}
}
