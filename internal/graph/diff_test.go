// diff_test.go — unit tests for the graph diff engine.
package graph

import (
	"testing"
)

// diffTestEntity returns a minimal Entity with the given id, kind, name, and file.
func diffTestEntity(id, kind, name, sourceFile string, startLine, endLine int) Entity {
	return Entity{
		ID:         id,
		Kind:       kind,
		Name:       name,
		SourceFile: sourceFile,
		StartLine:  startLine,
		EndLine:    endLine,
	}
}

// diffTestRel returns a minimal Relationship.
func diffTestRel(fromID, toID, kind string) Relationship {
	return Relationship{
		ID:     RelationshipID(fromID, toID, kind),
		FromID: fromID,
		ToID:   toID,
		Kind:   kind,
	}
}

// TestDiffRefs_AddedRemovedModified verifies the three entity change buckets
// when both graphs have a known delta.
func TestDiffRefs_AddedRemovedModified(t *testing.T) {
	t.Parallel()
	docA := &Document{
		Entities: []Entity{
			diffTestEntity("aaa", "Function", "funcA", "pkg/a.go", 10, 20),
			diffTestEntity("bbb", "Function", "funcB", "pkg/b.go", 5, 15),
			diffTestEntity("ccc", "Class", "MyClass", "pkg/c.go", 1, 50),
		},
	}
	docB := &Document{
		Entities: []Entity{
			// bbb — same everything, NOT modified.
			diffTestEntity("bbb", "Function", "funcB", "pkg/b.go", 5, 15),
			// ccc — name changed → modified.
			diffTestEntity("ccc", "Class", "MyRenamedClass", "pkg/c.go", 1, 50),
			// ddd — brand new → added.
			diffTestEntity("ddd", "Function", "funcD", "pkg/d.go", 1, 10),
		},
		// aaa is absent → removed.
	}

	got := DiffDocs(docA, docB)

	// Added
	if len(got.Entities.Added) != 1 {
		t.Fatalf("want 1 added, got %d: %v", len(got.Entities.Added), got.Entities.Added)
	}
	if got.Entities.Added[0].ID != "ddd" {
		t.Errorf("added entity should be ddd, got %s", got.Entities.Added[0].ID)
	}

	// Removed
	if len(got.Entities.Removed) != 1 {
		t.Fatalf("want 1 removed, got %d: %v", len(got.Entities.Removed), got.Entities.Removed)
	}
	if got.Entities.Removed[0].ID != "aaa" {
		t.Errorf("removed entity should be aaa, got %s", got.Entities.Removed[0].ID)
	}

	// Modified
	if len(got.Entities.Modified) != 1 {
		t.Fatalf("want 1 modified, got %d: %v", len(got.Entities.Modified), got.Entities.Modified)
	}
	mod := got.Entities.Modified[0]
	if mod.ID != "ccc" {
		t.Errorf("modified entity should be ccc, got %s", mod.ID)
	}
	foundNameField := false
	for _, f := range mod.ModifiedFields {
		if f == "name" {
			foundNameField = true
		}
	}
	if !foundNameField {
		t.Errorf("modified entity ccc should have 'name' in ModifiedFields, got %v", mod.ModifiedFields)
	}

	// Summary
	if got.Summary.EntitiesAdded != 1 {
		t.Errorf("summary.entities_added want 1, got %d", got.Summary.EntitiesAdded)
	}
	if got.Summary.EntitiesRemoved != 1 {
		t.Errorf("summary.entities_removed want 1, got %d", got.Summary.EntitiesRemoved)
	}
	if got.Summary.EntitiesModified != 1 {
		t.Errorf("summary.entities_modified want 1, got %d", got.Summary.EntitiesModified)
	}
}

