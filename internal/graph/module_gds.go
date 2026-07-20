// Package graph — module_gds.go implements module-LEVEL graph data-science
// algorithms (issue #1384, part of epic #1380).
//
// # Why
//
// algorithms.go runs SCC / PageRank / betweenness over the ENTITY graph
// (one node per function / class / file). On a real-world codebase that
// produces tens of thousands of nodes and the high-centrality picture is
// dominated by widely-shared utility entities. The strategic question the
// user actually has — "which MODULES form a cycle? which MODULE is the
// blast-radius hub?" — needs a bird's-eye view at module granularity.
//
// # Aggregated module graph
//
// We collapse the entity graph onto modules:
//
//	for each entity-level edge A → B with kind ∈ realKinds:
//	    let mA = module(A), mB = module(B)
//	    if mA == mB:           skip (intra-module: not a module-edge)
//	    if mA == "" || mB == "": skip (untagged: would pollute results)
//	    accumulate edgeWeight[mA → mB] += 1
//
// "module" is read from Entity.Properties["module"] when present; if the
// document also carries pre-baked synthetic Module containers (the post-#1383
// shape — Kind="Module", DEPENDS_ON edges between them with weight) we use
// those directly to avoid recomputing. realKinds excludes the synthetic
// scaffolding kinds (CONTAINS, STEP_IN_PROCESS, ENTRY_POINT_OF, DEPENDS_ON
// when between module nodes — those are themselves the aggregated edges).
//
// # Outputs
//
//   - ModuleSCC: strongly-connected components of size >= 2 in the module
//     graph. These are the circular module dependencies. Tarjan iterative.
//   - PageRank + betweenness: module-level centrality scores. The top-N are
//     the modules everything else depends on (PageRank) and the modules that
//     sit on the most shortest paths (betweenness — bottlenecks).
//
// # Determinism
//
// Same input ⇒ same output. Adjacency lists are sorted, Tarjan walks nodes
// in sorted order, ties in score-ranked output are tiebroken on module ID.
// Scores are rounded to 4 decimal places (same policy as algorithms.go) so
// minor solver-tolerance noise does not flip the top-N membership across runs.
package graph

import (
	"sort"

	"gonum.org/v1/gonum/graph/network"
	"gonum.org/v1/gonum/graph/path"
	"gonum.org/v1/gonum/graph/simple"
)

// Module-graph synthetic kinds that must NOT be re-aggregated. See package
// doc for the rationale.
const (
	kindModule     = "Module"
	relContains    = "CONTAINS"
	relDependsOn   = "DEPENDS_ON"
	relStepIn      = "STEP_IN_PROCESS"
	relEntryPoint  = "ENTRY_POINT_OF"
	moduleExternal = "_external" // synthetic catch-all from internal/module aggregator
)

// ModuleEdge is a directed aggregated edge between two module IDs.
type ModuleEdge struct {
	FromModule string `json:"from_module"`
	ToModule   string `json:"to_module"`
	// Weight is the count of underlying entity-edges that collapsed onto
	// this module-pair (or, when DEPENDS_ON edges already carry a weight
	// property, that pre-baked count).
	Weight int `json:"weight"`
}

// ModuleSCC is one strongly-connected component of size >= 2 in the
// aggregated module graph.
type ModuleSCC struct {
	// ID is a deterministic integer assigned to the SCC for cross-reference.
	// Stable across runs of the same input (Tarjan-discovery order over the
	// sorted node set).
	ID int `json:"id"`
	// Members lists module IDs participating in the cycle, sorted.
	Members []string `json:"members"`
	// MemberNames mirrors Members but carries the human-readable module name
	// (Module entity.Name when available; falls back to the ID).
	MemberNames []string `json:"member_names"`
	// Edges are the directed module→module edges INTERNAL to the SCC, sorted.
	Edges []ModuleEdge `json:"edges"`
	// Size is len(Members), for fast filtering.
	Size int `json:"size"`
}

// ModuleCentrality is one module's score pair.
type ModuleCentrality struct {
	ModuleID    string  `json:"module_id"`
	ModuleName  string  `json:"module_name"`
	PageRank    float64 `json:"pagerank"`
	Betweenness float64 `json:"betweenness"`
	// InDegree / OutDegree are the unweighted node-degrees in the module
	// graph — cheap sanity-check metrics that often correlate with PageRank
	// but with very different units, useful for hub vs. fan-out distinction.
	InDegree  int `json:"in_degree"`
	OutDegree int `json:"out_degree"`
}

