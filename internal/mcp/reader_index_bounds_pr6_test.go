package mcp

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// Memory epic #5850, Path P PR6: getByID / LabelIndex.at MUST bound their
// vector-index lookups against the RESIDENT READER's row count on the
// GRAFEL_SERVE_FROM_MMAP-ON path, not len(Doc.Entities) — a PR7 Doc-emptying
// drops doc.Entities to length 0 while the reader still holds every row, and
// a Doc-sourced bound would wrongly report every valid index out of range.
//
// These tests simulate the post-PR7 shape by handing the index a Document
// whose Entities slice has been emptied, while the reader over the SAME
// underlying graph.fb still has every row resident — exactly the split PR7
// introduces.

// emptiedParityDoc returns a shallow copy of doc with Entities zeroed out —
// simulating the PR7 Doc-emptying while leaving everything else (used only
// for logging/asserts, not read by the reader path) intact.
func emptiedParityDoc(doc *graph.Document) *graph.Document {
	cp := *doc
	cp.Entities = nil
	return &cp
}

func withServeFromMMap(t *testing.T, v bool) {
	t.Helper()
	prev := serveFromMMapEnabled
	serveFromMMapEnabled = v
	t.Cleanup(func() { serveFromMMapEnabled = prev })
}

// TestLabelIndexAt_ReaderSourcedBound_PR6 is the mutation-catching test: with
// GRAFEL_SERVE_FROM_MMAP ON, a resident reader, and an EMPTIED Doc, at() must
// still resolve every valid reader-vector index (and correctly report nil for
// an out-of-range one) using the reader's own EntityCount as the bound — not
// len(doc.Entities), which is 0 here. A regression back to
// `len(l.doc.Entities)` on the ON path makes this fail: every idx >= 0 would
// be rejected as out-of-range and this test would see nil for every valid id.
func TestLabelIndexAt_ReaderSourcedBound_PR6(t *testing.T) {
	withServeFromMMap(t, true)

	doc, r := loadParityIndexFixture(t)
	if len(doc.Entities) == 0 {
		t.Fatalf("fixture setup: doc.Entities is unexpectedly empty")
	}
	full := BuildLabelIndexFromReader(r, doc)
	full.doc = emptiedParityDoc(doc) // simulate PR7's Doc-emptying

	if got := int(r.EntityCount()); got != len(doc.Entities) {
		t.Fatalf("fixture sanity: reader EntityCount=%d != doc entity count=%d", got, len(doc.Entities))
	}

	// Every valid vector index must still resolve to the correct entity ID,
	// sourced from the reader (Doc is empty, so a Doc-sourced value would be a
	// zero-value/garbage entity, not a nil pointer — the failure mode this test
	// pins is a wrong BOUND, not a wrong value source, which PR2 already covers).
	for i, want := range doc.Entities {
		got := full.at(int32(i))
		if got == nil {
			t.Fatalf("at(%d): got nil, want entity id=%q (reader-sourced bound should admit this index)", i, want.ID)
		}
		if got.ID != want.ID {
			t.Errorf("at(%d): got id=%q, want id=%q", i, got.ID, want.ID)
		}
	}

	// Out-of-range must still correctly report not-found via the reader bound.
	oob := int32(len(doc.Entities))
	if got := full.at(oob); got != nil {
		t.Errorf("at(%d) out-of-range: got %+v, want nil", oob, got)
	}
	if got := full.at(-1); got != nil {
		t.Errorf("at(-1): got %+v, want nil", got)
	}
}

// TestLabelIndexAt_FlagOff_DocSourcedBound_Unchanged_PR6 pins that the
// flag-OFF path is byte-identical to pre-PR6: it still bounds against
// len(doc.Entities) and therefore correctly treats an emptied Doc as having
// zero valid indices (there is no reader fallback on the OFF path).
func TestLabelIndexAt_FlagOff_DocSourcedBound_Unchanged_PR6(t *testing.T) {
	withServeFromMMap(t, false)

	doc, r := loadParityIndexFixture(t)
	full := BuildLabelIndexFromReader(r, doc)

	// With the real (non-emptied) Doc, every valid index still resolves.
	for i, want := range doc.Entities {
		got := full.at(int32(i))
		if got == nil || got.ID != want.ID {
			t.Fatalf("at(%d) flag-off: got %+v, want id=%q", i, got, want.ID)
		}
	}

	// Emptying the Doc on the flag-OFF path must now correctly find nothing —
	// there is no reader fallback here by design (OFF never dereferences mmap).
	full.doc = emptiedParityDoc(doc)
	if got := full.at(0); got != nil {
		t.Errorf("at(0) flag-off with emptied Doc: got %+v, want nil (no reader fallback on OFF)", got)
	}
}

// TestGetByID_ReaderSourcedBound_PR6 is the getByID counterpart: with the
// flag ON, a resident LabelIndex+reader, and an emptied Doc, getByID must
// still return every entity — the loop bound must come from the reader's
// EntityCount, not len(lr.Doc.Entities) (which is 0 here and would silently
// collapse the map to empty).
func TestGetByID_ReaderSourcedBound_PR6(t *testing.T) {
	withServeFromMMap(t, true)

	doc, r := loadParityIndexFixture(t)
	idx := BuildLabelIndexFromReader(r, doc)

	lr := &LoadedRepo{
		Repo:       "repo",
		Doc:        emptiedParityDoc(doc), // simulate PR7's Doc-emptying
		Reader:     r,
		LabelIndex: idx,
	}
	idx.doc = lr.Doc // at() reads l.doc; keep it consistent with lr.Doc

	got := lr.getByID()
	if len(got) != len(doc.Entities) {
		t.Fatalf("getByID with emptied Doc + resident reader: got %d entities, want %d (reader-sourced bound should visit every row)", len(got), len(doc.Entities))
	}
	for _, want := range doc.Entities {
		e, ok := got[want.ID]
		if !ok {
			t.Errorf("getByID: missing entity id=%q", want.ID)
			continue
		}
		if e.ID != want.ID {
			t.Errorf("getByID[%q]: got id=%q", want.ID, e.ID)
		}
	}
}

// TestGetByID_FlagOff_Unchanged_PR6 pins flag-OFF getByID is unaffected by
// PR6: still sources the count and every value straight off the live Doc.
func TestGetByID_FlagOff_Unchanged_PR6(t *testing.T) {
	withServeFromMMap(t, false)

	doc, _ := loadParityIndexFixture(t)
	lr := &LoadedRepo{Repo: "repo", Doc: doc}

	got := lr.getByID()
	if len(got) != len(doc.Entities) {
		t.Fatalf("getByID flag-off: got %d entities, want %d", len(got), len(doc.Entities))
	}
	for _, want := range doc.Entities {
		e, ok := got[want.ID]
		if !ok || e.ID != want.ID {
			t.Errorf("getByID[%q] flag-off: got %+v", want.ID, e)
		}
	}
}
