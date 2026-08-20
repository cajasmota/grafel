package tier_test

// tier_watcher_test.go — integration tests for PH2a watcher pause/resume
// driven by tier transitions. PH2a of epic #2087 (#2096).
//
// #2645 deferred Pause to COLD→EXPIRED so the subscription outlived the COLD
// window. #6267 reverses that: WARM→COLD fires Pause again, because it is the
// only mechanism that returns file descriptors at scale (~10k per idle repo).
// Without it a repo refused for want of descriptor budget can never subscribe —
// every incumbent is default-branch-pinned, so no incumbent ever reaches
// EXPIRED and nothing else calls Pause. The accepted trade is #2645's symptom
// reduced from "the edit is never detected" to "the edit is detected one query
// late", via the resume-side rescan (which is asynchronous by design).

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/tier"
)

// fakeWatcherHook records Pause/Resume calls for assertion.
type fakeWatcherHook struct {
	mu      sync.Mutex
	paused  []string // "repoPath@ref" entries in order
	resumed []string
}

func (f *fakeWatcherHook) Pause(repoPath, ref string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paused = append(f.paused, repoPath+"@"+ref)
}

func (f *fakeWatcherHook) Resume(repoPath, ref string) time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resumed = append(f.resumed, repoPath+"@"+ref)
	return time.Microsecond // synthetic latency for logging
}

func (f *fakeWatcherHook) pausedAt(i int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i >= len(f.paused) {
		return ""
	}
	return f.paused[i]
}

func (f *fakeWatcherHook) pausedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.paused)
}

func (f *fakeWatcherHook) resumedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.resumed)
}

// ---------------------------------------------------------------------------
// Test: WARM→COLD fires Pause (#6267)
//
// #2645 removed this call so the subscription outlived the COLD window. The
// consequence, measured in #6267: descriptors are never returned, so a repo
// refused for budget retries once per ~65-minute idle cycle and fails every
// time for the life of the daemon. Pause on WARM→COLD is what returns them.
// ---------------------------------------------------------------------------

func TestWatcherPausedOnCold(t *testing.T) {
	clock, advance := makeClock()
	hook := &fakeWatcherHook{}
	var evictCount atomic.Int32

	m := tier.NewManagerForTest(tier.DefaultTTLConfig(), clock,
		func(k tier.SlotKey) { evictCount.Add(1) },
		noopReload,
	)
	m.SetWatcherHook(hook)

	// Pinned-main, which is the case #6267 is about: such a slot can go COLD
	// but never EXPIRED, so COLD→EXPIRED could never release its descriptors.
	key := tier.SlotKey{RepoPath: "/repo/ph2a", Ref: "main"}
	m.Register(key, true, tier.SlotKindBranchMain)

	// Drive HOT → WARM → COLD.
	advance(6 * time.Minute)
	m.Scan()
	if got := m.Get(key); got != tier.TierWarm {
		t.Fatalf("prereq: want WARM, got %s", got)
	}
	if hook.pausedCount() != 0 {
		t.Fatalf("Pause must not fire on HOT→WARM; got %d", hook.pausedCount())
	}

	advance(61 * time.Minute)
	m.Scan()
	if got := m.Get(key); got != tier.TierCold {
		t.Fatalf("want COLD, got %s", got)
	}

	// The in-memory graph should have been evicted…
	if evictCount.Load() != 1 {
		t.Fatalf("want 1 eviction, got %d", evictCount.Load())
	}
	// …and the fsnotify subscription released with it (#6267).
	if hook.pausedCount() != 1 {
		t.Fatalf("#6267: want 1 Pause call on WARM→COLD, got %d", hook.pausedCount())
	}
	if got := hook.pausedAt(0); got != "/repo/ph2a@main" {
		t.Fatalf("Pause fired for the wrong slot: %q", got)
	}
	if hook.resumedCount() != 0 {
		t.Fatalf("want 0 Resume calls before wake, got %d", hook.resumedCount())
	}
}

