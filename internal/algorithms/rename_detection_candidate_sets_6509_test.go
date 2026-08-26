package algorithms_test

import (
	"fmt"
	"testing"

	"github.com/cajasmota/grafel/internal/algorithms"
	"github.com/cajasmota/grafel/internal/graph"
)

// ─── #6509 — the candidate sets must exclude entities that exist in BOTH graphs ─
//
// DetectRenamesBounded builds two candidate sets:
//
//	deleted — prior entities whose ID is absent from the new graph (newIDs guard)
//	added   — new entities whose ID is absent from the prior graph (prevIDs guard)
//
// Dropping either guard used to survive every test in the repo. Both existing
// fixture families build prev ≈ deleted with NO survivors at all (noiseDocs
// makes n deleted and n added; renameDocs makes n renames), so widening a
// candidate set from "the entities that actually churned" to "every entity in
// the graph" is a no-op on all of them.
//
// The axis these tests vary is therefore the RATIO OF SURVIVORS TO CHURN: a
// prior graph of S+K entities of which only K disappear, and a new graph of
// S+A entities of which only A are new. The guarded sets are then K and A,
// while the unguarded ones are S+K and S+A — observable through the exported
// RenameStats.Candidates, which is len(deleted) * len(added).

// survivorSkewDocs builds a prev/new graph pair with `surv` entities that are
// present in BOTH graphs (identical IDs), `del` entities that exist only in
// prev, and `add` entities that exist only in the new graph.
//
// The three name families share no structure, so nothing matches anything and
// no RENAMED_FROM edge is emitted: these fixtures isolate the SIZE of the
// candidate sets from the matching that consumes them.
func survivorSkewDocs(surv, del, add int, survKind, churnKind string) (prev, next *graph.Document) {
	var prevEnts, nextEnts []graph.Entity

	for i := 0; i < surv; i++ {
		name := fmt.Sprintf("keepStable%04d", i)
		file := fmt.Sprintf("keep/s%04d.go", i)
		e := graph.Entity{ID: entityID(survKind, name, file), Name: name, Kind: survKind, SourceFile: file}
		prevEnts = append(prevEnts, e)
		nextEnts = append(nextEnts, e)
	}
	for i := 0; i < del; i++ {
		name := fmt.Sprintf("vanishOld%04d", i)
		file := fmt.Sprintf("gone/d%04d.go", i)
		prevEnts = append(prevEnts, graph.Entity{ID: entityID(churnKind, name, file), Name: name, Kind: churnKind, SourceFile: file})
	}
	for i := 0; i < add; i++ {
		name := fmt.Sprintf("arriveNew%04d", i)
		file := fmt.Sprintf("fresh/a%04d.go", i)
		nextEnts = append(nextEnts, graph.Entity{ID: entityID(churnKind, name, file), Name: name, Kind: churnKind, SourceFile: file})
	}

	return makeDoc(prevEnts, nil), makeDoc(nextEnts, nil)
}

// TestDetectRenamesBounded_CandidateSetsExcludeSurvivors_6509 pins the size of
// both candidate sets through RenameStats.Candidates == len(deleted)*len(added).
//
// On every row except the negative control, the correct product differs from
// BOTH mutant products — (surv+del)*add with the newIDs guard dropped, and
// del*(surv+add) with the prevIDs guard dropped — so no such row can pass by
// accident under either mutant.
//
// The two mutant products are distinct from each other on only three rows
// (many_survivors_del_lt_add, many_survivors_del_gt_add, one_survivor). On
// single_churn_pair_in_large_graph (1 vs 26 vs 26) and survivors_of_other_kind
// (4 vs 64 vs 64) they coincide: those rows prove a guard is missing but not
// WHICH one. Attributing the failure to a specific arm is the job of
// TestDetectRenamesBounded_SurvivorIsNeverRenameCandidate_6509 below, whose two
// traps fire independently.
func TestDetectRenamesBounded_CandidateSetsExcludeSurvivors_6509(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name                string
		surv, del, add      int
		survKind, churnKind string
		wantCandidates      int
	}{
		// Negative control: the shape every EXISTING fixture has. With no
		// survivors both guards are no-ops, so this row is expected to pass
		// even with a guard removed. It is here to document that the existing
		// coverage cannot discriminate, not to catch anything.
		{name: "no_survivors_control", surv: 0, del: 2, add: 2, survKind: "Function", churnKind: "Function", wantCandidates: 4},

		// Survivor-heavy, del < add.
		{name: "many_survivors_del_lt_add", surv: 40, del: 2, add: 3, survKind: "Function", churnKind: "Function", wantCandidates: 6},
		// Survivor-heavy, del > add — the product must not be symmetric-blind.
		{name: "many_survivors_del_gt_add", surv: 10, del: 5, add: 2, survKind: "Function", churnKind: "Function", wantCandidates: 10},
		// A single survivor is already enough to separate all three products
		// (15 correct, 20 with newIDs dropped, 18 with prevIDs dropped).
		{name: "one_survivor", surv: 1, del: 3, add: 5, survKind: "Function", churnKind: "Function", wantCandidates: 15},
		// Minimal churn against a large stable graph — the bound the function
		// is named for: work stays proportional to K, not to N. The one
		// disappeared and one new entity are UNRELATED (survivorSkewDocs builds
		// name families that match nothing), so this row emits no RENAMED_FROM
		// edge; it is a churn pair, not a rename.
		{name: "single_churn_pair_in_large_graph", surv: 25, del: 1, add: 1, survKind: "Function", churnKind: "Function", wantCandidates: 1},
		// Survivors of a DIFFERENT kind from the churn: membership is decided
		// by entity ID, not by kind bucketing, so kind must not rescue a
		// missing guard.
		{name: "survivors_of_other_kind", surv: 30, del: 2, add: 2, survKind: "Class", churnKind: "Function", wantCandidates: 4},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			prevDoc, newDoc := survivorSkewDocs(tc.surv, tc.del, tc.add, tc.survKind, tc.churnKind)
			stats := algorithms.DetectRenamesBounded(prevDoc, newDoc, algorithms.DefaultRenameWorkBudget)

			if stats.Truncated {
				t.Fatalf("fixture truncated; Candidates is not a readable measure of the candidate sets: %+v", stats)
			}

			// The load-bearing assertion. Candidates == len(deleted)*len(added).
			if stats.Candidates != tc.wantCandidates {
				t.Errorf("Candidates = %d, want %d (deleted must be the %d disappeared entities, not all %d prior entities; added must be the %d new entities, not all %d entities of the new graph)",
					stats.Candidates, tc.wantCandidates,
					tc.del, tc.surv+tc.del, tc.add, tc.surv+tc.add)
			}

			// Phase 2 never examines more pairs than there are candidates, so
			// an inflated candidate set inflates the work actually done too.
			if stats.PairsExamined > tc.wantCandidates {
				t.Errorf("PairsExamined = %d, must not exceed the %d genuine candidate pairs", stats.PairsExamined, tc.wantCandidates)
			}
		})
	}
}

