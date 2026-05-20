package graph

import (
	"fmt"
	"math"
	"testing"
)

// makeEntities builds a slice of Entity stubs with the given IDs. Other fields
// are populated minimally — the algorithms only need ID + Name.
func makeEntities(ids ...string) []Entity {
	out := make([]Entity, 0, len(ids))
	for _, id := range ids {
		out = append(out, Entity{ID: id, Name: id, Kind: "function"})
	}
	return out
}

// rel builds an undirected-flavoured relationship; algorithms use the directed
// graph for PageRank but the community / articulation pieces project to
// undirected so a single edge per logical pair is sufficient.
func rel(from, to string) Relationship {
	return Relationship{ID: from + "->" + to, FromID: from, ToID: to, Kind: "CALLS"}
}

func relW(from, to string, calls int) Relationship {
	r := rel(from, to)
	r.Properties = map[string]string{"callsite_count": itoa(calls)}
	return r
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	out := ""
	for n > 0 {
		out = string(rune('0'+n%10)) + out
		n /= 10
	}
	return out
}

// TestLouvainTwoCommunities — 4-node graph with two obvious clusters
// (A-B densely linked, C-D densely linked, single bridge B-C). Louvain
// should split A,B from C,D.
func TestLouvainTwoCommunities(t *testing.T) {
	ents := makeEntities("A", "B", "C", "D")
	rels := []Relationship{
		rel("A", "B"), rel("B", "A"),
		rel("C", "D"), rel("D", "C"),
		rel("B", "C"),
	}
	res := RunAlgorithms(ents, rels)
	if len(res.Communities) < 2 {
		t.Fatalf("expected >= 2 communities, got %d", len(res.Communities))
	}
	if res.CommunityID["A"] != res.CommunityID["B"] {
		t.Errorf("A and B should share a community, got %d vs %d",
			res.CommunityID["A"], res.CommunityID["B"])
	}
	if res.CommunityID["C"] != res.CommunityID["D"] {
		t.Errorf("C and D should share a community, got %d vs %d",
			res.CommunityID["C"], res.CommunityID["D"])
	}
	if res.CommunityID["A"] == res.CommunityID["C"] {
		t.Error("A and C should be in different communities")
	}
}

// TestPageRankStarGraph — center connected to 4 leaves; PageRank of center
// should exceed PageRank of any leaf.
func TestPageRankStarGraph(t *testing.T) {
	ents := makeEntities("CENTER", "L1", "L2", "L3", "L4")
	rels := []Relationship{
		rel("L1", "CENTER"), rel("L2", "CENTER"),
		rel("L3", "CENTER"), rel("L4", "CENTER"),
	}
	res := RunAlgorithms(ents, rels)
	cpr := res.PageRank["CENTER"]
	for _, leaf := range []string{"L1", "L2", "L3", "L4"} {
		if res.PageRank[leaf] >= cpr {
			t.Errorf("leaf %s has PR %f >= center PR %f", leaf, res.PageRank[leaf], cpr)
		}
	}
}

// TestBetweennessPathGraph — 1-2-3-4-5; betweenness peaks at the middle node.
func TestBetweennessPathGraph(t *testing.T) {
	ents := makeEntities("1", "2", "3", "4", "5")
	rels := []Relationship{
		rel("1", "2"), rel("2", "1"),
		rel("2", "3"), rel("3", "2"),
		rel("3", "4"), rel("4", "3"),
		rel("4", "5"), rel("5", "4"),
	}
	res := RunAlgorithms(ents, rels)
	mid := res.Centrality["3"]
	for _, other := range []string{"1", "2", "4", "5"} {
		if res.Centrality[other] >= mid {
			t.Errorf("node %s centrality %f >= middle %f", other, res.Centrality[other], mid)
		}
	}
}

// TestArticulationBridge — two triangles connected via a single bridge node.
// The bridge node must be flagged as an articulation point.
func TestArticulationBridge(t *testing.T) {
	ents := makeEntities("A1", "A2", "A3", "BRIDGE", "B1", "B2", "B3")
	rels := []Relationship{
		rel("A1", "A2"), rel("A2", "A3"), rel("A3", "A1"),
		rel("A1", "BRIDGE"),
		rel("BRIDGE", "B1"),
		rel("B1", "B2"), rel("B2", "B3"), rel("B3", "B1"),
	}
	res := RunAlgorithms(ents, rels)
	if !res.ArticulationPoints["BRIDGE"] {
		t.Errorf("BRIDGE not flagged as articulation point; got %v", res.ArticulationPoints)
	}
}

// TestSurpriseEdges — two dense 3-cliques connected by a single edge. That
// single edge should be flagged as a surprise.
func TestSurpriseEdges(t *testing.T) {
	ents := makeEntities("A1", "A2", "A3", "B1", "B2", "B3")
	rels := []Relationship{
		rel("A1", "A2"), rel("A2", "A3"), rel("A3", "A1"),
		rel("B1", "B2"), rel("B2", "B3"), rel("B3", "B1"),
		rel("A1", "B1"), // the lone cross edge
	}
	res := RunAlgorithms(ents, rels)
	if len(res.SurpriseEdges) == 0 {
		t.Fatalf("expected at least one surprise edge")
	}
	found := false
	for _, s := range res.SurpriseEdges {
		if s.FromID == "A1" && s.ToID == "B1" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("A1->B1 not flagged as surprise; got %v", res.SurpriseEdges)
	}
	if !res.SurpriseEndpoints["A1"] || !res.SurpriseEndpoints["B1"] {
		t.Errorf("surprise endpoints not flagged: %v", res.SurpriseEndpoints)
	}
}

