package main

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// Issue #6275 — a #6104 merge facet's grafel.twin_of property is computed by
// internal/extractors/custom_dispatch.go's effectiveID BEFORE stampEntityIDs
// runs: when the base entity's ID has not been stamped yet, effectiveID falls
// back to EntityRecord.ComputeID(), a DIFFERENT hash than the one
// stampEntityIDs finally assigns via graph.EntityID(repoTag, Kind, Name,
// SourceFile). The twin_of VALUE is a static string, never rekeyed the way
// relationship FromID/ToID endpoints are (see the #4406 dedup path and the
// #1613 class-shadow fold's remap maps) — so it is permanently orphaned: it
// names an ID that will never appear in the graph, and
// internal/resolve/symbol_index.go's facet-alias rule (#6104) can never match
// it to its anchor.
//
// The fix mirrors the existing precedent (retired/remap maps at the merge and
// fold boundaries): stampEntityIDs now also records old-computed-ID ->
// final-stamped-ID for every record, and rewrites any grafel.twin_of value
// found in that map so it points at the anchor's REAL final ID.
func TestStampEntityIDs_RewritesTwinOfToFinalID(t *testing.T) {
	const (
		repo = "test_repo"
		file = "com/example/demo/model/User.java"
		name = "User"
	)

	// The base AST class node — no ID set yet, mirrors a language extractor's
	// raw output before stampEntityIDs runs.
	base := types.EntityRecord{
		Kind: "SCOPE.Component", Name: name, SourceFile: file,
		Subtype: "class", StartLine: 7, EndLine: 20,
	}
	// twin_of computed the way enrichFromTwin computes it: effectiveID falls
	// back to ComputeID() because base.ID is still empty at merge time.
	preStampAnchorID := base.ComputeID()

	// Pinned to the LITERAL orphan id from the original #6275 report — this
	// test's (Kind, Name, SourceFile) match the real java-spring-mini fixture
	// exactly, and ComputeID() (sha256 of OrgID+ProjectID+SourceFile+Kind+Name,
	// both org/project empty here as in the real run) is fully deterministic,
	// so this is not a coincidence: it proves this test reproduces the actual
	// production defect rather than a same-shaped-but-different bug.
	const originalReportOrphanID = "162ef96c9dd92041"
	if preStampAnchorID != originalReportOrphanID {
		t.Fatalf("preStampAnchorID = %q; want the literal orphan id %q from the original #6275 report — "+
			"if this legitimately changed (e.g. ComputeID()'s hash formula), update this constant deliberately",
			preStampAnchorID, originalReportOrphanID)
	}

	facet := types.EntityRecord{
		Kind: "SCOPE.Schema", Name: name, SourceFile: file,
		StartLine:  7,
		EndLine:    20,
		Properties: map[string]string{types.EntityTwinOfProperty: preStampAnchorID},
	}

	// A same-(Kind,Name,SourceFile) collider — the ormlink sentinel shape:
	// same Kind as base ("SCOPE.Component"), so it collapses onto the SAME
	// graph.EntityID as base at assembly time (#6275's actual collision).
	sentinel := types.EntityRecord{
		Kind: "SCOPE.Component", Name: name, SourceFile: file,
		Subtype: "orm_model_sentinel",
	}

	idx := &Indexer{repoTag: repo}
	pass1 := []types.EntityRecord{sentinel, base, facet}
	doc := idx.buildDocument(&pass1, nil, nil, nil)

	wantAnchorID := graph.EntityID(repo, "SCOPE.Component", name, file)
	if preStampAnchorID == wantAnchorID {
		t.Fatalf("test setup invalid: ComputeID() and graph.EntityID() coincidentally agree (%s) — this test does not exercise the hash mismatch", preStampAnchorID)
	}

	got := findEntity(t, doc.Entities, graph.EntityID(repo, "SCOPE.Schema", name, file))
	gotTwin, ok := got.PropLookup(types.EntityTwinOfProperty)
	if !ok {
		t.Fatalf("facet lost its grafel.twin_of property entirely")
	}
	if gotTwin != wantAnchorID {
		t.Errorf("facet grafel.twin_of = %q; want it rewritten to the anchor's FINAL stamped id %q (was pre-stamp id %q)",
			gotTwin, wantAnchorID, preStampAnchorID)
	}
	// The anchor itself must actually exist in the document under that id
	// (the #6275 symptom was precisely that it did not).
	findEntity(t, doc.Entities, wantAnchorID)
}

// TestStampEntityIDs_TwinOfUnaffectedWhenAlreadyCorrect — a twin_of value
// that already matches its anchor's final ID (the common case: no early/late
// hash mismatch, e.g. the anchor was assigned its ID before merge) must pass
// through unchanged.
func TestStampEntityIDs_TwinOfUnaffectedWhenAlreadyCorrect(t *testing.T) {
	const (
		repo = "test_repo"
		file = "app/models/order.py"
		name = "Order"
	)
	anchorID := graph.EntityID(repo, "SCOPE.Component", name, file)

	anchor := types.EntityRecord{Kind: "SCOPE.Component", Name: name, SourceFile: file, StartLine: 3}
	facet := types.EntityRecord{
		Kind: "SCOPE.Schema", Name: name, SourceFile: file, StartLine: 3,
		Properties: map[string]string{types.EntityTwinOfProperty: anchorID},
	}

	idx := &Indexer{repoTag: repo}
	pass1 := []types.EntityRecord{anchor, facet}
	doc := idx.buildDocument(&pass1, nil, nil, nil)

	got := findEntity(t, doc.Entities, graph.EntityID(repo, "SCOPE.Schema", name, file))
	gotTwin, _ := got.PropLookup(types.EntityTwinOfProperty)
	if gotTwin != anchorID {
		t.Errorf("facet grafel.twin_of = %q; want unchanged %q", gotTwin, anchorID)
	}
}
