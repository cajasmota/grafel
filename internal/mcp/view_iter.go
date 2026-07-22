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
	"github.com/cajasmota/grafel/internal/graph/flows"
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
	// Flow side-table (#5904 PR-b): REPLACE. When a fresh cross-repo flow overlay
	// is present, SUPPRESS baked SCOPE.Process/SCOPE.EventFlow entities from the
	// base source (flowFilteredYield) and INJECT the sidecar's cross-repo-aware
	// flow entities after the base scan (injectFlowEntities). fo==nil → unchanged.
	fo := lr.flowOverlaySnapshot()
	if !lr.forEachEntityBase(flowFilteredYield(fo, yield)) {
		return // base scan stopped early; do not inject
	}
	injectFlowEntities(fo, nil, yield)
}

// forEachEntityBase is the base entity source (Doc or mmap Reader) for
// forEachEntity — the pre-#5904-PR-b body. It returns false iff yield stopped
// the scan early (so the caller skips flow injection).
func (lr *LoadedRepo) forEachEntityBase(yield func(*graph.Entity) bool) bool {
	if !serveFromMMap() {
		return lr.forEachDocEntity(yield)
	}

	// Flag-ON: strictly-innermost readerMu held across the WHOLE scan (Option-B
	// tradeoff, intentional — see doc comment above and ADR-0027).
	lr.rmu().Lock()
	rdr := lr.Reader
	h := lr.handle
	if rdr == nil || (h != nil && h.readRetired) {
		lr.rmu().Unlock()
		return lr.forEachDocEntity(yield)
	}
	defer lr.rmu().Unlock()

	var overlay map[int32]entityOverlay
	var descOverlay map[int32]string
	if lr.LabelIndex != nil {
		overlay = lr.LabelIndex.overlay
		descOverlay = lr.LabelIndex.descOverlay
	}
	n := rdr.EntityCount()
	for i := 0; i < n; i++ {
		e := materializeEntityOverlay(rdr, overlay, descOverlay, int32(i))
		if !yield(e) {
			return false
		}
	}
	return true
}

// flowFilteredYield wraps yield so that, when a flow overlay is active, baked
// flow entities (SCOPE.Process / SCOPE.EventFlow) are SKIPPED from the base
// source — they are substituted by injectFlowEntities (REPLACE). fo==nil returns
// yield unchanged (zero overhead on the no-overlay path).
func flowFilteredYield(fo *flows.Sidecar, yield func(*graph.Entity) bool) func(*graph.Entity) bool {
	if fo == nil {
		return yield
	}
	return func(e *graph.Entity) bool {
		if flows.IsFlowEntityKind(e.Kind) {
			return true // suppress baked flow entity; substituted below
		}
		return yield(e)
	}
}

// injectFlowEntities yields each overlay flow entity whose kind satisfies pred
// (nil pred = every overlay entity). Returns false if yield stopped early. A nil
// overlay is a no-op.
func injectFlowEntities(fo *flows.Sidecar, pred func(kind string) bool, yield func(*graph.Entity) bool) bool {
	if fo == nil {
		return true
	}
	for i := range fo.Entities {
		e := &fo.Entities[i]
		if pred != nil && !pred(e.Kind) {
			continue
		}
		if !yield(e) {
			return false
		}
	}
	return true
}

// forEachEntityOfKinds is the by-Kind analogue of forEachEntity (memory epic
// #5850 / mmap-flip #5870): it calls yield only for the entities whose Kind
// satisfies pred, in vector-index order. yield returning false stops iteration
// early. It is the seam the hot Kind-predicate scanners (endpoint / flow /
// dashboard tools) route through instead of forEachEntity + an in-loop Kind
// filter, so — flag-ON — they materialize ONLY the predicate-matching entities
// (dozens-of-kinds → matching subset) rather than the whole 427k-entity set.
//
// ORDER is byte-identical to forEachEntity + `if !pred(e.Kind) { skip }`: the
// visited indices come from LabelIndex.indicesForKinds, which returns the union
// of the matching kinds' index lists sorted ASCENDING — the same order the entity
// vector (and therefore forEachEntity) uses.
//
// Sourcing mirrors forEachEntity exactly:
//   - No by-Kind index available (LabelIndex or byKind nil — directly-constructed
//     test indexes / JSON-only fallback): degrade to forEachEntity + an in-loop
//     pred filter. Output-identical; simply NOT selectively-materializing.
//   - Flag-OFF (default): yield &lr.Doc.Entities[idx] for each matching index —
//     the same pointer semantics as forEachEntity's flag-off path.
//   - Flag-ON: lr.readerMu held across the WHOLE scan (Option-B), retired/nil
//     Reader falls back to the Doc, else materialize each matching index via the
//     SAME graph.MaterializeEntity + overlay merge forEachEntity uses.
func (lr *LoadedRepo) forEachEntityOfKinds(pred func(kind string) bool, yield func(*graph.Entity) bool) {
	if lr == nil || pred == nil {
		return
	}
	// Flow side-table (#5904 PR-b): REPLACE. Suppress baked flow entities from the
	// base by-Kind scan (only relevant when pred matches a flow kind) and inject
	// the sidecar's cross-repo-aware flow entities that also satisfy pred.
	fo := lr.flowOverlaySnapshot()
	if !lr.forEachEntityOfKindsBase(pred, flowFilteredYield(fo, yield)) {
		return
	}
	injectFlowEntities(fo, pred, yield)
}

