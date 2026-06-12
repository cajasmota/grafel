// v2_paths_control_flow.go — endpoint handler control-flow (CFG) surface for
// WebUI v2 (#4819, control-flow epic #4820).
//
// Route:
//
//	GET /api/v2/groups/:id/paths/:hash/control-flow?verb=&detail= → v2ControlFlowResponse
//
// Given an HTTP endpoint (resolved from the path hash + optional ?verb), this
// returns the ON-DEMAND control-flow graph (CFG) of the endpoint's HANDLER
// function — the flowchart the Downstream-flow modal renders when the user
// flips the View toggle from Tree to Flowchart.
//
// It is the dashboard sibling of the archigraph_control_flow MCP tool
// (internal/mcp/control_flow_tool.go): both REUSE the one CFG builder in
// internal/substrate (BuildControlFlowGraphCached) — no basic-block entities are
// ever written to the graph (the graph stays lean, per #4822). The CFG is built
// for the one handler function at request time and cached in-memory by
// (entity id, source hash).
//
// Handler resolution mirrors the downstream-DAG (#4349): the endpoint root is
// resolved by (path hash, verb) and the handler is the far side of the reversed
// `handler --IMPLEMENTS--> http_endpoint_definition` continuation edge. The CFG
// is built from THAT handler's source window.
//
// Detail levels (token control, #2828) parameterise the payload exactly like the
// MCP tool so the frontend slider maps 1:1:
//
//	outline    → node shapes + lines + complexity, no conditions/effects/labels
//	decisions  → outline + condition text on decision/loop nodes (default)
//	data       → decisions + effect annotations on process nodes
//	full       → data + node labels (the trimmed source line per node)
//
// Languages: Python + JS/TS first (the validated set). Other languages return
// supported=false with a degenerate start→process→end graph so the frontend can
// show a graceful "flowchart not available for this language" state. Read-only,
// deterministic.

package dashboard

import (
	"net/http"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/substrate"
)

// ---------------------------------------------------------------------------
// Wire types — the contract the Downstream-flow flowchart view (#4819) consumes.
// ---------------------------------------------------------------------------

// v2CFGEffect is a terse effect annotation on a process node.
type v2CFGEffect struct {
	Effect string `json:"effect"`
	Sink   string `json:"sink,omitempty"`
}

// v2CFGNode is one basic block / decision / terminal in the handler's CFG. The
// shape drives the flowchart glyph: start/end terminals (rounded), decision
// (diamond, carries Condition), loop (carries Condition + is a back-edge
// target), process (rectangle, carries Effects), return/throw (terminals).
type v2CFGNode struct {
	ID    string `json:"id"`
	Shape string `json:"shape"`
	Line  int    `json:"line,omitempty"`
	// Label is the trimmed source line (full detail only).
	Label string `json:"label,omitempty"`
	// Condition is the predicate text on decision/loop nodes (decisions detail+).
	Condition string `json:"condition,omitempty"`
	// Effects annotate a process node (data detail+).
	Effects []v2CFGEffect `json:"effects,omitempty"`
	// Func is the inlined-function frame this node belongs to (#4883), keyed by
	// the per-hop frame id ("f0" = the handler, "f1"…= inlined callees). The
	// frontend uses it to draw a group box / labelled divider per inlined hop.
	// Empty/"f0" for the handler's own nodes (depth 1).
	Func string `json:"func,omitempty"`
	// External marks a call node whose in-repo callee could not be resolved /
	// recursed into (a third-party / library / cross-repo call); it is a leaf
	// terminal of the interprocedural CFG (#4883). The frontend badges these.
	External bool `json:"external,omitempty"`
}

// v2CFGEdge is one directed control-flow edge between two node IDs. Kind is one
// of seq / branch_true / branch_false / loop_back / exit so the renderer can
// label and route the flowchart edges (the true/false branches off a diamond,
// the loop back-edge, the early return/throw exits).
type v2CFGEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

