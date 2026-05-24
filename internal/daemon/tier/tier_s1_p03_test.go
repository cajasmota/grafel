package tier_test

// tier_s1_p03_test.go — tests for S1 lazy hydration (#2151) and
// P0.3 pressure-driven eviction (#2141).

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon/tier"
)

// ---------------------------------------------------------------------------
// S1: RegisterCold
// ---------------------------------------------------------------------------

// TestRegisterColdStartsCold verifies that RegisterCold places a slot at COLD
// without triggering any eviction or reload callback.
func TestRegisterColdStartsCold(t *testing.T) {
	clock, _ := makeClock()
	var evictCount, reloadCount atomic.Int32
	m := tier.NewManagerForTest(tier.DefaultTTLConfig(), clock,
		func(k tier.SlotKey) { evictCount.Add(1) },
		func(k tier.SlotKey) error { reloadCount.Add(1); return nil },
	)
	key := tier.SlotKey{RepoPath: "/repo/lazy", Ref: "main"}
	m.RegisterCold(key, true, tier.SlotKindBranchMain)

	if got := m.Get(key); got != tier.TierCold {
		t.Fatalf("RegisterCold: want COLD, got %s", got)
	}
	if evictCount.Load() != 0 {
		t.Fatalf("RegisterCold must not fire evict callback; got %d calls", evictCount.Load())
	}
	if reloadCount.Load() != 0 {
		t.Fatalf("RegisterCold must not fire reload callback; got %d calls", reloadCount.Load())
	}
}

// TestRegisterColdDoesNotDowngradeHot verifies that calling RegisterCold on an
// already-HOT slot is a no-op (the slot stays HOT).
func TestRegisterColdDoesNotDowngradeHot(t *testing.T) {
	clock, _ := makeClock()
	m := tier.NewManagerForTest(tier.DefaultTTLConfig(), clock, noopEvict, noopReload)
	key := tier.SlotKey{RepoPath: "/repo/active", Ref: "main"}

	// Register HOT via the normal path (simulates a completed index pass).
	m.Register(key, true, tier.SlotKindBranchMain)
	if got := m.Get(key); got != tier.TierHot {
		t.Fatalf("prereq: want HOT, got %s", got)
	}

	// A subsequent RegisterCold (e.g. from a daemon restart scan) must NOT
	// downgrade the HOT slot that was just freshly indexed.
	m.RegisterCold(key, true, tier.SlotKindBranchMain)
	if got := m.Get(key); got != tier.TierHot {
		t.Fatalf("RegisterCold must not downgrade HOT slot; got %s", got)
	}
}

// TestRegisterColdThenColdWake verifies the full S1 lifecycle:
// RegisterCold → slot is COLD → Touch triggers reload → slot becomes HOT.
func TestRegisterColdThenColdWake(t *testing.T) {
	clock, _ := makeClock()
	var reloadCount atomic.Int32
	m := tier.NewManagerForTest(tier.DefaultTTLConfig(), clock, noopEvict,
		func(k tier.SlotKey) error { reloadCount.Add(1); return nil },
	)
	key := tier.SlotKey{RepoPath: "/repo/cold-then-warm", Ref: "main"}
	m.RegisterCold(key, true, tier.SlotKindBranchMain)

	if got := m.Get(key); got != tier.TierCold {
		t.Fatalf("prereq: want COLD after lazy registration, got %s", got)
	}

	// First MCP query → cold-wake.
	if err := m.Touch(key); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if got := m.Get(key); got != tier.TierHot {
		t.Fatalf("want HOT after cold-wake, got %s", got)
	}
	if reloadCount.Load() != 1 {
		t.Fatalf("want exactly 1 reload on cold-wake, got %d", reloadCount.Load())
	}
}

