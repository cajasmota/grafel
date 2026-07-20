package mcp

// bm25_eviction_test.go — idle-eviction lifecycle for the lazily-built BM25
// index (resident-graph memory epic #5850, BM25-evictable track).
//
// BM25 is the single largest lazy resident consumer (~313 MB warmed on the
// corpus). It is built lazily on first search (getBM25) and, with this track,
// dropped after an idle window so a session that has stopped searching does not
// pin it — rebuilding transparently and single-flighted on the next search.
//
// These tests assert: (1) idle eviction actually nils the index and re-arms the
// lazy build; (2) a not-yet-idle index is kept; (3) search results are identical
// whether the index was freshly built, warm, or rebuilt-after-eviction; (4)
// concurrent search during eviction is safe (run under -race); (5) the rebuild
// after eviction is single-flighted (exactly one build under concurrent first
// searches); (6) the State-level sweep evicts idle repos; (7) eviction composes
// with reload's resetIndexes without a double-free.

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// hitKey renders a hit as a stable (entity-id, rounded-score) string so two
// rankings can be compared for byte-identical results across build paths.
func hitKey(h Hit) string {
	id := ""
	if h.Entity != nil {
		id = h.Entity.ID
	}
	return fmt.Sprintf("%s|%.9f", id, h.Score)
}

func hitKeys(hits []Hit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = hitKey(h)
	}
	return out
}

// TestBM25EvictIdleReleasesAndRebuilds asserts an idle index is dropped (field
// nilled) and the next getBM25 transparently rebuilds a live index.
func TestBM25EvictIdleReleasesAndRebuilds(t *testing.T) {
	doc := buildSyntheticDoc(400)
	lr := &LoadedRepo{Repo: "corpus", Doc: doc}

	if got := lr.getBM25(); got == nil {
		t.Fatal("getBM25 returned nil for a repo with a Doc")
	}
	if lr.BM25 == nil {
		t.Fatal("BM25 field not populated after getBM25")
	}

	// Age the last-use past the idle window and sweep.
	last := lr.bm25LastUse
	if !lr.evictBM25IfIdle(time.Minute, last.Add(2*time.Minute)) {
		t.Fatal("evictBM25IfIdle returned false for an index idle past the window")
	}
	if lr.BM25 != nil {
		t.Fatal("BM25 field not nilled after idle eviction")
	}

	// Rebuild-on-demand: next getBM25 must transparently rebuild.
	if got := lr.getBM25(); got == nil {
		t.Fatal("getBM25 did not rebuild after eviction")
	}
	if lr.BM25 == nil {
		t.Fatal("BM25 field not repopulated after rebuild")
	}
}

// TestBM25EvictNotIdleKept asserts a recently-used index is NOT evicted.
func TestBM25EvictNotIdleKept(t *testing.T) {
	doc := buildSyntheticDoc(200)
	lr := &LoadedRepo{Repo: "corpus", Doc: doc}
	_ = lr.getBM25()
	before := lr.BM25

	// now is only 10s after last use, window is 5m → keep.
	if lr.evictBM25IfIdle(5*time.Minute, lr.bm25LastUse.Add(10*time.Second)) {
		t.Fatal("evictBM25IfIdle evicted an index that was used within the window")
	}
	if lr.BM25 != before {
		t.Fatal("BM25 index changed despite being within the idle window")
	}
}

// TestBM25EvictNeverBuiltNoop asserts eviction on a repo that never built BM25
// is a safe no-op (guards the reload double-free / nil-deref path).
func TestBM25EvictNeverBuiltNoop(t *testing.T) {
	lr := &LoadedRepo{Repo: "corpus", Doc: buildSyntheticDoc(50)}
	if lr.evictBM25IfIdle(time.Minute, time.Now().Add(time.Hour)) {
		t.Fatal("evictBM25IfIdle reported an eviction for a never-built index")
	}
	// Disabled window (idle<=0) must also be a no-op even when built.
	_ = lr.getBM25()
	if lr.evictBM25IfIdle(0, time.Now().Add(time.Hour)) {
		t.Fatal("evictBM25IfIdle evicted with a disabled (0) idle window")
	}
	if lr.BM25 == nil {
		t.Fatal("disabled window must not drop the index")
	}
}

// TestBM25SearchAfterEvictionIdenticalResults is the correctness guard: results
// must be byte-identical whether BM25 was freshly built, warm, or rebuilt after
// an eviction.
func TestBM25SearchAfterEvictionIdenticalResults(t *testing.T) {
	doc := buildSyntheticDoc(1200)
	lr := &LoadedRepo{Repo: "corpus", Doc: doc}
	queries := []string{
		"handleOrderRequest customer processor payload",
		"order kafka fulfilment pipeline",
		"premium checklistType 2",
		"validating persisting entity",
	}

	warm := map[string][]string{}
	for _, q := range queries {
		warm[q] = hitKeys(lr.getBM25().Search(q, 10))
	}

	// Evict and rebuild.
	if !lr.evictBM25IfIdle(time.Minute, lr.bm25LastUse.Add(2*time.Minute)) {
		t.Fatal("expected eviction")
	}
	for _, q := range queries {
		got := hitKeys(lr.getBM25().Search(q, 10))
		want := warm[q]
		if len(got) != len(want) {
			t.Fatalf("query %q: result count changed after rebuild: got %d want %d", q, len(got), len(want))
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("query %q rank %d: rebuilt result differs\n got=%s\nwant=%s", q, i, got[i], want[i])
			}
		}
	}
}

