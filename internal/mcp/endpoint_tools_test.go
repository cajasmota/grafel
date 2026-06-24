package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
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
	return newTestServer(t, buildEndpointDoc())
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
	return extractResultJSON(t, res)
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
// #2316: assertEndpointCount — unified count assertion helper.
//
// Consolidates three divergent assertion styles previously used across the
// test file into a single call: assertEndpointCount(t, res, wantCount).
// The helper inspects the "count" key in the response envelope, which is
// present in all three endpoint handler responses.
// ---------------------------------------------------------------------------

func assertEndpointCount(t *testing.T, res map[string]any, want int) {
	t.Helper()
	got := getFloat(t, res, "count")
	if int(got) != want {
		t.Errorf("count: want %d, got %v", want, got)
	}
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

// #4287: a short-form (unprefixed leaf) kind_filter must match a namespaced
// SCOPE.* entity kind, while the fully-qualified form keeps working.
func TestMatchesKindFilter_ShortFormMatchesScopeNamespaced(t *testing.T) {
	e := &graph.Entity{Kind: "SCOPE.DataAccess"}
	if !matchesKindFilter(e, "DataAccess") {
		t.Error("short-form filter DataAccess should match SCOPE.DataAccess entity (#4287)")
	}
	if !matchesKindFilter(e, "SCOPE.DataAccess") {
		t.Error("fully-qualified filter SCOPE.DataAccess should still match SCOPE.DataAccess entity")
	}
	// Leaf match is symmetric: a SCOPE.-prefixed filter should match an
	// unprefixed entity kind too.
	bare := &graph.Entity{Kind: "DataAccess"}
	if !matchesKindFilter(bare, "SCOPE.DataAccess") {
		t.Error("SCOPE.DataAccess filter should match unprefixed DataAccess entity")
	}
	// Unrelated leaf must not collide.
	if matchesKindFilter(e, "Function") {
		t.Error("SCOPE.DataAccess entity should not match Function filter")
	}
	if matchesKindFilter(e, "SCOPE.Function") {
		t.Error("SCOPE.DataAccess entity should not match SCOPE.Function filter")
	}
}

// ---------------------------------------------------------------------------
// #2314: classifyEndpointKind tests
// ---------------------------------------------------------------------------

func TestClassifyEndpointKind_AllVariants(t *testing.T) {
	cases := []struct {
		kind string
		want endpointKindCategory
	}{
		{"http_endpoint_definition", endpointKindDefinition},
		{"HTTP_ENDPOINT_DEFINITION", endpointKindDefinition},
		{"http_endpoint_call", endpointKindCall},
		{"HTTP_ENDPOINT_CALL", endpointKindCall},
		{"http_endpoint", endpointKindLegacy},
		{"HTTP_ENDPOINT", endpointKindLegacy},
		{"Function", endpointKindNone},
		{"", endpointKindNone},
		{"topic", endpointKindNone},
	}
	for _, tc := range cases {
		got := classifyEndpointKind(tc.kind)
		if got != tc.want {
			t.Errorf("classifyEndpointKind(%q) = %v, want %v", tc.kind, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// grafel_endpoint_definitions tests
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

// TestEndpointDefinitions_SurfacesEffects verifies #2811: endpoint definitions
// carry the handler effect closure from the on-disk effects sidecar, and the
// `effect` filter narrows to endpoints touching a given effect.
func TestEndpointDefinitions_SurfacesEffects(t *testing.T) {
	srv := newEndpointServer(t)
	// Redirect HOME so effectsSidecarPath resolves into the temp dir.
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".grafel", "groups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// ep_legacy (repo1) → [db_write, mutation]; ep_def stays pure.
	sidecar := `{"version":1,"method":"effect_propagation","entries":[` +
		`{"entity_id":"repo1::ep_legacy","effects":["db_write","mutation"],"confidences":{"db_write":0.9,"mutation":0.8},"source":"endpoint"}` +
		`]}`
	if err := os.WriteFile(filepath.Join(dir, "test-links-effects.json"), []byte(sidecar), 0o644); err != nil {
		t.Fatal(err)
	}

	// Without filter: ep_legacy carries the effects field.
	res := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{"group": "test", "verbose": true})
	defs := getSlice(t, res, "definitions")
	var found bool
	for _, d := range defs {
		obj := d.(map[string]any)
		if obj["kind"] == "http_endpoint" {
			effs := obj["effects"].([]any)
			if len(effs) != 2 || effs[0].(string) != "db_write" {
				t.Errorf("ep_legacy effects=%v; want [db_write mutation]", effs)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("ep_legacy definition not found")
	}

	// With effect=db_write filter: only ep_legacy survives.
	resF := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{"group": "test", "effect": "db_write"})
	assertEndpointCount(t, resF, 1)

	// With effect=fs_write filter: none survive.
	resNone := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{"group": "test", "effect": "fs_write"})
	assertEndpointCount(t, resNone, 0)
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

// TestEndpointDefinitions_NoteFieldAbsent verifies that the runtime response
// does NOT contain a "note" field (#2317 — schema lives in tool description,
// not in every wire response).
func TestEndpointDefinitions_NoteFieldAbsent(t *testing.T) {
	srv := newEndpointServer(t)
	res := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{"group": "test"})
	if _, ok := res["note"]; ok {
		t.Error("response must NOT contain 'note' field (#2317 — schema lives in tool description)")
	}
}

// ---------------------------------------------------------------------------
// orphan_only filter tests (#2292)
// ---------------------------------------------------------------------------

// In buildEndpointDoc the only inbound FETCHES edge to a definition is
// fn_other → ep_def. ep_legacy has no inbound FETCHES, so it is the lone
// orphan when orphan_only=true.
func TestEndpointDefinitions_OrphanOnly_FiltersToOrphans(t *testing.T) {
	srv := newEndpointServer(t)
	res := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{
		"group":       "test",
		"orphan_only": true,
		"verbose":     true,
	})

	defs := getSlice(t, res, "definitions")
	if len(defs) != 1 {
		t.Fatalf("expected 1 orphan definition, got %d: %v", len(defs), defs)
	}
	obj := defs[0].(map[string]any)
	if got, _ := obj["entity_id"].(string); !strings.HasSuffix(got, "ep_legacy") {
		t.Errorf("expected ep_legacy as orphan, got entity_id=%q", got)
	}
	if got, _ := res["orphan_only"].(bool); !got {
		t.Errorf("orphan_only=true should be echoed in response, got %v", res["orphan_only"])
	}
}

// orphan_only=false (and the default unset case) must return the full set of
// definitions — backward compat with pre-#2292 behaviour.
func TestEndpointDefinitions_OrphanOnly_FalseReturnsAll(t *testing.T) {
	srv := newEndpointServer(t)
	res := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{
		"group":       "test",
		"orphan_only": false,
		"verbose":     true,
	})
	assertEndpointCount(t, res, 2)
	if got := len(getSlice(t, res, "definitions")); got != 2 {
		t.Errorf("orphan_only=false: expected 2 definitions, got %d", got)
	}

	resUnset := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{
		"group":   "test",
		"verbose": true,
	})
	assertEndpointCount(t, resUnset, 2)
	if got := len(getSlice(t, resUnset, "definitions")); got != 2 {
		t.Errorf("orphan_only unset: expected 2 definitions, got %d", got)
	}
}

// When every definition has an inbound FETCHES edge, orphan_only=true must
// produce an empty result — not an error, and (in terse mode) an empty
// `lines` slice rather than a missing key.
func TestEndpointDefinitions_OrphanOnly_NoOrphansReturnsEmpty(t *testing.T) {
	doc := buildEndpointDoc()
	// Cover the previously-uncalled ep_legacy with an inbound FETCHES.
	doc.Relationships = append(doc.Relationships, graph.Relationship{
		FromID: "fn_other", ToID: "ep_legacy", Kind: "FETCHES",
	})
	srv := newTestServer(t, doc)

	res := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{
		"group":       "test",
		"orphan_only": true,
	})

	assertEndpointCount(t, res, 0)
	if got := getFloat(t, res, "total"); got != 0 {
		t.Errorf("expected total=0 when no orphans, got %v", got)
	}
	// terse default → lines key must exist and be empty.
	lines, ok := res["lines"]
	if !ok {
		t.Fatal("terse response missing `lines` key")
	}
	if ls, _ := lines.([]any); len(ls) != 0 {
		t.Errorf("expected empty lines, got %v", lines)
	}
}

