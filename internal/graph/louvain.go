// Package graph — louvain.go implements Louvain modularity maximisation
// in-repo, replacing gonum's community.Modularize.
//
// # Why
//
// gonum v0.17.0's undirectedLocalMover.deltaQ evaluates a candidate move of
// node i into community C by iterating EVERY MEMBER of C, reading edge weights
// out of a map[[2]int]float64. One local-moving sweep therefore costs
// Σ_c |c|² map lookups. On the 25-repo production corpus (427k entities /
// 1.85M relationships) the partition has a 109k-node community, Σ|c|² = 2.75e10,
// and community detection took 3h15m — 99.5% of the whole indexing pipeline.
// Measured scaling was N^1.78–1.88 where an O(E)-per-sweep Louvain is N^1.0.
//
// # What this file does differently
//
// Same algorithm (multi-level local moving → aggregation → repeat), same
// resolution parameter, computed the standard way:
//
//  1. sigmaTot[c] (Σ of weighted degrees in c) is a running total updated in
//     O(1) on every move. A community is never rescanned to compute it.
//  2. k_{i,C} is accumulated by walking node i's NEIGHBOURS (degree ~4-10 on
//     code graphs) into a scratch array indexed by community id, reset in
//     O(deg) via a monotone stamp. Community membership is never iterated.
//  3. Adjacency is flat CSR ([]int32 offsets/targets + []float64 weights)
//     rather than map[[2]int]float64, which removes the cache-miss constant
//     on top of the asymptotic win.
//
// Per sweep: Σ|c|² → O(E).
//
// # Determinism
//
// There is no PRNG. Nodes are indexed by ascending gonum node id, each node's
// adjacency list is sorted by target index, sweeps run in ascending index
// order, and ties in the move decision are broken toward the lowest community
// index. Same input ⇒ same output, run to run.
//
// # Termination, and why louvainMaxSweeps is a REAL bound
//
// The intended argument is that the tie-break "move on equal gain only to a
// strictly lower community index" guarantees termination: a strict-gain move
// raises Q, a tie move holds Q and lowers Σ comm-index, so (Q, -Σ idx) is
// lexicographically monotone. That argument has a hole. The tie branch in
// localMoving assigns bestGain = gain, so bestGain can ratchet DOWNWARD by up
// to louvainMinGain per accepted tie; a chain of ties can therefore land a move
// whose true gain is below staying put by O(deg · 1e-12). Q is consequently not
// strictly monotone, and the pair is not strictly lexicographically monotone.
// No cycle has been constructed or observed, so this is theoretical — but it
// means louvainMaxSweeps is the actual backstop, not a formality.
//
// More importantly, the cap BINDS on realistic input. Sweeps-to-convergence at
// level 0, measured on Barabási-Albert graphs (scale-free, the shape a real
// code graph has), grows roughly N^0.65:
//
//	barabasi-albert n=  8000  117 sweeps
//	barabasi-albert n= 32000  266 sweeps
//	barabasi-albert n=128000  387 sweeps
//	planted-partition n=16000  10 sweeps   <- what the planted fixtures measure
//
// Extrapolated to the 427k-entity corpus that is several hundred sweeps, so the
// original cap of 100 would have truncated level 0 well short of convergence.
//
// WHAT THAT TRUNCATION ACTUALLY COSTS, MEASURED — this matters, because the
// intuitive answer is wrong. Q at cap=100 vs cap=1000 on the BA family:
//
//	n=  8000   cap100 Q=0.388869   cap1000 Q=0.388219   (-0.000650)
//	n= 32000   cap100 Q=0.380061   cap1000 Q=0.378383   (-0.001678)
//	n=128000   cap100 Q=0.375377   cap1000 Q=0.371322   (-0.004055)
//
// Running to convergence scores slightly WORSE here, not better. That is not a
// paradox: multi-level Louvain's final Q is not monotone in how far level 0 is
// converged — a truncated level 0 hands a different (here, slightly luckier)
// starting partition to aggregation. An independent review measured the
// opposite sign on its own BA fixtures (-0.001223 at n=128000 for cap=100).
// Both results are inside ±0.005. The honest reading is that the cap's effect
// on quality is NOISE, not a mechanism.
//
// So the cap is 1000 for well-definedness, not for a quality win: "Louvain run
// to convergence" is a specified algorithm whose output depends only on the
// graph, whereas "Louvain truncated at 100 sweeps" makes the partition a
// function of an arbitrary constant, drifting with graph size in a way nothing
// measures — and it was entirely unmeasured at 427k. Choosing 100 because it
// scored +0.004 on one synthetic family would be tuning to fixtures. Cost of
// the headroom: BA-128k goes 944ms -> 3.26s; each corpus sweep is O(E)=1.85e6,
// so this is seconds against 3h15m saved.
//
// louvainMaxLevels is genuinely slack (max observed depth: 6).
package graph

