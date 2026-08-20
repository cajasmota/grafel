package watch

// pause_on_cold_6267_test.go — end-to-end proof for #6267 arm (b).
//
// The defect: a repo refused for want of descriptor budget stays unwatched for
// the life of the daemon. It retries (once per ~65-minute idle cycle) but every
// retry fails, because nothing ever returns descriptors: every incumbent's only
// slot is its default branch, which is pinned, so no incumbent can reach
// EXPIRED — and COLD→EXPIRED was the only transition that called Pause.
//
// The fix: WARM→COLD calls Pause again. This test drives the whole thing
// through the real tier.Manager, the real watch.DefaultManager and a real
// fsnotify-backed Watcher on a real (tiny) descriptor budget, and asserts on
// the outcome that matters — the refused repo actually subscribes and actually
// receives events — not on a counter.
//
// Note what is deliberately NOT asserted: which repos win the initial race.
// The heartbeat restart re-subscribes in map-iteration order, so a winner-set
// assertion would encode a non-deterministic outcome (#6267). Here the winner
// is forced by subscribing serially, and the assertions are about descriptors
// being returned and a previously-refused repo succeeding.

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/tier"
)

// seedRepo makes a flat directory of nFiles source files. Flat matters: the
// kqueue cost model charges one descriptor for the directory plus one per
// entry, so a flat tree has an exactly predictable cost.
func seedRepo(t *testing.T, nFiles int) string {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i < nFiles; i++ {
		p := filepath.Join(dir, "f"+string(rune('a'+i))+".go")
		if err := os.WriteFile(p, []byte("package main\n"), 0o644); err != nil {
			t.Fatalf("seed %s: %v", p, err)
		}
	}
	return dir
}

