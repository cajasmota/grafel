package graph

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"testing"

	"gonum.org/v1/gonum/graph/community"
	"gonum.org/v1/gonum/graph/simple"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// plantedSpec describes a planted-partition graph: a set of communities with
// dense intra-community wiring and sparse inter-community wiring.
type plantedSpec struct {
	// Sizes are the community sizes. Sum(Sizes) is the node count.
	Sizes []int
	// IntraDeg is the average intra-community degree per node.
	IntraDeg int
	// InterEdges is the number of random cross-community edges.
	InterEdges int
	Seed       uint64
}

// plantedSizes returns community sizes for a graph of n nodes shaped like the
// real 25-repo corpus partition: one community holding ~25% of the nodes,
// a second holding ~22%, and the remainder spread over many smaller ones.
// This is the pathological case for gonum's Σ|c|² local mover.
func plantedSizes(n int) []int {
	sizes := []int{n * 25 / 100, n * 22 / 100}
	rest := n - sizes[0] - sizes[1]
	// 20 mid-size communities absorbing the remainder.
	const k = 20
	for i := 0; i < k; i++ {
		s := rest / k
		if i == k-1 {
			s = rest - (rest/k)*(k-1)
		}
		if s > 0 {
			sizes = append(sizes, s)
		}
	}
	return sizes
}

// buildPlantedGraph materialises a plantedSpec as a weighted undirected gonum
// graph. Deterministic for a given seed.
func buildPlantedGraph(spec plantedSpec) *simple.WeightedUndirectedGraph {
	rng := rand.New(rand.NewPCG(spec.Seed, spec.Seed^0x9e3779b9))
	und := simple.NewWeightedUndirectedGraph(0, 0)

	// Assign node ids per community.
	var n int
	for _, s := range spec.Sizes {
		n += s
	}
	for i := 0; i < n; i++ {
		und.AddNode(simple.Node(int64(i)))
	}
	starts := make([]int, len(spec.Sizes))
	acc := 0
	for i, s := range spec.Sizes {
		starts[i] = acc
		acc += s
	}

	addEdge := func(u, v int, w float64) {
		if u == v {
			return
		}
		if e := und.WeightedEdge(int64(u), int64(v)); e != nil {
			nw := e.Weight() + w
			und.RemoveEdge(int64(u), int64(v))
			und.SetWeightedEdge(und.NewWeightedEdge(simple.Node(int64(u)), simple.Node(int64(v)), nw))
			return
		}
		und.SetWeightedEdge(und.NewWeightedEdge(simple.Node(int64(u)), simple.Node(int64(v)), w))
	}

	for ci, size := range spec.Sizes {
		if size < 2 {
			continue
		}
		start := starts[ci]
		// Ring backbone guarantees connectivity within the community.
		for i := 0; i < size; i++ {
			addEdge(start+i, start+(i+1)%size, 1.0)
		}
		extra := size * spec.IntraDeg / 2
		for e := 0; e < extra; e++ {
			u := start + int(rng.Uint64N(uint64(size)))
			v := start + int(rng.Uint64N(uint64(size)))
			addEdge(u, v, 1.0+float64(rng.Uint64N(3)))
		}
	}
	for e := 0; e < spec.InterEdges; e++ {
		u := int(rng.Uint64N(uint64(n)))
		v := int(rng.Uint64N(uint64(n)))
		addEdge(u, v, 1.0)
	}
	return und
}

// defaultPlanted returns the standard test fixture at size n.
//
// NOTE its limits: IntraDeg=6 with only n/4 inter-community edges converges in
// 8-10 local-moving sweeps at ANY size, so this family cannot exercise the
// louvainMaxSweeps truncation bound. plantedSizes reproduces the corpus's
// community-size skew but not its degree heavy tail. Use buildBarabasiAlbert
// for anything that depends on convergence behaviour.
func defaultPlanted(n int, seed uint64) *simple.WeightedUndirectedGraph {
	return buildPlantedGraph(plantedSpec{
		Sizes:      plantedSizes(n),
		IntraDeg:   6,
		InterEdges: n / 4,
		Seed:       seed,
	})
}

