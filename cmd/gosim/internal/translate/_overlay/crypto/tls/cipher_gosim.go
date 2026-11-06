//go:build sim

package tls

// gosim adds this file to crypto/tls. A TLS 1.3 handshake picks AES-GCM
// or ChaCha20-Poly1305 by whether the CPU has AES instructions, and no
// Config setting changes that. Under gosim a run must not depend on the
// machine it runs on, so every simulation picks ChaCha20-Poly1305, as on
// a CPU without those instructions, which every run did while gosim hid
// the CPU's features.
func init() { hasAESGCMHardwareSupport = false }
