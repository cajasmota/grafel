// Package graph — algorithms.go implements Pass 4: graph algorithm
// computation over the merged entity/relationship set.
//
// Six attributes are pre-baked into every entity:
//   - community_id              (Louvain modularity-maximising community)
//   - centrality                (betweenness centrality, weighted)
//   - pagerank                  (PageRank, damping=0.85, tol=1e-6)
//   - is_god_node               (top 5% by combined betweenness+pagerank rank)
//   - is_surprise_endpoint      (endpoint of one of the top-K cross-community
//     "surprise" edges)
//   - is_articulation_point     (cut vertex in the undirected graph)
//
// Top-level corpus aggregates are exposed via AlgorithmResults: per-community
// stats, surprise-edge list, and timing.
package graph

import (
	"math"
	"math/rand/v2"
	"os"
	"sort"
	"time"

	"gonum.org/v1/gonum/graph/community"
	"gonum.org/v1/gonum/graph/network"
	"gonum.org/v1/gonum/graph/path"
	"gonum.org/v1/gonum/graph/simple"
)

// CommunityResult summarises one Louvain community for the on-disk output.
//
// AutoName is the deterministic Layer-1 label produced by AssignCommunityNames
// (TF-IDF over member entity names). It is always populated when communities
// are computed; consumers that previously fell back to "community_<id>" can
// now display AutoName directly.
//
// AgentName is the Layer-2 label resolved by an MCP agent via
// submit_enrichment(kind="name_community"). It takes precedence over AutoName
// when present (issue #426).
type CommunityResult struct {
	ID          int      `json:"id"`
	Size        int      `json:"size"`
	Modularity  float64  `json:"modularity"`
	TopEntities []string `json:"top_entities"`
	AutoName    string   `json:"auto_name,omitempty"`
	AgentName   string   `json:"agent_name,omitempty"`
}

// SurpriseEdge is a cross-community edge whose pair frequency is rare.
type SurpriseEdge struct {
	FromID string  `json:"from_id"`
	ToID   string  `json:"to_id"`
	Score  float64 `json:"score"`
	Reason string  `json:"reason"`
}

// AlgorithmStats are the corpus-level metrics exposed both inside graph.json
// and inside the .archigraph/graph-stats.json sidecar.
type AlgorithmStats struct {
	LouvainModularity  float64 `json:"louvain_modularity"`
	NumCommunities     int     `json:"num_communities"`
	NumGodNodes        int     `json:"num_god_nodes"`
	NumArticulationPts int     `json:"num_articulation_points"`
	NumSurpriseEdges   int     `json:"num_surprise_edges"`
	RuntimeMS          int64   `json:"runtime_ms"`
}

// AlgorithmResults bundles the per-entity and corpus-level outputs of Pass 4.
type AlgorithmResults struct {
	CommunityID        map[string]int     // entity id -> community id
	Centrality         map[string]float64 // entity id -> betweenness
	PageRank           map[string]float64 // entity id -> pagerank
	GodNodes           map[string]bool
	ArticulationPoints map[string]bool
	SurpriseEndpoints  map[string]bool
	Communities        []CommunityResult
	SurpriseEdges      []SurpriseEdge
	Stats              AlgorithmStats
}

// nodeIndex maps stable string entity IDs onto contiguous int64 node IDs
// (gonum/graph addresses nodes by int64 only).
type nodeIndex struct {
	toInt   map[string]int64
	fromInt map[int64]string
	next    int64
}

func newNodeIndex() *nodeIndex {
	return &nodeIndex{
		toInt:   make(map[string]int64),
		fromInt: make(map[int64]string),
	}
}

func (n *nodeIndex) get(id string) int64 {
	if v, ok := n.toInt[id]; ok {
		return v
	}
	v := n.next
	n.next++
	n.toInt[id] = v
	n.fromInt[v] = id
	return v
}

