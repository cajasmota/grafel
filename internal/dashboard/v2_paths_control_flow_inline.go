// v2_paths_control_flow_inline.go — DEPTH-controlled INTERPROCEDURAL (inlined)
// CFG for the Flowchart view (#4883, control-flow epic #4820).
//
// #4819 shipped the per-HANDLER CFG (one function's basic blocks). #4883 turns
// the Flowchart into ONE continuous control-flow story from the HTTP entry down
// the call chain:
//
//	Depth 1 — the endpoint handler's own CFG.
//	Depth 2 — at each in-repo CALL site inside the handler, INLINE the callee's
//	          CFG (the service method spliced in at the call node).
//	Depth 3..n — keep following CALLS and inlining to the chosen depth.
//
// This is the flowchart sibling of the downstream-DAG's depth slider, and it
// REUSES that surface's controller→service→repo CALLS resolution (the forward
// adjacency over CALLS + the in-repo / external classification) so the two views
// never disagree on which callee a call site binds to. Each frame's blocks come
// from the SAME substrate.BuildControlFlowGraph builder #4819 uses — never a
// reimplementation.
//
// Inlining mechanics. For a frame's CFG, each `process` node whose source line
// invokes an in-repo callee is a SPLICE point: the callee's CFG is built and its
// entry…exit is wired in place of the call node — the call node's predecessors
// flow into the callee's start, and the callee's exit(s) flow on to the call
// node's successors (continuation). Node ids are namespaced per frame ("f1:n3")
// so frames never collide, and every node carries its frame id (Func) so the UI
// can draw a group box / labelled divider per inlined hop.
//
// Boundaries / safety:
//   - External, cross-repo, or library calls have no in-repo source → they are
//     LEAF terminals (marked External), never recursed into.
//   - A cycle-guard (visited entity-id set down the current spine) + the depth
//     cap stop infinite / runaway recursion (recursive or mutually-recursive
//     methods inline once on the path, then stop).
//   - Detail gating is applied per frame exactly as the single-function CFG did.
//
// Branch-level granularity only (we do not extract deeper internal conditionals
// than #4819 already does). Read-only, deterministic.

package dashboard

import (
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/substrate"
)

// cfgInlineResult is the assembled interprocedural CFG: wire nodes/edges (with
// per-frame Func tags + External leaves), the combined complexity, and the
// ordered inlined-function frames for boundary rendering.
type cfgInlineResult struct {
	Supported   bool
	Cyclomatic  int
	BranchCount int
	Nodes       []v2CFGNode
	Edges       []v2CFGEdge
	Functions   []v2CFGFunction
}

// cfgInliner builds the inlined CFG, reusing the downstream-DAG's CALLS
// resolution against the endpoint's own repo (the handler + its service/repo
// chain live in the same repo as the endpoint, like the DAG walk).
type cfgInliner struct {
	grp      *DashGroup
	repo     *DashRepo
	maxDepth int

	byID   map[string]*graph.Entity
	byName map[string][]*graph.Entity // lower-cased in-repo name → defining entities
	// callsOut maps an entity id → the in-repo callee entities it CALLS, in
	// deterministic id order. External / unresolved callees are kept separately
	// in extCallsOut so a call site to them renders as an External leaf.
	callsOut map[string][]*graph.Entity
	// extCallsOut maps an entity id → the short callee names it CALLS that are
	// external / library / unresolved (no in-repo frame to descend into).
	extCallsOut map[string][]string

	detail string

	frameSeq  int             // next frame id suffix
	functions []v2CFGFunction // frames in inline order
}

