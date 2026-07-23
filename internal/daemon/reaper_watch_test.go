package daemon

import (
	"os"
	"path/filepath"
	"strconv"
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
// alive (but disposable) child process — spawned via the package's existing
// portable helper (spawnLiveChild, internal/daemon/pidfile_test.go) so this
// runs cleanly on the Windows leg of the release matrix, not just
// darwin/linux — stamped with an OwnerDaemonPID standing in for the daemon
// pidfile's PID: a value guaranteed to differ from this test process's own
// os.Getpid() (the stand-in for the engine's pid, per the real sweepWatchers
// default). With LiveDaemonPID left unset (today's engineplane.go wiring),
// the fix requires the sweep to fail CLOSED (skip the orphan-kill branch)
// rather than default to os.Getpid(), so the entry — and the process behind
// it — must survive.
func TestReaper_sweepWatchers_NilLiveDaemonPIDFailsClosed(t *testing.T) {
	reg := watchreg.New(filepath.Join(t.TempDir(), watchreg.FileName))

	watcherPID, cleanup := spawnLiveChild(t)
	defer cleanup()

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
}

// TestReaper_sweepWatchers_LiveDaemonPIDFromPidfile is the #5933 positive-case
// companion to the fail-closed test above: it proves the OTHER half of the
// contract — that a correctly-resolving LiveDaemonPID (the same shape
// engineplane.go wires: a closure reading the daemon/serve pidfile via
// ReadPIDFile) both (a) KEEPS a live watcher whose OwnerDaemonPID matches the
// pidfile's contents, and (b) still REAPS a live watcher whose OwnerDaemonPID
// does not — i.e. genuine orphan detection still works once the wiring is
// correct. Without this, only the "everything survives" half of #5933 would
// be covered, and a regression that wires LiveDaemonPID to the WRONG pid (or
// removes it) would not necessarily be caught.
func TestReaper_sweepWatchers_LiveDaemonPIDFromPidfile(t *testing.T) {
	reg := watchreg.New(filepath.Join(t.TempDir(), watchreg.FileName))

	// A real on-disk pidfile, written the same way AcquirePIDFile does, so the
	// closure below exercises the actual ReadPIDFile parse path — not a faked
	// int — mirroring engineplane.go's `func() int { return
	// ReadPIDFile(cfg.Layout.PIDPath) }` wiring exactly.
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	const daemonPID = 424242
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(daemonPID)+"\n"), 0o600); err != nil {
		t.Fatalf("write fake daemon pidfile: %v", err)
	}

	survivorPID, survivorCleanup := spawnLiveChild(t)
	defer survivorCleanup()
	orphanPID, orphanCleanup := spawnLiveChild(t)
	defer orphanCleanup()

	if err := reg.Register(watchreg.Entry{PID: survivorPID, Repo: "/kept", OwnerDaemonPID: daemonPID}); err != nil {
		t.Fatalf("register survivor: %v", err)
	}
	if err := reg.Register(watchreg.Entry{PID: orphanPID, Repo: "/reaped", OwnerDaemonPID: daemonPID + 1}); err != nil {
		t.Fatalf("register orphan: %v", err)
	}

	r := NewReaper(ReaperConfig{
		WatchRegistry: reg,
		LiveDaemonPID: func() int { return ReadPIDFile(pidPath) },
	})
	res := r.Sweep()
	if res.WatchersReaped != 1 {
		t.Fatalf("WatchersReaped = %d, want 1 (only the mismatched-owner watcher)", res.WatchersReaped)
	}
	got, _ := reg.List()
	if len(got) != 1 || got[0].PID != survivorPID {
		t.Fatalf("registry after sweep = %v, want only survivor pid %d kept", got, survivorPID)
	}
}
