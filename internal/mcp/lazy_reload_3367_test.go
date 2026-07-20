// lazy_reload_3367_test.go — tests for the lazy + debounced graph-index reload
// that kills the ~400ms warm-call floor (#3367).
//
// Coverage:
//   - cheap path: building no traversal index leaves the lazy fields nil
//   - lazy correctness: a getter returns the same index as the old eager build
//   - reset-on-reload: resetIndexes() re-arms the Once so the next access
//     rebuilds against the fresh Doc
//   - debounce: resolveReloadDebounce honours the env overrides + default, and
//     the server debounce fast-path skips reloads within the window
//   - concurrency: concurrent getter calls build exactly one index (race-safe)
package mcp

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
)

// lazyTestDoc builds a small graph with CALLS / STEP_IN_PROCESS edges and
// PageRank so every derived index has something to construct.
func lazyTestDoc() *graph.Document {
	pr1, pr2, pr3 := 0.2, 0.9, 0.5
	return &graph.Document{
		Entities: []graph.Entity{
			{ID: "a", Name: "A", Kind: "Function", SourceFile: "a.go", StartLine: 1, PageRank: &pr1},
			{ID: "b", Name: "B", Kind: "Function", SourceFile: "b.go", StartLine: 1, PageRank: &pr2},
			{ID: "p", Name: "Proc", Kind: "process_flow", SourceFile: "p.go", StartLine: 1, PageRank: &pr3},
		},
		Relationships: []graph.Relationship{
			{FromID: "a", ToID: "b", Kind: "CALLS"},
			graph.Relationship{FromID: "p", ToID: "a", Kind: stepInProcessEdge}.WithProperties(map[string]string{"step_index": "0"}),
			graph.Relationship{FromID: "p", ToID: "b", Kind: stepInProcessEdge}.WithProperties(map[string]string{"step_index": "1"}),
		},
	}
}

// TestLazyIndexes_CheapPathBuildsNothing asserts that constructing a LoadedRepo
// (as reloadLocked does, via resetIndexes) and NOT calling any traversal getter
// leaves every lazy field nil. This is the cheap-call invariant: whoami / stats
// / feedback_event must not pay the adjacency / PageRank build cost (#3367).
func TestLazyIndexes_CheapPathBuildsNothing(t *testing.T) {
	doc := lazyTestDoc()
	lr := &LoadedRepo{Repo: "r", Doc: doc, LabelIndex: BuildLabelIndex(doc)}
	lr.resetIndexes()

	if lr.adjacency != nil {
		t.Error("adjacency built without a getter call — cheap path regressed")
	}
	if lr.callsAdj != nil {
		t.Error("callsAdj built without a getter call")
	}
	if lr.stepAdj != nil {
		t.Error("stepAdj built without a getter call")
	}
	if lr.byID != nil {
		t.Error("byID built without a getter call")
	}
	if lr.topKPageRank != nil {
		t.Error("topKPageRank built without a getter call")
	}

	// Touching only the adjacency getter must build adjacency but still leave
	// the heavier PageRank cache untouched.
	_ = lr.getAdjacency()
	if lr.adjacency == nil {
		t.Error("getAdjacency did not populate adjacency")
	}
	if lr.topKPageRank != nil {
		t.Error("getAdjacency leaked into the PageRank cache — indexes not independent")
	}
	if lr.callsAdj != nil {
		t.Error("getAdjacency leaked into callsAdj")
	}
}

// TestLazyIndexes_MatchEagerBuild confirms each lazy getter returns exactly what
// the old eager build produced for the same Document.
func TestLazyIndexes_MatchEagerBuild(t *testing.T) {
	doc := lazyTestDoc()
	lr := &LoadedRepo{Repo: "r", Doc: doc, LabelIndex: BuildLabelIndex(doc)}
	lr.resetIndexes()

	// Adjacency.
	wantAdj := buildAdjacency(doc, "r")
	gotAdj := lr.getAdjacency()
	if !reflect.DeepEqual(gotAdj.out, wantAdj.out) || !reflect.DeepEqual(gotAdj.in, wantAdj.in) {
		t.Errorf("getAdjacency mismatch:\n got out=%v in=%v\nwant out=%v in=%v",
			gotAdj.out, gotAdj.in, wantAdj.out, wantAdj.in)
	}

	// CallsAdj.
	if got, want := lr.getCallsAdj(), buildCallsAdjacency(doc); !reflect.DeepEqual(got, want) {
		t.Errorf("getCallsAdj = %v; want %v", got, want)
	}

	// StepAdj.
	if got, want := lr.getStepAdj(), buildStepAdjacency(doc); !reflect.DeepEqual(got, want) {
		t.Errorf("getStepAdj = %v; want %v", got, want)
	}

	// ByID.
	byID := lr.getByID()
	for i := range doc.Entities {
		id := doc.Entities[i].ID
		if byID[id] != &doc.Entities[i] {
			t.Errorf("getByID[%q] does not point at the Document entity", id)
		}
	}

	// TopKPageRank — highest PageRank ("b", 0.9) must be first.
	top := lr.getTopKPageRank()
	if len(top) == 0 || top[0] != "b" {
		t.Errorf("getTopKPageRank[0] = %v; want \"b\"", top)
	}
}

