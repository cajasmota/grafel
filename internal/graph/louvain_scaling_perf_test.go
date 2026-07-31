//go:build perf

// Performance / scaling assertions for the Louvain implementation.
//
// Gated behind the `perf` build tag (see CONTRIBUTING.md, "Performance tests").
// These assertions measure wall-clock behaviour and are only meaningful on a
// machine that is not sharing its CPU with anything else. On a shared GitHub
// Actions runner the fitted exponent moves with the neighbours, not with the
// code — TestLouvainScalingExponent has failed the release gate at N^1.72 on a
// loaded Windows runner with no algorithmic change in the diff.
//
// Run locally, or via the `perf` workflow:
//
//	go test -tags perf ./internal/graph/ -run Scaling -v

package graph

import (
	"math"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Scaling
// ---------------------------------------------------------------------------

// TestLouvainScalingExponent — gonum's local mover scales ~N^1.8 because a
// sweep costs Σ|c|²; the neighbour-side formulation costs O(E) per sweep, i.e.
// ~N^1.0-1.2 on a fixed-degree family.
//
// Pinning the EXPONENT rather than a wall-clock threshold is deliberate: a
// fixed threshold passes on a fast machine even after an asymptotic
// regression, an exponent does not. That property is exactly why it cannot
// live in a shared-runner gate — the fit is only stable on a quiet machine.
func TestLouvainScalingExponent(t *testing.T) {
	sizes := []int{8000, 16000, 32000, 64000}
	var logN, logT []float64
	for _, n := range sizes {
		und := defaultPlanted(n, 21)
		// Warm up allocator/page cache, then take the best of 3 to damp noise.
		best := time.Duration(math.MaxInt64)
		for r := 0; r < 3; r++ {
			start := time.Now()
			louvainPartition(und, 1.0)
			if d := time.Since(start); d < best {
				best = d
			}
		}
		t.Logf("n=%-6d louvain=%v", n, best)
		logN = append(logN, math.Log(float64(n)))
		logT = append(logT, math.Log(float64(best.Nanoseconds())))
	}
	exp := leastSquaresSlope(logN, logT)
	t.Logf("fitted scaling exponent: N^%.2f", exp)
	if exp > 1.45 {
		t.Errorf("scaling exponent N^%.2f exceeds 1.45 — the O(E)-per-sweep property has regressed", exp)
	}
}
