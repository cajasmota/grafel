package fbwriter_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/graph/fbreader"
	"github.com/cajasmota/archigraph/internal/graph/fbwriter"
)

func TestRoundtripSmallGraph(t *testing.T) {
	doc := &graph.Document{
		Version:     1,
		GeneratedAt: time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC),
		Repo:        "fixture-mini",
		Entities: []graph.Entity{
			{ID: "ent0000000000000a", Name: "foo", Kind: "function", SourceFile: "a.go", StartLine: 10, Properties: map[string]string{"module": "pkg/a"}},
			{ID: "ent0000000000000b", Name: "bar", Kind: "function", SourceFile: "b.go", StartLine: 20, Properties: map[string]string{"module": "pkg/b", "visibility": "public"}},
			{ID: "ent0000000000000c", Name: "Baz", Kind: "type", SourceFile: "c.go", StartLine: 30},
		},
		Relationships: []graph.Relationship{
			{ID: "rel000000000000aa", FromID: "ent0000000000000a", ToID: "ent0000000000000b", Kind: "calls"},
			{ID: "rel000000000000ab", FromID: "ent0000000000000a", ToID: "ent0000000000000c", Kind: "references", Properties: map[string]string{"resolved": "true"}},
			{ID: "rel000000000000bc", FromID: "ent0000000000000b", ToID: "ent0000000000000c", Kind: "calls"},
		},
	}
	doc.Stats.Entities = len(doc.Entities)
	doc.Stats.Relationships = len(doc.Relationships)

	out := filepath.Join(t.TempDir(), "graph.fb")
	if err := fbwriter.WriteAtomic(out, doc); err != nil {
		t.Fatalf("write: %v", err)
	}

	r, err := fbreader.Open(out)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer r.Close()

	if got := r.Version(); got != fbwriter.FormatVersion {
		t.Errorf("version: got %d want %d", got, fbwriter.FormatVersion)
	}
	if got := r.EntityCount(); got != 3 {
		t.Errorf("entity count: got %d want 3", got)
	}
	if got := r.RelationshipCount(); got != 3 {
		t.Errorf("relationship count: got %d want 3", got)
	}

	ent := r.LookupEntityByID("ent0000000000000b")
	if ent == nil {
		t.Fatal("lookup ent0000000000000b: nil")
	}
	if got := string(ent.Name()); got != "bar" {
		t.Errorf("name: got %q want %q", got, "bar")
	}
	if got := string(ent.SourceFile()); got != "b.go" {
		t.Errorf("source_file: got %q want %q", got, "b.go")
	}
	if got := ent.SourceLine(); got != 20 {
		t.Errorf("source_line: got %d want 20", got)
	}

	// Negative lookup.
	if got := r.LookupEntityByID("does-not-exist"); got != nil {
		t.Errorf("expected nil for missing key, got %+v", got)
	}

	// Relationship traversal by from_id.
	out2 := r.IterateRelationshipsFromID("ent0000000000000a")
	if len(out2) != 2 {
		t.Errorf("rels from a: got %d want 2", len(out2))
	}
}
