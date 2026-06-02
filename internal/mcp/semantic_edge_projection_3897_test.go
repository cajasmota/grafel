package mcp

// semantic_edge_projection_3897_test.go — #3897: surface ALL semantically
// meaningful, non-structural edge kinds in the MCP read tools, generalizing the
// DI-only #3870/#3894 projection.
//
// Before #3897 inspect projected only CALLS (calls/called_by), DISCRIMINATES_ON
// (discriminators) and the DI subset (di_edges); expand annotated only CALLS
// depth + di_kind/di_direction. A node could carry a JOINS_COLLECTION /
// GRAPH_RELATES / DEPENDS_ON_SERVICE / THROWS / CATCHES / DATA_FLOWS_TO edge and
// the rewrite agent would never see it.
//
// These tests are VALUE-ASSERTING: each asserts the SPECIFIC semantic edge
// surfaces with the right kind + direction + far-side entity in the new
// `semantic_edges` inspect section and the new {semantic_kind, semantic_direction}
// expand annotation — not merely that some non-empty output exists. They also
// guard the #3870 regressions (di_edges, CALLS sections) and the exclusion of
// structural kinds.

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// makeSemanticEdgeDoc builds a graph exercising a representative spread of the
// projected semantic edge kinds plus a structural CALLS edge (which must NOT
// appear in semantic_edges) and a DI edge (which must appear in BOTH di_edges
// and semantic_edges).
//
//	repo:    handler --DEPENDS_ON_SERVICE--> paymentSvc
//	         handler --THROWS--------------> appError
//	         handler --CATCHES-------------> dbError
//	         handler --DATA_FLOWS_TO-------> sink
//	         model   --JOINS_COLLECTION----> orders     (model → collection)
//	         model   --GRAPH_RELATES-------> userNode
//	         provider --INJECTED_INTO------> handler     (DI: also in di_edges)
//	         handler --CALLS---------------> paymentSvc  (structural: excluded)
func makeSemanticEdgeDoc() *graph.Document {
	return &graph.Document{
		Repo: "api",
		Entities: []graph.Entity{
			{ID: "handler", Name: "OrderHandler", Kind: "SCOPE.Operation", SourceFile: "order.ts", StartLine: 1},
			{ID: "paymentSvc", Name: "PaymentService", Kind: "SCOPE.Component", SourceFile: "payment.ts", StartLine: 1},
			{ID: "appError", Name: "AppError", Kind: "SCOPE.Type", SourceFile: "errors.ts", StartLine: 1},
			{ID: "dbError", Name: "DbError", Kind: "SCOPE.Type", SourceFile: "errors.ts", StartLine: 10},
			{ID: "sink", Name: "AuditSink", Kind: "SCOPE.Operation", SourceFile: "audit.ts", StartLine: 1},
			{ID: "model", Name: "OrderModel", Kind: "SCOPE.Type", SourceFile: "model.ts", StartLine: 1},
			{ID: "orders", Name: "orders", Kind: "SCOPE.Datastore", SourceFile: "model.ts", StartLine: 1},
			{ID: "userNode", Name: "UserNode", Kind: "SCOPE.Type", SourceFile: "graph.ts", StartLine: 1},
			{ID: "provider", Name: "OrderProvider", Kind: "SCOPE.Component", SourceFile: "provider.ts", StartLine: 1},
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "handler", ToID: "paymentSvc", Kind: "DEPENDS_ON_SERVICE", Properties: map[string]string{"line": "5"}},
			{ID: "r2", FromID: "handler", ToID: "appError", Kind: "THROWS", Properties: map[string]string{"line": "12"}},
			{ID: "r3", FromID: "handler", ToID: "dbError", Kind: "CATCHES", Properties: map[string]string{"line": "18"}},
			{ID: "r4", FromID: "handler", ToID: "sink", Kind: "DATA_FLOWS_TO", Properties: map[string]string{"line": "22"}},
			{ID: "r5", FromID: "model", ToID: "orders", Kind: "JOINS_COLLECTION", Properties: map[string]string{"line": "3"}},
			{ID: "r6", FromID: "model", ToID: "userNode", Kind: "GRAPH_RELATES", Properties: map[string]string{"line": "7"}},
			{ID: "r7", FromID: "provider", ToID: "handler", Kind: "INJECTED_INTO", Properties: map[string]string{"line": "2"}},
			// structural CALLS — must be EXCLUDED from semantic_edges.
			{ID: "c1", FromID: "handler", ToID: "paymentSvc", Kind: "CALLS", Properties: map[string]string{"line": "5"}},
		},
	}
}

// findSemEdge returns the first semantic_edges row matching kind/direction/other.
func findSemEdge(rows []any, kind, direction, other string) map[string]any {
	for _, r := range rows {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if m["kind"] == kind && m["direction"] == direction && m["other"] == other {
			return m
		}
	}
	return nil
}

