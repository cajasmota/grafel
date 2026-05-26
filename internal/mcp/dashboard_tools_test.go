package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

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
	return extractResultJSON(t, res)
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
	srv := newTestServer(t, minDoc(entities, rels))
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
	srv := newTestServer(t, minDoc(entities, rels))
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
	srv := newTestServer(t, minDoc(entities, rels))
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
	srv := newTestServer(t, minDoc(nil, nil))
	out := callDashboardTool(t, srv.handleTopologyTopicDetail, map[string]any{
		"group":    "test",
		"topic_id": "nonexistent",
	})
	if out["found"] != false {
		t.Errorf("expected found=false for nonexistent topic")
	}
}

// ---------------------------------------------------------------------------
// #1703 — search_entities → topic_detail round-trip consistency
// ---------------------------------------------------------------------------

// TestTopicDetail_PrefixedIDRoundtrip verifies that the entity_id returned by
// search_entities (a "repo::hash" prefixed form) is accepted by topic_detail.
// This is the core tool-pair-consistency requirement from #1703.
func TestTopicDetail_PrefixedIDRoundtrip(t *testing.T) {
	entities := []graph.Entity{
		{ID: "t1", Name: "payments.settled", Kind: "SCOPE.Queue"},
		{ID: "pub", Name: "PaymentService", Kind: "Class"},
		{ID: "sub", Name: "AuditService", Kind: "Class"},
	}
	rels := []graph.Relationship{
		{ID: "r1", FromID: "pub", ToID: "t1", Kind: "PUBLISHES_TO"},
		{ID: "r2", FromID: "sub", ToID: "t1", Kind: "SUBSCRIBES_TO"},
	}
	srv := newTestServer(t, minDoc(entities, rels))

	// Simulate what search_entities returns: prefixedID(r.Repo, e.ID) = "repo1::t1".
	prefixedTopicID := prefixedID("repo1", "t1")

	out := callDashboardTool(t, srv.handleTopologyTopicDetail, map[string]any{
		"group":    "test",
		"topic_id": prefixedTopicID,
	})
	if out["found"] != true {
		t.Fatalf("prefixed id round-trip: expected found=true, got: %v", out)
	}
	if out["topic_name"] != "payments.settled" {
		t.Errorf("expected topic_name=payments.settled, got %v", out["topic_name"])
	}
	pubs, _ := out["publishers"].([]any)
	subs, _ := out["subscribers"].([]any)
	if len(pubs) != 1 {
		t.Errorf("expected 1 publisher, got %d", len(pubs))
	}
	if len(subs) != 1 {
		t.Errorf("expected 1 subscriber, got %d", len(subs))
	}
}

// TestTopicDetail_NameLookupRoundtrip verifies that topic_detail accepts the
// topic entity's NAME as topic_id (not just the hash ID), so that an LLM that
// copies the "name" field instead of "entity_id" from search_entities output
// still gets a useful result. (#1703)
func TestTopicDetail_NameLookupRoundtrip(t *testing.T) {
	entities := []graph.Entity{
		{ID: "t1", Name: "payments.settled", Kind: "SCOPE.Queue"},
		{ID: "pub", Name: "PaymentService", Kind: "Class"},
	}
	rels := []graph.Relationship{
		{ID: "r1", FromID: "pub", ToID: "t1", Kind: "PUBLISHES_TO"},
	}
	srv := newTestServer(t, minDoc(entities, rels))

	// Pass the topic name directly — LabelIndex.Lookup must bridge the gap.
	out := callDashboardTool(t, srv.handleTopologyTopicDetail, map[string]any{
		"group":    "test",
		"topic_id": "payments.settled",
	})
	if out["found"] != true {
		t.Fatalf("name-based lookup: expected found=true, got: %v", out)
	}
	if out["topic_name"] != "payments.settled" {
		t.Errorf("expected topic_name=payments.settled, got %v", out["topic_name"])
	}
}

