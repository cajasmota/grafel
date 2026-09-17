package daemon

import (
	"log/slog"
	"net"
	"net/rpc"
	"regexp"
	"sync"
	"syscall"
	"testing"
	"time"
)

// acceptloop_backoff_factor_test.go grades the SECOND of the two sites #7167
// names: the growth FACTOR in acceptLoop's transient-Accept-error path.
//
//	if isTransientAcceptErr(err) {
//	    if backoff == 0 { backoff = acceptBackoffStart } else { backoff *= 2 }  <- THE FACTOR (this file)
//	    if backoff > acceptBackoffMax { backoff = acceptBackoffMax }            <- the clamp (not graded here)
//	    logger.Warn("accept: transient error, backing off", "retry_in", backoff)
//	    time.Sleep(backoff)
//	}
//
// `backoff *= 2` -> `*= 3` left all of ./internal/daemon/ green: 0 `--- FAIL`,
// re-derived on this tree before this file existed. Same shape as #7162 at the
// supervisor site and as the rebuildBackoff site graded in
// requests_drain_backoff_factor_test.go, but measured here on its own: a verdict
// at one occurrence of `*= 2` says nothing about another.
//
// WHY THE NEAREST PIN DID NOT SEE IT. TestAcceptLoopSurvivesTransientErrors
// asserts `elapsed >= 10ms` after two transient errors — a FLOOR on a total, and
// on a wall clock. Under a tripling factor those two errors take 5ms + 15ms =
// 20ms, which clears the floor by 2x; under any enlargement the floor gets
// EASIER to clear, so it can never fail on this direction. TestAcceptLoopBackoffResets
// scripts a success between transient errors to check the reset, and asserts only
// Accept-call and served-conn counts — both factor-independent. So the growth
// STEP had no observer, only its existence did.
//
// THE SEAM, and that it exists is not inherited. #7165 could assert a ratio
// deterministically only because the supervisor ANNOUNCES each wait; #7167
// explicitly left open whether these two sites do. This one does: the
// `logger.Warn("accept: transient error, backing off", ..., "retry_in", backoff)`
// on the line before `time.Sleep(backoff)` emits the very local that is about to
// be slept. This file reads that attribute out of a captured log stream, so
// nothing here is gated on elapsed wall clock: `-race` shifts this package's
// timings by ~7x and the automatic CI legs run without it (#7062), so a window
// on 10ms-vs-20ms would be scheduling luck rather than evidence. The real sleeps
// still happen — 5+10+20+40+80+160ms, ~315ms in total at the shipped tuning —
// but no assertion reads a clock.
//
// WHAT IS NOT OBSERVABLE THROUGH IT, stated rather than left implied: the
// ANNOUNCED wait, not the slept one. A defect that announced X and slept 4X
// (`time.Sleep(backoff)` -> `backoff*4`) passes here. That is inherent to the
// seam and is the right trade for this package; the SHRINK direction of it is
// weakly covered by TestAcceptLoopSurvivesTransientErrors' 10ms floor, which is
// the one thing that floor is good for.
//
// WHAT THE ASSERTION IS, and what it deliberately is NOT. A RATIO between
// CONSECUTIVE announced waits, applied only where the clamp did not engage:
//
//	for every consecutive pair (a, b) of announced waits with b < acceptBackoffMax:
//	    b must equal 2*a
//
// It is NOT a literal sequence (`[5ms 10ms 20ms 40ms 80ms 160ms]`). A literal
// encodes acceptBackoffStart, acceptBackoffMax and the factor at once and breaks
// on any legitimate retune of the other two; the relation between neighbours
// breaks only on the factor. It is NOT a floor on one wait or on a total — that
// is precisely the assertion shape measured blind above and at the supervisor
// site.
//
// WHAT A WRONG FACTOR COSTS, with the adjective verified rather than borrowed.
// This loop, unlike the supervisor's, has NO terminating counter: it retries a
// transient Accept error forever, and by design — returning would make Run
// unlink the socket and drop every MCP client. But the WAIT is clamped at
// acceptBackoffMax, so a wrong factor is still a SHAPE change and NOT an
// unbounded tail: it changes how many cheap retries happen before the cadence
// settles at the 1s ceiling. Arithmetic from the shipped constants: doubling from
// 5ms takes 8 growth steps to reach 1s and retries 9 times in the first ~1.3s,
// where tripling takes 5 steps and retries 6 times. An EMFILE burst that clears
// in a few hundred milliseconds is therefore re-attempted fewer times before the
// daemon drops to once-a-second accepts. That is a tuning regression, not a
// correctness defect, and no runtime measurement of it is claimed — in
// particular the supervisor site's 1.39x time-to-give-up is NOT carried across:
// this loop never gives up, so the figure has no meaning here.
//
// MAGNITUDE, SCORED RATHER THAN INHERITED. At the supervisor site `*= 100` was
// already caught by an unrelated 30s bound while `*= 3` was invisible, so
// detectability there was non-monotone in magnitude and the unqualified claim
// "enlargement is ungraded" was false. That does not reproduce here, and the
// reason is structural: the clamp holds every wait at or below 1s, so even a
// factor of 100 costs the six scripted errors only ~4.5s and no leg's timeout
// is disturbed — there is no bound for a large factor to blow. Measured pre-fix
// (see MEASURED below): factors 1, 3, 8 and 100 are ALL ALIVE at 0 `--- FAIL`,
// so the survival band is every magnitude in both directions.
//
// WHICH ASSERTION KILLS WHICH MAGNITUDE. At factors 1, 3 and 4 the RATIO fires
// and the vacuity floor never does. At factors 8 and 100 the ratio still fires
// on the first pair (at 100 the first announced step is 500ms, still strictly
// below the 1s ceiling, so the pair is graded rather than skipped) and the floor
// fires TOO, because the clamp swallows the later steps. No row on the FACTOR
// axis is killed by the floor alone, and none is killed by anything outside
// this file.
//
// THE FLOOR'S OWN POSITIVE CONTROL, so "no factor row kills on the floor alone"
// is not read as "this floor is never exercised" — which would make it
// indistinguishable from a dead vacuity guard. It IS exercised, on the
// START-VALUE axis rather than the factor axis: mutating acceptBackoffStart
// 5ms -> 900ms in production (both new files present) yields exactly 1
// `--- FAIL`, this test, on the FLOOR ALONE — `only 0 of 5
// consecutive-backoff ratios sat below the 1s ceiling (sequence
// [900ms 1s 1s 1s 1s 1s])`, with 0 ratio errors. So the floor is live, it
// tracks the sequence production actually emits rather than being satisfied by
// a constant, and each site's floor now has a named killer: site A's on the
// factor axis (`*= 100`), site B's here. Two consequences worth stating
// plainly: this row is NOT this PR's arm (acceptBackoffStart's value is graded
// by nothing else in the package — see #7174), and it is the row that proves
// the guard is not self-satisfying.
//
// AXES. VARIED: the growth factor (1, 1.5, 3, 8 and 100 as well as 2 — both
// directions, enlargement split by magnitude, and one NON-INTEGER factor so the
// band is closed continuously rather than only at sampled integers), and the number of
// consecutive growth steps measured (five, versus the single total the existing
// floor looks at). HELD CONSTANT: the transient-error classification (the
// scripted errors are the ones TestIsTransientAcceptErr already pins as
// transient), the reset-on-success behaviour (no successful Accept is scripted
// mid-burst — that is TestAcceptLoopBackoffResets' role), and
// acceptBackoffStart/acceptBackoffMax, which this file reads but never asserts a
// value for.
//
// NOT GRADED HERE, on purpose: the clamp, the `backoff == 0` first-wait branch,
// the reset-on-success, and the shipped values of the two constants. A ratio
// between neighbours is blind to all four — deleting the clamp leaves every
// pre-clamp ratio at 2 and this file green, which keeps this arm disjoint from
// the existing ones rather than overlapping them. Nothing ELSE in the package
// grades that clamp either — #7126 pinned the SUPERVISOR's clamp, not this twin
// — so it is a real pre-existing hole, filed as #7174 and deliberately NOT
// fixed here rather than left implied.
//
// The two constants were hoisted out of acceptLoop's body to package scope in
// the same change, with no value or behaviour change, purely so the clamp-skip
// guard below can name the ceiling instead of hard-coding it. Hard-coding it
// would have put an absolute duration back into the assertion, which is the
// thing this file exists to avoid.