// v2ControlFlowResponse is the payload for
// GET /api/v2/groups/:id/paths/:hash/control-flow.
type v2ControlFlowResponse struct {
	Path string `json:"path"`
	Verb string `json:"verb"`
	// Detail echoes the resolved detail level (outline|decisions|data|full).
	Detail string `json:"detail"`
	// Language is the resolved handler language slug ("python","jsts",…).
	Language string `json:"language"`
	// Supported is false when no block detector exists for the language (the CFG
	// degenerates to start→process→end). The frontend shows a graceful
	// "flowchart not available for this language" state when false.
	Supported bool `json:"supported"`
	// Note carries a human explanation when Supported is false (or the handler
	// could not be resolved / read).
	Note string `json:"note,omitempty"`
	// Handler describes the resolved handler function the CFG was built from, so
	// the modal can title the flowchart. Empty when no handler resolved.
	Handler *v2CFGHandler `json:"handler,omitempty"`
	// Cyclomatic is the McCabe cyclomatic complexity of the handler.
	Cyclomatic int `json:"cyclomatic_complexity"`
	// BranchCount is the raw decision-point count (Cyclomatic - 1).
	BranchCount int         `json:"branch_count"`
	Nodes       []v2CFGNode `json:"nodes"`
	Edges       []v2CFGEdge `json:"edges"`
	// Depth echoes the resolved inline depth (#4883): 1 = handler CFG only,
	// >=2 = callee CFGs inlined at in-repo call sites to that hop count.
	Depth int `json:"depth"`
	// Functions are the inlined-function frames (#4883), in inline order, so the
	// UI can label each group box / divider. f0 is always the handler.
	Functions []v2CFGFunction `json:"functions,omitempty"`
}

// v2CFGFunction describes one inlined-function frame in the interprocedural CFG
// (#4883) so the renderer can delineate the boundary of each inlined hop.
type v2CFGFunction struct {
	// Func is the frame id ("f0","f1",…) matching v2CFGNode.Func.
	Func string `json:"func"`
	// Name is the function/method name.
	Name string `json:"name"`
	// Kind is the (scope-stripped) entity kind.
	Kind string `json:"kind,omitempty"`
	// File / Line locate the function definition.
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
	// Depth is the inline hop this frame was spliced at (0 = handler).
	Depth int `json:"depth"`
	// Cyclomatic is this frame's own cyclomatic complexity.
	Cyclomatic int `json:"cyclomatic_complexity,omitempty"`
}

// v2CFGHandler describes the handler function the CFG was built from.
type v2CFGHandler struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
	Repo string `json:"repo"`
}

