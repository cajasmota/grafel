package daemon

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// supervise_defaults_test.go grades #7106: both engine-supervisor give-up
// budgets — defaultEngineMaxSpawnFailures and defaultEngineMaxCeilingHits —
// shipped a PRODUCTION default that no test observed. Every supervisor test
// injects its own tuning (which is what lets them run in milliseconds), so the
// suite pinned "the budget mechanism works at whatever value you hand it" and
// said nothing about the value users run: raising either default to 1000 left
// ./internal/daemon/ green, fully restoring the ~91s walk #7103 exists to
// prevent.
//
// The pins here are BEHAVIOURAL, never constant-equality: each runs a
// supervisor that takes its give-up budget from newEngineSupervisor's
// production defaults, and asserts the give-up verdict arrives within a bounded
// number of spawn attempts — counted by the injected command factory, the
// external observation #7103 already uses — and NOT sooner than a couple of
// attempts, so a too-small default is caught too.
//
// No assertion is gated on wall-clock timing (#7062). The attempt bound is
// checked as attempts happen, so an over-large budget fails in a fraction of a
// second instead of waiting out a deadline; the only deadline is a generous
// outer bound whose message names what actually failed.

// spawnBudgetMaxAttempts is the behavioural bound on how many times production
// re-attempts a deterministic construction failure before giving up: a handful,
// not a long budget. This is what makes a deterministic `Start` error die in
// ~1.5s rather than walk the crash budget (#7087).
const spawnBudgetMaxAttempts = 5

// spawnBudgetMinAttempts is the other direction: some construction failures are
// transient (EAGAIN under fork pressure, a briefly-locked binary mid-upgrade),
// so serve must not die on the FIRST one. A default of 1 would make one hiccup
// fatal.
const spawnBudgetMinAttempts = 2

// crashBudgetMaxAttempts bounds how many relaunches at the backoff ceiling
// production tolerates before declaring the engine unkeepable.
const crashBudgetMaxAttempts = 6

// crashBudgetMinAttempts: a crashing engine gets more than a couple of
// relaunches before serve recycles itself; a default of 1 would surrender after
// the first ceiling hit.
const crashBudgetMinAttempts = 3

