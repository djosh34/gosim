package translate

import "testing"

func TestIsBuildConstraint(t *testing.T) {
	for line, want := range map[string]bool{
		"//go:build linux":   true,
		"// +build linux":    true,
		"//+build !noasm":    true, // as in klauspost/cpuid v2.0.9
		"// build linux":     false,
		"// a comment":       false,
		"//go:generate true": false,
	} {
		if got := isBuildConstraint(line); got != want {
			t.Errorf("%q: got %v, want %v", line, got, want)
		}
	}
}
