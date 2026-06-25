package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/transport"
)

// bridgeCWD returns the best available working-directory hint for the bridge
// process. It is called once at startup and stored on the bridge struct.
// Errors are silently swallowed — an empty cwd is handled gracefully by the
// daemon (falls back to singleton / explicit-group mode).
func bridgeCWD() string {
	cwd, _ := os.Getwd()
	return cwd
}

// newMCPBridgeCmd returns the hidden `grafel mcp-bridge` subcommand.
//
// The bridge is a short-lived stdio process (one per Claude Code session)
// that translates JSON-RPC 2.0 requests from Claude's MCP protocol into
// JSON-RPC 1.0 calls to the daemon's Unix-domain socket, and translates
// replies back.
//
// Wire shape:
//
//	stdin  → newline-delimited JSON-RPC 2.0 requests from the client
//	stdout → newline-delimited JSON-RPC 2.0 responses to the client
//	stderr → diagnostic logging (protocol errors, daemon not running, etc.)
//
// The bridge handles three MCP method families:
//
//   - initialize              — responded to locally (capability handshake)
//   - notifications/…        — acknowledged silently (no response needed)
//   - tools/list             — calls Daemon.MCPToolList on the socket
//   - tools/call             — calls Daemon.MCPToolCall on the socket
//
// When the daemon is not running the bridge returns a structured MCP error
// instead of crashing so the caller sees a clean "daemon not running"
// rather than a dead process.
func newMCPBridgeCmd() *cobra.Command {
	var socketPath string
	cmd := &cobra.Command{
		Use:    "mcp-bridge",
		Hidden: true,
		Short:  "stdio↔socket bridge: translate MCP JSON-RPC 2.0 to daemon JSON-RPC 1.0",
		Long: `mcp-bridge reads MCP (JSON-RPC 2.0) from stdin and forwards each request
to the grafel daemon via its Unix-domain socket (JSON-RPC 1.0).
Responses are translated back and written to stdout.

This command is invoked automatically by Claude Code via the mcpServers
entry written by 'grafel install'. It should not be run directly.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := log.New(os.Stderr, "grafel-mcp-bridge: ", log.LstdFlags)
			b := &bridge{
				logger:     logger,
				socketPath: socketPath,
				startupCWD: bridgeCWD(),
			}
			return b.run(os.Stdin, os.Stdout)
		},
	}
	cmd.Flags().StringVar(&socketPath, "socket", "",
		"override daemon socket path (default: ~/.grafel/sockets/daemon.sock)")
	return cmd
}

// ── JSON-RPC 2.0 wire types ───────────────────────────────────────────────────

type rpc2Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpc2Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpc2Error      `json:"error,omitempty"`
}

type rpc2Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ── MCP-layer types ───────────────────────────────────────────────────────────

// mcpToolInfo is the shape the MCP tools/list result uses.
type mcpToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

// mcpInitializeResult is the fixed capability handshake response.
type mcpInitializeResult struct {
	ProtocolVersion string            `json:"protocolVersion"`
	Capabilities    map[string]any    `json:"capabilities"`
	ServerInfo      map[string]string `json:"serverInfo"`
}

// mcpToolCallResult wraps the daemon's reply for the tools/call response.
type mcpToolCallResult struct {
	Content []map[string]any `json:"content"`
	IsError bool             `json:"isError,omitempty"`
}

// ── Daemon RPC types ──────────────────────────────────────────────────────────

// MCPToolListArgs / MCPToolListReply are the wire types for Daemon.MCPToolList.
// CWD is the bridge's startup working directory, forwarded so the daemon can
// gate the tool list to the cwd-covered group (#1769).
type MCPToolListArgs struct {
	CWD string `json:"cwd,omitempty"`
}

type MCPToolListReply struct {
	Tools []mcpToolInfo `json:"tools"`
}

// MCPToolCallArgs / MCPToolCallReply are the wire types for Daemon.MCPToolCall.
// This must stay in sync with daemon.MCPToolCallArgs (internal/daemon/mcp_rpc.go).
type MCPToolCallArgs struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
	// CWD is the caller's working directory forwarded to the daemon for
	// ADR-0008 CWD-aware group inference (#1661/#1679).
	CWD string `json:"cwd,omitempty"`
}

type MCPToolCallReply struct {
	Content []map[string]any `json:"content"`
	IsError bool             `json:"is_error,omitempty"`
}

// ── Bridge ────────────────────────────────────────────────────────────────────

type bridge struct {
	logger     *log.Logger
	socketPath string

	// startupCWD is the working directory of the bridge process at startup,
	// used as the fallback CWD when the MCP request does not include a
	// _meta.cwd hint. Since mcp-bridge is spawned by the MCP client inside
	// the user's project directory, this matches the user's effective cwd
	// for the Claude Code session.
	startupCWD string

	// callCount is used for integration test liveness only.
	callCount int64

	// rpcMu guards the lazy-init, single-flight reuse of rpcClient. The bridge
	// dials the daemon once per session and reuses the same jsonrpc.Client for
	// every tools/list and tools/call. net/rpc serialises calls per client, so
	// no extra locking is needed around Call itself — rpcMu only guards the
	// (re)connect lifecycle.
	rpcMu     sync.Mutex
	rpcClient *rpc.Client
}

// getRPCClient returns a cached jsonrpc client, dialling the daemon on first
// use and after a previous transport error. The returned client is shared
// across calls — net/rpc serialises in-flight requests per client.
//
// Perf rationale (#1671): the pre-#1671 bridge dialled the unix socket and
// constructed a fresh jsonrpc.Client on every tools/call, then closed both on
// return. Profiling showed ~300µs per call burnt on dial + codec setup with
// no useful side-effect. Reusing one client eliminates that overhead and lets
// the OS keep the socket's send/recv buffers hot.
func (b *bridge) getRPCClient() (*rpc.Client, error) {
	b.rpcMu.Lock()
	defer b.rpcMu.Unlock()
	if b.rpcClient != nil {
		return b.rpcClient, nil
	}
	socketPath, err := b.defaultSocketPath()
	if err != nil {
		return nil, err
	}
	// transport.Dial selects the OS-appropriate transport: AF_UNIX on
	// Linux/mac (identical to the previous net.Dial("unix", ...)) and a named
	// pipe on Windows, matching transport.Listen on the daemon side. Hardcoding
	// "unix" here meant the bridge could never reach the daemon on Windows.
	conn, err := transport.Dial(socketPath)
	if err != nil {
		return nil, err
	}
	b.rpcClient = jsonrpc.NewClient(conn)
	return b.rpcClient, nil
}

// resetRPCClient drops the cached client. Call this after a transport error so
// the next request reconnects rather than reusing a dead socket.
func (b *bridge) resetRPCClient() {
	b.rpcMu.Lock()
	if b.rpcClient != nil {
		_ = b.rpcClient.Close()
		b.rpcClient = nil
	}
	b.rpcMu.Unlock()
}

// bridgeMaxRetries is the number of additional attempts the bridge makes for a
// single daemon RPC after a transient connection-shutdown (#5633). A daemon
// restart drops the socket mid-call; the bridge reconnects to the replacement
// daemon and retries rather than surfacing the failure to the MCP client. All
// grafel MCP tools are read-mostly graph queries, so retrying is safe — there
// is no non-idempotent call to special-case.
const bridgeMaxRetries = 3

// bridgeRetryBackoff is the pause between reconnect attempts, giving the
// replacement daemon a brief window to (re)bind its socket after a restart.
var bridgeRetryBackoff = 150 * time.Millisecond

// isRetryableRPCErr reports whether a daemon RPC error is a transient
// connection-shutdown / restart condition the bridge should reconnect+retry on,
// rather than a genuine tool/protocol failure. Covers:
//   - rpc.ErrShutdown        — the cached client's connection was torn down
//   - io.EOF / ErrUnexpectedEOF — the daemon closed the socket mid-read
//   - the daemon's "restarting — retry" drain sentinel (#5633), which arrives
//     as a plain error string over the wire (net/rpc collapses the typed error
//     to ServerError(text)), so we string-match it.
func isRetryableRPCErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, rpc.ErrShutdown) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	return strings.Contains(err.Error(), daemon.ErrDaemonDrainingMsg)
}

// callDaemon invokes a daemon RPC method with reconnect+retry on transient
// connection-shutdown (#5633). On a retryable error it drops the dead client,
// backs off briefly, reconnects, and retries up to bridgeMaxRetries times. A
// non-retryable error (a real tool/protocol failure) is returned immediately.
// The final error after exhausting retries is returned to the caller, which
// renders it as a structured MCP error.
//
// reply must be a pointer; it is reused across attempts (net/rpc overwrites it
// on each Call, and a failed Call leaves it at its zero value).
func (b *bridge) callDaemon(method string, args, reply any) error {
	var lastErr error
	for attempt := 0; attempt <= bridgeMaxRetries; attempt++ {
		if attempt > 0 {
			// Transient failure on the previous attempt: drop the dead client
			// so getRPCClient redials, and pause so a restarting daemon has a
			// moment to rebind its socket.
			b.resetRPCClient()
			time.Sleep(bridgeRetryBackoff)
			b.log("retrying %s after transient error (attempt %d/%d): %v",
				method, attempt, bridgeMaxRetries, lastErr)
		}
		rpcClient, err := b.getRPCClient()
		if err != nil {
			// Could not even dial — treat as retryable (the replacement daemon
			// may not have rebound yet) but remember the error.
			lastErr = err
			continue
		}
		callErr := rpcClient.Call(method, args, reply)
		if callErr == nil {
			return nil
		}
		lastErr = callErr
		if !isRetryableRPCErr(callErr) {
			// Genuine failure (bad args, tool error, unknown method) — do not
			// retry; surface it to the caller immediately.
			return callErr
		}
	}
	return lastErr
}

// closeRPCClient is the shutdown counterpart to getRPCClient. Safe to call
// multiple times.
func (b *bridge) closeRPCClient() {
	b.resetRPCClient()
}

// defaultSocketPath returns the daemon's default socket path.
func (b *bridge) defaultSocketPath() (string, error) {
	if b.socketPath != "" {
		return b.socketPath, nil
	}
	layout, err := daemon.DefaultLayout()
	if err != nil {
		return "", err
	}
	return layout.SocketPath, nil
}

// run is the main loop: reads from r (stdin), writes to w (stdout).
//
// Perf rationale (#1671):
//   - Pre-#1671 the loop used bufio.Scanner with a 4MiB hard cap. MCP responses
//     for big calls (expand d=2 has been observed at 1.3 MiB; future graph_export
//     could exceed 4 MiB) would silently truncate. bufio.Reader.ReadBytes('\n')
//     has no fixed cap and grows as needed.
//   - The encoder is wrapped in a bufio.Writer sized for typical MCP payloads
//     so large responses flush in one syscall instead of dribbling out through
//     the default stdio buffer.
//   - The cached jsonrpc client (getRPCClient) is closed on exit so the daemon
//     sees a clean disconnect.
func (b *bridge) run(r io.Reader, w io.Writer) error {
	defer b.closeRPCClient()

	// #5633: guarantee exactly one bridge per daemon socket. Reap an orphaned
	// prior bridge (e.g. one left attached after a daemon restart) and claim a
	// per-socket pidfile. Best-effort: a failure here must not stop us serving,
	// so we log and continue. Skipped when the socket path cannot be resolved.
	if socketPath, serr := b.defaultSocketPath(); serr == nil && socketPath != "" {
		if release, aerr := acquireBridgeSingleton(socketPath, b.log); aerr != nil {
			b.log("bridge singleton: %v (continuing)", aerr)
		} else {
			defer release()
		}
	}

	br := bufio.NewReaderSize(r, 64*1024)
	bw := bufio.NewWriterSize(w, 64*1024)
	enc := json.NewEncoder(bw)

	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			// Trim trailing newline (and optional \r) before parsing.
			trimmed := line
			for len(trimmed) > 0 && (trimmed[len(trimmed)-1] == '\n' || trimmed[len(trimmed)-1] == '\r') {
				trimmed = trimmed[:len(trimmed)-1]
			}
			if len(trimmed) > 0 {
				if perr := b.processLine(trimmed, enc); perr != nil {
					b.log("write error: %v", perr)
					_ = bw.Flush()
					return perr
				}
				if ferr := bw.Flush(); ferr != nil {
					b.log("flush error: %v", ferr)
					return ferr
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("stdin: %w", err)
		}
	}
}

// processLine parses one JSON-RPC 2.0 request and writes the response (if any)
// through enc. It does NOT flush — the run loop batches the flush so large
// payloads still go out in one go but multiple notifications would not pay
// per-message flush cost.
func (b *bridge) processLine(line []byte, enc *json.Encoder) error {
	var req rpc2Request
	if err := json.Unmarshal(line, &req); err != nil {
		b.log("parse error: %v", err)
		return enc.Encode(rpc2Response{
			JSONRPC: "2.0",
			Error:   &rpc2Error{Code: -32700, Message: "parse error: " + err.Error()},
		})
	}
	resp := b.handle(req)
	if resp == nil {
		// Notification — no response needed.
		return nil
	}
	if err := enc.Encode(resp); err != nil {
		return err
	}
	atomic.AddInt64(&b.callCount, 1)
	return nil
}

// log is a nil-safe logger call.
func (b *bridge) log(format string, args ...any) {
	if b.logger != nil {
		b.logger.Printf(format, args...)
	}
}

// handle dispatches a single JSON-RPC 2.0 request and returns the response.
// Returns nil for notifications (no-response methods).
func (b *bridge) handle(req rpc2Request) *rpc2Response {
	switch req.Method {
	case "initialize":
		return b.handleInitialize(req)
	case "notifications/initialized", "notifications/cancelled":
		// Notifications — acknowledge silently.
		return nil
	case "tools/list":
		return b.handleToolsList(req)
	case "tools/call":
		return b.handleToolsCall(req)
	default:
		b.log("unknown method: %s", req.Method)
		return &rpc2Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpc2Error{Code: -32601, Message: "method not found: " + req.Method},
		}
	}
}

// handleInitialize returns the MCP capability handshake. The bridge handles
// this locally — the daemon is not involved. This also avoids a race where
// the daemon might not be running yet at the moment Claude Code starts.
func (b *bridge) handleInitialize(req rpc2Request) *rpc2Response {
	result := mcpInitializeResult{
		ProtocolVersion: "2024-11-05",
		Capabilities: map[string]any{
			"tools": map[string]any{},
		},
		ServerInfo: map[string]string{
			"name":    "grafel",
			"version": "1.0",
		},
	}
	raw, _ := json.Marshal(result)
	return &rpc2Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  json.RawMessage(raw),
	}
}

// handleToolsList proxies the tools/list call to the daemon.
// The bridge's startup cwd is forwarded so the daemon can gate the tool list
// to the cwd-covered group (#1769): sessions outside all registered groups
// receive only the sentinel tool (grafel_status) instead of the full list.
// Falls back to a static minimal tool catalog when the daemon is unreachable
// so Claude Code always sees _some_ tools and can display a useful error.
func (b *bridge) handleToolsList(req rpc2Request) *rpc2Response {
	var reply MCPToolListReply
	if err := b.callDaemon("Daemon.MCPToolList", MCPToolListArgs{CWD: b.startupCWD}, &reply); err != nil {
		b.log("Daemon.MCPToolList: %v", err)
		// callDaemon already reconnected+retried on transient
		// connection-shutdown (#5633). A persistent failure here means the
		// daemon is unreachable or pre-Phase D — return the static list so
		// Claude Code keeps working in the interim.
		return b.offlineToolList(req.ID)
	}

	result := map[string]any{"tools": reply.Tools}
	raw, _ := json.Marshal(result)
	return &rpc2Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  json.RawMessage(raw),
	}
}

// handleToolsCall proxies the tools/call call to the daemon.
func (b *bridge) handleToolsCall(req rpc2Request) *rpc2Response {
	// Decode the call params.
	//
	// _meta is the MCP extension envelope (MCP spec §6.5). Claude Code may
	// include a cwd hint there so the daemon can infer the active group
	// without requiring an explicit group= argument (ADR-0008 / #1661 / #1679).
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
		Meta      struct {
			CWD string `json:"cwd,omitempty"`
		} `json:"_meta,omitempty"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return b.errorResp(req.ID, -32602, "invalid params: "+err.Error())
		}
	}
	if params.Name == "" {
		return b.errorResp(req.ID, -32602, "tools/call: name is required")
	}

	// Resolve effective CWD for group inference (ADR-0008 / #1679).
	//   1. Use _meta.cwd from the MCP request if the client provided one.
	//   2. Fall back to the bridge process's startup working directory, which
	//      is set by the MCP client to the user's project directory when it
	//      spawns the bridge.
	//   3. Leave empty if neither is available — the daemon gracefully falls
	//      back to singleton / explicit-group mode.
	cwd := params.Meta.CWD
	if cwd == "" {
		cwd = b.startupCWD
	}

	// Fast path: if we cannot even dial the daemon, surface the clean
	// "daemon not running" guidance rather than a retry storm.
	if _, err := b.getRPCClient(); err != nil {
		b.log("daemon not reachable (%v)", err)
		return b.daemonError(req.ID, "grafel daemon is not running — run 'grafel start' or 'grafel install'")
	}

	args := MCPToolCallArgs{
		Name:      params.Name,
		Arguments: params.Arguments,
		CWD:       cwd,
	}
	var reply MCPToolCallReply
	// callDaemon reconnects+retries on a transient connection-shutdown so a
	// daemon restart mid-call does not hard-fail the MCP client (#5633). Only a
	// persistent failure (or a genuine tool/protocol error) reaches here.
	if err := b.callDaemon("Daemon.MCPToolCall", args, &reply); err != nil {
		b.log("Daemon.MCPToolCall %s: %v", params.Name, err)
		// Return a structured MCP tool error so Claude sees the message.
		toolErr := mcpToolCallResult{
			IsError: true,
			Content: []map[string]any{
				{"type": "text", "text": "grafel daemon error: " + err.Error()},
			},
		}
		raw, _ := json.Marshal(toolErr)
		return &rpc2Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(raw),
		}
	}

	toolResult := mcpToolCallResult{
		Content: reply.Content,
		IsError: reply.IsError,
	}
	if toolResult.Content == nil {
		toolResult.Content = []map[string]any{}
	}
	raw, _ := json.Marshal(toolResult)
	return &rpc2Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  json.RawMessage(raw),
	}
}

