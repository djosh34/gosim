package behavior_test

import (
	"testing"
	"time"
)

// TestSeedGrowthTwoTimers is the two-timer reproduction from tigerstore #531.
func TestSeedGrowthTwoTimers(t *testing.T) {
	a := time.NewTimer(time.Second)
	b := time.NewTimer(time.Second)
	select {
	case <-a.C:
		t.Log("a")
	case <-b.C:
		t.Log("b")
	}
}
