package mcp

// inspect_trio_2640_2641_2642_test.go — tests for three inspect improvements.
//
// #2640: calls[] filters unresolved edges by default; include_unresolved=true
//        shows all with annotation.
// #2641: called_by always present (empty array when no callers).
// #2642: metadata block exposes index-staleness fields (indexed_at/age_seconds).
//        #2780: indexed_ref/indexed_sha removed — owned by whoami only.

import (
	"context"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// callInspectWithArgs calls handleGetNode with arbitrary extra arguments.
func callInspectWithArgs(t *testing.T, srv *Server, args map[string]any) map[string]any {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	res, err := srv.handleGetNode(context.Background(), req)
	if err != nil {
		t.Fatalf("handleGetNode error: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.Content)
	}
	return extractResultJSON(t, res)
}

// makeDocWithMixedCalls builds a Document where entity "caller" has 5 outbound
// CALLS edges: 3 fully resolved (target_path set via entity SourceFile) and
// 2 unresolved (no entity in the graph for those IDs, so target_path == "").
func makeDocWithMixedCalls() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			{ID: "caller", Name: "Caller", Kind: "SCOPE.Operation", SourceFile: "caller.go", StartLine: 1},
			// Three resolved callees
			{ID: "callee1", Name: "CalleeA", Kind: "SCOPE.Operation", SourceFile: "a.go", StartLine: 10},
			{ID: "callee2", Name: "CalleeB", Kind: "SCOPE.Operation", SourceFile: "b.go", StartLine: 20},
			{ID: "callee3", Name: "CalleeC", Kind: "SCOPE.Operation", SourceFile: "c.go", StartLine: 30},
			// No entities for "ghost1" / "ghost2" — those are unresolved targets.
		},
		Relationships: []graph.Relationship{
			graph.Relationship{ID: "r1", FromID: "caller", ToID: "callee1", Kind: "CALLS"}.WithProperties(map[string]string{"line": "5"}),
			graph.Relationship{ID: "r2", FromID: "caller", ToID: "callee2", Kind: "CALLS"}.WithProperties(map[string]string{"line": "6"}),
			graph.Relationship{ID: "r3", FromID: "caller", ToID: "callee3", Kind: "CALLS"}.WithProperties(map[string]string{"line": "7"}),
			// ghost1 / ghost2 have no entity → target_path will be empty → unresolved
			graph.Relationship{ID: "r4", FromID: "caller", ToID: "ghost1", Kind: "CALLS"}.WithProperties(map[string]string{"line": "8"}),
			graph.Relationship{ID: "r5", FromID: "caller", ToID: "ghost2", Kind: "CALLS"}.WithProperties(map[string]string{"line": "9"}),
		},
	}
}

// TestInspect_FiltersUnresolvedCallsByDefault verifies that with the default
// include_unresolved=false param, only the 3 resolved calls are returned.
func TestInspect_FiltersUnresolvedCallsByDefault(t *testing.T) {
	doc := makeDocWithMixedCalls()
	srv := newTestServer(t, doc)

	out := callInspectWithArgs(t, srv, map[string]any{
		"group":     "test",
		"entity_id": "caller",
		// include_unresolved omitted → defaults to false
	})

	calls, ok := out["calls"]
	if !ok {
		t.Fatalf("missing 'calls' key; got keys: %v", mapKeys(out))
	}
	arr, ok := calls.([]any)
	if !ok {
		t.Fatalf("'calls' is %T, want []any", calls)
	}
	if len(arr) != 3 {
		t.Errorf("expected 3 resolved calls (unresolved filtered), got %d: %v", len(arr), arr)
	}
	// None should have unresolved=true
	for i, raw := range arr {
		m := raw.(map[string]any)
		if v, ok := m["unresolved"]; ok && v == true {
			t.Errorf("calls[%d] has unresolved=true but should have been filtered out", i)
		}
	}
}

