//go:build darwin || linux

package daemon

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// supervise_defaults_drain_test.go grades #7111's third ungraded supervisor
// default: defaultEngineDrainTimeout, the graceful SIGTERM→exit window before
// the drain escalates to SIGKILL. Shrinking it to 1ns left ./internal/daemon/
// green — every drain test injects its own drainTimeout, so nothing observed
// the 5s users actually run.
//
// Both directions are graded, each by its own test, and both from EMITTED
// artefacts — the two mutually exclusive lines terminateChild writes:
//
//	"engine child exited after SIGTERM"                     (graceful)
//	"engine child did not exit within drain window — SIGKILL" (escalation)
//
// #7119's review found that "engine child exited" is ALSO a prefix of the crash
// path's own line, so the graceful line is matched by the whole distinguishing
// fragment ("exited after SIGTERM"), never by that prefix.
//
// Unix-only: on Windows signalTerminate IS p.Kill() (see supervise_windows.go),
// so a child cannot ignore the first signal and the drain window is not
// observable there at all. Stated rather than faked.
//
// Timing discipline (#7062): neither arm here is a FLOOR, so both are
// enumerated with their slack rather than waved through (#7123).
//
//   - The graceful arm's "the escalation line was NOT emitted" needs a child
//     that takes drainSlowChildExitDelay to unwind to finish inside
//     production's real 5s window: 12.5x slack, so a machine would have to
//     stretch a 400ms unwind past 5s to break it.
//   - The escalation arm's waitFor(drainForceKillOuterBound) is a ceiling with
//     4x slack, the shape #7110's outer bounds use.

// drainSlowChildExitDelay is how long the graceful-arm helper takes to shut
// down after SIGTERM. Production's drain window must comfortably cover a child
// that needs a fraction of a second to unwind (the engine closes a scheduler, a
// watcher and an mmap'd graph) — it must not SIGKILL an engine mid-unwind.
const drainSlowChildExitDelay = 400 * time.Millisecond

// drainForceKillOuterBound bounds the OTHER direction: a child that will never
// exit on SIGTERM must be force-killed, and serve's shutdown must not hang
// waiting for it. 4x the shipped 5s, so only a materially inflated window
// fails, and a loaded machine cannot flake it.
const drainForceKillOuterBound = 20 * time.Second

// drainGracefulLine and drainForceKillLine are the two emitted artefacts that
// tell the drain's arms apart. drainGracefulLine deliberately starts mid-phrase:
// "engine child exited" alone also matches the crash path's line (#7119).
const (
	drainGracefulLine  = "engine child exited after SIGTERM"
	drainForceKillLine = "engine child did not exit within drain window"
)

// TestSupervisorDrainSlowExitHelper is the child-process entrypoint for the
// graceful arm: it catches SIGTERM and takes drainSlowChildExitDelay to exit,
// like an engine unwinding its scheduler. Inert in the parent suite.
func TestSupervisorDrainSlowExitHelper(t *testing.T) {
	if os.Getenv("GRAFEL_DRAIN_SLOW_HELPER") != "1" {
		return
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM)
	// Readiness is published only AFTER the handler is installed, so the
	// parent can never SIGTERM this process while the default disposition
	// (immediate death) is still in force — which would make the graceful arm
	// pass without the drain window ever being the reason.
	drainHelperPublishReady()
	select {
	case <-sigs:
		time.Sleep(drainSlowChildExitDelay)
	case <-time.After(60 * time.Second):
	}
	os.Exit(0)
}

// TestSupervisorDrainIgnoreSIGTERMHelper is the child-process entrypoint for
// the escalation arm: it swallows SIGTERM entirely, so only the supervisor's
// SIGKILL can end it. Inert in the parent suite.
func TestSupervisorDrainIgnoreSIGTERMHelper(t *testing.T) {
	if os.Getenv("GRAFEL_DRAIN_IGNORE_HELPER") != "1" {
		return
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM)
	drainHelperPublishReady()
	time.Sleep(60 * time.Second)
	os.Exit(0)
}

// drainHelperPublishReady writes the readiness marker the parent waits on.
func drainHelperPublishReady() {
	if p := os.Getenv("GRAFEL_DRAIN_READY_FILE"); p != "" {
		_ = os.WriteFile(p, []byte("1"), 0o600)
	}
}

// drainHelperCommand builds a child command that re-invokes the test binary at
// the named helper entrypoint, in its own process group (mirroring production's
// engineChildSysProcAttr, which is what makes the drain signals group-directed).
func drainHelperCommand(selfExe, root, runName, envKey, readyFile string) *exec.Cmd {
	cmd := exec.Command(selfExe, "-test.run="+runName, "-test.timeout=90s")
	cmd.Env = append(os.Environ(),
		envKey+"=1",
		"GRAFEL_DRAIN_READY_FILE="+readyFile,
		EnvRoot+"="+root,
	)
	cmd.SysProcAttr = engineChildSysProcAttr()
	return cmd
}