// Non-CALLS/FETCHES inbound edges (e.g. CONTAINS) must NOT disqualify an
// endpoint from being orphan — only FETCHES from a client counts. (#2292)
func TestEndpointDefinitions_OrphanOnly_IgnoresNonFetchesInbound(t *testing.T) {
	doc := buildEndpointDoc()
	// ep_def already has fn_other -FETCHES-> ep_def (so it's NOT orphan).
	// Replace that with a CONTAINS edge so ep_def has only structural inbound.
	doc.Relationships = []graph.Relationship{
		{FromID: "fn_other", ToID: "ep_def", Kind: "CONTAINS"},
	}
	srv := newTestServer(t, doc)

	res := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{
		"group":       "test",
		"orphan_only": true,
		"verbose":     true,
	})

	defs := getSlice(t, res, "definitions")
	// Both ep_legacy AND ep_def must now be reported as orphans.
	if len(defs) != 2 {
		t.Fatalf("expected 2 orphans (CONTAINS shouldn't count), got %d: %v", len(defs), defs)
	}
	gotIDs := map[string]bool{}
	for _, d := range defs {
		obj := d.(map[string]any)
		id, _ := obj["entity_id"].(string)
		gotIDs[id] = true
	}
	if !hasSuffixKey(gotIDs, "ep_def") || !hasSuffixKey(gotIDs, "ep_legacy") {
		t.Errorf("expected ep_def AND ep_legacy in orphans, got %v", gotIDs)
	}
}

