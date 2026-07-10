package diff_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/indexer/diff"
)

// initGitRepoForCommitTest creates a minimal git repo with a committer identity
// configured so `git commit` succeeds in CI sandboxes with no global config.
func initGitRepoForCommitTest(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
}

func gitCommitAllForCommitTest(t *testing.T, dir, msg string) string {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("add", "-A")
	run("commit", "-q", "-m", msg)

	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// TestSaveManifest_CapturesFullCommitSHA is the RED test for #5727/#5729-W1.
//
// diff.Manifest already records the SHORT (12-char) HEAD commit in GitCommit
// (see headCommit in diff.go). Nothing today records the FULL 40-char SHA,
// which grafel_index_status / `grafel status` need to expose an unambiguous
// indexed_commit alongside the existing short form. This asserts SaveManifest
// populates a new GitCommitFull field with the full SHA, and that it is a
// prefix-consistent match with the existing short GitCommit.
func TestSaveManifest_CapturesFullCommitSHA(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()

	initGitRepoForCommitTest(t, repo)
	writeFile(t, repo, "main.go", "package main\n\nfunc main() {}\n")
	fullSHA := gitCommitAllForCommitTest(t, repo, "initial commit")

	m := diff.LoadManifest(stateDir)
	if err := diff.SaveManifest(stateDir, repo, m); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	m2 := diff.LoadManifest(stateDir)
	if m2.GitCommit == "" {
		t.Fatal("sanity: GitCommit (short) was not populated")
	}
	if m2.GitCommitFull == "" {
		t.Fatal("GitCommitFull was not populated by SaveManifest")
	}
	if m2.GitCommitFull != fullSHA {
		t.Errorf("GitCommitFull = %q, want %q", m2.GitCommitFull, fullSHA)
	}
	if !strings.HasPrefix(m2.GitCommitFull, m2.GitCommit) {
		t.Errorf("GitCommitFull %q does not start with short GitCommit %q", m2.GitCommitFull, m2.GitCommit)
	}
}
