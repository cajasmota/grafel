package mcp

import (
	"container/heap"
	"math"
	"sort"
	"strconv"

	"github.com/cajasmota/grafel/internal/graph"
)

// adjacency is a per-repo precomputed neighbor index (#5852: CSR layout).
//
// Historically this was two map[string][]edge (out/in), ~290 MB resident at
// corpus scale (~3.70M edge structs across ~753k distinct keys — 225,143 out
// keys / 528,424 in keys — each edge{target string; kind string; weight
// float64; relIdx int} costing 48 bytes plus map/slice overhead). That's
// replaced here with a compressed-sparse-row (CSR) layout over a dense int32
// node index: one shared map[string]int32 (nodes) instead of two
// map[string][]edge, plus four parallel primitive-typed arrays per direction
// (offsets/targets/kindCode/weight/relIdx) instead of 3.70M individually
// heap-allocated-via-append edge structs. kind strings are dictionary-encoded
// (small cardinality — extractors emit a few dozen relationship kinds) and
// weight is stored as float32 (edgeWeight is a heuristic score; the values
// observed in practice — small integers or simple decimals from
// Properties["count"]/["weight"] — round-trip exactly through float32; see
// TestCSR5852_WeightFallbackSemantics).
//
// The external contract is unchanged: Outgoing(id)/Incoming(id) still return
// []edge in the original per-node relationship-append order, so none of the
// ~20 reader call-sites change. The []edge slice is materialized on demand
// from the CSR row inside the accessor.
type adjacency struct {
	nodes nodeIndex
	kinds kindDict
	out   csrDir
	in    csrDir
}

// nodeIndex assigns a dense int32 code to every string id encountered while
// building adjacency (either side of a relationship). Codes are local to one
// build (one adjacency instance) and index into `ids`. When ids coincide with
// interned graph.Entity.ID / Relationship.FromID/ToID strings (the common
// case post-#5847 interning), no new string backing is allocated here — only
// the code slice/map.
type nodeIndex struct {
	ids  []string
	code map[string]int32
}

func (n *nodeIndex) intern(id string) int32 {
	if n.code == nil {
		n.code = make(map[string]int32)
	}
	if c, ok := n.code[id]; ok {
		return c
	}
	c := int32(len(n.ids))
	n.ids = append(n.ids, id)
	n.code[id] = c
	return c
}

func (n *nodeIndex) lookup(id string) (int32, bool) {
	c, ok := n.code[id]
	return c, ok
}

// kindDict dictionary-encodes relationship-kind strings (CALLS, IMPORTS,
// REFERENCES, ...) to a small integer code. Cardinality is a few dozen kinds
// across all extractors, well within uint16 (and uint8) range; uint16 is used
// for headroom without meaningfully changing the memory profile (2 bytes vs.
// 1 is negligible against the string-per-edge cost it replaces).
type kindDict struct {
	names []string
	code  map[string]uint16
}

func (k *kindDict) intern(name string) uint16 {
	if k.code == nil {
		k.code = make(map[string]uint16)
	}
	if c, ok := k.code[name]; ok {
		return c
	}
	c := uint16(len(k.names))
	k.names = append(k.names, name)
	k.code[name] = c
	return c
}

func (k *kindDict) name(c uint16) string {
	if int(c) >= len(k.names) {
		return ""
	}
	return k.names[c]
}

// csrDir is one direction (out or in) of the CSR adjacency: row i's edges are
// the half-open slice [offsets[i], offsets[i+1]) into targets/kindCode/
// weight/relIdx. len(offsets) == number of distinct nodes + 1.
type csrDir struct {
	offsets  []int32
	targets  []int32 // node-index code of the edge's other endpoint
	kindCode []uint16
	weight   []float32
	relIdx   []int32
}

