package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/transport"
)

// ---------------------------------------------------------------------------
// #6722: after a daemon restart the bridge never recovered — every later tool
// call failed until the MCP client itself was restarted.
//
// The mechanism is NOT a missing retry loop (callDaemon has one) and NOT a
// volatile socket path (defaultSocketPath is fixed). It is that the cached RPC
// client was invalidated in exactly one place — the top of the retry loop —
// which only runs when the previous error was classified retryable. Any error
// the classifier does not recognise is returned immediately and leaves
// b.rpcClient pointing at the old connection forever.
// ---------------------------------------------------------------------------

// shortSocketDir returns a temp dir whose path is short enough for an AF_UNIX
// address on macOS (sun_path is 104 bytes). t.TempDir() embeds the test name,
// which pushes long-named tests past the limit and makes bind() fail with
// "invalid argument" — the same reason startMockServer uses os.MkdirTemp.
func shortSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "agbr")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// zombieDaemon models the daemon a bridge is still holding a live connection
// to after `grafel restart`: it answers normally until it is retired, then
// answers with a plain application error. net/rpc collapses that to
// ServerError(text) on the client, which the bridge's classifier treats as a
// genuine tool failure, not a transport failure — so it is returned to the
// caller, and pre-#6722 it was returned without dropping the cached client.
//
// A non-retryable *application* error is used rather than a raw transport
// error on purpose: it makes the test deterministic and transport-independent.
// It exercises the exact poisoning path on Windows, where the real teardown
// errno differs and the classifier's POSIX sentinels do not apply.
type zombieDaemon struct {
	dead atomic.Bool
}

func (z *zombieDaemon) MCPToolList(_ *MCPToolListArgs, reply *MCPToolListReply) error {
	if z.dead.Load() {
		return errors.New("engine is closed")
	}
	reply.Tools = []mcpToolInfo{{Name: "grafel_whoami", Description: "zombie"}}
	return nil
}

func (z *zombieDaemon) MCPToolCall(args *MCPToolCallArgs, reply *MCPToolCallReply) error {
	if z.dead.Load() {
		return errors.New("engine is closed")
	}
	reply.Content = []map[string]any{{"type": "text", "text": "called: " + args.Name}}
	return nil
}

// serveOn registers rcvr as "Daemon" on socketPath and serves every accepted
// connection. stopAccepting closes the listener and unlinks the socket but
// deliberately leaves already-accepted connections serving, so a bridge that
// cached a client keeps talking to this server.
// closeConns additionally closes every connection this server accepted, which
// is what a daemon process exiting does.
func serveOn(t *testing.T, socketPath string, rcvr any) (stopAccepting func(), closeConns func()) {
	t.Helper()
	srv := rpc.NewServer()
	if err := srv.RegisterName("Daemon", rcvr); err != nil {
		t.Fatalf("register: %v", err)
	}
	l, err := transport.Listen(socketPath)
	if err != nil {
		t.Fatalf("listen %s: %v", socketPath, err)
	}
	done := make(chan struct{})
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		defer close(done)
		for {
			conn, aerr := l.Accept()
			if aerr != nil {
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
			go srv.ServeCodec(jsonrpc.NewServerCodec(conn))
		}
	}()
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		l.Close()
		<-done
		if runtime.GOOS != "windows" {
			os.Remove(socketPath)
		}
	}
	closeAll := func() {
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			c.Close()
		}
		conns = nil
	}
	t.Cleanup(func() { stop(); closeAll() })
	return stop, closeAll
}

