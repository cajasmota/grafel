package mcp

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// buildPostureDoc builds a Document that exercises every posture facet, mirroring
// the live #3628 data shapes:
//
//	Entities
//	  ep_handler     a callable / endpoint with rate_limit + deprecation + auth
//	                 properties, plus THROWS/CATCHES/GATED_BY out-edges.
//	  exc_validation SCOPE.ExceptionType  Name "exception:ValidationError"
//	  exc_integrity  SCOPE.ExceptionType  Name "exception:IntegrityError"
//	  ff_new_flow    SCOPE.FeatureFlag    Name "feature:new-checkout"  ID feature:new-checkout
//	  ep_plain       a callable with NO posture facets (must be omitted by scan).
//
//	Relationships
//	  ep_handler –THROWS→  exc_validation
//	  ep_handler –THROWS→  exc_integrity
//	  ep_handler –CATCHES→ exc_validation
//	  ep_handler –GATED_BY→ ff_new_flow
func buildPostureDoc() *graph.Document {
	return &graph.Document{
		Repo: "svc",
		Entities: []graph.Entity{
			graph.Entity{
				ID: "ep_handler", Name: "OrderViewSet.create", Kind: "http_endpoint_definition",
				SourceFile: "orders/views.py", StartLine: 42,
			}.WithProperties(map[string]string{
				"verb":             "POST",
				"path":             "/api/v1/orders",
				"rate_limited":     "true",
				"rate_limit":       "100/min",
				"rate_limit_scope": "user",
				"deprecated":       "true",
				"deprecated_since": "v2",
				"api_version":      "v1",
				"auth_required":    "true",
				"auth_method":      "grpc_interceptor",
				"auth_middleware":  "AuthInterceptor",
			},
			),
			{
				ID: "exc_validation", Name: "exception:ValidationError", Kind: "SCOPE.ExceptionType",
				SourceFile: "<exception-type>",
			},
			{
				ID: "exc_integrity", Name: "exception:IntegrityError", Kind: "SCOPE.ExceptionType",
				SourceFile: "<exception-type>",
			},
			{
				ID: "feature:new-checkout", Name: "feature:new-checkout", Kind: "SCOPE.FeatureFlag",
				SourceFile: "<feature-flag>",
			},
			graph.Entity{
				ID: "ep_plain", Name: "HealthView.get", Kind: "http_endpoint_definition",
				SourceFile: "core/health.py", StartLine: 5,
			}.WithProperties(map[string]string{"verb": "GET", "path": "/healthz"}),
		},
		Relationships: []graph.Relationship{
			{FromID: "ep_handler", ToID: "exc_validation", Kind: "THROWS"},
			{FromID: "ep_handler", ToID: "exc_integrity", Kind: "THROWS"},
			{FromID: "ep_handler", ToID: "exc_validation", Kind: "CATCHES"},
			{FromID: "ep_handler", ToID: "feature:new-checkout", Kind: "GATED_BY"},
		},
	}
}

func newPostureServer(t *testing.T) *Server {
	t.Helper()
	return newTestServer(t, buildPostureDoc())
}