// TestTopicDetail_RepoAliasRoundtrip verifies that topic_detail resolves the
// repo prefix through the alias map when the slug uses dashes but the repo key
// uses underscores (the slug/path-basename divergence from #1690). (#1703)
func TestTopicDetail_RepoAliasRoundtrip(t *testing.T) {
	docA := &graph.Document{
		Repo: "payments-service", // fleet slug
		Entities: []graph.Entity{
			{ID: "t1", Name: "payments.settled", Kind: "SCOPE.Queue"},
			{ID: "pub", Name: "PaymentService", Kind: "Class"},
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "pub", ToID: "t1", Kind: "PUBLISHES_TO"},
		},
	}
	// Register under the slug key — repo1 path-basename is "payments_service"
	// (underscore variant).  buildRepoAliasMap must alias both forms.
	srv := newTestServer(t, docA)

	// search_entities would emit "payments-service::t1" (canonical slug prefix).
	// topic_detail must resolve this even if an internal alias is involved.
	out := callDashboardTool(t, srv.handleTopologyTopicDetail, map[string]any{
		"group":    "test",
		"topic_id": "payments-service::t1",
	})
	if out["found"] != true {
		t.Fatalf("repo-alias round-trip: expected found=true, got: %v", out)
	}

	// Also accept the underscore variant prefix (underscore alias → same repo).
	out2 := callDashboardTool(t, srv.handleTopologyTopicDetail, map[string]any{
		"group":    "test",
		"topic_id": "payments_service::t1",
	})
	if out2["found"] != true {
		t.Fatalf("underscore-alias round-trip: expected found=true, got: %v", out2)
	}
}

// TestSearchEntities_SchemaFieldFold verifies that SCOPE.Schema/field members
// are suppressed from default search_entities results and restored when
// include_noise:true is set (#1712).
func TestSearchEntities_SchemaFieldFold(t *testing.T) {
	// Serializer class + 3 field members, all matching the query "Deficiency".
	entities := []graph.Entity{
		{
			ID:         "s1",
			Name:       "DeficiencyCreateSerializer",
			Kind:       "SCOPE.Schema",
			SourceFile: "core/serializers/deficiency_serializer.py",
			StartLine:  8,
		},
		{
			ID:         "f1",
			Name:       "DeficiencyCreateSerializer.amount",
			Kind:       "SCOPE.Schema",
			Subtype:    "field",
			SourceFile: "core/serializers/deficiency_serializer.py",
			StartLine:  12,
		},
		{
			ID:         "f2",
			Name:       "DeficiencyCreateSerializer.created_at",
			Kind:       "SCOPE.Schema",
			Subtype:    "field",
			SourceFile: "core/serializers/deficiency_serializer.py",
			StartLine:  13,
		},
		{
			ID:         "f3",
			Name:       "DeficiencyCreateSerializer.updated_at",
			Kind:       "SCOPE.Schema",
			Subtype:    "field",
			SourceFile: "core/serializers/deficiency_serializer.py",
			StartLine:  14,
		},
		// Unrelated real entity — must always be returned.
		{
			ID:        "op1",
			Name:      "get_deficiency_list",
			Kind:      "SCOPE.Operation",
			StartLine: 40,
		},
	}
	srv := newTestServer(t, minDoc(entities, nil))

	// Default: only the parent class + the operation should appear; fields suppressed.
	out := callDashboardTool(t, srv.handleSearchEntities, map[string]any{
		"group": "test",
		"query": "deficiency",
	})
	results, _ := out["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("default: expected 2 results (parent class + operation), got %d: %v", len(results), results)
	}
	for _, r := range results {
		obj, _ := r.(map[string]any)
		name, _ := obj["name"].(string)
		if strings.HasPrefix(name, "DeficiencyCreateSerializer.") {
			t.Errorf("default: schema field %q should be suppressed", name)
		}
	}

	// include_noise:true — all 5 entities returned.
	outNoise := callDashboardTool(t, srv.handleSearchEntities, map[string]any{
		"group":         "test",
		"query":         "deficiency",
		"include_noise": true,
	})
	noiseResults, _ := outNoise["results"].([]any)
	if len(noiseResults) != 5 {
		t.Fatalf("include_noise:true: expected 5 results, got %d: %v", len(noiseResults), noiseResults)
	}
}