// ---------------------------------------------------------------------------
// Test: COLD→EXPIRED fires Pause
//
// The subscription should be removed only when the graph.fb is deleted from
// disk (COLD→EXPIRED), because at that point there is nothing to reindex into.
// ---------------------------------------------------------------------------

func TestWatcherPausedOnExpired(t *testing.T) {
	cfg := tier.DefaultTTLConfig()
	// Use short expired window so we can drive COLD→EXPIRED in the test.
	cfg.ExpiredWindow = 2 * time.Minute
	cfg.ExpiredWindowWorktree = 2 * time.Minute

	clock, advance := makeClock()
	hook := &fakeWatcherHook{}
	var diskEvictCount atomic.Int32

	m := tier.NewManagerForTestWithDiskEvict(cfg, clock,
		noopEvict,
		noopReload,
		func(k tier.SlotKey) (int64, error) {
			diskEvictCount.Add(1)
			return 0, nil
		},
	)
	m.SetWatcherHook(hook)

	key := tier.SlotKey{RepoPath: "/repo/expired", Ref: "feat/gone"}
	m.Register(key, false, tier.SlotKindBranchFeature)

	// Drive to COLD.
	advance(6 * time.Minute)
	m.Scan()
	advance(61 * time.Minute)
	m.Scan()
	if got := m.Get(key); got != tier.TierCold {
		t.Fatalf("prereq: want COLD, got %s", got)
	}
	// Pause already fired on WARM→COLD (#6267).
	if hook.pausedCount() != 1 {
		t.Fatalf("#6267: want 1 Pause call on WARM→COLD, got %d", hook.pausedCount())
	}

	// Drive COLD→EXPIRED.
	advance(3 * time.Minute)
	m.Scan()
	if got := m.Get(key); got != tier.TierExpired {
		t.Fatalf("want EXPIRED, got %s", got)
	}

	// COLD→EXPIRED pauses again. Pause is idempotent per (repo, ref) in
	// watch.DefaultManager, so the second call is a no-op there; it is kept
	// because a slot can be registered COLD and expire without this process
	// ever having observed its WARM→COLD.
	if hook.pausedCount() != 2 {
		t.Fatalf("want a 2nd Pause call on EXPIRED, got %d", hook.pausedCount())
	}
	if diskEvictCount.Load() != 1 {
		t.Fatalf("want 1 disk evict, got %d", diskEvictCount.Load())
	}
}

// ---------------------------------------------------------------------------
// Test: COLD→HOT fires Resume before reload
// ---------------------------------------------------------------------------

func TestWatcherResumedOnColdWake(t *testing.T) {
	clock, advance := makeClock()
	hook := &fakeWatcherHook{}

	reloadOrder := make([]string, 0)
	var mu sync.Mutex

	m := tier.NewManagerForTest(tier.DefaultTTLConfig(), clock,
		noopEvict,
		func(k tier.SlotKey) error {
			mu.Lock()
			// At reload time, Resume must have already fired.
			if len(hook.resumed) == 0 {
				reloadOrder = append(reloadOrder, "reload-before-resume")
			} else {
				reloadOrder = append(reloadOrder, "reload-after-resume")
			}
			mu.Unlock()
			return nil
		},
	)
	m.SetWatcherHook(hook)

	key := tier.SlotKey{RepoPath: "/repo/ph2a-wake", Ref: "feat/x"}
	m.Register(key, false, tier.SlotKindBranchFeature)

	// Drive to COLD.
	advance(6 * time.Minute)
	m.Scan()
	advance(61 * time.Minute)
	m.Scan()
	if got := m.Get(key); got != tier.TierCold {
		t.Fatalf("prereq: want COLD, got %s", got)
	}

	// Touch → cold wake.
	if err := m.Touch(key); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if got := m.Get(key); got != tier.TierHot {
		t.Fatalf("want HOT after wake, got %s", got)
	}

	if hook.resumedCount() != 1 {
		t.Fatalf("want 1 Resume call after wake, got %d", hook.resumedCount())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reloadOrder) != 1 || reloadOrder[0] != "reload-after-resume" {
		t.Fatalf("want reload to happen AFTER resume; got order=%v", reloadOrder)
	}
}

