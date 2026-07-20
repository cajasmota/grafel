package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/coverage"
	"github.com/cajasmota/grafel/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// buildEffectivenessDoc builds entities stamped with both #5037 reachability
// and #5036 LCOV line coverage (as #5061's enrichment pass would), exercising
// every cross-product quadrant the grafel_coverage_effectiveness tool
// classifies. The tool reads these directly — it never recomputes.
//
//	fn_ineffective  reachable=true  coverage_pct=0   → reachable_no_lines (headline)
//	fn_weak         reachable=true  coverage_pct=20  → reachable_low_coverage
//	fn_covered      reachable=true  coverage_pct=92  → reachable_covered
//	fn_no_cov       reachable=true  (no coverage prop) → reachable_no_coverage
//	ep_orphan       reachable=false                   → untested
//	schema_x        (no reachability prop)            → ignored
func buildEffectivenessDoc() *graph.Document {
	return &graph.Document{
		Repo: "svc",
		Entities: []graph.Entity{
			graph.Entity{
				ID: "fn_ineffective", Name: "ValidateTax", Kind: "SCOPE.Function",
				SourceFile: "tax/validate.go", StartLine: 8,
			}.WithProperties(map[string]string{
				coverage.PropTestReachable:     "true",
				coverage.PropReachDepth:        "1",
				coverage.PropReachingTestCount: "2",
				coverage.PropReachingTests:     "test_tax,test_tax2",
				coverage.PropCoveragePct:       "0.0",
				coverage.PropCoveredLines:      "0",
				coverage.PropTotalLines:        "14",
			},
			),
			graph.Entity{
				ID: "fn_weak", Name: "ApplyDiscount", Kind: "SCOPE.Function",
				SourceFile: "pricing/discount.go", StartLine: 20,
			}.WithProperties(map[string]string{
				coverage.PropTestReachable:     "true",
				coverage.PropReachDepth:        "2",
				coverage.PropReachingTestCount: "1",
				coverage.PropCoveragePct:       "20.0",
			},
			),
			graph.Entity{
				ID: "fn_covered", Name: "ProcessOrder", Kind: "SCOPE.Function",
				SourceFile: "orders/process.go", StartLine: 10,
			}.WithProperties(map[string]string{
				coverage.PropTestReachable:     "true",
				coverage.PropReachDepth:        "1",
				coverage.PropReachingTestCount: "3",
				coverage.PropCoveragePct:       "92.0",
			},
			),
			graph.Entity{
				ID: "fn_no_cov", Name: "ReconcileLedger", Kind: "SCOPE.Function",
				SourceFile: "ledger/reconcile.go", StartLine: 40,
			}.WithProperties(map[string]string{
				coverage.PropTestReachable:     "true",
				coverage.PropReachDepth:        "1",
				coverage.PropReachingTestCount: "1",
			},
			),
			graph.Entity{
				ID: "ep_orphan", Name: "POST /inspections", Kind: "SCOPE.Endpoint",
				SourceFile: "inspections/routes.go", StartLine: 12,
			}.WithProperties(map[string]string{
				coverage.PropTestReachable: "false",
			},
			),
			{
				ID: "schema_x", Name: "Order", Kind: "SCOPE.Schema",
				SourceFile: "orders/model.go", StartLine: 1,
				// No reachability prop — must be ignored.
			},
		},
	}
}

func callEffectiveness(t *testing.T, srv *Server, args map[string]any) string {
	t.Helper()
	args["group"] = "test"
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	res, err := srv.handleCoverageEffectiveness(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	return resultText(res)
}

func TestCoverageEffectiveness_Quadrants(t *testing.T) {
	srv := newTestServer(t, buildEffectivenessDoc())
	out := callEffectiveness(t, srv, map[string]any{})

	// Group quadrant counts (5 stamped production entities; schema_x ignored).
	for _, want := range []string{
		"Production entities (reachability-stamped) : 5",
		"reachable + 0% lines (ineffective?)      : 1",
		"reachable + low coverage",
		"reachable + covered                       : 1",
		"reachable + no line-coverage measurement  : 1",
		"unreachable (untested surface)            : 1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}

	// The headline ineffective-test must be surfaced by name.
	if !strings.Contains(out, "Ineffective tests — reachable but 0% lines (1)") {
		t.Errorf("missing ineffective header:\n%s", out)
	}
	if !strings.Contains(out, "ValidateTax") {
		t.Errorf("ineffective test ValidateTax not surfaced:\n%s", out)
	}
	// Healthy/covered entities must NOT be in the ineffective list.
	idxIneff := strings.Index(out, "Ineffective tests")
	idxQuad := strings.Index(out, "Entities by quadrant")
	ineffSection := out[idxIneff:idxQuad]
	if strings.Contains(ineffSection, "ProcessOrder") {
		t.Errorf("covered fn leaked into ineffective list:\n%s", ineffSection)
	}

	// schema_x must never appear.
	if strings.Contains(out, "schema_x") || strings.Contains(out, "model.go") {
		t.Errorf("non-production entity leaked:\n%s", out)
	}
}

func TestCoverageEffectiveness_IneffectiveOnly(t *testing.T) {
	srv := newTestServer(t, buildEffectivenessDoc())
	out := callEffectiveness(t, srv, map[string]any{"ineffective_only": true})

	if !strings.Contains(out, "ValidateTax") {
		t.Errorf("expected ineffective test listed:\n%s", out)
	}
	// The full quadrant listing must be suppressed.
	if strings.Contains(out, "Entities by quadrant") {
		t.Errorf("ineffective_only should suppress full listing:\n%s", out)
	}
}

// TestCoverageEffectiveness_HonestDegradation: a group with reachability but no
// ingested line coverage reports reachability quadrants and explicitly says the
// line-coverage cross is unavailable — it does NOT fabricate ineffective tests.
func TestCoverageEffectiveness_HonestDegradation(t *testing.T) {
	doc := &graph.Document{
		Repo: "svc",
		Entities: []graph.Entity{
			graph.Entity{ID: "fn_a", Name: "Alpha", Kind: "SCOPE.Function", SourceFile: "a.go", StartLine: 1}.WithProperties(map[string]string{coverage.PropTestReachable: "true", coverage.PropReachDepth: "1", coverage.PropReachingTestCount: "1"}),
			graph.Entity{ID: "fn_b", Name: "Beta", Kind: "SCOPE.Function", SourceFile: "b.go", StartLine: 1}.WithProperties(map[string]string{coverage.PropTestReachable: "false"}),
		},
	}
	srv := newTestServer(t, doc)
	out := callEffectiveness(t, srv, map[string]any{})

	if !strings.Contains(out, "Line-coverage cross unavailable for this group") {
		t.Errorf("expected degradation note, got:\n%s", out)
	}
	if !strings.Contains(out, "Ineffective tests — reachable but 0% lines (0)") {
		t.Errorf("must not fabricate ineffective tests:\n%s", out)
	}
}

// TestCoverageEffectiveness_Unstamped: no reachability props → reindex hint.
func TestCoverageEffectiveness_Unstamped(t *testing.T) {
	doc := &graph.Document{
		Repo: "svc",
		Entities: []graph.Entity{
			{ID: "fn_a", Name: "Alpha", Kind: "SCOPE.Function", SourceFile: "a.go", StartLine: 1},
		},
	}
	srv := newTestServer(t, doc)
	out := callEffectiveness(t, srv, map[string]any{})
	if !strings.Contains(out, "Reindex the group") {
		t.Errorf("expected reindex hint for unstamped group:\n%s", out)
	}
}
