// mem_watermark_evict_5872_pr3_test.go — tests for the memory-watermark eviction
// TRIGGER (memory epic #5850, issue #5872 PR3): State.SweepToMemoryBudget sheds the
// single oldest-lastAccess non-pinned group (LRU victim) per sweep under memory
// pressure, PINNING the active (max-lastAccess) group exactly as PR2's idle sweep
// does, and converging across successive reloadBeforeCall sweeps. Companion to
// idle_group_evict_5872_pr2_test.go (the TIME trigger) and evict_group_5872_test.go
// (the PR1 primitive).
//
// The sampler models the PRODUCTION HeapAlloc hazard: the sample does NOT drop the
// instant a group is evicted — the freed heap is only reflected after a GC, and under
// keepReader=true the evict itself allocates a fresh cold shell (transiently RAISING
// HeapAlloc). A fixed over-budget sampler that never falls is therefore the exact
// condition a buggy "re-sample after each evict and keep going" loop would mishandle:
// it would shed EVERY non-pinned group in one sweep (a thundering-herd revive storm).
// These tests assert the correct one-victim-per-sweep behaviour under that hazard —
// which a count-based sampler that magically dropped on delete() would have hidden.
package mcp

import (
	"sync"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
)

// fixedSampler installs a memSampler that always returns v, independent of how many
// groups remain resident — modelling GC lag / keepReader shell allocation where the
// sample does not fall when a group is evicted. Set under s.mu, the same lock
// SweepToMemoryBudget samples under.
func fixedSampler(st *State, v uint64) {
	st.mu.Lock()
	st.memSampler = func() uint64 { return v }
	st.mu.Unlock()
}

const overBudget = uint64(1 << 40)  // 1 TiB — always over any test budget
const testBudget = uint64(1 << 30)  // 1 GiB
const underBudget = uint64(1 << 20) // 1 MiB — always under testBudget

// Criterion (the CRUX — catches the GC-lag/over-evict bug): with the sample stuck
// OVER budget (as production HeapAlloc is until GC runs), a single sweep evicts
// EXACTLY ONE group — the LRU (oldest) non-pinned group — NOT every non-pinned group.
// The old re-sample-mid-loop code evicted all non-pinned groups here (thundering herd);
// this asserts the graduated one-victim-per-sweep contract.
func TestSweepToMemoryBudget_OneVictimPerSweepNoThunderingHerd(t *testing.T) {
	st := seedGroupsOnDisk(t, map[string]*graph.Document{
		"oldest": lazyTestDoc(),
		"middle": lazyTestDoc(),
		"newest": lazyTestDoc(),
	})
	fixedSampler(st, overBudget) // never falls under budget within a sweep
	now := time.Now()
	setLastAccess(st, "oldest", now.Add(-30*time.Minute))
	setLastAccess(st, "middle", now.Add(-20*time.Minute))
	setLastAccess(st, "newest", now.Add(-1*time.Minute)) // active pin

	n := st.SweepToMemoryBudget(testBudget, true)
	if n != 1 {
		t.Fatalf("expected EXACTLY 1 eviction per sweep (no thundering herd), got %d", n)
	}
	if st.groupResident("oldest") {
		t.Error("LRU (oldest) group was not the victim")
	}
	if !st.groupResident("middle") || !st.groupResident("newest") {
		t.Error("over-evicted: only the single oldest group may go in one sweep")
	}
	if !st.gated("oldest") {
		t.Error("evicted group not recorded in the cold-gate")
	}
}

