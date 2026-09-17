package daemon

import (
	"testing"
	"time"
)

// requests_drain_backoff_factor_test.go grades the FIRST of the two sites #7167
// names: the growth FACTOR inside rebuildBackoff.
//
//	var rebuildBackoff = func(attempts int) time.Duration {
//	    d := rebuildBackoffBase
//	    for i := 1; i < attempts; i++ {
//	        d *= 2                              <- THE FACTOR (this file)
//	        if d >= rebuildBackoffMax { ... }   <- the clamp (not graded here)
//	    }
//	    ...
//	}
//
// `d *= 2` -> `d *= 3` left all of ./internal/daemon/ green: 0 `--- FAIL`,
// re-derived on this tree before this file existed. It is the same defect shape
// as #7162 at the supervisor site, found by a pair audit on #7165 rather than by
// reading an issue — and the verdict there says nothing about here, so it was
// re-measured at this site independently.
//
// WHY NOTHING SAW IT. One test CALLS it; no test OBSERVES it. The distinction
// is not pedantic — "nothing calls it" and "something calls it but cannot see
// the result" imply different fixes, and the second is the true one here. An
// earlier draft of this block claimed the first, inferred from a grep rather
// than measured; it was wrong, and this is the corrected, re-derived statement.
//
// rebuildBackoff is reached in production through exactly one reference,
// `backoff: rebuildBackoff` in newRebuildWorker (requests_drain.go:280). THREE
// tests that exercise that path replace the field with a constant of their own
// — requests_drain_unblock_test.go:167 (`return time.Minute`, in
// TestRebuildWorker_CrashResumeBackoffGate), :230 (`return 0`, in
// TestRebuildWorker_DeadLetterIsObservable) and :417 (`return 0`, in
// TestDrainOnce_DuplicateDoesNotResetCrashAttempts). That is THREE injections
// in THREE tests, counted off the file — not four.
//
// A FOURTH test reaches the SHIPPED function, and a grep for `rebuildBackoff`
// cannot see it because it never names the symbol:
// TestDrain_KindRebuild_CrashLoopIsBounded (requests_drain_resume_test.go:33)
// calls drainRequestsOnce (requests_drain.go:236), which builds a fresh
// requestsDrainer whose newRebuildWorker (requests_drain.go:172) carries the
// DEFAULT `backoff: rebuildBackoff`, and whose panicking rebuildFn drives the
// OutcomeCrashed branch that calls `w.backoff(rec.Attempts+1)`
// (requests_drain.go:392). The shipped curve really is evaluated, at the
// shipped factor.
//
// It cannot OBSERVE the factor, and THAT is the mechanism: each of that test's
// 8 drain passes constructs a new drainer and therefore a new worker, so the
// duration computed at :392 is written into that worker's in-memory
// `nextAttempt` gate map (requests_drain.go:265) and discarded when the pass
// ends. It never survives to the pass whose gate read (requests_drain.go:364)
// would consult it, and nothing else in the package reads a backoff duration at
// all. So the production function's growth was not weakly graded, as at the
// supervisor site — it was CALLED AND UNOBSERVED, which is why every magnitude
// in MEASURED below is ALIVE, the extremes included.
//
// Injecting a constant is right for the three gate tests (they grade the GATE,
// not the curve); it just means the curve needs its own arm, which is this file.
//
// THE SEAM. There is one, and it is the cheapest kind: rebuildBackoff is a pure
// function of `attempts` — no clock, no goroutine, no sleep, no process. This
// file calls it directly. Nothing here is gated on elapsed wall clock, so the
// ~7x timing shift `-race` applies to this package (#7062) cannot move the
// verdict; the automatic CI legs run without `-race` and see the identical
// numbers. It also does not swap the package var, so it grades the SHIPPED
// function rather than a stand-in.
//
// WHAT THE ASSERTION IS, and what it deliberately is NOT. A RATIO between the
// backoffs of CONSECUTIVE attempts, applied only where the clamp did not
// engage:
//
//	for consecutive attempts (n, n+1) with rebuildBackoff(n+1) < rebuildBackoffMax:
//	    rebuildBackoff(n+1) must equal 2 * rebuildBackoff(n)
//
// It is NOT a literal sequence (`[30s 60s 120s 240s 300s]`). A literal encodes
// rebuildBackoffBase, rebuildBackoffMax and the factor at once and would break
// on any legitimate retune of the other two; the relation between neighbours
// breaks only on the factor. It is also NOT a floor on one backoff — that is
// exactly the assertion shape that failed at the supervisor site (#7162): a
// tripled 400ms is 1.2s, which clears a `>= 600ms` floor. A floor pins "growth
// happened"; only a ratio pins "growth is by two".
//
// WHAT A WRONG FACTOR COSTS, stated no more strongly than the code supports.
// This loop is NOT unbounded, and the word is not used for it: ApplyAndAckBounded
// dead-letters the survivor once rec.Attempts reaches maxRebuildAttempts (3), so
// a doomed rebuild takes at most two backoff waits before it is dead-lettered
// and clearBackoff wipes the gate. The factor therefore changes the WIDTH of
// that fixed-length window, not whether it closes. Arithmetic from the shipped
// constants: the two production waits are rebuildBackoff(2) and rebuildBackoff(3)
// = 60s + 120s = 180s at factor 2, versus 90s + 270s = 360s at factor 3 — a 2.0x
// stretch of the time a crash-looping rebuild occupies its group's single
// in-flight slot before being dead-lettered. rebuildBackoffMax (5m) is never
// reached in production at all, because attempt 4 dead-letters instead of
// waiting; the clamp only shows up under a large mutant, which is why the
// vacuity floor below is load-bearing.
//
// MAGNITUDE, SCORED RATHER THAN INHERITED. At the supervisor site (#7165) the
// magnitudes were NOT equivalent: `*= 100` was already caught by an unrelated
// 30s bound while `*= 3` was invisible, so detectability there was non-monotone
// in magnitude and the unqualified claim "enlargement is ungraded" was false.
// At THIS site that does not reproduce, and the reason is structural rather
// than lucky: nothing in the package observes rebuildBackoff at all, so there
// is no bound for a large factor to blow. Measured pre-fix (see MEASURED
// below): factors 1, 3, 8 and 100 are ALL ALIVE at 0 `--- FAIL`. The band in
// which a mutant survives is therefore every magnitude, in both directions —
// which is a wider hole than the supervisor's, not a narrower one.
//
// WHICH ASSERTION KILLS WHICH MAGNITUDE, and why the vacuity floor is
// load-bearing rather than decorative. At factors 1 and 3 the RATIO fires on
// the first pair and the floor never fires. At factors 4 and 8 the first step
// is still below the 5m ceiling so the ratio fires, but every later step
// clamps, so the floor fires TOO (1 graded pair of 5 in both rows). At factor
// 100 the very first step clamps (30s * 100 >= 5m), every pair is skipped, and
// the FLOOR is the only thing left to fire — without it this file would pass a
// factor of 100 having asserted nothing. Note what that means for the shipped
// tuning: only THREE of the five pairs this walk offers ever sit below the
// ceiling at factor 2, so this site has little headroom and the floor is the
// assertion doing the work at the large end.
//
// AXES. VARIED: the growth factor (1, 1.5, 3, 8 and 100 as well as 2 — both
// directions, enlargement split by magnitude, and one NON-INTEGER factor so the
// band is closed continuously rather than only at sampled integers), and the attempt index at
// which the ratio is read (several consecutive pairs, versus the single value
// the injected constants in requests_drain_unblock_test.go see). HELD CONSTANT:
// rebuildBackoffBase and rebuildBackoffMax (this file injects neither and
// asserts no absolute duration — their own values are the subject of no arm
// here), the clamp's comparison, and the `attempts < 1` normalisation.
//
// THE CLAMP-SKIP GUARD IN THIS FILE'S OWN GRADING LOOP is graded, in BOTH
// directions, and — unlike its twin at the acceptLoop site — it is graded by
// the SHIPPED walk with no extra fixture. #7162 established that a guard which
// can silently empty its own input set grades nothing, so both arms are scored
// (full package per row, `--- FAIL` LINES counted, vet 0):
//
//	`if true  || cur >= rebuildBackoffMax` -> DEAD 1, vacuity floor
//	                                         (`only 0 of 5 ... want at least 2`)
//	`if false && cur >= rebuildBackoffMax` -> DEAD 1, ratio, on exactly the two
//	                                         pairs sitting at the 5m ceiling
//
// WHY THIS SITE NEEDS NO EXTRA PROBE AND THE OTHER ONE DOES — the two are NOT
// symmetric, and saying so is the point. Here the shipped walk
// [30s 1m0s 2m0s 4m0s 5m0s 5m0s] REACHES its 5m ceiling twice, so the exclusion
// branch is taken by the natural fixture and both directions are live. At the
// acceptLoop site the shipped start (5ms) is 200x below its ceiling (1s), so a
// six-error script never reaches it: there, the `false &&` arm was ALIVE and
// needed a longer, ceiling-crossing sub-probe
// (TestAcceptLoopBackoffClampSkipExcludesOnlyCeilingPairs) before the guard was
// graded in that direction at all.
//
// NOT GRADED HERE, on purpose: the clamp (`d >= rebuildBackoffMax`), the
// `attempts < 1` floor, and the shipped values of the two constants. A ratio
// between neighbours is deliberately blind to all three — deleting the clamp
// leaves every pre-clamp ratio at 2 and this file green. Say so rather than
// leave it implied, so the next reader does not fill the gap with a vacuous
// fixture.

