package graph

import (
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"sort"
	"strings"
	"testing"

	gonumgraph "gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/simple"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// awkwardFixture builds an entity/relationship set that exercises every edge
// case BuildGraph's admission rules cover, all at once:
//
//   - isolated entities (no incident relationship at all)
//   - self-loop relationships
//   - parallel relationships (same from/to, different weights)
//   - relationships whose endpoints are not entities ("dangling")
//   - relationships with a blank endpoint id
//   - out-of-order relationship arrival, so the collapse cannot rely on runs
//
// Weights are deliberately irregular (callsite_count / confidence combinations
// that do not sum exactly in binary) so a re-ordered accumulation shows up as a
// bit difference rather than cancelling out.
func awkwardFixture(n int, seed uint64) ([]Entity, []Relationship) {
	rng := rand.New(rand.NewPCG(seed, seed^0xa5a5a5a5)) //nolint:gosec // fixtures, not security
	ents := make([]Entity, n)
	for i := range ents {
		ents[i] = Entity{
			ID:   fmt.Sprintf("e%04d", i),
			Name: fmt.Sprintf("E%d", i),
			Kind: "function",
		}
	}
	// Leave the last 10% of entities isolated on purpose.
	linked := n - n/10
	if linked < 2 {
		linked = n
	}

	props := func() map[string]string {
		return map[string]string{
			"callsite_count": fmt.Sprintf("%d", 1+rng.IntN(4)),
			"confidence":     fmt.Sprintf("0.%02d", 10+rng.IntN(89)),
		}
	}
	var rels []Relationship
	add := func(from, to string) {
		rels = append(rels, Relationship{
			ID:     fmt.Sprintf("r%d", len(rels)),
			FromID: from,
			ToID:   to,
			Kind:   "CALLS",
		}.WithProperties(props()))
	}
	for i := 0; i < linked; i++ {
		for d := 0; d < 3; d++ {
			add(ents[i].ID, ents[rng.IntN(linked)].ID)
		}
		if i%7 == 0 {
			add(ents[i].ID, ents[i].ID) // self-loop
		}
		if i%11 == 0 {
			add(ents[i].ID, "not-an-entity")
			add("not-an-entity", ents[i].ID)
		}
		if i%13 == 0 {
			add("", ents[i].ID)
			add(ents[i].ID, "")
		}
	}
	// Duplicate a chunk of the relationships (shifted) so parallel edges are
	// dense, not incidental, and so duplicates are NOT adjacent in arrival
	// order.
	dupes := make([]Relationship, 0, len(rels))
	for i := 0; i < len(rels); i += 3 {
		r := rels[i]
		r.ID = "dup-" + r.ID
		dupes = append(dupes, r.WithProperties(props()))
	}
	rels = append(rels, dupes...)
	return ents, rels
}

// gonumAdjacency reads the directed adjacency straight off the gonum graph, the
// slow-but-obviously-correct way (per-node From iterators), and returns it as
// sorted target lists plus weights. This is the oracle for directedCSR.
func gonumAdjacency(g *simple.WeightedDirectedGraph) (map[int64][]int64, map[[2]int64]float64) {
	adj := make(map[int64][]int64)
	w := make(map[[2]int64]float64)
	nodes := gonumgraph.NodesOf(g.Nodes())
	for _, n := range nodes {
		u := n.ID()
		var row []int64
		to := g.From(u)
		for to.Next() {
			v := to.Node().ID()
			row = append(row, v)
			we := g.WeightedEdge(u, v)
			w[[2]int64{u, v}] = we.Weight()
		}
		sort.Slice(row, func(i, j int) bool { return row[i] < row[j] })
		adj[u] = row
	}
	return adj, w
}

// ---------------------------------------------------------------------------
// The CSR reproduces BuildGraph's structure exactly
// ---------------------------------------------------------------------------

func TestDirectedCSRMatchesGonumAdjacencyBitForBit(t *testing.T) {
	for _, tc := range []struct {
		name string
		n    int
		seed uint64
	}{
		{"tiny", 12, 1},
		{"small", 200, 2},
		{"mid", 2500, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ents, rels := awkwardFixture(tc.n, tc.seed)
			g, idx := BuildGraph(ents, rels)
			assertCSRMatchesLegacyBuildGraph(t, ents, rels, g, idx)
		})
	}
}

