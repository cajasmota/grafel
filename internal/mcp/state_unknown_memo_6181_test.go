// state_unknown_memo_6181_test.go — issue #6181, second sticky site.
//
// gitmeta no longer memoizes a capture that failed because git could not be
// RUN. But internal/mcp memoizes the DERIVED value — the resolved per-ref state
// directory — on the very same HEAD-pointer token, which is an os.Stat that
// succeeds regardless of load. So a repo that resolved to the "_unknown"
// sentinel during a transient git failure keeps being served "_unknown" from
// this layer even after gitmeta has recovered, until HEAD next moves.
//
// The consequence is not cosmetic: applyFlowOverlay feeds this directory
// straight into flows.Read, so a memoized "_unknown" silently drops the
// process-flow overlay from every State.Group call — a successful MCP response
// with data missing and no error anywhere.
package mcp

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
)

// detachedHeadRepo creates a real git repo and detaches HEAD, which is the
// cheapest honest way to make StateDirForRepo resolve to the "_unknown"
// sentinel: `git symbolic-ref --short HEAD` exits non-zero on a detached HEAD,
// so Ref is "" through an entirely healthy git.
func detachedHeadRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	run("init")
	run("checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")
	run("checkout", "--detach", "HEAD")
	return dir
}

// TestCurrentRefDir_DoesNotSealAnUnknownResolution is the #6181 F2 regression.
//
// "_unknown" is the resolution the daemon reaches when it could not determine a
// ref — which happens both legitimately (detached HEAD) and transiently (git
// could not be run). It is never a durable answer worth sealing a memo on: the
// state dir it names holds no graph, so a sealed memo converts a momentary
// failure into a permanent one for the life of the HEAD generation.
//
// The assertion is on the memo flag rather than on a second differing return
// value on purpose: making the underlying resolution change without moving HEAD
// requires injecting a git failure, and that seam is unexported inside gitmeta.
// The flag IS the stickiness.
func TestCurrentRefDir_DoesNotSealAnUnknownResolution(t *testing.T) {
	repo := detachedHeadRepo(t)

	unknownDir := daemon.StateDirForRepoRef(repo, "")
	if unknownDir == "" {
		t.Fatal("fixture unusable: no _unknown sentinel dir")
	}

	lr := &LoadedRepo{Path: repo}
	got := lr.currentRefDirLocked(repo)

	if got != unknownDir {
		t.Fatalf("fixture degenerate: resolved to %q, not the _unknown sentinel %q — "+
			"this test cannot exercise the sticky path", got, unknownDir)
	}
	// Behaviour for the caller must be unchanged: it still gets the resolution.
	if lr.curRefKnown {
		t.Fatalf("an _unknown resolution was sealed into the memo (curRefKnown=true): " +
			"once a transient git failure resolves here, every later State.Group call " +
			"is served _unknown until HEAD moves — flows.Read misses and the process-flow " +
			"overlay is silently dropped (#6181)")
	}
}

// TestCurrentRefDir_StillMemoizesAResolvedRef guards the optimisation the
// mitigation must not damage: a repo on a real branch seals its memo on the
// first call and is served from it thereafter, with no re-resolution. This is
// what #6060 bought and F2 must not spend.
func TestCurrentRefDir_StillMemoizesAResolvedRef(t *testing.T) {
	repo := detachedHeadRepo(t)
	// Re-attach to a branch: now there is a real ref to resolve.
	cmd := exec.Command("git", "checkout", "main")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("checkout main: %v\n%s", err, out)
	}

	lr := &LoadedRepo{Path: repo}
	first := lr.currentRefDirLocked(repo)
	if first == daemon.StateDirForRepoRef(repo, "") {
		t.Fatalf("fixture degenerate: an attached HEAD still resolved to _unknown (%q)", first)
	}
	if !lr.curRefKnown {
		t.Fatal("a real ref resolution was not memoized — the #6060 optimisation is gone")
	}

	// Break the memo's inputs: if it re-resolves, it cannot return the same dir.
	sealed := lr.curRefDir
	lr.curRefDir = "SENTINEL-NOT-RERESOLVED"
	if got := lr.currentRefDirLocked(repo); got != "SENTINEL-NOT-RERESOLVED" {
		t.Fatalf("memo was not used on the second call: got %q, want the sealed value "+
			"(re-resolution costs a Capture per State.Group call per repo)", got)
	}
	lr.curRefDir = sealed
}
