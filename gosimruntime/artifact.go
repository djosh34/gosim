package gosimruntime

import (
	"bytes"
	mathrand "math/rand"
	"unsafe"

	"github.com/jellevandenhooff/gosim/internal/race"
)

// WriteArtifact saves a copy of data under name in the result of the current
// run. Writing a name again replaces its data.
func WriteArtifact(name string, data []byte) {
	data = bytes.Clone(data)
	// A kill after RunConfig.WallKill reads the copy on a goroutine of
	// its own (soFar), which acquires killEdge; simulated goroutines never
	// do, so this orders no two of them for the race detector.
	race.ReleaseMerge(unsafe.Pointer(&killEdge))
	getg().upcall(func() {
		gs.get().setArtifact(name, data)
	})
}

// setArtifact saves data under name, under soFarMu.
func (s *scheduler) setArtifact(name string, data []byte) {
	soFarMu.Lock()
	defer soFarMu.Unlock()
	if s.artifacts == nil {
		s.artifacts = make(map[string][]byte)
	}
	s.artifacts[name] = data
}

// takeArtifacts returns what the run saved, once it ended.
func (s *scheduler) takeArtifacts() map[string][]byte {
	soFarMu.Lock()
	defer soFarMu.Unlock()
	return s.artifacts
}

// Reseed sets the run's random state from seed, as a run that started
// with seed sets it: the scheduler's choices, and every random value the
// simulation draws, from then on depend on seed and what the simulation
// does, not on what ran before.
func Reseed(seed int64) {
	getg().upcall(func() {
		gs.get().fastrander.state = mathrand.New(mathrand.NewSource(seed)).Uint64()
	})
}
