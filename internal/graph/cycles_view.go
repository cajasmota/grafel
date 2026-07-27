package graph

// cycles_view.go — mmap-backed import-cycle counting (#5954).
//
// FindImportCycles takes []Entity and []Relationship purely to derive two
// things: the set of entity ids that exist (so dangling edges are skipped) and
// the IMPORTS adjacency. Neither needs a materialised row. The daemon's
// post-rebuild analytics batch then reads a single integer — len(cycles) — off
// the result, so the whole graph was being copied onto the heap to count
// strongly-connected components.
//
// CountImportCyclesFromView builds the minimal structures the algorithm
// actually needs (an id set and an id→id adjacency map) directly from the mmap
// and runs the SAME Tarjan core through the SAME edge filter, so the count is
// identical by construction.

import (
	fb "github.com/cajasmota/grafel/internal/graph/fbgraph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
)

// CountImportCyclesFromView returns len(FindImportCycles(...)) for the graph
// behind v, without materialising a Document.
//
// pagerank is not accepted because the count is independent of it: pagerank
// only annotates each cycle with a weakest link and an extraction candidate,
// never adds or removes one. (The daemon's caller always passed nil anyway.)
func CountImportCyclesFromView(v fbreader.GraphView) int {
	if v == nil {
		return 0
	}

	// Pass 1: the id set. This is the ONLY per-entity state the cycle finder
	// needs — no names, kinds, files or properties.
	known := make(map[string]bool, v.EntityCount())
	v.IterateEntities(func(e *fb.Entity) bool {
		if e != nil {
			known[string(e.Id())] = true
		}
		return true
	})

	// Pass 2: the IMPORTS adjacency, id→id only.
	importsAdj := make(map[string][]string)
	v.IterateRelationships(func(rel *fb.Relationship) bool {
		if rel == nil {
			return true
		}
		addImportEdge(importsAdj, known, string(rel.Kind()), string(rel.FromId()), string(rel.ToId()))
		return true
	})

	return len(findImportCyclesFromAdjacency(importsAdj, nil))
}
