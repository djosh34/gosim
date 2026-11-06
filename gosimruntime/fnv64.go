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

// newFnv64 returns a new 64-bit FNV hash: FNV-1 over bytes (Hash), one
// FNV-1a-like step over words (HashWord).
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

// HashWord hashes w as one 64-bit word, with two multiplications instead
// of one per byte. A multiplication carries a bit only upward, so the
// sum's top half is folded into its bottom half between them; otherwise
// small differences in one or two words could cancel each other.
func (s *fnv64) HashWord(w uint64) {
	h := (*s ^ fnv64(w)) * prime64
	h ^= h >> 32
	*s = h * prime64
}