// edges materializes the []edge row for node id, preserving the original
// relationship-append order (rows are filled in doc.Relationships order
// during build). Returns nil when id is unknown or has no edges in this
// direction, matching the old map[string][]edge lookup-miss semantics.
func (c *csrDir) edges(id string, nodes *nodeIndex, kinds *kindDict) []edge {
	code, ok := nodes.lookup(id)
	if !ok || int(code) >= len(c.offsets)-1 {
		return nil
	}
	start, end := c.offsets[code], c.offsets[code+1]
	if start == end {
		return nil
	}
	out := make([]edge, end-start)
	for i := start; i < end; i++ {
		out[i-start] = edge{
			target: nodes.ids[c.targets[i]],
			kind:   kinds.name(c.kindCode[i]),
			weight: float64(c.weight[i]),
			relIdx: int(c.relIdx[i]),
		}
	}
	return out
}

// edge is a typed neighbor reference; weight is for shortest-path scoring.
//
// relIdx is the index back into the source Doc.Relationships slice; -1 when
// the edge is synthetic (e.g. reversed/cross-repo edges constructed at query
// time in dashboard_tools.go / tools.go). Handlers that need Relationship
// Properties (e.g. agent-repair audit) use it to fetch the full record
// without re-scanning Relationships. (#2285)
type edge struct {
	target string
	kind   string
	weight float64
	relIdx int
}

// Outgoing returns the out-edges from id (empty when id has none). Safe on a
// nil receiver to simplify handler call sites that may run before reload. The
// returned slice is freshly materialized from the CSR row on each call —
// callers must not mutate it, and hot-path callers should call this once per
// node and reuse the result rather than re-invoking per-edge. (#2285, #5852)
func (a *adjacency) Outgoing(id string) []edge {
	if a == nil {
		return nil
	}
	return a.out.edges(id, &a.nodes, &a.kinds)
}

// Incoming mirrors Outgoing for in-edges. (#2285, #5852)
func (a *adjacency) Incoming(id string) []edge {
	if a == nil {
		return nil
	}
	return a.in.edges(id, &a.nodes, &a.kinds)
}

// buildAdjacency constructs the in/out CSR neighbor index for one repo.
//
// Built ONCE per reload, lazily on first use via repo.getAdjacency()
// (#3367, formerly eager #1656). Handlers must NOT call this per-query — they
// pay an O(R)=117k scan plus thousands of allocations every time. Use
// repo.getAdjacency() instead, which caches the result until the next reload.
//
// Edge weight: defaults to 1.0 but reads Properties["count"] or
// Properties["weight"] when present. Extractors that deduplicate call sites
// into a single CALLS edge with a numeric "count" property (e.g. "30" for a
// file that calls a function 30 times) will have their frequency honoured by
// any consumer that sums edge.weight instead of counting raw edges (#2591).
//
// Two-pass CSR build (#5852): pass 1 interns every FromID/ToID/Kind and
// records per-relationship codes + degree counts; a prefix sum over degree
// counts yields row offsets; pass 2 scatters each relationship into its row
// using a per-row write cursor initialised from the offsets. Because pass 2
// walks doc.Relationships in the same order as the old
// `a.out[r.FromID] = append(...)` loop, and the write cursor only advances,
// each row's edges land in the exact original append order.
func buildAdjacency(doc *graph.Document, repo string) *adjacency {
	n := len(doc.Relationships)
	a := &adjacency{}
	a.nodes.code = make(map[string]int32, len(doc.Entities))
	a.kinds.code = make(map[string]uint16, 32)

	fromCodes := make([]int32, n)
	toCodes := make([]int32, n)
	kindCodes := make([]uint16, n)
	weights := make([]float32, n)

	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		fromCodes[i] = a.nodes.intern(r.FromID)
		toCodes[i] = a.nodes.intern(r.ToID)
		kindCodes[i] = a.kinds.intern(r.Kind)
		weights[i] = float32(edgeWeight(r))
	}

	numNodes := len(a.nodes.ids)
	outDeg := make([]int32, numNodes)
	inDeg := make([]int32, numNodes)
	for i := 0; i < n; i++ {
		outDeg[fromCodes[i]]++
		inDeg[toCodes[i]]++
	}

	a.out = newCSRDir(numNodes, n, outDeg)
	a.in = newCSRDir(numNodes, n, inDeg)
	outCursor := append([]int32(nil), a.out.offsets[:numNodes]...)
	inCursor := append([]int32(nil), a.in.offsets[:numNodes]...)

	for i := 0; i < n; i++ {
		fc, tc, kc, w := fromCodes[i], toCodes[i], kindCodes[i], weights[i]

		op := outCursor[fc]
		outCursor[fc]++
		a.out.targets[op] = tc
		a.out.kindCode[op] = kc
		a.out.weight[op] = w
		a.out.relIdx[op] = int32(i)

		ip := inCursor[tc]
		inCursor[tc]++
		a.in.targets[ip] = fc
		a.in.kindCode[ip] = kc
		a.in.weight[ip] = w
		a.in.relIdx[ip] = int32(i)
	}

	return a
}

