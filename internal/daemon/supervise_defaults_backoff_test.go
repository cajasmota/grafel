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
// blew). Two observations are used, both external to the supervisor:
//
//   - the SPACING BETWEEN SPAWN ATTEMPTS, counted and timestamped by the
//     injected command factory — the same artefact #7103/#7110 already
//     observe;
//   - whether the crash-loop GIVE-UP VERDICT is emitted at all, read from
//     sup.fatal(), for the ceiling's growth direction (see below).
//
// Every bound is chosen so a loaded machine cannot break it:
//
//   - The FLOOR bounds ("attempt N+1 did not arrive sooner than X") cannot be
//     broken by load at all. Load only ever makes a relaunch LATER, never
//     earlier, so a slow machine can never fail a floor.
//   - The ceiling band's two arms have NO timing bound at all: each waits for
//     whichever of two mutually exclusive emitted lines the supervisor
//     produces on the first crash (give up, or announce a wait), so load
//     changes when the answer arrives and never which answer it is. Their
//     shared outer bound asserts nothing about a default — exceeding it means
//     no crash was supervised, and the message says so.
//   - That leaves ONE ceiling bound on a default in this file: "attempt 2
//     arrives within 5s" against a 500ms production wait, 10x slack, the shape
//     #7110's outer bounds use. (supervise_defaults_drain_test.go carries the
//     other two non-floor bounds, enumerated in its own header.)
//
// Nothing here asserts a constant's value, in any notation: the pins are the
// observed relaunch spacing and the presence or absence of an emitted verdict,
// and the bands are wide enough that many values other than the shipped ones
// pass (scored: 250ms/1s/2s all survive the backoff-initial pin; 20s, 45s and
// 60s all survive the two-sided ceiling band).
//
// The GROWTH direction of defaultEngineBackoffMax looks like it needs a
// multi-minute wall-clock test — walking the doublings up to a 30s ceiling is
// ~60s of sleeping, with the give-up ~60s after that. It does not, because
// backoffAndMaybeGiveUp compares the PENDING backoff against s.backoffMax
// BEFORE waitBackoff sleeps for it. Injecting the first WAIT (a constant
// #7111 grades separately, never backoffMax itself) together with
// maxCeilingHits=1 (graded by #7110) therefore decides the verdict the instant
// the first child dies, at any value of backoffMax. That gives a two-sided
// band on the ceiling — 15s < backoffMax <= 60s — for two child spawns of wall
// clock and no sleeping at all (#7123).

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
// The GROWTH direction of the same constant is graded by the two
// DefaultBackoffCeiling*Wait tests below, through the give-up verdict rather
// than through relaunch spacing.
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

// backoffMaxAtCeilingWait and backoffMaxBelowCeilingWait are the two FIRST
// waits injected by the growth-direction arms below. Neither is a default:
// together they bracket the shipped ceiling from both sides without injecting
// backoffMax at all.
//
// backoffMaxAtCeilingWait is a wait that MUST already be at or above the
// ceiling. If it is not, a crash-looping engine sleeps a minute-plus between
// relaunches and serve takes minutes to hand the crash loop to the OS unit
// that would recycle it — the 10m mutant's behaviour.
//
// backoffMaxBelowCeilingWait is a wait that must NOT yet be at the ceiling. If
// it is, the ceiling is at or below 15s, leaving no room for the doublings to
// climb and making the very first crash a ceiling hit.
const (
	backoffMaxAtCeilingWait    = 60 * time.Second
	backoffMaxBelowCeilingWait = 15 * time.Second
)

// backoffCeilingProbeOuterBound is the probe's own machinery bound, not a
// bound on any default: it is how long the probe waits for the supervisor to
// REACH its verdict on the first crash. It asserts nothing about backoffMax —
// exceeding it means no crash was supervised at all, and the failure message
// says exactly that. The verdict itself is decided with zero sleeping, so on
// every value inside the band this is reached in milliseconds; the bound only
// has to cover spawning and killing one child process (seconds under -race).
const backoffCeilingProbeOuterBound = 30 * time.Second

// relaunchAfterBackoffLine is the line waitBackoff emits when the supervisor
// decides to WAIT rather than give up. On the first crash it is mutually
// exclusive with the crash-loop verdict: backoffAndMaybeGiveUp either declares
// the engine unkeepable and returns, or falls through to waitBackoff. Which of
// the two the probe observes IS the observation.
const relaunchAfterBackoffLine = "relaunching engine after backoff"

