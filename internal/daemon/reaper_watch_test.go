package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon/watchreg"
)

// TestReaper_sweepsWatchRegistry verifies the #5142 wiring: Reaper.Sweep drops
// dead watcher PIDs and reaps orphaned ones via the injected WatchRegistry,
// independent of the vanished-repo GC. It uses the registry's real on-disk
// store but relies on the package-level liveness probe — so we register only
// PIDs that are genuinely dead (a never-allocated high PID and the reaper's own
// orphan logic) to keep the assertion deterministic without spawning processes.
func TestReaper_sweepsWatchRegistry(t *testing.T) {
	reg := watchreg.New(filepath.Join(t.TempDir(), watchreg.FileName))

	// A PID that is certainly dead (process enumeration treats it as gone).
	deadPID := 999_999_999
	if err := reg.Register(watchreg.Entry{PID: deadPID, Repo: "/gone", OwnerDaemonPID: 4242}); err != nil {
		t.Fatalf("register: %v", err)
	}

	r := NewReaper(ReaperConfig{
		WatchRegistry: reg,
		// No TrackedRepos → vanished-repo GC is a no-op; only watcher sweep runs.
	})
	res := r.Sweep()
	if res.WatchersReaped != 1 {
		t.Fatalf("WatchersReaped = %d, want 1 (dead PID reaped)", res.WatchersReaped)
	}
	got, _ := reg.List()
	if len(got) != 0 {
		t.Fatalf("dead watcher entry should be gone, registry has %d entries", len(got))
	}
}

// TestReaper_watchSweepDisabledWhenNil: with no WatchRegistry, the watcher
// sweep is a no-op and the zero-tracker result stays empty (matches the
// existing TestReaper_noOpWhenNoTracker contract).
func TestReaper_watchSweepDisabledWhenNil(t *testing.T) {
	r := NewReaper(ReaperConfig{})
	if res := r.Sweep(); res != (ReapResult{}) {
		t.Fatalf("nil WatchRegistry + nil TrackedRepos should yield zero result, got %+v", res)
	}
}

// TestReaper_sweepWatchers_NilLiveDaemonPIDFailsClosed is the #5933 regression
// test.
//
// In split mode (ADR-0024) the watcher stamps OwnerDaemonPID from the
// daemon/serve pidfile (internal/cli/watch.go), but the Reaper that performs
// the sweep runs inside the ENGINE process — a different PID. Before this fix,
// an unset ReaperConfig.LiveDaemonPID fell back to os.Getpid() (the ENGINE's
// own pid), which can never equal the daemon pidfile's pid, so every live
// watcher was misclassified as orphaned and SIGTERM'd on every sweep.
//
// This test simulates exactly that shape: the watcher entry is a genuinely
// alive (but disposable) child process, stamped with an OwnerDaemonPID
// standing in for the daemon pidfile's PID — a value guaranteed to differ
// from this test process's own os.Getpid() (the stand-in for the engine's
// pid, per the real sweepWatchers default). With LiveDaemonPID left unset
// (today's engineplane.go wiring), the fix requires the sweep to fail CLOSED
// (skip the orphan-kill branch) rather than default to os.Getpid(), so the
// entry — and the process behind it — must survive.
func TestReaper_sweepWatchers_NilLiveDaemonPIDFailsClosed(t *testing.T) {
	reg := watchreg.New(filepath.Join(t.TempDir(), watchreg.FileName))

	// A real, disposable child process so the sweep's hardcoded liveness
	// probe (signal-0) sees a genuinely alive PID without touching this test
	// process itself (which the buggy pre-fix path would SIGTERM).
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start watcher stand-in process: %v", err)
	}
	watcherPID := cmd.Process.Pid
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	// A daemon pidfile PID that is guaranteed to differ from this test
	// process's own PID (the stand-in for the engine's os.Getpid()).
	daemonPID := os.Getpid() + 1

	if err := reg.Register(watchreg.Entry{PID: watcherPID, Repo: "/live", OwnerDaemonPID: daemonPID}); err != nil {
		t.Fatalf("register: %v", err)
	}

	r := NewReaper(ReaperConfig{
		WatchRegistry: reg,
		// LiveDaemonPID intentionally left nil — reproduces engineplane.go's
		// pre-fix wiring (#5933).
	})
	res := r.Sweep()
	if res.WatchersReaped != 0 {
		t.Fatalf("WatchersReaped = %d, want 0 (live watcher must survive when LiveDaemonPID is unset)", res.WatchersReaped)
	}
	got, _ := reg.List()
	if len(got) != 1 || got[0].PID != watcherPID {
		t.Fatalf("live watcher entry should still be registered, got %v", got)
	}
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("watcher stand-in process should still be alive, signal-0 probe: %v", err)
	}
}