// MEASURED (full ./internal/daemon/, -count=1, gofmt clean, `go vet ./internal/daemon/`
// exit 0 recorded separately on every row; verdicts are counted `--- FAIL`
// LINES, never an exit code). Baseline on f9e7838e9: 0 `--- FAIL`. Unmutated
// tree with this file and its sibling: 0 `--- FAIL`, 487 tests listed.
//
// PRE-FIX (this file absent), site internal/daemon/server.go `backoff *= 2`:
//
//	*= 1                    -> 0 `--- FAIL`   ALIVE
//	backoff = backoff*3 / 2  -> 0 `--- FAIL`   ALIVE   (NON-INTEGER, x1.5)
//	*= 3                    -> 0 `--- FAIL`   ALIVE   (the row #7167 reported, re-derived here)
//	*= 8                    -> 0 `--- FAIL`   ALIVE
//	*= 100                  -> 0 `--- FAIL`   ALIVE
//
// The non-integer row is what makes the band claim a BAND rather than a set of
// sampled points: `backoff *= 2` is integer-nanosecond arithmetic, which is
// exactly where a rounding artefact could park a step back on an exact doubling
// and make a continuous claim false between the integers. Measured, it does
// not: x1.5 survives pre-fix and dies post-fix on the ratio like every integer
// row, so the band closes CONTINUOUSLY at this site too.
//
// WITH THIS FILE, every row is exactly 1 `--- FAIL` and it is THIS test, with
// no other test in the package reacting at any magnitude:
//
//	*= 1   -> ratio (5 errors), sequence [5ms 5ms 5ms 5ms 5ms 5ms]
//	*3/2   -> ratio (5 errors), floor NOT fired (5 of 5 pairs graded),
//	          sequence [5ms 7.5ms 11.25ms 16.875ms 25.3125ms 37.96875ms]
//	*= 3   -> ratio, sequence [5ms 15ms 45ms 135ms 405ms 1s]
//	*= 4   -> ratio, sequence [5ms 20ms 80ms 320ms 1s 1s]
//	*= 8   -> ratio AND floor (2 graded pairs), sequence [5ms 40ms 320ms 1s 1s 1s]
//	*= 100 -> ratio AND floor (1 graded pair), sequence [5ms 500ms 1s 1s 1s 1s]
//
// `*= 1` is NOT a confound at this site — it yields exactly 1 `--- FAIL`, this
// test — which is worth recording because it WAS one at the supervisor site,
// where removing growth entirely made five unrelated tests react. In
// particular TestAcceptLoopSurvivesTransientErrors' `elapsed >= 10ms` floor
// does not fire on it: six ungrown 5ms waits still total 30ms.
//
// HOLD-OUT POSITIVE CONTROL: `backoff *= 3` with THIS FILE HELD OUT (moved
// aside, not edited) -> 0 `--- FAIL`. Positive proof of the hold-out:
// `go test -list` shows 487 tests with the file and 486 without it, and
// TestAcceptLoopBackoffGrowsByExactlyTwoPerStep is absent from the 486. So the
// kills above are earned by this file and not inherited from a pre-existing
// test.
//
// SIBLING PINS UNTOUCHED: supervise_backoff_factor_test.go,
// supervise_backoff_clamp_test.go, #7123's arms and the two pre-existing
// acceptLoop tests are neither re-tuned nor re-keyed, and all are green in the
// unmutated run and in every mutant row above (each row's single `--- FAIL` is
// this test). `./cmd/grafel/` was NOT run and is not required: the diff stays
// inside internal/daemon/ and touches no extractor, so the graph digest cannot
// move.

