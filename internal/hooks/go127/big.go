package go127

import (
	"math/big"
	"math/rand"
)

// BigIntRand is (*big.Int).Rand for translated code: math/big is not
// translated, so its Rand takes the native *rand.Rand, where translated
// code has the simulation's (rewriteBigIntRand in cmd/gosim). It sets z to
// a number drawn uniformly from [0, n), or to 0 if n <= 0, as Rand does.
func BigIntRand(z *big.Int, rnd *rand.Rand, n *big.Int) *big.Int {
	if n.Sign() <= 0 {
		return z.SetInt64(0)
	}
	bits := n.BitLen()
	buf := make([]byte, (bits+7)/8)
	// Each draw goes to its own Int: z may be n itself, as in n.Rand(r, n).
	draw := new(big.Int)
	for {
		_, _ = rnd.Read(buf)
		// Clear the bits above n's highest one, so that at least half of
		// the draws are below n.
		buf[0] &= byte(0xff) >> (len(buf)*8 - bits)
		draw.SetBytes(buf)
		if draw.Cmp(n) < 0 {
			return z.Set(draw)
		}
	}
}
