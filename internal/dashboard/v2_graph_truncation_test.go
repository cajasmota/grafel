package dashboard

// v2_graph_truncation_test.go — the OVER-firing direction of the v2 graph
// truncation metadata (#7146 item 1).
//
// The pre-existing truncation tests all assert `EdgeTruncated == true` on a
// payload that really was truncated, which a stuck-true flag satisfies: forcing
// `EdgeTruncated: true` in buildV2GraphWithLimits survived the whole
// ./internal/dashboard/ suite. `NodeTruncated` had its negative assertion
// (v2_graph_test.go asserts `!got.NodeTruncated` on the edge-cap-only case);
// edges had none anywhere in the package. TestV2GraphCompleteGraphReportsNoEdgeTruncation
// is that missing negative, taken through the production HTTP handler.
//
// TestEdgeTruncatedEqualsServedEdgeDeficit is the enumeration behind the
// equivalence note on the `edgeCapTruncated ||` arm in v2_graph.go.

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// TestV2GraphCompleteGraphReportsNoEdgeTruncation serves a graph that is under
// BOTH caps through the real handler and asserts the payload says so. The
// fixture size is checked against lodLimits("full") in the test body, so if a
// cap is ever lowered below the fixture the test fails loudly instead of
// quietly turning into a truncated-graph test that asserts the opposite.
func TestV2GraphCompleteGraphReportsNoEdgeTruncation(t *testing.T) {
	const (
		entityCount       = 40
		relationshipCount = 60
	)
	nodeCap, edgeCap := lodLimits("full")
	if entityCount >= nodeCap || relationshipCount >= edgeCap {
		t.Fatalf("fixture is not under both caps: %d nodes vs cap %d, %d edges vs cap %d",
			entityCount, nodeCap, relationshipCount, edgeCap)
	}

	entities := make([]graph.Entity, entityCount)
	for i := range entities {
		rank := float64(i) / float64(entityCount)
		entities[i] = graph.Entity{ID: fmt.Sprintf("e%d", i), Kind: "function", PageRank: &rank}
	}
	// Distinct (from,to) pairs so no two relationships collapse into one wire
	// edge — len(Edges) must equal relationshipCount exactly.
	rels := make([]graph.Relationship, 0, relationshipCount)
	for i := 0; len(rels) < relationshipCount; i++ {
		from := i % entityCount
		to := (from + 1 + i/entityCount) % entityCount
		if from == to {
			continue
		}
		rels = append(rels, graph.Relationship{FromID: entities[from].ID, ToID: entities[to].ID, Kind: "CALLS"})
	}

	ts := newV2GraphTestServerWithGroup(t, makeGraphTestGroup(entities, rels))
	got := fetchV2Graph(t, ts, "full")

	if got.TotalNodeCount != entityCount || len(got.Nodes) != entityCount {
		t.Fatalf("nodes = %d served / %d total, want %d/%d",
			len(got.Nodes), got.TotalNodeCount, entityCount, entityCount)
	}
	if got.TotalEdgeCount != relationshipCount || len(got.Edges) != relationshipCount {
		t.Fatalf("edges = %d served / %d total, want %d/%d",
			len(got.Edges), got.TotalEdgeCount, relationshipCount, relationshipCount)
	}
	// The point of the test: nothing was dropped, so BOTH flags must be false.
	if got.NodeTruncated {
		t.Errorf("node_truncated = true on a complete graph (%d/%d nodes served)", len(got.Nodes), got.TotalNodeCount)
	}
	if got.EdgeTruncated {
		t.Errorf("edge_truncated = true on a complete graph (%d/%d edges served)", len(got.Edges), got.TotalEdgeCount)
	}
}

