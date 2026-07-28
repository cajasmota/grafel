package cli

// reset_sse_completion_test.go — the SSE branch of `grafel reset` (#5991).
//
// The first cut of the #5991 fix (WaitForCompletion + wrapResetFailure) did NOT
// reach the path users are actually on. The daemon's embedded dashboard is
// started by default, so `c.Status()` reports a DashboardPort > 0 and
// runRebuildClient takes the BROKER (SSE) branch — the poll fallback only runs
// when the dashboard is down.
//
// On that branch, runBrokerProgress's `case ev, ok := <-sseCh:` `!ok` arm gave
// up 5 seconds after the SSE stream closed and returned the ZERO rebuildOutcome
// with err == nil. The real dashboard handler closes the WHOLE group stream on
// the first per-repo terminal event (handlers_progress.go checks only RunToken,
// never repo scope), so for any group that keeps working past its first repo
// finishing, the stream closes long before the rebuild is done. wrapResetFailure
// then saw a nil error and was a no-op, finishRebuild took its len(repos)==0
// branch and printed nothing, and reset exited 0 — #5991 verbatim.
//
// Worse, asking for WaitForCompletion made this the NORMAL interleaving rather
// than a rarity: the RPC now deliberately blocks for the whole rebuild instead
// of returning in milliseconds, so SSE-close-beats-RPC is expected.
//
// These tests drive the REAL cobra `reset` command against a stub daemon that
// reports a dashboard port and a stub SSE endpoint that closes the stream early,
// so the SSE branch is bound by execution rather than by argument.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/transport"
	"github.com/cajasmota/grafel/internal/progress"
)

// rebuildRelease is the outcome a blocked Rebuild handler returns once the test
// releases it — standing in for the real WaitForCompletion behaviour, where the
// RPC blocks until the engine writes its terminal ack.
type rebuildRelease struct {
	reply proto.RebuildReply
	err   error
}

// blockingRebuildService is a stub daemon that (a) advertises a dashboard port
// so the CLI takes the SSE branch, and (b) blocks its Rebuild RPC until the
// test releases it.
type blockingRebuildService struct {
	dashPort int
	release  chan rebuildRelease

	mu      sync.Mutex
	gotArgs proto.RebuildArgs
}

func (s *blockingRebuildService) Status(_ *proto.StatusArgs, reply *proto.StatusReply) error {
	reply.DashboardPort = s.dashPort
	return nil
}

func (s *blockingRebuildService) Rebuild(args *proto.RebuildArgs, reply *proto.RebuildReply) error {
	s.mu.Lock()
	s.gotArgs = *args
	s.mu.Unlock()
	r := <-s.release
	*reply = r.reply
	return r.err
}

func (s *blockingRebuildService) args() proto.RebuildArgs {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gotArgs
}

// startEarlyCloseSSE serves /api/index-progress/{group}: it emits ONE per-repo
// terminal progress event and then returns, closing the stream — exactly what
// the real handler does when it sees the first terminal event for the run token.
func startEarlyCloseSSE(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	mux := http.NewServeMux()
	mux.HandleFunc("/api/index-progress/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		ev := progress.Event{GroupSlug: "mygroup", RepoSlug: "repo-a", Phase: progress.PhaseDone, EntitiesSoFar: 7}
		body, _ := json.Marshal(ev)
		fmt.Fprintf(w, "event: progress\ndata: %s\n\n", body)
		if flusher != nil {
			flusher.Flush()
		}
		// Return → stream closes while the rebuild is still running.
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	return ln.Addr().(*net.TCPAddr).Port
}

// stubBlockingDaemon wires blockingRebuildService onto the socket client.Dial()
// resolves from GRAFEL_DAEMON_ROOT.
func stubBlockingDaemon(t *testing.T, svc *blockingRebuildService) {
	t.Helper()
	root, err := os.MkdirTemp("", "ag-sse-")
	if err != nil {
		t.Fatalf("mkdirtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	t.Setenv(daemon.EnvRoot, root)

	layout, err := daemon.DefaultLayout()
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	if layout.SocketDir != "" {
		if mkErr := os.MkdirAll(layout.SocketDir, 0o755); mkErr != nil {
			t.Fatalf("mkdir socket dir: %v", mkErr)
		}
	}
	ln, err := transport.Listen(layout.SocketPath)
	if err != nil {
		t.Fatalf("listen %s: %v", layout.SocketPath, err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	srv := rpc.NewServer()
	if regErr := srv.RegisterName(proto.ServiceName, svc); regErr != nil {
		t.Fatalf("register: %v", regErr)
	}
	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			go srv.ServeCodec(jsonrpc.NewServerCodec(conn))
		}
	}()
}

// sseGiveUpWindow is runBrokerProgress's post-SSE-close give-up window. The
// tests below must outlive it to prove the CLI no longer returns on it.
const sseGiveUpWindow = 5 * time.Second

// TestReset_SSEStreamClosesEarly_DoesNotFalselySucceed is the #5991 regression
// on the path users are actually on: the SSE stream closes while the rebuild is
// still running, and the CLI must NOT return a nil error at the 5s give-up.
func TestReset_SSEStreamClosesEarly_DoesNotFalselySucceed(t *testing.T) {
	withSandboxHome(t)
	port := startEarlyCloseSSE(t)
	svc := &blockingRebuildService{dashPort: port, release: make(chan rebuildRelease, 1)}
	stubBlockingDaemon(t, svc)

	var buf bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- runResetCmd(t, &buf, "mygroup", "--plain") }()

	select {
	case err := <-done:
		t.Fatalf("reset returned (err=%v) before the daemon confirmed anything — the SSE give-up path reports success for an unconfirmed rebuild (#5991)\noutput:\n%s", err, buf.String())
	case <-time.After(sseGiveUpWindow + 2*time.Second):
		// Still waiting on the RPC — correct.
	}

	svc.release <- rebuildRelease{err: fmt.Errorf("group rebuild failed: engine died mid-rebuild")}

	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("reset must surface the daemon's failure\noutput:\n%s", buf.String())
		}
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, "not wiped or rebuilt") || !strings.Contains(msg, "engine died") {
			t.Fatalf("reset error %q must name what did not happen and why", err.Error())
		}
	case <-time.After(15 * time.Second):
		t.Fatal("reset never returned after the daemon answered")
	}
}

