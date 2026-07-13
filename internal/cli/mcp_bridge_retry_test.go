package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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
	// Keep reconnect backoff tiny so retry tests stay fast, regardless of how
	// large bridgeMaxRetries / bridgeRetryMaxBackoff are in production (#5717).
	bridgeRetryBackoff = 1 * time.Millisecond
	bridgeRetryMaxBackoff = 5 * time.Millisecond
}

// TestBridgeRetryBudget_WidenedPast450ms is a regression guard for #5717: the
// pre-fix budget was bridgeMaxRetries=3 * bridgeRetryBackoff=150ms (flat) =
// ~450ms total — far too short to ride out a daemon restart + reindex, which
// takes seconds to minutes. The fix must widen the budget substantially.
func TestBridgeRetryBudget_WidenedPast450ms(t *testing.T) {
	if bridgeMaxRetries <= 3 {
		t.Fatalf("bridgeMaxRetries = %d, want > 3 (#5717 widens the retry budget)", bridgeMaxRetries)
	}
}

// TestBridgeBackoffForAttempt_Exponential verifies the reconnect backoff
// doubles with each attempt and is capped at bridgeRetryMaxBackoff (#5717).
// Uses fixed local values (not the package vars, which tests shrink for
// speed) so the exponential/cap shape is verified independent of scale.
func TestBridgeBackoffForAttempt_Exponential(t *testing.T) {
	saved, savedMax := bridgeRetryBackoff, bridgeRetryMaxBackoff
	defer func() { bridgeRetryBackoff, bridgeRetryMaxBackoff = saved, savedMax }()
	bridgeRetryBackoff = 100 * time.Millisecond
	bridgeRetryMaxBackoff = 800 * time.Millisecond

	want := []time.Duration{
		100 * time.Millisecond, // attempt 1
		200 * time.Millisecond, // attempt 2
		400 * time.Millisecond, // attempt 3
		800 * time.Millisecond, // attempt 4 (would be 800, at cap)
		800 * time.Millisecond, // attempt 5 (would be 1600, capped)
		800 * time.Millisecond, // attempt 6 (still capped)
	}
	for i, w := range want {
		attempt := i + 1
		if got := bridgeBackoffForAttempt(attempt); got != w {
			t.Errorf("bridgeBackoffForAttempt(%d) = %v, want %v", attempt, got, w)
		}
	}
}

// TestBridgeRetryBudget_SurvivalWindow asserts the total worst-case retry
// window (sum of backoffs across bridgeMaxRetries attempts, using the real
// production bridgeRetryBackoff/bridgeRetryMaxBackoff defaults) lands in the
// ~30-60s range the issue calls for — long enough to ride out a daemon
// restart + reindex, not so long that a genuinely dead daemon hangs the MCP
// client forever.
func TestBridgeRetryBudget_SurvivalWindow(t *testing.T) {
	const (
		prodInitial = 150 * time.Millisecond
		prodCap     = 3 * time.Second
	)
	saved, savedMax := bridgeRetryBackoff, bridgeRetryMaxBackoff
	defer func() { bridgeRetryBackoff, bridgeRetryMaxBackoff = saved, savedMax }()
	bridgeRetryBackoff = prodInitial
	bridgeRetryMaxBackoff = prodCap

	var total time.Duration
	for attempt := 1; attempt <= bridgeMaxRetries; attempt++ {
		total += bridgeBackoffForAttempt(attempt)
	}
	if total < 20*time.Second || total > 90*time.Second {
		t.Fatalf("total retry survival window = %v, want roughly 20s-90s (target ~30-60s)", total)
	}
}