// TestBM25ConcurrentSearchDuringEviction runs many searches concurrently with a
// racing eviction loop. Under -race this proves no use-after-free / data race:
// an in-flight search completes on the pointer it borrowed even as the field is
// nilled and rebuilt underneath it.
func TestBM25ConcurrentSearchDuringEviction(t *testing.T) {
	doc := buildSyntheticDoc(800)
	lr := &LoadedRepo{Repo: "corpus", Doc: doc}
	_ = lr.getBM25() // warm once

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Searchers: continuously borrow-and-search.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				hits := lr.getBM25().Search("order kafka fulfilment premium", 10)
				_ = hits
			}
		}()
	}

	// Evictor: aggressively evict (force by treating every borrow as idle).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			// now far in the future so any built index is considered idle.
			lr.evictBM25IfIdle(time.Nanosecond, time.Now().Add(time.Hour))
			time.Sleep(50 * time.Microsecond)
		}
	}()

	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// TestBM25SingleFlightRebuild asserts the rebuild after eviction is single-
// flighted: concurrent first-searches all observe exactly ONE built index
// (identical pointer), never N independent builds.
func TestBM25SingleFlightRebuild(t *testing.T) {
	doc := buildSyntheticDoc(600)
	lr := &LoadedRepo{Repo: "corpus", Doc: doc}
	_ = lr.getBM25()
	if !lr.evictBM25IfIdle(time.Minute, lr.bm25LastUse.Add(2*time.Minute)) {
		t.Fatal("expected eviction")
	}

	const n = 16
	var wg sync.WaitGroup
	ptrs := make([]*BM25Index, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ptrs[i] = lr.getBM25()
		}(i)
	}
	close(start)
	wg.Wait()

	first := ptrs[0]
	if first == nil {
		t.Fatal("rebuild produced a nil index")
	}
	for i, p := range ptrs {
		if p != first {
			t.Fatalf("single-flight violated: goroutine %d built a distinct index (%p != %p)", i, p, first)
		}
	}
}

// TestSweepIdleBM25 exercises the State-level sweep the server fires from
// reloadBeforeCall: an idle repo is evicted, a fresh one is kept.
func TestSweepIdleBM25(t *testing.T) {
	idleDoc := buildSyntheticDoc(300)
	idleDoc.Repo = "idle"
	freshDoc := buildSyntheticDoc(300)
	freshDoc.Repo = "fresh"
	srv := newTestServer(t, idleDoc, freshDoc)

	lg := srv.State.groups["test"]
	idle := lg.Repos["idle"]
	fresh := lg.Repos["fresh"]

	// Warm both through the search path so bm25LastUse is stamped.
	_ = idle.getBM25()
	_ = fresh.getBM25()

	// Backdate the idle repo's last-use well past the window.
	idle.idxMu.Lock()
	idle.bm25LastUse = time.Now().Add(-10 * time.Minute)
	idle.idxMu.Unlock()

	evicted := srv.State.SweepIdleBM25(5 * time.Minute)
	if evicted != 1 {
		t.Fatalf("SweepIdleBM25 evicted %d repos, want 1", evicted)
	}
	if idle.BM25 != nil {
		t.Fatal("idle repo BM25 not evicted by sweep")
	}
	if fresh.BM25 == nil {
		t.Fatal("fresh repo BM25 wrongly evicted by sweep")
	}

	// Disabled window is a no-op.
	if got := srv.State.SweepIdleBM25(0); got != 0 {
		t.Fatalf("SweepIdleBM25(0) evicted %d, want 0 (disabled)", got)
	}
}

// TestBM25EvictionReloadInteraction asserts eviction composes with reload's
// resetIndexes without a double-free: resetIndexes already nils BM25, so a
// following eviction finds nil and no-ops, and getBM25 still rebuilds cleanly.
func TestBM25EvictionReloadInteraction(t *testing.T) {
	doc := buildSyntheticDoc(200)
	lr := &LoadedRepo{Repo: "corpus", Doc: doc, LabelIndex: BuildLabelIndex(doc)}
	_ = lr.getBM25()

	// Simulate a reload swap invalidating the derived indexes.
	lr.resetIndexes()
	if lr.BM25 != nil {
		t.Fatal("resetIndexes should have nilled BM25")
	}
	// Eviction after reset must be a safe no-op (nothing to free).
	if lr.evictBM25IfIdle(time.Nanosecond, time.Now().Add(time.Hour)) {
		t.Fatal("eviction reported a drop after resetIndexes already cleared BM25")
	}
	// Rebuild still works.
	if lr.getBM25() == nil {
		t.Fatal("getBM25 failed to rebuild after reset+evict")
	}

	// And a rebuilt index returns the same ranking as a plain fresh build.
	fresh := BuildBM25(doc)
	q := "order kafka premium checklistType"
	got := hitKeys(lr.getBM25().Search(q, 10))
	want := hitKeys(fresh.Search(q, 10))
	if len(got) != len(want) {
		t.Fatalf("result count mismatch after reload/rebuild: %d vs %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("rank %d differs after reload/rebuild: %s vs %s", i, got[i], want[i])
		}
	}
}
