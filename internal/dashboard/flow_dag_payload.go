// flow_dag_payload.go — server-side FlowDag payload for the Flows view (#4363).
//
// The Flows-view rebuild (#4354) renders each selected process flow on the
// shared React Flow `<FlowDag>` component, which consumes a
// v2DownstreamDAGResponse (the same shape the v2 paths /downstream-dag endpoint
// produces, #4349). Historically the flows detail endpoint emitted only
// `steps[]` + a JSON `branches_dag` ChainStep tree, and the frontend bridged
// the gap client-side via lib/flow-to-dag.ts (flowToDagPayload) — a heuristic
// reshape that inferred roles from step_kind and edge kinds from edge_kind.
//
// #4363 moves that reshape to the backend so the frontend renders the server
// payload directly and the client adapter retires. The daemon has the richer
// edge semantics the flattened JSON tree dropped, so the server build yields
// first-class roles + the real branch structure.
//
// The payload is built from the SAME inputs the adapter used — the annotated
// steps + the persisted branches_dag ChainStep tree — and keeps the adapter's
// node-id scheme ("flow-step-<step_index>") so every id-based bit of frontend
// wiring (step selection, the replay comet/scrubber #4362, node-click) keeps
// working unchanged. Output is therefore equivalent to what flowToDagPayload
// produced for the same flow, plus the richer roles/edge-kinds/effects the
// resolved entities carry.
package dashboard

import (
	"encoding/json"
	"sort"
	"strings"
)

// flowDagStepKind names the data-sink step kinds that read as terminal
// collection nodes (the same set the client adapter's SINK_STEP_KINDS used).
var flowDagSinkStepKinds = map[string]bool{
	StepKindDBQuery:        true,
	StepKindDBWrite:        true,
	StepKindMessagePublish: true,
	StepKindMessageConsume: true,
	StepKindHTTPFetch:      true,
	StepKindExternalLib:    true,
}

// flowDagChainStep mirrors engine.ChainStep for decoding the persisted
// branches_dag JSON without importing the engine package (avoids a dashboard →
// engine cycle). Only the fields the payload build needs are decoded.
type flowDagChainStep struct {
	StepIndex int                 `json:"step_index"`
	EntityID  string              `json:"entity_id"`
	Reason    string              `json:"reason,omitempty"`
	Branches  []*flowDagChainStep `json:"branches,omitempty"`
}

// flowDagNodeID is the stable React Flow node id for a step. step_index is
// unique per flow (an entity_id can recur when a flow revisits a callee), so we
// key on it — matching lib/flow-to-dag.ts stepNodeId.
func flowDagNodeID(stepIndex int) string {
	return "flow-step-" + itoa(stepIndex)
}

// buildFlowDagPayload assembles the server-side FlowDag payload (the
// v2DownstreamDAGResponse shape) for a process flow from its annotated steps,
// the persisted branches_dag JSON, and flow-level metadata. Returns nil when
// the flow carries no steps (the frontend shows its empty state, same as the
// adapter returning null).
func buildFlowDagPayload(steps []AnnotatedStep, branchesDAG, label, entryKind string) *v2DownstreamDAGResponse {
	if len(steps) == 0 {
		return nil
	}

	// Sort by step_index so node[0] is the entry regardless of slice order.
	ordered := append([]AnnotatedStep(nil), steps...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].StepIndex < ordered[j].StepIndex })

	byIndex := make(map[int]AnnotatedStep, len(ordered))
	for _, s := range ordered {
		byIndex[s.StepIndex] = s
	}
	entryIndex := ordered[0].StepIndex
	terminalIndex := ordered[len(ordered)-1].StepIndex

	nodes := make([]v2DAGNode, 0, len(ordered))
	for _, s := range ordered {
		nodes = append(nodes, v2DAGNode{
			ID:      flowDagNodeID(s.StepIndex),
			Name:    flowDagStepLabel(s),
			Kind:    flowDagStepNodeKind(s),
			File:    s.SourceFile,
			Line:    s.StartLine,
			Repo:    s.Repo,
			Role:    flowDagStepRole(s, s.StepIndex == entryIndex, entryKind),
			Effects: flowDagStepEffects(s),
			// Terminal patched below once the edge set is known.
		})
	}

	// Prefer the persisted branching DAG; fall back to the linear chain when it
	// is absent or references step indices the steps slice doesn't carry.
	root := parseFlowDagBranches(branchesDAG)
	edges, fanoutCapped := flowDagEdgesFromBranches(root, byIndex)
	if edges == nil {
		edges = make([]v2DAGEdge, 0, len(ordered))
		for i := 1; i < len(ordered); i++ {
			edges = append(edges, v2DAGEdge{
				From: flowDagNodeID(ordered[i-1].StepIndex),
				To:   flowDagNodeID(ordered[i].StepIndex),
				Kind: flowDagEdgeKind(ordered[i]),
			})
		}
	}

	// Mark leaves (no outgoing edge) terminal so collection sinks get the sink
	// styling; always mark the last step so a strictly-linear chain reads as
	// terminating.
	hasOut := make(map[string]bool, len(edges))
	for _, e := range edges {
		hasOut[e.From] = true
	}
	terminalNodeID := flowDagNodeID(terminalIndex)
	for i := range nodes {
		if !hasOut[nodes[i].ID] || nodes[i].ID == terminalNodeID {
			nodes[i].Terminal = true
		}
	}

	return &v2DownstreamDAGResponse{
		RootID: flowDagNodeID(entryIndex),
		Path:   label,
		Verb:   entryKind,
		Mode:   "full",
		Depth:  len(ordered),
		Nodes:  nodes,
		Edges:  edges,
		Truncation: v2DAGTruncation{
			// The engine applies its own fan-out cap upstream (surfaced as
			// "fanout_cap" sentinels in branches_dag); flag it so the legend is honest.
			FanoutTruncated: fanoutCapped,
		},
		BranchCount: flowDagBranchCount(edges),
	}
}

