package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Fixture helper + its own premise assertion.
//
// Fixture vacuity is the failure mode these tests are written against: a tree
// that does not actually contain the dirs/files the assertions assume would
// make every budget assertion true for the wrong reason. countTree walks the
// fixture INDEPENDENTLY of any production code and is asserted against the
// numbers each test does arithmetic with.
// ---------------------------------------------------------------------------

// makeTree builds root/ with nFiles files, plus root/src/ with nFiles files.
// Returns the tree root. Names are deliberately mundane so no skip layer
// (SkipDirs, ExcludeDirs, .gitignore) removes anything.
func makeTree(t *testing.T, nFiles int) string {
	t.Helper()
	root := t.TempDir()
	sub := filepath.Join(root, "src")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, d := range []string{root, sub} {
		for i := 0; i < nFiles; i++ {
			p := filepath.Join(d, "f"+string(rune('a'+i))+".go")
			if err := os.WriteFile(p, []byte("package p\n"), 0o644); err != nil {
				t.Fatalf("write %s: %v", p, err)
			}
		}
	}
	return root
}

// countTree counts directories and regular files under root, using nothing
// from the production watcher. This is the premise oracle.
func countTree(t *testing.T, root string) (dirs, files int) {
	t.Helper()
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			dirs++
		} else {
			files++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("countTree: %v", err)
	}
	return dirs, files
}

func TestMakeTreePremise(t *testing.T) {
	root := makeTree(t, 4)
	dirs, files := countTree(t, root)
	if dirs != 2 {
		t.Fatalf("fixture premise broken: want 2 dirs (root + src), got %d", dirs)
	}
	if files != 8 {
		t.Fatalf("fixture premise broken: want 8 files (4 per dir), got %d", files)
	}
}

// ---------------------------------------------------------------------------
// The cost model.
// ---------------------------------------------------------------------------

// TestKqueueCostModelChargesPerFile pins the arithmetic that the whole fix
// rests on: on kqueue a watched directory costs one descriptor for itself AND
// one for every file inside it (fsnotify's watchDirectoryFiles). The inotify
// model must NOT charge per file — asserted here so a copy-paste that made
// both models identical is caught.
func TestKqueueCostModelChargesPerFile(t *testing.T) {
	if got := kqueueCostModel.cost(3, 100); got != 103 {
		t.Fatalf("kqueue cost(3 dirs, 100 files) = %d, want 103", got)
	}
	if got := inotifyCostModel.cost(3, 100); got != 3 {
		t.Fatalf("inotify cost(3 dirs, 100 files) = %d, want 3 (per-watch, not per-fd)", got)
	}
	if kqueueCostModel == inotifyCostModel {
		t.Fatal("premise broken: the two cost models are identical, so gating is vacuous")
	}
}

// TestDefaultCostModelIsPlatformGated asserts the platform selection, not the
// arithmetic. Build-tagged constants (not runtime.GOOS + t.Skip) decide, so
// this compiles and runs on every platform (#6218).
func TestDefaultCostModelIsPlatformGated(t *testing.T) {
	got := defaultCostModel()
	if fdDescriptorPerFile {
		if got != kqueueCostModel {
			t.Fatalf("on a per-file-descriptor platform want kqueue model, got %+v", got)
		}
	} else {
		if got != inotifyCostModel {
			t.Fatalf("on a per-watch platform want inotify model, got %+v", got)
		}
	}
}

// ---------------------------------------------------------------------------
// The budget itself.
// ---------------------------------------------------------------------------

func TestFDBudgetReserveRefusesOverLimit(t *testing.T) {
	b := newFDBudget(10)

	if !b.reserve(6) {
		t.Fatal("reserve(6) against a limit of 10 was refused")
	}
	// Premise: the budget genuinely read the injected limit and genuinely
	// recorded the reservation. If used stayed 0 the next assertion would
	// pass vacuously.
	if used, limit := b.snapshot(); used != 6 || limit != 10 {
		t.Fatalf("after reserve(6): used=%d limit=%d, want 6/10", used, limit)
	}

	if b.reserve(5) {
		t.Fatal("reserve(5) on top of 6/10 was granted — budget not enforced")
	}
	if used, _ := b.snapshot(); used != 6 {
		t.Fatalf("refused reservation must not consume: used=%d, want 6", used)
	}

	if !b.reserve(4) {
		t.Fatal("reserve(4) to exactly fill 10 was refused — off-by-one")
	}
	if used, _ := b.snapshot(); used != 10 {
		t.Fatalf("used=%d, want 10", used)
	}

	b.release(10)
	if used, _ := b.snapshot(); used != 0 {
		t.Fatalf("after release: used=%d, want 0", used)
	}
}