// BuildGraph constructs a weighted directed graph plus an index mapping
// string entity IDs to gonum int64 node IDs. Edge weight follows the spec:
//
//	weight = max(1, callsite_count) * confidence
//
// with both properties drawn from Relationship.Properties (string-typed).
func BuildGraph(entities []Entity, rels []Relationship) (*simple.WeightedDirectedGraph, *nodeIndex) {
	g := simple.NewWeightedDirectedGraph(0, 0)
	idx := newNodeIndex()

	// Insert every entity as a node so isolated nodes still receive scores.
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
		// Skip edges whose endpoints aren't in the entity set (e.g. bare
		// stdlib names): they'd inflate node count without contributing
		// real structure.
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
		w := edgeWeight(r.Properties)
		// If the edge already exists, accumulate weight (multiple call sites).
		if existing := g.WeightedEdge(from, to); existing != nil {
			w += existing.Weight()
			g.RemoveEdge(from, to)
		}
		g.SetWeightedEdge(g.NewWeightedEdge(simple.Node(from), simple.Node(to), w))
	}
	return g, idx
}

func edgeWeight(props map[string]string) float64 {
	calls := 1
	if v, ok := props["callsite_count"]; ok {
		if n := atoiSafe(v); n > 1 {
			calls = n
		}
	}
	conf := 1.0
	if v, ok := props["confidence"]; ok {
		if f := atofSafe(v); f > 0 {
			conf = f
		}
	}
	return float64(calls) * conf
}

func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func atofSafe(s string) float64 {
	// Tiny non-locale-aware parser to avoid pulling strconv panics on
	// adversarial input. Returns 0 on parse failure (caller falls back to 1).
	if s == "" {
		return 0
	}
	neg := false
	i := 0
	if s[0] == '-' {
		neg = true
		i = 1
	} else if s[0] == '+' {
		i = 1
	}
	whole, frac, fracDiv := 0.0, 0.0, 1.0
	seenDot := false
	for ; i < len(s); i++ {
		c := s[i]
		if c == '.' {
			if seenDot {
				return 0
			}
			seenDot = true
			continue
		}
		if c < '0' || c > '9' {
			return 0
		}
		if seenDot {
			frac = frac*10 + float64(c-'0')
			fracDiv *= 10
		} else {
			whole = whole*10 + float64(c-'0')
		}
	}
	v := whole + frac/fracDiv
	if neg {
		v = -v
	}
	return v
}