// TestLazyIndexes_CachedAcrossCalls verifies the getter returns the SAME
// instance on repeated calls (built at most once until reset).
func TestLazyIndexes_CachedAcrossCalls(t *testing.T) {
	doc := lazyTestDoc()
	lr := &LoadedRepo{Repo: "r", Doc: doc, LabelIndex: BuildLabelIndex(doc)}
	lr.resetIndexes()

	a1 := lr.getAdjacency()
	a2 := lr.getAdjacency()
	if a1 != a2 {
		t.Error("getAdjacency rebuilt on the second call — sync.Once not caching")
	}
}

// TestLazyIndexes_ResetRebuildsAgainstFreshDoc simulates a reload: swap Doc and
// call resetIndexes; the next getter must reflect the new Document.
func TestLazyIndexes_ResetRebuildsAgainstFreshDoc(t *testing.T) {
	doc1 := lazyTestDoc()
	lr := &LoadedRepo{Repo: "r", Doc: doc1, LabelIndex: BuildLabelIndex(doc1)}
	lr.resetIndexes()
	_ = lr.getAdjacency() // build against doc1
	if len(lr.getAdjacency().Outgoing("a")) != 1 {
		t.Fatalf("expected one out-edge from a in doc1")
	}

	// Reload: new Doc with an extra CALLS edge a->p, then reset.
	doc2 := lazyTestDoc()
	doc2.Relationships = append(doc2.Relationships, graph.Relationship{FromID: "a", ToID: "p", Kind: "CALLS"})
	lr.Doc = doc2
	lr.LabelIndex = BuildLabelIndex(doc2)
	lr.resetIndexes()

	gotOut := lr.getAdjacency().Outgoing("a")
	if len(gotOut) != 2 {
		t.Errorf("after reload+reset, a has %d out-edges; want 2 (stale index served)", len(gotOut))
	}
	// CallsAdj must also reflect the fresh doc.
	if len(lr.getCallsAdj().Get("a")) != 2 {
		t.Errorf("after reload+reset, callsAdj.Get(a) = %v; want 2 callees", lr.getCallsAdj().Get("a"))
	}
}

// TestLazyIndexes_ConcurrentGettersBuildOnce hammers the getters from many
// goroutines; with -race this guards the sync.Once + idxMu correctness.
func TestLazyIndexes_ConcurrentGettersBuildOnce(t *testing.T) {
	doc := lazyTestDoc()
	lr := &LoadedRepo{Repo: "r", Doc: doc, LabelIndex: BuildLabelIndex(doc)}
	lr.resetIndexes()

	const goroutines = 32
	var wg sync.WaitGroup
	results := make([]*adjacency, goroutines)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx] = lr.getAdjacency()
			_ = lr.getByID()
			_ = lr.getCallsAdj()
			_ = lr.getStepAdj()
			_ = lr.getTopKPageRank()
		}(i)
	}
	wg.Wait()

	// All goroutines must observe the identical adjacency instance.
	for i := 1; i < goroutines; i++ {
		if results[i] != results[0] {
			t.Fatalf("goroutine %d saw a different adjacency instance — Once raced", i)
		}
	}
}

