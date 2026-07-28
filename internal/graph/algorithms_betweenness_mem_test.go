package graph

import (
	"fmt"
	"math"
	"math/rand/v2"
	"runtime"
	"sort"
	"testing"
	"time"

	gonumgraph "gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/simple"
)

// sampledBetweennessLegacy is a verbatim copy of the pre-#5954-slice-3
// implementation of sampledBetweenness: four per-pivot maps hinted at V
// entries (pred/sigma/dist/delta) plus a per-pivot stack, for every pivot.
//
// It is retained ONLY as the byte-identity oracle for the scratch-reusing
// production implementation. Do not call it outside tests.
func sampledBetweennessLegacy(g *simple.WeightedDirectedGraph, k int, seed uint64) map[int64]float64 {
	nodes := gonumgraph.NodesOf(g.Nodes())
	v := len(nodes)
	cb := make(map[int64]float64, v)
	if v == 0 {
		return cb
	}
	ids := make([]int64, v)
	for i, n := range nodes {
		ids[i] = n.ID()
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	if k > v {
		k = v
	}
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)) //nolint:gosec // reproducibility, not security
	perm := make([]int64, v)
	copy(perm, ids)
	for i := 0; i < k; i++ {
		j := i + int(rng.Uint64N(uint64(v-i)))
		perm[i], perm[j] = perm[j], perm[i]
	}
	pivots := perm[:k]

	succ := make(map[int64][]int64, v)
	for _, id := range ids {
		var out []int64
		it := g.From(id)
		for it.Next() {
			out = append(out, it.Node().ID())
		}
		sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
		succ[id] = out
	}

	for _, s := range pivots {
		stack := make([]int64, 0, v)
		pred := make(map[int64][]int64, v)
		sigma := make(map[int64]float64, v)
		dist := make(map[int64]int, v)
		sigma[s] = 1
		dist[s] = 0
		queue := []int64{s}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			stack = append(stack, cur)
			for _, w := range succ[cur] {
				if _, seen := dist[w]; !seen {
					dist[w] = dist[cur] + 1
					queue = append(queue, w)
				}
				if dist[w] == dist[cur]+1 {
					sigma[w] += sigma[cur]
					pred[w] = append(pred[w], cur)
				}
			}
		}
		delta := make(map[int64]float64, len(stack))
		for i := len(stack) - 1; i >= 0; i-- {
			w := stack[i]
			for _, p := range pred[w] {
				delta[p] += (sigma[p] / sigma[w]) * (1 + delta[w])
			}
			if w != s {
				cb[w] += delta[w]
			}
		}
	}

	scale := float64(v) / float64(k)
	for id := range cb {
		cb[id] *= scale
	}
	return cb
}

// buildHeavyTailedGraph builds a deterministic directed graph whose in-degree
// distribution is heavy-tailed (preferential attachment), and whose nodes are
// clustered so one cluster holds a disproportionate share of the entities.
// This matches the reference corpus shape (hub nodes, one community holding
// ~26% of entities) far better than a uniform-random synthetic graph, so
// memory numbers measured on it are not misleading.
func buildHeavyTailedGraph(n, edgesPerNode int, seed uint64) ([]Entity, []Relationship) {
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	ents := make([]Entity, n)
	for i := 0; i < n; i++ {
		// Cluster 0 holds ~26% of entities; the remainder spread over 40 more.
		cluster := 0
		if i%100 >= 26 {
			cluster = 1 + (i % 40)
		}
		ents[i] = Entity{
			ID:         fmt.Sprintf("e%07d", i),
			Name:       fmt.Sprintf("E%d", i),
			Kind:       "function",
			SourceFile: fmt.Sprintf("pkg%02d/file%d.go", cluster, i/64),
			Language:   "go",
		}
	}

	// Preferential attachment: repeat[] holds each node once per inbound edge
	// already received, so drawing uniformly from it draws proportional to
	// current in-degree — the standard Barabasi-Albert generator.
	repeat := make([]int32, 0, n*edgesPerNode*2)
	for i := 0; i < n && i < 32; i++ {
		repeat = append(repeat, int32(i))
	}

	rels := make([]Relationship, 0, n*edgesPerNode)
	seen := make(map[uint64]struct{}, n*edgesPerNode)
	for i := 32; i < n; i++ {
		for d := 0; d < edgesPerNode; d++ {
			to := int(repeat[rng.Uint64N(uint64(len(repeat)))])
			if to == i {
				continue
			}
			key := uint64(i)<<32 | uint64(uint32(to))
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			rels = append(rels, Relationship{
				ID:     fmt.Sprintf("e%07d->e%07d-%d", i, to, d),
				FromID: ents[i].ID,
				ToID:   ents[to].ID,
				Kind:   "CALLS",
			})
			repeat = append(repeat, int32(to))
		}
		repeat = append(repeat, int32(i))
	}
	return ents, rels
}