func TestFDBudgetZeroLimitIsUnlimited(t *testing.T) {
	b := newFDBudget(0)
	if !b.reserve(1 << 20) {
		t.Fatal("a zero (disabled) budget refused a reservation")
	}
	if _, limit := b.snapshot(); limit != 0 {
		t.Fatalf("limit=%d, want 0 (disabled)", limit)
	}
}

// ---------------------------------------------------------------------------
// End-to-end: the watcher must refuse a subscription it cannot afford.
// ---------------------------------------------------------------------------

func newBudgetedWatcher(t *testing.T, budget int) *Watcher {
	t.Helper()
	return newBudgetedWatcherCfg(t, Config{FDBudget: budget})
}

// newBudgetedWatcherCfg is newBudgetedWatcher with extra Config fields. Only
// FDBudget and the fields the caller sets differ; the ledger arithmetic and the
// timings are the same in both.
func newBudgetedWatcherCfg(t *testing.T, cfg Config) *Watcher {
	t.Helper()
	cfg.Debounce = time.Hour
	cfg.BulkThreshold = 10000
	cfg.HeartbeatInterval = time.Hour
	cfg.fdCost = kqueueCostModel // test the macOS arithmetic everywhere
	w, err := NewWatcherConfig(cfg, func(string, bool) {}, nil)
	if err != nil {
		t.Fatalf("NewWatcherConfig: %v", err)
	}
	t.Cleanup(w.Stop)
	return w
}

// TestSubscribeSucceedsWhenBudgetCoversDirsAndFiles is the positive premise
// for the refusal test below: with a budget of EXACTLY dirs+files the repo is
// accepted and every directory is subscribed. If this failed, the refusal test
// would be proving nothing about the budget.
func TestSubscribeSucceedsWhenBudgetCoversDirsAndFiles(t *testing.T) {
	root := makeTree(t, 4)
	dirs, files := countTree(t, root)
	w := newBudgetedWatcher(t, dirs+files)

	added, err := w.AddRepo(root)
	if err != nil {
		t.Fatalf("AddRepo with an exactly-sufficient budget failed: %v", err)
	}
	if added != dirs {
		t.Fatalf("subscribed %d dirs, want %d", added, dirs)
	}
	used, limit := w.fdb.snapshot()
	if limit != dirs+files {
		t.Fatalf("watcher budget limit=%d, want the injected %d", limit, dirs+files)
	}
	if used != dirs+files {
		t.Fatalf("charged %d descriptors, want dirs+files=%d — the estimate is not counting files", used, dirs+files)
	}
	if _, _, _, _, unwatched := w.Stats(); unwatched != 0 {
		t.Fatalf("unwatched=%d on a successful subscription, want 0", unwatched)
	}
}

// TestSubscribeRefusedWhenBudgetCoversOnlyDirs is the defect. Today the cap
// counts directories only, so a budget large enough for every DIRECTORY but
// far too small for the per-file descriptors kqueue also opens is accepted —
// and the process walks on toward EMFILE. With the fix the subscription is
// refused.
func TestSubscribeRefusedWhenBudgetCoversOnlyDirs(t *testing.T) {
	root := makeTree(t, 4)
	dirs, files := countTree(t, root)
	if files == 0 {
		t.Fatal("fixture premise broken: no files, so dir-only accounting is indistinguishable")
	}
	// Enough for every directory, one short of the true dirs+files cost.
	budget := dirs + files - 1
	if budget < dirs {
		t.Fatalf("premise broken: budget %d cannot even cover %d dirs", budget, dirs)
	}
	w := newBudgetedWatcher(t, budget)

	added, err := w.AddRepo(root)
	if err == nil {
		t.Fatalf("AddRepo was accepted with budget=%d for a tree costing %d descriptors (added %d dirs)",
			budget, dirs+files, added)
	}
	if !isFDBudgetError(err) {
		t.Fatalf("AddRepo failed with %v, want an FD-budget refusal", err)
	}
	if added != 0 {
		t.Fatalf("a refused subscription reported %d dirs added, want 0", added)
	}
}

// TestRefusalUnwindsEveryDescriptor: a refusal must leave nothing behind, or
// the descriptors it opened before giving up are leaked for the process
// lifetime and the budget slowly poisons itself.
func TestRefusalUnwindsEveryDescriptor(t *testing.T) {
	root := makeTree(t, 4)
	dirs, files := countTree(t, root)
	w := newBudgetedWatcher(t, dirs+files-1)

	if _, err := w.AddRepo(root); err == nil {
		t.Fatal("premise broken: AddRepo was not refused")
	}
	if used, _ := w.fdb.snapshot(); used != 0 {
		t.Fatalf("after a refusal the budget still holds %d descriptors, want 0", used)
	}
	w.mu.Lock()
	nDirs := len(w.dirToRepo)
	_, stillRegistered := w.repos[root]
	w.mu.Unlock()
	if nDirs != 0 {
		t.Fatalf("refusal left %d subscribed dirs behind, want 0", nDirs)
	}
	if stillRegistered {
		t.Fatal("refused repo is still in the repos map — it would look watched but receive nothing")
	}
}