// newCFGInliner indexes the endpoint repo's entities + CALLS adjacency once.
func newCFGInliner(grp *DashGroup, repo *DashRepo, maxDepth int) *cfgInliner {
	in := &cfgInliner{
		grp:         grp,
		repo:        repo,
		maxDepth:    maxDepth,
		byID:        map[string]*graph.Entity{},
		byName:      map[string][]*graph.Entity{},
		callsOut:    map[string][]*graph.Entity{},
		extCallsOut: map[string][]string{},
	}
	if repo == nil || repo.Doc == nil {
		return in
	}
	for i := range repo.Doc.Entities {
		e := &repo.Doc.Entities[i]
		in.byID[e.ID] = e
		if !isExternalEntity(e) {
			if nm := strings.ToLower(strings.TrimSpace(e.Name)); nm != "" {
				in.byName[nm] = append(in.byName[nm], e)
			}
		}
	}
	// Forward CALLS adjacency restricted to in-repo callees (the spine the
	// inliner can actually descend into). Mirrors the downstream-DAG's CALLS
	// edge harvest, minus the semantic side-edges (a CFG follows control flow,
	// not data joins).
	seen := map[string]map[string]bool{}
	for i := range repo.Doc.Relationships {
		r := &repo.Doc.Relationships[i]
		if r.Kind != "CALLS" || r.FromID == r.ToID {
			continue
		}
		callee := in.byID[r.ToID]
		if callee == nil || isExternalEntity(callee) {
			// External / unresolved → record its short name so the call site
			// renders as an External leaf, but never descend into it.
			nm := cfgShortCalleeName(recoverExternalName(r.ToID, dagOutEdge{kind: "CALLS", props: r.PropsSnapshot()}))
			if nm != "" {
				in.extCallsOut[r.FromID] = append(in.extCallsOut[r.FromID], nm)
			}
			continue
		}
		if seen[r.FromID] == nil {
			seen[r.FromID] = map[string]bool{}
		}
		if seen[r.FromID][r.ToID] {
			continue
		}
		seen[r.FromID][r.ToID] = true
		in.callsOut[r.FromID] = append(in.callsOut[r.FromID], callee)
	}
	for k := range in.callsOut {
		es := in.callsOut[k]
		sort.Slice(es, func(i, j int) bool { return es[i].ID < es[j].ID })
	}
	for k := range in.extCallsOut {
		sort.Strings(in.extCallsOut[k])
	}
	return in
}

// build assembles the interprocedural CFG rooted at the handler. src/start are
// the handler's already-read source window (the caller did the path resolution
// + read). detail gates each frame's node fields exactly as the single-function
// CFG did. At depth 1 the result is the old single-function CFG (one frame).
func (in *cfgInliner) build(handler *graph.Entity, lang, src string, start int, detail string) cfgInlineResult {
	in.detail = detail
	root := substrate.BuildControlFlowGraphCached(dashPrefixedID(in.repo.Slug, handler.ID), lang, src, start)

	res := cfgInlineResult{Supported: root.Supported}
	visited := map[string]bool{handler.ID: true}
	frame := in.inline(handler, root, lang, 0, visited)

	res.Nodes = frame.nodes
	res.Edges = frame.edges
	res.Functions = in.functions
	// Combined complexity: sum the per-frame cyclomatic numbers minus the
	// double-counted "+1" of every inlined frame beyond the first, so the
	// number reads as the McCabe complexity of the flattened super-graph
	// (decision points across all frames + 1).
	totalBranches := 0
	for _, f := range in.functions {
		totalBranches += f.Cyclomatic - 1
	}
	res.BranchCount = totalBranches
	res.Cyclomatic = totalBranches + 1
	return res
}

// cfgFrame is one inlined function's contribution to the combined graph: its
// (re-namespaced) nodes/edges, plus the frame's entry/exit ids so a parent call
// site can wire predecessors→entry and exit→continuation.
type cfgFrame struct {
	nodes   []v2CFGNode
	edges   []v2CFGEdge
	entryID string   // the frame's start node id (namespaced)
	exitIDs []string // node ids that flow OUT of the frame (the exit node)
}

