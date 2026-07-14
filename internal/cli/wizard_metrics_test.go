package cli

import (
	"testing"

	"github.com/cajasmota/grafel/internal/statusfile"
)

// TestEngineMetricsFuncWith_ReturnsFreshRSSAndCPU is the RED test for the
// wizard CPU/RAM readout's cli-side reader: given a fresh engine-liveness
// sidecar carrying RSSMB/CPUPct, the built wiztui.MetricsFunc must surface
// them unchanged.
func TestEngineMetricsFuncWith_ReturnsFreshRSSAndCPU(t *testing.T) {
	fake := func(root string) (*statusfile.File, bool) {
		if root != "/fake/root" {
			t.Fatalf("root = %q, want /fake/root", root)
		}
		return &statusfile.File{RSSMB: 2355, CPUPct: 412}, true
	}
	fn := engineMetricsFuncWith("/fake/root", fake)

	got := fn()
	if got.RSSMB != 2355 {
		t.Errorf("RSSMB = %d, want 2355", got.RSSMB)
	}
	if got.CPUPct != 412 {
		t.Errorf("CPUPct = %v, want 412", got.CPUPct)
	}
}

// TestEngineMetricsFuncWith_MissingOrStaleReturnsZero proves the reader
// degrades gracefully — never panics, never errors — when no engine has ever
// written the sidecar, or the last write is stale: the TUI must never break
// because the metric is unavailable, it must simply omit the readout.
func TestEngineMetricsFuncWith_MissingOrStaleReturnsZero(t *testing.T) {
	missing := func(root string) (*statusfile.File, bool) { return nil, false }
	fn := engineMetricsFuncWith("/fake/root", missing)

	got := fn()
	if got.RSSMB != 0 || got.CPUPct != 0 {
		t.Errorf("got = %+v, want zero Metrics on missing status file", got)
	}

	stale := func(root string) (*statusfile.File, bool) {
		return &statusfile.File{RSSMB: 999, CPUPct: 50}, false // fresh=false
	}
	fn2 := engineMetricsFuncWith("/fake/root", stale)
	got2 := fn2()
	if got2.RSSMB != 0 || got2.CPUPct != 0 {
		t.Errorf("got = %+v, want zero Metrics on stale status file", got2)
	}
}
