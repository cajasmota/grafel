package dashboard

// search_index_test.go — correctness + performance tests for SearchIndex (#2104)
//
// Tests verify:
//  1. Correctness: exact, prefix, substring matches; empty query guard.
//  2. Scale: 1k / 10k / 50k entity fixtures each complete in < 500 ms.
//  3. Concurrency: 10 parallel goroutines complete in < 2 s, no deadlock.

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// makeScaleGroup creates a DashGroup with n entities spread across two repos.
// Entity names follow the pattern "<prefix>Entity<i>" so we can search for
// specific prefixes and measure latency.
func makeScaleGroup(n int) *DashGroup {
	half := n / 2
	doc1 := &graph.Document{}
	doc2 := &graph.Document{}
	for i := 0; i < half; i++ {
		doc1.Entities = append(doc1.Entities, graph.Entity{
			ID:   fmt.Sprintf("r1e%d", i),
			Name: fmt.Sprintf("AlphaEntity%d", i),
			Kind: "SCOPE.Class",
		})
	}
	for i := 0; i < n-half; i++ {
		doc2.Entities = append(doc2.Entities, graph.Entity{
			ID:   fmt.Sprintf("r2e%d", i),
			Name: fmt.Sprintf("BetaService%d", i),
			Kind: "SCOPE.Class",
		})
	}
	grp := &DashGroup{
		Name: "scale-test",
		Repos: map[string]*DashRepo{
			"repo1": {Slug: "repo1", Doc: doc1},
			"repo2": {Slug: "repo2", Doc: doc2},
		},
	}
	grp.Search = buildSearchIndex(grp)
	return grp
}

// ---------------------------------------------------------------------------
// correctness tests
// ---------------------------------------------------------------------------

func TestSearchIndex_ExactMatch(t *testing.T) {
	grp := makeScaleGroup(100)
	hits := grp.Search.searchEntities("alphaentity42", 20)
	if len(hits) == 0 {
		t.Fatal("expected at least one hit for exact-ish query")
	}
	// Exact (score 3) or prefix (score 2) should come first.
	if hits[0].score < 2 {
		t.Errorf("expected high-score hit first, got score=%d name=%s", hits[0].score, hits[0].entity.Name)
	}
}

func TestSearchIndex_PrefixBeforeSubstring(t *testing.T) {
	grp := makeScaleGroup(200)
	// "alpha" is a prefix of "AlphaEntity*"; "beta" contains "beta" but not as prefix.
	hits := grp.Search.searchEntities("alpha", 50)
	if len(hits) == 0 {
		t.Fatal("expected hits for 'alpha'")
	}
	for _, h := range hits {
		if h.score < 2 {
			t.Errorf("all 'alpha' matches should be prefix (score 2), got score=%d name=%s",
				h.score, h.entity.Name)
		}
	}
}

func TestSearchIndex_SubstringMatch(t *testing.T) {
	// "Entity" is a substring of all names in repo1.
	grp := makeScaleGroup(100)
	hits := grp.Search.searchEntities("entity", 20)
	if len(hits) == 0 {
		t.Fatal("expected substring hits for 'entity'")
	}
}

func TestSearchIndex_EmptyQuery(t *testing.T) {
	grp := makeScaleGroup(100)
	hits := grp.Search.searchEntities("", 20)
	if len(hits) != 0 {
		t.Errorf("empty query should return no hits, got %d", len(hits))
	}
}

func TestSearchIndex_LimitRespected(t *testing.T) {
	grp := makeScaleGroup(1000)
	// "entity" matches all AlphaEntity* (500 entities). Limit is 10.
	hits := grp.Search.searchEntities("entity", 10)
	if len(hits) > 10 {
		t.Errorf("result count %d exceeds limit 10", len(hits))
	}
}

func TestSearchIndex_NoMatch(t *testing.T) {
	grp := makeScaleGroup(100)
	hits := grp.Search.searchEntities("zzznomatch", 20)
	if len(hits) != 0 {
		t.Errorf("expected no hits for unmatched query, got %d", len(hits))
	}
}

