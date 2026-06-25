package watch

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSkipPath_BuildArtifacts5392 is the #5392 regression guard for the
// static event-boundary filter: build/output artifact dirs and mobile
// binary outputs must be dropped before they can arm a reindex, while
// real source paths still pass through.
func TestSkipPath_BuildArtifacts5392(t *testing.T) {
	cases := map[string]bool{
		// build/output dirs (the AAB/gradle churn that caused the incident)
		"/repo/AAB/app-release.aab":              true,
		"/repo/android/app/build/outputs/x.dex":  true,
		"/repo/.gradle/8.0/fileHashes/hash.bin":  true,
		"/repo/app/.dart_tool/package_config.js": true,
		"/repo/dist/bundle.js":                   true,
		"/repo/out/main.js":                      true,
		"/repo/target/debug/app":                 true,
		"/repo/node_modules/react/index.js":      true,
		"/repo/.next/static/chunk.js":            true,
		"/repo/bin/tool":                         true,
		"/repo/obj/Debug/app.dll":                true,
		"/repo/ios/Pods/Manifest.lock":           true,
		// mobile binary outputs by extension
		"/repo/build/app-release.aab": true,
		"/repo/build/app-release.apk": true,
		"/repo/build/MyApp.ipa":       true,
		"/repo/build/lib.aar":         true,
		// generated-file globs
		"/repo/src/schema.generated.ts": true,
		"/repo/lib/models.g.dart":       true,
		"/repo/lib/user.freezed.dart":   true,
		"/repo/api/service.pb.go":       true,
		// real source — must NOT be skipped
		"/repo/src/index.ts":      false,
		"/repo/internal/cli/a.go": false,
		"/repo/lib/main.dart":     false,
		"/repo/app/src/User.kt":   false,
	}
	for p, wantSkip := range cases {
		if got := ShouldSkipPath(p); got != wantSkip {
			t.Errorf("ShouldSkipPath(%q) = %v, want %v", p, got, wantSkip)
		}
	}
}

// TestSkipPathForRepo_Gitignore5392 verifies that the repo-aware
// event-boundary filter drops events under a gitignored path even when
// the path is NOT a well-known static artifact name (the general case
// that motivated honouring .gitignore at the event boundary).
func TestSkipPathForRepo_Gitignore5392(t *testing.T) {
	repo := t.TempDir()
	// Gitignore an arbitrarily-named output dir + a bespoke artifact glob
	// that the static lists do not know about.
	gi := "secret-build/\n*.myartifact\n"
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(gi), 0o644); err != nil {
		t.Fatal(err)
	}
	evictRepoIgnoreState(repo)

	mustSkip := []string{
		filepath.Join(repo, "secret-build", "out.bin"),
		filepath.Join(repo, "thing.myartifact"),
	}
	for _, p := range mustSkip {
		if !ShouldSkipPathForRepo(repo, p) {
			t.Errorf("ShouldSkipPathForRepo(%q) = false, want true (gitignored)", p)
		}
	}

	mustPass := []string{
		filepath.Join(repo, "src", "main.go"),
		filepath.Join(repo, "README.md"),
	}
	for _, p := range mustPass {
		if ShouldSkipPathForRepo(repo, p) {
			t.Errorf("ShouldSkipPathForRepo(%q) = true, want false (real source)", p)
		}
	}

	// Path outside the repo degrades to the static filter (no panic, no
	// false skip for a real source path).
	if ShouldSkipPathForRepo(repo, "/elsewhere/main.go") {
		t.Errorf("expected out-of-repo source path not to be skipped")
	}
}

// TestExtraSkipDirsEnv5392 verifies the GRAFEL_WATCH_EXTRA_SKIP_DIRS
// ops escape hatch. Because the parse is sync.Once-guarded we set the
// env before the first call in this binary; if another test already
// triggered the parse this asserts the no-op default behaviour instead.
func TestExtraSkipDirsEnv5392(t *testing.T) {
	t.Setenv("GRAFEL_WATCH_EXTRA_SKIP_DIRS", "myoutdir, another ")
	// Reset the once so this test deterministically observes the env
	// regardless of whether another test already triggered the parse.
	envExtraSkipOnce = sync.Once{}
	got := extraSkipDirsFromEnv()
	if _, ok := got["myoutdir"]; !ok {
		t.Errorf("expected myoutdir in env extra-skip set, got %v", got)
	}
	if _, ok := got["another"]; !ok {
		t.Errorf("expected 'another' (trimmed) in env extra-skip set, got %v", got)
	}
	if !ShouldSkipDir("myoutdir") {
		t.Errorf("ShouldSkipDir(myoutdir) = false, want true via env")
	}
}

// TestCoalesce_RapidWrites5392 is the per-repo coalesce guard: a burst of
// N rapid writes to a single repo within the debounce window collapses to
// at most ONE sink invocation (one reindex), not N.
//
// Deterministic via the injected fake clock (no wall-clock dependence): the
// debounce timer only fires when the test ADVANCES the clock, so all 20
// writes are guaranteed to land inside a single debounce window regardless of
// how slowly CI delivers the fsnotify events. We wait for the events to be
// observed by polling the watcher's event counter (a guarded predicate, not a
// sleep), then advance time once past the window — exactly one coalesced call.
func TestCoalesce_RapidWrites5392(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	repo := t.TempDir()
	src := filepath.Join(repo, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	evictRepoIgnoreState(repo)

	const debounce = 200 * time.Millisecond
	fc := newManualClock()

	var calls atomic.Int32
	w, err := newTestWatcher(debounce, func(string, bool) {
		calls.Add(1)
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	// Install the fake clock BEFORE subscribing the repo so no event is ever
	// armed against the real clock. Safe: no events flow until AddRepo runs.
	w.clk = fc
	defer w.Stop()
	if _, err := w.AddRepo(repo); err != nil {
		t.Fatalf("add: %v", err)
	}

	// 20 rapid writes. The fake clock does not advance, so every armed
	// debounce reset stays inside one window — coalescing is forced.
	for i := 0; i < 20; i++ {
		f := filepath.Join(src, "f"+itoa5392(i)+".go")
		if err := os.WriteFile(f, []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Wait until all 20 write events have been observed by the watcher. This
	// is the only real-time wait, and it polls a guarded counter rather than
	// asserting on a fixed sleep, so slow CI just waits a little longer — it
	// can never split the batch (the clock has not advanced).
	waitForEvents5392(t, w, 20, 5*time.Second)

	// Now advance the clock once past the debounce window. The single pending
	// debounce timer fires exactly once, on this goroutine, before Advance
	// returns — so the assertion below is race-free.
	fc.Advance(debounce + time.Millisecond)

	if n := calls.Load(); n != 1 {
		t.Errorf("expected exactly 1 coalesced sink call for 20 rapid writes, got %d", n)
	}
}

// waitForEvents5392 blocks until the watcher has processed at least n total
// fs events, or fails the test on timeout. Polls a guarded counter — never a
// fixed sleep — so it is robust to slow event delivery without coupling the
// outcome to wall-clock timing.
func waitForEvents5392(t *testing.T, w *Watcher, n uint64, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if _, _, ev, _ := w.Stats(); ev >= n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	if _, _, ev, _ := w.Stats(); ev < n {
		t.Fatalf("watcher observed %d events, want ≥ %d within %s", ev, n, d)
	}
}

func itoa5392(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
