package dashboard

// graphstate_pass4_test.go — tests for the non-blocking served-graph load,
// durable (mtime-keyed) cache, and the Pass-4 discard-bug fix (#50).

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
)

// pathGraphDocN builds an n-node path 0—1—…—(n-1). Every interior node is an
// articulation point with non-zero betweenness, giving RunAlgorithms real work
// and widening the concurrent-read window for the -race test.
func pathGraphDocN(n int) *graph.Document {
	doc := &graph.Document{Repo: "pass4-test"}
	for i := 0; i < n; i++ {
		doc.Entities = append(doc.Entities, graph.Entity{
			ID: fmt.Sprintf("n%d", i), Name: fmt.Sprintf("n%d", i), Kind: "function",
		})
		if i > 0 {
			doc.Relationships = append(doc.Relationships, graph.Relationship{
				FromID: fmt.Sprintf("n%d", i-1), ToID: fmt.Sprintf("n%d", i), Kind: "CALLS",
			})
		}
	}
	return doc
}

// pathGraphDoc builds a 3-node path A—B—C. B is an articulation point (removing
// it disconnects A from C) and lies on the A→C shortest path, so it has non-zero
// betweenness centrality. A and C are leaves (not articulation points).
func pathGraphDoc() *graph.Document {
	return &graph.Document{
		Repo: "pass4-test",
		Entities: []graph.Entity{
			{ID: "A", Name: "A", Kind: "function"},
			{ID: "B", Name: "B", Kind: "function"},
			{ID: "C", Name: "C", Kind: "function"},
		},
		Relationships: []graph.Relationship{
			{FromID: "A", ToID: "B", Kind: "CALLS"},
			{FromID: "B", ToID: "C", Kind: "CALLS"},
		},
	}
}

func entByID(doc *graph.Document, id string) *graph.Entity {
	for i := range doc.Entities {
		if doc.Entities[i].ID == id {
			return &doc.Entities[i]
		}
	}
	return nil
}

// TestApplyAlgorithmResults_PersistsCentralityAndArticulation is the discard-bug
// fix: attachAlgorithmResults must write the betweenness Centrality and the
// ArticulationPoints it computes, not silently drop them.
func TestApplyAlgorithmResults_PersistsCentralityAndArticulation(t *testing.T) {
	doc := pathGraphDoc()
	attachAlgorithmResults(doc)

	b := entByID(doc, "B")
	if b == nil {
		t.Fatal("entity B missing")
	}
	if b.Centrality == nil {
		t.Fatal("B.Centrality is nil — betweenness centrality was DISCARDED (discard bug)")
	}
	if *b.Centrality <= 0 {
		t.Fatalf("B.Centrality = %v, want > 0 (B is on the A→C path)", *b.Centrality)
	}
	if !b.IsArticulationPt {
		t.Fatal("B.IsArticulationPt = false — articulation point was DISCARDED (discard bug)")
	}
	// A leaf must NOT be an articulation point.
	if a := entByID(doc, "A"); a == nil || a.IsArticulationPt {
		t.Fatalf("A.IsArticulationPt = %v, want false", a.IsArticulationPt)
	}
}

// TestApplyAlgorithmResults_ZeroBetweennessKeepsExplicitCentrality pins the
// serialisation boundary #5954 depends on, on the dashboard's attachment path.
//
// Entity.Centrality is *float64 with `json:",omitempty"`: nil DROPS the key,
// pointer-to-zero emits `"centrality": 0` (and handlers_graph.go only reports
// `betweenness` when the pointer is non-nil). Before #5954 the result map held
// an explicit 0 for every entity, so `if bt, ok := res.Centrality[e.ID]; ok`
// was always true. The map is sparse now, so the loop keys off PageRank
// membership — dense over the entity set — and reads centrality with the zero
// default. Reverting that would silently strip the zero from every
// zero-betweenness entity; this test fails if it does.
func TestApplyAlgorithmResults_ZeroBetweennessKeepsExplicitCentrality(t *testing.T) {
	// A→B→C plus an isolated D: only B has non-zero betweenness.
	doc := pathGraphDoc()
	doc.Entities = append(doc.Entities, graph.Entity{ID: "D", Name: "D", Kind: "function"})
	attachAlgorithmResults(doc)

	if b := entByID(doc, "B"); b == nil || b.Centrality == nil || *b.Centrality <= 0 {
		t.Fatalf("B must carry a positive betweenness (it is on the A→C path)")
	}
	for _, id := range []string{"A", "C", "D"} {
		e := entByID(doc, id)
		if e == nil {
			t.Fatalf("entity %s missing", id)
		}
		if e.Centrality == nil {
			t.Errorf("%s.Centrality is nil — a zero-betweenness entity lost its explicit 0, so the "+
				"serialised graph omits the \"centrality\"/\"betweenness\" key it has always carried", id)
			continue
		}
		if *e.Centrality != 0 {
			t.Errorf("%s.Centrality = %v, want 0", id, *e.Centrality)
		}
	}
}

