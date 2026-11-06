//go:build sim && linux && (amd64 || arm64)

package cpu

// gosim adds this file to internal/cpu. The runtime initializes the
// package for a native program; under gosim this init does it, so that
// the CPU's features pick the assembly of crypto, hash/crc32 and the
// others, as natively. The features come from the machine the simulation
// runs on, and GODEBUG's cpu options of its process turn them off, as
// GODEBUG=cpu.all=off does natively.

// gosimGODEBUG returns the GODEBUG of the process; gosim hooks it.
func gosimGODEBUG() string

func init() {
	gosimInitHWCap()
	Initialize(gosimGODEBUG())
}
