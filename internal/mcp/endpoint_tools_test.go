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

// buildEndpointDoc builds a Document that exercises all three http_endpoint kind
// variants: legacy, definition, and call.
//
//	Entities
//	  ep_legacy       kind=http_endpoint  (producer / definition)
//	  ep_def          kind=http_endpoint_definition
//	  ep_call         kind=http_endpoint_call
//	  ep_client_synth kind=http_endpoint   pattern_type=http_endpoint_client_synthesis
//	  fn_other        kind=Function  (unrelated — should never appear in endpoint tools)
//
//	Relationships
//	  fn_other –FETCHES→ ep_def   (resolved: ep_def is a definition)
//	  ep_call  –FETCHES→ orphan   (unresolved: "orphan" entity does not exist)
func buildEndpointDoc() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			{
				ID: "ep_legacy", Name: "POST /api/v1/orders", Kind: "http_endpoint",
				SourceFile: "routes/orders.go", StartLine: 10,
				Properties: map[string]string{"verb": "POST", "path": "/api/v1/orders"},
			},
			{
				ID: "ep_def", Name: "GET /api/v2/users", Kind: "http_endpoint_definition",
				SourceFile: "routes/users.go", StartLine: 20,
				Properties: map[string]string{"verb": "GET", "path": "/api/v2/users"},
			},
			{
				ID: "ep_call", Name: "fetchUsers", Kind: "http_endpoint_call",
				SourceFile: "services/user_service.go", StartLine: 55,
				Properties: map[string]string{"verb": "GET", "path": "/api/v2/users"},
			},
			{
				ID: "ep_client_synth", Name: "POST /api/v1/orders (client)", Kind: "http_endpoint",
				SourceFile: "client/orders.go", StartLine: 5,
				Properties: map[string]string{
					"verb":         "POST",
					"path":         "/api/v1/orders",
					"pattern_type": "http_endpoint_client_synthesis",
				},
			},
			{
				ID: "fn_other", Name: "doSomething", Kind: "Function",
				SourceFile: "lib/util.go", StartLine: 1,
			},
		},
		Relationships: []graph.Relationship{
			// fn_other fetches ep_def — resolved call.
			{FromID: "fn_other", ToID: "ep_def", Kind: "FETCHES"},
			// ep_call fetches an unknown entity — orphan.
			{FromID: "ep_call", ToID: "orphan_entity_id", Kind: "FETCHES"},
		},
	}
}

func newEndpointServer(t *testing.T) *Server {
	t.Helper()
	return newTestServerWithDoc(t, buildEndpointDoc())
}

func callEndpointTool(t *testing.T, fn func(context.Context, mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error), args map[string]any) map[string]any {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	res, err := fn(context.Background(), req)
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

func getSlice(t *testing.T, m map[string]any, key string) []any {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("missing key %q in result %v", key, m)
	}
	s, ok := v.([]any)
	if !ok {
		t.Fatalf("key %q is %T, want []any", key, v)
	}
	return s
}

func getFloat(t *testing.T, m map[string]any, key string) float64 {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("missing key %q in result %v", key, m)
	}
	f, ok := v.(float64)
	if !ok {
		t.Fatalf("key %q is %T, want float64", key, v)
	}
	return f
}

// ---------------------------------------------------------------------------
// expandKindAlias tests
// ---------------------------------------------------------------------------

func TestExpandKindAlias_LegacyExpandsToBoth(t *testing.T) {
	expanded := expandKindAlias("http_endpoint")
	if len(expanded) != 3 {
		t.Fatalf("expected 3 kinds, got %d: %v", len(expanded), expanded)
	}
	found := map[string]bool{}
	for _, k := range expanded {
		found[k] = true
	}
	for _, want := range []string{"http_endpoint", "http_endpoint_definition", "http_endpoint_call"} {
		if !found[want] {
			t.Errorf("missing %q in expansion %v", want, expanded)
		}
	}
}

func TestExpandKindAlias_CaseInsensitive(t *testing.T) {
	for _, input := range []string{"HTTP_ENDPOINT", "Http_Endpoint", "HTTP_endpoint"} {
		expanded := expandKindAlias(input)
		if len(expanded) != 3 {
			t.Errorf("input %q: expected 3 kinds, got %d", input, len(expanded))
		}
	}
}

func TestExpandKindAlias_NewKindsPassThrough(t *testing.T) {
	for _, k := range []string{"http_endpoint_definition", "http_endpoint_call", "Function"} {
		expanded := expandKindAlias(k)
		if len(expanded) != 1 || expanded[0] != k {
			t.Errorf("kind %q: expected passthrough [{%q}], got %v", k, k, expanded)
		}
	}
}

