package daemon

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/indexer/diff"
	"github.com/cajasmota/grafel/internal/statusfile"
)

func runGit5822(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(bytes.TrimSpace(out))
}

// TestStatusPlane_RejectPathDoesNotAdvanceSurfacedCommit is the #5822 ② test on
// the SURFACED plane: what `grafel status`, the dashboard and the statusline
// actually print.
//
// The per-repo SHAs printed by `grafel status` itself come from the graph.fb
// header (internal/cli/status_stats.go) and were always right. The two readers
// pinned here are the ones that prefer the DIFF MANIFEST over that header —
// indexedCommitShortNoGit (the status-file write path, i.e. the statusline and
// dashboard) and IndexedCommitForRepo (the RPC path, i.e. grafel_index_status)
// — so a manifest that advanced without an index makes both of them report a
// commit the graph has never contained, and IndexedCommitForRepo additionally
// reports AtHead=true for a graph that is not at HEAD.
//
// The sequence is reject-then-NO-reindex, which is the whole complaint: a
// rejected incremental pass returns a full-reindex REQUEST, and that request
// can be cancelled, watchdog-killed, or simply queued behind other work. Until
// it runs, the graph is at c1. The label must say c1.
//
// Fixture: a REAL git repo whose HEAD genuinely moves (asserted). Against a
// non-git tmpdir every commit field is "" and both assertions below would hold
// against the defective code — the failure mode this repo keeps re-committing.
// The graph.fb here is written WITHOUT an indexed-SHA header on purpose: both
// readers fall back to the header only when the manifest field is EMPTY, and
// it is non-empty in both the fixed and the defective world, so the assertion
// is unambiguously about the manifest.
func TestStatusPlane_RejectPathDoesNotAdvanceSurfacedCommit(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())
	t.Setenv("GRAFEL_INCREMENTAL_MAX_FILES", "2")

	repo := t.TempDir()
	runGit5822(t, repo, "init", "-q")
	runGit5822(t, repo, "config", "user.email", "test@test")
	runGit5822(t, repo, "config", "user.name", "Test")

	files := []string{"a.go", "b.go", "c.go", "d.go", "e.go"}
	writeAll := func(tag int) {
		for i, rel := range files {
			body := fmt.Sprintf("package p\n\nfunc F%d() { _ = %d }\n", i, tag)
			if err := os.WriteFile(filepath.Join(repo, rel), []byte(body), 0o644); err != nil {
				t.Fatalf("write %s: %v", rel, err)
			}
		}
	}
	commit := func(msg string) (string, string) {
		runGit5822(t, repo, "add", "-A")
		runGit5822(t, repo, "commit", "-q", "-m", msg)
		return runGit5822(t, repo, "rev-parse", "--short", "HEAD"),
			runGit5822(t, repo, "rev-parse", "HEAD")
	}

	writeAll(0)
	c1Short, c1Full := commit("c1")

	stateDir := StateDirForRepo(repo)
	if stateDir == "" {
		t.Fatal("fixture: StateDirForRepo returned empty")
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir stateDir: %v", err)
	}
	doc := &graph.Document{
		Version:     graph.SchemaVersion,
		GeneratedAt: time.Now().UTC(),
		Repo:        "test-repo",
		Entities: []graph.Entity{{
			ID:   graph.EntityID("test-repo", "SCOPE.Operation", "F0", "a.go"),
			Name: "F0", Kind: "SCOPE.Operation", SourceFile: "a.go", Language: "go",
		}},
		Stats: graph.Stats{Entities: 1},
	}
	if err := fbwriter.WriteAtomic(filepath.Join(stateDir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}

	m := diff.LoadManifest(stateDir)
	diff.UpdateManifest(repo, files, m)
	if err := diff.SaveManifest(stateDir, repo, m); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}
	if seeded := diff.LoadManifest(stateDir); seeded.GitCommit != c1Short {
		t.Fatalf("fixture: seeded GitCommit = %q, want %q — the fixture is not being read as a git repo, so this test would assert nothing", seeded.GitCommit, c1Short)
	}

	// Advance HEAD. Working tree ends clean, so only the #5710 commit-range
	// diff can surface these files — the post-pull shape.
	writeAll(1)
	c2Short, c2Full := commit("c2")
	if c1Short == c2Short || c1Full == c2Full {
		t.Fatalf("fixture: HEAD did not move (c1=%s c2=%s)", c1Short, c2Short)
	}

	// The reject. No full reindex follows it — that is the point.
	res := extractors.TryIncremental(context.Background(), repo, stateDir, nil, nil)
	if res.Done {
		t.Fatalf("expected the too-many-changed reject, got Done=true — the path under test never ran")
	}

	// Reader 1: the status-file write path (statusline / dashboard).
	writeRepoStatusFile(repo, nil)
	got, err := statusfile.Read(repo)
	if err != nil {
		t.Fatalf("statusfile.Read: %v", err)
	}
	if got.IndexedCommit != c1Short {
		t.Errorf("status file IndexedCommit = %q, want %q — the statusline is showing a commit that was never indexed (%q is live HEAD; the reject built no graph)", got.IndexedCommit, c1Short, c2Short)
	}

	// Reader 2: the RPC path (grafel_index_status).
	info := IndexedCommitForRepo(repo)
	if info.CommitShort != c1Short {
		t.Errorf("IndexedCommitForRepo CommitShort = %q, want %q", info.CommitShort, c1Short)
	}
	if info.Commit != c1Full {
		t.Errorf("IndexedCommitForRepo Commit = %q, want %q", info.Commit, c1Full)
	}
	if info.AtHead {
		t.Errorf("IndexedCommitForRepo AtHead = true, want false — the graph is at %s and HEAD is %s; reporting AtHead here is the self-conceal that stops anything ever asking for the reindex again", c1Short, c2Short)
	}
}