// Criterion (LRU order + convergence + termination): successive sweeps — each the real
// entry-sampled decision reloadBeforeCall would make once GC reflects prior frees —
// shed groups OLDEST-FIRST, one per sweep, and STOP once only the pinned group remains
// (never evicting the active set). Sampler stays over budget throughout.
func TestSweepToMemoryBudget_ConvergesAcrossSweepsLRUOrder(t *testing.T) {
	st := seedGroupsOnDisk(t, map[string]*graph.Document{
		"g_old": lazyTestDoc(),
		"g_mid": lazyTestDoc(),
		"g_new": lazyTestDoc(), // active pin (newest)
	})
	fixedSampler(st, overBudget)
	now := time.Now()
	setLastAccess(st, "g_old", now.Add(-30*time.Minute))
	setLastAccess(st, "g_mid", now.Add(-20*time.Minute))
	setLastAccess(st, "g_new", now.Add(-1*time.Minute))

	// Sweep 1 → oldest.
	if n := st.SweepToMemoryBudget(testBudget, true); n != 1 {
		t.Fatalf("sweep 1: want 1 eviction, got %d", n)
	}
	if st.groupResident("g_old") || !st.groupResident("g_mid") || !st.groupResident("g_new") {
		t.Fatal("sweep 1 must evict only the oldest (g_old)")
	}
	// Sweep 2 → next-oldest.
	if n := st.SweepToMemoryBudget(testBudget, true); n != 1 {
		t.Fatalf("sweep 2: want 1 eviction, got %d", n)
	}
	if st.groupResident("g_mid") || !st.groupResident("g_new") {
		t.Fatal("sweep 2 must evict the next-oldest (g_mid), keeping the pin")
	}
	// Sweep 3 → only the pinned active group remains: evict NOTHING, converged.
	if n := st.SweepToMemoryBudget(testBudget, true); n != 0 {
		t.Fatalf("sweep 3: only the pin remains, want 0 evictions, got %d", n)
	}
	if !st.groupResident("g_new") {
		t.Fatal("PIN VIOLATED: the active group was evicted once alone under pressure")
	}
	// Idempotent thereafter — never sacrifices the active set no matter how over budget.
	if n := st.SweepToMemoryBudget(testBudget, true); n != 0 {
		t.Fatalf("post-convergence sweep must stay 0, got %d", n)
	}
}

// Criterion: already under budget → zero evictions, no residency change, even with
// ancient groups a positive idle window would evict.
func TestSweepToMemoryBudget_UnderBudgetNoEvictions(t *testing.T) {
	st := seedGroupsOnDisk(t, map[string]*graph.Document{
		"a": lazyTestDoc(),
		"b": lazyTestDoc(),
	})
	fixedSampler(st, underBudget)
	old := time.Now().Add(-24 * time.Hour)
	setLastAccess(st, "a", old)
	setLastAccess(st, "b", old)

	if n := st.SweepToMemoryBudget(testBudget, true); n != 0 {
		t.Fatalf("under-budget sweep must evict nothing, got %d", n)
	}
	if !st.groupResident("a") || !st.groupResident("b") {
		t.Fatal("under-budget sweep changed residency")
	}
}

// Criterion (the pin CRUX): the active (max-lastAccess) group is PINNED even under
// sustained extreme pressure — repeated sweeps with the sample permanently over budget
// shed every OTHER group one at a time but never the active one.
func TestSweepToMemoryBudget_PinsActiveGroupUnderPressure(t *testing.T) {
	st := seedGroupsOnDisk(t, map[string]*graph.Document{
		"A_active": lazyTestDoc(),
		"B":        lazyTestDoc(),
		"C":        lazyTestDoc(),
	})
	fixedSampler(st, overBudget)
	now := time.Now()
	setLastAccess(st, "A_active", now.Add(-1*time.Minute)) // newest → active pin
	setLastAccess(st, "B", now.Add(-10*time.Minute))
	setLastAccess(st, "C", now.Add(-20*time.Minute))

	total := 0
	for i := 0; i < 10; i++ { // far more sweeps than groups
		total += st.SweepToMemoryBudget(testBudget, true)
	}
	if total != 2 {
		t.Fatalf("expected exactly the 2 non-pinned groups shed across sweeps, got %d", total)
	}
	if !st.groupResident("A_active") {
		t.Fatal("PIN VIOLATED: the active group was evicted under sustained pressure")
	}
	if st.groupResident("B") || st.groupResident("C") {
		t.Error("non-pinned groups B/C should both be shed under sustained pressure")
	}
}

