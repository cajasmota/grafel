package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	fb "github.com/cajasmota/grafel/internal/graph/fbgraph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/statusfile"
)

// writeOldFormatGraphFBAt builds a minimal valid graph.fb via fbwriter.Marshal,
// then patches the on-disk Graph.version scalar down to oldVersion (mirrors
// internal/graph/reindex_required_test.go's writeOldFormatGraphFB) so this
// test can fabricate a graph.fb written by an older grafel build without an
// actual old binary. dir is the repo's state dir (daemon.StateDirForRepo).
func writeOldFormatGraphFBAt(t *testing.T, dir string, oldVersion int) {
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

// TestWriteRepoStatusFile_ReindexRequired is the RED test for the
// "reindex-required after graph-format change" epic, PR1 (detection + state
// only — no auto-reindex, no prompt). writeRepoStatusFile is the SOLE
// statusfile writer (per package doc); it must independently recompute
// ReindexRequired/ReindexReason from the actual on-disk graph.fb bytes on
// every call, so an old-format graph is reflected in the persisted status
// even though nothing here triggers a reindex.
func TestWriteRepoStatusFile_ReindexRequired(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())

	repo := t.TempDir()
	stateDir := StateDirForRepo(repo)
	writeOldFormatGraphFBAt(t, stateDir, 2)

	writeRepoStatusFile(repo, nil)

	got, err := statusfile.Read(repo)
	if err != nil {
		t.Fatalf("statusfile.Read: %v", err)
	}
	if !got.ReindexRequired {
		t.Fatal("expected ReindexRequired=true for a repo whose graph.fb is an old format version")
	}
	if got.ReindexReason == "" {
		t.Fatal("expected a non-empty ReindexReason")
	}
}

// TestWriteRepoStatusFile_ReindexRequired_CurrentVersionRegression is the
// regression guard: a repo whose graph.fb is at the current format version
// (or has no graph.fb at all) must never be flagged ReindexRequired.
func TestWriteRepoStatusFile_ReindexRequired_CurrentVersionRegression(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())

	repo := t.TempDir()
	stateDir := StateDirForRepo(repo)
	doc := &graph.Document{
		Version:     1,
		GeneratedAt: time.Now().UTC(),
		Repo:        "fixture-current-version",
		Entities: []graph.Entity{
			{ID: "ent0000000000000a", Name: "foo", Kind: "function", SourceFile: "a.go"},
		},
	}
	doc.Stats.Entities = 1
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir stateDir: %v", err)
	}
	if err := fbwriter.WriteAtomic(filepath.Join(stateDir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}

	writeRepoStatusFile(repo, nil)

	got, err := statusfile.Read(repo)
	if err != nil {
		t.Fatalf("statusfile.Read: %v", err)
	}
	if got.ReindexRequired {
		t.Errorf("expected ReindexRequired=false for a current-version graph.fb, got reason %q", got.ReindexReason)
	}

	// A repo with no graph.fb at all (never indexed) must also not be flagged.
	repo2 := t.TempDir()
	writeRepoStatusFile(repo2, nil)
	got2, err := statusfile.Read(repo2)
	if err != nil {
		t.Fatalf("statusfile.Read repo2: %v", err)
	}
	if got2.ReindexRequired {
		t.Errorf("expected ReindexRequired=false for a never-indexed repo, got reason %q", got2.ReindexReason)
	}
}