// assertCSRMatchesLegacyBuildGraph checks the CSR **and** the gonum graph that
// BuildGraph produced against legacyBuildGraph — the verbatim pre-change
// implementation — node by node, weights included.
//
// Both are checked because production now derives one from the other: g is
// materialised from the CSR, so comparing them to each other would be
// vacuous. legacyBuildGraph is the independent derivation.
func assertCSRMatchesLegacyBuildGraph(t *testing.T, ents []Entity, rels []Relationship,
	g *simple.WeightedDirectedGraph, idx *nodeIndex,
) {
	t.Helper()
	csr := idx.csr

	if csr.n != int(idx.next) {
		t.Fatalf("csr.n = %d, want %d (one row per entity)", csr.n, idx.next)
	}
	legacyG, legacyIdx := legacyBuildGraph(ents, rels)
	if !reflect.DeepEqual(idx.toInt, legacyIdx.toInt) {
		t.Fatal("node id assignment differs from the legacy implementation")
	}
	wantAdj, wantW := gonumAdjacency(legacyG)

	// The gonum graph BuildGraph now materialises from the CSR must itself be
	// identical to the legacy one, edge for edge and weight for weight —
	// ComputeCentrality's PageRank and IdentifyArticulationPoints still read it.
	gotAdj, gotW := gonumAdjacency(g)
	if !reflect.DeepEqual(gotAdj, wantAdj) {
		t.Fatal("gonum adjacency differs from the legacy implementation")
	}
	for k, wv := range wantW {
		if math.Float64bits(gotW[k]) != math.Float64bits(wv) {
			t.Fatalf("gonum weight %v: got %v want %v", k, gotW[k], wv)
		}
	}
	if len(gotW) != len(wantW) {
		t.Fatalf("gonum edge count: got %d want %d", len(gotW), len(wantW))
	}

	if len(wantAdj) != csr.n {
		t.Fatalf("legacy node count %d != csr.n %d", len(wantAdj), csr.n)
	}
	var edges int
	for u := int32(0); u < int32(csr.n); u++ {
		lo, hi := csr.row(u)
		row := make([]int64, 0, hi-lo)
		for p := lo; p < hi; p++ {
			row = append(row, int64(csr.adj[p]))
			wv := wantW[[2]int64{int64(u), int64(csr.adj[p])}]
			if math.Float64bits(wv) != math.Float64bits(csr.w[p]) {
				t.Fatalf("weight %d->%d: legacy=%v csr=%v (bits %x vs %x)",
					u, csr.adj[p], wv, csr.w[p],
					math.Float64bits(wv), math.Float64bits(csr.w[p]))
			}
		}
		if !reflect.DeepEqual(row, wantAdj[int64(u)]) && !(len(row) == 0 && len(wantAdj[int64(u)]) == 0) {
			t.Fatalf("row %d: csr=%v legacy=%v", u, row, wantAdj[int64(u)])
		}
		edges += len(row)
	}
	if edges == 0 {
		t.Fatal("fixture produced no edges — the comparison would be vacuous")
	}
	t.Logf("V=%d E=%d (from %d relationships)", csr.n, edges, len(rels))
}