// pendingGrp builds a cache + a published group with one repo whose doc needs a
// background Pass-4 sweep, mirroring the shape loadGroupForRef publishes.
func pendingGrp(t *testing.T, doc *graph.Document, stateDir string, fbMtime time.Time) (*GraphCache, *DashGroup) {
	t.Helper()
	grp := &DashGroup{
		Name:      "g",
		Repos:     map[string]*DashRepo{"r": {Slug: "r", Doc: doc}},
		stateDirs: map[string]string{"r": stateDir},
		pendingAlgo: []pendingAlgoRepo{
			{doc: doc, stateDir: stateDir, fbMtime: fbMtime},
		},
	}
	c := NewGraphCache(60 * time.Second)
	c.mu.Lock()
	c.entries["g"] = &cacheEntry{group: grp, loadedAt: time.Now()}
	c.mu.Unlock()
	return c, grp
}

// TestApplyAlgorithmsOnLoad_NonBlockingThenPersistsAndEvicts proves the load
// path does NOT block on Pass-4 and the deferred sweep NEVER mutates the live
// published doc: applyAlgorithmsOnLoad returns immediately with a degree
// fallback (communities empty), then schedulePendingAlgo persists the sidecar
// and EVICTS the entry (so the next load re-applies it) — leaving the live doc
// untouched (Fix A#1 + B, data-race fix #50).
func TestApplyAlgorithmsOnLoad_NonBlockingThenPersistsAndEvicts(t *testing.T) {
	// Gate the background sweep so we can deterministically observe the load
	// returned BEFORE the full sweep ran.
	gate := make(chan struct{})
	done := make(chan string, 1)
	backgroundAlgoGate = gate
	backgroundAlgoDone = func(k string) { done <- k }
	t.Cleanup(func() { backgroundAlgoGate = nil; backgroundAlgoDone = nil })

	stateDir := t.TempDir()
	doc := pathGraphDoc()
	fbMtime := time.Now()

	if !applyAlgorithmsOnLoad(doc, stateDir, fbMtime) {
		t.Fatal("expected needsBackground=true (communities==0, no sidecar)")
	}
	// Non-blocking: no synchronous full sweep, so communities MUST be empty…
	if len(doc.Communities) != 0 {
		t.Fatalf("load blocked on Pass-4: got %d communities synchronously", len(doc.Communities))
	}
	// …but a cheap degree fallback ordering IS applied immediately.
	if b := entByID(doc, "B"); b == nil || b.PageRank == nil {
		t.Fatal("degree-based fallback PageRank not applied on the served graph")
	}

	c, grp := pendingGrp(t, doc, stateDir, fbMtime)
	c.schedulePendingAlgo("g", grp)

	// Still gated: entry present, live doc untouched.
	c.mu.Lock()
	_, present := c.entries["g"]
	c.mu.Unlock()
	if !present {
		t.Fatal("entry evicted before the background sweep ran")
	}

	close(gate)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("background Pass-4 never completed")
	}

	// The sidecar was persisted keyed to the fb mtime…
	if res, ok := loadPersistedAlgoResults(stateDir, fbMtime); !ok || res == nil {
		t.Fatal("persisted sidecar did not round-trip for the current fb mtime")
	}
	// …the entry was evicted so the next load re-applies it…
	c.mu.Lock()
	_, stillThere := c.entries["g"]
	c.mu.Unlock()
	if stillThere {
		t.Fatal("cache entry not evicted after background compute")
	}
	// …and CRUCIALLY the live, published doc was NEVER mutated in place — that
	// was the data race. Communities remain empty on the doc handlers hold.
	if len(doc.Communities) != 0 {
		t.Fatal("background sweep mutated the live published doc (data race)")
	}
}