// ComputeCommunities runs Louvain modularity maximisation on the undirected
// projection of g. Returns:
//   - per-community summary (size, modularity contribution, top entity names)
//   - mapping from entity ID -> community id
//   - overall modularity score
func ComputeCommunities(g *simple.WeightedDirectedGraph, idx *nodeIndex, entityNames map[string]string) ([]CommunityResult, map[string]int, float64) {
	// Project the directed graph onto an undirected graph; community detection
	// in gonum operates on undirected (or otherwise symmetric) inputs.
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

	// Deterministic source so the test suite is repeatable.
	src := rand.NewPCG(1, 2)
	reduced := community.Modularize(und, 1.0, src)

	groups := reduced.Communities()

	// Issue #633 phase-2 — pprof showed `community.Q` accounted for ~90% of
	// indexing allocations (21.6 GB on client-fixture-b: 9,549 communities ×
	// per-call O(|V|+|E|) iteration over the undirected graph). Each Q call
	// re-builds the weighted-degree table `k[]` and scans every node. We
	// replace ALL gonum Q calls with a single pre-computed pass:
	//
	//   1. Compute k[uid] (weighted degree) and m2 = Σ k[u] in one sweep.
	//   2. For each community, accumulate internal edge weight and ΣK in
	//      O(|E_C|) using a node→community map (built below).
	//   3. Per-community contribution: q_c = (2*internal_w - K_C^2/m2) / m2
	//      (BuildGraph drops self-loops, so the diagonal term collapses to 0
	//      and gonum's "2*w_uv for u<v" off-diagonal sum becomes 2*internal_w).
	//   4. Overall Q = Σ q_c — matches gonum's `community.Q(und, groups, 1)`
	//      to the rounding tolerance enforced by roundForDeterminism().
	const resolution = 1.0
	type nodeStat struct {
		k      float64
		cidIdx int // index into groups
		degree int
	}
	nodeStats := make(map[int64]*nodeStat, idx.next)
	// First, mark community membership so the edge sweep can classify edges
	// in O(1) without consulting `groups` repeatedly.
	for cid, gg := range groups {
		for _, n := range gg {
			nid := n.ID()
			if _, ok := nodeStats[nid]; !ok {
				nodeStats[nid] = &nodeStat{cidIdx: cid}
			} else {
				nodeStats[nid].cidIdx = cid
			}
		}
	}
	// Walk every undirected edge once: contribute weight to each endpoint's
	// `k` (weighted degree) and, when both endpoints share a community, to
	// that community's internal-weight accumulator.
	internalW := make([]float64, len(groups))
	var m2 float64
	wedges := und.WeightedEdges()
	for wedges.Next() {
		e := wedges.WeightedEdge()
		w := e.Weight()
		fid, tid := e.From().ID(), e.To().ID()
		nf, ok := nodeStats[fid]
		if !ok {
			// Isolated-from-Modularize node: gonum's Communities() still
			// covers every node from the original graph, but defensively
			// guard so the loop is total.
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

	// Per-community K_C = Σ k[u] for u in c.
	K := make([]float64, len(groups))
	for cid, gg := range groups {
		var k float64
		for _, n := range gg {
			if ns, ok := nodeStats[n.ID()]; ok {
				k += ns.k
			}
		}
		K[cid] = k
	}

	// Compute per-community q_c and overall Q analytically.
	var overallQRaw float64
	communityQ := make([]float64, len(groups))
	if m2 > 0 {
		for cid := range groups {
			// q_c = (2*internal_w_c - resolution * K_c^2 / m2) / m2
			q := (2*internalW[cid] - resolution*K[cid]*K[cid]/m2) / m2
			communityQ[cid] = q
			overallQRaw += q
		}
	}
	overallQ := roundForDeterminism(sanitizeFloat(overallQRaw))

	communityOf := make(map[string]int, idx.next)
	// Default every node into community -1; Modularize's Communities() lists
	// only nodes that participate in the reduced graph, but we want a value
	// for isolated entities too.
	for sid := range idx.toInt {
		communityOf[sid] = -1
	}
	results := make([]CommunityResult, 0, len(groups))

	for cid, g := range groups {
		// Sort member nodes by degree (proxy for "top entity") — degree of an
		// undirected weighted graph is best approximated by edge count.
		type member struct {
			id     int64
			degree int
		}
		members := make([]member, 0, len(g))
		for _, n := range g {
			nid := n.ID()
			communityOf[idx.fromInt[nid]] = cid
			deg := 0
			if ns, ok := nodeStats[nid]; ok {
				deg = ns.degree
			}
			members = append(members, member{nid, deg})
		}
		// Issue #481 — degree ties were resolved by map-iteration order
		// (g.Nodes / und.From); tiebreak on the gonum int64 node id so
		// TopEntities is reproducible.
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
			name := entityNames[eid]
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
	// Issue #481 — tiebreak Size-equal communities on the integer community
	// id assigned by Modularize so result ordering is stable across runs.
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Size != results[j].Size {
			return results[i].Size > results[j].Size
		}
		return results[i].ID < results[j].ID
	})
	return results, communityOf, overallQ
}

// ComputeCentrality returns betweenness centrality and PageRank, both keyed by
// the original string entity IDs.
//
// Betweenness uses gonum's BetweennessWeighted for graphs with at most
// betweennessExactCutoff nodes. Above that threshold we fall back to the
// unweighted Brandes implementation (much cheaper) and document the trade-off.
const betweennessExactCutoff = 3000

func ComputeCentrality(g *simple.WeightedDirectedGraph, idx *nodeIndex) (map[string]float64, map[string]float64) {
	betw := make(map[string]float64, idx.next)
	pr := make(map[string]float64, idx.next)
	// Pre-seed every entity with a 0 so callers can rely on the keys being
	// present even when gonum's algorithms only populate the active subset
	// (e.g. unreachable nodes in PageRank, leaf nodes in Betweenness).
	for _, id := range idx.toInt {
		betw[idx.fromInt[id]] = 0
		pr[idx.fromInt[id]] = 0
	}

	// Betweenness — choose exact-weighted vs unweighted based on graph size.
	var raw map[int64]float64
	if int(idx.next) <= betwennessNodeCount(idx) {
		// FloydWarshall is O(V^3) and precomputes all shortest paths; on
		// graphs <= cutoff this is the most accurate option.
		shortest, ok := path.FloydWarshall(g)
		if ok {
			raw = network.BetweennessWeighted(g, shortest)
		}
	}
	if raw == nil {
		raw = network.Betweenness(g)
	}
	for nid, v := range raw {
		betw[idx.fromInt[nid]] = roundForDeterminism(sanitizeFloat(v))
	}

	// PageRank requires a directed graph — use g directly. damping=0.85,
	// tolerance=1e-6 per spec.
	//
	// Issue #633 phase-2 — pprof showed `network.PageRank` allocates a dense
	// N×N transition matrix via `mat.NewDense` (~1.74 GB live for 15k nodes
	// on client-fixture-b). `network.PageRankSparse` solves the SAME fixed
	// point with identical damping/tolerance using a sparse row-compressed
	// matrix proportional to |E|. Both gonum variants use the same un-seeded
	// init vector and converge to the same scores; roundForDeterminism()
	// rounds to 1e-4 (well above the 1e-6 solver tolerance) so the on-disk
	// bytes stay stable. Always use sparse — code graphs are sparse by nature.
	prRaw := network.PageRankSparse(g, 0.85, 1e-6)
	for nid, v := range prRaw {
		pr[idx.fromInt[nid]] = roundForDeterminism(sanitizeFloat(v))
	}
	return betw, pr
}

// runtimeMSFor returns wall-clock milliseconds elapsed since start, or 0
// when SOURCE_DATE_EPOCH is set so reproducible-builds mode (#481) emits a
// stable byte stream.
func runtimeMSFor(start time.Time) int64 {
	if os.Getenv("SOURCE_DATE_EPOCH") != "" {
		return 0
	}
	return time.Since(start).Milliseconds()
}

// roundForDeterminism rounds a gonum-derived score to 4 decimal digits.
// Issue #481 — gonum's PageRank and Betweenness implementations iterate
// over node maps internally, so tiny floating-point reorderings accumulate
// to differences of ~1e-8 across runs of the same input. The PageRank
// solver converges to a tolerance of 1e-6 (see the call site below).
//
// Issue #489 — on larger graphs (gin ~6.4k entities, spdlog ~1.8k entities)
// the accumulated float drift crosses the 1e-5 boundary occasionally,
// causing 2/10 runs to produce different byte output even though the logical
// PageRank ranking is identical. Widening the rounding bucket to 1e-4 (4
// decimal places) absorbs all solver-tolerance noise while still retaining
// actionable score differences — practical PageRank delta thresholds for
// ranking are well above 1e-4.
func roundForDeterminism(v float64) float64 {
	if v == 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return v
	}
	const scale = 1e4
	return math.Round(v*scale) / scale
}