// TestRefusalIsVisibleInStats: silently not watching is the failure mode this
// codebase keeps producing. A budget-refused repo must be countable.
func TestRefusalIsVisibleInStats(t *testing.T) {
	root := makeTree(t, 4)
	dirs, files := countTree(t, root)
	w := newBudgetedWatcher(t, dirs+files-1)

	if _, err := w.AddRepo(root); err == nil {
		t.Fatal("premise broken: AddRepo was not refused")
	}
	repos, _, _, _, unwatched := w.Stats()
	if repos != 0 {
		t.Fatalf("repos=%d, want 0 — a refused repo must not be counted as watched", repos)
	}
	if unwatched != 1 {
		t.Fatalf("Stats unwatched=%d, want 1", unwatched)
	}
	used, limit, un, paths := w.FDBudgetStats()
	if limit != dirs+files-1 {
		t.Fatalf("FDBudgetStats limit=%d, want the injected %d", limit, dirs+files-1)
	}
	if used != 0 {
		t.Fatalf("FDBudgetStats used=%d, want 0", used)
	}
	if un != 1 || len(paths) != 1 || paths[0] != root {
		t.Fatalf("FDBudgetStats unwatched=%d paths=%v, want 1 / [%s]", un, paths, root)
	}
}

// TestRemoveRepoReleasesBudget: without release, a store that churns repos
// would exhaust the budget permanently even though no descriptors are held.
func TestRemoveRepoReleasesBudget(t *testing.T) {
	a := makeTree(t, 4)
	b := makeTree(t, 4)
	dirs, files := countTree(t, a)
	// Room for exactly one of the two trees.
	w := newBudgetedWatcher(t, dirs+files)

	if _, err := w.AddRepo(a); err != nil {
		t.Fatalf("premise broken: first AddRepo failed: %v", err)
	}
	if _, err := w.AddRepo(b); err == nil {
		t.Fatal("premise broken: second AddRepo fitted in a budget sized for one tree")
	}

	w.RemoveRepo(a)
	if used, _ := w.fdb.snapshot(); used != 0 {
		t.Fatalf("RemoveRepo left %d descriptors reserved, want 0", used)
	}
	if _, err := w.AddRepo(b); err != nil {
		t.Fatalf("AddRepo after RemoveRepo freed the budget still failed: %v", err)
	}
	if _, _, _, _, unwatched := w.Stats(); unwatched != 0 {
		t.Fatalf("unwatched=%d after the repo was successfully re-admitted, want 0", unwatched)
	}
}

// TestBudgetIsGlobalAcrossRepos: the cap must be a single global ledger, not a
// per-repo one. Two trees that each fit individually must not both fit.
func TestBudgetIsGlobalAcrossRepos(t *testing.T) {
	a := makeTree(t, 4)
	b := makeTree(t, 4)
	dirs, files := countTree(t, a)
	one := dirs + files
	w := newBudgetedWatcher(t, one+1) // room for one tree and one spare fd

	if _, err := w.AddRepo(a); err != nil {
		t.Fatalf("premise broken: first tree did not fit in a budget of %d: %v", one+1, err)
	}
	if _, err := w.AddRepo(b); err == nil {
		t.Fatal("both trees fitted a budget sized for one — the budget is not global")
	}
	if _, _, _, _, unwatched := w.Stats(); unwatched != 1 {
		t.Fatalf("unwatched=%d, want 1", unwatched)
	}
}

// TestDefaultBudgetLimitIsPlatformGated: on platforms where a watch does not
// cost a descriptor per file, grafel must not impose a macOS-shaped ceiling.
func TestDefaultBudgetLimitIsPlatformGated(t *testing.T) {
	t.Setenv("GRAFEL_WATCH_FD_BUDGET", "")
	got := defaultFDBudgetLimit()
	if fdDescriptorPerFile {
		if got <= 0 {
			t.Fatalf("defaultFDBudgetLimit()=%d on a per-file-descriptor platform, want a positive ceiling", got)
		}
	} else if got != 0 {
		t.Fatalf("defaultFDBudgetLimit()=%d on a per-watch platform, want 0 (accounting disabled)", got)
	}
}

func TestFDBudgetEnvOverride(t *testing.T) {
	t.Setenv("GRAFEL_WATCH_FD_BUDGET", "4096")
	if got := defaultFDBudgetLimit(); got != 4096 {
		t.Fatalf("GRAFEL_WATCH_FD_BUDGET=4096 gave %d", got)
	}
	t.Setenv("GRAFEL_WATCH_FD_BUDGET", "0")
	if got := defaultFDBudgetLimit(); got != 0 {
		t.Fatalf("GRAFEL_WATCH_FD_BUDGET=0 gave %d, want 0 (disabled)", got)
	}
}
