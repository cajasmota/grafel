package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestDeferredPayload_ElapsedMSExactlyOnce verifies the on-the-wire bytes
// from the deferred single-marshal path carry elapsed_ms exactly once and
// parse as valid JSON. This is the core invariant of #2287.
func TestDeferredPayload_ElapsedMSExactlyOnce(t *testing.T) {
	t.Setenv("MCP_WIRE_FORMAT", "json")

	cases := []struct {
		name string
		v    any
	}{
		{
			name: "map_with_items",
			v: map[string]any{
				"results": []any{
					map[string]any{"id": "a", "name": "Alpha"},
					map[string]any{"id": "b", "name": "Beta"},
				},
				"count": 2,
			},
		},
		{
			name: "map_no_items",
			v: map[string]any{
				"id":   "x",
				"name": "Solo",
			},
		},
		{
			name: "any_array",
			v: []any{
				map[string]any{"id": "a"},
				map[string]any{"id": "b"},
			},
		},
		{
			name: "typed_struct_slice",
			v: []map[string]any{
				{"id": "a"},
				{"id": "b"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text, err := finalizeDeferred(tc.v, 42, nil)
			if err != nil {
				t.Fatalf("finalizeDeferred: %v", err)
			}

			// elapsed_ms must appear exactly once.
			if c := strings.Count(text, "\"elapsed_ms\""); c != 1 {
				t.Errorf("expected exactly one elapsed_ms, got %d in %s", c, text)
			}

			// Bytes must parse as JSON and elapsed_ms must equal 42.
			var obj map[string]any
			if err := json.Unmarshal([]byte(text), &obj); err != nil {
				t.Fatalf("wire bytes do not parse as JSON: %v — %s", err, text)
			}
			ms, ok := obj["elapsed_ms"].(float64)
			if !ok {
				t.Fatalf("elapsed_ms missing or wrong type: %v", obj["elapsed_ms"])
			}
			if ms != 42 {
				t.Errorf("expected elapsed_ms=42, got %v", ms)
			}
		})
	}
}

// TestDeferredPayload_WrapStashUnstashRoundtrip verifies that the
// jsonResult → stash → wrap → finalize cycle leaves the wire payload
// shape-compatible with the legacy injectElapsedMS path.
func TestDeferredPayload_WrapStashUnstashRoundtrip(t *testing.T) {
	t.Setenv("MCP_WIRE_FORMAT", "json")

	v := map[string]any{
		"results": []any{
			map[string]any{"id": "a", "name": "Alpha", "kind": "function"},
		},
		"count": 1,
	}
	res := jsonResult(v)

	// jsonResult stashed v; verify takeDeferred retrieves the same map.
	got, ok := takeDeferred(res)
	if !ok {
		t.Fatal("expected deferred value to be stashed")
	}
	if _, ok := got.(map[string]any); !ok {
		t.Fatalf("expected map[string]any, got %T", got)
	}

	// Second take must return nothing (LoadAndDelete semantics).
	if _, ok := takeDeferred(res); ok {
		t.Error("deferred map entry was not deleted on first take")
	}
}

// TestDeferredPayload_NoTOON_NoFields verifies the byte shape of the
// no-TOON, no-fields= happy path: single object envelope with elapsed_ms
// appended.
func TestDeferredPayload_NoTOON_NoFields(t *testing.T) {
	t.Setenv("MCP_WIRE_FORMAT", "json")

	text, err := finalizeDeferred(map[string]any{
		"id":   "x",
		"name": "Solo",
	}, 7, nil)
	if err != nil {
		t.Fatal(err)
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(text), &obj); err != nil {
		t.Fatalf("invalid JSON: %v — %s", err, text)
	}
	if obj["id"] != "x" {
		t.Errorf("id field lost in deferred path: %v", obj)
	}
	if obj["elapsed_ms"].(float64) != 7 {
		t.Errorf("elapsed_ms=7 expected, got %v", obj["elapsed_ms"])
	}
}

// TestDeferredPayload_ArrayWrap verifies a top-level array is wrapped in
// the {items, count, elapsed_ms} envelope (parity with the legacy
// injectElapsedMS array branch).
func TestDeferredPayload_ArrayWrap(t *testing.T) {
	t.Setenv("MCP_WIRE_FORMAT", "json")

	text, err := finalizeDeferred([]any{
		map[string]any{"id": "1"},
		map[string]any{"id": "2"},
		map[string]any{"id": "3"},
	}, 99, nil)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(text), &obj); err != nil {
		t.Fatalf("invalid JSON: %v — %s", err, text)
	}
	if obj["count"].(float64) != 3 {
		t.Errorf("count=3 expected, got %v", obj["count"])
	}
	if obj["elapsed_ms"].(float64) != 99 {
		t.Errorf("elapsed_ms=99 expected, got %v", obj["elapsed_ms"])
	}
	items, ok := obj["items"].([]any)
	if !ok {
		t.Fatalf("expected items []any, got %T", obj["items"])
	}
	if len(items) != 3 {
		t.Errorf("expected 3 items, got %d", len(items))
	}
}

