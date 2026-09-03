package engine

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// commit_coupling_derived_kind_6773_test.go — #6773.
//
// The pass declared its kind as a package-local string literal, absent from
// every vocabulary accessor, which is how 99.1% of the graph's non-enum edges
// came to be produced by one site nobody could enumerate (#6757 arm C).
//
// These tests observe the EMITTED EDGES, not just the constant: a constant
// wired to types.RelationshipKindCommitCoupled while the emit site wrote some
// other string would satisfy an equality check on the constant alone.

func TestCommitCoupledConstantIsTheSharedDerivedKind(t *testing.T) {
	if KindCommitCoupled != string(types.RelationshipKindCommitCoupled) {
		t.Errorf("engine.KindCommitCoupled = %q, types.RelationshipKindCommitCoupled = %q; "+
			"the emitted kind and the declared kind have drifted", KindCommitCoupled,
			types.RelationshipKindCommitCoupled)
	}
}

// TestApplyCommitCoupling_EmitsADerivedKind is the call-site assertion: it runs
// the pass over a real fixture repository and classifies the kinds that
// actually reached the document.
func TestApplyCommitCoupling_EmitsADerivedKind(t *testing.T) {
	if !gitAvailable(t) {
		t.Skip("git not available")
	}
	trio := []string{"a.go", "b.go", "c.go"}
	dir := t.TempDir()
	makeFixtureRepo(t, dir, [][]string{trio, trio, trio, trio, trio})

	doc := &graph.Document{Repo: "fixture"}
	stats := ApplyCommitCoupling(doc, dir, DefaultCommitCouplingConfig())
	if stats.Skipped {
		t.Fatalf("pass skipped: %s", stats.SkipReason)
	}
	if stats.CoupledEdges == 0 || len(doc.Relationships) == 0 {
		t.Fatalf("no edges emitted (CoupledEdges=%d, relationships=%d); every assertion below "+
			"would be vacuous", stats.CoupledEdges, len(doc.Relationships))
	}

	seen := map[string]int{}
	for _, r := range doc.Relationships {
		seen[r.Kind]++
	}
	if n := seen[string(types.RelationshipKindCommitCoupled)]; n != len(doc.Relationships) {
		t.Errorf("%d of %d emitted edges carry %q; kinds seen: %v", n, len(doc.Relationships),
			types.RelationshipKindCommitCoupled, seen)
	}
	for kind := range seen {
		if !types.IsDerivedRelationshipKind(kind) {
			t.Errorf("the pass emitted kind %q, which no vocabulary declares as derived — the "+
				"emit site and internal/types have drifted", kind)
		}
		if !types.IsDeclaredRelationshipKind(kind) {
			t.Errorf("the pass emitted kind %q, which IsDeclaredRelationshipKind rejects", kind)
		}
		// The negative half. COMMIT_COUPLED is a statistical co-change signal,
		// not a structural fact an extractor observed; if the structural
		// predicate starts accepting it, the separation #6773 decided on has
		// collapsed and a consumer asking for structural edges gets 27k
		// inferences it never asked for.
		if types.IsValidRelationshipKind(kind) {
			t.Errorf("IsValidRelationshipKind(%q) = true; the commit-coupling pass emits a DERIVED "+
				"kind and must not enter the structural vocabulary", kind)
		}
	}
}
