package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/coverage"
	"github.com/cajasmota/grafel/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// buildReachabilityDoc builds a Document whose entities carry the static
// test-reachability properties that #5061's enrichment pass stamps. The tool
// reads these directly — it never recomputes.
//
//	fn_reached   Function  test_reachable=true  depth=2  tests=test_a,test_b
//	ep_reached   Endpoint  test_reachable=true  (via handler) depth=1
//	ep_orphan    Endpoint  test_reachable=false  ← the orphan we want surfaced
//	fn_orphan    Function  test_reachable=false  reachable-but-0%-lines? no
//	fn_no_lines  Function  test_reachable=true   line_pct=0 → reachable_no_lines
//	schema_x     Schema    (no reachability prop — not a production entity)
func buildReachabilityDoc() *graph.Document {
	return &graph.Document{
		Repo: "svc",
		Entities: []graph.Entity{
			graph.Entity{
				ID: "fn_reached", Name: "ProcessOrder", Kind: "SCOPE.Function",
				SourceFile: "orders/process.go", StartLine: 10,
			}.WithProperties(map[string]string{
				coverage.PropTestReachable:     "true",
				coverage.PropReachDepth:        "2",
				coverage.PropReachingTestCount: "2",
				coverage.PropReachingTests:     "test_a,test_b",
			},
			),
			graph.Entity{
				ID: "ep_reached", Name: "GET /orders", Kind: "SCOPE.Endpoint",
				SourceFile: "orders/routes.go", StartLine: 5,
			}.WithProperties(map[string]string{
				coverage.PropTestReachable:     "true",
				coverage.PropReachDepth:        "1",
				coverage.PropReachingTestCount: "1",
				coverage.PropReachingTests:     "test_e2e",
			},
			),
			graph.Entity{
				ID: "ep_orphan", Name: "POST /inspections", Kind: "SCOPE.Endpoint",
				SourceFile: "inspections/routes.go", StartLine: 12,
			}.WithProperties(map[string]string{
				coverage.PropTestReachable: "false",
			},
			),
			graph.Entity{
				ID: "fn_orphan", Name: "ReconcileLedger", Kind: "SCOPE.Function",
				SourceFile: "ledger/reconcile.go", StartLine: 40,
			}.WithProperties(map[string]string{
				coverage.PropTestReachable: "false",
			},
			),
			graph.Entity{
				ID: "fn_no_lines", Name: "ValidateTax", Kind: "SCOPE.Function",
				SourceFile: "tax/validate.go", StartLine: 8,
			}.WithProperties(map[string]string{
				coverage.PropTestReachable:     "true",
				coverage.PropReachDepth:        "1",
				coverage.PropReachingTestCount: "1",
				coverage.PropReachingTests:     "test_tax",
				coverage.PropCoveragePct:       "0",
			},
			),
			{
				ID: "schema_x", Name: "Order", Kind: "SCOPE.Schema",
				SourceFile: "orders/model.go", StartLine: 1,
				// No reachability prop — not a production entity; must be ignored.
			},
		},
	}
}