// TestDiffRefs_RelationshipSetDiff verifies relationship-level added/removed.
func TestDiffRefs_RelationshipSetDiff(t *testing.T) {
	t.Parallel()
	docA := &Document{
		Entities: []Entity{
			diffTestEntity("e1", "Function", "fn1", "a.go", 1, 5),
			diffTestEntity("e2", "Function", "fn2", "a.go", 6, 10),
			diffTestEntity("e3", "Function", "fn3", "a.go", 11, 15),
		},
		Relationships: []Relationship{
			diffTestRel("e1", "e2", "calls"),
			diffTestRel("e2", "e3", "calls"),
		},
	}
	docB := &Document{
		Entities: []Entity{
			diffTestEntity("e1", "Function", "fn1", "a.go", 1, 5),
			diffTestEntity("e2", "Function", "fn2", "a.go", 6, 10),
			diffTestEntity("e3", "Function", "fn3", "a.go", 11, 15),
			diffTestEntity("e4", "Function", "fn4", "a.go", 16, 20),
		},
		Relationships: []Relationship{
			// e1→e2 kept.
			diffTestRel("e1", "e2", "calls"),
			// e2→e3 removed (not in docB).
			// NEW: e1→e4, e3→e4.
			diffTestRel("e1", "e4", "calls"),
			diffTestRel("e3", "e4", "calls"),
		},
	}

	got := DiffDocs(docA, docB)

	if got.Summary.RelationshipsAdded != 2 {
		t.Errorf("want 2 relationships added, got %d", got.Summary.RelationshipsAdded)
	}
	if got.Summary.RelationshipsRemoved != 1 {
		t.Errorf("want 1 relationship removed, got %d", got.Summary.RelationshipsRemoved)
	}

	// Verify the removed rel is e2→e3/calls.
	if len(got.Relationships.Removed) != 1 {
		t.Fatalf("want 1 removed rel, got %d", len(got.Relationships.Removed))
	}
	rem := got.Relationships.Removed[0]
	if rem.FromID != "e2" || rem.ToID != "e3" || rem.Kind != "calls" {
		t.Errorf("removed rel want e2→e3/calls, got %s→%s/%s", rem.FromID, rem.ToID, rem.Kind)
	}
}

// TestDiffRefs_IdenticalGraphs verifies that two identical graphs produce an
// empty diff.
func TestDiffRefs_IdenticalGraphs(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		diffTestEntity("x1", "Function", "fn1", "a.go", 1, 5),
		diffTestEntity("x2", "Class", "MyClass", "b.go", 1, 100),
	}
	rels := []Relationship{diffTestRel("x1", "x2", "uses")}

	docA := &Document{Entities: entities, Relationships: rels}
	docB := &Document{Entities: entities, Relationships: rels}

	got := DiffDocs(docA, docB)

	if got.Summary.EntitiesAdded != 0 || got.Summary.EntitiesRemoved != 0 || got.Summary.EntitiesModified != 0 {
		t.Errorf("identical graphs should have zero entity changes, got %+v", got.Summary)
	}
	if got.Summary.RelationshipsAdded != 0 || got.Summary.RelationshipsRemoved != 0 {
		t.Errorf("identical graphs should have zero relationship changes, got %+v", got.Summary)
	}
}

// TestDiffRefs_EmptyDocuments verifies that diffing two empty documents is safe.
func TestDiffRefs_EmptyDocuments(t *testing.T) {
	t.Parallel()
	got := DiffDocs(&Document{}, &Document{})

	if got.Summary.EntitiesAdded != 0 {
		t.Errorf("want 0 added, got %d", got.Summary.EntitiesAdded)
	}
	// Ensure non-nil slices.
	if got.Entities.Added == nil {
		t.Error("Entities.Added should not be nil")
	}
	if got.Relationships.Added == nil {
		t.Error("Relationships.Added should not be nil")
	}
}

// TestDiffRefs_SourceWindowChange verifies that changing the line range of
// an entity (without renaming it) is detected as a source_window modification.
func TestDiffRefs_SourceWindowChange(t *testing.T) {
	t.Parallel()
	docA := &Document{
		Entities: []Entity{diffTestEntity("zzz", "Function", "fn", "a.go", 1, 10)},
	}
	docB := &Document{
		Entities: []Entity{diffTestEntity("zzz", "Function", "fn", "a.go", 5, 50)},
	}

	got := DiffDocs(docA, docB)

	if got.Summary.EntitiesModified != 1 {
		t.Fatalf("want 1 modified, got %d", got.Summary.EntitiesModified)
	}
	mod := got.Entities.Modified[0]
	found := false
	for _, f := range mod.ModifiedFields {
		if f == "source_window" {
			found = true
		}
	}
	if !found {
		t.Errorf("want 'source_window' in ModifiedFields, got %v", mod.ModifiedFields)
	}
}
