package daemon

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// supervise_backoff_factor_test.go grades #7162: the THIRD role carried by the
// two lines at the end of waitBackoff — the growth FACTOR.
//
//	backoffAndMaybeGiveUp: if *backoff >= s.backoffMax { ceilingHits++ }  <- role 1, give-up THRESHOLD (#7111/#7123)
//	waitBackoff:           *backoff *= 2                                 <- role 3, the FACTOR (this file)
//	                       if *backoff > s.backoffMax {                  <- role 2, the CLAMP (#7126)
//	                           *backoff = s.backoffMax
//	                       }
//
// `*backoff *= 2` -> `*= 3` left all of ./internal/daemon/ green (0 `--- FAIL`).
// The nearest existing pin, #7123's shrink arm
// (TestEngineSupervisor_DefaultBackoffCeilingLeavesRoomToGrow), asserts the
// second wait is >= 600ms against an injected 400ms first wait. Under a
// tripling factor the second wait is 1.2s, which clears that floor
// comfortably. That arm pins "growth HAPPENED"; this file pins "growth is by a
// factor of TWO". Those are different claims, and a FLOOR on one wait cannot
// express the second one — only a RATIO between two consecutive waits can.
//
// WHAT IS WRONG WITH A WRONG FACTOR, stated no more strongly than it is. A
// changed factor changes the SHAPE of the retry curve — how many intermediate
// waits the supervisor spends climbing to the ceiling — not whether it ever
// gives up. With role 2 pinned, no wait exceeds the ceiling; with
// maxCeilingHits ending the crash loop, the wait COUNT is bounded too. An
// enlarged factor is therefore a tuning regression: an engine that is slow to
// become healthy is re-attempted fewer times at the short, cheap waits before
// the cadence jumps to the ceiling. It is NOT an unbounded tail (that word was
// measured false for role 2 and is equally false here) and not a correctness
// defect.
//
// WHAT IS OBSERVED, and why there is no timing dependence. The artefact is the
// SEQUENCE of waits the supervisor announces ("relaunching engine after
// backoff", backoff=<d>) — the same local handed to time.NewTimer on the next
// line. #7126 established that seam and this file reuses its reader,
// relaunchWaits, unchanged. Nothing here is gated on elapsed wall clock:
// `-race` moves this package's timings by ~7x and the automatic CI legs run
// without it, so a window on 4ms-vs-8ms would be scheduling luck rather than
// evidence (#7062).
//
// WHAT THE ASSERTION IS, and what it deliberately is NOT. It is a RATIO
// between CONSECUTIVE announced waits, applied only to the steps where the
// clamp did not engage:
//
//	for every consecutive pair (a, b) with b < s.backoffMax:  b must equal 2*a
//
// It is NOT the literal sequence (`[1 2 4 8 16 25 25]`). A literal sequence
// encodes backoffInitial, the spawn budget and the factor all at once and
// would break on any legitimate change to the other two; #7126's review
// checked that it pinned the BOUND rather than the sequence, and this file
// keeps that property by pinning a RELATION between neighbours instead. Each
// checked pair is self-contained: it says only "this wait is twice the
// previous one", so retuning the injected initial wait or the budget changes
// how MANY pairs are checked, never what any one of them asserts.
//
// The `b < s.backoffMax` guard is what keeps the clamp out of the ratio: once
// the clamp engages, b IS the ceiling and the ratio is 1:1 to 2:1 by design,
// not by the factor. The tuning below is chosen so no doubling ever lands
// EXACTLY on the ceiling (1ms against 25ms — deliberately not a power-of-two
// ratio), so every pre-clamp step is visible to the guard rather than being
// skipped as an ambiguous equal-to-ceiling step.
//
// WHY THIS FILE IS BLIND TO ROLES 1 AND 2, ON PURPOSE. Deleting the clamp
// yields 1, 2, 4, 8, 16, 32, 64: the pairs the guard still checks
// ((1,2)...(8,16)) all have ratio 2, so this file PASSES — role 2 stays
// #7126's alone. Role 1's arms settle their verdict before any wait is taken
// (backoffAndMaybeGiveUp tests the threshold BEFORE calling waitBackoff), so
// they are factor-independent and this file does not touch them. Neither
// sibling pin is re-tuned or re-keyed here.
//
// AXES. VARIED: the growth factor (scored at 1, 3 and 4 as well as 2 — both
// directions, with ENLARGEMENT the direction nothing previously saw), and the
// number of consecutive pre-clamp steps available to measure (four, versus the
// single step #7123's floor looks at). HELD CONSTANT: the failure mode (a
// construction failure that fails identically every time, so no child process
// ever exists and nothing depends on child scheduling), the injected
// initial:ceiling ratio, and the spawn budget.
//
// MEASURED (full ./internal/daemon/, -count=1, go vet exit 0 on every row).
// Baseline and the unmutated tree with this file: 0 `--- FAIL`. Positive
// control — `*= 3` with THIS FILE HELD OUT: 0 `--- FAIL` (484 tests compiled
// versus 485 with it, and `go test -list` shows this test absent), so the
// kills below are earned here and not inherited. With the file present:
//
//	*= 3 -> 1 `--- FAIL`, this test only, sequence [1ms 3ms 9ms 25ms 25ms 25ms 25ms]
//	*= 4 -> 1 `--- FAIL`, this test only, sequence [1ms 4ms 16ms 25ms 25ms 25ms 25ms]
//	*= 1 -> 6 `--- FAIL`, this test among them, sequence [1ms 1ms 1ms 1ms 1ms 1ms 1ms]
//
// Every one of those kills came from the RATIO assertion; the vacuity floor
// never fired in any row. `*= 1` is a COARSE mutant — it removes growth
// altogether, so five pre-existing tests also fail on it. The enlargement rows
// (`*= 3`, `*= 4`) are the ones that isolate this role: nothing else in the
// package reacts to them at all. Under `-race` this test passes in 0.13s with
// an identical sequence.
//
// DISJOINT FROM THE SIBLING PINS, measured the same way: deleting the CLAMP
// yields exactly 1 `--- FAIL`, TestEngineSupervisor_BackoffWaitsAreBoundedByTheCeiling,
// and this test stays green; inflating defaultEngineBackoffMax to 10m yields
// exactly 1 `--- FAIL`, TestEngineSupervisor_DefaultBackoffCeilingIsReachedByAMinuteScaleWait,
// and this test stays green too. Three roles, three killers, no overlap on the
// enlargement rows.
//
// This pin says NOTHING about the shipped VALUES of defaultEngineBackoffInitial
// or defaultEngineBackoffMax: it injects millisecond-scale tuning on purpose,
// because the FACTOR is the mechanism under test. Those two constants keep
// their own bands in supervise_defaults_backoff_test.go.

