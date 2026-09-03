package relkinds_test

// derived_kind_resolvable_6773_test.go — the ratchet #6773 owes the ledger.
//
// #6773 declared COMMIT_COUPLED (in the DERIVED vocabulary) and, in the same
// change, deleted its entry from undeclaredKindsDeferred. That entry was the
// only thing making the sweep notice if the kind stopped being VISIBLE to the
// scan: the stale-ledger check fires when a ledgered kind's site disappears,
// and a declared kind is skipped by the sweep entirely.
//
// The disappearance is not hypothetical. relkinds resolves a relationship-kind
// field only when it can read the value as a source constant, and a local
// `const K = string(types.X)` is a CallExpr, which stringConsts does not
// collect. So writing `Kind: KindCommitCoupled` at the emit site — the obvious
// tidy-up, and what the file said before #6773 — silently takes the busiest
// relationship kind in the graph out of the static ledger's field of view,
// with `go vet` clean and every package's suite green.
//
// This is the guard for that. It is stated over the whole derived vocabulary
// rather than over COMMIT_COUPLED alone, so the next derived kind added
// inherits it.

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/relkinds"
	"github.com/cajasmota/grafel/internal/types"
)

// TestEveryDerivedKindIsStaticallyVisibleToTheScan requires each declared
// derived kind to be RESOLVED at a real site by the repo-wide scan.
//
// A declared kind is skipped by the sweep, so nothing else in this package
// would ever mention it again: without this, "the ledger tracks the population"
// silently stops being true for the derived half.
func TestEveryDerivedKindIsStaticallyVisibleToTheScan(t *testing.T) {
	derived := types.AllDerivedRelationshipKinds()
	if len(derived) == 0 {
		t.Fatal("no derived kinds declared; this guard would be vacuous")
	}
	res := scanRepo(t)
	if len(res.Sites) == 0 {
		t.Fatal("the scan produced no sites at all; every assertion below would be vacuous")
	}
	for _, k := range derived {
		sites := res.SitesFor(string(k))
		if len(sites) == 0 {
			t.Errorf("the scan resolves NO site emitting %q.\n\n"+
				"A derived kind is declared, so the sweep skips it and the ledger no longer names "+
				"it — this test is the only thing left that observes it. The usual cause is an emit "+
				"site spelling a local alias whose value is `string(types.X)`: stringConsts collects "+
				"only string LITERALS, so the alias is not a source constant and the site is reported "+
				"unresolved. Spell the shared constant at the emit site "+
				"(`Kind: string(types.RelationshipKind...)`), which resolves through the scan's "+
				"cross-package const table.", k)
			continue
		}
		for _, s := range sites {
			if s.Origin != relkinds.OriginGo {
				t.Errorf("site %s for derived kind %q has origin %q, want %q",
					s, k, s.Origin, relkinds.OriginGo)
			}
		}
	}
}

// TestCommitCoupledResolvesAtItsEmitSite is the named half: the property above
// is general, this pins that the site the scan resolves is the commit-coupling
// pass and not some incidental mention elsewhere. A general property satisfied
// by the wrong file is not the property #6773 needs.
func TestCommitCoupledResolvesAtItsEmitSite(t *testing.T) {
	res := scanRepo(t)
	sites := res.SitesFor(string(types.RelationshipKindCommitCoupled))
	if len(sites) == 0 {
		t.Fatalf("no resolved site for %q at all", types.RelationshipKindCommitCoupled)
	}
	const want = "internal/engine/commit_coupling_edges.go"
	for _, s := range sites {
		if strings.Contains(s.File, want) {
			return
		}
	}
	t.Errorf("%q is resolved at %v, but not in %s — the emit site itself is invisible to the scan",
		types.RelationshipKindCommitCoupled, sites, want)
}