const (
	// acceptFactorProbeErrors is how many consecutive transient Accept errors
	// the fixture scripts. Six produces six announced waits, five consecutive
	// pairs, and — at the shipped tuning — a longest wait of 160ms, an order of
	// magnitude below the 1s ceiling, so every pair is graded rather than
	// skipped.
	acceptFactorProbeErrors = 6
	// acceptFactorMinRatios is the vacuity floor: how many consecutive pairs
	// must sit strictly below acceptBackoffMax, i.e. how many steps the ratio
	// loop actually GRADED. Doubling from 5ms to a 1s ceiling gives five here;
	// three is required, so a retune that drove the early waits to the ceiling
	// fails loudly instead of passing with nothing asserted. Measured on the
	// mutants: factor 4 grades exactly three pairs and passes this floor (dying
	// on the ratio alone), while 8 grades two and 100 grades one, so both of
	// those trip it in addition to the ratio.
	acceptFactorMinRatios = 3
	// acceptFactorCeilingProbeErrors is the SECOND probe's script length: long
	// enough that the SHIPPED start value actually reaches the ceiling, so the
	// clamp-skip guard has something to exclude. Doubling 5ms needs eight
	// growth steps to pass 1s, so ten errors give seven sub-ceiling pairs and
	// two at the ceiling. Six (the other probe) never reaches it at all, which
	// is precisely why that probe cannot grade the guard.
	acceptFactorCeilingProbeErrors = 10
	// acceptFactorExpected is the factor under test. It appears once, as a
	// RATIO between neighbouring waits — never as a sequence and never as an
	// absolute duration.
	acceptFactorExpected = 2
)

