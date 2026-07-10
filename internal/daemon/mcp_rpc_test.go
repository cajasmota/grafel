package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func testService(listTools MCPListToolsFunc, callTool MCPCallToolFunc) *Service {
	return &Service{
		mcpListTools: listTools,
		mcpCallTool:  callTool,
		progress:     make(map[string]*rebuildSession),
	}
}

// stubSchema is a minimal valid JSONSchema for test tools.
var stubSchema = json.RawMessage(`{"type":"object","properties":{}}`)

// ── MCPToolList tests ─────────────────────────────────────────────────────────

func TestMCPToolList_NilFunc_ReturnsEmpty(t *testing.T) {
	svc := testService(nil, nil)
	var reply MCPToolListReply
	if err := svc.MCPToolList(&MCPToolListArgs{}, &reply); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reply.Tools) != 0 {
		t.Fatalf("expected empty tools when mcpListTools is nil, got %d", len(reply.Tools))
	}
}

func TestMCPToolList_ReturnsCatalog(t *testing.T) {
	wantTools := []MCPToolEntry{
		{Name: "grafel_find", Description: "BM25 search", InputSchema: stubSchema},
		{Name: "grafel_stats", Description: "Corpus metrics", InputSchema: stubSchema},
	}
	svc := testService(func(_ string) ([]MCPToolEntry, error) {
		return wantTools, nil
	}, nil)

	var reply MCPToolListReply
	if err := svc.MCPToolList(&MCPToolListArgs{}, &reply); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reply.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(reply.Tools))
	}
	if reply.Tools[0].Name != "grafel_find" {
		t.Errorf("first tool name: %q", reply.Tools[0].Name)
	}
	if reply.Tools[1].Name != "grafel_stats" {
		t.Errorf("second tool name: %q", reply.Tools[1].Name)
	}
}

func TestMCPToolList_PropagatesError(t *testing.T) {
	svc := testService(func(_ string) ([]MCPToolEntry, error) {
		return nil, fmt.Errorf("registry read failed")
	}, nil)

	var reply MCPToolListReply
	err := svc.MCPToolList(&MCPToolListArgs{}, &reply)
	if err == nil {
		t.Fatal("expected error when listTools returns error")
	}
}

