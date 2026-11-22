package behavior_test

import (
	"testing"

	dep "github.com/jellevandenhooff/gosim/internal/tests/behavior/initdep"
	. "github.com/jellevandenhooff/gosim/internal/tests/behavior/initdot"
	"github.com/jellevandenhooff/gosim/internal/tests/behavior/initflag"
)

// The constant len below is folded away by translation. The aliased
// import is still used after it, so it must keep its alias; the dot import
// is not, so its package must still be initialized.

const (
	lenAliased = len([1]*dep.T{nil})
	lenDot     = len([1]*U{nil})
)

// aliasedT uses the alias where a local has the package's own name.
func aliasedT() dep.T {
	initdep := 1
	return dep.T(initdep)
}

func TestFoldKeepsImports(t *testing.T) {
	if lenAliased != 1 || lenDot != 1 || aliasedT() != 1 || !initflag.DotSet {
		t.Errorf("got %d, %d, %d, initdot initialized %v", lenAliased, lenDot, aliasedT(), initflag.DotSet)
	}
}