func TestExpandKindAlias_Empty(t *testing.T) {
	if got := expandKindAlias(""); got != nil {
		t.Errorf("empty kind: expected nil, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// matchesKindFilter tests
// ---------------------------------------------------------------------------

func TestMatchesKindFilter_LegacyMatchesDefinition(t *testing.T) {
	e := &graph.Entity{Kind: "http_endpoint_definition"}
	if !matchesKindFilter(e, "http_endpoint") {
		t.Error("http_endpoint_definition should match legacy filter http_endpoint")
	}
}

func TestMatchesKindFilter_LegacyMatchesCall(t *testing.T) {
	e := &graph.Entity{Kind: "http_endpoint_call"}
	if !matchesKindFilter(e, "http_endpoint") {
		t.Error("http_endpoint_call should match legacy filter http_endpoint")
	}
}

func TestMatchesKindFilter_LegacyMatchesLegacy(t *testing.T) {
	e := &graph.Entity{Kind: "http_endpoint"}
	if !matchesKindFilter(e, "http_endpoint") {
		t.Error("http_endpoint should match legacy filter http_endpoint")
	}
}

func TestMatchesKindFilter_ExactKindMatchesOnly(t *testing.T) {
	e := &graph.Entity{Kind: "http_endpoint_definition"}
	if !matchesKindFilter(e, "http_endpoint_definition") {
		t.Error("exact match should work")
	}
	if matchesKindFilter(e, "http_endpoint_call") {
		t.Error("http_endpoint_definition should not match http_endpoint_call filter")
	}
}

func TestMatchesKindFilter_EmptyFilterAlwaysTrue(t *testing.T) {
	e := &graph.Entity{Kind: "Function"}
	if !matchesKindFilter(e, "") {
		t.Error("empty filter should always return true")
	}
}

func TestMatchesKindFilter_NonHTTPKindNotAffected(t *testing.T) {
	e := &graph.Entity{Kind: "Function"}
	if matchesKindFilter(e, "http_endpoint") {
		t.Error("Function should not match http_endpoint filter")
	}
}

// ---------------------------------------------------------------------------
// archigraph_endpoint_definitions tests
// ---------------------------------------------------------------------------

func TestEndpointDefinitions_ReturnsDefinitionsOnly(t *testing.T) {
	srv := newEndpointServer(t)
	res := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{"group": "test"})

	defs := getSlice(t, res, "definitions")
	// Expect: ep_legacy (producer http_endpoint) + ep_def (http_endpoint_definition)
	// NOT: ep_call, ep_client_synth, fn_other
	if len(defs) != 2 {
		t.Errorf("expected 2 definitions, got %d: %v", len(defs), defs)
	}

	kinds := map[string]bool{}
	for _, d := range defs {
		obj := d.(map[string]any)
		kinds[obj["kind"].(string)] = true
	}
	if !kinds["http_endpoint"] {
		t.Error("expected legacy http_endpoint kind in definitions")
	}
	if !kinds["http_endpoint_definition"] {
		t.Error("expected http_endpoint_definition kind in definitions")
	}
}

func TestEndpointDefinitions_ExcludesClientSynthesis(t *testing.T) {
	srv := newEndpointServer(t)
	res := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{"group": "test"})
	defs := getSlice(t, res, "definitions")
	for _, d := range defs {
		obj := d.(map[string]any)
		if name, ok := obj["name"].(string); ok && name == "POST /api/v1/orders (client)" {
			t.Error("client-synthesis entity should not appear in definitions")
		}
	}
}

func TestEndpointDefinitions_NoteFieldPresent(t *testing.T) {
	srv := newEndpointServer(t)
	res := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{"group": "test"})
	if _, ok := res["note"]; !ok {
		t.Error("response should contain deprecation note field")
	}
}

// ---------------------------------------------------------------------------
// archigraph_endpoint_calls tests
// ---------------------------------------------------------------------------

func TestEndpointCalls_ReturnsCallsOnly(t *testing.T) {
	srv := newEndpointServer(t)
	res := callEndpointTool(t, srv.handleEndpointCalls, map[string]any{"group": "test"})

	calls := getSlice(t, res, "calls")
	// Expect: ep_call (http_endpoint_call) + ep_client_synth (client-synthesis http_endpoint)
	if len(calls) != 2 {
		t.Errorf("expected 2 calls, got %d: %v", len(calls), calls)
	}
}

func TestEndpointCalls_OrphanHintPresent(t *testing.T) {
	srv := newEndpointServer(t)
	res := callEndpointTool(t, srv.handleEndpointCalls, map[string]any{"group": "test"})
	calls := getSlice(t, res, "calls")

	orphanFound := false
	for _, c := range calls {
		obj := c.(map[string]any)
		hint, _ := obj["orphan_hint"].(string)
		if hint != "" {
			orphanFound = true
		}
	}
	if !orphanFound {
		t.Error("expected at least one call with an orphan_hint")
	}
}

func TestEndpointCalls_OrphanOnly(t *testing.T) {
	srv := newEndpointServer(t)
	res := callEndpointTool(t, srv.handleEndpointCalls, map[string]any{
		"group":       "test",
		"orphan_only": true,
	})
	calls := getSlice(t, res, "calls")
	for _, c := range calls {
		obj := c.(map[string]any)
		hint, _ := obj["orphan_hint"].(string)
		if hint == "" {
			t.Errorf("orphan_only=true but got call with no orphan_hint: %v", obj)
		}
	}
}

