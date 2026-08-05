package install

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/install/watchers"
	"github.com/cajasmota/grafel/internal/registry"
)

// seedWatchStarts writes an over-threshold-looking start history for repo.
func seedWatchStarts(t *testing.T, repo string) string {
	t.Helper()
	p := watchers.WatchStartsPath(repo)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"starts":["2026-01-01T00:00:00Z"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestReconcileWatcherUnits_ResetsWatchStartsOnReRegistration pins #6179 F4-a
// on the reconcile path.
//
// The crash-loop detector counts starts, and a bootstrap is a start. If
// re-registering a unit does not clear the history, a watcher that gave up can
// never come back inside the counting window — the act of bringing it back
// counts against it. Registration is an explicit "I want this running", so it
// must reset.
func TestReconcileWatcherUnits_ResetsWatchStartsOnReRegistration(t *testing.T) {
	home := reconcileSandbox(t)
	bin := filepath.Join(home, "bin", "grafel")

	stale := filepath.Join(home, "repos", "stale")
	current := filepath.Join(home, "repos", "current")
	registerGroup(t, home, "g", []string{stale, current}, true)

	staleUnit := watchers.Unit{Group: "g", Repo: stale, BinPath: bin}
	currentUnit := watchers.Unit{Group: "g", Repo: current, BinPath: bin}
	writeUnitFile(t, staleUnit, "OLD UNIT BODY\n")
	writeUnitFile(t, currentUnit, watchers.Render(currentUnit))

	stalePath := seedWatchStarts(t, stale)
	currentPath := seedWatchStarts(t, current)

	fake := &fakeLoader{}
	prev := newWatcherLoader
	newWatcherLoader = func() watchers.Loader { return fake }
	t.Cleanup(func() { newWatcherLoader = prev })

	if _, err := ReconcileWatcherUnits(ReconcileWatcherOptions{BinPath: bin}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Errorf("re-registered unit kept its start history (stat err = %v); the bootstrap "+
			"this triggers is itself a counted start, so the watcher would give up again "+
			"immediately (#6179 F4-a)", err)
	}
	// A unit that was NOT re-registered must keep its history: nothing about
	// it changed, and clearing it would quietly disarm the detector on every
	// upgrade for every repo.
	if _, err := os.Stat(currentPath); err != nil {
		t.Errorf("an untouched unit lost its start history (%v); only re-registration resets", err)
	}
}

// TestApply_ResetsWatchStartsWhenRegisteringUnit pins the same property on the
// path a human actually reaches: `grafel install` / `grafel group add`. This is
// the remedy the detector's own message points at, so it has to work.
func TestApply_ResetsWatchStartsWhenRegisteringUnit(t *testing.T) {
	home := reconcileSandbox(t)
	bin := filepath.Join(home, "bin", "grafel")

	// The repo deliberately does NOT live under a t.TempDir. Apply creates
	// <repo>/.grafel/logs shortly AFTER it returns — observed 1-13 poll
	// iterations later, so something in that path writes asynchronously — and
	// t.TempDir's own RemoveAll then races it and fails the test with
	// "directory not empty" roughly one run in three. That async write is
	// pre-existing and outside #6179's scope; owning the directory here keeps
	// this test measuring what it claims to measure instead of inheriting an
	// unrelated flake.
	repoRoot, err := os.MkdirTemp("", "grafel-6179-apply-")
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(repoRoot, "app")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// Best-effort, and retried: the same async writer can lose us a race
		// here too, and a cleanup failure must not fail a passing test.
		for i := 0; i < 3; i++ {
			if err := os.RemoveAll(repoRoot); err == nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	})
	recPath := seedWatchStarts(t, repo)

	fake := &fakeLoader{}
	prev := newWatcherLoader
	newWatcherLoader = func() watchers.Loader { return fake }
	t.Cleanup(func() { newWatcherLoader = prev })

	cfg := &registry.GroupConfig{Name: "g", Repos: []registry.Repo{{Slug: "app", Path: repo}}}
	cfg.Features.Watchers = true

	if _, err := Apply(Options{
		Group: "g", Config: cfg, BinPath: bin,
		SkipHooks: true, SkipMCP: true,
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if _, err := os.Stat(recPath); !os.IsNotExist(err) {
		t.Errorf("`grafel install` did not clear the watcher start history (stat err = %v). "+
			"The crash-loop detector tells the user to re-run install; if install does not "+
			"reset the count, that instruction is false (#6179 F4-a)", err)
	}
}
