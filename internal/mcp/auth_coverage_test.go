package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// buildAuthCoverageDoc builds a Document with a mix of protected and unprotected
// HTTP endpoints plus auth_policy entities.
//
//	Endpoints
//	  ep_login_required   — protected by file-level auth_policy (login_required)
//	  ep_tagged_auth      — protected via TAGGED_AS edge to auth_policy
//	  ep_prop_auth        — protected via auth_decorator property
//	  ep_public           — no auth (severity: warn)
//	  ep_delete_no_auth   — DELETE on /users/{user_id}, no auth (severity: error, IDOR+sensitive)
//	  ep_payment_no_auth  — POST /checkout, no auth (severity: error, sensitive)
//	  ep_call_site        — http_endpoint_call — should be excluded
//
//	Auth entities
//	  auth_login_required  — subtype=auth_policy in same file as ep_login_required
//	  auth_policy_tagged   — subtype=auth_policy, TAGGED_AS from ep_tagged_auth
func buildAuthCoverageDoc() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			// Protected: shares source file with auth_policy entity.
			{
				ID: "ep_login_required", Name: "get_dashboard",
				Kind:       "http_endpoint_definition",
				SourceFile: "views/dashboard.py", StartLine: 10,
				Properties: map[string]string{"verb": "GET", "path": "/dashboard"},
			},
			// Protected: TAGGED_AS edge to auth_policy.
			{
				ID: "ep_tagged_auth", Name: "list_orders",
				Kind:       "http_endpoint_definition",
				SourceFile: "routes/orders.py", StartLine: 20,
				Properties: map[string]string{"verb": "GET", "path": "/orders"},
			},
			// Protected: auth_decorator property on entity itself.
			{
				ID: "ep_prop_auth", Name: "create_post",
				Kind:       "http_endpoint_definition",
				SourceFile: "routes/posts.py", StartLine: 30,
				Properties: map[string]string{
					"verb":           "POST",
					"path":           "/posts",
					"auth_decorator": "jwt_required",
				},
			},
			// Unprotected: no auth signal → severity warn.
			{
				ID: "ep_public", Name: "list_articles",
				Kind:       "http_endpoint_definition",
				SourceFile: "routes/public.py", StartLine: 5,
				Properties: map[string]string{"verb": "GET", "path": "/articles"},
			},
			// Unprotected: sensitive (delete) + IDOR ({user_id}) → severity error.
			{
				ID: "ep_delete_no_auth", Name: "delete_user",
				Kind:       "http_endpoint_definition",
				SourceFile: "routes/users.py", StartLine: 42,
				Properties: map[string]string{"verb": "DELETE", "path": "/users/{user_id}"},
			},
			// Unprotected: sensitive (payment/checkout) → severity error.
			{
				ID: "ep_payment_no_auth", Name: "checkout",
				Kind:       "http_endpoint_definition",
				SourceFile: "routes/billing.py", StartLine: 15,
				Properties: map[string]string{"verb": "POST", "path": "/checkout"},
			},
			// Call-site — must NOT appear in auth coverage results.
			{
				ID: "ep_call_site", Name: "fetchOrders",
				Kind:       "http_endpoint_call",
				SourceFile: "services/order_service.py", StartLine: 8,
				Properties: map[string]string{"verb": "GET", "path": "/orders"},
			},
			// Auth policy entity: shares file with ep_login_required.
			{
				ID: "auth_login_required", Name: "login_required@views/dashboard.py:9",
				Kind:       "SCOPE.Config",
				Subtype:    "auth_policy",
				SourceFile: "views/dashboard.py", StartLine: 9,
				Properties: map[string]string{
					"kind":            "auth_policy",
					"annotation_name": "@login_required",
					"middleware_name": "login_required",
				},
			},
			// Auth policy entity: linked to ep_tagged_auth via TAGGED_AS.
			{
				ID: "auth_policy_tagged", Name: "auth_policy_nestjs_JwtAuthGuard",
				Kind:       "SCOPE.Config",
				Subtype:    "auth_policy",
				SourceFile: "routes/orders.py", StartLine: 18,
				Properties: map[string]string{
					"kind":            "auth_policy",
					"annotation_name": "@UseGuards",
					"middleware_name": "JwtAuthGuard",
				},
			},
		},
		Relationships: []graph.Relationship{
			// TAGGED_AS: ep_tagged_auth → auth_policy_tagged (but ep_tagged_auth and
			// auth_policy_tagged are in the same file anyway — test TAGGED_AS path too
			// by using a separate file; file-level signal won't help here because
			// ep_tagged_auth SourceFile != auth_policy_tagged SourceFile below — wait,
			// they ARE equal in the fixture. Override by using a relationship only
			// (TAGGED_AS detection runs before file-level detection, but file-level
			// would also fire here). The TAGGED_AS path is exercised in TestAuthCoverage_TaggedAS.
			{ID: "rel_tagged", FromID: "ep_tagged_auth", ToID: "auth_policy_tagged", Kind: "TAGGED_AS"},
		},
	}
}

