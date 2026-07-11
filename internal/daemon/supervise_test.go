//go:build darwin || linux

package daemon

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestEngineChildHelper is the subprocess entrypoint the supervisor tests spawn
// in place of a real `grafel engine` binary (the standard os/exec
// subprocess-testing pattern). It runs a genuine RunEngine — writing engine.pid,
// publishing the engine-global liveness heartbeat, and blocking until SIGTERM —
// so the supervisor's spawn/health/restart/drain paths exercise the real thing.
// It is inert unless GRAFEL_ENGINE_CHILD_HELPER=1 is set by the spawner.
func TestEngineChildHelper(t *testing.T) {
	if os.Getenv("GRAFEL_ENGINE_CHILD_HELPER") != "1" {
		return
	}
	root := os.Getenv(EnvRoot)
	// A short heartbeat keeps the liveness file fresh so the parent's health
	// gate flips to HEALTHY quickly.
	_ = os.Setenv(EnvStatusHeartbeatSeconds, "1")
	layout := layoutFromRoot(root, "")
	// RunEngine blocks until SIGTERM/SIGINT; the supervisor delivers SIGTERM on
	// drain, at which point this returns and the helper process exits 0.
	if err := RunEngine(context.Background(), EngineConfig{Config: Config{Layout: layout}}); err != nil {
		t.Fatalf("RunEngine (helper): %v", err)
	}
}

// helperEngineCommand returns an engineChildCommand override that spawns the
// TestEngineChildHelper subprocess instead of a real grafel binary.
func helperEngineCommand() func(selfExe, root string) *exec.Cmd {
	return func(selfExe, root string) *exec.Cmd {
		cmd := exec.Command(selfExe, "-test.run=TestEngineChildHelper", "-test.timeout=120s")
		cmd.Env = append(os.Environ(),
			"GRAFEL_ENGINE_CHILD_HELPER=1",
			EnvRoot+"="+root,
		)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.SysProcAttr = engineChildSysProcAttr()
		return cmd
	}
}

// isolateSupervisorEnv points GRAFEL_DAEMON_ROOT and GRAFEL_HOME at a fresh
// short temp dir (so both the engine.pid under the daemon root and the
// statusfile under GRAFEL_HOME land in the sandbox) and disables self-defense.
// Returns the root.
func isolateSupervisorEnv(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "archi-sup-")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	t.Setenv(EnvRoot, root)
	t.Setenv("GRAFEL_HOME", root)
	t.Setenv(EnvDisableSelfDefense, "1")
	// Fast heartbeat so health gating and restart detection resolve quickly.
	t.Setenv(EnvStatusHeartbeatSeconds, "1")
	return root
}

// newTestSupervisor builds a supervisor tuned for millisecond-scale tests.
func newTestSupervisor(t *testing.T, root string) *engineSupervisor {
	t.Helper()
	s := newEngineSupervisor(layoutFromRoot(root, ""), buildSlogLogger(os.Stderr))
	s.backoffInitial = 150 * time.Millisecond
	s.backoffMax = 400 * time.Millisecond
	s.healthyUptime = 400 * time.Millisecond
	s.drainTimeout = 3 * time.Second
	return s
}

func pidAliveUnix(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return false
}

func readEnginePID(root string) int {
	b, err := os.ReadFile(EnginePIDPath(root))
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return pid
}

// TestEngineSupervisor_SpawnsHealthyChildWritesPidAndHeartbeat covers harness
// scenario 1: serve starts, spawns an engine child, engine.pid is written, and
// the liveness statusfile shows a fresh EnginePID/HeartbeatAt matching the
// spawned child. Then stop() reaps the child (no orphan).
func TestEngineSupervisor_SpawnsHealthyChildWritesPidAndHeartbeat(t *testing.T) {
	root := isolateSupervisorEnv(t)
	defer SetEngineChildCommandForTest(helperEngineCommand())()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sup := newTestSupervisor(t, root)
	if err := sup.start(ctx); err != nil {
		t.Fatalf("start supervisor: %v", err)
	}

	if !waitFor(t, 30*time.Second, func() bool {
		ok, _ := sup.healthy()
		return ok
	}) {
		_, why := sup.healthy()
		t.Fatalf("engine child never became HEALTHY: %s", why)
	}

	pid := sup.getChildPID()
	if pid == 0 {
		t.Fatal("supervisor reports no child pid despite HEALTHY")
	}
	if got := readEnginePID(root); got != pid {
		t.Fatalf("engine.pid = %d, want spawned child pid %d", got, pid)
	}

	// Drain: stop() must reap the child — no orphan.
	sup.stop()
	if !waitFor(t, 6*time.Second, func() bool { return !pidAliveUnix(pid) }) {
		t.Fatalf("engine child pid %d still alive after stop() — orphaned", pid)
	}
}