// offlineToolList returns a static minimal catalog for when the daemon is
// not running. The tool list tells Claude Code that grafel is installed
// but the daemon is offline — users can call grafel_whoami to get the
// actionable error.
func (b *bridge) offlineToolList(id any) *rpc2Response {
	stub := []mcpToolInfo{
		{
			Name:        "grafel_whoami",
			Description: "Return grafel status. NOTE: daemon is currently offline — run 'grafel start'.",
			// Always carry a valid JSON Schema, even in the degraded/offline
			// path. Without this the field is omitted (omitempty) and Claude
			// Code's Zod validation rejects the tool with a cryptic
			// "expected object, received undefined" on inputSchema — masking
			// the real cause (the daemon was unreachable).
			InputSchema: json.RawMessage(`{"type":"object"}`),
		},
	}
	result := map[string]any{"tools": stub}
	raw, _ := json.Marshal(result)
	return &rpc2Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  json.RawMessage(raw),
	}
}

// daemonError wraps a daemon connectivity error as a JSON-RPC error response.
func (b *bridge) daemonError(id any, msg string) *rpc2Response {
	return b.errorResp(id, -32000, msg)
}

// errorResp builds a JSON-RPC 2.0 error response.
func (b *bridge) errorResp(id any, code int, msg string) *rpc2Response {
	return &rpc2Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpc2Error{Code: code, Message: msg},
	}
}
