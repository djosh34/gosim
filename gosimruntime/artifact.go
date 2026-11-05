package gosimruntime

import "bytes"

// WriteArtifact saves a copy of data under name in the result of the current
// run. Writing a name again replaces its data.
func WriteArtifact(name string, data []byte) {
	data = bytes.Clone(data)
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