// TestSearchEntities_TopicKindAlias verifies that search_entities with
// kind_filter="topic" matches SCOPE.Queue and SCOPE.Topic entities so that
// the entity_ids it returns are always valid inputs for topic_detail. (#1703)
func TestSearchEntities_TopicKindAlias(t *testing.T) {
	entities := []graph.Entity{
		{ID: "q1", Name: "payments.settled", Kind: "SCOPE.Queue"},
		{ID: "q2", Name: "user.updated", Kind: "SCOPE.Topic"},
		{ID: "q3", Name: "order.placed", Kind: "Topic"},
		{ID: "x1", Name: "PaymentService", Kind: "Class"}, // must not match
	}
	srv := newTestServer(t, minDoc(entities, nil))

	out := callDashboardTool(t, srv.handleSearchEntities, map[string]any{
		"group":       "test",
		"query":       ".", // matches the dot in all topic names
		"kind_filter": "topic",
	})
	results, _ := out["results"].([]any)
	if len(results) != 3 {
		t.Errorf("expected 3 topic results (SCOPE.Queue + SCOPE.Topic + Topic), got %d: %v", len(results), results)
	}
	// Verify none of the results are the Class entity.
	for _, r := range results {
		obj, _ := r.(map[string]any)
		if obj["name"] == "PaymentService" {
			t.Errorf("Class entity should not match kind_filter=topic")
		}
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
	srv := newTestServer(t, minDoc(entities, rels))
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
	srv := newTestServer(t, minDoc(entities, nil))
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
	srv := newTestServer(t, minDoc(entities, rels))
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
	srv := newTestServer(t, minDoc(entities, nil))
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
	srv := newTestServer(t, minDoc(entities, rels))
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
	srv := newTestServer(t, minDoc(entities, nil))
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
	srv := newTestServer(t, minDoc(entities, nil))
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
	srv := newTestServer(t, minDoc(entities, rels))
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
	srv := newTestServer(t, minDoc(entities, nil))
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

// ---------------------------------------------------------------------------
// archigraph_topology dispatch (#1281)
// ---------------------------------------------------------------------------

func TestHandleTopology_OrphanPublishers(t *testing.T) {
	entities := []graph.Entity{
		{ID: "t1", Name: "order.created", Kind: "Topic"},
		{ID: "svc", Name: "OrderService", Kind: "Class"},
	}
	rels := []graph.Relationship{
		{ID: "r1", FromID: "svc", ToID: "t1", Kind: "PUBLISHES_TO"},
	}
	srv := newTestServer(t, minDoc(entities, rels))
	out := callDashboardTool(t, srv.handleTopology, map[string]any{
		"group":  "test",
		"action": "orphan_publishers",
	})
	if _, ok := out["orphan_publishers"]; !ok {
		t.Error("expected orphan_publishers key in response")
	}
}

func TestHandleTopology_OrphanSubscribers(t *testing.T) {
	entities := []graph.Entity{
		{ID: "t2", Name: "ghost.topic", Kind: "Topic"},
		{ID: "svc", Name: "ConsumerService", Kind: "Class"},
	}
	rels := []graph.Relationship{
		{ID: "r1", FromID: "svc", ToID: "t2", Kind: "SUBSCRIBES_TO"},
	}
	srv := newTestServer(t, minDoc(entities, rels))
	out := callDashboardTool(t, srv.handleTopology, map[string]any{
		"group":  "test",
		"action": "orphan_subscribers",
	})
	if _, ok := out["orphan_subscribers"]; !ok {
		t.Error("expected orphan_subscribers key in response")
	}
}

func TestHandleTopology_TopicDetail(t *testing.T) {
	entities := []graph.Entity{
		{ID: "t1", Name: "payment.processed", Kind: "Topic"},
		{ID: "pub", Name: "PaymentService", Kind: "Class"},
	}
	rels := []graph.Relationship{
		{ID: "r1", FromID: "pub", ToID: "t1", Kind: "PUBLISHES_TO"},
	}
	srv := newTestServer(t, minDoc(entities, rels))
	out := callDashboardTool(t, srv.handleTopology, map[string]any{
		"group":    "test",
		"action":   "topic_detail",
		"topic_id": "t1",
	})
	if out["found"] != true {
		t.Error("expected found=true for existing topic")
	}
}

func TestHandleTopology_UnknownAction(t *testing.T) {
	srv := newTestServer(t, minDoc(nil, nil))
	req := newReq(map[string]any{"group": "test", "action": "bogus"})
	res, err := srv.handleTopology(ctxBg(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true for unknown action")
	}
}

// ---------------------------------------------------------------------------
// archigraph_flows dispatch (#1281)
// ---------------------------------------------------------------------------

func TestHandleFlows_DeadEnds(t *testing.T) {
	srv := newTestServer(t, minDoc(nil, nil))
	out := callDashboardTool(t, srv.handleFlows, map[string]any{
		"group":  "test",
		"action": "dead_ends",
	})
	if _, ok := out["dead_ends"]; !ok {
		t.Error("expected dead_ends key in response")
	}
}

func TestHandleFlows_Truncated(t *testing.T) {
	srv := newTestServer(t, minDoc(nil, nil))
	out := callDashboardTool(t, srv.handleFlows, map[string]any{
		"group":  "test",
		"action": "truncated",
	})
	if _, ok := out["truncated_flows"]; !ok {
		t.Error("expected truncated_flows key in response")
	}
}

func TestHandleFlows_Detail_NotFound(t *testing.T) {
	srv := newTestServer(t, minDoc(nil, nil))
	out := callDashboardTool(t, srv.handleFlows, map[string]any{
		"group":      "test",
		"action":     "detail",
		"process_id": "nonexistent",
	})
	if out["found"] != false {
		t.Error("expected found=false for nonexistent process")
	}
}

func TestHandleFlows_UnknownAction(t *testing.T) {
	srv := newTestServer(t, minDoc(nil, nil))
	req := newReq(map[string]any{"group": "test", "action": "bogus"})
	res, err := srv.handleFlows(ctxBg(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true for unknown action")
	}
}

// ---------------------------------------------------------------------------
// archigraph_graph_patterns dispatch (#1281)
// ---------------------------------------------------------------------------

func TestHandleGraphPatterns_List(t *testing.T) {
	entities := []graph.Entity{
		{ID: "p1", Name: "RepositoryPattern", Kind: "SCOPE.Pattern",
			Properties: map[string]string{"status": "active", "confidence": "0.9"}},
	}
	srv := newTestServer(t, minDoc(entities, nil))
	out := callDashboardTool(t, srv.handleGraphPatterns, map[string]any{
		"group":  "test",
		"action": "list",
	})
	if _, ok := out["patterns"]; !ok {
		t.Error("expected patterns key in response")
	}
	count := int(out["count"].(float64))
	if count != 1 {
		t.Errorf("expected 1 pattern, got %d", count)
	}
}

func TestHandleGraphPatterns_Get(t *testing.T) {
	entities := []graph.Entity{
		{ID: "p1", Name: "RepositoryPattern", Kind: "SCOPE.Pattern"},
	}
	srv := newTestServer(t, minDoc(entities, nil))
	out := callDashboardTool(t, srv.handleGraphPatterns, map[string]any{
		"group":      "test",
		"action":     "get",
		"pattern_id": "p1",
	})
	if out["found"] != true {
		t.Error("expected found=true for existing pattern")
	}
}

func TestHandleGraphPatterns_UnknownAction(t *testing.T) {
	srv := newTestServer(t, minDoc(nil, nil))
	req := newReq(map[string]any{"group": "test", "action": "bogus"})
	res, err := srv.handleGraphPatterns(ctxBg(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true for unknown action")
	}
}

// ---------------------------------------------------------------------------
// helpers for dispatch tests
// ---------------------------------------------------------------------------

func newReq(args map[string]any) mcpapi.CallToolRequest {
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	return req
}

func ctxBg() context.Context {
	return context.Background()
}
