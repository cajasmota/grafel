package daemon_test

// mcp_rpc_integration_test.go exercises the two new RPC methods
// (Daemon.MCPToolList / Daemon.MCPToolCall) end-to-end:
//
//  1. Start a real daemon with stub MCP functions injected.
//  2. Dial the socket with a net/rpc JSON-RPC 1.0 client.
//  3. Call MCPToolList → assert 14+ tools returned.
//  4. Call MCPToolCall(archigraph_stats) → assert non-error reply.
//
// A separate bridge-subprocess test would require the archigraph binary
// to be built first, which is out of scope for a package test. The bridge
// itself is covered by internal/cli/mcp_bridge_test.go.

import (
	"context"
	"encoding/json"
	"net/rpc"
	"net/rpc/jsonrpc"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/daemon/transport"
)

// ── stub MCP functions ────────────────────────────────────────────────────────

// stubToolCatalog is the 14-tool canonical list used in the integration test.
// All names must match exactly what the real mcp.Server registers, but the
// descriptions / schemas here are minimal stubs — we only test the RPC
// plumbing, not the handler business logic.
var stubToolCatalog = []daemon.MCPToolEntry{
	{Name: "archigraph_find", Description: "BM25 search"},
	{Name: "archigraph_inspect", Description: "entity lookup"},
	{Name: "archigraph_expand", Description: "neighbor expansion"},
	{Name: "archigraph_clusters", Description: "Louvain communities"},
	{Name: "archigraph_stats", Description: "corpus metrics"},
	{Name: "archigraph_traces", Description: "process-flow chains"},
	{Name: "archigraph_cross_links", Description: "cross-repo links"},
	{Name: "archigraph_get_source", Description: "source snippet"},
	{Name: "archigraph_repairs", Description: "repair queue"},
	{Name: "archigraph_patterns", Description: "pattern store"},
	{Name: "archigraph_enrichments", Description: "enrichment candidates"},
	{Name: "archigraph_save_finding", Description: "persist Q/A pair"},
	{Name: "archigraph_recent_activity", Description: "recently changed entities"},
	{Name: "archigraph_get_telemetry", Description: "server uptime + per-tool counters"},
}

func stubListTools(_ string) ([]daemon.MCPToolEntry, error) {
	return stubToolCatalog, nil
}

func stubCallTool(name string, args map[string]any, _ string) (daemon.MCPCallResult, error) {
	switch name {
	case "archigraph_stats":
		// Minimal well-formed stats response.
		payload, _ := json.Marshal(map[string]any{
			"node_count": 0,
			"edge_count": 0,
		})
		return daemon.MCPCallResult{
			Content: []map[string]any{
				{"type": "text", "text": string(payload)},
			},
		}, nil
	default:
		return daemon.MCPCallResult{
			Content: []map[string]any{
				{"type": "text", "text": "stub: " + name},
			},
		}, nil
	}
}

// ── test wiring ───────────────────────────────────────────────────────────────

// runDaemonWithConfig starts daemon.Run with a fully-specified Config and
// waits for the socket to appear. The daemon stops when the test ends.
// Returns the Config's Layout for convenience.
func runDaemonWithConfig(t *testing.T, cfg daemon.Config) daemon.Layout {
	t.Helper()
	ctx, cancel := cancelOnCleanup(t)
	done := make(chan error, 1)
	go func() { done <- daemon.Run(ctx, cfg) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Logf("daemon did not exit within 3s")
		}
	})
	// Wait for the daemon to become ready using a dial-based probe.
	// os.Stat is not usable for Windows named pipes.
	waitDaemonReady(t, cfg.Layout.SocketPath, 3*time.Second)
	return cfg.Layout
}

// cancelOnCleanup returns a context that is cancelled during t.Cleanup.
func cancelOnCleanup(t *testing.T) (ctx context.Context, cancel context.CancelFunc) {
	t.Helper()
	ctx, cancel = context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return ctx, cancel
}

// runDaemonWithMCP starts a daemon with stub MCP functions and returns the
// socket path. The daemon stops when the test ends.
func runDaemonWithMCP(t *testing.T) string {
	t.Helper()
	root := shortTempRoot(t)
	t.Setenv(daemon.EnvRoot, root)
	layout, err := daemon.DefaultLayout()
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	if err := daemon.EnsureLayout(layout); err != nil {
		t.Fatalf("ensure layout: %v", err)
	}

	runDaemonWithConfig(t, daemon.Config{
		Layout:       layout,
		MCPListTools: stubListTools,
		MCPCallTool:  stubCallTool,
	})
	return layout.SocketPath
}