// retryInRe reads the announced backoff out of the logfmt line acceptLoop emits
// immediately before sleeping it.
var retryInRe = regexp.MustCompile(`retry_in=([0-9a-zA-Z.]+)`)

// announcedAcceptBackoffs runs acceptLoop against a listener that returns n
// transient errors and then behaves as closed, and returns the sequence of
// waits the loop ANNOUNCED. It asserts the loop really walked the whole script,
// so a fixture that stopped early cannot pass vacuously.
func announcedAcceptBackoffs(t *testing.T, n int) []time.Duration {
	t.Helper()

	steps := make([]acceptStep, 0, n+1)
	for i := 0; i < n; i++ {
		steps = append(steps, acceptStep{err: syscall.EMFILE})
	}
	steps = append(steps, acceptStep{err: net.ErrClosed})
	fl := &fakeListener{steps: steps}

	sink := &lockedBuf{}
	srv := rpc.NewServer()
	var wg sync.WaitGroup
	done := make(chan struct{})
	logger := slog.New(slog.NewTextHandler(sink, nil))

	go acceptLoop(fl, srv, &wg, logger, done)
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatalf("acceptLoop never returned within the TEST's OWN 60s bound — this is the test having stopped waiting, not a statement about any backoff value; logs:\n%s", sink.String())
	}
	wg.Wait()

	logs := sink.String()
	matches := retryInRe.FindAllStringSubmatch(logs, -1)
	waits := make([]time.Duration, 0, len(matches))
	for _, m := range matches {
		d, err := time.ParseDuration(m[1])
		if err != nil {
			t.Fatalf("could not parse announced retry_in %q: %v; logs:\n%s", m[1], err, logs)
		}
		waits = append(waits, d)
	}

	// Positive control: every scripted transient error was seen and announced,
	// so the waits read below are this loop's backoffs and not some other
	// emission. The count is script-driven and factor-independent, so it
	// cannot stand in for the ratio pin.
	if len(waits) != n {
		t.Fatalf("acceptLoop announced %d backoffs (%v) for %d scripted transient errors — the loop did not walk the script, so the growth steps this test measures were never taken; logs:\n%s",
			len(waits), waits, n, logs)
	}
	return waits
}

// gradeAcceptBackoffRatios is THE PIN, and the SINGLE occurrence of the
// clamp-skip guard in this file. Every consecutive pair whose SECOND wait is
// strictly below the ceiling is a step the clamp did not touch, so the whole of
// its ratio is the growth factor; a pair AT the ceiling has the ceiling's ratio,
// not the factor's, and is excluded rather than graded.
//
// It lives in one place on purpose. The guard was written out twice in an
// earlier draft — once per probe — and a guard with two copies is graded at
// whichever copy a mutant happens to land on, which is how a twinned key hides
// a hole (#7172 review). One occurrence means one thing to grade.
//
// It returns BOTH halves of the partition rather than asserting on them,
// because the two probes below need different assertions about the split; each
// caller keeps its own floor, so extracting the loop did not free either caller
// from asserting.
func gradeAcceptBackoffRatios(t *testing.T, waits []time.Duration) (graded, excluded int) {
	t.Helper()
	for i := 1; i < len(waits); i++ {
		prev, cur := waits[i-1], waits[i]
		if cur >= acceptBackoffMax {
			excluded++
			continue
		}
		graded++
		if want := acceptFactorExpected * prev; cur != want {
			t.Errorf("announced transient-Accept backoff %d was %s after a %s wait, want %s — consecutive backoffs below the %s ceiling must grow by exactly %dx, and this step grew by %.3gx instead (whole sequence %v): the retry curve's shape is wrong, so the daemon spends the wrong number of cheap re-accepts before its cadence drops to the ceiling and MCP clients wait a full ceiling per connection",
				i+1, cur, prev, want, acceptBackoffMax, acceptFactorExpected,
				float64(cur)/float64(prev), waits)
		}
	}
	return graded, excluded
}

