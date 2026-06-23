package groupalgo

import (
	"fmt"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// TestBuildOverlay_GodNodesHaveNonZeroPageRank is the flaw-4 regression.
//
// On a large group union the PageRank mass (which sums to 1) is spread over
// thousands of nodes, so every individual score — INCLUDING the top-5%
// god-nodes — is small in absolute terms (often well below 1e-4). The old
// roundForDeterminism rounded to a fixed 1e-4 ABSOLUTE bucket, which collapsed
// those small-but-meaningful scores to 0. The overlay then reported a flagged
// god-node with pagerank 0 — a direct contradiction (a god-node is by
// definition among the MOST central entities, so its PageRank must be high,
// never 0).
//
// The fix rounds to significant figures (relative precision) instead, so small
// scores survive. This test builds a real overlay from a synthetic graph big
// enough that god-node pageranks are below 1e-4, then asserts:
//   - every is_god entity has a NON-ZERO pagerank (the regression),
//   - that pagerank equals the value computed by the algorithm pass,
//   - at least one god-node's pagerank is below 1e-4 (so the old absolute
//     rounding WOULD have zeroed it) yet survives here.
//
// (A god-node can be flagged via top-5% betweenness rather than top-5%
// pagerank, so it is NOT required to be among the very highest pageranks — only
// that whatever its real pagerank is, the overlay reports it and it is not 0.)
func TestBuildOverlay_GodNodesHaveNonZeroPageRank(t *testing.T) {
	t.Parallel()

	// Build a hub-and-spoke graph: a handful of hubs each receive edges from
	// many distinct leaves. With several thousand nodes the PageRank mass is
	// spread thin enough that god-node scores land below the old 1e-4
	// absolute-rounding bucket — exactly the large-union regime of flaw 4.
	const numHubs = 8
	const leavesPerHub = 1600

	var entities []graph.Entity
	var rels []graph.Relationship
	add := func(id string) {
		entities = append(entities, graph.Entity{ID: id, Name: id, Kind: "function"})
	}

	hubs := make([]string, numHubs)
	for h := 0; h < numHubs; h++ {
		hub := fmt.Sprintf("hub-%d", h)
		hubs[h] = hub
		add(hub)
	}
	for h := 0; h < numHubs; h++ {
		for l := 0; l < leavesPerHub; l++ {
			leaf := fmt.Sprintf("leaf-%d-%d", h, l)
			add(leaf)
			// Leaf -> hub: pumps PageRank into the hub, making it central.
			rels = append(rels, graph.Relationship{
				ID:     fmt.Sprintf("e-%d-%d", h, l),
				FromID: leaf,
				ToID:   hubs[h],
				Kind:   "CALLS",
			})
		}
	}

	res := graph.RunAlgorithms(entities, rels)
	gar := &GroupAlgoResult{
		Group:        "synthetic",
		Results:      res,
		EntityRepo:   map[string]string{},
		SourceMtimes: map[string]int64{},
		NumEntities:  len(entities),
		NumRels:      len(rels),
		NumRepos:     1,
	}

	ov := BuildOverlay(gar)
	if ov == nil {
		t.Fatal("expected non-nil overlay")
	}

	// Sanity: there should be god-nodes, and the synthetic graph is large enough
	// that their pageranks land below the old 1e-4 absolute-rounding bucket
	// (otherwise this test wouldn't exercise the regression).
	godCount := 0
	var maxPR float64
	for _, eo := range ov.Results {
		if eo.PageRank > maxPR {
			maxPR = eo.PageRank
		}
	}
	if maxPR == 0 {
		t.Fatal("no entity has a non-zero pagerank — overlay/algorithm setup is wrong")
	}

	for id, eo := range ov.Results {
		if !eo.IsGodNode {
			continue
		}
		godCount++

		// Regression: a flagged god-node must never report pagerank 0.
		if eo.PageRank == 0 {
			t.Errorf("god-node %q has pagerank 0 (flaw 4 regression): the is_god flag and pagerank are inconsistent", id)
		}

		// The overlay must carry the ACTUAL value the algorithm computed.
		if got, want := eo.PageRank, res.PageRank[id]; got != want {
			t.Errorf("god-node %q overlay pagerank=%v, want computed %v", id, got, want)
		}
	}

	if godCount == 0 {
		t.Fatal("expected at least one god-node in the overlay")
	}

	// Demonstrate the regression is real: at least one god-node pagerank is
	// below 5e-5, which the OLD math.Round(v*1e4)/1e4 rounding would have
	// truncated to exactly 0 (round(0.49)/1e4 == 0). Under the significant-figure
	// fix it survives as a non-zero value (the no-zero invariant above proves it).
	const oldRoundingZeroThreshold = 5e-5
	zeroedByOld := 0
	for _, eo := range ov.Results {
		if eo.IsGodNode && eo.PageRank > 0 && eo.PageRank < oldRoundingZeroThreshold {
			zeroedByOld++
		}
	}
	if zeroedByOld == 0 {
		t.Fatalf("test no longer exercises flaw 4: no god-node pagerank fell below %v (the old rounding's zero threshold) — make the graph larger", oldRoundingZeroThreshold)
	}
	t.Logf("flaw-4 coverage: %d/%d god-nodes have a pagerank the old 1e-4 rounding would have zeroed; all now carry their real value", zeroedByOld, godCount)
}
