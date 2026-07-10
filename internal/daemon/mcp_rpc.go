package daemon

// mcp_rpc.go — ADR-0017 Phase D
//
// Daemon.MCPToolList and Daemon.MCPToolCall are the two RPC methods the
// mcp-bridge subcommand (internal/cli/mcp_bridge.go, PR #831) calls over
// the daemon's Unix-domain socket. Together they expose the full 14-tool
// grafel catalog to Claude Code without re-spawning a standalone MCP
// server process.
//
// Design:
//   - To avoid an import cycle (internal/mcp → internal/daemon → internal/mcp),
//     the daemon receives the MCP dispatch surface as two injected function
//     values on Config (MCPListTools, MCPCallTool). cmd/grafel wires
//     these from a lazily-initialised *mcp.Server.
//   - The dispatcher routes through the *actual* handlers registered on the
//     mcp.Server so existing business logic — BM25 scoring, lazy graph reload,
//     telemetry, ADR-0008 CWD routing — is exercised without duplication.
//   - Telemetry counters are incremented via mcp.Server's wrap() middleware,
//     so grafel_get_telemetry sees the same numbers regardless of whether
//     the call arrived via the old stdio path or the new bridge path.

import (
	"encoding/json"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/cajasmota/grafel/internal/perf"
	"github.com/cajasmota/grafel/internal/registry"
)

// ── JSON-lines log constants (issue #2299) ────────────────────────────────────
//
// EnvDaemonLogJSON is the environment variable that switches daemon log output
// from human-readable text to structured JSON-lines. Set to "1" or "true" to
// enable; unset or any other value keeps the default human-readable format.
// Handler selection happens at construction time in newService — no runtime
// flag check is needed at call sites (slog cannot be misconfigured this way).
//
//	GRAFEL_DAEMON_LOG_JSON=1  →  {"time":"…","level":"INFO","msg":"mcp_rpc","tool":"…","elapsed_ms":…,"repo":"…"}
const EnvDaemonLogJSON = "GRAFEL_DAEMON_LOG_JSON"

// Log event and field-name constants for the mcp_rpc structured log lines.
// Centralised here so future consumers (log shippers, tests, dashboards)
// do not re-derive the strings independently. These are used as the slog
// message name (LogEventMCPRPC) and attribute key names (LogField*).
const (
	// LogEventMCPRPC is the slog message for every mcp_rpc log line.
	LogEventMCPRPC = "mcp_rpc"

	// LogFieldPhase is the slog attribute key for the dispatch phase ("received" or "done").
	LogFieldPhase = "phase"

	// LogFieldTool is the slog attribute key for the tool name.
	LogFieldTool = "tool"

	// LogFieldElapsedMS is the slog attribute key for elapsed wall-clock time in ms.
	LogFieldElapsedMS = "elapsed_ms"

	// LogFieldRepo is the slog attribute key for the caller's repo / CWD label.
	LogFieldRepo = "repo"

	// LogFieldWireBytes is the slog attribute key for the final on-wire
	// tool-result payload size in bytes (issue #2828). Emitted on the
	// phase=done line. Absent (and parsed as 0) on legacy logs.
	LogFieldWireBytes = "wire_bytes"

	// LogFieldTokenEst is the slog attribute key for the char/4 token
	// estimate of the wire payload (issue #2828). Emitted on phase=done.
	LogFieldTokenEst = "payload_token_estimate"

	// LogFieldTS is the slog attribute key for the RFC3339 timestamp.
	// Note: slog's built-in handler emits its own "time" key; LogFieldTS is
	// retained for compatibility with log-shipper field expectations.
	LogFieldTS = "ts"
)

// ErrDaemonDraining is the retryable error returned by the MCP RPC methods
// when the daemon is shutting down (#5633). The bridge treats this — like a
// connection-shutdown — as a transient condition and reconnects+retries
// against the replacement daemon rather than surfacing it to the MCP client.
// The string is matched by the bridge, so keep it stable.
const ErrDaemonDrainingMsg = "grafel daemon is restarting — retry"

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
	// WireBytes is the size in bytes of the final on-wire tool-result
	// payload (sum of len(TextContent.Text) across Content), measured by
	// the injected MCPCallToolFunc AFTER applyIDInterning so it reflects
	// the real serialized size. Used by `bench-capture rpc` to attribute
	// billed-token cost to daemon-side payload size vs model ingestion
	// (issue #2828 measure-first prerequisite). Zero when not measured.
	WireBytes int
	// TokenEstimate is a rough char/4 estimate of WireBytes, matching the
	// quality-bench skill's token-estimate convention. Approximate (the
	// host tokenizer differs) — treat as a relative lever-finder, not an
	// exact reconciliation against billed input tokens.
	TokenEstimate int
}

// MCPListToolsFunc returns the tool catalog for a given caller cwd (#1769).
// When cwd is empty the function falls back to singleton/explicit-group mode.
// The function returns either the full catalog (cwd matches a registered group)
// or a single sentinel entry (cwd is outside all registered groups).
// Injected from cmd/grafel; nil means "not configured" (bridge returns empty list).
type MCPListToolsFunc func(cwd string) ([]MCPToolEntry, error)

