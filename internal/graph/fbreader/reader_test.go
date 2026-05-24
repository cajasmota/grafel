package fbreader_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/graph/fbreader"
	fbgraph "github.com/cajasmota/archigraph/internal/graph/fbgraph"
	"github.com/cajasmota/archigraph/internal/graph/fbwriter"
	"github.com/cajasmota/archigraph/internal/types"
)

func writeAndOpen(t *testing.T, doc *graph.Document) *fbreader.Reader {
	t.Helper()
	path := filepath.Join(t.TempDir(), "g.fb")
	if err := fbwriter.WriteAtomic(path, doc); err != nil {
		t.Fatalf("write: %v", err)
	}
	r, err := fbreader.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestPhaseDReaderMethods(t *testing.T) {
	doc := &graph.Document{
		Repo:        "demo",
		GeneratedAt: time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC),
		Entities: []graph.Entity{
			{ID: "a", QualifiedName: "pkg.A", Kind: "function", Name: "A"},
			{ID: "b", QualifiedName: "pkg.B", Kind: "struct", Name: "B"},
			{ID: "c", QualifiedName: "pkg.C", Kind: "function", Name: "C"},
		},
		Relationships: []graph.Relationship{
			{FromID: "a", ToID: "b", Kind: "CALLS"},
			{FromID: "c", ToID: "b", Kind: "CALLS"},
			{FromID: "a", ToID: "c", Kind: "REFERENCES"},
		},
	}
	r := writeAndOpen(t, doc)

	if r.LookupEntityByID("b") == nil {
		t.Errorf("expected entity b")
	}

	fromA := r.IterateRelationshipsFromID("a")
	if len(fromA) != 2 {
		t.Errorf("from a: got %d, want 2", len(fromA))
	}

	toB := r.IterateRelationshipsToID("b")
	if len(toB) != 2 {
		t.Errorf("to b: got %d, want 2", len(toB))
	}

	funcs := r.FilterEntitiesByKind("function")
	if len(funcs) != 2 {
		t.Errorf("functions: got %d, want 2", len(funcs))
	}

	meta := r.LoadGraphMeta()
	if meta.Version == 0 {
		t.Errorf("expected non-zero version")
	}
	if meta.RepoTag != "demo" {
		t.Errorf("repo tag = %q, want demo", meta.RepoTag)
	}
	if meta.ComputedAt == "" {
		t.Errorf("expected non-empty computed_at")
	}
}