// ModuleAlgorithmResults bundles every output of RunModuleAlgorithms.
type ModuleAlgorithmResults struct {
	// ModuleIDs is the deterministic sorted list of all module IDs present
	// in the aggregated graph (including singletons with no inbound/outbound
	// edges, when they're present in the input as Module entities).
	ModuleIDs []string `json:"module_ids"`
	// ModuleNames maps module ID → human-readable name. Always populated.
	ModuleNames map[string]string `json:"module_names"`
	// Edges is the full set of aggregated module→module edges, sorted.
	Edges []ModuleEdge `json:"edges"`
	// SCCs are the strongly-connected components of size >= 2.
	SCCs []ModuleSCC `json:"sccs"`
	// SCCOf maps module ID → SCC ID; -1 when the module is not in any
	// non-trivial SCC. Always populated for every module ID.
	SCCOf map[string]int `json:"scc_of"`
	// Centrality is per-module PageRank + betweenness, sorted by ID.
	Centrality []ModuleCentrality `json:"centrality"`
	// Stats are corpus-level summaries.
	Stats ModuleAlgorithmStats `json:"stats"`
}

// ModuleAlgorithmStats are the corpus-level summary numbers.
type ModuleAlgorithmStats struct {
	NumModules        int `json:"num_modules"`
	NumModuleEdges    int `json:"num_module_edges"`
	NumSCCs           int `json:"num_sccs"`
	LargestSCCSize    int `json:"largest_scc_size"`
	NumModulesInCycle int `json:"num_modules_in_cycle"`
}

// RunModuleAlgorithms is the top-level entry point. It builds the aggregated
// module graph from (entities, rels) and runs SCC + PageRank + betweenness
// over it.
//
// Inputs:
//
//   - entities: the full Document.Entities slice. Both Module container
//     entities (Kind="Module") and regular entities (carrying
//     Properties["module"]) are accepted; the function reconciles them.
//   - rels:     the full Document.Relationships slice. CONTAINS, STEP_IN_PROCESS
//     and ENTRY_POINT_OF edges are dropped (they are scaffolding, not real
//     dependencies). DEPENDS_ON edges BETWEEN two Module nodes are taken as
//     pre-aggregated module-level edges (with their "weight" property when
//     present); all other edges are aggregated by collapsing their endpoints
//     onto their respective modules.
//
// Returns a populated *ModuleAlgorithmResults. Never returns nil — an empty
// graph produces a zero-stats result with empty slices.
func RunModuleAlgorithms(entities []Entity, rels []Relationship) *ModuleAlgorithmResults {
	moduleIDs, moduleNames, entityToModule := collectModules(entities)
	edges := aggregateModuleEdges(entities, rels, entityToModule, moduleIDs)

	res := &ModuleAlgorithmResults{
		ModuleIDs:   moduleIDs,
		ModuleNames: moduleNames,
		Edges:       edges,
		SCCOf:       make(map[string]int, len(moduleIDs)),
	}

	if len(moduleIDs) == 0 {
		return res
	}

	// Initialise SCCOf to -1 for every module before SCC pass.
	for _, m := range moduleIDs {
		res.SCCOf[m] = -1
	}

	// SCC pass (size >= 2). Returns SCCs sorted by descending size.
	sccs := findModuleSCCs(moduleIDs, edges)
	for _, c := range sccs {
		for _, m := range c.Members {
			res.SCCOf[m] = c.ID
		}
	}
	// Enrich SCCs with names.
	for i := range sccs {
		sccs[i].MemberNames = lookupNames(sccs[i].Members, moduleNames)
	}
	res.SCCs = sccs

	// Centrality pass via gonum on the same aggregated graph.
	res.Centrality = computeModuleCentrality(moduleIDs, edges, moduleNames)

	// Stats summary.
	res.Stats = ModuleAlgorithmStats{
		NumModules:     len(moduleIDs),
		NumModuleEdges: len(edges),
		NumSCCs:        len(sccs),
	}
	for _, c := range sccs {
		if c.Size > res.Stats.LargestSCCSize {
			res.Stats.LargestSCCSize = c.Size
		}
		res.Stats.NumModulesInCycle += c.Size
	}
	return res
}

