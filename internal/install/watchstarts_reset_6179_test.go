package install

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

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

	// Plain t.TempDir (#6188). This test used to own its repo directory via
	// os.MkdirTemp plus a retrying cleanup, on the stated grounds that "Apply
	// creates <repo>/.grafel/logs shortly AFTER it returns". It does not: no Go
	// code in this tree creates that path at all. The only occurrence is the
	// string interpolated into the launchd plist's StandardOutPath /
	// StandardErrorPath (watchers.LaunchdPlist), which launchd itself
	// materialises when it spawns the job — and this test stubs both
	// newWatcherLoader and the launchctl runner, so no launchd is reached. The
	// TestApply_DoesNotCreateRepoLogDir case below pins that directly.
	repo := filepath.Join(t.TempDir(), "app")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	recPath := seedWatchStarts(t, repo)

	// Belt AND braces. Swapping newWatcherLoader is what keeps Apply away from
	// launchctl; stubbing the runner is what keeps a future refactor that
	// reintroduces a direct watchers.NewLoader() call from silently leaking a
	// launchd job again. This test issued a real bootout+bootstrap into the
	// developer's session on every run until #6183 (see stubLaunchctlRunner).
	stubLaunchctlRunner(t)

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

// TestApply_DoesNotCreateRepoLogDir refutes #6188's premise deterministically.
//
// #6188 claimed Apply creates <repo>/.grafel/logs asynchronously, some poll
// iterations after it returns. Apply is fully synchronous — it starts no
// goroutine — and no Go code in this tree creates that directory. The path
// exists only as the StandardOutPath/StandardErrorPath strings in the launchd
// plist (watchers.LaunchdPlist), so on darwin it is launchd, a separate
// process, that materialises the parents when it spawns the bootstrapped job.
// With newWatcherLoader and the service-call runner both stubbed, no launchd is
// reached, so nothing can create it — before OR after Apply returns.
//
// The assertion is a single stat taken the instant Apply returns, which is
// deterministic where polling for an absence never can be. It is guarded
// against vacuity two ways: the checked path is taken from the plist the
// production renderer emits for this very unit (not hand-written here), and the
// parent <repo>/.grafel is asserted to exist, so a miss cannot be explained by
// stat-ing into a tree that was never there.
func TestApply_DoesNotCreateRepoLogDir(t *testing.T) {
	home := reconcileSandbox(t)
	bin := filepath.Join(home, "bin", "grafel")

	repo := filepath.Join(t.TempDir(), "app")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	seedWatchStarts(t, repo) // creates <repo>/.grafel

	stubLaunchctlRunner(t)
	fake := &fakeLoader{}
	prev := newWatcherLoader
	newWatcherLoader = func() watchers.Loader { return fake }
	t.Cleanup(func() { newWatcherLoader = prev })

	// Take the log directory from the renderer, so this test cannot drift onto
	// a path production stopped using.
	unit := watchers.Unit{Group: "g", Repo: repo, BinPath: bin}
	logDir := path.Join(repo, ".grafel", "logs")
	plist := watchers.LaunchdPlist(unit)
	if !strings.Contains(plist, "<string>"+logDir+"/watcher.out.log</string>") {
		t.Fatalf("the plist no longer names %s; this test is watching the wrong path.\nplist:\n%s", logDir, plist)
	}

	cfg := &registry.GroupConfig{Name: "g", Repos: []registry.Repo{{Slug: "app", Path: repo}}}
	cfg.Features.Watchers = true

	res, err := Apply(Options{
		Group: "g", Config: cfg, BinPath: bin,
		SkipHooks: true, SkipMCP: true,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Non-vacuity: the watcher block must actually have RUN. A watchers.Write
	// failure inside Apply is non-fatal — it appends a WatcherWarning and
	// `continue`s, still returning a nil error — so without this the block
	// could be skipped entirely, nothing would ever have created the log
	// directory, and the assertion below would pass having observed nothing.
	if len(res.WatcherUnits) != 1 {
		t.Fatalf("the watcher block did not run: WatcherUnits = %v, warnings = %v; "+
			"the log-directory assertion below would be vacuous", res.WatcherUnits, res.WatcherWarnings)
	}

	// Non-vacuity: the parent must be there, so "logs is absent" is a real
	// observation about a live directory and not about a missing ancestor.
	if st, err := os.Stat(filepath.Join(repo, ".grafel")); err != nil || !st.IsDir() {
		t.Fatalf("<repo>/.grafel should exist (seedWatchStarts made it): stat err = %v", err)
	}
	if _, err := os.Stat(filepath.FromSlash(logDir)); !os.IsNotExist(err) {
		t.Errorf("Apply created %s (stat err = %v). It is not supposed to create this "+
			"directory at all: no Go code in this tree does, the path is only a string in "+
			"the launchd plist, and both the loader and the service-call runner are stubbed "+
			"here. If this fires, #6188's async-writer premise deserves a fresh look", logDir, err)
	}
}