import (
	"sort"

	"gonum.org/v1/gonum/graph/simple"
)

const (
	// louvainMaxSweeps bounds local-moving iterations within a single level.
	// This is a TRUNCATION BOUND, not a formality: scale-free graphs need
	// hundreds of sweeps at level 0 and the count grows ~N^0.65, so a low cap
	// makes the partition a function of the cap rather than of the graph. Sized
	// so the 427k-entity corpus converges with headroom. See the "Termination"
	// section of the package comment for measured sweep counts, and for why the
	// quality effect of truncation is noise rather than a reason to tune this.
	louvainMaxSweeps = 1000
	// louvainMaxLevels bounds the aggregation hierarchy. Each level strictly
	// reduces the node count; max depth observed across the fixture set is 5,
	// so unlike louvainMaxSweeps this bound is genuine slack.
	louvainMaxLevels = 64
	// louvainMinGain is the epsilon under which two modularity gains are
	// considered equal (and the lowest-community-index tie-break applies).
	louvainMinGain = 1e-12
)

// csrGraph is a weighted undirected graph in compressed sparse row form.
//
// Self-loops live in selfw rather than in adj/w: aggregation creates them in
// bulk (every intra-community edge of one level becomes self-weight at the
// next) and keeping them out of the adjacency arrays means the local-moving
// inner loop never has to test for them.
type csrGraph struct {
	n     int
	off   []int32   // len n+1
	adj   []int32   // len off[n]
	w     []float64 // len off[n], parallel to adj
	selfw []float64 // len n, self-loop weight (counted ONCE)
	k     []float64 // len n, weighted degree: Σ incident w + 2*selfw
	m2    float64   // Σ k, i.e. 2m. Invariant across aggregation levels.
}

