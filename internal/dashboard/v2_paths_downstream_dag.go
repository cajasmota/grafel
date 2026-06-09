// v2_paths_downstream_dag.go — endpoint downstream-DAG surface for WebUI v2
// (#4349, epic #4348 "endpoint-flow modal").
//
// Route:
//
//	GET /api/v2/groups/:id/paths/:hash/downstream-dag → v2DownstreamDAGResponse
//
// Given an HTTP endpoint (resolved from the path hash + optional ?verb), this
// returns the endpoint's DOWNSTREAM as a branching DAG rooted at the endpoint:
//
//	endpoint → handler → service → repository → pipeline
//	                                          → JOINS_COLLECTION → collection (leaf)
//
// plus distinct service/repo branches, $facet splits, and THROWS / VALIDATES
// side-branches. It is the data source for the endpoint-flow modal (#4350).
//
// Traversal (reuses the same graph primitives the process-flow DAG + MCP
// flow_tools rely on, so the surfaces never drift):
//
//   - Root at the http_endpoint_definition for (path hash, verb).
//   - Cross the HTTP boundary via the handler-continuation edge — the reversed
//     `handler --IMPLEMENTS--> http_endpoint_definition` (#1639/#4316/#4344),
//     resolved here with the SAME buildRepoEntityIndex.resolveHandlers used by
//     the paths detail (#1646).
//   - From the handler, BFS forward over CALLS (the spine) plus the projected
//     SEMANTIC edges (JOINS_COLLECTION, THROWS, VALIDATES — toggleable). Each
//     node is emitted ONCE; a node reached via multiple paths gets multiple
//     in-edges so real convergence (a $facet count+data → result merge, or a
//     shared util/collection) renders as one node, not duplicated subtrees.
//
// Modes:
//
//   - spine (default): collapse low-level query-builder / predicate calls (the
//     aggregation.builder.ts + where.builder.ts methods: eq/gte/in/lt/…) INTO
//     their owning pipeline node, returned as `collapsed_children` so the
//     frontend can expand them on demand. The meaningful spine survives.
//   - full: return every reachable node (still capped).
//
// Caps: bounded depth (default 8) + per-node fan-out. Truncation is honest —
// `depth_truncated` / `fanout_truncated` / `node_truncated` flags are set when
// anything was dropped, never a silent drop. The joined collection (Class:X via
// JOINS_COLLECTION) is a terminal leaf.

package dashboard

import (
	"net/http"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Wire types — the contract the endpoint-flow modal (#4350) consumes.
// ---------------------------------------------------------------------------

// v2DAGNode is one node in the downstream DAG. IDs are repo-prefixed
// ("<slug>::<entityID>") so they are stable + globally unique across repos and
// match the ids the rest of the v2 surface emits.
type v2DAGNode struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
	Repo string `json:"repo"`
	// Role labels the node's place on the spine so the modal can lay it out
	// without re-deriving from kind: "endpoint" | "handler" | "node" |
	// "collection". The endpoint root is always "endpoint".
	Role string `json:"role,omitempty"`
	// Terminal marks a leaf the walk deliberately stops at (a joined
	// collection). The frontend renders these as sinks.
	Terminal bool `json:"terminal,omitempty"`
	// CollapsedChildren are the low-level builder/predicate calls collapsed
	// into this node in spine mode (eq/gte/in/$lookup helpers, …). Empty in
	// full mode. The frontend renders an expander; expanding does NOT need a
	// second round-trip — the rows are already here.
	CollapsedChildren []v2DAGCollapsedChild `json:"collapsed_children,omitempty"`
}

// v2DAGCollapsedChild is one collapsed builder/predicate call folded into a
// spine node. It carries enough to render the expanded row in place.
type v2DAGCollapsedChild struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
	// EdgeKind is the relationship via which the parent reached this collapsed
	// child (usually CALLS).
	EdgeKind string `json:"edge_kind"`
}

// v2DAGEdge is one directed in-edge of the DAG. A convergence node has >1
// edge with the same `to`.
type v2DAGEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

// v2DAGTruncation flags what (if anything) the caps dropped. All-false means
// the DAG is complete within the requested depth.
type v2DAGTruncation struct {
	DepthTruncated  bool `json:"depth_truncated"`
	FanoutTruncated bool `json:"fanout_truncated"`
	NodeTruncated   bool `json:"node_truncated"`
}

