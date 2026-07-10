// state_path_incremental_ref_test.go — regression coverage for issue #5719:
// the SchedulerIncremental closure in cmd/grafel/daemon.go built the graph
// store path from a possibly-EMPTY ref via StateDirForRepoRef, which encodes
// the empty ref as the literal "_unknown" sentinel. The scheduler legitimately
// invokes the incremental path with ref="" whenever the ref was unknown at
// enqueue time, so the incremental extractor looked for the graph under
// "refs/_unknown/" instead of the real "refs/main/" (or whatever HEAD is),
// the load failed, and the dashboard Graph view fell back to
// "incremental_fallback" forever.
//
// ResolveIncrementalStateDir is the fix: it resolves an empty ref to the
// repo's current HEAD (via StateDirForRepo → gitmeta.Capture) BEFORE
// building the store path, mirroring the pattern already used by
// StateDirForRepo itself and by the dashboard's GraphCache.loadGroupForRef.
package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitRunIncrementalFixture runs a git command inside dir, failing the test on
// error. Mirrors the gitRun helper in e2e_multi_ref_test.go.
func gitRunIncrementalFixture(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@test.invalid",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@test.invalid",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// buildIncrementalRefFixture creates a minimal git repo checked out on
// branch "main" with one commit, so gitmeta.Capture(repoPath).Ref == "main".
func buildIncrementalRefFixture(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH; skipping incremental-ref fixture test")
	}

	var tmpBase string
	if _, err := os.Stat("/tmp"); err == nil {
		tmpBase = "/tmp"
	} else {
		tmpBase = os.TempDir()
	}
	base, err := os.MkdirTemp(tmpBase, "archi-incref-")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })

	repoPath := filepath.Join(base, "repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	gitRunIncrementalFixture(t, repoPath, "init", "-b", "main")
	gitRunIncrementalFixture(t, repoPath, "config", "user.email", "test@test.invalid")
	gitRunIncrementalFixture(t, repoPath, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	gitRunIncrementalFixture(t, repoPath, "add", "README.md")
	gitRunIncrementalFixture(t, repoPath, "commit", "-m", "init")

	return repoPath
}

// TestResolveIncrementalStateDir_EmptyRefResolvesToHEAD is the RED test for
// #5719: with ref="" (the scheduler's "unknown at enqueue time" sentinel
// value) the resolved state dir must be the repo's real HEAD ref directory
// ("refs/main/" for this fixture), NOT the "refs/_unknown/" sentinel that
// StateDirForRepoRef(repoPath, "") alone would produce.
func TestResolveIncrementalStateDir_EmptyRefResolvesToHEAD(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	repoPath := buildIncrementalRefFixture(t)

	got := ResolveIncrementalStateDir(repoPath, "")
	slashGot := filepath.ToSlash(got)

	if strings.Contains(slashGot, "/refs/_unknown") {
		t.Errorf("ResolveIncrementalStateDir(repoPath, \"\") = %q resolved to the _unknown sentinel; want the HEAD (refs/main/) dir", got)
	}
	if !strings.Contains(slashGot, "/refs/main") {
		t.Errorf("ResolveIncrementalStateDir(repoPath, \"\") = %q does not contain '/refs/main'", got)
	}

	// Must match what the dashboard/incremental-fallback path uses for HEAD.
	want := StateDirForRepo(repoPath)
	if got != want {
		t.Errorf("ResolveIncrementalStateDir(repoPath, \"\") = %q, want %q (StateDirForRepo)", got, want)
	}
}

// TestRefSafeEncode_EmptyStillUnknown pins down that RefSafeEncode("") must
// remain "_unknown" — that sentinel is still the documented, correct
// behavior for the low-level encoder. The bug was in SchedulerIncremental
// calling StateDirForRepoRef directly with an empty ref instead of resolving
// it first; the encoder itself is not being changed by this fix.
func TestRefSafeEncode_EmptyStillUnknown(t *testing.T) {
	if got := RefSafeEncode(""); got != "_unknown" {
		t.Errorf("RefSafeEncode(\"\") = %q, want %q (sentinel must stay unchanged)", got, "_unknown")
	}
}

// TestResolveIncrementalStateDir_KnownRefUnchanged is the regression guard:
// a KNOWN, non-empty ref must route through StateDirForRepoRef exactly as
// before — the fix must only change the empty-ref case, preserving
// single-repo / multi-repo / monorepo behavior when the ref is known (e.g. a
// feature branch with a "/" in its name, which must still be percent-encoded
// as a single path segment).
func TestResolveIncrementalStateDir_KnownRefUnchanged(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	got := ResolveIncrementalStateDir("/some/repo", "feature/x")
	want := StateDirForRepoRef("/some/repo", "feature/x")
	if got != want {
		t.Errorf("ResolveIncrementalStateDir(repoPath, \"feature/x\") = %q, want %q (StateDirForRepoRef unchanged)", got, want)
	}
	slashGot := filepath.ToSlash(got)
	if !strings.Contains(slashGot, "/refs/feature%2Fx") {
		t.Errorf("known ref path %q does not contain encoded '/refs/feature%%2Fx' segment", got)
	}
}
