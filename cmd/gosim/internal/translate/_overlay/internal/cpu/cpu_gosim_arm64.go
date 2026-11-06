//go:build sim && linux && arm64

package cpu

// gosimHWCap returns the process's HWCAP bits from its auxiliary vector,
// which the runtime reads for a native program; gosim hooks it.
func gosimHWCap() uint

func gosimInitHWCap() { HWCap = gosimHWCap() }