// dialRPC opens a net/rpc JSON-RPC 1.0 client on the given socket/pipe path.
// Uses the platform-appropriate transport (Unix socket on Linux/macOS, named
// pipe on Windows) so the test runs unchanged on all platforms.
func dialRPC(t *testing.T, socketPath string) *rpc.Client {
	t.Helper()
	// Wait up to 3 s for the endpoint to become available.
	deadline := time.Now().Add(3 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := transport.Dial(socketPath)
		if err == nil {
			return rpc.NewClientWithCodec(jsonrpc.NewClientCodec(conn))
		}
		lastErr = err
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("dial %s: %v", socketPath, lastErr)
	return nil
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestMCPToolList_Integration_Returns14Tools(t *testing.T) {
	socketPath := runDaemonWithMCP(t)
	c := dialRPC(t, socketPath)
	defer c.Close()

	var reply daemon.MCPToolListReply
	if err := c.Call("Daemon.MCPToolList", &daemon.MCPToolListArgs{}, &reply); err != nil {
		t.Fatalf("Daemon.MCPToolList: %v", err)
	}

	// The stub catalog has exactly 14 entries.
	const wantCount = 14
	if len(reply.Tools) != wantCount {
		t.Errorf("expected %d tools, got %d: %v", wantCount, len(reply.Tools), toolNames(reply.Tools))
	}

	// Verify each canonical tool name is present.
	byName := make(map[string]struct{}, len(reply.Tools))
	for _, tool := range reply.Tools {
		byName[tool.Name] = struct{}{}
	}
	canonical := []string{
		"archigraph_find", "archigraph_inspect", "archigraph_expand",
		"archigraph_clusters", "archigraph_stats", "archigraph_traces",
		"archigraph_cross_links", "archigraph_get_source", "archigraph_repairs",
		"archigraph_patterns", "archigraph_enrichments", "archigraph_save_finding",
		"archigraph_recent_activity", "archigraph_get_telemetry",
	}
	for _, name := range canonical {
		if _, ok := byName[name]; !ok {
			t.Errorf("canonical tool %q missing from MCPToolList reply", name)
		}
	}
}

func TestMCPToolCall_Integration_StatsReturnsContent(t *testing.T) {
	socketPath := runDaemonWithMCP(t)
	c := dialRPC(t, socketPath)
	defer c.Close()

	args := daemon.MCPToolCallArgs{
		Name:      "archigraph_stats",
		Arguments: map[string]any{},
		CWD:       "/tmp/test-project",
	}
	var reply daemon.MCPToolCallReply
	if err := c.Call("Daemon.MCPToolCall", &args, &reply); err != nil {
		t.Fatalf("Daemon.MCPToolCall: %v", err)
	}
	if reply.IsError {
		t.Fatalf("unexpected tool error: %v", reply.Content)
	}
	if len(reply.Content) == 0 {
		t.Fatal("expected content in reply")
	}
	text, _ := reply.Content[0]["text"].(string)
	if text == "" {
		t.Fatal("expected non-empty text in reply content")
	}
	// Should be parseable JSON.
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatalf("reply content is not valid JSON: %v (got: %q)", err, text)
	}
}

func TestMCPToolCall_Integration_NilCallTool_ReturnsErrorBlock(t *testing.T) {
	// Start a daemon where MCPCallTool is nil — the service should return
	// a structured error block (IsError=true) rather than a protocol error.
	root := shortTempRoot(t)
	t.Setenv(daemon.EnvRoot, root)
	layout, err := daemon.DefaultLayout()
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	if err := daemon.EnsureLayout(layout); err != nil {
		t.Fatalf("ensure layout: %v", err)
	}
	runDaemonWithConfig(t, daemon.Config{
		Layout:       layout,
		MCPListTools: stubListTools,
		MCPCallTool:  nil, // nil → "not configured" error block
	})

	c := dialRPC(t, layout.SocketPath)
	defer c.Close()

	args := daemon.MCPToolCallArgs{Name: "archigraph_find"}
	var reply daemon.MCPToolCallReply
	if err := c.Call("Daemon.MCPToolCall", &args, &reply); err != nil {
		t.Fatalf("Daemon.MCPToolCall: %v", err)
	}
	if !reply.IsError {
		t.Fatal("expected IsError=true when mcpCallTool is nil")
	}
	if len(reply.Content) == 0 {
		t.Fatal("expected error content block")
	}
}

// toolNames extracts names for error messages.
func toolNames(tools []daemon.MCPToolInfo) []string {
	out := make([]string, len(tools))
	for i, t := range tools {
		out[i] = t.Name
	}
	return out
}