// betwennessNodeCount is a tiny indirection so we can stub the cutoff in
// tests without relying on a global mutable variable.
func betwennessNodeCount(idx *nodeIndex) int { return betweennessExactCutoff }

// sanitizeFloat scrubs NaN/+Inf/-Inf values to 0 so the JSON encoder doesn't
// reject them. Gonum's modularity computation can produce NaN on degenerate
// inputs (single-node communities, empty edge sets); 0 is the right neutral
// value to surface in the on-disk schema.
func sanitizeFloat(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

// IdentifyGodNodes returns the union of the top-5% nodes by betweenness AND
// the top-5% nodes by PageRank. Empty maps yield an empty result.
func IdentifyGodNodes(betw, pr map[string]float64) map[string]bool {
	out := make(map[string]bool)
	if len(betw) == 0 && len(pr) == 0 {
		return out
	}
	pickTop5 := func(m map[string]float64) []string {
		type pair struct {
			id string
			v  float64
		}
		ps := make([]pair, 0, len(m))
		for k, v := range m {
			ps = append(ps, pair{k, v})
		}
		// Issue #481 — ties on score were resolved by map-iteration order,
		// so the top-5% set flipped between runs. Stable sort with an ID
		// tiebreaker pins the membership.
		sort.SliceStable(ps, func(i, j int) bool {
			if ps[i].v != ps[j].v {
				return ps[i].v > ps[j].v
			}
			return ps[i].id < ps[j].id
		})
		k := len(ps) / 20 // 5%
		if k == 0 && len(ps) > 0 {
			k = 1
		}
		out := make([]string, 0, k)
		for i := 0; i < k; i++ {
			out = append(out, ps[i].id)
		}
		return out
	}
	for _, id := range pickTop5(betw) {
		out[id] = true
	}
	for _, id := range pickTop5(pr) {
		out[id] = true
	}
	return out
}

// ComputeSurpriseEdges scans every relationship; an edge whose endpoints sit
// in different communities is a "cross-community" edge. We score surprise as
// 1/frequency of the (commA, commB) pair: a once-only cross is maximally
// surprising. Top 20 by score are returned.
func ComputeSurpriseEdges(rels []Relationship, communityOf map[string]int) []SurpriseEdge {
	type pair struct{ a, b int }
	freq := make(map[pair]int)
	type candidate struct {
		from, to string
		p        pair
	}
	candidates := make([]candidate, 0)

	for _, r := range rels {
		ca, oka := communityOf[r.FromID]
		cb, okb := communityOf[r.ToID]
		if !oka || !okb || ca == cb {
			continue
		}
		// Order pair canonically so direction doesn't fragment frequency.
		p := pair{ca, cb}
		if p.a > p.b {
			p.a, p.b = p.b, p.a
		}
		freq[p]++
		candidates = append(candidates, candidate{r.FromID, r.ToID, p})
	}

	scored := make([]SurpriseEdge, 0, len(candidates))
	for _, c := range candidates {
		f := freq[c.p]
		score := 1.0 / float64(f)
		scored = append(scored, SurpriseEdge{
			FromID: c.from,
			ToID:   c.to,
			Score:  score,
			Reason: "rare_cross_community_pair",
		})
	}
	// Issue #481 — score ties were tiebroken by candidates-slice order,
	// which inherits goroutine-scheduling order through rels. Tiebreak on
	// (FromID, ToID) so the top-20 surface is reproducible.
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		if scored[i].FromID != scored[j].FromID {
			return scored[i].FromID < scored[j].FromID
		}
		return scored[i].ToID < scored[j].ToID
	})
	if len(scored) > 20 {
		scored = scored[:20]
	}
	return scored
}