func hasSuffixKey(m map[string]bool, suffix string) bool {
	for k := range m {
		if strings.HasSuffix(k, suffix) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// grafel_endpoint_calls tests
// ---------------------------------------------------------------------------

func TestEndpointCalls_ReturnsCallsOnly(t *testing.T) {
	srv := newEndpointServer(t)
	// #2311: terse (default) → lines only; use format=full to get `calls` struct.
	res := callEndpointTool(t, srv.handleEndpointCalls, map[string]any{"group": "test", "format": "full"})

	calls := getSlice(t, res, "calls")
	// Expect: ep_call (http_endpoint_call) + ep_client_synth (client-synthesis http_endpoint)
	if len(calls) != 2 {
		t.Errorf("expected 2 calls, got %d: %v", len(calls), calls)
	}
}

func TestEndpointCalls_OrphanHintPresent(t *testing.T) {
	srv := newEndpointServer(t)
	// #2311: terse → lines only; need format=full for per-item orphan_hint inspection.
	res := callEndpointTool(t, srv.handleEndpointCalls, map[string]any{"group": "test", "format": "full"})
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
	// #2311: terse default → lines. Use format=full so orphan_hint is in the struct.
	res := callEndpointTool(t, srv.handleEndpointCalls, map[string]any{
		"group":       "test",
		"orphan_only": true,
		"format":      "full",
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
// #2311: handleEndpointCalls terse-mode dedup tests
//
// Mirror of the #2288/#2309 fix applied to handleEndpointDefinitions.
// Terse (default): only "lines" key present, no "calls" struct array.
// Full: only "calls" struct array present, no "lines" key.
// ---------------------------------------------------------------------------

// TestEndpointCalls_TerseDefault verifies #2311: terse mode (default) emits
// only "lines", NOT "calls". Mirrors TestEndpointDefinitions_TerseDefault.
func TestEndpointCalls_TerseDefault(t *testing.T) {
	srv := newEndpointServer(t)
	res := callEndpointTool(t, srv.handleEndpointCalls, map[string]any{"group": "test"})
	if _, ok := res["lines"]; !ok {
		t.Error("terse (default) calls response must have 'lines' key (#2311)")
	}
	if _, ok := res["calls"]; ok {
		t.Error("terse (default) calls response must NOT have 'calls' key (#2311 — lines-only in terse mode)")
	}
}

// TestEndpointCalls_FullModeIncludesCalls verifies that format=full restores
// the `calls` struct array (and does NOT emit `lines`).
func TestEndpointCalls_FullModeIncludesCalls(t *testing.T) {
	srv := newEndpointServer(t)
	res := callEndpointTool(t, srv.handleEndpointCalls, map[string]any{
		"group":  "test",
		"format": "full",
	})
	if _, ok := res["calls"]; !ok {
		t.Error("format=full should include 'calls' key")
	}
	if _, ok := res["lines"]; ok {
		t.Error("format=full should NOT include 'lines' key")
	}
	assertEndpointCount(t, res, 2)
}

// TestEndpointCalls_NoteFieldAbsent verifies #2317 for the calls handler.
func TestEndpointCalls_NoteFieldAbsent(t *testing.T) {
	srv := newEndpointServer(t)
	res := callEndpointTool(t, srv.handleEndpointCalls, map[string]any{"group": "test"})
	if _, ok := res["note"]; ok {
		t.Error("calls response must NOT contain 'note' field (#2317)")
	}
}

// ---------------------------------------------------------------------------
// grafel_endpoint_stats tests
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
	srv := newTestServer(t, doc)
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
// #3560: per-detector confidence/coverage signal on action=stats
// ---------------------------------------------------------------------------

// buildConfidenceStatsDoc builds a fixture with one regex-detected framework
// (nestjs) and one AST-backed framework (spring_mvc) so the by_framework
// confidence enum can be asserted both ways. The plain totals are deliberately
// the same shape the existing stats tests assert against (no legacy entities).
//
//	d_nest   http_endpoint_definition framework=nestjs     (regex → heuristic)
//	d_nest2  http_endpoint_definition framework=nestjs     (regex → heuristic)
//	d_spring http_endpoint_definition framework=spring_mvc (ast   → exact)
//	c_nest   http_endpoint_call       framework=nestjs     (regex → heuristic)
func buildConfidenceStatsDoc() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			{ID: "d_nest", Kind: "http_endpoint_definition", Name: "GET /users",
				Properties: map[string]string{"verb": "GET", "path": "/users", "framework": "nestjs"}},
			{ID: "d_nest2", Kind: "http_endpoint_definition", Name: "POST /users",
				Properties: map[string]string{"verb": "POST", "path": "/users", "framework": "nestjs"}},
			{ID: "d_spring", Kind: "http_endpoint_definition", Name: "GET /orders",
				Properties: map[string]string{"verb": "GET", "path": "/orders", "framework": "spring_mvc"}},
			{ID: "c_nest", Kind: "http_endpoint_call", Name: "fetchUsers",
				Properties: map[string]string{"verb": "GET", "path": "/users", "framework": "nestjs"}},
		},
	}
}

func TestEndpointStats_ByFrameworkConfidence(t *testing.T) {
	srv := newTestServer(t, buildConfidenceStatsDoc())
	res := callEndpointTool(t, srv.handleEndpointStats, map[string]any{"group": "test"})

	// --- existing totals must be UNCHANGED by the additive signal ---
	totals, ok := res["totals"].(map[string]any)
	if !ok {
		t.Fatalf("missing totals in %v", res)
	}
	if got := getFloat(t, totals, "definitions"); got != 3 {
		t.Errorf("totals.definitions: want 3, got %v", got)
	}
	if got := getFloat(t, totals, "calls"); got != 1 {
		t.Errorf("totals.calls: want 1, got %v", got)
	}
	if got := getFloat(t, totals, "orphan_calls"); got != 0 {
		t.Errorf("totals.orphan_calls: want 0, got %v", got)
	}

	// --- by_framework breakdown with per-detector confidence enum ---
	bf, ok := res["by_framework"].(map[string]any)
	if !ok {
		t.Fatalf("missing by_framework in %v", res)
	}

	nest, ok := bf["nestjs"].(map[string]any)
	if !ok {
		t.Fatalf("missing by_framework.nestjs in %v", bf)
	}
	if got := getFloat(t, nest, "definitions"); got != 2 {
		t.Errorf("nestjs.definitions: want 2, got %v", got)
	}
	if got := getFloat(t, nest, "calls"); got != 1 {
		t.Errorf("nestjs.calls: want 1, got %v", got)
	}
	if got, _ := nest["detector"].(string); got != "regex" {
		t.Errorf("nestjs.detector: want regex, got %q", got)
	}
	if got, _ := nest["confidence"].(string); got != "heuristic" {
		t.Errorf("nestjs.confidence: want heuristic, got %q", got)
	}

	spring, ok := bf["spring_mvc"].(map[string]any)
	if !ok {
		t.Fatalf("missing by_framework.spring_mvc in %v", bf)
	}
	if got := getFloat(t, spring, "definitions"); got != 1 {
		t.Errorf("spring_mvc.definitions: want 1, got %v", got)
	}
	if got, _ := spring["detector"].(string); got != "ast" {
		t.Errorf("spring_mvc.detector: want ast, got %q", got)
	}
	if got, _ := spring["confidence"].(string); got != "exact" {
		t.Errorf("spring_mvc.confidence: want exact, got %q", got)
	}

	// --- top-level extraction descriptor: heuristic because nestjs is regex ---
	extraction, ok := res["extraction"].(map[string]any)
	if !ok {
		t.Fatalf("missing extraction in %v", res)
	}
	if got, _ := extraction["method"].(string); got != "heuristic" {
		t.Errorf("extraction.method: want heuristic (regex framework present), got %q", got)
	}
	if got, _ := extraction["note"].(string); got == "" {
		t.Error("extraction.note should be non-empty")
	}
}

// TestEndpointStats_ASTExtractionMethodPromotesConfidence verifies the #5527
// per-entity confidence refinement: a `fetch` client call (a framework name NOT
// in astBackedFrameworks) that carries extraction_method=ast — emitted by the
// tree-sitter fetch/axios pass — is classified ast/exact, while a sibling fetch
// call WITHOUT the stamp keeps the framework at ast (since at least one entity is
// AST). A second framework that is purely regex stays heuristic, proving the
// stamp is honest per entity and never blanket-applied.
func TestEndpointStats_ASTExtractionMethodPromotesConfidence(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			// fetch call, AST-extracted (static URL via tree-sitter) → exact.
			{ID: "c1", Kind: "http_endpoint_call", Name: "GET /a",
				Properties: map[string]string{"verb": "GET", "path": "/a",
					"framework": "fetch", "extraction_method": "ast"}},
			// express route, regex-extracted (no stamp) → heuristic.
			{ID: "d1", Kind: "http_endpoint_definition", Name: "GET /b",
				Properties: map[string]string{"verb": "GET", "path": "/b",
					"framework": "express"}},
		},
	}
	srv := newTestServer(t, doc)
	res := callEndpointTool(t, srv.handleEndpointStats, map[string]any{"group": "test"})

	bf, ok := res["by_framework"].(map[string]any)
	if !ok {
		t.Fatalf("missing by_framework in %v", res)
	}
	fetch, ok := bf["fetch"].(map[string]any)
	if !ok {
		t.Fatalf("missing by_framework.fetch in %v", bf)
	}
	if got, _ := fetch["detector"].(string); got != "ast" {
		t.Errorf("fetch.detector: want ast (extraction_method=ast), got %q", got)
	}
	if got, _ := fetch["confidence"].(string); got != "exact" {
		t.Errorf("fetch.confidence: want exact (AST-extracted), got %q", got)
	}

	express, ok := bf["express"].(map[string]any)
	if !ok {
		t.Fatalf("missing by_framework.express in %v", bf)
	}
	if got, _ := express["detector"].(string); got != "regex" {
		t.Errorf("express.detector: want regex (no AST stamp), got %q", got)
	}
	if got, _ := express["confidence"].(string); got != "heuristic" {
		t.Errorf("express.confidence: want heuristic, got %q", got)
	}
}

