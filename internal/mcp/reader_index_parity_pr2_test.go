package mcp

import (
	"reflect"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// ADR-0027 Cutover PR2: the LabelIndex holds ENTITY INDICES (int32) and
// MATERIALIZES a fresh heap-copy entity per lookup, instead of pinning
// *graph.Entity pointers ALIASING Document.Entities. The int32 index MAPS are
// built by iterating the resident mmap Reader (BuildLabelIndexFromReader); the
// per-lookup VALUE copy is taken from the live Document (behavior-neutral vs the
// post-load in-place group-algo overlay — see the LabelIndex type doc).
//
// These tests assert:
//   1. MAP PARITY — the Reader-built int32 maps equal the Document-built maps
//      over the SAME real graph.fb (Reader iteration order == loader order).
//   2. VALUE PARITY — ByID / ByQName / Lookup / LookupAll return byte-equal
//      results on the Reader-built and Document-built index.
//   3. POINTER INSTABILITY — two lookups of the same id return DISTINCT pointers
//      (the property the PR2 retained-pointer audit gates PR7 on) yet byte-equal
//      VALUES; getByID by contrast returns a stable per-reload map of copies.
//
// The fixture (parityIndexDoc / loadParityIndexFixture) is shared with the PR1
// parity tests in reader_index_parity_pr1_test.go.

// labelIndexIDs returns every entity id in the fixture, in Document order.
func labelIndexIDs(doc *graph.Document) []string {
	ids := make([]string, 0, len(doc.Entities))
	for i := range doc.Entities {
		ids = append(ids, doc.Entities[i].ID)
	}
	return ids
}

// TestLabelIndexMapsReaderParity_PR2 asserts the int32 index MAPS built by
// iterating the Reader are identical to those built from the Document — i.e. the
// Reader iteration order and keying exactly match the loader's. This is the
// core "index can be built from the Reader, no Doc.Entities needed" guarantee.
func TestLabelIndexMapsReaderParity_PR2(t *testing.T) {
	t.Parallel()
	doc, r := loadParityIndexFixture(t)
	wantIdx := BuildLabelIndex(doc)
	gotIdx := BuildLabelIndexFromReader(r, doc)

	if !reflect.DeepEqual(gotIdx.byID, wantIdx.byID) {
		t.Errorf("byID map Reader-built != Document-built\n got=%v\nwant=%v", gotIdx.byID, wantIdx.byID)
	}
	if !reflect.DeepEqual(gotIdx.byLabel, wantIdx.byLabel) {
		t.Errorf("byLabel map Reader-built != Document-built\n got=%v\nwant=%v", gotIdx.byLabel, wantIdx.byLabel)
	}
	if !reflect.DeepEqual(gotIdx.byQName, wantIdx.byQName) {
		t.Errorf("byQName map Reader-built != Document-built\n got=%v\nwant=%v", gotIdx.byQName, wantIdx.byQName)
	}
}

func TestLabelIndexByIDReaderParity_PR2(t *testing.T) {
	t.Parallel()
	doc, r := loadParityIndexFixture(t)
	wantIdx := BuildLabelIndex(doc)             // Document-sourced (nil-Reader) baseline
	gotIdx := BuildLabelIndexFromReader(r, doc) // Reader-sourced

	for _, id := range labelIndexIDs(doc) {
		want := wantIdx.ByID(id)
		got := gotIdx.ByID(id)
		if want == nil || got == nil {
			t.Fatalf("ByID(%q): want=%v got=%v (neither may be nil)", id, want, got)
		}
		if !reflect.DeepEqual(*got, *want) {
			t.Errorf("ByID(%q) Reader-sourced != Document-sourced\n got=%#v\nwant=%#v", id, *got, *want)
		}
	}
	// Unknown id resolves to nil on both branches.
	if gotIdx.ByID("nope::x") != nil || wantIdx.ByID("nope::x") != nil {
		t.Errorf("ByID(unknown) must be nil on both branches")
	}
	// HasID must not materialize and must agree with membership.
	if !gotIdx.HasID("id::a") || gotIdx.HasID("nope::x") {
		t.Errorf("HasID contract broken: HasID(id::a)=%v HasID(nope)=%v",
			gotIdx.HasID("id::a"), gotIdx.HasID("nope::x"))
	}
}

func TestLabelIndexByQNameReaderParity_PR2(t *testing.T) {
	t.Parallel()
	doc, r := loadParityIndexFixture(t)
	wantIdx := BuildLabelIndex(doc)
	gotIdx := BuildLabelIndexFromReader(r, doc)

	for i := range doc.Entities {
		qn := doc.Entities[i].QualifiedName
		if qn == "" {
			continue
		}
		want := wantIdx.ByQName(qn)
		got := gotIdx.ByQName(qn)
		if want == nil || got == nil {
			t.Fatalf("ByQName(%q): want=%v got=%v", qn, want, got)
		}
		if !reflect.DeepEqual(*got, *want) {
			t.Errorf("ByQName(%q) Reader-sourced != Document-sourced\n got=%#v\nwant=%#v", qn, *got, *want)
		}
		// Case-insensitivity: an all-upper qname must resolve identically.
		if gotIdx.ByQName(qn) == nil {
			t.Errorf("ByQName(%q) case-insensitive lookup returned nil", qn)
		}
	}
}

func TestLabelIndexLookupReaderParity_PR2(t *testing.T) {
	t.Parallel()
	doc, r := loadParityIndexFixture(t)
	wantIdx := BuildLabelIndex(doc)
	gotIdx := BuildLabelIndexFromReader(r, doc)

	// Lookup by id, by qname, and by label; plus LookupAll on every label.
	probes := []string{}
	for i := range doc.Entities {
		e := &doc.Entities[i]
		probes = append(probes, e.ID, e.Name, e.QualifiedName)
	}
	for _, s := range probes {
		if s == "" {
			continue
		}
		want := wantIdx.Lookup(s)
		got := gotIdx.Lookup(s)
		switch {
		case want == nil && got == nil:
			// both miss — fine
		case want == nil || got == nil:
			t.Errorf("Lookup(%q): want=%v got=%v (branch disagreement)", s, want, got)
		default:
			if !reflect.DeepEqual(*got, *want) {
				t.Errorf("Lookup(%q) Reader != Document\n got=%#v\nwant=%#v", s, *got, *want)
			}
		}

		wantAll := wantIdx.LookupAll(s)
		gotAll := gotIdx.LookupAll(s)
		if len(wantAll) != len(gotAll) {
			t.Fatalf("LookupAll(%q) len: want=%d got=%d", s, len(wantAll), len(gotAll))
		}
		for i := range wantAll {
			if !reflect.DeepEqual(*gotAll[i], *wantAll[i]) {
				t.Errorf("LookupAll(%q)[%d] Reader != Document\n got=%#v\nwant=%#v",
					s, i, *gotAll[i], *wantAll[i])
			}
		}
	}
}

// TestLabelIndexMaterializesFreshPointers_PR2 locks in the pointer-instability
// contract the PR7 flip depends on: two lookups of the same id return DISTINCT
// *graph.Entity pointers (fresh heap copies) whose VALUES are byte-equal. A
// consumer that dedups/compares by pointer identity would break — which is why
// the PR2 audit swept them.
func TestLabelIndexMaterializesFreshPointers_PR2(t *testing.T) {
	t.Parallel()
	doc, r := loadParityIndexFixture(t)

	for _, idx := range []*LabelIndex{BuildLabelIndexFromReader(r, doc), BuildLabelIndex(doc)} {
		a := idx.ByID("id::a")
		b := idx.ByID("id::a")
		if a == nil || b == nil {
			t.Fatalf("ByID(id::a) returned nil")
		}
		if a == b {
			t.Errorf("expected DISTINCT pointers from two lookups; got the same pointer %p", a)
		}
		if !reflect.DeepEqual(*a, *b) {
			t.Errorf("materialized copies must be byte-equal:\n a=%#v\n b=%#v", *a, *b)
		}
		// Mutating one copy must not affect a subsequent lookup (no aliasing).
		a.Name = "MUTATED"
		if c := idx.ByID("id::a"); c == nil || c.Name == "MUTATED" {
			t.Errorf("materialized copy aliased the index source (mutation leaked): %#v", c)
		}
	}
}

// TestGetByIDMaterializesStableMap_PR2 asserts getByID still returns a stable
// id->*Entity map (values fixed for the reload) sourced from the Reader, so its
// consumers are insulated from the per-lookup pointer instability above.
func TestGetByIDMaterializesStableMap_PR2(t *testing.T) {
	t.Parallel()
	doc, r := loadParityIndexFixture(t)
	lr := &LoadedRepo{Repo: "repo", Doc: doc, Reader: r, LabelIndex: BuildLabelIndexFromReader(r, doc)}

	m1 := lr.getByID()
	m2 := lr.getByID()
	for _, id := range labelIndexIDs(doc) {
		e1, ok1 := m1[id]
		e2, ok2 := m2[id]
		if !ok1 || !ok2 {
			t.Fatalf("getByID missing id %q (ok1=%v ok2=%v)", id, ok1, ok2)
		}
		// Same cached map => stable pointer across calls.
		if e1 != e2 {
			t.Errorf("getByID(%q) pointer not stable across calls", id)
		}
		// Byte-equal to the Document row.
		want := docEntityByID(doc, id)
		if want == nil || !reflect.DeepEqual(*e1, *want) {
			t.Errorf("getByID(%q) != Document row\n got=%#v\nwant=%#v", id, e1, want)
		}
	}
}

func docEntityByID(doc *graph.Document, id string) *graph.Entity {
	for i := range doc.Entities {
		if doc.Entities[i].ID == id {
			return &doc.Entities[i]
		}
	}
	return nil
}

// TestLabelIndexSurfacesOverlayStampedFields_PR2 is the load-bearing invariant
// test for "values come from the overlaid Doc, NEVER the Reader".
//
// The other PR2 value-parity tests compare two DOC-sourced indexes over a
// fixture that stamps NO overlay-only fields, so they would pass even if at()
// were (wrongly) flipped to read the mmap Reader for values. This test closes
// that blind spot — the EXACT regression class that bit PR1.
//
// It stamps overlay-ONLY fields (CommunityID / Centrality / IsGodNode) onto
// Doc.Entities the way applyGroupAlgoOverlay does — those values live in the
// <group>-algo.json overlay, NOT in graph.fb, so the resident Reader carries
// only sentinels/zero for them — then asserts LabelIndex.ByID / Lookup / getByID
// SURFACE the stamped values. Mutation check (verified once, then reverted):
// flipping LabelIndex.at() to materialize from the Reader
// (graph.MaterializeEntity(l.reader, idx)) makes this test FAIL — ByID(id::a)
// surfaces the graph.fb sentinel CommunityID, not the stamped 80 — proving the
// Doc value-source is load-bearing.
func TestLabelIndexSurfacesOverlayStampedFields_PR2(t *testing.T) {
	t.Parallel()
	doc, r := loadParityIndexFixture(t)
	lr := &LoadedRepo{Repo: "repo", Doc: doc, Reader: r, LabelIndex: BuildLabelIndexFromReader(r, doc)}

	const wantCID = 80
	const wantCen = 0.4242
	stamp := func(id string, cid int, cen float64, god bool) {
		e := docEntityByID(doc, id)
		if e == nil {
			t.Fatalf("fixture missing entity %q", id)
		}
		c, ce := cid, cen
		e.CommunityID = &c
		e.Centrality = &ce
		e.IsGodNode = god
	}
	stamp("id::a", wantCID, wantCen, true)
	stamp("id::b", 7, 0.99, false)
	// Mirror the overlay's post-stamp index re-arm so getByID rebuilds off the
	// stamped Doc (LabelIndex reads Doc live, so it needs no rebuild).
	lr.resetIndexes()

	// LabelIndex.ByID surfaces the stamped overlay-only fields.
	a := lr.LabelIndex.ByID("id::a")
	if a == nil {
		t.Fatal("LabelIndex.ByID(id::a) = nil")
	}
	if a.CommunityID == nil || *a.CommunityID != wantCID {
		t.Fatalf("LabelIndex.ByID(id::a).CommunityID = %+v; want %d — overlay stamp not surfaced; "+
			"values MUST come from the overlaid Doc, not the Reader", a.CommunityID, wantCID)
	}
	if a.Centrality == nil || *a.Centrality != wantCen {
		t.Errorf("LabelIndex.ByID(id::a).Centrality = %+v; want %v (overlay stamp)", a.Centrality, wantCen)
	}
	if !a.IsGodNode {
		t.Error("LabelIndex.ByID(id::a).IsGodNode = false; want true (overlay stamp)")
	}

	// Lookup (by label) routes through the same at() materialization path.
	if l := lr.LabelIndex.Lookup("A"); l == nil || l.CommunityID == nil || *l.CommunityID != wantCID {
		t.Errorf("Lookup(A).CommunityID = %+v; want %d (overlay stamp)", l, wantCID)
	}

	// getByID surfaces the stamps too (map of copies built off the stamped Doc).
	if g := lr.getByID()["id::a"]; g == nil || g.CommunityID == nil || *g.CommunityID != wantCID {
		t.Errorf("getByID[id::a].CommunityID = %+v; want %d (overlay stamp)", g, wantCID)
	}

	// Sanity: the second stamped entity carries its own distinct value.
	if b := lr.LabelIndex.ByID("id::b"); b == nil || b.CommunityID == nil || *b.CommunityID != 7 {
		t.Errorf("LabelIndex.ByID(id::b).CommunityID = %+v; want 7", b)
	}
}