// TestAgentPatternKindRoundTrip verifies that the new "AgentPattern" entity kind
// and the ADR-0018 pattern edge kinds (EXEMPLAR, TOUCHES, ANTI_EXEMPLAR,
// SUPERSEDES, CONFLICTS_WITH, CO_APPLIES_WITH, PREREQUISITE, CREATED_BY) survive
// a write → read round-trip through the FlatBuffers format.
//
// The FB schema stores kind as a free-form string, so no schema change is
// required — this test confirms the strings are preserved exactly.
func TestAgentPatternKindRoundTrip(t *testing.T) {
	patternID := "adr0018pattern001"
	entityID := "someentity0000001"
	pattern2ID := "adr0018pattern002"

	doc := &graph.Document{
		Repo:        "patterns-test",
		GeneratedAt: time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC),
		Entities: []graph.Entity{
			{
				ID:   patternID,
				Kind: string(types.EntityKindAgentPattern),
				Name: "chi-handler-pattern",
				Properties: map[string]string{
					"category":   "code",
					"confidence": "0.75",
				},
			},
			{
				ID:   entityID,
				Kind: string(types.EntityKindFunction),
				Name: "RegisterRoutes",
			},
			{
				ID:   pattern2ID,
				Kind: string(types.EntityKindAgentPattern),
				Name: "old-handler-pattern",
			},
		},
		Relationships: []graph.Relationship{
			{ID: "r01", FromID: patternID, ToID: entityID, Kind: string(types.RelationshipKindExemplar)},
			{ID: "r02", FromID: patternID, ToID: entityID, Kind: string(types.RelationshipKindTouches)},
			{ID: "r03", FromID: patternID, ToID: entityID, Kind: string(types.RelationshipKindAntiExemplar)},
			{ID: "r04", FromID: patternID, ToID: pattern2ID, Kind: string(types.RelationshipKindSupersedes)},
			{ID: "r05", FromID: patternID, ToID: pattern2ID, Kind: string(types.RelationshipKindConflictsWith)},
			{ID: "r06", FromID: patternID, ToID: pattern2ID, Kind: string(types.RelationshipKindCoAppliesWith)},
			{ID: "r07", FromID: patternID, ToID: pattern2ID, Kind: string(types.RelationshipKindPrerequisite)},
			{ID: "r08", FromID: entityID, ToID: patternID, Kind: string(types.RelationshipKindCreatedBy)},
		},
	}
	doc.Stats.Entities = len(doc.Entities)
	doc.Stats.Relationships = len(doc.Relationships)

	r := writeAndOpen(t, doc)

	// Verify AgentPattern entity kind survives.
	agentPatternEntity := r.LookupEntityByID(patternID)
	if agentPatternEntity == nil {
		t.Fatal("AgentPattern entity not found after round-trip")
	}
	if got := string(agentPatternEntity.Kind()); got != string(types.EntityKindAgentPattern) {
		t.Errorf("kind: got %q want %q", got, types.EntityKindAgentPattern)
	}
	if got := string(agentPatternEntity.Name()); got != "chi-handler-pattern" {
		t.Errorf("name: got %q want chi-handler-pattern", got)
	}

	// Verify all 8 pattern edge kinds survive via IterateRelationshipsFromID /
	// IterateRelationshipsToID.
	fromPattern := r.IterateRelationshipsFromID(patternID)
	if len(fromPattern) != 7 {
		t.Errorf("relationships from pattern: got %d want 7", len(fromPattern))
	}

	// Collect actual kinds from the round-tripped relationships.
	kindSet := map[string]bool{}
	for _, rel := range fromPattern {
		kindSet[string(rel.Kind())] = true
	}

	wantKinds := []types.RelationshipKind{
		types.RelationshipKindExemplar,
		types.RelationshipKindTouches,
		types.RelationshipKindAntiExemplar,
		types.RelationshipKindSupersedes,
		types.RelationshipKindConflictsWith,
		types.RelationshipKindCoAppliesWith,
		types.RelationshipKindPrerequisite,
	}
	for _, k := range wantKinds {
		if !kindSet[string(k)] {
			t.Errorf("edge kind %q not found after round-trip", k)
		}
	}

	// Verify CREATED_BY (incoming to pattern, from entity).
	toPattern := r.IterateRelationshipsToID(patternID)
	if len(toPattern) != 1 {
		t.Errorf("relationships to pattern: got %d want 1", len(toPattern))
	}
	if len(toPattern) == 1 {
		if got := string(toPattern[0].Kind()); got != string(types.RelationshipKindCreatedBy) {
			t.Errorf("CREATED_BY kind: got %q want %q", got, types.RelationshipKindCreatedBy)
		}
	}

	// Confirm the count of AgentPattern entities.
	agentPatterns := r.FilterEntitiesByKind(string(types.EntityKindAgentPattern))
	if len(agentPatterns) != 2 {
		t.Errorf("FilterEntitiesByKind(AgentPattern): got %d want 2", len(agentPatterns))
	}
}