// ---------------------------------------------------------------------------
// Test: Resume latency: cold wake must not block
// ---------------------------------------------------------------------------

func TestColdWakeResumeLatency(t *testing.T) {
	clock, advance := makeClock()
	hook := &fakeWatcherHook{}

	m := tier.NewManagerForTest(tier.DefaultTTLConfig(), clock, noopEvict, noopReload)
	m.SetWatcherHook(hook)

	key := tier.SlotKey{RepoPath: "/repo/latency", Ref: "main"}
	m.Register(key, false, tier.SlotKindBranchFeature)

	// Drive to COLD.
	advance(6 * time.Minute)
	m.Scan()
	advance(61 * time.Minute)
	m.Scan()

	start := time.Now()
	if err := m.Touch(key); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	elapsed := time.Since(start)

	// This asserts Touch does not BLOCK, not that it is fast. The hook is a
	// fake, so the healthy path is microseconds; the failure mode (a synchronous
	// fsnotify re-subscribe on the wake path) costs seconds or hangs. 5s keeps
	// that separation with ~10000x headroom over the healthy path, and cannot be
	// tripped by a busy runner the way 500ms could.
	if elapsed > 5*time.Second {
		t.Errorf("cold-wake round-trip %s exceeds the 5s non-blocking budget", elapsed)
	}
}

// ---------------------------------------------------------------------------
// Test: daemon shutdown — Pause not called on already-HOT slots
// ---------------------------------------------------------------------------

func TestNoPauseFiredForHotSlots(t *testing.T) {
	clock, _ := makeClock()
	hook := &fakeWatcherHook{}

	m := tier.NewManagerForTest(tier.DefaultTTLConfig(), clock, noopEvict, noopReload)
	m.SetWatcherHook(hook)

	key := tier.SlotKey{RepoPath: "/repo/hot", Ref: "main"}
	m.Register(key, true, tier.SlotKindBranchMain)

	// Run a scan — slot should stay HOT (only 0s idle).
	m.Scan()

	if hook.pausedCount() != 0 {
		t.Fatalf("Pause must not fire for HOT slot; got %d", hook.pausedCount())
	}
}

// ---------------------------------------------------------------------------
// Test: 10 concurrent cold-wakes — no race/deadlock
// ---------------------------------------------------------------------------

func TestConcurrentColdWakesWithWatcherHook(t *testing.T) {
	clock, advance := makeClock()
	hook := &fakeWatcherHook{}
	var reloadCount atomic.Int32

	m := tier.NewManagerForTest(tier.DefaultTTLConfig(), clock,
		noopEvict,
		func(k tier.SlotKey) error { reloadCount.Add(1); return nil },
	)
	m.SetWatcherHook(hook)

	const N = 10
	keys := make([]tier.SlotKey, N)
	for i := 0; i < N; i++ {
		keys[i] = tier.SlotKey{RepoPath: "/repo/concurrent", Ref: string(rune('a' + i))}
		m.Register(keys[i], false, tier.SlotKindBranchFeature)
	}

	// Drive all to COLD.
	advance(6 * time.Minute)
	m.Scan()
	advance(61 * time.Minute)
	m.Scan()
	for _, k := range keys {
		if got := m.Get(k); got != tier.TierCold {
			t.Fatalf("prereq: want COLD for %s, got %s", k.Ref, got)
		}
	}

	// Concurrent cold wakes.
	var wg sync.WaitGroup
	wg.Add(N)
	for _, k := range keys {
		k := k
		go func() {
			defer wg.Done()
			if err := m.Touch(k); err != nil {
				t.Errorf("Touch %s: %v", k.Ref, err)
			}
		}()
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent cold-wakes deadlocked")
	}

	if int(reloadCount.Load()) != N {
		t.Errorf("want %d reloads, got %d", N, reloadCount.Load())
	}
	if hook.resumedCount() != N {
		t.Errorf("want %d Resume calls, got %d", N, hook.resumedCount())
	}
}

