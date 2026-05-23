// Process-flow DAG walker (#1945 Phase 1).
//
// The original `bfsTraces` walker projected a branched call tree into
// multiple LINEAR chains sharing a prefix. That collapsed real branches
// (if/else, try/catch, Promise.all, early returns) into 3-5× inflated
// linear flows. Phase 1 replaces that projection with a true DAG: one
// walk per entry, branches captured as child `ChainStep` nodes.
//
// Phase 1 scope:
//   - Walker emits the DAG shape (this file).
//   - `RunProcessFlowWithCompanions` derives a "primary path" (leftmost
//     depth-first) for backward-compatible persistence of the `chain` /
//     `chain_labels` properties + STEP_IN_PROCESS edges. Existing
//     consumers (dashboard flows.tsx, JARVIS comet, MCP traces) ignore
//     Branches and keep working.
//   - New properties on the Process entity encode the full DAG:
//       branch_count           — number of internal fan-out points
//       dag_node_count         — total unique nodes reached
//       is_dag                 — "true" iff any node has >1 children
//       branches_dag           — JSON-serialised DAG (root ChainStep)
//
// Phase 2 (separate ticket) will populate `Reason` from per-language
// control-flow extraction (if/try/switch/Promise.all). Phase 1 leaves
// Reason empty.

package engine

import (
	"encoding/json"
	"sort"
	"strconv"

	"github.com/cajasmota/archigraph/internal/graph"
)

// ChainStep is one node in a process-flow DAG. Linear chains have
// Branches == nil. Fan-out points (a node with >1 outgoing CALLS that
// the walker decided to keep) populate Branches with one ChainStep per
// child. Existing consumers can ignore Branches and walk only the first
// step — the leftmost branch is the canonical "primary path".
//
// `Reason` is reserved for Phase 2 control-flow extraction
// ("if" / "try" / "switch" / "promise_all"). Phase 1 always leaves it
// empty.
type ChainStep struct {
	EntityID  string       `json:"entity_id"`
	NodeID    string       `json:"node_id"`
	StepIndex int          `json:"step_index"`
	Name      string       `json:"name,omitempty"`
	File      string       `json:"file,omitempty"`
	Line      int          `json:"line,omitempty"`
	Repo      string       `json:"repo,omitempty"`
	Branches  []*ChainStep `json:"branches,omitempty"`
	Reason    string       `json:"reason,omitempty"`
}

// dagBuildResult captures everything the caller needs after one DAG
// walk: the root step, plus telemetry it can fold into processStats.
type dagBuildResult struct {
	Root            *ChainStep
	NodeCount       int
	BranchCount     int  // internal fan-out points (steps with len(Branches) > 1)
	DepthTruncated  int  // branches dropped because depth cap was hit
	FanoutTruncated int  // branches dropped because fan-out cap was hit
	NodeCapHit      bool // expansion stopped because dag-node cap was reached
}

// dagWalkConfig is the subset of ProcessFlowConfig relevant to the DAG
// walker, with Phase-1 specific bounds added.
type dagWalkConfig struct {
	MaxDepth        int // depth cap (cfg.MaxDepth)
	BranchingFactor int // per-step fan-out cap (cfg.BranchingFactor)
	MaxNodes        int // total nodes per DAG (Phase-1 cap = 50)
}

// defaultDAGMaxNodes is the per-flow node-count cap from #1945 Phase 1.
// Caps DAG explosion in dense call graphs.
const defaultDAGMaxNodes = 50

// branchOverflowSuffix is appended to the EntityID of the sentinel
// ChainStep that replaces dropped branches when the fan-out cap is hit.
// (`+N more`). Stable suffix so the JSON encoding is deterministic.
const branchOverflowEntityID = "__overflow__"

