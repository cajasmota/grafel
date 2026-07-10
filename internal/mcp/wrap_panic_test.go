package mcp

import (
	"context"
	"testing"
	"time"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// TestWrapRecoversPanic is the RED/GREEN test for #5717: wrap() is
// documented as "telemetry + lazy reload + panic guard" (see the comment on
// wrap in server.go) but before the fix its deferred func only recorded
// telemetry — it never called recover(). Any registered tool handler that
// panics (nil-deref, out-of-range index, etc.) would propagate the panic out
// of the returned mcpsrv.ToolHandlerFunc and, in the daemon's in-process
// dispatch path, crash the whole process and sever every attached MCP
// bridge. This test registers a deliberately panicking handler through the
// real wrap() middleware and asserts:
//  1. the call returns a well-formed IsError tool result (not a crash), and
//  2. a subsequent, unrelated call through the SAME server still succeeds —
//     i.e. the dispatch loop survives one bad call.
func TestWrapRecoversPanic(t *testing.T) {
	s := &Server{Tel: NewTelemetry(0)}
	// Skip reloadBeforeCall's real reload path (s.State is nil in this
	// minimal unit-test server) by pinning the debounce window open.
	s.reloadDebounce = time.Hour
	s.reloadLastAt.Store(time.Now().UnixNano())

	panicky := s.wrap("grafel_boom", func(_ context.Context, _ mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
		var p *int
		_ = *p // nil-deref panic, simulating a buggy tool handler
		return nil, nil
	})

	res, err := panicky(context.Background(), mcpapi.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error surfaced from recovered panic: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected an IsError tool result for a panicking handler, got %#v", res)
	}

	// The middleware (and the server it is attached to) must survive: a
	// healthy call afterwards still succeeds.
	ok := s.wrap("grafel_ok", func(_ context.Context, _ mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
		return mcpapi.NewToolResultText("fine"), nil
	})
	res2, err2 := ok(context.Background(), mcpapi.CallToolRequest{})
	if err2 != nil {
		t.Fatalf("unexpected error on call after recovered panic: %v", err2)
	}
	if res2 == nil || res2.IsError {
		t.Fatalf("expected a healthy result after a recovered panic, got %#v", res2)
	}
}
