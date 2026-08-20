package feedback

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// buildRouteFixture6378 returns entities+relationships for a fixture where the
// kind "Route" is functionally unwired: 12 Route entities, 11 of which have no
// semantic edge in either direction. The 12th participates (a CALLS edge), so
// the kind stays in OrphanByKind rather than falling through to
// OrphanTerminalByKind, which check 2b — not check 2 — gates.
//
// Raw rate: 11/12 == 91.7%, at or above orphanRateFailThreshold (90.0), so the
// orphan-rate gate must fire.
func buildRouteFixture6378() ([]graph.Entity, []graph.Relationship) {
	ents := []graph.Entity{
		makeEntity("rfile", "routes.go", "SCOPE.Component", "go", "routes.go", 1),
	}
	var rels []graph.Relationship

	for i := 0; i < 12; i++ {
		id := "route" + string(rune('a'+i))
		ents = append(ents, makeEntity(id, "GET /x", "Route", "go", "routes.go", 10+i))
		// Structural only — CONTAINS never counts as participation.
		rels = append(rels, rel6346("c"+id, "rfile", id, "CONTAINS"))
	}
	// Exactly one Route participates semantically.
	ents = append(ents, makeEntity("rhandler", "handler", "SCOPE.Operation", "go", "routes.go", 90))
	rels = append(rels, rel6346("rwire", "routea", "rhandler", "CALLS"))

	pe, pr := pad6346()
	ents = append(ents, pe...)
	rels = append(rels, pr...)
	return ents, rels
}

func routeStats6378(t *testing.T, docs []*graph.Document) KindStats {
	t.Helper()
	r, err := Generate(context.Background(), docs, Opts{GroupName: "g", Version: "t"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	ks, ok := r.OrphanByKind["Route"]
	if !ok {
		t.Fatalf("Route missing from OrphanByKind: %+v (terminal: %+v)", r.OrphanByKind, r.OrphanTerminalByKind)
	}
	return ks
}

func orphanGatePassed6378(t *testing.T, docs []*graph.Document) bool {
	t.Helper()
	r, err := Generate(context.Background(), docs, Opts{GroupName: "g", Version: "t"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	results, _ := runSanityChecks(r)
	name := orphanCheckName("Route")
	for _, res := range results {
		if res.Name == name {
			return res.Passed
		}
	}
	t.Fatalf("orphan-rate check %q not present in results", name)
	return false
}

// TestOrphanRate_DuplicatedDocumentDoesNotDiluteRate is the regression test for
// #6378.
//
// kindTotals was incremented once per entity OCCURRENCE per document, while the
// orphan numerator is keyed by unique entity ID (entityEdges / kindOrphans).
// Emitting the same document twice therefore doubled the denominator without
// touching the numerator: Route went 11/12 (91.7%, gate FIRES) → 11/24 (45.8%,
// gate SILENT). A genuinely unwired kind was silenced by duplication.
//
// The gate asks "what share of the entities of this kind have no semantic edge
// in either direction" — a question about entities, not about emissions — so
// both sides must be counted per unique entity ID.
func TestOrphanRate_DuplicatedDocumentDoesNotDiluteRate(t *testing.T) {
	ents, rels := buildRouteFixture6378()

	single := routeStats6378(t, []*graph.Document{makeDoc(ents, rels)})
	if single.Total != 12 || single.OrphanCount != 11 {
		t.Fatalf("fixture drifted: want 11/12, got %d/%d (%.1f%%)", single.OrphanCount, single.Total, single.OrphanPct)
	}
	if single.OrphanPct < orphanRateFailThreshold {
		t.Fatalf("fixture drifted: single-doc rate %.1f%% must be at/above the %.0f%% threshold", single.OrphanPct, orphanRateFailThreshold)
	}
	if orphanGatePassed6378(t, []*graph.Document{makeDoc(ents, rels)}) {
		t.Fatalf("fixture drifted: single-doc orphan-rate gate must FIRE for an unwired kind")
	}

	// Same document emitted twice — identical entity IDs, no new entities.
	dup := []*graph.Document{makeDoc(ents, rels), makeDoc(ents, rels)}

	got := routeStats6378(t, dup)
	if got.Total != single.Total {
		t.Errorf("duplicated document inflated the denominator: Total %d → %d; kindTotals counts occurrences while the orphan numerator counts unique IDs (#6378)",
			single.Total, got.Total)
	}
	if got.OrphanPct != single.OrphanPct {
		t.Errorf("duplicated document diluted the orphan rate: %.1f%% → %.1f%% (%d/%d → %d/%d) (#6378)",
			single.OrphanPct, got.OrphanPct, single.OrphanCount, single.Total, got.OrphanCount, got.Total)
	}
	if orphanGatePassed6378(t, dup) {
		t.Errorf("duplicating the document SILENCED the orphan-rate gate for a functionally unwired kind (%d/%d = %.1f%%) (#6378)",
			got.OrphanCount, got.Total, got.OrphanPct)
	}
}

// TestOrphanRate_HealthyKindStillPasses is the other direction: the fix must
// not make the gate stricter everywhere. A well-wired kind — 12 Routes, every
// one of them carrying an outgoing CALLS edge — must pass both as a single
// document and when that document is duplicated.
func TestOrphanRate_HealthyKindStillPasses(t *testing.T) {
	ents := []graph.Entity{
		makeEntity("hfile", "routes.go", "SCOPE.Component", "go", "routes.go", 1),
	}
	var rels []graph.Relationship
	for i := 0; i < 12; i++ {
		id := "hroute" + string(rune('a'+i))
		hid := "hhandler" + string(rune('a'+i))
		ents = append(ents,
			makeEntity(id, "GET /x", "Route", "go", "routes.go", 10+i),
			makeEntity(hid, "handler", "SCOPE.Operation", "go", "routes.go", 50+i),
		)
		rels = append(rels,
			rel6346("hc"+id, "hfile", id, "CONTAINS"),
			rel6346("hw"+id, id, hid, "CALLS"),
		)
	}
	pe, pr := pad6346()
	ents = append(ents, pe...)
	rels = append(rels, pr...)

	for _, tc := range []struct {
		name string
		docs []*graph.Document
	}{
		{"single", []*graph.Document{makeDoc(ents, rels)}},
		{"duplicated", []*graph.Document{makeDoc(ents, rels), makeDoc(ents, rels)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ks := routeStats6378(t, tc.docs)
			if ks.Total != 12 {
				t.Errorf("healthy Route total: want 12 unique entities, got %d", ks.Total)
			}
			if ks.OrphanCount != 0 {
				t.Errorf("healthy Route counted %d orphans (%.1f%%)", ks.OrphanCount, ks.OrphanPct)
			}
			if !orphanGatePassed6378(t, tc.docs) {
				t.Errorf("orphan-rate gate FIRED on a fully-wired kind (%d/%d = %.1f%%) — the fix made the gate stricter everywhere",
					ks.OrphanCount, ks.Total, ks.OrphanPct)
			}
		})
	}
}
