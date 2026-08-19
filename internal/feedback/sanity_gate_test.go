package feedback

import (
	"testing"
)

// TestRunSanityChecks_OrphanGateFiresBelowTotalFailure is the regression test
// for #6346 direction 1. The gate used to be `OrphanPct < 100.0`, so the
// 99.6%-orphan http_endpoint_definition reported in #6313 passed cleanly and
// the report told the user everything was fine. A kind where more than 9 in 10
// entities carry no semantic edge is functionally unwired and MUST fail.
func TestRunSanityChecks_OrphanGateFiresBelowTotalFailure(t *testing.T) {
	r := &Report{
		TotalEntities:      1000,
		EntitiesByLanguage: map[string]int{"typescript": 1000},
		OrphanByKind: map[string]KindStats{
			// The exact shape reported in #6313.
			"http_endpoint_definition": {Total: 560, OrphanCount: 558, OrphanPct: 99.64},
		},
		ResolutionTotal: 0,
		FrameworkHits:   map[string]int{},
	}

	results, _ := runSanityChecks(r)
	found := false
	for _, res := range results {
		if res.Name != orphanCheckName("http_endpoint_definition") {
			continue
		}
		found = true
		if res.Passed {
			t.Errorf("orphan gate PASSED at 99.64%% orphan — #6346 direction 1 regression; note=%q", res.Note)
		}
		if res.Note == "" {
			t.Error("failing orphan check must carry a note naming the problem")
		}
	}
	if !found {
		t.Fatalf("no orphan check emitted for http_endpoint_definition; got %+v", results)
	}
}

// TestRunSanityChecks_OrphanGateThresholdBoundary pins the chosen threshold so
// a future edit cannot quietly walk it back toward the degenerate 100%.
func TestRunSanityChecks_OrphanGateThresholdBoundary(t *testing.T) {
	if orphanRateFailThreshold >= 100.0 {
		t.Fatalf("orphanRateFailThreshold = %.1f: a gate at 100%% cannot fire (#6346)", orphanRateFailThreshold)
	}
	if orphanRateFailThreshold != 90.0 {
		t.Fatalf("orphanRateFailThreshold = %.1f, want 90.0 (see the rationale in sanity.go)", orphanRateFailThreshold)
	}

	cases := []struct {
		pct        float64
		wantPassed bool
	}{
		{89.9, true},
		{90.0, true},  // at the bound, not over it
		{90.1, false}, // over the bound
		{99.64, false},
		{100.0, false},
	}
	for _, tc := range cases {
		r := &Report{
			TotalEntities:      1000,
			EntitiesByLanguage: map[string]int{"go": 1000},
			OrphanByKind: map[string]KindStats{
				"SCOPE.Route": {Total: 100, OrphanCount: int(tc.pct), OrphanPct: tc.pct},
			},
			FrameworkHits: map[string]int{},
		}
		results, _ := runSanityChecks(r)
		for _, res := range results {
			if res.Name != orphanCheckName("SCOPE.Route") {
				continue
			}
			if res.Passed != tc.wantPassed {
				t.Errorf("orphan %.2f%%: Passed=%v, want %v (note=%q)", tc.pct, res.Passed, tc.wantPassed, res.Note)
			}
		}
	}
}

// TestRunSanityChecks_OrphanGateKeeps6374Visible asserts the #6374 signal —
// 23–40% of http_endpoint_definition entities with no IMPLEMENTS edge — is
// still REPORTED (a non-zero orphan count in the table) while not tripping the
// gate. The metric fix must not hide the genuine defect along with the
// direction artifact.
func TestRunSanityChecks_OrphanGateKeeps6374Visible(t *testing.T) {
	// Measured aggregate over 12 corpus repos: 85/369 (23.04%).
	ks := KindStats{Total: 369, OrphanCount: 85, OrphanPct: 23.04}
	r := &Report{
		TotalEntities:      5000,
		EntitiesByLanguage: map[string]int{"python": 5000},
		OrphanByKind:       map[string]KindStats{"http_endpoint_definition": ks},
		FrameworkHits:      map[string]int{},
	}
	results, _ := runSanityChecks(r)
	for _, res := range results {
		if res.Name == orphanCheckName("http_endpoint_definition") && !res.Passed {
			t.Errorf("23%% orphan should not FAIL the gate (that is a per-instance gap, reported in the table): %s", res.Note)
		}
	}
	if r.OrphanByKind["http_endpoint_definition"].OrphanCount == 0 {
		t.Error("#6374 signal erased from the orphan table")
	}
}