// TestAcceptLoopBackoffGrowsByExactlyTwoPerStep is #7167's pin for the server.go
// site: each announced transient-error backoff that the clamp did not cap must be
// exactly twice its predecessor. Asserted as a ratio between neighbours, never as
// a literal sequence and never as an elapsed duration.
func TestAcceptLoopBackoffGrowsByExactlyTwoPerStep(t *testing.T) {
	waits := announcedAcceptBackoffs(t, acceptFactorProbeErrors)

	graded, _ := gradeAcceptBackoffRatios(t, waits)

	// Vacuity floor: the loop above must actually have graded steps. A tuning
	// (or a mutant) that drove the early waits to the ceiling would skip every
	// pair and assert nothing at all.
	if graded < acceptFactorMinRatios {
		t.Errorf("only %d of %d consecutive-backoff ratios sat below the %s ceiling (sequence %v), want at least %d: with fewer steps below the ceiling this test measures the clamp rather than the growth factor, and would pass vacuously",
			graded, len(waits)-1, acceptBackoffMax, waits, acceptFactorMinRatios)
	}
}

// TestAcceptLoopBackoffClampSkipExcludesOnlyCeilingPairs grades the clamp-skip
// guard itself — the `cur >= acceptBackoffMax { continue }` exclusion in THIS
// FILE's grading loop — in the direction the probe above structurally cannot
// reach.
//
// NOT TO BE CONFUSED WITH the PRODUCTION clamp in server.go
// (`if backoff > acceptBackoffMax { backoff = acceptBackoffMax }`). That one is
// still ungraded by anything in the package and is filed as #7174; this test
// does not close it and does not claim to. What is graded here is the TEST's
// own exclusion rule — whether this file's assertions can be silently emptied.
//
// WHY A SECOND PROBE IS NEEDED, measured rather than assumed. #7162 established
// that this guard must be load-bearing in BOTH directions: a guard that can
// silently empty its own input set grades nothing. That property was never
// transferred to this site.
//
// GUARD-DIRECTION ROWS, full ./internal/daemon/ per row, one occurrence mutated
// at a time, `--- FAIL` LINES counted, `go vet` 0 on every row. BEFORE this
// probe existed (on 62602d1b9):
//
//	site A `if true  || cur >= rebuildBackoffMax` -> DEAD 1 (graded-floor)
//	site A `if false && cur >= rebuildBackoffMax` -> DEAD 1 (ratio)
//	site B `if true  || cur >= acceptBackoffMax`  -> DEAD 1 (graded-floor)
//	site B `if false && cur >= acceptBackoffMax`  -> ALIVE, 0 `--- FAIL`
//
// AFTER this probe, re-scored from scratch (baseline 0 `--- FAIL`):
//
//	site A `if true  || ...` -> DEAD 1: graded-floor, `only 0 of 5 ... want at
//	                            least 2`, sequence [30s 1m0s 2m0s 4m0s 5m0s 5m0s]
//	site A `if false && ...` -> DEAD 1: ratio, 2 errors — exactly the two pairs
//	                            at the 5m ceiling
//	site B `if true  || ...` -> DEAD 2: graded-floor in BOTH site-B tests
//	                            (`only 0 of 5` and `only 0 of 9`) — one guard,
//	                            two callers, so both react
//	site B `if false && ...` -> DEAD 1: THIS test, killed twice over — the
//	                            excluded-floor (`no consecutive pair reached the
//	                            1s ceiling (... 9 graded / 0 excluded)`) AND the
//	                            ratio (2 errors, the two ceiling pairs). The
//	                            six-error probe still does not react, which is
//	                            the masking below, preserved and now visible.
//
// The ALIVE row is NOT the guard being equivalent — recording it that way would
// retire a real hole permanently. It is the guard being MASKED BY THE FIXTURE:
// the probe above scripts six errors, whose announced sequence
// [5ms 10ms 20ms 40ms 80ms 160ms] never reaches the 1s ceiling, so
// `cur >= acceptBackoffMax` is false at every pair and deleting the branch
// cannot change the graded set. The guard would matter the moment a sequence
// reached the ceiling. This probe is that sequence.
//
// THE SITES ARE NOT SYMMETRIC HERE, and the file should not pretend otherwise.
// At the rebuildBackoff site the natural probe ALREADY crosses its ceiling —
// [30s 1m0s 2m0s 4m0s 5m0s 5m0s] hits 5m twice — so both directions of that
// guard are graded by the shipped walk with no extra fixture. At THIS site the
// shipped start (5ms) is 200x below the ceiling (1s), so a six-error script
// cannot get there and the guard needs a longer script to be exercised at all.
//
// HOW IT CROSSES, and why by script length rather than by start value. The
// obvious alternative was to raise acceptBackoffStart to 100ms, giving
// [100ms 200ms 400ms 800ms 1s 1s]. Rejected on two counts, both checked rather
// than asserted: acceptBackoffStart is a CONST, so there is no test seam for it
// and using it would mean a production const->var change purely to serve a
// test; and its split is 3 graded / 2 excluded, which sits EXACTLY on
// acceptFactorMinRatios (3) with zero margin, so any retune of the other two
// constants turns a vacuity floor into a spurious failure. Scripting more
// errors instead keeps the SHIPPED start value — this probe grades the shipped
// tuning, not an injected one — and needs no production change at all.
//
// THE ARITHMETIC, from the shipped constants. Doubling 5ms takes eight growth
// steps to pass 1s, so ten scripted errors announce
// [5ms 10ms 20ms 40ms 80ms 160ms 320ms 640ms 1s 1s]: nine consecutive pairs,
// of which SEVEN sit below the ceiling and TWO are at it. Those counts are
// documented, not asserted — asserting 7/2 would encode the start, the ceiling
// and the factor at once, which is the literal-sequence shape this file exists
// to avoid. What IS asserted is structural and survives a retune: the excluded
// set must be NON-EMPTY (otherwise this probe has silently degenerated into a
// duplicate of the one above and grades the guard no better), and the graded
// set must still clear the same vacuity floor.
//
// COST: the real sleeps total ~3.3s (5+10+20+40+80+160+320+640+1000+1000 ms).
// No assertion reads a clock — the waits are read out of the announced log
// stream, as above — so `-race` cannot move the verdict (#7062).
func TestAcceptLoopBackoffClampSkipExcludesOnlyCeilingPairs(t *testing.T) {
	waits := announcedAcceptBackoffs(t, acceptFactorCeilingProbeErrors)

	graded, excluded := gradeAcceptBackoffRatios(t, waits)

	// THE GUARD'S OWN VACUITY FLOOR, and the reason this test exists: if the
	// script never reached the ceiling, the clamp-skip branch was never taken
	// and this probe graded exactly what the shorter one already did.
	if excluded < 1 {
		t.Errorf("no consecutive pair reached the %s ceiling (sequence %v, %d graded / %d excluded) — this probe is supposed to be the one that CROSSES the clamp, so with nothing excluded the clamp-skip guard was never exercised and this test grades no more than the six-error probe does",
			acceptBackoffMax, waits, graded, excluded)
	}

	// ...and the guard must not have swallowed everything either, which is the
	// other direction and the one the `true ||` mutant takes.
	if graded < acceptFactorMinRatios {
		t.Errorf("only %d of %d consecutive-backoff ratios sat below the %s ceiling (sequence %v), want at least %d: the clamp-skip guard excluded so much that this test measures the clamp rather than the growth factor, and would pass vacuously",
			graded, len(waits)-1, acceptBackoffMax, waits, acceptFactorMinRatios)
	}
}
