// #7328 arm (c) — the guard's verdict logic, graded where it lives.
//
// The nineteen per-package tests in internal/custom/**, internal/patterns and
// internal/extractors/** grade the WIRING: that each constructor reaches this
// function at all. They kill an inert guard, but they kill it a package or
// more away from the file that broke, and `go test ./internal/types` on its
// own says nothing about it — every mutation of ValidateProducedEntityKind
// scored ALIVE against this package before this file existed.
//
// So this file grades the three-way verdict at the source: valid kinds pass,
// recorded kinds pass, everything else panics, and the panic names the offender.

package types_test

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

func TestProducedEntityKindGuard7328_Verdict(t *testing.T) {
	const site = "internal/types.test"

	t.Run("enum kind passes", func(t *testing.T) {
		kind := string(types.EntityKindClass)
		if !types.IsValidEntityKind(kind) {
			t.Fatalf("premise gone: %q is no longer an enum kind", kind)
		}
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("guard rejected the enum kind %q: %v", kind, r)
			}
		}()
		types.ValidateProducedEntityKind(site, kind)
	})

	t.Run("every recorded kind passes", func(t *testing.T) {
		roster := types.KnownUndeclaredProducedKinds()
		if len(roster) == 0 {
			t.Fatal("roster is empty: this case grades nothing")
		}
		for kind := range roster {
			if types.IsValidEntityKind(kind) {
				t.Errorf("%q is on the known-undeclared roster AND in the enum; it should have "+
					"been removed from the roster when it was declared", kind)
				continue
			}
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("guard rejected the recorded kind %q: %v", kind, r)
					}
				}()
				types.ValidateProducedEntityKind(site, kind)
			}()
		}
	})

	t.Run("unrecorded kind panics and names the offender", func(t *testing.T) {
		const kind = "SCOPE.NotAKind7328"
		if types.IsValidEntityKind(kind) {
			t.Fatalf("premise gone: %q became a declared kind", kind)
		}
		if _, recorded := types.KnownUndeclaredProducedKinds()[kind]; recorded {
			t.Fatalf("premise gone: %q was added to the roster", kind)
		}
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("guard accepted an unrecorded kind: it enforces nothing")
			}
			msg, _ := r.(string)
			for _, want := range []string{"#7328", kind, site} {
				if !strings.Contains(msg, want) {
					t.Errorf("panic message does not contain %q, so it cannot be acted on: %v", want, r)
				}
			}
		}()
		types.ValidateProducedEntityKind(site, kind)
	})

	// An un-prefixed spelling is the shape that escaped #7328's own scoping
	// sweep (`"Handler"`, seven sites in internal/custom/java). The guard must
	// not have a `SCOPE.`-shaped hole of its own.
	t.Run("unprefixed unrecorded kind panics too", func(t *testing.T) {
		const kind = "NotAKind7328"
		defer func() {
			if recover() == nil {
				t.Fatalf("guard accepted the unprefixed unrecorded kind %q: a widening to "+
					"`SCOPE.`-prefixed spellings would be invisible here", kind)
			}
		}()
		types.ValidateProducedEntityKind(site, kind)
	})

	t.Run("empty kind panics", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("guard accepted an empty kind: a producer that forgot to set one is " +
					"exactly the defect this is for")
			}
		}()
		types.ValidateProducedEntityKind(site, "")
	})
}

// TestProducedEntityKindGuard7328_RosterCopyIsDefensive pins that a caller
// cannot mutate the roster the guard consults. Without the copy,
// KnownUndeclaredProducedKinds() would hand every test and every caller a
// writable handle on the enforcement set.
func TestProducedEntityKindGuard7328_RosterCopyIsDefensive(t *testing.T) {
	got := types.KnownUndeclaredProducedKinds()
	if len(got) == 0 {
		t.Fatal("roster is empty: this case grades nothing")
	}
	got["SCOPE.InjectedByCaller7328"] = 1
	defer func() {
		if recover() == nil {
			t.Fatal("a caller's write to the returned map reached the guard's own roster")
		}
	}()
	types.ValidateProducedEntityKind("internal/types.test", "SCOPE.InjectedByCaller7328")
}