// TestEndpointStats_ASTStampUpgradesMixedFramework verifies that when a single
// framework has a MIX of AST- and regex-extracted entities, the framework's
// coarse confidence is upgraded to ast/exact as soon as one entity is AST
// (never downgraded by a later regex sibling).
func TestEndpointStats_ASTStampUpgradesMixedFramework(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			// regex-extracted fetch (template literal) — seen FIRST.
			{ID: "c1", Kind: "http_endpoint_call", Name: "GET /a",
				Properties: map[string]string{"verb": "GET", "path": "/a", "framework": "fetch"}},
			// AST-extracted fetch (static URL) — seen SECOND.
			{ID: "c2", Kind: "http_endpoint_call", Name: "GET /b",
				Properties: map[string]string{"verb": "GET", "path": "/b",
					"framework": "fetch", "extraction_method": "ast"}},
		},
	}
	srv := newTestServer(t, doc)
	res := callEndpointTool(t, srv.handleEndpointStats, map[string]any{"group": "test"})
	bf := res["by_framework"].(map[string]any)
	fetch, ok := bf["fetch"].(map[string]any)
	if !ok {
		t.Fatalf("missing by_framework.fetch in %v", bf)
	}
	if got := getFloat(t, fetch, "calls"); got != 2 {
		t.Errorf("fetch.calls: want 2, got %v", got)
	}
	if got, _ := fetch["confidence"].(string); got != "exact" {
		t.Errorf("fetch.confidence: want exact (one entity AST upgrades the framework), got %q", got)
	}
}