// TestMultipleRegisterColdNoDuplicates verifies that calling RegisterCold
// multiple times for the same key creates exactly one slot.
func TestMultipleRegisterColdNoDuplicates(t *testing.T) {
	clock, _ := makeClock()
	m := tier.NewManagerForTest(tier.DefaultTTLConfig(), clock, noopEvict, noopReload)
	key := tier.SlotKey{RepoPath: "/repo/dup", Ref: "main"}

	m.RegisterCold(key, true, tier.SlotKindBranchMain)
	m.RegisterCold(key, true, tier.SlotKindBranchMain)
	m.RegisterCold(key, true, tier.SlotKindBranchMain)

	snap := m.Snapshot()
	count := 0
	for _, s := range snap {
		if s.Key == key {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("want exactly 1 slot, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// P0.3: Pressure-driven eviction
// ---------------------------------------------------------------------------

// TestPressureEvictOldestSlots verifies: 8 HOT slots loaded, synthetic heap
// breach → oldest 4 (half of 8) are evicted to COLD; pinned-main is exempt.
func TestPressureEvictOldestSlots(t *testing.T) {
	// Use a synthetic clock so we can control lastAccessedAt ordering.
	clock, advance := makeClock()

	var mu sync.Mutex
	var evicted []tier.SlotKey
	evictFn := func(k tier.SlotKey) {
		mu.Lock()
		evicted = append(evicted, k)
		mu.Unlock()
	}

	// Heap: starts above threshold immediately.
	heapVal := uint64(700 * 1024 * 1024) // 700 MB
	// System: 1 GB — threshold at 60% = 600 MB.
	sysBytes := uint64(1024 * 1024 * 1024)

	ttl := tier.DefaultTTLConfig()
	ttl.HeapMaxPct = 60
	ttl.SystemMemoryBytes = sysBytes

	m := tier.NewManagerForTestWithHeap(ttl, clock, evictFn, noopReload,
		func() uint64 { return heapVal },
		func() uint64 { return sysBytes },
	)

	// Register 8 non-pinned HOT slots with different access times.
	// Slots 0-3 are oldest (should be evicted), 4-7 newest.
	keys := make([]tier.SlotKey, 8)
	for i := range keys {
		keys[i] = tier.SlotKey{RepoPath: "/repo/slot", Ref: string(rune('a' + i))}
		m.Register(keys[i], false, tier.SlotKindBranchFeature)
		advance(1 * time.Minute) // each slot 1 min newer than the previous
	}

	// Also register a pinned-main slot (must NOT be pressure-evicted).
	pinnedKey := tier.SlotKey{RepoPath: "/repo/pinned", Ref: "main"}
	m.Register(pinnedKey, true, tier.SlotKindBranchMain)

	// Trigger a scan — this should fire pressure eviction.
	m.Scan()

	mu.Lock()
	got := len(evicted)
	evictedCopy := make([]tier.SlotKey, len(evicted))
	copy(evictedCopy, evicted)
	mu.Unlock()

	// Expect half of 8 = 4 evictions.
	if got != 4 {
		t.Fatalf("want 4 pressure-evictions (half of 8), got %d: %v", got, evictedCopy)
	}

	// Evicted slots should be the oldest 4 (a, b, c, d).
	evictedSet := make(map[string]bool)
	for _, k := range evictedCopy {
		evictedSet[k.Ref] = true
	}
	for _, ref := range []string{"a", "b", "c", "d"} {
		if !evictedSet[ref] {
			t.Errorf("expected oldest slot %q to be pressure-evicted, but it wasn't; evicted=%v", ref, evictedCopy)
		}
	}

	// Pinned-main must not have been evicted.
	if evictedSet["main"] {
		t.Fatalf("pinned-main must be exempt from pressure eviction")
	}

	// Pinned-main slot must still be HOT.
	if got := m.Get(pinnedKey); got == tier.TierCold {
		t.Fatalf("pinned-main was pressure-evicted to COLD — must be exempt")
	}
}

// TestPressureEvictDisabledWhenBelowThreshold verifies no eviction occurs when
// heap is below the configured threshold.
func TestPressureEvictDisabledWhenBelowThreshold(t *testing.T) {
	clock, advance := makeClock()
	var evictCount atomic.Int32

	// Heap: 100 MB below 60% of 1 GB (600 MB threshold).
	heapVal := uint64(100 * 1024 * 1024) // 100 MB
	sysBytes := uint64(1024 * 1024 * 1024)

	ttl := tier.DefaultTTLConfig()
	ttl.HeapMaxPct = 60
	ttl.SystemMemoryBytes = sysBytes

	m := tier.NewManagerForTestWithHeap(ttl, clock,
		func(k tier.SlotKey) { evictCount.Add(1) },
		noopReload,
		func() uint64 { return heapVal },
		func() uint64 { return sysBytes },
	)

	for i := 0; i < 5; i++ {
		k := tier.SlotKey{RepoPath: "/repo/ok", Ref: string(rune('a' + i))}
		m.Register(k, false, tier.SlotKindBranchFeature)
		advance(time.Minute)
	}

	m.Scan()

	// No evictions expected — heap is well below threshold.
	if evictCount.Load() != 0 {
		t.Fatalf("no pressure eviction expected below threshold, got %d", evictCount.Load())
	}
}

// TestPressureEvictZeroPctDisabled verifies that HeapMaxPct=0 disables
// pressure eviction entirely (used to opt-out).
func TestPressureEvictZeroPctDisabled(t *testing.T) {
	clock, _ := makeClock()
	var evictCount atomic.Int32

	// Even with heap way over threshold, HeapMaxPct=0 should suppress it.
	sysBytes := uint64(1024 * 1024 * 1024)
	ttl := tier.DefaultTTLConfig()
	ttl.HeapMaxPct = 0
	ttl.SystemMemoryBytes = sysBytes

	m := tier.NewManagerForTestWithHeap(ttl, clock,
		func(k tier.SlotKey) { evictCount.Add(1) },
		noopReload,
		func() uint64 { return 900 * 1024 * 1024 }, // 900 MB — above any threshold
		func() uint64 { return sysBytes },
	)

	for i := 0; i < 3; i++ {
		k := tier.SlotKey{RepoPath: "/repo/noop", Ref: string(rune('a' + i))}
		m.Register(k, false, tier.SlotKindBranchFeature)
	}

	m.Scan()

	if evictCount.Load() != 0 {
		t.Fatalf("HeapMaxPct=0 should disable pressure eviction; got %d evictions", evictCount.Load())
	}
}

// TestPressureEvictPinnedMainExempt verifies that a pinned-main slot is never
// pressure-evicted even when heap is massively above threshold.
func TestPressureEvictPinnedMainExempt(t *testing.T) {
	clock, _ := makeClock()
	var evictCount atomic.Int32

	// Heap massively over threshold (1% of 1 GB = 10 MB threshold; 900 MB heap).
	sysBytes := uint64(1024 * 1024 * 1024)
	ttl := tier.DefaultTTLConfig()
	ttl.HeapMaxPct = 1
	ttl.SystemMemoryBytes = sysBytes

	m := tier.NewManagerForTestWithHeap(ttl, clock,
		func(k tier.SlotKey) { evictCount.Add(1) },
		noopReload,
		func() uint64 { return 900 * 1024 * 1024 }, // 900 MB
		func() uint64 { return sysBytes },
	)

	pinnedKey := tier.SlotKey{RepoPath: "/repo/main", Ref: "main"}
	m.Register(pinnedKey, true, tier.SlotKindBranchMain)

	m.Scan()

	if evictCount.Load() != 0 {
		t.Fatalf("pinned-main must be exempt from pressure eviction; got %d evictions", evictCount.Load())
	}
	if got := m.Get(pinnedKey); got == tier.TierCold {
		t.Fatalf("pinned-main was pressure-evicted to COLD — must be exempt")
	}
}