// v2DownstreamDAGResponse is the payload for
// GET /api/v2/groups/:id/paths/:hash/downstream-dag.
type v2DownstreamDAGResponse struct {
	RootID     string          `json:"root_id"`
	Path       string          `json:"path"`
	Verb       string          `json:"verb"`
	Mode       string          `json:"mode"`
	Depth      int             `json:"depth"`
	Nodes      []v2DAGNode     `json:"nodes"`
	Edges      []v2DAGEdge     `json:"edges"`
	Truncation v2DAGTruncation `json:"truncation"`
	// BranchCount is the number of internal fan-out points (nodes whose
	// out-degree in the kept DAG is > 1) — the modal uses it for a "N branches"
	// badge without re-walking the edge list.
	BranchCount int `json:"branch_count"`
}

// ---------------------------------------------------------------------------
// Defaults + caps
// ---------------------------------------------------------------------------

const (
	dagDefaultDepth = 8
	dagMaxDepth     = 24
	dagMaxFanout    = 12
	dagMaxNodes     = 600
)

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// handleV2PathDownstreamDAG — GET /api/v2/groups/:id/paths/:hash/downstream-dag
//
// Query params:
//
//	verb        — disambiguate when a path has multiple verb endpoints (optional;
//	              default = first verb by deterministic ID order).
//	mode        — "spine" (default) | "full".
//	depth       — max hops from the endpoint (default 8, clamped to [1, 24]).
//	semantic    — "1"/"true" (default) to include JOINS_COLLECTION/THROWS/VALIDATES
//	              side-edges; "0"/"false" to walk the CALLS spine only.
func (s *Server) handleV2PathDownstreamDAG(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pathHash := r.PathValue("hash")
	if id == "" || pathHash == "" {
		writeV2Err(w, http.StatusBadRequest, "params_required", "group id and path hash required")
		return
	}

	grp, err := s.graphs.GetGroup(id)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "group_not_found", err.Error())
		return
	}

	q := r.URL.Query()
	mode := strings.ToLower(strings.TrimSpace(q.Get("mode")))
	if mode != "full" {
		mode = "spine"
	}
	depth := dagDefaultDepth
	if v := strings.TrimSpace(q.Get("depth")); v != "" {
		if n := atoiSafe(v); n > 0 {
			depth = n
		}
	}
	if depth < 1 {
		depth = 1
	}
	if depth > dagMaxDepth {
		depth = dagMaxDepth
	}
	includeSemantic := true
	if v := strings.ToLower(strings.TrimSpace(q.Get("semantic"))); v == "0" || v == "false" || v == "no" {
		includeSemantic = false
	}
	wantVerb := strings.ToUpper(strings.TrimSpace(q.Get("verb")))

	// Resolve the root endpoint entity for (path hash, verb).
	root := resolveDAGRoot(grp, pathHash, wantVerb)
	if root == nil {
		writeV2Err(w, http.StatusNotFound, "path_not_found", "no endpoint found for path hash: "+pathHash)
		return
	}

	b := newDAGBuilder(root.repo, mode, depth, includeSemantic)
	b.build(root)

	writeV2JSON(w, http.StatusOK, v2OK(v2DownstreamDAGResponse{
		RootID:      b.rootID,
		Path:        root.path,
		Verb:        root.verb,
		Mode:        mode,
		Depth:       depth,
		Nodes:       b.orderedNodes(),
		Edges:       b.orderedEdges(),
		Truncation:  b.trunc,
		BranchCount: b.branchCount(),
	}))
}

// ---------------------------------------------------------------------------
// Root resolution
// ---------------------------------------------------------------------------

// dagRoot is the resolved endpoint root: the http_endpoint definition entity,
// its owning repo, and human-facing path/verb.
type dagRoot struct {
	repo *DashRepo
	ent  *graph.Entity
	path string
	verb string
}

