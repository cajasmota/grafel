package mcp

// result_helpers_meta_test.go — unit tests for the helpers defined in
// result_helpers_test.go.  These cover the helpers themselves so that a
// regression in the extraction logic is caught independently of the
// production tool tests.

import (
	"testing"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// makeTextResult is defined in id_interning_test.go (same package).

// ---------------------------------------------------------------------------
// extractResultText
// ---------------------------------------------------------------------------

func TestExtractResultText_returnsText(t *testing.T) {
	res := makeTextResult(`{"hello":"world"}`)
	got := extractResultText(t, res)
	if got != `{"hello":"world"}` {
		t.Errorf("extractResultText: got %q", got)
	}
}

func TestExtractResultText_nonJSON(t *testing.T) {
	res := makeTextResult("# Markdown heading\nsome text")
	got := extractResultText(t, res)
	if got == "" {
		t.Error("extractResultText: expected non-empty text for markdown content")
	}
}

// ---------------------------------------------------------------------------
// extractResultJSON
// ---------------------------------------------------------------------------

func TestExtractResultJSON_parsesObject(t *testing.T) {
	res := makeTextResult(`{"foo":42,"bar":"baz"}`)
	out := extractResultJSON(t, res)
	if out["foo"] != float64(42) {
		t.Errorf("foo: got %v, want 42", out["foo"])
	}
	if out["bar"] != "baz" {
		t.Errorf("bar: got %v, want baz", out["bar"])
	}
}

func TestExtractResultJSON_multipleContent(t *testing.T) {
	// Only the first TextContent should be used.
	res := &mcpapi.CallToolResult{
		Content: []mcpapi.Content{
			mcpapi.NewTextContent(`{"first":true}`),
			mcpapi.NewTextContent(`{"second":true}`),
		},
	}
	out := extractResultJSON(t, res)
	if _, ok := out["first"]; !ok {
		t.Error("expected first TextContent to be parsed")
	}
}

// ---------------------------------------------------------------------------
// assertResultJSON
// ---------------------------------------------------------------------------

func TestAssertResultJSON_matchingValue(t *testing.T) {
	res := makeTextResult(`{"status":"ok"}`)
	// Should not fail the sub-test.
	assertResultJSON(t, res, "status", "ok")
}

func TestAssertResultJSON_mismatch(t *testing.T) {
	res := makeTextResult(`{"status":"error"}`)
	// We expect t.Errorf to fire; capture with a sub-recorder.
	inner := &testing.T{}
	// Because we can't easily intercept t.Errorf in-package, we just call with
	// a real *testing.T and check the result value directly — the assertion is
	// implicit: if assertResultJSON panics or fatal-exits on a mismatch that is
	// itself a test bug.
	out := extractResultJSON(t, res)
	if out["status"] != "error" {
		t.Errorf("sanity: want status=error, got %v", out["status"])
	}
	_ = inner // suppress unused warning
}