// buildBarabasiAlbert generates a scale-free graph by preferential attachment:
// each new node attaches m edges to existing nodes chosen with probability
// proportional to degree. Deterministic for a given seed.
//
// This is the shape a real code graph has — a power-law degree tail with a few
// very-high-degree hubs — and unlike the planted fixtures it needs HUNDREDS of
// local-moving sweeps to converge. It has no planted community structure at
// all, so it is also the honest test of whether the implementation finds
// structure comparable to gonum's where none was designed in.
func buildBarabasiAlbert(n, m int, seed uint64) *simple.WeightedUndirectedGraph {
	rng := rand.New(rand.NewPCG(seed, seed^0xdeadbeef))
	und := simple.NewWeightedUndirectedGraph(0, 0)
	for i := 0; i < n; i++ {
		und.AddNode(simple.Node(int64(i)))
	}
	if m < 1 {
		m = 1
	}
	// targets is the degree-weighted urn: each node appears once per incident
	// edge endpoint, so a uniform draw is a preferential-attachment draw.
	targets := make([]int, 0, 2*n*m)
	// Seed core: a small clique so the urn is non-empty.
	for i := 0; i <= m; i++ {
		for j := i + 1; j <= m && j < n; j++ {
			und.SetWeightedEdge(und.NewWeightedEdge(simple.Node(int64(i)), simple.Node(int64(j)), 1))
			targets = append(targets, i, j)
		}
	}
	for v := m + 1; v < n; v++ {
		chosen := make(map[int]bool, m)
		for len(chosen) < m && len(targets) > 0 {
			u := targets[rng.Uint64N(uint64(len(targets)))]
			if u == v {
				continue
			}
			chosen[u] = true
		}
		picks := make([]int, 0, len(chosen))
		for u := range chosen {
			picks = append(picks, u)
		}
		sort.Ints(picks) // determinism: map iteration order is not stable
		for _, u := range picks {
			und.SetWeightedEdge(und.NewWeightedEdge(simple.Node(int64(u)), simple.Node(int64(v)), 1))
			targets = append(targets, u, v)
		}
	}
	return und
}

// ---------------------------------------------------------------------------
// Modularity helper (independent of the implementation under test)
// ---------------------------------------------------------------------------

// modularityOfGroups computes Q for a partition given as node-id groups,
// straight from the definition:
//
//	Q = Σ_c [ in_c/(2m) - γ (tot_c/(2m))² ]
//
// where in_c counts intra-community edge weight twice.
func modularityOfGroups(und *simple.WeightedUndirectedGraph, groups [][]int64, resolution float64) float64 {
	commOf := make(map[int64]int, 1024)
	for c, g := range groups {
		for _, id := range g {
			commOf[id] = c
		}
	}
	deg := make(map[int64]float64, len(commOf))
	in := make([]float64, len(groups))
	var m2 float64
	it := und.WeightedEdges()
	if it != nil {
		for it.Next() {
			e := it.WeightedEdge()
			u, v, w := e.From().ID(), e.To().ID(), e.Weight()
			deg[u] += w
			deg[v] += w
			m2 += 2 * w
			if cu, ok := commOf[u]; ok {
				if cv, ok2 := commOf[v]; ok2 && cu == cv {
					in[cu] += 2 * w
				}
			}
		}
	}
	if m2 == 0 {
		return 0
	}
	tot := make([]float64, len(groups))
	for c, g := range groups {
		for _, id := range g {
			tot[c] += deg[id]
		}
	}
	var q float64
	for c := range groups {
		q += in[c]/m2 - resolution*(tot[c]/m2)*(tot[c]/m2)
	}
	return q
}