// TestEdgeTruncatedEqualsServedEdgeDeficit enumerates the reachable
// (node layout, edge layout, nodeCap, edgeCap) state space of
// buildV2GraphWithLimits and asserts the served payload's EdgeTruncated always
// equals `TotalEdgeCount > len(Edges)` — i.e. the flag means exactly "fewer
// edges were served than exist".
//
// This is the measurement behind the equivalence note on the
// `edgeCapTruncated ||` arm (#7146): if any reachable state made
// edgeCapTruncated true while TotalEdgeCount == len(Edges), that state would
// fail here and the arm would be load-bearing. The test also refuses to pass
// vacuously: it requires the enumeration to have actually produced all five
// truncation buckets (edgeless / neither / edge-cap only / node-thinning only /
// both), and requires those five to SUM to the case count, so a reader doing
// the arithmetic is not left with an unexplained remainder.
//
// What this test does NOT grade, said plainly so it is not read into the case
// count: the invariant compares two quantities both derived from the same
// served payload, so it is cap-VALUE-agnostic by construction and survives any
// mutant that only changes a cap's magnitude. Cap values are graded by
// TestBuildV2GraphMetadataReportsEdgeCapWithoutNodeThinning and the LoD tests.
func TestEdgeTruncatedEqualsServedEdgeDeficit(t *testing.T) {
	entityCounts := []int{1, 2, 3, 4, 6, 9}
	nodeCaps := []int{0, 1, 2, 3, 5, 9, 1000}
	edgeCaps := []int{0, 1, 2, 3, 5, 1000}
	layouts := edgeLayoutsForTruncationEnumeration()
	splits := []bool{false, true}

	cases := 0
	// The buckets PARTITION the space: every case lands in exactly one, and
	// the totals are asserted to sum to `cases` below. `edgeless` is the
	// TotalEdgeCount == 0 states — the invariant is asserted there too (and a
	// stuck-true flag dies there first), they simply cannot be "not truncated
	// but had edges", so they need their own arm rather than falling through.
	var buckets struct{ edgeless, none, capOnly, thinOnly, both int }
	sawEdges := false
	for _, entityCount := range entityCounts {
		for _, layout := range layouts {
			for _, split := range splits {
				grp := makeTruncationEnumerationGroup(entityCount, layout, split)
				for _, nodeCap := range nodeCaps {
					for _, edgeCap := range edgeCaps {
						cases++
						got := (&Server{}).buildV2GraphWithLimits(sortedRepos(grp), grp, "", false, false, nodeCap, edgeCap)
						deficit := got.TotalEdgeCount > len(got.Edges)
						if got.EdgeTruncated != deficit {
							t.Fatalf("distinguishing state: entities=%d layout=%q split=%v nodeCap=%d edgeCap=%d -> edge_truncated=%v but total=%d served=%d",
								entityCount, layout.name, split, nodeCap, edgeCap,
								got.EdgeTruncated, got.TotalEdgeCount, len(got.Edges))
						}
						if got.TotalEdgeCount > 0 {
							sawEdges = true
						}
						switch {
						case got.TotalEdgeCount == 0:
							buckets.edgeless++
						case !got.EdgeTruncated && got.TotalEdgeCount > 0:
							buckets.none++
						case got.EdgeTruncated && got.NodeTruncated && edgeCap > 0 && len(got.Edges) == edgeCap:
							buckets.both++
						case got.EdgeTruncated && got.NodeTruncated:
							buckets.thinOnly++
						case got.EdgeTruncated:
							buckets.capOnly++
						}
					}
				}
			}
		}
	}

	if !sawEdges {
		t.Fatalf("enumeration produced no edges at all in %d cases", cases)
	}
	if buckets.none == 0 || buckets.capOnly == 0 || buckets.thinOnly == 0 || buckets.both == 0 || buckets.edgeless == 0 {
		t.Fatalf("enumeration is vacuous: %d cases, buckets edgeless=%d none=%d cap-only=%d thin-only=%d both=%d — every bucket must be non-empty",
			cases, buckets.edgeless, buckets.none, buckets.capOnly, buckets.thinOnly, buckets.both)
	}
	if sum := buckets.edgeless + buckets.none + buckets.capOnly + buckets.thinOnly + buckets.both; sum != cases {
		t.Fatalf("buckets do not partition the space: %d classified vs %d cases", sum, cases)
	}
	t.Logf("%d cases, buckets edgeless=%d none=%d cap-only=%d thin-only=%d both=%d (sum=%d), zero differing",
		cases, buckets.edgeless, buckets.none, buckets.capOnly, buckets.thinOnly, buckets.both,
		buckets.edgeless+buckets.none+buckets.capOnly+buckets.thinOnly+buckets.both)
}

type truncationEdgeLayout struct {
	name string
	// pairs returns (from, to) index pairs over [0,entityCount).
	pairs func(entityCount int) [][2]int
	// danglingLinks adds group links whose endpoints are not visible nodes,
	// exercising the drop-before-yield path in visitEdges.
	danglingLinks int
}

func edgeLayoutsForTruncationEnumeration() []truncationEdgeLayout {
	return []truncationEdgeLayout{
		{name: "empty", pairs: func(int) [][2]int { return nil }},
		{name: "chain", pairs: func(n int) [][2]int {
			var out [][2]int
			for i := 0; i+1 < n; i++ {
				out = append(out, [2]int{i, i + 1})
			}
			return out
		}},
		{name: "star", pairs: func(n int) [][2]int {
			var out [][2]int
			for i := 1; i < n; i++ {
				out = append(out, [2]int{0, i})
			}
			return out
		}},
		{name: "complete", pairs: func(n int) [][2]int {
			var out [][2]int
			for i := 0; i < n; i++ {
				for j := i + 1; j < n; j++ {
					out = append(out, [2]int{i, j})
				}
			}
			return out
		}},
		{name: "half-chain-half-isolated", pairs: func(n int) [][2]int {
			var out [][2]int
			for i := 0; i+1 < n/2; i++ {
				out = append(out, [2]int{i, i + 1})
			}
			return out
		}},
		{name: "duplicated-chain", pairs: func(n int) [][2]int {
			var out [][2]int
			for i := 0; i+1 < n; i++ {
				out = append(out, [2]int{i, i + 1}, [2]int{i, i + 1})
			}
			return out
		}},
		{name: "chain-plus-dangling-links", danglingLinks: 3, pairs: func(n int) [][2]int {
			var out [][2]int
			for i := 0; i+1 < n; i++ {
				out = append(out, [2]int{i, i + 1})
			}
			return out
		}},
	}
}

