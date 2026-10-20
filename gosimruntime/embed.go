package gosimruntime

import "bytes"

// embeddedBytes is a //go:embed variable of type []byte and its embedded
// contents.
type embeddedBytes struct {
	variable *[]byte
	contents []byte
}

var allEmbeddedBytes []embeddedBytes

// RegisterEmbeddedBytes records a //go:embed variable of type []byte, so that
// every run starts with its embedded contents even when an earlier run changed
// them. Translated packages call it from init.
func RegisterEmbeddedBytes(variable *[]byte) {
	allEmbeddedBytes = append(allEmbeddedBytes, embeddedBytes{variable: variable, contents: bytes.Clone(*variable)})
}

// resetEmbeddedBytes gives every //go:embed []byte variable its embedded
// contents again.
func resetEmbeddedBytes() {
	for _, e := range allEmbeddedBytes {
		*e.variable = bytes.Clone(e.contents)
	}
}