// TestDirectedCSRHighFanOutRow covers the one shape neither awkwardFixture nor
// buildHeavyTailedGraph reaches: a single node with a very large out-degree,
// including parallel edges.
//
// Sheer graph size is covered elsewhere; what is specific to this shape is that
// it is the only thing that stresses (a) the `keys`/`rowW` scratch buffers,
// which are sized by the LARGEST row rather than by n, (b) the (target, arrival)
// int64 key packing at arrival indices far above zero, and (c) the hasEdge /
// weightOf binary searches over a row big enough for the search to actually
// recurse. A pathological degree distribution, not a big graph, is what changes
// what those three do.
func TestDirectedCSRHighFanOutRow(t *testing.T) {
	const fanOut = 60_000
	ents := make([]Entity, fanOut+2)
	for i := range ents {
		ents[i] = Entity{ID: fmt.Sprintf("e%06d", i), Name: fmt.Sprintf("E%d", i), Kind: "function"}
	}
	hub := ents[0].ID
	p := func(i int) map[string]string {
		return map[string]string{
			"callsite_count": fmt.Sprintf("%d", 1+i%5),
			"confidence":     fmt.Sprintf("0.%02d", 11+i%80),
		}
	}
	rels := make([]Relationship, 0, fanOut*2)
	// One huge row out of the hub...
	for i := 1; i <= fanOut; i++ {
		rels = append(rels, Relationship{
			ID: fmt.Sprintf("r%d", i), FromID: hub, ToID: ents[i].ID, Kind: "CALLS",
		}.WithProperties(p(i)))
	}
	// ...then a second, interleaved wave of parallel edges over every third
	// target, so the collapse runs deep inside a single very long row and the
	// duplicates are far apart in arrival order.
	for i := 1; i <= fanOut; i += 3 {
		rels = append(rels, Relationship{
			ID: fmt.Sprintf("d%d", i), FromID: hub, ToID: ents[i].ID, Kind: "CALLS",
		}.WithProperties(p(i+7)))
	}
	// A back-edge from the far end, so the reciprocal-pair handling in the
	// undirected projection also sees the huge row.
	rels = append(rels, Relationship{
		ID: "back", FromID: ents[fanOut].ID, ToID: hub, Kind: "CALLS",
	}.WithProperties(p(3)))

	g, idx := BuildGraph(ents, rels)
	assertCSRMatchesLegacyBuildGraph(t, ents, rels, g, idx)
	legacyG, _ := legacyBuildGraph(ents, rels)

	h := int32(idx.toInt[hub])
	lo, hi := idx.csr.row(h)
	if hi-lo != fanOut {
		t.Fatalf("hub row has %d entries, want %d after collapse", hi-lo, fanOut)
	}
	// Point lookups deep inside the row must agree with gonum.
	for _, target := range []int{1, 2, 3, fanOut / 2, fanOut/2 + 1, fanOut - 1, fanOut} {
		v := int32(idx.toInt[ents[target].ID])
		got, ok := idx.csr.weightOf(h, v)
		if !ok || !idx.csr.hasEdge(h, v) {
			t.Fatalf("hub->%d missing from the CSR", target)
		}
		// legacyG, not g: g is materialised from the CSR under test.
		want := legacyG.WeightedEdge(int64(h), int64(v)).Weight()
		if math.Float64bits(got) != math.Float64bits(want) {
			t.Fatalf("hub->%d weight: csr=%v legacy=%v", target, got, want)
		}
	}
	if idx.csr.hasEdge(h, h) {
		t.Fatal("hub self-edge present")
	}

	// The undirected projection must survive the same row.
	ucsr, wantIDs := buildCSRFromDirected(idx)
	want, gotIDs := buildCSRFromUndirected(legacyUndirectedProjection(g))
	if !reflect.DeepEqual(wantIDs, gotIDs) {
		t.Fatal("id mapping differs")
	}
	if !reflect.DeepEqual(ucsr.off, want.off) || !reflect.DeepEqual(ucsr.adj, want.adj) {
		t.Fatal("undirected projection differs on the high-fan-out shape")
	}
	assertFloatSlicesBitIdentical(t, "w", ucsr.w, want.w)
	assertFloatSlicesBitIdentical(t, "k", ucsr.k, want.k)
	if math.Float64bits(ucsr.m2) != math.Float64bits(want.m2) {
		t.Fatalf("m2: got %v want %v", ucsr.m2, want.m2)
	}
	t.Logf("hub out-degree %d (from %d relationships), undirected E=%d",
		hi-lo, len(rels), len(ucsr.adj)/2)
}

