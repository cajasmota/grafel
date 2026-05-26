package mcp

// result_helpers_test.go — shared test-only helpers for extracting content
// from *mcpapi.CallToolResult values.
//
// These helpers were introduced in #2330 to replace 35 scattered
// Content[0].(mcpapi.TextContent).Text / range-and-assert patterns across
// internal/mcp/*_test.go files.  Centralising the extraction unblocks
// #2325-#2329 (full retirement of the legacy injectElapsedMS path): those
// refactors can flip handler return shapes without churning every test file.

import (
	"encoding/json"
	"testing"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// extractResultText returns the raw text from the first TextContent item in
// res.Content.  Use this helper when the test genuinely needs the raw bytes
// (e.g. newline-structure checks, trailer-format assertions, or non-JSON
// content such as markdown).
func extractResultText(t *testing.T, res *mcpapi.CallToolResult) string {
	t.Helper()
	if res == nil {
		t.Fatal("extractResultText: result is nil")
	}
	for _, c := range res.Content {
		if tc, ok := c.(mcpapi.TextContent); ok {
			return tc.Text
		}
	}
	t.Fatal("extractResultText: no TextContent found in result")
	return ""
}

// extractResultJSON extracts the first TextContent from res and unmarshals it
// as a JSON object (map[string]any).  The test fails immediately if there is
// no TextContent or if the text is not valid JSON.
func extractResultJSON(t *testing.T, res *mcpapi.CallToolResult) map[string]any {
	t.Helper()
	text := extractResultText(t, res)
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("extractResultJSON: unmarshal failed: %v\nraw: %s", err, text)
	}
	return out
}

// assertResultJSON is a convenience wrapper that extracts the JSON map and
// checks that out[key] == want (using ==).  It calls t.Errorf (not Fatalf) so
// all key assertions in a test can fire before the test exits.
func assertResultJSON(t *testing.T, res *mcpapi.CallToolResult, key string, want any) {
	t.Helper()
	out := extractResultJSON(t, res)
	got := out[key]
	if got != want {
		t.Errorf("result JSON key %q: got %v (%T), want %v (%T)", key, got, got, want, want)
	}
}
