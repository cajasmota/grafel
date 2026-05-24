package tier_test

// tier_watcher_test.go — integration tests for PH2a watcher pause/resume
// driven by tier transitions. PH2a of epic #2087 (#2096).

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon/tier"
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
// Test: WARM→COLD fires Pause
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

	key := tier.SlotKey{RepoPath: "/repo/ph2a", Ref: "main"}
	m.Register(key, false, tier.SlotKindBranchFeature)

	// Drive HOT → WARM → COLD.
	advance(6 * time.Minute)
	m.Scan()
	if got := m.Get(key); got != tier.TierWarm {
		t.Fatalf("prereq: want WARM, got %s", got)
	}

	advance(61 * time.Minute)
	m.Scan()
	if got := m.Get(key); got != tier.TierCold {
		t.Fatalf("want COLD, got %s", got)
	}

	if evictCount.Load() != 1 {
		t.Fatalf("want 1 eviction, got %d", evictCount.Load())
	}
	if hook.pausedCount() != 1 {
		t.Fatalf("want 1 Pause call, got %d", hook.pausedCount())
	}
	if hook.resumedCount() != 0 {
		t.Fatalf("want 0 Resume calls before wake, got %d", hook.resumedCount())
	}
}

// ---------------------------------------------------------------------------
// Test: COLD→HOT fires Resume before reload
// ---------------------------------------------------------------------------

func TestWatcherResumedOnColdWake(t *testing.T) {
	clock, advance := makeClock()
	hook := &fakeWatcherHook{}

	resumeOrder := make([]string, 0)
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
	_ = resumeOrder

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
// Test: Resume latency within 500ms
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

	// The 500ms budget is well within reach for a fake hook; real budget is
	// for the fsnotify re-subscribe. Verify we didn't introduce any blocking.
	if elapsed > 500*time.Millisecond {
		t.Errorf("cold-wake round-trip %s exceeds 500ms budget", elapsed)
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
// Test: stale detection — IsPaused is false after cold-wake
// ---------------------------------------------------------------------------

func TestSlotNotPausedAfterWake(t *testing.T) {
	// Simulate the full cycle: register → evict → wake.
	// After the wake the slot should be HOT and watcher should be resumed.
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

	// After the wake the slot is HOT; the hook should have 1 Pause + 1 Resume.
	if hook.pausedCount() != 1 {
		t.Errorf("want 1 Pause call, got %d", hook.pausedCount())
	}
	if hook.resumedCount() != 1 {
		t.Errorf("want 1 Resume call, got %d", hook.resumedCount())
	}
}
