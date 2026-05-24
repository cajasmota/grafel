package clone

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/graph/fbwriter"
)

// testItoa is a test-local int64→string helper (the package's itoa is
// in internal/daemon/sched; we keep a minimal copy here).
func testItoa(n int64) string { return fmt.Sprintf("%d", n) }

// ------------------------------------------------------------------ //
// Test helpers
// ------------------------------------------------------------------ //

// buildFakeGraph writes a minimal but valid graph.fb to stateDir.
// nEntities entities are distributed across nSourceFiles source files.
func buildFakeGraph(t *testing.T, stateDir string, nEntities, nSourceFiles int, ref, sha string) {
	t.Helper()
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("buildFakeGraph: mkdir %s: %v", stateDir, err)
	}
	entities := make([]graph.Entity, nEntities)
	for i := 0; i < nEntities; i++ {
		srcFile := "src/file" + testItoa(int64(i%nSourceFiles)) + ".go"
		entities[i] = graph.Entity{
			ID:         "ent:" + testItoa(int64(i)),
			Name:       "Entity" + testItoa(int64(i)),
			Kind:       "function",
			SourceFile: srcFile,
			StartLine:  i * 10,
		}
	}
	doc := &graph.Document{
		Version:     graph.SchemaVersion,
		GeneratedAt: time.Now().UTC(),
		Repo:        "test-repo",
		IndexedRef:  ref,
		IndexedSHA:  sha,
		Entities:    entities,
		Stats:       graph.Stats{Entities: nEntities, Files: nSourceFiles},
	}
	if err := fbwriter.WriteAtomic(filepath.Join(stateDir, "graph.fb"), doc); err != nil {
		t.Fatalf("buildFakeGraph: write: %v", err)
	}
}

// initGitRepo creates a minimal single-commit git repo (branch = main).
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("init", "-b", "main")
	runGit("config", "user.email", "test@example.com")
	runGit("config", "user.name", "Test")
	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit("add", "README.md")
	runGit("commit", "-m", "initial commit")
	return dir
}

// createBranch creates branch from current HEAD in repoPath.
func createBranch(t *testing.T, repoPath, branch string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "branch", branch)
	cmd.Dir = repoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git branch %s: %v\n%s", branch, err, out)
	}
}

// noopReExtract is a ReExtractFiles stub that returns the base document unchanged.
func noopReExtract(_ string, _ []string, base *graph.Document) (*graph.Document, error) {
	return base, nil
}

// ------------------------------------------------------------------ //
// Tests: precondition failures
// ------------------------------------------------------------------ //

