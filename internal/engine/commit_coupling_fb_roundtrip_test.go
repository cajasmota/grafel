// Round-trip test for commit-coupling edges through graph.fb.
//
// The .fb schema treats Kind as an opaque string (see internal/graph/
// fbwriter/writer.go: kindOff := b.CreateString(e.Kind) / r.Kind), so a new
// edge kind needs no schema bump. This test verifies that empirically by
// writing a Document containing File entities + COMMIT_COUPLED edges to .fb,
// reading it back via the standard loader, and comparing counts + a sample
// edge's properties.
package engine_test

import (
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/engine"
	"github.com/cajasmota/grafel/internal/graph"
	fbwriter "github.com/cajasmota/grafel/internal/graph/fbwriter"
)

func TestCommitCoupling_FlatBufferRoundTrip(t *testing.T) {
	// Hand-build a tiny Document mimicking what ApplyCommitCoupling would
	// produce — avoids needing a real git fixture in this test.
	doc := &graph.Document{
		Repo:    "fixture",
		Version: graph.SchemaVersion,
	}
	aID := graph.EntityID("fixture", engine.KindFile, "a.go", "a.go")
	bID := graph.EntityID("fixture", engine.KindFile, "b.go", "b.go")
	doc.Entities = []graph.Entity{
		graph.Entity{ID: aID, Name: "a.go", Kind: engine.KindFile, SourceFile: "a.go"}.WithProperties(map[string]string{"synthetic": "true", "source": "commit-coupling"}),
		graph.Entity{ID: bID, Name: "b.go", Kind: engine.KindFile, SourceFile: "b.go"}.WithProperties(map[string]string{"synthetic": "true", "source": "commit-coupling"}),
	}
	relID := graph.RelationshipID(aID, bID, engine.KindCommitCoupled)
	doc.Relationships = []graph.Relationship{
		graph.Relationship{
			ID:     relID,
			FromID: aID,
			ToID:   bID,
			Kind:   engine.KindCommitCoupled,
		}.WithProperties(map[string]string{
			"support":    "7",
			"confidence": "0.7000",
		},
		),
	}
	doc.Stats.Entities = len(doc.Entities)
	doc.Stats.Relationships = len(doc.Relationships)

	dir := t.TempDir()
	fbPath := filepath.Join(dir, "graph.fb")
	if err := fbwriter.WriteAtomic(fbPath, doc); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}

	// LoadGraphFromDir picks graph.fb when present.
	loaded, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	if len(loaded.Entities) != 2 {
		t.Fatalf("entities round-tripped: got %d, want 2", len(loaded.Entities))
	}
	if len(loaded.Relationships) != 1 {
		t.Fatalf("relationships round-tripped: got %d, want 1", len(loaded.Relationships))
	}
	r := loaded.Relationships[0]
	if r.Kind != engine.KindCommitCoupled {
		t.Errorf("kind round-trip: got %q, want %q", r.Kind, engine.KindCommitCoupled)
	}
	if r.PropGet("support") != "7" {
		t.Errorf("support property lost: got %q", r.PropGet("support"))
	}
	if r.PropGet("confidence") != "0.7000" {
		t.Errorf("confidence property lost: got %q", r.PropGet("confidence"))
	}
	// Entity kind round-trip.
	foundFile := false
	for _, e := range loaded.Entities {
		if e.Kind == engine.KindFile {
			foundFile = true
		}
	}
	if !foundFile {
		t.Errorf("File entity kind did not survive round-trip")
	}
}
