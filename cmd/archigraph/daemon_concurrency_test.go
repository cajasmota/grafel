package main

import (
	"os"
	"testing"
)

// TestDefaultRebuildConcurrency verifies the auto-tune formula:
// min(8, totalMemoryMB/4096), floored at 2.
func TestDefaultRebuildConcurrency(t *testing.T) {
	cases := []struct {
		sysMB int64
		want  int
		label string
	}{
		{sysMB: 0, want: 2, label: "sysinfo unavailable → fallback floor"},
		{sysMB: 2048, want: 2, label: "4GB: 2048/4096=0 → floor=2"},
		{sysMB: 4096, want: 2, label: "4GB (exact): 4096/4096=1 → floor=2"},
		{sysMB: 8192, want: 2, label: "8GB: 8192/4096=2 → 2"},
		{sysMB: 16384, want: 4, label: "16GB: 16384/4096=4 → 4"},
		{sysMB: 32768, want: 8, label: "32GB: 32768/4096=8 → 8"},
		{sysMB: 131072, want: 8, label: "128GB: 131072/4096=32 → ceiling=8"},
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
