//go:build darwin || linux

package daemon_test

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/client"
	"github.com/cajasmota/grafel/internal/daemon/proto"
)

// readEnginePIDE2E reads <root>/engine.pid (0 if absent/unreadable).
func readEnginePIDE2E(root string) int {
	b, err := os.ReadFile(daemon.EnginePIDPath(root))
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return pid
}

func pidAliveE2E(pid int) bool {
	return pid > 0 && syscall.Kill(pid, 0) == nil
}

func waitForE2E(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return false
}

// TestRunServe_SplitMode_EngineFaultIsIsolated is the end-to-end fault-isolation
// harness (ADR-0024 PR2, epic #5729): with GRAFEL_SPLIT_MODE ON, RunServe runs
// the serve plane in-process and supervises a real `grafel engine` child
// (here: the TestEngineChildHelper subprocess). It asserts:
//
//  1. serve spawns an engine child (engine.pid written).
//  2. Fault isolation: SIGKILL the engine child, and (a) serve STAYS UP —
//     MCP RPC keeps answering — and (b) serve RESTARTS the engine (a fresh,
//     different, live child appears).
//  3. Graceful drain: shutting down serve reaps the engine child (no orphan).
//
// index_status parity in split-mode is deliberately NOT asserted — that wiring
// lands in PR3. This test only exercises RPC-read survival + process lifecycle.
func TestRunServe_SplitMode_EngineFaultIsIsolated(t *testing.T) {
	root := shortTempRoot(t)
	t.Setenv(daemon.EnvRoot, root)
	t.Setenv("GRAFEL_HOME", root)
	t.Setenv(daemon.EnvDisableSelfDefense, "1")
	t.Setenv("GRAFEL_SPLIT_MODE", "1")
	t.Setenv(daemon.EnvStatusHeartbeatSeconds, "1")

	// Spawn the engine child as the in-binary helper subprocess rather than a
	// real grafel binary.
	restore := daemon.SetEngineChildCommandForTest(func(selfExe, root string) *exec.Cmd {
		cmd := exec.Command(selfExe, "-test.run=TestEngineChildHelper", "-test.timeout=120s")
		cmd.Env = append(os.Environ(),
			"GRAFEL_ENGINE_CHILD_HELPER=1",
			daemon.EnvRoot+"="+root,
		)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		return cmd
	})
	defer restore()

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
			Layout: layout,
			Index: func(a proto.IndexArgs) (string, string, error) {
				return a.RepoPath + "/.grafel/graph.json", `{"ok":true}`, nil
			},
		}})
	}()
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			cancel()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Log("RunServe did not exit within 10s")
			}
		}
	})

	// serve's MCP socket must come up (in-process serve plane).
	waitDaemonReady(t, layout.SocketPath, 15*time.Second)

	// (1) engine child spawned → engine.pid present + alive.
	var pid1 int
	if !waitForE2E(20*time.Second, func() bool {
		pid1 = readEnginePIDE2E(root)
		return pid1 > 0 && pidAliveE2E(pid1)
	}) {
		t.Fatalf("engine child never spawned (no live engine.pid at %s)", daemon.EnginePIDPath(root))
	}

	// (2) Fault: SIGKILL the engine child.
	if err := syscall.Kill(pid1, syscall.SIGKILL); err != nil {
		t.Fatalf("SIGKILL engine child %d: %v", pid1, err)
	}

	// (2a) serve STAYS UP: MCP RPC keeps answering across the engine's death.
	for i := 0; i < 6; i++ {
		c, derr := client.DialPath(layout.SocketPath)
		if derr != nil {
			t.Fatalf("serve socket dropped after engine kill (iter %d): dial: %v", i, derr)
		}
		if _, perr := c.Ping(); perr != nil {
			c.Close()
			t.Fatalf("serve Ping failed after engine kill (iter %d): %v — engine fault was NOT isolated", i, perr)
		}
		c.Close()
		time.Sleep(150 * time.Millisecond)
	}

	// (2b) serve RESTARTS the engine: a fresh, different, live child appears.
	if !waitForE2E(30*time.Second, func() bool {
		p := readEnginePIDE2E(root)
		return p > 0 && p != pid1 && pidAliveE2E(p)
	}) {
		t.Fatalf("engine child was not restarted after fault (still pid %d)", pid1)
	}
	lastPid := readEnginePIDE2E(root)
	if pidAliveE2E(pid1) {
		t.Errorf("killed engine child %d still alive", pid1)
	}

	// (3) Graceful drain: shutting down serve reaps the engine child.
	cancel()
	stopped = true
	select {
	case <-done:
	case <-time.After(12 * time.Second):
		t.Fatal("RunServe did not shut down within 12s")
	}
	if !waitForE2E(6*time.Second, func() bool { return !pidAliveE2E(lastPid) }) {
		t.Fatalf("engine child %d orphaned after serve shutdown", lastPid)
	}
}