// collectModules walks the entity set and returns:
//
//   - the sorted deduplicated list of module IDs;
//   - module ID → display name map;
//   - per-entity-ID → module ID map (for entities that are not themselves
//     Module containers).
//
// Module identity strategy: if a Module-kind container exists for a given
// (repo, module) pair, its entity ID is used as the module ID and its Name
// is used as the display name. Otherwise the module name itself (e.g.
// "core/views") doubles as the module ID. This keeps post-#1383 documents
// (with pre-baked Module containers) and pre-aggregation documents working
// from the same surface.
func collectModules(entities []Entity) (ids []string, names map[string]string, entityToModule map[string]string) {
	names = map[string]string{}
	entityToModule = map[string]string{}

	// First pass: pick up Module containers (Kind="Module").
	// We build two indexes so we can match real entities back to their
	// container even when only the module name is on Properties["module"]:
	//
	//   - containerByLabel:   "(repo)|(name)" → ID (precise, used when both
	//                         endpoints carry a repo tag — the multi-repo
	//                         group document case).
	//   - containerByName:    "name" → ID (fallback, used when the real
	//                         entity has no Properties["repo"] — single-repo
	//                         documents written by the indexer, where the
	//                         module-aggregator stamps "repo" on Module
	//                         entities only).
	//
	// containerByName collides when two repos have a module with the same
	// name; in that case we still prefer the precise label match, and only
	// fall back to the name-only index when nothing else matches.
	containerByLabel := map[string]string{}
	containerByName := map[string]string{}
	containerByNameAmbiguous := map[string]bool{}
	idSet := map[string]bool{}
	for i := range entities {
		e := &entities[i]
		if e.Kind != kindModule {
			continue
		}
		mid := e.ID
		idSet[mid] = true
		names[mid] = e.Name
		repo := ""
		modName := e.Name
		if e.PropLen() > 0 {
			if r, ok := e.PropLookup("repo"); ok {
				repo = r
			}
			if m, ok := e.PropLookup("module"); ok && m != "" {
				modName = m
			}
		}
		containerByLabel[repo+"|"+modName] = mid
		if existing, ok := containerByName[modName]; ok && existing != mid {
			containerByNameAmbiguous[modName] = true
		}
		containerByName[modName] = mid
	}

	// Second pass: real entities. Resolve each to its module ID.
	for i := range entities {
		e := &entities[i]
		if e.Kind == kindModule {
			continue
		}
		modName := ""
		repo := ""
		if e.PropLen() > 0 {
			modName = e.PropGet("module")
			repo = e.PropGet("repo")
		}
		if modName == "" {
			// Untagged entity — skip from aggregation. Including it would
			// pollute the synthetic "_external" bucket with everything that
			// happens to lack a module label, which is not signal.
			continue
		}
		// Three-tier lookup:
		//   1. Exact (repo, name) match — multi-repo group documents.
		//   2. Name-only match when the name is unambiguous — single-repo
		//      documents (real entities have no "repo" property; containers do).
		//   3. Synthesised string-ID fallback — pre-#1383 documents with no
		//      Module containers at all.
		mid, ok := containerByLabel[repo+"|"+modName]
		if !ok && !containerByNameAmbiguous[modName] {
			if v, ok2 := containerByName[modName]; ok2 {
				mid = v
				ok = true
			}
		}
		if !ok {
			mid = modName
			if !idSet[mid] {
				idSet[mid] = true
				names[mid] = modName
			}
		}
		entityToModule[e.ID] = mid
	}

	// Drop the synthetic "_external" bucket — it aggregates "everything we
	// failed to label" and is never useful signal for SCC / centrality.
	for mid, name := range names {
		if name == moduleExternal {
			delete(idSet, mid)
			delete(names, mid)
		}
	}
	for eid, mid := range entityToModule {
		if names[mid] == "" && idSet[mid] == false {
			delete(entityToModule, eid)
		}
	}

	ids = make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, names, entityToModule
}

