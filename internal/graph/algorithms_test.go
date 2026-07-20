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
	r.PropsReplace(map[string]string{"callsite_count": itoa(calls)})
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
// should split A,B from C,D. Uses MinSize=1 to disable denoising so the
// structural community assignment is testable on small fixtures.
func TestLouvainTwoCommunities(t *testing.T) {
	t.Parallel()
	ents := makeEntities("A", "B", "C", "D")
	rels := []Relationship{
		rel("A", "B"), rel("B", "A"),
		rel("C", "D"), rel("D", "C"),
		rel("B", "C"),
	}
	res := RunAlgorithmsWithOptions(ents, rels, CommunityOptions{MinSize: 1})
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
// single edge should be flagged as a surprise. Uses MinSize=1 to disable
// denoising so the cross-community edge detection works on small fixtures.
func TestSurpriseEdges(t *testing.T) {
	t.Parallel()
	ents := makeEntities("A1", "A2", "A3", "B1", "B2", "B3")
	rels := []Relationship{
		rel("A1", "A2"), rel("A2", "A3"), rel("A3", "A1"),
		rel("B1", "B2"), rel("B2", "B3"), rel("B3", "B1"),
		rel("A1", "B1"), // the lone cross edge
	}
	res := RunAlgorithmsWithOptions(ents, rels, CommunityOptions{MinSize: 1})
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
	t.Parallel()
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
// Uses MinSize=1 so small fixtures (2×3-node communities) are not denoised.
func TestAlgorithmStatsPopulated(t *testing.T) {
	t.Parallel()
	ents := makeEntities("A", "B", "C", "D", "E", "F")
	rels := []Relationship{
		rel("A", "B"), rel("B", "C"), rel("C", "A"),
		rel("D", "E"), rel("E", "F"), rel("F", "D"),
		rel("A", "D"),
	}
	res := RunAlgorithmsWithOptions(ents, rels, CommunityOptions{MinSize: 1})
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
	t.Parallel()
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

// TestRoundForDeterminism_Precision — verify that roundForDeterminism rounds to
// 6 SIGNIFICANT figures (relative precision), which absorbs the ~1e-6 solver
// noise from issue #481/#489 on every graph size WITHOUT collapsing small
// scores to 0 (flaw 4: large group unions have god-node pageranks < 1e-4 that
// the old absolute-1e-4 rounding wrongly truncated to 0).
func TestRoundForDeterminism_Precision(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input float64
		want  float64
	}{
		// |v| >= 1e-3: absolute 1e-4 bucket (the proven #489 determinism path).
		{0.123456789, 0.1235},
		{0.12344, 0.1234},
		{0.12345, 0.1235}, // rounds half-up
		{0.0015, 0.0015},
		{0.0, 0.0},
		// |v| < 1e-3: 4 significant figures. Small scores typical of a 28k-node
		// group union are PRESERVED (non-zero), not truncated to 0 like the old
		// absolute-1e-4 rounding did.
		{0.00003571, 0.00003571},    // god-node pagerank ~3.5e-5 survives
		{0.000012345678, 1.235e-05}, // 4 sig figs, rounds half-up at the 5th
		{0.0009994, 0.0009994},      // just below the 1e-3 cutoff: 4 sig figs
		{1e-9, 1e-9},
	}
	for _, tc := range cases {
		got := roundForDeterminism(tc.input)
		if got != tc.want {
			t.Errorf("roundForDeterminism(%v) = %v, want %v", tc.input, got, tc.want)
		}
		// Regression: a non-zero input must never round to exactly 0.
		if tc.input != 0 && got == 0 {
			t.Errorf("roundForDeterminism(%v) collapsed a non-zero score to 0", tc.input)
		}
	}
}

// TestDefaultCommunityOptions — DefaultCommunityOptions must set MinSize=5.
func TestDefaultCommunityOptions(t *testing.T) {
	t.Parallel()
	opts := DefaultCommunityOptions()
	if opts.MinSize != 5 {
		t.Errorf("DefaultCommunityOptions().MinSize = %d, want 5", opts.MinSize)
	}
}

// TestDenoise_SingletonsMergedToUngrouped — a graph with two dense clusters of
// 8 nodes each plus two isolated singleton nodes. With MinSize=5, the
// singletons should be assigned community_id=-1 and not appear in
// Communities. The two large clusters should survive.
func TestDenoise_SingletonsMergedToUngrouped(t *testing.T) {
	t.Parallel()
	// Build two 8-cliques (each node connects to all 7 others) plus 2 singletons.
	var ids []string
	var rels []Relationship
	clique := func(prefix string, n int) []string {
		ns := make([]string, n)
		for i := range ns {
			ns[i] = fmt.Sprintf("%s%d", prefix, i)
			ids = append(ids, ns[i])
		}
		// fully connected
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				if i != j {
					rels = append(rels, rel(ns[i], ns[j]))
				}
			}
		}
		return ns
	}
	clique("X", 8) // cluster X
	clique("Y", 8) // cluster Y
	// Two isolated singletons (no edges).
	ids = append(ids, "SOLO1", "SOLO2")

	ents := makeEntities(ids...)
	res := RunAlgorithmsWithOptions(ents, rels, CommunityOptions{MinSize: 5})

	// Singletons must be ungrouped.
	if res.CommunityID["SOLO1"] != -1 {
		t.Errorf("SOLO1 expected community -1 (ungrouped), got %d", res.CommunityID["SOLO1"])
	}
	if res.CommunityID["SOLO2"] != -1 {
		t.Errorf("SOLO2 expected community -1 (ungrouped), got %d", res.CommunityID["SOLO2"])
	}

	// The two 8-cliques should appear in Communities (size >= 5).
	if len(res.Communities) < 2 {
		t.Fatalf("expected >= 2 named communities, got %d", len(res.Communities))
	}
	for _, c := range res.Communities {
		if c.Size < 5 {
			t.Errorf("community %d has size %d < MinSize 5 (should have been denoised)", c.ID, c.Size)
		}
	}

	// DenoisedCommunities in stats should reflect the dropped singletons.
	if res.Stats.DenoisedCommunities == 0 {
		t.Error("expected DenoisedCommunities > 0 (singletons should have been denoised)")
	}
}

