package cli

import (
	"bytes"
	"encoding/json"
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"strings"
	"testing"
)

// mockDaemonService is a minimal RPC service that stands in for the real
// daemon during bridge unit tests. It implements the two MCP-facing
// methods the bridge can call.
type mockDaemonService struct{}

func (m *mockDaemonService) MCPToolList(_ *MCPToolListArgs, reply *MCPToolListReply) error {
	reply.Tools = []mcpToolInfo{
		{Name: "archigraph_whoami", Description: "test tool"},
	}
	return nil
}

func (m *mockDaemonService) MCPToolCall(args *MCPToolCallArgs, reply *MCPToolCallReply) error {
	reply.Content = []map[string]any{
		{"type": "text", "text": "called: " + args.Name},
	}
	return nil
}

// startMockDaemon starts a net/rpc JSON-RPC 1.0 server on a temp Unix socket
// and returns the socket path. The caller must close the returned net.Listener.
func startMockDaemon(t *testing.T) (socketPath string, stop func()) {
	t.Helper()
	tmp := t.TempDir()
	socketPath = tmp + "/daemon.sock"

	srv := rpc.NewServer()
	if err := srv.RegisterName("Daemon", &mockDaemonService{}); err != nil {
		t.Fatalf("register mock: %v", err)
	}

	l, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go srv.ServeCodec(jsonrpc.NewServerCodec(conn))
		}
	}()

	stop = func() {
		l.Close()
		<-done
	}
	return socketPath, stop
}

// roundTrip sends a JSON-RPC 2.0 request through the bridge (backed by the
// mock daemon) and returns the decoded response.
func roundTrip(t *testing.T, socketPath, method string, params any) rpc2Response {
	t.Helper()

	var paramsRaw json.RawMessage
	if params != nil {
		b, _ := json.Marshal(params)
		paramsRaw = b
	}

	req := rpc2Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  method,
		Params:  paramsRaw,
	}
	reqBytes, _ := json.Marshal(req)
	reqBytes = append(reqBytes, '\n')

	var out bytes.Buffer
	b := &bridge{socketPath: socketPath}
	if err := b.run(bytes.NewReader(reqBytes), &out); err != nil {
		t.Fatalf("bridge.run: %v", err)
	}

	var resp rpc2Response
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatalf("decode response: %v (raw: %s)", err, out.String())
	}
	return resp
}

func TestBridge_Initialize(t *testing.T) {
	socketPath, stop := startMockDaemon(t)
	defer stop()

	resp := roundTrip(t, socketPath, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
	})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	var result mcpInitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.ProtocolVersion == "" {
		t.Fatal("protocolVersion missing from initialize response")
	}
	if _, ok := result.Capabilities["tools"]; !ok {
		t.Fatal("tools capability missing from initialize response")
	}
	if result.ServerInfo["name"] != "archigraph" {
		t.Fatalf("server name: %q", result.ServerInfo["name"])
	}
}

func TestBridge_ToolsList(t *testing.T) {
	socketPath, stop := startMockDaemon(t)
	defer stop()

	resp := roundTrip(t, socketPath, "tools/list", nil)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	var result struct {
		Tools []mcpToolInfo `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if len(result.Tools) == 0 {
		t.Fatal("expected at least one tool in tools/list response")
	}
	if result.Tools[0].Name != "archigraph_whoami" {
		t.Fatalf("first tool name: %q", result.Tools[0].Name)
	}
}

func TestBridge_ToolsCall(t *testing.T) {
	socketPath, stop := startMockDaemon(t)
	defer stop()

	resp := roundTrip(t, socketPath, "tools/call", map[string]any{
		"name":      "archigraph_whoami",
		"arguments": map[string]any{"cwd": "/tmp"},
	})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	var result mcpToolCallResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if len(result.Content) == 0 {
		t.Fatal("expected content in tools/call response")
	}
	text, _ := result.Content[0]["text"].(string)
	if !strings.Contains(text, "archigraph_whoami") {
		t.Fatalf("unexpected tool call content: %q", text)
	}
}

func TestBridge_DaemonNotRunning_ToolsList(t *testing.T) {
	b := &bridge{socketPath: "/nonexistent/daemon.sock"}
	req := rpc2Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"}
	resp := b.handle(req)
	// Should return a stub (not an error) — offline mode.
	if resp.Error != nil {
		t.Fatalf("expected offline stub, got error: %+v", resp.Error)
	}
	var result struct {
		Tools []mcpToolInfo `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode offline stub: %v", err)
	}
	if len(result.Tools) == 0 {
		t.Fatal("offline stub should have at least one tool entry")
	}
}

func TestBridge_DaemonNotRunning_ToolsCall(t *testing.T) {
	b := &bridge{socketPath: "/nonexistent/daemon.sock"}
	params, _ := json.Marshal(map[string]any{"name": "archigraph_whoami"})
	req := rpc2Request{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: params}
	resp := b.handle(req)
	// Should return a JSON-RPC error.
	if resp.Error == nil {
		t.Fatal("expected error when daemon is not running for tools/call")
	}
}

func TestBridge_Notification_NoResponse(t *testing.T) {
	b := &bridge{socketPath: "/nonexistent/daemon.sock"}
	req := rpc2Request{JSONRPC: "2.0", Method: "notifications/initialized"}
	resp := b.handle(req)
	if resp != nil {
		t.Fatalf("notification should not produce a response, got: %+v", resp)
	}
}

func TestBridge_UnknownMethod(t *testing.T) {
	b := &bridge{socketPath: "/nonexistent/daemon.sock"}
	req := rpc2Request{JSONRPC: "2.0", ID: 1, Method: "unknown/method"}
	resp := b.handle(req)
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Fatalf("expected method-not-found code -32601, got %d", resp.Error.Code)
	}
}

func TestBridge_MultipleRequests(t *testing.T) {
	socketPath, stop := startMockDaemon(t)
	defer stop()

	// Send two requests in one stdin stream.
	req1, _ := json.Marshal(rpc2Request{JSONRPC: "2.0", ID: 1, Method: "initialize"})
	req2, _ := json.Marshal(rpc2Request{JSONRPC: "2.0", ID: 2, Method: "tools/list"})
	input := string(req1) + "\n" + string(req2) + "\n"

	var out bytes.Buffer
	b := &bridge{socketPath: socketPath}
	if err := b.run(strings.NewReader(input), &out); err != nil {
		t.Fatalf("bridge.run: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 response lines, got %d: %q", len(lines), out.String())
	}

	for i, line := range lines {
		var resp rpc2Response
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("decode line %d: %v", i, err)
		}
		if resp.Error != nil {
			t.Fatalf("line %d error: %+v", i, resp.Error)
		}
	}
}
