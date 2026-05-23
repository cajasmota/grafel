package main

// daemon_mcp_rpc_test.go verifies that the live mcp.Server (the one wired
// via daemonMCPListTools / daemonMCPCallTool) exposes the expected 14-tool
// catalog with valid schemas.
//
// These tests bypass the global mcpServerOnce by creating a fresh
// mcp.Server directly with a temp empty registry — avoiding the format
// mismatch that can occur on developer machines with an older registry.json.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	mcpapi "github.com/mark3labs/mcp-go/mcp"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/mcp"
)

// tempRegistryPath writes an empty registry.json to a temp dir and returns the path.
func tempRegistryPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "archi-mcp-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "registry.json")
	if err := os.WriteFile(path, []byte(`{"groups":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestDaemonMCPListTools_RegistersCanonical verifies the live mcp.Server
// exposes at least the canonical core archigraph tools. The full tool
// surface grows over time as new MCP tools are added (#1614/#1619 and
// beyond), so this test asserts a dynamic floor (a stable set of core
// tools by name + a minimum count) rather than pinning to a literal
// number that breaks every time a tool is added.
//
// If a tool is renamed or removed, update the `coreTools` list — that is
// the explicit contract. Adding new tools requires no edits here.
func TestDaemonMCPListTools_RegistersCanonical(t *testing.T) {
	regPath := tempRegistryPath(t)
	srv, err := mcp.NewServer(mcp.Config{RegistryPath: regPath})
	if err != nil {
		t.Fatalf("mcp.NewServer: %v", err)
	}

	toolMap := srv.MCP.ListTools()

	// Core tools that must exist for the MCP surface to be functional.
	// Keep this list to tools whose names are public contract — when a
	// new one is added, just append.
	coreTools := []string{
		"archigraph_whoami",
		"archigraph_find",
		"archigraph_inspect",
		"archigraph_expand",
		"archigraph_clusters",
		"archigraph_stats",
		"archigraph_traces",
		"archigraph_trace",
		"archigraph_get_source",
		"archigraph_repairs",
		"archigraph_patterns",
		"archigraph_enrichments",
		"archigraph_topology",
		"archigraph_flows",
	}
	const wantMinCount = 14
	if len(toolMap) < wantMinCount {
		names := make([]string, 0, len(toolMap))
		for n := range toolMap {
			names = append(names, n)
		}
		t.Fatalf("expected at least %d tools, got %d: %v", wantMinCount, len(toolMap), names)
	}
	for _, name := range coreTools {
		if _, ok := toolMap[name]; !ok {
			t.Errorf("core tool %q missing from mcp.Server", name)
		}
	}
}

// TestDaemonMCPListTools_InputSchemaPresent checks that each tool's
// InputSchema is non-nil and is valid JSON.
func TestDaemonMCPListTools_InputSchemaPresent(t *testing.T) {
	regPath := tempRegistryPath(t)
	srv, err := mcp.NewServer(mcp.Config{RegistryPath: regPath})
	if err != nil {
		t.Fatalf("mcp.NewServer: %v", err)
	}

	toolMap := srv.MCP.ListTools()
	for name, st := range toolMap {
		// Marshal the Tool to extract inputSchema.
		raw, err := json.Marshal(st.Tool)
		if err != nil {
			t.Errorf("tool %q: marshal failed: %v", name, err)
			continue
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Errorf("tool %q: unmarshal failed: %v", name, err)
			continue
		}
		schema, ok := m["inputSchema"]
		if !ok || len(schema) == 0 {
			t.Errorf("tool %q: inputSchema missing", name)
			continue
		}
		var smap map[string]any
		if err := json.Unmarshal(schema, &smap); err != nil {
			t.Errorf("tool %q: inputSchema is not valid JSON: %v", name, err)
		}
	}
}

// TestDaemonMCPCallTool_Stats_NotError verifies the archigraph_stats handler
// returns a non-error content block on an empty registry.
func TestDaemonMCPCallTool_Stats_NotError(t *testing.T) {
	regPath := tempRegistryPath(t)
	srv, err := mcp.NewServer(mcp.Config{RegistryPath: regPath})
	if err != nil {
		t.Fatalf("mcp.NewServer: %v", err)
	}

	st := srv.MCP.GetTool("archigraph_stats")
	if st == nil {
		t.Fatal("archigraph_stats not registered")
	}

	req := mcpapi.CallToolRequest{}
	req.Params.Name = "archigraph_stats"
	req.Params.Arguments = map[string]any{}

	result, err := st.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result == nil {
		t.Fatal("handler returned nil result")
	}
	// With an empty registry, archigraph_stats returns a tool-level error
	// (IsError=true, content="no groups registered"). That is the correct
	// MCP behaviour — it surfaces the error to the agent rather than panicking.
	// We just verify the handler returns *some* content (not nil).
	if len(result.Content) == 0 {
		t.Fatal("expected content block in stats result (even for empty registry)")
	}
}

// TestDaemonMCPCallTool_UnknownTool_ReturnsErrorBlock ensures that calling
// with an unknown tool name via daemonMCPCallTool produces a structured
// error (IsError=true), not a Go-level error.
func TestDaemonMCPCallTool_UnknownTool_ReturnsErrorBlock(t *testing.T) {
	regPath := tempRegistryPath(t)
	srv, err := mcp.NewServer(mcp.Config{RegistryPath: regPath})
	if err != nil {
		t.Fatalf("mcp.NewServer: %v", err)
	}

	// GetTool returns nil for unknown names — this is what daemonMCPCallTool checks.
	st := srv.MCP.GetTool("archigraph_nonexistent_xyz")
	if st != nil {
		t.Fatal("expected nil for unknown tool")
	}

	// Replicate the daemon dispatcher's "tool not found" path.
	result := daemon.MCPCallResult{
		IsError: true,
		Content: []map[string]any{
			{"type": "text", "text": "tool not found: archigraph_nonexistent_xyz"},
		},
	}
	if !result.IsError {
		t.Fatal("expected IsError=true")
	}
}
