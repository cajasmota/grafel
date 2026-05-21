package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
)

// ── Renderer unit tests ───────────────────────────────────────────────────────

func TestRenderMermaid_basic(t *testing.T) {
	nodes := []exportNode{
		{ID: "repo::funcA", Label: "funcA", Kind: "Function", Repo: "repo"},
		{ID: "repo::funcB", Label: "funcB", Kind: "Class", Repo: "repo"},
	}
	edges := []exportEdge{
		{FromID: "repo::funcA", ToID: "repo::funcB", Kind: "calls"},
	}

	out := renderMermaid(nodes, edges)

	if !strings.Contains(out, "flowchart LR") {
		t.Error("mermaid output should start with flowchart LR")
	}
	if !strings.Contains(out, "funcA") {
		t.Error("mermaid output should contain funcA label")
	}
	if !strings.Contains(out, "calls") {
		t.Error("mermaid output should contain edge kind 'calls'")
	}
	if !strings.Contains(out, "fill:") {
		t.Error("mermaid output should contain style fill declarations")
	}
	// Color tokens: Function → blue
	if !strings.Contains(out, "#bfdbfe") {
		t.Errorf("Function nodes should use blue fill #bfdbfe; got:\n%s", out)
	}
}

func TestRenderGraphviz_basic(t *testing.T) {
	nodes := []exportNode{
		{ID: "repo::ep1", Label: "GET /users", Kind: "Endpoint", Repo: "repo"},
		{ID: "repo::svc1", Label: "UserService", Kind: "Service", Repo: "repo"},
	}
	edges := []exportEdge{
		{FromID: "repo::ep1", ToID: "repo::svc1", Kind: "invokes"},
	}

	out := renderGraphviz(nodes, edges)

	if !strings.Contains(out, "digraph subgraph") {
		t.Error("graphviz output should contain 'digraph subgraph'")
	}
	if !strings.Contains(out, "rankdir=LR") {
		t.Error("graphviz output should set rankdir=LR")
	}
	if !strings.Contains(out, "invokes") {
		t.Error("graphviz output should contain edge kind")
	}
	// Endpoint → green
	if !strings.Contains(out, "#bbf7d0") {
		t.Errorf("Endpoint nodes should use green fill; got:\n%s", out)
	}
}

func TestRenderPlantUML_basic(t *testing.T) {
	nodes := []exportNode{
		{ID: "repo::topic1", Label: "user.created", Kind: "MessageTopic", Repo: "repo"},
		{ID: "repo::svc1", Label: "NotifSvc", Kind: "Service", Repo: "repo"},
	}
	edges := []exportEdge{
		{FromID: "repo::svc1", ToID: "repo::topic1", Kind: "publishes"},
	}

	out := renderPlantUML(nodes, edges)

	if !strings.Contains(out, "@startuml") {
		t.Error("plantuml output should start with @startuml")
	}
	if !strings.Contains(out, "@enduml") {
		t.Error("plantuml output should end with @enduml")
	}
	if !strings.Contains(out, "publishes") {
		t.Error("plantuml output should contain edge kind")
	}
	// MessageTopic → purple e9d5ff
	if !strings.Contains(out, "e9d5ff") {
		t.Errorf("MessageTopic nodes should use purple; got:\n%s", out)
	}
}

func TestRenderD2_basic(t *testing.T) {
	nodes := []exportNode{
		{ID: "repo::proc1", Label: "OrderProcess", Kind: "Process", Repo: "repo"},
		{ID: "repo::comp1", Label: "PaymentComp", Kind: "Component", Repo: "repo"},
	}
	edges := []exportEdge{
		{FromID: "repo::proc1", ToID: "repo::comp1", Kind: "uses"},
	}

	out := renderD2(nodes, edges)

	if !strings.Contains(out, "direction: right") {
		t.Error("d2 output should set direction")
	}
	if !strings.Contains(out, "uses") {
		t.Error("d2 output should contain edge kind")
	}
	// Process → orange fed7aa
	if !strings.Contains(out, "fed7aa") {
		t.Errorf("Process nodes should use orange fill; got:\n%s", out)
	}
}

func TestRenderMermaid_labelTruncation(t *testing.T) {
	long := strings.Repeat("x", 60)
	nodes := []exportNode{
		{ID: "repo::n1", Label: long, Kind: "Function", Repo: "repo"},
	}
	out := renderMermaid(nodes, nil)
	// Should contain truncated label ending in …
	if !strings.Contains(out, "…") {
		t.Error("long labels should be truncated with ellipsis in mermaid")
	}
}