// TestDetectRenamesBounded_SurvivorIsNeverRenameCandidate_6509 is the content
// counterpart: an entity present in BOTH graphs must never appear at either end
// of a RENAMED_FROM edge, however tempting a fuzzy match it would make.
//
// The fixture plants one trap per guard:
//
//   - "renderInvoiceTable" survives, and "renderInvoiceTables" is genuinely new
//     in the same file. With the newIDs guard dropped, the survivor enters
//     `deleted` and absorbs the new entity as a rename TARGET.
//   - "listOpenTickets" survives, and "listOpenTicket" is genuinely deleted from
//     the same file. With the prevIDs guard dropped, the survivor enters `added`
//     and becomes a rename SOURCE.
//
// Each trap has exactly one plausible partner, so which edge appears does not
// depend on map or bucket ordering.
func TestDetectRenamesBounded_SurvivorIsNeverRenameCandidate_6509(t *testing.T) {
	t.Parallel()

	const kind = "Function"

	survRender := graph.Entity{
		ID: entityID(kind, "renderInvoiceTable", "pkg/render.go"), Name: "renderInvoiceTable",
		Kind: kind, SourceFile: "pkg/render.go",
	}
	survTickets := graph.Entity{
		ID: entityID(kind, "listOpenTickets", "pkg/tickets.go"), Name: "listOpenTickets",
		Kind: kind, SourceFile: "pkg/tickets.go",
	}

	// Genuinely deleted.
	delTax := graph.Entity{
		ID: entityID(kind, "computeTaxRate", "pkg/tax.go"), Name: "computeTaxRate",
		Kind: kind, SourceFile: "pkg/tax.go",
	}
	delTicket := graph.Entity{
		ID: entityID(kind, "listOpenTicket", "pkg/tickets.go"), Name: "listOpenTicket",
		Kind: kind, SourceFile: "pkg/tickets.go",
	}
	// Genuinely added.
	addTax := graph.Entity{
		ID: entityID(kind, "computeTaxRates", "pkg/tax.go"), Name: "computeTaxRates",
		Kind: kind, SourceFile: "pkg/tax.go",
	}
	addRender := graph.Entity{
		ID: entityID(kind, "renderInvoiceTables", "pkg/render.go"), Name: "renderInvoiceTables",
		Kind: kind, SourceFile: "pkg/render.go",
	}

	prevDoc := makeDoc([]graph.Entity{survRender, survTickets, delTax, delTicket}, nil)
	newDoc := makeDoc([]graph.Entity{survRender, survTickets, addTax, addRender}, nil)

	stats := algorithms.DetectRenamesBounded(prevDoc, newDoc, algorithms.DefaultRenameWorkBudget)

	// deleted = {computeTaxRate, listOpenTicket}, added = {computeTaxRates,
	// renderInvoiceTables} → 2 × 2. Widening either set gives 8.
	if stats.Candidates != 4 {
		t.Errorf("Candidates = %d, want 4 (2 disappeared × 2 new; the 2 survivors belong to neither set)", stats.Candidates)
	}

	survivors := map[string]string{
		survRender.ID:  survRender.Name,
		survTickets.ID: survTickets.Name,
	}
	var renames int
	for _, r := range newDoc.Relationships {
		if r.Kind != algorithms.RelKindRenamedFrom {
			continue
		}
		renames++
		if name, ok := survivors[r.FromID]; ok {
			t.Errorf("RENAMED_FROM originates from %q, which exists in BOTH graphs and can never be a newly-added entity (edge %s → %s)", name, r.FromID, r.ToID)
		}
		if name, ok := survivors[r.ToID]; ok {
			t.Errorf("RENAMED_FROM points at %q, which exists in BOTH graphs and can never have been renamed away (edge %s → %s)", name, r.FromID, r.ToID)
		}
	}

	// The one genuine rename must still be found — this test must not pass by
	// the pass doing nothing at all.
	if renames != 1 {
		t.Errorf("emitted %d RENAMED_FROM edges, want exactly 1", renames)
	}
	if got := findRenamedFrom(newDoc, addTax.ID); got == nil {
		t.Error("the genuine rename computeTaxRate → computeTaxRates was not detected")
	} else if got.ToID != delTax.ID {
		t.Errorf("computeTaxRates renamed from %s, want %s (computeTaxRate)", got.ToID, delTax.ID)
	}
}
