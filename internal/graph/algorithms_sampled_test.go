package graph

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"testing"

	"gonum.org/v1/gonum/graph/network"
)

// buildSyntheticGraph generates a deterministic synthetic graph with n entities
// and roughly avgDeg outbound edges per node, using a fixed seed so tests are
// reproducible. A handful of "hub" nodes receive a disproportionate share of
// inbound edges so betweenness/PageRank have a clear top tier to preserve.
func buildSyntheticGraph(n, avgDeg int, seed uint64) ([]Entity, []Relationship) {
	rng := rand.New(rand.NewPCG(seed, seed^0xabcdef))
	ents := make([]Entity, n)
	for i := 0; i < n; i++ {
		ents[i] = Entity{
			ID:         fmt.Sprintf("e%07d", i),
			Name:       fmt.Sprintf("E%d", i),
			Kind:       "function",
			SourceFile: fmt.Sprintf("pkg%d/file%d.go", i%50, i),
			Language:   "go",
		}
	}
	// Designate ~0.5% of nodes as hubs.
	numHubs := n / 200
	if numHubs < 5 {
		numHubs = 5
	}
	hubs := make([]int, numHubs)
	for i := range hubs {
		hubs[i] = int(rng.Uint64N(uint64(n)))
	}

	var rels []Relationship
	for i := 0; i < n; i++ {
		deg := avgDeg
		for d := 0; d < deg; d++ {
			var to int
			// 40% of edges point at a hub (creates the importance tier).
			if rng.Uint64N(100) < 40 {
				to = hubs[int(rng.Uint64N(uint64(numHubs)))]
			} else {
				to = int(rng.Uint64N(uint64(n)))
			}
			if to == i {
				continue
			}
			rels = append(rels, Relationship{
				ID:     fmt.Sprintf("e%07d->e%07d-%d", i, to, d),
				FromID: ents[i].ID,
				ToID:   ents[to].ID,
				Kind:   "CALLS",
			})
		}
	}
	return ents, rels
}

// topKByValue returns the IDs of the top-k entries by value, ties broken by id
// (matches the determinism contract used elsewhere).
func topKByValue(m map[string]float64, k int) []string {
	type row struct {
		id string
		v  float64
	}
	rows := make([]row, 0, len(m))
	for id, v := range m {
		rows = append(rows, row{id, v})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].v != rows[j].v {
			return rows[i].v > rows[j].v
		}
		return rows[i].id < rows[j].id
	})
	if k > len(rows) {
		k = len(rows)
	}
	out := make([]string, k)
	for i := 0; i < k; i++ {
		out[i] = rows[i].id
	}
	return out
}

// TestBetweennessSampleThresholdGate verifies the node-count gate: below the
// threshold the exact path runs, above it the sampled approximation runs. We
// drive it through the env override so the test stays fast.
func TestBetweennessSampleThresholdGate(t *testing.T) {
	// Force a tiny threshold so a small graph trips the sampled path.
	t.Setenv("GRAFEL_BETWEENNESS_SAMPLE_THRESHOLD", "10")
	if got := betweennessSampleThresholdValue(); got != 10 {
		t.Fatalf("threshold override not honoured: got %d want 10", got)
	}

	ents, rels := buildSyntheticGraph(200, 4, 1)
	g, idx := BuildGraph(ents, rels)
	// 200 nodes > threshold(10) -> sampled path must be taken. We can't observe
	// the branch directly, but we CAN assert the result is non-empty and that
	// the same call is deterministic (seeded), which only the sampled path
	// guarantees at this size (exact Betweenness is also deterministic, so we
	// additionally verify the dedicated sampled function matches the gated call).
	betw, _ := ComputeCentrality(g, idx)
	direct := sampledBetweenness(g, idx.csr, betweennessSampleSize, betweennessSampleSeed)
	// The gated ComputeCentrality rounds for determinism; compare top-tier.
	gatedTop := topKByValue(betw, 10)
	directScaled := map[string]float64{}
	for nid, v := range direct {
		directScaled[idx.fromInt[nid]] = v
	}
	directTop := topKByValue(directScaled, 10)
	overlap := overlapFraction(gatedTop, directTop)
	if overlap < 0.9 {
		t.Errorf("gated sampled path diverges from direct sampledBetweenness: overlap=%.2f", overlap)
	}

	// Below threshold: exact path. Make the threshold huge so a small graph
	// stays exact, and a separate run must still produce sensible scores.
	t.Setenv("GRAFEL_BETWEENNESS_SAMPLE_THRESHOLD", "100000")
	if got := betweennessSampleThresholdValue(); got != 100000 {
		t.Fatalf("threshold override not honoured: got %d want 100000", got)
	}
	betwExact, _ := ComputeCentrality(g, idx)
	// The betweenness map is sparse (#5954): it carries non-zero scores only,
	// never a zero pre-seed, so it must be non-empty but no larger than the
	// node set.
	if len(betwExact) == 0 || len(betwExact) > len(ents) {
		t.Errorf("exact path: betw map has %d keys, want 1..%d", len(betwExact), len(ents))
	}
	for id, v := range betwExact {
		if v == 0 {
			t.Errorf("exact path: betw map holds an explicit zero for %q; zeros must be absent", id)
			break
		}
	}
}

