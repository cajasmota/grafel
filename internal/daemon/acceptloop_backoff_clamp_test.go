package daemon

import (
	"testing"
	"time"
)

// acceptloop_backoff_clamp_test.go grades the THIRD of the three roles that
// share acceptLoop's transient-Accept-error branch: the CLAMP.
//
//	if isTransientAcceptErr(err) {
//	    if backoff == 0 { backoff = acceptBackoffStart } else { backoff *= 2 }
//	    //                            ^ role 1, START VALUE   ^ role 2, FACTOR (#7167/#7172)
//	    if backoff > acceptBackoffMax { backoff = acceptBackoffMax }  <- role 3, THE CLAMP (this file)
//	    logger.Warn("accept: transient error, backing off", "retry_in", backoff)
//	    time.Sleep(backoff)
//	}
//
// WHY THIS FILE EXISTS. #7126 pinned the SUPERVISOR's clamp
// (supervise_backoff_clamp_test.go, waitBackoff in supervise.go) and was never
// transferred to this twin. The same three-role structure exists at two sites,
// and pinning a role at one says nothing about the other — this repo has now
// measured that five times (#7118, #7162/#7167, #7135/#7164, #7126/#7174).
// Pre-fix, deleting THIS clamp outright left the whole of ./internal/daemon/
// green: 0 `--- FAIL`, re-derived on this tree before this file existed.
//
// WHY THE NEAREST PINS DO NOT SEE IT. acceptloop_backoff_factor_test.go asserts
// a RATIO between consecutive announced waits and explicitly EXCLUDES every pair
// at the ceiling, so deleting the clamp leaves every pre-clamp ratio at 2 and
// that file green — it says so in its own header. #7126's test walks a different
// function in a different file with its own injected constants. And
// TestAcceptLoopSurvivesTransientErrors asserts `elapsed >= 10ms`, a FLOOR: an
// unclamped backoff only makes it easier to clear.
//
// THE SEAM, inherited rather than rebuilt. acceptLoop ANNOUNCES the wait it is
// about to take — `logger.Warn(..., "retry_in", backoff)` sits on the line
// before `time.Sleep(backoff)` — so the intended waits are readable out of a
// captured log stream with no clock involved. `-race` shifts this package's
// timings by ~7x and the automatic CI legs run WITHOUT it (#7062), so a
// wall-clock assertion here would be scheduling luck rather than evidence. This
// file reuses announcedAcceptBackoffs from acceptloop_backoff_factor_test.go
// rather than building a second harness: a twinned helper is graded at
// whichever copy a mutant happens to land on, which is how a twinned key hides
// a hole.
//
// WHAT IS NOT OBSERVABLE THROUGH IT, stated rather than left implied: the
// ANNOUNCED wait, not the slept one. A defect that announced X and slept 4X
// passes here. That is inherent to the seam and is the right trade for this
// package.
//
// WHAT THE ASSERTION IS. A BOUND, not a sequence:
//
//	every announced wait must be <= acceptBackoffMax
//
// It is NOT the literal `[5ms 10ms 20ms 40ms 80ms 160ms 320ms 640ms 1s 1s]`. A
// literal encodes the start value, the factor and the ceiling at once and breaks
// on any legitimate retune of the other two; #7126 established that shape
// deliberately and #7172 preserved it, and this file does not regress it.
//
// AND WHY A BOUND ALONE IS NOT ENOUGH — the self-satisfaction problem. The bound
// names acceptBackoffMax, which is package-scoped, so it can be satisfied BY THE
// CONSTANT rather than by what production does with it: enlarge the constant far
// enough and every announced wait is under it no matter what the clamp does.
// Measured, not argued — `acceptBackoffMax 1s -> 1h` leaves the bound clean at
// every one of the ten waits. Two structural floors close that, and they are the
// two directions of this test's ONE graded-set guard (`w == acceptBackoffMax`,
// which partitions the announced waits into at-ceiling and below-ceiling):
//
//   - at least acceptClampMinAtCeiling waits must sit EXACTLY at the ceiling, so
//     the clamp is known to have ENGAGED and the bound is not passing on a
//     sequence that never got near it. This is the vacuity floor, and its NAMED
//     KILLER is that `acceptBackoffMax 1s -> 1h` row: measured, it fires ALONE
//     inside this test — zero bound errors, the start-value pin below still
//     green — on the unchanged sequence [5ms ... 1.28s 2.56s], which is exactly
//     the bound being satisfied by the constant rather than by the clamp.
//     It also reaches part of the SHRINK direction the bound is structurally
//     blind to: `backoff = acceptBackoffMax / 3` caps at 333ms and nothing then
//     sits at the ceiling, so the floor fires.
//   - NOT every wait may sit at the ceiling, so the curve is known to have GROWN
//     INTO the ceiling rather than started at it. Its named killer is
//     `acceptBackoffStart = acceptBackoffMax`.
//
// WHERE THE FLOOR DOES NOT REACH, measured rather than assumed, because the
// obvious version of the claim above is FALSE. `backoff = acceptBackoffMax / 2`
// is ALIVE under this test: it caps at 500ms, the NEXT doubling lands on exactly
// 1s, and the floor is satisfied by that one wait — announced sequence
// [5ms 10ms 20ms 40ms 80ms 160ms 320ms 640ms 500ms 1s]. It is killed instead,
// and solely, by the RATIO ASSERTION (not either floor) inside
// TestAcceptLoopBackoffClampSkipExcludesOnlyCeilingPairs in
// acceptloop_backoff_factor_test.go, which sees the 640ms -> 500ms step as
// 0.781x. So the shrink direction of the clamped-to VALUE is covered at this
// site jointly by the two files and not by this one alone.
//
// THAT CROSS-FILE DEPENDENCY IS NAMED ON PURPOSE, file and test and assertion.
// It is load-bearing — the shrink direction is ALIVE in this file — and an
// unnamed one is exactly what gets retuned away by someone who has no idea
// another file leans on it. Anyone narrowing that test's ratio assertion, or
// shortening its ten-error script, silently un-grades `max/2` here.
//
// Both floors are load-bearing in both directions of the guard, measured on the
// guard itself rather than assumed: `if true || w == acceptBackoffMax` makes the
// at-ceiling set everything and trips the second floor; `if false && ...` empties
// it and trips the first. Each gives a distinct, attributable failure at a
// DIFFERENT ASSERTION for a DIFFERENT REASON — the `every one of the N
// announced backoffs sat at the ceiling` line reports the set SWALLOWED, the
// `no announced backoff reached the ceiling` line reports it EMPTIED — so this
// test cannot silently empty its own input set. (Named by their messages rather
// than by line number on purpose: a line citation inside the comment block that
// precedes the code it cites is invalidated by any edit to the comment itself.)
//
// AND THE FIXTURE ACTUALLY DRIVES BOTH BRANCHES, which is the difference
// between a guard that is graded and one that merely looks graded. The shipped
// ten-error sequence [5ms 10ms 20ms 40ms 80ms 160ms 320ms 640ms 1s 1s] puts
// EIGHT waits below the ceiling and TWO at it, so `w == acceptBackoffMax` is
// taken in both directions by the fixture itself. That is not free: the
// six-error probe next door never reaches the ceiling at all, and a guard whose
// branch its own fixture never takes is dead code that reads as covered —
// exactly the hole #7172's review found. The input set also cannot be empty
// upstream: announcedAcceptBackoffs t.Fatalf's unless len(waits) == n.
//
// THE TWO SITES ARE DISJOINT, measured in BOTH directions rather than argued
// from the fact that they live in different files. If one test killed both
// clamps the "separate sites" framing would not be earned and the next site's
// gap would hide behind it:
//
//	acceptLoop's clamp deleted  -> 1 `--- FAIL`, this test only;
//	                               supervise_backoff_clamp_test.go GREEN
//	waitBackoff's clamp deleted -> 1 `--- FAIL`, #7126's
//	                               TestEngineSupervisor_BackoffWaitsAreBoundedByTheCeiling only;
//	                               this test GREEN
//
// AXES. VARIED: the clamp's presence (deleted), its threshold
// (`> 2*acceptBackoffMax`), its clamped-to value in BOTH directions
// (`2*acceptBackoffMax`, `acceptBackoffMax/2`), and the number of waits observed
// at and below the ceiling. HELD CONSTANT: the growth factor (that is
// acceptloop_backoff_factor_test.go's arm and this file asserts no ratio), the
// transient-error classification, the reset-on-success behaviour, and the SLEPT
// duration (only the announced one is read).
//
// NOT GRADED HERE, on purpose: `>` -> `>=` on the clamp comparison. It is
// EQUIVALENT FOR EVERY VALUE OF `backoff`, not merely for the ones this fixture
// can reach — which makes the verdict cheaper to re-check than an enumeration
// would. The two forms differ only in the single state
// `backoff == acceptBackoffMax`, and there the branch body is an idempotent
// write of the value already held. Nothing observes the difference: the `if` and
// its body are ADJACENT with no read between the comparison and the assignment,
// `backoff` is a plain local time.Duration with no pointer escaping, and there
// is no counter, no logging and no early return, so the next statement
// (`logger.Warn` then `time.Sleep`) sees the same post-state either way. The
// equivalence therefore holds over the whole int64 domain, not just the
// alphabet. The reachable alphabet — acceptBackoffStart or a doubling of a
// previous value, i.e. {5ms, 10ms, ..., 640ms, 1s, 2s} — agrees on every
// element, but that is belt-and-braces rather than load-bearing; the measured
// row confirms it at 0 `--- FAIL`. The same equivalence is already recorded at
// the twin in supervise.go. It is left untested ON PURPOSE — a test pinning `>`
// here would be vacuous by construction.
//
// MEASURED (full ./internal/daemon/, -count=1, `go vet ./internal/daemon/` exit
// 0 captured to its OWN file on every row; verdicts are ANCHORED `--- FAIL`
// LINE counts, never an exit code). Unmutated tree with this file: 0 `--- FAIL`,
// 490 tests listed. The full table lives in the PR body for #7174; the rows this
// file's own design rests on are repeated here:
//
//	clamp block DELETED                -> 1 `--- FAIL`  DEAD: this test, bound (waits 9,10 = 1.28s, 2.56s) AND floor
//	clamp block DELETED, FILE HELD OUT -> 0 `--- FAIL`  ALIVE (positive control)
//	`> acceptBackoffMax` -> `> 2*acceptBackoffMax`      -> 1  DEAD: this test, bound (1.28s)
//	`backoff = acceptBackoffMax` -> `2 * acceptBackoffMax` -> 1  DEAD: this test, bound (2s)
//	`backoff = acceptBackoffMax` -> `acceptBackoffMax / 3` -> 2  DEAD: this test (floor alone) + the ratio pin
//	`backoff = acceptBackoffMax` -> `acceptBackoffMax / 2` -> 1  ALIVE HERE, DEAD via the ratio pin (see above)
//	`>` -> `>=`                                         -> 0  ALIVE, EQUIVALENT (see above)
//	`acceptBackoffMax` 1s -> 1h                         -> 2  DEAD: this test (FLOOR ALONE) + the ratio pin's excluded-floor
//	`acceptBackoffStart` = acceptBackoffMax             -> 4  DEAD: this test (upper floor) + the start pin + both ratio-pin tests
//	`acceptBackoffStart` 5ms -> 7ms                     -> 1  DEAD: the start pin ALONE
//
// The hold-out is positively proved, not asserted, and the proof is the `-list`
// diff ALONE: `go test -list` shows 490 tests with this file and 488 without it,
// and the difference between the two sorted listings is EXACTLY this file's two
// test names, neither of which appears in the 488. A `=== RUN` count is
// deliberately NOT cited as evidence — `=== RUN` is emitted only under `-v`, so
// on these non-verbose legs it is zero for every test in the package whether or
// not it ran, and it cannot distinguish a held-out test from a run one.
//
// DECLARED CONFOUNDS, because a mutant that moves more than the role under test
// must say so. The two constants are read by the ratio pin next door as well, so
// every row that retunes one of them reacts there too; the decompositions above
// name which test contributed each `--- FAIL` line and they sum. One row was
// DISCARDED rather than reported: `acceptBackoffStart` 5ms -> 5us produces 5
// `--- FAIL` lines, but they are a HARNESS artefact — the shared `retry_in`
// regex does not match the `u` of `5us`, so the helper fatals on a parse error
// before any assertion here runs. It grades nothing and is excluded; 7ms is the
// row that actually exercises the start value.
//
// `./cmd/grafel/` was NOT run and is not required: the diff stays inside
// internal/daemon/ and touches no extractor, so the graph digest cannot move.

