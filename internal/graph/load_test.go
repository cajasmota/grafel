// Tests for the FB-first graph loader introduced by ADR-0016 flip-day (#808).
package graph_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/graph/fbwriter"
)

// makeTestDoc creates a small Document for use in loader tests.
func makeTestDoc() *graph.Document {
	return &graph.Document{
		Version:     graph.SchemaVersion,
		GeneratedAt: time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC),
		Repo:        "test-repo",
		Entities: []graph.Entity{
			{
				ID:            "aabbccdd00000001",
				Name:          "MyHandler",
				QualifiedName: "pkg.MyHandler",
				Kind:          "FUNCTION",
				SourceFile:    "handler.go",
				StartLine:     10,
				Properties:    map[string]string{"language": "go"},
			},
			{
				ID:            "aabbccdd00000002",
				Name:          "OtherFunc",
				QualifiedName: "pkg.OtherFunc",
				Kind:          "FUNCTION",
				SourceFile:    "other.go",
				StartLine:     5,
				Properties:    map[string]string{"language": "go"},
			},
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
	// Add a property that should survive FB serialization.
	doc.Entities[0].Properties["framework"] = "gin"

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
	if handlerEnt.Properties["framework"] != "gin" {
		t.Errorf("Properties[framework]: got %q want %q",
			handlerEnt.Properties["framework"], "gin")
	}
}