// TestSchedulePendingAlgo_NoDataRaceWithConcurrentReaders is the -race guard the
// review required: concurrent handler-style readers range doc.Entities and read
// Communities/PageRank/Centrality WHILE the background sweep runs. Because the
// sweep is read-only over the published doc, `go test -race` must be clean.
func TestSchedulePendingAlgo_NoDataRaceWithConcurrentReaders(t *testing.T) {
	backgroundAlgoGate = nil // run immediately, maximise overlap with readers
	done := make(chan string, 1)
	backgroundAlgoDone = func(k string) { done <- k }
	t.Cleanup(func() { backgroundAlgoDone = nil })

	stateDir := t.TempDir()
	doc := pathGraphDocN(60)
	fbMtime := time.Now()
	applyAlgorithmsOnLoad(doc, stateDir, fbMtime) // stamps degree fallback

	c, grp := pendingGrp(t, doc, stateDir, fbMtime)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				// Mirror the lock-free reads handlers do on the shared doc.
				for j := range doc.Entities {
					_ = doc.Entities[j].PageRank
					_ = doc.Entities[j].CommunityID
					_ = doc.Entities[j].Centrality
					_ = doc.Entities[j].IsArticulationPt
				}
				_ = len(doc.Communities)
				_ = doc.AlgorithmStats
			}
		}()
	}

	c.schedulePendingAlgo("g", grp)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		close(stop)
		wg.Wait()
		t.Fatal("background Pass-4 never completed")
	}
	close(stop)
	wg.Wait()
}

// TestSchedulePendingAlgo_PersistFailureDoesNotRecomputeForever is the CPU-spin
// regression guard. When persistAlgoResults cannot write the sidecar (best-effort,
// error-swallowed: read-only state dir, disk-full, EPERM), the reloaded graph
// still omits Pass-4, so applyAlgorithmsOnLoad re-flags the repo as pending. Before
// the fix, schedulePendingAlgo therefore recomputed the multi-minute Pass-4 sweep
// on EVERY reload and evicted again — an unbounded, CPU-bound loop that pinned the
// serve daemon at hundreds of % CPU while the dashboard polled the group's graph
// endpoint (graph.fb untouched — only the algo sidecar failed). The in-memory
// algoComputed guard must make the compute happen at most ONCE per graph.fb
// version regardless of whether the sidecar persisted.
func TestSchedulePendingAlgo_PersistFailureDoesNotRecomputeForever(t *testing.T) {
	if runtime.GOOS == "windows" {
		// This test forces persistAlgoResults to fail via os.Chmod(stateDir,
		// 0o500) on the theory that a read-only directory blocks file
		// creation inside it — true on POSIX, but on Windows Chmod only
		// toggles FILE_ATTRIBUTE_READONLY on the directory entry itself and
		// does NOT block os.CreateTemp from creating a NEW file inside that
		// directory. The test's own precondition assertion ("sidecar
		// unexpectedly persisted despite read-only dir") would fail before
		// ever reaching the CPU-spin regression it's meant to guard, so
		// POSIX dir-write-permission semantics are a real requirement here.
		t.Skip("POSIX dir-write-permission semantics required")
	}
	backgroundAlgoGate = nil // run immediately
	var computes int32
	done := make(chan string, 4)
	backgroundAlgoDone = func(k string) { atomic.AddInt32(&computes, 1); done <- k }
	t.Cleanup(func() { backgroundAlgoDone = nil })

	stateDir := t.TempDir()
	// Force persistAlgoResults to fail: a read-only state dir makes its
	// os.CreateTemp(stateDir, …) return an error, which it swallows.
	if err := os.Chmod(stateDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(stateDir, 0o700) })
	fbMtime := time.Now()

	// First reload cycle: the repo needs Pass-4 (no sidecar, empty communities).
	doc1 := pathGraphDoc()
	c, grp1 := pendingGrp(t, doc1, stateDir, fbMtime)
	c.schedulePendingAlgo("g", grp1)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("first background Pass-4 never completed")
	}
	// Precondition: the sidecar genuinely did NOT persist (the loop's trigger).
	if _, ok := loadPersistedAlgoResults(stateDir, fbMtime); ok {
		t.Fatal("test precondition broken: sidecar unexpectedly persisted despite read-only dir")
	}

	// Second reload cycle — a FRESH pending repo for the SAME graph.fb version,
	// exactly what the eviction + next dashboard poll reproduces. This is where the
	// old code recomputed Pass-4 again (and again, forever).
	doc2 := pathGraphDoc()
	grp2 := &DashGroup{
		Name:        "g",
		Repos:       map[string]*DashRepo{"r": {Slug: "r", Doc: doc2}},
		stateDirs:   map[string]string{"r": stateDir},
		pendingAlgo: []pendingAlgoRepo{{doc: doc2, stateDir: stateDir, fbMtime: fbMtime}},
	}
	c.mu.Lock()
	c.entries["g"] = &cacheEntry{group: grp2, loadedAt: time.Now()}
	c.mu.Unlock()
	c.schedulePendingAlgo("g", grp2)

	select {
	case <-done:
		t.Fatal("Pass-4 recomputed for an already-computed graph.fb version — the compute→evict→reload CPU spin is NOT fixed")
	case <-time.After(500 * time.Millisecond):
		// good: the guard filtered the already-computed repo, no second sweep.
	}
	// The second schedule must NOT have evicted the entry: staying warm is what
	// stops the reload→recompute cycle.
	c.mu.Lock()
	_, present := c.entries["g"]
	c.mu.Unlock()
	if !present {
		t.Fatal("entry evicted on the second schedule — the following reload re-enters the spin")
	}
	if got := atomic.LoadInt32(&computes); got != 1 {
		t.Fatalf("Pass-4 ran %d times for one graph.fb version, want exactly 1", got)
	}
}

