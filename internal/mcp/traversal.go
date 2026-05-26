package mcp

import (
	"container/heap"
	"math"

	"github.com/cajasmota/archigraph/internal/graph"
)

// adjacency is a per-repo precomputed neighbor map.
type adjacency struct {
	out map[string][]edge
	in  map[string][]edge
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
	repo   string
	relIdx int
}

// Outgoing returns the out-edges from id (empty when id has none). Safe on a
// nil receiver to simplify handler call sites that may run before reload. The
// returned slice is owned by the adjacency index — callers must not mutate it.
// (#2285)
func (a *adjacency) Outgoing(id string) []edge {
	if a == nil {
		return nil
	}
	return a.out[id]
}

// Incoming mirrors Outgoing for in-edges. (#2285)
func (a *adjacency) Incoming(id string) []edge {
	if a == nil {
		return nil
	}
	return a.in[id]
}

// buildAdjacency constructs in/out neighbor lists for one repo.
//
// Called ONCE per reload (cached on LoadedRepo.Adjacency, #1656). Handlers
// must NOT call this per-query — they pay an O(R)=117k scan plus thousands
// of allocations every time. Use repo.Adjacency instead.
func buildAdjacency(doc *graph.Document, repo string) *adjacency {
	a := &adjacency{
		out: make(map[string][]edge, len(doc.Entities)),
		in:  make(map[string][]edge, len(doc.Entities)),
	}
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		w := 1.0
		a.out[r.FromID] = append(a.out[r.FromID], edge{target: r.ToID, kind: r.Kind, weight: w, repo: repo, relIdx: i})
		a.in[r.ToID] = append(a.in[r.ToID], edge{target: r.FromID, kind: r.Kind, weight: w, repo: repo, relIdx: i})
	}
	return a
}

// buildCallsAdjacency precomputes the forward CALLS adjacency consumed by
// traces.followCallsBFS. Built ONCE per reload (cached on
// LoadedRepo.CallsAdj, #1656). Targets within each entry are pre-sorted so
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
	visited := map[string]int{start: 0}
	frontier := []string{start}
	for d := 0; d < depth; d++ {
		next := []string{}
		for _, n := range frontier {
			for _, e := range adj.out[n] {
				if contextFilter != nil && !contextFilter[e.kind] {
					continue
				}
				if _, seen := visited[e.target]; seen {
					continue
				}
				visited[e.target] = d + 1
				next = append(next, e.target)
			}
			for _, e := range adj.in[n] {
				if contextFilter != nil && !contextFilter[e.kind] {
					continue
				}
				if _, seen := visited[e.target]; seen {
					continue
				}
				visited[e.target] = d + 1
				next = append(next, e.target)
			}
		}
		frontier = next
		if len(frontier) == 0 {
			break
		}
	}
	return visited
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