func callAuthCoverageTool(t *testing.T, s *Server, args map[string]any) map[string]any {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	res, err := s.handleAuthCoverage(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.Content)
	}
	var out map[string]any
	for _, c := range res.Content {
		tc, ok := c.(mcpapi.TextContent)
		if !ok {
			continue
		}
		if err := json.Unmarshal([]byte(tc.Text), &out); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
	}
	return out
}

// endpointsByID extracts the endpoints array and indexes by entity_id suffix
// (after "::" separator) for easy lookup in tests.
func endpointsByID(t *testing.T, out map[string]any) map[string]map[string]any {
	t.Helper()
	eps, ok := out["endpoints"].([]any)
	if !ok {
		t.Fatalf("endpoints is %T, want []any", out["endpoints"])
	}
	result := make(map[string]map[string]any, len(eps))
	for _, raw := range eps {
		ep, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("endpoint entry is %T", raw)
		}
		id, _ := ep["entity_id"].(string)
		// Strip the "repo1::" prefix.
		if idx := len("repo1::"); len(id) > idx {
			id = id[idx:]
		}
		result[id] = ep
	}
	return result
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestAuthCoverage_CallSitesExcluded(t *testing.T) {
	s := newTestServerWithDoc(t, buildAuthCoverageDoc())
	out := callAuthCoverageTool(t, s, map[string]any{"group": "test"})
	eps := endpointsByID(t, out)
	if _, found := eps["ep_call_site"]; found {
		t.Error("http_endpoint_call should not appear in auth coverage results")
	}
}

func TestAuthCoverage_FileLevel_HasAuth(t *testing.T) {
	s := newTestServerWithDoc(t, buildAuthCoverageDoc())
	out := callAuthCoverageTool(t, s, map[string]any{"group": "test"})
	eps := endpointsByID(t, out)

	ep, ok := eps["ep_login_required"]
	if !ok {
		t.Fatal("ep_login_required not found in results")
	}
	if hasAuth, _ := ep["has_auth"].(bool); !hasAuth {
		t.Errorf("ep_login_required should have has_auth=true (file-level auth_policy)")
	}
	if sev, _ := ep["severity"].(string); sev != "info" {
		t.Errorf("ep_login_required: want severity=info, got %q", sev)
	}
}

func TestAuthCoverage_PropertyLevel_HasAuth(t *testing.T) {
	s := newTestServerWithDoc(t, buildAuthCoverageDoc())
	out := callAuthCoverageTool(t, s, map[string]any{"group": "test"})
	eps := endpointsByID(t, out)

	ep, ok := eps["ep_prop_auth"]
	if !ok {
		t.Fatal("ep_prop_auth not found in results")
	}
	if hasAuth, _ := ep["has_auth"].(bool); !hasAuth {
		t.Errorf("ep_prop_auth should have has_auth=true (auth_decorator property)")
	}
	if sev, _ := ep["severity"].(string); sev != "info" {
		t.Errorf("ep_prop_auth: want severity=info, got %q", sev)
	}
}

func TestAuthCoverage_Public_SeverityWarn(t *testing.T) {
	s := newTestServerWithDoc(t, buildAuthCoverageDoc())
	out := callAuthCoverageTool(t, s, map[string]any{"group": "test"})
	eps := endpointsByID(t, out)

	ep, ok := eps["ep_public"]
	if !ok {
		t.Fatal("ep_public not found in results")
	}
	if hasAuth, _ := ep["has_auth"].(bool); hasAuth {
		t.Errorf("ep_public should have has_auth=false")
	}
	if sev, _ := ep["severity"].(string); sev != "warn" {
		t.Errorf("ep_public: want severity=warn, got %q", sev)
	}
}