const (
	// acceptClampProbeErrors is how many consecutive transient Accept errors
	// the fixture scripts. Doubling 5ms needs eight growth steps to pass the 1s
	// ceiling, so ten errors announce ten waits of which the last two are AT
	// the ceiling — i.e. the script is long enough that the clamp actually
	// engages. Six (the factor file's shorter probe) never reaches it, which is
	// exactly why that probe cannot grade this role. Those counts are
	// documented, not asserted: asserting 8/2 would encode the start, the
	// factor and the ceiling at once.
	acceptClampProbeErrors = 10
	// acceptClampMinAtCeiling is the vacuity floor: how many announced waits
	// must sit EXACTLY at the ceiling for the bound assertion above to be
	// evidence about the clamp rather than about a sequence that never got
	// near it. One is required rather than two so the floor survives a retune
	// of the start value or the factor that changes how many steps land on the
	// ceiling within the script.
	acceptClampMinAtCeiling = 1
	// acceptClampStartValue is the SHIPPED acceptBackoffStart, written as a
	// literal on purpose. It is the one place in this package that pins that
	// constant's VALUE rather than merely its liveness; see the start-value
	// test below for why it is a literal and not a reference to the const.
	acceptClampStartValue = 5 * time.Millisecond
)

// TestAcceptLoopAnnouncedBackoffIsBoundedByTheCeiling is #7174's pin: every wait
// acceptLoop announces on the transient-Accept-error path must be at or below
// acceptBackoffMax, the clamp must be shown to have engaged, and the curve must
// be shown to have grown into the ceiling rather than started at it.
func TestAcceptLoopAnnouncedBackoffIsBoundedByTheCeiling(t *testing.T) {
	waits := announcedAcceptBackoffs(t, acceptClampProbeErrors)

	// THE BOUND. Note there is no skip and no exclusion here: the graded set is
	// the WHOLE announced sequence, so unlike the factor file's ratio pin this
	// assertion has no input set that could be silently emptied. The
	// at-ceiling partition below is this test's only guard, and both of its
	// directions are floored.
	atCeiling := 0
	for i, w := range waits {
		if w > acceptBackoffMax {
			t.Errorf("announced transient-Accept backoff %d of %d was %s, which exceeds the %s ceiling (whole sequence %v): the clamp did not hold, so a sustained fd-pressure burst drives the retry interval up without limit and every MCP client that reconnects during it waits an unbounded time for the daemon to accept",
				i+1, len(waits), w, acceptBackoffMax, waits)
		}
		if w == acceptBackoffMax {
			atCeiling++
		}
	}

	// VACUITY FLOOR, and the reason the bound above is evidence about behaviour
	// rather than about the constant it names. Named killer:
	// `acceptBackoffMax 1s -> 1h`, which leaves every bound check clean and
	// fires this line alone.
	if atCeiling < acceptClampMinAtCeiling {
		t.Errorf("no announced backoff reached the %s ceiling: %d of %d waits sat at it (sequence %v), want at least %d — the bound asserted above is then satisfied by a curve that never got near the ceiling rather than by the clamp holding it there, so this test would pass vacuously (and a clamp that capped BELOW the ceiling, which the bound cannot see, would pass too)",
			acceptBackoffMax, atCeiling, len(waits), waits, acceptClampMinAtCeiling)
	}

	// THE OTHER DIRECTION of the same partition: the curve must GROW INTO the
	// ceiling, not begin at it. Named killer:
	// `acceptBackoffStart = acceptBackoffMax`.
	if len(waits) > 0 && atCeiling == len(waits) {
		t.Errorf("every one of the %d announced backoffs sat at the %s ceiling (sequence %v) — the retry curve never spent a single cheap re-accept below the ceiling, so the daemon waits a full ceiling before its FIRST retry of a transient Accept error; the at-ceiling partition this test grades has also swallowed its whole input set, which would make the floor above assert nothing",
			len(waits), acceptBackoffMax, waits)
	}
}

