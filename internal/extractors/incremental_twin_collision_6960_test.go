// Package extractors — incremental_twin_collision_6960_test.go
//
// remapTwinOfAnchors keys a map on types.EntityRecord.ComputeID and stores
// graph.EntityID. The two disagree about FIELD BOUNDARIES:
//
//	ComputeID   OrgID + ProjectID + SourceFile + Kind + Name   — concatenated, NO separators
//	EntityID    every field NUL-separated
//
// so a pair of records can be one key and two entities. This file is the direct
// unit test for what happens then. It needs no fixture, no repo and no corpus:
// the collision is a property of the two hash preimages, and the pair below is
// constructed from that property rather than discovered in a codebase.
package extractors

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

const twinRepoTag6960 = "test-repo"

// collidingPair6960 returns two records that share one ComputeID and have two
// distinct EntityIDs: "A"+"BC" and "AB"+"C" concatenate identically.
//
// Both names are NON-EMPTY on purpose. The `r.Name == ""` guard in
// remapTwinOfAnchors skips nameless records, so a pair that leans on an empty
// name proves nothing about the reachable path — it would be testing the guard,
// not the collision.
func collidingPair6960() (types.EntityRecord, types.EntityRecord) {
	a := types.EntityRecord{SourceFile: "x.go", Kind: "A", Name: "BC"}
	b := types.EntityRecord{SourceFile: "x.go", Kind: "AB", Name: "C"}
	return a, b
}

func TestRemapTwinOfAnchorsSkipsAmbiguousPreStampIDs6960(t *testing.T) {
	a, b := collidingPair6960()

	// PREMISE 1 — the two records really do collide under the map's key. If
	// ComputeID ever gains separators this stops being true, and the test must
	// fail loudly rather than pass vacuously: with no collision there is no
	// ambiguity and the assertion below would hold for the wrong reason.
	shared := a.ComputeID()
	if shared != b.ComputeID() {
		t.Fatalf("premise: ComputeID no longer collides for {%s,%s} and {%s,%s} (%s vs %s) — "+
			"the separator-free preimage this test is built on is gone, so this test grades "+
			"nothing and should be re-derived, not deleted",
			a.Kind, a.Name, b.Kind, b.Name, shared, b.ComputeID())
	}
	// PREMISE 2 — and they are genuinely DIFFERENT entities. Without this the
	// "ambiguous" case is a distinction without a difference and skipping is
	// pointless.
	finalA := graph.EntityID(twinRepoTag6960, a.Kind, a.Name, a.SourceFile)
	finalB := graph.EntityID(twinRepoTag6960, b.Kind, b.Name, b.SourceFile)
	if finalA == finalB {
		t.Fatalf("premise: graph.EntityID also collides (%s) — the two records are one entity, "+
			"so there is no wrong node to be pointed at", finalA)
	}

	// The facet: a third record whose twin_of names the shared preimage.
	facet := types.EntityRecord{
		SourceFile: "x.go", Kind: "SCOPE.Type", Name: "Facet",
		Properties: map[string]string{types.EntityTwinOfProperty: shared},
	}

	records := []types.EntityRecord{a, b, facet}
	remapTwinOfAnchors(records, twinRepoTag6960)

	got := records[2].Properties[types.EntityTwinOfProperty]
	if got == finalA || got == finalB {
		t.Errorf("#6369/#6960: an ambiguous grafel.twin_of was remapped to %q — one of the two "+
			"entities (%s / %s) that share the pre-stamp key %s. Which one is decided by arrival "+
			"order, not by evidence, so this is a CONFIDENTLY WRONG anchor: classfold's "+
			"twinAnchors and the symbol index's facet-alias rule both read it as valid and alias "+
			"the name onto whichever record happened to come last. Leaving it dangling is the "+
			"detectable failure and the intended behaviour",
			got, finalA, finalB, shared)
	}
	if got != shared {
		t.Errorf("ambiguous twin_of = %q, want it left at its pre-stamp value %q — the skip must "+
			"leave the property untouched, not blank it, so the value remains diagnosable",
			got, shared)
	}

	// REVERSED ORDER. The whole hazard is that the survivor is decided by index
	// order, so a test that only ever sees one order cannot tell "skipped" from
	// "happened to pick the one I asserted".
	reversed := []types.EntityRecord{b, a, facet}
	reversed[2].Properties = map[string]string{types.EntityTwinOfProperty: shared}
	remapTwinOfAnchors(reversed, twinRepoTag6960)
	if got := reversed[2].Properties[types.EntityTwinOfProperty]; got != shared {
		t.Errorf("ambiguous twin_of = %q with the colliding records in the opposite order, want "+
			"%q — the outcome must not depend on which record arrives first", got, shared)
	}
}

// TestRemapTwinOfAnchorsStillRemapsUnambiguousKeys6960 is the positive control
// for the test above: a check that skipped EVERYTHING would satisfy every
// assertion there while silently reverting the #6275 fix this PR exists for.
func TestRemapTwinOfAnchorsStillRemapsUnambiguousKeys6960(t *testing.T) {
	anchor := types.EntityRecord{SourceFile: "x.go", Kind: "SCOPE.Component", Name: "Widget"}
	pre := anchor.ComputeID()
	final := graph.EntityID(twinRepoTag6960, anchor.Kind, anchor.Name, anchor.SourceFile)
	if pre == final {
		t.Fatalf("premise: ComputeID and EntityID agree for this record (%s), so there is no "+
			"remap to perform and the assertion below is vacuous", pre)
	}
	facet := types.EntityRecord{
		SourceFile: "x.go", Kind: "SCOPE.Type", Name: "Widget",
		Properties: map[string]string{types.EntityTwinOfProperty: pre},
	}

	records := []types.EntityRecord{anchor, facet}
	remapTwinOfAnchors(records, twinRepoTag6960)

	if got := records[1].Properties[types.EntityTwinOfProperty]; got != final {
		t.Errorf("unambiguous twin_of = %q, want the anchor's final id %q — the collision check "+
			"must narrow the remap to ambiguous keys only, not disable it", got, final)
	}
}

// TestRemapTwinOfAnchorsIsInertWithoutAFacet6960 pins the cheap pre-scan: a
// batch carrying no twin_of at all must not be walked twice or hashed at all.
// Asserted behaviourally — no record's properties change — because the counter
// version of this claim would be a diagnostic signature, not the consequence.
func TestRemapTwinOfAnchorsIsInertWithoutAFacet6960(t *testing.T) {
	a, b := collidingPair6960()
	a.Properties = map[string]string{"role": "class"}
	records := []types.EntityRecord{a, b}
	remapTwinOfAnchors(records, twinRepoTag6960)
	if records[0].Properties["role"] != "class" || len(records[0].Properties) != 1 {
		t.Errorf("a batch with no grafel.twin_of had its properties touched: %v", records[0].Properties)
	}
	if records[1].Properties != nil {
		t.Errorf("a record with no properties gained a map: %v", records[1].Properties)
	}
}
