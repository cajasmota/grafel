package daemon_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/indexer/diff"
)

func writeFileIC(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func runGitIC(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func initGitRepoIC(t *testing.T, dir string) {
	t.Helper()
	runGitIC(t, dir, "init", "-q")
	runGitIC(t, dir, "config", "user.email", "test@example.com")
	runGitIC(t, dir, "config", "user.name", "Test")
}

// TestIndexedCommitForRepo_AtHeadTrueThenFalse is the RED test for the
// #5727/#5729-W1 "expose indexed commit" deliverable.
//
// grafel_index_status / `grafel status` report indexed_ref but never the
// exact indexed commit, so there is no way to tell whether the graph on disk
// still matches HEAD (at_head) without a manual diff. This exercises the new
// daemon.IndexedCommitForRepo helper: it must read the already-recorded
// indexed commit (from the diff-manifest sidecar written by SaveManifest,
// internal/indexer/diff/diff.go) and compare it against the repo's current
// HEAD, WITHOUT requiring a fresh reindex.
func TestIndexedCommitForRepo_AtHeadTrueThenFalse(t *testing.T) {
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())

	repo := t.TempDir()
	initGitRepoIC(t, repo)
	if err := writeAndCommit(repo, "main.go", "package main\nfunc main(){}\n"); err != nil {
		t.Fatalf("seed commit: %v", err)
	}
	commitA := runGitIC(t, repo, "rev-parse", "HEAD")
	commitAShort := runGitIC(t, repo, "rev-parse", "--short", "HEAD")

	// Seed the diff manifest as if `grafel index` had just run at commit A.
	stateDir := daemon.StateDirForRepo(repo)
	m := diff.LoadManifest(stateDir)
	if err := diff.SaveManifest(stateDir, repo, m); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	info := daemon.IndexedCommitForRepo(repo)
	if info.CommitShort != commitAShort {
		t.Errorf("CommitShort = %q, want %q", info.CommitShort, commitAShort)
	}
	if info.Commit != commitA {
		t.Errorf("Commit = %q, want %q", info.Commit, commitA)
	}
	if !info.AtHead {
		t.Error("AtHead should be true immediately after indexing at current HEAD")
	}

	// Advance HEAD without re-running SaveManifest — the graph is now stale.
	if err := writeAndCommit(repo, "main.go", "package main\nfunc main(){println(1)}\n"); err != nil {
		t.Fatalf("advance commit: %v", err)
	}

	info2 := daemon.IndexedCommitForRepo(repo)
	if info2.CommitShort != commitAShort {
		t.Errorf("CommitShort after HEAD advance should still report the indexed commit %q, got %q",
			commitAShort, info2.CommitShort)
	}
	if info2.AtHead {
		t.Error("AtHead should be false once HEAD has advanced past the indexed commit")
	}
}

// TestIndexedCommitForRepo_NeverIndexed asserts a repo with no manifest and no
// graph.fb reports empty commit info rather than fabricating a value.
func TestIndexedCommitForRepo_NeverIndexed(t *testing.T) {
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())
	repo := t.TempDir()
	initGitRepoIC(t, repo)
	if err := writeAndCommit(repo, "a.go", "package a\n"); err != nil {
		t.Fatalf("seed commit: %v", err)
	}

	info := daemon.IndexedCommitForRepo(repo)
	if info.Commit != "" || info.CommitShort != "" {
		t.Errorf("expected empty commit info for never-indexed repo, got %+v", info)
	}
	if info.AtHead {
		t.Error("AtHead must be false when nothing has been indexed")
	}
}

// TestIndexedCommitForRepo_GraphFBFallback_AtHead is the regression test for
// review #5734 non-blocking #4: for a manifest-less graph, IndexedCommitForRepo
// falls back to the graph.fb header's IndexedSHA, which gitmeta records with
// `--short=12` (12 chars). The old code compared that 12-char short against
// `git rev-parse --short HEAD` (git's default ~7 chars) → never equal → AtHead
// always false. Comparing against the FULL HEAD SHA via prefix fixes it.
func TestIndexedCommitForRepo_GraphFBFallback_AtHead(t *testing.T) {
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())

	repo := t.TempDir()
	initGitRepoIC(t, repo)
	if err := writeAndCommit(repo, "main.go", "package main\nfunc main(){}\n"); err != nil {
		t.Fatalf("seed commit: %v", err)
	}
	// gitmeta captures IndexedSHA as --short=12; mirror that exactly.
	short12 := runGitIC(t, repo, "rev-parse", "--short=12", "HEAD")

	// Write a graph.fb (NO diff manifest) into the repo's state dir carrying
	// that 12-char short SHA in its header, simulating a graph produced by a
	// path that never wrote the diff manifest.
	stateDir := daemon.StateDirForRepo(repo)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir stateDir: %v", err)
	}
	sw, err := fbwriter.NewStreamingWriter(filepath.Join(stateDir, "graph.fb"))
	if err != nil {
		t.Fatalf("NewStreamingWriter: %v", err)
	}
	e := graph.Entity{ID: "ent0000000000000a", Name: "foo", Kind: "function", SourceFile: "main.go"}
	if err := sw.WriteEntity(&e); err != nil {
		t.Fatalf("WriteEntity: %v", err)
	}
	if err := sw.Close(fbwriter.GraphMetadata{
		Repo:        "fallback-repo",
		GeneratedAt: time.Now().UTC(),
		IndexedRef:  "main",
		IndexedSHA:  short12,
	}); err != nil {
		t.Fatalf("Close: %v", err)
	}

	info := daemon.IndexedCommitForRepo(repo)
	if info.CommitShort != short12 {
		t.Errorf("CommitShort = %q, want graph.fb short %q", info.CommitShort, short12)
	}
	if !info.AtHead {
		t.Errorf("AtHead should be true: graph.fb 12-char short %q is a prefix of current HEAD", short12)
	}
}

func writeAndCommit(dir, relPath, content string) error {
	full := dir + "/" + relPath
	if err := writeFileIC(full, content); err != nil {
		return err
	}
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return &execErr{out: out, err: err}
	}
	cmd = exec.Command("git", "commit", "-q", "-m", "c")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return &execErr{out: out, err: err}
	}
	return nil
}

type execErr struct {
	out []byte
	err error
}

func (e *execErr) Error() string { return e.err.Error() + ": " + string(e.out) }
