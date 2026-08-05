package gitmeta

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// withGitUnavailable installs a seam that simulates git being un-runnable — the
// 2s deadline firing, or fork returning EAGAIN under load. It is deliberately
// injected rather than provoked: you cannot reliably starve a machine into a
// fork failure, and #6181 is exactly the load-correlated case.
func withGitUnavailable(t *testing.T) *int {
	t.Helper()
	calls := 0
	prev := runGitFn
	runGitFn = func(dir string, args ...string) (string, gitStatus) {
		calls++
		return "", gitUnavailable
	}
	t.Cleanup(func() { runGitFn = prev })
	return &calls
}

// generousDeadline removes the production 2s deadline for the duration of a
// test that needs a git invocation to actually COMPLETE.
//
// This is not belt-and-braces. It was added after a real failure: under load
// average 8.2 on a shared machine, forking a stub `git` that does nothing but
// `exit 128` took longer than 2s, the deadline fired, and the classifier
// correctly reported gitUnavailable to a test asserting gitAnswered. That is the
// #6181 production trigger reproducing spontaneously in the test suite — which
// is a fine thing to have observed and a terrible thing to leave in a test whose
// subject is the OTHER branch. Tests that assert on the timeout branch set their
// own short deadline instead.
func generousDeadline(t *testing.T) {
	t.Helper()
	prev := gitCallTimeout
	gitCallTimeout = 2 * time.Minute
	t.Cleanup(func() { gitCallTimeout = prev })
}

// TestGitCallTimeoutDefault pins the production deadline.
//
// generousDeadline now lifts it in most tests in this file, and the only tests
// that set it deliberately set it short — so nothing else would notice if the
// default changed. The value is load-bearing in both directions: too low and
// healthy forks are classified unavailable (measured p99.9 ≈ 171ms under load,
// so 2s is ~12× headroom); too high and a genuinely wedged git blocks the MCP
// query path for that long.
func TestGitCallTimeoutDefault(t *testing.T) {
	if gitCallTimeout != 2*time.Second {
		t.Fatalf("gitCallTimeout default is %v, want 2s — if this is a deliberate "+
			"change, weigh it against both failure modes above, not just one", gitCallTimeout)
	}
}

// countingRealGit installs a seam that forwards to real git while counting
// invocations, so a test can prove a second CaptureCached did (or did not) fork.
func countingRealGit(t *testing.T) *int {
	t.Helper()
	calls := 0
	prev := runGitFn
	runGitFn = func(dir string, args ...string) (string, gitStatus) {
		calls++
		return prev(dir, args...)
	}
	t.Cleanup(func() { runGitFn = prev })
	return &calls
}

// TestCaptureCached_DoesNotMemoizeUnrunnableGit is the #6181 regression.
//
// A transient failure to RUN git (2s timeout, EAGAIN on fork) makes Capture
// return the zero Info with Ref == "". headPointerKey is a pure os.Stat that
// succeeds regardless of load, so the "don't cache non-git paths" escape does
// not fire and that momentary zero value gets memoized against a HEAD that has
// not moved. Every later lookup then resolves the repo to refs/_unknown — a
// state dir that never holds a graph — so every incremental pass falls back to
// a full reindex until HEAD moves or the daemon restarts.
//
// The trigger is transient; the consequence must not be sticky.
func TestCaptureCached_DoesNotMemoizeUnrunnableGit(t *testing.T) {
	generousDeadline(t)
	resetCaptureCacheForTest()
	dir := initGitRepo(t)

	stop := withGitUnavailable(t)
	if got := CaptureCached(dir); got != (Info{}) {
		t.Fatalf("expected zero Info while git is unavailable, got %+v", got)
	}
	if *stop == 0 {
		t.Fatal("seam was never exercised — the test is not testing anything")
	}
	runGitFn = runGitReal // git is schedulable again; HEAD never moved

	got := CaptureCached(dir)
	if got.Ref != "main" {
		t.Fatalf("repo is stuck on a memoized failure: Ref=%q want %q "+
			"(a failed Capture was cached against an unchanged HEAD, pinning the "+
			"repo to refs/_unknown and a full reindex every pass)", got.Ref, "main")
	}
}

