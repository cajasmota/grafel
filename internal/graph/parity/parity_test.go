package parity

import (
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
)

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// ent is a tiny helper for building a structurally-complete entity.
func ent(kind, name, file string) graph.Entity {
	return graph.Entity{
		ID:         graph.EntityID("repo", kind, name, file),
		Name:       name,
		Kind:       kind,
		SourceFile: file,
		Language:   "go",
	}
}

func ci(v int) *int { return &v }

func TestCompare_IdenticalGraphsAreEquivalent(t *testing.T) {
	a := &graph.Document{
		Entities: []graph.Entity{
			ent("SCOPE.Operation", "Alpha", "a.go"),
			ent("SCOPE.Operation", "Beta", "b.go"),
		},
		Relationships: []graph.Relationship{
			{FromID: "x", ToID: "y", Kind: "CALLS"},
		},
	}
	// b is the same entities/edges in a DIFFERENT order — must still be equivalent.
	b := &graph.Document{
		Entities: []graph.Entity{
			ent("SCOPE.Operation", "Beta", "b.go"),
			ent("SCOPE.Operation", "Alpha", "a.go"),
		},
		Relationships: []graph.Relationship{
			{FromID: "x", ToID: "y", Kind: "CALLS"},
		},
	}
	rep := Compare(a, b)
	if !rep.Equivalent {
		t.Fatalf("expected equivalent, got:\n%s", rep.String())
	}
}

// TestCompare_TimestampToleranceOnly verifies the comparator ignores the
// non-deterministic document-level GeneratedAt while staying strict on
// structure.
func TestCompare_TimestampToleranceOnly(t *testing.T) {
	a := &graph.Document{
		GeneratedAt: mustTime("2024-01-01T00:00:00Z"),
		Entities:    []graph.Entity{ent("SCOPE.Operation", "Alpha", "a.go")},
	}
	b := &graph.Document{
		GeneratedAt: mustTime("2025-06-25T12:34:56Z"),
		Entities:    []graph.Entity{ent("SCOPE.Operation", "Alpha", "a.go")},
	}
	if rep := Compare(a, b); !rep.Equivalent {
		t.Fatalf("timestamps must be tolerated; got:\n%s", rep.String())
	}
}

func TestCompare_DetectsEntityOnlyInOneSide(t *testing.T) {
	a := &graph.Document{Entities: []graph.Entity{
		ent("SCOPE.Operation", "Alpha", "a.go"),
		ent("SCOPE.Operation", "Gone", "g.go"),
	}}
	b := &graph.Document{Entities: []graph.Entity{
		ent("SCOPE.Operation", "Alpha", "a.go"),
	}}
	rep := Compare(a, b)
	if rep.Equivalent {
		t.Fatal("expected NOT equivalent (missing entity)")
	}
	if len(rep.EntitiesOnlyInA) != 1 {
		t.Fatalf("EntitiesOnlyInA=%v want 1", rep.EntitiesOnlyInA)
	}
	if !strings.Contains(rep.EntitiesOnlyInA[0], "Gone") {
		t.Errorf("diff should name the missing entity, got %q", rep.EntitiesOnlyInA[0])
	}
}

func TestCompare_DetectsEntityFieldDrift(t *testing.T) {
	base := ent("SCOPE.Operation", "Alpha", "a.go")
	drifted := base
	drifted.Signature = "func Alpha(x int)" // same identity, changed signature
	a := &graph.Document{Entities: []graph.Entity{base}}
	b := &graph.Document{Entities: []graph.Entity{drifted}}
	rep := Compare(a, b)
	if rep.Equivalent {
		t.Fatal("expected NOT equivalent (signature drift)")
	}
	if len(rep.EntityFieldDiffs) != 1 || !strings.Contains(rep.EntityFieldDiffs[0].Detail, "signature") {
		t.Fatalf("expected a signature field diff, got %+v", rep.EntityFieldDiffs)
	}
}

func TestCompare_DetectsRelationshipDrift(t *testing.T) {
	a := &graph.Document{Relationships: []graph.Relationship{
		{FromID: "x", ToID: "y", Kind: "CALLS"},
		{FromID: "x", ToID: "z", Kind: "CALLS"}, // only in A
	}}
	b := &graph.Document{Relationships: []graph.Relationship{
		{FromID: "x", ToID: "y", Kind: "CALLS"},
		{FromID: "x", ToID: "w", Kind: "CALLS"}, // only in B
	}}
	rep := Compare(a, b)
	if rep.Equivalent {
		t.Fatal("expected NOT equivalent (edge set differs)")
	}
	if len(rep.RelsOnlyInA) != 1 || len(rep.RelsOnlyInB) != 1 {
		t.Fatalf("RelsOnlyInA=%v RelsOnlyInB=%v want 1/1", rep.RelsOnlyInA, rep.RelsOnlyInB)
	}
}

func TestCompare_DetectsRelationshipPropertyDrift(t *testing.T) {
	a := &graph.Document{Relationships: []graph.Relationship{
		graph.Relationship{FromID: "x", ToID: "y", Kind: "CALLS"}.WithProperties(map[string]string{"callsite_count": "1"}),
	}}
	b := &graph.Document{Relationships: []graph.Relationship{
		graph.Relationship{FromID: "x", ToID: "y", Kind: "CALLS"}.WithProperties(map[string]string{"callsite_count": "2"}),
	}}
	rep := Compare(a, b)
	if rep.Equivalent || len(rep.RelPropDiffs) != 1 {
		t.Fatalf("expected one rel property diff, got equivalent=%v diffs=%+v", rep.Equivalent, rep.RelPropDiffs)
	}
}

