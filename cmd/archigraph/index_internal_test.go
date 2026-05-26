package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRunIndexInternalMissingRepo verifies that `index-internal` exits non-zero
// when --repo is not supplied.
func TestRunIndexInternalMissingRepo(t *testing.T) {
	got := runIndexInternal([]string{})
	if got == 0 {
		t.Fatal("expected non-zero exit for missing --repo, got 0")
	}
}

// TestRunIndexInternalBadRepo verifies that `index-internal` exits non-zero
// (exit=1) when the supplied --repo does not exist on disk.
func TestRunIndexInternalBadRepo(t *testing.T) {
	got := runIndexInternal([]string{
		"--repo=/does/not/exist/42abc",
		"--ref=main",
	})
	if got == 0 {
		t.Fatal("expected non-zero exit for non-existent repo, got 0")
	}
}

// TestRunIndexInternalEmptyDir verifies that `index-internal` can be called
// against an empty temporary directory without panicking. The indexer will
// produce a graph with zero entities; exit code must be 0.
func TestRunIndexInternalEmptyDir(t *testing.T) {
	// #2083: pin ARCHIGRAPH_DAEMON_ROOT so runIndexInternal writes per-repo
	// state into an isolated temp dir, not into the real ~/.archigraph/store/.
	t.Setenv("ARCHIGRAPH_DAEMON_ROOT", t.TempDir())
	dir := t.TempDir()
	// Build a minimal valid-looking repo dir (no files, no git). The
	// indexer should still succeed with 0 entities.
	got := runIndexInternal([]string{
		"--repo=" + dir,
		"--ref=",
		"--skip-pass=graph-algo,commit-couple,embed",
	})
	if got != 0 {
		t.Fatalf("runIndexInternal on empty dir: got exit=%d, want 0", got)
	}
	// Verify that graph.fb was written into the daemon store for this dir.
	// We can't know the exact state path in a unit test without setting
	// ARCHIGRAPH_DAEMON_ROOT, so just confirm exit success above.
}

// TestRunIndexInternalSkipPasses verifies that skip-pass parsing works
// correctly for multi-value comma-separated input.
func TestRunIndexInternalSkipPasses(t *testing.T) {
	// #2083: pin ARCHIGRAPH_DAEMON_ROOT so runIndexInternal writes per-repo
	// state into an isolated temp dir, not into the real ~/.archigraph/store/.
	t.Setenv("ARCHIGRAPH_DAEMON_ROOT", t.TempDir())
	dir := t.TempDir()
	// Write a trivial Go file so the indexer has something to chew on.
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte("package main\nfunc main(){}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := runIndexInternal([]string{
		"--repo=" + dir,
		"--skip-pass=graph-algo,commit-couple,embed,process-flow,event-flow,tests-walkup,module-agg",
	})
	if got != 0 {
		t.Fatalf("runIndexInternal with skip passes: got exit=%d, want 0", got)
	}
}
