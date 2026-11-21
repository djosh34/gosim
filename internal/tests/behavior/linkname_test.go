package behavior_test

import (
	"testing"

	"github.com/jellevandenhooff/gosim"

	"github.com/jellevandenhooff/gosim/internal/tests/behavior/linkname"
)

func TestRuntimeLinknames(t *testing.T) {
	for range 100 {
		if n := linkname.Fastrandn(10); n >= 10 {
			t.Fatalf("fastrandn(10) = %d", n)
		}
	}
	if linkname.Fastrand() == linkname.Fastrand() && linkname.Fastrand64() == linkname.Fastrand64() {
		t.Error("fastrand repeats itself")
	}

	b := linkname.Bytes(100)
	for i, c := range b {
		if c != 0 {
			t.Fatalf("byte %d of new memory is %d", i, c)
		}
		b[i] = byte(i)
	}
	if b[99] != 99 {
		t.Error("memory does not keep what was written")
	}
	if len(linkname.Bytes(0)) != 0 {
		t.Error("0 bytes")
	}
}

func TestTypelinks(t *testing.T) {
	sections, offsets := linkname.Typelinks()
	if gosim.IsSim() && (sections != nil || offsets != nil) {
		t.Errorf("got %d sections in a simulation", len(sections))
	}
	if !gosim.IsSim() && len(sections) == 0 {
		t.Error("no type sections")
	}
}
