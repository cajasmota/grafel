package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/registry"
)

// initGitRepo creates a real git repo at dir with one commit so that
// `git rev-parse --git-common-dir` and `git worktree list` behave normally.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
	run("add", ".")
	run("commit", "-m", "init")
}

// writeWorktreeGroup registers a group named `name` with the given repos and
// track_worktrees enabled, via a temp GRAFEL_HOME registry.
func writeWorktreeGroup(t *testing.T, name string, repos []registry.Repo) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GRAFEL_HOME", home)

	cfg := &registry.GroupConfig{Name: name, Repos: repos}
	cfg.Features.TrackWorktrees = true

	cfgPath := filepath.Join(home, name+".json")
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save group config: %v", err)
	}
	if err := registry.AddGroup(name, cfgPath); err != nil {
		t.Fatalf("add group: %v", err)
	}
}

// TestDaemonWorktreeParents_MonorepoCollapses is the #5675 regression proof.
// One git root with TWO registered module slugs at distinct subdir paths must
// produce exactly ONE worktree parent (they share a git common-dir, so they
// would otherwise independently discover the SAME worktrees → reindex storm).
func TestDaemonWorktreeParents_MonorepoCollapses(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	modA := filepath.Join(root, "services", "a")
	modB := filepath.Join(root, "services", "b")
	if err := os.MkdirAll(modA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(modB, 0o755); err != nil {
		t.Fatal(err)
	}

	writeWorktreeGroup(t, "mono", []registry.Repo{
		{Slug: "mono-a", Path: modA},
		{Slug: "mono-b", Path: modB},
	})

	got := daemonWorktreeParents()
	if len(got) != 1 {
		t.Fatalf("monorepo: got %d parents, want 1 (slugs sharing one git root must collapse): %+v", len(got), got)
	}
}

// TestDaemonWorktreeParents_SeparateReposStaySeparate proves no regression for
// a group of RELATED but independent repos (each its own .git / distinct git
// root): each must remain its OWN parent.
func TestDaemonWorktreeParents_SeparateReposStaySeparate(t *testing.T) {
	base := t.TempDir()
	repo1 := filepath.Join(base, "r1")
	repo2 := filepath.Join(base, "r2")
	initGitRepo(t, repo1)
	initGitRepo(t, repo2)

	writeWorktreeGroup(t, "related", []registry.Repo{
		{Slug: "r1", Path: repo1},
		{Slug: "r2", Path: repo2},
	})

	got := daemonWorktreeParents()
	if len(got) != 2 {
		t.Fatalf("separate repos: got %d parents, want 2 (distinct git roots must stay separate): %+v", len(got), got)
	}
}

// TestDaemonWorktreeParents_SingleRepo: one repo → one parent.
func TestDaemonWorktreeParents_SingleRepo(t *testing.T) {
	repo := t.TempDir()
	initGitRepo(t, repo)

	writeWorktreeGroup(t, "single", []registry.Repo{
		{Slug: "single", Path: repo},
	})

	got := daemonWorktreeParents()
	if len(got) != 1 {
		t.Fatalf("single repo: got %d parents, want 1: %+v", len(got), got)
	}
}

// TestDaemonWorktreeParents_NonGitGraceful: a registered path that is NOT a git
// repo must not panic and must fall back to path-keyed behavior (kept, not
// dropped). Two distinct non-git paths → two parents.
func TestDaemonWorktreeParents_NonGitGraceful(t *testing.T) {
	base := t.TempDir()
	p1 := filepath.Join(base, "notgit1")
	p2 := filepath.Join(base, "notgit2")
	if err := os.MkdirAll(p1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(p2, 0o755); err != nil {
		t.Fatal(err)
	}

	writeWorktreeGroup(t, "nongit", []registry.Repo{
		{Slug: "ng1", Path: p1},
		{Slug: "ng2", Path: p2},
	})

	got := daemonWorktreeParents()
	if len(got) != 2 {
		t.Fatalf("non-git paths: got %d parents, want 2 (fallback path-keyed, none dropped): %+v", len(got), got)
	}
}