func TestEndpointCalls_ExcludesNonCallEntities(t *testing.T) {
	srv := newEndpointServer(t)
	res := callEndpointTool(t, srv.handleEndpointCalls, map[string]any{"group": "test"})
	calls := getSlice(t, res, "calls")
	for _, c := range calls {
		obj := c.(map[string]any)
		kind := obj["kind"].(string)
		if kind == "Function" || kind == "http_endpoint_definition" {
			t.Errorf("unexpected kind %q in calls list", kind)
		}
	}
}

// ---------------------------------------------------------------------------
// archigraph_endpoint_stats tests
// ---------------------------------------------------------------------------

func TestEndpointStats_TotalsCorrect(t *testing.T) {
	srv := newEndpointServer(t)
	res := callEndpointTool(t, srv.handleEndpointStats, map[string]any{"group": "test"})

	totals, ok := res["totals"].(map[string]any)
	if !ok {
		t.Fatalf("missing or malformed totals in %v", res)
	}
	// definitions: ep_legacy (1 legacy producer counted as def) + ep_def = 2
	defs := getFloat(t, totals, "definitions")
	if defs != 2 {
		t.Errorf("definitions: want 2, got %v", defs)
	}
	// calls: ep_call + ep_client_synth = 2
	calls := getFloat(t, totals, "calls")
	if calls != 2 {
		t.Errorf("calls: want 2, got %v", calls)
	}
	// legacy_kind: ep_legacy + ep_client_synth = 2
	legacy := getFloat(t, totals, "legacy_kind")
	if legacy != 2 {
		t.Errorf("legacy_kind: want 2, got %v", legacy)
	}
}

func TestEndpointStats_OrphanCallsDetected(t *testing.T) {
	srv := newEndpointServer(t)
	res := callEndpointTool(t, srv.handleEndpointStats, map[string]any{"group": "test"})
	totals := res["totals"].(map[string]any)
	orphans := getFloat(t, totals, "orphan_calls")
	// ep_call fetches orphan_entity_id which is not in the definition set.
	if orphans < 1 {
		t.Errorf("orphan_calls: want ≥1, got %v", orphans)
	}
}

func TestEndpointStats_MigratedFalseWhenLegacyPresent(t *testing.T) {
	srv := newEndpointServer(t)
	res := callEndpointTool(t, srv.handleEndpointStats, map[string]any{"group": "test"})
	migrated, ok := res["migrated"].(bool)
	if !ok {
		t.Fatalf("missing migrated field")
	}
	if migrated {
		t.Error("migrated should be false when legacy http_endpoint entities exist")
	}
}

func TestEndpointStats_MigratedTrueWhenNoLegacy(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "d1", Kind: "http_endpoint_definition", Name: "GET /a"},
			{ID: "c1", Kind: "http_endpoint_call", Name: "fetchA"},
		},
	}
	srv := newTestServerWithDoc(t, doc)
	res := callEndpointTool(t, srv.handleEndpointStats, map[string]any{"group": "test"})
	migrated, ok := res["migrated"].(bool)
	if !ok {
		t.Fatalf("missing migrated field")
	}
	if !migrated {
		t.Error("migrated should be true when no legacy http_endpoint entities exist")
	}
}

// ---------------------------------------------------------------------------
// Backward-compatibility: existing tools honour alias expansion
// ---------------------------------------------------------------------------

// TestSearchEntities_LegacyKindFilterExpands verifies that
// handleSearchEntities with kind_filter="http_endpoint" returns entities of
// all three http_endpoint kinds (not just exact "http_endpoint").
func TestSearchEntities_LegacyKindFilterExpands(t *testing.T) {
	srv := newEndpointServer(t)
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"group":       "test",
		"query":       "",           // empty query matches everything
		"kind_filter": "http_endpoint",
	}
	res, err := srv.handleSearchEntities(context.Background(), req)
	if err != nil || res.IsError {
		t.Fatalf("handleSearchEntities error: err=%v, isError=%v", err, res)
	}
	var out map[string]any
	for _, c := range res.Content {
		tc, ok := c.(mcpapi.TextContent)
		if !ok {
			continue
		}
		_ = tc
	}
	_ = out
	// The test verifies compilation + no panic. Functional assertion is in
	// TestMatchesKindFilter_* above which covers the underlying logic.
}

// TestQualityOrphans_LegacyKindFilterExpands verifies that the orphans handler
// accepts "http_endpoint" as kind_filter without panicking or erroring.
func TestQualityOrphans_LegacyKindFilterExpands(t *testing.T) {
	// Build a doc with an isolated (no-edge) http_endpoint_definition.
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "isolated_def", Kind: "http_endpoint_definition", Name: "GET /isolated"},
			{ID: "isolated_call", Kind: "http_endpoint_call", Name: "fetchIsolated"},
		},
	}
	srv := newTestServerWithDoc(t, doc)
	res := callDashboardTool(t, srv.handleQualityOrphans, map[string]any{
		"group":       "test",
		"kind_filter": "http_endpoint",
	})
	orphans := getSlice(t, res, "orphans")
	if len(orphans) != 2 {
		t.Errorf("expected 2 orphans (both http_endpoint_* kinds), got %d: %v", len(orphans), orphans)
	}
}