func TestAuthCoverage_DeleteWithIDOR_SeverityError(t *testing.T) {
	s := newTestServerWithDoc(t, buildAuthCoverageDoc())
	out := callAuthCoverageTool(t, s, map[string]any{"group": "test"})
	eps := endpointsByID(t, out)

	ep, ok := eps["ep_delete_no_auth"]
	if !ok {
		t.Fatal("ep_delete_no_auth not found in results")
	}
	if hasAuth, _ := ep["has_auth"].(bool); hasAuth {
		t.Errorf("ep_delete_no_auth should have has_auth=false")
	}
	if sev, _ := ep["severity"].(string); sev != "error" {
		t.Errorf("ep_delete_no_auth: want severity=error, got %q", sev)
	}
	if idorRisk, _ := ep["idor_risk"].(bool); !idorRisk {
		t.Errorf("ep_delete_no_auth: want idor_risk=true")
	}
	if sensitiveOp, _ := ep["sensitive_op"].(bool); !sensitiveOp {
		t.Errorf("ep_delete_no_auth: want sensitive_op=true")
	}
}

func TestAuthCoverage_PaymentEndpoint_SeverityError(t *testing.T) {
	s := newTestServerWithDoc(t, buildAuthCoverageDoc())
	out := callAuthCoverageTool(t, s, map[string]any{"group": "test"})
	eps := endpointsByID(t, out)

	ep, ok := eps["ep_payment_no_auth"]
	if !ok {
		t.Fatal("ep_payment_no_auth not found in results")
	}
	if sev, _ := ep["severity"].(string); sev != "error" {
		t.Errorf("ep_payment_no_auth: want severity=error, got %q", sev)
	}
	if sensitiveOp, _ := ep["sensitive_op"].(bool); !sensitiveOp {
		t.Errorf("ep_payment_no_auth: want sensitive_op=true (checkout matches payment/checkout terms)")
	}
}

func TestAuthCoverage_OnlyMissing(t *testing.T) {
	s := newTestServerWithDoc(t, buildAuthCoverageDoc())
	out := callAuthCoverageTool(t, s, map[string]any{
		"group":        "test",
		"only_missing": true,
	})
	eps := endpointsByID(t, out)

	// Protected endpoints must not appear.
	for _, id := range []string{"ep_login_required", "ep_tagged_auth", "ep_prop_auth"} {
		if _, found := eps[id]; found {
			t.Errorf("only_missing=true: protected endpoint %q should be excluded", id)
		}
	}
	// Unprotected endpoints must appear.
	for _, id := range []string{"ep_public", "ep_delete_no_auth", "ep_payment_no_auth"} {
		if _, found := eps[id]; !found {
			t.Errorf("only_missing=true: unprotected endpoint %q should be present", id)
		}
	}
}

func TestAuthCoverage_RepoSummary(t *testing.T) {
	s := newTestServerWithDoc(t, buildAuthCoverageDoc())
	out := callAuthCoverageTool(t, s, map[string]any{"group": "test"})

	summaries, ok := out["repo_summaries"].([]any)
	if !ok || len(summaries) == 0 {
		t.Fatal("expected non-empty repo_summaries")
	}
	summary := summaries[0].(map[string]any)

	total := int(summary["total"].(float64))
	covered := int(summary["covered"].(float64))
	uncovered := int(summary["uncovered"].(float64))

	// 6 definition endpoints (ep_call_site excluded), 3 covered, 3 uncovered.
	if total != 6 {
		t.Errorf("total: want 6, got %d", total)
	}
	if covered != 3 {
		t.Errorf("covered: want 3, got %d", covered)
	}
	if uncovered != 3 {
		t.Errorf("uncovered: want 3, got %d", uncovered)
	}

	// 3/6 = 50% < 80% → default-allow
	policy, _ := summary["default_policy"].(string)
	if policy != "default-allow" {
		t.Errorf("default_policy: want default-allow, got %q", policy)
	}

	errorCount := int(summary["error_count"].(float64))
	warnCount := int(summary["warn_count"].(float64))
	if errorCount != 2 {
		t.Errorf("error_count: want 2, got %d", errorCount)
	}
	if warnCount != 1 {
		t.Errorf("warn_count: want 1, got %d", warnCount)
	}
}