// TestDirectedCSRIndexLimitIsEnforced pins that the int32 bound is a real
// check on the real path, not a comment.
//
// The failure mode it rules out is specific and silent: before enforcement, an
// oversized graph produced a CSR with n set and an all-zero offset array, which
// newBCScratch accepts happily — the result is all-zero betweenness and n
// singleton communities, PERSISTED as if they were scores. A panic is the
// correct outcome, so that is what is asserted, on both bounds, by lowering the
// limit and driving BuildGraph itself.
func TestDirectedCSRIndexLimitIsEnforced(t *testing.T) {
	orig := csrIndexLimit
	t.Cleanup(func() { csrIndexLimit = orig })

	ents, rels := awkwardFixture(40, 77)

	t.Run("node count", func(t *testing.T) {
		csrIndexLimit = 5 // fewer than the 40 entities
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("no panic: an oversized node count was accepted")
			}
			if msg, _ := r.(string); !strings.Contains(msg, "entities exceeds") {
				t.Fatalf("panic message does not name the node bound: %v", r)
			}
		}()
		BuildGraph(ents, rels)
	})

	t.Run("edge count", func(t *testing.T) {
		// Above the entity count, below the admitted-relationship count, so only
		// the edge bound can fire.
		csrIndexLimit = 41
		if int64(len(rels)) <= csrIndexLimit {
			t.Fatalf("fixture has only %d relationships — the edge bound cannot fire", len(rels))
		}
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("no panic: an oversized edge count was accepted")
			}
			if msg, _ := r.(string); !strings.Contains(msg, "relationships exceeds") {
				t.Fatalf("panic message does not name the edge bound: %v", r)
			}
		}()
		BuildGraph(ents, rels)
	})

	t.Run("at the limit it does not fire", func(t *testing.T) {
		csrIndexLimit = orig
		if _, idx := BuildGraph(ents, rels); idx.csr.n != 40 {
			t.Fatalf("csr.n = %d, want 40", idx.csr.n)
		}
	})
}

// TestDirectedCSRKeepsIsolatedEntities pins behaviour 1 of BuildGraph
// INDEPENDENTLY: an entity with no incident relationship is still a node, so the
// CSR has a row for it and every later index (community id, betweenness) lines
// up with the gonum node id.
func TestDirectedCSRKeepsIsolatedEntities(t *testing.T) {
	ents := []Entity{
		{ID: "a", Name: "A", Kind: "function"},
		{ID: "lonely", Name: "L", Kind: "function"},
		{ID: "b", Name: "B", Kind: "function"},
	}
	rels := []Relationship{{ID: "r1", FromID: "a", ToID: "b", Kind: "CALLS"}}
	_, idx := BuildGraph(ents, rels)

	if idx.csr.n != 3 {
		t.Fatalf("csr.n = %d, want 3 (one row per entity, isolated included)", idx.csr.n)
	}
	lonely := int32(idx.toInt["lonely"])
	lo, hi := idx.csr.row(lonely)
	if lo != hi {
		t.Fatalf("isolated node row is non-empty: [%d,%d)", lo, hi)
	}
	// And the rows either side must still be addressable at the right index.
	alo, ahi := idx.csr.row(int32(idx.toInt["a"]))
	if ahi-alo != 1 || idx.csr.adj[alo] != int32(idx.toInt["b"]) {
		t.Fatalf("row for 'a' = %v, want exactly [b]", idx.csr.adj[alo:ahi])
	}
}