// inline builds frame `g` for entity `ent` at `depth`, splicing every in-repo
// call site (to depth maxDepth) with the callee's frame. visited guards cycles
// down the current spine.
func (in *cfgInliner) inline(ent *graph.Entity, g *substrate.ControlFlowGraph, lang string, depth int, visited map[string]bool) cfgFrame {
	frameID := in.nextFrameID()
	in.functions = append(in.functions, v2CFGFunction{
		Func:       frameID,
		Name:       ent.Name,
		Kind:       dashStripScopePrefix(ent.Kind),
		File:       ent.SourceFile,
		Line:       ent.StartLine,
		Depth:      depth,
		Cyclomatic: g.Cyclomatic,
	})

	// Namespace this frame's node ids so frames never collide.
	ns := func(id string) string { return frameID + ":" + id }
	wireNodes := cfgNodesToWire(g.Nodes, in.detail)

	// Index the substrate nodes by their (un-namespaced) id for call-site lookup.
	rawByID := map[string]substrate.CFGNode{}
	for _, n := range g.Nodes {
		rawByID[n.ID] = n
	}

	// Resolve which call sites this frame can descend into: a process node whose
	// source line invokes one of the entity's in-repo callees. callee resolution
	// reuses the downstream-DAG adjacency (callsOut), matched to the call node by
	// the callee name appearing in the node's source label.
	callees := in.callsOut[ent.ID]
	type spliceTarget struct {
		nodeID string // namespaced call-node id being replaced
		callee *graph.Entity
	}
	var splices []spliceTarget
	// Splice only while a deeper hop remains (depth is 0-indexed: the handler is
	// depth 0, so maxDepth=1 means handler-only, maxDepth=2 inlines one hop, …).
	canRecurse := depth+1 < in.maxDepth
	if canRecurse && len(callees) > 0 {
		for _, n := range g.Nodes {
			// A call site can appear on a straight-line process node OR on a
			// terminal `return <call>()` / `throw <call>()` node — the dominant
			// NestJS thin-delegator pattern (`return this.service.create(body)`)
			// is a RETURN node, not a process node, so we must consider those too
			// or the handler's single delegating return never inlines (#4883).
			if !cfgShapeSpliceable(n.Shape) || n.Label == "" {
				continue
			}
			callee := in.matchCallee(n.Label, callees, visited)
			if callee == nil {
				continue
			}
			splices = append(splices, spliceTarget{nodeID: n.ID, callee: callee})
		}
	}
	spliceAt := map[string]*graph.Entity{}
	for _, s := range splices {
		spliceAt[s.nodeID] = s.callee
	}

	frame := cfgFrame{}
	// Successor index (un-namespaced) for continuation wiring.
	succ := map[string][]substrate.CFGEdge{}
	for _, e := range g.Edges {
		succ[e.From] = append(succ[e.From], e)
	}

	// Emit this frame's nodes, tagging Func; a spliced call node is replaced by
	// the callee frame (we DON'T emit the call node itself — its inbound edges
	// retarget to the callee entry and its outbound edges originate from the
	// callee exits).
	childFrames := map[string]cfgFrame{} // call-node id → built callee frame
	for i, wn := range wireNodes {
		raw := g.Nodes[i]
		if callee, ok := spliceAt[raw.ID]; ok {
			cf := in.buildCallee(callee, lang, depth+1, visited)
			if cf == nil {
				// Source unreadable / unsupported → keep the call node as an
				// External leaf rather than dropping the flow.
				wn.Func = frameID
				wn.External = true
				wn.ID = ns(wn.ID)
				frame.nodes = append(frame.nodes, wn)
				continue
			}
			childFrames[raw.ID] = *cf
			frame.nodes = append(frame.nodes, cf.nodes...)
			frame.edges = append(frame.edges, cf.edges...)
			continue
		}
		wn.Func = frameID
		wn.ID = ns(wn.ID)
		if raw.Shape == substrate.ShapeProcess && in.isExternalCall(raw.Label, ent) {
			wn.External = true
		}
		frame.nodes = append(frame.nodes, wn)
	}

	// Rewrite this frame's edges, redirecting endpoints that landed on a spliced
	// call node to the corresponding callee entry (as a `to`) or exits (as a
	// `from`).
	resolveFrom := func(id string) []string {
		if cf, ok := childFrames[id]; ok {
			return cf.exitIDs
		}
		return []string{ns(id)}
	}
	resolveTo := func(id string) string {
		if cf, ok := childFrames[id]; ok {
			return cf.entryID
		}
		return ns(id)
	}
	for _, e := range g.Edges {
		for _, from := range resolveFrom(e.From) {
			frame.edges = append(frame.edges, v2CFGEdge{
				From: from,
				To:   resolveTo(e.To),
				Kind: string(e.Kind),
			})
		}
	}

	// Frame entry/exit for the PARENT to wire to. The substrate start node is
	// always "n0"; the end node is the ShapeEnd node. If n0 was itself spliced
	// (cannot happen — start is never a process node) we'd fall back to its
	// callee entry via resolveTo.
	frame.entryID = resolveTo("n0")
	for _, n := range g.Nodes {
		if n.Shape == substrate.ShapeEnd {
			frame.exitIDs = append(frame.exitIDs, resolveFrom(n.ID)...)
		}
	}
	if len(frame.exitIDs) == 0 {
		frame.exitIDs = []string{frame.entryID}
	}
	return frame
}

// buildCallee builds the callee's frame, reading its source window + building
// its CFG, then recursing. Returns nil when the callee source is unreadable or
// the language is unsupported (the caller then keeps the call node as a leaf).
func (in *cfgInliner) buildCallee(callee *graph.Entity, lang string, depth int, visited map[string]bool) *cfgFrame {
	if visited[callee.ID] {
		return nil // cycle-guard: already on the spine.
	}
	src, start, ok := readCFGHandlerSource(in.grp, in.repo, callee)
	if !ok {
		return nil
	}
	cLang := substrate.LanguageForPath(callee.SourceFile)
	g := substrate.BuildControlFlowGraphCached(dashPrefixedID(in.repo.Slug, callee.ID), cLang, src, start)
	if !g.Supported {
		return nil
	}
	visited[callee.ID] = true
	frame := in.inline(callee, g, cLang, depth, visited)
	delete(visited, callee.ID) // allow the callee to appear on a sibling path.
	return &frame
}