// IdentifyArticulationPoints implements Tarjan's articulation-point algorithm
// on the undirected projection of g. A node u is an articulation point if:
//
//   - u is the root of the DFS tree and has at least two children; OR
//   - u has a child v such that no descendant of v has a back-edge to a
//     proper ancestor of u — i.e. low[v] >= disc[u].
//
// Returns a set of original entity IDs.
func IdentifyArticulationPoints(g *simple.WeightedDirectedGraph, idx *nodeIndex) map[string]bool {
	// Build undirected adjacency from the directed graph.
	adj := make(map[int64][]int64, idx.next)
	nodes := g.Nodes()
	for nodes.Next() {
		adj[nodes.Node().ID()] = nil
	}
	edges := g.Edges()
	seen := make(map[[2]int64]bool)
	for edges.Next() {
		e := edges.Edge()
		u, v := e.From().ID(), e.To().ID()
		if u == v {
			continue
		}
		key := [2]int64{u, v}
		if u > v {
			key = [2]int64{v, u}
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		adj[u] = append(adj[u], v)
		adj[v] = append(adj[v], u)
	}

	disc := make(map[int64]int, len(adj))
	low := make(map[int64]int, len(adj))
	parent := make(map[int64]int64, len(adj))
	visited := make(map[int64]bool, len(adj))
	ap := make(map[int64]bool)
	timer := 0

	// Iterative DFS to avoid stack overflow on very large graphs.
	type frame struct {
		u    int64
		i    int
		root bool
	}
	// Issue #481 — DFS root choice is observable in the articulation-point
	// set (the root is an articulation point iff it has >= 2 DFS children,
	// and the children we discover depend on neighbour ordering). Walk the
	// adjacency map in deterministic order: keys sorted ascending, and each
	// neighbour list sorted ascending so the DFS itself is reproducible.
	keys := make([]int64, 0, len(adj))
	for k := range adj {
		sort.Slice(adj[k], func(a, b int) bool { return adj[k][a] < adj[k][b] })
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for _, start := range keys {
		if visited[start] {
			continue
		}
		stack := []frame{{u: start, i: 0, root: true}}
		visited[start] = true
		disc[start] = timer
		low[start] = timer
		timer++
		parent[start] = -1
		children := 0

		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			if top.i < len(adj[top.u]) {
				v := adj[top.u][top.i]
				top.i++
				if !visited[v] {
					parent[v] = top.u
					visited[v] = true
					disc[v] = timer
					low[v] = timer
					timer++
					if top.root {
						children++
					}
					stack = append(stack, frame{u: v, i: 0})
				} else if v != parent[top.u] {
					if disc[v] < low[top.u] {
						low[top.u] = disc[v]
					}
				}
				continue
			}
			// All neighbours of top.u processed — propagate low to parent.
			u := top.u
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				// Root: articulation iff it had >= 2 DFS children.
				if children >= 2 {
					ap[u] = true
				}
				break
			}
			pu := stack[len(stack)-1].u
			if low[u] < low[pu] {
				low[pu] = low[u]
			}
			if low[u] >= disc[pu] && parent[pu] != -1 {
				ap[pu] = true
			}
		}
	}

	out := make(map[string]bool, len(ap))
	for nid := range ap {
		out[idx.fromInt[nid]] = true
	}
	return out
}

