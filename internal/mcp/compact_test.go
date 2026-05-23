// compact_test.go — #1663 compact serializer tests.
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
