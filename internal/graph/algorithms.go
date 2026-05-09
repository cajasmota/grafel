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
	"sort"
	"time"

	gonumgraph "gonum.org/v1/gonum/graph"
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
// now display AutoName directly. A future Layer-2 agent-resolved label will
// take precedence over AutoName when present.
type CommunityResult struct {
	ID          int      `json:"id"`
	Size        int      `json:"size"`
	Modularity  float64  `json:"modularity"`
	TopEntities []string `json:"top_entities"`
	AutoName    string   `json:"auto_name,omitempty"`
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
	overallQ := sanitizeFloat(community.Q(und, groups, 1.0))

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
			communityOf[idx.fromInt[n.ID()]] = cid
			deg := 0
			it := und.From(n.ID())
			for it.Next() {
				deg++
			}
			members = append(members, member{n.ID(), deg})
		}
		sort.Slice(members, func(i, j int) bool { return members[i].degree > members[j].degree })

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

		// Per-community modularity contribution: Q of the singleton partition
		// containing only this group is meaningless; instead we expose this
		// community's own size-weighted contribution to overall Q.
		cQ := community.Q(und, [][]gonumgraph.Node{g}, 1.0)
		cQ = sanitizeFloat(cQ)

		results = append(results, CommunityResult{
			ID:          cid,
			Size:        len(g),
			Modularity:  cQ,
			TopEntities: top,
		})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Size > results[j].Size })
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
		betw[idx.fromInt[nid]] = sanitizeFloat(v)
	}

	// PageRank requires a directed graph — use g directly. damping=0.85,
	// tolerance=1e-6 per spec.
	prRaw := network.PageRank(g, 0.85, 1e-6)
	for nid, v := range prRaw {
		pr[idx.fromInt[nid]] = sanitizeFloat(v)
	}
	return betw, pr
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
		sort.Slice(ps, func(i, j int) bool { return ps[i].v > ps[j].v })
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
	sort.Slice(scored, func(i, j int) bool { return scored[i].Score > scored[j].Score })
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
	for start := range adj {
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
			RuntimeMS:          time.Since(start).Milliseconds(),
		},
	}
}
