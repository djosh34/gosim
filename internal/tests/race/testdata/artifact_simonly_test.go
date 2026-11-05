//go:build sim

package race_test

import (
	"testing"
	"time"

	"github.com/jellevandenhooff/gosim"
)

// TestRaceWriteArtifact checks that gosim.WriteArtifact orders no two
// simulated goroutines: the race between them is still reported.
func TestRaceWriteArtifact(t *testing.T) {
	x := 0
	go func() {
		x = 1
		gosim.WriteArtifact("a", nil)
	}()
	time.Sleep(time.Second)
	gosim.WriteArtifact("b", nil)
	_ = x
}
