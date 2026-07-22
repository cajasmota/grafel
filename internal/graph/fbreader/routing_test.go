package fbreader_test

import (
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// twoRangeSegments writes two disjoint, key-sorted entity segments:
//
//	seg0: entities a, b   (key range [a,b])
//	seg1: entities m, n   (key range [m,n])
//
// and returns their paths plus the parallel fbreader.KeyRange slice a manifest would
// carry.
func twoRangeSegments(t *testing.T) ([]string, []fbreader.KeyRange) {
	t.Helper()
	dir := t.TempDir()
	seg0 := &graph.Document{Repo: "r", Entities: []graph.Entity{
		{ID: "a", QualifiedName: "p.A", Kind: "function", Name: "A"},
		{ID: "b", QualifiedName: "p.B", Kind: "struct", Name: "B"},
	}}
	seg1 := &graph.Document{Repo: "r", Entities: []graph.Entity{
		{ID: "m", QualifiedName: "p.M", Kind: "function", Name: "M"},
		{ID: "n", QualifiedName: "p.N", Kind: "struct", Name: "N"},
	}}
	p0 := filepath.Join(dir, "seg-0000.fb")
	p1 := filepath.Join(dir, "seg-0001.fb")
	if err := fbwriter.WriteAtomic(p0, seg0); err != nil {
		t.Fatal(err)
	}
	if err := fbwriter.WriteAtomic(p1, seg1); err != nil {
		t.Fatal(err)
	}
	ranges := []fbreader.KeyRange{
		{HasEntities: true, Min: "a", Max: "b"},
		{HasEntities: true, Min: "m", Max: "n"},
	}
	return []string{p0, p1}, ranges
}

// TestMultiReader_KeyRoutingSkipsOutOfRangeSegment: a lookup for a key that
// falls only in seg0's range must NOT search seg1 (decision 4). We assert the
// pruning via fbreader.SegmentSearchHook, which records every segment actually searched.
func TestMultiReader_KeyRoutingSkipsOutOfRangeSegment(t *testing.T) {
	paths, ranges := twoRangeSegments(t)
	mr, err := fbreader.OpenSegmentsWithRanges(paths, ranges)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mr.Close() })

	var searched []int
	fbreader.SegmentSearchHook = func(seg int) { searched = append(searched, seg) }
	t.Cleanup(func() { fbreader.SegmentSearchHook = nil })

	// "a" lives in seg0 only → seg1 must be pruned.
	searched = nil
	if e := mr.LookupEntityByID("a"); e == nil || string(e.Id()) != "a" {
		t.Fatalf("lookup a: got %v", e)
	}
	for _, s := range searched {
		if s == 1 {
			t.Fatalf("seg1 was searched for key 'a' despite being out of range; searched=%v", searched)
		}
	}
	if len(searched) != 1 || searched[0] != 0 {
		t.Fatalf("expected only seg0 searched for 'a', got %v", searched)
	}

	// "n" lives in seg1 only → seg0 must be pruned.
	searched = nil
	if e := mr.LookupEntityByID("n"); e == nil || string(e.Id()) != "n" {
		t.Fatalf("lookup n: got %v", e)
	}
	if len(searched) != 1 || searched[0] != 1 {
		t.Fatalf("expected only seg1 searched for 'n', got %v", searched)
	}

	// A key absent from every range searches NOTHING and returns nil.
	searched = nil
	if e := mr.LookupEntityByID("zzz"); e != nil {
		t.Fatalf("lookup zzz: expected nil, got %v", e)
	}
	if len(searched) != 0 {
		t.Fatalf("out-of-all-range key should search no segment, got %v", searched)
	}
}

// TestMultiReader_NoRangesSearchesAll: OpenSegments (no ranges) preserves the
// legacy fan-out — every segment is searched until a hit.
func TestMultiReader_NoRangesSearchesAll(t *testing.T) {
	paths, _ := twoRangeSegments(t)
	mr, err := fbreader.OpenSegments(paths)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mr.Close() })

	var searched []int
	fbreader.SegmentSearchHook = func(seg int) { searched = append(searched, seg) }
	t.Cleanup(func() { fbreader.SegmentSearchHook = nil })

	// "n" is in seg1; with no routing, seg0 is searched first (miss) then seg1.
	if e := mr.LookupEntityByID("n"); e == nil {
		t.Fatal("lookup n: not found")
	}
	if len(searched) != 2 || searched[0] != 0 || searched[1] != 1 {
		t.Fatalf("no-range lookup should fan out across all segments, got %v", searched)
	}
}

// TestMultiReader_PureRelationshipSegmentSkipped: a segment whose KeyRange has
// HasEntities=false (a pure relationship stream) is never searched for an
// entity key, while an entity segment with unknown bounds is always searched.
func TestMultiReader_PureRelationshipSegmentSkipped(t *testing.T) {
	paths, _ := twoRangeSegments(t)
	// seg0 declared as a pure-relationship segment (HasEntities=false) even
	// though it physically holds entities — routing must trust the manifest and
	// skip it; seg1 declared with unknown bounds (HasEntities, no Min/Max) so it
	// is always searched.
	ranges := []fbreader.KeyRange{
		{HasEntities: false},
		{HasEntities: true},
	}
	mr, err := fbreader.OpenSegmentsWithRanges(paths, ranges)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mr.Close() })

	var searched []int
	fbreader.SegmentSearchHook = func(seg int) { searched = append(searched, seg) }
	t.Cleanup(func() { fbreader.SegmentSearchHook = nil })

	// "a" physically lives in seg0, but seg0 is flagged pure-relationship, so it
	// is skipped and the lookup misses (n is only in seg1, unknown-bounds).
	_ = mr.LookupEntityByID("a")
	for _, s := range searched {
		if s == 0 {
			t.Fatalf("pure-relationship seg0 must never be searched, got %v", searched)
		}
	}
	if len(searched) != 1 || searched[0] != 1 {
		t.Fatalf("unknown-bounds seg1 must always be searched, got %v", searched)
	}
}
