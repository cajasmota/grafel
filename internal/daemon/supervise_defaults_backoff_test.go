package daemon

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// supervise_defaults_backoff_test.go grades #7111's two WAIT-SHAPE defaults —
// defaultEngineBackoffInitial and defaultEngineBackoffMax — which shipped
// production values nothing observed: shrinking them to 1ns and 3ms
// respectively left ./internal/daemon/ green. Same cause as #7106/#7110: every
// supervisor test injects its own tuning, so the suite pins "the backoff
// mechanism works at whatever value you hand it" and says nothing about the
// values users run.
//
// These two are harder than #7110's give-up budgets, because their direct
// consequence IS elapsed time, and elapsed time is the one thing this package
// must not assert naively (#7062: a 100ms behaviour behind two 5s deadlines
// whose failure message blamed a leak when the test's own budget was what
// blew). The observation used here is the SPACING BETWEEN SPAWN ATTEMPTS,
// counted and timestamped by the injected command factory — the same external
// artefact #7103/#7110 already observe — and every bound is chosen so that it
// cannot flake:
//
//   - The FLOOR bounds ("attempt N+1 did not arrive sooner than X") cannot be
//     broken by a loaded machine at all. Load only ever makes a relaunch
//     LATER, never earlier, so a slow machine can never fail a floor.
//   - The single CEILING bound (attempt 2 arrives within 5s against a 500ms
//     production wait) carries 10x slack, the shape #7110's outer bounds use.
//
// Nothing here asserts a constant's value, in any notation: the pins are the
// observed relaunch spacing, and the bands are wide enough that many values
// other than the shipped ones pass (scored: 250ms/1s/2s all survive the
// backoff-initial pin, 1s/2s/30s all survive the ceiling pin).
//
// NOT graded here, and deliberately: the "too LARGE" direction of
// defaultEngineBackoffMax. Its only consequence is how long the crash-loop
// give-up takes to arrive, and observing the ceiling at all requires walking
// the doublings up to it — ~60s of cumulative sleeping at production tuning
// before the first ceiling hit even happens, with the give-up ~60s after that.
// That cannot be pinned without a multi-minute wall-clock test, which is worse
// than leaving the direction ungraded (#7062). It is recorded as ungraded, not
// as graded.

// backoffInitialFloor is the floor on the FIRST relaunch wait: after a failed
// spawn, production must not re-attempt within this window. A degenerate
// initial backoff (the 1ns mutant) turns the supervisor into a hot relaunch
// loop that burns the whole spawn budget in microseconds and takes fork
// pressure with it.
const backoffInitialFloor = 100 * time.Millisecond

// backoffInitialCeiling is the other direction: the first relaunch must be
// PROMPT. A transient spawn failure (EAGAIN under fork pressure, a binary
// locked mid-upgrade) must not leave the engine down for seconds. 10x the
// shipped 500ms, so only a materially inflated default fails it.
const backoffInitialCeiling = 5 * time.Second

// backoffCeilingProbeInitial is the FIRST wait injected by the backoff-ceiling
// pin. It is the test's own choice, not a default, which is what keeps that pin
// independent of defaultEngineBackoffInitial: mutating the initial-backoff
// default cannot fail the ceiling pin, so each constant is killed by its own
// assertion rather than the two failing together.
const backoffCeilingProbeInitial = 400 * time.Millisecond

// backoffCeilingFloor: with backoffCeilingProbeInitial injected, the SECOND
// wait is min(2*400ms, backoffMax). Requiring it to exceed this floor requires
// the production ceiling to leave room for at least one real doubling — the
// property a 3ms ceiling destroys, collapsing exponential backoff into a fixed
// millisecond-scale retry. A floor, so load can never break it.
const backoffCeilingFloor = 600 * time.Millisecond

// spawnAttemptClock timestamps each spawn attempt the supervisor makes. The
// attempt count and its timing are observed from OUTSIDE the supervisor (the
// injected command factory), never from a counter it keeps about itself.
type spawnAttemptClock struct {
	mu sync.Mutex
	at []time.Time
}

func (c *spawnAttemptClock) mark() {
	c.mu.Lock()
	c.at = append(c.at, time.Now())
	c.mu.Unlock()
}

func (c *spawnAttemptClock) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.at)
}

// gap returns the interval between attempt i and attempt i+1 (1-based: gap(1)
// is the wait between the first and second spawn attempt), and whether both
// attempts have happened yet.
func (c *spawnAttemptClock) gap(i int) (time.Duration, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.at) < i+1 {
		return 0, false
	}
	return c.at[i].Sub(c.at[i-1]), true
}

