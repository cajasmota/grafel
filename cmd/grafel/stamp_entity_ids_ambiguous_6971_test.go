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

// TestStampEntityIDs_BothPathsSkipTheSameRecords pins the invariant that
// stampEntityIDs' two loops — the fast path taken when no record in the batch
// carries a grafel.twin_of, and the slow remap-building path taken when one
// does — stamp exactly the SAME SET of records. They differ only in whether
// they build the remap.
//
// Nothing held this before. Coordinator mutant MS-2 on #6971 removed the
// `r.Name == ""` skip from the SLOW path only and survived the whole
// ./cmd/grafel/ suite. Under it an empty-name record acquires an ID only when
// some OTHER, entirely unrelated record in the same batch happens to carry a
// twin_of — a record's identity depending on a stranger's properties, which is
// action at a distance and reads as a heisenbug when it bites.
//
// So the assertion is deliberately a COMPARISON between the two paths, not a
// restatement of either one's rule: the same empty-name record is stamped in
// two batches identical but for a twin_of carrier, and the results must agree.
//
// KNOWN LIMIT OF THE Fatalf PINS BELOW: they call recordsHaveTwinOf on each
// batch, i.e. they pin the PREDICATE, not the DISPATCH — they do not observe
// which loop stampEntityIDs actually entered. Reviewer mutant R4 (force the
// gate always-fast) therefore passes THIS test in isolation; it dies at suite
// level on the #6275 twin_of tests, which can only pass if the slow path runs.
// The coverage exists, just not where a reader of this test would look.
func TestStampEntityIDs_BothPathsSkipTheSameRecords(t *testing.T) {
	const (
		repo         = "test_repo"
		file         = "app/models/order.py"
		carrierFile  = "app/models/carrier.py"
		carrierName  = "Carrier"
		carrierKind  = "SCOPE.Schema"
		namedRecName = "Order"
	)

	// The record under test: Name == "", which BOTH paths must skip.
	emptyName := func() types.EntityRecord {
		return types.EntityRecord{Kind: "SCOPE.Component", Name: "", SourceFile: file, StartLine: 3}
	}
	// A normally-named record, present in both batches, so the batches differ
	// in exactly one thing: the twin_of that selects the path.
	named := func() types.EntityRecord {
		return types.EntityRecord{Kind: "SCOPE.Component", Name: namedRecName, SourceFile: file, StartLine: 9}
	}
	// The path selector. Its twin_of names an anchor that is not in the batch,
	// so it only flips recordsHaveTwinOf — it changes nothing else.
	carrier := func() types.EntityRecord {
		return types.EntityRecord{
			Kind: carrierKind, Name: carrierName, SourceFile: carrierFile,
			Properties: map[string]string{types.EntityTwinOfProperty: "not-an-anchor-in-this-batch"},
		}
	}

	idx := &Indexer{repoTag: repo}

	fastBatch := []types.EntityRecord{emptyName(), named()}
	if recordsHaveTwinOf(fastBatch) {
		t.Fatalf("premise gone: the twin_of-free batch takes the SLOW path — this test would compare the slow path with itself")
	}
	idx.stampEntityIDs(fastBatch)

	slowBatch := []types.EntityRecord{emptyName(), named(), carrier()}
	if !recordsHaveTwinOf(slowBatch) {
		t.Fatalf("premise gone: the twin_of-carrying batch takes the FAST path — this test would compare the fast path with itself")
	}
	idx.stampEntityIDs(slowBatch)

	if fastBatch[0].ID != slowBatch[0].ID {
		t.Errorf("the SAME empty-name record was stamped differently by the two paths: fast=%q slow=%q — "+
			"both loops must stamp the same SET of records; they differ only in whether they build the twin_of remap, "+
			"so a record's identity must never depend on whether an unrelated record in the batch carries a grafel.twin_of",
			fastBatch[0].ID, slowBatch[0].ID)
	}

	// The paths must agree by SKIPPING the record, not by both stamping it.
	// Reviewer mutant R6 — delete the `Name == ""` skip from BOTH loops — kept
	// the parity assertion above satisfied (both stamped 58e1ead07da096b0) and
	// left the ENTIRE ./cmd/grafel/ package green. The lockstep was pinned; the
	// rule the two loops are in lockstep ABOUT was not. This observes it.
	if fastBatch[0].ID != "" || slowBatch[0].ID != "" {
		t.Errorf("the empty-name record was STAMPED by both paths (fast=%q slow=%q); the paths must agree by SKIPPING it, not by both stamping it", fastBatch[0].ID, slowBatch[0].ID)
	}

	// Positive control for the other direction: agreement must not come from the
	// loops being globally inert. A normally-named record must be stamped,
	// identically, by each.
	wantNamed := graph.EntityID(repo, "SCOPE.Component", namedRecName, file)
	if fastBatch[1].ID != wantNamed {
		t.Errorf("fast path did not stamp the named record: got %q want %q", fastBatch[1].ID, wantNamed)
	}
	if slowBatch[1].ID != wantNamed {
		t.Errorf("slow path did not stamp the named record: got %q want %q", slowBatch[1].ID, wantNamed)
	}
}
