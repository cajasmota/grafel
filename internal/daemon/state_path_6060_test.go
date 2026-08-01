// state_path_6060_test.go — issue #6060. StateDirForRepo sits under
// FindGraphFileAnyRef, i.e. under the MCP cold-wake group revive. Its HEAD
// capture used to fork ~5 git subprocesses on EVERY call (~14 ms measured),
// which was the entirety of the full-reload revive cost on a 3-entity graph.
package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initGitRepo6060 creates a real git repo with one commit on branch "main".
func initGitRepo6060(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runIn := func(args ...string) {
		t.Helper()
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
	runIn("init")
	runIn("checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runIn("add", ".")
	runIn("commit", "-m", "init")
	return dir
}

// A repeat StateDirForRepo on an unchanged HEAD must not shell out to git.
//
// Proof the fixture can fail: with the raw gitmeta.Capture restored, the second
// call re-runs git — which is now absent from PATH — so meta.Ref comes back
// empty and the resolved directory collapses to the "_unknown" ref sentinel,
// diverging from the primed value.
func TestStateDirForRepo_NoGitSubprocessOnRepeatCall(t *testing.T) {
	dir := initGitRepo6060(t)

	primed := StateDirForRepo(dir)
	if primed == "" {
		t.Fatal("priming call returned empty state dir")
	}
	wantRef := StateDirForRepoRef(dir, "main")
	if primed != wantRef {
		t.Fatalf("priming call resolved an unexpected ref dir:\n got  %s\n want %s", primed, wantRef)
	}

	// Remove git from PATH: any live capture from here on yields a zero Info,
	// which would resolve to the "_unknown" ref sentinel instead of "main".
	t.Setenv("PATH", "")
	if got := StateDirForRepo(dir); got != primed {
		t.Errorf("repeat StateDirForRepo re-forked git on an unchanged HEAD:\n got  %s\n want %s", got, primed)
	}
}

// The memo must not outlive a HEAD move: a checkout onto a new branch has to be
// reflected in the resolved per-ref directory.
func TestStateDirForRepo_FollowsHeadMove(t *testing.T) {
	dir := initGitRepo6060(t)

	before := StateDirForRepo(dir)
	if before != StateDirForRepoRef(dir, "main") {
		t.Fatalf("unexpected pre-checkout dir %s", before)
	}

	cmd := exec.Command("git", "checkout", "-b", "feature")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git checkout: %v\n%s", err, out)
	}

	after := StateDirForRepo(dir)
	if want := StateDirForRepoRef(dir, "feature"); after != want {
		t.Errorf("state dir did not follow the HEAD move:\n got  %s\n want %s", after, want)
	}
}