// TestLoadPersistedAlgoResults_StaleOnMtimeChange verifies the sidecar is
// rejected when graph.fb has changed since it was computed.
func TestLoadPersistedAlgoResults_StaleOnMtimeChange(t *testing.T) {
	stateDir := t.TempDir()
	computedAt := time.Now()
	seed := pathGraphDoc()
	res := graph.RunAlgorithms(seed.Entities, seed.Relationships)
	persistAlgoResults(stateDir, computedAt, res)

	if _, ok := loadPersistedAlgoResults(stateDir, computedAt); !ok {
		t.Fatal("sidecar should be accepted for the mtime it was computed against")
	}
	// A different (newer) graph.fb mtime must invalidate the sidecar.
	newer := computedAt.Add(1 * time.Second)
	if _, ok := loadPersistedAlgoResults(stateDir, newer); ok {
		t.Fatal("stale sidecar accepted: graph.fb mtime changed but sidecar was reused")
	}
}

// TestGetGroupForRef_DurableCacheSurvivesTTL verifies the durable, mtime-keyed
// cache: a loaded group is NOT evicted just because more than the old 60s TTL
// has elapsed. Under the old wall-clock TTL this entry would be discarded and a
// reload attempted (which, for this synthetic group, would fail).
func TestGetGroupForRef_DurableCacheSurvivesTTL(t *testing.T) {
	c := NewGraphCache(60 * time.Second)
	grp := &DashGroup{Name: "g", Repos: map[string]*DashRepo{}}
	// No stateDirs → diskUnchanged() is true (nothing on disk changed).
	c.mu.Lock()
	c.entries["g"] = &cacheEntry{group: grp, loadedAt: time.Now().Add(-10 * time.Minute)}
	c.mu.Unlock()

	got, err := c.GetGroup("g")
	if err != nil {
		t.Fatalf("GetGroup returned error (entry was TTL-evicted and reload failed): %v", err)
	}
	if got != grp {
		t.Fatalf("GetGroup returned a different group pointer — the durable cache evicted a still-valid entry")
	}
}