func gonumGroups(und *simple.WeightedUndirectedGraph, resolution float64) [][]int64 {
	src := rand.NewPCG(1, 2)
	reduced := community.Modularize(und, resolution, src)
	var out [][]int64
	for _, g := range reduced.Communities() {
		ids := make([]int64, 0, len(g))
		for _, n := range g {
			ids = append(ids, n.ID())
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		out = append(out, ids)
	}
	return out
}

// ---------------------------------------------------------------------------
// Correctness
// ---------------------------------------------------------------------------

// TestLouvainPartitionIsAPartition — every node appears in exactly one group.
func TestLouvainPartitionIsAPartition(t *testing.T) {
	t.Parallel()
	und := defaultPlanted(2000, 7)
	groups := louvainPartition(und, 1.0)

	seen := make(map[int64]int)
	for _, g := range groups {
		for _, id := range g {
			seen[id]++
		}
	}
	nodes := und.Nodes()
	count := 0
	for nodes.Next() {
		count++
		id := nodes.Node().ID()
		if seen[id] != 1 {
			t.Fatalf("node %d appears %d times in the partition, want 1", id, seen[id])
		}
	}
	if len(seen) != count {
		t.Fatalf("partition covers %d nodes, graph has %d", len(seen), count)
	}
}

// TestLouvainRecoversPlantedCommunities — on a graph with obvious block
// structure the partition must recover it: two nodes planted together should
// (overwhelmingly) land together.
func TestLouvainRecoversPlantedCommunities(t *testing.T) {
	t.Parallel()
	sizes := []int{100, 100, 100, 100}
	und := buildPlantedGraph(plantedSpec{Sizes: sizes, IntraDeg: 8, InterEdges: 20, Seed: 3})
	groups := louvainPartition(und, 1.0)

	commOf := map[int64]int{}
	for c, g := range groups {
		for _, id := range g {
			commOf[id] = c
		}
	}
	// Each planted block should be dominated by a single detected community.
	start := 0
	for bi, s := range sizes {
		counts := map[int]int{}
		for i := start; i < start+s; i++ {
			counts[commOf[int64(i)]]++
		}
		best := 0
		for _, v := range counts {
			if v > best {
				best = v
			}
		}
		if float64(best)/float64(s) < 0.9 {
			t.Errorf("planted block %d: dominant community holds only %d/%d nodes", bi, best, s)
		}
		start += s
	}
}

// TestLouvainTinyGraphs — degenerate inputs must not panic and must produce
// sane partitions.
func TestLouvainTinyGraphs(t *testing.T) {
	t.Parallel()
	t.Run("empty", func(t *testing.T) {
		und := simple.NewWeightedUndirectedGraph(0, 0)
		if g := louvainPartition(und, 1.0); len(g) != 0 {
			t.Fatalf("empty graph produced %d groups", len(g))
		}
	})
	t.Run("isolated nodes only", func(t *testing.T) {
		und := simple.NewWeightedUndirectedGraph(0, 0)
		for i := 0; i < 5; i++ {
			und.AddNode(simple.Node(int64(i)))
		}
		g := louvainPartition(und, 1.0)
		if len(g) != 5 {
			t.Fatalf("5 isolated nodes -> %d groups, want 5 singletons", len(g))
		}
	})
	t.Run("single edge", func(t *testing.T) {
		und := simple.NewWeightedUndirectedGraph(0, 0)
		und.SetWeightedEdge(und.NewWeightedEdge(simple.Node(0), simple.Node(1), 1))
		g := louvainPartition(und, 1.0)
		if len(g) != 1 || len(g[0]) != 2 {
			t.Fatalf("single edge -> %v, want one group of 2", g)
		}
	})
}

// TestLouvainDeterminism — same input, same output, run to run. Run with
// -race -count=2 in CI.
func TestLouvainDeterminism(t *testing.T) {
	t.Parallel()
	und := defaultPlanted(3000, 11)
	want := louvainPartition(und, 1.0)
	for run := 0; run < 3; run++ {
		got := louvainPartition(und, 1.0)
		if len(got) != len(want) {
			t.Fatalf("run %d: %d groups, want %d", run, len(got), len(want))
		}
		for c := range want {
			if len(got[c]) != len(want[c]) {
				t.Fatalf("run %d: group %d size %d, want %d", run, c, len(got[c]), len(want[c]))
			}
			for i := range want[c] {
				if got[c][i] != want[c][i] {
					t.Fatalf("run %d: group %d member %d = %d, want %d", run, c, i, got[c][i], want[c][i])
				}
			}
		}
	}
}

// TestLouvainMemberOrderIsSorted — members are emitted in ascending node-id
// order, and groups in ascending lowest-member order. Downstream code sorts
// again, but the contract is asserted here.
func TestLouvainMemberOrderIsSorted(t *testing.T) {
	t.Parallel()
	groups := louvainPartition(defaultPlanted(1000, 5), 1.0)
	prevMin := int64(-1)
	for c, g := range groups {
		if !sort.SliceIsSorted(g, func(i, j int) bool { return g[i] < g[j] }) {
			t.Fatalf("group %d members not sorted: %v", c, g)
		}
		if g[0] <= prevMin {
			t.Fatalf("group %d lowest member %d <= previous group's %d", c, g[0], prevMin)
		}
		prevMin = g[0]
	}
}

// ---------------------------------------------------------------------------
// Modularity parity with gonum — THE quality gate
// ---------------------------------------------------------------------------

// modularityParityEpsilon is the tolerance on Q_new >= Q_gonum - eps. Louvain
// is a heuristic; float summation order and tie-breaks differ, so exact
// equality is not the contract. Partition QUALITY is.
const modularityParityEpsilon = 0.01

func TestLouvainModularityParityWithGonum(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		und  *simple.WeightedUndirectedGraph
	}{
		{"planted-1k-corpus-shape", defaultPlanted(1000, 1)},
		{"planted-4k-corpus-shape", defaultPlanted(4000, 2)},
		{"planted-uniform-blocks", buildPlantedGraph(plantedSpec{
			Sizes: []int{250, 250, 250, 250, 250, 250, 250, 250}, IntraDeg: 6, InterEdges: 400, Seed: 4,
		})},
		{"planted-one-giant-community", buildPlantedGraph(plantedSpec{
			Sizes: []int{1500, 100, 100, 100, 100, 50, 50}, IntraDeg: 6, InterEdges: 300, Seed: 5,
		})},
		{"sparse-weak-structure", buildPlantedGraph(plantedSpec{
			Sizes: []int{300, 300, 300}, IntraDeg: 2, InterEdges: 900, Seed: 6,
		})},
		// Scale-free, no planted structure, hard-convergence regime (hundreds
		// of sweeps). The planted fixtures above all converge in <10 sweeps and
		// so cannot detect louvainMaxSweeps truncation; these can.
		{"barabasi-albert-2k", buildBarabasiAlbert(2000, 3, 41)},
		{"barabasi-albert-8k", buildBarabasiAlbert(8000, 3, 42)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			qNew := modularityOfGroups(tc.und, louvainPartition(tc.und, 1.0), 1.0)
			qOld := modularityOfGroups(tc.und, gonumGroups(tc.und, 1.0), 1.0)
			t.Logf("Q_new=%.6f Q_gonum=%.6f delta=%+.6f", qNew, qOld, qNew-qOld)
			if qNew < qOld-modularityParityEpsilon {
				t.Errorf("modularity regression: Q_new=%.6f < Q_gonum=%.6f - %g", qNew, qOld, modularityParityEpsilon)
			}
		})
	}
}