func TestAuthCoverage_DefaultDeny(t *testing.T) {
	// Build a repo where ≥80% of endpoints are protected.
	doc := &graph.Document{
		Entities: []graph.Entity{
			// 4 protected (via file-level auth_policy), 1 unprotected → 80% → default-deny.
			{
				ID: "e1", Kind: "http_endpoint_definition",
				SourceFile: "views/a.py", Name: "view_a",
				Properties: map[string]string{"verb": "GET", "path": "/a"},
			},
			{
				ID: "e2", Kind: "http_endpoint_definition",
				SourceFile: "views/a.py", Name: "view_b",
				Properties: map[string]string{"verb": "GET", "path": "/b"},
			},
			{
				ID: "e3", Kind: "http_endpoint_definition",
				SourceFile: "views/a.py", Name: "view_c",
				Properties: map[string]string{"verb": "GET", "path": "/c"},
			},
			{
				ID: "e4", Kind: "http_endpoint_definition",
				SourceFile: "views/a.py", Name: "view_d",
				Properties: map[string]string{"verb": "GET", "path": "/d"},
			},
			{
				ID: "e5", Kind: "http_endpoint_definition",
				SourceFile: "views/public.py", Name: "public_view",
				Properties: map[string]string{"verb": "GET", "path": "/public"},
			},
			// auth_policy in views/a.py covers e1-e4.
			{
				ID: "auth1", Kind: "SCOPE.Config", Subtype: "auth_policy",
				SourceFile: "views/a.py", Name: "login_required@views/a.py:1",
				Properties: map[string]string{"middleware_name": "login_required"},
			},
		},
	}
	s := newTestServerWithDoc(t, doc)
	out := callAuthCoverageTool(t, s, map[string]any{"group": "test"})

	summaries, _ := out["repo_summaries"].([]any)
	if len(summaries) == 0 {
		t.Fatal("expected non-empty repo_summaries")
	}
	policy, _ := summaries[0].(map[string]any)["default_policy"].(string)
	if policy != "default-deny" {
		t.Errorf("want default-deny (80%% coverage), got %q", policy)
	}
}

func TestAuthCoverage_TaggedAS(t *testing.T) {
	// Endpoint in a DIFFERENT file than the auth_policy entity, but linked via TAGGED_AS.
	doc := &graph.Document{
		Entities: []graph.Entity{
			{
				ID: "ep1", Kind: "http_endpoint_definition",
				SourceFile: "routes/api.py", Name: "protected_endpoint",
				Properties: map[string]string{"verb": "GET", "path": "/protected"},
			},
			{
				ID: "auth1", Kind: "SCOPE.Config", Subtype: "auth_policy",
				SourceFile: "middleware/auth.py", Name: "JwtAuthGuard",
				Properties: map[string]string{"middleware_name": "JwtAuthGuard"},
			},
		},
		Relationships: []graph.Relationship{
			{ID: "rel1", FromID: "ep1", ToID: "auth1", Kind: "TAGGED_AS"},
		},
	}
	s := newTestServerWithDoc(t, doc)
	out := callAuthCoverageTool(t, s, map[string]any{"group": "test"})
	eps := endpointsByID(t, out)
	ep, ok := eps["ep1"]
	if !ok {
		t.Fatal("ep1 not found in results")
	}
	if hasAuth, _ := ep["has_auth"].(bool); !hasAuth {
		t.Errorf("ep1 should have has_auth=true (TAGGED_AS auth_policy)")
	}
}

func TestAuthCoverage_SensitiveTermDetection(t *testing.T) {
	cases := []struct {
		name, path string
		wantSens   bool
		wantTerm   string
	}{
		{"checkout", "/api/checkout", true, "checkout"},
		{"change_password", "/api/password/change", true, "password"},
		{"admin_panel", "/admin/users", true, "admin"},
		{"list_articles", "/articles", false, ""},
		{"delete_comment", "/comments/123", true, "delete"},
	}

	for _, tc := range cases {
		got, match := isSensitiveOperation(tc.name, tc.path)
		if got != tc.wantSens {
			t.Errorf("isSensitiveOperation(%q, %q) = %v, want %v", tc.name, tc.path, got, tc.wantSens)
		}
		if tc.wantSens && tc.wantTerm != "" && match == "" {
			t.Errorf("isSensitiveOperation(%q, %q): expected match containing %q, got %q",
				tc.name, tc.path, tc.wantTerm, match)
		}
	}
}

func TestAuthCoverage_IDORRiskDetection(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/api/users/{user_id}", true},
		{"/api/users/:user_id", true},
		{"/api/accounts/{account_id}", true},
		{"/api/posts/123", false},
		{"/api/items", false},
	}

	for _, tc := range cases {
		got := hasIDORRisk(tc.path, nil)
		if got != tc.want {
			t.Errorf("hasIDORRisk(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestAuthCoverage_SeverityOrdering(t *testing.T) {
	// Errors must appear before warns, warns before infos.
	s := newTestServerWithDoc(t, buildAuthCoverageDoc())
	out := callAuthCoverageTool(t, s, map[string]any{"group": "test"})
	eps, _ := out["endpoints"].([]any)

	lastRank := -1
	for _, raw := range eps {
		ep := raw.(map[string]any)
		sev, _ := ep["severity"].(string)
		rank := severityRank(sev)
		if rank < lastRank {
			t.Errorf("severity ordering violated: %q (rank %d) came after rank %d", sev, rank, lastRank)
		}
		lastRank = rank
	}
}