// matchCallee returns the in-repo callee a process node's source line invokes,
// or nil. A line "invokes" a callee when the callee's (short) name appears as a
// call token in the line. Already-visited callees (cycle) are skipped so a
// recursive call inlines once on the path, then stops. When several callees
// match, the longest-named (most specific) wins, id-tiebroken — deterministic.
func (in *cfgInliner) matchCallee(label string, callees []*graph.Entity, visited map[string]bool) *graph.Entity {
	var best *graph.Entity
	for _, c := range callees {
		if visited[c.ID] {
			continue
		}
		short := cfgShortCalleeName(c.Name)
		if short == "" {
			continue
		}
		if !cfgLabelInvokes(label, short) {
			continue
		}
		if best == nil ||
			len(short) > len(cfgShortCalleeName(best.Name)) ||
			(len(short) == len(cfgShortCalleeName(best.Name)) && c.ID < best.ID) {
			best = c
		}
	}
	return best
}

// isExternalCall reports whether a process node's source line invokes a callee
// that is NOT an in-repo definition this frame can descend into — a library /
// builtin / cross-repo call recorded in extCallsOut. It must never mark an
// in-repo call external (that would hide a real frame), so an in-repo callee on
// the same line wins.
func (in *cfgInliner) isExternalCall(label string, ent *graph.Entity) bool {
	if !strings.Contains(label, "(") {
		return false
	}
	for _, c := range in.callsOut[ent.ID] {
		if cfgLabelInvokes(label, cfgShortCalleeName(c.Name)) {
			return false // an in-repo callee is on this line — not external.
		}
	}
	for _, nm := range in.extCallsOut[ent.ID] {
		if cfgLabelInvokes(label, nm) {
			return true
		}
	}
	return false
}

// cfgShapeSpliceable reports whether a CFG node of this shape can host an inlined
// callee call site. A call can appear on a straight-line statement (process) OR
// on a `return <call>()` / `throw <call>()` terminal — the latter being the
// dominant NestJS thin-delegator form (`return this.service.create(body)`) that
// the original splice loop (process-only) silently skipped, leaving the handler's
// single delegating return un-inlined at every depth (#4883).
func cfgShapeSpliceable(shape substrate.CFGNodeShape) bool {
	switch shape {
	case substrate.ShapeProcess, substrate.ShapeReturn, substrate.ShapeThrow:
		return true
	default:
		return false
	}
}

// cfgShortCalleeName reduces a (possibly scoped) callee name to the bare method
// token used at the call site: "InspectionRepo.find" → "find",
// "svc::handle" → "handle".
func cfgShortCalleeName(name string) string {
	n := strings.TrimSpace(name)
	if sc := strings.LastIndex(n, "::"); sc >= 0 && sc < len(n)-2 {
		n = n[sc+2:]
	}
	if dot := strings.LastIndex(n, "."); dot >= 0 && dot < len(n)-1 {
		n = n[dot+1:]
	}
	return n
}

// cfgLabelInvokes reports whether a source line invokes `name` as a call: the
// name appears immediately followed by `(` (allowing whitespace), with a
// non-identifier char (or start) before it so "find" does not match "refind".
func cfgLabelInvokes(label, name string) bool {
	if name == "" {
		return false
	}
	idx := 0
	for {
		i := strings.Index(label[idx:], name)
		if i < 0 {
			return false
		}
		pos := idx + i
		// Boundary before.
		if pos > 0 && cfgIsIdentChar(label[pos-1]) {
			idx = pos + len(name)
			continue
		}
		// `(` after (skipping spaces).
		j := pos + len(name)
		for j < len(label) && (label[j] == ' ' || label[j] == '\t') {
			j++
		}
		if j < len(label) && label[j] == '(' {
			return true
		}
		idx = pos + len(name)
	}
}

func cfgIsIdentChar(b byte) bool {
	return b == '_' ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}

// nextFrameID returns the next per-hop frame id ("f0","f1",…).
func (in *cfgInliner) nextFrameID() string {
	id := "f" + itoa(in.frameSeq)
	in.frameSeq++
	return id
}