// cfgBodyFallbackSpan mirrors the MCP branchSourceSpan fallback: an entity whose
// recorded EndLine is degenerate (<= StartLine) gets a generous window; the
// CFG builder self-bounds by indentation / brace depth.
const cfgBodyFallbackSpan = 400

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// handleV2PathControlFlow — GET /api/v2/groups/:id/paths/:hash/control-flow
//
// Query params:
//
//	verb   — disambiguate when a path has multiple verb endpoints (optional;
//	         default = first verb by deterministic ID order — same as the DAG).
//	detail — outline | decisions (default) | data | full.
func (s *Server) handleV2PathControlFlow(w http.ResponseWriter, r *http.Request) {
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
	detail := normalizeCFGDetail(q.Get("detail"))
	depth := normalizeCFGDepth(q.Get("depth"))
	wantVerb := strings.ToUpper(strings.TrimSpace(q.Get("verb")))

	// Resolve the endpoint root for (path hash, verb) — same resolver the
	// downstream-DAG uses so the two surfaces never drift on which endpoint a
	// hash maps to.
	root := resolveDAGRoot(grp, pathHash, wantVerb)
	if root == nil {
		writeV2Err(w, http.StatusNotFound, "path_not_found", "no endpoint found for path hash: "+pathHash)
		return
	}

	resp := v2ControlFlowResponse{
		Path:   root.path,
		Verb:   root.verb,
		Detail: detail,
		Depth:  depth,
		Nodes:  []v2CFGNode{},
		Edges:  []v2CFGEdge{},
	}

	// Cross the HTTP boundary: the handler is the far side of the reversed
	// `handler --IMPLEMENTS--> endpoint def` continuation edge.
	handler := resolveCFGHandler(root)
	if handler == nil {
		resp.Supported = false
		resp.Note = "no handler function is linked to this endpoint (no IMPLEMENTS edge); cannot build a flowchart."
		writeV2JSON(w, http.StatusOK, v2OK(resp))
		return
	}
	resp.Handler = &v2CFGHandler{
		ID:   dashPrefixedID(root.repo.Slug, handler.ID),
		Name: handler.Name,
		Kind: dashStripScopePrefix(handler.Kind),
		File: handler.SourceFile,
		Line: handler.StartLine,
		Repo: root.repo.Slug,
	}

	lang := substrate.LanguageForPath(handler.SourceFile)
	resp.Language = lang

	src, start, ok := readCFGHandlerSource(grp, root.repo, handler)
	if !ok {
		resp.Supported = false
		resp.Note = "handler source window unreadable; cannot build a flowchart."
		writeV2JSON(w, http.StatusOK, v2OK(resp))
		return
	}

	// Build the INTERPROCEDURAL (inlined) CFG (#4883): the handler's own CFG at
	// depth 1, then at each in-repo CALL site splice the callee's CFG, recursing
	// to `depth`. The inliner REUSES the one substrate CFG builder per frame and
	// the downstream-DAG's controller→service→repo CALLS resolution so the two
	// surfaces never drift. depth==1 collapses to exactly the old single-function
	// CFG.
	in := newCFGInliner(grp, root.repo, depth)
	combined := in.build(handler, lang, src, start, detail)
	resp.Supported = combined.Supported
	resp.Cyclomatic = combined.Cyclomatic
	resp.BranchCount = combined.BranchCount
	if !combined.Supported {
		resp.Note = "flowchart not available for this language yet (validated: python, jsts); showing a degenerate graph."
	}
	resp.Nodes = combined.Nodes
	resp.Edges = combined.Edges
	resp.Functions = combined.Functions

	writeV2JSON(w, http.StatusOK, v2OK(resp))
}

// ---------------------------------------------------------------------------
// Handler resolution + source read
// ---------------------------------------------------------------------------

// resolveCFGHandler finds the handler function entity for an endpoint root by
// reversing the `handler --IMPLEMENTS--> http_endpoint_definition` edge — the
// same HTTP-boundary crossing the downstream-DAG uses. When several handlers
// implement the same endpoint (rare) the first by entity ID wins, deterministic.
func resolveCFGHandler(root *dagRoot) *graph.Entity {
	if root == nil || root.repo == nil || root.repo.Doc == nil {
		return nil
	}
	doc := root.repo.Doc
	byID := make(map[string]*graph.Entity, len(doc.Entities))
	for i := range doc.Entities {
		byID[doc.Entities[i].ID] = &doc.Entities[i]
	}
	var best *graph.Entity
	for i := range doc.Relationships {
		rel := &doc.Relationships[i]
		if rel.Kind != "IMPLEMENTS" || rel.ToID != root.ent.ID || rel.FromID == rel.ToID {
			continue
		}
		h := byID[rel.FromID]
		if h == nil {
			continue
		}
		if best == nil || h.ID < best.ID {
			best = h
		}
	}
	return best
}

