// cache_sha_6079_test.go — issue #6079.
//
// captureCache is keyed on the <gitdir>/HEAD pointer's mtime+size. The doc
// comment claimed "a commit ... always updates <gitdir>/HEAD", and that is not
// true: `git commit` on the branch you are already on writes
// refs/heads/<branch> and appends to logs/HEAD; HEAD itself is a symbolic ref
// and is only rewritten when the BRANCH changes (checkout / switch / detach).
//
// meta.Ref is therefore correctly protected — it changes exactly when the
// symbolic ref changes, which is exactly when the key changes — but Info.SHA
// changes on every commit and goes stale for the lifetime of the key. Its
// consumers (ResolveCWD, grafel_whoami's indexed_sha) then report the previous
// commit with a success response, which is the silent-wrong-answer shape.
package gitmeta

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// commitOnSameBranch adds a commit to dir's current branch and returns the new
// abbreviated SHA.
func commitOnSameBranch(t *testing.T, dir, content string) string {
	t.Helper()
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
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", content)
	return Capture(dir).SHA
}

// TestCaptureCached_HeadPointerIsUnmovedBySameBranchCommit pins the mechanism
// the issue rests on, by execution rather than by assertion in prose: a
// same-branch commit leaves <gitdir>/HEAD byte- and mtime-identical, so the
// HEAD-pointer cache key cannot possibly observe it, while the branch tip does
// move.
func TestCaptureCached_HeadPointerIsUnmovedBySameBranchCommit(t *testing.T) {
	dir := initGitRepo(t)

	before, ok := headPointerKey(dir)
	if !ok {
		t.Fatal("headPointerKey failed on a real checkout")
	}
	tipPath := filepath.Join(dir, ".git", "refs", "heads", "main")
	tipBefore, err := os.ReadFile(tipPath)
	if err != nil {
		t.Fatalf("read branch tip: %v", err)
	}

	commitOnSameBranch(t, dir, "second")

	after, ok := headPointerKey(dir)
	if !ok {
		t.Fatal("headPointerKey failed after commit")
	}
	if after != before {
		t.Fatalf("fixture invalid: the HEAD pointer DID move on a same-branch commit\n before=%+v\n after =%+v", before, after)
	}
	tipAfter, err := os.ReadFile(tipPath)
	if err != nil {
		t.Fatalf("read branch tip after commit: %v", err)
	}
	if string(tipAfter) == string(tipBefore) {
		t.Fatal("fixture invalid: the branch tip did not move — no commit happened")
	}
}

// TestCaptureCachedFresh_InvalidatesOnSameBranchCommit is the #6079 regression:
// the SHA-fresh variant must report the NEW commit after a same-branch commit.
func TestCaptureCachedFresh_InvalidatesOnSameBranchCommit(t *testing.T) {
	resetCaptureCacheForTest()
	dir := initGitRepo(t)

	first := CaptureCachedFresh(dir)
	if first.SHA == "" {
		t.Fatal("fixture degenerate: no SHA captured for the initial commit")
	}
	if first.Ref != "main" {
		t.Fatalf("fixture degenerate: Ref=%q, want main", first.Ref)
	}

	wantSHA := commitOnSameBranch(t, dir, "second")
	if wantSHA == "" || wantSHA == first.SHA {
		t.Fatalf("fixture degenerate: SHA did not advance (%q -> %q)", first.SHA, wantSHA)
	}

	got := CaptureCachedFresh(dir)
	if got.SHA != wantSHA {
		t.Errorf("stale SHA served after a same-branch commit:\n got  %q\n want %q\n"+
			"(the cache key identifies the ref, not the state of the ref)", got.SHA, wantSHA)
	}
	// The branch did not change, so Ref must be untouched — this is the guard
	// against "fixed" by accidentally moving off main.
	if got.Ref != "main" {
		t.Errorf("vacuous: Ref changed to %q — the HEAD-pointer key would have caught that already", got.Ref)
	}
}

// TestCaptureCachedFresh_TipTokenIsContentNotMtime: the branch-tip component of
// the commit token must discriminate on the tip's CONTENT, not on its mtime.
//
// A loose ref is a fixed-width 41-byte file, so a stat-based token varies only
// in mtime — and two commits landing inside one filesystem timestamp tick would
// then produce the same token and serve the earlier commit's SHA. That is
// unreachable on APFS but not a property to leave resting on the filesystem.
// The fixture forces the collision by stamping the post-commit tip with the
// pre-commit mtime, which is exactly what a coarse-granularity filesystem does
// on its own.
func TestCaptureCachedFresh_TipTokenIsContentNotMtime(t *testing.T) {
	resetCaptureCacheForTest()
	dir := initGitRepo(t)
	tipPath := filepath.Join(dir, ".git", "refs", "heads", "main")

	first := CaptureCachedFresh(dir)
	fiBefore, err := os.Stat(tipPath)
	if err != nil {
		t.Fatalf("stat tip: %v", err)
	}

	wantSHA := commitOnSameBranch(t, dir, "second")
	if wantSHA == first.SHA {
		t.Fatalf("fixture degenerate: SHA did not advance (%q)", first.SHA)
	}
	// Collapse the mtime difference the token must NOT depend on.
	if err := os.Chtimes(tipPath, fiBefore.ModTime(), fiBefore.ModTime()); err != nil {
		t.Fatalf("chtimes tip: %v", err)
	}
	fiAfter, err := os.Stat(tipPath)
	if err != nil {
		t.Fatalf("stat tip after: %v", err)
	}
	if !fiAfter.ModTime().Equal(fiBefore.ModTime()) {
		t.Fatalf("fixture degenerate: mtimes still differ (%v vs %v)", fiBefore.ModTime(), fiAfter.ModTime())
	}
	// Non-vacuity: this only proves something if size is genuinely constant, so
	// a stat token really would have collided.
	if fiAfter.Size() != fiBefore.Size() {
		t.Fatalf("vacuous: tip size changed (%d -> %d), so a stat token would have caught this anyway",
			fiBefore.Size(), fiAfter.Size())
	}

	if got := CaptureCachedFresh(dir); got.SHA != wantSHA {
		t.Errorf("commit token collided across a same-tick commit:\n got  %q\n want %q\n"+
			"(the branch-tip component is resting on mtime, not on the tip's bytes)", got.SHA, wantSHA)
	}
}

