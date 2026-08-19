package feedback

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
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
		found := false
		for _, res := range results {
			if res.Name != orphanCheckName("SCOPE.Route") {
				continue
			}
			found = true
			if res.Passed != tc.wantPassed {
				t.Errorf("orphan %.2f%%: Passed=%v, want %v (note=%q)", tc.pct, res.Passed, tc.wantPassed, res.Note)
			}
		}
		if !found {
			t.Fatalf("orphan %.2f%%: %s not emitted — the case would be vacuous", tc.pct, orphanCheckName("SCOPE.Route"))
		}
	}
}

// TestRunSanityChecks_OrphanGateKeeps6374Visible asserts the #6374 signal —
// http_endpoint_definition entities whose handler was never bound — is still
// REPORTED (a non-zero orphan count in the table) while not tripping the gate.
// The metric fix must not hide the genuine defect along with the direction
// artifact.
//
// Driven through Generate rather than a hand-built Report: the counts under
// test must be PRODUCED by the classification code, not asserted against a map
// literal the test itself wrote.
func TestRunSanityChecks_OrphanGateKeeps6374Visible(t *testing.T) {
	// The django shape measured in #6374, scaled down: roughly a quarter of
	// definitions have no IMPLEMENTS edge and no process participation.
	const total, unbound = 24, 6
	var ents []graph.Entity
	var rels []graph.Relationship
	ents = append(ents, makeEntity("file1", "urls.py", "SCOPE.Component", "python", "urls.py", 1))
	for i := 0; i < total; i++ {
		id := "d" + string(rune('a'+i))
		hid := "h" + string(rune('a'+i))
		ents = append(ents,
			makeEntity(id, "GET /x", "http_endpoint_definition", "python", "urls.py", 10+i),
			makeEntity(hid, "view", "SCOPE.Operation", "python", "views.py", 10+i))
		rels = append(rels, rel6346("c"+id, "file1", id, "CONTAINS"))
		if i >= unbound {
			rels = append(rels, rel6346("i"+id, hid, id, "IMPLEMENTS"))
		}
	}
	pe, pr := pad6346()
	ents = append(ents, pe...)
	rels = append(rels, pr...)

	r, err := Generate(context.Background(), []*graph.Document{makeDoc(ents, rels)}, Opts{GroupName: "g", Version: "t"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	ks := r.OrphanByKind["http_endpoint_definition"]
	if ks.OrphanCount != unbound {
		t.Errorf("#6374 signal: OrphanCount = %d, want %d (unbound-handler endpoints must stay counted)", ks.OrphanCount, unbound)
	}
	found := false
	for _, res := range r.SanityResults {
		if res.Name != orphanCheckName("http_endpoint_definition") {
			continue
		}
		found = true
		if !res.Passed {
			t.Errorf("%.1f%% orphan should not FAIL the gate (a per-instance gap belongs in the table): %s", ks.OrphanPct, res.Note)
		}
	}
	if !found {
		t.Fatalf("%s not emitted — the guard would be vacuous", orphanCheckName("http_endpoint_definition"))
	}
	// And it must NOT be swallowed by the terminal bucket: the kind clearly
	// participates (18 of 24 are bound).
	if tks, ok := r.OrphanTerminalByKind["http_endpoint_definition"]; ok && tks.OrphanCount != 0 {
		t.Errorf("#6374 signal routed to the terminal bucket (%d)", tks.OrphanCount)
	}
}