// TestEdgeWeightingAffectsCentrality — same topology, different weights.
// Heavier weights on a path should *reduce* shortest-path use elsewhere.
// We verify that betweenness is *not* identical when weights change.
func TestEdgeWeightingAffectsCentrality(t *testing.T) {
	ents := makeEntities("S", "A", "B", "T")
	// Two parallel 2-hop routes from S to T: via A or via B.
	relsLight := []Relationship{
		rel("S", "A"), rel("A", "T"),
		rel("S", "B"), rel("B", "T"),
	}
	relsHeavyA := []Relationship{
		relW("S", "A", 100), relW("A", "T", 100),
		rel("S", "B"), rel("B", "T"),
	}
	r1 := RunAlgorithms(ents, relsLight)
	r2 := RunAlgorithms(ents, relsHeavyA)
	// Heavily-weighted edges *cost more* in shortest-path distance, so traffic
	// shifts toward B; centrality of A should drop relative to B.
	if r1.Centrality["A"] == r2.Centrality["A"] && r1.Centrality["B"] == r2.Centrality["B"] {
		t.Errorf("centrality scores identical despite weight change: %v vs %v", r1, r2)
	}
}

// TestAlgorithmStatsPopulated — RunAlgorithms must populate every stat field.
func TestAlgorithmStatsPopulated(t *testing.T) {
	ents := makeEntities("A", "B", "C", "D", "E", "F")
	rels := []Relationship{
		rel("A", "B"), rel("B", "C"), rel("C", "A"),
		rel("D", "E"), rel("E", "F"), rel("F", "D"),
		rel("A", "D"),
	}
	res := RunAlgorithms(ents, rels)
	if res.Stats.NumCommunities == 0 {
		t.Error("NumCommunities should be > 0")
	}
	if res.Stats.RuntimeMS < 0 {
		t.Error("RuntimeMS should be >= 0")
	}
}

// makeLargeGraph constructs a synthetic graph with n nodes arranged in
// overlapping cliques and random-ish cross-links, mimicking the structure of
// real code corpora (gin ~6 k nodes, spdlog ~1.8 k nodes) where PageRank
// float drift was observed crossing the 1e-5 rounding boundary.
//
// The topology is a ring of size-8 cliques with every clique connected to the
// next via a single bridge node. This produces a mix of high-degree hub nodes
// (inside cliques) and low-degree bridge nodes — exactly the shapes where
// PageRankSparse summation order matters.
func makeLargeGraph(cliqueCount int) ([]Entity, []Relationship) {
	nodes := cliqueCount * 8
	ids := make([]string, nodes)
	for i := range ids {
		ids[i] = fmt.Sprintf("e%04d", i)
	}
	ents := makeEntities(ids...)

	var rels []Relationship
	for c := 0; c < cliqueCount; c++ {
		base := c * 8
		// fully-connected clique of 8
		for i := 0; i < 8; i++ {
			for j := 0; j < 8; j++ {
				if i == j {
					continue
				}
				rels = append(rels, rel(ids[base+i], ids[base+j]))
			}
		}
		// bridge to next clique
		next := (c + 1) % cliqueCount
		rels = append(rels, rel(ids[base], ids[next*8]))
	}
	return ents, rels
}

// TestDeterminism_PageRank — issue #489. Run ComputeCentrality 10 times on a
// 400-node (50-clique) graph and verify that every run produces byte-identical
// PageRank scores. This catches float drift that crosses the rounding boundary
// introduced by non-deterministic map iteration order inside PageRankSparse.
func TestDeterminism_PageRank(t *testing.T) {
	const runs = 10
	ents, rels := makeLargeGraph(50) // 400 nodes, mimics mid-size real corpus

	g, idx := BuildGraph(ents, rels)

	// Capture baseline on first run.
	_, base := ComputeCentrality(g, idx)

	for i := 1; i < runs; i++ {
		_, pr := ComputeCentrality(g, idx)
		for id, want := range base {
			got := pr[id]
			if got != want {
				t.Errorf("run %d: PageRank[%s] = %v, want %v (delta=%e)",
					i, id, got, want, math.Abs(got-want))
			}
		}
		if t.Failed() {
			t.Fatalf("pagerank is non-deterministic after %d runs — see above", i)
		}
	}
}

// TestRoundForDeterminism_Precision — verify that roundForDeterminism buckets
// values to 4 decimal places (1e-4 tolerance), which is the guarantee
// established by issue #489 to absorb larger-graph float drift.
func TestRoundForDeterminism_Precision(t *testing.T) {
	cases := []struct {
		input float64
		want  float64
	}{
		{0.123456789, 0.1235},
		{0.00001, 0.0},    // below 1e-4 threshold rounds to 0
		{0.00005, 0.0001}, // rounds up to 1e-4
		{0.12344, 0.1234},
		{0.12345, 0.1235}, // rounds half-up
		{0.0, 0.0},
	}
	for _, tc := range cases {
		got := roundForDeterminism(tc.input)
		if got != tc.want {
			t.Errorf("roundForDeterminism(%v) = %v, want %v", tc.input, got, tc.want)
		}
	}
}
