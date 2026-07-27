package mcp

import (
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/statusfile"
)

// A user-awaited rebuild is DONE once extraction + links have written graph.fb.
// The group-algo annotation pass that follows writes a separate overlay and
// never touches graph.fb, so it must not keep grafel_stats reporting
// "is_indexing" — that is the flag agents and the dashboard poll to decide
// whether the graph is usable, and on the reference corpus the annotation pass
// runs for hours.
func TestApplyBusyTotals_EngineSidecar_GroupAlgoIsEnhancingNotIndexing(t *testing.T) {
	started := time.Now().Add(-3 * time.Hour).UTC().Truncate(time.Second)
	totals := map[string]any{}
	applyBusyTotals(totals, &statusfile.File{
		EngineInFlight:          0, // extraction finished
		EngineGroupAlgoInFlight: 1, // annotation pass still running
		EngineBusyStartedAt:     started,
	}, true)

	if got := totals["is_indexing"]; got != false {
		t.Fatalf("a running group-algo pass must NOT report is_indexing (the rebuild is complete and the graph is queryable), got %v", got)
	}
	if got := totals["is_enhancing"]; got != true {
		t.Fatalf("a running group-algo pass must report is_enhancing so the pending overlay is diagnosable, got %v", got)
	}
	if got := totals["group_algo_in_flight"]; got != 1 {
		t.Fatalf("group_algo_in_flight = %v, want 1", got)
	}
	if _, ok := totals["indexing_in_flight"]; ok {
		t.Fatalf("indexing_in_flight must be absent when nothing is extracting: %v", totals["indexing_in_flight"])
	}
	if got := totals["indexing_started_at"]; got != started.Format(time.RFC3339) {
		t.Fatalf("indexing_started_at = %v, want %v", got, started.Format(time.RFC3339))
	}
}

// Real extraction still reports is_indexing.
func TestApplyBusyTotals_EngineSidecar_ExtractionIsIndexing(t *testing.T) {
	totals := map[string]any{}
	applyBusyTotals(totals, &statusfile.File{EngineInFlight: 2}, true)

	if got := totals["is_indexing"]; got != true {
		t.Fatalf("is_indexing = %v, want true", got)
	}
	if got := totals["is_enhancing"]; got != false {
		t.Fatalf("is_enhancing = %v, want false", got)
	}
	if got := totals["indexing_in_flight"]; got != 2 {
		t.Fatalf("indexing_in_flight = %v, want 2", got)
	}
}

// An idle daemon reports both flags false — is_enhancing must be PRESENT and
// false, not omitted, so "not yet computed" is a defined state a consumer can
// read rather than an absent key it has to guess about.
func TestApplyBusyTotals_EngineSidecar_IdleReportsBothFlags(t *testing.T) {
	totals := map[string]any{}
	applyBusyTotals(totals, &statusfile.File{}, true)

	if got, ok := totals["is_indexing"]; !ok || got != false {
		t.Fatalf("is_indexing = %v (present=%v), want false", got, ok)
	}
	if got, ok := totals["is_enhancing"]; !ok || got != false {
		t.Fatalf("is_enhancing = %v (present=%v), want false", got, ok)
	}
}

// "Not yet computed" must be a DEFINED, distinguishable state. Every overlay
// consumer degrades silently when the overlay is absent (nil community
// pointers, empty cluster lists), so an empty communities count on its own
// cannot tell a caller whether to retry.
func TestOverlayStateFor(t *testing.T) {
	for _, tc := range []struct {
		name      string
		loaded    bool
		enhancing bool
		want      string
	}{
		{"loaded overlay is current", true, false, overlayStateCurrent},
		{"loaded overlay wins over a running pass", true, true, overlayStateCurrent},
		{"absent overlay with a running pass is computing", false, true, overlayStateComputing},
		{"absent overlay with no pass is pending", false, false, overlayStatePending},
	} {
		if got := overlayStateFor(tc.loaded, tc.enhancing); got != tc.want {
			t.Errorf("%s: overlayStateFor(%v,%v) = %q, want %q", tc.name, tc.loaded, tc.enhancing, got, tc.want)
		}
	}
	if overlayStateComputing == overlayStatePending {
		t.Fatal("computing (retry) and pending (do not spin) must be distinguishable states")
	}
}
