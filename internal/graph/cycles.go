// Package graph — cycles.go implements Tarjan's strongly-connected-components
// (SCC) algorithm over IMPORTS edges between entities. Each SCC of size > 1
// represents a circular dependency (import cycle).
//
// Beyond raw detection, each cycle is annotated with:
//   - WeakestLinkID: the edge whose source has the lowest PageRank —
//     severing it causes the least disruption to the rest of the graph.
//   - SuggestedExtraction: the entity with the highest PageRank inside the
//     cycle (best candidate to be extracted into a shared module that both
//     sides can depend on without the cycle).
package graph

import (
	"sort"
)

// ImportCycle describes one strongly-connected component of size > 1 in the
// IMPORTS sub-graph.
type ImportCycle struct {
	// Members lists entity IDs that form the cycle, sorted for determinism.
	Members []string `json:"members"`
	// Edges lists the IMPORTS relationships (FromID → ToID) inside the cycle,
	// sorted for determinism.
	Edges []CycleEdge `json:"edges"`
	// WeakestLinkFromID / WeakestLinkToID identify the edge whose source has
	// the lowest PageRank — the best edge to sever to break the cycle cheaply.
	WeakestLinkFromID string `json:"weakest_link_from_id"`
	WeakestLinkToID   string `json:"weakest_link_to_id"`
	// SuggestedExtractionID is the entity inside the cycle with the highest
	// PageRank — the best candidate for extraction into a common module.
	SuggestedExtractionID string `json:"suggested_extraction_id"`
	// Size is len(Members) for fast filtering.
	Size int `json:"size"`
}

// CycleEdge is a directed IMPORTS relationship within a cycle.
type CycleEdge struct {
	FromID string `json:"from_id"`
	ToID   string `json:"to_id"`
}

