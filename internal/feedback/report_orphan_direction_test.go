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
