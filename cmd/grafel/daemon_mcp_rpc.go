package main

// daemon_mcp_rpc.go wires the internal/mcp tool catalog and dispatcher
// into the daemon.Config as MCPListTools / MCPCallTool function values.
//
// This file lives in cmd/grafel (not internal/daemon) to break the
// import cycle: internal/mcp imports internal/daemon for layout paths.
// The wiring layer sits above both packages, so it can import freely.
//
// The *mcp.Server is initialised lazily on first call (via sync.Once)
// using the default registry path (~/.grafel/registry.json). This
// mirrors the standalone `grafel mcp serve` startup without
// blocking the daemon's socket listener.

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	mcpapi "github.com/mark3labs/mcp-go/mcp"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/mcp"
)

// mcpServerOnce guards single initialisation of the global MCP server used
// by the daemon's MCPToolList / MCPToolCall RPC methods.
var (
	mcpServerOnce    sync.Once
	mcpServerShared  *mcp.Server
	mcpServerInitErr error

	// daemonWarmingFn is the read-only scheduler warming accessor (#5690),
	// published by daemon.Config.OnSchedulerReady once the scheduler is up and
	// consumed lazily by the MCP server's State. A holder (rather than direct
	// wiring) decouples the two lifecycles: the scheduler starts inside
	// daemon.Serve while the *mcp.Server is initialised lazily on first RPC —
	// neither ordering matters because mcpServerInstance reads the holder on
	// every call. Nil until the scheduler publishes; then the MCP surface
	// reports warming state. Pointer-to-func so the atomic can carry a func.
	daemonWarmingFn atomic.Pointer[func() daemon.WarmingSnapshot]
)

// setDaemonWarmingFn publishes the scheduler's warming accessor for the MCP
// server to consume (#5690). Wired as daemon.Config.OnSchedulerReady.
func setDaemonWarmingFn(fn func() daemon.WarmingSnapshot) {
	daemonWarmingFn.Store(&fn)
}

// mcpServerInstance returns the lazily-initialised *mcp.Server. The first
// call constructs it from the default registry; subsequent calls return the
// cached instance. Returns an error string if construction failed.
func mcpServerInstance() (*mcp.Server, error) {
	mcpServerOnce.Do(func() {
		srv, err := mcp.NewServer(mcp.Config{})
		if err != nil {
			mcpServerInitErr = fmt.Errorf("mcp server init: %w", err)
			return
		}
		// #5729 PR3: wire the warming accessor to the status-plane sidecar
		// (daemon.WarmingFromStatusFile) rather than the live in-process
		// scheduler handle. This is the ONE code path that answers identically
		// in monolith mode (flag off) and split mode (serve has no in-process
		// scheduler at all) — both write the SAME engine-liveness file (see
		// startEnginePlane), so there is nothing mode-specific left here.
		// daemonWarmingFn/setDaemonWarmingFn (still wired as
		// daemon.Config.OnSchedulerReady) is retained for any other in-process
		// consumer but is no longer this closure's source.
		srv.State.SetWarmingSnapshot(func() daemon.WarmingSnapshot {
			return daemon.WarmingFromStatusFile()
		})
		mcpServerShared = srv
	})
	return mcpServerShared, mcpServerInitErr
}

// daemonMCPListTools is the MCPListToolsFunc injected into daemon.Config.
// It gates the tool list to the caller's cwd (#1769): sessions whose cwd is
// not under any registered group receive only the sentinel tool
// (grafel_status), reducing the handshake from ~2,319 to ~80 tokens.
func daemonMCPListTools(cwd string) ([]daemon.MCPToolEntry, error) {
	srv, err := mcpServerInstance()
	if err != nil {
		return nil, err
	}

	// ListToolsForCWD handles the full cwd-gate decision: full list vs sentinel.
	mcpEntries, err := srv.ListToolsForCWD(cwd)
	if err != nil {
		return nil, err
	}

	out := make([]daemon.MCPToolEntry, 0, len(mcpEntries))
	for _, e := range mcpEntries {
		out = append(out, daemon.MCPToolEntry{
			Name:        e.Name,
			Description: e.Description,
			InputSchema: e.InputSchema,
		})
	}
	return out, nil
}

// daemonMCPCallTool is the MCPCallToolFunc injected into daemon.Config.
// It dispatches a single tool call via the *mcp.Server's registered handler,
// forwarding CWD for ADR-0008 routing.
func daemonMCPCallTool(name string, args map[string]any, cwd string) (daemon.MCPCallResult, error) {
	srv, err := mcpServerInstance()
	if err != nil {
		return daemon.MCPCallResult{
			IsError: true,
			Content: []map[string]any{
				{"type": "text", "text": fmt.Sprintf("mcp server unavailable: %v", err)},
			},
		}, nil
	}

	// Look up the handler.
	st := srv.MCP.GetTool(name)
	if st == nil {
		return daemon.MCPCallResult{
			IsError: true,
			Content: []map[string]any{
				{"type": "text", "text": fmt.Sprintf("tool not found: %s", name)},
			},
		}, nil
	}

	// Build the CallToolRequest. Inject CWD into arguments so
	// ADR-0008 CWD-aware routing works identically to the stdio path.
	callArgs := make(map[string]any, len(args)+1)
	for k, v := range args {
		callArgs[k] = v
	}
	if cwd != "" {
		if _, exists := callArgs["cwd"]; !exists {
			callArgs["cwd"] = cwd
		}
	}

	req := mcpapi.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = callArgs

	result, err := st.Handler(context.Background(), req)
	if err != nil {
		return daemon.MCPCallResult{
			IsError: true,
			Content: []map[string]any{
				{"type": "text", "text": fmt.Sprintf("tool error: %v", err)},
			},
		}, nil
	}
	if result == nil {
		return daemon.MCPCallResult{Content: []map[string]any{}}, nil
	}

	// #2828 measure-first: capture the final on-wire payload size HERE rather
	// than inside internal/mcp.wrap. This is the cleanest placement because
	// (a) result is the *mcpapi.CallToolResult AFTER wrap has run the whole
	// finalizeDeferred → appendElapsedTrailer → applyIDInterning pipeline, so
	// it already reflects the real, interned on-wire payload; and (b) it keeps
	// mcp-go's CallToolResult type out of the internal/daemon package (which
	// only sees the plain MCPCallResult wire shape). wireBytes is the sum of
	// len(TextContent.Text) across Content; tokenEst is the char/4 estimate
	// matching the quality-bench skill convention.
	wireBytes := mcpWireBytes(result.Content)
	return daemon.MCPCallResult{
		IsError:       result.IsError,
		Content:       mcpContentToMaps(result.Content),
		WireBytes:     wireBytes,
		TokenEstimate: wireBytes / 4,
	}, nil
}

// mcpWireBytes returns the total on-wire payload size of a tool result: the
// sum of len(Text) across every TextContent block. Non-text content blocks
// (images, embedded resources) are not currently produced by grafel tools
// and contribute 0. Measured after applyIDInterning (see daemonMCPCallTool).
func mcpWireBytes(content []mcpapi.Content) int {
	total := 0
	for _, c := range content {
		if tc, ok := mcpapi.AsTextContent(c); ok {
			total += len(tc.Text)
		}
	}
	return total
}

// mcpContentToMaps converts the mcp-go Content slice to the
// []map[string]any wire shape expected by the bridge.
func mcpContentToMaps(content []mcpapi.Content) []map[string]any {
	if len(content) == 0 {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(content))
	for _, c := range content {
		raw, err := json.Marshal(c)
		if err != nil {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		out = append(out, m)
	}
	return out
}