// forEachEntityOfKindsBase is the base by-Kind entity source (the pre-#5904-PR-b
// body). Returns false iff yield stopped the scan early.
func (lr *LoadedRepo) forEachEntityOfKindsBase(pred func(kind string) bool, yield func(*graph.Entity) bool) bool {
	li := lr.LabelIndex
	if li == nil || li.byKind == nil {
		// No by-Kind index — preserve output via a filtered full scan (no
		// selective materialization on this fallback). forEachEntityBase (not the
		// public forEachEntity) so flow suppression/injection is applied exactly
		// once, by the public forEachEntityOfKinds wrapper.
		return lr.forEachEntityBase(func(e *graph.Entity) bool {
			if !pred(e.Kind) {
				return true
			}
			return yield(e)
		})
	}
	idxs := li.indicesForKinds(pred)
	if len(idxs) == 0 {
		return true
	}

	if !serveFromMMap() {
		// Flag-OFF: Doc-sourced, same &Doc.Entities[idx] pointer semantics as
		// forEachEntity's flag-off branch.
		if lr.Doc == nil {
			return true
		}
		for _, idx := range idxs {
			if int(idx) >= len(lr.Doc.Entities) {
				continue
			}
			if !yield(&lr.Doc.Entities[idx]) {
				return false
			}
		}
		return true
	}

	// Flag-ON: strictly-innermost readerMu held across the WHOLE scan (Option-B
	// tradeoff, identical to forEachEntity).
	lr.rmu().Lock()
	rdr := lr.Reader
	h := lr.handle
	if rdr == nil || (h != nil && h.readRetired) {
		lr.rmu().Unlock()
		return lr.forEachDocEntityOfIdxs(idxs, yield)
	}
	defer lr.rmu().Unlock()

	var overlay map[int32]entityOverlay
	var descOverlay map[int32]string
	if lr.LabelIndex != nil {
		overlay = lr.LabelIndex.overlay
		descOverlay = lr.LabelIndex.descOverlay
	}
	n := rdr.EntityCount()
	for _, idx := range idxs {
		if int(idx) >= n {
			continue
		}
		e := materializeEntityOverlay(rdr, overlay, descOverlay, idx)
		if !yield(e) {
			return false
		}
	}
	return true
}

// forEachDocEntityOfIdxs yields &lr.Doc.Entities[idx] for each index in idxs (in
// the given order), the Doc-sourced path shared by forEachEntityOfKinds's flag-ON
// retired/nil-Reader fallback. Callers must NOT hold readerMu (this touches only
// the Doc).
func (lr *LoadedRepo) forEachDocEntityOfIdxs(idxs []int32, yield func(*graph.Entity) bool) bool {
	if lr.Doc == nil {
		return true
	}
	for _, idx := range idxs {
		if int(idx) >= len(lr.Doc.Entities) {
			continue
		}
		if !yield(&lr.Doc.Entities[idx]) {
			return false
		}
	}
	return true
}

// forEachDocEntity is the Doc-sourced path shared by forEachEntity's flag-off
// branch and its flag-on retired/nil-Reader fallback. Returns false iff yield
// stopped the scan early.
func (lr *LoadedRepo) forEachDocEntity(yield func(*graph.Entity) bool) bool {
	if lr.Doc == nil {
		return true
	}
	for i := range lr.Doc.Entities {
		if !yield(&lr.Doc.Entities[i]) {
			return false
		}
	}
	return true
}

// materializeEntityOverlay decodes the i-th entity from the mmap Reader and
// merges the group-algo overlay side-table entry (if any), mirroring
// LabelIndex.materializeFromReader (index.go) exactly so a forEachEntity scan
// is byte-identical to a LabelIndex.at()-based lookup for the same index.
// Callers MUST hold the owning LoadedRepo's readerMu — the mmap dereference
// happens here.
func materializeEntityOverlay(r *fbreader.Reader, overlay map[int32]entityOverlay, descOverlay map[int32]string, idx int32) *graph.Entity {
	// Test-only observability seam (memory epic #5850 Path P): count each mmap
	// entity materialization so the selective-materialization tests can assert a
	// forEachEntityOfKinds scan materializes ONLY its matching-Kind subset, not the
	// whole set. Nil in production — one predictable nil-check, shared with
	// LabelIndex.materializeFromReader.
	if atMaterializeHook != nil {
		atMaterializeHook()
	}
	e := graph.MaterializeEntity(r, int(idx))
	if ov, ok := overlay[idx]; ok {
		e.CommunityID = ov.CommunityID
		e.PageRank = ov.PageRank
		e.Centrality = ov.Centrality
		e.IsGodNode = ov.IsGodNode
		e.IsArticulationPt = ov.IsArticulationPt
	}
	// Description side-table (#5904 PR-a): ADDITIVE merge, mirroring
	// LabelIndex.materializeFromReader — a miss leaves the base description intact.
	if d, ok := descOverlay[idx]; ok {
		e.PropSet("description", d)
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

	lr.rmu().Lock()
	rdr := lr.Reader
	h := lr.handle
	if rdr == nil || (h != nil && h.readRetired) {
		lr.rmu().Unlock()
		lr.forEachDocRelationship(yield)
		return
	}
	defer lr.rmu().Unlock()

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