// TestBetweennessSampledDeterministic confirms the sampled approximation is
// byte-reproducible (fixed seed) across repeated calls on the same graph.
func TestBetweennessSampledDeterministic(t *testing.T) {
	ents, rels := buildSyntheticGraph(1000, 4, 7)
	g, idx := BuildGraph(ents, rels)
	a := sampledBetweenness(g, idx.csr, 256, betweennessSampleSeed)
	b := sampledBetweenness(g, idx.csr, 256, betweennessSampleSeed)
	if len(a) != len(b) {
		t.Fatalf("non-deterministic key count: %d vs %d", len(a), len(b))
	}
	for k, va := range a {
		if vb := b[k]; va != vb {
			t.Fatalf("non-deterministic value for %d: %v vs %v", k, va, vb)
		}
	}
}

// TestBetweennessSampledTop50Overlap is the quality acceptance test: on a
// mid-size synthetic graph, the sampled betweenness must agree with EXACT
// betweenness on the top-50 nodes by >= 0.9 overlap (the important nodes — the
// god-node tier — are preserved by the approximation).
func TestBetweennessSampledTop50Overlap(t *testing.T) {
	ents, rels := buildSyntheticGraph(2500, 5, 42)
	g, idx := BuildGraph(ents, rels)

	// EXACT unweighted Brandes (network.Betweenness) as ground truth.
	exactRaw := network.Betweenness(g)
	exact := map[string]float64{}
	for nid, v := range exactRaw {
		exact[idx.fromInt[nid]] = v
	}

	// Sampled with the production K.
	sampRaw := sampledBetweenness(g, idx.csr, betweennessSampleSize, betweennessSampleSeed)
	samp := map[string]float64{}
	for nid, v := range sampRaw {
		samp[idx.fromInt[nid]] = v
	}

	exactTop := topKByValue(exact, 50)
	sampTop := topKByValue(samp, 50)
	overlap := overlapFraction(exactTop, sampTop)
	t.Logf("full-vs-sampled top-50 betweenness overlap = %.3f (K=%d, V=%d)", overlap, betweennessSampleSize, len(ents))
	if overlap < 0.9 {
		t.Errorf("top-50 overlap %.3f < 0.9 — sampled approximation lost too many important nodes", overlap)
	}
}

// TestBetweennessPerfBudget_28k lives in algorithms_perf_test.go behind the
// `perf` build tag: a 60s wall-clock budget on a 28k-node synthetic graph is
// not a measurement a shared CI runner can make.

// overlapFraction returns |A ∩ B| / |A| for two ID slices.
func overlapFraction(a, b []string) float64 {
	if len(a) == 0 {
		return 1
	}
	set := make(map[string]struct{}, len(b))
	for _, id := range b {
		set[id] = struct{}{}
	}
	hit := 0
	for _, id := range a {
		if _, ok := set[id]; ok {
			hit++
		}
	}
	return float64(hit) / float64(len(a))
}