const (
	// factorProbeInitial and factorProbeMax are the test's own tuning, not
	// defaults. The 1:25 ratio is deliberately NOT a power of two: doubling
	// from 1ms (1, 2, 4, 8, 16) never lands exactly on 25ms, so every step
	// before the clamp engages produces a wait strictly BELOW the ceiling and
	// is therefore measured, rather than being skipped as an
	// indistinguishable at-the-ceiling step.
	factorProbeInitial = 1 * time.Millisecond
	factorProbeMax     = 25 * time.Millisecond
	// factorProbeSpawnBudget is widened well past the 3 waits production
	// tolerates so the walk takes enough waits for several consecutive RATIOS
	// to exist below the ceiling. The wait count it produces is budget-driven
	// and independent of the factor, so it cannot itself react to the mutant.
	factorProbeSpawnBudget = 8
	// factorProbeWaits is the exact number of relaunch waits the budget above
	// must produce: one per failed spawn except the last, which gives up
	// instead of waiting. Asserted, so a fixture that stops walking early
	// cannot pass vacuously.
	factorProbeWaits = factorProbeSpawnBudget - 1
	// factorProbeMinRatios is the vacuity floor: how many consecutive pairs
	// must sit strictly below the ceiling, i.e. how many steps the ratio
	// assertion actually GRADED. Without it, tuning (or a mutant) that drove
	// the very first wait to the ceiling would leave the loop below with
	// nothing to check and the test would pass having asserted nothing.
	// Doubling from 1ms to a 25ms ceiling gives four; two is required, which
	// every factor from 1 to 4 clears, so each of those mutants dies on the
	// RATIO itself and not on this floor.
	factorProbeMinRatios = 2
	// factorProbeExpectedFactor is the factor under test. It appears here
	// once, as a RATIO between neighbouring waits — not as a sequence, and
	// not as any absolute duration.
	factorProbeExpectedFactor = 2
)

