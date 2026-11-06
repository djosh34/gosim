package gosimruntime

import (
	"os"
	_ "unsafe" // for go:linkname
)

// GODEBUG returns the GODEBUG of the process, whose cpu options turn off
// CPU features for the simulation's internal/cpu as for the process's own.
func GODEBUG() string {
	return os.Getenv("GODEBUG")
}

// atHWCap is the auxiliary vector's key of the HWCAP bits.
const atHWCap = 16

// HWCap returns the HWCAP bits of the process's auxiliary vector, from
// which internal/cpu reads the CPU's features on arm64 Linux, or 0.
func HWCap() uint {
	auxv := runtime_getAuxv()
	for i := 0; i+1 < len(auxv); i += 2 {
		if auxv[i] == atHWCap {
			return uint(auxv[i+1])
		}
	}
	return 0
}

//go:linkname runtime_getAuxv runtime.getAuxv
func runtime_getAuxv() []uintptr
