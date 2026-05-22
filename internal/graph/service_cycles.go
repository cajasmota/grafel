// Package graph — service_cycles.go implements strongly-connected-component
// (SCC) detection over a SERVICE-level (repo-level) graph.
//
// Per-entity import-cycle detection (cycles.go) cannot see cross-service
// coupling that flows THROUGH synthetic boundary nodes: an HTTP `FETCHES` edge
// lands on an endpoint-synthetic node, and a Kafka publish/subscribe pair is
// mediated by a topic node. As a result two services that mutually depend on
// each other (orders calls payments over REST while payments calls back over a
// Kafka topic) never share a direct entity→entity edge, so Tarjan over IMPORTS
// finds nothing.
//
// The cross-repo link passes (internal/links P1-P7) already resolve those
// boundaries into direct service→service edges (`<repo>::<id>` source/target,
// relation calls / imports / publishes_to). FindServiceCycles aggregates those
// resolved links into a directed service graph and runs Tarjan SCC over it,
// reporting every SCC of size >= 2 as a service-level circular dependency.
package graph

import "sort"

// ServiceEdge is a directed service→service edge, aggregated from one or more
// cross-repo links. Relations records the distinct relation labels (calls,
// imports, publishes_to, ...) that contributed to this edge, for explainability.
type ServiceEdge struct {
	From      string   `json:"from"`
	To        string   `json:"to"`
	Relations []string `json:"relations,omitempty"`
}

// ServiceCycle describes one strongly-connected component of size >= 2 in the
// service-level graph.
type ServiceCycle struct {
	// Members lists service (repo) names that form the cycle, sorted.
	Members []string `json:"members"`
	// Edges lists the directed service→service edges internal to the cycle,
	// sorted for determinism.
	Edges []ServiceEdge `json:"edges"`
	// Size is len(Members), for fast filtering.
	Size int `json:"size"`
}

// ServiceLink is the minimal directed cross-service edge FindServiceCycles
// consumes. It is decoupled from the MCP CrossRepoLink / links.Link shapes so
// the graph package stays dependency-free; callers map their link type onto it.
type ServiceLink struct {
	FromService string
	ToService   string
	Relation    string
}

// directedRelations is the set of link relations that express a real DIRECTION
// of RUNTIME service coupling and may therefore participate in a service cycle.
//
//   - calls        consumer → producer (HTTP / gRPC FETCHES, resolved through
//     the endpoint-synthetic boundary node)
//   - publishes_to publisher → subscriber (Kafka/Redis topic, resolved through
//     the topic boundary node)
//
// Deliberately EXCLUDED:
//   - imports               cross-repo module imports are build-time coupling
//     (everything importing a shared lib, or a Python
//     service "importing" another), not service→service
//     runtime calls. Including them collapses unrelated
//     services into one giant SCC.
//   - shared_label /        UNDIRECTED co-occurrence signals (two services
//     string_match          merely reference the same label / string). Treating
//     them as edges manufactures spurious cycles.
var directedRelations = map[string]bool{
	"calls":        true,
	"publishes_to": true,
}

// IsDirectedServiceRelation reports whether a link relation expresses a
// directed dependency that may participate in a service-level cycle.
func IsDirectedServiceRelation(relation string) bool {
	return directedRelations[relation]
}

