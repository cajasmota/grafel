// routing_cwd_worktree_test.go — PH1c (#2087) worktree-sibling resolution tests.
//
// These tests require a real git binary on PATH (they create temp git repos
// and worktrees). Tests are skipped automatically when git is unavailable.
package mcp

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitAvailable returns true when git is on PATH.
func gitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// runSetupGit runs a git command for test setup; fatals on error.
func runSetupGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

// initGitRepo initialises a minimal git repo with one commit so that
// `git worktree add` can succeed (it needs at least one commit).
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	os.MkdirAll(dir, 0o755)
	runSetupGit(t, dir, "init", "-b", "main")
	runSetupGit(t, dir, "config", "user.email", "test@test.invalid")
	runSetupGit(t, dir, "config", "user.name", "Test")
	// Commit an empty file so HEAD is set.
	placeholder := filepath.Join(dir, "README.md")
	if err := os.WriteFile(placeholder, []byte("test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runSetupGit(t, dir, "add", ".")
	runSetupGit(t, dir, "commit", "-m", "init")
}

// TestResolveCWD_WorktreeSiblingResolution verifies that when the cwd is
// inside a linked git worktree of a registered repo — but the worktree
// directory is NOT itself in the registry — ResolveCWD returns
// Source="worktree" with the correct group, repo slug, and ref.
func TestResolveCWD_WorktreeSiblingResolution(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not on PATH — skipping worktree-sibling resolution test")
	}

	tmp := t.TempDir()

	// Create a real git repo (the "registered" repo).
	mainRepo := filepath.Join(tmp, "main-repo")
	initGitRepo(t, mainRepo)

	// Create a linked worktree on a new branch "feat/ph1c-test".
	worktreePath := filepath.Join(tmp, "feat-ph1c-worktree")
	runSetupGit(t, mainRepo, "worktree", "add", "-b", "feat/ph1c-test", worktreePath)

	// Build a registry that knows about mainRepo but NOT the worktree path.
	reg := &Registry{
		Groups: map[string]RegistryGroup{
			"my-group": {
				Repos: map[string]RegistryRepo{
					"main-svc": {Path: mainRepo},
				},
			},
		},
	}
	st := NewState(reg)

	// A subdir inside the worktree — this is the "cwd" the MCP server sees.
	worktreeSub := filepath.Join(worktreePath, "src")
	os.MkdirAll(worktreeSub, 0o755)

	res := ResolveCWD(st, worktreeSub)

	if res.Source != "worktree" {
		t.Fatalf("Source: want %q, got %q (full: %+v)", "worktree", res.Source, res)
	}
	if res.Group != "my-group" {
		t.Errorf("Group: want %q, got %q", "my-group", res.Group)
	}
	if res.RepoSlug != "main-svc" {
		t.Errorf("RepoSlug: want %q, got %q", "main-svc", res.RepoSlug)
	}
	if !res.IsWorktree {
		t.Errorf("IsWorktree: want true, got false")
	}
	// The ref should be the worktree branch we created.
	if res.Ref != "feat/ph1c-test" {
		t.Errorf("Ref: want %q, got %q", "feat/ph1c-test", res.Ref)
	}
	if res.ParentRepoPath != mainRepo {
		t.Errorf("ParentRepoPath: want %q, got %q", mainRepo, res.ParentRepoPath)
	}
}

// TestResolveCWD_DirectRegistryMatch verifies the existing direct-containment
// path still works via ResolveCWD (regression guard for PH1c).
func TestResolveCWD_DirectRegistryMatch(t *testing.T) {
	tmp := t.TempDir()
	repoPath := filepath.Join(tmp, "svc-a")
	os.MkdirAll(repoPath, 0o755)

	reg := &Registry{
		Groups: map[string]RegistryGroup{
			"alpha": {
				Repos: map[string]RegistryRepo{
					"svc-a": {Path: repoPath},
				},
			},
		},
	}
	st := NewState(reg)

	sub := filepath.Join(repoPath, "pkg", "handler")
	os.MkdirAll(sub, 0o755)

	res := ResolveCWD(st, sub)

	if res.Source != "cwd_registry" {
		t.Fatalf("Source: want cwd_registry, got %q", res.Source)
	}
	if res.Group != "alpha" {
		t.Errorf("Group: want alpha, got %q", res.Group)
	}
	if res.RepoSlug != "svc-a" {
		t.Errorf("RepoSlug: want svc-a, got %q", res.RepoSlug)
	}
}

// TestResolveCWD_NoneWhenOutsideAllRepos verifies that cwd outside any
// registered repo and not a worktree returns Source="none".
func TestResolveCWD_NoneWhenOutsideAllRepos(t *testing.T) {
	tmp := t.TempDir()
	repoPath := filepath.Join(tmp, "registered-repo")
	os.MkdirAll(repoPath, 0o755)

	reg := &Registry{
		Groups: map[string]RegistryGroup{
			"grp": {
				Repos: map[string]RegistryRepo{
					"r": {Path: repoPath},
				},
			},
		},
	}
	st := NewState(reg)

	// A completely unrelated directory (not a git repo either).
	unrelated := filepath.Join(tmp, "nowhere")
	os.MkdirAll(unrelated, 0o755)

	res := ResolveCWD(st, unrelated)
	if res.Source != "none" {
		t.Errorf("Source: want none, got %q", res.Source)
	}
}
