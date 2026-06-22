package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon/transport"
)

// mockDaemonService is a minimal RPC service that stands in for the real
// daemon during bridge unit tests. It implements the two MCP-facing
// methods the bridge can call.
//
// lastCWD captures the CWD field from the most recent MCPToolCall invocation
// so tests can assert it was forwarded correctly (#1679).
type mockDaemonService struct {
	lastCWD string
}

func (m *mockDaemonService) MCPToolList(_ *MCPToolListArgs, reply *MCPToolListReply) error {
	reply.Tools = []mcpToolInfo{
		{Name: "grafel_whoami", Description: "test tool"},
	}
	return nil
}

func (m *mockDaemonService) MCPToolCall(args *MCPToolCallArgs, reply *MCPToolCallReply) error {
	m.lastCWD = args.CWD
	reply.Content = []map[string]any{
		{"type": "text", "text": "called: " + args.Name},
	}
	return nil
}

// startMockDaemon starts a net/rpc JSON-RPC 1.0 server on the
// platform-appropriate IPC transport (Unix-domain socket on Linux/macOS, named
// pipe on Windows) and returns the listen address and the mock service (for
// inspecting captured fields after calls). The caller must invoke stop() to
// clean up.
//
// The mock MUST listen via the same transport abstraction the production bridge
// dials with (transport.Dial in getRPCClient). Previously it listened on a raw
// AF_UNIX socket while the bridge dialed a named pipe on Windows, so the two
// never connected and the tools/call tests failed on Windows CI (test.yml
// regression introduced by the transport.Dial switch in #5320). Going through
// transport.Listen/transport.Dial keeps listen and dial in agreement on every
// OS.
//
// On Unix, macOS caps socket paths at 104 bytes so we use os.MkdirTemp (shorter
// names than t.TempDir()) to avoid EINVAL on long test names. On Windows the
// address is a named-pipe path of the form \\.\pipe\<name>, which has no
// path-length restriction; we make it unique per call via stubPipeSeq.
func startMockDaemon(t *testing.T) (socketPath string, mock *mockDaemonService, stop func()) {
	t.Helper()

	var tmp string
	if runtime.GOOS == "windows" {
		socketPath = fmt.Sprintf(`\\.\pipe\agbr-%d`, stubPipeSeq(t))
	} else {
		var err error
		tmp, err = os.MkdirTemp("", "agbr")
		if err != nil {
			t.Fatalf("MkdirTemp: %v", err)
		}
		socketPath = filepath.Join(tmp, "d.sock")
	}

	mock = &mockDaemonService{}
	srv := rpc.NewServer()
	if err := srv.RegisterName("Daemon", mock); err != nil {
		t.Fatalf("register mock: %v", err)
	}

	l, err := transport.Listen(socketPath)
	if err != nil {
		t.Fatalf("listen %s: %v", socketPath, err)
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
		if tmp != "" {
			os.RemoveAll(tmp)
		}
	}
	return socketPath, mock, stop
}

// roundTrip sends a JSON-RPC 2.0 request through the bridge (backed by the
// mock daemon) and returns the decoded response. startupCWD is injected into
// the bridge for tests that assert CWD forwarding behaviour (#1679).
func roundTrip(t *testing.T, socketPath, method string, params any, startupCWD string) rpc2Response {
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
	b := &bridge{socketPath: socketPath, startupCWD: startupCWD}
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
	socketPath, _, stop := startMockDaemon(t)
	defer stop()

	resp := roundTrip(t, socketPath, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
	}, "")

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
	if result.ServerInfo["name"] != "grafel" {
		t.Fatalf("server name: %q", result.ServerInfo["name"])
	}
}

func TestBridge_ToolsList(t *testing.T) {
	socketPath, _, stop := startMockDaemon(t)
	defer stop()

	resp := roundTrip(t, socketPath, "tools/list", nil, "")

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
	if result.Tools[0].Name != "grafel_whoami" {
		t.Fatalf("first tool name: %q", result.Tools[0].Name)
	}
}

