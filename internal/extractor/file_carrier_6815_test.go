package extractor

// #6815 — unit grading for FileCarrierFor's two guard clauses. Each clause has
// at least one input where THAT clause alone rejects, so neither is graded only
// through the other.

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

const carrierPath = "src/cache_server.erl"

func recWithRel(name string, rel types.RelationshipRecord) types.EntityRecord {
	return types.EntityRecord{
		Name:          name,
		Kind:          "SCOPE.Component",
		SourceFile:    carrierPath,
		Relationships: []types.RelationshipRecord{rel},
	}
}

// Axis VARIED: nothing — the baseline positive. HELD CONSTANT: one record, one
// path-anchored edge, no record named after the path.
func TestFileCarrierFor_EmitsWhenAnEdgeIsPathAnchored_6815(t *testing.T) {
	recs := []types.EntityRecord{
		recWithRel("cache.hrl", types.RelationshipRecord{
			FromID: carrierPath, ToID: "cache.hrl", Kind: "IMPORTS"}),
	}
	got, ok := FileCarrierFor(carrierPath, "erlang", recs)
	if !ok {
		t.Fatal("a path-anchored FromID must produce a carrier")
	}
	if got.Name != carrierPath || got.Kind != "SCOPE.Component" || got.Subtype != "file" {
		t.Fatalf("carrier shape = {%q %q %q}, want {%q SCOPE.Component file}",
			got.Name, got.Kind, got.Subtype, carrierPath)
	}
	if got.Language != "erlang" {
		t.Fatalf("carrier Language = %q, want erlang", got.Language)
	}
	if len(got.Relationships) != 0 {
		t.Fatalf("carrier must own no relationships, got %d", len(got.Relationships))
	}
}

// CLAUSE 1 ALONE rejects. Axis VARIED: the edge's FromID (empty — owner-anchored
// — instead of the path). HELD CONSTANT: same record, same kind, and NO record
// is named after the path, so clause 2 is satisfied and cannot be what rejects.
func TestFileCarrierFor_NoCarrierWhenNoEdgeIsPathAnchored_6815(t *testing.T) {
	recs := []types.EntityRecord{
		recWithRel("cache.hrl", types.RelationshipRecord{
			ToID: "cache.hrl", Kind: "IMPORTS"}),
	}
	if _, ok := FileCarrierFor(carrierPath, "erlang", recs); ok {
		t.Fatal("no path-anchored FromID: no carrier is due")
	}
}

// CLAUSE 1 ALONE rejects, second input: a FromID that is a DIFFERENT path.
// Axis VARIED: which path the edge is anchored on. HELD CONSTANT: the edge is
// path-anchored, so a guard that merely asked "is FromID non-empty" would pass.
func TestFileCarrierFor_AnotherFilesAnchorDoesNotCount_6815(t *testing.T) {
	recs := []types.EntityRecord{
		recWithRel("cache.hrl", types.RelationshipRecord{
			FromID: "src/other.erl", ToID: "cache.hrl", Kind: "IMPORTS"}),
	}
	if _, ok := FileCarrierFor(carrierPath, "erlang", recs); ok {
		t.Fatal("an edge anchored on a different path must not mint this file's carrier")
	}
}

// CLAUSE 2 ALONE rejects. Axis VARIED: an existing record already named after
// the path. HELD CONSTANT: the path-anchored edge is present, so clause 1 is
// satisfied and cannot be what rejects.
func TestFileCarrierFor_NoSecondCarrierWhenOneIsAlreadyNamedAfterThePath_6815(t *testing.T) {
	recs := []types.EntityRecord{
		{Name: carrierPath, Kind: "SCOPE.Component", Subtype: "file", SourceFile: carrierPath},
		recWithRel("cache.hrl", types.RelationshipRecord{
			FromID: carrierPath, ToID: "cache.hrl", Kind: "IMPORTS"}),
	}
	if _, ok := FileCarrierFor(carrierPath, "erlang", recs); ok {
		t.Fatal("a record already carries this path as its Name: no second carrier is due")
	}
}