func TestRenderGraphviz_labelTruncation(t *testing.T) {
	long := strings.Repeat("y", 50)
	nodes := []exportNode{
		{ID: "repo::n1", Label: long, Kind: "Class", Repo: "repo"},
	}
	out := renderGraphviz(nodes, nil)
	if !strings.Contains(out, "...") {
		t.Error("long labels should be truncated with ... in graphviz")
	}
}

func TestRenderMermaid_defaultStyleFallback(t *testing.T) {
	nodes := []exportNode{
		{ID: "repo::n1", Label: "Unknown", Kind: "WeirdKind", Repo: "repo"},
	}
	out := renderMermaid(nodes, nil)
	// Should use default grey fill
	if !strings.Contains(out, "#f1f5f9") {
		t.Errorf("unknown kind should fall back to default fill; got:\n%s", out)
	}
}

// ── BFS subgraph tests ────────────────────────────────────────────────────────

func makeBFSTestRepo() *DashRepo {
	entities := []graph.Entity{
		{ID: "e1", Name: "Entry", Kind: "Function"},
		{ID: "e2", Name: "Middle", Kind: "Class"},
		{ID: "e3", Name: "Leaf", Kind: "Function"},
		{ID: "e4", Name: "DeepLeaf", Kind: "Function"},
		{ID: "e5", Name: "Unconnected", Kind: "Function"},
	}
	rels := []graph.Relationship{
		{FromID: "e1", ToID: "e2", Kind: "calls"},
		{FromID: "e2", ToID: "e3", Kind: "calls"},
		{FromID: "e3", ToID: "e4", Kind: "calls"},
	}
	return &DashRepo{
		Slug: "testrepo",
		Doc: &graph.Document{
			Entities:      entities,
			Relationships: rels,
		},
	}
}

func TestBFSSubgraph_depth1(t *testing.T) {
	repo := makeBFSTestRepo()
	root := &repo.Doc.Entities[0] // e1
	nodes, _ := bfsSubgraph(repo, root, 1, 50)

	ids := nodeIDs(nodes)
	if !ids["testrepo::e1"] {
		t.Error("root should be in nodes")
	}
	if !ids["testrepo::e2"] {
		t.Error("depth-1 neighbor e2 should be in nodes")
	}
	if ids["testrepo::e3"] {
		t.Error("depth-2 node e3 should NOT be in nodes at depth=1")
	}
}

func TestBFSSubgraph_depth2(t *testing.T) {
	repo := makeBFSTestRepo()
	root := &repo.Doc.Entities[0] // e1
	nodes, edges := bfsSubgraph(repo, root, 2, 50)

	ids := nodeIDs(nodes)
	if !ids["testrepo::e1"] || !ids["testrepo::e2"] || !ids["testrepo::e3"] {
		t.Error("depth-2 traversal should include e1, e2, e3")
	}
	if ids["testrepo::e4"] {
		t.Error("depth-3 node e4 should NOT appear at depth=2")
	}
	if ids["testrepo::e5"] {
		t.Error("unconnected node e5 should never appear")
	}

	if len(edges) == 0 {
		t.Error("edges should be non-empty")
	}
}

func TestBFSSubgraph_limit(t *testing.T) {
	repo := makeBFSTestRepo()
	root := &repo.Doc.Entities[0]
	nodes, _ := bfsSubgraph(repo, root, 10, 2) // limit=2

	if len(nodes) > 2 {
		t.Errorf("limit=2 should produce at most 2 nodes, got %d", len(nodes))
	}
}

func TestBFSSubgraph_nilDoc(t *testing.T) {
	repo := &DashRepo{Slug: "empty", Doc: nil}
	nodes, edges := bfsSubgraph(repo, &graph.Entity{ID: "x"}, 2, 50)
	if len(nodes) != 0 || len(edges) != 0 {
		t.Error("nil doc should return empty results")
	}
}

func nodeIDs(nodes []exportNode) map[string]bool {
	m := map[string]bool{}
	for _, n := range nodes {
		m[n.ID] = true
	}
	return m
}

// ── HTTP handler tests ────────────────────────────────────────────────────────