// newCSRDir allocates a csrDir's row backing given per-node degree counts and
// the total edge count for this direction (= len(doc.Relationships), since
// every relationship contributes exactly one row entry per direction).
func newCSRDir(numNodes, numEdges int, deg []int32) csrDir {
	offsets := make([]int32, numNodes+1)
	for i := 0; i < numNodes; i++ {
		offsets[i+1] = offsets[i] + deg[i]
	}
	return csrDir{
		offsets:  offsets,
		targets:  make([]int32, numEdges),
		kindCode: make([]uint16, numEdges),
		weight:   make([]float32, numEdges),
		relIdx:   make([]int32, numEdges),
	}
}

// edgeWeight returns the numeric weight for a relationship edge. It reads
// Properties["count"] first (call-site count emitted by extractors that
// deduplicate edges), then Properties["weight"] (module-aggregate weight),
// falling back to 1.0. Values <= 0 are treated as 1.0.
func edgeWeight(r *graph.Relationship) float64 {
	for _, key := range []string{"count", "weight"} {
		if r.PropLen() == 0 {
			break
		}
		if v, ok := r.PropLookup(key); ok && v != "" {
			if n, err := strconv.ParseFloat(v, 64); err == nil && n > 0 {
				return n
			}
		}
	}
	return 1.0
}

// stepEdge is a single STEP_IN_PROCESS edge entry stored in StepAdj.
// It carries the step target ID and the step_index property so that
// buildProcessStepsWithCrossRepo can sort and render without touching
// Doc.Relationships at query time (#2417).
type stepEdge struct {
	toID string
	idx  int
}

// buildStepAdjacency precomputes the forward STEP_IN_PROCESS adjacency
// consumed by buildProcessStepsWithCrossRepo. Built ONCE per reload, lazily on
// first use via repo.getStepAdj() (#3367, formerly eager #2417). Eliminates the O(R) scan over
// Doc.Relationships that the function previously paid on every
// process-flow query.
func buildStepAdjacency(doc *graph.Document) map[string][]stepEdge {
	adj := make(map[string][]stepEdge)
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		if r.Kind != stepInProcessEdge {
			continue
		}
		idxStr := ""
		if r.PropLen() > 0 {
			idxStr = r.PropGet("step_index")
		}
		n, _ := strconv.Atoi(idxStr)
		adj[r.FromID] = append(adj[r.FromID], stepEdge{toID: r.ToID, idx: n})
	}
	return adj
}

// buildCallsAdjacency precomputes the forward CALLS adjacency consumed by
// traces.followCallsBFS. Built ONCE per reload, lazily on first use via
// repo.getCallsAdj() (#3367, formerly eager #1656). Targets within each entry are pre-sorted so
// callers don't need to sort.Strings on the hot path.
func buildCallsAdjacency(doc *graph.Document) map[string][]string {
	adj := make(map[string][]string)
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		if r.Kind != "CALLS" {
			continue
		}
		adj[r.FromID] = append(adj[r.FromID], r.ToID)
	}
	for k := range adj {
		// Sort once at build time so query-time can copy directly.
		sortStrings(adj[k])
	}
	return adj
}

// sortStrings is a tiny insertion sort wrapper to avoid pulling "sort" into
// the hot path here (keeps the file self-contained). Lists are small (most
// entries are <= 16 callees).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		v := s[i]
		j := i - 1
		for j >= 0 && s[j] > v {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = v
	}
}

