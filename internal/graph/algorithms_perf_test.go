//go:build perf

// Performance / memory-budget assertions for the centrality algorithms.
//
// Gated behind the `perf` build tag (see CONTRIBUTING.md, "Performance tests").
// Both tests here build 28k- and 60k-node synthetic graphs and assert on
// wall-clock or on runtime.MemStats deltas — neither is a stable measurement on
// a shared CI runner, and neither is a correctness property.
//
//	go test -tags perf ./internal/graph/ -run 'PerfBudget|ScratchIsReused' -v

package graph

import (
	"runtime"
	"testing"
	"time"
)

// TestBetweennessPerfBudget_28k is the perf guard: on a >=28k-entity synthetic
// group the centrality pass (with sampling enabled by the node-count gate)
// completes under a budget.
//
// The shape assertions at the end (pr density, betw sparsity) are duplicated by
// TestBetweennessSampleThresholdGate in the default suite, which runs the same
// code path on a smaller graph — gating this file does not remove them from the
// release gate.
func TestBetweennessPerfBudget_28k(t *testing.T) {
	const n = 28000
	const budget = 60 * time.Second

	ents, rels := buildSyntheticGraph(n, 5, 99)
	g, idx := BuildGraph(ents, rels)
	if int(idx.next) <= betweennessSampleThresholdValue() {
		t.Fatalf("28k graph (%d nodes) did not exceed sampling threshold %d", idx.next, betweennessSampleThresholdValue())
	}

	start := time.Now()
	betw, pr := ComputeCentrality(g, idx)
	elapsed := time.Since(start)
	t.Logf("28k-node centrality (sampled betweenness) completed in %s (budget %s)", elapsed, budget)
	if elapsed > budget {
		t.Errorf("centrality pass took %s, over budget %s", elapsed, budget)
	}
	// PageRank is dense (a score per node); betweenness is sparse since #5954 —
	// non-zero scores only, no zero pre-seed.
	if len(pr) != n {
		t.Errorf("expected %d pr keys, got %d", n, len(pr))
	}
	if len(betw) == 0 || len(betw) > n {
		t.Errorf("expected 1..%d betw keys, got %d", n, len(betw))
	}
}

// TestSampledBetweenness_ScratchIsReusedAcrossPivots pins that the per-pivot
// scratch is allocated once, not per pivot.
//
// The assertion is on the MARGINAL cost of a pivot: the same graph is run at
// two pivot counts and the churn difference is divided by the pivot-count
// difference. Every one-time cost (CSR build, scratch allocation, result map)
// cancels, so the number left is exactly "bytes allocated per additional
// pivot". Legacy allocates four V-hinted maps + a V-capacity stack per pivot,
// which at V=60k is several MB; the scratch-reusing version must be ~0.
//
// runtime.MemStats deltas on a 60k-node graph move with the GC's mood and with
// whatever else the runner is doing, so this is a perf measurement, not a
// correctness check. The correctness property it shadows — that scratch reuse
// does not change the numbers — is
// TestSampledBetweenness_BitIdenticalToLegacy, which stays in the gate.
func TestSampledBetweenness_ScratchIsReusedAcrossPivots(t *testing.T) {
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