// resolveDAGRoot finds the endpoint-definition entity to root the DAG at.
//
// A path hash can map to several (verb) endpoints; when wantVerb is set we pick
// that verb, otherwise the first by deterministic (repo slug, entity ID) order
// so the same request always returns the same root.
func resolveDAGRoot(grp *DashGroup, pathHash, wantVerb string) *dagRoot {
	var best *dagRoot
	for _, repo := range sortedRepos(grp) {
		if repo.Doc == nil {
			continue
		}
		for i := range repo.Doc.Entities {
			e := &repo.Doc.Entities[i]
			kind := dashStripScopePrefix(e.Kind)
			isHTTP := types.IsHTTPEndpointKind(kind) ||
				strings.EqualFold(kind, httpEndpointKind) ||
				e.Kind == "Endpoint" || e.Kind == "Route"
			if !isHTTP {
				continue
			}
			if e.Kind == "http_endpoint_call" ||
				e.Properties["pattern_type"] == "http_endpoint_client_synthesis" {
				continue
			}
			path := e.Properties["path"]
			if path == "" {
				path = e.Name
			}
			if hashStr(path) != pathHash {
				continue
			}
			verb := strings.ToUpper(e.Properties["verb"])
			if verb == "" {
				verb = "ANY"
			}
			cand := &dagRoot{repo: repo, ent: e, path: path, verb: verb}
			if wantVerb != "" {
				if verb == wantVerb {
					return cand
				}
				continue
			}
			if best == nil || candLess(cand, best) {
				best = cand
			}
		}
	}
	return best
}

// candLess gives a deterministic ID-tiebroken ordering for root selection.
func candLess(a, b *dagRoot) bool {
	if a.repo.Slug != b.repo.Slug {
		return a.repo.Slug < b.repo.Slug
	}
	return a.ent.ID < b.ent.ID
}

// ---------------------------------------------------------------------------
// DAG builder
// ---------------------------------------------------------------------------

// dagBuilder accumulates the DAG nodes + edges with dedupe, collapse, and caps.
//
// The walk is single-repo (rooted at the endpoint's own repo): the handler and
// its service/repository/pipeline chain live in the same repo as the endpoint
// definition. Cross-repo fan-out (a backend calling another backend) is out of
// scope for the endpoint-flow modal and handled by the dedicated process-flow
// / traces surfaces.
type dagBuilder struct {
	repo            *DashRepo
	byID            map[string]*graph.Entity
	out             map[string][]dagOutEdge
	mode            string
	maxDepth        int
	includeSemantic bool

	rootID string

	nodes    map[string]*v2DAGNode // prefixed id -> node
	nodeKeys []string              // insertion order (deterministic post-sort)
	edgeSet  map[string]bool       // "from|to|kind" dedupe
	edges    []v2DAGEdge

	trunc v2DAGTruncation
}

// dagOutEdge is one outbound edge in the builder's local adjacency.
type dagOutEdge struct {
	to   string // local (un-prefixed) target id
	kind string
}

func newDAGBuilder(repo *DashRepo, mode string, maxDepth int, includeSemantic bool) *dagBuilder {
	b := &dagBuilder{
		repo:            repo,
		byID:            make(map[string]*graph.Entity, len(repo.Doc.Entities)),
		out:             make(map[string][]dagOutEdge),
		mode:            mode,
		maxDepth:        maxDepth,
		includeSemantic: includeSemantic,
		nodes:           map[string]*v2DAGNode{},
		edgeSet:         map[string]bool{},
	}
	for i := range repo.Doc.Entities {
		e := &repo.Doc.Entities[i]
		b.byID[e.ID] = e
	}
	// Build the forward adjacency over the kinds the DAG cares about:
	// CALLS (the spine) + the handler-continuation reversal of
	// `handler --IMPLEMENTS--> http_endpoint_definition` + projected SEMANTIC
	// edges when enabled. We reverse IMPLEMENTS exactly like the process-flow
	// adjacency does (#1639) so the endpoint definition gains an outgoing
	// continuation edge into its backend handler.
	defKinds := map[string]bool{}
	for id, e := range b.byID {
		if strings.EqualFold(dashStripScopePrefix(e.Kind), httpEndpointDefinitionKind) {
			defKinds[id] = true
		}
	}
	for i := range repo.Doc.Relationships {
		r := &repo.Doc.Relationships[i]
		switch {
		case r.Kind == "CALLS":
			if r.FromID == r.ToID {
				continue
			}
			b.out[r.FromID] = append(b.out[r.FromID], dagOutEdge{to: r.ToID, kind: "CALLS"})
		case r.Kind == "IMPLEMENTS" && defKinds[r.ToID]:
			// reverse: definition --(handler continuation)--> handler.
			if r.FromID == r.ToID {
				continue
			}
			b.out[r.ToID] = append(b.out[r.ToID], dagOutEdge{to: r.FromID, kind: handlerContEdgeKind})
		case includeSemantic && isDAGSemanticKind(r.Kind):
			if r.FromID == r.ToID {
				continue
			}
			b.out[r.FromID] = append(b.out[r.FromID], dagOutEdge{to: r.ToID, kind: strings.ToUpper(r.Kind)})
		}
	}
	// Deterministic adjacency ordering: by (kind, target). Stable so the BFS
	// frontier — and thus node insertion + fan-out truncation — is reproducible.
	for k := range b.out {
		es := b.out[k]
		sort.Slice(es, func(i, j int) bool {
			if es[i].kind != es[j].kind {
				return es[i].kind < es[j].kind
			}
			return es[i].to < es[j].to
		})
	}
	return b
}