func semanticEdgesOf(t *testing.T, srv *Server, entityID string) []any {
	t.Helper()
	out := callInspectWithArgs(t, srv, map[string]any{"group": "test", "entity_id": entityID})
	sem, ok := out["semantic_edges"]
	if !ok {
		t.Fatalf("expected semantic_edges on inspect of %s; keys: %v", entityID, mapKeys(out))
	}
	rows, ok := sem.([]any)
	if !ok || len(rows) == 0 {
		t.Fatalf("semantic_edges is %T len=%d on %s, want non-empty []any", sem, len(rows), entityID)
	}
	return rows
}

// TestInspect_SurfacesDependsOnService asserts a DEPENDS_ON_SERVICE edge surfaces
// outbound on the handler — a kind that was previously invisible to inspect.
func TestInspect_SurfacesDependsOnService(t *testing.T) {
	srv := newTestServer(t, makeSemanticEdgeDoc())
	rows := semanticEdgesOf(t, srv, "handler")
	got := findSemEdge(rows, "DEPENDS_ON_SERVICE", "outbound", "paymentSvc")
	if got == nil {
		t.Fatalf("did not find DEPENDS_ON_SERVICE(outbound, other=paymentSvc): %v", rows)
	}
	if line, _ := got["line"].(float64); line != 5 {
		t.Errorf("DEPENDS_ON_SERVICE line = %v, want 5", got["line"])
	}
}

// TestInspect_SurfacesErrorFlow asserts both THROWS and CATCHES surface on the
// handler — the error-flow edge family was previously invisible.
func TestInspect_SurfacesErrorFlow(t *testing.T) {
	srv := newTestServer(t, makeSemanticEdgeDoc())
	rows := semanticEdgesOf(t, srv, "handler")
	if findSemEdge(rows, "THROWS", "outbound", "appError") == nil {
		t.Errorf("did not find THROWS(outbound, other=appError): %v", rows)
	}
	if findSemEdge(rows, "CATCHES", "outbound", "dbError") == nil {
		t.Errorf("did not find CATCHES(outbound, other=dbError): %v", rows)
	}
}

// TestInspect_SurfacesDataFlowsTo asserts a DATA_FLOWS_TO edge surfaces.
func TestInspect_SurfacesDataFlowsTo(t *testing.T) {
	srv := newTestServer(t, makeSemanticEdgeDoc())
	rows := semanticEdgesOf(t, srv, "handler")
	if findSemEdge(rows, "DATA_FLOWS_TO", "outbound", "sink") == nil {
		t.Errorf("did not find DATA_FLOWS_TO(outbound, other=sink): %v", rows)
	}
}

// TestInspect_SurfacesJoinsCollectionAndGraphRelates asserts the data-layer
// (JOINS_COLLECTION) and graph-DB (GRAPH_RELATES) edges surface on the model.
func TestInspect_SurfacesJoinsCollectionAndGraphRelates(t *testing.T) {
	srv := newTestServer(t, makeSemanticEdgeDoc())
	rows := semanticEdgesOf(t, srv, "model")
	if findSemEdge(rows, "JOINS_COLLECTION", "outbound", "orders") == nil {
		t.Errorf("did not find JOINS_COLLECTION(outbound, other=orders): %v", rows)
	}
	if findSemEdge(rows, "GRAPH_RELATES", "outbound", "userNode") == nil {
		t.Errorf("did not find GRAPH_RELATES(outbound, other=userNode): %v", rows)
	}
}

// TestInspect_SurfacesInboundSemanticEdge asserts the far side of a semantic edge
// sees it as INBOUND — e.g. appError is THROWN by the handler.
func TestInspect_SurfacesInboundSemanticEdge(t *testing.T) {
	srv := newTestServer(t, makeSemanticEdgeDoc())
	rows := semanticEdgesOf(t, srv, "appError")
	if findSemEdge(rows, "THROWS", "inbound", "handler") == nil {
		t.Fatalf("did not find THROWS(inbound, other=handler) on appError: %v", rows)
	}
}

// TestInspect_DIEdgeAppearsInBothSections asserts the DI subset (INJECTED_INTO)
// surfaces in BOTH the legacy di_edges section (regression) AND the new
// semantic_edges section (superset).
func TestInspect_DIEdgeAppearsInBothSections(t *testing.T) {
	srv := newTestServer(t, makeSemanticEdgeDoc())
	out := callInspectWithArgs(t, srv, map[string]any{"group": "test", "entity_id": "handler"})

	di, ok := out["di_edges"].([]any)
	if !ok || len(di) == 0 {
		t.Fatalf("regression: di_edges missing/empty on handler; keys: %v", mapKeys(out))
	}
	if findSemEdge(di, "INJECTED_INTO", "inbound", "provider") == nil {
		t.Errorf("di_edges should contain INJECTED_INTO(inbound, other=provider): %v", di)
	}

	sem := out["semantic_edges"].([]any)
	if findSemEdge(sem, "INJECTED_INTO", "inbound", "provider") == nil {
		t.Errorf("semantic_edges should ALSO contain INJECTED_INTO(inbound, other=provider): %v", sem)
	}
}

