// cross_link_cache_test.go — unit tests for CrossLinkCache.
//
// Issue #2224: ref-keyed cross-repo cache with O(k) invalidation on
// ref-switch.
package mcp

import (
	"sync"
	"testing"
)

// TestCrossLinkCache_BasicGetOrCompute verifies that GetOrCompute returns
// the same result on a hit without calling fn a second time.
func TestCrossLinkCache_BasicGetOrCompute(t *testing.T) {
	cache := NewCrossLinkCache()

	calls := 0
	fn := func() []CrossRepoLink {
		calls++
		return []CrossRepoLink{
			{Source: "repoA::EntityA", Target: "repoB::EntityB", Kind: "call"},
		}
	}

	// First call: cache miss → fn is called.
	links := cache.GetOrCompute("repoA", "main", "repoB", "main", fn)
	if len(links) != 1 {
		t.Fatalf("want 1 link, got %d", len(links))
	}
	if calls != 1 {
		t.Fatalf("want fn called 1 time, got %d", calls)
	}

	// Second call with same key: cache hit → fn NOT called again.
	links2 := cache.GetOrCompute("repoA", "main", "repoB", "main", fn)
	if calls != 1 {
		t.Fatalf("want fn called 1 time total (cache hit), got %d", calls)
	}
	if len(links2) != 1 {
		t.Fatalf("want 1 link from cache hit, got %d", len(links2))
	}
}

// TestCrossLinkCache_DifferentRefsAreDistinctEntries verifies that
// (repoA, main, repoB, main) and (repoA, feature/foo, repoB, main) are
// treated as separate cache entries — this is the core ref-key guarantee.
func TestCrossLinkCache_DifferentRefsAreDistinctEntries(t *testing.T) {
	cache := NewCrossLinkCache()

	mainLinks := []CrossRepoLink{{Source: "repoA::Main", Target: "repoB::Main", Kind: "call"}}
	fooLinks := []CrossRepoLink{
		{Source: "repoA::Foo", Target: "repoB::Foo", Kind: "call"},
		{Source: "repoA::NewEntity", Target: "repoB::Handler", Kind: "import"},
	}

	cache.Set("repoA", "main", "repoB", "main", mainLinks)
	cache.Set("repoA", "feature/foo", "repoB", "main", fooLinks)

	if cache.Len() != 2 {
		t.Fatalf("want 2 cache entries for distinct ref pairs, got %d", cache.Len())
	}

	gotMain, ok := cache.Get("repoA", "main", "repoB", "main")
	if !ok {
		t.Fatal("(repoA, main, repoB, main) should be a cache hit")
	}
	if len(gotMain) != 1 {
		t.Fatalf("(repoA, main) entry: want 1 link, got %d", len(gotMain))
	}

	gotFoo, ok := cache.Get("repoA", "feature/foo", "repoB", "main")
	if !ok {
		t.Fatal("(repoA, feature/foo, repoB, main) should be a cache hit")
	}
	if len(gotFoo) != 2 {
		t.Fatalf("(repoA, feature/foo) entry: want 2 links, got %d", len(gotFoo))
	}
}

// TestCrossLinkCache_InvalidateRepo verifies that InvalidateRepo removes all
// cache entries referencing the (repo, ref) pair — both as A-side and B-side
// participants — and returns the correct eviction count.
func TestCrossLinkCache_InvalidateRepo(t *testing.T) {
	cache := NewCrossLinkCache()

	links := []CrossRepoLink{{Source: "repoA::X", Target: "repoB::Y", Kind: "call"}}

	// Populate three entries.
	cache.Set("repoA", "main", "repoB", "main", links)        // pair 1: A@main × B@main
	cache.Set("repoA", "feature/foo", "repoB", "main", links) // pair 2: A@foo  × B@main
	cache.Set("repoA", "main", "repoC", "main", links)        // pair 3: A@main × C@main

	if cache.Len() != 3 {
		t.Fatalf("want 3 entries before invalidation, got %d", cache.Len())
	}

	// Invalidate (repoA, main): should evict pairs 1 and 3.
	evicted := cache.InvalidateRepo("repoA", "main")
	if evicted != 2 {
		t.Errorf("InvalidateRepo(repoA, main): want evicted=2, got %d", evicted)
	}

	// pair 2 (repoA, feature/foo) must still be present.
	if cache.Len() != 1 {
		t.Errorf("want 1 entry after invalidation, got %d", cache.Len())
	}
	_, ok := cache.Get("repoA", "feature/foo", "repoB", "main")
	if !ok {
		t.Error("(repoA, feature/foo, repoB, main) should survive invalidation of (repoA, main)")
	}
}

// TestCrossLinkCache_InvalidateRepo_BsideParticipant verifies that
// invalidating (repoB, main) evicts entries where repoB appears on
// EITHER side of the cache key.
func TestCrossLinkCache_InvalidateRepo_BsideParticipant(t *testing.T) {
	cache := NewCrossLinkCache()

	links := []CrossRepoLink{{Source: "repoA::X", Target: "repoB::Y", Kind: "call"}}
	cache.Set("repoA", "main", "repoB", "main", links) // repoB is B-side
	cache.Set("repoB", "main", "repoC", "main", links) // repoB is A-side
	cache.Set("repoA", "main", "repoC", "main", links) // repoB not involved

	evicted := cache.InvalidateRepo("repoB", "main")
	if evicted != 2 {
		t.Errorf("InvalidateRepo(repoB, main): want evicted=2, got %d", evicted)
	}

	if cache.Len() != 1 {
		t.Errorf("want 1 surviving entry, got %d", cache.Len())
	}
	// The entry not involving repoB must survive.
	_, ok := cache.Get("repoA", "main", "repoC", "main")
	if !ok {
		t.Error("(repoA, main, repoC, main) should survive repoB invalidation")
	}
}

