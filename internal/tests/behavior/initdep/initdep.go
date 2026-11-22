// Package initdep has an initializer with a side effect, for a test whose
// only reference to the package is folded away by translation.
package initdep

import "github.com/jellevandenhooff/gosim/internal/tests/behavior/initflag"

// T is the type the test names.
type T int

func init() {
	initflag.Set = true
}
