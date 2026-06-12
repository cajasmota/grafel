package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// makeControlFlowFixture writes a real handler source file to a temp repo root
// (the CFG builder reads verbatim source from disk) and returns a DashGroup
// wired:
//
//	http_endpoint_definition (GET /inspections)
//	  <--IMPLEMENTS-- InspectionController.list (handler, the CFG target)
//
// The handler body has a decision (if), a loop (for), an early return, a throw,
// and a db_write effect line so the CFG exercises every shape + detail level.
func makeControlFlowFixture(t *testing.T) *DashGroup {
	t.Helper()
	root := t.TempDir()
	handlerSrc := `async list(req, res) {
  const user = req.user;
  if (!user) {
    throw new ForbiddenError("no user");
  }
  const rows = await this.repo.find(req.query);
  for (const row of rows) {
    await this.repo.save(row);
  }
  return res.json(rows);
}
`
	file := "inspection.controller.ts"
	if err := os.WriteFile(filepath.Join(root, file), []byte(handlerSrc), 0o644); err != nil {
		t.Fatalf("write handler src: %v", err)
	}

	ent := func(id, name, kind string, start, end int) graph.Entity {
		return graph.Entity{ID: id, Name: name, Kind: kind, SourceFile: file, StartLine: start, EndLine: end}
	}
	epEnt := graph.Entity{
		ID: "ep", Name: "GET /inspections", Kind: "http_endpoint_definition",
		SourceFile: "routers.ts", StartLine: 1,
		Properties: map[string]string{"verb": "GET", "path": "/inspections"},
	}
	// Handler spans lines 1..11 of the file written above.
	handler := ent("handler", "InspectionController.list", "Operation", 1, 11)

	doc := &graph.Document{
		Repo:     "api",
		Entities: []graph.Entity{epEnt, handler},
		Relationships: []graph.Relationship{
			{FromID: "handler", ToID: "ep", Kind: "IMPLEMENTS"},
		},
	}
	return &DashGroup{
		Name:  "testgrp",
		Repos: map[string]*DashRepo{"api": {Slug: "api", Path: root, Doc: doc}},
	}
}

