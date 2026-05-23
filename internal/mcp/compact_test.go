// compact_test.go — #1663 compact serializer tests; #1672 TOON wire helpers.
package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestCompactJSON_Minified verifies the output has no indentation whitespace
// and round-trips back to the same shape.
func TestCompactJSON_Minified(t *testing.T) {
	v := map[string]any{
		"id":          "abc",
		"source_file": "foo/bar.go",
		"start_line":  42,
		"nested":      map[string]any{"k": "v"},
	}
	got := compactJSON(v)
	if strings.Contains(got, "  ") || strings.Contains(got, "\n") {
		t.Errorf("compactJSON should not contain pretty whitespace: %q", got)
	}
	var back map[string]any
	if err := json.Unmarshal([]byte(got), &back); err != nil {
		t.Fatalf("round-trip failed: %v", err)
	}
	if back["id"] != "abc" || back["source_file"] != "foo/bar.go" {
		t.Errorf("schema lost on round-trip: %v", back)
	}
}

// TestJSONResult_NoIndent verifies the public helper now emits minified JSON.
func TestJSONResult_NoIndent(t *testing.T) {
	res := jsonResult(map[string]any{"foo": "bar", "baz": []int{1, 2, 3}})
	if res == nil || len(res.Content) == 0 {
		t.Fatal("nil result")
	}
	// Inspect the text content.
	type texter interface{ GetText() string }
	var text string
	for _, c := range res.Content {
		// mcpapi.TextContent has a Text field; rely on JSON-marshalling the
		// content to read it portably here.
		data, err := json.Marshal(c)
		if err != nil {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal(data, &obj); err == nil {
			if t, ok := obj["text"].(string); ok {
				text = t
				break
			}
		}
	}
	if text == "" {
		t.Fatal("could not read text content")
	}
	if strings.Contains(text, "\n  ") {
		t.Errorf("jsonResult emitted indented JSON: %q", text)
	}
}

// TestTabularEncode_Shape verifies the schema/row format and escaping.
func TestTabularEncode_Shape(t *testing.T) {
	got := tabularEncode(
		[]string{"id", "kind", "file", "line"},
		[][]any{
			{"abc", "View", "routers.py", 12},
			{"def", "Method", "view.py", 40},
			{"weird,name", "Class", "a\\b.py", 1},
		},
	)
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 4 {
		t.Fatalf("want 4 lines (1 schema + 3 rows), got %d: %q", len(lines), got)
	}
	if lines[0] != "[!schema {id,kind,file,line}]" {
		t.Errorf("schema line wrong: %q", lines[0])
	}
	if lines[1] != "{abc,View,routers.py,12}" {
		t.Errorf("row 1 wrong: %q", lines[1])
	}
	// Escaped comma and backslash.
	if !strings.Contains(lines[3], `weird\,name`) {
		t.Errorf("comma should be escaped: %q", lines[3])
	}
	if !strings.Contains(lines[3], `a\\b.py`) {
		t.Errorf("backslash should be escaped: %q", lines[3])
	}
}

// TestTabularEncode_SavesTokensVsJSON spot-checks that for list-of-record
// payloads, the tabular form is meaningfully shorter than the equivalent
// minified JSON array.
func TestTabularEncode_SavesTokensVsJSON(t *testing.T) {
	rows := make([][]any, 50)
	for i := range rows {
		rows[i] = []any{"id_" + strings.Repeat("x", 4), "Method", "path/to/file.go", 100 + i}
	}
	tab := tabularEncode([]string{"id", "kind", "file", "line"}, rows)

	// Equivalent JSON: array of objects.
	arr := make([]map[string]any, len(rows))
	for i, r := range rows {
		arr[i] = map[string]any{"id": r[0], "kind": r[1], "file": r[2], "line": r[3]}
	}
	data, _ := json.Marshal(arr)
	if len(tab) >= len(data) {
		t.Errorf("tabular (%d) should be shorter than JSON array (%d) for list-of-record",
			len(tab), len(data))
	}
}

// ---------------------------------------------------------------------------
// #1672 — recordsToTOON helper tests
// ---------------------------------------------------------------------------

// TestRecordsToTOON_HomogeneousConverts verifies homogeneous record arrays are
// converted to TOON with a sorted schema line and one row per record.
func TestRecordsToTOON_HomogeneousConverts(t *testing.T) {
	// Simulate what json.Unmarshal produces for []any of map[string]any.
	input := []any{
		map[string]any{"id": "e1", "name": "OrderService", "repo": "svc"},
		map[string]any{"id": "e2", "name": "UserService", "repo": "svc"},
	}
	got, ok := recordsToTOON(input)
	if !ok {
		t.Fatal("expected recordsToTOON to return ok=true for homogeneous input")
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	// 1 schema line + 2 rows
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), got)
	}
	// Keys sorted: id, name, repo
	if lines[0] != "[!schema {id,name,repo}]" {
		t.Errorf("schema line wrong: %q", lines[0])
	}
	// Both rows present (exact order is deterministic via sorted keys).
	if !strings.Contains(got, "e1") || !strings.Contains(got, "OrderService") {
		t.Errorf("missing row 1 data: %q", got)
	}
	if !strings.Contains(got, "e2") || !strings.Contains(got, "UserService") {
		t.Errorf("missing row 2 data: %q", got)
	}
}