// bfs walks `depth` hops outward from `start` along the given adjacency,
// returning the visited set as a map id->depth.
func bfs(adj *adjacency, start string, depth int, contextFilter map[string]bool) map[string]int {
	visited, _ := bfsBounded(adj, start, depth, contextFilter, 0)
	return visited
}

// bfsDir selects which adjacency directions a bounded BFS expands.
type bfsDir int

const (
	bfsBoth bfsDir = iota // in+out (default neighbourhood walk)
	bfsOut                // out-edges only (callees)
	bfsIn                 // in-edges only (callers)
)

// bfsCandidate is a pending expansion target discovered while processing one
// frontier level. depth is the target's hop distance (parent depth + 1).
type bfsCandidate struct {
	target string
	depth  int
}

// bfsRanker customises bounded BFS for locality-aware truncation and hub
// containment (#5691). Both hooks are optional; a nil *bfsRanker yields a plain
// deterministic walk (candidates ordered by target id) with no hub guard.
type bfsRanker struct {
	// less orders same-level candidates; when a level is truncated the cap
	// keeps the smallest under this ordering, so local structure survives while
	// a hub's far-flung fan-out is dropped. Must be a strict weak ordering with
	// a deterministic final tiebreak.
	less func(a, b bfsCandidate) bool
	// isHub reports whether expanding id would inherit a high-degree hub's
	// fan-out. A hub node is still ADDED to the visited set (so the caller sees
	// the boundary) but its neighbours are NOT expanded — the walk stops at it
	// and the caller annotates the crossing. Never applied to the start node
	// (the query target is always expanded).
	isHub func(id string) bool
}

// rankCandidates orders a frontier level's candidates in place. With a ranker's
// less hook it applies locality-first ordering; otherwise it falls back to a
// stable order by target id. This replaces the old Go map-iteration order,
// which made truncation non-deterministic (#5691).
func rankCandidates(cands []bfsCandidate, ranker *bfsRanker) {
	if ranker != nil && ranker.less != nil {
		sort.SliceStable(cands, func(i, j int) bool { return ranker.less(cands[i], cands[j]) })
		return
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].target < cands[j].target })
}

// bfsBounded is bfs with an optional node-count cap. When maxNodes > 0 and the
// visited set reaches that many nodes, expansion stops early and truncated is
// returned true. The cap bounds the pathological high-degree tail (a hub at
// depth>1 fanning out to thousands of nodes) for both BFS work and downstream
// serialization, while leaving the common small-subgraph case complete
// (maxNodes<=0 disables the cap). Truncation is honest: callers surface a
// marker rather than silently dropping nodes. (#3924)
func bfsBounded(adj *adjacency, start string, depth int, contextFilter map[string]bool, maxNodes int) (map[string]int, bool) {
	visited, truncated, _ := bfsBoundedRanked(adj, start, depth, contextFilter, maxNodes, nil, bfsBoth)
	return visited, truncated
}