// TestLouvainResolutionParameterHasEffect — a higher resolution must not
// produce fewer communities than a lower one (standard Louvain behaviour); the
// parameter is genuinely threaded through.
func TestLouvainResolutionParameterHasEffect(t *testing.T) {
	t.Parallel()
	und := defaultPlanted(2000, 9)
	lo := len(louvainPartition(und, 0.5))
	hi := len(louvainPartition(und, 4.0))
	if hi <= lo {
		t.Errorf("resolution 4.0 gave %d communities, resolution 0.5 gave %d — resolution has no effect", hi, lo)
	}
}

// TestLouvainSweepCapIsNotBinding — louvainMaxSweeps is a TRUNCATION bound, not
// a formality: on scale-free graphs level 0 needs hundreds of sweeps and the
// count grows ~N^0.65. If the cap binds, the partition is silently cut off
// mid-convergence and quality degrades by an amount that grows with n. That is
// the one way this implementation can quietly cost MCP quality, so it is
// asserted directly rather than inferred from modularity.
func TestLouvainSweepCapIsNotBinding(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		und  *simple.WeightedUndirectedGraph
	}{
		{"barabasi-albert-2k", buildBarabasiAlbert(2000, 3, 41)},
		{"barabasi-albert-8k", buildBarabasiAlbert(8000, 3, 42)},
		{"barabasi-albert-16k", buildBarabasiAlbert(16000, 3, 43)},
		{"planted-16k", defaultPlanted(16000, 44)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, sweeps := louvainPartitionWithSweeps(tc.und, 1.0)
			t.Logf("sweeps per level: %v (cap %d)", sweeps, louvainMaxSweeps)
			for lvl, s := range sweeps {
				if s >= louvainMaxSweeps {
					t.Errorf("level %d consumed %d sweeps and hit the cap (%d): the partition was "+
						"TRUNCATED mid-convergence, not converged", lvl, s, louvainMaxSweeps)
				}
			}
		})
	}
}