// buildCSRFromUndirected converts a gonum weighted undirected simple graph
// into CSR form. Nodes are indexed by ascending node id; ids[i] is the gonum
// node id of index i. Adjacency lists are sorted by target index so every
// downstream float summation happens in a fixed order.
func buildCSRFromUndirected(und *simple.WeightedUndirectedGraph) (*csrGraph, []int64) {
	var ids []int64
	nodes := und.Nodes()
	if nodes != nil {
		ids = make([]int64, 0, nodes.Len())
		for nodes.Next() {
			ids = append(ids, nodes.Node().ID())
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	n := len(ids)
	idxOf := make(map[int64]int32, n)
	for i, id := range ids {
		idxOf[id] = int32(i)
	}

	g := &csrGraph{
		n:     n,
		off:   make([]int32, n+1),
		selfw: make([]float64, n),
		k:     make([]float64, n),
	}
	if n == 0 {
		return g, ids
	}

	// Pass 1: degree count. gonum's edge iteration order is map-derived and
	// therefore unstable, so we materialise endpoints and sort later rather
	// than relying on iteration order for anything.
	type edge struct {
		u, v int32
		w    float64
	}
	var edges []edge
	if it := und.WeightedEdges(); it != nil {
		edges = make([]edge, 0, it.Len())
		for it.Next() {
			e := it.WeightedEdge()
			u, v := idxOf[e.From().ID()], idxOf[e.To().ID()]
			if u == v {
				// simple.WeightedUndirectedGraph rejects self-loops; guard anyway.
				g.selfw[u] += e.Weight()
				continue
			}
			edges = append(edges, edge{u, v, e.Weight()})
		}
	}
	for i := range edges {
		g.off[edges[i].u+1]++
		g.off[edges[i].v+1]++
	}
	for i := 0; i < n; i++ {
		g.off[i+1] += g.off[i]
	}
	total := g.off[n]
	g.adj = make([]int32, total)
	g.w = make([]float64, total)
	cursor := make([]int32, n)
	copy(cursor, g.off[:n])
	for i := range edges {
		e := edges[i]
		g.adj[cursor[e.u]] = e.v
		g.w[cursor[e.u]] = e.w
		cursor[e.u]++
		g.adj[cursor[e.v]] = e.u
		g.w[cursor[e.v]] = e.w
		cursor[e.v]++
	}
	g.sortAdjacency()
	g.computeDegrees()
	return g, ids
}

// sortAdjacency sorts each node's neighbour slice by target index, carrying the
// parallel weight slice along. Fixed order ⇒ fixed float summation order.
func (g *csrGraph) sortAdjacency() {
	for i := 0; i < g.n; i++ {
		lo, hi := g.off[i], g.off[i+1]
		if hi-lo < 2 {
			continue
		}
		sort.Sort(&adjSlice{adj: g.adj[lo:hi], w: g.w[lo:hi]})
	}
}

// adjSlice sorts a (targets, weights) pair of parallel slices by target.
type adjSlice struct {
	adj []int32
	w   []float64
}

func (a *adjSlice) Len() int           { return len(a.adj) }
func (a *adjSlice) Less(i, j int) bool { return a.adj[i] < a.adj[j] }
func (a *adjSlice) Swap(i, j int) {
	a.adj[i], a.adj[j] = a.adj[j], a.adj[i]
	a.w[i], a.w[j] = a.w[j], a.w[i]
}

// computeDegrees fills k (weighted degree, self-loops counted twice) and m2.
func (g *csrGraph) computeDegrees() {
	g.m2 = 0
	for i := 0; i < g.n; i++ {
		var d float64
		for p := g.off[i]; p < g.off[i+1]; p++ {
			d += g.w[p]
		}
		d += 2 * g.selfw[i]
		g.k[i] = d
		g.m2 += d
	}
}

// localMoving runs the Louvain local-moving phase: repeatedly sweep every node
// in ascending index order and move it into the neighbouring community that
// maximises the modularity gain
//
//	ΔQ ∝ k_{i,C} - resolution * k_i * sigmaTot[C] / m2
//
// Returns the (compacted) community label per node, the community count,
// whether any node moved at all, and the number of sweeps consumed. The sweep
// count is reported so tests can assert louvainMaxSweeps is not binding — see
// TestLouvainSweepCapIsNotBinding.
func (g *csrGraph) localMoving(resolution float64) (comm []int32, nComm int, moved bool, sweeps int) {
	n := g.n
	comm = make([]int32, n)
	for i := range comm {
		comm[i] = int32(i)
	}
	if n == 0 || g.m2 <= 0 {
		return comm, n, false, 0
	}

	sigmaTot := make([]float64, n)
	copy(sigmaTot, g.k)

	kIn := make([]float64, n) // scratch: k_{i,C} indexed by community id
	stamp := make([]int64, n) // scratch: freshness marker for kIn
	for i := range stamp {
		stamp[i] = -1
	}
	touched := make([]int32, 0, 64)
	var mark int64

	for sweep := 0; sweep < louvainMaxSweeps; sweep++ {
		sweeps++
		moves := 0
		for i := 0; i < n; i++ {
			ci := comm[i]
			ki := g.k[i]

			// Neighbour-side accumulation of k_{i,C}. Cost O(deg(i)); the
			// scratch reset is implicit via the monotone stamp.
			mark++
			touched = touched[:0]
			stamp[ci] = mark
			kIn[ci] = 0
			touched = append(touched, ci)
			for p := g.off[i]; p < g.off[i+1]; p++ {
				c := comm[g.adj[p]]
				if stamp[c] != mark {
					stamp[c] = mark
					kIn[c] = 0
					touched = append(touched, c)
				}
				kIn[c] += g.w[p]
			}

			// Remove i from its own community — O(1), no rescan.
			sigmaTot[ci] -= ki

			bestC := ci
			bestGain := kIn[ci] - resolution*ki*sigmaTot[ci]/g.m2
			for _, c := range touched {
				if c == ci {
					continue
				}
				gain := kIn[c] - resolution*ki*sigmaTot[c]/g.m2
				switch {
				case gain > bestGain+louvainMinGain:
					bestGain, bestC = gain, c
				case gain > bestGain-louvainMinGain && c < bestC:
					// Equal gain: prefer the lowest community index. This is
					// both the determinism rule and the termination argument
					// (see the package comment).
					bestGain, bestC = gain, c
				}
			}

			sigmaTot[bestC] += ki
			if bestC != ci {
				comm[i] = bestC
				moves++
				moved = true
			}
		}
		if moves == 0 {
			break
		}
	}

	nComm = compactLabels(comm)
	return comm, nComm, moved, sweeps
}

// compactLabels renumbers labels to 0..k-1 in order of first appearance
// scanning ascending node index. Returns the number of distinct labels.
func compactLabels(comm []int32) int {
	remap := make(map[int32]int32, len(comm))
	next := int32(0)
	for i, c := range comm {
		nc, ok := remap[c]
		if !ok {
			nc = next
			remap[c] = nc
			next++
		}
		comm[i] = nc
	}
	return int(next)
}

// aggregate collapses each community into a single node, producing the graph
// for the next Louvain level. Intra-community edge weight becomes the new
// node's self-loop; inter-community weight becomes the new edges.
func (g *csrGraph) aggregate(comm []int32, nc int) *csrGraph {
	// Bucket members by community (counting sort, O(n)).
	counts := make([]int32, nc+1)
	for _, c := range comm {
		counts[c+1]++
	}
	for i := 0; i < nc; i++ {
		counts[i+1] += counts[i]
	}
	members := make([]int32, g.n)
	cursor := make([]int32, nc)
	copy(cursor, counts[:nc])
	for i, c := range comm {
		members[cursor[c]] = int32(i)
		cursor[c]++
	}

	out := &csrGraph{
		n:     nc,
		off:   make([]int32, nc+1),
		selfw: make([]float64, nc),
		k:     make([]float64, nc),
		m2:    g.m2, // total weight is invariant under aggregation
	}
	out.adj = make([]int32, 0, len(g.adj))
	out.w = make([]float64, 0, len(g.w))

	acc := make([]float64, nc)
	stamp := make([]int64, nc)
	for i := range stamp {
		stamp[i] = -1
	}
	touched := make([]int32, 0, 64)

	for c := 0; c < nc; c++ {
		mark := int64(c)
		touched = touched[:0]
		var selfSum, internal float64
		for mi := counts[c]; mi < counts[c+1]; mi++ {
			u := members[mi]
			selfSum += g.selfw[u]
			out.k[c] += g.k[u]
			for p := g.off[u]; p < g.off[u+1]; p++ {
				d := comm[g.adj[p]]
				if d == int32(c) {
					internal += g.w[p]
					continue
				}
				if stamp[d] != mark {
					stamp[d] = mark
					acc[d] = 0
					touched = append(touched, d)
				}
				acc[d] += g.w[p]
			}
		}
		// Every intra-community edge was seen from both endpoints.
		out.selfw[c] = selfSum + internal/2
		sort.Slice(touched, func(i, j int) bool { return touched[i] < touched[j] })
		for _, d := range touched {
			out.adj = append(out.adj, d)
			out.w = append(out.w, acc[d])
		}
		out.off[c+1] = int32(len(out.adj))
	}
	return out
}

// louvainPartition runs multi-level Louvain over the undirected projection and
// returns the communities as slices of gonum node ids.
//
// Community ordering is deterministic: communities are numbered by the
// ascending node index of their lowest-indexed member, and members within a
// community are listed in ascending node-id order.
func louvainPartition(und *simple.WeightedUndirectedGraph, resolution float64) [][]int64 {
	groups, _ := louvainPartitionWithSweeps(und, resolution)
	return groups
}

// louvainPartitionWithSweeps is louvainPartition plus the per-level
// sweeps-to-convergence counts. Exposed (unexported, test-facing) because
// louvainMaxSweeps is a real truncation bound and silent truncation is the one
// way this implementation can degrade partition quality at scale — so it has to
// be observable, not inferred.
func louvainPartitionWithSweeps(und *simple.WeightedUndirectedGraph, resolution float64) ([][]int64, []int) {
	base, ids := buildCSRFromUndirected(und)
	n := base.n
	if n == 0 {
		return nil, nil
	}
	var sweepsPerLevel []int

	global := make([]int32, n)
	for i := range global {
		global[i] = int32(i)
	}

	cur := base
	for level := 0; level < louvainMaxLevels; level++ {
		comm, nc, moved, sweeps := cur.localMoving(resolution)
		sweepsPerLevel = append(sweepsPerLevel, sweeps)
		if !moved {
			break
		}
		for i := range global {
			global[i] = comm[global[i]]
		}
		if nc >= cur.n || nc <= 1 {
			break
		}
		cur = cur.aggregate(comm, nc)
	}

	// Renumber final labels by ascending lowest member index, then emit.
	nGroups := compactLabels(global)
	sizes := make([]int, nGroups)
	for _, c := range global {
		sizes[c]++
	}
	groups := make([][]int64, nGroups)
	for c := range groups {
		groups[c] = make([]int64, 0, sizes[c])
	}
	for i, c := range global {
		groups[c] = append(groups[c], ids[i])
	}
	return groups, sweepsPerLevel
}
