package graph

import "sort"

// Issue #5974 — canonical emission order, shared by every producer.
//
// The full-index path (cmd/grafel) and the incremental-reindex path
// (internal/extractors) each used to carry their own copy of this sort. They
// drifted: the incremental copy ordered entities by
// (SourceFile, Kind, QualifiedName, Name, StartLine, ID) instead of by ID.
// That matters because the emitted FlatBuffers entity vector is searched with
// the generated `(key)` binary search (fbreader.Reader.LookupEntityByID), which
// has no linear fallback — a vector that is not sorted by ID makes lookups
// silently miss entities that are demonstrably present. One implementation,
// here, so the two paths cannot drift again.

// SortDocumentForEmission sorts doc in place into the canonical order every
// on-disk graph must be written in: entities by byte-wise ID (the FlatBuffers
// `(key)` order the reader binary-searches), relationships by
// (FromID, ToID, Kind, ID), and the Pass-4 outputs by their own stable keys.
//
// It is the final, post-everything sort applied immediately before the graph
// is serialized. Even with every fan-in already sorted, intermediate passes
// that mutate slices in place (external synthesis appends, Pass 4 reads via
// maps) deserve a defensive belt-and-braces sort. Sorting on the canonical
// graph IDs keeps diffs minimal and is idempotent, so callers may run it more
// than once.
func SortDocumentForEmission(doc *Document) {
	if doc == nil {
		return
	}
	// Entity IDs are unique, so ID alone is a total order — no secondary keys.
	sort.SliceStable(doc.Entities, func(i, j int) bool {
		return doc.Entities[i].ID < doc.Entities[j].ID
	})
	sort.SliceStable(doc.Relationships, func(i, j int) bool {
		a, b := &doc.Relationships[i], &doc.Relationships[j]
		if a.FromID != b.FromID {
			return a.FromID < b.FromID
		}
		if a.ToID != b.ToID {
			return a.ToID < b.ToID
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.ID < b.ID
	})
	// Pass 4 outputs — order by community ID (already deterministic from the
	// algorithms layer) then size as a tiebreaker, then top-entity name to
	// stabilise ties across re-runs.
	sort.SliceStable(doc.Communities, func(i, j int) bool {
		a, b := &doc.Communities[i], &doc.Communities[j]
		if a.Size != b.Size {
			return a.Size > b.Size
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		ai, bi := "", ""
		if len(a.TopEntities) > 0 {
			ai = a.TopEntities[0]
		}
		if len(b.TopEntities) > 0 {
			bi = b.TopEntities[0]
		}
		return ai < bi
	})
	sort.SliceStable(doc.SurpriseEdges, func(i, j int) bool {
		a, b := &doc.SurpriseEdges[i], &doc.SurpriseEdges[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.FromID != b.FromID {
			return a.FromID < b.FromID
		}
		return a.ToID < b.ToID
	})
}
