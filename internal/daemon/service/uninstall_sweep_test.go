package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
)

// TestSweepOrphanEngine_LiveEngineTerminated verifies that when engine.pid
// names a still-live process, the sweep terminates it (ADR-0024 PR5, epic
// #5729's belt-and-suspenders orphan-engine sweep on Uninstall).
func TestSweepOrphanEngine_LiveEngineTerminated(t *testing.T) {
	var killedPID int
	killCalled := false
	sweepOrphanEngine(sweepOrphanEngineDeps{
		root:    t.TempDir(),
		readPID: func(string) (int, error) { return 4242, nil },
		isAlive: func(pid int) bool { return pid == 4242 },
		kill: func(pid int) error {
			killCalled = true
			killedPID = pid
			return nil
		},
	})
	if !killCalled {
		t.Fatal("kill was not called for a live engine.pid")
	}
	if killedPID != 4242 {
		t.Errorf("killed pid = %d, want 4242", killedPID)
	}
}

// TestSweepOrphanEngine_NoEnginePID_NoOp verifies the sweep is a safe no-op
// when no engine.pid exists — the monolith-mode default (SplitMode off) has
// no separate engine process, so there is nothing to sweep.
func TestSweepOrphanEngine_NoEnginePID_NoOp(t *testing.T) {
	killCalled := false
	sweepOrphanEngine(sweepOrphanEngineDeps{
		root:    t.TempDir(),
		readPID: func(string) (int, error) { return 0, os.ErrNotExist },
		isAlive: func(int) bool { return true },
		kill:    func(int) error { killCalled = true; return nil },
	})
	if killCalled {
		t.Error("kill must not be called when engine.pid is absent (monolith mode)")
	}
}

// TestSweepOrphanEngine_DeadPID_NoOp verifies the sweep is a safe no-op when
// engine.pid names a pid that is no longer alive (the engine already exited
// — e.g. serve's own graceful drain already reaped it).
func TestSweepOrphanEngine_DeadPID_NoOp(t *testing.T) {
	killCalled := false
	sweepOrphanEngine(sweepOrphanEngineDeps{
		root:    t.TempDir(),
		readPID: func(string) (int, error) { return 4242, nil },
		isAlive: func(int) bool { return false },
		kill:    func(int) error { killCalled = true; return nil },
	})
	if killCalled {
		t.Error("kill must not be called when the recorded pid is no longer alive")
	}
}

// TestSweepOrphanEngine_EmptyRoot_NoOp verifies the sweep never dereferences
// an unresolved root (e.g. daemon.DefaultLayout failed).
func TestSweepOrphanEngine_EmptyRoot_NoOp(t *testing.T) {
	killCalled := false
	sweepOrphanEngine(sweepOrphanEngineDeps{
		root:    "",
		readPID: func(string) (int, error) { return 4242, nil },
		isAlive: func(int) bool { return true },
		kill:    func(int) error { killCalled = true; return nil },
	})
	if killCalled {
		t.Error("kill must not be called with an empty root")
	}
}

// TestSweepOrphanEngine_ReadsRealPIDFile exercises defaultReadEnginePID end
// to end against a real engine.pid file at daemon.EnginePIDPath(root),
// confirming the sweep reads the SAME path RunEngine writes.
func TestSweepOrphanEngine_ReadsRealPIDFile(t *testing.T) {
	root := t.TempDir()
	pidPath := daemon.EnginePIDPath(root)
	if err := os.WriteFile(pidPath, []byte("777\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var killedPID int
	sweepOrphanEngine(sweepOrphanEngineDeps{
		root:    root,
		readPID: defaultReadEnginePID,
		isAlive: func(pid int) bool { return pid == 777 },
		kill:    func(pid int) error { killedPID = pid; return nil },
	})
	if killedPID != 777 {
		t.Errorf("killed pid = %d, want 777 (read from %s)", killedPID, pidPath)
	}
}

// TestSweepOrphanEngine_NoRealPIDFile_NoOp confirms defaultReadEnginePID
// against a directory with no engine.pid (monolith mode) results in no kill,
// using the REAL isAlive/readPID production functions end to end (kill is
// still faked so the test never signals a real process).
func TestSweepOrphanEngine_NoRealPIDFile_NoOp(t *testing.T) {
	root := t.TempDir() // no engine.pid written
	if _, err := os.Stat(filepath.Join(root, "engine.pid")); !os.IsNotExist(err) {
		t.Fatalf("test setup: engine.pid unexpectedly exists in %s", root)
	}

	killCalled := false
	sweepOrphanEngine(sweepOrphanEngineDeps{
		root:    root,
		readPID: defaultReadEnginePID,
		isAlive: func(int) bool { return true },
		kill:    func(int) error { killCalled = true; return nil },
	})
	if killCalled {
		t.Error("kill must not be called when no real engine.pid file exists")
	}
}