// TestInspect_IncludesUnresolved_WhenRequested verifies that with
// include_unresolved=true, all 5 edges are returned and the 2 unresolved ones
// carry an "unresolved: true" annotation.
func TestInspect_IncludesUnresolved_WhenRequested(t *testing.T) {
	doc := makeDocWithMixedCalls()
	srv := newTestServer(t, doc)

	out := callInspectWithArgs(t, srv, map[string]any{
		"group":              "test",
		"entity_id":          "caller",
		"include_unresolved": true,
	})

	calls, ok := out["calls"]
	if !ok {
		t.Fatalf("missing 'calls' key; got keys: %v", mapKeys(out))
	}
	arr, ok := calls.([]any)
	if !ok {
		t.Fatalf("'calls' is %T, want []any", calls)
	}
	if len(arr) != 5 {
		t.Errorf("expected 5 calls (3 resolved + 2 unresolved), got %d", len(arr))
	}

	unresolvedCount := 0
	for _, raw := range arr {
		m := raw.(map[string]any)
		if v, ok := m["unresolved"]; ok {
			if b, ok := v.(bool); ok && b {
				unresolvedCount++
			}
		}
	}
	if unresolvedCount != 2 {
		t.Errorf("expected 2 entries with unresolved=true, got %d", unresolvedCount)
	}
}

// TestInspect_AlwaysIncludesCalledByKey verifies that an entity with no
// inbound CALLS edges still returns called_by: [] (not missing the key).
func TestInspect_AlwaysIncludesCalledByKey(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "lonely", Name: "LonelyFunc", Kind: "SCOPE.Operation", SourceFile: "lonely.go", StartLine: 1},
		},
		Relationships: []graph.Relationship{}, // no callers
	}
	srv := newTestServer(t, doc)

	out := callInspect(t, srv, "lonely")

	// called_by must be present even though there are zero callers.
	raw, ok := out["called_by"]
	if !ok {
		t.Fatalf("called_by key is missing; got keys: %v", mapKeys(out))
	}
	arr, ok := raw.([]any)
	if !ok {
		t.Fatalf("called_by is %T, want []any", raw)
	}
	if len(arr) != 0 {
		t.Errorf("expected empty called_by, got %d entries", len(arr))
	}
}

// TestInspect_IncludesMetadata verifies that the metadata block is present and
// contains the expected fields. When the graph has no GeneratedAt set, the
// indexed_at field is empty but the key is still present.
func TestInspect_IncludesMetadata(t *testing.T) {
	indexedAt := time.Date(2026, 5, 27, 7, 14, 0, 0, time.UTC)
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "e1", Name: "SomeFunc", Kind: "SCOPE.Operation", SourceFile: "x.go", StartLine: 1},
		},
		IndexedRef:  "develop",
		IndexedSHA:  "d9eb1bb801ba",
		GeneratedAt: indexedAt,
	}
	srv := newTestServer(t, doc)

	out := callInspect(t, srv, "e1")

	rawMeta, ok := out["metadata"]
	if !ok {
		t.Fatalf("metadata key is missing; got keys: %v", mapKeys(out))
	}
	meta, ok := rawMeta.(map[string]any)
	if !ok {
		t.Fatalf("metadata is %T, want map[string]any", rawMeta)
	}

	// #2780: indexed_ref and indexed_sha are session-stable provenance fields
	// owned exclusively by grafel_whoami. inspect's metadata block carries
	// only the staleness signal (indexed_at/age_seconds), so these keys must NOT
	// be present here — see TestNoSessionMetaInNonWhoamiHandlers.
	if _, ok := meta["indexed_ref"]; ok {
		t.Error("metadata.indexed_ref must not be present on inspect (#2780); it belongs to whoami")
	}
	if _, ok := meta["indexed_sha"]; ok {
		t.Error("metadata.indexed_sha must not be present on inspect (#2780); it belongs to whoami")
	}

	// indexed_at must be a non-empty RFC3339 string.
	at, _ := meta["indexed_at"].(string)
	if at == "" {
		t.Error("metadata.indexed_at is empty; expected RFC3339 timestamp")
	}

	// age_seconds must be present (any numeric type after JSON round-trip).
	if _, ok := meta["age_seconds"]; !ok {
		t.Error("metadata.age_seconds key missing")
	}
}
