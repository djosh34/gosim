package gosimruntime

import "bytes"

// WriteArtifact saves a copy of data under name in the result of the current
// run. Writing a name again replaces its data. It runs on the simulated
// goroutine, under soFarMu, so that a kill after RunConfig.WallKill, on
// another goroutine, reads what it saved.
func WriteArtifact(name string, data []byte) {
	gs.get().setArtifact(name, data)
}

// setArtifact saves a copy of data under name, under soFarMu.
func (s *scheduler) setArtifact(name string, data []byte) {
	soFarMu.Lock()
	defer soFarMu.Unlock()
	if s.artifacts == nil {
		s.artifacts = make(map[string][]byte)
	}
	s.artifacts[name] = bytes.Clone(data)
}

// takeArtifacts returns what the run saved, once it ended.
func (s *scheduler) takeArtifacts() map[string][]byte {
	soFarMu.Lock()
	defer soFarMu.Unlock()
	return s.artifacts
}
