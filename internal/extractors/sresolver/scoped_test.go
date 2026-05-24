package sresolver_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/extractors/sresolver"
	"github.com/cajasmota/archigraph/internal/graph"
)

// ─────────────────────────────────────────────────────────────────────────────
// ResolveScoped unit tests
// ─────────────────────────────────────────────────────────────────────────────

func makeEntity(id, name, qualName, sourceFile string) graph.Entity {
	return graph.Entity{
		ID:            id,
		Name:          name,
		QualifiedName: qualName,
		Kind:          "SCOPE.Operation",
		SourceFile:    sourceFile,
		Language:      "go",
	}
}

func makeRel(fromID, toID, kind string) graph.Relationship {
	return graph.Relationship{
		ID:     graph.RelationshipID(fromID, toID, kind),
		FromID: fromID,
		ToID:   toID,
		Kind:   kind,
	}
}

// TestResolveScoped_NoChanges verifies that when there are no new entities or
// relationships the existing ones are returned unchanged.
func TestResolveScoped_NoChanges(t *testing.T) {
	existing := []graph.Entity{
		makeEntity("aaa1bbbb11224455", "Alpha", "pkg.Alpha", "a.go"),
	}
	existingRel := makeRel("aaa1bbbb11224455", "bbb222cc44556677", "CALLS")
	existingRels := []graph.Relationship{existingRel}

	res := sresolver.ResolveScoped(
		nil,      // no new entities
		existing, // existing entities
		nil,      // no new rels
		existingRels,
		nil,
	)

	if res.FallbackRequired {
		t.Errorf("expected no fallback, got FallbackRequired=true target=%q", res.UnresolvedTarget)
	}
	if len(res.NewRelationships) != 1 {
		t.Errorf("expected 1 relationship, got %d", len(res.NewRelationships))
	}
}

// TestResolveScoped_StubToIDResolved verifies that a stub (bare-name) ToID in
// a new relationship is resolved to a hex ID when the entity is found.
func TestResolveScoped_StubToIDResolved(t *testing.T) {
	existing := []graph.Entity{
		makeEntity("beef0011beef0011", "Callee", "pkg.Callee", "callee.go"),
	}

	// New relationship from a newly extracted entity with a stub ToID.
	newRel := graph.Relationship{
		ID:     "stub0000stub0000",
		FromID: "cafe1234cafe1234",
		ToID:   "Callee", // bare name — should be resolved to hex ID
		Kind:   "CALLS",
	}

	res := sresolver.ResolveScoped(
		nil,
		existing,
		[]graph.Relationship{newRel},
		nil,
		nil,
	)

	if res.FallbackRequired {
		t.Fatalf("unexpected fallback: %s", res.UnresolvedTarget)
	}

	found := false
	for _, r := range res.NewRelationships {
		if r.Kind == "CALLS" && r.ToID == "beef0011beef0011" {
			found = true
		}
	}
	if !found {
		t.Errorf("stub ToID 'Callee' should have been resolved to 'beef0011beef0011', rels=%+v",
			res.NewRelationships)
	}
}

// TestResolveScoped_InboundFixed verifies that a surviving relationship
// whose ToID is a stub matching a newly extracted entity gets its ToID updated.
func TestResolveScoped_InboundFixed(t *testing.T) {
	// newEntity replaces the old entity that had the same name.
	newEntity := makeEntity("dead0001dead0001", "Alpha", "pkg.Alpha", "a.go")

	// Existing relationship (inbound): B → "Alpha" (stub, not yet hex)
	existingRel := graph.Relationship{
		ID:     "stub1111stub1111",
		FromID: "cafe5678cafe5678",
		ToID:   "Alpha", // bare name
		Kind:   "CALLS",
	}

	res := sresolver.ResolveScoped(
		[]graph.Entity{newEntity},
		nil,
		nil,
		[]graph.Relationship{existingRel},
		nil,
	)

	if res.FallbackRequired {
		t.Fatalf("unexpected fallback: %s", res.UnresolvedTarget)
	}
	if res.InboundFixed != 1 {
		t.Errorf("expected InboundFixed=1, got %d", res.InboundFixed)
	}
	// The ToID should now be the new entity's ID.
	found := false
	for _, r := range res.NewRelationships {
		if r.ToID == "dead0001dead0001" {
			found = true
		}
	}
	if !found {
		t.Errorf("inbound rel ToID should have been updated to 'dead0001dead0001', rels=%+v",
			res.NewRelationships)
	}
}

