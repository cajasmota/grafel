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
		// 90.0 must FAIL. The check is named orphan-rate-BELOW-90pct and that
		// name ships in the report, so passing must mean strictly below. It is
		// also the only way the smallest published bucket can ever fire: a
		// 10-entity kind cannot exceed 90.0% (see
		// TestGenerate_TenEntityKindCanFireTheGate).
		{90.0, false},
		{90.1, false},
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

// TestGenerate_TenEntityKindCanFireTheGate closes a one-value blind spot at the
// bottom of the reporting range (#6346 round-3 review, item 4).
//
// kindOrphans only accrues for kinds with at least one participating entity, so
// the maximum defect rate a kind of size N can reach is 100*(N-1)/N. At N == 10
// — the smallest size Generate publishes at all — that ceiling is exactly 90.0,
// so a `<=` predicate made the gate unfirable for the entire smallest bucket: a
// kind with 10 entities and 9 of them unwired raised nothing whatsoever.
// Neither check covered it: check 2b needs zero participation, and this one let
// the ceiling through.
func TestGenerate_TenEntityKindCanFireTheGate(t *testing.T) {
	var ents []graph.Entity
	var rels []graph.Relationship
	ents = append(ents, makeEntity("file1", "a.go", "SCOPE.Component", "go", "a.go", 1))
	for i := 0; i < 10; i++ {
		id := "k" + string(rune('a'+i))
		ents = append(ents, makeEntity(id, "K", "MadeUpTenKind", "go", "a.go", 10+i))
		rels = append(rels, rel6346("c"+id, "file1", id, "CONTAINS"))
	}
	// Exactly one of the ten participates — the most a 10-entity kind can have
	// while still being 9/10 unwired.
	rels = append(rels, rel6346("wire", "ka", "file1", "REFERENCES"))
	pe, pr := pad6346()
	ents = append(ents, pe...)
	rels = append(rels, pr...)

	r, err := Generate(context.Background(), []*graph.Document{makeDoc(ents, rels)}, Opts{GroupName: "g", Version: "t"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	ks := r.OrphanByKind["MadeUpTenKind"]
	if ks.Total != 10 || ks.OrphanCount != 9 {
		t.Fatalf("fixture drifted: Total=%d OrphanCount=%d, want 10/9", ks.Total, ks.OrphanCount)
	}

	found, failed := false, false
	for _, res := range r.SanityResults {
		if res.Name != orphanCheckName("MadeUpTenKind") {
			continue
		}
		found = true
		if !res.Passed {
			failed = true
		}
	}
	if !found {
		t.Fatalf("%s not emitted", orphanCheckName("MadeUpTenKind"))
	}
	if !failed {
		t.Errorf("a 10-entity kind with 9 of 10 unwired (%.1f%%) raised nothing — the ceiling for N=10 is exactly the threshold, so the whole smallest bucket is unfirable", ks.OrphanPct)
	}
}

// TestRunSanityChecks_ParticipationCheckCannotFalsePass guards the invariant
// behind check 2b (#6346 round-3 review, item 3).
//
// report.go routes a kind into OrphanTerminalByKind only when NO entity of it
// participates, so every entity is unwired and OrphanCount == Total for every
// report Generate produces. A comparison there is therefore unreachable-false —
// and worse, the two fields are not counted the same way: Total counts entity
// OCCURRENCES across docs while OrphanCount counts UNIQUE ids. A duplicate
// entity ID across two docs (the #6368 shape, which landed two commits before
// this branch) makes OrphanCount < Total and would silently turn the failure
// into a pass. The check must fail on the substance, not on a comparison of two
// differently-counted numbers.
//
// UPDATE (#6378): the skew this test used to construct no longer exists.
// kindTotals is now derived from the unique-entity-ID index, so Total counts
// unique IDs exactly as OrphanCount does, and the two docs below yield
// OrphanCount == Total == 10 rather than 10 < 20. The precondition is inverted
// to pin that — but the substance assertion is unchanged and still load-bearing:
// a zero-participation kind emitted across multiple docs must FAIL check 2b.
// Check 2b's unconditional form (#6375) remains the defense if a future change
// reintroduces any denominator that is not unique-by-ID.
func TestRunSanityChecks_ParticipationCheckCannotFalsePass(t *testing.T) {
	// Same 10 entity IDs emitted by two docs: 20 occurrences, 10 unique ids,
	// none carrying a semantic edge.
	build := func() *graph.Document {
		var ents []graph.Entity
		var rels []graph.Relationship
		ents = append(ents, makeEntity("file1", "a.css", "SCOPE.Component", "css", "a.css", 1))
		for i := 0; i < 10; i++ {
			id := "s" + string(rune('a'+i))
			ents = append(ents, makeEntity(id, "sel", "SCOPE.Stylesheet", "css", "a.css", 10+i))
			rels = append(rels, rel6346("c"+id, "file1", id, "CONTAINS"))
		}
		pe, pr := pad6346()
		ents = append(ents, pe...)
		rels = append(rels, pr...)
		return makeDoc(ents, rels)
	}
	r, err := Generate(context.Background(), []*graph.Document{build(), build()}, Opts{GroupName: "g", Version: "t"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	tks := r.OrphanTerminalByKind["SCOPE.Stylesheet"]
	if tks.Total != 10 || tks.OrphanCount != tks.Total {
		t.Fatalf("occurrence/unique-id skew is back: want OrphanCount==Total==10 unique ids across the two docs, got OrphanCount=%d Total=%d (#6378)", tks.OrphanCount, tks.Total)
	}
	found, failed := false, false
	for _, res := range r.SanityResults {
		if res.Name != participationCheckName("SCOPE.Stylesheet") {
			continue
		}
		found = true
		failed = !res.Passed
	}
	if !found {
		t.Fatalf("%s not emitted", participationCheckName("SCOPE.Stylesheet"))
	}
	if !failed {
		t.Errorf("a zero-participation kind (%d/%d) emitted across two docs PASSED check 2b — the check must fail on the substance, not on a comparison of two counts",
			tks.OrphanCount, tks.Total)
	}
}