// TestLouvainAggregationPreservesModularity — aggregating a level must not
// change Q: collapsing each community into one node with the intra-community
// weight as its self-loop is supposed to be exactly Q-preserving.
//
// This test exists specifically because selfw is otherwise WRITE-ONLY in the
// production path: localMoving never reads it and computeDegrees is never
// called on an aggregated graph, so dropping the 2*selfw term from
// computeDegrees, or the internal/2 term from aggregate, leaves every other
// assertion in this file bit-identical. An incorrect selfw would be silently
// untestable — a trap for anyone later adding Leiden refinement or reading
// selfw in the ΔQ expression.
func TestLouvainAggregationPreservesModularity(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		und  *simple.WeightedUndirectedGraph
	}{
		{"planted-1k", defaultPlanted(1000, 61)},
		{"planted-uniform", buildPlantedGraph(plantedSpec{
			Sizes: []int{200, 200, 200, 200}, IntraDeg: 6, InterEdges: 300, Seed: 62,
		})},
		{"barabasi-albert-2k", buildBarabasiAlbert(2000, 3, 63)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g, _ := buildCSRFromUndirected(tc.und)
			comm, nc, moved, _ := g.localMoving(1.0)
			if !moved {
				t.Skip("no moves at level 0; nothing to aggregate")
			}
			qBefore := csrModularity(g, comm, nc, 1.0)

			agg := g.aggregate(comm, nc)

			// Identity partition on the aggregated graph.
			identity := make([]int32, agg.n)
			for i := range identity {
				identity[i] = int32(i)
			}
			qAfter := csrModularity(agg, identity, agg.n, 1.0)

			if math.Abs(qBefore-qAfter) > 1e-9 {
				t.Errorf("aggregation changed Q: before=%.12f after=%.12f delta=%.3e",
					qBefore, qAfter, qAfter-qBefore)
			}

			// The degree invariant k[c] == Σ incident w + 2*selfw[c] is what
			// makes selfw load-bearing; assert it on the aggregated graph,
			// which is the only place self-loops are ever non-zero.
			var anySelf bool
			for c := 0; c < agg.n; c++ {
				var inc float64
				for p := agg.off[c]; p < agg.off[c+1]; p++ {
					inc += agg.w[p]
				}
				if agg.selfw[c] != 0 {
					anySelf = true
				}
				if got, want := agg.k[c], inc+2*agg.selfw[c]; math.Abs(got-want) > 1e-9 {
					t.Fatalf("aggregated node %d: k=%.12f but Σw+2*selfw=%.12f", c, got, want)
				}
			}
			if !anySelf {
				t.Error("aggregated graph has no self-loops at all — the fixture is not exercising selfw")
			}
			if math.Abs(agg.m2-g.m2) > 1e-9 {
				t.Errorf("aggregation changed total weight: m2 %.12f -> %.12f", g.m2, agg.m2)
			}
		})
	}
}

// TestCSRComputeDegreesCountsSelfLoopsTwice — computeDegrees is only ever
// called on the level-0 graph, where simple.WeightedUndirectedGraph guarantees
// selfw is all-zero, so its `2*selfw` term is unreachable in production and
// deleting it changes nothing observable. That makes it a live trap: the term
// is REQUIRED the moment anyone calls computeDegrees on an aggregated graph
// (e.g. adding Leiden refinement). Pin the contract on a hand-built CSR graph
// with non-zero self-loops.
func TestCSRComputeDegreesCountsSelfLoopsTwice(t *testing.T) {
	t.Parallel()
	// Two nodes, one edge of weight 3 between them; self-loops 5 and 7.
	g := &csrGraph{
		n:     2,
		off:   []int32{0, 1, 2},
		adj:   []int32{1, 0},
		w:     []float64{3, 3},
		selfw: []float64{5, 7},
		k:     make([]float64, 2),
	}
	g.computeDegrees()

	// A self-loop contributes both of its endpoints to the node's degree.
	if want := 3 + 2*5.0; g.k[0] != want {
		t.Errorf("k[0] = %v, want %v (edge 3 + twice the self-loop 5)", g.k[0], want)
	}
	if want := 3 + 2*7.0; g.k[1] != want {
		t.Errorf("k[1] = %v, want %v (edge 3 + twice the self-loop 7)", g.k[1], want)
	}
	if want := (3 + 10.0) + (3 + 14.0); g.m2 != want {
		t.Errorf("m2 = %v, want %v", g.m2, want)
	}
}