// TestResolveReloadDebounce covers the env-override precedence and default.
func TestResolveReloadDebounce(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		t.Setenv("GRAFEL_MCP_RELOAD_DEBOUNCE_MS", "")
		t.Setenv("GRAFEL_RELOAD_DEBOUNCE_MS", "")
		if got := resolveReloadDebounce(); got != defaultReloadDebounceMS*time.Millisecond {
			t.Errorf("default = %v; want %v", got, defaultReloadDebounceMS*time.Millisecond)
		}
	})
	t.Run("primary env", func(t *testing.T) {
		t.Setenv("GRAFEL_MCP_RELOAD_DEBOUNCE_MS", "5000")
		t.Setenv("GRAFEL_RELOAD_DEBOUNCE_MS", "200")
		if got := resolveReloadDebounce(); got != 5000*time.Millisecond {
			t.Errorf("got %v; want 5s (primary env wins)", got)
		}
	})
	t.Run("legacy env fallback", func(t *testing.T) {
		t.Setenv("GRAFEL_MCP_RELOAD_DEBOUNCE_MS", "")
		t.Setenv("GRAFEL_RELOAD_DEBOUNCE_MS", "750")
		if got := resolveReloadDebounce(); got != 750*time.Millisecond {
			t.Errorf("got %v; want 750ms (legacy env)", got)
		}
	})
	t.Run("zero disables", func(t *testing.T) {
		t.Setenv("GRAFEL_MCP_RELOAD_DEBOUNCE_MS", "0")
		if got := resolveReloadDebounce(); got != 0 {
			t.Errorf("got %v; want 0 (disabled)", got)
		}
	})
	t.Run("malformed falls back to default", func(t *testing.T) {
		t.Setenv("GRAFEL_MCP_RELOAD_DEBOUNCE_MS", "not-a-number")
		t.Setenv("GRAFEL_RELOAD_DEBOUNCE_MS", "")
		if got := resolveReloadDebounce(); got != defaultReloadDebounceMS*time.Millisecond {
			t.Errorf("got %v; want default on malformed input", got)
		}
	})
}

// TestReloadDebounce_WindowSkipsReload drives reloadBeforeCall through a series
// of rapid calls and asserts the State reloads at most once within the window,
// then again after the window elapses. Reload count is observed via the
// telemetry MarkReload hook by counting actual reloadLocked entries through a
// fresh server with a controllable window.
func TestReloadDebounce_WindowSkipsReload(t *testing.T) {
	doc := lazyTestDoc()
	doc.Repo = "r"
	srv := newTestServer(t, doc)

	// Use a short, deterministic window for the test.
	window := 50 * time.Millisecond
	srv.reloadDebounce = window

	// Count how many times reloadLocked actually runs by watching reloadLastAt
	// changes (it is only stored after a real reload in the slow path).
	srv.reloadLastAt.Store(0)

	// First call: window not yet started (last==0) → reload runs.
	srv.reloadBeforeCall()
	first := srv.reloadLastAt.Load()
	if first == 0 {
		t.Fatal("first reloadBeforeCall did not run a reload")
	}

	// Rapid follow-up calls within the window → all skipped, timestamp frozen.
	for i := 0; i < 10; i++ {
		srv.reloadBeforeCall()
	}
	if got := srv.reloadLastAt.Load(); got != first {
		t.Errorf("reload ran within debounce window: ts moved %d -> %d", first, got)
	}

	// After the window elapses, the next call reloads again (timestamp moves).
	time.Sleep(window + 20*time.Millisecond)
	srv.reloadBeforeCall()
	if got := srv.reloadLastAt.Load(); got == first {
		t.Error("reload did not run after the debounce window elapsed")
	}
}

// TestReloadDebounce_ZeroDisabled verifies that a zero window reloads on every
// call (no skipping).
func TestReloadDebounce_ZeroDisabled(t *testing.T) {
	doc := lazyTestDoc()
	doc.Repo = "r"
	srv := newTestServer(t, doc)
	srv.reloadDebounce = 0
	srv.reloadLastAt.Store(0)

	srv.reloadBeforeCall()
	prev := srv.reloadLastAt.Load()
	// With debounce disabled, a subsequent call must run again and (almost
	// always) advance the timestamp. Loop a few times to avoid same-nanosecond
	// flakiness on very fast clocks.
	advanced := false
	for i := 0; i < 5; i++ {
		time.Sleep(time.Millisecond)
		srv.reloadBeforeCall()
		if srv.reloadLastAt.Load() != prev {
			advanced = true
			break
		}
	}
	if !advanced {
		t.Error("with debounce disabled, reload timestamp never advanced across calls")
	}
}
