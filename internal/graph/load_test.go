// Tests for the FB-first graph loader introduced by ADR-0016 flip-day (#808).
package graph_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// makeTestDoc creates a small Document for use in loader tests.
func makeTestDoc() *graph.Document {
	return &graph.Document{
		Version:     graph.SchemaVersion,
		GeneratedAt: time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC),
		Repo:        "test-repo",
		Entities: []graph.Entity{
			graph.Entity{
				ID:            "aabbccdd00000001",
				Name:          "MyHandler",
				QualifiedName: "pkg.MyHandler",
				Kind:          "FUNCTION",
				SourceFile:    "handler.go",
				StartLine:     10,
			}.WithProperties(map[string]string{"language": "go"}),
			graph.Entity{
				ID:            "aabbccdd00000002",
				Name:          "OtherFunc",
				QualifiedName: "pkg.OtherFunc",
				Kind:          "FUNCTION",
				SourceFile:    "other.go",
				StartLine:     5,
			}.WithProperties(map[string]string{"language": "go"}),
		},
		Relationships: []graph.Relationship{
			{
				ID:     "rel-001",
				FromID: "aabbccdd00000001",
				ToID:   "aabbccdd00000002",
				Kind:   "CALLS",
			},
		},
	}
}

// TestLoadGraphFromDir_FBOnly verifies that LoadGraphFromDir loads from
// graph.fb when only the binary file is present.
func TestLoadGraphFromDir_FBOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	doc := makeTestDoc()

	if err := fbwriter.WriteAtomic(filepath.Join(dir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}

	got, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	if got.Repo != doc.Repo {
		t.Errorf("repo: got %q want %q", got.Repo, doc.Repo)
	}
	if len(got.Entities) != len(doc.Entities) {
		t.Errorf("entities: got %d want %d", len(got.Entities), len(doc.Entities))
	}
	if len(got.Relationships) != len(doc.Relationships) {
		t.Errorf("relationships: got %d want %d", len(got.Relationships), len(doc.Relationships))
	}
}

// TestLoadGraphFromDir_JSONOnly verifies the JSON fallback path.
func TestLoadGraphFromDir_JSONOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	doc := makeTestDoc()

	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "graph.json"), b, 0o644); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}

	got, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	if got.Repo != doc.Repo {
		t.Errorf("repo: got %q want %q", got.Repo, doc.Repo)
	}
	if len(got.Entities) != len(doc.Entities) {
		t.Errorf("entities: got %d want %d", len(got.Entities), len(doc.Entities))
	}
}

// TestLoadGraphFromDir_BothPresent verifies that graph.fb is preferred when
// both files exist.
func TestLoadGraphFromDir_BothPresent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	doc := makeTestDoc()

	// Write graph.fb.
	if err := fbwriter.WriteAtomic(filepath.Join(dir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}

	// Write a graph.json with a different Repo tag so we can tell which
	// file LoadGraphFromDir actually read.
	docJSON := makeTestDoc()
	docJSON.Repo = "json-repo"
	b, err := json.Marshal(docJSON)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "graph.json"), b, 0o644); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}

	got, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	// Should have read from graph.fb (Repo = "test-repo"), NOT graph.json.
	if got.Repo != doc.Repo {
		t.Errorf("expected fb-sourced repo %q, got %q — LoadGraphFromDir did not prefer graph.fb",
			doc.Repo, got.Repo)
	}
}

// TestLoadGraphFromDir_NeitherPresent verifies that an error is returned
// when the directory is empty.
func TestLoadGraphFromDir_NeitherPresent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := graph.LoadGraphFromDir(dir)
	if err == nil {
		t.Fatal("expected error when neither graph.fb nor graph.json exists")
	}
}

// TestLoadGraphFromDir_EntityProperties verifies that Properties on
// entities are preserved through the FB round-trip.
func TestLoadGraphFromDir_EntityProperties(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	doc := makeTestDoc()

	doc.Entities[0].PropSet("framework", "gin")

	if err := fbwriter.WriteAtomic(filepath.Join(dir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}

	got, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}

	var handlerEnt *graph.Entity
	for i := range got.Entities {
		if got.Entities[i].Name == "MyHandler" {
			handlerEnt = &got.Entities[i]
			break
		}
	}
	if handlerEnt == nil {
		t.Fatal("MyHandler entity not found after FB round-trip")
	}
	if handlerEnt.PropGet("framework") != "gin" {
		t.Errorf("Properties[framework]: got %q want %q",
			handlerEnt.PropGet("framework"), "gin")
	}
}

// TestLoadGraphFromDir_EmbeddingRefRoundTrip verifies that an entity's
// EmbeddingRef (PH8 / #2100) is preserved through the FB round-trip.
//
// Regression test: fbEntityToGraphEntity (the shared FB->Document entity
// conversion used by both the single-file and segment-set load paths) never
// copied e.EmbeddingRef() into the resulting graph.Entity, even though
// fbwriter correctly persists it. Every FB-backed load (single-file or
// segment-set) silently came back with EmbeddingRef == "" regardless of what
// was written, which defeated internal/cli/cleanup.go's
// collectActiveEmbeddingHashes: an entity's embedding could never be
// reported "active", so the embedding-cache TTL sweep in `grafel cleanup`
// determined unreferenced-ness by age alone. Discovered while building the
// #5915 J2 slice-3 segment-set fixture for that walk. Purely additive fix:
// EmbeddingRef was always the zero value before, so this can only ever
// populate a previously-empty field.
func TestLoadGraphFromDir_EmbeddingRefRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	doc := makeTestDoc()

	doc.Entities[0].EmbeddingRef = "sha256:embedding-round-trip-hash"

	if err := fbwriter.WriteAtomic(filepath.Join(dir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}

	got, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}

	var handlerEnt *graph.Entity
	for i := range got.Entities {
		if got.Entities[i].Name == "MyHandler" {
			handlerEnt = &got.Entities[i]
			break
		}
	}
	if handlerEnt == nil {
		t.Fatal("MyHandler entity not found after FB round-trip")
	}
	if handlerEnt.EmbeddingRef != "sha256:embedding-round-trip-hash" {
		t.Errorf("EmbeddingRef: got %q want %q",
			handlerEnt.EmbeddingRef, "sha256:embedding-round-trip-hash")
	}
}
