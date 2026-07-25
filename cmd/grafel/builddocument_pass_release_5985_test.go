package main

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// Issue #5985 — buildDocument used to hold the three per-pass entity backing
// arrays alive for its whole duration, alongside the merged copy built from
// them. Both were live at the index peak (~165 MB of pass-slice spine +
// ~183 MB of `merged` on a large corpus).
//
// The hazard is a LIFETIME one, not a size one: the pass slices must stop being
// reachable from buildDocument's frame the moment their records have been
// appended into `merged`. These tests assert exactly that release, plus the
// invariant that releasing early changes nothing about what buildDocument
// computes.

// recordsFixture returns three distinguishable per-pass slices.
func passReleaseFixture() (p1, p2, p3 []types.EntityRecord) {
	p1 = []types.EntityRecord{
		{Kind: "Function", Name: "p1a", SourceFile: "a.go"},
		{Kind: "Function", Name: "p1b", SourceFile: "a.go"},
	}
	p2 = []types.EntityRecord{
		{Kind: "Class", Name: "p2a", SourceFile: "b.go"},
	}
	p3 = []types.EntityRecord{
		{Kind: "Class", Name: "p3a", SourceFile: "c.go"},
		{Kind: "Class", Name: "p3b", SourceFile: "c.go"},
		{Kind: "Class", Name: "p3c", SourceFile: "c.go"},
	}
	return p1, p2, p3
}

// TestMergePassRecordsReleasesEachSlice is the direct hazard assertion: the
// merge helper must nil out each caller-owned slice header as it consumes it,
// so the per-pass backing array becomes unreachable while `merged` is live.
// Asserted on the release itself (the slice headers), not on a memory proxy.
func TestMergePassRecordsReleasesEachSlice(t *testing.T) {
	p1, p2, p3 := passReleaseFixture()
	wantNames := []string{"p1a", "p1b", "p2a", "p3a", "p3b", "p3c"}

	merged := mergePassRecords(&p1, &p2, &p3)

	if p1 != nil {
		t.Errorf("pass1 slice not released: len=%d cap=%d (backing array still reachable from buildDocument's frame)", len(p1), cap(p1))
	}
	if p2 != nil {
		t.Errorf("pass2 slice not released: len=%d cap=%d", len(p2), cap(p2))
	}
	if p3 != nil {
		t.Errorf("pass3 slice not released: len=%d cap=%d", len(p3), cap(p3))
	}

	if len(merged) != len(wantNames) {
		t.Fatalf("merged len = %d, want %d", len(merged), len(wantNames))
	}
	for i, want := range wantNames {
		if merged[i].Name != want {
			t.Fatalf("merged[%d].Name = %q, want %q (append order regression: pass1, then pass2, then pass3)", i, merged[i].Name, want)
		}
	}
}