// FindImportCycles runs Tarjan SCC on the IMPORTS sub-graph of rels, using
// pagerank scores to annotate each cycle with a weakest link and suggested
// extraction target.
//
// pagerank may be nil or empty — in that case all nodes are treated as
// equally weighted (weakest-link = first alphabetical source).
//
// Returns cycles sorted by descending size, then ascending first member ID.
func FindImportCycles(entities []Entity, rels []Relationship, pagerank map[string]float64) []ImportCycle {
	// Collect entity IDs that actually exist so we skip dangling edges.
	known := make(map[string]bool, len(entities))
	for i := range entities {
		known[entities[i].ID] = true
	}

	// Build adjacency list of IMPORTS edges (directed: from → to).
	// importsAdj[u] = list of v where u IMPORTS v.
	importsAdj := make(map[string][]string)
	// importsEdge is a lookup set for edges inside cycles.
	type edgeKey struct{ from, to string }
	edgeSet := make(map[edgeKey]bool)

	for i := range rels {
		r := &rels[i]
		if r.Kind != "IMPORTS" {
			continue
		}
		if !known[r.FromID] || !known[r.ToID] {
			continue
		}
		if r.FromID == r.ToID {
			continue // self-import: not a cycle
		}
		importsAdj[r.FromID] = append(importsAdj[r.FromID], r.ToID)
		edgeSet[edgeKey{r.FromID, r.ToID}] = true
	}

	if len(importsAdj) == 0 {
		return nil
	}

	// Ensure deterministic iteration by sorting adjacency lists.
	for id := range importsAdj {
		sort.Strings(importsAdj[id])
	}

	// --- Tarjan's iterative SCC ---
	type nodeState struct {
		index   int
		lowlink int
		onStack bool
	}
	state := make(map[string]*nodeState, len(importsAdj))
	var stack []string
	index := 0
	var sccs [][]string

	// Iterative DFS to avoid stack overflows on deep graphs.
	type frame struct {
		u    string
		i    int // next child index in importsAdj[u]
	}

	// Collect all nodes (sources + any targets that are also sources).
	allNodes := make(map[string]bool)
	for u, vs := range importsAdj {
		allNodes[u] = true
		for _, v := range vs {
			allNodes[v] = true
		}
	}
	// Sort for determinism.
	sortedNodes := make([]string, 0, len(allNodes))
	for n := range allNodes {
		sortedNodes = append(sortedNodes, n)
	}
	sort.Strings(sortedNodes)

	var dfsStack []frame
	for _, start := range sortedNodes {
		if state[start] != nil {
			continue
		}
		state[start] = &nodeState{index: index, lowlink: index}
		index++
		stack = append(stack, start)
		state[start].onStack = true
		dfsStack = append(dfsStack, frame{u: start, i: 0})

		for len(dfsStack) > 0 {
			top := &dfsStack[len(dfsStack)-1]
			u := top.u
			neighbours := importsAdj[u]

			if top.i < len(neighbours) {
				v := neighbours[top.i]
				top.i++
				if sv := state[v]; sv == nil {
					// Tree edge: push v.
					state[v] = &nodeState{index: index, lowlink: index}
					index++
					stack = append(stack, v)
					state[v].onStack = true
					dfsStack = append(dfsStack, frame{u: v, i: 0})
				} else if sv.onStack {
					// Back edge.
					if sv.index < state[u].lowlink {
						state[u].lowlink = sv.index
					}
				}
				continue
			}

			// All neighbours processed — pop u.
			dfsStack = dfsStack[:len(dfsStack)-1]
			su := state[u]

			if len(dfsStack) > 0 {
				parent := dfsStack[len(dfsStack)-1].u
				sp := state[parent]
				if su.lowlink < sp.lowlink {
					sp.lowlink = su.lowlink
				}
			}

			// Root of an SCC?
			if su.lowlink == su.index {
				var scc []string
				for {
					w := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					state[w].onStack = false
					scc = append(scc, w)
					if w == u {
						break
					}
				}
				sccs = append(sccs, scc)
			}
		}
	}

	// Build ImportCycle for each SCC of size > 1.
	var cycles []ImportCycle
	for _, scc := range sccs {
		if len(scc) < 2 {
			continue
		}
		memberSet := make(map[string]bool, len(scc))
		for _, m := range scc {
			memberSet[m] = true
		}
		// Collect internal edges.
		var edges []CycleEdge
		for _, u := range scc {
			for _, v := range importsAdj[u] {
				if memberSet[v] {
					edges = append(edges, CycleEdge{FromID: u, ToID: v})
				}
			}
		}
		// Sort members and edges for determinism.
		sort.Strings(scc)
		sort.Slice(edges, func(i, j int) bool {
			if edges[i].FromID != edges[j].FromID {
				return edges[i].FromID < edges[j].FromID
			}
			return edges[i].ToID < edges[j].ToID
		})

		// Weakest link: edge whose source has the lowest PageRank.
		// If PageRank is unavailable, fall back to alphabetically first source.
		weakFrom, weakTo := pickWeakestLink(edges, pagerank)

		// Suggested extraction: member with the highest PageRank.
		bestExtraction := pickBestExtraction(scc, pagerank)

		cycles = append(cycles, ImportCycle{
			Members:               scc,
			Edges:                 edges,
			WeakestLinkFromID:     weakFrom,
			WeakestLinkToID:       weakTo,
			SuggestedExtractionID: bestExtraction,
			Size:                  len(scc),
		})
	}

	// Sort: descending size, then ascending first member for determinism.
	sort.SliceStable(cycles, func(i, j int) bool {
		if cycles[i].Size != cycles[j].Size {
			return cycles[i].Size > cycles[j].Size
		}
		mi := ""
		if len(cycles[i].Members) > 0 {
			mi = cycles[i].Members[0]
		}
		mj := ""
		if len(cycles[j].Members) > 0 {
			mj = cycles[j].Members[0]
		}
		return mi < mj
	})
	return cycles
}

// pickWeakestLink returns the edge whose source has the lowest PageRank.
// Tie-breaks on source ID alphabetically to stay deterministic.
func pickWeakestLink(edges []CycleEdge, pr map[string]float64) (fromID, toID string) {
	if len(edges) == 0 {
		return "", ""
	}
	best := edges[0]
	bestPR := prScore(best.FromID, pr)
	for _, e := range edges[1:] {
		p := prScore(e.FromID, pr)
		if p < bestPR || (p == bestPR && e.FromID < best.FromID) {
			best = e
			bestPR = p
		}
	}
	return best.FromID, best.ToID
}

// pickBestExtraction returns the member with the highest PageRank (best
// candidate for extraction into a shared module). Tie-breaks alphabetically.
func pickBestExtraction(members []string, pr map[string]float64) string {
	if len(members) == 0 {
		return ""
	}
	best := members[0]
	bestPR := prScore(best, pr)
	for _, m := range members[1:] {
		p := prScore(m, pr)
		if p > bestPR || (p == bestPR && m < best) {
			best = m
			bestPR = p
		}
	}
	return best
}

// prScore returns the PageRank score for id, or 0.0 if missing.
func prScore(id string, pr map[string]float64) float64 {
	if pr == nil {
		return 0
	}
	return pr[id]
}
