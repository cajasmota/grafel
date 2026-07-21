// view_iter.go — ADR-0027 mmap-cutover (memory epic #5850), Path P, PR1: the
// additive iterator primitive. forEachEntity/forEachRelationship are the
// range-func replacement for the ubiquitous
//
//	for i := range lr.Doc.Entities { e := &lr.Doc.Entities[i]; ... }
//
// (and its relationship equivalent) shape scattered across the handler
// package. This PR is BEHAVIOR-NEUTRAL and adds NO caller migrations: it only
// lands the two methods everything else will route through in a later PR.
// yield returning false stops iteration early (standard Go range-func
// convention, see golang.org/x/exp iter / the accepted rangefunc proposal).
package mcp

import (
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
)

// forEachEntity calls yield for every entity in lr, in index order. yield
// returning false stops iteration early.
//
// Flag-OFF (default, production): behavior-identical to today — iterates
// lr.Doc.Entities in order, yielding &lr.Doc.Entities[i]. No mmap is touched;
// byte-identical to the inline loops it replaces.
//
// Flag-ON (serveFromMMap() true): reuses the EXACT SIGBUS-safety pattern
// already shipped in LabelIndex.at() (index.go) and the merged readerMu
// substrate (ADR-0027, #5850/#5869):
//   - lr.readerMu is acquired around the ENTIRE mmap scan (not per-entity).
//     This is the accepted Option-B tradeoff: a concurrent reload's munmap
//     defers until the scan finishes, same property the adjacency builders
//     already have (getAdjacency/getCallsAdj/getStepAdj).
//   - If the mapping was retired (lr.handle.readRetired) or lr.Reader is nil,
//     falls back to the Doc path instead of dereferencing a freed mapping.
//   - Else iterates [0, lr.Reader.EntityCount()), materializing each row via
//     graph.MaterializeEntity + merging the group-algo overlay side-table
//     (lr.LabelIndex.overlay) exactly as LabelIndex.at() does, so overlay
//     fields (PageRank/CommunityID/Centrality/god/articulation) are
//     byte-identical to the flag-off/Doc-sourced result. MaterializeEntity
//     copies every string out of the mmap region, so the yielded *graph.Entity
//     is heap-safe past the scan.
//
// Flag-on transiently materializes EVERY entity (heap-safe copies) — fine for
// the rare/cold full-scan callers this primitive targets today. Hot
// per-query full-scanners should later be converted to INDEXED lookups (not
// forEach-all) in a subsequent PR; this PR does NOT migrate any callers.
func (lr *LoadedRepo) forEachEntity(yield func(*graph.Entity) bool) {
	if lr == nil {
		return
	}
	if !serveFromMMap() {
		lr.forEachDocEntity(yield)
		return
	}

	// Flag-ON: strictly-innermost readerMu held across the WHOLE scan (Option-B
	// tradeoff, intentional — see doc comment above and ADR-0027).
	lr.readerMu.Lock()
	rdr := lr.Reader
	h := lr.handle
	if rdr == nil || (h != nil && h.readRetired) {
		lr.readerMu.Unlock()
		lr.forEachDocEntity(yield)
		return
	}
	defer lr.readerMu.Unlock()

	var overlay map[int32]entityOverlay
	if lr.LabelIndex != nil {
		overlay = lr.LabelIndex.overlay
	}
	n := rdr.EntityCount()
	for i := 0; i < n; i++ {
		e := materializeEntityOverlay(rdr, overlay, int32(i))
		if !yield(e) {
			return
		}
	}
}

// forEachDocEntity is the Doc-sourced path shared by forEachEntity's flag-off
// branch and its flag-on retired/nil-Reader fallback.
func (lr *LoadedRepo) forEachDocEntity(yield func(*graph.Entity) bool) {
	if lr.Doc == nil {
		return
	}
	for i := range lr.Doc.Entities {
		if !yield(&lr.Doc.Entities[i]) {
			return
		}
	}
}

// materializeEntityOverlay decodes the i-th entity from the mmap Reader and
// merges the group-algo overlay side-table entry (if any), mirroring
// LabelIndex.materializeFromReader (index.go) exactly so a forEachEntity scan
// is byte-identical to a LabelIndex.at()-based lookup for the same index.
// Callers MUST hold the owning LoadedRepo's readerMu — the mmap dereference
// happens here.
func materializeEntityOverlay(r *fbreader.Reader, overlay map[int32]entityOverlay, idx int32) *graph.Entity {
	e := graph.MaterializeEntity(r, int(idx))
	if ov, ok := overlay[idx]; ok {
		e.CommunityID = ov.CommunityID
		e.PageRank = ov.PageRank
		e.Centrality = ov.Centrality
		e.IsGodNode = ov.IsGodNode
		e.IsArticulationPt = ov.IsArticulationPt
	}
	return &e
}

// forEachRelationship calls yield for every relationship in lr, in index
// order. yield returning false stops iteration early. Same flag-gated
// Doc-vs-Reader sourcing as forEachEntity, minus the overlay merge (the
// group-algo overlay side-table only ever carries entity fields — there is no
// relationship-side overlay).
func (lr *LoadedRepo) forEachRelationship(yield func(*graph.Relationship) bool) {
	if lr == nil {
		return
	}
	if !serveFromMMap() {
		lr.forEachDocRelationship(yield)
		return
	}

	lr.readerMu.Lock()
	rdr := lr.Reader
	h := lr.handle
	if rdr == nil || (h != nil && h.readRetired) {
		lr.readerMu.Unlock()
		lr.forEachDocRelationship(yield)
		return
	}
	defer lr.readerMu.Unlock()

	n := rdr.RelationshipCount()
	for i := 0; i < n; i++ {
		rel := graph.MaterializeRelationship(rdr, i)
		if !yield(&rel) {
			return
		}
	}
}

func (lr *LoadedRepo) forEachDocRelationship(yield func(*graph.Relationship) bool) {
	if lr.Doc == nil {
		return
	}
	for i := range lr.Doc.Relationships {
		if !yield(&lr.Doc.Relationships[i]) {
			return
		}
	}
}
