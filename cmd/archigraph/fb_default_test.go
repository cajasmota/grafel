// Tests for ADR-0016 flip-day (#808): graph.fb always emitted; --skip-json opt-in.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/graph/fbreader"
)

// TestIndex_FBOnlyByDefault verifies that Index() writes graph.fb by default
// without graph.json (ADR-0016 flip-day, issue #808).
func TestIndex_FBOnlyByDefault(t *testing.T) {
	t.Setenv("ARCHIGRAPH_DAEMON_ROOT", t.TempDir())
	tmp := t.TempDir()
	outPath := filepath.Join(tmp, "graph.json")

	if err := Index("testdata/crossfile_go", outPath, "test-repo", nil, false, false); err != nil {
		t.Fatalf("Index: %v", err)
	}

	// graph.json MUST NOT be written by default (FB-only, ADR-0016 flip-day).
	if _, err := os.Stat(outPath); err == nil {
		t.Errorf("graph.json was written by default (should be FB-only)")
	}

	// graph.fb MUST be written.
	fbPath := filepath.Join(tmp, "graph.fb")
	info, err := os.Stat(fbPath)
	if err != nil {
		t.Fatalf("graph.fb not written (ADR-0016 flip-day regression): %v", err)
	}
	if info.Size() == 0 {
		t.Fatalf("graph.fb is empty")
	}

	// Verify the FB file is a valid FlatBuffers graph by opening it.
	r, err := fbreader.Open(fbPath)
	if err != nil {
		t.Fatalf("fbreader.Open graph.fb: %v", err)
	}
	defer r.Close()
	if r.EntityCount() == 0 {
		t.Errorf("graph.fb has 0 entities — expected > 0 from crossfile_go fixture")
	}
}

// TestIndex_ExportJSON verifies that Index() with WithExportJSON(true)
// writes both graph.fb and graph.json.
func TestIndex_ExportJSON(t *testing.T) {
	t.Setenv("ARCHIGRAPH_DAEMON_ROOT", t.TempDir())
	tmp := t.TempDir()
	outPath := filepath.Join(tmp, "graph.json")

	if err := Index("testdata/crossfile_go", outPath, "test-repo", nil, false, false,
		WithExportJSON(true)); err != nil {
		t.Fatalf("Index with --export-json: %v", err)
	}

	// graph.json MUST be present when --export-json is used.
	if _, err := os.Stat(outPath); err != nil {
		t.Errorf("graph.json not written with --export-json flag: %v", err)
	}

	// graph.fb MUST also be present.
	fbPath := filepath.Join(tmp, "graph.fb")
	if _, err := os.Stat(fbPath); err != nil {
		t.Fatalf("graph.fb not written with --export-json: %v", err)
	}
}

// TestIndex_ExportFBDeprecatedNoOp verifies that passing WithExportFB(true)
// still results in a valid graph.fb being written (the no-op doesn't break
// the existing write path).
func TestIndex_ExportFBDeprecatedNoOp(t *testing.T) {
	t.Setenv("ARCHIGRAPH_DAEMON_ROOT", t.TempDir())
	tmp := t.TempDir()
	outPath := filepath.Join(tmp, "graph.json")

	if err := Index("testdata/crossfile_go", outPath, "test-repo", nil, false, false,
		WithExportFB(true)); err != nil {
		t.Fatalf("Index with deprecated --export-fb: %v", err)
	}

	// graph.fb must exist (always-on since #808).
	fbPath := filepath.Join(tmp, "graph.fb")
	if _, err := os.Stat(fbPath); err != nil {
		t.Fatalf("graph.fb not written even with deprecated --export-fb: %v", err)
	}
}

// TestFBRoundTrip_LoadGraphFromDir verifies that a graph written by Index()
// can be loaded back via graph.LoadGraphFromDir and has matching entity count.
func TestFBRoundTrip_LoadGraphFromDir(t *testing.T) {
	t.Setenv("ARCHIGRAPH_DAEMON_ROOT", t.TempDir())
	tmp := t.TempDir()
	outPath := filepath.Join(tmp, "graph.json")

	if err := Index("testdata/crossfile_go", outPath, "test-repo", nil, false, false,
		WithExportJSON(true)); err != nil {
		t.Fatalf("Index: %v", err)
	}

	// LoadGraphFromDir should prefer graph.fb.
	doc, err := graph.LoadGraphFromDir(tmp)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	if doc.Repo != "test-repo" {
		t.Errorf("repo: got %q want %q", doc.Repo, "test-repo")
	}
	if len(doc.Entities) == 0 {
		t.Errorf("no entities loaded from graph.fb via LoadGraphFromDir")
	}
	if len(doc.Relationships) == 0 {
		t.Errorf("no relationships loaded from graph.fb via LoadGraphFromDir")
	}

	// Entity count should match the JSON-side count.
	if data, err2 := os.ReadFile(outPath); err2 == nil {
		var jsonDoc graph.Document
		if merr := json.Unmarshal(data, &jsonDoc); merr == nil {
			if len(doc.Entities) != len(jsonDoc.Entities) {
				t.Errorf("FB entity count %d != JSON entity count %d",
					len(doc.Entities), len(jsonDoc.Entities))
			}
		}
	}
}