// TestDirectedCSRDropsSelfLoops pins behaviour 2 INDEPENDENTLY.
func TestDirectedCSRDropsSelfLoops(t *testing.T) {
	ents := []Entity{
		{ID: "a", Name: "A", Kind: "function"},
		{ID: "b", Name: "B", Kind: "function"},
	}
	rels := []Relationship{
		{ID: "r1", FromID: "a", ToID: "a", Kind: "CALLS"},
		{ID: "r2", FromID: "a", ToID: "b", Kind: "CALLS"},
		{ID: "r3", FromID: "b", ToID: "b", Kind: "CALLS"},
	}
	_, idx := BuildGraph(ents, rels)

	a, b := int32(idx.toInt["a"]), int32(idx.toInt["b"])
	if idx.csr.hasEdge(a, a) || idx.csr.hasEdge(b, b) {
		t.Fatal("self-loop present in the CSR")
	}
	if !idx.csr.hasEdge(a, b) {
		t.Fatal("a->b missing")
	}
	if total := idx.csr.off[idx.csr.n]; total != 1 {
		t.Fatalf("edge count = %d, want 1 (two self-loops dropped)", total)
	}
}

// TestDirectedCSRCollapsesParallelEdgesWithAccumulatedWeight pins behaviour 3
// INDEPENDENTLY, and to the bit: three parallel relationships must collapse to
// one CSR entry whose weight is the same float BuildGraph's
// `w += existing.Weight()` chain produces.
func TestDirectedCSRCollapsesParallelEdgesWithAccumulatedWeight(t *testing.T) {
	ents := []Entity{
		{ID: "a", Name: "A", Kind: "function"},
		{ID: "b", Name: "B", Kind: "function"},
		{ID: "c", Name: "C", Kind: "function"},
	}
	p := func(count, conf string) map[string]string {
		return map[string]string{"callsite_count": count, "confidence": conf}
	}
	// Interleaved, so the collapse cannot rely on duplicates being adjacent.
	rels := []Relationship{
		Relationship{ID: "r1", FromID: "a", ToID: "b", Kind: "CALLS"}.WithProperties(p("3", "0.17")),
		Relationship{ID: "r2", FromID: "a", ToID: "c", Kind: "CALLS"}.WithProperties(p("1", "0.31")),
		Relationship{ID: "r3", FromID: "a", ToID: "b", Kind: "CALLS"}.WithProperties(p("7", "0.29")),
		Relationship{ID: "r4", FromID: "a", ToID: "b", Kind: "CALLS"}.WithProperties(p("2", "0.83")),
	}
	_, idx := BuildGraph(ents, rels)

	a, b := int32(idx.toInt["a"]), int32(idx.toInt["b"])
	lo, hi := idx.csr.row(a)
	if hi-lo != 2 {
		t.Fatalf("row for 'a' has %d entries, want 2 (three parallel a->b collapsed)", hi-lo)
	}
	got, ok := idx.csr.weightOf(a, b)
	if !ok {
		t.Fatal("a->b missing from the CSR")
	}
	// Compared against legacyBuildGraph, NOT against the graph BuildGraph
	// returned: production materialises that graph FROM the CSR, so comparing
	// the two would be comparing a value with itself.
	legacyG, _ := legacyBuildGraph(ents, rels)
	want := legacyG.WeightedEdge(int64(a), int64(b)).Weight()
	if math.Float64bits(got) != math.Float64bits(want) {
		t.Fatalf("collapsed weight = %v, legacy = %v (bits %x vs %x)",
			got, want, math.Float64bits(got), math.Float64bits(want))
	}
	// Sanity: the accumulation is actually non-trivial.
	single := edgeWeight(rels[0].PropsSnapshot())
	if got == single {
		t.Fatalf("collapsed weight %v equals a single edge's weight — nothing accumulated", got)
	}
}

// ---------------------------------------------------------------------------
// The CSR is derived once — and consumed, not re-derived
// ---------------------------------------------------------------------------

