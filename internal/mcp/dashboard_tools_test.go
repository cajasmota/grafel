package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newTestServerWithDoc builds a minimal Server with one group ("test") and
// one repo ("repo1") loaded from the supplied Document.
func newTestServerWithDoc(t *testing.T, doc *graph.Document) *Server {
	t.Helper()
	reg := &Registry{Groups: map[string]RegistryGroup{
		"test": {
			Repos: map[string]RegistryRepo{
				"repo1": {Path: t.TempDir()},
			},
		},
	}}
	st := NewState(reg)
	// Inject the document directly rather than loading from disk.
	st.mu.Lock()
	st.groups["test"] = &LoadedGroup{
		Name: "test",
		Repos: map[string]*LoadedRepo{
			"repo1": {
				Repo:       "repo1",
				Doc:        doc,
				LabelIndex: BuildLabelIndex(doc),
				BM25:       BuildBM25(doc),
			},
		},
	}
	st.mu.Unlock()
	srv := &Server{State: st, Tel: NewTelemetry(0)}
	return srv
}

// callDashboardTool invokes a handler directly and decodes the JSON result.
func callDashboardTool(t *testing.T, fn func(context.Context, mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error), args map[string]any) map[string]any {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	res, err := fn(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil {
		t.Fatal("handler returned nil result")
	}
	if res.IsError {
		t.Fatalf("handler returned tool error: %v", res.Content)
	}
	var out map[string]any
	for _, c := range res.Content {
		if tc, ok := c.(mcpapi.TextContent); ok {
			if err := json.Unmarshal([]byte(tc.Text), &out); err != nil {
				t.Fatalf("decode result: %v\nraw: %s", err, tc.Text)
			}
			return out
		}
	}
	t.Fatal("no text content in result")
	return nil
}

// minDoc builds a minimal document.
func minDoc(entities []graph.Entity, rels []graph.Relationship) *graph.Document {
	return &graph.Document{
		Version:       1,
		Repo:          "repo1",
		Entities:      entities,
		Relationships: rels,
	}
}

// ---------------------------------------------------------------------------
// archigraph_topology_orphan_publishers
// ---------------------------------------------------------------------------

func TestHandleTopologyOrphanPublishers(t *testing.T) {
	entities := []graph.Entity{
		{ID: "t1", Name: "order.created", Kind: "Topic", SourceFile: "events.go"},
		{ID: "t2", Name: "user.updated", Kind: "Topic", SourceFile: "events.go"},
		{ID: "svc", Name: "OrderService", Kind: "Class", SourceFile: "service.go"},
	}
	// t1: published but not subscribed → orphan publisher
	// t2: published AND subscribed → not orphan
	rels := []graph.Relationship{
		{ID: "r1", FromID: "svc", ToID: "t1", Kind: "PUBLISHES_TO"},
		{ID: "r2", FromID: "svc", ToID: "t2", Kind: "PUBLISHES_TO"},
		{ID: "r3", FromID: "svc", ToID: "t2", Kind: "SUBSCRIBES_TO"},
	}
	srv := newTestServerWithDoc(t, minDoc(entities, rels))
	out := callDashboardTool(t, srv.handleTopologyOrphanPublishers, map[string]any{"group": "test"})

	count := int(out["count"].(float64))
	if count != 1 {
		t.Fatalf("expected 1 orphan publisher, got %d", count)
	}
	pubs := out["orphan_publishers"].([]any)
	first := pubs[0].(map[string]any)
	if first["topic_name"] != "order.created" {
		t.Errorf("expected order.created, got %v", first["topic_name"])
	}
}

// ---------------------------------------------------------------------------
// archigraph_topology_orphan_subscribers
// ---------------------------------------------------------------------------

func TestHandleTopologyOrphanSubscribers(t *testing.T) {
	entities := []graph.Entity{
		{ID: "t1", Name: "order.created", Kind: "Topic"},
		{ID: "t2", Name: "ghost.topic", Kind: "Topic"}, // subscribed but never published
		{ID: "svc", Name: "ConsumerService", Kind: "Class"},
	}
	rels := []graph.Relationship{
		{ID: "r1", FromID: "svc", ToID: "t1", Kind: "PUBLISHES_TO"},
		{ID: "r2", FromID: "svc", ToID: "t1", Kind: "SUBSCRIBES_TO"},
		{ID: "r3", FromID: "svc", ToID: "t2", Kind: "SUBSCRIBES_TO"},
	}
	srv := newTestServerWithDoc(t, minDoc(entities, rels))
	out := callDashboardTool(t, srv.handleTopologyOrphanSubscribers, map[string]any{"group": "test"})
	count := int(out["count"].(float64))
	if count != 1 {
		t.Fatalf("expected 1 orphan subscriber, got %d", count)
	}
	subs := out["orphan_subscribers"].([]any)
	first := subs[0].(map[string]any)
	if first["topic_name"] != "ghost.topic" {
		t.Errorf("expected ghost.topic, got %v", first["topic_name"])
	}
}

// ---------------------------------------------------------------------------
// archigraph_topology_topic_detail
// ---------------------------------------------------------------------------

func TestHandleTopologyTopicDetail(t *testing.T) {
	entities := []graph.Entity{
		{ID: "t1", Name: "payment.processed", Kind: "Topic"},
		{ID: "pub", Name: "PaymentService", Kind: "Class"},
		{ID: "sub", Name: "NotificationService", Kind: "Class"},
	}
	rels := []graph.Relationship{
		{ID: "r1", FromID: "pub", ToID: "t1", Kind: "PUBLISHES_TO"},
		{ID: "r2", FromID: "sub", ToID: "t1", Kind: "SUBSCRIBES_TO"},
	}
	srv := newTestServerWithDoc(t, minDoc(entities, rels))
	out := callDashboardTool(t, srv.handleTopologyTopicDetail, map[string]any{
		"group":    "test",
		"topic_id": "t1",
	})
	if out["found"] != true {
		t.Fatal("expected found=true")
	}
	pubs := out["publishers"].([]any)
	subs := out["subscribers"].([]any)
	if len(pubs) != 1 {
		t.Errorf("expected 1 publisher, got %d", len(pubs))
	}
	if len(subs) != 1 {
		t.Errorf("expected 1 subscriber, got %d", len(subs))
	}
}

func TestHandleTopologyTopicDetail_NotFound(t *testing.T) {
	srv := newTestServerWithDoc(t, minDoc(nil, nil))
	out := callDashboardTool(t, srv.handleTopologyTopicDetail, map[string]any{
		"group":    "test",
		"topic_id": "nonexistent",
	})
	if out["found"] != false {
		t.Errorf("expected found=false for nonexistent topic")
	}
}

// ---------------------------------------------------------------------------
// archigraph_flow_dead_ends
// ---------------------------------------------------------------------------

func TestHandleFlowDeadEnds(t *testing.T) {
	entities := []graph.Entity{
		{ID: "p1", Name: "CheckoutFlow", Kind: "SCOPE.Process", Properties: map[string]string{
			"terminal_id": "fn2",
			"step_count":  "2",
		}},
		{ID: "fn1", Name: "addToCart", Kind: "Function"},
		{ID: "fn2", Name: "processPayment", Kind: "Function"},
	}
	// fn2 has no outbound CALLS — dead end
	rels := []graph.Relationship{
		{ID: "r1", FromID: "fn1", ToID: "fn2", Kind: "CALLS"},
	}
	srv := newTestServerWithDoc(t, minDoc(entities, rels))
	out := callDashboardTool(t, srv.handleFlowDeadEnds, map[string]any{"group": "test"})
	count := int(out["count"].(float64))
	if count != 1 {
		t.Fatalf("expected 1 dead end, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// archigraph_flow_truncated
// ---------------------------------------------------------------------------

func TestHandleFlowTruncated(t *testing.T) {
	entities := []graph.Entity{
		{ID: "p1", Name: "PaymentFlow", Kind: "SCOPE.Process", Properties: map[string]string{
			"truncated":        "true",
			"truncated_reason": "max_depth exceeded",
			"step_count":       "3",
		}},
		{ID: "p2", Name: "NormalFlow", Kind: "SCOPE.Process", Properties: map[string]string{
			"step_count": "5",
		}},
	}
	srv := newTestServerWithDoc(t, minDoc(entities, nil))
	out := callDashboardTool(t, srv.handleFlowTruncated, map[string]any{"group": "test"})
	count := int(out["count"].(float64))
	if count != 1 {
		t.Fatalf("expected 1 truncated flow, got %d", count)
	}
	flows := out["truncated_flows"].([]any)
	first := flows[0].(map[string]any)
	if first["process_name"] != "PaymentFlow" {
		t.Errorf("expected PaymentFlow, got %v", first["process_name"])
	}
}

// ---------------------------------------------------------------------------
// archigraph_flow_detail
// ---------------------------------------------------------------------------

func TestHandleFlowDetail(t *testing.T) {
	entities := []graph.Entity{
		{ID: "p1", Name: "LoginFlow", Kind: "SCOPE.Process", Properties: map[string]string{
			"entry_id":    "fn1",
			"terminal_id": "fn2",
			"cross_stack": "true",
		}},
		{ID: "fn1", Name: "validateCredentials", Kind: "Function"},
		{ID: "fn2", Name: "issueToken", Kind: "Function"},
		{ID: "fx1", Name: "emitAuditLog", Kind: "Function"},
	}
	rels := []graph.Relationship{
		{ID: "r1", FromID: "p1", ToID: "fn1", Kind: "STEP_IN_PROCESS", Properties: map[string]string{"step_index": "0"}},
		{ID: "r2", FromID: "p1", ToID: "fn2", Kind: "STEP_IN_PROCESS", Properties: map[string]string{"step_index": "1"}},
		{ID: "r3", FromID: "fx1", ToID: "fn2", Kind: "SIDE_EFFECT_OF"},
	}
	srv := newTestServerWithDoc(t, minDoc(entities, rels))
	out := callDashboardTool(t, srv.handleFlowDetail, map[string]any{
		"group":      "test",
		"process_id": "p1",
	})
	if out["found"] != true {
		t.Fatalf("expected found=true")
	}
	steps := out["steps"].([]any)
	if len(steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(steps))
	}
	fx := out["side_effects"].([]any)
	if len(fx) != 1 {
		t.Errorf("expected 1 side effect, got %d", len(fx))
	}
}

// ---------------------------------------------------------------------------
// archigraph_diagnostics
// ---------------------------------------------------------------------------

func TestHandleDiagnostics(t *testing.T) {
	entities := []graph.Entity{{ID: "e1", Name: "Foo", Kind: "Function"}}
	srv := newTestServerWithDoc(t, minDoc(entities, nil))
	out := callDashboardTool(t, srv.handleDiagnostics, map[string]any{"group": "test"})
	if out["group"] != "test" {
		t.Errorf("expected group=test, got %v", out["group"])
	}
	repos := out["repos"].([]any)
	if len(repos) != 1 {
		t.Errorf("expected 1 repo entry, got %d", len(repos))
	}
	r := repos[0].(map[string]any)
	if r["loaded"] != true {
		t.Errorf("expected repo to be loaded")
	}
	if r["entities"].(float64) != 1 {
		t.Errorf("expected 1 entity, got %v", r["entities"])
	}
}

// ---------------------------------------------------------------------------
// archigraph_quality_orphans
// ---------------------------------------------------------------------------

func TestHandleQualityOrphans(t *testing.T) {
	entities := []graph.Entity{
		{ID: "e1", Name: "connected", Kind: "Function"},
		{ID: "e2", Name: "isolated", Kind: "Function"},
	}
	rels := []graph.Relationship{
		{ID: "r1", FromID: "e1", ToID: "e1", Kind: "CALLS"}, // self loop — e1 is connected
	}
	srv := newTestServerWithDoc(t, minDoc(entities, rels))
	out := callDashboardTool(t, srv.handleQualityOrphans, map[string]any{"group": "test"})
	count := int(out["count"].(float64))
	if count != 1 {
		t.Fatalf("expected 1 orphan, got %d", count)
	}
	orphans := out["orphans"].([]any)
	first := orphans[0].(map[string]any)
	if first["entity_name"] != "isolated" {
		t.Errorf("expected isolated, got %v", first["entity_name"])
	}
}

// ---------------------------------------------------------------------------
// archigraph_search_entities
// ---------------------------------------------------------------------------

func TestHandleSearchEntities(t *testing.T) {
	entities := []graph.Entity{
		{ID: "e1", Name: "PaymentService", Kind: "Class", SourceFile: "payment.go"},
		{ID: "e2", Name: "PaymentRepository", Kind: "Class", SourceFile: "repo.go"},
		{ID: "e3", Name: "UserService", Kind: "Class", SourceFile: "user.go"},
	}
	srv := newTestServerWithDoc(t, minDoc(entities, nil))
	out := callDashboardTool(t, srv.handleSearchEntities, map[string]any{
		"group": "test",
		"query": "Payment",
	})
	count := int(out["count"].(float64))
	if count != 2 {
		t.Fatalf("expected 2 results for 'Payment', got %d", count)
	}
}

func TestHandleSearchEntities_KindFilter(t *testing.T) {
	entities := []graph.Entity{
		{ID: "e1", Name: "processPayment", Kind: "Function"},
		{ID: "e2", Name: "PaymentService", Kind: "Class"},
	}
	srv := newTestServerWithDoc(t, minDoc(entities, nil))
	out := callDashboardTool(t, srv.handleSearchEntities, map[string]any{
		"group":       "test",
		"query":       "payment",
		"kind_filter": "Function",
	})
	count := int(out["count"].(float64))
	if count != 1 {
		t.Fatalf("expected 1 result with kind_filter=Function, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// archigraph_get_subgraph
// ---------------------------------------------------------------------------

func TestHandleGetSubgraph(t *testing.T) {
	entities := []graph.Entity{
		{ID: "root", Name: "Root", Kind: "Function"},
		{ID: "child", Name: "Child", Kind: "Function"},
		{ID: "grandchild", Name: "GrandChild", Kind: "Function"},
		{ID: "distant", Name: "Distant", Kind: "Function"},
	}
	rels := []graph.Relationship{
		{ID: "r1", FromID: "root", ToID: "child", Kind: "CALLS"},
		{ID: "r2", FromID: "child", ToID: "grandchild", Kind: "CALLS"},
		{ID: "r3", FromID: "grandchild", ToID: "distant", Kind: "CALLS"},
	}
	srv := newTestServerWithDoc(t, minDoc(entities, rels))

	// depth=1: root + child
	out := callDashboardTool(t, srv.handleGetSubgraph, map[string]any{
		"group":     "test",
		"entity_id": "root",
		"depth":     float64(1),
	})
	nc := int(out["node_count"].(float64))
	if nc != 2 {
		t.Errorf("depth=1 expected 2 nodes, got %d", nc)
	}

	// depth=2: root + child + grandchild
	out2 := callDashboardTool(t, srv.handleGetSubgraph, map[string]any{
		"group":     "test",
		"entity_id": "root",
		"depth":     float64(2),
	})
	nc2 := int(out2["node_count"].(float64))
	if nc2 != 3 {
		t.Errorf("depth=2 expected 3 nodes, got %d", nc2)
	}
}

// ---------------------------------------------------------------------------
// archigraph_find_paths
// ---------------------------------------------------------------------------

func TestHandleFindPaths(t *testing.T) {
	entities := []graph.Entity{
		{ID: "a", Name: "A", Kind: "Function"},
		{ID: "b", Name: "B", Kind: "Function"},
		{ID: "c", Name: "C", Kind: "Function"},
	}
	rels := []graph.Relationship{
		{ID: "r1", FromID: "a", ToID: "b", Kind: "CALLS"},
		{ID: "r2", FromID: "b", ToID: "c", Kind: "CALLS"},
	}
	srv := newTestServerWithDoc(t, minDoc(entities, rels))
	out := callDashboardTool(t, srv.handleFindPaths, map[string]any{
		"group": "test",
		"from":  "a",
		"to":    "c",
	})
	if out["found"] != true {
		t.Fatalf("expected found=true")
	}
	hops := int(out["hop_count"].(float64))
	if hops != 2 {
		t.Errorf("expected 2 hops (a→b→c), got %d", hops)
	}
}

func TestHandleFindPaths_NoPath(t *testing.T) {
	entities := []graph.Entity{
		{ID: "a", Name: "A", Kind: "Function"},
		{ID: "b", Name: "B", Kind: "Function"},
	}
	srv := newTestServerWithDoc(t, minDoc(entities, nil))
	out := callDashboardTool(t, srv.handleFindPaths, map[string]any{
		"group": "test",
		"from":  "a",
		"to":    "b",
	})
	if out["found"] != false {
		t.Errorf("expected found=false when no path exists")
	}
}

// ---------------------------------------------------------------------------
// parseSimpleFloat
// ---------------------------------------------------------------------------

func TestParseSimpleFloat(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"0.85", 0.85},
		{"1", 1.0},
		{"0.0", 0.0},
		{"1.23", 1.23},
	}
	for _, tc := range cases {
		got := parseSimpleFloat(tc.in)
		if got != tc.want {
			t.Errorf("parseSimpleFloat(%q) = %f, want %f", tc.in, got, tc.want)
		}
	}
}
