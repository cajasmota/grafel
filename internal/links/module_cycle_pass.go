// Phase 3B module-cycle detection pass (#2774).
//
// Tarjan strongly-connected-components over IMPORTS edges. Surfaces
// every non-trivial cycle (SCC of size >= 2) as a ModuleCycle record.
// Language-agnostic — the IMPORTS edge graph is the same shape across
// every T1 language because the per-language extractors emit a common
// edge kind.
//
// Storage model: in-memory annotation of every entity that participates
// in a cycle with the property `module_cycle_id` (the index of the
// cycle in the sidecar). The persistent <group>-links-module-cycles.json
// sidecar surfaces the full cycle members + size for the MCP
// archigraph_module_cycles tool.
package links

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MethodModuleCycles identifies sidecar artefacts from this pass.
const MethodModuleCycles = "module_cycles"

// ModuleCyclePropertyKey is the stable name under which the pass stamps
// the cycle membership tag onto entity properties.
const ModuleCyclePropertyKey = "module_cycle_id"

// moduleCycle is one strongly-connected component of size >= 2 over the
// IMPORTS edge graph. The Members list is sorted (repo, id) for stable
// output across runs.
type moduleCycle struct {
	ID      int                 `json:"id"`
	Size    int                 `json:"size"`
	Members []moduleCycleMember `json:"members"`
}