// buildFlowDAG walks the CALLS adjacency forward from `entry` and
// returns a single DAG rooted at the entry. All outgoing edges are
// walked (not just the first), bounded by:
//
//   - cfg.MaxDepth        — depth cap (root counts as depth 1)
//   - cfg.BranchingFactor — per-node fan-out cap; beyond N a synthetic
//     "+N more" sentinel step is appended
//   - cfg.MaxNodes        — total unique nodes per DAG (Phase-1 = 50)
//   - cycle detection     — a node already on the current ancestor path
//     is not re-expanded (matches the linear walker's cycle gate)
//
// Determinism: outgoing edges are sorted by target ID before expansion,
// matching the existing linear walker's contract.
func buildFlowDAG(entry string, adj *callsAdjacency, byID map[string]*graph.Entity, cfg dagWalkConfig) dagBuildResult {
	res := dagBuildResult{}
	if entry == "" || adj == nil {
		return res
	}
	if cfg.MaxDepth <= 0 {
		cfg.MaxDepth = 10
	}
	if cfg.BranchingFactor <= 0 {
		cfg.BranchingFactor = 4
	}
	if cfg.MaxNodes <= 0 {
		cfg.MaxNodes = defaultDAGMaxNodes
	}

	// visited tracks every node currently included anywhere in the DAG
	// (across all branches). This is the global node-count cap. Cycle
	// detection uses the per-path ancestor set passed into expand().
	visited := map[string]bool{entry: true}

	root := newChainStep(entry, 0, byID)
	res.Root = root
	res.NodeCount = 1

	// expand walks one node's children. ancestors is the set of node IDs
	// currently on the path from root → this node (inclusive). depth is
	// 1-based so a single-node DAG has depth 1.
	var expand func(node *ChainStep, ancestors map[string]bool, depth int)
	expand = func(node *ChainStep, ancestors map[string]bool, depth int) {
		if depth >= cfg.MaxDepth {
			// Past the depth cap. Count outgoing-but-dropped children
			// against DepthTruncated so stats stay comparable with the
			// linear walker.
			if n := len(adj.out[node.EntityID]); n > 0 {
				res.DepthTruncated += n
			}
			return
		}
		neighbors := adj.out[node.EntityID]
		if len(neighbors) == 0 {
			return
		}
		sortedN := append([]string(nil), neighbors...)
		sort.Strings(sortedN)

		// Cycle filter: drop neighbors already on the current ancestor
		// path. Preserves the linear walker's cycle invariant.
		filtered := sortedN[:0]
		// Use a fresh slice so we don't alias sortedN above.
		filtered = make([]string, 0, len(sortedN))
		for _, nb := range sortedN {
			if ancestors[nb] {
				continue
			}
			filtered = append(filtered, nb)
		}

		overflow := 0
		if len(filtered) > cfg.BranchingFactor {
			overflow = len(filtered) - cfg.BranchingFactor
			res.FanoutTruncated += overflow
			filtered = filtered[:cfg.BranchingFactor]
		}

		// Track which children we actually add so Branches stays in a
		// deterministic order.
		for i, nb := range filtered {
			if res.NodeCount >= cfg.MaxNodes {
				res.NodeCapHit = true
				// Count any further dropped edges (including the ones
				// we'd have expanded here) so the stats are honest.
				res.DepthTruncated += len(filtered) - i + overflow
				overflow = 0
				break
			}
			// A child can appear multiple times in the DAG if reached
			// from multiple parents (true DAG, not a tree). We add it
			// as a fresh ChainStep node every time so the tree shape
			// is unambiguous, but visited-tracking still counts unique
			// EntityIDs for the global cap.
			child := newChainStep(nb, depth, byID)
			node.Branches = append(node.Branches, child)
			if !visited[nb] {
				visited[nb] = true
				res.NodeCount++
			}
			// Recurse with extended ancestor set.
			nextAncestors := make(map[string]bool, len(ancestors)+1)
			for k := range ancestors {
				nextAncestors[k] = true
			}
			nextAncestors[nb] = true
			expand(child, nextAncestors, depth+1)
		}

		// Append "+N more" sentinel if we dropped branches to the
		// fan-out cap. Sentinel is a leaf (no Branches) and does not
		// count against MaxNodes (it isn't a real entity).
		if overflow > 0 {
			sentinel := &ChainStep{
				EntityID:  branchOverflowEntityID,
				NodeID:    branchOverflowEntityID,
				StepIndex: depth, // sentinel sits at the child depth
				Name:      "+" + strconv.Itoa(overflow) + " more",
				Reason:    "fanout_cap",
			}
			node.Branches = append(node.Branches, sentinel)
		}

		// Tally internal fan-out (>1 real children, excluding the
		// sentinel since it's a synthetic placeholder).
		realKids := 0
		for _, b := range node.Branches {
			if b.EntityID != branchOverflowEntityID {
				realKids++
			}
		}
		if realKids > 1 {
			res.BranchCount++
		}
	}

	initAncestors := map[string]bool{entry: true}
	expand(root, initAncestors, 1)

	return res
}

// newChainStep materialises a ChainStep for a given entity ID, looking
// up metadata in byID. Missing entities yield a step with only EntityID
// / NodeID set (this is the phantom-cross-repo-target case).
func newChainStep(id string, stepIndex int, byID map[string]*graph.Entity) *ChainStep {
	s := &ChainStep{
		EntityID:  id,
		NodeID:    id,
		StepIndex: stepIndex,
	}
	if e, ok := byID[id]; ok && e != nil {
		s.Name = e.Name
		s.File = e.SourceFile
		s.Line = e.StartLine
	}
	return s
}

// primaryPath returns the leftmost depth-first path through the DAG.
// This is the backward-compatible "linear chain" we still persist on
// the Process entity so existing consumers (dashboard, MCP traces,
// JARVIS) keep working unchanged. Stops at the first leaf or at the
// overflow sentinel (which is never a real entity).
func primaryPath(root *ChainStep) []string {
	if root == nil {
		return nil
	}
	out := []string{root.EntityID}
	cur := root
	for len(cur.Branches) > 0 {
		next := cur.Branches[0]
		if next.EntityID == branchOverflowEntityID {
			break
		}
		out = append(out, next.EntityID)
		cur = next
	}
	return out
}

// encodeDAGJSON marshals the DAG to a compact JSON blob suitable for
// stashing on the Process entity's Properties map. Errors are swallowed:
// the DAG fields are advisory metadata, not load-bearing semantics, and
// the linear chain remains the authoritative timeline.
func encodeDAGJSON(root *ChainStep) string {
	if root == nil {
		return ""
	}
	b, err := json.Marshal(root)
	if err != nil {
		return ""
	}
	return string(b)
}