// serveGenerational serves ONE address where the first accepted connection and
// every later one get different receivers: `stale` answers the connection the
// bridge established first, `fresh` answers every connection dialled after it.
//
// This is how the restart is modelled, and it is deliberately not two
// listeners. The bridge-observable content of `grafel restart` is: the address
// is stable, the connection cached behind it is dead-or-erroring, and a FRESH
// dial to that same address reaches a working daemon. One listener reproduces
// exactly that, and does so identically on both transports.
//
// Two listeners cannot: on Windows a named pipe is not a filesystem path that
// gets unlinked and recreated, the NAME stays owned while any instance handle
// is open, so a replacement listener on the same name is refused with
// ERROR_ACCESS_DENIED while the old daemon's connection is still alive — which
// is precisely the state this test needs to hold. (That is the CI failure this
// helper replaces.) Giving the replacement a different name would have been
// the wrong fix: it would make the bridge redial a NEW address, when the whole
// point is that the address never changed and only the connection died.
func serveGenerational(t *testing.T, socketPath string, stale, fresh any) {
	t.Helper()
	l, err := transport.Listen(socketPath)
	if err != nil {
		t.Fatalf("listen %s: %v", socketPath, err)
	}
	done := make(chan struct{})
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		defer close(done)
		n := 0
		for {
			conn, aerr := l.Accept()
			if aerr != nil {
				return
			}
			rcvr := fresh
			if n == 0 {
				rcvr = stale
			}
			n++
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
			// A per-connection server is what lets one address answer with a
			// different receiver per generation.
			srv := rpc.NewServer()
			if rerr := srv.RegisterName("Daemon", rcvr); rerr != nil {
				t.Errorf("register: %v", rerr)
				conn.Close()
				continue
			}
			go srv.ServeCodec(jsonrpc.NewServerCodec(conn))
		}
	}()
	t.Cleanup(func() {
		l.Close()
		<-done
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			c.Close()
		}
		if runtime.GOOS != "windows" {
			os.Remove(socketPath)
		}
	})
}

// bridgeSocketPath returns an address the platform transport can listen on:
// a short-enough AF_UNIX path on POSIX, a unique named pipe on Windows.
func bridgeSocketPath(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return fmt.Sprintf(`\\.\pipe\agbr-%d`, stubPipeSeq(t))
	}
	return filepath.Join(shortSocketDir(t), "d.sock")
}

// callTool issues a tools/call through the full bridge handler. tools/call is
// used rather than tools/list because tools/list masks a daemon failure behind
// the static offline catalog, which would hide exactly the behaviour under
// test.
func callTool(t *testing.T, b *bridge, id int) mcpToolCallResult {
	t.Helper()
	params, err := json.Marshal(map[string]any{"name": "grafel_whoami"})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	resp := b.handle(rpc2Request{JSONRPC: "2.0", ID: id, Method: "tools/call", Params: params})
	if resp.Error != nil {
		t.Fatalf("call %d returned a protocol error: %+v", id, resp.Error)
	}
	var result mcpToolCallResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode tools/call result: %v", err)
	}
	return result
}

func resultText(t *testing.T, r mcpToolCallResult) string {
	t.Helper()
	if len(r.Content) == 0 {
		t.Fatalf("tool result has no content: %+v", r)
	}
	text, _ := r.Content[0]["text"].(string)
	return text
}

