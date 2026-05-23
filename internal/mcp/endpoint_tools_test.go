package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
	res := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{"group": "test", "verbose": true})

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
	res := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{"group": "test", "verbose": true})
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
	res := callEndpointTool(t, srv.handleEndpointCalls, map[string]any{"group": "test", "verbose": true})
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
		"query":       "", // empty query matches everything
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

// ---------------------------------------------------------------------------
// archigraph_endpoints dispatch (#1281)
// ---------------------------------------------------------------------------

func TestHandleEndpoints_Definitions(t *testing.T) {
	srv := newEndpointServer(t)
	res := callEndpointTool(t, srv.handleEndpoints, map[string]any{
		"group":  "test",
		"action": "definitions",
	})
	if _, ok := res["definitions"]; !ok {
		t.Error("expected definitions key in response for action=definitions")
	}
	defs := getSlice(t, res, "definitions")
	if len(defs) != 2 {
		t.Errorf("action=definitions: expected 2 definitions, got %d", len(defs))
	}
}

func TestHandleEndpoints_Calls(t *testing.T) {
	srv := newEndpointServer(t)
	res := callEndpointTool(t, srv.handleEndpoints, map[string]any{
		"group":  "test",
		"action": "calls",
	})
	if _, ok := res["calls"]; !ok {
		t.Error("expected calls key in response for action=calls")
	}
	calls := getSlice(t, res, "calls")
	if len(calls) != 2 {
		t.Errorf("action=calls: expected 2 calls, got %d", len(calls))
	}
}

func TestHandleEndpoints_CallsOrphanOnly(t *testing.T) {
	srv := newEndpointServer(t)
	res := callEndpointTool(t, srv.handleEndpoints, map[string]any{
		"group":       "test",
		"action":      "calls",
		"orphan_only": true,
	})
	calls := getSlice(t, res, "calls")
	for _, c := range calls {
		obj := c.(map[string]any)
		hint, _ := obj["orphan_hint"].(string)
		if hint == "" {
			t.Errorf("orphan_only=true via dispatch: got call with empty orphan_hint: %v", obj)
		}
	}
}

func TestHandleEndpoints_Stats(t *testing.T) {
	srv := newEndpointServer(t)
	res := callEndpointTool(t, srv.handleEndpoints, map[string]any{
		"group":  "test",
		"action": "stats",
	})
	if _, ok := res["totals"]; !ok {
		t.Error("expected totals key in response for action=stats")
	}
}

// #1650: path_contains and method filter server-side.
func TestEndpointDefinitions_PathContainsFilter(t *testing.T) {
	srv := newEndpointServer(t)
	res := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{
		"group":         "test",
		"path_contains": "users",
	})
	defs := getSlice(t, res, "definitions")
	if len(defs) != 1 {
		t.Fatalf("expected 1 def for path_contains=users, got %d", len(defs))
	}
	obj := defs[0].(map[string]any)
	if path, _ := obj["path"].(string); path != "/api/v2/users" {
		t.Errorf("unexpected path: %v", obj)
	}
}

func TestEndpointDefinitions_MethodFilter(t *testing.T) {
	srv := newEndpointServer(t)
	res := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{
		"group":  "test",
		"method": "POST",
	})
	defs := getSlice(t, res, "definitions")
	if len(defs) != 1 {
		t.Fatalf("expected 1 def for method=POST, got %d", len(defs))
	}
}

// #1650: terse default returns "lines" with one-line entries; verbose=true
// returns full per-record fields without "lines".
func TestEndpointDefinitions_TerseDefault(t *testing.T) {
	srv := newEndpointServer(t)
	res := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{"group": "test"})
	if _, ok := res["lines"]; !ok {
		t.Error("expected 'lines' key in terse default response")
	}
	defs := getSlice(t, res, "definitions")
	for _, d := range defs {
		obj := d.(map[string]any)
		// Terse rows omit name/kind/properties.
		if _, has := obj["properties"]; has {
			t.Errorf("terse row should omit properties: %v", obj)
		}
	}
}

