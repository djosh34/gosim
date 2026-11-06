package gosimruntime

import "testing"

// TestCPUTimeGrows checks that CPUTime counts work the process does.
func TestCPUTimeGrows(t *testing.T) {
	before, err := CPUTime()
	if err != nil {
		t.Fatal(err)
	}
	for {
		now, err := CPUTime()
		if err != nil {
			t.Fatal(err)
		}
		if now > before {
			return
		}
	}
}
