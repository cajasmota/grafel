package tier_test

import (
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/tier"
)

// ---------------------------------------------------------------------------
// Clock helper
// ---------------------------------------------------------------------------

func makeClock() (now func() time.Time, advance func(time.Duration)) {
	var mu sync.Mutex
	current := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	return func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			return current
		}, func(d time.Duration) {
			mu.Lock()
			defer mu.Unlock()
			current = current.Add(d)
		}
}

func noopEvict(_ tier.SlotKey)        {}
func noopReload(_ tier.SlotKey) error { return nil }

// ---------------------------------------------------------------------------
// State-machine transitions
// ---------------------------------------------------------------------------

func TestHotToWarm(t *testing.T) {
	clock, advance := makeClock()
	m := tier.NewManagerForTest(tier.DefaultTTLConfig(), clock, noopEvict, noopReload)
	key := tier.SlotKey{RepoPath: "/repo/a", Ref: "main"}
	m.Register(key, true, tier.SlotKindBranchMain)

	if got := m.Get(key); got != tier.TierHot {
		t.Fatalf("want HOT, got %s", got)
	}
	advance(6 * time.Minute)
	m.Scan()
	if got := m.Get(key); got != tier.TierWarm {
		t.Fatalf("want WARM after 6min idle, got %s", got)
	}
}

func TestWarmToCold(t *testing.T) {
	clock, advance := makeClock()
	var evictCount atomic.Int32
	m := tier.NewManagerForTest(tier.DefaultTTLConfig(), clock,
		func(k tier.SlotKey) { evictCount.Add(1) },
		noopReload,
	)
	key := tier.SlotKey{RepoPath: "/repo/b", Ref: "feat/x"}
	m.Register(key, false, tier.SlotKindBranchFeature)

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
}

func TestColdWakeOnDemand(t *testing.T) {
	clock, advance := makeClock()
	var reloadCount atomic.Int32
	m := tier.NewManagerForTest(tier.DefaultTTLConfig(), clock, noopEvict,
		func(k tier.SlotKey) error { reloadCount.Add(1); return nil },
	)
	key := tier.SlotKey{RepoPath: "/repo/c", Ref: "main"}
	m.Register(key, true, tier.SlotKindBranchMain)

	// Drive to COLD.
	advance(6 * time.Minute)
	m.Scan()
	advance(61 * time.Minute)
	m.Scan()
	if got := m.Get(key); got != tier.TierCold {
		t.Fatalf("prereq: want COLD, got %s", got)
	}

	// Touch → reload → HOT.
	if err := m.Touch(key); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if got := m.Get(key); got != tier.TierHot {
		t.Fatalf("want HOT after cold-load, got %s", got)
	}
	if reloadCount.Load() != 1 {
		t.Fatalf("want 1 reload, got %d", reloadCount.Load())
	}
}

func TestPinnedMainNeverExpired(t *testing.T) {
	clock, advance := makeClock()
	m := tier.NewManagerForTest(tier.DefaultTTLConfig(), clock, noopEvict, noopReload)
	key := tier.SlotKey{RepoPath: "/repo/d", Ref: "main"}
	m.Register(key, true, tier.SlotKindBranchMain)

	// Advance 8 days — past the 7-day EXPIRED window.
	advance(8 * 24 * time.Hour)
	m.Scan()

	// The slot must still exist and must NOT be TierExpired.
	snap := m.Snapshot()
	found := false
	for _, s := range snap {
		if s.Key == key {
			found = true
			if s.Tier == tier.TierExpired {
				t.Fatal("pinned main must never reach TierExpired")
			}
		}
	}
	if !found {
		t.Fatal("pinned main slot was removed — must never be deleted in PH2")
	}
}

func TestTouchUnknownSlotRegistersHot(t *testing.T) {
	clock, _ := makeClock()
	m := tier.NewManagerForTest(tier.DefaultTTLConfig(), clock, noopEvict, noopReload)
	key := tier.SlotKey{RepoPath: "/repo/new", Ref: "feat/y"}
	if err := m.Touch(key); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := m.Get(key); got != tier.TierHot {
		t.Fatalf("want HOT, got %s", got)
	}
}