// testClock is a manually advanced clock for the tier manager.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// TestPauseOnCold_ReturnsBudgetSoARefusedRepoCanSubscribe is the #6267 arm (b)
// acceptance test.
func TestPauseOnCold_ReturnsBudgetSoARefusedRepoCanSubscribe(t *testing.T) {
	const nFiles = 3
	repoA := seedRepo(t, nFiles)
	repoB := seedRepo(t, nFiles)

	// Budget for exactly ONE repo, under the kqueue arithmetic on every
	// platform (the injected cost model is why this is not a darwin-only test).
	costOne := kqueueCostModel.cost(1, nFiles)

	rec := &sinkRecorder{}
	w, err := NewWatcherConfig(Config{
		Debounce:          25 * time.Millisecond,
		HeartbeatInterval: time.Hour, // no restart may reshuffle the ledger mid-test
		FDBudget:          costOne,
		fdCost:            kqueueCostModel,
	}, rec.sink, nil)
	if err != nil {
		t.Fatalf("NewWatcherConfig: %v", err)
	}
	t.Cleanup(w.Stop)

	mgr := NewDefaultManager(w, nil)
	clock := &testClock{now: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)}
	tm := tier.NewManagerForTest(tier.DefaultTTLConfig(), clock.Now,
		func(tier.SlotKey) {}, func(tier.SlotKey) error { return nil })
	tm.SetWatcherHook(mgr)

	keyA := tier.SlotKey{RepoPath: repoA, Ref: "main"}
	keyB := tier.SlotKey{RepoPath: repoB, Ref: "main"}
	// Pinned main is the whole scenario: such a slot never reaches EXPIRED.
	tm.RegisterCold(keyA, true, tier.SlotKindBranchMain)
	tm.RegisterCold(keyB, true, tier.SlotKindBranchMain)
	mgr.Register(repoA, "main")
	mgr.Register(repoB, "main")

	// --- 1. A is queried first and takes the whole budget. ------------------
	if err := tm.Touch(keyA); err != nil {
		t.Fatalf("Touch A: %v", err)
	}
	if got := w.Repos(); len(got) != 1 || got[0] != repoA {
		t.Fatalf("prereq: want repoA watched, got %v", got)
	}
	used, limit, _, _ := w.FDBudgetStats()
	if used != costOne || limit != costOne {
		t.Fatalf("prereq: want the budget fully committed to A (used=%d limit=%d), want %d/%d",
			used, limit, costOne, costOne)
	}

	// --- 2. B is refused for want of budget. --------------------------------
	if err := tm.Touch(keyB); err != nil {
		t.Fatalf("Touch B: %v", err)
	}
	if !mgr.IsPaused(repoB, "main") {
		t.Fatalf("refusal: B must remain paused after a refused AddRepo")
	}
	if got := w.Repos(); len(got) != 1 || got[0] != repoA {
		t.Fatalf("refusal: want only repoA watched, got %v", got)
	}
	_, _, unwatched, unwatchedRepos := w.FDBudgetStats()
	if unwatched != 1 || len(unwatchedRepos) != 1 || unwatchedRepos[0] != repoB {
		t.Fatalf("refusal: want repoB recorded unwatched, got %d %v", unwatched, unwatchedRepos)
	}
	// The graph reload succeeded, so the slot is HOT even though the
	// subscription failed — this is the state the daemon actually serves from.
	if got := tm.Get(keyB); got != tier.TierHot {
		t.Fatalf("refusal: want B HOT (reload succeeded, only the subscription failed), got %s", got)
	}

	// --- 3. Both go idle. A's WARM→COLD must return its descriptors. --------
	clock.advance(6 * time.Minute)
	tm.Scan()
	if got := tm.Get(keyA); got != tier.TierWarm {
		t.Fatalf("prereq: want A WARM, got %s", got)
	}
	usedWarm, _, _, _ := w.FDBudgetStats()
	if usedWarm != costOne {
		t.Fatalf("HOT→WARM must not release descriptors: used=%d want %d", usedWarm, costOne)
	}

	clock.advance(61 * time.Minute)
	tm.Scan()
	if got := tm.Get(keyA); got != tier.TierCold {
		t.Fatalf("prereq: want A COLD, got %s", got)
	}
	usedCold, _, _, _ := w.FDBudgetStats()
	if usedCold != 0 {
		t.Fatalf("#6267: WARM→COLD must return A's descriptors: used=%d want 0 (of %d)", usedCold, costOne)
	}
	if got := w.Repos(); len(got) != 0 {
		t.Fatalf("#6267: A's subscription must be gone after WARM→COLD, got %v", got)
	}

	// --- 4. B's next attempt succeeds. --------------------------------------
	// B went COLD in the same scan, so the next query is a cold wake — the
	// production retry path.
	baseline := rec.count()
	if err := tm.Touch(keyB); err != nil {
		t.Fatalf("Touch B (retry): %v", err)
	}
	if mgr.IsPaused(repoB, "main") {
		t.Fatalf("#6267: B must be active after re-subscribing")
	}
	if got := w.Repos(); len(got) != 1 || got[0] != repoB {
		t.Fatalf("#6267: want repoB watched after A released its budget, got %v", got)
	}
	usedB, _, unwatchedB, _ := w.FDBudgetStats()
	if usedB != costOne {
		t.Fatalf("retry: want B holding the budget (used=%d), want %d", usedB, costOne)
	}
	if unwatchedB != 0 {
		t.Fatalf("retry: want repoB cleared from the unwatched set, got %d", unwatchedB)
	}

	// --- 5. …and it is a live subscription, not just bookkeeping. -----------
	// An edit under repoB must reach the sink as a non-bulk event. This is the
	// claim the whole issue is about: "edits there will not re-index".
	if err := os.WriteFile(filepath.Join(repoB, "fa.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("edit under repoB: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		for _, c := range rec.snapshot() {
			if c.repo == repoB && !c.bulk {
				return // event delivered: the repo is genuinely watched again
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("#6267: no fsnotify-driven sink call for repoB within 10s (baseline %d, calls %+v)",
				baseline, rec.snapshot())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestPauseOfUnregisteredSlotLeavesSiblingSubscriptionIntact guards the
// regression that #6267's WARM→COLD Pause exposed.
//
// The tier keyspace and this manager's keyspace are not required to agree:
// tier.Touch auto-registers an unknown (repo, ref) as HOT without calling
// Resume, so a ref that was queried but never indexed in this process has a
// tier slot and NO watch slot. Before #6267 such a slot only reached Pause via
// COLD→EXPIRED (non-pinned refs, after the 48h window); now it reaches it every
// idle cycle. If Pause decrements the repo-level refcount for it, the count
// hits 0 and RemoveRepo kills the SIBLING ref's live subscription — while the
// sibling's own paused flag stays false, so every later Resume takes the
// "already active" early return and never retries AddRepo. Permanently
// unwatched and permanently reported as watched: the #6267 failure mode itself.
func TestPauseOfUnregisteredSlotLeavesSiblingSubscriptionIntact(t *testing.T) {
	repo := seedRepo(t, 2)

	rec := &sinkRecorder{}
	w, err := NewWatcherConfig(Config{
		Debounce:          time.Hour,
		HeartbeatInterval: time.Hour,
	}, rec.sink, nil)
	if err != nil {
		t.Fatalf("NewWatcherConfig: %v", err)
	}
	t.Cleanup(w.Stop)

	mgr := NewDefaultManager(w, nil)
	clock := &testClock{now: time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC)}
	tm := tier.NewManagerForTest(tier.DefaultTTLConfig(), clock.Now,
		func(tier.SlotKey) {}, func(tier.SlotKey) error { return nil })
	tm.SetWatcherHook(mgr)

	// The production pair after an index completes: declare the slot, then
	// subscribe it.
	mgr.Register(repo, "main")
	mgr.Resume(repo, "main")
	keyMain := tier.SlotKey{RepoPath: repo, Ref: "main"}
	tm.Register(keyMain, true, tier.SlotKindBranchMain)
	if got := w.Repos(); len(got) != 1 || got[0] != repo {
		t.Fatalf("prereq: want repo watched, got %v", got)
	}

	// A ref that is queried but never indexed here: tier knows it, this
	// manager does not.
	keyFeat := tier.SlotKey{RepoPath: repo, Ref: "feat/x"}
	if err := tm.Touch(keyFeat); err != nil {
		t.Fatalf("Touch feat: %v", err)
	}

	// Age feat past HOT→WARM→COLD while main keeps being queried.
	clock.advance(6 * time.Minute)
	_ = tm.Touch(keyMain)
	tm.Scan()
	clock.advance(61 * time.Minute)
	_ = tm.Touch(keyMain)
	tm.Scan()
	if got := tm.Get(keyFeat); got != tier.TierCold {
		t.Fatalf("prereq: want feat COLD, got %s", got)
	}
	if got := tm.Get(keyMain); got == tier.TierCold {
		t.Fatalf("prereq: main must still be live, got %s", got)
	}

	// main is still an active ref, so its subscription must survive feat's
	// eviction.
	if got := w.Repos(); len(got) != 1 || got[0] != repo {
		t.Fatalf("pausing an unregistered ref killed the sibling's subscription: w.Repos()=%v", got)
	}
	if mgr.IsPaused(repo, "main") {
		t.Fatalf("main must still be reported active")
	}

	// And the ledger must still be honest: main can be paused and resumed,
	// which is impossible if its refcount was already spent by feat.
	mgr.Pause(repo, "main")
	if got := w.Repos(); len(got) != 0 {
		t.Fatalf("refcount corrupted: pausing the last active ref did not unsubscribe, got %v", got)
	}
	mgr.Resume(repo, "main")
	if got := w.Repos(); len(got) != 1 || got[0] != repo {
		t.Fatalf("refcount corrupted: resume after pause did not re-subscribe, got %v", got)
	}
}