// handlerContEdgeKind labels the reversed-IMPLEMENTS continuation edge in the
// emitted DAG so the frontend can distinguish the HTTP-boundary crossing from
// an ordinary CALLS edge.
const handlerContEdgeKind = "HANDLER_CONTINUATION"

// build runs the BFS from the endpoint root, materialising nodes + edges with
// dedupe, spine-collapse, and the depth/fan-out/node caps.
func (b *dagBuilder) build(root *dagRoot) {
	b.rootID = b.pid(root.ent.ID)
	b.addNode(root.ent.ID, "endpoint", false)

	type frontierItem struct {
		local string
		depth int
	}
	// queued tracks which locals already entered the queue so a convergence
	// target is enqueued once (DAG dedupe) but still receives every in-edge.
	queued := map[string]bool{root.ent.ID: true}
	queue := []frontierItem{{local: root.ent.ID, depth: 0}}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if cur.depth >= b.maxDepth {
			if len(b.out[cur.local]) > 0 {
				b.trunc.DepthTruncated = true
			}
			continue
		}

		// A joined collection is a terminal leaf — never expand past it.
		if b.isTerminal(cur.local) {
			continue
		}

		curPID := b.pid(cur.local)
		kept := 0
		for _, e := range b.out[cur.local] {
			// Spine mode: collapse low-level builder/predicate callees INTO the
			// current node rather than expanding them as DAG nodes.
			if b.mode == "spine" && e.kind == "CALLS" && b.isBuilderNoise(e.to) {
				b.collapseChild(curPID, e)
				continue
			}
			if kept >= dagMaxFanout {
				b.trunc.FanoutTruncated = true
				break
			}
			kept++

			role := b.roleFor(e.to, e.kind)
			terminal := b.isTerminal(e.to)
			b.addNode(e.to, role, terminal)
			b.addEdge(curPID, b.pid(e.to), e.kind)

			if len(b.nodes) >= dagMaxNodes {
				b.trunc.NodeTruncated = true
			}
			if queued[e.to] {
				continue // already scheduled — convergence: edge added, no re-expand.
			}
			if len(b.nodes) >= dagMaxNodes {
				continue
			}
			queued[e.to] = true
			queue = append(queue, frontierItem{local: e.to, depth: cur.depth + 1})
		}
	}
}

// pid returns the repo-prefixed id for a local entity id.
func (b *dagBuilder) pid(local string) string { return dashPrefixedID(b.repo.Slug, local) }

// addNode inserts (or, on convergence, leaves) a node for local id.
func (b *dagBuilder) addNode(local, role string, terminal bool) {
	pid := b.pid(local)
	if _, ok := b.nodes[pid]; ok {
		return
	}
	n := &v2DAGNode{
		ID:       pid,
		Repo:     b.repo.Slug,
		Role:     role,
		Terminal: terminal,
	}
	if e := b.byID[local]; e != nil {
		n.Name = e.Name
		n.Kind = dashStripScopePrefix(e.Kind)
		n.File = e.SourceFile
		n.Line = e.StartLine
	} else {
		// Far side of a semantic edge (e.g. Class:Inspection joined via
		// JOINS_COLLECTION) may not be a stamped entity. Surface it with the id
		// as the name so the leaf still renders (mirrors the #4288 fallback).
		n.Name = leafNameFromID(local)
		n.Kind = kindFromID(local)
	}
	b.nodes[pid] = n
	b.nodeKeys = append(b.nodeKeys, pid)
}