// aggregateModuleEdges produces the directed module-level edge multiset.
//
// Pre-aggregated DEPENDS_ON edges (when both endpoints are Module containers)
// are taken at face value: their "weight" property is parsed if present, else
// defaults to 1. All other relationships are collapsed by looking up each
// endpoint's module; self-loops (intra-module) are dropped.
//
// Excluded relationship kinds: CONTAINS (Module→entity scaffolding),
// STEP_IN_PROCESS / ENTRY_POINT_OF (process-flow scaffolding). They have no
// dependency semantics at module granularity.
func aggregateModuleEdges(_ []Entity, rels []Relationship, entityToModule map[string]string, moduleIDs []string) []ModuleEdge {
	moduleSet := make(map[string]bool, len(moduleIDs))
	for _, m := range moduleIDs {
		moduleSet[m] = true
	}

	type edgeKey struct{ from, to string }
	weight := map[edgeKey]int{}

	for i := range rels {
		r := &rels[i]
		switch r.Kind {
		case relContains, relStepIn, relEntryPoint:
			continue
		}
		if r.FromID == "" || r.ToID == "" || r.FromID == r.ToID {
			continue
		}

		// Pre-aggregated module→module edge?
		if r.Kind == relDependsOn && moduleSet[r.FromID] && moduleSet[r.ToID] {
			w := 1
			if r.PropLen() > 0 {
				if v, ok := r.PropLookup("weight"); ok {
					if n := atoiSafe(v); n > 0 {
						w = n
					}
				}
			}
			weight[edgeKey{r.FromID, r.ToID}] += w
			continue
		}

		// Regular entity-level edge — collapse onto module pair.
		fromMod, okF := entityToModule[r.FromID]
		toMod, okT := entityToModule[r.ToID]
		if !okF || !okT {
			continue
		}
		if fromMod == toMod {
			continue
		}
		weight[edgeKey{fromMod, toMod}]++
	}

	edges := make([]ModuleEdge, 0, len(weight))
	for k, w := range weight {
		edges = append(edges, ModuleEdge{FromModule: k.from, ToModule: k.to, Weight: w})
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].FromModule != edges[j].FromModule {
			return edges[i].FromModule < edges[j].FromModule
		}
		return edges[i].ToModule < edges[j].ToModule
	})
	return edges
}

// findModuleSCCs runs an iterative Tarjan SCC over the directed module graph
// described by (moduleIDs, edges) and returns every SCC of size >= 2, sorted
// by descending size with ID-tiebreak.
func findModuleSCCs(moduleIDs []string, edges []ModuleEdge) []ModuleSCC {
	if len(moduleIDs) == 0 || len(edges) == 0 {
		return nil
	}

	adj := make(map[string][]string, len(moduleIDs))
	for _, m := range moduleIDs {
		adj[m] = nil
	}
	for _, e := range edges {
		adj[e.FromModule] = append(adj[e.FromModule], e.ToModule)
	}
	// Sort adjacency lists for deterministic DFS.
	for k := range adj {
		sort.Strings(adj[k])
	}

	type nodeState struct {
		index   int
		lowlink int
		onStack bool
	}
	state := make(map[string]*nodeState, len(moduleIDs))
	var stack []string
	index := 0
	var rawSCCs [][]string

	type frame struct {
		u string
		i int
	}
	var dfsStack []frame

	// Iterate in sorted order — determinism, and ensures every node is touched.
	for _, start := range moduleIDs {
		if state[start] != nil {
			continue
		}
		state[start] = &nodeState{index: index, lowlink: index, onStack: true}
		index++
		stack = append(stack, start)
		dfsStack = append(dfsStack, frame{u: start})

		for len(dfsStack) > 0 {
			top := &dfsStack[len(dfsStack)-1]
			u := top.u
			neighbours := adj[u]

			if top.i < len(neighbours) {
				v := neighbours[top.i]
				top.i++
				sv := state[v]
				if sv == nil {
					state[v] = &nodeState{index: index, lowlink: index, onStack: true}
					index++
					stack = append(stack, v)
					dfsStack = append(dfsStack, frame{u: v})
				} else if sv.onStack {
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
				rawSCCs = append(rawSCCs, scc)
			}
		}
	}

	// Filter to size >= 2 and build ModuleSCC with internal edges + names.
	memberSetFor := func(members []string) map[string]bool {
		s := make(map[string]bool, len(members))
		for _, m := range members {
			s[m] = true
		}
		return s
	}
	weightOf := make(map[string]int, len(edges))
	for _, e := range edges {
		weightOf[e.FromModule+"\x00"+e.ToModule] = e.Weight
	}

	out := make([]ModuleSCC, 0)
	for _, members := range rawSCCs {
		if len(members) < 2 {
			continue
		}
		sort.Strings(members)
		set := memberSetFor(members)
		var internal []ModuleEdge
		for _, u := range members {
			for _, v := range adj[u] {
				if set[v] {
					internal = append(internal, ModuleEdge{
						FromModule: u,
						ToModule:   v,
						Weight:     weightOf[u+"\x00"+v],
					})
				}
			}
		}
		sort.Slice(internal, func(i, j int) bool {
			if internal[i].FromModule != internal[j].FromModule {
				return internal[i].FromModule < internal[j].FromModule
			}
			return internal[i].ToModule < internal[j].ToModule
		})
		out = append(out, ModuleSCC{
			Members: members,
			Edges:   internal,
			Size:    len(members),
		})
	}

	// Sort by descending size, then by first member ID ascending.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Size != out[j].Size {
			return out[i].Size > out[j].Size
		}
		return out[i].Members[0] < out[j].Members[0]
	})
	// Assign deterministic IDs after sorting.
	for i := range out {
		out[i].ID = i
	}
	return out
}