func TestBridge_ToolsCall(t *testing.T) {
	socketPath, _, stop := startMockDaemon(t)
	defer stop()

	resp := roundTrip(t, socketPath, "tools/call", map[string]any{
		"name":      "grafel_whoami",
		"arguments": map[string]any{"cwd": "/tmp"},
	}, "")

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
	if !strings.Contains(text, "grafel_whoami") {
		t.Fatalf("unexpected tool call content: %q", text)
	}
}

// unreachableAddr returns a transport-valid address that nothing is listening
// on, so transport.Dial fails fast and the bridge falls back to its offline
// stub. On Unix this is a nonexistent socket path; on Windows it is a
// named-pipe path with no listener (a Unix-style path is not a valid pipe
// address, so we must use the pipe form to exercise the real "daemon
// unreachable" path rather than an address-parse error).
func unreachableAddr() string {
	if runtime.GOOS == "windows" {
		return `\\.\pipe\agbr-nonexistent`
	}
	return "/nonexistent/daemon.sock"
}

func TestBridge_DaemonNotRunning_ToolsList(t *testing.T) {
	b := &bridge{socketPath: unreachableAddr()}
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
	b := &bridge{socketPath: unreachableAddr()}
	params, _ := json.Marshal(map[string]any{"name": "grafel_whoami"})
	req := rpc2Request{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: params}
	resp := b.handle(req)
	// Should return a JSON-RPC error.
	if resp.Error == nil {
		t.Fatal("expected error when daemon is not running for tools/call")
	}
}

func TestBridge_Notification_NoResponse(t *testing.T) {
	b := &bridge{socketPath: unreachableAddr()}
	req := rpc2Request{JSONRPC: "2.0", Method: "notifications/initialized"}
	resp := b.handle(req)
	if resp != nil {
		t.Fatalf("notification should not produce a response, got: %+v", resp)
	}
}

func TestBridge_UnknownMethod(t *testing.T) {
	b := &bridge{socketPath: unreachableAddr()}
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
	socketPath, _, stop := startMockDaemon(t)
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

// TestBridge_ToolsCall_CWDFromMetaHint asserts that when the MCP request
// carries a _meta.cwd hint the bridge forwards it verbatim to the daemon's
// MCPToolCall RPC (Fixes #1679).
func TestBridge_ToolsCall_CWDFromMetaHint(t *testing.T) {
	socketPath, mock, stop := startMockDaemon(t)
	defer stop()

	const wantCWD = "/home/user/myproject"
	resp := roundTrip(t, socketPath, "tools/call", map[string]any{
		"name":      "grafel_whoami",
		"arguments": map[string]any{},
		"_meta":     map[string]any{"cwd": wantCWD},
	}, "/other/dir") // startupCWD should be ignored when _meta.cwd is present

	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if mock.lastCWD != wantCWD {
		t.Errorf("CWD not forwarded from _meta.cwd: got %q, want %q", mock.lastCWD, wantCWD)
	}
}

// TestBridge_ToolsCall_CWDFromStartup asserts that when no _meta.cwd hint is
// present the bridge falls back to its startup working directory (#1679).
func TestBridge_ToolsCall_CWDFromStartup(t *testing.T) {
	socketPath, mock, stop := startMockDaemon(t)
	defer stop()

	const wantCWD = "/home/user/projects/myrepo"
	resp := roundTrip(t, socketPath, "tools/call", map[string]any{
		"name":      "grafel_whoami",
		"arguments": map[string]any{},
	}, wantCWD)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if mock.lastCWD != wantCWD {
		t.Errorf("CWD not forwarded from startupCWD: got %q, want %q", mock.lastCWD, wantCWD)
	}
}

// TestBridge_ToolsCall_CWDEmpty asserts that when neither _meta.cwd nor a
// startupCWD is available the bridge sends an empty CWD (daemon falls back to
// singleton / explicit-group mode) without erroring (#1679).
func TestBridge_ToolsCall_CWDEmpty(t *testing.T) {
	socketPath, mock, stop := startMockDaemon(t)
	defer stop()

	resp := roundTrip(t, socketPath, "tools/call", map[string]any{
		"name": "grafel_whoami",
	}, "") // no startupCWD and no _meta.cwd

	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if mock.lastCWD != "" {
		t.Errorf("expected empty CWD, got %q", mock.lastCWD)
	}
}