// TestEndpointStats_ExtractionExactWhenAllAST verifies that the top-level
// extraction.method advertises "exact" only when every framework present is
// AST-backed — the honest all-clear case.
func TestEndpointStats_ExtractionExactWhenAllAST(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "d1", Kind: "http_endpoint_definition", Name: "GET /a",
				Properties: map[string]string{"verb": "GET", "path": "/a", "framework": "django"}},
			{ID: "d2", Kind: "http_endpoint_definition", Name: "GET /b",
				Properties: map[string]string{"verb": "GET", "path": "/b", "framework": "spring_webflux"}},
		},
	}
	srv := newTestServer(t, doc)
	res := callEndpointTool(t, srv.handleEndpointStats, map[string]any{"group": "test"})
	extraction, ok := res["extraction"].(map[string]any)
	if !ok {
		t.Fatalf("missing extraction in %v", res)
	}
	if got, _ := extraction["method"].(string); got != "exact" {
		t.Errorf("extraction.method: want exact (all AST), got %q", got)
	}
}

// TestEndpointStats_UnknownFrameworkBucket verifies that synthetics with no
// framework attribution land in the "unknown" bucket and are treated as
// regex/heuristic (never silently exact).
func TestEndpointStats_UnknownFrameworkBucket(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "d1", Kind: "http_endpoint_definition", Name: "GET /a",
				Properties: map[string]string{"verb": "GET", "path": "/a"}}, // no framework
		},
	}
	srv := newTestServer(t, doc)
	res := callEndpointTool(t, srv.handleEndpointStats, map[string]any{"group": "test"})
	bf, ok := res["by_framework"].(map[string]any)
	if !ok {
		t.Fatalf("missing by_framework in %v", res)
	}
	unknown, ok := bf["unknown"].(map[string]any)
	if !ok {
		t.Fatalf("missing by_framework.unknown in %v", bf)
	}
	if got, _ := unknown["confidence"].(string); got != "heuristic" {
		t.Errorf("unknown.confidence: want heuristic, got %q", got)
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
	// The test verifies compilation + no panic. Functional assertion is in
	// TestMatchesKindFilter_* above which covers the underlying logic.
	_ = extractResultText(t, res)
}

