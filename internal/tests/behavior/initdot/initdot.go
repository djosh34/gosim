// Package initdot has an initializer with a side effect, for a test that
// dot-imports it and whose only reference to it is folded away by
// translation.
package initdot

import "github.com/jellevandenhooff/gosim/internal/tests/behavior/initflag"

// U is the type the test names.
type U int

func init() {
	initflag.DotSet = true
}