// TestDirectedCSRIsBuiltExactlyOncePerBuildGraph is the guard that keeps this
// change from silently unwinding. The whole saving is "derive adjacency once
// and share it"; without this, a future edit can reintroduce a second walk and
// every other test in the package still passes.
//
// It asserts both counters across a FULL pass-4 run (the path production
// actually takes), not just across BuildGraph in isolation.
func TestDirectedCSRIsBuiltExactlyOncePerBuildGraph(t *testing.T) {
	t.Setenv("GRAFEL_BETWEENNESS_SAMPLE_THRESHOLD", "10")
	ents, rels := awkwardFixture(600, 9)

	g, idx := BuildGraph(ents, rels)
	if idx.csrBuilds != 1 {
		t.Fatalf("after BuildGraph: csrBuilds = %d, want 1", idx.csrBuilds)
	}
	if idx.undirectedDerivations != 0 {
		t.Fatalf("after BuildGraph: undirectedDerivations = %d, want 0", idx.undirectedDerivations)
	}

	names := nodeNames(ents, idx)
	ComputeCommunities(idx, names, DefaultCommunityOptions())
	ComputeCentrality(g, idx)
	IdentifyArticulationPoints(g, idx)

	if idx.csrBuilds != 1 {
		t.Errorf("after a full pass: csrBuilds = %d, want 1 — the directed adjacency is being re-derived", idx.csrBuilds)
	}
	if idx.undirectedDerivations != 1 {
		t.Errorf("after a full pass: undirectedDerivations = %d, want 1 — the undirected projection is being re-derived", idx.undirectedDerivations)
	}
}

// TestSampledBetweennessUsesSharedCSROnTheBuildGraphPath pins that the shared
// CSR is what betweenness actually reads. A counter on construction alone would
// not catch a consumer that quietly falls back to walking the gonum graph.
func TestSampledBetweennessUsesSharedCSROnTheBuildGraphPath(t *testing.T) {
	ents, rels := awkwardFixture(400, 17)
	g, idx := BuildGraph(ents, rels)

	ids := make([]int64, idx.next)
	for i := range ids {
		ids[i] = int64(i)
	}
	shared := newBCScratch(g, ids, idx.csr)
	if !shared.sharedCSR {
		t.Fatal("newBCScratch did not take the shared-CSR path on a BuildGraph graph")
	}
	if &shared.adj[0] != &idx.csr.adj[0] {
		t.Fatal("shared-CSR path copied the adjacency instead of aliasing it")
	}

	// The gonum-derived fallback must produce byte-identical adjacency, which is
	// what makes the aliasing safe.
	fallback := newBCScratch(g, ids, nil)
	if fallback.sharedCSR {
		t.Fatal("fallback path reported sharedCSR")
	}
	if !reflect.DeepEqual(shared.off, fallback.off) {
		t.Fatal("shared CSR offsets differ from the gonum-derived offsets")
	}
	if !reflect.DeepEqual(shared.adj, fallback.adj) {
		t.Fatal("shared CSR adjacency differs from the gonum-derived adjacency")
	}
}

// ---------------------------------------------------------------------------
// Equivalence: Louvain / community results
// ---------------------------------------------------------------------------

// TestUndirectedProjectionFromCSRMatchesGonumProjection compares the flat-array
// projection against the gonum route it replaced, field by field and bit by
// bit, including the derived weighted degrees and 2m.
func TestUndirectedProjectionFromCSRMatchesGonumProjection(t *testing.T) {
	for _, tc := range []struct {
		name string
		n    int
		seed uint64
	}{
		{"tiny", 15, 4},
		{"small", 300, 5},
		{"mid", 3000, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ents, rels := awkwardFixture(tc.n, tc.seed)
			g, idx := BuildGraph(ents, rels)

			got, gotIDs := buildCSRFromDirected(idx)
			want, wantIDs := buildCSRFromUndirected(legacyUndirectedProjection(g))

			if !reflect.DeepEqual(gotIDs, wantIDs) {
				t.Fatalf("id mapping differs")
			}
			if got.n != want.n {
				t.Fatalf("n: got %d want %d", got.n, want.n)
			}
			if !reflect.DeepEqual(got.off, want.off) {
				t.Fatalf("offsets differ")
			}
			if !reflect.DeepEqual(got.adj, want.adj) {
				t.Fatalf("adjacency differs")
			}
			assertFloatSlicesBitIdentical(t, "w", got.w, want.w)
			assertFloatSlicesBitIdentical(t, "selfw", got.selfw, want.selfw)
			assertFloatSlicesBitIdentical(t, "k", got.k, want.k)
			if math.Float64bits(got.m2) != math.Float64bits(want.m2) {
				t.Fatalf("m2: got %v want %v", got.m2, want.m2)
			}
			t.Logf("%s: V=%d undirected E=%d m2=%v", tc.name, got.n, len(got.adj)/2, got.m2)
		})
	}
}

