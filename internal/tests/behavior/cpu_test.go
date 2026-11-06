//go:build sim

package behavior

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"hash/crc32"
	"math/big"
	"net"
	"testing"
	"time"

	"golang.org/x/crypto/chacha20"

	"github.com/jellevandenhooff/gosim"
)

// TestAcceleratedAESGCM seals 32 MiB with AES-GCM. With the CPU's AES
// instructions that takes about 10 ms; in Go, without them, half a
// second or more. So the assembly runs, chosen by the CPU's features
// (internal/cpu).
func TestAcceleratedAESGCM(t *testing.T) {
	block, err := aes.NewCipher(make([]byte, 16))
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1<<20)
	nonce := make([]byte, gcm.NonceSize())
	before, err := gosim.CPUTime()
	if err != nil {
		t.Fatal(err)
	}
	for range 32 {
		buf = gcm.Seal(buf[:0], nonce, buf[:1<<20], nil)
	}
	after, err := gosim.CPUTime()
	if err != nil {
		t.Fatal(err)
	}
	if took := after - before; took > 100*time.Millisecond {
		t.Errorf("sealing 32 MiB with AES-GCM took %v of CPU time: no AES instructions?", took)
	}
}

// TestXCryptoChaCha20 runs golang.org/x/crypto's ChaCha20, whose
// assembly gosim keeps, as the standard library's vendored copy.
func TestXCryptoChaCha20(t *testing.T) {
	c, err := chacha20.NewUnauthenticatedCipher(make([]byte, chacha20.KeySize), make([]byte, chacha20.NonceSize))
	if err != nil {
		t.Fatal(err)
	}
	out := make([]byte, 256)
	c.XORKeyStream(out, out)
	// The first bytes of the keystream for the zero key and nonce, from
	// RFC 8439's test vector 1.
	if want := []byte{0x76, 0xb8, 0xe0, 0xad, 0xa0, 0xf1, 0x3d, 0x90}; !bytes.Equal(out[:8], want) {
		t.Errorf("keystream starts %x, want %x", out[:8], want)
	}
}

// TestCRC32 checks that hash/crc32, whose assembly gosim keeps, gives
// the check values of both tables. The Go code gives them too, so this
// does not show that the assembly runs.
func TestCRC32(t *testing.T) {
	for _, c := range []struct {
		poly uint32
		want uint32
	}{{crc32.IEEE, 0xcbf43926}, {crc32.Castagnoli, 0xe3069283}} {
		if got := crc32.Checksum([]byte("123456789"), crc32.MakeTable(c.poly)); got != c.want {
			t.Errorf("CRC-32 of %x: got %x, want %x", c.poly, got, c.want)
		}
	}
}

// TestTLSCipher checks that a TLS 1.3 handshake picks ChaCha20-Poly1305,
// whatever the CPU, so that a run does not depend on the machine.
func TestTLSCipher(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	done := make(chan error, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		defer c.Close()
		done <- tls.Server(c, &tls.Config{Certificates: []tls.Certificate{cert}}).Handshake()
	}()
	c, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := c.ConnectionState().CipherSuite; got != tls.TLS_CHACHA20_POLY1305_SHA256 {
		t.Errorf("the handshake picked %s, want TLS_CHACHA20_POLY1305_SHA256", tls.CipherSuiteName(got))
	}
}
