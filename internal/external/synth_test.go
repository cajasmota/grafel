package external

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// TestSynthesize_HappyPath confirms an IMPORTS-django relationship
// produces a single ext:django placeholder and rewrites the edge.
func TestSynthesize_HappyPath(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "0123456789abcdef", Name: "models", Kind: "SCOPE.Component", SourceFile: "myapp/models.py"},
		},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "myapp/models.py", ToID: "django.db.models", Kind: "IMPORTS"},
		},
	}
	stats := Synthesize(doc)
	if stats.Synthesized != 1 {
		t.Fatalf("synthesized=%d, want 1", stats.Synthesized)
	}
	if stats.RelationshipsResolved != 1 {
		t.Fatalf("resolved=%d, want 1", stats.RelationshipsResolved)
	}
	if doc.Relationships[0].ToID != "ext:django" {
		t.Fatalf("rel ToID=%q, want ext:django", doc.Relationships[0].ToID)
	}
	found := false
	for _, e := range doc.Entities {
		if e.ID == "ext:django" {
			found = true
			if e.Kind != KindExternal {
				t.Fatalf("placeholder kind=%q, want %q", e.Kind, KindExternal)
			}
			if e.Subtype != "package" {
				t.Fatalf("placeholder subtype=%q, want package", e.Subtype)
			}
			if v, ok := e.Metadata["is_external"].(bool); !ok || !v {
				t.Fatalf("placeholder is_external missing or false: %+v", e.Metadata)
			}
		}
	}
	if !found {
		t.Fatalf("ext:django entity not appended; entities=%+v", doc.Entities)
	}
}

// TestSynthesize_Idempotent confirms running the pass twice on the
// same document doesn't create duplicate placeholders.
func TestSynthesize_Idempotent(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "a", ToID: "django", Kind: "IMPORTS"},
			{ID: "rel-2", FromID: "b", ToID: "django.db", Kind: "IMPORTS"},
		},
	}
	first := Synthesize(doc)
	if first.Synthesized != 1 {
		t.Fatalf("first run synthesized=%d, want 1", first.Synthesized)
	}
	beforeEntities := len(doc.Entities)
	second := Synthesize(doc)
	if second.Synthesized != 0 {
		t.Fatalf("second run synthesized=%d, want 0 (idempotent)", second.Synthesized)
	}
	if len(doc.Entities) != beforeEntities {
		t.Fatalf("second run grew entities from %d to %d", beforeEntities, len(doc.Entities))
	}
	// Both relationships should now point at ext:django.
	for k, r := range doc.Relationships {
		if r.ToID != "ext:django" {
			t.Fatalf("rel[%d].ToID=%q, want ext:django", k, r.ToID)
		}
	}
}

// TestSynthesize_LocalUnaffected confirms relationships pointing at
// already-resolved (hex-id) entities are not touched.
func TestSynthesize_LocalUnaffected(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "0123456789abcdef", Name: "Foo", Kind: "Function"},
			{ID: "fedcba9876543210", Name: "Bar", Kind: "Function"},
		},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "0123456789abcdef", ToID: "fedcba9876543210", Kind: "CALLS"},
		},
	}
	stats := Synthesize(doc)
	if stats.Synthesized != 0 || stats.RelationshipsResolved != 0 {
		t.Fatalf("expected no synthesis on hex-resolved edges; got %+v", stats)
	}
	if doc.Relationships[0].ToID != "fedcba9876543210" {
		t.Fatalf("local edge was rewritten: ToID=%q", doc.Relationships[0].ToID)
	}
	if len(doc.Entities) != 2 {
		t.Fatalf("entity count changed: %d", len(doc.Entities))
	}
}

// TestSynthesize_StdlibBareName confirms a bare "Println" stub becomes
// ext:Println with subtype function.
func TestSynthesize_StdlibBareName(t *testing.T) {
	doc := &graph.Document{
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "main.go", ToID: "Println", Kind: "CALLS"},
		},
	}
	stats := Synthesize(doc)
	if stats.Synthesized != 1 {
		t.Fatalf("synthesized=%d, want 1", stats.Synthesized)
	}
	if doc.Relationships[0].ToID != "ext:Println" {
		t.Fatalf("ToID=%q", doc.Relationships[0].ToID)
	}
	if doc.Entities[0].Subtype != "function" {
		t.Fatalf("subtype=%q, want function", doc.Entities[0].Subtype)
	}
}

// TestSynthesize_UnknownLeftAlone confirms truly-unknown stubs are
// neither rewritten nor synthesised — they continue to count as
// "unmatched" upstream.
func TestSynthesize_UnknownLeftAlone(t *testing.T) {
	doc := &graph.Document{
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "a", ToID: "SomeRandomLocalThing", Kind: "CALLS"},
		},
	}
	stats := Synthesize(doc)
	if stats.Synthesized != 0 {
		t.Fatalf("synthesized=%d, want 0", stats.Synthesized)
	}
	if doc.Relationships[0].ToID != "SomeRandomLocalThing" {
		t.Fatalf("unknown stub was rewritten to %q", doc.Relationships[0].ToID)
	}
}

// TestSynthesize_NilDoc confirms calling on a nil document is a no-op.
func TestSynthesize_NilDoc(t *testing.T) {
	stats := Synthesize(nil)
	if stats.Synthesized != 0 || stats.RelationshipsResolved != 0 {
		t.Fatalf("nil doc produced stats: %+v", stats)
	}
}
