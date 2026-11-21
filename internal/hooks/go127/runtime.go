package go127

import (
	"unsafe"

	"github.com/jellevandenhooff/gosim/gosimruntime"
)

// These implement runtime functions that packages outside the standard
// library reach with a go:linkname, such as Pebble's internal/fastrand and
// internal/rawalloc (runtimeLinknameHooks in cmd/gosim).

func Runtime_fastrand() uint32 {
	return uint32(gosimruntime.Fastrand64())
}

func Runtime_fastrandn(n uint32) uint32 {
	return gosimruntime.Fastrandn(n)
}

func Runtime_fastrand64() uint64 {
	return gosimruntime.Fastrand64()
}

// Runtime_mallocgc allocates size bytes that hold no pointers. The runtime's
// mallocgc takes the type of the memory, so that the garbage collector finds
// its pointers. A caller that passes a type gets an unimplemented hook, since
// the memory here is a byte slice and the collector would miss its pointers.
func Runtime_mallocgc(size uintptr, typ unsafe.Pointer, needzero bool) unsafe.Pointer {
	if typ != nil {
		gosimruntime.Unimplemented("runtime.mallocgc with a type")
	}
	if size == 0 {
		return unsafe.Pointer(&zerobase)
	}
	return unsafe.Pointer(unsafe.SliceData(make([]byte, size)))
}

// zerobase is the address of every allocation of 0 bytes, as in the runtime.
var zerobase uintptr
