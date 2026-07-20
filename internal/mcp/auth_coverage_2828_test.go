package mcp

// auth_coverage_2828_test.go — #2828 token-cost optimisation tests.
//
// Live telemetry flagged grafel_auth_coverage as the single biggest token
// hog (~7.5K tok/call). These tests prove the new format=terse default + the
// lowered limit + the token_budget arg cut payload bytes substantially while
// preserving the security-essential facts an agent acts on, and that the
// legacy format=full structured shape is still available + unchanged.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// buildManyEndpointDoc builds a doc with n unprotected endpoints (half sensitive
// DELETEs, half plain GETs) so the finding list is large enough to exercise
// limit/budget truncation and to measure terse-vs-full byte deltas.
func buildManyEndpointDoc(n int) *graph.Document {
	ents := make([]graph.Entity, 0, n)
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			ents = append(ents, graph.Entity{
				ID:         fmt.Sprintf("ep_del_%d", i),
				Name:       fmt.Sprintf("delete_user_%d", i),
				Kind:       "http_endpoint_definition",
				SourceFile: fmt.Sprintf("routes/users_%d.py", i),
				StartLine:  10 + i,
			}.WithProperties(map[string]string{"verb": "DELETE", "path": fmt.Sprintf("/users/{user_id}/%d", i)}))
		} else {
			ents = append(ents, graph.Entity{
				ID:         fmt.Sprintf("ep_get_%d", i),
				Name:       fmt.Sprintf("list_things_%d", i),
				Kind:       "http_endpoint_definition",
				SourceFile: fmt.Sprintf("routes/things_%d.py", i),
				StartLine:  20 + i,
			}.WithProperties(map[string]string{"verb": "GET", "path": fmt.Sprintf("/things/%d", i)}))
		}
	}
	return &graph.Document{Entities: ents}
}

// rawAuthBytes calls the handler and returns the raw wire payload length plus
// the decoded map, so a test can assert a concrete byte reduction.
func rawAuthBytes(t *testing.T, s *Server, args map[string]any) (int, map[string]any) {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	res, err := s.handleAuthCoverage(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("tool error: %+v", res)
	}
	text := extractResultText(t, res)
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, text)
	}
	return len(text), out
}

func TestAuthCoverage_2828_TerseIsDefaultAndSmaller(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, buildManyEndpointDoc(40))

	terseBytes, terseOut := rawAuthBytes(t, s, map[string]any{"group": "test"})
	fullBytes, fullOut := rawAuthBytes(t, s, map[string]any{"group": "test", "format": "full"})

	// Default must be terse.
	if got := terseOut["format"]; got != "terse" {
		t.Errorf("default format: got %v, want terse", got)
	}
	if got := fullOut["format"]; got != "full" {
		t.Errorf("format=full: got %v, want full", got)
	}

	// Terse must be substantially smaller than full on the same data.
	if terseBytes >= fullBytes {
		t.Fatalf("terse (%d B) not smaller than full (%d B)", terseBytes, fullBytes)
	}
	reduction := 100.0 * float64(fullBytes-terseBytes) / float64(fullBytes)
	if reduction < 30.0 {
		t.Errorf("terse reduction only %.1f%% (terse=%d full=%d); want >=30%%", reduction, terseBytes, fullBytes)
	}
	t.Logf("auth_coverage bytes: terse=%d full=%d (%.1f%% smaller); est tokens terse=%d full=%d",
		terseBytes, fullBytes, reduction, terseBytes/4, fullBytes/4)

	// Terse shape: findings present, endpoints/note absent.
	if _, ok := terseOut["findings"]; !ok {
		t.Error("terse missing `findings`")
	}
	if _, ok := terseOut["endpoints"]; ok {
		t.Error("terse must not include the verbose `endpoints` array")
	}
	if _, ok := terseOut["note"]; ok {
		t.Error("terse must not include the static `note` blob")
	}
	// Full shape: endpoints + note present.
	if _, ok := fullOut["endpoints"]; !ok {
		t.Error("full missing `endpoints`")
	}
	if _, ok := fullOut["note"]; !ok {
		t.Error("full missing `note`")
	}
}