func callReach(t *testing.T, srv *Server, args map[string]any) string {
	t.Helper()
	args["group"] = "test"
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	res, err := srv.handleTestReachability(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	return resultText(res)
}

func TestTestReachability_DefaultListing(t *testing.T) {
	srv := newTestServer(t, buildReachabilityDoc())
	out := callReach(t, srv, map[string]any{})

	// Roll-ups: 5 production entities (4 reachable: fn_reached, ep_reached,
	// fn_no_lines; ep_orphan + fn_orphan are not). schema_x excluded.
	if !strings.Contains(out, "Production entities : 5") {
		t.Errorf("expected 5 production entities, got:\n%s", out)
	}
	if !strings.Contains(out, "Test-reachable      : 3") {
		t.Errorf("expected 3 reachable, got:\n%s", out)
	}
	if !strings.Contains(out, "Orphans (untested)  : 2") {
		t.Errorf("expected 2 orphans, got:\n%s", out)
	}
	// Endpoint roll-up: 2 endpoints, 1 reachable.
	if !strings.Contains(out, "Endpoints           : 2") {
		t.Errorf("expected 2 endpoints, got:\n%s", out)
	}
	// Orphans should sort to the top (endpoint orphan first).
	if !strings.Contains(out, "[ORPHAN]") {
		t.Errorf("expected ORPHAN marker, got:\n%s", out)
	}
	idxOrphan := strings.Index(out, "POST /inspections")
	idxReachable := strings.Index(out, "ProcessOrder")
	if idxOrphan == -1 || idxReachable == -1 || idxOrphan > idxReachable {
		t.Errorf("orphan endpoint should sort before reachable fn:\n%s", out)
	}
	// schema_x (a non-production Schema, with no reachability prop) must never
	// appear — neither by id nor by name.
	if strings.Contains(out, "schema_x") || strings.Contains(out, "model.go") {
		t.Errorf("non-production entity leaked into output:\n%s", out)
	}
}

func TestTestReachability_UntestedOnly(t *testing.T) {
	srv := newTestServer(t, buildReachabilityDoc())
	out := callReach(t, srv, map[string]any{"untested_only": true})

	if !strings.Contains(out, "POST /inspections") || !strings.Contains(out, "ReconcileLedger") {
		t.Errorf("expected both orphans listed, got:\n%s", out)
	}
	// Reachable entities must be filtered out of the listing.
	if strings.Contains(out, "[reachable]") {
		t.Errorf("untested_only should not list reachable rows:\n%s", out)
	}
}

func TestTestReachability_EndpointsOnly(t *testing.T) {
	srv := newTestServer(t, buildReachabilityDoc())
	out := callReach(t, srv, map[string]any{"endpoints_only": true})

	if !strings.Contains(out, "POST /inspections") || !strings.Contains(out, "GET /orders") {
		t.Errorf("expected both endpoints, got:\n%s", out)
	}
	// Plain functions must not appear in endpoints_only listing.
	if strings.Contains(out, "ProcessOrder") || strings.Contains(out, "ReconcileLedger") {
		t.Errorf("endpoints_only leaked a function:\n%s", out)
	}
}

func TestTestReachability_EntityFocus(t *testing.T) {
	srv := newTestServer(t, buildReachabilityDoc())
	out := callReach(t, srv, map[string]any{"entity_id": "fn_reached"})

	if !strings.Contains(out, "ProcessOrder") {
		t.Errorf("expected focused entity, got:\n%s", out)
	}
	if !strings.Contains(out, "depth=2") || !strings.Contains(out, "tests=2") {
		t.Errorf("expected depth/test count detail, got:\n%s", out)
	}
	if !strings.Contains(out, "reaching_tests: test_a,test_b") {
		t.Errorf("expected reaching tests for focused entity, got:\n%s", out)
	}
	// Other entities should be filtered out.
	if strings.Contains(out, "ReconcileLedger") {
		t.Errorf("entity_id focus leaked other rows:\n%s", out)
	}
}

func TestTestReachability_CrossSignal(t *testing.T) {
	srv := newTestServer(t, buildReachabilityDoc())
	out := callReach(t, srv, map[string]any{})
	// fn_no_lines is reachable but has line_pct=0 → reachable-but-0%-lines tag.
	if !strings.Contains(out, "ValidateTax") {
		t.Fatalf("expected ValidateTax row, got:\n%s", out)
	}
	if !strings.Contains(out, "reachable-but-0%-lines") {
		t.Errorf("expected cross-signal tag for reachable_no_lines entity:\n%s", out)
	}
}

func TestTestReachability_NotComputed(t *testing.T) {
	// A doc with NO reachability props at all → honesty path.
	doc := &graph.Document{
		Repo: "svc",
		Entities: []graph.Entity{
			{ID: "f1", Name: "Foo", Kind: "SCOPE.Function", SourceFile: "a.go", StartLine: 1},
		},
	}
	srv := newTestServer(t, doc)
	out := callReach(t, srv, map[string]any{})

	if !strings.Contains(out, "Reachability not computed") {
		t.Errorf("expected not-computed honesty message, got:\n%s", out)
	}
	if !strings.Contains(out, "Reindex") {
		t.Errorf("expected reindex guidance, got:\n%s", out)
	}
}