// startDrainSupervisor starts a supervisor with NO tuning injected — its drain
// window is newEngineSupervisor's production default, the constant under test —
// spawning the named helper, and returns once the helper has published
// readiness (its SIGTERM disposition is settled).
func startDrainSupervisor(t *testing.T, runName, envKey string) (*engineSupervisor, *lockedBuf) {
	t.Helper()
	root := isolateSupervisorEnv(t)
	readyFile := filepath.Join(root, "drain-helper-ready")
	t.Cleanup(SetEngineChildCommandForTest(func(selfExe, r string) *exec.Cmd {
		return drainHelperCommand(selfExe, r, runName, envKey, readyFile)
	}))

	sink := &lockedBuf{}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	sup := newEngineSupervisor(layoutFromRoot(root, ""), buildSlogLogger(sink))
	if err := sup.start(ctx); err != nil {
		t.Fatalf("supervisor start: %v", err)
	}
	t.Cleanup(sup.stop)

	if !waitFor(t, 30*time.Second, func() bool {
		_, err := os.Stat(readyFile)
		return err == nil
	}) {
		t.Fatalf("drain helper never published readiness — no supervised child to drain; logs:\n%s", sink.String())
	}
	return sup, sink
}

// TestEngineSupervisor_DefaultDrainTimeoutSurvivesASlowExit pins
// defaultEngineDrainTimeout in the TOO-SMALL direction: a child that needs
// drainSlowChildExitDelay to unwind after SIGTERM must be allowed to finish, so
// the drain emits the graceful line and never the escalation one. A 1ns (or
// 100ms) window SIGKILLs an engine mid-unwind. Costs ~0.4s of wall clock.
func TestEngineSupervisor_DefaultDrainTimeoutSurvivesASlowExit(t *testing.T) {
	sup, sink := startDrainSupervisor(t, "TestSupervisorDrainSlowExitHelper", "GRAFEL_DRAIN_SLOW_HELPER")

	sup.stop()
	logs := sink.String()

	if !strings.Contains(logs, drainGracefulLine) {
		t.Errorf("the production drain window did not let a child that needs %s to unwind exit on its own; logs:\n%s",
			drainSlowChildExitDelay, logs)
	}
	if strings.Contains(logs, drainForceKillLine) {
		t.Errorf("the production drain window escalated to SIGKILL against a child that only needed %s to exit — an engine killed mid-unwind; logs:\n%s",
			drainSlowChildExitDelay, logs)
	}
	// Positive control: a drain really happened (rather than the child dying
	// for some unrelated reason before stop was called).
	if !strings.Contains(logs, "draining engine child") {
		t.Errorf("no drain was ever attempted — this test graded nothing; logs:\n%s", logs)
	}
}

// TestEngineSupervisor_DefaultDrainTimeoutEscalatesPromptly pins the same
// constant in the TOO-LARGE direction: against a child that never exits on
// SIGTERM, the escalation must arrive — and serve's shutdown must complete —
// well inside drainForceKillOuterBound. An inflated window hangs every serve
// shutdown, upgrade and restart for its full length.
//
// This is the one assertion here that costs the window itself: ~5s at
// production tuning, against a 20s bound. There is no cheaper observation —
// "the wait ends" cannot be observed without the wait.
func TestEngineSupervisor_DefaultDrainTimeoutEscalatesPromptly(t *testing.T) {
	sup, sink := startDrainSupervisor(t, "TestSupervisorDrainIgnoreSIGTERMHelper", "GRAFEL_DRAIN_IGNORE_HELPER")

	stopped := make(chan struct{})
	go func() {
		sup.stop()
		close(stopped)
	}()

	if !waitFor(t, drainForceKillOuterBound, func() bool {
		return strings.Contains(sink.String(), drainForceKillLine)
	}) {
		t.Fatalf("a child that ignores SIGTERM was still not force-killed %s into the drain: the production drain window hangs every serve shutdown for its full length; logs:\n%s",
			drainForceKillOuterBound, sink.String())
	}
	select {
	case <-stopped:
	case <-time.After(30 * time.Second):
		t.Fatalf("supervisor stop did not return after the force kill; logs:\n%s", sink.String())
	}
	// Positive control: the escalation was reached because SIGTERM was
	// swallowed, not because the child exited gracefully.
	if strings.Contains(sink.String(), drainGracefulLine) {
		t.Errorf("the child exited on SIGTERM after all — the escalation branch was not what this test observed; logs:\n%s", sink.String())
	}
}