// collapseChild folds a builder/predicate callee into its parent node's
// collapsed_children (spine mode). The collapsed child is NOT added as a DAG
// node and gets no DAG edge — it lives inside the parent for on-demand expand.
func (b *dagBuilder) collapseChild(parentPID string, e dagOutEdge) {
	parent := b.nodes[parentPID]
	if parent == nil {
		return
	}
	childPID := b.pid(e.to)
	for _, c := range parent.CollapsedChildren {
		if c.ID == childPID {
			return // dedupe
		}
	}
	cc := v2DAGCollapsedChild{ID: childPID, EdgeKind: e.kind}
	if ent := b.byID[e.to]; ent != nil {
		cc.Name = ent.Name
		cc.Kind = dashStripScopePrefix(ent.Kind)
		cc.File = ent.SourceFile
		cc.Line = ent.StartLine
	} else {
		cc.Name = leafNameFromID(e.to)
		cc.Kind = kindFromID(e.to)
	}
	parent.CollapsedChildren = append(parent.CollapsedChildren, cc)
}

// addEdge records a directed in-edge, deduplicated on (from, to, kind). A
// convergence node simply accumulates >1 edge with the same `to`.
func (b *dagBuilder) addEdge(from, to, kind string) {
	key := from + "|" + to + "|" + kind
	if b.edgeSet[key] {
		return
	}
	b.edgeSet[key] = true
	b.edges = append(b.edges, v2DAGEdge{From: from, To: to, Kind: kind})
}

// isTerminal reports whether a node is a deliberate leaf the walk stops at:
// the joined collection reached via JOINS_COLLECTION (a Class:X data sink).
func (b *dagBuilder) isTerminal(local string) bool {
	e := b.byID[local]
	if e == nil {
		// Unstamped far side of a JOINS_COLLECTION (Class:X id) — terminal.
		return strings.HasPrefix(local, "Class:") || strings.HasPrefix(local, "Collection:")
	}
	k := strings.ToLower(dashStripScopePrefix(e.Kind))
	return k == "collection" || k == "table" || k == "datastore"
}

// roleFor labels a node's spine role from how it was reached + its kind.
func (b *dagBuilder) roleFor(local, viaKind string) string {
	if viaKind == handlerContEdgeKind {
		return "handler"
	}
	if b.isTerminal(local) {
		return "collection"
	}
	return "node"
}

// isBuilderNoise reports whether a callee is a low-level query-builder /
// predicate method that should collapse into its owning pipeline node in spine
// mode (the aggregation.builder.ts + where.builder.ts methods: eq/gte/in/lt/ne/
// or/addFields/shape/path/mongo/set/limit/skip/count/…). The classification is
// file-driven (the builder modules) with a method-name fallback so it survives
// minor file renames.
func (b *dagBuilder) isBuilderNoise(local string) bool {
	e := b.byID[local]
	if e == nil {
		return false
	}
	// An entity that owns downstream meaning (a JOINS_COLLECTION, a THROWS, or
	// further CALLS into a real service) is NOT noise even if its name matches —
	// collapsing it would hide a real branch. Builder helpers are leaves.
	file := strings.ToLower(e.SourceFile)
	if strings.Contains(file, "aggregation.builder") || strings.Contains(file, "where.builder") {
		return !b.hasMeaningfulOut(local)
	}
	if isBuilderMethodName(e.Name) {
		return !b.hasMeaningfulOut(local)
	}
	return false
}

// hasMeaningfulOut reports whether a node has any outgoing edge that carries
// real downstream meaning (a non-builder CALLS, or any semantic edge). Such a
// node is kept on the spine even if its name looks builder-ish, so we never
// collapse away a real branch.
func (b *dagBuilder) hasMeaningfulOut(local string) bool {
	for _, e := range b.out[local] {
		if e.kind != "CALLS" {
			return true // semantic / handler-continuation — meaningful.
		}
		if !b.isBuilderLeafName(e.to) {
			return true
		}
	}
	return false
}

// isBuilderLeafName is the cheap name/file test used by hasMeaningfulOut to
// avoid infinite mutual recursion with isBuilderNoise (it does not recurse into
// hasMeaningfulOut).
func (b *dagBuilder) isBuilderLeafName(local string) bool {
	e := b.byID[local]
	if e == nil {
		return false
	}
	file := strings.ToLower(e.SourceFile)
	if strings.Contains(file, "aggregation.builder") || strings.Contains(file, "where.builder") {
		return true
	}
	return isBuilderMethodName(e.Name)
}

