package membench

import (
	"runtime"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// TestAlgoResultMapFootprint measures the allocation cost of the Pass-4
// algorithm sweep and pins the two structural properties that #5954 slice-2
// depends on:
//
//  1. The betweenness map returned by ComputeCentrality is SPARSE — gonum
//     reports non-zero scores only, so an entry per entity means storing zeros
//     nobody wrote. Consumers read an absent key as 0.
//  2. RunAlgorithms must not build an entity-name map: community naming needs
//     at most 5 names per community, indexed off the node index.
//
// Property 1 is directly assertable (map length) and is the mutation gate: it
// fails immediately if the zero pre-seed is restored. Property 2 is enforced by
// ComputeCommunities' signature (a []string indexed by node id, not a
// map[string]string).
//
// SIZE OF THE PRIZE, measured on this fixture at 433,333 entities: the sweep
// allocates ~40 GB cumulatively and the two maps are worth ~17 MB of it, so the
// bytes/allocs are LOGGED rather than asserted — a tight bound on a 40 GB total
// would pin nothing. Note that betweenness turns out to be non-zero for ~91% of
// nodes on a realistically-connected graph (394,494 of 433,333 here), so the
// sparse map is only ~9% smaller than the pre-seeded one; the entity-name map
// (21.5 MB -> 6.6 MB) is where most of the saving is.
func TestAlgoResultMapFootprint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping heavy memory bench in -short")
	}
	spec := specFromEnv()
	doc := BuildSyntheticDocument(spec)
	nEntities := len(doc.Entities)

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	res := graph.RunAlgorithms(doc.Entities, doc.Relationships)

	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	// Live heap with the result still referenced: what the caller keeps paying.
	runtime.GC()
	var live runtime.MemStats
	runtime.ReadMemStats(&live)

	t.Logf("ALGO-MAPS entities=%d rels=%d", nEntities, len(doc.Relationships))
	t.Logf("  total-alloc delta = %d MB (%d allocs)",
		(after.TotalAlloc-before.TotalAlloc)/(1024*1024), after.Mallocs-before.Mallocs)
	t.Logf("  live heap holding result = %d MB", live.HeapAlloc/(1024*1024))
	t.Logf("  len(PageRank)=%d len(Centrality)=%d len(CommunityID)=%d",
		len(res.PageRank), len(res.Centrality), len(res.CommunityID))

	// PageRank is dense by construction (gonum returns a score for every node).
	if len(res.PageRank) != nEntities {
		t.Errorf("PageRank map = %d entries, want one per entity (%d)", len(res.PageRank), nEntities)
	}
	// Betweenness must NOT be pre-seeded with a zero for every entity.
	if len(res.Centrality) >= nEntities {
		t.Errorf("Centrality map = %d entries for %d entities: betweenness map is dense; "+
			"the zero pre-seed must be gone (absent key == 0)", len(res.Centrality), nEntities)
	}

	runtime.KeepAlive(doc)
	runtime.KeepAlive(res)
}