// MEASURED (full ./internal/daemon/, -count=1, gofmt clean, `go vet ./internal/daemon/`
// exit 0 recorded separately on every row; verdicts are counted `--- FAIL`
// LINES, never an exit code). Baseline on f9e7838e9: 0 `--- FAIL`. Unmutated
// tree with this file and its sibling: 0 `--- FAIL`, 487 tests listed.
//
// PRE-FIX (this file absent), site internal/daemon/requests_drain.go `d *= 2`:
//
//	*= 1        -> 0 `--- FAIL`   ALIVE
//	d = d*3 / 2 -> 0 `--- FAIL`   ALIVE   (NON-INTEGER, x1.5)
//	*= 3        -> 0 `--- FAIL`   ALIVE   (the row #7167 reported, re-derived here)
//	*= 8        -> 0 `--- FAIL`   ALIVE
//	*= 100      -> 0 `--- FAIL`   ALIVE
//
// The non-integer row is the one that makes the band claim a BAND rather than a
// set of sampled points: `d *= 2` is integer-nanosecond arithmetic, which is
// exactly where a rounding artefact could park a step back on an exact
// doubling and make a continuous claim false between the integers. Measured, it
// does not: x1.5 survives pre-fix and dies post-fix like every integer row, so
// the band closes CONTINUOUSLY. (Same row, same conclusion, at the supervisor
// site in #7165.)
//
// WITH THIS FILE, every row is exactly 1 `--- FAIL` and it is THIS test, with
// no other test in the package reacting at any magnitude:
//
//	*= 1   -> ratio (5 errors), sequence [30s 30s 30s 30s 30s 30s]
//	d*3/2  -> ratio (5 errors), floor NOT fired (5 of 5 pairs graded),
//	          sequence [30s 45s 1m7.5s 1m41.25s 2m31.875s 3m47.8125s]
//	*= 3   -> ratio, sequence [30s 1m30s 4m30s 5m0s 5m0s 5m0s]
//	*= 4   -> ratio AND floor (1 graded pair), sequence [30s 2m0s 5m0s 5m0s 5m0s 5m0s]
//	*= 8   -> ratio AND floor (1 graded pair), sequence [30s 4m0s 5m0s 5m0s 5m0s 5m0s]
//	*= 100 -> floor ALONE (0 graded pairs), sequence [30s 5m0s 5m0s 5m0s 5m0s 5m0s]
//
// `*= 1` is NOT a confound at this site, which is worth stating because it was
// one at the supervisor site (five unrelated tests reacted there). Here it
// produces exactly 1 `--- FAIL` — this test — because the production function
// has no other observer, which is the same fact that made every row above
// ALIVE.
//
// HOLD-OUT POSITIVE CONTROL: `d *= 3` with THIS FILE HELD OUT (moved aside,
// not edited) -> 0 `--- FAIL`. Positive proof of the hold-out: `go test -list`
// shows 487 tests with the file and 486 without it, and
// TestRebuildBackoff_GrowsByExactlyTwoPerAttempt is absent from the 486. So the
// kills above are earned by this file and not inherited from a pre-existing
// test.
//
// SIBLING PINS UNTOUCHED: supervise_backoff_factor_test.go,
// supervise_backoff_clamp_test.go and #7123's arms are neither re-tuned nor
// re-keyed, and they are green in the unmutated run and in every mutant row
// above (each row's single `--- FAIL` is this test). `./cmd/grafel/` was NOT
// run and is not required: the diff stays inside internal/daemon/ and touches
// no extractor, so the graph digest cannot move.