// runSpawnFailUntilFatalWithFactorTuning runs a supervisor on the
// construction-failure path (a child path that does not exist, so cmd.Start
// fails identically every time and no process is ever created) with the factor
// probe's tuning, waits for the unspawnable fatal, and returns the emitted log
// stream.
//
// The construction-failure path is used because the factor lives in
// waitBackoff, which BOTH paths share — the crash path reaches it through
// backoffAndMaybeGiveUp and the spawn-failure path calls it directly — so this
// grades the same code with no child processes, no child scheduling and no
// dependence on how fast a re-exec of the test binary starts.
func runSpawnFailUntilFatalWithFactorTuning(t *testing.T) string {
	t.Helper()
	root := isolateSpawnFailEnv(t)
	// Pin the log HANDLER: relaunchWaits reads logfmt, and buildSlogLogger
	// switches to JSON on EnvDaemonLogJSON.
	t.Setenv(EnvDaemonLogJSON, "0")

	defer SetEngineChildCommandForTest(func(_ string, _ string) *exec.Cmd {
		return unspawnableCommand(t)
	})()

	sink := &lockedBuf{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sup := newEngineSupervisor(layoutFromRoot(root, ""), buildSlogLogger(sink))
	sup.backoffInitial = factorProbeInitial
	sup.backoffMax = factorProbeMax
	sup.maxSpawnFailures = factorProbeSpawnBudget
	sup.healthyUptime = time.Hour // nothing here ever "recovers" by uptime
	sup.drainTimeout = 2 * time.Second
	if err := sup.start(ctx); err != nil {
		t.Fatalf("supervisor start: %v", err)
	}
	t.Cleanup(sup.stop)

	// A generous outer bound that asserts nothing about any default: the whole
	// walk is 7 millisecond-scale waits, so exceeding this means no
	// supervision happened at all, and the message says so.
	select {
	case <-sup.fatal():
	case <-time.After(60 * time.Second):
		t.Fatalf("supervisor never surfaced the unspawnable fatal within the TEST's OWN 60s bound — this is the test having stopped waiting, not a statement about any backoff default; logs:\n%s",
			sink.String())
	}
	sup.stop()
	return sink.String()
}

// TestEngineSupervisor_BackoffGrowsByExactlyTwoPerStep is #7162's pin: each
// announced relaunch wait that the clamp did not cap must be exactly twice its
// predecessor. Asserted as a ratio between neighbours, never as a literal
// sequence and never as an elapsed duration.
func TestEngineSupervisor_BackoffGrowsByExactlyTwoPerStep(t *testing.T) {
	logs := runSpawnFailUntilFatalWithFactorTuning(t)
	waits := relaunchWaits(t, logs)

	// Positive control: this really exercised the construction-failure path,
	// so the waits read are backoffs and not some other emission.
	if !strings.Contains(logs, "no child process was created") {
		t.Fatalf("no construction failure was ever emitted — this test graded nothing; logs:\n%s", logs)
	}
	// Positive control: the walk went all the way to the budget, so there were
	// as many steps to measure as the tuning provides. The wait count is
	// budget-driven and factor-independent, so this cannot stand in for the
	// pin below.
	if len(waits) != factorProbeWaits {
		t.Fatalf("observed %d relaunch waits (%v), want exactly %d — the walk stopped early, so the growth steps this test measures were never taken; logs:\n%s",
			len(waits), waits, factorProbeWaits, logs)
	}

	// THE PIN. Every consecutive pair whose SECOND wait is strictly below the
	// ceiling is a step the clamp did not touch, so the whole of its ratio is
	// the growth factor. A tripling or quadrupling factor breaks the very
	// first such pair; a factor of one (no growth at all) breaks all of them.
	// A floor on a single wait — which is all #7123's shrink arm has — cannot
	// see any of this, because 1.2s and 800ms both clear a 600ms floor.
	graded := 0
	for i := 1; i < len(waits); i++ {
		prev, cur := waits[i-1], waits[i]
		if cur >= factorProbeMax {
			// The clamp engaged (or the step landed on the ceiling): the
			// ratio here is the ceiling's, not the factor's. Role 2's
			// business, graded in supervise_backoff_clamp_test.go.
			continue
		}
		graded++
		if want := factorProbeExpectedFactor * prev; cur != want {
			t.Errorf("relaunch wait %d was %s after a %s wait, want %s — consecutive waits below the %s ceiling must grow by exactly %dx, and this step grew by %.3gx instead (whole sequence %v): the retry curve's shape is wrong, so the supervisor spends the wrong number of short relaunch attempts before its cadence reaches the ceiling",
				i+1, cur, prev, want, factorProbeMax, factorProbeExpectedFactor,
				float64(cur)/float64(prev), waits)
		}
	}

	// Vacuity floor: the loop above must actually have graded steps. A
	// sequence that began at the ceiling would skip every pair and assert
	// nothing at all.
	if graded < factorProbeMinRatios {
		t.Errorf("only %d of %d consecutive-wait ratios sat below the %s ceiling (sequence %v), want at least %d: with fewer steps below the ceiling this test measures the clamp rather than the growth factor, and would pass vacuously",
			graded, len(waits)-1, factorProbeMax, waits, factorProbeMinRatios)
	}
}