// readCFGHandlerSource reads the handler's source window (raw, no line-number
// prefix — the CFG builder regexes over verbatim source) and returns it with the
// 1-indexed absolute start line. Resolves the on-disk path through the group's
// repo roots (path-traversal guarded by resolveSourcePath).
func readCFGHandlerSource(grp *DashGroup, repo *DashRepo, e *graph.Entity) (src string, start int, ok bool) {
	start = e.StartLine
	if start <= 0 {
		return "", 0, false
	}
	end := e.EndLine
	if end <= start {
		end = start + cfgBodyFallbackSpan // builder self-bounds by indent/brace.
	}

	abs, _, _, found := resolveSourcePath(grp, e.SourceFile, repo.Slug)
	if !found {
		return "", 0, false
	}
	all, err := readAllLines(abs)
	if err != nil || len(all) == 0 {
		return "", 0, false
	}
	if start > len(all) {
		return "", 0, false
	}
	if end > len(all) {
		end = len(all)
	}
	var b strings.Builder
	for i := start; i <= end; i++ {
		b.WriteString(all[i-1])
		b.WriteByte('\n')
	}
	out := b.String()
	if strings.TrimSpace(out) == "" {
		return "", 0, false
	}
	return out, start, true
}

// ---------------------------------------------------------------------------
// Detail-level serialisation (mirrors the MCP tool's cfgNodesToJSON, #2828)
// ---------------------------------------------------------------------------

// normalizeCFGDetail clamps the detail query param to a known level, defaulting
// to "decisions" (the same default as the MCP tool + the frontend slider).
func normalizeCFGDetail(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "outline":
		return "outline"
	case "data":
		return "data"
	case "full":
		return "full"
	case "", "decisions":
		return "decisions"
	default:
		return "decisions"
	}
}

// cfgMaxInlineDepth bounds the interprocedural inlining (#4883) so a deep /
// recursive call graph can never blow the payload or the request budget. The
// cycle-guard (visited-set) already prevents infinite recursion; this is the
// belt-and-braces hop cap, matching the downstream-DAG's depth ceiling spirit.
const cfgMaxInlineDepth = 8

// normalizeCFGDepth clamps the inline-depth query param to [1, cfgMaxInlineDepth],
// defaulting to 1 (handler-only CFG — the pre-#4883 behaviour).
func normalizeCFGDepth(s string) int {
	n := atoiSafe(strings.TrimSpace(s))
	if n < 1 {
		return 1
	}
	if n > cfgMaxInlineDepth {
		return cfgMaxInlineDepth
	}
	return n
}

// cfgDetailRank gives a numeric ordering so the serialiser can gate fields by
// "at least this level": outline < decisions < data < full.
func cfgDetailRank(detail string) int {
	switch detail {
	case "outline":
		return 0
	case "decisions":
		return 1
	case "data":
		return 2
	case "full":
		return 3
	default:
		return 1
	}
}

// cfgNodesToWire serialises CFG nodes, including only the fields the detail
// level asks for (token control, #2828) — identical gating to the MCP tool so
// the two surfaces produce the same shapes at the same level.
func cfgNodesToWire(nodes []substrate.CFGNode, detail string) []v2CFGNode {
	rank := cfgDetailRank(detail)
	out := make([]v2CFGNode, 0, len(nodes))
	for _, n := range nodes {
		w := v2CFGNode{ID: n.ID, Shape: string(n.Shape), Line: n.Line}
		if rank >= 1 && n.Condition != "" { // decisions+
			w.Condition = n.Condition
		}
		if rank >= 2 && len(n.Effects) > 0 { // data+
			effs := make([]v2CFGEffect, 0, len(n.Effects))
			for _, ef := range n.Effects {
				effs = append(effs, v2CFGEffect{Effect: ef.Effect, Sink: ef.Sink})
			}
			w.Effects = effs
		}
		if rank >= 3 && n.Label != "" { // full
			w.Label = n.Label
		}
		out = append(out, w)
	}
	return out
}

// cfgEdgesToWire serialises the control-flow edges (kind preserved for the
// flowchart edge labelling/routing).
func cfgEdgesToWire(edges []substrate.CFGEdge) []v2CFGEdge {
	out := make([]v2CFGEdge, 0, len(edges))
	for _, e := range edges {
		out = append(out, v2CFGEdge{From: e.From, To: e.To, Kind: string(e.Kind)})
	}
	return out
}