// assertSameBetweenness fails unless two raw betweenness maps are identical
// key-set and BIT-identical value-for-value (not merely close).
func assertSameBetweenness(t *testing.T, want, got map[int64]float64) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("key count differs: legacy=%d new=%d", len(want), len(got))
	}
	var maxRelErr float64
	mismatch := 0
	for id, wv := range want {
		gv, ok := got[id]
		if !ok {
			t.Fatalf("node %d present in legacy output but missing from new output", id)
		}
		if math.Float64bits(wv) != math.Float64bits(gv) {
			mismatch++
			if wv != 0 {
				if e := math.Abs(gv-wv) / math.Abs(wv); e > maxRelErr {
					maxRelErr = e
				}
			}
			if mismatch <= 5 {
				t.Errorf("node %d: legacy=%v new=%v (bits %x vs %x)", id, wv, gv,
					math.Float64bits(wv), math.Float64bits(gv))
			}
		}
	}
	if mismatch > 0 {
		t.Fatalf("%d/%d values are not bit-identical (max rel err %g)", mismatch, len(want), maxRelErr)
	}
}

// TestSampledBetweenness_BitIdenticalToLegacy is the correctness contract: the
// scratch-reusing implementation must reproduce the legacy per-pivot-allocating
// implementation BIT for BIT, on a heavy-tailed graph with hubs, multiple
// shortest paths and unreachable nodes.
func TestSampledBetweenness_BitIdenticalToLegacy(t *testing.T) {
	for _, tc := range []struct {
		name string
		n    int
		deg  int
		k    int
		seed uint64
	}{
		{"small", 300, 3, 64, 1},
		{"mid", 3000, 4, 128, 7},
		{"k-exceeds-v", 40, 3, 512, 11},
		{"dense", 800, 8, 256, 13},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ents, rels := buildHeavyTailedGraph(tc.n, tc.deg, tc.seed)
			g, idx := BuildGraph(ents, rels)
			want := sampledBetweennessLegacy(g, tc.k, betweennessSampleSeed)
			got := sampledBetweenness(g, idx.csr, tc.k, betweennessSampleSeed)
			assertSameBetweenness(t, want, got)
			nonzero := 0
			for _, v := range got {
				if v != 0 {
					nonzero++
				}
			}
			t.Logf("%s: V=%d entries=%d nonzero=%d", tc.name, tc.n, len(got), nonzero)
		})
	}
}

// TestSampledBetweenness_EmptyAndIsolated covers the degenerate shapes: an
// empty graph, and a graph with no edges at all (every BFS visits one node).
func TestSampledBetweenness_EmptyAndIsolated(t *testing.T) {
	empty := simple.NewWeightedDirectedGraph(0, 0)
	if got := sampledBetweenness(empty, nil, 8, betweennessSampleSeed); len(got) != 0 {
		t.Fatalf("empty graph: want 0 entries, got %d", len(got))
	}

	ents := make([]Entity, 50)
	for i := range ents {
		ents[i] = Entity{ID: fmt.Sprintf("e%d", i), Name: fmt.Sprintf("E%d", i), Kind: "function"}
	}
	g, idx := BuildGraph(ents, nil)
	want := sampledBetweennessLegacy(g, 8, betweennessSampleSeed)
	got := sampledBetweenness(g, idx.csr, 8, betweennessSampleSeed)
	assertSameBetweenness(t, want, got)
}