type moduleCycleMember struct {
	Repo       string `json:"repo"`
	EntityID   string `json:"entity_id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	SourceFile string `json:"source_file,omitempty"`
}

// moduleCycleDocument is the on-disk shape of <group>-links-module-cycles.json.
type moduleCycleDocument struct {
	Version    int           `json:"version"`
	Method     string        `json:"method"`
	TotalNodes int           `json:"total_nodes"`
	TotalEdges int           `json:"total_edges"`
	Cycles     []moduleCycle `json:"cycles"`
}

// runModuleCyclePass enumerates SCCs over the IMPORTS edges aggregated
// across every repo in the group. The cross-repo IMPORTS edges emitted
// by the link pipeline are honoured by reading both per-repo edges
// (FromID/ToID local) and any persisted cross-repo links of relation
// `imports` from the same RunAllPasses invocation — for Phase 3B we
// keep the analysis intra-repo only (per-repo SCCs), which matches the
// classical Tarjan-on-imports use-case ("circular dependency cluster
// inside this repo's module tree").
//
// Future cross-repo cycles can be lifted by feeding the cross-repo
// links.json entries with relation=imports into the same algorithm;
// out of Phase 3B scope.
func runModuleCyclePass(graphs []repoGraph, paths Paths) (PassResult, error) {
	res := PassResult{Pass: "module_cycles"}

	var cycles []moduleCycle
	totalNodes := 0
	totalEdges := 0
	cycleID := 0

	for ri := range graphs {
		g := &graphs[ri]

		// Build the adjacency: nodeID -> sorted set of nodeIDs reachable
		// via an IMPORTS edge. We use entity IDs as nodes since the
		// extractor emits a stable per-file/per-module entity that owns
		// the IMPORTS outbound edges.
		nodeSet := map[string]bool{}
		adj := map[string][]string{}
		for _, e := range g.Edges {
			if !strings.EqualFold(e.Kind, "IMPORTS") && e.Kind != "imports" {
				continue
			}
			adj[e.FromID] = append(adj[e.FromID], e.ToID)
			nodeSet[e.FromID] = true
			nodeSet[e.ToID] = true
			totalEdges++
		}
		totalNodes += len(nodeSet)
		if len(nodeSet) == 0 {
			continue
		}

		nodes := make([]string, 0, len(nodeSet))
		for n := range nodeSet {
			nodes = append(nodes, n)
		}
		sort.Strings(nodes)

		// Tarjan's algorithm. Indexed iterative-friendly form — but a
		// recursive form is cleaner and the call depth is bounded by
		// the per-repo IMPORTS graph diameter (well under Go's stack).
		state := newTarjanState(adj)
		for _, n := range nodes {
			if _, ok := state.index[n]; !ok {
				state.strongConnect(n)
			}
		}

		// Surface non-trivial SCCs (size >= 2) only. Single-node SCCs
		// without a self-edge are not cycles; single-node SCCs with a
		// self-edge are technically cycles but almost always recursion
		// noise (Phase 3B targets module-graph cycles, not function-
		// recursion analysis).
		byID := make(map[string]*entityNode, len(g.Entities))
		for ei := range g.Entities {
			byID[g.Entities[ei].ID] = &g.Entities[ei]
		}
		for _, scc := range state.sccs {
			if len(scc) < 2 {
				continue
			}
			cycleID++
			tag := fmt.Sprintf("%s/%d", g.Repo, cycleID)
			mem := make([]moduleCycleMember, 0, len(scc))
			ids := append([]string(nil), scc...)
			sort.Strings(ids)
			for _, id := range ids {
				e := byID[id]
				if e == nil {
					continue
				}
				if e.Properties == nil {
					e.Properties = map[string]string{}
				}
				e.Properties[ModuleCyclePropertyKey] = tag
				mem = append(mem, moduleCycleMember{
					Repo:       g.Repo,
					EntityID:   id,
					Name:       e.Name,
					Kind:       e.Kind,
					SourceFile: e.SourceFile,
				})
			}
			cycles = append(cycles, moduleCycle{
				ID:      cycleID,
				Size:    len(mem),
				Members: mem,
			})
		}
	}

	res.LinksAdded = len(cycles)
	res.Candidates = totalNodes
	res.Skipped = totalEdges

	if paths.Links == "" {
		return res, nil
	}
	sidecar := trimSuffix(paths.Links, ".json") + "-module-cycles.json"
	doc := moduleCycleDocument{
		Version:    1,
		Method:     MethodModuleCycles,
		TotalNodes: totalNodes,
		TotalEdges: totalEdges,
		Cycles:     cycles,
	}
	buf, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return res, err
	}
	if err := os.MkdirAll(filepath.Dir(sidecar), 0o755); err != nil {
		return res, err
	}
	if err := os.WriteFile(sidecar, buf, 0o644); err != nil {
		return res, fmt.Errorf("write module-cycles doc: %w", err)
	}
	return res, nil
}

// tarjanState is the per-graph book-keeping for Tarjan's algorithm. The
// recursive strongConnect closure operates on the receiver, populating
// `sccs` with every component on its way back up the DFS stack.
type tarjanState struct {
	adj     map[string][]string
	index   map[string]int
	lowlink map[string]int
	onStack map[string]bool
	stack   []string
	counter int
	sccs    [][]string
}

func newTarjanState(adj map[string][]string) *tarjanState {
	return &tarjanState{
		adj:     adj,
		index:   map[string]int{},
		lowlink: map[string]int{},
		onStack: map[string]bool{},
	}
}

func (t *tarjanState) strongConnect(v string) {
	t.index[v] = t.counter
	t.lowlink[v] = t.counter
	t.counter++
	t.stack = append(t.stack, v)
	t.onStack[v] = true

	for _, w := range t.adj[v] {
		if _, ok := t.index[w]; !ok {
			t.strongConnect(w)
			if t.lowlink[w] < t.lowlink[v] {
				t.lowlink[v] = t.lowlink[w]
			}
		} else if t.onStack[w] {
			if t.index[w] < t.lowlink[v] {
				t.lowlink[v] = t.index[w]
			}
		}
	}

	if t.lowlink[v] == t.index[v] {
		var scc []string
		for {
			n := len(t.stack) - 1
			w := t.stack[n]
			t.stack = t.stack[:n]
			t.onStack[w] = false
			scc = append(scc, w)
			if w == v {
				break
			}
		}
		t.sccs = append(t.sccs, scc)
	}
}
