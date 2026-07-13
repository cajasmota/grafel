package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/process"
)

// TestReapStaleEngine_LiveGrafelEngineTerminated verifies that when
// engine.pid names a still-live grafel process, the serve-startup reap
// (ADR-0024 orphan-engine hardening, epic #5729, SECONDARY belt-and-
// suspenders layer) SIGTERMs it before serve spawns its own engine child.
// This catches an orphan left behind by a previous unclean serve death
// (SIGKILL / crash / OOM) whose engine.pid is still on disk.
func TestReapStaleEngine_LiveGrafelEngineTerminated(t *testing.T) {
	var killedPID int
	killCalled := false
	reapStaleEngine(reapStaleEngineDeps{
		root:     t.TempDir(),
		readPID:  func(string) (int, bool) { return 4242, true },
		isAlive:  func(pid int) bool { return pid == 4242 },
		isGrafel: func(int) (bool, error) { return true, nil },
		kill: func(pid int) error {
			killCalled = true
			killedPID = pid
			return nil
		},
		waitDead: func(pid int) {}, // no-op: skip the real wait in tests
	})
	if !killCalled {
		t.Fatal("kill was not called for a live grafel engine.pid")
	}
	if killedPID != 4242 {
		t.Errorf("killed pid = %d, want 4242", killedPID)
	}
}

// TestReapStaleEngine_NoEnginePID_NoOp confirms the reap is a safe no-op when
// no engine.pid exists (fresh daemon root, or monolith mode never wrote one).
func TestReapStaleEngine_NoEnginePID_NoOp(t *testing.T) {
	killCalled := false
	reapStaleEngine(reapStaleEngineDeps{
		root:     t.TempDir(),
		readPID:  func(string) (int, bool) { return 0, false },
		isAlive:  func(int) bool { return true },
		isGrafel: func(int) (bool, error) { return true, nil },
		kill:     func(int) error { killCalled = true; return nil },
		waitDead: func(int) {},
	})
	if killCalled {
		t.Error("kill must not be called when engine.pid is absent")
	}
}

// TestReapStaleEngine_DeadPID_NoOp confirms the reap is a safe no-op when
// engine.pid names a pid that is no longer alive (the previous engine
// already exited cleanly and its own defer removed the pidfile, or a prior
// reap already handled it).
func TestReapStaleEngine_DeadPID_NoOp(t *testing.T) {
	killCalled := false
	reapStaleEngine(reapStaleEngineDeps{
		root:     t.TempDir(),
		readPID:  func(string) (int, bool) { return 4242, true },
		isAlive:  func(int) bool { return false },
		isGrafel: func(int) (bool, error) { return true, nil },
		kill:     func(int) error { killCalled = true; return nil },
		waitDead: func(int) {},
	})
	if killCalled {
		t.Error("kill must not be called when the recorded pid is no longer alive")
	}
}

// TestReapStaleEngine_EmptyRoot_NoOp verifies the reap never dereferences an
// unresolved root.
func TestReapStaleEngine_EmptyRoot_NoOp(t *testing.T) {
	killCalled := false
	reapStaleEngine(reapStaleEngineDeps{
		root:     "",
		readPID:  func(string) (int, bool) { return 4242, true },
		isAlive:  func(int) bool { return true },
		isGrafel: func(int) (bool, error) { return true, nil },
		kill:     func(int) error { killCalled = true; return nil },
		waitDead: func(int) {},
	})
	if killCalled {
		t.Error("kill must not be called with an empty root")
	}
}

// TestReapStaleEngine_RecycledPID_NotGrafel_NoOp is the PID-reuse safety
// case: engine.pid is stale and the OS has recycled that pid to an
// unrelated, innocent process. isAlive passes but PidIsGrafel returns false
// — the reap MUST NOT signal it.
func TestReapStaleEngine_RecycledPID_NotGrafel_NoOp(t *testing.T) {
	killCalled := false
	reapStaleEngine(reapStaleEngineDeps{
		root:     t.TempDir(),
		readPID:  func(string) (int, bool) { return 4242, true },
		isAlive:  func(int) bool { return true },
		isGrafel: func(int) (bool, error) { return false, nil },
		kill:     func(int) error { killCalled = true; return nil },
		waitDead: func(int) {},
	})
	if killCalled {
		t.Error("kill must not be called when the live pid is not a grafel process (recycled pid)")
	}
}

// TestReapStaleEngine_IdentityUnverifiable_NoOp confirms the fail-safe: when
// grafel-identity cannot be confirmed (e.g. a platform that cannot enumerate
// processes), the reap skips the kill.
func TestReapStaleEngine_IdentityUnverifiable_NoOp(t *testing.T) {
	killCalled := false
	reapStaleEngine(reapStaleEngineDeps{
		root:     t.TempDir(),
		readPID:  func(string) (int, bool) { return 4242, true },
		isAlive:  func(int) bool { return true },
		isGrafel: func(int) (bool, error) { return false, process.ErrUnsupported },
		kill:     func(int) error { killCalled = true; return nil },
		waitDead: func(int) {},
	})
	if killCalled {
		t.Error("kill must not be called when grafel-identity cannot be confirmed")
	}
}

// TestReapStaleEngine_ReadsRealPIDFile exercises the reap end to end against
// a real engine.pid file at EnginePIDPath(root), confirming it reads the
// SAME path RunEngine writes.
func TestReapStaleEngine_ReadsRealPIDFile(t *testing.T) {
	root := t.TempDir()
	pidPath := EnginePIDPath(root)
	if err := os.WriteFile(pidPath, []byte("777\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var killedPID int
	reapStaleEngine(reapStaleEngineDeps{
		root:     root,
		readPID:  readPID,
		isAlive:  func(pid int) bool { return pid == 777 },
		isGrafel: func(int) (bool, error) { return true, nil },
		kill:     func(pid int) error { killedPID = pid; return nil },
		waitDead: func(int) {},
	})
	if killedPID != 777 {
		t.Errorf("killed pid = %d, want 777 (read from %s)", killedPID, pidPath)
	}
}

// TestReapStaleEngine_NoRealPIDFile_NoOp confirms a directory with no
// engine.pid results in no kill, using the real readPID production function.
func TestReapStaleEngine_NoRealPIDFile_NoOp(t *testing.T) {
	root := t.TempDir()
	if _, err := os.Stat(filepath.Join(root, "engine.pid")); !os.IsNotExist(err) {
		t.Fatalf("test setup: engine.pid unexpectedly exists in %s", root)
	}

	killCalled := false
	reapStaleEngine(reapStaleEngineDeps{
		root:     root,
		readPID:  readPID,
		isAlive:  func(int) bool { return true },
		isGrafel: func(int) (bool, error) { return true, nil },
		kill:     func(int) error { killCalled = true; return nil },
		waitDead: func(int) {},
	})
	if killCalled {
		t.Error("kill must not be called when no real engine.pid file exists")
	}
}
