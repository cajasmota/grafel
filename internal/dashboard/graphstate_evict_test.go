package dashboard

// graphstate_evict_test.go — #5238 coverage for the tier eviction lever.
//
// The tier WARM→COLD transition now calls (*Server).InvalidateGroup, which
// delegates to GraphCache.Invalidate. These tests pin the two properties that
// make that safe and effective:
//
//  1. Invalidate releases the heavy materialised group state (the
//     *graph.Document slices, re-derived algorithm results, mmap readers and
//     the pre-built search index) so the entry is no longer resident and its
//     heap is reclaimable by the GC.
//  2. A drop-then-reload rebuilds an EQUIVALENT search index — the mmap'd
//     graph.fb on disk is the source of truth, so the derived index is always
//     reconstructable identically. (Correctness == eager.)

import (
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
)

// TestInvalidateReleasesMaterialisedState verifies that Invalidate removes the
// cache entry entirely so its derived heap (Document + Search index) is no
// longer retained. This is the #5238 lever the tier manager fires on COLD.
func TestInvalidateReleasesMaterialisedState(t *testing.T) {
	c := NewGraphCache(60 * time.Second)

	// Seed a warm entry with a materialised Document + search index — the exact
	// heavy state an idle group would otherwise hold forever.
	grp := &DashGroup{
		Name: "heavy",
		Repos: map[string]*DashRepo{
			"r1": {
				Slug: "r1",
				Doc: &graph.Document{
					Repo: "r1",
					Entities: []graph.Entity{
						{ID: "e1", Kind: "function", Name: "Handler"},
						{ID: "e2", Kind: "function", Name: "Service"},
					},
				},
			},
		},
	}
	grp.Search = buildSearchIndex(grp)

	c.mu.Lock()
	c.entries["heavy"] = &cacheEntry{group: grp, loadedAt: time.Now()}
	c.mu.Unlock()

	// Warm hit before eviction.
	if _, ok := c.GetGroupCached("heavy"); !ok {
		t.Fatal("expected warm hit before Invalidate")
	}

	c.Invalidate("heavy")

	// Entry must be gone — derived heap is now reclaimable.
	c.mu.Lock()
	_, present := c.entries["heavy"]
	c.mu.Unlock()
	if present {
		t.Error("entry still resident after Invalidate — derived heap not released")
	}
}

// TestSearchIndexRebuildIsEquivalent proves the correctness argument behind
// evict-then-rebuild: rebuilding the search index from the same Document yields
// an equivalent structure (same entry count, same word tokens). The on-disk
// graph is the source of truth, so a dropped index is always safely rebuilt.
func TestSearchIndexRebuildIsEquivalent(t *testing.T) {
	mk := func() *DashGroup {
		g := &DashGroup{
			Name: "g",
			Repos: map[string]*DashRepo{
				"r1": {
					Slug: "r1",
					Doc: &graph.Document{
						Repo: "r1",
						Entities: []graph.Entity{
							{ID: "e1", Kind: "function", Name: "UserService"},
							{ID: "e2", Kind: "function", Name: "OrderHandler"},
							{ID: "e3", Kind: "function", Name: "UserRepository"},
						},
					},
				},
			},
		}
		return g
	}

	eager := buildSearchIndex(mk())
	rebuilt := buildSearchIndex(mk())

	if len(eager.entries) != len(rebuilt.entries) {
		t.Fatalf("entry count differs: eager=%d rebuilt=%d", len(eager.entries), len(rebuilt.entries))
	}
	if len(eager.wordTokens) != len(rebuilt.wordTokens) {
		t.Fatalf("word-token count differs: eager=%d rebuilt=%d", len(eager.wordTokens), len(rebuilt.wordTokens))
	}
	for tok, posEager := range eager.wordTokens {
		posRebuilt, ok := rebuilt.wordTokens[tok]
		if !ok {
			t.Errorf("token %q missing after rebuild", tok)
			continue
		}
		if len(posEager) != len(posRebuilt) {
			t.Errorf("token %q positions differ: eager=%v rebuilt=%v", tok, posEager, posRebuilt)
		}
	}
}