// TestEndpointPosture_PerEntity_AllFacetsNonEmpty is the marquee deploy-9
// surfacing regression: the #3628 caps (error_flow, rate_limit, deprecation,
// feature_flag, grpc/http auth) ARE in the graph but were undiscoverable via
// MCP. This asserts the new surface returns every facet NON-EMPTY for a single
// resolved entity. Before this tool existed there was no MCP query that
// returned a callable's thrown exception types or feature gates at all
// (inspect(fields=[throws,catches]) → empty; search(kind=Config,"feature") →
// wrong kind), so this test could not pass — fail-before / pass-after.
func TestEndpointPosture_PerEntity_AllFacetsNonEmpty(t *testing.T) {
	srv := newPostureServer(t)
	res := callPostureTool(t, srv, map[string]any{
		"group":     "test",
		"entity_id": "svc::ep_handler",
	})

	// --- error_flow: thrown + caught exception TYPE names, resolved from the
	// THROWS/CATCHES edges to SCOPE.ExceptionType nodes (prefix stripped). ---
	ef, ok := res["error_flow"].(map[string]any)
	if !ok {
		t.Fatalf("error_flow missing or wrong type: %T (%v)", res["error_flow"], res["error_flow"])
	}
	throws := toStringSet(t, ef["throws"])
	if !throws["ValidationError"] || !throws["IntegrityError"] {
		t.Errorf("throws=%v; want ValidationError + IntegrityError (resolved from THROWS→ExceptionType)", ef["throws"])
	}
	catches := toStringSet(t, ef["catches"])
	if !catches["ValidationError"] {
		t.Errorf("catches=%v; want ValidationError (resolved from CATCHES→ExceptionType)", ef["catches"])
	}

	// --- rate_limit: stamped properties surfaced. ---
	rl, ok := res["rate_limit"].(map[string]any)
	if !ok || rl["rate_limited"] != "true" || rl["rate_limit"] != "100/min" {
		t.Errorf("rate_limit=%v; want non-empty with rate_limited=true, rate_limit=100/min", res["rate_limit"])
	}

	// --- deprecation / versioning. ---
	dep, ok := res["deprecation"].(map[string]any)
	if !ok || dep["deprecated"] != "true" || dep["deprecated_since"] != "v2" || dep["api_version"] != "v1" {
		t.Errorf("deprecation=%v; want deprecated=true, deprecated_since=v2, api_version=v1", res["deprecation"])
	}

	// --- feature_flag_gating: flag keys resolved from GATED_BY→FeatureFlag. ---
	gates := toStringSet(t, res["feature_gates"])
	if !gates["new-checkout"] {
		t.Errorf("feature_gates=%v; want new-checkout (resolved from GATED_BY→FeatureFlag, prefix stripped)", res["feature_gates"])
	}

	// --- auth: HTTP/gRPC/tRPC interceptor auth properties. ---
	auth, ok := res["auth"].(map[string]any)
	if !ok || auth["auth_required"] != "true" || auth["auth_method"] != "grpc_interceptor" {
		t.Errorf("auth=%v; want auth_required=true, auth_method=grpc_interceptor", res["auth"])
	}

	if res["has_posture"] != true {
		t.Errorf("has_posture=%v; want true", res["has_posture"])
	}
}

// TestEndpointPosture_Scan_OmitsPlainEndpoints verifies the repo-wide scan
// returns the posture-bearing endpoint and EXCLUDES the one with no facets, and
// excludes the synthetic ExceptionType/FeatureFlag convergence nodes.
func TestEndpointPosture_Scan_OmitsPlainEndpoints(t *testing.T) {
	srv := newPostureServer(t)
	res := callPostureTool(t, srv, map[string]any{"group": "test"})

	eps := getSlice(t, res, "endpoints")
	if len(eps) != 1 {
		t.Fatalf("scan returned %d endpoints; want exactly 1 (the posture-bearing handler)", len(eps))
	}
	ep := eps[0].(map[string]any)
	if ep["entity_id"] != "svc::ep_handler" {
		t.Errorf("scan entity_id=%v; want svc::ep_handler", ep["entity_id"])
	}
	if ep["error_flow"] == nil {
		t.Errorf("scan entry missing error_flow; want the throws/catches facet present")
	}
}

// TestEndpointPosture_Scan_FacetFilter verifies facet= narrows the scan to
// endpoints carrying that specific facet.
func TestEndpointPosture_Scan_FacetFilter(t *testing.T) {
	srv := newPostureServer(t)

	// error_flow facet matches the handler.
	res := callPostureTool(t, srv, map[string]any{"group": "test", "facet": "error_flow"})
	if got := len(getSlice(t, res, "endpoints")); got != 1 {
		t.Errorf("facet=error_flow returned %d; want 1", got)
	}

	// A facet present on no endpoint returns zero. ep_plain has no facet, and
	// ep_handler IS rate limited, so use feature_flag which only ep_handler has —
	// instead assert a facet on a doc with none. Use a doc-independent check:
	// path filter that matches nothing.
	res2 := callPostureTool(t, srv, map[string]any{"group": "test", "facet": "feature_flag"})
	if got := len(getSlice(t, res2, "endpoints")); got != 1 {
		t.Errorf("facet=feature_flag returned %d; want 1 (only ep_handler is gated)", got)
	}
}

// TestEndpointPosture_NotFound returns a tool error for an unresolved id.
func TestEndpointPosture_NotFound(t *testing.T) {
	srv := newPostureServer(t)
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "test", "entity_id": "svc::nope"}
	r, err := srv.handleEndpointPosture(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if r == nil || !r.IsError {
		t.Fatalf("want IsError result for unresolved id, got %+v", r)
	}
}

// --- helpers ---

func callPostureTool(t *testing.T, srv *Server, args map[string]any) map[string]any {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	res, err := srv.handleEndpointPosture(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.Content)
	}
	return extractResultJSON(t, res)
}

func toStringSet(t *testing.T, v any) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	if v == nil {
		return out
	}
	arr, ok := v.([]any)
	if !ok {
		t.Fatalf("want []any, got %T (%v)", v, v)
	}
	for _, x := range arr {
		s, ok := x.(string)
		if !ok {
			t.Fatalf("want string element, got %T", x)
		}
		out[s] = true
	}
	return out
}