// parseFlowDagBranches decodes the persisted branches_dag JSON. Returns nil on
// absent/garbage input so the caller falls back to the linear chain.
func parseFlowDagBranches(raw string) *flowDagChainStep {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var root flowDagChainStep
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		return nil
	}
	return &root
}

// flowDagEdgesFromBranches builds the branch edges from the persisted ChainStep
// tree — every parent→child link becomes a directed edge keyed on step_index,
// including fan-out arms a linear chain would drop. "fanout_cap" overflow
// sentinels carry no real step, so they are skipped (the truncation flag still
// reflects the cap). Returns (nil, _) when the tree references step indices the
// steps slice doesn't carry, so the caller falls back to the linear chain.
func flowDagEdgesFromBranches(root *flowDagChainStep, byIndex map[int]AnnotatedStep) (edges []v2DAGEdge, fanoutCapped bool) {
	if root == nil {
		return nil, false
	}
	edges = []v2DAGEdge{}
	ok := true
	var walk func(n *flowDagChainStep)
	walk = func(n *flowDagChainStep) {
		for _, child := range n.Branches {
			if child == nil {
				continue
			}
			if child.Reason == "fanout_cap" {
				fanoutCapped = true
				continue
			}
			_, fromOK := byIndex[n.StepIndex]
			to, toOK := byIndex[child.StepIndex]
			if !fromOK || !toOK {
				ok = false
				continue
			}
			edges = append(edges, v2DAGEdge{
				From: flowDagNodeID(n.StepIndex),
				To:   flowDagNodeID(child.StepIndex),
				Kind: flowDagEdgeKind(to),
			})
			walk(child)
		}
	}
	walk(root)
	if !ok {
		return nil, fanoutCapped
	}
	return edges, fanoutCapped
}

// flowDagStepLabel returns the best human label for a step.
func flowDagStepLabel(s AnnotatedStep) string {
	if s.Name != "" {
		return s.Name
	}
	if s.Label != "" {
		return s.Label
	}
	return s.EntityID
}

// flowDagStepNodeKind returns the node kind, preferring the resolved entity kind
// (scope-stripped) and falling back to the functional step_kind.
func flowDagStepNodeKind(s AnnotatedStep) string {
	if k := strings.TrimSpace(dashStripScopePrefix(s.EntityKind)); k != "" {
		return k
	}
	if s.StepKind != "" {
		return s.StepKind
	}
	return "step"
}

// flowDagStepRole maps a step onto a DAG role so the shared node styling lights
// up: the entry step → "endpoint" when it's an HTTP handler (the request
// boundary), else "handler"; data-sink steps → "collection"; everything else →
// "node" (the generic spine). Mirrors the client adapter's stepRole.
func flowDagStepRole(s AnnotatedStep, isEntry bool, entryKind string) string {
	if isEntry {
		if entryKind == "http_handler" {
			return "endpoint"
		}
		return "handler"
	}
	if s.StepKind != "" && flowDagSinkStepKinds[s.StepKind] {
		return "collection"
	}
	return "node"
}

// flowDagEdgeKind maps a step's incoming edge_kind onto a DAG edge kind so the
// edge styling/legend stays meaningful. The DAG vocabulary is a closed set; the
// flow chain is overwhelmingly CALLS, with a few semantic hops mapping to the
// dashed JOINS_COLLECTION arm. Mirrors the client adapter's edgeKindFor.
func flowDagEdgeKind(s AnnotatedStep) string {
	switch s.EdgeKind {
	case "QUERIES", "FETCHES", "PUBLISHES_TO", "SUBSCRIBES_TO":
		return "JOINS_COLLECTION"
	default:
		return "CALLS"
	}
}

// flowDagStepEffects surfaces the per-step side-effect kind as a node effect
// badge when the step is an observable side effect (db write, publish, http
// out, …) — the richer-than-adapter enrichment the resolved annotation carries.
func flowDagStepEffects(s AnnotatedStep) []string {
	if s.StepKind != "" && effectKinds[s.StepKind] {
		return []string{s.StepKind}
	}
	return nil
}

// flowDagBranchCount counts internal fan-out points (kept nodes with distinct
// out-degree > 1). Mirrors the builder's branchCount + the client countBranches.
func flowDagBranchCount(edges []v2DAGEdge) int {
	deg := map[string]map[string]bool{}
	for _, e := range edges {
		if deg[e.From] == nil {
			deg[e.From] = map[string]bool{}
		}
		deg[e.From][e.To] = true
	}
	n := 0
	for _, targets := range deg {
		if len(targets) > 1 {
			n++
		}
	}
	return n
}