// TestComputeCentrality_ByteIdenticalOnSampledPath asserts the rounded,
// string-keyed Centrality map that actually reaches disk is unchanged.
func TestComputeCentrality_ByteIdenticalOnSampledPath(t *testing.T) {
	t.Setenv("GRAFEL_BETWEENNESS_SAMPLE_THRESHOLD", "10")
	ents, rels := buildHeavyTailedGraph(4000, 4, 3)
	g, idx := BuildGraph(ents, rels)

	legacyRaw := sampledBetweennessLegacy(g, betweennessSampleSize, betweennessSampleSeed)
	// ComputeCentrality's betweenness map is SPARSE: #5954 stopped storing a
	// rounded score of zero so that "absent" is the single representation of a
	// zero betweenness. This expectation used to be built by pre-seeding a zero
	// for every entity, which made the key-count assertion below compare 4000
	// pre-seeded keys against the ~850 the production map actually holds — the
	// test has been failing on main since that change. Build it the same way
	// ComputeCentrality does instead.
	want := make(map[string]float64, len(legacyRaw))
	for nid, v := range legacyRaw {
		if rv := roundForDeterminism(sanitizeFloat(v)); rv != 0 {
			want[idx.fromInt[nid]] = rv
		}
	}

	got, _ := ComputeCentrality(g, idx)
	if len(want) != len(got) {
		t.Fatalf("centrality key count: legacy=%d new=%d", len(want), len(got))
	}
	nonzero, mismatch := 0, 0
	for id, wv := range want {
		gv := got[id]
		if math.Float64bits(wv) != math.Float64bits(gv) {
			mismatch++
			if mismatch <= 5 {
				t.Errorf("centrality[%s]: legacy=%v new=%v", id, wv, gv)
			}
		}
		if gv != 0 {
			nonzero++
		}
	}
	if mismatch > 0 {
		t.Fatalf("%d centrality values differ after rounding", mismatch)
	}
	t.Logf("centrality: %d keys, %d nonzero, 0 mismatches after roundForDeterminism", len(got), nonzero)
}

// TestSampledBetweenness_Deterministic re-runs the same input and requires a
// bit-identical result, including across an interleaved unrelated run (which
// would expose scratch left dirty between calls).
func TestSampledBetweenness_Deterministic(t *testing.T) {
	ents, rels := buildHeavyTailedGraph(2000, 4, 21)
	g, idx := BuildGraph(ents, rels)
	a := sampledBetweenness(g, idx.csr, 128, betweennessSampleSeed)
	other, orels := buildHeavyTailedGraph(500, 3, 22)
	og, oidx := BuildGraph(other, orels)
	_ = sampledBetweenness(og, oidx.csr, 64, betweennessSampleSeed)
	b := sampledBetweenness(g, idx.csr, 128, betweennessSampleSeed)
	assertSameBetweenness(t, a, b)
}

// betweennessMemProbe runs fn while sampling runtime HeapInuse, and returns the
// peak HeapInuse observed above the pre-call baseline plus the total bytes
// allocated (churn) during the call.
//
// Peak HeapInuse — not TotalAlloc — is the number that decides whether the
// indexer swaps, so it is what this asserts. TotalAlloc is reported alongside
// because it is deterministic and pins down per-pivot allocation directly.
func betweennessMemProbe(fn func()) (peakHeapInuse, totalAlloc uint64) {
	runtime.GC()
	var base runtime.MemStats
	runtime.ReadMemStats(&base)

	done := make(chan struct{})
	peakCh := make(chan uint64, 1)
	go func() {
		var peak uint64
		var ms runtime.MemStats
		for {
			select {
			case <-done:
				peakCh <- peak
				return
			default:
			}
			runtime.ReadMemStats(&ms)
			if ms.HeapInuse > peak {
				peak = ms.HeapInuse
			}
			time.Sleep(200 * time.Microsecond)
		}
	}()

	fn()

	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	close(done)
	peak := <-peakCh
	if after.HeapInuse > peak {
		peak = after.HeapInuse
	}
	if peak > base.HeapInuse {
		peakHeapInuse = peak - base.HeapInuse
	}
	return peakHeapInuse, after.TotalAlloc - base.TotalAlloc
}