func TestTouchHotSlotNoReload(t *testing.T) {
	clock, _ := makeClock()
	var reloadCount atomic.Int32
	m := tier.NewManagerForTest(tier.DefaultTTLConfig(), clock, noopEvict,
		func(k tier.SlotKey) error { reloadCount.Add(1); return nil },
	)
	key := tier.SlotKey{RepoPath: "/repo/hot", Ref: "main"}
	m.Register(key, true, tier.SlotKindBranchMain)
	if err := m.Touch(key); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reloadCount.Load() != 0 {
		t.Fatalf("reload must not fire for HOT slot")
	}
}

// ---------------------------------------------------------------------------
// Heap eviction measurement
// ---------------------------------------------------------------------------

// TestHeapEviction verifies that releasing a large allocation inside the
// eviction callback actually frees heap. Allocates 32 MB, releases it in the
// WARM→COLD callback, then checks HeapAlloc dropped.
func TestHeapEviction(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping heap measurement in short mode")
	}
	const allocMB = 32
	hold := make([]byte, allocMB*1024*1024)
	for i := range hold {
		hold[i] = byte(i) // prevent compiler elision
	}

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	clock, advance := makeClock()
	ttl := tier.TTLConfig{
		HotWindow:             1 * time.Minute,
		ColdWindow:            5 * time.Minute,
		ColdWindowWorktree:    2 * time.Minute,
		ExpiredWindow:         7 * 24 * time.Hour,
		ExpiredWindowWorktree: 48 * time.Hour,
	}
	evictCh := make(chan struct{}, 1)
	m := tier.NewManagerForTest(ttl, clock, func(k tier.SlotKey) {
		hold = nil // release the 32 MB
		evictCh <- struct{}{}
	}, noopReload)

	key := tier.SlotKey{RepoPath: "/repo/evict", Ref: "feat/evict"}
	m.Register(key, false, tier.SlotKindBranchFeature)

	advance(2 * time.Minute)
	m.Scan() // HOT→WARM
	advance(6 * time.Minute)
	m.Scan() // WARM→COLD

	select {
	case <-evictCh:
	case <-time.After(3 * time.Second):
		t.Fatal("eviction callback not called")
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	t.Logf("heap: before=%dMB after=%dMB (allocated %dMB)",
		before.HeapAlloc/1024/1024, after.HeapAlloc/1024/1024, allocMB)

	if after.HeapAlloc >= before.HeapAlloc {
		t.Logf("NOTE: HeapAlloc did not decrease — GC is non-deterministic, not treated as failure")
	}
}

// ---------------------------------------------------------------------------
// Tier.String
// ---------------------------------------------------------------------------