// FindServiceCycles aggregates directed cross-service links into a service
// graph and returns every strongly-connected component of size >= 2.
//
// Only links whose relation is a directed dependency (see directedRelations)
// are considered; undirected co-occurrence relations (shared_label,
// string_match) are ignored so they cannot fabricate cycles. Self-edges
// (FromService == ToService) are dropped.
//
// Results are sorted by descending size, then by first member name.
func FindServiceCycles(links []ServiceLink) []ServiceCycle {
	// Aggregate links into a deduplicated directed service adjacency map,
	// tracking the contributing relation labels per edge.
	type edgeKey struct{ from, to string }
	adj := make(map[string]map[string]bool)
	relsByEdge := make(map[edgeKey]map[string]bool)

	for _, l := range links {
		if !directedRelations[l.Relation] {
			continue
		}
		if l.FromService == "" || l.ToService == "" {
			continue
		}
		if l.FromService == l.ToService {
			continue // self-coupling is not a cycle
		}
		if adj[l.FromService] == nil {
			adj[l.FromService] = make(map[string]bool)
		}
		adj[l.FromService][l.ToService] = true

		k := edgeKey{l.FromService, l.ToService}
		if relsByEdge[k] == nil {
			relsByEdge[k] = make(map[string]bool)
		}
		relsByEdge[k][l.Relation] = true
	}

	if len(adj) == 0 {
		return nil
	}

	// Build sorted adjacency lists for deterministic traversal.
	adjList := make(map[string][]string, len(adj))
	allNodes := make(map[string]bool)
	for u, vs := range adj {
		allNodes[u] = true
		list := make([]string, 0, len(vs))
		for v := range vs {
			list = append(list, v)
			allNodes[v] = true
		}
		sort.Strings(list)
		adjList[u] = list
	}

	sccs := tarjanSCC(allNodes, adjList)

	var cycles []ServiceCycle
	for _, scc := range sccs {
		if len(scc) < 2 {
			continue
		}
		member := make(map[string]bool, len(scc))
		for _, m := range scc {
			member[m] = true
		}
		var edges []ServiceEdge
		for _, u := range scc {
			for _, v := range adjList[u] {
				if !member[v] {
					continue
				}
				relSet := relsByEdge[edgeKey{u, v}]
				rels := make([]string, 0, len(relSet))
				for r := range relSet {
					rels = append(rels, r)
				}
				sort.Strings(rels)
				edges = append(edges, ServiceEdge{From: u, To: v, Relations: rels})
			}
		}
		sort.Strings(scc)
		sort.Slice(edges, func(i, j int) bool {
			if edges[i].From != edges[j].From {
				return edges[i].From < edges[j].From
			}
			return edges[i].To < edges[j].To
		})
		cycles = append(cycles, ServiceCycle{
			Members: scc,
			Edges:   edges,
			Size:    len(scc),
		})
	}

	sort.SliceStable(cycles, func(i, j int) bool {
		if cycles[i].Size != cycles[j].Size {
			return cycles[i].Size > cycles[j].Size
		}
		mi, mj := "", ""
		if len(cycles[i].Members) > 0 {
			mi = cycles[i].Members[0]
		}
		if len(cycles[j].Members) > 0 {
			mj = cycles[j].Members[0]
		}
		return mi < mj
	})
	return cycles
}

// tarjanSCC runs Tarjan's iterative strongly-connected-components algorithm
// over the directed graph described by nodes + sorted adjacency lists. It
// returns one slice per SCC. Iteration is deterministic given sorted input.
func tarjanSCC(nodes map[string]bool, adj map[string][]string) [][]string {
	type nodeState struct {
		index   int
		lowlink int
		onStack bool
	}
	state := make(map[string]*nodeState, len(nodes))
	var stack []string
	index := 0
	var sccs [][]string

	type frame struct {
		u string
		i int
	}

	sorted := make([]string, 0, len(nodes))
	for n := range nodes {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)

	var dfs []frame
	for _, start := range sorted {
		if state[start] != nil {
			continue
		}
		state[start] = &nodeState{index: index, lowlink: index}
		index++
		stack = append(stack, start)
		state[start].onStack = true
		dfs = append(dfs, frame{u: start})

		for len(dfs) > 0 {
			top := &dfs[len(dfs)-1]
			u := top.u
			neighbours := adj[u]
			if top.i < len(neighbours) {
				v := neighbours[top.i]
				top.i++
				if sv := state[v]; sv == nil {
					state[v] = &nodeState{index: index, lowlink: index}
					index++
					stack = append(stack, v)
					state[v].onStack = true
					dfs = append(dfs, frame{u: v})
				} else if sv.onStack {
					if sv.index < state[u].lowlink {
						state[u].lowlink = sv.index
					}
				}
				continue
			}

			dfs = dfs[:len(dfs)-1]
			su := state[u]
			if len(dfs) > 0 {
				parent := dfs[len(dfs)-1].u
				sp := state[parent]
				if su.lowlink < sp.lowlink {
					sp.lowlink = su.lowlink
				}
			}
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
	return sccs
}
