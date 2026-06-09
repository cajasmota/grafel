package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// callHandlerResult invokes a handler directly and returns the result (which may
// be an error result). Unlike callEndpointToolText it does not require the tool
// to be registered on srv.MCP, and it surfaces IsError.
func callHandlerResult(t *testing.T, fn func(context.Context, mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error), args map[string]any) *mcpapi.CallToolResult {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	res, err := fn(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned go error: %v", err)
	}
	return res
}

// Test_handlePRImpact_ArgValidation covers the mode-selection / required-arg
// error paths, which are pure handler logic validated before any disk access.
func Test_handlePRImpact_ArgValidation(t *testing.T) {
	srv := newTestServer(t, &graph.Document{Repo: "demo"})

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{
			name: "missing_repo",
			args: map[string]any{"group": "test", "base": "main", "head": "x"},
			want: "repo is required",
		},
		{
			name: "single_missing_head",
			args: map[string]any{"group": "test", "repo": "demo", "base": "main"},
			want: "single mode requires both base= and head=",
		},
		{
			name: "conflicts_too_few_refs",
			args: map[string]any{"group": "test", "repo": "demo", "refs": []any{"only-one"}},
			want: "at least 2 refs",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := callHandlerResult(t, srv.handlePRImpact, tc.args)
			if res == nil || !res.IsError {
				t.Fatalf("expected an error result, got %v", res)
			}
			if got := resultText(res); !strings.Contains(got, tc.want) {
				t.Errorf("expected error containing %q, got: %s", tc.want, got)
			}
		})
	}
}