func assertFloatSlicesBitIdentical(t *testing.T, label string, got, want []float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: length %d != %d", label, len(got), len(want))
	}
	for i := range got {
		if math.Float64bits(got[i]) != math.Float64bits(want[i]) {
			t.Fatalf("%s[%d]: got %v want %v (bits %x vs %x)",
				label, i, got[i], want[i], math.Float64bits(got[i]), math.Float64bits(want[i]))
		}
	}
}

// TestComputeCommunitiesMatchesLegacyGonumPath is the golden test for the
// Louvain half of the change: the entire returned tuple — community results
// (id, size, modularity, top entities), the entity->community map, the overall
// modularity, and the denoise count — must equal what the pre-change
// implementation produces, with modularity compared to FULL PRECISION.
//
// The legacy edge walk it is compared against iterates gonum's map-ordered edge
// iterator, so the legacy raw float sums are not reproducible run to run. The
// published values are quantised by roundForDeterminism, and the assertion is on
// those; the loop repeats the legacy computation so a case where the legacy side
// itself is unstable at the published precision would be caught rather than
// silently blamed on this change.
func TestComputeCommunitiesMatchesLegacyGonumPath(t *testing.T) {
	for _, tc := range []struct {
		name    string
		n       int
		seed    uint64
		minSize int
	}{
		{"tiny-nodenoise", 20, 21, 1},
		{"small-denoise", 400, 22, 5},
		{"mid-denoise", 3000, 23, 5},
		{"mid-nodenoise", 3000, 24, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ents, rels := awkwardFixture(tc.n, tc.seed)
			g, idx := BuildGraph(ents, rels)
			names := nodeNames(ents, idx)
			opts := CommunityOptions{MinSize: tc.minSize}

			gotRes, gotOf, gotQ, gotDen := ComputeCommunities(idx, names, opts)

			for rep := 0; rep < 3; rep++ {
				// legacyComputeCommunities mutates nothing shared; rebuild the
				// index each rep so its own bookkeeping starts clean.
				lg, lidx := BuildGraph(ents, rels)
				lnames := nodeNames(ents, lidx)
				wantRes, wantOf, wantQ, wantDen := legacyComputeCommunities(lg, lidx, lnames, opts)
				_ = g

				if math.Float64bits(gotQ) != math.Float64bits(wantQ) {
					t.Fatalf("rep %d: overall modularity got %v want %v (bits %x vs %x)",
						rep, gotQ, wantQ, math.Float64bits(gotQ), math.Float64bits(wantQ))
				}
				if gotDen != wantDen {
					t.Fatalf("rep %d: denoised got %d want %d", rep, gotDen, wantDen)
				}
				if len(gotRes) != len(wantRes) {
					t.Fatalf("rep %d: community count got %d want %d", rep, len(gotRes), len(wantRes))
				}
				for i := range gotRes {
					a, b := gotRes[i], wantRes[i]
					if a.ID != b.ID || a.Size != b.Size || !reflect.DeepEqual(a.TopEntities, b.TopEntities) {
						t.Fatalf("rep %d: community %d: got %+v want %+v", rep, i, a, b)
					}
					if math.Float64bits(a.Modularity) != math.Float64bits(b.Modularity) {
						t.Fatalf("rep %d: community %d modularity got %v want %v",
							rep, i, a.Modularity, b.Modularity)
					}
				}
				if !reflect.DeepEqual(gotOf, wantOf) {
					t.Fatalf("rep %d: entity->community map differs", rep)
				}
			}
			t.Logf("%s: %d communities, Q=%v, denoised=%d", tc.name, len(gotRes), gotQ, gotDen)
		})
	}
}