func TestSearchIndex_ShortQuery(t *testing.T) {
	// Single-char query — falls back to linear scan within index.
	grp := makeScaleGroup(200)
	hits := grp.Search.searchEntities("a", 20)
	if len(hits) == 0 {
		t.Fatal("expected hits for single-char query 'a'")
	}
}

// ---------------------------------------------------------------------------
// performance tests — each must complete in < 500 ms
// ---------------------------------------------------------------------------

func assertSearchFast(t *testing.T, grp *DashGroup, q string, deadline time.Duration) {
	t.Helper()
	start := time.Now()
	hits := grp.Search.searchEntities(q, 20)
	elapsed := time.Since(start)
	t.Logf("searchEntities(%q) over %d entries → %d hits in %v",
		q, len(grp.Search.entries), len(hits), elapsed)
	if elapsed > deadline {
		t.Errorf("search took %v, want < %v", elapsed, deadline)
	}
}

func TestSearchIndex_Scale_1k(t *testing.T) {
	grp := makeScaleGroup(1_000)
	assertSearchFast(t, grp, "alpha", 500*time.Millisecond)
	assertSearchFast(t, grp, "entity", 500*time.Millisecond)
	assertSearchFast(t, grp, "beta", 500*time.Millisecond)
}

func TestSearchIndex_Scale_10k(t *testing.T) {
	grp := makeScaleGroup(10_000)
	assertSearchFast(t, grp, "alpha", 500*time.Millisecond)
	assertSearchFast(t, grp, "entity", 500*time.Millisecond)
	assertSearchFast(t, grp, "service5", 500*time.Millisecond)
}

func TestSearchIndex_Scale_50k(t *testing.T) {
	grp := makeScaleGroup(50_000)
	assertSearchFast(t, grp, "alpha", 500*time.Millisecond)
	assertSearchFast(t, grp, "entity", 500*time.Millisecond)
	assertSearchFast(t, grp, "beta", 500*time.Millisecond)
}

// ---------------------------------------------------------------------------
// concurrency test
// ---------------------------------------------------------------------------

func TestSearchIndex_Concurrent(t *testing.T) {
	grp := makeScaleGroup(50_000)
	const numGoroutines = 10
	queries := []string{"alpha", "beta", "entity", "service", "class"}

	var wg sync.WaitGroup
	errCh := make(chan string, numGoroutines)
	deadline := 2 * time.Second
	start := time.Now()

	for i := 0; i < numGoroutines; i++ {
		q := queries[i%len(queries)]
		wg.Add(1)
		go func(q string) {
			defer wg.Done()
			hits := grp.Search.searchEntities(q, 20)
			if time.Since(start) > deadline {
				errCh <- fmt.Sprintf("goroutine query %q exceeded %v deadline", q, deadline)
				return
			}
			_ = hits
		}(q)
	}
	wg.Wait()
	close(errCh)

	for msg := range errCh {
		t.Error(msg)
	}
	elapsed := time.Since(start)
	t.Logf("10 concurrent searches over 50k entities completed in %v", elapsed)
	if elapsed > deadline {
		t.Errorf("concurrent search took %v, want < %v", elapsed, deadline)
	}
}

// ---------------------------------------------------------------------------
// benchmark — go test -bench=. -benchmem
// ---------------------------------------------------------------------------

func BenchmarkSearchIndex_Build_50k(b *testing.B) {
	grp := &DashGroup{
		Name: "bench",
		Repos: func() map[string]*DashRepo {
			doc := &graph.Document{}
			for i := 0; i < 50_000; i++ {
				doc.Entities = append(doc.Entities, graph.Entity{
					ID:   fmt.Sprintf("e%d", i),
					Name: fmt.Sprintf("Entity%dService", i),
					Kind: "SCOPE.Class",
				})
			}
			return map[string]*DashRepo{"r": {Slug: "r", Doc: doc}}
		}(),
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		grp.Search = buildSearchIndex(grp)
	}
}

func BenchmarkSearchIndex_Query_50k(b *testing.B) {
	grp := makeScaleGroup(50_000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = grp.Search.searchEntities("alpha", 20)
	}
}

func BenchmarkSearchIndex_QuerySubstring_50k(b *testing.B) {
	grp := makeScaleGroup(50_000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = grp.Search.searchEntities("entity", 20)
	}
}