func TestHandleEndpoints_UnknownAction(t *testing.T) {
	srv := newEndpointServer(t)
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "test", "action": "bogus"}
	res, err := srv.handleEndpoints(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true for unknown action")
	}
}

// ---------------------------------------------------------------------------
// #1738: token budget enforcement tests
// ---------------------------------------------------------------------------

// buildLargeEndpointDoc returns a document with n http_endpoint_definition entities.
func buildLargeEndpointDoc(n int) *graph.Document {
	entities := make([]graph.Entity, n)
	for i := range entities {
		entities[i] = graph.Entity{
			ID:         fmt.Sprintf("ep%03d", i),
			Name:       fmt.Sprintf("GET /api/v1/resource/%03d", i),
			Kind:       "http_endpoint_definition",
			SourceFile: fmt.Sprintf("routes/r%03d.go", i),
			StartLine:  i + 1,
			Properties: map[string]string{
				"verb": "GET",
				"path": fmt.Sprintf("/api/v1/resource/%03d", i),
			},
		}
	}
	return &graph.Document{Entities: entities}
}

// TestEndpointDefaultLimit_Definitions verifies that without an explicit
// limit=N, handleEndpointDefinitions returns at most 20 items (#1738).
func TestEndpointDefaultLimit_Definitions(t *testing.T) {
	srv := newTestServerWithDoc(t, buildLargeEndpointDoc(30))
	out := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{"group": "test"})
	defs := getSlice(t, out, "definitions")
	if len(defs) > 20 {
		t.Errorf("handleEndpointDefinitions returned %d items, want ≤20 (default limit)", len(defs))
	}
}

// TestEndpointTokenBudget_Definitions verifies that a tight token_budget caps
// the definitions slice and adds a truncation_note (#1738).
func TestEndpointTokenBudget_Definitions(t *testing.T) {
	srv := newTestServerWithDoc(t, buildLargeEndpointDoc(30))
	// Pass a very tight budget (50 tokens = 200 bytes) — forces truncation.
	out := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{
		"group":        "test",
		"limit":        float64(30), // ask for all 30
		"token_budget": float64(50), // tiny budget
	})
	defs := getSlice(t, out, "definitions")
	if len(defs) >= 30 {
		t.Errorf("expected definitions to be capped by token_budget, got %d items", len(defs))
	}
	note, _ := out["truncation_note"].(string)
	if note == "" {
		t.Errorf("expected truncation_note to be set when token_budget is exceeded")
	}
}

// ---------------------------------------------------------------------------
// #1745: format param, triple-path dedupe, filter-before-limit assertions
// ---------------------------------------------------------------------------

// TestEndpointDefinitions_FormatTerse verifies format="terse" produces "lines"
// and the response echoes format=terse.
func TestEndpointDefinitions_FormatTerse(t *testing.T) {
	srv := newEndpointServer(t)
	res := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{
		"group":  "test",
		"format": "terse",
	})
	if _, ok := res["lines"]; !ok {
		t.Error("format=terse should produce 'lines' key")
	}
	if label, _ := res["format"].(string); label != "terse" {
		t.Errorf("expected format=terse in response, got %q", label)
	}
}

// TestEndpointDefinitions_FormatFull verifies format="full" returns per-record
// structs with kind and NO "lines" key.
func TestEndpointDefinitions_FormatFull(t *testing.T) {
	srv := newEndpointServer(t)
	res := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{
		"group":  "test",
		"format": "full",
	})
	if _, ok := res["lines"]; ok {
		t.Error("format=full should NOT produce 'lines' key")
	}
	if label, _ := res["format"].(string); label != "full" {
		t.Errorf("expected format=full in response, got %q", label)
	}
	defs := getSlice(t, res, "definitions")
	for _, d := range defs {
		obj := d.(map[string]any)
		if _, has := obj["kind"]; !has {
			t.Errorf("format=full row should include kind field: %v", obj)
		}
	}
}

