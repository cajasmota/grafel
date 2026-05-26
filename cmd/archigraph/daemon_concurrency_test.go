package main

import (
	"os"
	"runtime"
	"testing"
)

// TestDefaultRebuildConcurrency verifies the Phase 1 auto-tune formula
// (post-#2141 P0.2, streaming FB writes): min(16, totalMemoryMB/2048), floored at 2.
//
// Previous formula was min(8, sysMB/4096); the cap was raised because per-rebuild
// peak RSS dropped from ~2 GB to ~800 MB after streaming FB writes landed.
// See issue #2147 for the phased evolution plan.
func TestDefaultRebuildConcurrency(t *testing.T) {
	cases := []struct {
		sysMB int64
		want  int
		label string
	}{
		{sysMB: 0, want: 2, label: "sysinfo unavailable → fallback floor"},
		{sysMB: 2048, want: 2, label: "2GB: 2048/2048=1 → floor=2"},
		{sysMB: 4096, want: 2, label: "4GB: 4096/2048=2 → 2"},
		{sysMB: 8192, want: 4, label: "8GB: 8192/2048=4 → 4"},
		{sysMB: 16384, want: 8, label: "16GB: 16384/2048=8 → 8"},
		{sysMB: 32768, want: 16, label: "32GB: 32768/2048=16 → 16"},
		{sysMB: 65536, want: 16, label: "64GB: 65536/2048=32 → ceiling=16"},
		{sysMB: 131072, want: 16, label: "128GB: 131072/2048=64 → ceiling=16"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.label, func(t *testing.T) {
			got := computeRebuildConcurrency(tc.sysMB)
			if got != tc.want {
				t.Errorf("computeRebuildConcurrency(%d) = %d, want %d", tc.sysMB, got, tc.want)
			}
		})
	}
}

// TestRebuildConcurrencyEnvOverride verifies that ARCHIGRAPH_REBUILD_CONCURRENCY
// overrides the auto-tuned default when runDaemon parses its flags.
// We test the env-parse path directly by inspecting the resolved default.
func TestRebuildConcurrencyEnvOverride(t *testing.T) {
	orig := os.Getenv("ARCHIGRAPH_REBUILD_CONCURRENCY")
	defer os.Setenv("ARCHIGRAPH_REBUILD_CONCURRENCY", orig)

	// Set override to 6.
	t.Setenv("ARCHIGRAPH_REBUILD_CONCURRENCY", "6")

	// resolveEnvRebuildConcurrency replicates the env-parse logic from runDaemon.
	got := resolveEnvRebuildConcurrency()
	if got != 6 {
		t.Errorf("ARCHIGRAPH_REBUILD_CONCURRENCY=6: got %d, want 6", got)
	}
}

// TestRebuildConcurrencyEnvInvalid verifies that an invalid env value falls
// back to the auto-tuned default rather than crashing.
func TestRebuildConcurrencyEnvInvalid(t *testing.T) {
	orig := os.Getenv("ARCHIGRAPH_REBUILD_CONCURRENCY")
	defer os.Setenv("ARCHIGRAPH_REBUILD_CONCURRENCY", orig)

	t.Setenv("ARCHIGRAPH_REBUILD_CONCURRENCY", "not-a-number")

	got := resolveEnvRebuildConcurrency()
	// Should be the auto-tuned value (≥2), not 0 or an error.
	if got < 2 {
		t.Errorf("invalid env: got %d, want ≥2 (auto-tuned floor)", got)
	}
}

// TestRebuildConcurrencyScenarios exercises the three scenarios from issue #2147:
//  1. 1-repo workload: concurrency is floored (no over-parallelization).
//  2. Small memory (≤8 GB): concurrency stays low even with many repos.
//  3. Large memory (32 GB): concurrency reaches the new Phase 1 ceiling of 16.
//
// These scenarios validate that the Phase 1 formula (min(16, sysMB/2048), floor=2)
// is correct across the range of realistic hardware configurations.
func TestRebuildConcurrencyScenarios(t *testing.T) {
	t.Run("scenario_1repo_floor", func(t *testing.T) {
		// Single-repo workload: any machine gives at least 2, but the rebuild
		// path itself serialises when only 1 repo exists (conc==1 || len(work)<=1).
		// This test verifies the formula floor is 2, not 0 or 1.
		got := computeRebuildConcurrency(8192) // 8 GB machine, 1 repo scenario
		if got < 2 {
			t.Errorf("1-repo scenario: computeRebuildConcurrency(8192) = %d, want ≥2", got)
		}
		// The rebuild core will take the serial path when len(work)==1,
		// so the formula only needs to be safe — floor=2 is correct.
		if got != 4 {
			t.Errorf("1-repo scenario: computeRebuildConcurrency(8192) = %d, want 4", got)
		}
	})

	t.Run("scenario_4repos_small_memory", func(t *testing.T) {
		// 4 repos on a memory-constrained machine (8 GB):
		// formula gives min(16, 8192/2048) = min(16, 4) = 4.
		// All 4 repos can potentially run in parallel on 8 GB.
		got := computeRebuildConcurrency(8192)
		want := 4
		if got != want {
			t.Errorf("4-repo small-memory scenario: computeRebuildConcurrency(8192) = %d, want %d", got, want)
		}
		// On 8 GB, 4 concurrent × 800 MB peak = 3.2 GB — well within headroom.
	})

	t.Run("scenario_10repos_large_memory", func(t *testing.T) {
		// 10+ repos on a large machine (32 GB):
		// formula gives min(16, 32768/2048) = min(16, 16) = 16.
		// Ceiling of 16 is reached; 16 × 800 MB = 12.8 GB peak, safe on 32 GB.
		got := computeRebuildConcurrency(32768)
		want := 16
		if got != want {
			t.Errorf("10-repo large-memory scenario: computeRebuildConcurrency(32768) = %d, want %d", got, want)
		}
	})
}

// TestRebuildConcurrencyFormulaProperties verifies key correctness properties
// of the Phase 1 formula (issue #2147):
//   - Result is always in [2, 16].
//   - Result is monotonically non-decreasing with sysMB.
//   - The ceiling (16) is not exceeded even on very large machines.
func TestRebuildConcurrencyFormulaProperties(t *testing.T) {
	samples := []int64{0, 512, 1024, 2048, 4096, 8192, 16384, 32768, 65536, 131072, 524288}
	prev := 0
	for _, sysMB := range samples {
		got := computeRebuildConcurrency(sysMB)
		if got < 2 {
			t.Errorf("sysMB=%d: result %d < floor 2", sysMB, got)
		}
		if got > 16 {
			t.Errorf("sysMB=%d: result %d > ceiling 16", sysMB, got)
		}
		if got < prev {
			t.Errorf("sysMB=%d: result %d decreased from %d (must be monotonic)", sysMB, got, prev)
		}
		prev = got
	}
}

// TestRebuildConcurrencyNumCPURelationship documents the relationship between
// the memory-based formula and the CPU count on the current machine.
// This is a documentation test — it always passes but prints a useful log line.
func TestRebuildConcurrencyNumCPURelationship(t *testing.T) {
	// The formula is intentionally memory-based (not CPU-based) because rebuild
	// jobs are I/O and heap-intensive, not CPU-bound. CPU info is provided for
	// context only. See issue #2147 for rationale.
	numCPU := runtime.NumCPU()
	// Use a representative 16 GB machine value.
	concurrency := computeRebuildConcurrency(16384)
	t.Logf("host: NumCPU=%d, formula(16GB)=%d concurrent rebuilds", numCPU, concurrency)
}