// TestCaptureCachedFresh_DoesNotMemoizeUnrunnableGit is the same defect on the
// commit-fresh variant (ResolveCWD / grafel_whoami), which memoizes on the same
// unconditional write.
func TestCaptureCachedFresh_DoesNotMemoizeUnrunnableGit(t *testing.T) {
	generousDeadline(t)
	resetCaptureCacheForTest()
	dir := initGitRepo(t)

	withGitUnavailable(t)
	if got := CaptureCachedFresh(dir); got != (Info{}) {
		t.Fatalf("expected zero Info while git is unavailable, got %+v", got)
	}
	runGitFn = runGitReal

	got := CaptureCachedFresh(dir)
	if got.Ref != "main" || got.SHA == "" {
		t.Fatalf("stuck on a memoized failure: %+v", got)
	}
}

// TestCaptureCached_DoesNotMemoizeAMidCaptureFailure covers the partial case:
// `rev-parse --show-toplevel` succeeds, so Capture proceeds, and the LATER
// `symbolic-ref` — the call that produces .Ref, the only field the hot caller
// reads — is the one that cannot be run. The resulting Ref == "" is just as
// poisonous as a total failure and must not be memoized either. Trust is a
// property of the whole capture, not of its first call.
func TestCaptureCached_DoesNotMemoizeAMidCaptureFailure(t *testing.T) {
	generousDeadline(t)
	resetCaptureCacheForTest()
	dir := initGitRepo(t)

	prev := runGitFn
	runGitFn = func(d string, args ...string) (string, gitStatus) {
		if len(args) > 0 && args[0] == "symbolic-ref" {
			return "", gitUnavailable
		}
		return prev(d, args...)
	}
	t.Cleanup(func() { runGitFn = prev })

	if got := CaptureCached(dir); got.Ref != "" {
		t.Fatalf("fixture broken: expected empty Ref, got %+v", got)
	}
	runGitFn = prev

	if got := CaptureCached(dir); got.Ref != "main" {
		t.Fatalf("repo stuck on a memoized mid-capture failure: Ref=%q want \"main\"", got.Ref)
	}
}

// TestCapture_DistrustIsPerCall_EverySubcommand closes the gap that the
// mid-capture test alone leaves open (F3).
//
// captureStatus claims trust is a property of the WHOLE capture, but only the
// --show-toplevel and symbolic-ref calls were actually pinned. The remaining
// three feed .SHA and .IsWorktree, which CaptureCachedFresh serves to
// grafel_whoami and ResolveCWD — so a narrowing of the distrust rule would
// memoize SHA=""/IsWorktree=false from a moment and no test would notice.
//
// One case per git invocation Capture makes after the top-level probe.
func TestCapture_DistrustIsPerCall_EverySubcommand(t *testing.T) {
	matches := func(args, want []string) bool {
		if len(args) < len(want) {
			return false
		}
		for i, w := range want {
			if args[i] != w {
				return false
			}
		}
		return true
	}

	cases := []struct {
		name string
		args []string
	}{
		{"sha", []string{"rev-parse", "--short=12", "HEAD"}},
		{"symbolic-ref", []string{"symbolic-ref", "--short", "HEAD"}},
		{"git-dir", []string{"rev-parse", "--git-dir"}},
		{"git-common-dir", []string{"rev-parse", "--git-common-dir"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			generousDeadline(t)
			resetCaptureCacheForTest()
			dir := initGitRepo(t)

			prev := runGitFn
			hit := false
			runGitFn = func(d string, args ...string) (string, gitStatus) {
				if matches(args, tc.args) {
					hit = true
					return "", gitUnavailable
				}
				return prev(d, args...)
			}
			t.Cleanup(func() { runGitFn = prev })

			_, trusted := captureStatus(dir)
			if !hit {
				t.Fatalf("fixture never intercepted %v — Capture no longer makes this "+
					"call, so this case proves nothing", tc.args)
			}
			if trusted {
				t.Fatalf("capture marked trusted despite %v being un-runnable: a "+
					"moment's failure would be memoized", tc.args)
			}

			// And end-to-end: nothing may have been memoized by either variant.
			_ = CaptureCached(dir)
			_ = CaptureCachedFresh(dir)
			runGitFn = prev

			if got := CaptureCached(dir); got.Ref != "main" || got.TopLevel == "" {
				t.Fatalf("CaptureCached stuck on a memoized failure: %+v", got)
			}
			if got := CaptureCachedFresh(dir); got.Ref != "main" || got.SHA == "" {
				t.Fatalf("CaptureCachedFresh stuck on a memoized failure: %+v", got)
			}
		})
	}
}

