package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/rpc/jsonrpc"
	"os"
	"sync/atomic"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/daemon"
)

// newMCPBridgeCmd returns the hidden `archigraph mcp-bridge` subcommand.
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
to the archigraph daemon via its Unix-domain socket (JSON-RPC 1.0).
Responses are translated back and written to stdout.

This command is invoked automatically by Claude Code via the mcpServers
entry written by 'archigraph install'. It should not be run directly.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := log.New(os.Stderr, "archigraph-mcp-bridge: ", log.LstdFlags)
			b := &bridge{
				logger:     logger,
				socketPath: socketPath,
			}
			return b.run(os.Stdin, os.Stdout)
		},
	}
	cmd.Flags().StringVar(&socketPath, "socket", "",
		"override daemon socket path (default: ~/.archigraph/sockets/daemon.sock)")
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
	ProtocolVersion string              `json:"protocolVersion"`
	Capabilities    map[string]any      `json:"capabilities"`
	ServerInfo      map[string]string   `json:"serverInfo"`
}

// mcpToolCallResult wraps the daemon's reply for the tools/call response.
type mcpToolCallResult struct {
	Content []map[string]any `json:"content"`
	IsError bool             `json:"isError,omitempty"`
}

// ── Daemon RPC types ──────────────────────────────────────────────────────────

// MCPToolListArgs / MCPToolListReply are the wire types for Daemon.MCPToolList.
type MCPToolListArgs struct{}

type MCPToolListReply struct {
	Tools []mcpToolInfo `json:"tools"`
}

// MCPToolCallArgs / MCPToolCallReply are the wire types for Daemon.MCPToolCall.
type MCPToolCallArgs struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type MCPToolCallReply struct {
	Content []map[string]any `json:"content"`
	IsError bool             `json:"is_error,omitempty"`
}

// ── Bridge ────────────────────────────────────────────────────────────────────

type bridge struct {
	logger     *log.Logger
	socketPath string

	// callCount is used for integration test liveness only.
	callCount int64
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
func (b *bridge) run(r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	// Expand the scanner buffer to handle large MCP messages.
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	enc := json.NewEncoder(w)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req rpc2Request
		if err := json.Unmarshal(line, &req); err != nil {
			b.log("parse error: %v", err)
			// Write a parse error response. ID may be unknown; use null.
			_ = enc.Encode(rpc2Response{
				JSONRPC: "2.0",
				Error:   &rpc2Error{Code: -32700, Message: "parse error: " + err.Error()},
			})
			continue
		}

		resp := b.handle(req)
		if resp == nil {
			// Notification — no response needed.
			continue
		}
		if err := enc.Encode(resp); err != nil {
			b.log("write error: %v", err)
			return err
		}
		atomic.AddInt64(&b.callCount, 1)
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		return fmt.Errorf("stdin: %w", err)
	}
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
			"name":    "archigraph",
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
// Falls back to a static minimal tool catalog when the daemon is unreachable
// so Claude Code always sees _some_ tools and can display a useful error.
func (b *bridge) handleToolsList(req rpc2Request) *rpc2Response {
	socketPath, err := b.defaultSocketPath()
	if err != nil {
		return b.daemonError(req.ID, "resolve socket path: "+err.Error())
	}

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		b.log("daemon not running (%v); returning offline stub for tools/list", err)
		return b.offlineToolList(req.ID)
	}
	defer conn.Close()

	rpcClient := jsonrpc.NewClient(conn)
	defer rpcClient.Close()

	var reply MCPToolListReply
	if err := rpcClient.Call("Daemon.MCPToolList", MCPToolListArgs{}, &reply); err != nil {
		b.log("Daemon.MCPToolList: %v", err)
		// Daemon is running but doesn't implement MCPToolList yet (pre-Phase D).
		// Return the static list so Claude Code works in the interim.
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
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return b.errorResp(req.ID, -32602, "invalid params: "+err.Error())
		}
	}
	if params.Name == "" {
		return b.errorResp(req.ID, -32602, "tools/call: name is required")
	}

	socketPath, err := b.defaultSocketPath()
	if err != nil {
		return b.daemonError(req.ID, "resolve socket path: "+err.Error())
	}

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		b.log("daemon not running (%v)", err)
		return b.daemonError(req.ID, "archigraph daemon is not running — run 'archigraph start' or 'archigraph install'")
	}
	defer conn.Close()

	rpcClient := jsonrpc.NewClient(conn)
	defer rpcClient.Close()

	args := MCPToolCallArgs{
		Name:      params.Name,
		Arguments: params.Arguments,
	}
	var reply MCPToolCallReply
	if err := rpcClient.Call("Daemon.MCPToolCall", args, &reply); err != nil {
		b.log("Daemon.MCPToolCall %s: %v", params.Name, err)
		// Return a structured MCP tool error so Claude sees the message.
		toolErr := mcpToolCallResult{
			IsError: true,
			Content: []map[string]any{
				{"type": "text", "text": "archigraph daemon error: " + err.Error()},
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
// not running. The tool list tells Claude Code that archigraph is installed
// but the daemon is offline — users can call archigraph_whoami to get the
// actionable error.
func (b *bridge) offlineToolList(id any) *rpc2Response {
	stub := []mcpToolInfo{
		{
			Name:        "archigraph_whoami",
			Description: "Return archigraph status. NOTE: daemon is currently offline — run 'archigraph start'.",
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
