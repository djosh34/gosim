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
