// Copyright 2011 The Go Authors. All rights reserved.  Use of this source code
// is governed by a BSD-style license that can be found at
// https://go.googlesource.com/go/+/refs/heads/master/LICENSE.

package gosimruntime

type (
	fnv64 uint64
)

const (
	fnv64init = 14695981039346656037
	prime64   = 1099511628211
)

// newFnv64 returns a new 64-bit FNV-1 hash.Hash.
// Its Sum method will lay the value out in big-endian byte order.
func newFnv64() fnv64 {
	var s fnv64 = fnv64init
	return s
}

func (s *fnv64) Sum() uint64 { return uint64(*s) }

func (s *fnv64) Hash(data []byte) {
	hash := *s
	for _, c := range data {
		hash *= prime64
		hash ^= fnv64(c)
	}
	*s = hash
}

// HashWord hashes w as one 64-bit word: one multiplication instead of
// one per byte.
func (s *fnv64) HashWord(w uint64) {
	*s = (*s ^ fnv64(w)) * prime64
}
