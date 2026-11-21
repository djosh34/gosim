package behavior_test

import (
	"math/big"
	"math/rand"
	"testing"
)

// Constructs that translation once got wrong.

// varNilMap is a blank global that is nil.
var _ map[string]int = nil

type hidden map[string]int

type holder struct{ h hidden }

// hide's parameter hides the type hidden, which the translated assignment
// of a map to h names.
func hide(hidden []string) holder {
	m := make(map[string]int, 0)
	for _, k := range hidden {
		m[k]++
	}
	return holder{h: m}
}

func TestShadowedMapType(t *testing.T) {
	if got := hide([]string{"a", "a", "b"}).h; got["a"] != 2 || got["b"] != 1 {
		t.Errorf("got %v", got)
	}
}

func TestMakeSizes(t *testing.T) {
	n := uint16(3)
	ch := make(chan int, n)
	ch <- 1
	ch <- 2
	ch <- 3
	if len(ch) != 3 || cap(ch) != 3 {
		t.Errorf("len %d, cap %d", len(ch), cap(ch))
	}
	size := 10
	m := make(map[int]int, size)
	m[1] = 1
	if len(m) != 1 {
		t.Errorf("got %v", m)
	}
}

func TestBigIntRand(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	n := big.NewInt(1000)
	seen := make(map[int64]bool)
	for range 200 {
		v := new(big.Int).Rand(r, n)
		if v.Sign() < 0 || v.Cmp(n) >= 0 {
			t.Fatalf("got %v, not in [0, 1000)", v)
		}
		seen[v.Int64()] = true
	}
	if len(seen) < 100 {
		t.Errorf("only %d values in 200 draws", len(seen))
	}
	if v := new(big.Int).Rand(r, big.NewInt(0)); v.Sign() != 0 {
		t.Errorf("Rand with n = 0 got %v", v)
	}
}

func TestGoPrintln(t *testing.T) {
	done := make(chan struct{})
	go println("from a goroutine")
	go func() { close(done) }()
	<-done
}