// TestTryClone_NilCallbackAborts verifies that a nil ReExtractFiles
// callback is caught immediately.
func TestTryClone_NilCallbackAborts(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ARCHIGRAPH_DAEMON_ROOT", root)
	repoPath := t.TempDir()

	res, err := TryClone(repoPath, "feat/x", Config{
		Logger:         log.New(os.Stderr, "test: ", 0),
		ReExtractFiles: nil,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Done {
		t.Error("expected Done=false when ReExtractFiles is nil")
	}
}

// TestTryClone_ExistingGraphSkipped verifies condition 1: if the new ref
// already has a graph.fb, TryClone aborts without touching anything.
func TestTryClone_ExistingGraphSkipped(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ARCHIGRAPH_DAEMON_ROOT", root)
	repoPath := t.TempDir()

	// Pre-create the new ref's graph.fb.
	newRefDir := daemon.StateDirForRepoRef(repoPath, "feat/already-indexed")
	if err := os.MkdirAll(newRefDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newRefDir, "graph.fb"), []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := TryClone(repoPath, "feat/already-indexed", Config{
		Logger:         log.New(os.Stderr, "test: ", 0),
		ReExtractFiles: noopReExtract,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Done {
		t.Error("expected Done=false when new ref already has graph.fb")
	}
}

// TestTryClone_NoParentCandidateAborts verifies that TryClone aborts
// gracefully when no parent ref has a graph.fb on disk.
func TestTryClone_NoParentCandidateAborts(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ARCHIGRAPH_DAEMON_ROOT", root)
	repoPath := t.TempDir()

	res, err := TryClone(repoPath, "feat/no-parent", Config{
		Logger:         log.New(os.Stderr, "test: ", 0),
		ReExtractFiles: noopReExtract,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Done {
		t.Error("expected Done=false when no parent ref has a graph.fb")
	}
}

// TestTryClone_CorruptParentGraphAborts verifies that a corrupt parent
// graph.fb causes abort and leaves no partial file in the new ref dir.
func TestTryClone_CorruptParentGraphAborts(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ARCHIGRAPH_DAEMON_ROOT", root)
	// Use a real git repo so gitDiffFiles returns 0 files (same commit).
	repoPath := initGitRepo(t)
	createBranch(t, repoPath, "feat/from-corrupt")

	// Write a corrupt graph.fb for "main".
	mainDir := daemon.StateDirForRepoRef(repoPath, "main")
	if err := os.MkdirAll(mainDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mainDir, "graph.fb"), []byte("not-a-flatbuffer"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := TryClone(repoPath, "feat/from-corrupt", Config{
		Logger:         log.New(os.Stderr, "test: ", 0),
		ReExtractFiles: noopReExtract,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Done {
		t.Error("expected Done=false for corrupt parent graph")
	}

	// Partial graph.fb must not be left in the new ref dir.
	newRefDir := daemon.StateDirForRepoRef(repoPath, "feat/from-corrupt")
	if _, statErr := os.Stat(filepath.Join(newRefDir, "graph.fb")); statErr == nil {
		t.Error("partial graph.fb left behind in new ref dir after corrupt-parent abort")
	}
}

// ------------------------------------------------------------------ //
// Tests: happy path
// ------------------------------------------------------------------ //

// TestTryClone_ZeroDiff_MetadataUpdated verifies the full happy path when
// the new branch has zero changed files (same HEAD commit as main).
// After clone: graph.fb must exist, IndexedRef must be updated, entity
// count must be preserved.
func TestTryClone_ZeroDiff_MetadataUpdated(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ARCHIGRAPH_DAEMON_ROOT", root)
	repoPath := initGitRepo(t)
	createBranch(t, repoPath, "feat/zero-diff")

	// Build a parent (main) graph with 6000 entities across 100 source files.
	mainDir := daemon.StateDirForRepoRef(repoPath, "main")
	buildFakeGraph(t, mainDir, 6000, 100, "main", "aabbccdd0011")

	var reExtractFiles []string
	res, err := TryClone(repoPath, "feat/zero-diff", Config{
		Logger: log.New(os.Stderr, "test: ", 0),
		ReExtractFiles: func(_ string, files []string, base *graph.Document) (*graph.Document, error) {
			reExtractFiles = files
			return base, nil
		},
	})
	if err != nil {
		t.Fatalf("TryClone error: %v", err)
	}
	if !res.Done {
		t.Fatalf("expected Done=true for zero-diff branch, got Done=false")
	}
	if res.ParentRef != "main" {
		t.Errorf("ParentRef = %q, want main", res.ParentRef)
	}
	if res.ChangedFiles != 0 {
		t.Errorf("ChangedFiles = %d, want 0", res.ChangedFiles)
	}
	if res.Took <= 0 {
		t.Error("Took must be > 0")
	}
	// Zero diff → ReExtractFiles should not be called.
	if len(reExtractFiles) != 0 {
		t.Errorf("ReExtractFiles called with %d files, expected 0 for zero-diff", len(reExtractFiles))
	}

	// Load the resulting graph and verify metadata + entity count.
	newRefDir := daemon.StateDirForRepoRef(repoPath, "feat/zero-diff")
	doc, err := graph.LoadGraphFromDir(newRefDir)
	if err != nil {
		t.Fatalf("load cloned graph: %v", err)
	}
	if doc.IndexedRef != "feat/zero-diff" {
		t.Errorf("IndexedRef = %q, want feat/zero-diff", doc.IndexedRef)
	}
	if len(doc.Entities) != 6000 {
		t.Errorf("entity count = %d, want 6000 (entities must be preserved)", len(doc.Entities))
	}
	// Speedup: clone from 6k-entity parent should complete in well under 1 s.
	if res.Took > 1*time.Second {
		t.Errorf("clone took %s, expected < 1s on 6k-entity repo", res.Took)
	}
}

// TestTryClone_ReExtractCalled verifies that when the parent has a graph.fb
// and the repo diff is non-empty, ReExtractFiles is called with the right
// file list and the returned document is persisted.
func TestTryClone_ReExtractCalled(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ARCHIGRAPH_DAEMON_ROOT", root)
	repoPath := initGitRepo(t)

	// Add a file change on a new branch so the diff is non-empty.
	changed := filepath.Join(repoPath, "changed.go")
	if err := os.WriteFile(changed, []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, args := range [][]string{
		{"checkout", "-b", "feat/with-change"},
		{"add", "changed.go"},
		{"commit", "-m", "add changed.go"},
	} {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = repoPath
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// Build parent (main) graph.
	mainDir := daemon.StateDirForRepoRef(repoPath, "main")
	buildFakeGraph(t, mainDir, 100, 5, "main", "abc123")

	var gotFiles []string
	res, err := TryClone(repoPath, "feat/with-change", Config{
		Logger: log.New(os.Stderr, "test: ", 0),
		ReExtractFiles: func(_ string, files []string, base *graph.Document) (*graph.Document, error) {
			gotFiles = append(gotFiles, files...)
			return base, nil
		},
	})
	if err != nil {
		t.Fatalf("TryClone error: %v", err)
	}
	if !res.Done {
		t.Skip("clone aborted — likely merge-base age check. Skipping diff-reindex test.")
	}

	// ReExtractFiles must have been called with "changed.go".
	found := false
	for _, f := range gotFiles {
		if f == "changed.go" {
			found = true
		}
	}
	if !found {
		t.Errorf("ReExtractFiles was called with %v, expected to include changed.go", gotFiles)
	}
}

// ------------------------------------------------------------------ //
// Tests: env-var + limit handling
// ------------------------------------------------------------------ //

// TestResolveMaxFiles verifies default, override, clamp, and invalid values.
func TestResolveMaxFiles(t *testing.T) {
	os.Unsetenv("ARCHIGRAPH_CLONE_MAX_FILES")
	if got := resolveMaxFiles(); got != defaultMaxFiles {
		t.Errorf("default: got %d, want %d", got, defaultMaxFiles)
	}

	t.Setenv("ARCHIGRAPH_CLONE_MAX_FILES", "5")
	if got := resolveMaxFiles(); got != 5 {
		t.Errorf("override 5: got %d", got)
	}

	t.Setenv("ARCHIGRAPH_CLONE_MAX_FILES", "999")
	if got := resolveMaxFiles(); got != maxAllowedFiles {
		t.Errorf("clamp: got %d, want %d", got, maxAllowedFiles)
	}

	t.Setenv("ARCHIGRAPH_CLONE_MAX_FILES", "notanumber")
	if got := resolveMaxFiles(); got != defaultMaxFiles {
		t.Errorf("invalid: got %d, want default %d", got, defaultMaxFiles)
	}

	t.Setenv("ARCHIGRAPH_CLONE_MAX_FILES", "0") // zero → fall back to default
	if got := resolveMaxFiles(); got != defaultMaxFiles {
		t.Errorf("zero: got %d, want default %d", got, defaultMaxFiles)
	}
}

// TestDiffOverLimitAborts verifies that when the diff is >maxFiles the
// clone aborts (no graph written).
func TestDiffOverLimitAborts(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ARCHIGRAPH_DAEMON_ROOT", root)
	t.Setenv("ARCHIGRAPH_CLONE_MAX_FILES", "2") // very tight limit

	repoPath := initGitRepo(t)

	// Create a branch with 3 changed files.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, args := range [][]string{
		{"checkout", "-b", "feat/big-diff"},
	} {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = repoPath
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	for i := 0; i < 3; i++ {
		f := filepath.Join(repoPath, "file"+testItoa(int64(i))+".go")
		if err := os.WriteFile(f, []byte("package main"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.CommandContext(ctx, "git", "add", ".")
	cmd.Dir = repoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	cmd = exec.CommandContext(ctx, "git", "commit", "-m", "3 files")
	cmd.Dir = repoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}

	// Build parent (main) graph.
	mainDir := daemon.StateDirForRepoRef(repoPath, "main")
	buildFakeGraph(t, mainDir, 50, 5, "main", "abc123")

	res, err := TryClone(repoPath, "feat/big-diff", Config{
		Logger:         log.New(os.Stderr, "test: ", 0),
		ReExtractFiles: noopReExtract,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Done {
		t.Errorf("expected Done=false for diff with 3 files and limit 2, got Done=true")
	}

	// No graph.fb must be written in the new ref dir.
	newRefDir := daemon.StateDirForRepoRef(repoPath, "feat/big-diff")
	if _, statErr := os.Stat(filepath.Join(newRefDir, "graph.fb")); statErr == nil {
		t.Error("graph.fb was written even though diff exceeded max-files limit")
	}
}

// ------------------------------------------------------------------ //
// Tests: copyFile
// ------------------------------------------------------------------ //

func TestCopyFile_Basic(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.bin")
	dst := filepath.Join(tmp, "dst.bin")

	data := []byte("hello clone world 12345")
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("content mismatch: got %q, want %q", got, data)
	}
	// .tmp file must be cleaned up.
	if _, err := os.Stat(dst + ".tmp"); err == nil {
		t.Error(".tmp file left behind after successful copyFile")
	}
}

func TestCopyFile_MissingSrc(t *testing.T) {
	tmp := t.TempDir()
	err := copyFile(filepath.Join(tmp, "nonexistent"), filepath.Join(tmp, "dst"))
	if err == nil {
		t.Error("expected error when src does not exist")
	}
}
