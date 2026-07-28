package cli

// reset_completion_test.go — `grafel reset` completion honesty (#5991).
//
// The bug: split mode is the DEFAULT (daemon.SplitModeEnabled returns true
// unless GRAFEL_SPLIT_MODE is explicitly falsy), and in split mode
// Service.Rebuild returns nil the instant the KindRebuild request file is
// written to disk — fire-and-forget. The reply is the zero RebuildReply, so
// `reset` printed "Rebuilding group 'X'..." and exited 0 whether or not the
// engine ever drained that request; the `.grafel/` wipe (which happens inside
// RebuildFunc, i.e. engine-side) never happened either. The exit code carried
// no information at all.
//
// The fix makes `reset` request WaitForCompletion (the same #5790 contract
// `group add --index` uses), so err==nil means the engine really wiped and
// rebuilt, and any non-completion (timeout / engine-death / failed rebuild)
// becomes a NON-ZERO exit naming what did not happen.

import (
	"bytes"
	"fmt"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/transport"
)

// stubDefaultLocationDaemon points GRAFEL_DAEMON_ROOT at a private temp root
// and serves svc on the socket `client.Dial()` resolves from it. This drives
// the REAL command path (cobra RunE → runRebuildClient → client.Dial), with no
// production-side injection seam.
func stubDefaultLocationDaemon(t *testing.T, svc *mockRebuildService) {
	t.Helper()
	// os.MkdirTemp (not t.TempDir) keeps the UDS path short enough for the
	// 104-byte sun_path limit on macOS.
	root, err := os.MkdirTemp("", "ag-reset-")
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
		if err := os.MkdirAll(layout.SocketDir, 0o755); err != nil {
			t.Fatalf("mkdir socket dir: %v", err)
		}
	}
	ln, err := transport.Listen(layout.SocketPath)
	if err != nil {
		t.Fatalf("listen %s: %v", layout.SocketPath, err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	srv := rpc.NewServer()
	if err := srv.RegisterName(proto.ServiceName, svc); err != nil {
		t.Fatalf("register: %v", err)
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

// runResetCmd executes the real `reset` cobra command against the stub daemon.
func runResetCmd(t *testing.T, buf *bytes.Buffer, args ...string) error {
	t.Helper()
	cmd := newResetCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs(args)
	return cmd.Execute()
}

// TestReset_ForwardsWaitForCompletion: `reset` must ask the daemon to block for
// real completion. Without it the split-mode daemon fire-and-forgets and the
// exit code says nothing about whether the wipe+rebuild happened (#5991).
func TestReset_ForwardsWaitForCompletion(t *testing.T) {
	withSandboxHome(t)
	svc := &mockRebuildService{}
	stubDefaultLocationDaemon(t, svc)

	var buf bytes.Buffer
	if err := runResetCmd(t, &buf, "mygroup", "--plain"); err != nil {
		t.Fatalf("reset: %v\n%s", err, buf.String())
	}
	if !svc.gotArgs.Wipe {
		t.Fatal("reset must request Wipe=true")
	}
	if !svc.gotArgs.WaitForCompletion {
		t.Fatal("reset must set WaitForCompletion=true so err==nil means the wipe+rebuild really ran (#5991)")
	}
}

// TestReset_QuietForwardsWaitForCompletion: the --quiet path is a separate
// c.Rebuild call site and must carry the same contract.
func TestReset_QuietForwardsWaitForCompletion(t *testing.T) {
	withSandboxHome(t)
	svc := &mockRebuildService{}
	stubDefaultLocationDaemon(t, svc)

	var buf bytes.Buffer
	if err := runResetCmd(t, &buf, "mygroup", "--quiet"); err != nil {
		t.Fatalf("reset --quiet: %v\n%s", err, buf.String())
	}
	if !svc.gotArgs.WaitForCompletion {
		t.Fatal("reset --quiet must also set WaitForCompletion=true (#5991)")
	}
}

// TestReset_NonCompletionExitsNonZero: when the daemon cannot confirm the
// rebuild ran (engine never became live / timed out), `reset` must FAIL with a
// message naming what did not happen — never print "Rebuilding..." and exit 0.
func TestReset_NonCompletionExitsNonZero(t *testing.T) {
	withSandboxHome(t)
	svc := &mockRebuildService{
		rErr: fmt.Errorf("index engine never became live within 30s; is the daemon/engine running?"),
	}
	stubDefaultLocationDaemon(t, svc)

	var buf bytes.Buffer
	err := runResetCmd(t, &buf, "mygroup", "--plain")
	if err == nil {
		t.Fatalf("reset must exit non-zero when the daemon did not confirm the rebuild (#5991)\noutput:\n%s", buf.String())
	}
	msg := err.Error()
	for _, want := range []string{"mygroup", "not wiped or rebuilt", "never became live"} {
		if !strings.Contains(strings.ToLower(msg), strings.ToLower(want)) {
			t.Fatalf("reset error %q must name what did not happen and why; missing %q", msg, want)
		}
	}
}

// TestReset_QuietNonCompletionExitsNonZero: same honesty on the --quiet path.
func TestReset_QuietNonCompletionExitsNonZero(t *testing.T) {
	withSandboxHome(t)
	svc := &mockRebuildService{rErr: fmt.Errorf("group rebuild failed: boom")}
	stubDefaultLocationDaemon(t, svc)

	var buf bytes.Buffer
	err := runResetCmd(t, &buf, "mygroup", "--quiet")
	if err == nil {
		t.Fatalf("reset --quiet must exit non-zero on an unconfirmed rebuild (#5991)\noutput:\n%s", buf.String())
	}
	if !strings.Contains(strings.ToLower(err.Error()), "not wiped or rebuilt") {
		t.Fatalf("reset --quiet error %q must name what did not happen", err.Error())
	}
}

// runRebuildCmd executes the real `rebuild` cobra command against the stub.
func runRebuildCmd(t *testing.T, buf *bytes.Buffer, args ...string) error {
	t.Helper()
	cmd := newRebuildCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs(args)
	return cmd.Execute()
}

// TestRebuild_ScopePin pins the OTHER side of the #5991 guard: the fix is
// deliberately scoped to `reset` (the destructive escape hatch), so plain
// `rebuild` keeps its fire-and-forget RPC and its unwrapped error message. A
// guard that fired for every rebuild would pass the reset tests above while
// silently changing `rebuild`; this test makes that mutation visible.
func TestRebuild_ScopePin(t *testing.T) {
	withSandboxHome(t)
	svc := &mockRebuildService{rErr: fmt.Errorf("boom-from-daemon")}
	stubDefaultLocationDaemon(t, svc)

	var buf bytes.Buffer
	err := runRebuildCmd(t, &buf, "mygroup", "--plain")
	if svc.gotArgs.Wipe {
		t.Fatal("rebuild must not request a wipe")
	}
	if svc.gotArgs.WaitForCompletion {
		t.Fatal("the #5991 completion contract is scoped to reset; rebuild must be unchanged")
	}
	if err == nil || err.Error() != "boom-from-daemon" {
		t.Fatalf("rebuild error must pass through unwrapped, got %v", err)
	}
}

// TestReset_SuccessStillExitsZero: the honesty fix must NOT turn every reset
// into a failure — a daemon that confirms completion still exits 0.
func TestReset_SuccessStillExitsZero(t *testing.T) {
	withSandboxHome(t)
	repo := filepath.Join(t.TempDir(), "alpha")
	makeRepo(t, repo)
	svc := &mockRebuildService{reply: proto.RebuildReply{Repos: []string{repo}, ElapsedSec: 1.5}}
	stubDefaultLocationDaemon(t, svc)

	var buf bytes.Buffer
	if err := runResetCmd(t, &buf, "mygroup", "--plain"); err != nil {
		t.Fatalf("a confirmed reset must exit 0, got %v\n%s", err, buf.String())
	}
}
