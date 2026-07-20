// Tests for the reindex-required detection primitive added to
// internal/graph/load.go (PR1 of the "reindex-required after graph-format
// change" epic). This slice is detection + state ONLY — no auto-reindex.
package graph_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	fb "github.com/cajasmota/grafel/internal/graph/fbgraph"
	"github.com/cajasmota/grafel/internal/graph/fbversion"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// writeOldFormatGraphFB builds a minimal valid graph.fb via fbwriter.Marshal,
// then patches the on-disk Graph.version scalar down to oldVersion (mirrors
// the technique in internal/graph/fbwriter/writer_test.go's
// TestLoaderRejectsOldFormatVersion) so tests can fabricate a graph.fb
// written by an older grafel build without needing an actual old binary.
func writeOldFormatGraphFB(t *testing.T, dir string, oldVersion int) {
	t.Helper()
	doc := &graph.Document{
		Version:     1,
		GeneratedAt: time.Now().UTC(),
		Repo:        "fixture-old-version",
		Entities: []graph.Entity{
			{ID: "ent0000000000000a", Name: "foo", Kind: "function", SourceFile: "a.go"},
		},
	}
	doc.Stats.Entities = 1
	buf, err := fbwriter.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	root := fb.GetRootAsGraph(buf, 0)
	if !root.MutateVersion(int32(oldVersion)) {
		t.Fatalf("MutateVersion(%d) returned false — slot missing?", oldVersion)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "graph.fb"), buf, 0o644); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
}

// TestLoadGraphFromDir_OldFormatVersion_ReturnsTypedError is the RED test
// proving the loader rejects an old-format graph.fb AND that the rejection
// is detectable via errors.As(&graph.FormatVersionError{}) — not just a
// human-readable string — so a caller (internal/mcp's reload loop) can
// record durable ReindexRequired state instead of only stashing an opaque
// error and silently continuing.
func TestLoadGraphFromDir_OldFormatVersion_ReturnsTypedError(t *testing.T) {
	dir := t.TempDir()
	writeOldFormatGraphFB(t, dir, 2)

	_, err := graph.LoadGraphFromDir(dir)
	if err == nil {
		t.Fatal("expected an error for an old-format graph.fb, got nil")
	}

	var fvErr *graph.FormatVersionError
	if !errors.As(err, &fvErr) {
		t.Fatalf("expected errors.As to find a *graph.FormatVersionError in %v", err)
	}
	if fvErr.Found != 2 {
		t.Errorf("FormatVersionError.Found = %d, want 2", fvErr.Found)
	}
	if fvErr.Required != fbversion.Version {
		t.Errorf("FormatVersionError.Required = %d, want %d", fvErr.Required, fbversion.Version)
	}
}

// TestReindexRequiredReason_OldFormatVersion is the RED test for the cheap
// header-only detection helper: given a directory whose graph.fb is stamped
// with a version below fbversion.Version, ReindexRequiredReason must report
// required=true with a reason naming BOTH the found and required versions.
func TestReindexRequiredReason_OldFormatVersion(t *testing.T) {
	dir := t.TempDir()
	writeOldFormatGraphFB(t, dir, 2)

	required, reason := graph.ReindexRequiredReason(dir)
	if !required {
		t.Fatal("expected required=true for an old-format graph.fb")
	}
	if reason == "" {
		t.Fatal("expected a non-empty reason")
	}
	wantSubstrs := []string{fmt.Sprintf("v%d", 2), fmt.Sprintf("v%d", fbversion.Version), "reindex"}
	for _, want := range wantSubstrs {
		if !strings.Contains(strings.ToLower(reason), strings.ToLower(want)) {
			t.Errorf("reason %q missing expected substring %q", reason, want)
		}
	}
}

// TestReindexRequiredReason_CurrentVersion_NotRequired is the regression
// guard: a graph.fb written at the CURRENT fbversion.Version must never be
// flagged, and a directory with no graph.fb at all must never be flagged
// either (both are "nothing to report", not an error).
func TestReindexRequiredReason_CurrentVersion_NotRequired(t *testing.T) {
	dir := t.TempDir()
	doc := &graph.Document{
		Version:     1,
		GeneratedAt: time.Now().UTC(),
		Repo:        "fixture-current-version",
		Entities: []graph.Entity{
			{ID: "ent0000000000000a", Name: "foo", Kind: "function", SourceFile: "a.go"},
		},
	}
	doc.Stats.Entities = 1
	if err := fbwriter.WriteAtomic(filepath.Join(dir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}

	if required, reason := graph.ReindexRequiredReason(dir); required {
		t.Errorf("expected required=false for a current-version graph.fb, got reason %q", reason)
	}

	emptyDir := t.TempDir()
	if required, reason := graph.ReindexRequiredReason(emptyDir); required {
		t.Errorf("expected required=false for a directory with no graph.fb, got reason %q", reason)
	}
}