const (
	// drainFactorProbeAttempts is how many consecutive attempt indices the
	// walk below reads. Six is past the point where the shipped constants
	// clamp (30s doubling reaches 5m at attempt 5), so the walk covers both
	// the growing and the clamped region and the skip logic is exercised
	// rather than assumed.
	drainFactorProbeAttempts = 6
	// drainFactorMinRatios is the vacuity floor: how many consecutive pairs
	// must sit strictly below rebuildBackoffMax, i.e. how many steps the ratio
	// loop actually GRADED. Doubling from 30s to a 5m ceiling gives three
	// (30->60, 60->120, 120->240); two is required. Measured: factors 1 and 3
	// die on the RATIO with this floor never firing; factors 4 and 8 trip both
	// (1 graded pair each); factor 100 clamps on its first step and dies on
	// this floor alone, which is the row that proves the floor is live.
	drainFactorMinRatios = 2
	// drainFactorExpected is the factor under test. It appears once, as a
	// RATIO between neighbouring attempts — never as a sequence and never as
	// an absolute duration.
	drainFactorExpected = 2
)

// TestRebuildBackoff_GrowsByExactlyTwoPerAttempt is #7167's pin for the
// requests_drain.go site: each crash-resume backoff that the clamp did not cap
// must be exactly twice the previous attempt's. Asserted as a ratio between
// neighbours, never as a literal sequence and never as an elapsed duration.
func TestRebuildBackoff_GrowsByExactlyTwoPerAttempt(t *testing.T) {
	// Grade the SHIPPED function, not a stand-in: read the package var once
	// and do not replace it.
	fn := rebuildBackoff

	waits := make([]time.Duration, 0, drainFactorProbeAttempts)
	for n := 1; n <= drainFactorProbeAttempts; n++ {
		waits = append(waits, fn(n))
	}

	// Positive control: the walk really produced one backoff per attempt, so
	// there are as many steps to measure as the loop below expects. The count
	// is attempt-driven and factor-independent, so it cannot stand in for the
	// pin.
	if len(waits) != drainFactorProbeAttempts {
		t.Fatalf("read %d backoffs (%v), want %d — the walk graded nothing",
			len(waits), waits, drainFactorProbeAttempts)
	}

	// THE PIN. Every consecutive pair whose SECOND backoff is strictly below
	// rebuildBackoffMax is a step the clamp did not touch, so the whole of its
	// ratio is the growth factor.
	graded := 0
	for i := 1; i < len(waits); i++ {
		prev, cur := waits[i-1], waits[i]
		if cur >= rebuildBackoffMax {
			// The clamp engaged (or the step landed on the ceiling): the ratio
			// here is the ceiling's, not the factor's. Not this file's role.
			continue
		}
		graded++
		if want := drainFactorExpected * prev; cur != want {
			t.Errorf("rebuildBackoff(%d) = %s after rebuildBackoff(%d) = %s, want %s — consecutive crash-resume backoffs below the %s ceiling must grow by exactly %dx, and this step grew by %.3gx instead (whole sequence %v): the re-apply curve's shape is wrong, so a crash-looping rebuild holds its group's single in-flight slot for the wrong span before maxRebuildAttempts dead-letters it",
				i+1, cur, i, prev, want, rebuildBackoffMax, drainFactorExpected,
				float64(cur)/float64(prev), waits)
		}
	}

	// Vacuity floor. A factor large enough to clamp on its FIRST step skips
	// every pair above and would otherwise pass having asserted nothing; this
	// is the only assertion that fires on `d *= 100`.
	if graded < drainFactorMinRatios {
		t.Errorf("only %d of %d consecutive-backoff ratios sat below the %s ceiling (sequence %v), want at least %d: with fewer steps below the ceiling this test measures the clamp rather than the growth factor, and would pass vacuously",
			graded, len(waits)-1, rebuildBackoffMax, waits, drainFactorMinRatios)
	}
}
