//go:build perf

// Scaling assertion for the response-shape Python memoization (#5143).
//
// Gated behind the `perf` build tag (see CONTRIBUTING.md, "Performance tests").
// The correctness sibling that stays in the default suite is
// TestResponseShapePython_MemoizationIdentical. Do not overstate what it
// covers: it compares a warm run against a run after resetPyIndexCache(), so
// both sides execute the SAME memoized code. That proves cache-reset
// determinism — the memo does not leak state across resets — not equivalence
// with a pre-memoization reference implementation, which no longer exists in
// the tree to compare against. Only the wall-clock ratio lives here.
//
//	go test -tags perf ./internal/engine/ -run NearLinearScaling -v

package engine

import (
	"testing"
	"time"
)

// TestResponseShapePython_NearLinearScaling times the corpus pass at two corpus
// sizes and bounds the ratio.
//
// Aggregation: the MINIMUM of the per-N samples, not the median. The minimum is
// the least-contended observation in the batch, so it is the best available
// estimate of true algorithmic cost: any noise event can only push a sample
// *away* from the true cost (upward), never below it, so the smallest sample is
// the one noise corrupted least.
//
// Threshold: a 30x bound gives 3x headroom over the 10x linear expectation —
// generous slack for fixed overhead, GC, and residual runner contention — while
// staying ~3.3x below the quadratic signal (100x). A genuine O(n²)
// reintroduction produces a ratio an order of magnitude past this bound.
//
// Even so this is a wall-clock measurement, and #5751 was a false red on a
// contended Windows runner. It belongs on a quiet machine, not in the gate.
func TestResponseShapePython_NearLinearScaling(t *testing.T) {
	const (
		smallN  = 100
		largeN  = 1000 // 10x the corpus → linear≈10x time, quadratic≈100x
		samples = 8    // min of N runs damps CI outliers without masking true cost
		bound   = 30.0 // ~3x over linear (10x), ~3.3x under quadratic (100x)
	)
	timeSmall := minCorpus(t, smallN, samples)
	timeLarge := minCorpus(t, largeN, samples)

	// +1µs floor on the denominator guards against a 0ns small measurement.
	ratio := float64(timeLarge) / float64(timeSmall+time.Microsecond)
	t.Logf("scaling: N=%d min %v, N=%d min %v, ratio=%.2f (linear≈10.0, quadratic≈100.0)",
		smallN, timeSmall, largeN, timeLarge, ratio)
	if ratio > bound {
		t.Fatalf("super-linear scaling detected: %dx corpus took %.2fx time (want <%.0fx); "+
			"the O(n^2) regression is back", largeN/smallN, ratio, bound)
	}
}