// TestCrossLinkCache_InvalidateRepo_NoOp verifies that invalidating a
// (repo, ref) pair that has no cached entries is a safe no-op.
func TestCrossLinkCache_InvalidateRepo_NoOp(t *testing.T) {
	cache := NewCrossLinkCache()

	// Empty cache: invalidate on a non-existent repo must not panic.
	evicted := cache.InvalidateRepo("nonexistent-repo", "main")
	if evicted != 0 {
		t.Errorf("empty cache: want evicted=0, got %d", evicted)
	}

	// Non-empty cache, but the target (repo, ref) is not referenced.
	cache.Set("repoA", "main", "repoB", "main", nil)
	evicted = cache.InvalidateRepo("repoX", "main")
	if evicted != 0 {
		t.Errorf("miss: want evicted=0, got %d", evicted)
	}
	if cache.Len() != 1 {
		t.Errorf("unrelated entry should survive: want 1, got %d", cache.Len())
	}
}

// TestCrossLinkCache_EmptyRefIgnored verifies that InvalidateRepo with an
// empty ref string is a safe no-op (empty ref = sentinel "_unknown", which
// is not a valid ref-switch target).
func TestCrossLinkCache_EmptyRefIgnored(t *testing.T) {
	cache := NewCrossLinkCache()
	cache.Set("repoA", "", "repoB", "main", nil)
	evicted := cache.InvalidateRepo("repoA", "") // empty ref → no-op
	if evicted != 0 {
		t.Errorf("empty ref: want evicted=0, got %d", evicted)
	}
}

// TestCrossLinkCache_Flush verifies that Flush removes all entries and
// leaves the secondary index empty.
func TestCrossLinkCache_Flush(t *testing.T) {
	cache := NewCrossLinkCache()
	cache.Set("repoA", "main", "repoB", "main", nil)
	cache.Set("repoA", "feature/foo", "repoB", "main", nil)
	cache.Flush()
	if cache.Len() != 0 {
		t.Errorf("after Flush: want 0 entries, got %d", cache.Len())
	}
	// Post-flush operations must not panic.
	evicted := cache.InvalidateRepo("repoA", "main")
	if evicted != 0 {
		t.Errorf("post-flush invalidation: want 0, got %d", evicted)
	}
}

// TestCrossLinkCache_ConcurrentAccess exercises concurrent Get, Set, and
// InvalidateRepo to detect data races under the race detector (-race).
func TestCrossLinkCache_ConcurrentAccess(t *testing.T) {
	cache := NewCrossLinkCache()
	links := []CrossRepoLink{{Source: "repoA::X", Target: "repoB::Y", Kind: "call"}}

	var wg sync.WaitGroup
	const goroutines = 8

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ref := "main"
			if id%2 == 0 {
				ref = "feature/x"
			}
			cache.GetOrCompute("repoA", ref, "repoB", "main", func() []CrossRepoLink {
				return links
			})
			cache.InvalidateRepo("repoA", ref)
			cache.Set("repoA", ref, "repoB", "main", links)
			_, _ = cache.Get("repoA", ref, "repoB", "main")
		}(i)
	}
	wg.Wait()
}

// TestCrossLinkCache_SingleRefScenario verifies that the cache behaves
// identically to the old (non-ref-keyed) behaviour for single-ref
// installations: no false invalidations, no extra fn calls.
func TestCrossLinkCache_SingleRefScenario(t *testing.T) {
	cache := NewCrossLinkCache()
	calls := 0
	result := []CrossRepoLink{
		{Source: "repoA::Svc", Target: "repoB::DB", Kind: "import", Confidence: 0.95},
	}

	// Populate once.
	got := cache.GetOrCompute("repoA", "main", "repoB", "main", func() []CrossRepoLink {
		calls++
		return result
	})
	if calls != 1 {
		t.Fatalf("first call: want fn called 1 time, got %d", calls)
	}
	if len(got) != 1 {
		t.Fatalf("first call: want 1 link, got %d", len(got))
	}

	// Repeat queries should all be cache hits.
	for i := 0; i < 5; i++ {
		got2 := cache.GetOrCompute("repoA", "main", "repoB", "main", func() []CrossRepoLink {
			calls++
			return result
		})
		if calls != 1 {
			t.Errorf("repeat query %d: fn should not have been called again; calls=%d", i, calls)
		}
		if len(got2) != 1 {
			t.Errorf("repeat query %d: want 1 link, got %d", i, len(got2))
		}
	}

	// No ref-switch: InvalidateRepo for an unrelated ref must not evict.
	evicted := cache.InvalidateRepo("repoA", "feature/unrelated")
	if evicted != 0 {
		t.Errorf("unrelated ref: want evicted=0, got %d", evicted)
	}
	if cache.Len() != 1 {
		t.Errorf("unrelated invalidation must not evict; want 1 entry, got %d", cache.Len())
	}
}