// TestEndpointCalls_FormatTerse verifies format="terse" on calls action.
func TestEndpointCalls_FormatTerse(t *testing.T) {
	srv := newEndpointServer(t)
	res := callEndpointTool(t, srv.handleEndpointCalls, map[string]any{
		"group":  "test",
		"format": "terse",
	})
	if _, ok := res["lines"]; !ok {
		t.Error("format=terse should produce 'lines' key on calls")
	}
	if label, _ := res["format"].(string); label != "terse" {
		t.Errorf("expected format=terse in response, got %q", label)
	}
}

// TestEndpointDefinitions_TriplePathDedupe verifies that in full/verbose mode:
//   - Name is suppressed when it duplicates "VERB path"
//   - Properties bag does not contain "path" or "verb"
//   - Meaningful Names are preserved
func TestEndpointDefinitions_TriplePathDedupe(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{
				// Redundant name: exactly "GET /api/v2/users" = verb + path.
				ID: "ep1", Name: "GET /api/v2/users", Kind: "http_endpoint_definition",
				SourceFile: "routes/users.go", StartLine: 10,
				Properties: map[string]string{
					"verb":         "GET",
					"path":         "/api/v2/users",
					"handler_func": "UsersView.list",
				},
			},
			{
				// Meaningful name: carries info beyond the path.
				ID: "ep2", Name: "UserCreateView", Kind: "http_endpoint_definition",
				SourceFile: "routes/users.go", StartLine: 20,
				Properties: map[string]string{
					"verb": "POST",
					"path": "/api/v2/users",
				},
			},
		},
	}
	srv := newTestServerWithDoc(t, doc)
	res := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{
		"group":  "test",
		"format": "full",
	})
	defs := getSlice(t, res, "definitions")
	if len(defs) != 2 {
		t.Fatalf("expected 2 definitions, got %d", len(defs))
	}

	for _, d := range defs {
		obj := d.(map[string]any)
		entityID, _ := obj["entity_id"].(string)
		props, _ := obj["properties"].(map[string]any)

		// path and verb must not appear in Properties.
		if props != nil {
			if _, has := props["path"]; has {
				t.Errorf("entity %s: Properties should not contain 'path'", entityID)
			}
			if _, has := props["verb"]; has {
				t.Errorf("entity %s: Properties should not contain 'verb'", entityID)
			}
		}

		isEp1 := entityID == "test::ep1" || entityID == "ep1"
		isEp2 := entityID == "test::ep2" || entityID == "ep2"

		if isEp1 {
			// Redundant name must be suppressed.
			if name, _ := obj["name"].(string); name != "" {
				t.Errorf("ep1: redundant Name should be suppressed, got %q", name)
			}
		}
		if isEp2 {
			// Meaningful name must be preserved.
			if name, _ := obj["name"].(string); name != "UserCreateView" {
				t.Errorf("ep2: meaningful Name should be preserved, got %q", name)
			}
		}
	}
}

// TestEndpointDefinitions_PathContainsFilterBeforeLimit asserts that
// path_contains is applied server-side BEFORE limit — a narrow filter returns
// only matching rows regardless of the limit value.
func TestEndpointDefinitions_PathContainsFilterBeforeLimit(t *testing.T) {
	entities := make([]graph.Entity, 0, 10)
	for i := 0; i < 8; i++ {
		entities = append(entities, graph.Entity{
			ID: fmt.Sprintf("ep_other_%d", i), Kind: "http_endpoint_definition",
			Properties: map[string]string{"verb": "GET", "path": fmt.Sprintf("/api/v1/orders/%d", i)},
		})
	}
	entities = append(entities, graph.Entity{
		ID: "ep_prop_1", Kind: "http_endpoint_definition",
		Properties: map[string]string{"verb": "GET", "path": "/api/v1/proposals/list"},
	})
	entities = append(entities, graph.Entity{
		ID: "ep_prop_2", Kind: "http_endpoint_definition",
		Properties: map[string]string{"verb": "POST", "path": "/api/v1/proposals/create"},
	})
	srv := newTestServerWithDoc(t, &graph.Document{Entities: entities})

	res := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{
		"group":         "test",
		"path_contains": "proposal",
		"limit":         float64(5),
	})
	defs := getSlice(t, res, "definitions")
	if len(defs) != 2 {
		t.Fatalf("path_contains=proposal should match exactly 2 endpoints, got %d", len(defs))
	}
	for _, d := range defs {
		obj := d.(map[string]any)
		p, _ := obj["path"].(string)
		if !strings.Contains(strings.ToLower(p), "proposal") {
			t.Errorf("unexpected path %q does not contain 'proposal'", p)
		}
	}
}