func TestMCPToolList_InputSchemaIncluded(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"question":{"type":"string"}}}`)
	svc := testService(func(_ string) ([]MCPToolEntry, error) {
		return []MCPToolEntry{
			{Name: "grafel_find", Description: "BM25 search", InputSchema: schema},
		}, nil
	}, nil)

	var reply MCPToolListReply
	if err := svc.MCPToolList(&MCPToolListArgs{}, &reply); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reply.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(reply.Tools))
	}
	var parsedSchema map[string]any
	if err := json.Unmarshal(reply.Tools[0].InputSchema, &parsedSchema); err != nil {
		t.Fatalf("inputSchema is not valid JSON: %v", err)
	}
	if parsedSchema["type"] != "object" {
		t.Errorf("inputSchema type: %v", parsedSchema["type"])
	}
}

// ── MCPToolCall tests ─────────────────────────────────────────────────────────

func TestMCPToolCall_NilFunc_ReturnsErrorBlock(t *testing.T) {
	svc := testService(nil, nil)
	var reply MCPToolCallReply
	if err := svc.MCPToolCall(&MCPToolCallArgs{Name: "grafel_stats"}, &reply); err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if !reply.IsError {
		t.Fatal("expected IsError=true when mcpCallTool is nil")
	}
	if len(reply.Content) == 0 {
		t.Fatal("expected content block when mcpCallTool is nil")
	}
}

func TestMCPToolCall_NilArgs_ReturnsError(t *testing.T) {
	svc := testService(func(_ string) ([]MCPToolEntry, error) { return nil, nil }, nil)
	var reply MCPToolCallReply
	err := svc.MCPToolCall(nil, &reply)
	if err == nil {
		t.Fatal("expected error for nil args")
	}
}

func TestMCPToolCall_EmptyName_ReturnsError(t *testing.T) {
	svc := testService(nil, nil)
	var reply MCPToolCallReply
	err := svc.MCPToolCall(&MCPToolCallArgs{Name: ""}, &reply)
	if err == nil {
		t.Fatal("expected error for empty tool name")
	}
}

func TestMCPToolCall_DispatchesToHandler(t *testing.T) {
	called := false
	svc := testService(nil, func(name string, args map[string]any, cwd string) (MCPCallResult, error) {
		called = true
		if name != "grafel_stats" {
			t.Errorf("unexpected tool name: %q", name)
		}
		return MCPCallResult{
			Content: []map[string]any{
				{"type": "text", "text": `{"node_count":42}`},
			},
		}, nil
	})

	var reply MCPToolCallReply
	if err := svc.MCPToolCall(&MCPToolCallArgs{Name: "grafel_stats"}, &reply); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("handler was not called")
	}
	if reply.IsError {
		t.Fatal("expected IsError=false on success")
	}
	if len(reply.Content) == 0 {
		t.Fatal("expected content block in reply")
	}
	text, _ := reply.Content[0]["text"].(string)
	if text != `{"node_count":42}` {
		t.Errorf("unexpected content: %q", text)
	}
}

func TestMCPToolCall_ForwardsCWD(t *testing.T) {
	var gotCWD string
	svc := testService(nil, func(_ string, args map[string]any, cwd string) (MCPCallResult, error) {
		gotCWD = cwd
		return MCPCallResult{Content: []map[string]any{{"type": "text", "text": "ok"}}}, nil
	})

	const wantCWD = "/home/user/myproject"
	var reply MCPToolCallReply
	if err := svc.MCPToolCall(&MCPToolCallArgs{
		Name: "grafel_find",
		CWD:  wantCWD,
	}, &reply); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotCWD != wantCWD {
		t.Errorf("CWD not forwarded: got %q, want %q", gotCWD, wantCWD)
	}
}

func TestMCPToolCall_HandlerError_ReturnsErrorBlock(t *testing.T) {
	svc := testService(nil, func(_ string, _ map[string]any, _ string) (MCPCallResult, error) {
		return MCPCallResult{}, fmt.Errorf("internal tool failure")
	})

	var reply MCPToolCallReply
	if err := svc.MCPToolCall(&MCPToolCallArgs{Name: "grafel_find"}, &reply); err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if !reply.IsError {
		t.Fatal("expected IsError=true on handler failure")
	}
	if len(reply.Content) == 0 {
		t.Fatal("expected error content block")
	}
}

// TestMCPToolCall_HandlerPanic_RecoversAndSurvives is the RED/GREEN test for
// #5717: a tool handler that panics (e.g. a nil-deref bug in one grafel_*
// handler) must not crash the whole daemon and sever every MCP bridge
// connection. Before the fix, s.mcpCallTool(...) had no defer/recover, so an
// unrecovered panic here would propagate out of MCPToolCall, out of the
// net/rpc dispatch goroutine, and terminate the process — this test would
// crash `go test` itself rather than fail an assertion.
func TestMCPToolCall_HandlerPanic_RecoversAndSurvives(t *testing.T) {
	svc := testService(nil, func(name string, _ map[string]any, _ string) (MCPCallResult, error) {
		if name == "grafel_boom" {
			var p *int
			_ = *p // nil-deref panic, simulating a buggy tool handler
		}
		return MCPCallResult{Content: []map[string]any{{"type": "text", "text": "ok"}}}, nil
	})

	var reply MCPToolCallReply
	err := svc.MCPToolCall(&MCPToolCallArgs{Name: "grafel_boom"}, &reply)
	if err != nil {
		t.Fatalf("unexpected protocol error surfaced from recovered panic: %v", err)
	}
	if !reply.IsError {
		t.Fatal("expected IsError=true for a panicking tool handler")
	}
	if len(reply.Content) == 0 {
		t.Fatal("expected an error content block describing the panic")
	}

	// The dispatch loop must survive: a subsequent, unrelated call still works.
	var reply2 MCPToolCallReply
	if err := svc.MCPToolCall(&MCPToolCallArgs{Name: "grafel_stats"}, &reply2); err != nil {
		t.Fatalf("unexpected error on call after recovered panic: %v", err)
	}
	if reply2.IsError {
		t.Fatal("expected IsError=false for the healthy call after a recovered panic")
	}
}

func TestMCPToolCall_EmptyContent_NormalisedToEmptySlice(t *testing.T) {
	svc := testService(nil, func(_ string, _ map[string]any, _ string) (MCPCallResult, error) {
		return MCPCallResult{Content: nil}, nil
	})

	var reply MCPToolCallReply
	if err := svc.MCPToolCall(&MCPToolCallArgs{Name: "grafel_stats"}, &reply); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reply.Content == nil {
		t.Fatal("Content should be normalised to empty slice, not nil")
	}
}

// ── cwd-gate (#1769) tests ────────────────────────────────────────────────────

// TestMCPToolList_ForwardsCWD_ToListFunc verifies that the CWD from
// MCPToolListArgs is forwarded to the injected MCPListToolsFunc (#1769).
func TestMCPToolList_ForwardsCWD_ToListFunc(t *testing.T) {
	var receivedCWD string
	svc := testService(func(cwd string) ([]MCPToolEntry, error) {
		receivedCWD = cwd
		return []MCPToolEntry{{Name: "grafel_find"}}, nil
	}, nil)

	const wantCWD = "/home/user/myproject"
	var reply MCPToolListReply
	if err := svc.MCPToolList(&MCPToolListArgs{CWD: wantCWD}, &reply); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedCWD != wantCWD {
		t.Errorf("CWD not forwarded to list func: got %q, want %q", receivedCWD, wantCWD)
	}
}

// TestMCPToolList_NilArgs_EmptyCWD verifies that nil MCPToolListArgs is
// handled gracefully (cwd treated as "").
func TestMCPToolList_NilArgs_EmptyCWD(t *testing.T) {
	var receivedCWD string
	svc := testService(func(cwd string) ([]MCPToolEntry, error) {
		receivedCWD = cwd
		return []MCPToolEntry{{Name: "grafel_find"}}, nil
	}, nil)

	var reply MCPToolListReply
	if err := svc.MCPToolList(nil, &reply); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedCWD != "" {
		t.Errorf("expected empty cwd for nil args, got %q", receivedCWD)
	}
}

// TestMCPToolList_SentinelReturned verifies that when the listing func returns
// only the sentinel, the reply contains exactly one tool.
func TestMCPToolList_SentinelReturned(t *testing.T) {
	sentinel := MCPToolEntry{
		Name:        "grafel_status",
		Description: "Grafel: no indexed group covers this directory.",
	}
	svc := testService(func(_ string) ([]MCPToolEntry, error) {
		return []MCPToolEntry{sentinel}, nil
	}, nil)

	var reply MCPToolListReply
	if err := svc.MCPToolList(&MCPToolListArgs{CWD: "/tmp"}, &reply); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reply.Tools) != 1 {
		t.Fatalf("expected 1 sentinel tool, got %d", len(reply.Tools))
	}
	if reply.Tools[0].Name != "grafel_status" {
		t.Errorf("unexpected sentinel name: %q", reply.Tools[0].Name)
	}
}

// ── JSON-lines log mode tests (issue #2299, updated for slog in #2375) ───────
//
// After the log/slog migration the logging shape changes:
//   - Text mode (GRAFEL_DAEMON_LOG_JSON unset): slog.NewTextHandler emits
//     logfmt lines like: time=... level=INFO msg=mcp_rpc tool=... repo=...
//   - JSON mode (GRAFEL_DAEMON_LOG_JSON=1|true): slog.NewJSONHandler emits
//     JSON objects: {"time":"...","level":"INFO","msg":"mcp_rpc","tool":"...","repo":"..."}
//
// The mcpRPCLogEntry struct and daemonLogJSON() helper were deleted; handler
// selection happens at construction time in buildSlogLogger / newService,
// so call sites never inspect the env var.

// testServiceWithTextLogger returns a Service wired with a text-mode slog logger.
func testServiceWithTextLogger(callTool MCPCallToolFunc) (*Service, *bytes.Buffer) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	svc := &Service{
		mcpCallTool: callTool,
		progress:    make(map[string]*rebuildSession),
		logger:      logger,
	}
	return svc, &buf
}

// testServiceWithJSONLogger returns a Service wired with a JSON-mode slog logger.
func testServiceWithJSONLogger(callTool MCPCallToolFunc) (*Service, *bytes.Buffer) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	svc := &Service{
		mcpCallTool: callTool,
		progress:    make(map[string]*rebuildSession),
		logger:      logger,
	}
	return svc, &buf
}

// TestMCPToolCall_DefaultLog_TextFormat verifies that in text mode the slog
// output contains the expected structured fields (logfmt key=value pairs).
// The old "[mcp-rpc]" prefix is gone; slog emits msg=mcp_rpc instead.
func TestMCPToolCall_DefaultLog_TextFormat(t *testing.T) {
	svc, buf := testServiceWithTextLogger(func(name string, _ map[string]any, _ string) (MCPCallResult, error) {
		return MCPCallResult{Content: []map[string]any{{"type": "text", "text": "ok"}}}, nil
	})

	var reply MCPToolCallReply
	if err := svc.MCPToolCall(&MCPToolCallArgs{Name: "grafel_find", CWD: "/repo/myproject"}, &reply); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	// slog text handler emits logfmt: msg=mcp_rpc tool=grafel_find ...
	if !strings.Contains(out, "msg="+LogEventMCPRPC) {
		t.Errorf("expected msg=%s in text log, got: %q", LogEventMCPRPC, out)
	}
	if !strings.Contains(out, LogFieldTool+"=grafel_find") {
		t.Errorf("expected %s=grafel_find in text log, got: %q", LogFieldTool, out)
	}
	if !strings.Contains(out, LogFieldElapsedMS+"=") {
		t.Errorf("expected %s= field in text log, got: %q", LogFieldElapsedMS, out)
	}
	// Text mode lines must NOT be valid JSON.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, LogFieldElapsedMS+"=") {
			var m map[string]any
			if json.Unmarshal([]byte(line), &m) == nil {
				t.Errorf("text mode line looks like JSON: %q", line)
			}
		}
	}
}

// TestMCPToolCall_JSONLog_ParseableJSON verifies that in JSON mode every log
// line is valid JSON containing the expected structured fields. This replaces
// the old GRAFEL_DAEMON_LOG_JSON env-var test: handler selection now
// happens at construction time so no env var check is needed at call sites.
func TestMCPToolCall_JSONLog_ParseableJSON(t *testing.T) {
	svc, buf := testServiceWithJSONLogger(func(name string, _ map[string]any, _ string) (MCPCallResult, error) {
		return MCPCallResult{Content: []map[string]any{{"type": "text", "text": "ok"}}}, nil
	})

	const wantTool = "grafel_find"
	const wantRepo = "/repo/myproject"
	var reply MCPToolCallReply
	if err := svc.MCPToolCall(&MCPToolCallArgs{Name: wantTool, CWD: wantRepo}, &reply); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	lines := strings.Split(out, "\n")
	// Expect at least two lines: "received" and the elapsed completion line.
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 log lines, got %d: %q", len(lines), out)
	}

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Errorf("line %d is not valid JSON: %v — raw: %q", i, err, line)
			continue
		}
		// slog JSON handler emits "msg" not "event"; check the msg field.
		if entry["msg"] != LogEventMCPRPC {
			t.Errorf("line %d: msg=%q, want %q", i, entry["msg"], LogEventMCPRPC)
		}
		if entry[LogFieldTool] != wantTool {
			t.Errorf("line %d: tool=%q, want %q", i, entry[LogFieldTool], wantTool)
		}
		if entry[LogFieldRepo] != wantRepo {
			t.Errorf("line %d: repo=%q, want %q", i, entry[LogFieldRepo], wantRepo)
		}
		if _, ok := entry[LogFieldTS]; !ok {
			t.Errorf("line %d: missing %q field", i, LogFieldTS)
		}
		// Validate ts is parseable RFC3339.
		if ts, _ := entry[LogFieldTS].(string); ts != "" {
			if _, err := time.Parse(time.RFC3339, ts); err != nil {
				t.Errorf("line %d: ts %q is not RFC3339: %v", i, ts, err)
			}
		}
	}

	// The second (completion) line must have elapsed_ms field.
	var lastEntry map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(lines[len(lines)-1])), &lastEntry)
	if _, ok := lastEntry[LogFieldElapsedMS]; !ok {
		t.Errorf("completion line missing %q field", LogFieldElapsedMS)
	}
}

// TestMCPToolCall_DoneLine_HasWireBytes verifies the #2828 payload-size fields
// (wire_bytes / payload_token_estimate) propagate from the MCPCallResult onto
// the phase=done log line. The injected dispatcher reports a known size.
func TestMCPToolCall_DoneLine_HasWireBytes(t *testing.T) {
	const wantBytes = 4096
	const wantTokens = 1024
	svc, buf := testServiceWithJSONLogger(func(name string, _ map[string]any, _ string) (MCPCallResult, error) {
		return MCPCallResult{
			Content:       []map[string]any{{"type": "text", "text": "ok"}},
			WireBytes:     wantBytes,
			TokenEstimate: wantTokens,
		}, nil
	})

	var reply MCPToolCallReply
	if err := svc.MCPToolCall(&MCPToolCallArgs{Name: "grafel_find", CWD: "/r"}, &reply); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	var done map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(lines[len(lines)-1])), &done); err != nil {
		t.Fatalf("done line not JSON: %v", err)
	}
	if done[LogFieldPhase] != "done" {
		t.Fatalf("last line phase=%v, want done", done[LogFieldPhase])
	}
	// JSON numbers decode as float64.
	if b, _ := done[LogFieldWireBytes].(float64); int(b) != wantBytes {
		t.Errorf("%s=%v, want %d", LogFieldWireBytes, done[LogFieldWireBytes], wantBytes)
	}
	if tk, _ := done[LogFieldTokenEst].(float64); int(tk) != wantTokens {
		t.Errorf("%s=%v, want %d", LogFieldTokenEst, done[LogFieldTokenEst], wantTokens)
	}
}

// TestMCPToolCall_JSONLog_TrueVariant verifies that a JSON-handler slog logger
// produces parseable JSON lines (mirrors the old GRAFEL_DAEMON_LOG_JSON=true
// variant; handler selection is now at construction time).
func TestMCPToolCall_JSONLog_TrueVariant(t *testing.T) {
	svc, buf := testServiceWithJSONLogger(func(_ string, _ map[string]any, _ string) (MCPCallResult, error) {
		return MCPCallResult{Content: []map[string]any{{"type": "text", "text": "ok"}}}, nil
	})

	var reply MCPToolCallReply
	if err := svc.MCPToolCall(&MCPToolCallArgs{Name: "grafel_stats"}, &reply); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("expected JSON line, got: %q (%v)", line, err)
		}
	}
}