// computeModuleCentrality returns per-module PageRank + betweenness scores
// over the aggregated module graph. The graph is built on the fly as a
// gonum simple.WeightedDirectedGraph so we can reuse the same algorithms as
// the entity-level pass.
func computeModuleCentrality(moduleIDs []string, edges []ModuleEdge, names map[string]string) []ModuleCentrality {
	if len(moduleIDs) == 0 {
		return nil
	}
	// Build a stable string→int64 index.
	toInt := make(map[string]int64, len(moduleIDs))
	fromInt := make(map[int64]string, len(moduleIDs))
	g := simple.NewWeightedDirectedGraph(0, 0)
	for i, m := range moduleIDs {
		id := int64(i)
		toInt[m] = id
		fromInt[id] = m
		g.AddNode(simple.Node(id))
	}
	inDeg := make(map[string]int, len(moduleIDs))
	outDeg := make(map[string]int, len(moduleIDs))
	for _, e := range edges {
		from, okF := toInt[e.FromModule]
		to, okT := toInt[e.ToModule]
		if !okF || !okT || from == to {
			continue
		}
		w := float64(e.Weight)
		if w <= 0 {
			w = 1
		}
		if existing := g.WeightedEdge(from, to); existing != nil {
			w += existing.Weight()
			g.RemoveEdge(from, to)
		}
		g.SetWeightedEdge(g.NewWeightedEdge(simple.Node(from), simple.Node(to), w))
		outDeg[e.FromModule]++
		inDeg[e.ToModule]++
	}

	// Betweenness — modules are O(hundreds), so exact weighted is cheap.
	betw := map[int64]float64{}
	if shortest, ok := path.FloydWarshall(g); ok {
		betw = network.BetweennessWeighted(g, shortest)
	} else {
		betw = network.Betweenness(g)
	}
	// PageRank — sparse variant matches algorithms.go policy (#633).
	pr := network.PageRankSparse(g, 0.85, 1e-6)

	out := make([]ModuleCentrality, 0, len(moduleIDs))
	for _, m := range moduleIDs {
		nid := toInt[m]
		out = append(out, ModuleCentrality{
			ModuleID:    m,
			ModuleName:  fallbackName(m, names),
			PageRank:    roundForDeterminism(sanitizeFloat(pr[nid])),
			Betweenness: roundForDeterminism(sanitizeFloat(betw[nid])),
			InDegree:    inDeg[m],
			OutDegree:   outDeg[m],
		})
	}
	// Sort by module ID for stable enumeration; rank-ordered views are a
	// projection of this list (see TopByPageRank / TopByBetweenness).
	sort.Slice(out, func(i, j int) bool { return out[i].ModuleID < out[j].ModuleID })
	return out
}

// TopByPageRank returns the top-N centrality entries by PageRank, ties
// broken by ascending module ID. n<=0 returns the full list.
func TopByPageRank(cents []ModuleCentrality, n int) []ModuleCentrality {
	cp := make([]ModuleCentrality, len(cents))
	copy(cp, cents)
	sort.SliceStable(cp, func(i, j int) bool {
		if cp[i].PageRank != cp[j].PageRank {
			return cp[i].PageRank > cp[j].PageRank
		}
		return cp[i].ModuleID < cp[j].ModuleID
	})
	if n > 0 && n < len(cp) {
		cp = cp[:n]
	}
	return cp
}

// TopByBetweenness returns the top-N centrality entries by betweenness, ties
// broken by ascending module ID. n<=0 returns the full list.
func TopByBetweenness(cents []ModuleCentrality, n int) []ModuleCentrality {
	cp := make([]ModuleCentrality, len(cents))
	copy(cp, cents)
	sort.SliceStable(cp, func(i, j int) bool {
		if cp[i].Betweenness != cp[j].Betweenness {
			return cp[i].Betweenness > cp[j].Betweenness
		}
		return cp[i].ModuleID < cp[j].ModuleID
	})
	if n > 0 && n < len(cp) {
		cp = cp[:n]
	}
	return cp
}

func lookupNames(ids []string, names map[string]string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = fallbackName(id, names)
	}
	return out
}

func fallbackName(id string, names map[string]string) string {
	if n, ok := names[id]; ok && n != "" {
		return n
	}
	return id
}