// bfsBoundedRanked is the ranked, hub-aware core behind bfsBounded (#5691).
//
// Each frontier level is processed atomically: all candidate targets are
// gathered, ranked (locality-first via the ranker, else deterministically by
// id), and only then added up to the cap. Ranking BEFORE the cap is what keeps
// local structure alive under truncation — the old code stopped in arbitrary
// map-iteration order, so a hub reached mid-walk dumped whatever iterated first.
//
// When nothing is truncated the visited SET is identical to the naive walk
// (ranking only changes discovery order, and callers sort their output), so
// small/normal graphs are unaffected.
//
// hubs holds the ids of hub nodes the walk declined to expand (stop-and-
// annotate). dir selects in/out/both adjacency directions.
func bfsBoundedRanked(adj *adjacency, start string, depth int, contextFilter map[string]bool, maxNodes int, ranker *bfsRanker, dir bfsDir) (map[string]int, bool, []string) {
	visited := map[string]int{start: 0}
	frontier := []string{start}
	truncated := false
	var hubs []string
	hubSeen := map[string]bool{}

	consider := func(cands *[]bfsCandidate, candSeen map[string]bool, e edge, d int) {
		if contextFilter != nil && !contextFilter[e.kind] {
			return
		}
		if _, seen := visited[e.target]; seen {
			return
		}
		if candSeen[e.target] {
			return
		}
		candSeen[e.target] = true
		*cands = append(*cands, bfsCandidate{target: e.target, depth: d + 1})
	}

	for d := 0; d < depth && !truncated; d++ {
		cands := make([]bfsCandidate, 0)
		candSeen := map[string]bool{}
		for _, n := range frontier {
			// Hub-aware stop: don't inherit a hub's fan-out. The start node is
			// exempt — it is the explicit query target and is always expanded.
			if ranker != nil && ranker.isHub != nil && n != start && ranker.isHub(n) {
				if !hubSeen[n] {
					hubSeen[n] = true
					hubs = append(hubs, n)
				}
				continue
			}
			if dir == bfsBoth || dir == bfsOut {
				for _, e := range adj.Outgoing(n) {
					consider(&cands, candSeen, e, d)
				}
			}
			if dir == bfsBoth || dir == bfsIn {
				for _, e := range adj.Incoming(n) {
					consider(&cands, candSeen, e, d)
				}
			}
		}
		rankCandidates(cands, ranker)
		next := make([]string, 0, len(cands))
		for _, c := range cands {
			if maxNodes > 0 && len(visited) >= maxNodes {
				truncated = true
				break
			}
			visited[c.target] = c.depth
			next = append(next, c.target)
		}
		frontier = next
		if len(frontier) == 0 {
			break
		}
	}
	return visited, truncated, hubs
}

// pqItem is one entry in the dijkstra priority queue.
type pqItem struct {
	id    string // prefixed: <repo>::<localId>
	cost  float64
	prev  string
	prevK string
	index int
}

type pq []*pqItem

func (p pq) Len() int            { return len(p) }
func (p pq) Less(i, j int) bool  { return p[i].cost < p[j].cost }
func (p pq) Swap(i, j int)       { p[i], p[j] = p[j], p[i]; p[i].index = i; p[j].index = j }
func (p *pq) Push(x interface{}) { *p = append(*p, x.(*pqItem)) }
func (p *pq) Pop() interface{}   { old := *p; n := len(old); x := old[n-1]; *p = old[:n-1]; return x }

// dijkstra finds the shortest path from src to dst using a callable that
// expands neighbors. node IDs are prefixed (<repo>::<localId>) so this works
// across repos via overlay edges. Returns (path, edgeKinds, weakest, ok).
//
// expand returns []edge for the given prefixed node id.
func dijkstra(src, dst string, expand func(string) []edge) ([]string, []string, float64, bool) {
	if src == dst {
		return []string{src}, nil, 1.0, true
	}
	dist := map[string]float64{src: 0}
	prev := map[string]string{}
	prevKind := map[string]string{}
	q := &pq{}
	heap.Init(q)
	heap.Push(q, &pqItem{id: src, cost: 0})
	for q.Len() > 0 {
		cur := heap.Pop(q).(*pqItem)
		if cur.id == dst {
			path := []string{dst}
			edges := []string{prevKind[dst]}
			at := dst
			for prev[at] != "" {
				at = prev[at]
				path = append([]string{at}, path...)
				if k, ok := prevKind[at]; ok && at != src {
					edges = append([]string{k}, edges...)
				}
			}
			weakest := math.Inf(1)
			for _, p := range path[1:] {
				if d, ok := dist[p]; ok && d-(dist[prev[p]]) < weakest {
					weakest = d - dist[prev[p]]
				}
			}
			if math.IsInf(weakest, 1) {
				weakest = 1.0
			}
			// Convert "weight" cost back to confidence. Higher weight = lower
			// confidence; we used cost = -log(confidence).
			conf := math.Exp(-weakest)
			return path, edges, conf, true
		}
		if cur.cost > dist[cur.id] {
			continue
		}
		for _, e := range expand(cur.id) {
			nd := cur.cost + e.weight
			if old, ok := dist[e.target]; !ok || nd < old {
				dist[e.target] = nd
				prev[e.target] = cur.id
				prevKind[e.target] = e.kind
				heap.Push(q, &pqItem{id: e.target, cost: nd})
			}
		}
	}
	return nil, nil, 0, false
}