// CLAUSE 2 ALONE rejects, REORDERED. Axis VARIED: the POSITION of the
// path-named record — after the anchoring record instead of before it. HELD
// CONSTANT: both records, their contents, and the verdict, which must not
// depend on emission order.
//
// This is a separate case rather than a table row on the one above because the
// two grade different mutants. The case above is satisfied by an
// implementation that stops scanning for a path-named record as soon as
// anchoring is established (hoisting `if anchored { continue }` above the Name
// check): the path-named record is seen first there, so the early exit is never
// reached. Under that implementation THIS case mints a second carrier — two
// nodes under one id, which is exactly the ambiguous rewrite target clause 2
// exists to prevent.
//
// NOT justified by a known in-tree emission order: none of the three callers
// mints a path-named container at all today, so neither ordering is currently
// produced by a real extractor. That is the reason to pin it rather than a
// reason not to — clause 2's correctness must not rest on an ordering
// assumption nothing in the tree states or enforces.
func TestFileCarrierFor_NoSecondCarrierWhenThePathNamedRecordComesLast_6815(t *testing.T) {
	recs := []types.EntityRecord{
		recWithRel("cache.hrl", types.RelationshipRecord{
			FromID: carrierPath, ToID: "cache.hrl", Kind: "IMPORTS"}),
		{Name: carrierPath, Kind: "SCOPE.Component", Subtype: "file", SourceFile: carrierPath},
	}
	if _, ok := FileCarrierFor(carrierPath, "erlang", recs); ok {
		t.Fatal("a record named after this path rejects wherever it sits in the slice, " +
			"not only when it precedes the anchoring record")
	}
}

// Any edge kind anchors, not only IMPORTS: the resolution requirement is about
// the FromID, not about the kind. Axis VARIED: the relationship Kind. HELD
// CONSTANT: the anchoring FromID.
func TestFileCarrierFor_AnchorIsAboutFromIDNotKind_6815(t *testing.T) {
	recs := []types.EntityRecord{
		recWithRel("thing", types.RelationshipRecord{
			FromID: carrierPath, ToID: "thing", Kind: "CONTAINS"}),
	}
	if _, ok := FileCarrierFor(carrierPath, "erlang", recs); !ok {
		t.Fatal("a CONTAINS edge anchored on the path needs the carrier just as much")
	}
}

// An empty path can never be an anchor; guard against minting a nameless node.
func TestFileCarrierFor_EmptyPathNeverCarries_6815(t *testing.T) {
	recs := []types.EntityRecord{
		recWithRel("x", types.RelationshipRecord{ToID: "x", Kind: "IMPORTS"}),
	}
	if _, ok := FileCarrierFor("", "erlang", recs); ok {
		t.Fatal("an empty path must never mint a carrier")
	}
}

// PrependFileCarrier puts the carrier FIRST (the #577 convention several
// consumers index on) and leaves the rest in order.
func TestPrependFileCarrier_CarrierIsFirstAndOrderIsKept_6815(t *testing.T) {
	recs := []types.EntityRecord{
		recWithRel("cache.hrl", types.RelationshipRecord{
			FromID: carrierPath, ToID: "cache.hrl", Kind: "IMPORTS"}),
		{Name: "cache_server", Kind: "SCOPE.Component", SourceFile: carrierPath},
	}
	out := PrependFileCarrier(carrierPath, "erlang", recs)
	if len(out) != 3 {
		t.Fatalf("want 3 records, got %d", len(out))
	}
	if out[0].Name != carrierPath || out[0].Subtype != "file" {
		t.Fatalf("carrier must be first, got %q/%q", out[0].Name, out[0].Subtype)
	}
	if out[1].Name != "cache.hrl" || out[2].Name != "cache_server" {
		t.Fatalf("original order not preserved: %q %q", out[1].Name, out[2].Name)
	}
}

// PrependFileCarrier is a no-op when nothing is due — it must not allocate a
// different slice content or drop records.
func TestPrependFileCarrier_NoOpWhenNothingIsDue_6815(t *testing.T) {
	recs := []types.EntityRecord{
		{Name: "cache_server", Kind: "SCOPE.Component", SourceFile: carrierPath},
	}
	out := PrependFileCarrier(carrierPath, "erlang", recs)
	if len(out) != 1 || out[0].Name != "cache_server" {
		t.Fatalf("no-op expected, got %d records (first %q)", len(out), out[0].Name)
	}
}
