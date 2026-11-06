//go:build sim && linux && amd64

package cpu

// gosimInitHWCap does nothing: on amd64 the features come from CPUID.
func gosimInitHWCap() {}