// TestRebuild_SSEGiveUpUnchanged pins the OTHER side of the SSE guard. Plain
// `rebuild` does NOT request a completion guarantee, so it keeps the historical
// 5-second give-up after the stream closes. A change that made runBrokerProgress
// wait unconditionally would pass every reset test above while silently turning
// `rebuild` (and the wizard's indexGroupWithProgress, which also passes false)
// into a command that blocks for the whole rebuild.
func TestRebuild_SSEGiveUpUnchanged(t *testing.T) {
	withSandboxHome(t)
	port := startEarlyCloseSSE(t)
	svc := &blockingRebuildService{dashPort: port, release: make(chan rebuildRelease, 1)}
	stubBlockingDaemon(t, svc)

	var buf bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- runRebuildCmd(t, &buf, "mygroup", "--plain") }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("rebuild's give-up path must stay a nil-error return, got %v", err)
		}
	case <-time.After(sseGiveUpWindow + 5*time.Second):
		t.Fatal("rebuild no longer gives up after the SSE stream closes; the #5991 wait is scoped to reset")
	}
	if svc.args().WaitForCompletion {
		t.Fatal("rebuild must not request WaitForCompletion")
	}
}

// TestReset_SSEStreamClosesEarly_SuccessIsStillReported: the same interleaving
// with a SUCCESSFUL daemon outcome must exit 0 AND report the real repos from
// the RPC reply. The old give-up path returned the ZERO outcome, so it exited 0
// with an EMPTY summary — indistinguishable from the failure above.
func TestReset_SSEStreamClosesEarly_SuccessIsStillReported(t *testing.T) {
	withSandboxHome(t)
	port := startEarlyCloseSSE(t)
	svc := &blockingRebuildService{dashPort: port, release: make(chan rebuildRelease, 1)}
	stubBlockingDaemon(t, svc)

	var buf bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- runResetCmd(t, &buf, "mygroup", "--json-progress") }()

	select {
	case err := <-done:
		t.Fatalf("reset returned early (err=%v); the RPC had not answered yet\noutput:\n%s", err, buf.String())
	case <-time.After(sseGiveUpWindow + 2*time.Second):
	}

	svc.release <- rebuildRelease{reply: proto.RebuildReply{
		Repos: []string{"/tmp/repo-a"}, ElapsedSec: 12, TotalEntities: 99, TotalRels: 42,
	}}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("a confirmed reset must exit 0, got %v\noutput:\n%s", err, buf.String())
		}
	case <-time.After(15 * time.Second):
		t.Fatal("reset never returned after the daemon answered")
	}

	if !strings.Contains(buf.String(), `"repo-a"`) {
		t.Fatalf("the final summary must carry the RPC's real repos, not the zero outcome, got:\n%s", buf.String())
	}
	if !svc.args().WaitForCompletion {
		t.Fatal("reset must still request WaitForCompletion on the SSE path")
	}
}