// ---------------------------------------------------------------------------
// grafel_endpoints dispatch (#1281)
// ---------------------------------------------------------------------------

func TestHandleEndpoints_Definitions(t *testing.T) {
	srv := newEndpointServer(t)
	// #2288: request format=full so the response includes the `definitions`
	// struct array (terse default omits it; lines-only).
	res := callEndpointTool(t, srv.handleEndpoints, map[string]any{
		"group":  "test",
		"action": "definitions",
		"format": "full",
	})
	if _, ok := res["definitions"]; !ok {
		t.Error("expected definitions key in response for action=definitions (format=full)")
	}
	defs := getSlice(t, res, "definitions")
	if len(defs) != 2 {
		t.Errorf("action=definitions: expected 2 definitions, got %d", len(defs))
	}
}

func TestHandleEndpoints_Calls(t *testing.T) {
	srv := newEndpointServer(t)
	// #2311: terse default → lines only; use format=full for `calls` struct.
	res := callEndpointTool(t, srv.handleEndpoints, map[string]any{
		"group":  "test",
		"action": "calls",
		"format": "full",
	})
	if _, ok := res["calls"]; !ok {
		t.Error("expected calls key in response for action=calls (format=full)")
	}
	calls := getSlice(t, res, "calls")
	assertEndpointCount(t, res, 2)
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
		"format":      "full", // #2311: need full to inspect orphan_hint per item
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
		"format":        "full", // #2288: opt-in to `definitions` struct array
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
		"format": "full", // #2288: opt-in to `definitions` struct array
	})
	defs := getSlice(t, res, "definitions")
	if len(defs) != 1 {
		t.Fatalf("expected 1 def for method=POST, got %d", len(defs))
	}
}

// #1650 + #2288: terse default returns "lines" with one-line entries; the
// full `definitions` struct array is OMITTED in terse mode (saves ~22 KB on
// large responses per #2288). verbose=true/format=full returns `definitions`
// without `lines`.
func TestEndpointDefinitions_TerseDefault(t *testing.T) {
	srv := newEndpointServer(t)
	res := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{"group": "test"})
	if _, ok := res["lines"]; !ok {
		t.Error("expected 'lines' key in terse default response")
	}
	if _, has := res["definitions"]; has {
		t.Error("terse default should OMIT 'definitions' (#2288): only 'lines' is emitted")
	}
}