// TestCaptureCached_StillCachesAGenuineNonRepo pins the other half of the
// distinction. A path where git RAN and reported "not a git repository" is a
// durable fact, and caching it is a real optimisation (the daemon walks such
// paths repeatedly). The fix must not throw that away by refusing to cache
// every empty result.
//
// The fixture is a directory with an empty .git dir: headPointerKey resolves it
// (so the :330 escape does NOT fire and the cache write is reached), while git
// exits non-zero with "not a git repository".
func TestCaptureCached_StillCachesAGenuineNonRepo(t *testing.T) {
	generousDeadline(t)
	resetCaptureCacheForTest()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := headPointerKey(dir); !ok {
		t.Fatal("fixture unusable: headPointerKey must resolve, else the cache " +
			"write is never reached and this test proves nothing")
	}

	calls := countingRealGit(t)
	if got := CaptureCached(dir); got != (Info{}) {
		t.Fatalf("expected zero Info for a non-repo, got %+v", got)
	}
	first := *calls
	if first == 0 {
		t.Fatal("fixture unusable: no git invocation happened")
	}
	if got := CaptureCached(dir); got != (Info{}) {
		t.Fatalf("second call: %+v", got)
	}
	if *calls != first {
		t.Fatalf("non-repo answer was not cached: %d extra git invocations on the "+
			"second call — the caching optimisation was thrown away", *calls-first)
	}
}

// TestRunGitStatus_TimeoutIsUnavailableNotAnAnswer pins the branch that the
// production incident actually took: the 2s deadline firing.
//
// CommandContext kills the child when the deadline fires, and a killed child
// surfaces as an *exec.ExitError — indistinguishable, by error type alone, from
// git exiting 128 with "not a git repository". Only the ctx.Err() check
// separates them. Without it a timeout is classified as a durable answer and
// the zero Info gets memoized again, which is #6181 verbatim.
//
// A real 2s wait is not needed to exercise it: gitCallTimeout is shrunk and a
// stub `git` that sleeps stands in for a wedged one.
func TestRunGitStatus_TimeoutIsUnavailableNotAnAnswer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub git script is POSIX-shell only")
	}
	binDir := t.TempDir()
	stub := filepath.Join(binDir, "git")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	prev := gitCallTimeout
	gitCallTimeout = 50 * time.Millisecond
	t.Cleanup(func() { gitCallTimeout = prev })

	out, st := runGitStatus(t.TempDir(), "rev-parse", "--show-toplevel")
	if st != gitUnavailable || out != "" {
		t.Fatalf("a timed-out git was classified as (%q, %v), want (\"\", gitUnavailable) — "+
			"a killed child is an *exec.ExitError, so without the deadline check it "+
			"reads as a durable answer and gets cached (#6181)", out, st)
	}
}

// TestCaptureCached_DoesNotMemoizeATimeout is the end-to-end form of the above:
// a timing-out git must leave no memo behind.
func TestCaptureCached_DoesNotMemoizeATimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub git script is POSIX-shell only")
	}
	resetCaptureCacheForTest()
	dir := initGitRepo(t)

	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	realPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir)
	prev := gitCallTimeout
	gitCallTimeout = 50 * time.Millisecond
	t.Cleanup(func() { gitCallTimeout = prev })

	if got := CaptureCached(dir); got != (Info{}) {
		t.Fatalf("expected zero Info from a timing-out git, got %+v", got)
	}

	// git is healthy again; HEAD never moved.
	t.Setenv("PATH", realPath)
	gitCallTimeout = prev
	if got := CaptureCached(dir); got.Ref != "main" {
		t.Fatalf("repo stuck on a memoized timeout: Ref=%q want \"main\"", got.Ref)
	}
}