func newExportTestServer(t *testing.T) *Server {
	t.Helper()
	srv, err := NewServer(DefaultConfig(), newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv
}

func TestHandleExportDSL_missingFormat(t *testing.T) {
	srv := newExportTestServer(t)
	// path variable "format" will be empty → 400
	req := httptest.NewRequest(http.MethodGet, "/api/export/mygroup/myentity/", nil)
	req.SetPathValue("group", "mygroup")
	req.SetPathValue("entity_id", "myentity")
	req.SetPathValue("format", "")
	w := httptest.NewRecorder()
	srv.handleExportDSL(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleExportDSL_invalidFormat(t *testing.T) {
	srv := newExportTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/export/g/e/svg", nil)
	req.SetPathValue("group", "g")
	req.SetPathValue("entity_id", "e")
	req.SetPathValue("format", "svg")
	w := httptest.NewRecorder()
	srv.handleExportDSL(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400 for unknown format, got %d", w.Code)
	}
}

func TestHandleExportDSL_groupNotFound(t *testing.T) {
	srv := newExportTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/export/nogroup/eid/mermaid", nil)
	req.SetPathValue("group", "nogroup")
	req.SetPathValue("entity_id", "eid")
	req.SetPathValue("format", "mermaid")
	w := httptest.NewRecorder()
	srv.handleExportDSL(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404 for unknown group, got %d", w.Code)
	}
}

func TestHandleExportDSL_allFormats(t *testing.T) {
	// Wire a fake store with a live group+entity.
	srv, _ := NewServer(DefaultConfig(), newFakeStore())
	// Inject a graph group with a real entity so handler reaches renderers.
	grp := &DashGroup{
		Name: "testgroup",
		Repos: map[string]*DashRepo{
			"repo1": {
				Slug: "repo1",
				Doc: &graph.Document{
					Entities: []graph.Entity{
						{ID: "fn1", Name: "MyFunc", Kind: "Function"},
						{ID: "fn2", Name: "Helper", Kind: "Function"},
					},
					Relationships: []graph.Relationship{
						{FromID: "fn1", ToID: "fn2", Kind: "calls"},
					},
				},
			},
		},
	}
	// Inject into cache directly.
	srv.graphs.mu.Lock()
	srv.graphs.entries["testgroup"] = &cacheEntry{group: grp, loadedAt: time.Now()}
	srv.graphs.mu.Unlock()

	for _, fmt := range []string{"mermaid", "graphviz", "plantuml", "d2"} {
		t.Run(fmt, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet,
				"/api/export/testgroup/repo1::fn1/"+fmt+"?depth=1", nil)
			req.SetPathValue("group", "testgroup")
			req.SetPathValue("entity_id", "repo1::fn1")
			req.SetPathValue("format", fmt)
			w := httptest.NewRecorder()
			srv.handleExportDSL(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
			}
			ct := w.Header().Get("Content-Type")
			if !strings.HasPrefix(ct, "text/plain") {
				t.Errorf("want text/plain content-type, got %q", ct)
			}
			body := w.Body.String()
			if len(body) == 0 {
				t.Error("expected non-empty DSL body")
			}
			// Verify node count header.
			nc := w.Header().Get("X-Export-NodeCount")
			if nc == "" {
				t.Error("X-Export-NodeCount header should be present")
			}
		})
	}
}

func TestHandleExportDSL_entityNotFound(t *testing.T) {
	srv, _ := NewServer(DefaultConfig(), newFakeStore())
	grp := &DashGroup{
		Name: "testgroup",
		Repos: map[string]*DashRepo{
			"repo1": {
				Slug: "repo1",
				Doc: &graph.Document{
					Entities: []graph.Entity{{ID: "fn1", Name: "MyFunc", Kind: "Function"}},
				},
			},
		},
	}
	srv.graphs.mu.Lock()
	srv.graphs.entries["testgroup"] = &cacheEntry{group: grp, loadedAt: time.Now()}
	srv.graphs.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/export/testgroup/repo1::doesnotexist/mermaid", nil)
	req.SetPathValue("group", "testgroup")
	req.SetPathValue("entity_id", "repo1::doesnotexist")
	req.SetPathValue("format", "mermaid")
	w := httptest.NewRecorder()
	srv.handleExportDSL(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("want 404 for unknown entity, got %d", w.Code)
	}
}

func TestHandleExportDSL_depthCap(t *testing.T) {
	// depth=99 should be capped to 5.
	srv, _ := NewServer(DefaultConfig(), newFakeStore())
	grp := &DashGroup{
		Name: "g",
		Repos: map[string]*DashRepo{
			"r": {
				Slug: "r",
				Doc: &graph.Document{
					Entities: []graph.Entity{{ID: "e1", Name: "Root", Kind: "Function"}},
				},
			},
		},
	}
	srv.graphs.mu.Lock()
	srv.graphs.entries["g"] = &cacheEntry{group: grp, loadedAt: time.Now()}
	srv.graphs.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/export/g/r::e1/mermaid?depth=99", nil)
	req.SetPathValue("group", "g")
	req.SetPathValue("entity_id", "r::e1")
	req.SetPathValue("format", "mermaid")
	w := httptest.NewRecorder()
	// Must not hang or blow the stack.
	srv.handleExportDSL(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
}