// ---------------------------------------------------------------------------
// Output ordering — deterministic, ID-tiebroken.
// ---------------------------------------------------------------------------

// orderedNodes returns the nodes with the root first, then by id. Collapsed
// children inside each node are id-sorted too.
func (b *dagBuilder) orderedNodes() []v2DAGNode {
	out := make([]v2DAGNode, 0, len(b.nodes))
	for _, pid := range b.nodeKeys {
		n := b.nodes[pid]
		if len(n.CollapsedChildren) > 1 {
			sort.Slice(n.CollapsedChildren, func(i, j int) bool {
				return n.CollapsedChildren[i].ID < n.CollapsedChildren[j].ID
			})
		}
		out = append(out, *n)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ID == b.rootID {
			return true
		}
		if out[j].ID == b.rootID {
			return false
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// orderedEdges returns edges sorted by (from, to, kind).
func (b *dagBuilder) orderedEdges() []v2DAGEdge {
	out := append([]v2DAGEdge(nil), b.edges...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		if out[i].To != out[j].To {
			return out[i].To < out[j].To
		}
		return out[i].Kind < out[j].Kind
	})
	if out == nil {
		out = []v2DAGEdge{}
	}
	return out
}

// branchCount is the number of internal fan-out points: kept nodes whose
// out-degree (distinct targets) in the emitted DAG is > 1.
func (b *dagBuilder) branchCount() int {
	deg := map[string]map[string]bool{}
	for _, e := range b.edges {
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

// ---------------------------------------------------------------------------
// Small helpers
// ---------------------------------------------------------------------------

// isDAGSemanticKind reports whether a relationship kind is one of the SEMANTIC
// side-edges the downstream DAG surfaces: JOINS_COLLECTION (the data sink),
// THROWS + VALIDATES (handler/pipeline side-branches). Case-insensitive against
// the on-graph casing. This is intentionally a TIGHT subset of the broader
// mcp.semanticEdgeKinds set — the endpoint-flow modal wants the data + error +
// validation branches, not the full DI/caching/translation universe.
func isDAGSemanticKind(k string) bool {
	switch strings.ToUpper(k) {
	case string(types.RelationshipKindJoinsCollection),
		string(types.RelationshipKindThrows),
		string(types.RelationshipKindValidates):
		return true
	}
	return false
}

// builderMethodNames is the set of low-level aggregation/predicate builder
// method names that collapse into their owning pipeline node in spine mode.
var builderMethodNames = map[string]bool{
	"eq": true, "ne": true, "gt": true, "gte": true, "lt": true, "lte": true,
	"in": true, "nin": true, "or": true, "and": true, "not": true,
	"addfields": true, "shape": true, "path": true, "mongo": true, "set": true,
	"limit": true, "skip": true, "count": true, "sort": true, "match": true,
	"project": true, "group": true, "unwind": true, "lookup": true,
	"exists": true, "regex": true, "elemmatch": true, "size": true,
}

// isBuilderMethodName reports whether a method name is a known builder/predicate
// helper. Strips any class scope ("AggregationBuilder.eq" → "eq") and lower-
// cases before the lookup.
func isBuilderMethodName(name string) bool {
	n := name
	if dot := strings.LastIndex(n, "."); dot >= 0 && dot < len(n)-1 {
		n = n[dot+1:]
	}
	if sc := strings.LastIndex(n, "::"); sc >= 0 && sc < len(n)-2 {
		n = n[sc+2:]
	}
	return builderMethodNames[strings.ToLower(n)]
}

// leafNameFromID derives a human label for an unstamped semantic-edge target id
// (e.g. "Class:Inspection" → "Inspection").
func leafNameFromID(id string) string {
	if i := strings.LastIndex(id, ":"); i >= 0 && i < len(id)-1 {
		return id[i+1:]
	}
	return id
}

// kindFromID derives a kind label for an unstamped semantic-edge target id.
func kindFromID(id string) string {
	if i := strings.Index(id, ":"); i > 0 {
		return strings.ToLower(id[:i])
	}
	return "external"
}

// atoiSafe parses a non-negative int, returning 0 on any non-digit input.
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
