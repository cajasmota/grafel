package gitmeta_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/gitmeta"
)

// normalizeTopLevel converts a path to a canonical form for comparison.
// git rev-parse --show-toplevel returns forward-slash paths even on Windows,
// and may return long-form paths where t.TempDir() returns short 8.3 form
// (e.g. "RUNNER~1" vs "runneradmin").  We normalize to forward slashes and
// lowercase; callers should compare only the test-unique suffix (the part
// after the OS temp root) to avoid mismatches on the user-home segment.
func normalizeTopLevel(p string) string {
	return strings.ToLower(filepath.ToSlash(p))
}

// tempUniqueSuffix extracts the test-unique portion of a temp path, which
// is everything from the first path component that contains the test function
// name onward.  This avoids false mismatches caused by Windows 8.3 short
// names (RUNNER~1) vs long names (runneradmin) in the user-home segment.
func tempUniqueSuffix(normPath string) string {
	// os.TempDir() normed to forward slash is the common prefix to strip.
	tempBase := strings.ToLower(filepath.ToSlash(os.TempDir()))
	// Ensure consistent trailing slash for TrimPrefix.
	if !strings.HasSuffix(tempBase, "/") {
		tempBase += "/"
	}
	if strings.HasPrefix(normPath, tempBase) {
		return strings.TrimPrefix(normPath, tempBase)
	}
	// Fallback: return last two path components (testname/subdir).
	parts := strings.Split(normPath, "/")
	if len(parts) >= 2 {
		return strings.Join(parts[len(parts)-2:], "/")
	}
	return normPath
}

// initBareRepo creates a temp dir with a git repo, an initial commit on
// the given branch name, and returns the repo path.
func initBareRepo(t *testing.T, branch string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch="+branch)
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")

	// Write a file and commit so HEAD is not empty.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")
	return dir
}

func TestCapture_mainBranch(t *testing.T) {
	dir := initBareRepo(t, "main")
	info := gitmeta.Capture(dir)

	if info.Ref != "main" {
		t.Errorf("Ref = %q, want %q", info.Ref, "main")
	}
	if len(info.SHA) != 12 {
		t.Errorf("SHA len = %d, want 12 (got %q)", len(info.SHA), info.SHA)
	}
	if info.IsWorktree {
		t.Error("IsWorktree should be false for a regular checkout")
	}
	// Normalize both to forward-slash lowercase for cross-platform comparison.
	// On Windows, t.TempDir() may return short 8.3 paths (RUNNER~1) while git
	// returns long-form paths (runneradmin), so exact match is unreliable.
	// We strip the OS temp-root prefix and compare only the test-unique suffix
	// (the test-name+random sub-directory), which is identical in both forms.
	normTop := normalizeTopLevel(info.TopLevel)
	normDir := normalizeTopLevel(dir)
	suffixTop := tempUniqueSuffix(normTop)
	suffixDir := tempUniqueSuffix(normDir)
	if suffixTop != suffixDir {
		t.Errorf("TopLevel = %q, want suffix %q", info.TopLevel, dir)
	}
}

func TestCapture_featureBranch(t *testing.T) {
	dir := initBareRepo(t, "main")
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("checkout", "-b", "feat/my-feature")

	info := gitmeta.Capture(dir)
	if info.Ref != "feat/my-feature" {
		t.Errorf("Ref = %q, want %q", info.Ref, "feat/my-feature")
	}
}

func TestCapture_detachedHEAD(t *testing.T) {
	dir := initBareRepo(t, "main")

	// Get SHA to detach at.
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.TrimSpace(string(out))

	cmd := exec.Command("git", "checkout", "--detach", sha)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git checkout --detach: %v\n%s", err, out)
	}

	info := gitmeta.Capture(dir)
	if info.Ref != "" {
		t.Errorf("Ref = %q, want empty for detached HEAD", info.Ref)
	}
	if len(info.SHA) != 12 {
		t.Errorf("SHA len = %d, want 12 (got %q)", len(info.SHA), info.SHA)
	}
}

func TestCapture_nonGitDir(t *testing.T) {
	dir := t.TempDir() // plain dir, no .git
	info := gitmeta.Capture(dir)

	if info.SHA != "" || info.Ref != "" || info.IsWorktree || info.TopLevel != "" {
		t.Errorf("expected zero-value Info for non-git dir, got %+v", info)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// IsDefaultBranch tests (#2170)
// ─────────────────────────────────────────────────────────────────────────────

func TestIsDefaultBranch_MainBranchReturnsTrue(t *testing.T) {
	dir := initBareRepo(t, "main")
	if !gitmeta.IsDefaultBranch(dir) {
		t.Error("IsDefaultBranch should return true on a repo checked out on main")
	}
}

func TestIsDefaultBranch_MasterBranchReturnsTrue(t *testing.T) {
	dir := initBareRepo(t, "master")
	if !gitmeta.IsDefaultBranch(dir) {
		t.Error("IsDefaultBranch should return true on a repo checked out on master")
	}
}

func TestIsDefaultBranch_FeatureBranchReturnsFalse(t *testing.T) {
	dir := initBareRepo(t, "main")
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("checkout", "-b", "feat/some-feature")

	if gitmeta.IsDefaultBranch(dir) {
		t.Error("IsDefaultBranch should return false on a feature branch")
	}
}

func TestIsDefaultBranch_NonGitDirReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	if gitmeta.IsDefaultBranch(dir) {
		t.Error("IsDefaultBranch should return false for a non-git directory")
	}
}

func TestIsDefaultBranch_DetachedHEADReturnsFalse(t *testing.T) {
	dir := initBareRepo(t, "main")
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.TrimSpace(string(out))

	cmd := exec.Command("git", "checkout", "--detach", sha)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git checkout --detach: %v\n%s", err, out)
	}

	if gitmeta.IsDefaultBranch(dir) {
		t.Error("IsDefaultBranch should return false for detached HEAD")
	}
}

func TestCapture_worktree(t *testing.T) {
	main := initBareRepo(t, "main")
	wtDir := t.TempDir()

	// Remove the auto-created dir so git worktree add can use it.
	os.RemoveAll(wtDir)

	cmd := exec.Command("git", "worktree", "add", "-b", "wt-branch", wtDir)
	cmd.Dir = main
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("git worktree add failed (old git?): %v\n%s", err, out)
	}

	info := gitmeta.Capture(wtDir)
	if !info.IsWorktree {
		t.Errorf("IsWorktree = false, want true for linked worktree")
	}
	if info.Ref != "wt-branch" {
		t.Errorf("Ref = %q, want %q", info.Ref, "wt-branch")
	}
	if len(info.SHA) != 12 {
		t.Errorf("SHA len = %d, want 12 (got %q)", len(info.SHA), info.SHA)
	}
}
