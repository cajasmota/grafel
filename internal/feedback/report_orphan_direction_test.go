package feedback

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// rel6346 builds a relationship for the direction-awareness fixtures.
func rel6346(id, from, to, kind string) graph.Relationship {
	return graph.Relationship{ID: id, FromID: from, ToID: to, Kind: kind}
}

// pad6346 returns 60 fully-wired filler entities so the fixture clears
// minEntitiesForReport (50) and the report is not suppressed.
func pad6346() ([]graph.Entity, []graph.Relationship) {
	var e []graph.Entity
	var r []graph.Relationship
	for i := 0; i < 60; i++ {
		id := "pad" + string(rune('A'+i%26)) + string(rune('a'+i/26))
		e = append(e, makeEntity(id, "pad", "SCOPE.Operation", "go", "pad.go", 1+i))
		if i > 0 {
			prev := "pad" + string(rune('A'+(i-1)%26)) + string(rune('a'+(i-1)/26))
			r = append(r, rel6346("padc"+id, prev, id, "CALLS"))
		}
	}
	return e, r
}

// TestOrphan_InboundSemanticEdgeIsNotOrphan is the regression test for #6313.
//
// http_endpoint_definition is a SINK BY CONSTRUCTION in NestJS/Django/Flask:
// internal/engine/http_endpoint_resolve.go bridgeEndpointToHandler emits
// `FromID: handler, ToID: definition`, so a correctly-wired definition sources
// nothing and is pointed AT. The old metric counted only outgoing semantic
// edges, so every one of these looked orphaned — measured 33.4% on 10 corpus
// repos while 0.00% were actually isolated, and ~99.6% at the reporter's scale
// once the process-entry cap starved the one outgoing kind (ENTRY_POINT_OF).
//
// An entity with an inbound semantic edge is attached to the graph and must
// not be counted as an orphan.
func TestOrphan_InboundSemanticEdgeIsNotOrphan(t *testing.T) {
	var ents []graph.Entity
	var rels []graph.Relationship

	// One file container.
	ents = append(ents, makeEntity("file1", "routes.py", "SCOPE.Component", "python", "routes.py", 1))

	// 12 endpoint definitions, each CONTAINed by the file (structural) and
	// each pointed at by its handler via IMPLEMENTS (inbound semantic).
	// None of them sources any edge — exactly the real graph shape.
	for i := 0; i < 12; i++ {
		id := string(rune('a'+i)) + "def"
		hid := string(rune('a'+i)) + "handler"
		ents = append(ents,
			makeEntity(id, "GET /x", "http_endpoint_definition", "python", "routes.py", 10+i),
			makeEntity(hid, "handler", "SCOPE.Operation", "python", "routes.py", 50+i),
		)
		rels = append(rels,
			rel6346("c"+id, "file1", id, "CONTAINS"),
			rel6346("i"+id, hid, id, "IMPLEMENTS"),
		)
	}

	pe, pr := pad6346()
	ents = append(ents, pe...)
	rels = append(rels, pr...)

	r, err := Generate(context.Background(), []*graph.Document{makeDoc(ents, rels)}, Opts{GroupName: "g", Version: "t"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	ks, ok := r.OrphanByKind["http_endpoint_definition"]
	if !ok {
		t.Fatalf("http_endpoint_definition missing from OrphanByKind: %+v", r.OrphanByKind)
	}
	if ks.OrphanCount != 0 {
		t.Errorf("sink with inbound IMPLEMENTS counted as orphan: %d/%d (%.1f%%) — the orphan metric is still outgoing-only (#6313)",
			ks.OrphanCount, ks.Total, ks.OrphanPct)
	}
}

// TestOrphan_UnwiredSinkStaysVisible is the #6374 half of the same fixture:
// definitions whose handler was never bound have NO semantic edge in either
// direction and must still be counted as orphans. The direction fix must not
// hide the genuine defect along with the artifact.
func TestOrphan_UnwiredSinkStaysVisible(t *testing.T) {
	var ents []graph.Entity
	var rels []graph.Relationship
	ents = append(ents, makeEntity("file1", "routes.py", "SCOPE.Component", "python", "routes.py", 1))

	const total, bound = 12, 9
	for i := 0; i < total; i++ {
		id := string(rune('a'+i)) + "def"
		hid := string(rune('a'+i)) + "handler"
		ents = append(ents,
			makeEntity(id, "GET /x", "http_endpoint_definition", "python", "routes.py", 10+i),
			makeEntity(hid, "handler", "SCOPE.Operation", "python", "routes.py", 50+i),
		)
		rels = append(rels, rel6346("c"+id, "file1", id, "CONTAINS"))
		if i < bound {
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
	if ks.OrphanCount != total-bound {
		t.Errorf("unbound-handler endpoints: OrphanCount = %d, want %d — the #6374 signal was hidden by the direction fix",
			ks.OrphanCount, total-bound)
	}
	// And they must be in the DEFECT bucket, not the terminal bucket: the
	// kind demonstrably carries semantic edges here (9 of 12 do).
	if tks, ok := r.OrphanTerminalByKind["http_endpoint_definition"]; ok && tks.OrphanCount > 0 {
		t.Errorf("unbound endpoints routed to the expected/terminal bucket (%d) — a kind with observed semantic participation is not terminal",
			tks.OrphanCount)
	}
}

// TestOrphan_TerminalDerivedFromParticipation asserts terminality is DERIVED
// from the graph — "does any entity of this kind carry a semantic edge in
// either direction?" — rather than read off a hand-maintained name list
// (#6346, and the #6361 failure class). A kind with zero observed semantic
// participation goes to the expected/terminal bucket; a kind with any
// participation keeps its unwired members in the defect bucket.
func TestOrphan_TerminalDerivedFromParticipation(t *testing.T) {
	var ents []graph.Entity
	var rels []graph.Relationship
	ents = append(ents, makeEntity("file1", "a.go", "SCOPE.Component", "go", "a.go", 1))

	// Kind A: 12 entities, containment only, no semantic edge anywhere.
	// Nothing in the group shows this kind ever carries semantics → terminal.
	for i := 0; i < 12; i++ {
		id := string(rune('a'+i)) + "leaf"
		ents = append(ents, makeEntity(id, "leaf", "MadeUpTerminalKind", "go", "a.go", 100+i))
		rels = append(rels, rel6346("c"+id, "file1", id, "CONTAINS"))
	}
	// Kind B: 12 entities, containment only, EXCEPT one that sources a
	// REFERENCES edge. That single instance proves the kind can be wired, so
	// the other 11 are defects, not terminals.
	for i := 0; i < 12; i++ {
		id := string(rune('a'+i)) + "wire"
		ents = append(ents, makeEntity(id, "w", "MadeUpWiredKind", "go", "a.go", 200+i))
		rels = append(rels, rel6346("c"+id, "file1", id, "CONTAINS"))
	}
	rels = append(rels, rel6346("ref", "awire", "file1", "REFERENCES"))

	pe, pr := pad6346()
	ents = append(ents, pe...)
	rels = append(rels, pr...)

	r, err := Generate(context.Background(), []*graph.Document{makeDoc(ents, rels)}, Opts{GroupName: "g", Version: "t"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if ks := r.OrphanByKind["MadeUpTerminalKind"]; ks.OrphanCount != 0 {
		t.Errorf("zero-participation kind counted as defect orphan: %d/%d — terminality is not being derived",
			ks.OrphanCount, ks.Total)
	}
	if tks := r.OrphanTerminalByKind["MadeUpTerminalKind"]; tks.OrphanCount != 12 {
		t.Errorf("zero-participation kind not routed to the terminal bucket: got %d, want 12", tks.OrphanCount)
	}
	if ks := r.OrphanByKind["MadeUpWiredKind"]; ks.OrphanCount != 11 {
		t.Errorf("kind with observed participation: defect OrphanCount = %d, want 11", ks.OrphanCount)
	}
	if tks, ok := r.OrphanTerminalByKind["MadeUpWiredKind"]; ok && tks.OrphanCount != 0 {
		t.Errorf("kind with observed participation wrongly exempted as terminal: %d", tks.OrphanCount)
	}
}

// TestGenerate_TotalRegressionOnOneKindFailsTheGate closes the hole the
// participation derivation opened (#6346 review C2).
//
// Zero-participation kinds are routed to OrphanTerminalByKind and never reach
// OrphanByKind, so the per-kind orphan-rate gate — which iterates OrphanByKind
// — cannot see them. Without a second check, a kind that loses EVERY semantic
// edge reports 0% defect orphan and 100% confidence, while the previous
// `OrphanPct < 100.0` rule (which fired exactly and only at 100%) DID catch it.
// That is the one case the direction fix must not give away: it is the shape of
// a new language extractor emitting entities before its resolver lands.
func TestGenerate_TotalRegressionOnOneKindFailsTheGate(t *testing.T) {
	var ents []graph.Entity
	var rels []graph.Relationship
	ents = append(ents, makeEntity("file1", "routes.py", "SCOPE.Component", "python", "routes.py", 1))

	// 12 endpoint definitions that lost every IMPLEMENTS edge: contained by
	// their file, semantically wired to nothing.
	for i := 0; i < 12; i++ {
		id := string(rune('a'+i)) + "def"
		ents = append(ents, makeEntity(id, "GET /x", "http_endpoint_definition", "python", "routes.py", 10+i))
		rels = append(rels, rel6346("c"+id, "file1", id, "CONTAINS"))
	}
	// The rest of the group is healthy.
	pe, pr := pad6346()
	ents = append(ents, pe...)
	rels = append(rels, pr...)

	r, err := Generate(context.Background(), []*graph.Document{makeDoc(ents, rels)}, Opts{GroupName: "g", Version: "t"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	found, failed := false, false
	for _, res := range r.SanityResults {
		if res.Name != participationCheckName("http_endpoint_definition") {
			continue
		}
		found = true
		if !res.Passed {
			failed = true
			if res.Note == "" {
				t.Error("failing participation check must carry a note naming the problem")
			}
		}
	}
	if !found {
		var names []string
		for _, res := range r.SanityResults {
			names = append(names, res.Name)
		}
		t.Fatalf("no participation check emitted for a 100%%-unwired kind; checks were %v", names)
	}
	if !failed {
		t.Error("a kind that lost EVERY semantic edge PASSED every sanity check — the 100% end of the gate is blind (#6346 review C2)")
	}
	if r.Confidence == 100 {
		t.Errorf("confidence = 100%% for a group with a totally unwired kind")
	}
}

// TestGenerate_WhollyUnwiredGroupIsNotReportedHealthy is the extractor-without-
// resolver shape (VB.NET is on this milestone): every entity carries CONTAINS
// and nothing else. The report must not call that healthy.
func TestGenerate_WhollyUnwiredGroupIsNotReportedHealthy(t *testing.T) {
	var ents []graph.Entity
	var rels []graph.Relationship
	ents = append(ents, makeEntity("file1", "Mod.vb", "SCOPE.Component", "vbnet", "Mod.vb", 1))
	for i := 0; i < 60; i++ {
		id := "e" + string(rune('A'+i%26)) + string(rune('a'+i/26))
		ents = append(ents, makeEntity(id, "Sub", "SCOPE.Operation", "vbnet", "Mod.vb", 10+i))
		rels = append(rels, rel6346("c"+id, "file1", id, "CONTAINS"))
	}

	r, err := Generate(context.Background(), []*graph.Document{makeDoc(ents, rels)}, Opts{GroupName: "g", Version: "t"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if r.Confidence == 100 {
		t.Errorf("a group with no semantic edge anywhere reports confidence 100%% — the report would tell a maintainer their unlanded resolver is perfect")
	}
	failing := 0
	for _, res := range r.SanityResults {
		if !res.Passed {
			failing++
		}
	}
	if failing == 0 {
		t.Error("no sanity check failed for a wholly unwired group")
	}
}

// TestGenerate_MixedParticipationSchemaFlipsFieldLeaves pins the granularity
// limitation the review (C3) identified, so it is a documented, tested property
// rather than an accident of the other fixtures.
//
// Terminality is derived per KIND, which is strictly coarser than the two
// per-entity rules it replaced (field leaves were exempt individually by
// Subtype). One participating non-field member of SCOPE.Schema is therefore
// enough to make the whole kind non-terminal, which moves every unwired field
// leaf in the group into the DEFECT bucket. This is the intended trade — the
// old per-subtype exemption is exactly the name list #6346 asked us to remove —
// but it must be visible, not discovered later.
func TestGenerate_MixedParticipationSchemaFlipsFieldLeaves(t *testing.T) {
	build := func(wireOne bool) *Report {
		t.Helper()
		var ents []graph.Entity
		var rels []graph.Relationship
		parent := makeEntity("parent", "Widget", "SCOPE.Class", "go", "w.go", 1)
		ents = append(ents, parent)
		for i := 0; i < 12; i++ {
			id := "f" + string(rune('a'+i))
			ents = append(ents, withSubtype(makeEntity(id, "field", "SCOPE.Schema", "go", "w.go", 10+i), "field"))
			rels = append(rels, rel6346("c"+id, "parent", id, "CONTAINS"))
		}
		// A non-field SCOPE.Schema member. Only in the wireOne case does it
		// carry a semantic edge.
		ents = append(ents, makeEntity("schemaDoc", "WidgetSchema", "SCOPE.Schema", "go", "w.go", 40))
		rels = append(rels, rel6346("cdoc", "parent", "schemaDoc", "CONTAINS"))
		if wireOne {
			rels = append(rels, rel6346("refdoc", "schemaDoc", "parent", "REFERENCES"))
		}
		pe, pr := pad6346()
		ents = append(ents, pe...)
		rels = append(rels, pr...)
		r, err := Generate(context.Background(), []*graph.Document{makeDoc(ents, rels)}, Opts{GroupName: "g", Version: "t"})
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		return r
	}

	// No participation anywhere in the kind → every field leaf is terminal.
	unwired := build(false)
	if got := unwired.OrphanByKind["SCOPE.Schema"].OrphanCount; got != 0 {
		t.Errorf("zero-participation SCOPE.Schema: defect orphans = %d, want 0", got)
	}
	if got := unwired.OrphanTerminalByKind["SCOPE.Schema"].OrphanCount; got != 13 {
		t.Errorf("zero-participation SCOPE.Schema: terminal orphans = %d, want 13", got)
	}

	// One participating member → the kind is no longer terminal, so all 12
	// unwired field leaves become defect orphans. Documented limitation.
	mixed := build(true)
	if got := mixed.OrphanByKind["SCOPE.Schema"].OrphanCount; got != 12 {
		t.Errorf("mixed-participation SCOPE.Schema: defect orphans = %d, want 12 (per-kind granularity flips every field leaf)", got)
	}
	if got := mixed.OrphanTerminalByKind["SCOPE.Schema"].OrphanCount; got != 0 {
		t.Errorf("mixed-participation SCOPE.Schema: terminal orphans = %d, want 0", got)
	}
	// 12/13 = 92.3% is above orphanRateFailThreshold, so the granularity loss
	// is not silent: it surfaces as a gate failure a human can triage.
	if got := mixed.OrphanByKind["SCOPE.Schema"].OrphanPct; got <= orphanRateFailThreshold {
		t.Errorf("mixed-participation SCOPE.Schema orphan pct = %.1f%%, expected above the %.0f%% gate", got, orphanRateFailThreshold)
	}
}