// fetchControlFlow issues the control-flow request and decodes the payload.
func fetchControlFlow(t *testing.T, ts *httptest.Server, query string) v2ControlFlowResponse {
	t.Helper()
	pathHash := hashStr("/inspections")
	url := ts.URL + "/api/v2/groups/testgrp/paths/" + pathHash + "/control-flow"
	if query != "" {
		url += "?" + query
	}
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET control-flow: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body struct {
		OK   bool                  `json:"ok"`
		Data v2ControlFlowResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.OK {
		t.Fatalf("ok: want true")
	}
	return body.Data
}

func cfgShapes(nodes []v2CFGNode, shape string) []v2CFGNode {
	var out []v2CFGNode
	for _, n := range nodes {
		if n.Shape == shape {
			out = append(out, n)
		}
	}
	return out
}

func cfgHasEdgeKind(edges []v2CFGEdge, kind string) bool {
	for _, e := range edges {
		if e.Kind == kind {
			return true
		}
	}
	return false
}

// TestControlFlow_HandlerResolvedAndCFGBuilt asserts the endpoint→handler→CFG
// pipeline: the handler is resolved via the reversed IMPLEMENTS edge, the CFG is
// JS/TS-supported, and it carries the start/end terminals, a decision, a loop, a
// throw, and a return — plus the branch/loop/exit edge kinds.
func TestControlFlow_HandlerResolvedAndCFGBuilt(t *testing.T) {
	ts := newPathsTestServer(t, makeControlFlowFixture(t))
	defer ts.Close()

	cf := fetchControlFlow(t, ts, "") // default detail=decisions.

	if cf.Path != "/inspections" || cf.Verb != "GET" {
		t.Errorf("path/verb: got %q %q", cf.Path, cf.Verb)
	}
	if cf.Detail != "decisions" {
		t.Errorf("detail: want decisions, got %q", cf.Detail)
	}
	if cf.Language != "jsts" {
		t.Errorf("language: want jsts, got %q", cf.Language)
	}
	if !cf.Supported {
		t.Fatalf("supported: want true for jsts; note=%q", cf.Note)
	}
	if cf.Handler == nil || cf.Handler.Name != "InspectionController.list" {
		t.Fatalf("handler not resolved: %+v", cf.Handler)
	}
	if cf.Handler.ID != "api::handler" {
		t.Errorf("handler id: want api::handler, got %q", cf.Handler.ID)
	}

	if len(cfgShapes(cf.Nodes, "start")) != 1 {
		t.Errorf("want exactly one start node")
	}
	if len(cfgShapes(cf.Nodes, "end")) != 1 {
		t.Errorf("want exactly one end node")
	}
	if len(cfgShapes(cf.Nodes, "decision")) < 1 {
		t.Errorf("want a decision node for the `if`")
	}
	if len(cfgShapes(cf.Nodes, "loop")) < 1 {
		t.Errorf("want a loop node for the `for`")
	}
	if len(cfgShapes(cf.Nodes, "throw")) < 1 {
		t.Errorf("want a throw terminal")
	}
	if len(cfgShapes(cf.Nodes, "return")) < 1 {
		t.Errorf("want a return terminal")
	}
	if !cfgHasEdgeKind(cf.Edges, "loop_back") {
		t.Errorf("want a loop_back edge")
	}
	if !cfgHasEdgeKind(cf.Edges, "exit") {
		t.Errorf("want an exit edge (early return/throw)")
	}
	if cf.Cyclomatic < 2 {
		t.Errorf("cyclomatic: want >= 2 (if + for), got %d", cf.Cyclomatic)
	}
}

// TestControlFlow_DetailLevels asserts the detail slider maps to progressively
// richer payloads: outline drops conditions, decisions adds them, data adds
// effects, full adds labels. Identical gating to the MCP tool (#2828).
func TestControlFlow_DetailLevels(t *testing.T) {
	ts := newPathsTestServer(t, makeControlFlowFixture(t))
	defer ts.Close()

	anyCondition := func(ns []v2CFGNode) bool {
		for _, n := range ns {
			if n.Condition != "" {
				return true
			}
		}
		return false
	}
	anyEffect := func(ns []v2CFGNode) bool {
		for _, n := range ns {
			if len(n.Effects) > 0 {
				return true
			}
		}
		return false
	}
	anyLabel := func(ns []v2CFGNode) bool {
		for _, n := range ns {
			if n.Label != "" {
				return true
			}
		}
		return false
	}

	outline := fetchControlFlow(t, ts, "detail=outline")
	if outline.Detail != "outline" {
		t.Errorf("detail echo: got %q", outline.Detail)
	}
	if anyCondition(outline.Nodes) {
		t.Errorf("outline must not carry conditions")
	}
	if anyEffect(outline.Nodes) || anyLabel(outline.Nodes) {
		t.Errorf("outline must not carry effects/labels")
	}

	decisions := fetchControlFlow(t, ts, "detail=decisions")
	if !anyCondition(decisions.Nodes) {
		t.Errorf("decisions must carry condition text")
	}
	if anyEffect(decisions.Nodes) || anyLabel(decisions.Nodes) {
		t.Errorf("decisions must not carry effects/labels")
	}

	data := fetchControlFlow(t, ts, "detail=data")
	if !anyCondition(data.Nodes) {
		t.Errorf("data must carry conditions")
	}
	if !anyEffect(data.Nodes) {
		t.Errorf("data must carry effect annotations (db_write on save)")
	}
	if anyLabel(data.Nodes) {
		t.Errorf("data must not carry labels")
	}

	full := fetchControlFlow(t, ts, "detail=full")
	if !anyLabel(full.Nodes) {
		t.Errorf("full must carry node labels")
	}
}

// makeInlineCFGFixture wires an endpoint whose handler CALLS an in-repo service
// method, so the depth-controlled interprocedural inliner (#4883) has a callee
// to splice:
//
//	GET /orders
//	  <--IMPLEMENTS-- OrderController.list (handler)
//	         --CALLS--> OrderService.fetch  (the callee, its own CFG)
//
// The handler body invokes `this.service.fetch(...)`; the callee body has its
// own decision + return so the splice is observable (callee-only nodes appear).
func makeInlineCFGFixture(t *testing.T) *DashGroup {
	t.Helper()
	root := t.TempDir()

	handlerSrc := `async list(req, res) {
  const rows = await this.service.fetch(req.query);
  return res.json(rows);
}
`
	// Callee lives lower in the same file. Its first line is line 6 (1-indexed):
	// blank line 5 separates them.
	calleeSrc := `
async fetch(query) {
  if (!query) {
    throw new BadRequest("missing");
  }
  const rows = await this.repo.find(query);
  return rows;
}
`
	full := handlerSrc + calleeSrc
	file := "order.controller.ts"
	if err := os.WriteFile(filepath.Join(root, file), []byte(full), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}

	ent := func(id, name, kind string, start, end int) graph.Entity {
		return graph.Entity{ID: id, Name: name, Kind: kind, SourceFile: file, StartLine: start, EndLine: end}
	}
	epEnt := graph.Entity{
		ID: "ep", Name: "GET /orders", Kind: "http_endpoint_definition",
		SourceFile: "routers.ts", StartLine: 1,
		Properties: map[string]string{"verb": "GET", "path": "/orders"},
	}
	// Handler spans lines 1..4; callee `fetch` starts at line 6 (after the
	// handler's 4 lines + 1 blank).
	handler := ent("handler", "OrderController.list", "Operation", 1, 4)
	callee := ent("callee", "OrderService.fetch", "Operation", 6, 13)

	doc := &graph.Document{
		Repo:     "api",
		Entities: []graph.Entity{epEnt, handler, callee},
		Relationships: []graph.Relationship{
			{FromID: "handler", ToID: "ep", Kind: "IMPLEMENTS"},
			{FromID: "handler", ToID: "callee", Kind: "CALLS"},
		},
	}
	return &DashGroup{
		Name:  "testgrp",
		Repos: map[string]*DashRepo{"api": {Slug: "api", Path: root, Doc: doc}},
	}
}

func fetchOrdersControlFlow(t *testing.T, ts *httptest.Server, query string) v2ControlFlowResponse {
	t.Helper()
	pathHash := hashStr("/orders")
	url := ts.URL + "/api/v2/groups/testgrp/paths/" + pathHash + "/control-flow"
	if query != "" {
		url += "?" + query
	}
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET control-flow: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body struct {
		OK   bool                  `json:"ok"`
		Data v2ControlFlowResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body.Data
}

// TestControlFlow_DepthInlining asserts the #4883 interprocedural behaviour:
// depth=1 yields the handler-only CFG (one function frame, no callee nodes),
// while depth>=2 splices the callee's CFG at the in-repo call site — the
// callee's own decision/throw nodes appear and a second function frame is
// reported with its boundary label.
func TestControlFlow_DepthInlining(t *testing.T) {
	ts := newPathsTestServer(t, makeInlineCFGFixture(t))
	defer ts.Close()

	// Depth 1 — handler only.
	d1 := fetchOrdersControlFlow(t, ts, "depth=1&detail=full")
	if !d1.Supported {
		t.Fatalf("depth1: want supported; note=%q", d1.Note)
	}
	if d1.Depth != 1 {
		t.Errorf("depth1: echo want 1, got %d", d1.Depth)
	}
	if len(d1.Functions) != 1 {
		t.Errorf("depth1: want exactly one function frame, got %d", len(d1.Functions))
	}
	// The callee's `throw new BadRequest` line must NOT be present at depth 1.
	if cfgAnyLabelContains(d1.Nodes, "BadRequest") {
		t.Errorf("depth1: callee body must not be inlined")
	}

	// Depth 2 — callee spliced at the call site.
	d2 := fetchOrdersControlFlow(t, ts, "depth=2&detail=full")
	if !d2.Supported {
		t.Fatalf("depth2: want supported; note=%q", d2.Note)
	}
	if d2.Depth != 2 {
		t.Errorf("depth2: echo want 2, got %d", d2.Depth)
	}
	if len(d2.Functions) != 2 {
		t.Fatalf("depth2: want two function frames (handler+callee), got %d: %+v", len(d2.Functions), d2.Functions)
	}
	// A frame for the inlined callee, tagged at depth 1.
	var calleeFrame *v2CFGFunction
	for i := range d2.Functions {
		if d2.Functions[i].Name == "OrderService.fetch" {
			calleeFrame = &d2.Functions[i]
		}
	}
	if calleeFrame == nil {
		t.Fatalf("depth2: no inlined frame for OrderService.fetch: %+v", d2.Functions)
	}
	if calleeFrame.Depth != 1 {
		t.Errorf("depth2: callee frame depth want 1, got %d", calleeFrame.Depth)
	}
	// Callee body nodes must now appear (its throw), tagged with the callee frame.
	var sawCalleeNode bool
	for _, n := range d2.Nodes {
		if n.Func == calleeFrame.Func && strings.Contains(n.Label, "BadRequest") {
			sawCalleeNode = true
		}
	}
	if !sawCalleeNode {
		t.Errorf("depth2: callee body (throw BadRequest) not inlined under frame %q", calleeFrame.Func)
	}
	// Combined complexity must reflect BOTH frames' decision points.
	if d2.Cyclomatic <= d1.Cyclomatic {
		t.Errorf("depth2: combined cyclomatic (%d) must exceed handler-only (%d)", d2.Cyclomatic, d1.Cyclomatic)
	}
}

func cfgAnyLabelContains(nodes []v2CFGNode, sub string) bool {
	for _, n := range nodes {
		if strings.Contains(n.Label, sub) {
			return true
		}
	}
	return false
}

// TestControlFlow_NoHandler asserts a graceful supported=false when no handler
// implements the endpoint (no IMPLEMENTS edge).
func TestControlFlow_NoHandler(t *testing.T) {
	grp := makeControlFlowFixture(t)
	// Drop the IMPLEMENTS edge so no handler resolves.
	grp.Repos["api"].Doc.Relationships = nil

	ts := newPathsTestServer(t, grp)
	defer ts.Close()

	cf := fetchControlFlow(t, ts, "")
	if cf.Supported {
		t.Errorf("supported: want false with no handler")
	}
	if cf.Handler != nil {
		t.Errorf("handler: want nil, got %+v", cf.Handler)
	}
	if cf.Note == "" {
		t.Errorf("want an explanatory note when no handler resolves")
	}
}
