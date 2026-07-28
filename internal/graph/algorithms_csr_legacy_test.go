package graph

import (
	"sort"

	"gonum.org/v1/gonum/graph/simple"
)

// legacyBuildGraph is a VERBATIM copy of BuildGraph's edge loop as it stood
// immediately before #5954 S5/S6: walk the relationship slice, apply the
// admission rules inline, and accumulate parallel edges into the gonum graph
// with a lookup/remove/re-add chain.
//
// It is the independent oracle for directedCSR. Production no longer derives
// the gonum edge set and the CSR separately — the CSR is built first and g is
// materialised from it — so without this copy the "CSR matches gonum" claim
// would be checking a representation against itself.
//
// Do NOT tidy this function. Its entire value is that it is unchanged.
func legacyBuildGraph(entities []Entity, rels []Relationship) (*simple.WeightedDirectedGraph, *nodeIndex) {
	g := simple.NewWeightedDirectedGraph(0, 0)
	idx := newNodeIndex()

	for _, e := range entities {
		nid := idx.get(e.ID)
		if g.Node(nid) == nil {
			g.AddNode(simple.Node(nid))
		}
	}

	for _, r := range rels {
		if r.FromID == "" || r.ToID == "" {
			continue
		}
		if _, ok := idx.toInt[r.FromID]; !ok {
			continue
		}
		if _, ok := idx.toInt[r.ToID]; !ok {
			continue
		}
		from := idx.get(r.FromID)
		to := idx.get(r.ToID)
		if from == to {
			continue // gonum rejects self-loops on simple graphs
		}
		w := edgeWeight(r.PropsSnapshot())
		if existing := g.WeightedEdge(from, to); existing != nil {
			w += existing.Weight()
			g.RemoveEdge(from, to)
		}
		g.SetWeightedEdge(g.NewWeightedEdge(simple.Node(from), simple.Node(to), w))
	}
	return g, idx
}

// legacyComputeCommunities is a VERBATIM copy of ComputeCommunities as it stood
// immediately before #5954 S5/S6, kept as the equivalence oracle for the
// shared-CSR rewrite. It builds its own simple.WeightedUndirectedGraph from the
// directed gonum graph and walks it with gonum's interface-returning edge
// iterators, exactly as production used to.
//
// Do NOT tidy this function. Its entire value is that it is unchanged.
func legacyComputeCommunities(g *simple.WeightedDirectedGraph, idx *nodeIndex, entityNames []string, opts CommunityOptions) ([]CommunityResult, map[string]int, float64, int) {
	und := legacyUndirectedProjection(g)

	const resolution = 1.0
	groups := louvainPartition(und, resolution)

	type nodeStat struct {
		k      float64
		cidIdx int // index into groups
		degree int
	}
	nodeStats := make(map[int64]*nodeStat, idx.next)
	for cid, gg := range groups {
		for _, nid := range gg {
			if _, ok := nodeStats[nid]; !ok {
				nodeStats[nid] = &nodeStat{cidIdx: cid}
			} else {
				nodeStats[nid].cidIdx = cid
			}
		}
	}
	internalW := make([]float64, len(groups))
	var m2 float64
	wedges := und.WeightedEdges()
	for wedges.Next() {
		e := wedges.WeightedEdge()
		w := e.Weight()
		fid, tid := e.From().ID(), e.To().ID()
		nf, ok := nodeStats[fid]
		if !ok {
			nf = &nodeStat{cidIdx: -1}
			nodeStats[fid] = nf
		}
		nt, ok := nodeStats[tid]
		if !ok {
			nt = &nodeStat{cidIdx: -1}
			nodeStats[tid] = nt
		}
		nf.k += w
		nt.k += w
		nf.degree++
		nt.degree++
		m2 += 2 * w // undirected: each edge contributes 2 to Σ k.
		if nf.cidIdx >= 0 && nf.cidIdx == nt.cidIdx {
			internalW[nf.cidIdx] += w
		}
	}

	K := make([]float64, len(groups))
	for cid, gg := range groups {
		var k float64
		for _, nid := range gg {
			if ns, ok := nodeStats[nid]; ok {
				k += ns.k
			}
		}
		K[cid] = k
	}

	var overallQRaw float64
	communityQ := make([]float64, len(groups))
	if m2 > 0 {
		for cid := range groups {
			q := (2*internalW[cid] - resolution*K[cid]*K[cid]/m2) / m2
			communityQ[cid] = q
			overallQRaw += q
		}
	}
	overallQ := roundForDeterminism(sanitizeFloat(overallQRaw))

	communityOf := make(map[string]int, idx.next)
	for sid := range idx.toInt {
		communityOf[sid] = -1
	}
	results := make([]CommunityResult, 0, len(groups))

	for cid, g := range groups {
		type member struct {
			id     int64
			degree int
		}
		members := make([]member, 0, len(g))
		for _, nid := range g {
			communityOf[idx.fromInt[nid]] = cid
			deg := 0
			if ns, ok := nodeStats[nid]; ok {
				deg = ns.degree
			}
			members = append(members, member{nid, deg})
		}
		sort.SliceStable(members, func(i, j int) bool {
			if members[i].degree != members[j].degree {
				return members[i].degree > members[j].degree
			}
			return members[i].id < members[j].id
		})

		topN := 5
		if topN > len(members) {
			topN = len(members)
		}
		top := make([]string, 0, topN)
		for k := 0; k < topN; k++ {
			eid := idx.fromInt[members[k].id]
			name := nameOf(entityNames, members[k].id)
			if name == "" {
				name = eid
			}
			top = append(top, name)
		}

		cQ := roundForDeterminism(sanitizeFloat(communityQ[cid]))

		results = append(results, CommunityResult{
			ID:          cid,
			Size:        len(g),
			Modularity:  cQ,
			TopEntities: top,
		})
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Size != results[j].Size {
			return results[i].Size > results[j].Size
		}
		return results[i].ID < results[j].ID
	})

	minSize := opts.MinSize
	if minSize < 1 {
		minSize = 1
	}
	var denoised int
	if minSize > 1 {
		kept := results[:0]
		for _, r := range results {
			if r.Size >= minSize {
				kept = append(kept, r)
			} else {
				denoised++
				for eid, cid := range communityOf {
					if cid == r.ID {
						communityOf[eid] = -1
					}
				}
			}
		}
		results = kept
	}

	return results, communityOf, overallQ, denoised
}

// legacyUndirectedProjection is the pre-#5954-S5/S6 directed->undirected
// projection, verbatim: one gonum node walk plus one boxed weighted-edge walk,
// accumulating reciprocal pairs into a simple.WeightedUndirectedGraph.
func legacyUndirectedProjection(g *simple.WeightedDirectedGraph) *simple.WeightedUndirectedGraph {
	und := simple.NewWeightedUndirectedGraph(0, 0)
	nodes := g.Nodes()
	for nodes.Next() {
		n := nodes.Node()
		if und.Node(n.ID()) == nil {
			und.AddNode(simple.Node(n.ID()))
		}
	}
	edges := g.WeightedEdges()
	for edges.Next() {
		e := edges.WeightedEdge()
		from, to := e.From().ID(), e.To().ID()
		if from == to {
			continue
		}
		if existing := und.WeightedEdge(from, to); existing != nil {
			w := existing.Weight() + e.Weight()
			und.RemoveEdge(from, to)
			und.SetWeightedEdge(und.NewWeightedEdge(simple.Node(from), simple.Node(to), w))
			continue
		}
		und.SetWeightedEdge(und.NewWeightedEdge(simple.Node(from), simple.Node(to), e.Weight()))
	}
	return und
}