// TestRunAlgorithmsEndToEndUnchanged checks the whole pass-4 output through the
// public entry point, against the same run reconstructed with the legacy
// community path — so a divergence anywhere between BuildGraph and the results
// struct shows up, not only inside the functions this change touched.
func TestRunAlgorithmsEndToEndUnchanged(t *testing.T) {
	t.Setenv("GRAFEL_BETWEENNESS_SAMPLE_THRESHOLD", "10")
	ents, rels := awkwardFixture(1200, 31)

	res := RunAlgorithmsWithOptions(ents, rels, DefaultCommunityOptions())

	g, idx := BuildGraph(ents, rels)
	names := nodeNames(ents, idx)
	wantRes, wantOf, wantQ, wantDen := legacyComputeCommunities(g, idx, names, DefaultCommunityOptions())
	AssignCommunityNames(wantRes, ents, wantOf)

	if math.Float64bits(res.Stats.LouvainModularity) != math.Float64bits(wantQ) {
		t.Fatalf("modularity got %v want %v", res.Stats.LouvainModularity, wantQ)
	}
	if res.Stats.DenoisedCommunities != wantDen {
		t.Fatalf("denoised got %d want %d", res.Stats.DenoisedCommunities, wantDen)
	}
	if !reflect.DeepEqual(res.Communities, wantRes) {
		t.Fatalf("community results differ")
	}
	if !reflect.DeepEqual(res.CommunityID, wantOf) {
		t.Fatalf("entity->community map differs")
	}

	// Betweenness on this path is sampled and reads the shared CSR; compare it
	// against the legacy gonum-walking implementation, rounded exactly as
	// ComputeCentrality rounds it.
	legacyRaw := sampledBetweennessLegacy(g, betweennessSampleSize, betweennessSampleSeed)
	wantBetw := make(map[string]float64, len(legacyRaw))
	for nid, v := range legacyRaw {
		if rv := roundForDeterminism(sanitizeFloat(v)); rv != 0 {
			wantBetw[idx.fromInt[nid]] = rv
		}
	}
	if len(wantBetw) == 0 {
		t.Fatal("fixture produced no non-zero betweenness — the comparison would be vacuous")
	}
	if !reflect.DeepEqual(res.Centrality, wantBetw) {
		t.Fatalf("betweenness differs: got %d entries, want %d", len(res.Centrality), len(wantBetw))
	}
	t.Logf("V=%d rels=%d: %d communities, Q=%v, %d scored nodes",
		len(ents), len(rels), len(res.Communities), res.Stats.LouvainModularity, len(res.Centrality))
}

// TestDirectedCSREmptyAndEdgeless covers the degenerate shapes that the
// count-then-fill allocation could trip over.
func TestDirectedCSREmptyAndEdgeless(t *testing.T) {
	_, idx := BuildGraph(nil, nil)
	if idx.csr.n != 0 || len(idx.csr.off) != 1 || idx.csr.off[0] != 0 {
		t.Fatalf("empty graph: csr = %+v", idx.csr)
	}
	if _, ids := buildCSRFromDirected(idx); len(ids) != 0 {
		t.Fatalf("empty graph: undirected projection produced %d ids", len(ids))
	}

	ents := []Entity{{ID: "a", Kind: "function"}, {ID: "b", Kind: "function"}}
	_, idx2 := BuildGraph(ents, nil)
	if idx2.csr.n != 2 || idx2.csr.off[2] != 0 {
		t.Fatalf("edgeless graph: n=%d total=%d", idx2.csr.n, idx2.csr.off[2])
	}
	ucsr, _ := buildCSRFromDirected(idx2)
	if ucsr.n != 2 || len(ucsr.adj) != 0 || ucsr.m2 != 0 {
		t.Fatalf("edgeless projection: n=%d adj=%d m2=%v", ucsr.n, len(ucsr.adj), ucsr.m2)
	}
}