// TestResolveScoped_SafetyNetFallback verifies that when a surviving relationship
// has a stub ToID that matches a source-file path from the re-extracted file set
// but the file-entity is absent from newEntities, FallbackRequired is set.
func TestResolveScoped_SafetyNetFallback(t *testing.T) {
	// newEntities has KeepFunc from target.go, but NOT the file entity itself.
	newEntities := []graph.Entity{
		makeEntity("keep1234keep1234", "KeepFunc", "pkg.KeepFunc", "target.go"),
	}

	// Existing relationship: IMPORTS target.go (file entity) — but target.go's
	// file-level entity (SCOPE.Component/file) is not in newEntities.
	fileRel := graph.Relationship{
		ID:     "filerelfilerela0",
		FromID: "importer0123abcd",
		ToID:   "target.go", // ToID = source-file path, matches newFileSet
		Kind:   "IMPORTS",
	}

	res := sresolver.ResolveScoped(
		newEntities,
		nil,
		nil,
		[]graph.Relationship{fileRel},
		nil,
	)

	if !res.FallbackRequired {
		t.Error("expected FallbackRequired=true when stub ToID matches re-extracted file path but file-entity absent")
	}
	if res.UnresolvedTarget != "target.go" {
		t.Errorf("expected UnresolvedTarget='target.go', got %q", res.UnresolvedTarget)
	}
}

// TestResolveScoped_HexIDsUntouched verifies that relationships with hex IDs
// are passed through unchanged (no name resolution attempted).
func TestResolveScoped_HexIDsUntouched(t *testing.T) {
	existing := []graph.Entity{
		makeEntity("abcd1234abcd1234", "Foo", "pkg.Foo", "foo.go"),
	}

	existingRel := makeRel("cafe0000cafe0000", "abcd1234abcd1234", "CALLS")

	res := sresolver.ResolveScoped(
		nil,
		existing,
		nil,
		[]graph.Relationship{existingRel},
		nil,
	)

	if res.FallbackRequired {
		t.Errorf("unexpected fallback for hex-ID relationship")
	}
	if res.InboundFixed != 0 {
		t.Errorf("hex-ID rels should not be counted as fixed, got InboundFixed=%d", res.InboundFixed)
	}
}

// TestResolveScoped_MergeOrder verifies that existing rels come before new rels
// in the merged output.
func TestResolveScoped_MergeOrder(t *testing.T) {
	existingRel := makeRel("aaaa0000aaaa0000", "bbbb1111bbbb1111", "CALLS")
	newRel := makeRel("cccc2222cccc2222", "dddd3333dddd3333", "DEPENDS_ON")

	res := sresolver.ResolveScoped(
		nil, nil,
		[]graph.Relationship{newRel},
		[]graph.Relationship{existingRel},
		nil,
	)

	if res.FallbackRequired {
		t.Fatalf("unexpected fallback")
	}
	if len(res.NewRelationships) != 2 {
		t.Fatalf("expected 2 relationships, got %d", len(res.NewRelationships))
	}
	if res.NewRelationships[0].Kind != "CALLS" {
		t.Errorf("first relationship should be the existing CALLS rel, got %s", res.NewRelationships[0].Kind)
	}
	if res.NewRelationships[1].Kind != "DEPENDS_ON" {
		t.Errorf("second relationship should be the new DEPENDS_ON rel, got %s", res.NewRelationships[1].Kind)
	}
}