// TestRecordsToTOON_HeterogeneousReturnsFalse verifies that arrays with
// mismatched key sets across elements return ok=false.
func TestRecordsToTOON_HeterogeneousReturnsFalse(t *testing.T) {
	input := []any{
		map[string]any{"id": "e1", "name": "fn1"},
		map[string]any{"id": "e2", "name": "fn2", "extra": "x"},
	}
	_, ok := recordsToTOON(input)
	if ok {
		t.Error("expected recordsToTOON to return ok=false for heterogeneous schema")
	}
}

// TestRecordsToTOON_EmptyReturnsFalse verifies empty slices return ok=false.
func TestRecordsToTOON_EmptyReturnsFalse(t *testing.T) {
	_, ok := recordsToTOON(nil)
	if ok {
		t.Error("expected ok=false for nil input")
	}
	_, ok2 := recordsToTOON([]any{})
	if ok2 {
		t.Error("expected ok=false for empty slice")
	}
}

// TestRecordsToTOON_NonObjectElementReturnsFalse verifies that a slice
// containing non-map elements is not TOON-encoded.
func TestRecordsToTOON_NonObjectElementReturnsFalse(t *testing.T) {
	input := []any{"just a string", "another"}
	_, ok := recordsToTOON(input)
	if ok {
		t.Error("expected ok=false when elements are not map[string]any")
	}
}

// TestRecordsToTOON_TokenSavings verifies that TOON text is shorter than the
// minified JSON for the same data — confirming actual token savings on the wire.
func TestRecordsToTOON_TokenSavings(t *testing.T) {
	const n = 40
	input := make([]any, n)
	for i := range input {
		input[i] = map[string]any{
			"entity_id":   "repo1::abcdef1234567890",
			"name":        "POST /api/v2/orders",
			"kind":        "http_endpoint_definition",
			"repo":        "orders-service",
			"source_file": "internal/handlers/orders.go",
			"start_line":  float64(42 + i),
			"method":      "POST",
			"path":        "/api/v2/orders",
		}
	}
	toonText, ok := recordsToTOON(input)
	if !ok {
		t.Fatal("expected homogeneous input to convert")
	}
	jsonData, _ := json.Marshal(input)

	toonTokens := estimateTokens(toonText)
	jsonTokens := estimateTokens(string(jsonData))
	savings := float64(jsonTokens-toonTokens) / float64(jsonTokens) * 100
	if toonTokens >= jsonTokens {
		t.Errorf("TOON (%d tokens) should be fewer than JSON (%d tokens)", toonTokens, jsonTokens)
	}
	// Expect at least 30% savings for this representative endpoint payload.
	if savings < 30 {
		t.Errorf("expected ≥30%% token savings, got %.1f%% (TOON=%d, JSON=%d)",
			savings, toonTokens, jsonTokens)
	}
	t.Logf("Token savings: %.1f%% (TOON=%d vs JSON=%d for %d endpoint records)",
		savings, toonTokens, jsonTokens, n)
}