// TestBridge_RecoversAfterNonRetryableError_OnRestartedDaemon is the
// end-to-end regression test for #6722 and observes DEFECT 2 (invalidation
// coupled to classification).
//
// Sequence, mirroring the field report:
//  1. bridge dials the address and succeeds        — positive control
//  2. the daemon behind the bridge's cached connection is retired: that
//     connection now answers with a non-retryable application error, while a
//     FRESH dial to the same address reaches the replacement
//  3. the next call surfaces the stale connection's error immediately
//     (fast-return preserved)
//  4. the call AFTER that must re-dial and reach the replacement
//
// On main step 4 fails forever: the non-retryable error at step 3 returned
// without dropping the cached client, so every later call re-uses the dead
// connection. That is the "only an IDE restart fixes it" symptom.
//
// Runs on every platform, and Windows is the one that matters most: the two
// POSIX sentinels added to the classifier are synthetic there and cannot match
// a real named-pipe teardown, so on Windows this test and its callDaemon-level
// sibling are the ONLY end-to-end evidence the fix works — the whole repair
// there is defect 2's unconditional reset. See serveGenerational for why the
// restart is modelled with one listener rather than two.
func TestBridge_RecoversAfterNonRetryableError_OnRestartedDaemon(t *testing.T) {
	socketPath := bridgeSocketPath(t)

	// The daemon behind the bridge's first connection; every later connection
	// is answered by the healthy replacement.
	zombie := &zombieDaemon{}
	serveGenerational(t, socketPath, zombie, okReplacementDaemon{})

	// 1. Positive control: the bridge genuinely connected and worked BEFORE
	// the restart. Without this a broken fixture would let the test "pass" by
	// everything failing.
	b := &bridge{socketPath: socketPath}
	first := callTool(t, b, 1)
	if first.IsError {
		t.Fatalf("pre-restart call failed, fixture never connected: %s", resultText(t, first))
	}
	if got := resultText(t, first); got != "called: grafel_whoami" {
		t.Fatalf("pre-restart call did not reach the original daemon, got %q", got)
	}

	// 2. `grafel restart`: the daemon behind the bridge's cached connection is
	// gone. The address is unchanged and the cached connection is still open,
	// but it now errors on everything.
	zombie.dead.Store(true)

	// 3. The stale connection's error must still surface immediately — a
	// non-retryable error is not turned into a ~35s retry hang.
	second := callTool(t, b, 2)
	if !second.IsError {
		t.Fatalf("expected the zombie daemon's error to surface immediately, got %q", resultText(t, second))
	}
	if got := resultText(t, second); !strings.Contains(got, "engine is closed") {
		t.Fatalf("expected the daemon's own error text, got %q", got)
	}

	// 4. THE REGRESSION: the next call must re-dial and reach the replacement.
	third := callTool(t, b, 3)
	if third.IsError {
		t.Fatalf("#6722: bridge never recovered after the restart — call 3 still fails: %s",
			resultText(t, third))
	}
	if got := resultText(t, third); got != "replacement: grafel_whoami" {
		t.Fatalf("#6722: bridge is still talking to the old daemon, got %q", got)
	}
}

type okReplacementDaemon struct{}

func (okReplacementDaemon) MCPToolList(_ *MCPToolListArgs, reply *MCPToolListReply) error {
	reply.Tools = []mcpToolInfo{{Name: "grafel_whoami", Description: "replacement"}}
	return nil
}

func (okReplacementDaemon) MCPToolCall(args *MCPToolCallArgs, reply *MCPToolCallReply) error {
	reply.Content = []map[string]any{{"type": "text", "text": "replacement: " + args.Name}}
	return nil
}

// TestBridge_NonRetryableError_DropsCachedClient observes DEFECT 2 directly at
// the callDaemon level: after ANY error the cached client must be gone, so the
// next call re-dials. This is the unit-level counterpart of the end-to-end test
// above and is what makes the mechanism (resetRPCClient on the non-retryable
// path) observable in isolation.
func TestBridge_NonRetryableError_DropsCachedClient(t *testing.T) {
	socketPath := bridgeSocketPath(t)
	zombie := &zombieDaemon{}
	serveOn(t, socketPath, zombie)
	b := &bridge{socketPath: socketPath}
	var reply MCPToolListReply
	if err := b.callDaemon(nil, "Daemon.MCPToolList", &MCPToolListArgs{}, &reply); err != nil {
		t.Fatalf("positive control: first call must succeed, got %v", err)
	}
	b.rpcMu.Lock()
	cached := b.rpcClient
	b.rpcMu.Unlock()
	if cached == nil {
		t.Fatal("positive control: expected a cached client after a successful call")
	}

	zombie.dead.Store(true)
	err := b.callDaemon(nil, "Daemon.MCPToolList", &MCPToolListArgs{}, &reply)
	if err == nil {
		t.Fatal("expected the daemon's application error to surface")
	}
	if !strings.Contains(err.Error(), "engine is closed") {
		t.Fatalf("expected the non-retryable error to be returned verbatim, got %v", err)
	}
	b.rpcMu.Lock()
	after := b.rpcClient
	b.rpcMu.Unlock()
	if after != nil {
		t.Fatal("#6722: cached RPC client survived a non-retryable error — the next call " +
			"will reuse it, which is what made the post-restart failure permanent")
	}
}