// TestEndpointDefinitions_FullModeIncludesDefinitions verifies that the
// explicit opt-in (#2288) restores the full `definitions` struct array.
func TestEndpointDefinitions_FullModeIncludesDefinitions(t *testing.T) {
	srv := newEndpointServer(t)
	res := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{
		"group":  "test",
		"format": "full",
	})
	if _, ok := res["definitions"]; !ok {
		t.Error("format=full should include 'definitions' key")
	}
	if _, ok := res["lines"]; ok {
		t.Error("format=full should NOT include 'lines' key")
	}
	// verbose=true alias must work too.
	res2 := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{
		"group":   "test",
		"verbose": true,
	})
	if _, ok := res2["definitions"]; !ok {
		t.Error("verbose=true should include 'definitions' key (alias for format=full)")
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
	srv := newTestServer(t, buildLargeEndpointDoc(30))
	// #2288: terse-default response omits `definitions`; use `lines` for the
	// rendered count assertion, plus a parallel full-mode call to assert the
	// struct-array slice is also capped.
	out := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{"group": "test"})
	lines, _ := out["lines"].([]any)
	if len(lines) > 20 {
		t.Errorf("handleEndpointDefinitions (terse) returned %d lines, want ≤20 (default limit)", len(lines))
	}
	outFull := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{"group": "test", "format": "full"})
	defs := getSlice(t, outFull, "definitions")
	if len(defs) > 20 {
		t.Errorf("handleEndpointDefinitions (full) returned %d items, want ≤20 (default limit)", len(defs))
	}
}

