package gosimruntime

import "testing"

// TestHashWordTopBit checks that two words that differ only in bit 63 do
// not cancel each other.
func TestHashWordTopBit(t *testing.T) {
	sum := func(ws ...uint64) uint64 {
		h := newFnv64()
		for _, w := range ws {
			h.HashWord(w)
		}
		return h.Sum()
	}
	if sum(0, 7, 123456, 99) == sum(0, 7|1<<63, 123456, 99|1<<63) {
		t.Error("two differences in bit 63 cancel")
	}
}
