package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/transport"
)

func init() {
	// Keep reconnect backoff tiny so retry tests stay fast.
	bridgeRetryBackoff = 1 * time.Millisecond
}

// TestIsRetryableRPCErr covers the classifier that decides whether the bridge
// reconnects+retries vs surfaces an error (#5633).
func TestIsRetryableRPCErr(t *testing.T) {
	retryable := []error{
		rpc.ErrShutdown,
		io.EOF,
		io.ErrUnexpectedEOF,
		// The daemon's drain sentinel arrives as a plain ServerError string.
		errors.New("some prefix: " + daemon.ErrDaemonDrainingMsg),
	}
	for _, e := range retryable {
		if !isRetryableRPCErr(e) {
			t.Errorf("isRetryableRPCErr(%v) = false, want true", e)
		}
	}
	notRetryable := []error{
		nil,
		errors.New("tool error: no such entity"),
		errors.New("invalid params"),
	}
	for _, e := range notRetryable {
		if isRetryableRPCErr(e) {
			t.Errorf("isRetryableRPCErr(%v) = true, want false", e)
		}
	}
}

// flakyDaemon is a mock that fails the first N tools/call invocations with a
// connection-shutdown (simulating a daemon restart mid-call), then succeeds.
type flakyDaemon struct {
	failsLeft int32
}

func (f *flakyDaemon) MCPToolList(_ *MCPToolListArgs, reply *MCPToolListReply) error {
	if atomic.AddInt32(&f.failsLeft, -1) >= 0 {
		// net/rpc surfaces a returned error as ServerError(text) on the client.
		return errors.New(daemon.ErrDaemonDrainingMsg)
	}
	reply.Tools = []mcpToolInfo{{Name: "grafel_whoami", Description: "ok"}}
	return nil
}

func (f *flakyDaemon) MCPToolCall(args *MCPToolCallArgs, reply *MCPToolCallReply) error {
	if atomic.AddInt32(&f.failsLeft, -1) >= 0 {
		return errors.New(daemon.ErrDaemonDrainingMsg)
	}
	reply.Content = []map[string]any{{"type": "text", "text": "called: " + args.Name}}
	return nil
}

// startFlakyDaemon registers a flakyDaemon on a real transport socket so the
// bridge dials it exactly as in production.
func startFlakyDaemon(t *testing.T, fails int32) (socketPath string, stop func()) {
	t.Helper()
	return startMockServer(t, &flakyDaemon{failsLeft: fails})
}

// startMockServer starts a JSON-RPC 1.0 server exposing rcvr as "Daemon" on the
// platform IPC transport, mirroring startMockDaemon but parameterised over the
// receiver so retry/flaky mocks can be plugged in.
func startMockServer(t *testing.T, rcvr any) (socketPath string, stop func()) {
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
	srv := rpc.NewServer()
	if err := srv.RegisterName("Daemon", rcvr); err != nil {
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
			conn, aerr := l.Accept()
			if aerr != nil {
				return
			}
			go srv.ServeCodec(jsonrpc.NewServerCodec(conn))
		}
	}()
	return socketPath, func() {
		l.Close()
		<-done
		if tmp != "" {
			os.RemoveAll(tmp)
		}
	}
}

// TestBridge_ToolsCall_RetriesOnDrain asserts that a transient
// connection-shutdown (the daemon's drain sentinel) is reconnected+retried so
// the MCP client sees a successful result, not a hard error (#5633 part 2).
func TestBridge_ToolsCall_RetriesOnDrain(t *testing.T) {
	socketPath, stop := startFlakyDaemon(t, 2) // fail twice, then succeed
	defer stop()

	b := &bridge{socketPath: socketPath}
	params, _ := json.Marshal(map[string]any{"name": "grafel_find"})
	req := rpc2Request{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: params}
	resp := b.handle(req)

	if resp.Error != nil {
		t.Fatalf("expected success after retry, got JSON-RPC error: %+v", resp.Error)
	}
	var result mcpToolCallResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool result marked as error after retry: %+v", result)
	}
	text, _ := result.Content[0]["text"].(string)
	if !strings.Contains(text, "grafel_find") {
		t.Fatalf("unexpected content after retry: %q", text)
	}
}

// TestBridge_ToolsList_RetriesOnDrain asserts the same retry behavior for
// tools/list — a transient connection-shutdown is retried, not collapsed to the
// offline stub.
func TestBridge_ToolsList_RetriesOnDrain(t *testing.T) {
	socketPath, stop := startFlakyDaemon(t, 1) // fail once, then succeed
	defer stop()

	b := &bridge{socketPath: socketPath}
	req := rpc2Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"}
	resp := b.handle(req)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	var result struct {
		Tools []mcpToolInfo `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// On a successful retry we get the real tool, not the single-entry offline
	// stub. (The offline stub also has one entry, grafel_whoami, so assert on
	// the description the real mock returns.)
	if len(result.Tools) != 1 || result.Tools[0].Description != "ok" {
		t.Fatalf("expected real tool list after retry, got %+v", result.Tools)
	}
}

// TestBridge_ToolsCall_PersistentFailureSurfaces asserts that when retries are
// exhausted (the daemon never recovers) the bridge surfaces a structured tool
// error rather than hanging or panicking.
func TestBridge_ToolsCall_PersistentFailureSurfaces(t *testing.T) {
	// Always fail: more than bridgeMaxRetries+1.
	socketPath, stop := startFlakyDaemon(t, int32(bridgeMaxRetries+5))
	defer stop()

	b := &bridge{socketPath: socketPath}
	params, _ := json.Marshal(map[string]any{"name": "grafel_find"})
	req := rpc2Request{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: params}
	resp := b.handle(req)

	// Surfaced as a structured tool error (IsError), not a JSON-RPC protocol
	// error, so Claude renders the message.
	if resp.Error != nil {
		t.Fatalf("expected a structured tool error, got JSON-RPC error: %+v", resp.Error)
	}
	var result mcpToolCallResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true for an exhausted-retry failure, got %+v", result)
	}
}