// TestCompare_DetectsCommunityDrift is the headline case for #5309: the entity
// set and edge set are identical, but the incremental community phase relabelled
// a partition. The comparator must catch this in its dedicated bucket.
func TestCompare_DetectsCommunityDrift(t *testing.T) {
	e := ent("SCOPE.Operation", "Alpha", "a.go")
	ea, eb := e, e
	ea.CommunityID = ci(3)
	eb.CommunityID = ci(7) // drifted assignment
	a := &graph.Document{Entities: []graph.Entity{ea}}
	b := &graph.Document{Entities: []graph.Entity{eb}}
	rep := Compare(a, b)
	if rep.Equivalent {
		t.Fatal("expected NOT equivalent (community drift)")
	}
	if len(rep.CommunityAssignmentDiffs) != 1 {
		t.Fatalf("expected one community assignment diff, got %+v", rep.CommunityAssignmentDiffs)
	}
	if len(rep.CommunitySetDiff) == 0 {
		t.Error("expected a corpus community membership diff too")
	}
}

func TestCompare_SameCommunityIsEquivalent(t *testing.T) {
	e := ent("SCOPE.Operation", "Alpha", "a.go")
	ea, eb := e, e
	ea.CommunityID = ci(5)
	eb.CommunityID = ci(5)
	a := &graph.Document{Entities: []graph.Entity{ea}}
	b := &graph.Document{Entities: []graph.Entity{eb}}
	if rep := Compare(a, b); !rep.Equivalent {
		t.Fatalf("identical community assignment must be equivalent, got:\n%s", rep.String())
	}
}

// TestCompareWithOptions_TolerancesNarrowlyApply verifies each tolerance knob
// suppresses exactly its target dimension and nothing else.
func TestCompareWithOptions_TolerancesNarrowlyApply(t *testing.T) {
	// Ignored relationship kind: a CONTAINS edge present only in A is tolerated,
	// but a CALLS edge only in A is still caught.
	a := &graph.Document{Relationships: []graph.Relationship{
		{FromID: "m", ToID: "x", Kind: "CONTAINS"},
		{FromID: "x", ToID: "y", Kind: "CALLS"},
	}}
	b := &graph.Document{Relationships: []graph.Relationship{
		{FromID: "x", ToID: "y", Kind: "CALLS"},
	}}
	opts := Options{IgnoreRelKinds: map[string]bool{"CONTAINS": true}}
	if rep := CompareWithOptions(a, b, opts); !rep.Equivalent {
		t.Fatalf("CONTAINS should be tolerated:\n%s", rep.String())
	}
	// Now make the CALLS edge diverge too — must be caught even with the tolerance.
	b.Relationships = nil
	if rep := CompareWithOptions(a, b, opts); rep.Equivalent {
		t.Fatal("a missing CALLS edge must still be caught under a CONTAINS tolerance")
	}

	// Ignored entity property: `module` drift tolerated, `kind` still strict.
	ea := ent("SCOPE.Operation", "Alpha", "a.go")
	eb := ea
	ea.PropsReplace(map[string]string{"module": "core"})
	eb.PropsReplace(map[string]string{"module": "api"})
	da := &graph.Document{Entities: []graph.Entity{ea}}
	db := &graph.Document{Entities: []graph.Entity{eb}}
	popts := Options{IgnoreEntityProps: map[string]bool{"module": true}}
	if rep := CompareWithOptions(da, db, popts); !rep.Equivalent {
		t.Fatalf("module property drift should be tolerated:\n%s", rep.String())
	}
}

// TestCompareWithOptions_NormalizeStubEndpoints folds an un-resolved stub
// endpoint onto the entity it names so a structurally-identical edge matches.
func TestCompareWithOptions_NormalizeStubEndpoints(t *testing.T) {
	target := ent("SCOPE.Operation", "Target", "t.go")
	caller := ent("SCOPE.Operation", "Caller", "c.go")
	// Full rebuild: edge uses resolved hashed ids.
	a := &graph.Document{
		Entities:      []graph.Entity{target, caller},
		Relationships: []graph.Relationship{{FromID: caller.ID, ToID: target.ID, Kind: "CALLS"}},
	}
	// Incremental: same edge, but FromID left as a stub naming the caller.
	b := &graph.Document{
		Entities:      []graph.Entity{target, caller},
		Relationships: []graph.Relationship{{FromID: "scope:operation:method:go:c.go:Caller", ToID: target.ID, Kind: "CALLS"}},
	}
	if rep := Compare(a, b); rep.Equivalent {
		t.Fatal("strict compare should see the stub vs hashed FromID as different edges")
	}
	if rep := CompareWithOptions(a, b, Options{NormalizeStubEndpoints: true}); !rep.Equivalent {
		t.Fatalf("stub endpoint should normalize to the named entity:\n%s", rep.String())
	}
}

func TestReport_StringIsEmptyish_WhenEquivalent(t *testing.T) {
	rep := Compare(&graph.Document{}, &graph.Document{})
	if !rep.Equivalent {
		t.Fatal("two empty docs must be equivalent")
	}
	if !strings.Contains(rep.String(), "equivalent") {
		t.Errorf("String() = %q", rep.String())
	}
}