// TestIsRetryableRPCErr_TransportErrors observes DEFECT 1: the classifier only
// knew four protocol-level errors and had no case for a raw transport error.
//
// MEASURED (macOS, go1.26, AF_UNIX + net/rpc/jsonrpc): a peer teardown is
// race-dependent. Read loop first → rpc.ErrShutdown / io.ErrUnexpectedEOF
// (already retryable pre-fix). Request write first → a raw
// &net.OpError{Op:"write", Err: syscall.EPIPE}, which the pre-fix classifier
// called a genuine tool failure. That is the #6722 field trigger. See
// TestBridge_PeerTeardown_SurfacesRetryableTransportErrors for the pinned
// measurement of both branches.
//
// POSIX-only: the EPIPE/ECONNRESET sentinels are synthetic on Windows and a
// named-pipe teardown (ERROR_BROKEN_PIPE / ERROR_PIPE_NOT_CONNECTED) will not
// match them. Windows is repaired by defect 2's unconditional reset, not by
// this classifier — see the doc comment on isRetryableRPCErr.
func TestIsRetryableRPCErr_TransportErrors(t *testing.T) {
	retryable := []error{
		syscall.EPIPE,
		syscall.ECONNRESET,
		net.ErrClosed,
		&net.OpError{Op: "write", Net: "unix", Err: syscall.EPIPE},
		&net.OpError{Op: "read", Net: "unix", Err: syscall.ECONNRESET},
		fmt.Errorf("writing request: %w", syscall.EPIPE),
	}
	for _, e := range retryable {
		if !isRetryableRPCErr(e) {
			t.Errorf("isRetryableRPCErr(%v) = false, want true (transport-dead, must reconnect)", e)
		}
	}
	// Guard the other direction: widening the classifier must not swallow
	// genuine tool/protocol failures, which would turn a fast, clear error into
	// a full retry-budget hang.
	notRetryable := []error{
		errors.New("tool error: no such entity"),
		errors.New("invalid params"),
		errors.New("engine is closed"),
		errors.New("connection reset by peer is mentioned in this tool output"),
		rpc.ServerError("unknown method Daemon.Nope"),
	}
	for _, e := range notRetryable {
		if isRetryableRPCErr(e) {
			t.Errorf("isRetryableRPCErr(%v) = true, want false (genuine failure must fail fast)", e)
		}
	}
}

