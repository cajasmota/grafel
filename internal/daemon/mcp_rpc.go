package daemon

// mcp_rpc.go — ADR-0017 Phase D
//
// Daemon.MCPToolList and Daemon.MCPToolCall are the two RPC methods the
// mcp-bridge subcommand (internal/cli/mcp_bridge.go, PR #831) calls over
// the daemon's Unix-domain socket. Together they expose the full 14-tool
// archigraph catalog to Claude Code without re-spawning a standalone MCP
// server process.
//
// Design:
//   - To avoid an import cycle (internal/mcp → internal/daemon → internal/mcp),
//     the daemon receives the MCP dispatch surface as two injected function
//     values on Config (MCPListTools, MCPCallTool). cmd/archigraph wires
//     these from a lazily-initialised *mcp.Server.
//   - The dispatcher routes through the *actual* handlers registered on the
//     mcp.Server so existing business logic — BM25 scoring, lazy graph reload,
//     telemetry, ADR-0008 CWD routing — is exercised without duplication.
//   - Telemetry counters are incremented via mcp.Server's wrap() middleware,
//     so archigraph_get_telemetry sees the same numbers regardless of whether
//     the call arrived via the old stdio path or the new bridge path.

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/cajasmota/archigraph/internal/perf"
	"github.com/cajasmota/archigraph/internal/registry"
)

// ── Injected function types ───────────────────────────────────────────────────

// MCPToolEntry is a single tool's metadata returned by MCPListTools.
type MCPToolEntry struct {
	Name        string
	Description string
	InputSchema json.RawMessage // JSONSchema-shaped
}

// MCPCallResult is the dispatcher's response for a single tool invocation.
type MCPCallResult struct {
	// Content is the list of content blocks (type+text or type+json).
	Content []map[string]any
	// IsError is true when the tool returned an error result (not a
	// protocol error — those surface as a returned Go error).
	IsError bool
}

// MCPListToolsFunc returns the registered tool catalog. Injected from
// cmd/archigraph; nil means "not configured" (bridge returns empty list).
type MCPListToolsFunc func() ([]MCPToolEntry, error)

// MCPCallToolFunc dispatches a single tool call. name is the tool name,
// args are the caller's arguments, cwd is the caller's working directory
// (may be empty). Injected from cmd/archigraph.
type MCPCallToolFunc func(name string, args map[string]any, cwd string) (MCPCallResult, error)

// ── Wire types ────────────────────────────────────────────────────────────────

// MCPToolListArgs is the argument struct for Daemon.MCPToolList.
// No fields are needed; the method returns the full, static catalog.
type MCPToolListArgs struct{}

// MCPToolInfo is a single tool's metadata, matching the mcpToolInfo shape
// the bridge uses for the MCP tools/list response.
type MCPToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

// MCPToolListReply is the reply struct for Daemon.MCPToolList.
type MCPToolListReply struct {
	Tools []MCPToolInfo `json:"tools"`
}

// MCPToolCallArgs is the argument struct for Daemon.MCPToolCall.
type MCPToolCallArgs struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
	// CWD is the caller's working directory for ADR-0008 CWD-aware routing.
	// The bridge extracts it from the Claude Code session and forwards it here.
	CWD string `json:"cwd,omitempty"`
}

// MCPToolCallReply is the reply struct for Daemon.MCPToolCall.
// Content mirrors the MCP tools/call wire shape that the bridge re-wraps
// into a JSON-RPC 2.0 response.
type MCPToolCallReply struct {
	Content []map[string]any `json:"content"`
	IsError bool             `json:"is_error,omitempty"`
}

// ── RPC methods ───────────────────────────────────────────────────────────────

// MCPToolList returns the full 14-tool archigraph catalog. The list is
// derived from the injected MCPListTools function (wired from *mcp.Server
// in cmd/archigraph), so the source of truth is always server.go's
// registerTools() — no duplication.
func (s *Service) MCPToolList(_ *MCPToolListArgs, reply *MCPToolListReply) error {
	if s.mcpListTools == nil {
		// Daemon started without MCP wiring (e.g. tests that only test
		// the index/rebuild surface). Return empty rather than an error
		// so the bridge degrades to the offline stub gracefully.
		reply.Tools = []MCPToolInfo{}
		return nil
	}

	entries, err := s.mcpListTools()
	if err != nil {
		return fmt.Errorf("MCPToolList: %w", err)
	}

	tools := make([]MCPToolInfo, 0, len(entries))
	for _, e := range entries {
		tools = append(tools, MCPToolInfo{
			Name:        e.Name,
			Description: e.Description,
			InputSchema: e.InputSchema,
		})
	}
	reply.Tools = tools

	// Record MCP handshake size for the performance budget monitor (#1319).
	// We serialize to JSON to measure the wire size faithfully.
	go func() {
		if b, jerr := json.Marshal(reply); jerr == nil {
			homeDir, _ := registry.HomeDir()
			if homeDir != "" {
				rec := perf.NewRecorder(homeDir + "/perf-history.jsonl")
				_ = rec.Record("mcp_handshake_bytes", "", float64(len(b)))
			}
		}
	}()

	return nil
}

// MCPToolCall dispatches a single tool invocation to the existing handler
// registered on the *mcp.Server (via the injected mcpCallTool function).
// This preserves the full middleware chain (telemetry, lazy reload, panic
// guard — see mcp.Server.wrap) without any duplication.
//
// CWD is forwarded so ADR-0008 caller-CWD routing works identically to
// the old stdio path.
func (s *Service) MCPToolCall(args *MCPToolCallArgs, reply *MCPToolCallReply) error {
	if args == nil || args.Name == "" {
		return fmt.Errorf("MCPToolCall: name is required")
	}

	if s.mcpCallTool == nil {
		reply.IsError = true
		reply.Content = []map[string]any{
			{"type": "text", "text": "archigraph daemon: MCP tool dispatch not configured — ensure daemon was started via 'archigraph install'"},
		}
		return nil
	}

	// #1678: emit a "received" log line BEFORE dispatching so a hung handler
	// still leaves a trace in daemon.log. The original "elapsed=Xms" line only
	// fired after mcpCallTool returned, which made hangs invisible (the call
	// looked like it had never reached the dispatcher).
	repoLabel := args.CWD
	if repoLabel == "" {
		repoLabel = "(cwd not provided)"
	}
	if s.logger != nil {
		s.logger.Printf("[mcp-rpc] tool=%s received repo=%s", args.Name, repoLabel)
	}

	start := time.Now()
	result, err := s.mcpCallTool(args.Name, args.Arguments, args.CWD)
	elapsed := time.Since(start)

	// Debug log: tool=name elapsed=Xms repo=Y (from CWD when available)
	if s.logger != nil {
		s.logger.Printf("[mcp-rpc] tool=%s elapsed=%dms repo=%s", args.Name, elapsed.Milliseconds(), repoLabel)
	}

	if err != nil {
		reply.IsError = true
		reply.Content = []map[string]any{
			{"type": "text", "text": fmt.Sprintf("tool error: %v", err)},
		}
		return nil
	}

	reply.IsError = result.IsError
	reply.Content = result.Content
	if reply.Content == nil {
		reply.Content = []map[string]any{}
	}
	return nil
}