// TestMergePassRecordsIdenticalToPlainAppend guards against any content or
// ordering divergence from the pre-change `append(pass1...); append(pass2...);
// append(pass3...)` behaviour, including the no-dedup property (the helper must
// NOT drop anything — dedup happens later, keyed on stamped IDs).
func TestMergePassRecordsIdenticalToPlainAppend(t *testing.T) {
	p1, p2, p3 := passReleaseFixture()
	// Add an exact duplicate across passes: the merge step must keep both.
	p2 = append(p2, p1[0])

	want := make([]types.EntityRecord, 0, len(p1)+len(p2)+len(p3))
	want = append(want, p1...)
	want = append(want, p2...)
	want = append(want, p3...)

	got := mergePassRecords(&p1, &p2, &p3)

	if len(got) != len(want) {
		t.Fatalf("merged len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Kind != want[i].Kind || got[i].Name != want[i].Name || got[i].SourceFile != want[i].SourceFile {
			t.Fatalf("merged[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestMergePassRecordsNilPointers — the caller may legitimately pass an absent
// pass (the unit tests and the daemon single-stream path both do).
func TestMergePassRecordsNilPointers(t *testing.T) {
	p1, _, _ := passReleaseFixture()
	got := mergePassRecords(&p1, nil, nil)
	if len(got) != 2 {
		t.Fatalf("merged len = %d, want 2", len(got))
	}
	if p1 != nil {
		t.Errorf("pass1 slice not released with nil siblings")
	}
	if got := mergePassRecords(nil, nil, nil); len(got) != 0 {
		t.Fatalf("all-nil merge returned %d records, want 0", len(got))
	}
}

// TestBuildDocumentReleasesPassSlices drives the real buildDocument (the golden
// #5955 fixture, so the produced Document is hash-checked by
// TestBuildDocumentGolden5955) and asserts the caller's slice variable is nil
// once buildDocument returns — i.e. the release happens inside buildDocument
// rather than at cmd/grafel/index.go:1754.
func TestBuildDocumentReleasesPassSlices(t *testing.T) {
	pass1, pass2Rels := buildDocumentGoldenFixture()
	pass2 := []types.EntityRecord{{Kind: "Function", Name: "fromPass2", SourceFile: "p2.go"}}
	pass3 := []types.EntityRecord{{Kind: "Function", Name: "fromPass3", SourceFile: "p3.go"}}

	idx := &Indexer{repoTag: "test_repo"}
	doc := idx.buildDocument(&pass1, &pass2, pass2Rels, &pass3)

	if pass1 != nil {
		t.Errorf("pass1 still held after buildDocument returned (len=%d)", len(pass1))
	}
	if pass2 != nil {
		t.Errorf("pass2 still held after buildDocument returned (len=%d)", len(pass2))
	}
	if pass3 != nil {
		t.Errorf("pass3 still held after buildDocument returned (len=%d)", len(pass3))
	}

	// The pass2/pass3 records must still be in the output — releasing the
	// spine must not drop payload.
	var sawP2, sawP3 bool
	for k := range doc.Entities {
		switch doc.Entities[k].Name {
		case "fromPass2":
			sawP2 = true
		case "fromPass3":
			sawP3 = true
		}
	}
	if !sawP2 || !sawP3 {
		t.Fatalf("pass2/pass3 entities missing from Document: sawP2=%v sawP3=%v", sawP2, sawP3)
	}
}

// TestBuildDocumentIncrementalCarryForwardUnaffected — the incremental path
// makes a SECOND full copy of `merged` (index.go:4826) to seed the resolver
// index with carried-forward unchanged-file entities. Releasing the pass slices
// must not change that behaviour: the carry-forward entities are index-only
// (never appended to the Document) and the Document must be identical to the
// non-incremental run over the same passes.
func TestBuildDocumentIncrementalCarryForwardUnaffected(t *testing.T) {
	carry := []types.EntityRecord{{
		ID:         graph.EntityID("test_repo", "Class", "CarriedService", "unchanged/svc.ts"),
		Kind:       "Class",
		Name:       "CarriedService",
		SourceFile: "unchanged/svc.ts",
	}}

	// Baseline: no carry-forward.
	p1, r1 := buildDocumentGoldenFixture()
	base := (&Indexer{repoTag: "test_repo"}).buildDocument(&p1, nil, r1, nil)
	baseHash := canonicalDocHash(t, base)

	// Same passes, with carry-forward entities seeded.
	p2, r2 := buildDocumentGoldenFixture()
	inc := &Indexer{repoTag: "test_repo", incrementalCarryForwardEntities: carry}
	incDoc := inc.buildDocument(&p2, nil, r2, nil)

	if p2 != nil {
		t.Errorf("pass1 not released on the incremental path (len=%d)", len(p2))
	}
	for k := range incDoc.Entities {
		if incDoc.Entities[k].Name == "CarriedService" {
			t.Fatalf("carry-forward entity leaked into the Document — it is resolver-index-only (mergeIncrementalPrevDoc re-adds it, so emitting here double-emits)")
		}
	}
	if got := canonicalDocHash(t, incDoc); got != baseHash {
		t.Fatalf("carry-forward run diverged from baseline: %s != %s", got, baseHash)
	}
	// The carry-forward slice itself must survive — it is read again by the
	// resolver-index construction and is owned by the Indexer, not by the pass.
	if len(inc.incrementalCarryForwardEntities) != 1 {
		t.Fatalf("incrementalCarryForwardEntities clobbered: len=%d, want 1", len(inc.incrementalCarryForwardEntities))
	}
}