// TestBridgeConnectBudget_SurvivalWindow asserts the dead-serve connect-only
// budget, evaluated at the PRODUCTION backoff defaults, spans a wall-clock
// window that comfortably covers a real deploy's serve-down window — so a
// normal serve restart still auto-recovers rather than fail-fasting
// (#5717 regression guard) — while still being meaningfully shorter than the
// full bridgeMaxRetries ride-out window (so the fail-fast + clearer
// errDaemonUnreachable signal remain a real improvement) (#5729).
//
// callDaemon bails the connect budget after applying the backoffs for
// attempts 1..(bridgeMaxConnectRetries-1) (attempt 0 dials with no backoff;
// each subsequent dial failure pays bridgeBackoffForAttempt(attempt) before
// the next try, and the (bridgeMaxConnectRetries)th failure returns without a
// further sleep). The sum of those backoffs is the worst-case wall-clock
// window a never-reachable serve blocks for.
func TestBridgeConnectBudget_SurvivalWindow(t *testing.T) {
	const (
		prodInitial = 150 * time.Millisecond
		prodCap     = 3 * time.Second
		// dev-deploy.sh tolerates up to 25s of graceful daemon shutdown before
		// swapping the binary; startup work before transport.Listen adds more.
		// The connect budget must cover this whole window so a deploy restart
		// (a dial failure) still rides out instead of fail-fasting (#5717).
		deployGracefulWindow = 25 * time.Second
	)
	saved, savedMax := bridgeRetryBackoff, bridgeRetryMaxBackoff
	defer func() { bridgeRetryBackoff, bridgeRetryMaxBackoff = saved, savedMax }()
	bridgeRetryBackoff = prodInitial
	bridgeRetryMaxBackoff = prodCap

	var connectWindow time.Duration
	for attempt := 1; attempt <= bridgeMaxConnectRetries-1; attempt++ {
		connectWindow += bridgeBackoffForAttempt(attempt)
	}

	// Lower bound: must comfortably exceed the deploy graceful-exit window so a
	// normal serve restart auto-recovers (#5717). "Comfortably" = a startup
	// margin on top of the 25s graceful window for the pre-Listen work.
	if connectWindow < deployGracefulWindow {
		t.Fatalf("connect-budget window = %v, want >= the %v deploy graceful-exit window "+
			"(a normal serve restart is a dial failure charged against this budget — "+
			"undersizing it regresses #5717 auto-recovery)", connectWindow, deployGracefulWindow)
	}
	if margin := connectWindow - deployGracefulWindow; margin < 5*time.Second {
		t.Fatalf("connect-budget window = %v leaves only %v over the %v deploy window; "+
			"want a comfortable startup margin (pre-Listen work can take several seconds)",
			connectWindow, margin, deployGracefulWindow)
	}

	// Upper bound: must stay shorter than the full ride-out window, otherwise
	// the dead-serve fail-fast is no faster than just riding out (#5729 intent).
	var rideOutWindow time.Duration
	for attempt := 1; attempt <= bridgeMaxRetries; attempt++ {
		rideOutWindow += bridgeBackoffForAttempt(attempt)
	}
	if connectWindow >= rideOutWindow {
		t.Fatalf("connect-budget window = %v is not shorter than the ride-out window %v — "+
			"the dead-serve fail-fast provides no earlier signal", connectWindow, rideOutWindow)
	}
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

// ── #5729: dead-serve fail-fast + ctx-cancel ────────────────────────────────

// TestCallDaemon_CtxCancel_ReturnsPromptly asserts that an already-cancelled
// context makes callDaemon return immediately (via ctx.Err()) instead of
// sleeping out the backoff before the next retry attempt. bridgeRetryBackoff
// is temporarily widened so the difference between "slept the backoff" and
// "returned promptly on cancel" is unambiguous.
func TestCallDaemon_CtxCancel_ReturnsPromptly(t *testing.T) {
	savedBackoff, savedMax := bridgeRetryBackoff, bridgeRetryMaxBackoff
	defer func() { bridgeRetryBackoff, bridgeRetryMaxBackoff = savedBackoff, savedMax }()
	bridgeRetryBackoff = 2 * time.Second
	bridgeRetryMaxBackoff = 2 * time.Second

	// Always-retryable-erroring mock so the first Call attempt fails and the
	// loop reaches the backoff-then-retry branch, where ctx cancellation must
	// short-circuit the sleep.
	socketPath, stop := startFlakyDaemon(t, 1000)
	defer stop()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the call even starts

	b := &bridge{socketPath: socketPath}
	var reply MCPToolListReply
	start := time.Now()
	err := b.callDaemon(ctx, "Daemon.MCPToolList", MCPToolListArgs{}, &reply)
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("callDaemon with cancelled ctx: got err %v, want context.Canceled", err)
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("callDaemon with cancelled ctx took %v, want well under the %v backoff (should not sleep it out)",
			elapsed, bridgeRetryBackoff)
	}
}

// TestCallDaemon_CtxDeadlineExceeded_ReturnsPromptly is the deadline-exceeded
// variant of the cancellation test: a context whose deadline is already in
// the past behaves the same as an explicitly cancelled one.
func TestCallDaemon_CtxDeadlineExceeded_ReturnsPromptly(t *testing.T) {
	savedBackoff, savedMax := bridgeRetryBackoff, bridgeRetryMaxBackoff
	defer func() { bridgeRetryBackoff, bridgeRetryMaxBackoff = savedBackoff, savedMax }()
	bridgeRetryBackoff = 2 * time.Second
	bridgeRetryMaxBackoff = 2 * time.Second

	socketPath, stop := startFlakyDaemon(t, 1000)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), -1*time.Millisecond)
	defer cancel()

	b := &bridge{socketPath: socketPath}
	var reply MCPToolListReply
	start := time.Now()
	err := b.callDaemon(ctx, "Daemon.MCPToolList", MCPToolListArgs{}, &reply)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("callDaemon with expired deadline: got err %v, want context.DeadlineExceeded", err)
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("callDaemon with expired deadline took %v, want well under the %v backoff", elapsed, bridgeRetryBackoff)
	}
}

