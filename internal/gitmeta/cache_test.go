package gitmeta

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// initGitRepo creates a real git repo with one commit and returns its path.
func initGitRepo(t *testing.T) string {
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

// TestCaptureCached_MatchesCapture asserts the cached variant returns the exact
// same Info as the uncached Capture (correctness — no response-shape change).
func TestCaptureCached_MatchesCapture(t *testing.T) {
	resetCaptureCacheForTest()
	dir := initGitRepo(t)

	want := Capture(dir)
	got := CaptureCached(dir)
	if got != want {
		t.Fatalf("CaptureCached != Capture\n got  %+v\n want %+v", got, want)
	}
	// Second call must also agree (served from cache).
	if got2 := CaptureCached(dir); got2 != want {
		t.Fatalf("cached second call diverged: %+v", got2)
	}
}

// TestCaptureCached_HitsCacheNoSubprocess proves the cache avoids the O(git)
// subprocess work on the steady-state path: after priming the cache we delete
// `git` from PATH so any subprocess Capture would yield a zero Info; the cached
// call must still return the primed (non-zero) value, proving it never shelled
// out. This is the anti-O(N)/anti-subprocess discipline check (#3325/#3648).
func TestCaptureCached_HitsCacheNoSubprocess(t *testing.T) {
	resetCaptureCacheForTest()
	dir := initGitRepo(t)

	primed := CaptureCached(dir) // populates the memo via real git
	if primed.SHA == "" || primed.TopLevel == "" {
		t.Fatalf("priming failed, got %+v (is git installed?)", primed)
	}

	// Make git unavailable: a live Capture would now return zero-value Info.
	t.Setenv("PATH", "")
	if live := Capture(dir); live != (Info{}) {
		t.Fatalf("expected empty Info without git on PATH, got %+v", live)
	}

	// The cached call must still return the primed value — proof it did NOT
	// re-run git (HEAD mtime unchanged ⇒ cache hit).
	if cached := CaptureCached(dir); cached != primed {
		t.Fatalf("cache miss on unchanged HEAD: re-ran subprocess?\n got  %+v\n want %+v", cached, primed)
	}
}

// TestCaptureCached_InvalidatesOnHeadChange asserts a checkout (which rewrites
// HEAD) busts the cache so a stale ref is never served.
func TestCaptureCached_InvalidatesOnHeadChange(t *testing.T) {
	resetCaptureCacheForTest()
	dir := initGitRepo(t)

	first := CaptureCached(dir)
	if first.Ref != "main" {
		t.Fatalf("expected ref main, got %q", first.Ref)
	}

	// Switch to a new branch — rewrites .git/HEAD.
	time.Sleep(10 * time.Millisecond) // ensure a distinct mtime on coarse FS clocks
	cmd := exec.Command("git", "checkout", "-b", "feature/x")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("checkout: %v\n%s", err, out)
	}

	got := CaptureCached(dir)
	if got.Ref != "feature/x" {
		t.Fatalf("stale ref served after checkout: got %q want feature/x", got.Ref)
	}
}

// BenchmarkCapture_vs_CaptureCached documents the steady-state speedup: the
// cached path is a single os.Stat vs ~5 git subprocesses.
func BenchmarkCaptureUncached(b *testing.B) {
	dir := benchRepo(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Capture(dir)
	}
}

func BenchmarkCaptureCached(b *testing.B) {
	resetCaptureCacheForTest()
	dir := benchRepo(b)
	_ = CaptureCached(dir) // prime
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CaptureCached(dir)
	}
}

func benchRepo(b *testing.B) string {
	b.Helper()
	dir := b.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			b.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("checkout", "-b", "main")
	_ = os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644)
	run("add", ".")
	run("commit", "-m", "init")
	return dir
}