// RunAlgorithms executes the full Pass 4 sweep and bundles every result into
// AlgorithmResults. The caller decides how to attach the per-entity attributes
// onto the on-disk Document and where to emit the corpus aggregate.
func RunAlgorithms(entities []Entity, rels []Relationship) *AlgorithmResults {
	start := time.Now()

	g, idx := BuildGraph(entities, rels)

	names := make(map[string]string, len(entities))
	for _, e := range entities {
		names[e.ID] = e.Name
	}

	commResults, commOf, overallQ := ComputeCommunities(g, idx, names)
	// Layer-1 deterministic naming (TF-IDF over member entity names +
	// qualified names + source-file basenames). Mutates commResults in place.
	AssignCommunityNames(commResults, entities, commOf)
	betw, pr := ComputeCentrality(g, idx)
	gods := IdentifyGodNodes(betw, pr)
	arts := IdentifyArticulationPoints(g, idx)
	surprises := ComputeSurpriseEdges(rels, commOf)

	endpoints := make(map[string]bool, len(surprises)*2)
	for _, s := range surprises {
		endpoints[s.FromID] = true
		endpoints[s.ToID] = true
	}

	return &AlgorithmResults{
		CommunityID:        commOf,
		Centrality:         betw,
		PageRank:           pr,
		GodNodes:           gods,
		ArticulationPoints: arts,
		SurpriseEndpoints:  endpoints,
		Communities:        commResults,
		SurpriseEdges:      surprises,
		Stats: AlgorithmStats{
			LouvainModularity:  overallQ,
			NumCommunities:     len(commResults),
			NumGodNodes:        len(gods),
			NumArticulationPts: len(arts),
			NumSurpriseEdges:   len(surprises),
			// Issue #481 — RuntimeMS is wall-clock and therefore varies run to
			// run. When SOURCE_DATE_EPOCH is set (reproducible-builds mode)
			// emit 0 so graph.json stays byte-stable.
			RuntimeMS: runtimeMSFor(start),
		},
	}
}
