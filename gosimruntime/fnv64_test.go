package gosimruntime

import "testing"

// TestHashWordTopBit checks that small differences in two words, in bit 63
// alone or with bit 31, do not cancel each other.
func TestHashWordTopBit(t *testing.T) {
	sum := func(ws ...uint64) uint64 {
		h := newFnv64()
		for _, w := range ws {
			h.HashWord(w)
		}
		return h.Sum()
	}
	for _, d := range []uint64{1 << 63, 1<<63 | 1<<31} {
		if sum(0, 7, 123456, 99) == sum(0, 7^d, 123456, 99^d) {
			t.Errorf("two differences of %#x cancel", d)
		}
	}
	if sum(0, 7, 123456, 99) == sum(0, 7^1<<63, 123456^(1<<63|1<<31), 99) {
		t.Error("differences in bit 63 and then bits 63 and 31 of the next word cancel")
	}
}