// TestCaptureCachedFresh_MatchesCapture: the fresh variant must be observably
// identical to the uncached Capture, including on a cache hit.
func TestCaptureCachedFresh_MatchesCapture(t *testing.T) {
	resetCaptureCacheForTest()
	dir := initGitRepo(t)

	want := Capture(dir)
	if got := CaptureCachedFresh(dir); got != want {
		t.Fatalf("CaptureCachedFresh != Capture\n got  %+v\n want %+v", got, want)
	}
	if got := CaptureCachedFresh(dir); got != want {
		t.Fatalf("cached second call diverged: %+v", got)
	}
}

// TestCaptureCachedFresh_HitsCacheNoSubprocess: the fix must not degenerate into
// "no caching". Prime the cache, then remove `git` from PATH — a subprocess
// Capture would then return a zero Info, so a non-zero result proves the second
// call was served from the memo.
func TestCaptureCachedFresh_HitsCacheNoSubprocess(t *testing.T) {
	resetCaptureCacheForTest()
	dir := initGitRepo(t)

	primed := CaptureCachedFresh(dir)
	if primed.SHA == "" {
		t.Fatal("fixture degenerate: nothing primed")
	}

	t.Setenv("PATH", t.TempDir()) // no git on PATH
	if live := Capture(dir); live.SHA != "" {
		t.Skip("git still reachable without PATH; cannot prove the no-subprocess property here")
	}
	if got := CaptureCachedFresh(dir); got != primed {
		t.Errorf("cache miss on an unchanged repo — the fresh variant is re-forking git every call:\n got  %+v\n want %+v", got, primed)
	}
}

// TestCaptureCachedFresh_InvalidatesOnBranchSwitch: the fresh key must still
// cover everything the HEAD-pointer key covered.
func TestCaptureCachedFresh_InvalidatesOnBranchSwitch(t *testing.T) {
	resetCaptureCacheForTest()
	dir := initGitRepo(t)

	if got := CaptureCachedFresh(dir); got.Ref != "main" {
		t.Fatalf("initial Ref=%q, want main", got.Ref)
	}
	cmd := exec.Command("git", "checkout", "-b", "feature")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git checkout: %v\n%s", err, out)
	}
	if got := CaptureCachedFresh(dir); got.Ref != "feature" {
		t.Errorf("stale Ref after a branch switch: %q, want feature", got.Ref)
	}
}

// TestCaptureCachedFresh_DetachedHead: with a detached HEAD the commit id lives
// in HEAD's own bytes, so the key must still track it.
func TestCaptureCachedFresh_DetachedHead(t *testing.T) {
	resetCaptureCacheForTest()
	dir := initGitRepo(t)
	first := CaptureCachedFresh(dir)

	wantSHA := commitOnSameBranch(t, dir, "second")
	if wantSHA == first.SHA {
		t.Fatal("fixture degenerate: SHA did not advance")
	}
	// Detach onto the first commit.
	cmd := exec.Command("git", "checkout", "--detach", "HEAD~1")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git checkout --detach: %v\n%s", err, out)
	}
	got := CaptureCachedFresh(dir)
	if got.SHA != first.SHA {
		t.Errorf("detached HEAD reported SHA %q, want %q", got.SHA, first.SHA)
	}
}

// TestCaptureCachedFresh_NonGitDir: a non-git directory must behave exactly like
// Capture (no panic, no cache).
func TestCaptureCachedFresh_NonGitDir(t *testing.T) {
	resetCaptureCacheForTest()
	dir := t.TempDir()
	if got, want := CaptureCachedFresh(dir), Capture(dir); got != want {
		t.Fatalf("non-git dir: got %+v want %+v", got, want)
	}
	if got := CaptureCachedFresh(""); got != Capture("") {
		t.Fatal("empty path diverged from Capture")
	}
}

// TestCaptureCached_RefStillCachedCheaply: the ref-only key must NOT be widened.
// #6060 removed a per-request git fork from StateDirForRepo by memoizing on the
// HEAD pointer; making that key commit-sensitive would re-fork Capture after
// every commit for a caller that only ever reads meta.Ref. This pins the split.
func TestCaptureCached_RefStillCachedCheaply(t *testing.T) {
	resetCaptureCacheForTest()
	dir := initGitRepo(t)

	primed := CaptureCached(dir)
	if primed.Ref != "main" {
		t.Fatalf("primed Ref=%q, want main", primed.Ref)
	}
	commitOnSameBranch(t, dir, "second")

	t.Setenv("PATH", t.TempDir()) // no git on PATH
	if live := Capture(dir); live.Ref != "" {
		t.Skip("git still reachable without PATH; cannot prove the no-subprocess property here")
	}
	if got := CaptureCached(dir); got.Ref != "main" {
		t.Errorf("CaptureCached re-forked after a same-branch commit — the cheap ref-only key regressed (got %+v)", got)
	}
}