func TestAuthCoverage_2828_TersePreservesEssentialFacts(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, buildManyEndpointDoc(4))
	_, out := rawAuthBytes(t, s, map[string]any{"group": "test"})

	findings, ok := out["findings"].([]any)
	if !ok || len(findings) == 0 {
		t.Fatalf("findings is %T (len?)", out["findings"])
	}
	joined := ""
	for _, f := range findings {
		joined += f.(string) + "\n"
	}
	// A sensitive unprotected DELETE on /users/{user_id} must surface severity,
	// verb, path, the NO-AUTH state and the IDOR + sensitive flags.
	for _, want := range []string{"error", "DELETE", "/users/{user_id}", "NO-AUTH", "idor", "sensitive"} {
		if !strings.Contains(joined, want) {
			t.Errorf("terse findings missing %q\n%s", want, joined)
		}
	}
	// Repo summaries / overall coverage must survive in terse mode (the agent
	// relies on these for the default-allow/deny posture).
	if _, ok := out["repo_summaries"].([]any); !ok {
		t.Error("terse dropped repo_summaries")
	}
	if _, ok := out["overall_coverage"]; !ok {
		t.Error("terse dropped overall_coverage")
	}
}

func TestAuthCoverage_2828_DefaultLimitCapsAt50WithMarker(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, buildManyEndpointDoc(120))
	_, out := rawAuthBytes(t, s, map[string]any{"group": "test"})

	count := int(out["count"].(float64))
	total := int(out["total"].(float64))
	if count != 50 {
		t.Errorf("default limit: count=%d, want 50", count)
	}
	if total != 120 {
		t.Errorf("total: got %d, want 120", total)
	}
	if out["truncated"] != true {
		t.Error("expected truncated=true")
	}
	if tc, ok := out["truncated_count"].(float64); !ok || int(tc) != 70 {
		t.Errorf("truncated_count: got %v, want 70", out["truncated_count"])
	}
	if _, ok := out["truncation_note"].(string); !ok {
		t.Error("expected a truncation_note marker")
	}
	findings := out["findings"].([]any)
	if len(findings) != 50 {
		t.Errorf("findings len=%d, want 50", len(findings))
	}
}

func TestAuthCoverage_2828_LimitOptInReturnsMore(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, buildManyEndpointDoc(120))
	_, out := rawAuthBytes(t, s, map[string]any{"group": "test", "limit": 200})
	if int(out["count"].(float64)) != 120 {
		t.Errorf("limit=200: count=%d, want 120 (all)", int(out["count"].(float64)))
	}
	if out["truncated"] != false {
		t.Error("limit=200 over 120 items should not be truncated")
	}
}

func TestAuthCoverage_2828_TokenBudgetTruncatesWithMarker(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, buildManyEndpointDoc(120))
	// A tight budget forces truncation well below the 50-row limit.
	rawBytes, out := rawAuthBytes(t, s, map[string]any{"group": "test", "limit": 200, "token_budget": 300})

	count := int(out["count"].(float64))
	total := int(out["total"].(float64))
	if count >= total {
		t.Fatalf("token_budget did not truncate: count=%d total=%d", count, total)
	}
	if out["truncated"] != true {
		t.Error("expected truncated=true under token_budget")
	}
	if _, ok := out["truncation_note"].(string); !ok {
		t.Error("expected truncation_note marker under token_budget")
	}
	// The findings list (the budgeted part) must fit roughly within the budget.
	findings := out["findings"].([]any)
	findingsBytes := 0
	for _, f := range findings {
		findingsBytes += len(f.(string))
	}
	if findingsBytes > 300*4 {
		t.Errorf("findings bytes %d exceed token_budget*4=%d", findingsBytes, 300*4)
	}
	t.Logf("token_budget=300: returned %d/%d findings, raw payload %d B", count, total, rawBytes)
}

func TestAuthCoverage_2828_FullShapeUnchanged(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, buildManyEndpointDoc(2))
	_, out := rawAuthBytes(t, s, map[string]any{"group": "test", "format": "full"})
	eps, ok := out["endpoints"].([]any)
	if !ok || len(eps) == 0 {
		t.Fatalf("full mode endpoints is %T", out["endpoints"])
	}
	ep := eps[0].(map[string]any)
	// The legacy per-record keys the structured callers depend on.
	for _, k := range []string{"entity_id", "has_auth", "severity", "repo"} {
		if _, ok := ep[k]; !ok {
			t.Errorf("full endpoint record missing key %q", k)
		}
	}
}