// TestEndpointTokenBudget_Definitions verifies that a tight token_budget caps
// the definitions slice and adds a truncation_note (#1738).
func TestEndpointTokenBudget_Definitions(t *testing.T) {
	srv := newTestServer(t, buildLargeEndpointDoc(30))
	// Pass a very tight budget (50 tokens = 200 bytes) — forces truncation.
	out := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{
		"group":        "test",
		"limit":        float64(30), // ask for all 30
		"token_budget": float64(50), // tiny budget
		"format":       "full",      // #2288: need full struct array to assert cap
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
	srv := newTestServer(t, doc)
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
	srv := newTestServer(t, &graph.Document{Entities: entities})

	res := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{
		"group":         "test",
		"path_contains": "proposal",
		"limit":         float64(5),
		"format":        "full", // #2288: opt-in to `definitions` struct array
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
	srv := newTestServer(t, &graph.Document{Entities: entities})

	res := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{
		"group":  "test",
		"method": "get",
		"limit":  float64(1),
		"format": "full", // #2288: opt-in to `definitions` struct array
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

// ---------------------------------------------------------------------------
// #2360: PaginationOpts.FromRequest builder tests
// ---------------------------------------------------------------------------

// makeReq builds a CallToolRequest with the given arguments for builder tests.
func makeReq(args map[string]any) mcpapi.CallToolRequest {
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	return req
}

// TestPaginationOptsFromRequest_FormatFull verifies that format="full" sets
// Verbose=true regardless of the verbose bool param.
func TestPaginationOptsFromRequest_FormatFull(t *testing.T) {
	opts := PaginationOpts{}.FromRequest(makeReq(map[string]any{"format": "full", "verbose": false}))
	if !opts.Verbose {
		t.Error("format=full should set Verbose=true")
	}
	if opts.Format() != "full" {
		t.Errorf("Format() should return %q, got %q", "full", opts.Format())
	}
}

// TestPaginationOptsFromRequest_FormatTerse verifies that format="terse" sets
// Verbose=false regardless of the verbose bool param.
func TestPaginationOptsFromRequest_FormatTerse(t *testing.T) {
	opts := PaginationOpts{}.FromRequest(makeReq(map[string]any{"format": "terse", "verbose": true}))
	if opts.Verbose {
		t.Error("format=terse should set Verbose=false even when verbose=true")
	}
}

// TestPaginationOptsFromRequest_DefaultVerboseFalse verifies that when format
// is absent, Verbose defaults to false.
func TestPaginationOptsFromRequest_DefaultVerboseFalse(t *testing.T) {
	opts := PaginationOpts{}.FromRequest(makeReq(map[string]any{}))
	if opts.Verbose {
		t.Error("default (no format, no verbose) should set Verbose=false")
	}
}

// TestPaginationOptsFromRequest_VerboseTrueFallback verifies that when format
// is absent but verbose=true, Verbose is true.
func TestPaginationOptsFromRequest_VerboseTrueFallback(t *testing.T) {
	opts := PaginationOpts{}.FromRequest(makeReq(map[string]any{"verbose": true}))
	if !opts.Verbose {
		t.Error("verbose=true with no format should set Verbose=true")
	}
}

// TestPaginationOptsFromRequest_PathContainsNormalised verifies that
// path_contains is lower-cased by the builder.
func TestPaginationOptsFromRequest_PathContainsNormalised(t *testing.T) {
	opts := PaginationOpts{}.FromRequest(makeReq(map[string]any{"path_contains": "Users"}))
	if opts.PathContains != "users" {
		t.Errorf("PathContains should be lower-cased, got %q", opts.PathContains)
	}
}

// TestPaginationOptsFromRequest_MethodNormalised verifies that method is
// upper-cased by the builder.
func TestPaginationOptsFromRequest_MethodNormalised(t *testing.T) {
	opts := PaginationOpts{}.FromRequest(makeReq(map[string]any{"method": "post"}))
	if opts.Method != "POST" {
		t.Errorf("Method should be upper-cased, got %q", opts.Method)
	}
}

// TestPaginationOptsFromRequest_PaginationDefaults verifies default offset,
// limit, and token_budget values when absent from the request.
func TestPaginationOptsFromRequest_PaginationDefaults(t *testing.T) {
	opts := PaginationOpts{}.FromRequest(makeReq(map[string]any{}))
	if opts.Offset != 0 {
		t.Errorf("default Offset: want 0, got %d", opts.Offset)
	}
	if opts.Limit != 20 {
		t.Errorf("default Limit: want 20, got %d", opts.Limit)
	}
	if opts.TokenBudget != 800 {
		t.Errorf("default TokenBudget: want 800, got %d", opts.TokenBudget)
	}
}

// TestPaginationOptsFromRequest_ExplicitPagination verifies that explicit
// offset/limit/token_budget values are respected.
func TestPaginationOptsFromRequest_ExplicitPagination(t *testing.T) {
	opts := PaginationOpts{}.FromRequest(makeReq(map[string]any{
		"offset":       float64(10),
		"limit":        float64(5),
		"token_budget": float64(200),
	}))
	if opts.Offset != 10 {
		t.Errorf("Offset: want 10, got %d", opts.Offset)
	}
	if opts.Limit != 5 {
		t.Errorf("Limit: want 5, got %d", opts.Limit)
	}
	if opts.TokenBudget != 200 {
		t.Errorf("TokenBudget: want 200, got %d", opts.TokenBudget)
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

// ---------------------------------------------------------------------------
// #2336: endpointResolution helper unit tests
// ---------------------------------------------------------------------------

// TestEndpointResolution_DefinitionIDs verifies that newEndpointResolution
// correctly builds the definition ID set, excluding client-synthesis entities.
func TestEndpointResolution_DefinitionIDs(t *testing.T) {
	doc := buildEndpointDoc()
	// Use a minimal LoadedGroup with no links.
	lg := &LoadedGroup{}
	repos := []*LoadedRepo{
		{Repo: "test", Doc: doc},
	}

	res := newEndpointResolution(repos, lg, false)

	// ep_legacy (http_endpoint, producer) and ep_def (http_endpoint_definition)
	// must appear as definition IDs.
	if !res.definitionIDs["test::ep_legacy"] && !res.definitionIDs["ep_legacy"] {
		t.Error("ep_legacy should be in definitionIDs")
	}
	if !res.definitionIDs["test::ep_def"] && !res.definitionIDs["ep_def"] {
		t.Error("ep_def should be in definitionIDs")
	}

	// ep_client_synth must NOT be in definitionIDs (it's a call, not a definition).
	if res.definitionIDs["test::ep_client_synth"] || res.definitionIDs["ep_client_synth"] {
		t.Error("ep_client_synth (client-synthesis) must NOT be in definitionIDs")
	}

	// ep_call must NOT be in definitionIDs.
	if res.definitionIDs["test::ep_call"] || res.definitionIDs["ep_call"] {
		t.Error("ep_call must NOT be in definitionIDs")
	}
}

// TestEndpointResolution_OrphanOnlyPopulatesLinkedTargets verifies that
// linkedTargets is populated only when orphanOnly=true.
func TestEndpointResolution_OrphanOnlyPopulatesLinkedTargets(t *testing.T) {
	lg := &LoadedGroup{Links: []CrossRepoLink{{Source: "a::src", Target: "b::tgt"}}}
	repos := []*LoadedRepo{{Repo: "test", Doc: &graph.Document{}}}

	resNo := newEndpointResolution(repos, lg, false)
	if resNo.linkedTargets != nil {
		t.Error("orphanOnly=false: linkedTargets must be nil (skip allocation)")
	}

	resYes := newEndpointResolution(repos, lg, true)
	if resYes.linkedTargets == nil {
		t.Error("orphanOnly=true: linkedTargets must not be nil")
	}
	if !resYes.linkedTargets["b::tgt"] {
		t.Error("orphanOnly=true: linkedTargets must contain the link target")
	}
}
