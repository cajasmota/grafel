// view_materialize.go — ADR-0027 mmap-cutover (memory epic #5850), deretain-flip
// PR7a (#5870). Behavior-neutral read-seam migration prep.
//
// The tools in this package that need the WHOLE entity/relationship set as a
// slice — the heavy/rare analytical passes (import cycles, module algorithms,
// orientation, coverage, auth coverage, doc-semantics validation) — historically
// read the RAW resident slices lr.Doc.Entities / lr.Doc.Relationships directly.
// Those raw reads touch no mmap and have NO Reader fallback, so a LATER slice
// that empties lr.Doc (to stop holding the graph twice — once in mmap, once in
// heap) would silently make every one of them return EMPTY.
//
// This file adds the small transient-materialize + random-access + count seams
// those consumers route through instead, so the future slice-drop is invisible
// to them:
//
//   - materializeAllEntities / materializeAllRelationships copy the whole set out
//     via the SAME flag-gated forEach* path (Reader flag-ON, Doc flag-OFF), a
//     transient per-call slice replacing what used to be a PERMANENT heap alias.
//   - relationshipAt is the random-access companion to forEachRelationship for
//     the adjacency-relIdx property lookups (inspect / mro / effective-contract).
//   - entityCount / relCount source the count from the Reader flag-ON (so they
//     stay correct after lr.Doc is emptied) and the Doc slice length flag-OFF.
//
// Every path here reuses the existing rmu()/readRetired SIGBUS guard; none adds
// new locking discipline. All are byte-identical to the raw-slice reads they
// replace: flag-OFF they resolve to the same &lr.Doc.* rows; flag-ON they
// materialize the identical rows from the mmap Reader.
package mcp

import "github.com/cajasmota/grafel/internal/graph"

// materializeAllEntities copies EVERY entity of lr into a fresh transient slice,
// in vector-index order, via forEachEntityBase — the base entity source
// (Reader flag-ON, Doc flag-OFF) WITHOUT the cross-repo flow-overlay
// suppression/injection the public forEachEntity applies. Using the BASE source
// (not the public wrapper) is deliberate: it is byte-identical to the raw
// lr.Doc.Entities read this helper replaces, which never saw the flow overlay.
// Flag-ON, each yielded *graph.Entity is a heap-safe MaterializeEntity copy, so
// the returned slice is safe past the scan.
func materializeAllEntities(lr *LoadedRepo) []graph.Entity {
	if lr == nil {
		return nil
	}
	out := make([]graph.Entity, 0, lr.entityCount())
	lr.forEachEntityBase(func(e *graph.Entity) bool {
		out = append(out, *e)
		return true
	})
	return out
}

// materializeAllRelationships copies EVERY relationship of lr into a fresh
// transient slice, in vector-index order, via forEachRelationship (Reader
// flag-ON, Doc flag-OFF). forEachRelationship carries no overlay, so this is
// byte-identical to the raw lr.Doc.Relationships read it replaces.
func materializeAllRelationships(lr *LoadedRepo) []graph.Relationship {
	if lr == nil {
		return nil
	}
	out := make([]graph.Relationship, 0, lr.relCount())
	lr.forEachRelationship(func(r *graph.Relationship) bool {
		out = append(out, *r)
		return true
	})
	return out
}

// materializedDoc builds a TRANSIENT *graph.Document whose Entities and
// Relationships are the Reader-served materialize copies of lr's full set. It is
// the adapter for the analytical graph.* functions that take a *graph.Document
// but read ONLY doc.Entities/doc.Relationships (ComputeCoverage /
// ComputeEntityCoverage). Byte-identical to passing lr.Doc pre-flip; the future
// slice-drop is invisible because the slices come from the Reader flag-ON.
func materializedDoc(lr *LoadedRepo) *graph.Document {
	return &graph.Document{
		Entities:      materializeAllEntities(lr),
		Relationships: materializeAllRelationships(lr),
	}
}

// relationshipAt returns a *graph.Relationship for vector index idx, or nil when
// idx is synthetic (<0) or out of range. It is the random-access companion to
// forEachRelationship for the adjacency-relIdx property lookups.
//
// Flag-OFF (default): returns &lr.Doc.Relationships[idx] — the exact pointer
// semantics of the `rels := lr.Doc.Relationships; &rels[relIdx]` reads it
// replaces. Flag-ON: materializes the row from the mmap Reader under the SAME
// rmu()/readRetired guard forEachRelationship uses, falling back to the Doc when
// the mapping is retired/nil. The flag-ON return is a heap-safe copy.
func (lr *LoadedRepo) relationshipAt(idx int) *graph.Relationship {
	if lr == nil || idx < 0 {
		return nil
	}
	if !serveFromMMap() {
		return lr.docRelationshipAt(idx)
	}
	lr.rmu().Lock()
	rdr := lr.Reader
	h := lr.handle
	if rdr == nil || (h != nil && h.readRetired) {
		lr.rmu().Unlock()
		return lr.docRelationshipAt(idx)
	}
	defer lr.rmu().Unlock()
	if idx >= rdr.RelationshipCount() {
		return nil
	}
	rel := graph.MaterializeRelationship(rdr, idx)
	return &rel
}

// docRelationshipAt is the Doc-sourced path shared by relationshipAt's flag-OFF
// branch and its flag-ON retired/nil-Reader fallback. Callers must NOT hold
// readerMu (this touches only the Doc).
func (lr *LoadedRepo) docRelationshipAt(idx int) *graph.Relationship {
	if lr.Doc == nil || idx < 0 || idx >= len(lr.Doc.Relationships) {
		return nil
	}
	return &lr.Doc.Relationships[idx]
}

// entityCount returns the number of entities in lr, sourced from the mmap Reader
// when the flag-on read path is active (so it stays correct after a later slice
// empties lr.Doc.Entities) else the Doc slice length. Byte-identical to
// len(lr.Doc.Entities) pre-flip (the Reader mirrors the Doc entity set). Mirrors
// how TestsEdgeCount / the *FromReader builders pick their source.
func (lr *LoadedRepo) entityCount() int {
	if lr == nil {
		return 0
	}
	if serveFromMMap() {
		lr.rmu().Lock()
		rdr := lr.Reader
		h := lr.handle
		if rdr != nil && (h == nil || !h.readRetired) {
			n := rdr.EntityCount()
			lr.rmu().Unlock()
			return n
		}
		lr.rmu().Unlock()
	}
	if lr.Doc == nil {
		return 0
	}
	return len(lr.Doc.Entities)
}

// relCount is the relationship analogue of entityCount.
func (lr *LoadedRepo) relCount() int {
	if lr == nil {
		return 0
	}
	if serveFromMMap() {
		lr.rmu().Lock()
		rdr := lr.Reader
		h := lr.handle
		if rdr != nil && (h == nil || !h.readRetired) {
			n := rdr.RelationshipCount()
			lr.rmu().Unlock()
			return n
		}
		lr.rmu().Unlock()
	}
	if lr.Doc == nil {
		return 0
	}
	return len(lr.Doc.Relationships)
}