// TestInspect_ExcludesStructuralCalls asserts the structural CALLS edge does NOT
// leak into semantic_edges (it has its own calls/called_by sections).
func TestInspect_ExcludesStructuralCalls(t *testing.T) {
	srv := newTestServer(t, makeSemanticEdgeDoc())
	out := callInspectWithArgs(t, srv, map[string]any{"group": "test", "entity_id": "handler"})

	sem, _ := out["semantic_edges"].([]any)
	for _, r := range sem {
		if m, ok := r.(map[string]any); ok && m["kind"] == "CALLS" {
			t.Errorf("CALLS must NOT appear in semantic_edges: %v", m)
		}
	}
	// CALLS still surfaces via its dedicated section (regression guard).
	if _, ok := out["calls"]; !ok {
		t.Errorf("regression: calls section missing")
	}
}

// TestInspect_NoSemanticEdges_OmitsSection asserts the section is omitted for an
// entity with only structural edges (additive / backward compatible).
func TestInspect_NoSemanticEdges_OmitsSection(t *testing.T) {
	doc := &graph.Document{
		Repo: "api",
		Entities: []graph.Entity{
			{ID: "a", Name: "A", Kind: "SCOPE.Operation", SourceFile: "a.ts", StartLine: 1},
			{ID: "b", Name: "B", Kind: "SCOPE.Operation", SourceFile: "b.ts", StartLine: 1},
		},
		Relationships: []graph.Relationship{
			{ID: "c", FromID: "a", ToID: "b", Kind: "CALLS"},
		},
	}
	srv := newTestServer(t, doc)
	out := callInspectWithArgs(t, srv, map[string]any{"group": "test", "entity_id": "a"})
	if _, ok := out["semantic_edges"]; ok {
		t.Fatalf("semantic_edges should be omitted when entity has only structural edges")
	}
}

// TestNeighbors_AnnotatesSemanticEdge asserts expand annotates a non-DI semantic
// neighbour (DEPENDS_ON_SERVICE) with semantic_kind/semantic_direction — which it
// previously could NOT do (only di_kind/di_direction existed).
func TestNeighbors_AnnotatesSemanticEdge(t *testing.T) {
	srv := newTestServer(t, makeSemanticEdgeDoc())
	rows := callNeighbors(t, srv, map[string]any{
		"group":        "test",
		"entity_id":    "handler",
		"depth":        1,
		"token_budget": 8000,
	})
	var svcRow map[string]any
	for _, r := range rows {
		if m, ok := r.(map[string]any); ok && m["id"] == "api::paymentSvc" {
			svcRow = m
		}
	}
	if svcRow == nil {
		t.Fatalf("neighbour api::paymentSvc not present: %v", rows)
	}
	if svcRow["semantic_kind"] != "DEPENDS_ON_SERVICE" {
		t.Errorf("semantic_kind = %v, want DEPENDS_ON_SERVICE", svcRow["semantic_kind"])
	}
	if svcRow["semantic_direction"] != "outbound" {
		t.Errorf("semantic_direction = %v, want outbound", svcRow["semantic_direction"])
	}
}

// TestNeighbors_DIStillAnnotated asserts the DI annotation (di_kind/di_direction)
// is preserved on the expand row alongside the new semantic_* annotation
// (regression for #3870).
func TestNeighbors_DIStillAnnotated(t *testing.T) {
	srv := newTestServer(t, makeSemanticEdgeDoc())
	rows := callNeighbors(t, srv, map[string]any{
		"group":        "test",
		"entity_id":    "handler",
		"depth":        1,
		"token_budget": 8000,
	})
	var provRow map[string]any
	for _, r := range rows {
		if m, ok := r.(map[string]any); ok && m["id"] == "api::provider" {
			provRow = m
		}
	}
	if provRow == nil {
		t.Fatalf("DI neighbour api::provider not present: %v", rows)
	}
	if provRow["di_kind"] != "INJECTED_INTO" || provRow["di_direction"] != "inbound" {
		t.Errorf("regression: DI annotation lost: di_kind=%v di_direction=%v", provRow["di_kind"], provRow["di_direction"])
	}
	// And the generalized annotation also covers it.
	if provRow["semantic_kind"] != "INJECTED_INTO" {
		t.Errorf("semantic_kind = %v, want INJECTED_INTO", provRow["semantic_kind"])
	}
}
