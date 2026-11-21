// Package linkname reaches runtime functions with a go:linkname, as Pebble's
// internal/fastrand and internal/rawalloc do.
package linkname

import "unsafe"

//go:linkname Fastrand runtime.fastrand
func Fastrand() uint32

//go:linkname Fastrandn runtime.fastrandn
func Fastrandn(n uint32) uint32

//go:linkname Fastrand64 runtime.fastrand64
func Fastrand64() uint64

//go:linkname mallocgc runtime.mallocgc
func mallocgc(size uintptr, typ unsafe.Pointer, needzero bool) unsafe.Pointer

// Bytes allocates n bytes the way Pebble's rawalloc does.
func Bytes(n int) []byte {
	return unsafe.Slice((*byte)(mallocgc(uintptr(n), nil, true)), n)
}
