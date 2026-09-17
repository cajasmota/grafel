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
// fires TOO, because the clamp swallows the later steps. No row here is killed
// by the floor alone, and none is killed by anything outside this file.
//
// AXES. VARIED: the growth factor (1, 3, 8 and 100 as well as 2 — both
// directions, with enlargement split by magnitude), and the number of
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
// the existing ones rather than overlapping them.
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
//	*= 1   -> 0 `--- FAIL`   ALIVE
//	*= 3   -> 0 `--- FAIL`   ALIVE   (the row #7167 reported, re-derived here)
//	*= 8   -> 0 `--- FAIL`   ALIVE
//	*= 100 -> 0 `--- FAIL`   ALIVE
//
// WITH THIS FILE, every row is exactly 1 `--- FAIL` and it is THIS test, with
// no other test in the package reacting at any magnitude:
//
//	*= 1   -> ratio, sequence [5ms 5ms 5ms 5ms 5ms 5ms]
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

// TestAcceptLoopBackoffGrowsByExactlyTwoPerStep is #7167's pin for the server.go
// site: each announced transient-error backoff that the clamp did not cap must be
// exactly twice its predecessor. Asserted as a ratio between neighbours, never as
// a literal sequence and never as an elapsed duration.
func TestAcceptLoopBackoffGrowsByExactlyTwoPerStep(t *testing.T) {
	waits := announcedAcceptBackoffs(t, acceptFactorProbeErrors)

	// THE PIN. Every consecutive pair whose SECOND wait is strictly below the
	// ceiling is a step the clamp did not touch, so the whole of its ratio is
	// the growth factor.
	graded := 0
	for i := 1; i < len(waits); i++ {
		prev, cur := waits[i-1], waits[i]
		if cur >= acceptBackoffMax {
			// The clamp engaged: the ratio here is the ceiling's, not the
			// factor's. Not this file's role.
			continue
		}
		graded++
		if want := acceptFactorExpected * prev; cur != want {
			t.Errorf("announced transient-Accept backoff %d was %s after a %s wait, want %s — consecutive backoffs below the %s ceiling must grow by exactly %dx, and this step grew by %.3gx instead (whole sequence %v): the retry curve's shape is wrong, so the daemon spends the wrong number of cheap re-accepts before its cadence drops to the ceiling and MCP clients wait a full ceiling per connection",
				i+1, cur, prev, want, acceptBackoffMax, acceptFactorExpected,
				float64(cur)/float64(prev), waits)
		}
	}

	// Vacuity floor: the loop above must actually have graded steps. A tuning
	// (or a mutant) that drove the early waits to the ceiling would skip every
	// pair and assert nothing at all.
	if graded < acceptFactorMinRatios {
		t.Errorf("only %d of %d consecutive-backoff ratios sat below the %s ceiling (sequence %v), want at least %d: with fewer steps below the ceiling this test measures the clamp rather than the growth factor, and would pass vacuously",
			graded, len(waits)-1, acceptBackoffMax, waits, acceptFactorMinRatios)
	}
}