// TestCallDaemon_DeadServe_FailsFast asserts that when the UDS connect fails
// outright and NEVER succeeds (socket file missing — simulating a serve
// process that is not running at all, as opposed to an engine mid-restart),
// callDaemon gives up on the smaller bridgeMaxConnectRetries budget instead
// of burning the full bridgeMaxRetries ride-out window (#5729).
//
// Retry attempts are counted via the bridge's logger (each retry emits one
// "retrying ..." line) rather than wall-clock timing, so the assertion is
// deterministic regardless of machine speed.
func TestCallDaemon_DeadServe_FailsFast(t *testing.T) {
	var logBuf strings.Builder
	b := &bridge{
		socketPath: unreachableAddr(),
		logger:     log.New(&logBuf, "", 0),
	}
	var reply MCPToolListReply
	err := b.callDaemon(context.Background(), "Daemon.MCPToolList", MCPToolListArgs{}, &reply)

	if !errors.Is(err, errDaemonUnreachable) {
		t.Fatalf("callDaemon against a never-reachable socket: got err %v, want wrapped errDaemonUnreachable", err)
	}

	retryLines := strings.Count(logBuf.String(), "retrying ")
	if retryLines >= bridgeMaxRetries {
		t.Fatalf("dead-serve path logged %d retries, want fewer than the full bridgeMaxRetries=%d ride-out budget (log: %s)",
			retryLines, bridgeMaxRetries, logBuf.String())
	}
	if retryLines > bridgeMaxConnectRetries {
		t.Fatalf("dead-serve path logged %d retries, want at most bridgeMaxConnectRetries=%d",
			retryLines, bridgeMaxConnectRetries)
	}
}

// TestCallDaemon_RideOutPreserved asserts that once the UDS connect succeeds
// at least once, subsequent RPC-transient errors (simulating an engine
// mid-restart, not a dead serve) are still retried up to the full generous
// bridgeMaxRetries budget — not truncated to bridgeMaxConnectRetries — and
// recover once the transient condition clears (#5729, must not regress
// #5717).
func TestCallDaemon_RideOutPreserved(t *testing.T) {
	if bridgeMaxRetries <= bridgeMaxConnectRetries+2 {
		t.Fatalf("test assumes bridgeMaxRetries (%d) comfortably exceeds bridgeMaxConnectRetries (%d)",
			bridgeMaxRetries, bridgeMaxConnectRetries)
	}
	// Fail more times than the connect-only budget would tolerate, but fewer
	// than the full retry budget, then recover.
	fails := int32(bridgeMaxConnectRetries + 2)
	socketPath, stop := startFlakyDaemon(t, fails)
	defer stop()

	b := &bridge{socketPath: socketPath}
	var reply MCPToolListReply
	err := b.callDaemon(context.Background(), "Daemon.MCPToolList", MCPToolListArgs{}, &reply)
	if err != nil {
		t.Fatalf("expected recovery within the full ride-out budget, got: %v", err)
	}
	if len(reply.Tools) != 1 || reply.Tools[0].Description != "ok" {
		t.Fatalf("expected the real tool list after riding out the transient errors, got %+v", reply.Tools)
	}
}

// TestCallDaemon_5717_RestartWindowRecovers is a regression guard: a serve
// that is down for a brief window (connect fails a few times) and then comes
// back up (as in a normal deploy restart) must still auto-recover via
// callDaemon, exactly as #5717 established — the smaller dead-serve budget
// introduced by #5729 must not make a normal restart window fail fast.
func TestCallDaemon_5717_RestartWindowRecovers(t *testing.T) {
	savedBackoff, savedMax := bridgeRetryBackoff, bridgeRetryMaxBackoff
	defer func() { bridgeRetryBackoff, bridgeRetryMaxBackoff = savedBackoff, savedMax }()
	bridgeRetryBackoff = 10 * time.Millisecond
	bridgeRetryMaxBackoff = 50 * time.Millisecond

	var tmp string
	var socketPath string
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
	if tmp != "" {
		defer os.RemoveAll(tmp)
	}

	// Simulate the restart window: nothing is listening on socketPath yet, so
	// the bridge's early dial attempts fail exactly like a serve that has not
	// rebound its socket after a deploy restart. The listener comes up after
	// a short delay, well inside the connect-only budget's window given the
	// backoff above.
	mock := &mockDaemonService{}
	srv := rpc.NewServer()
	if err := srv.RegisterName("Daemon", mock); err != nil {
		t.Fatalf("register mock: %v", err)
	}
	listenerUp := make(chan struct{})
	go func() {
		time.Sleep(15 * time.Millisecond)
		l, lerr := transport.Listen(socketPath)
		if lerr != nil {
			t.Errorf("delayed listen %s: %v", socketPath, lerr)
			close(listenerUp)
			return
		}
		close(listenerUp)
		for {
			conn, aerr := l.Accept()
			if aerr != nil {
				return
			}
			go srv.ServeCodec(jsonrpc.NewServerCodec(conn))
		}
	}()

	b := &bridge{socketPath: socketPath}
	var reply MCPToolListReply
	err := b.callDaemon(context.Background(), "Daemon.MCPToolList", MCPToolListArgs{}, &reply)
	<-listenerUp
	if err != nil {
		t.Fatalf("expected auto-recovery once the restart window closed (#5717 regression), got: %v", err)
	}
	if len(reply.Tools) != 1 || reply.Tools[0].Name != "grafel_whoami" {
		t.Fatalf("unexpected reply after recovery: %+v", reply.Tools)
	}
}