// TestIterateEntitiesCallback verifies IterateEntities (S8 callback API).
func TestIterateEntitiesCallback(t *testing.T) {
	doc := &graph.Document{
		Repo: "iter-test",
		Entities: []graph.Entity{
			{ID: "e1", Kind: "function", Name: "Foo"},
			{ID: "e2", Kind: "struct", Name: "Bar"},
			{ID: "e3", Kind: "function", Name: "Baz"},
		},
		Relationships: []graph.Relationship{
			{FromID: "e1", ToID: "e2", Kind: "CALLS"},
			{FromID: "e2", ToID: "e3", Kind: "CALLS"},
		},
	}
	r := writeAndOpen(t, doc)

	// Full iteration via IterateEntities callback.
	var ids []string
	r.IterateEntities(func(e *fbgraph.Entity) bool {
		ids = append(ids, string(e.Id()))
		return true
	})
	if len(ids) != 3 {
		t.Errorf("IterateEntities: got %d ids, want 3", len(ids))
	}

	// Early stop.
	count := 0
	r.IterateEntities(func(_ *fbgraph.Entity) bool {
		count++
		return false // stop after first
	})
	if count != 1 {
		t.Errorf("IterateEntities early-stop: visited %d, want 1", count)
	}

	// IterateRelationships full.
	var rels int
	r.IterateRelationships(func(_ *fbgraph.Relationship) bool {
		rels++
		return true
	})
	if rels != 2 {
		t.Errorf("IterateRelationships: got %d, want 2", rels)
	}

	// FindEntityByID ok-idiom.
	if e, ok := r.FindEntityByID("e2"); !ok || e == nil {
		t.Errorf("FindEntityByID(e2): ok=%v, e=%v", ok, e)
	}
	if _, ok := r.FindEntityByID("missing"); ok {
		t.Errorf("FindEntityByID(missing): expected ok=false")
	}
}

// TestEntityIteratorPull verifies the pull-style EntityIterator (S8).
func TestEntityIteratorPull(t *testing.T) {
	doc := &graph.Document{
		Repo: "pull-iter-test",
		Entities: []graph.Entity{
			{ID: "x1", Kind: "function", Name: "X1"},
			{ID: "x2", Kind: "function", Name: "X2"},
		},
		Relationships: []graph.Relationship{
			{FromID: "x1", ToID: "x2", Kind: "CALLS"},
		},
	}
	r := writeAndOpen(t, doc)

	var names []string
	it := fbreader.NewEntityIterator(r)
	for it.Next() {
		names = append(names, string(it.Entity().Name()))
	}
	if len(names) != 2 {
		t.Errorf("EntityIterator: got %d, want 2", len(names))
	}

	var kinds []string
	rit := fbreader.NewRelationshipIterator(r)
	for rit.Next() {
		kinds = append(kinds, string(rit.Relationship().Kind()))
	}
	if len(kinds) != 1 || kinds[0] != "CALLS" {
		t.Errorf("RelationshipIterator: got %v, want [CALLS]", kinds)
	}
}

// TestEmbeddingRefRoundTrip verifies that the PH8 embedding_ref field
// survives a write → read FlatBuffers round-trip (#2100).
//
// Backward-compat:
//   - an entity WITHOUT embedding_ref reads back as "".
//   - an entity WITH embedding_ref reads back verbatim.
//   - older graphs (without the field) continue to return "" (FlatBuffers
//     defaults absent fields to the zero value).
func TestEmbeddingRefRoundTrip(t *testing.T) {
	const wantRef = "abcdef1234567890abcdef1234567890abcdef12"

	doc := &graph.Document{
		Repo:        "ph8-test",
		GeneratedAt: time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC),
		Entities: []graph.Entity{
			// entity with embedding_ref
			{ID: "e1", Kind: "function", Name: "Foo", EmbeddingRef: wantRef},
			// entity without embedding_ref (pre-PH8 style)
			{ID: "e2", Kind: "function", Name: "Bar"},
		},
	}

	r := writeAndOpen(t, doc)

	// Check entity with ref.
	e1 := r.LookupEntityByID("e1")
	if e1 == nil {
		t.Fatal("entity e1 not found")
	}
	if got := fbreader.EntityEmbeddingRef(e1); got != wantRef {
		t.Errorf("e1 EmbeddingRef: got %q want %q", got, wantRef)
	}

	// Check entity without ref — should return "".
	e2 := r.LookupEntityByID("e2")
	if e2 == nil {
		t.Fatal("entity e2 not found")
	}
	if got := fbreader.EntityEmbeddingRef(e2); got != "" {
		t.Errorf("e2 EmbeddingRef: got %q want empty", got)
	}
}