// TestAcceptLoopFirstAnnouncedBackoffIsTheShippedStartValue pins role 1 — the
// VALUE of acceptBackoffStart — which #7172 left graded by nothing but a
// vacuity floor, and a floor is a liveness check, not a pin on a value (that gap
// is named in #7174's body).
//
// WHY A LITERAL AND NOT `acceptBackoffStart`. A guard that names the constant it
// is checking is satisfied BY the constant: a mutant that retunes
// acceptBackoffStart moves the expectation with it and survives. Measured on the
// same production mutant, one file changed between the two rows:
//
//	acceptBackoffStart 5ms -> 7ms, `waits[0] != acceptClampStartValue` (shipped) -> 1 `--- FAIL`, DEAD, this test alone
//	acceptBackoffStart 5ms -> 7ms, `waits[0] != acceptBackoffStart`   (reference) -> 0 `--- FAIL`, ALIVE
//
// So the literal is the pin and the reference would not be one. 7ms is chosen
// over a larger retune precisely so the row stays ISOLATED: at 7ms the announced
// sequence is [7ms ... 896ms 1s 1s], which still clears this file's bound and
// both of its floors and both of the ratio pin's, so the single `--- FAIL` line
// attributes to the start value and to nothing else.
//
// WHAT IT DELIBERATELY DOES NOT ENCODE: the factor and the ceiling. It reads
// ONLY waits[0], from a ONE-error script, so no growth step and no clamp
// interaction is in its expectation — a retune of either constant leaves this
// test untouched, which is the property #7126 established and this file
// preserves. It is also why this lives in its own test function: a single
// `--- FAIL` line then attributes to the role that actually moved.
//
// COST: one scripted error, one 5ms sleep.
func TestAcceptLoopFirstAnnouncedBackoffIsTheShippedStartValue(t *testing.T) {
	waits := announcedAcceptBackoffs(t, 1)

	if waits[0] != acceptClampStartValue {
		t.Errorf("the FIRST announced transient-Accept backoff was %s, want %s: the initial retry delay is the one that decides how fast the daemon re-attempts an Accept after a single transient failure, and a larger one makes every isolated EMFILE blip cost that much before the next client can connect (sequence %v)",
			waits[0], acceptClampStartValue, waits)
	}
}
