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
	"runtime"
	"strings"
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
// This asserts on the memo flag, which is the stickiness itself. The end-to-end
// form — the value the CALLER receives, before and after a git outage, with HEAD
// never moving — is TestCurrentRefDir_RecoversAfterATransientGitFailure below;
// no unexported seam is needed for it, a PATH stub reaches the same path.
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

// countingGitStub replaces `git` on PATH with a script that records every
// invocation to a file and then behaves as `exit <code>`. It returns a func
// reporting how many times git was forked. Reaching the real resolution path
// from inside package mcp needs nothing unexported — a PATH stub is enough.
func countingGitStub(t *testing.T, body string) func() int {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub git script is POSIX-shell only")
	}
	binDir := t.TempDir()
	counter := filepath.Join(binDir, "calls")
	script := "#!/bin/sh\necho x >> " + counter + "\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	return func() int {
		data, err := os.ReadFile(counter)
		if err != nil {
			return 0
		}
		return strings.Count(string(data), "x")
	}
}

// TestCurrentRefDir_NonGitPathDoesNotForkPerCall is the F2a regression.
//
// Refusing to seal an "_unknown" resolution (#6181) is correct for a repo whose
// ref might later resolve. It is WRONG for a registered path with no .git at
// all: gitmeta.CaptureCached cannot memoize such a path — headPointerKey fails,
// so it falls through to a live, uncached Capture — and re-resolving it on every
// call forks git every time. That is precisely the per-call fork #6060 removed,
// and this file's own comment (see currentRefDirLocked) names the non-git path
// as the entire reason the memo exists.
//
// A path with no HEAD has no ref that could later resolve, so there is nothing
// to recover to and nothing is lost by sealing it.
func TestCurrentRefDir_NonGitPathDoesNotForkPerCall(t *testing.T) {
	dir := t.TempDir() // deliberately no .git
	forks := countingGitStub(t, "exit 128")

	lr := &LoadedRepo{Path: dir}
	var last string
	for i := 0; i < 5; i++ {
		last = lr.currentRefDirLocked(dir)
	}
	if last == "" {
		t.Fatal("fixture unusable: no resolution at all")
	}
	if n := forks(); n > 1 {
		t.Fatalf("a non-git registered path forked git %d times across 5 calls "+
			"(want at most 1): the _unknown guard re-resolves a path gitmeta cannot "+
			"memoize, reinstating the per-call fork #6060 removed — one such directory "+
			"means one fork per MCP tool call, forever", n)
	}
}

// TestCurrentRefDir_DetachedHeadDoesNotForkPerCall pins the other side: a real
// repo on a detached HEAD is left unsealed on purpose, and that must stay cheap.
// gitmeta CAN memoize it (git ran and answered), so re-resolving is a map lookup
// and a stat, not a fork.
func TestCurrentRefDir_DetachedHeadDoesNotForkPerCall(t *testing.T) {
	repo := detachedHeadRepo(t)

	lr := &LoadedRepo{Path: repo}
	if got := lr.currentRefDirLocked(repo); got != daemon.StateDirForRepoRef(repo, "") {
		t.Fatalf("fixture degenerate: detached HEAD resolved to %q, not the sentinel", got)
	}

	// Only now install the counter: the priming call above is allowed to fork.
	forks := countingGitStub(t, "exit 128")
	for i := 0; i < 5; i++ {
		lr.currentRefDirLocked(repo)
	}
	if n := forks(); n != 0 {
		t.Fatalf("detached HEAD forked git %d times across 5 calls, want 0 — "+
			"gitmeta memoizes a capture git actually answered", n)
	}
}

// TestCurrentRefDir_RecoversAfterATransientGitFailure is the end-to-end
// assertion, on the value the CALLER receives rather than on the memo flag.
//
// A real repo on a real branch, git killed by a signal for the duration of the
// outage, HEAD never touched. During the outage the resolution is the "_unknown"
// sentinel; once git is healthy again the very next call must return the branch
// directory — with no HEAD movement and no daemon restart to rescue it.
func TestCurrentRefDir_RecoversAfterATransientGitFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub git script is POSIX-shell only")
	}
	repo := detachedHeadRepo(t)
	cmd := exec.Command("git", "checkout", "main")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("checkout main: %v\n%s", err, out)
	}
	wantDir := daemon.StateDirForRepoRef(repo, "main")
	realPath := os.Getenv("PATH")

	// The outage: every git dies by signal, so nothing may be memoized anywhere.
	countingGitStub(t, "kill -9 $$")

	lr := &LoadedRepo{Path: repo}
	during := lr.currentRefDirLocked(repo)
	if during != daemon.StateDirForRepoRef(repo, "") {
		t.Fatalf("during outage: resolved to %q, want the _unknown sentinel", during)
	}
	if lr.curRefKnown {
		t.Fatal("during outage: an _unknown resolution was sealed — the repo is now " +
			"pinned to a state dir that holds no graph until HEAD moves (#6181)")
	}

	// git recovers. HEAD has not moved, so nothing invalidates anything.
	t.Setenv("PATH", realPath)
	if after := lr.currentRefDirLocked(repo); after != wantDir {
		t.Fatalf("after recovery: resolved to %q, want %q — the repo did not recover "+
			"from a transient git failure", after, wantDir)
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