// TestDenoise_MinSizeOne_NoDenoise — with MinSize=1, no communities are dropped
// even for a graph where every node is isolated.
func TestDenoise_MinSizeOne_NoDenoise(t *testing.T) {
	t.Parallel()
	ents := makeEntities("A", "B", "C")
	// No edges: every node is isolated → 3 singleton communities.
	res := RunAlgorithmsWithOptions(ents, nil, CommunityOptions{MinSize: 1})
	if res.Stats.DenoisedCommunities != 0 {
		t.Errorf("MinSize=1 should not denoise anything, got DenoisedCommunities=%d",
			res.Stats.DenoisedCommunities)
	}
}

// TestDenoise_DefaultOptions_MatchesBehavior — RunAlgorithms (which uses
// DefaultCommunityOptions) should produce fewer or equal named communities
// compared to MinSize=1 on a graph that has small communities.
func TestDenoise_DefaultOptions_MatchesBehavior(t *testing.T) {
	t.Parallel()
	// Ring of 5-node cliques → mix of reasonable communities.
	ents, rels := makeLargeGraph(4) // 4 cliques × 8 nodes = 32 nodes
	// Add extra 3 isolated nodes to ensure singletons exist.
	ents = append(ents, makeEntities("ISO1", "ISO2", "ISO3")...)

	resDefault := RunAlgorithms(ents, rels)                                           // MinSize=5
	resNoFilter := RunAlgorithmsWithOptions(ents, rels, CommunityOptions{MinSize: 1}) // no denoise

	if len(resDefault.Communities) > len(resNoFilter.Communities) {
		t.Errorf("default (MinSize=5) has MORE communities (%d) than MinSize=1 (%d) — denoise logic inverted",
			len(resDefault.Communities), len(resNoFilter.Communities))
	}
}

// TestDenoise_Determinism — running denoise twice on the same graph must
// produce byte-identical community assignments.
func TestDenoise_Determinism(t *testing.T) {
	t.Parallel()
	ents, rels := makeLargeGraph(10) // 80 nodes
	// Add isolated singletons to ensure denoising is exercised.
	ents = append(ents, makeEntities("X1", "X2", "X3")...)

	r1 := RunAlgorithms(ents, rels)
	r2 := RunAlgorithms(ents, rels)

	if len(r1.Communities) != len(r2.Communities) {
		t.Fatalf("community count changed: %d vs %d", len(r1.Communities), len(r2.Communities))
	}
	for id := range r1.CommunityID {
		if r1.CommunityID[id] != r2.CommunityID[id] {
			t.Errorf("community_id[%s] = %d vs %d across runs", id, r1.CommunityID[id], r2.CommunityID[id])
		}
	}
	if r1.Stats.DenoisedCommunities != r2.Stats.DenoisedCommunities {
		t.Errorf("DenoisedCommunities differs: %d vs %d", r1.Stats.DenoisedCommunities, r2.Stats.DenoisedCommunities)
	}
}

// TestRunAlgorithmsWithOptions_EmptyDoc verifies that passing zero entities does
// not panic (regression for #937/#1795: gonum's PageRankSparse → mat.NewVecDense(0)
// panicked with "mat: zero length in matrix dimension") and returns an empty,
// well-formed AlgorithmResults.
func TestRunAlgorithmsWithOptions_EmptyDoc(t *testing.T) {
	t.Parallel()
	// Must not panic.
	res := RunAlgorithmsWithOptions(nil, nil, DefaultCommunityOptions())

	if res == nil {
		t.Fatal("expected non-nil AlgorithmResults for empty doc, got nil")
	}
	if len(res.Communities) != 0 {
		t.Errorf("expected 0 communities for empty doc, got %d", len(res.Communities))
	}
	if len(res.CommunityID) != 0 {
		t.Errorf("expected empty CommunityID map for empty doc, got %d entries", len(res.CommunityID))
	}
	if len(res.GodNodes) != 0 {
		t.Errorf("expected no god nodes for empty doc, got %d", len(res.GodNodes))
	}
	if len(res.ArticulationPoints) != 0 {
		t.Errorf("expected no articulation points for empty doc, got %d", len(res.ArticulationPoints))
	}
	if len(res.SurpriseEdges) != 0 {
		t.Errorf("expected no surprise edges for empty doc, got %d", len(res.SurpriseEdges))
	}

	// Also exercise the convenience wrapper.
	res2 := RunAlgorithms(nil, nil)
	if res2 == nil {
		t.Fatal("RunAlgorithms: expected non-nil result for empty doc")
	}
}
