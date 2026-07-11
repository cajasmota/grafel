package daemon_test

import (
	"context"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/client"
	"github.com/cajasmota/grafel/internal/daemon/proto"
)

// TestRunServe_MatchesDaemonInProcessWiring is the ADR-0024 Phase 1
// behavior-preservation regression (epic #5729): `grafel daemon` and
// `grafel serve` must be byte-for-byte behaviorally identical while the
// serve/engine split capability flag (GRAFEL_SPLIT_MODE) is off, which is
// the default. It starts an in-process daemon via daemon.RunServe — the
// same call both the `serve` cobra command and the back-compat `daemon`
// shim make — and asserts it:
//   - binds the MCP dispatch socket exactly like daemon.Run,
//   - starts the engine plane (here: the injected IndexFunc) in-process
//     with no separate process required, and
//   - answers a real MCP RPC round-trip (Ping, Status, Index), matching
//     the assertions daemon_test.go already makes against daemon.Run.
func TestRunServe_MatchesDaemonInProcessWiring(t *testing.T) {
	layout := runServeForTest(t, func(args proto.IndexArgs) (string, string, error) {
		return args.RepoPath + "/.grafel/graph.json", `{"ok":true}`, nil
	}, nil)

	c, err := client.DialPath(layout.SocketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	if r, err := c.Ping(); err != nil || r.Version == "" {
		t.Fatalf("ping: %v %+v", err, r)
	}

	st, err := c.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.PID == 0 || st.SocketPath != layout.SocketPath {
		t.Fatalf("status looks wrong: %+v", st)
	}

	reply, err := c.Index(proto.IndexArgs{RepoPath: "/tmp/fake-repo"})
	if err != nil {
		t.Fatalf("index rpc: %v", err)
	}
	if reply.StatsJSON == "" {
		t.Fatalf("index rpc returned empty stats — engine plane did not run in-process")
	}
}

// TestRunEngine_StandaloneNotYetImplemented documents the deliberate Phase 1
// scope boundary: `grafel engine` is wired as a hidden cobra subcommand and
// EngineConfig exists, but daemon.RunEngine does not yet run a serve-free
// engine-only process — that lands with PR2's actual process split. This
// pins the current, honest behavior (a clear error, not a silent duplicate
// daemon) so a future change to it is a deliberate, reviewed decision.
func TestRunEngine_StandaloneNotYetImplemented(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := daemon.RunEngine(ctx, daemon.EngineConfig{})
	if err == nil {
		t.Fatal("expected RunEngine to report standalone-not-yet-implemented, got nil error")
	}
}

// runServeForTest mirrors runDaemonForTest (daemon_test.go) but starts the
// daemon via daemon.RunServe instead of daemon.Run, so tests in this file
// exercise the exact entrypoint `grafel serve` / `grafel daemon` now use.
func runServeForTest(t *testing.T, idx daemon.IndexFunc, rb daemon.RebuildFunc) daemon.Layout {
	t.Helper()
	isolateDaemonEnv(t)
	layout, err := daemon.DefaultLayout()
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	if err := daemon.EnsureLayout(layout); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- daemon.RunServe(ctx, daemon.ServeConfig{Config: daemon.Config{
			Layout:  layout,
			Index:   idx,
			Rebuild: rb,
		}})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Logf("RunServe did not exit within 3s")
		}
	})
	waitDaemonReady(t, layout.SocketPath, 10*time.Second)
	return layout
}