// stubGit installs a fake `git` on PATH with the given shell body and returns
// the real PATH so a test can restore it.
func stubGit(t *testing.T, body string) (realPath string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub git script is POSIX-shell only")
	}
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	realPath = os.Getenv("PATH")
	t.Setenv("PATH", binDir)
	return realPath
}

// TestRunGitStatus_SignalDeathIsUnavailableNotAnAnswer covers the general case
// that the deadline check is only a subset of.
//
// A git killed by the OOM killer, macOS jetsam, or a SIGTERM propagated to the
// daemon's process group surfaces as an *exec.ExitError with ctx.Err() == nil —
// so a deadline-only check classifies it as a durable answer about the repo and
// caches the zero Info. That is #6181 verbatim, and it is MORE load-correlated
// than the timeout case, not less: the OOM killer fires under exactly the memory
// pressure that makes forks fail.
//
// The correct axis is signalled-vs-exited, not deadline-vs-not. ExitCode()
// returns -1 when the process was terminated by a signal, on every platform.
func TestRunGitStatus_SignalDeathIsUnavailableNotAnAnswer(t *testing.T) {
	stubGit(t, "kill -9 $$")
	// The deadline deliberately never fires: this must be caught by signal
	// detection, not by ctx.Err().
	generousDeadline(t)

	out, st := runGitStatus(t.TempDir(), "rev-parse", "--show-toplevel")
	if st != gitUnavailable || out != "" {
		t.Fatalf("a SIGKILLed git was classified as (%q, %v), want (\"\", gitUnavailable) — "+
			"a signalled process is an *exec.ExitError with ctx.Err()==nil, so a "+
			"deadline-only check reads it as a durable answer and caches it (#6181)", out, st)
	}
}

// TestCaptureCached_DoesNotMemoizeASignalledGit is the end-to-end form: an
// OOM-killed git must not pin the repo to refs/_unknown.
func TestCaptureCached_DoesNotMemoizeASignalledGit(t *testing.T) {
	resetCaptureCacheForTest()
	dir := initGitRepo(t)

	realPath := stubGit(t, "kill -9 $$")
	generousDeadline(t)

	if got := CaptureCached(dir); got != (Info{}) {
		t.Fatalf("expected zero Info from a killed git, got %+v", got)
	}

	// git is healthy again; HEAD never moved.
	t.Setenv("PATH", realPath)
	if got := CaptureCached(dir); got.Ref != "main" {
		t.Fatalf("repo pinned to _unknown by an OOM-killed git: Ref=%q want \"main\"", got.Ref)
	}
}

// TestRunGitStatus_NormalNonZeroExitStaysAnAnswer guards the other side of F1:
// splitting on signalled-vs-exited must not sweep git's ordinary non-zero exits
// (128 for "not a git repository") into gitUnavailable and throw the caching
// optimisation away.
func TestRunGitStatus_NormalNonZeroExitStaysAnAnswer(t *testing.T) {
	stubGit(t, "exit 128")
	generousDeadline(t)

	if out, st := runGitStatus(t.TempDir(), "rev-parse", "--show-toplevel"); st != gitAnswered || out != "" {
		t.Fatalf("exit 128: got (%q, %v), want (\"\", gitAnswered)", out, st)
	}
}

// TestRunGitStatus_ClassifiesAnsweredVsUnavailable pins the classification
// itself against real git: a non-zero exit is an ANSWER (reproducible, about
// the repo), while a missing binary means git never spoke.
func TestRunGitStatus_ClassifiesAnsweredVsUnavailable(t *testing.T) {
	generousDeadline(t)
	dir := t.TempDir() // no .git anywhere under TempDir

	if out, st := runGitStatus(dir, "rev-parse", "--show-toplevel"); st != gitAnswered || out != "" {
		t.Fatalf("non-repo: got (%q, %v), want (\"\", gitAnswered)", out, st)
	}
	t.Setenv("PATH", "")
	if out, st := runGitStatus(dir, "rev-parse", "--show-toplevel"); st != gitUnavailable || out != "" {
		t.Fatalf("git off PATH: got (%q, %v), want (\"\", gitUnavailable)", out, st)
	}
}
