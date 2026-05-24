package gitmeta_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/gitmeta"
)

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
	if !strings.HasSuffix(info.TopLevel, dir) && info.TopLevel != dir {
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