// makeTruncationEnumerationGroup builds a group of entityCount entities wired
// per layout. With split=true the entities are dealt alternately into two
// repos, so every cross-parity pair becomes a group-level CrossRepoLink and the
// grp.Links half of visitEdges is exercised too.
func makeTruncationEnumerationGroup(entityCount int, layout truncationEdgeLayout, split bool) *DashGroup {
	slugFor := func(i int) string {
		if split && i%2 == 1 {
			return "repo-b"
		}
		return "repo-a"
	}
	ids := make([]string, entityCount)
	byRepo := map[string][]graph.Entity{}
	for i := 0; i < entityCount; i++ {
		ids[i] = fmt.Sprintf("n%02d", i)
		rank := float64(entityCount - i)
		slug := slugFor(i)
		byRepo[slug] = append(byRepo[slug], graph.Entity{
			ID: ids[i], Name: ids[i], Kind: "function", PageRank: &rank,
		})
	}

	relsByRepo := map[string][]graph.Relationship{}
	var links []CrossRepoLink
	for _, pair := range layout.pairs(entityCount) {
		fromSlug, toSlug := slugFor(pair[0]), slugFor(pair[1])
		if fromSlug == toSlug {
			relsByRepo[fromSlug] = append(relsByRepo[fromSlug], graph.Relationship{
				FromID: ids[pair[0]], ToID: ids[pair[1]], Kind: "CALLS",
			})
			continue
		}
		links = append(links, CrossRepoLink{
			Source: fromSlug + "::" + ids[pair[0]],
			Target: toSlug + "::" + ids[pair[1]],
			Kind:   "calls",
		})
	}
	for i := 0; i < layout.danglingLinks; i++ {
		links = append(links, CrossRepoLink{
			Source: "repo-a::" + ids[i%max(entityCount, 1)],
			Target: fmt.Sprintf("ghost-repo::missing%d", i),
			Kind:   "calls",
		})
	}

	repos := map[string]*DashRepo{}
	for slug, entities := range byRepo {
		repos[slug] = &DashRepo{Slug: slug, Doc: &graph.Document{
			Repo: slug, Entities: entities, Relationships: relsByRepo[slug],
		}}
	}
	return &DashGroup{Name: "trunc-enum", Repos: repos, Links: links}
}

// TestCollectCappedGraphEdgesDropsInvisibleEndpoints grades collectCappedGraphEdges'
// `visible` re-filter at the function's own contract, in BOTH branches.
//
// It exists because deleting that re-filter entirely (both the two early
// returns in the cap <= 0 branch and the !sourceOK || !targetOK guard in the
// capped branch) used to leave the whole ./internal/dashboard/ suite green
// (#7146). The filter and the `kept[from] && kept[to]` gate in
// buildV2GraphWithLimits were MUTUALLY MASKING: each only ever fired where the
// other had already made the difference invisible, so neither was graded.
// TestCollectCappedGraphEdgesMatchesDeterministicOrdering looks like it covers
// this, but every edge it yields has both endpoints in `nodes`, so it never
// feeds the filter anything to drop — and the production caller never does
// either (measured: the `inputCount != candidateCount` disjunct fires 0 of
// 3,528 enumerated states). The contract therefore has to be pinned here,
// directly, or nowhere.
func TestCollectCappedGraphEdgesDropsInvisibleEndpoints(t *testing.T) {
	nodes := []v2GraphNode{{ID: "a", PageRank: 0.9}, {ID: "b", PageRank: 0.8}}
	// One fully-visible edge, then one with an invisible TARGET, one with an
	// invisible SOURCE, and one with neither endpoint visible — so both halves
	// of the guard are exercised, not just whichever is checked first.
	input := []v2GraphEdge{
		{Source: "a", Target: "b", Kind: "CALLS"},
		{Source: "a", Target: "ghost", Kind: "CALLS"},
		{Source: "ghost", Target: "b", Kind: "CALLS"},
		{Source: "ghost", Target: "ghost2", Kind: "CALLS"},
	}
	want := []v2GraphEdge{{Source: "a", Target: "b", Kind: "CALLS"}}

	for _, cap := range []int{0, -1, 1, 10} {
		got, truncated := collectCappedGraphEdges(nodes, cap, func(yield func(v2GraphEdge)) {
			for _, edge := range input {
				yield(edge)
			}
		})
		if !reflect.DeepEqual(got, want) {
			t.Errorf("cap=%d: edges = %#v, want only the a->b edge — the three edges with an invisible endpoint must be dropped", cap, got)
		}
		// Dropping an input edge IS truncation, in every branch: 4 edges went
		// in and 1 came out.
		if !truncated {
			t.Errorf("cap=%d: truncated = false after dropping 3 of 4 input edges", cap)
		}
	}
}
