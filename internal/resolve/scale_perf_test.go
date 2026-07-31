//go:build perf

// Scaling assertion for BuildIndexFromModules (M5).
//
// Gated behind the `perf` build tag (see CONTRIBUTING.md, "Performance tests").
// The correctness properties of BuildIndexFromModules — edge parity with the
// flat BuildIndex, and the resolution rate — stay in the default suite as
// TestBuildIndexFromModules_Parity and TestBuildIndexFromModules_ResolutionRate.
// Only the wall-clock ratio lives here.
//
//	go test -tags perf ./internal/resolve/ -run SubQuadratic -v

package resolve

import "testing"

// TestBuildIndexFromModules_SubQuadratic bounds the ratio of build times at
// 100 vs 1000 modules.
//
// Aggregation is the MINIMUM of the per-N samples: a GC pause or co-scheduled
// spike can only ever inflate a sample, so the smallest sample is the one least
// corrupted by noise. A quadratic code path is slower by construction on every
// run including the cleanest one, so switching to min loses none of the
// regression-detection power.
//
// A 40x bound sits ~2.7x above the n-log-n expectation yet ~2.5x below the
// quadratic signal, so a genuine O(N²) reintroduction still fails loudly
// (~100x) while CI jitter never does. It is still a wall-clock ratio, and so
// still not a measurement a shared runner can be trusted to make.
func TestBuildIndexFromModules_SubQuadratic(t *testing.T) {
	const (
		entPerMod = 50
		smallN    = 100
		largeN    = 1000 // 10x corpus → n-log-n ≈ 15x time, quadratic ≈ 100x
		samples   = 8    // min of N runs damps CI outliers without masking true cost
		bound     = 40.0 // ~2.7x over n-log-n (15x), ~2.5x under quadratic (100x)
	)

	minRun := func(numMods int) int64 {
		modules, _ := syntheticModules(numMods, entPerMod)
		best := int64(-1)
		for i := 0; i < samples; i++ {
			t0 := nanoTime()
			_ = BuildIndexFromModules(modules, 0)
			d := nanoTime() - t0
			if best < 0 || d < best {
				best = d
			}
		}
		return best
	}

	tSmall := minRun(smallN)
	tLarge := minRun(largeN)

	// +1µs floor on the denominator guards against a 0ns small measurement.
	ratio := float64(tLarge) / float64(tSmall+1000)
	t.Logf("%d-mod min: %dns  %d-mod min: %dns  ratio: %.2fx (n-log-n≈15.0, quadratic≈100.0)",
		smallN, tSmall, largeN, tLarge, ratio)

	if ratio > bound {
		t.Errorf("scaling ratio %.2fx exceeds %.0fx threshold (%dx corpus) — possible O(N²) regression",
			ratio, bound, largeN/smallN)
	}
}