// TestEngineSupervisor_FaultIsolationRestart covers harness scenario 2: kill the
// engine child (SIGKILL) and assert (a) the supervisor reports DEGRADED (health
// gate flips), and (b) it restarts the engine with a fresh, different child that
// heartbeats and returns to HEALTHY.
func TestEngineSupervisor_FaultIsolationRestart(t *testing.T) {
	root := isolateSupervisorEnv(t)
	defer SetEngineChildCommandForTest(helperEngineCommand())()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sup := newTestSupervisor(t, root)
	if err := sup.start(ctx); err != nil {
		t.Fatalf("start supervisor: %v", err)
	}
	t.Cleanup(sup.stop)

	if !waitFor(t, 30*time.Second, func() bool { ok, _ := sup.healthy(); return ok }) {
		_, why := sup.healthy()
		t.Fatalf("engine never HEALTHY initially: %s", why)
	}
	pid1 := sup.getChildPID()

	// Fault: hard-kill the engine child.
	if err := syscall.Kill(pid1, syscall.SIGKILL); err != nil {
		t.Fatalf("SIGKILL engine child %d: %v", pid1, err)
	}

	// (a) serve must observe DEGRADED (health gate false) after the kill.
	if !waitFor(t, 10*time.Second, func() bool { ok, _ := sup.healthy(); return !ok }) {
		t.Fatal("supervisor never reported DEGRADED after engine child was killed")
	}

	// (b) serve must restart the engine: a DIFFERENT child pid becomes HEALTHY
	// again with a fresh heartbeat.
	if !waitFor(t, 30*time.Second, func() bool {
		ok, _ := sup.healthy()
		return ok && sup.getChildPID() != pid1
	}) {
		t.Fatalf("engine was not restarted to a fresh HEALTHY child (still pid %d)", pid1)
	}
	pid2 := sup.getChildPID()
	if pid2 == pid1 || pid2 == 0 {
		t.Fatalf("restarted child pid = %d, want a new non-zero pid (was %d)", pid2, pid1)
	}
	// The killed process must be gone; the replacement must be alive.
	if pidAliveUnix(pid1) {
		t.Errorf("killed engine child %d is still alive", pid1)
	}
	if !pidAliveUnix(pid2) {
		t.Errorf("replacement engine child %d is not alive", pid2)
	}
}

// TestEngineSupervisor_GracefulDrainReapsChild covers harness scenario 3: a
// graceful stop SIGTERMs the child and reaps it — no orphan left behind.
func TestEngineSupervisor_GracefulDrainReapsChild(t *testing.T) {
	root := isolateSupervisorEnv(t)
	defer SetEngineChildCommandForTest(helperEngineCommand())()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sup := newTestSupervisor(t, root)
	if err := sup.start(ctx); err != nil {
		t.Fatalf("start supervisor: %v", err)
	}
	if !waitFor(t, 30*time.Second, func() bool { ok, _ := sup.healthy(); return ok }) {
		t.Fatal("engine never HEALTHY")
	}
	pid := sup.getChildPID()

	sup.stop()

	if pidAliveUnix(pid) {
		// Give the OS a beat to finish reaping, then re-check.
		if !waitFor(t, 3*time.Second, func() bool { return !pidAliveUnix(pid) }) {
			t.Fatalf("engine child %d orphaned after graceful drain", pid)
		}
	}
	// After a clean drain, no fatal should have been recorded.
	if err := sup.fatalError(); err != nil {
		t.Errorf("unexpected fatal after graceful drain: %v", err)
	}
}

// TestEngineSupervisor_UnkeepableEngineIsFatal asserts the one path where serve
// gives up: a child that cannot start at all (bad command) crash-loops at the
// backoff ceiling until the supervisor records + signals a fatal, which is what
// makes RunServe exit non-zero so the OS unit recycles it.
func TestEngineSupervisor_UnkeepableEngineIsFatal(t *testing.T) {
	root := isolateSupervisorEnv(t)
	// Command that exits immediately (nonexistent subcommand → helper env unset
	// → returns instantly), forcing a relentless crash loop.
	defer SetEngineChildCommandForTest(func(selfExe, root string) *exec.Cmd {
		cmd := exec.Command(selfExe, "-test.run=TestEngineChildHelper")
		// NOTE: GRAFEL_ENGINE_CHILD_HELPER intentionally NOT set, so the helper
		// returns immediately → the child exits ~instantly every time.
		cmd.Env = append(os.Environ(), EnvRoot+"="+root)
		cmd.SysProcAttr = engineChildSysProcAttr()
		return cmd
	})()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sup := newEngineSupervisor(layoutFromRoot(root, ""), buildSlogLogger(os.Stderr))
	// Tiny backoff + low ceiling so the fatal lands fast.
	sup.backoffInitial = 20 * time.Millisecond
	sup.backoffMax = 40 * time.Millisecond
	sup.healthyUptime = 10 * time.Second // never "recovers"
	sup.maxCeilingHits = 3
	sup.drainTimeout = time.Second
	if err := sup.start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(sup.stop)

	select {
	case err := <-sup.fatal():
		if err == nil {
			t.Fatal("fatal channel fired with nil error")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("supervisor never surfaced a fatal for an unkeepable engine")
	}
	if sup.fatalError() == nil {
		t.Error("fatalError() nil after fatal fired")
	}
}