// TestBridge_PeerTeardown_SurfacesRetryableTransportErrors pins the
// measurement the #6722 fix rests on: what net/rpc actually reports when the
// daemon goes away mid-session, on this transport.
//
// MEASURED, and the reason defect 1 is not cosmetic: the error is
// RACE-DEPENDENT. If the client's read loop notices the closed peer first, the
// call fails with rpc.ErrShutdown / io.ErrUnexpectedEOF — both of which the
// pre-#6722 classifier already handled. If the request write wins that race,
// it fails with a raw
//
//	&net.OpError{Op:"write", Net:"unix", Err: syscall.EPIPE}   ("broken pipe")
//
// which the pre-fix classifier called a genuine tool failure and returned
// without dropping the cached client: the permanent post-restart failure in
// #6722.
//
// Distribution measured on macOS/go1.26 (independent review of PR #6724):
// unloaded, 480 samples — 370 EPIPE / 110 ErrShutdown (77/23); loaded (full
// package, -shuffle=on), 132 samples — 70 EPIPE / 62 ErrShutdown (53/47),
// with ErrShutdown reaching 11 of 12 within a single run. So EPIPE dominates
// when idle, and load shifts the race TOWARD ErrShutdown. Both branches are
// real on every configuration; neither is safe to assume.
//
// The test asserts the invariant rather than one error value: every error a
// peer teardown can produce must be classified retryable and stay inside the
// documented family. Because ErrShutdown was already retryable before the fix,
// a run that sampled only ErrShutdown would pass while observing nothing the
// fix changed — 11-of-12 has been seen, so 12-of-12 is reachable on a slower
// scheduler. The run therefore also requires at least one sample from OUTSIDE
// the pre-fix set, and skips loudly (naming the distribution it saw) rather
// than passing quietly if the race never went that way.
func TestBridge_PeerTeardown_SurfacesRetryableTransportErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("AF_UNIX teardown semantics; the named-pipe transport differs")
	}
	seen := map[string]int{}
	// rawTransport counts samples from OUTSIDE the pre-#6722 classifier set —
	// the branch this fix actually added. Raising `rounds` would only lower the
	// odds of sampling none of them; the check after the loop removes the
	// silent pass entirely.
	rawTransport := 0
	const rounds = 12
	for i := 0; i < rounds; i++ {
		err := oneTeardownError(t)
		seen[teardownKind(err)]++
		preFixHandled := errors.Is(err, rpc.ErrShutdown) ||
			errors.Is(err, io.EOF) ||
			errors.Is(err, io.ErrUnexpectedEOF)
		raw := errors.Is(err, syscall.EPIPE) ||
			errors.Is(err, syscall.ECONNRESET) ||
			errors.Is(err, net.ErrClosed)
		if raw {
			rawTransport++
		}
		if !preFixHandled && !raw {
			t.Fatalf("measurement moved: peer teardown now surfaces %#v (%q), outside the "+
				"family the #6722 classifier was written against", err, err)
		}
		if !isRetryableRPCErr(err) {
			t.Fatalf("#6722: peer-teardown error %#v (%q) is NOT classified retryable — the "+
				"bridge will treat a dead socket as a tool failure and never reconnect", err, err)
		}
	}
	t.Logf("peer-teardown errors observed over %d rounds: %v", rounds, seen)
	if rawTransport == 0 {
		// Every sample landed on rpc.ErrShutdown / EOF, which was already
		// retryable before #6722 — this run observed nothing the fix changed.
		// Skip loudly rather than report a green that proves nothing. The
		// deterministic coverage of the three raw sentinels lives in
		// TestIsRetryableRPCErr_TransportErrors.
		t.Skipf("#6722: the write-vs-readloop race never produced a raw transport error in "+
			"%d rounds — distribution was %v. This run exercised only the pre-fix-handled "+
			"branch, so it says nothing about the classifier widening; see "+
			"TestIsRetryableRPCErr_TransportErrors for the deterministic pin.", rounds, seen)
	}
}

// teardownKind buckets a teardown error by sentinel so the sampled
// distribution is readable — the raw strings embed a per-round temp socket
// path and would otherwise never collapse.
func teardownKind(err error) string {
	switch {
	case errors.Is(err, syscall.EPIPE):
		return "EPIPE"
	case errors.Is(err, syscall.ECONNRESET):
		return "ECONNRESET"
	case errors.Is(err, net.ErrClosed):
		return "net.ErrClosed"
	case errors.Is(err, rpc.ErrShutdown):
		return "rpc.ErrShutdown"
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "io.ErrUnexpectedEOF"
	case errors.Is(err, io.EOF):
		return "io.EOF"
	default:
		return fmt.Sprintf("OTHER(%T: %v)", err, err)
	}
}

// oneTeardownError stands up a server, makes a successful call (the positive
// control), tears the peer down, and returns the error the next call produces.
func oneTeardownError(t *testing.T) error {
	t.Helper()
	dir := shortSocketDir(t)
	socketPath := filepath.Join(dir, "d.sock")

	stop, closeConns := serveOn(t, socketPath, okReplacementDaemon{})
	conn, err := transport.Dial(socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	client := jsonrpc.NewClient(conn)
	defer client.Close()

	var reply MCPToolListReply
	if err := client.Call("Daemon.MCPToolList", &MCPToolListArgs{}, &reply); err != nil {
		t.Fatalf("positive control: call before teardown must succeed, got %v", err)
	}

	// The daemon process goes away: listener closed, socket unlinked, and every
	// connection it had accepted torn down.
	stop()
	closeConns()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := client.Call("Daemon.MCPToolList", &MCPToolListArgs{}, &reply); err != nil {
			return err
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("expected an error after the peer went away")
	return nil
}