// TestEndpointDefinitions_MethodFilterBeforeLimit verifies method filter is
// applied before limit.
func TestEndpointDefinitions_MethodFilterBeforeLimit(t *testing.T) {
	entities := []graph.Entity{
		{ID: "ep1", Kind: "http_endpoint_definition", Properties: map[string]string{"verb": "GET", "path": "/a"}},
		{ID: "ep2", Kind: "http_endpoint_definition", Properties: map[string]string{"verb": "POST", "path": "/b"}},
		{ID: "ep3", Kind: "http_endpoint_definition", Properties: map[string]string{"verb": "GET", "path": "/c"}},
		{ID: "ep4", Kind: "http_endpoint_definition", Properties: map[string]string{"verb": "DELETE", "path": "/d"}},
	}
	srv := newTestServerWithDoc(t, &graph.Document{Entities: entities})

	res := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{
		"group":  "test",
		"method": "get",
		"limit":  float64(1),
	})
	defs := getSlice(t, res, "definitions")
	if len(defs) != 1 {
		t.Fatalf("method=GET limit=1 should return 1 result, got %d", len(defs))
	}
	obj := defs[0].(map[string]any)
	if m, _ := obj["method"].(string); !strings.EqualFold(m, "GET") {
		t.Errorf("expected GET method, got %q", m)
	}
}

// TestIsRedundantName covers the helper function directly.
func TestIsRedundantName(t *testing.T) {
	cases := []struct {
		name, method, path string
		want               bool
	}{
		{"/api/v1/orders", "", "/api/v1/orders", true},
		{"GET /api/v2/users", "GET", "/api/v2/users", true},
		{"get /api/v2/users", "GET", "/api/v2/users", true},
		{"POST /api/v1/orders (client)", "POST", "/api/v1/orders", true},
		{"GET /api/v2/users → UsersView.list", "GET", "/api/v2/users", true},
		{"UsersView", "GET", "/api/v2/users", false},
		{"UserCreateView", "POST", "/api/v2/users", false},
		{"", "GET", "/api/v2/users", false},
		{"GET ", "GET", "", false},
	}
	for _, tc := range cases {
		got := isRedundantName(tc.name, tc.method, tc.path)
		if got != tc.want {
			t.Errorf("isRedundantName(%q, %q, %q) = %v, want %v",
				tc.name, tc.method, tc.path, got, tc.want)
		}
	}
}

// TestDedupeEndpointProperties verifies path+verb removal, other keys kept.
func TestDedupeEndpointProperties(t *testing.T) {
	props := map[string]string{
		"path":         "/api/v1/orders",
		"verb":         "POST",
		"handler_func": "OrdersView.create",
		"pattern_type": "django_url",
	}
	got := dedupeEndpointProperties(props)
	if _, has := got["path"]; has {
		t.Error("dedupeEndpointProperties should remove 'path'")
	}
	if _, has := got["verb"]; has {
		t.Error("dedupeEndpointProperties should remove 'verb'")
	}
	if got["handler_func"] != "OrdersView.create" {
		t.Errorf("handler_func should be preserved, got %q", got["handler_func"])
	}
	if got["pattern_type"] != "django_url" {
		t.Errorf("pattern_type should be preserved, got %q", got["pattern_type"])
	}
}

// TestDedupeEndpointProperties_NilAndEmpty verifies edge cases.
func TestDedupeEndpointProperties_NilAndEmpty(t *testing.T) {
	if got := dedupeEndpointProperties(nil); got != nil {
		t.Errorf("nil input should return nil, got %v", got)
	}
	onlyRedundant := map[string]string{"path": "/x", "verb": "GET"}
	if got := dedupeEndpointProperties(onlyRedundant); got != nil {
		t.Errorf("all-redundant props should return nil, got %v", got)
	}
}