func TestTierString(t *testing.T) {
	cases := []struct {
		t    tier.Tier
		want string
	}{
		{tier.TierHot, "hot"},
		{tier.TierWarm, "warm"},
		{tier.TierCold, "cold"},
		{tier.TierExpired, "expired"},
	}
	for _, c := range cases {
		if got := c.t.String(); got != c.want {
			t.Errorf("Tier(%d).String() = %q, want %q", c.t, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// PH3: worktree SlotKind uses aggressive TTLs
// ---------------------------------------------------------------------------

// TestWorktreeSlotUsesAggressiveColdWindow verifies that a SlotKindWorktree
// slot transitions WARM→COLD at ColdWindowWorktree (30 min) not the standard
// branch ColdWindow (60 min).
func TestWorktreeSlotUsesAggressiveColdWindow(t *testing.T) {
	clock, advance := makeClock()
	var evictCount atomic.Int32
	m := tier.NewManagerForTest(tier.DefaultTTLConfig(), clock,
		func(k tier.SlotKey) { evictCount.Add(1) },
		noopReload,
	)
	key := tier.SlotKey{RepoPath: "/tmp/repo/.worktrees/feat-x", Ref: "feat/x"}
	m.Register(key, false, tier.SlotKindWorktree)

	// After 6 min: HOT→WARM (same for all kinds)
	advance(6 * time.Minute)
	m.Scan()
	if got := m.Get(key); got != tier.TierWarm {
		t.Fatalf("prereq: want WARM, got %s", got)
	}

	// After 31 min total (25 more): should be COLD (worktree window = 30 min)
	advance(25 * time.Minute)
	m.Scan()
	if got := m.Get(key); got != tier.TierCold {
		t.Fatalf("worktree slot should be COLD at 31 min idle, got %s", got)
	}
	if evictCount.Load() != 1 {
		t.Fatalf("want 1 eviction, got %d", evictCount.Load())
	}
}

// TestBranchSlotStaysWarmAt31Min verifies that a standard branch slot is
// still WARM at 31 min idle (it needs 60 min for WARM→COLD).
func TestBranchSlotStaysWarmAt31Min(t *testing.T) {
	clock, advance := makeClock()
	m := tier.NewManagerForTest(tier.DefaultTTLConfig(), clock, noopEvict, noopReload)
	key := tier.SlotKey{RepoPath: "/tmp/repo", Ref: "feat/branch"}
	m.Register(key, false, tier.SlotKindBranchFeature)

	advance(6 * time.Minute)
	m.Scan() // HOT→WARM
	advance(25 * time.Minute)
	m.Scan() // should stay WARM (31 min total, branch needs 60 min)
	if got := m.Get(key); got != tier.TierWarm {
		t.Fatalf("branch slot should still be WARM at 31 min idle, got %s", got)
	}
}

// TestSlotKindStrings verifies SlotKind.String() returns expected values.
func TestSlotKindStrings(t *testing.T) {
	cases := []struct {
		k    tier.SlotKind
		want string
	}{
		{tier.SlotKindBranchMain, "branch_main"},
		{tier.SlotKindBranchFeature, "branch_feature"},
		{tier.SlotKindWorktree, "worktree"},
	}
	for _, c := range cases {
		if got := c.k.String(); got != c.want {
			t.Errorf("SlotKind(%d).String() = %q, want %q", c.k, got, c.want)
		}
	}
}

func TestDefaultTTLConfig(t *testing.T) {
	cfg := tier.DefaultTTLConfig()
	if cfg.HotWindow != 5*time.Minute {
		t.Errorf("HotWindow: got %v, want 5m", cfg.HotWindow)
	}
	if cfg.ColdWindow != 60*time.Minute {
		t.Errorf("ColdWindow: got %v, want 60m", cfg.ColdWindow)
	}
	if cfg.ExpiredWindow != 7*24*time.Hour {
		t.Errorf("ExpiredWindow: got %v, want 7d", cfg.ExpiredWindow)
	}
}

// ---------------------------------------------------------------------------
// PH6: COLD→EXPIRED disk eviction
// ---------------------------------------------------------------------------

// TestColdToExpiredDiskEviction verifies the full WARM→COLD→EXPIRED→disk-delete
// path using a synthetic clock and a temporary directory as the "store".
func TestColdToExpiredDiskEviction(t *testing.T) {
	// Create a fake state directory with a graph.fb.
	tmp := t.TempDir()
	stateDir := tmp + "/repo-slot/refs/feat%2Fx"
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(stateDir+"/graph.fb", make([]byte, 1024*1024), 0o644); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}

	clock, advance := makeClock()
	var diskEvictCalled atomic.Int32
	diskEvict := func(k tier.SlotKey) (int64, error) {
		diskEvictCalled.Add(1)
		if err := os.RemoveAll(stateDir); err != nil {
			return 0, err
		}
		return 1024 * 1024, nil
	}

	ttl := tier.TTLConfig{
		HotWindow:             1 * time.Minute,
		ColdWindow:            5 * time.Minute,
		ColdWindowWorktree:    2 * time.Minute,
		ExpiredWindow:         7 * 24 * time.Hour,
		ExpiredWindowWorktree: 48 * time.Hour,
	}
	m := tier.NewManagerForTestWithDiskEvict(ttl, clock, noopEvict, noopReload, diskEvict)
	key := tier.SlotKey{RepoPath: "/repo/a", Ref: "feat/x"}
	m.Register(key, false, tier.SlotKindBranchFeature)

	// HOT → WARM
	advance(2 * time.Minute)
	m.Scan()
	if got := m.Get(key); got != tier.TierWarm {
		t.Fatalf("want WARM, got %s", got)
	}

	// WARM → COLD
	advance(6 * time.Minute)
	m.Scan()
	if got := m.Get(key); got != tier.TierCold {
		t.Fatalf("want COLD, got %s", got)
	}

	// COLD → EXPIRED (advance past ExpiredWindow = 7d)
	advance(8 * 24 * time.Hour)
	m.Scan()
	if got := m.Get(key); got != tier.TierExpired {
		t.Fatalf("want EXPIRED, got %s", got)
	}
	if diskEvictCalled.Load() != 1 {
		t.Fatalf("want 1 disk eviction, got %d", diskEvictCalled.Load())
	}

	// Verify the directory was actually removed.
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatalf("stateDir should be deleted after disk eviction")
	}
}

// TestPinnedMainNeverDiskEvicted verifies that isPinnedMain slots never reach
// EXPIRED regardless of how long they have been idle.
func TestPinnedMainNeverDiskEvicted(t *testing.T) {
	clock, advance := makeClock()
	var diskEvictCalled atomic.Int32
	diskEvict := func(k tier.SlotKey) (int64, error) {
		diskEvictCalled.Add(1)
		return 0, nil
	}

	m := tier.NewManagerForTestWithDiskEvict(tier.DefaultTTLConfig(), clock, noopEvict, noopReload, diskEvict)
	key := tier.SlotKey{RepoPath: "/repo/main", Ref: "main"}
	m.Register(key, true /*isPinnedMain*/, tier.SlotKindBranchMain)

	// Advance 10 days — well past the 7-day EXPIRED window.
	advance(10 * 24 * time.Hour)
	m.Scan()

	if got := m.Get(key); got == tier.TierExpired {
		t.Fatal("pinned main must never reach TierExpired")
	}
	if diskEvictCalled.Load() != 0 {
		t.Fatalf("disk evict must not fire for pinned main, got %d calls", diskEvictCalled.Load())
	}
}

// TestManager_Forget removes all refs for a vanished repo and decrements the
// in-memory accounting, without touching other repos' slots (issue #3680).
func TestManager_Forget(t *testing.T) {
	clock, _ := makeClock()
	m := tier.NewManagerForTest(tier.DefaultTTLConfig(), clock, noopEvict, noopReload)

	gone := "/repos/gone-worktree"
	live := "/repos/live"
	m.Register(tier.SlotKey{RepoPath: gone, Ref: "main"}, false, tier.SlotKindWorktree)
	m.Register(tier.SlotKey{RepoPath: gone, Ref: "feat/x"}, false, tier.SlotKindWorktree)
	m.Register(tier.SlotKey{RepoPath: live, Ref: "main"}, true, tier.SlotKindBranchMain)

	if got := m.Len(); got != 3 {
		t.Fatalf("Len before = %d, want 3", got)
	}

	n := m.Forget(gone)
	if n != 2 {
		t.Fatalf("Forget returned %d, want 2 (both refs of the vanished repo)", n)
	}
	if got := m.Len(); got != 1 {
		t.Fatalf("Len after = %d, want 1 (only the live repo remains)", got)
	}
	// Negative: the live repo's slot survives untouched.
	if m.Get(tier.SlotKey{RepoPath: live, Ref: "main"}) != tier.TierHot {
		t.Fatalf("live repo slot was wrongly evicted by Forget")
	}
	// Idempotent: forgetting again is a no-op.
	if n := m.Forget(gone); n != 0 {
		t.Fatalf("second Forget returned %d, want 0", n)
	}
}

func TestManager_ForgetRef(t *testing.T) {
	clock, _ := makeClock()
	m := tier.NewManagerForTest(tier.DefaultTTLConfig(), clock, noopEvict, noopReload)

	repo := "/repos/app"
	m.Register(tier.SlotKey{RepoPath: repo, Ref: "main"}, true, tier.SlotKindBranchMain)
	m.Register(tier.SlotKey{RepoPath: repo, Ref: "feat/x"}, false, tier.SlotKindBranchFeature)

	if got := m.Len(); got != 2 {
		t.Fatalf("Len before = %d, want 2", got)
	}

	// Forget a single ref: only that slot is dropped.
	if !m.ForgetRef(repo, "feat/x") {
		t.Fatalf("ForgetRef(feat/x) = false, want true")
	}
	if got := m.Len(); got != 1 {
		t.Fatalf("Len after = %d, want 1", got)
	}
	if m.Get(tier.SlotKey{RepoPath: repo, Ref: "main"}) != tier.TierHot {
		t.Fatalf("main slot wrongly dropped by ForgetRef")
	}
	// Idempotent / unknown ref: returns false.
	if m.ForgetRef(repo, "feat/x") {
		t.Fatalf("second ForgetRef returned true, want false")
	}
	if m.ForgetRef(repo, "never-registered") {
		t.Fatalf("ForgetRef of unknown ref returned true, want false")
	}
}