// TestSampledBetweenness_ScratchIsReusedAcrossPivots is the anti-regression
// guard for the whole point of this change: scratch must be allocated ONCE,
// not per pivot.
//
// The assertion is on the MARGINAL cost of a pivot: the same graph is run at
// two pivot counts and the churn difference is divided by the pivot-count
// difference. Every one-time cost (CSR build, scratch allocation, result map)
// cancels, so the number left is exactly "bytes allocated per additional
// pivot". Legacy allocates four V-hinted maps + a V-capacity stack per pivot,
// which at V=60k is several MB; the scratch-reusing version must be ~0.
func TestSampledBetweenness_ScratchIsReusedAcrossPivots(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping memory-budget test in -short mode")
	}
	const n = 60_000
	ents, rels := buildHeavyTailedGraph(n, 5, 0x5954)
	g, idx := BuildGraph(ents, rels)

	const kLo, kHi = 64, 512
	var out map[int64]float64
	_, churnLo := betweennessMemProbe(func() {
		out = sampledBetweenness(g, idx.csr, kLo, betweennessSampleSeed)
	})
	runtime.KeepAlive(out)
	peakHeap, churnHi := betweennessMemProbe(func() {
		out = sampledBetweenness(g, idx.csr, kHi, betweennessSampleSeed)
	})
	runtime.KeepAlive(out)
	runtime.KeepAlive(g)

	var marginal float64
	if churnHi > churnLo {
		marginal = float64(churnHi-churnLo) / float64(kHi-kLo)
	}
	t.Logf("V=%d E=%d: churn K=%d -> %.1f MB, K=%d -> %.1f MB; marginal = %.1f KB/pivot; peak HeapInuse over baseline (K=%d) = %.1f MB; result entries = %d",
		n, len(rels),
		kLo, float64(churnLo)/(1<<20), kHi, float64(churnHi)/(1<<20),
		marginal/(1<<10), kHi, float64(peakHeap)/(1<<20), len(out))

	// At V=60k the legacy per-pivot scratch is >1 MB. 32 KB/pivot leaves ample
	// room for the result map filling in as more pivots run, while being ~30x
	// below anything that allocates per-node state per pivot.
	if marginal > 32*1024 {
		t.Errorf("marginal allocation %.1f KB per additional pivot exceeds 32 KB — scratch is not being reused across pivots",
			marginal/(1<<10))
	}

	// Peak HeapInuse: scratch is O(V+E) ints/floats. At V=60k / E~300k that is
	// well under the budget even with the result map and GC slack. Legacy peaked
	// at ~161 MB over baseline on this exact shape.
	const peakBudget = 64 << 20
	if peakHeap > peakBudget {
		t.Errorf("peak HeapInuse over baseline %.1f MB exceeds budget %.1f MB",
			float64(peakHeap)/(1<<20), float64(peakBudget)/(1<<20))
	}
}

// BenchmarkSampledBetweenness_HeavyTailed measures the production pivot count
// on a heavy-tailed graph and reports peak HeapInuse over baseline as a custom
// metric, alongside the standard -benchmem churn numbers.
func BenchmarkSampledBetweenness_HeavyTailed(b *testing.B) {
	const n = 60_000
	ents, rels := buildHeavyTailedGraph(n, 5, 0x5954)
	g, idx := BuildGraph(ents, rels)

	var peakSum, iters uint64
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out map[int64]float64
		p, _ := betweennessMemProbe(func() {
			out = sampledBetweenness(g, idx.csr, betweennessSampleSize, betweennessSampleSeed)
		})
		runtime.KeepAlive(out)
		peakSum += p
		iters++
	}
	b.StopTimer()
	if iters > 0 {
		b.ReportMetric(float64(peakSum/iters)/(1<<20), "peakHeapInuseMB")
	}
	runtime.KeepAlive(g)
}

// BenchmarkSampledBetweennessLegacy_HeavyTailed is the before-picture for the
// same shape, so the peak-HeapInuse delta is reproducible on demand.
func BenchmarkSampledBetweennessLegacy_HeavyTailed(b *testing.B) {
	const n = 60_000
	ents, rels := buildHeavyTailedGraph(n, 5, 0x5954)
	g, _ := BuildGraph(ents, rels)

	var peakSum, iters uint64
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out map[int64]float64
		p, _ := betweennessMemProbe(func() {
			out = sampledBetweennessLegacy(g, betweennessSampleSize, betweennessSampleSeed)
		})
		runtime.KeepAlive(out)
		peakSum += p
		iters++
	}
	b.StopTimer()
	if iters > 0 {
		b.ReportMetric(float64(peakSum/iters)/(1<<20), "peakHeapInuseMB")
	}
	runtime.KeepAlive(g)
}