// runSpawnFailWithDefaultWaits runs a supervisor on the construction-failure
// path (a child binary that does not exist, so cmd.Start fails identically
// every time — no processes are created and nothing depends on child
// scheduling) with tune applied for anything the test legitimately injects.
// Everything tune does not touch comes from newEngineSupervisor's production
// defaults.
//
// It waits until want spawn attempts have been observed, or until the outer
// bound elapses, and returns the attempt clock and the log stream. The outer
// bound asserts nothing about the defaults: its failure message names what
// actually failed.
func runSpawnFailWithDefaultWaits(t *testing.T, tune func(*engineSupervisor), want int, outer time.Duration) (*spawnAttemptClock, string) {
	t.Helper()
	root := isolateSpawnFailEnv(t)

	clock := &spawnAttemptClock{}
	defer SetEngineChildCommandForTest(func(_ string, _ string) *exec.Cmd {
		clock.mark()
		return unspawnableCommand(t)
	})()

	sink := &lockedBuf{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sup := newEngineSupervisor(layoutFromRoot(root, ""), buildSlogLogger(sink))
	if tune != nil {
		tune(sup)
	}
	if err := sup.start(ctx); err != nil {
		t.Fatalf("supervisor start: %v", err)
	}
	defer sup.stop()

	deadline := time.Now().Add(outer)
	for clock.count() < want {
		if time.Now().After(deadline) {
			t.Fatalf("only %d spawn attempt(s) observed within the test's own %s outer bound, want %d — the supervisor stopped re-attempting\nlogs:\n%s",
				clock.count(), outer, want, sink.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	return clock, sink.String()
}

// TestEngineSupervisor_DefaultBackoffInitialIsAHumanScaleWait pins
// defaultEngineBackoffInitial with NO tuning injected at all, through the
// behaviour it buys: the interval between the first and second spawn attempt.
// Both directions are bounded — a degenerate wait (hot relaunch loop) and an
// inflated one (the engine stays down through a transient failure) each fail
// their own assertion. Costs ~0.5s of wall clock (one production first
// backoff).
func TestEngineSupervisor_DefaultBackoffInitialIsAHumanScaleWait(t *testing.T) {
	clock, logs := runSpawnFailWithDefaultWaits(t, nil, 2, backoffInitialCeiling+10*time.Second)

	gap, ok := clock.gap(1)
	if !ok {
		t.Fatalf("fewer than two spawn attempts recorded; logs:\n%s", logs)
	}
	if gap < backoffInitialFloor {
		t.Errorf("production relaunched only %s after the failed spawn, want at least %s: a degenerate initial backoff makes the supervisor a hot relaunch loop that burns the whole spawn budget in microseconds",
			gap, backoffInitialFloor)
	}
	if gap > backoffInitialCeiling {
		t.Errorf("production waited %s before relaunching, want at most %s: a transient spawn failure must not leave the engine down for seconds",
			gap, backoffInitialCeiling)
	}
	// Positive control: this really exercised the construction-failure path
	// (no child ever existed), so the gap measured is a backoff and not a
	// child's lifetime.
	if !strings.Contains(logs, "no child process was created") {
		t.Errorf("no construction failure was ever emitted — this test graded nothing; logs:\n%s", logs)
	}
	if !strings.Contains(logs, "relaunching engine after backoff") {
		t.Errorf("the supervisor never reported a backoff wait — the gap measured is not a backoff; logs:\n%s", logs)
	}
}

// TestEngineSupervisor_DefaultBackoffCeilingLeavesRoomToGrow pins
// defaultEngineBackoffMax in the SHRINK direction: with a 400ms first wait
// injected (the test's own value, so this pin is independent of
// defaultEngineBackoffInitial) and the ceiling left at production's, the SECOND
// wait must actually be a doubling rather than the ceiling clamping it. A 3ms
// ceiling collapses exponential backoff into a fixed millisecond retry, which
// is the shape that relaunches a dying engine hundreds of times a second.
//
// The GROWTH direction of this constant is NOT graded here — see the file
// comment: observing the ceiling itself costs minutes of wall clock.
//
// Costs ~1.2s of wall clock (400ms + 800ms of injected waits).
func TestEngineSupervisor_DefaultBackoffCeilingLeavesRoomToGrow(t *testing.T) {
	clock, logs := runSpawnFailWithDefaultWaits(t, func(s *engineSupervisor) {
		// The first WAIT only. backoffMax stays exactly what
		// newEngineSupervisor gave it — that is the constant under test.
		s.backoffInitial = backoffCeilingProbeInitial
	}, 3, 30*time.Second)

	gap, ok := clock.gap(2)
	if !ok {
		t.Fatalf("fewer than three spawn attempts recorded; logs:\n%s", logs)
	}
	if gap < backoffCeilingFloor {
		t.Errorf("the second relaunch wait was %s after a %s first wait, want at least %s: the production backoff ceiling is clamping the wait before it can double even once, so backoff is not exponential at all",
			gap, backoffCeilingProbeInitial, backoffCeilingFloor)
	}
	if !strings.Contains(logs, "no child process was created") {
		t.Errorf("no construction failure was ever emitted — this test graded nothing; logs:\n%s", logs)
	}
}