// Criterion: disabled (budget 0) → no-op, byte-identical residency, and NO sample is
// even taken (a sampler that fails the test if called proves the early-out).
func TestSweepToMemoryBudget_DisabledIsNoOp(t *testing.T) {
	st := seedGroupsOnDisk(t, map[string]*graph.Document{
		"a": lazyTestDoc(),
		"b": lazyTestDoc(),
	})
	st.mu.Lock()
	st.memSampler = func() uint64 {
		t.Error("memSampler called with budget 0 — must early-out before sampling")
		return overBudget
	}
	st.mu.Unlock()
	old := time.Now().Add(-24 * time.Hour)
	setLastAccess(st, "a", old)
	setLastAccess(st, "b", old)

	if n := st.SweepToMemoryBudget(0, true); n != 0 {
		t.Fatalf("budget 0 must evict nothing, got %d", n)
	}
	if !st.groupResident("a") || !st.groupResident("b") {
		t.Fatal("disabled sweep changed residency — must be byte-identical no-op")
	}

	// Resolver contract: unset env → OFF (0); malformed → OFF; explicit 0 → OFF;
	// positive → bytes.
	t.Setenv("GRAFEL_MCP_GROUP_RSS_BUDGET_MB", "")
	if got := resolveGroupRSSBudget(); got != 0 {
		t.Errorf("unset GRAFEL_MCP_GROUP_RSS_BUDGET_MB: want 0 (OFF), got %d", got)
	}
	t.Setenv("GRAFEL_MCP_GROUP_RSS_BUDGET_MB", "garbage")
	if got := resolveGroupRSSBudget(); got != 0 {
		t.Errorf("malformed budget: want 0 (OFF), got %d", got)
	}
	t.Setenv("GRAFEL_MCP_GROUP_RSS_BUDGET_MB", "0")
	if got := resolveGroupRSSBudget(); got != 0 {
		t.Errorf("explicit 0: want 0 (OFF), got %d", got)
	}
	t.Setenv("GRAFEL_MCP_GROUP_RSS_BUDGET_MB", "512")
	if got := resolveGroupRSSBudget(); got != 512*1024*1024 {
		t.Errorf("512MB budget: want %d bytes, got %d", 512*1024*1024, got)
	}
}

// Criterion (-race): a query to the active group A concurrent with watermark sweeps
// that shed the LRU tail (B, C) — no fault, A always survives. Run under `go test -race`.
func TestSweepToMemoryBudget_ConcurrentQueryDuringSweep(t *testing.T) {
	st := seedGroupsOnDisk(t, map[string]*graph.Document{
		"A": lazyTestDoc(),
		"B": lazyTestDoc(),
		"C": lazyTestDoc(),
	})
	fixedSampler(st, overBudget) // permanent pressure
	now := time.Now()
	setLastAccess(st, "B", now.Add(-20*time.Minute))
	setLastAccess(st, "C", now.Add(-30*time.Minute))
	// Establish A as the active (max-lastAccess) group BEFORE the sweep loop starts.
	// Without this synchronous touch, A's lastAccess is the zero value until the
	// query goroutine below happens to run for the first time — a race against the
	// sweep goroutine, which can start sweeping (and evict A as the oldest-lastAccess
	// group) before that first touch lands. This is a test-harness synchronization
	// gap, not a product bug: reproduced deterministically on macOS with
	// GOMAXPROCS=1 (the sweep goroutine's first iteration ran before the query
	// goroutine's), confirming the flake is scheduler-timing-dependent rather than a
	// real pin/eviction defect. A is queried continuously below (always the
	// freshly-stamped max-lastAccess pin, now that it has a non-zero baseline).
	if grp := st.Group("A"); grp == nil {
		t.Fatal("group A not resident after seeding")
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				if grp := st.Group("A"); grp == nil {
					t.Error("query to active group A returned nil during watermark sweep")
					return
				}
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			st.SweepToMemoryBudget(testBudget, true)
		}
		close(stop)
	}()

	wg.Wait()
	if !st.groupResident("A") {
		t.Fatal("active group A was evicted by a concurrent watermark sweep")
	}
}

// End-to-end: the trigger fires through the real reloadBeforeCall slow path when the
// GRAFEL_MCP_GROUP_RSS_BUDGET_MB knob is set, shedding ONE LRU group and pinning the
// just-routed active group.
func TestSweepToMemoryBudget_WiredThroughReloadBeforeCall(t *testing.T) {
	st := seedGroupsOnDisk(t, map[string]*graph.Document{
		"A": lazyTestDoc(),
		"B": lazyTestDoc(),
	})
	fixedSampler(st, overBudget)
	now := time.Now()
	setLastAccess(st, "B", now.Add(-20*time.Minute))
	setLastAccess(st, "A", now.Add(-1*time.Minute)) // active

	srv := &Server{
		State:           st,
		Tel:             NewTelemetry(0),
		groupRSSBudget:  512 * 1024 * 1024, // any positive budget; sample is over it
		groupKeepReader: true,
		reloadDebounce:  0, // force the slow path every call
	}
	srv.reloadBeforeCall()

	if !st.groupResident("A") {
		t.Fatal("PIN VIOLATED via reloadBeforeCall: active group A evicted under watermark")
	}
	if st.groupResident("B") {
		t.Error("idle LRU group B was not shed by the wired watermark sweep")
	}
}