// runCrashLoopCeilingProbe runs a supervisor whose child STARTS and dies at
// once (a crash, not a construction failure) with firstWait injected as the
// first relaunch wait and the ceiling-hit budget set to one, and reports
// whether the crash-loop give-up verdict was emitted within within.
//
// backoffMax is never injected: it stays exactly what newEngineSupervisor gave
// it, which is the constant under test. What makes this cheap is that
// backoffAndMaybeGiveUp tests `*backoff >= s.backoffMax` BEFORE handing the
// wait to waitBackoff, so with maxCeilingHits=1 the whole verdict — emitted or
// not — is settled by the first child's death, with no sleeping at any ceiling
// value.
func runCrashLoopCeilingProbe(t *testing.T, firstWait time.Duration) (gaveUp error, logs string) {
	t.Helper()
	selfExe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	root := isolateSpawnFailEnv(t)
	// Pin the log HANDLER, so the positive control below reads logfmt
	// regardless of what the ambient environment asks for (buildSlogLogger
	// switches to JSON on EnvDaemonLogJSON).
	t.Setenv(EnvDaemonLogJSON, "0")

	sink := &lockedBuf{}
	defer SetEngineChildCommandForTest(func(_ string, _ string) *exec.Cmd {
		return instantExitCommand(selfExe)
	})()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sup := newEngineSupervisor(layoutFromRoot(root, ""), buildSlogLogger(sink))
	// The first WAIT and the ceiling-hit budget only — both graded elsewhere
	// (#7111's backoffInitial pin above, #7110's give-up budget).
	sup.backoffInitial = firstWait
	sup.maxCeilingHits = 1
	sup.drainTimeout = 2 * time.Second
	if serr := sup.start(ctx); serr != nil {
		t.Fatalf("supervisor start: %v", serr)
	}
	defer sup.stop()

	// Wait for the supervisor to REACH its verdict on the first crash, rather
	// than sampling a fixed window: the two outcomes are mutually exclusive
	// emitted artefacts, so whichever appears first is the answer and no
	// timing budget stands between the observation and the truth. A loaded
	// machine only delays the answer, it cannot change which one arrives.
	deadline := time.Now().Add(backoffCeilingProbeOuterBound)
	for {
		select {
		case ferr := <-sup.fatal():
			gaveUp = ferr
		default:
		}
		logs = sink.String()
		if gaveUp != nil || strings.Contains(logs, relaunchAfterBackoffLine) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("within the probe's own %s bound the supervisor neither declared the engine unkeepable nor entered a backoff wait — no crash was supervised, so this probe graded nothing; logs:\n%s",
				backoffCeilingProbeOuterBound, logs)
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Positive control: a child really ran and died, so the verdict is the
	// CRASH path's and not the construction-failure path's (which reaches
	// waitBackoff without ever consulting backoffMax). "engine child exited"
	// alone is also a prefix of the drain's graceful line (#7119), so this
	// matches the crash line's own uptime field, which the drain never emits.
	if !strings.Contains(logs, "engine child exited") || !strings.Contains(logs, "uptime=") {
		t.Fatalf("no child crash was ever observed — this probe graded nothing; logs:\n%s", logs)
	}
	return gaveUp, logs
}

// TestEngineSupervisor_DefaultBackoffCeilingIsReachedByAMinuteScaleWait pins
// defaultEngineBackoffMax in the GROWTH direction, with the ceiling itself
// never injected: a pending relaunch wait of backoffMaxAtCeilingWait must
// already count as a ceiling hit, so the crash-loop give-up verdict is
// emitted. An inflated ceiling (the 10m mutant) leaves that wait below the
// ceiling, and the engine keeps crash-looping on a minutes-long cadence
// instead of being handed to the OS unit. Costs one child spawn of wall clock
// — the verdict precedes the sleep.
func TestEngineSupervisor_DefaultBackoffCeilingIsReachedByAMinuteScaleWait(t *testing.T) {
	gaveUp, logs := runCrashLoopCeilingProbe(t, backoffMaxAtCeilingWait)

	if gaveUp == nil {
		t.Fatalf("a pending %s relaunch wait was not yet AT the production backoff ceiling — the supervisor went on waiting instead of declaring the engine unkeepable: the ceiling is above %s, which means a crash-looping engine sleeps minutes between relaunches and serve takes minutes to exit so the OS unit can recycle it; logs:\n%s",
			backoffMaxAtCeilingWait, backoffMaxAtCeilingWait, logs)
	}
	// The EMITTED verdict, not a counter: the crash-loop wording is what tells
	// this give-up apart from the unspawnable one (#7087).
	if !strings.Contains(gaveUp.Error(), "crash-looping") {
		t.Errorf("the verdict emitted was not the crash-loop one: %v\nlogs:\n%s", gaveUp, logs)
	}
}

// TestEngineSupervisor_DefaultBackoffCeilingLeavesRoomBelowAMinute pins the
// other side of the same band: a pending wait of backoffMaxBelowCeilingWait
// must NOT yet be a ceiling hit. A collapsed ceiling makes the FIRST crash a
// ceiling hit, so the doublings never climb and every value of backoffInitial
// above the ceiling is indistinguishable from it. Together with the arm above
// this is a two-sided band (15s < backoffMax <= 60s), which 20s, 45s and 60s
// all satisfy — so neither arm is the constant renotated. Costs one child
// spawn of wall clock: the probe returns the moment the supervisor announces
// the wait, and never sleeps it.
func TestEngineSupervisor_DefaultBackoffCeilingLeavesRoomBelowAMinute(t *testing.T) {
	gaveUp, logs := runCrashLoopCeilingProbe(t, backoffMaxBelowCeilingWait)

	if gaveUp != nil {
		t.Fatalf("a pending %s relaunch wait was ALREADY at the production backoff ceiling: the ceiling is at or below %s, so the first crash counted as a ceiling hit and the engine was declared unkeepable at once, with the backoff never doubling: %v\nlogs:\n%s",
			backoffMaxBelowCeilingWait, backoffMaxBelowCeilingWait, gaveUp, logs)
	}
	// gaveUp == nil here means the probe observed relaunchAfterBackoffLine —
	// the supervisor announced the wait that a ceiling hit would have replaced
	// — so the outcome is a positive observation, not an elapsed timeout.
	if !strings.Contains(logs, relaunchAfterBackoffLine) {
		t.Errorf("the supervisor never entered a backoff wait — this test graded nothing; logs:\n%s", logs)
	}
}