// ---------------------------------------------------------------------------
// Test: full cycle — Pause on WARM→COLD, Resume after cold-wake
// ---------------------------------------------------------------------------

func TestSlotPausedThenResumedAfterWake(t *testing.T) {
	// Simulate the full cycle: register → evict → wake.
	// After the wake the slot should be HOT and watcher should be resumed.
	// #6267: WARM→COLD pauses, the cold wake resumes.
	clock, advance := makeClock()
	hook := &fakeWatcherHook{}

	m := tier.NewManagerForTest(tier.DefaultTTLConfig(), clock, noopEvict, noopReload)
	m.SetWatcherHook(hook)

	key := tier.SlotKey{RepoPath: "/repo/stale", Ref: "main"}
	m.Register(key, false, tier.SlotKindBranchFeature)

	advance(6 * time.Minute)
	m.Scan()
	advance(61 * time.Minute)
	m.Scan()
	_ = m.Touch(key) // cold wake

	// After the wake the slot is HOT; the subscription was released on
	// WARM→COLD (#6267) and re-acquired by the cold-wake Resume.
	if hook.pausedCount() != 1 {
		t.Errorf("#6267: want 1 Pause call on WARM→COLD, got %d", hook.pausedCount())
	}
	if hook.resumedCount() != 1 {
		t.Errorf("want 1 Resume call after cold wake, got %d", hook.resumedCount())
	}
}

// ---------------------------------------------------------------------------
// Test: pressure-driven WARM→COLD fires Pause too (#6267)
//
// A COLD slot holds no graph and no subscription, whichever transition made it
// COLD. If the pressure path kept the subscription, the descriptors it holds
// would stay committed until the slot expired — the shape of the #6267 bug,
// reached by the other road.
// ---------------------------------------------------------------------------

func TestWatcherPausedOnPressureEvict(t *testing.T) {
	clock, advance := makeClock()
	hook := &fakeWatcherHook{}

	sysBytes := uint64(1024 * 1024 * 1024) // 1 GB
	heapVal := uint64(700 * 1024 * 1024)   // above the 60% threshold

	ttl := tier.DefaultTTLConfig()
	ttl.HeapMaxPct = 60
	ttl.SystemMemoryBytes = sysBytes

	m := tier.NewManagerForTestWithHeap(ttl, clock, noopEvict, noopReload,
		func() uint64 { return heapVal },
		func() uint64 { return sysBytes },
	)
	m.SetWatcherHook(hook)

	// Two non-pinned slots (pinned slots are exempt from pressure eviction);
	// the scanner evicts half, oldest first.
	oldKey := tier.SlotKey{RepoPath: "/repo/pressure", Ref: "old"}
	m.Register(oldKey, false, tier.SlotKindBranchFeature)
	advance(time.Minute)
	newKey := tier.SlotKey{RepoPath: "/repo/pressure", Ref: "new"}
	m.Register(newKey, false, tier.SlotKindBranchFeature)

	m.Scan()

	if got := m.Get(oldKey); got != tier.TierCold {
		t.Fatalf("prereq: want the oldest slot pressure-evicted to COLD, got %s", got)
	}
	if hook.pausedCount() != 1 {
		t.Fatalf("#6267: want 1 Pause call on pressure-evict, got %d", hook.pausedCount())
	}
	if got := hook.pausedAt(0); got != "/repo/pressure@old" {
		t.Fatalf("Pause fired for the wrong slot: %q", got)
	}
}