// csrModularity computes Q directly from a CSR graph and a labelling, honouring
// self-loops. Independent of louvainPartition's own bookkeeping.
func csrModularity(g *csrGraph, comm []int32, nc int, resolution float64) float64 {
	if g.m2 <= 0 {
		return 0
	}
	in := make([]float64, nc)  // intra-community weight, counted twice
	tot := make([]float64, nc) // Σ k over members
	for u := 0; u < g.n; u++ {
		c := comm[u]
		tot[c] += g.k[u]
		in[c] += 2 * g.selfw[u] // a self-loop is entirely internal
		for p := g.off[u]; p < g.off[u+1]; p++ {
			if comm[g.adj[p]] == c {
				in[c] += g.w[p] // each intra edge is seen from both endpoints
			}
		}
	}
	var q float64
	for c := 0; c < nc; c++ {
		q += in[c]/g.m2 - resolution*(tot[c]/g.m2)*(tot[c]/g.m2)
	}
	return q
}

// ---------------------------------------------------------------------------
// Scaling
// ---------------------------------------------------------------------------

// TestLouvainScalingExponent lives in louvain_scaling_perf_test.go behind the
// `perf` build tag: a fitted wall-clock exponent is not measurable on a shared
// CI runner. Run `go test -tags perf ./internal/graph/` to exercise it.

// leastSquaresSlope fits y = a + b*x and returns b.
func leastSquaresSlope(x, y []float64) float64 {
	n := float64(len(x))
	var sx, sy, sxy, sxx float64
	for i := range x {
		sx += x[i]
		sy += y[i]
		sxy += x[i] * y[i]
		sxx += x[i] * x[i]
	}
	den := n*sxx - sx*sx
	if den == 0 {
		return 0
	}
	return (n*sxy - sx*sy) / den
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

func benchLouvainSizes() []int { return []int{2000, 8000, 32000} }

// BenchmarkLouvainNew measures the in-repo implementation on corpus-shaped
// planted partitions (one community holding ~25% of the nodes).
func BenchmarkLouvainNew(b *testing.B) {
	for _, n := range benchLouvainSizes() {
		und := defaultPlanted(n, 31)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				louvainPartition(und, 1.0)
			}
		})
	}
}

// BenchmarkLouvainGonum is the baseline: gonum's community.Modularize on the
// exact same graphs. Kept so the comparison can be re-run at any time.
func BenchmarkLouvainGonum(b *testing.B) {
	for _, n := range benchLouvainSizes() {
		und := defaultPlanted(n, 31)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				gonumGroups(und, 1.0)
			}
		})
	}
}

// BenchmarkLouvainNewScaleFree / BenchmarkLouvainGonumScaleFree measure the
// hard-convergence regime the planted fixtures never reach: scale-free graphs
// need hundreds of local-moving sweeps, so these are where louvainMaxSweeps
// headroom actually costs time. Reported separately from the planted numbers so
// the speedup claim is not quoted from the easy family alone.
func BenchmarkLouvainNewScaleFree(b *testing.B) {
	for _, n := range benchLouvainSizes() {
		und := buildBarabasiAlbert(n, 3, 37)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				louvainPartition(und, 1.0)
			}
		})
	}
}

func BenchmarkLouvainGonumScaleFree(b *testing.B) {
	for _, n := range benchLouvainSizes() {
		und := buildBarabasiAlbert(n, 3, 37)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				gonumGroups(und, 1.0)
			}
		})
	}
}
