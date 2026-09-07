package main

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// Issue #6971 — stampEntityIDs builds preStampToFinal keyed by the record's
// pre-stamp types.EntityRecord.ComputeID() and valued by the final
// graph.EntityID(). ComputeID concatenates OrgID+ProjectID+SourceFile+Kind+Name
// with NO separators; graph.EntityID NUL-separates every field. So two records
// whose Kind/Name split the same characters are ONE key and TWO entities.
//
// Before the fix the map had no ambiguity check at all: both records wrote the
// same slot and the survivor was whichever index came last, so a grafel.twin_of
// naming that preimage was repointed at an ARBITRARY one of two distinct
// entities — the #6369 wrong-node hazard, and strictly worse than a dangle
// because classfold's twinAnchors and the symbol index's facet-alias rule read
// a confidently-wrong anchor as valid.
//
// internal/extractors/incremental.go's remapTwinOfAnchors (#6965) already made
// this decision for the INCREMENTAL path; #6971 is the same decision mirrored
// onto the FULL-INDEX path, which is the one that runs by default. An ambiguous
// key is SKIPPED, not resolved — the twin_of is left dangling deliberately.
//
// ARRIVAL ORDER IS THE HAZARD, so both orders are driven. An ordinary,
// non-colliding facet/anchor pair rides along in the same batch as a positive
// control: skipping EVERY key would "fix" the collision by disabling the remap
// entirely, and that no-op must fail here.
func TestStampEntityIDs_AmbiguousPreStampKeyIsSkippedNotResolved(t *testing.T) {
	const (
		repo          = "test_repo"
		collidingFile = "x.go"
		ordinaryFile  = "y.go"
	)

	// Both names NON-EMPTY: stampEntityIDs skips Name=="" via a DIFFERENT
	// guard, so an empty-name pair would exercise that guard instead of the
	// collision this test is about.
	recA := types.EntityRecord{Kind: "A", Name: "BC", SourceFile: collidingFile}
	recB := types.EntityRecord{Kind: "AB", Name: "C", SourceFile: collidingFile}

	sharedPreStamp := recA.ComputeID()
	finalA := graph.EntityID(repo, recA.Kind, recA.Name, recA.SourceFile)
	finalB := graph.EntityID(repo, recB.Kind, recB.Name, recB.SourceFile)

	// PREMISE PINS. If ComputeID ever gains field separators (#6968, the root
	// fix) this test must fail LOUDLY rather than pass vacuously — at that
	// point it is no longer testing the ambiguity guard at all.
	if got := recB.ComputeID(); got != sharedPreStamp {
		t.Fatalf("premise gone: ComputeID no longer collides for {Kind:%q,Name:%q} vs {Kind:%q,Name:%q} (%q vs %q) — "+
			"if ComputeID gained separators (#6968) this test is now vacuous and must be rewritten, not deleted",
			recA.Kind, recA.Name, recB.Kind, recB.Name, sharedPreStamp, got)
	}
	if finalA == finalB {
		t.Fatalf("premise gone: graph.EntityID now ALSO collides for the pair (%q) — the two ids schemes no longer disagree", finalA)
	}
	if sharedPreStamp == finalA || sharedPreStamp == finalB {
		t.Fatalf("premise gone: the shared pre-stamp id %q equals a final id (finalA=%q finalB=%q) — "+
			"stampEntityIDs only records keys whose pre-stamp and final ids DIFFER, so nothing would be remapped either way",
			sharedPreStamp, finalA, finalB)
	}

	// The ordinary pair: an anchor whose pre-stamp id is unambiguous, and a
	// facet naming it. This must STILL be remapped after the fix.
	ordinaryAnchor := types.EntityRecord{Kind: "SCOPE.Component", Name: "Ordinary", SourceFile: ordinaryFile}
	ordinaryPreStamp := ordinaryAnchor.ComputeID()
	ordinaryFinal := graph.EntityID(repo, ordinaryAnchor.Kind, ordinaryAnchor.Name, ordinaryAnchor.SourceFile)
	if ordinaryPreStamp == ordinaryFinal {
		t.Fatalf("premise gone: the control anchor's ComputeID and graph.EntityID coincide (%q) — it would never be remapped", ordinaryPreStamp)
	}

	for _, tc := range []struct {
		name         string
		collidersABi bool // true: recA before recB; false: recB before recA
	}{
		{name: "A_then_B", collidersABi: true},
		{name: "B_then_A", collidersABi: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first, second := recA, recB
			if !tc.collidersABi {
				first, second = recB, recA
			}
			collidingFacet := types.EntityRecord{
				Kind: "SCOPE.Schema", Name: "Facet", SourceFile: collidingFile,
				Properties: map[string]string{types.EntityTwinOfProperty: sharedPreStamp},
			}
			ordinaryFacet := types.EntityRecord{
				Kind: "SCOPE.Schema", Name: "OrdinaryFacet", SourceFile: ordinaryFile,
				Properties: map[string]string{types.EntityTwinOfProperty: ordinaryPreStamp},
			}
			records := []types.EntityRecord{first, second, collidingFacet, ordinaryAnchor, ordinaryFacet}

			idx := &Indexer{repoTag: repo}
			idx.stampEntityIDs(records)

			// 1. The ambiguous anchor is left DANGLING, deliberately — the same
			//    value in both arrival orders, never one of the two final ids.
			gotTwin := records[2].Properties[types.EntityTwinOfProperty]
			if gotTwin != sharedPreStamp {
				which := "the SECOND colliding record's"
				switch gotTwin {
				case finalA:
					which = "recA's"
				case finalB:
					which = "recB's"
				}
				t.Errorf("colliding facet grafel.twin_of = %q (%s final id); want it LEFT UNREMAPPED at the pre-stamp id %q — "+
					"an ambiguous key must be skipped, not resolved by arrival order (finalA=%q finalB=%q)",
					gotTwin, which, sharedPreStamp, finalA, finalB)
			}

			// 2. POSITIVE CONTROL: the unambiguous facet in the SAME batch is
			//    still remapped. A guard that skipped every key would pass (1)
			//    and fail here.
			gotOrdinary := records[4].Properties[types.EntityTwinOfProperty]
			if gotOrdinary != ordinaryFinal {
				t.Errorf("ordinary facet grafel.twin_of = %q; want it remapped to the anchor's final id %q (pre-stamp %q) — "+
					"the ambiguity guard must not disable the remap for unambiguous keys",
					gotOrdinary, ordinaryFinal, ordinaryPreStamp)
			}

			// 3. Both colliders still get their OWN distinct final id stamped:
			//    the ambiguity guard governs the remap map, never EntityRecord.ID.
			wantFirst := graph.EntityID(repo, first.Kind, first.Name, first.SourceFile)
			wantSecond := graph.EntityID(repo, second.Kind, second.Name, second.SourceFile)
			if records[0].ID != wantFirst {
				t.Errorf("first colliding record ID = %q; want %q", records[0].ID, wantFirst)
			}
			if records[1].ID != wantSecond {
				t.Errorf("second colliding record ID = %q; want %q", records[1].ID, wantSecond)
			}
			if records[3].ID != ordinaryFinal {
				t.Errorf("ordinary anchor ID = %q; want %q", records[3].ID, ordinaryFinal)
			}
		})
	}
}
