package gosimruntime

import (
	"errors"
	"runtime"
	"testing"
)

// TestHostSlots checks that live owners hold slots of their own, that a
// slot comes back once its owner is garbage, that an owner may hold
// several slots and point into an object, and the errors of a slot of no
// bytes or of the wrong size, of an owner that is not a pointer and of a
// closed file.
func TestHostSlots(t *testing.T) {
	h, err := HostCreate()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := HostClose(h); err != nil {
			t.Error(err)
		}
	}()
	type owner struct{ n [64]byte }
	// A size of no bytes or less fails, and fixes no size for the file.
	for _, size := range []int{0, -16} {
		if _, err := HostSlot(h, &owner{}, size); !errors.Is(err, ErrSlotSize) {
			t.Errorf("a slot of %d bytes: %v, want %v", size, err, ErrSlotSize)
		}
	}
	a, b := &owner{}, &owner{}
	offA, err := HostSlot(h, a, 4096)
	if err != nil {
		t.Fatal(err)
	}
	offB, err := HostSlot(h, b, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if offA == offB || offA%4096 != 0 || offB%4096 != 0 {
		t.Fatalf("two live owners got slots at %d and %d", offA, offB)
	}
	runtime.KeepAlive(a)
	// Once b is garbage, its slot is free for the next owner.
	b = nil
	freed := false
	for range 50 {
		runtime.GC()
		hostMu.Lock()
		freed = len(hostSlots[h].free) > 0
		hostMu.Unlock()
		if freed {
			break
		}
	}
	if !freed {
		t.Fatal("the slot of an owner that is garbage never came back")
	}
	c := &owner{}
	if offC, err := HostSlot(h, c, 4096); err != nil || offC != offB {
		t.Errorf("the next owner got the slot at %d, %v; want the freed one at %d", offC, err, offB)
	}
	// An owner may hold two slots, and point into an object.
	type two struct{ x, y [64]byte }
	o := &two{}
	off1, err1 := HostSlot(h, o, 4096)
	off2, err2 := HostSlot(h, &o.y, 4096)
	if err1 != nil || err2 != nil || off1 == off2 || off1 == offA || off2 == offA {
		t.Errorf("slots of one object at %d, %v and %d, %v, beside %d", off1, err1, off2, err2, offA)
	}
	runtime.KeepAlive(o)
	runtime.KeepAlive(a)
	if _, err := HostSlot(h, &owner{}, 512); !errors.Is(err, ErrSlotSize) {
		t.Errorf("a slot of another size: %v, want %v", err, ErrSlotSize)
	}
	if _, err := HostSlot(h, owner{}, 4096); !errors.Is(err, ErrSlotOwner) {
		t.Errorf("an owner that is not a pointer: %v, want %v", err, ErrSlotOwner)
	}
	if _, err := HostSlot(h, (*owner)(nil), 4096); !errors.Is(err, ErrSlotOwner) {
		t.Errorf("a nil owner: %v, want %v", err, ErrSlotOwner)
	}
	if _, err := HostSlot(h+1000, &owner{}, 4096); !errors.Is(err, ErrNoHostFile) {
		t.Errorf("a slot of a file that is not open: %v, want %v", err, ErrNoHostFile)
	}
}