// MCPCallToolFunc dispatches a single tool call. name is the tool name,
// args are the caller's arguments, cwd is the caller's working directory
// (may be empty). Injected from cmd/grafel.
type MCPCallToolFunc func(name string, args map[string]any, cwd string) (MCPCallResult, error)

// ── Wire types ────────────────────────────────────────────────────────────────

// MCPToolListArgs is the argument struct for Daemon.MCPToolList.
// CWD is the caller's working directory forwarded by the bridge so the daemon
// can gate the tool list to the cwd-covered group (#1769).
type MCPToolListArgs struct {
	CWD string `json:"cwd,omitempty"`
}

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

// MCPToolList returns the tool list gated by the caller's cwd (#1769).
// When cwd is inside a registered group the full catalog is returned.
// When cwd is outside all registered groups only the sentinel tool
// (grafel_status) is returned, saving ~2,319 handshake tokens per session.
//
// The list is derived from the injected MCPListTools function (wired from
// *mcp.Server in cmd/grafel), so registerTools() remains the source of
// truth — no duplication.
func (s *Service) MCPToolList(args *MCPToolListArgs, reply *MCPToolListReply) error {
	// #5633: refuse new MCP work once graceful shutdown has begun, and register
	// this call with the drain WaitGroup so Run waits for it before closing the
	// socket. A draining daemon returns a retryable error the bridge handles.
	leave, ok := s.enterMCP()
	if !ok {
		return fmt.Errorf("%s", ErrDaemonDrainingMsg)
	}
	defer leave()

	if s.mcpListTools == nil {
		// Daemon started without MCP wiring (e.g. tests that only test
		// the index/rebuild surface). Return empty rather than an error
		// so the bridge degrades to the offline stub gracefully.
		reply.Tools = []MCPToolInfo{}
		return nil
	}

	cwd := ""
	if args != nil {
		cwd = args.CWD
	}

	entries, err := s.mcpListTools(cwd)
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
	// #5633: refuse new MCP work once graceful shutdown has begun, and register
	// this call with the drain WaitGroup so Run waits for it (bounded) before
	// closing the socket. The retryable error makes the bridge reconnect to the
	// replacement daemon instead of hard-failing the caller.
	leave, ok := s.enterMCP()
	if !ok {
		return fmt.Errorf("%s", ErrDaemonDrainingMsg)
	}
	defer leave()

	if args == nil || args.Name == "" {
		return fmt.Errorf("MCPToolCall: name is required")
	}

	if s.mcpCallTool == nil {
		reply.IsError = true
		reply.Content = []map[string]any{
			{"type": "text", "text": "grafel daemon: MCP tool dispatch not configured — ensure daemon was started via 'grafel install'"},
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
		s.logger.Info(LogEventMCPRPC,
			LogFieldPhase, "received",
			LogFieldTool, args.Name,
			LogFieldRepo, repoLabel,
			LogFieldTS, time.Now().UTC().Format(time.RFC3339),
		)
	}

	start := time.Now()
	result, err := s.callToolRecovered(args.Name, args.Arguments, args.CWD)
	elapsed := time.Since(start)

	// Debug log: tool=name elapsed_ms=X repo=Y (from CWD when available).
	// slog emits structured fields in both text and JSON handler modes —
	// no manual JSON marshalling needed.
	if s.logger != nil {
		s.logger.Info(LogEventMCPRPC,
			LogFieldPhase, "done",
			LogFieldTool, args.Name,
			LogFieldElapsedMS, elapsed.Milliseconds(),
			LogFieldWireBytes, result.WireBytes,
			LogFieldTokenEst, result.TokenEstimate,
			LogFieldRepo, repoLabel,
			LogFieldTS, time.Now().UTC().Format(time.RFC3339),
		)
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

// callToolRecovered invokes s.mcpCallTool with panic recovery (#5717).
//
// s.mcpCallTool is dispatched in-process inside the daemon, and this method
// runs on a per-request goroutine spawned by net/rpc's ServeCodec. net/rpc
// does NOT recover panics raised by service methods, and Go's runtime
// terminates the entire process on an unrecovered panic in any goroutine —
// so a single nil-deref (or any other bug) in ONE tool handler would crash
// the whole daemon and sever every MCP bridge connection currently attached
// to it (#5717). *mcp.Server's wrap() middleware is also hardened with its
// own recover as defense in depth, but this is the last line of defense for
// the daemon-hosted dispatch path: it converts a panicking handler into a
// well-formed error result instead of taking down the process.
//
// The panic message and a (best-effort, size-capped) stack trace are logged
// server-side for diagnosis; only a sanitized, generic message is returned
// to the caller so internal details are not leaked over the wire.
func (s *Service) callToolRecovered(name string, args map[string]any, cwd string) (result MCPCallResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			if s.logger != nil {
				s.logger.Error(LogEventMCPRPC,
					LogFieldPhase, "panic",
					LogFieldTool, name,
					LogFieldRepo, cwd,
					"panic", fmt.Sprintf("%v", r),
					"stack", string(debug.Stack()),
				)
			}
			result = MCPCallResult{
				IsError: true,
				Content: []map[string]any{
					{"type": "text", "text": fmt.Sprintf(
						"grafel daemon: tool %q panicked and was recovered — see daemon log for details", name)},
				},
			}
			err = nil
		}
	}()
	return s.mcpCallTool(name, args, cwd)
}