// runWithDefaultBudget runs a supervisor whose give-up budgets come from
// newEngineSupervisor's production defaults, with tune applied for anything the
// test may legitimately shrink (the WAITS, never a budget). mk builds the child
// command, once per spawn attempt.
//
// It returns the fatal error, the attempt count and the log stream. It fails the
// test as soon as attempts exceed maxAttempts — so an inflated default costs a
// fraction of a second, not a deadline — and its outer deadline names the real
// failure.
func runWithDefaultBudget(t *testing.T, tune func(*engineSupervisor), mk func() *exec.Cmd, maxAttempts int) (err error, attempts int, logs string) {
	t.Helper()
	root := isolateSpawnFailEnv(t)

	var mu sync.Mutex
	n := 0
	countAttempts := func() int {
		mu.Lock()
		defer mu.Unlock()
		return n
	}
	defer SetEngineChildCommandForTest(func(_ string, _ string) *exec.Cmd {
		mu.Lock()
		n++
		mu.Unlock()
		return mk()
	})()

	sink := &lockedBuf{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sup := newEngineSupervisor(layoutFromRoot(root, ""), buildSlogLogger(sink))
	if tune != nil {
		tune(sup)
	}
	if serr := sup.start(ctx); serr != nil {
		t.Fatalf("supervisor start: %v", serr)
	}
	t.Cleanup(sup.stop)

	// Outer bound only: nothing here asserts a duration. It is generous enough
	// that production tuning (whose waits are 0.5s + 1s on the spawn path)
	// finishes well inside it.
	deadline := time.Now().Add(60 * time.Second)
	for {
		select {
		case err = <-sup.fatal():
			sup.stop()
			return err, countAttempts(), sink.String()
		case <-time.After(2 * time.Millisecond):
		}
		if got := countAttempts(); got > maxAttempts {
			sup.stop()
			t.Fatalf("supervisor made %d spawn attempts without giving up, want at most %d: the production give-up budget lets serve re-attempt an identically-failing spawn far longer than it should\nlogs:\n%s",
				got, maxAttempts, sink.String())
		}
		if time.Now().After(deadline) {
			sup.stop()
			t.Fatalf("supervisor never surfaced a fatal within the test's own 60s outer bound (spawn attempts so far: %d) — the give-up verdict never arrived\nlogs:\n%s",
				countAttempts(), sink.String())
		}
	}
}

// TestEngineSupervisor_DefaultSpawnFailureBudgetIsBounded pins
// defaultEngineMaxSpawnFailures through the behaviour it buys, with NO tuning
// injected at all: a deterministic construction failure (a child binary that
// does not exist) must reach the unspawnable verdict within a handful of spawn
// attempts, and must not reach it on the first one. At production tuning the two
// intervening waits are 0.5s + 1s, so this costs ~1.5s of wall clock.
func TestEngineSupervisor_DefaultSpawnFailureBudgetIsBounded(t *testing.T) {
	err, attempts, logs := runWithDefaultBudget(t, nil,
		func() *exec.Cmd { return unspawnableCommand(t) }, spawnBudgetMaxAttempts)

	if err == nil {
		t.Fatalf("no fatal surfaced for a permanently unspawnable engine child; logs:\n%s", logs)
	}
	// Emitted artefact: the fatal text serve surfaces, not a counter the
	// supervisor keeps about itself.
	if !strings.Contains(err.Error(), "unspawnable") {
		t.Errorf("fatal does not name the unspawnable verdict: %q", err)
	}
	if attempts > spawnBudgetMaxAttempts {
		t.Errorf("gave up only after %d spawn attempts at production tuning, want at most %d: a deterministic Start error must die quickly, not walk a long budget",
			attempts, spawnBudgetMaxAttempts)
	}
	if attempts < spawnBudgetMinAttempts {
		t.Errorf("gave up after only %d spawn attempt(s) at production tuning, want at least %d: a single construction failure can be transient (EAGAIN, a binary locked mid-upgrade) and must not kill serve",
			attempts, spawnBudgetMinAttempts)
	}
	// Positive control: this test really did exercise the construction-failure
	// path, not a crash.
	if !strings.Contains(logs, "no child process was created") {
		t.Errorf("no construction failure was ever emitted — this test graded nothing; logs:\n%s", logs)
	}
}

// TestEngineSupervisor_DefaultCrashBudgetIsBounded pins
// defaultEngineMaxCeilingHits the same way. The crash path's WAIT is injectable
// separately from its BUDGET (waitBackoff owns backoffInitial/backoffMax;
// backoffAndMaybeGiveUp owns maxCeilingHits), so the two waits are shrunk to
// milliseconds while maxCeilingHits stays exactly what newEngineSupervisor gave
// it — which is how the ~91s production budget is graded in well under a second
// without waiting it out.
func TestEngineSupervisor_DefaultCrashBudgetIsBounded(t *testing.T) {
	selfExe, xerr := os.Executable()
	if xerr != nil {
		t.Fatalf("os.Executable: %v", xerr)
	}

	err, attempts, logs := runWithDefaultBudget(t, func(s *engineSupervisor) {
		// WAITS only. maxCeilingHits and healthyUptime are left at the
		// production defaults; the children here exit at once, so they can
		// never cross healthyUptime and reset the budget.
		s.backoffInitial = time.Millisecond
		s.backoffMax = 2 * time.Millisecond
		s.drainTimeout = 2 * time.Second
	}, func() *exec.Cmd { return instantExitCommand(selfExe) }, crashBudgetMaxAttempts)

	if err == nil {
		t.Fatalf("no fatal surfaced for an engine child that crashes every time; logs:\n%s", logs)
	}
	if !strings.Contains(err.Error(), "crash-looping") {
		t.Errorf("fatal does not name the crash-loop verdict: %q", err)
	}
	if attempts > crashBudgetMaxAttempts {
		t.Errorf("gave up only after %d relaunches at the production ceiling budget, want at most %d: serve must recycle rather than relaunch an unkeepable engine indefinitely",
			attempts, crashBudgetMaxAttempts)
	}
	if attempts < crashBudgetMinAttempts {
		t.Errorf("gave up after only %d relaunch(es) at the production ceiling budget, want at least %d: a crashing engine must be retried more than a couple of times before serve declares it unkeepable",
			attempts, crashBudgetMinAttempts)
	}
	// Positive control: children really did run and die (a crash), so this test
	// graded the crash budget and not the construction-failure one.
	if !strings.Contains(logs, "engine child exited") {
		t.Errorf("no child ever ran and exited — this test graded the wrong path; logs:\n%s", logs)
	}
	if strings.Contains(logs, "no child process was created") {
		t.Errorf("a construction failure leaked into the crash-budget pin; logs:\n%s", logs)
	}
}