// TestDeferredPayload_TOONItems verifies TOON conversion is applied to
// homogeneous items arrays in the deferred path, matching the legacy
// envelope behaviour (#1686).
func TestDeferredPayload_TOONItems(t *testing.T) {
	t.Setenv("MCP_WIRE_FORMAT", "toon")

	text, err := finalizeDeferred(map[string]any{
		"items": []any{
			map[string]any{"id": "ep1", "method": "POST"},
			map[string]any{"id": "ep2", "method": "GET"},
		},
		"count": 2,
	}, 13, nil)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(text), &obj); err != nil {
		t.Fatalf("invalid JSON: %v — %s", err, text)
	}
	items, ok := obj["items"].(string)
	if !ok {
		t.Fatalf("expected TOON string for items, got %T", obj["items"])
	}
	if !strings.HasPrefix(items, "[!schema {") {
		t.Errorf("expected TOON header, got: %s", items)
	}
	if obj["elapsed_ms"].(float64) != 13 {
		t.Errorf("elapsed_ms=13 expected, got %v", obj["elapsed_ms"])
	}
}

// TestDeferredPayload_FieldsFastPath verifies that fields= filtering now
// rides the single-marshal fast path (#2328). The filtered envelope is
// produced directly by finalizeDeferred without a parse cycle.
func TestDeferredPayload_FieldsFastPath(t *testing.T) {
	t.Setenv("MCP_WIRE_FORMAT", "json")

	v := map[string]any{
		"results": []any{
			map[string]any{"id": "a", "name": "Alpha", "kind": "function", "extra": "X"},
			map[string]any{"id": "b", "name": "Beta", "kind": "class", "extra": "Y"},
		},
		"count": 2,
	}
	text, err := finalizeDeferred(v, 5, []string{"name", "id"})
	if err != nil {
		t.Fatalf("finalizeDeferred: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(text), &obj); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if obj["elapsed_ms"].(float64) != 5 {
		t.Errorf("elapsed_ms=5 expected, got %v", obj["elapsed_ms"])
	}
	results, _ := obj["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		rec, _ := r.(map[string]any)
		if _, present := rec["kind"]; present {
			t.Errorf("fields= filter should have stripped 'kind' from %+v", rec)
		}
		if _, present := rec["extra"]; present {
			t.Errorf("fields= filter should have stripped 'extra' from %+v", rec)
		}
	}
}

// TestDeferredPayload_FieldsFastPath_TypedStruct verifies fields= filtering
// on typed-struct returns (#2328): a []struct passed via the envelope is
// reflection-converted, filtered, and routed through the single-marshal
// path with no marshal/unmarshal round trip in the legacy applyFieldsToResult.
func TestDeferredPayload_FieldsFastPath_TypedStruct(t *testing.T) {
	t.Setenv("MCP_WIRE_FORMAT", "json")

	type item struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Kind  string `json:"kind,omitempty"`
		Extra string `json:"extra,omitempty"`
	}
	v := map[string]any{
		"results": []item{
			{ID: "a", Name: "Alpha", Kind: "function", Extra: "X"},
			{ID: "b", Name: "Beta", Kind: "class", Extra: "Y"},
		},
		"count": 2,
	}
	text, err := finalizeDeferred(v, 7, []string{"name", "id"})
	if err != nil {
		t.Fatalf("finalizeDeferred: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(text), &obj); err != nil {
		t.Fatalf("parse: %v", err)
	}
	results, _ := obj["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		rec, _ := r.(map[string]any)
		if _, present := rec["kind"]; present {
			t.Errorf("kind should be stripped from typed struct: %+v", rec)
		}
		if rec["name"] == nil || rec["id"] == nil {
			t.Errorf("expected name+id present, got %+v", rec)
		}
	}
}

// TestStructuredContent_NotOnWire verifies #2327: the typed value carried
// via res.StructuredContent during dispatch is cleared before the wire
// envelope is emitted. (No package-level sync.Map; the carrier is the
// result struct itself.)
func TestStructuredContent_NotOnWire(t *testing.T) {
	v := map[string]any{"id": "x"}
	res := jsonResult(v)
	if res.StructuredContent == nil {
		t.Fatal("expected StructuredContent set by jsonResult")
	}
	got, ok := takeDeferred(res)
	if !ok || got == nil {
		t.Fatalf("takeDeferred should return the stashed value, got ok=%v v=%v", ok, got)
	}
	if res.StructuredContent != nil {
		t.Errorf("takeDeferred should clear StructuredContent (got %v)", res.StructuredContent)
	}
}

// TestCountTopLevelJSONArrayElements covers the #2329 byte-level count
// implementation across edge cases: empty arrays, nested arrays/objects,
// commas inside strings, escape sequences.
func TestCountTopLevelJSONArrayElements(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"empty", "[]", 0},
		{"whitespace_empty", "[ \n ]", 0},
		{"one_scalar", "[1]", 1},
		{"three_scalars", "[1,2,3]", 3},
		{"strings_with_commas", `["a,b","c,d","e"]`, 3},
		{"nested_objects", `[{"a":1,"b":2},{"c":3}]`, 2},
		{"nested_arrays", `[[1,2,3],[4,5],[6]]`, 3},
		{"escaped_quote_in_string", `["a\",b","c"]`, 2},
		{"deep_nesting", `[{"a":{"b":[1,2,3]}},{"x":[{"y":1}]}]`, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := countTopLevelJSONArrayElements([]byte(tc.in))
			if got != tc.want {
				t.Errorf("count(%s) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

// TestInjectElapsedMSIntoBytes_ArrayHasCount verifies #2329 — the byte-level
// array branch wraps the array in {items, count, elapsed_ms} with count
// correctly populated (was hard-omitted pre-#2329).
func TestInjectElapsedMSIntoBytes_ArrayHasCount(t *testing.T) {
	got := injectElapsedMSIntoBytes([]byte(`[{"a":1},{"a":2},{"a":3}]`), 11)
	var obj map[string]any
	if err := json.Unmarshal([]byte(got), &obj); err != nil {
		t.Fatalf("byte-injected output is not valid JSON: %v — %s", err, got)
	}
	if obj["count"].(float64) != 3 {
		t.Errorf("expected count=3, got %v in %s", obj["count"], got)
	}
	if obj["elapsed_ms"].(float64) != 11 {
		t.Errorf("expected elapsed_ms=11, got %v", obj["elapsed_ms"])
	}
}

// BenchmarkResponsePath_Legacy measures the marshal → parse → re-marshal cost
// on a representative endpoints-like payload. This path is retained as a
// baseline comparison for BenchmarkResponsePath_Deferred even though
// injectElapsedMS no longer exists: we simulate the old behaviour by
// marshaling, then calling finalizeDeferred (which parses + re-marshals via
// the map[string]any branch), matching the old round-trip cost profile.
func BenchmarkResponsePath_Legacy(b *testing.B) {
	payload := buildBenchPayload()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Step 1: marshal the handler value.
		data, _ := json.Marshal(payload)
		// Step 2: unmarshal back into map[string]any (simulates the old parse
		// step inside injectElapsedMS), then call finalizeDeferred for the
		// re-marshal — same round-trip cost as the retired function.
		var obj map[string]any
		_ = json.Unmarshal(data, &obj)
		_, _ = finalizeDeferred(obj, int64(i), nil)
	}
}

// BenchmarkResponsePath_Deferred measures the #2287 single-marshal path
// — handler's eager marshal + finalizeDeferred's wire marshal, no parse.
func BenchmarkResponsePath_Deferred(b *testing.B) {
	payload := buildBenchPayload()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// jsonResult marshals once for in-place res.Content[0] (so
		// internal callers see valid JSON) and stashes the value.
		res := jsonResult(payload)
		// wrap pops the value and finalizes — single marshal at wire,
		// no parse.
		v, _ := takeDeferred(res)
		_, _ = finalizeDeferred(v, int64(i), nil)
	}
}

// BenchmarkResponsePath_DeferredWithFields measures the #2328 fast-path
// for fields= callers — previously the legacy parse-based applyFieldsToResult
// kicked in here, now the structured filter runs in-place and the wire
// shape is built via finalizeDeferred. Numbers should be comparable to
// BenchmarkResponsePath_Deferred (a small per-record reflection overhead
// is the only delta when records are plain map[string]any).
func BenchmarkResponsePath_DeferredWithFields(b *testing.B) {
	payload := buildBenchPayload()
	fields := []string{"id", "method"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res := jsonResult(payload)
		v, _ := takeDeferred(res)
		_, _ = finalizeDeferred(v, int64(i), fields)
	}
}

// buildBenchPayload returns a representative response shape for the
// endpoints / clusters tools (the heaviest list tools per the strategy
// doc). 200 records of 6 fields each ≈ what a busy MCP call returns.
func buildBenchPayload() map[string]any {
	items := make([]any, 0, 200)
	for i := 0; i < 200; i++ {
		items = append(items, map[string]any{
			"id":          "upvate-core::endpoint_" + itoa64(int64(i)),
			"method":      "POST",
			"path":        "/api/v1/orders/" + itoa64(int64(i)),
			"handler":     "OrderViewSet.create",
			"source_file": "internal/orders/handlers.go",
			"start_line":  int64(100 + i),
		})
	}
	return map[string]any{
		"items": items,
		"count": len(items),
	}
}
