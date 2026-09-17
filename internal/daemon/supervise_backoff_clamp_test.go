package daemon

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// supervise_backoff_clamp_test.go grades #7126: the SECOND role of
// defaultEngineBackoffMax. The constant is read in two places, and #7111/#7123
// graded only the first:
//
//	backoffAndMaybeGiveUp: if *backoff >= s.backoffMax { ceilingHits++ }  <- give-up THRESHOLD (graded)
//	waitBackoff:           *backoff *= 2; if *backoff > s.backoffMax {    <- CLAMP on the wait (this file)
//	                           *backoff = s.backoffMax
//	                       }
//
// Deleting the clamp entirely left ./internal/daemon/ green (0 `--- FAIL`):
// the waits kept doubling PAST the ceiling the give-up verdict is counted at,
// and nothing noticed. What that costs is a BOUND VIOLATION, not an unbounded
// tail — maxCeilingHits still ends the loop, so an unclamped run takes the
// same NUMBER of waits and reaches the same verdict (measured on the crash
// path at a 1:4 ratio and production's maxCeilingHits=3: [1 2 4 4] = 11ms
// clamped versus [1 2 4 8] = 15ms unclamped, four waits and a verdict either
// way). Dropping the clamp buys maxCeilingHits-1 extra doublings: at the
// shipped tuning 91.5s becomes 127.5s, a bounded 1.39x (arithmetic over the
// constants, not executed — this file injects a millisecond ceiling). The
// defect graded here is the sleeps of 32s and 64s against a 30s counted
// ceiling, which is enough on its own.
//
// #7123's two backoffMax arms are blind to it BY CONSTRUCTION, which is why
// they must not be re-tuned to cover it:
//
//   - its shrink arm injects a 400ms first wait against the 30s production
//     ceiling and asserts the second wait is a real doubling; 800ms is far
//     under the ceiling, so the clamp never fires there;
//   - its growth arms inject a first wait >= backoffMax, so the verdict lands
//     with zero sleeping — no wait is ever taken, let alone clamped.
//
// WHAT IS OBSERVED, and why it is not a duration. The clamp's entire effect IS
// the value handed to the relaunch timer, so the artefact graded here is the
// SEQUENCE of waits the supervisor announces ("relaunching engine after
// backoff", backoff=<d>, the same local that is passed to time.NewTimer on the
// next line), and the assertion is a BOUND on that sequence — no announced
// wait may exceed s.backoffMax. Nothing here is gated on elapsed wall clock:
// `-race` moves this package's timings by roughly 7x and the automatic CI legs
// run without it, so a window on 4ms-vs-8ms would be scheduling luck rather
// than evidence (#7062, and #7123's own round 2 had to drop fixed 1s/2s
// windows for exactly this reason).
//
// That the announced value corresponds to a REAL sleep is graded elsewhere and
// deliberately not re-graded here: #7111's spawn-spacing floors measure the
// interval between spawn attempts from outside the supervisor, so a "stop
// sleeping" break fails them. This file grades the bound on the sequence; that
// file grades that the sequence is really slept. The two are disjoint.
//
// WHAT THE FIXTURE ACTUALLY WALKS. The construction-failure path, where NO
// ceiling hit ever occurs: ceilingHits stays 0 (only backoffAndMaybeGiveUp,
// which just the crash branch calls, touches it) and the verdict is the spawn
// budget's, spawnFailures >= maxSpawnFailures. What it exercises is waitBackoff
// — the clamp's home, shared by both paths — for seven consecutive waits, so
// four of them are taken with the pending backoff already AT the ceiling. That
// is the same code the crash path's post-ceiling-hit window runs, with no child
// processes and no dependence on child scheduling.
//
// WHAT THIS FILE DOES NOT GRADE. Only the clamp's BOUND. Removing the
// comparison so the clamp fires unconditionally (`*backoff = s.backoffMax`
// every time) keeps every wait at or below the ceiling, so this file PASSES it;
// that over-fire direction is owned by #7123's
// TestEngineSupervisor_DefaultBackoffCeilingLeavesRoomToGrow — and only via a
// 30s deadline whose message ("the supervisor stopped re-attempting")
// misdiagnoses a supervisor that is in fact sleeping at the ceiling. Inverting
// the comparison fails both files. The growth FACTOR on the same two lines
// (`*backoff *= 2`) is a third, independent role; it was ungraded when this
// file landed and is now graded by supervise_backoff_factor_test.go (#7162).
// The two pins are measurably disjoint: deleting the clamp fails only this
// file, and changing the factor to 3 or to 4 fails only that one.
//
// AXES. VARIED: the number of consecutive waits taken (the spawn budget is
// widened so the walk continues past the point the pending backoff reaches the
// ceiling — the region #7123 cannot reach), and the wait values themselves.
// HELD CONSTANT: the failure mode (a
// construction failure that fails identically every time, so no child process
// ever exists and nothing depends on child scheduling), and the ratio between
// the injected initial wait and the injected ceiling.
//
// This pin says NOTHING about the shipped VALUE of defaultEngineBackoffMax: it
// injects a millisecond ceiling on purpose, because the mechanism is what is
// ungraded. The production value keeps its own two-sided band in
// supervise_defaults_backoff_test.go, which never injects backoffMax at all.

const (
	// clampProbeInitial and clampProbeMax are the test's own tuning, not
	// defaults. The ratio (1:4) is what makes the walk cross the ceiling by
	// doubling — 1ms, 2ms, 4ms — so the waits AFTER the crossing are the ones
	// only the clamp can hold down.
	clampProbeInitial = 1 * time.Millisecond
	clampProbeMax     = 4 * time.Millisecond
	// clampProbeSpawnBudget is widened well past the 3 waits production
	// tolerates so the walk keeps taking waits after the ceiling is reached.
	// With a 1:4 ratio this yields 7 waits: three climbing (1ms, 2ms, 4ms) and
	// four that exist only because the budget outlives the ceiling.
	clampProbeSpawnBudget = 8
	// clampProbeWaits is the exact number of relaunch waits the budget above
	// must produce: one per failed spawn except the last, which gives up
	// instead of waiting. Asserted, so a fixture that stops walking early
	// cannot pass vacuously.
	clampProbeWaits = clampProbeSpawnBudget - 1
	// clampProbeAtCeiling is how many of those waits must sit AT the ceiling.
	// Under the clamp the sequence is 1, 2, 4, 4, 4, 4, 4 — five. Requiring at
	// least three fails both a clamp that stops capping (1, 2, 4, 8, 16, 32,
	// 64: one) and a clamp that caps to the wrong value, e.g. back to the
	// initial wait (1, 2, 4, 1, 2, 4, 1: two).
	clampProbeAtCeiling = 3
)

// runSpawnFailUntilFatalWithClampTuning runs a supervisor on the
// construction-failure path (a child path that does not exist, so cmd.Start
// fails identically every time and no process is ever created) with the clamp
// probe's tuning, waits for the unspawnable fatal, and returns the emitted log
// stream.
//
// The construction-failure path is used because the clamp lives in
// waitBackoff, which BOTH paths share — the crash path reaches it through
// backoffAndMaybeGiveUp and the spawn-failure path calls it directly — so this
// grades the same code with no child processes, no child scheduling and no
// dependence on how fast a re-exec of the test binary starts.
func runSpawnFailUntilFatalWithClampTuning(t *testing.T) string {
	t.Helper()
	root := isolateSpawnFailEnv(t)

	defer SetEngineChildCommandForTest(func(_ string, _ string) *exec.Cmd {
		return unspawnableCommand(t)
	})()

	sink := &lockedBuf{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sup := newEngineSupervisor(layoutFromRoot(root, ""), buildSlogLogger(sink))
	sup.backoffInitial = clampProbeInitial
	sup.backoffMax = clampProbeMax
	sup.maxSpawnFailures = clampProbeSpawnBudget
	sup.healthyUptime = time.Hour // nothing here ever "recovers" by uptime
	sup.drainTimeout = 2 * time.Second
	if err := sup.start(ctx); err != nil {
		t.Fatalf("supervisor start: %v", err)
	}
	t.Cleanup(sup.stop)

	// A generous outer bound that asserts nothing about any default: the whole
	// walk is 7 millisecond-scale waits, so exceeding this means no supervision
	// happened at all, and the message says so.
	select {
	case <-sup.fatal():
	case <-time.After(60 * time.Second):
		t.Fatalf("supervisor never surfaced the unspawnable fatal within the TEST's OWN 60s bound — this is the test having stopped waiting, not a statement about any backoff default; logs:\n%s",
			sink.String())
	}
	sup.stop()
	return sink.String()
}

// relaunchWaits extracts the announced relaunch waits, in order, from the
// supervisor's log stream. Only the relaunch line is read, and only its
// `backoff` attribute — the value handed to the relaunch timer.
func relaunchWaits(t *testing.T, logs string) []time.Duration {
	t.Helper()
	var out []time.Duration
	for _, line := range strings.Split(logs, "\n") {
		if !strings.Contains(line, relaunchAfterBackoffLine) {
			continue
		}
		_, rest, ok := strings.Cut(line, " backoff=")
		if !ok {
			t.Fatalf("relaunch line has no backoff attribute to read: %q", line)
		}
		field, _, _ := strings.Cut(rest, " ")
		d, err := time.ParseDuration(strings.TrimSpace(field))
		if err != nil {
			t.Fatalf("relaunch line announced an unparseable wait %q: %v (line %q)", field, err, line)
		}
		out = append(out, d)
	}
	return out
}

// TestEngineSupervisor_BackoffWaitsAreBoundedByTheCeiling is #7126's pin: every
// wait the supervisor takes must be bounded by s.backoffMax, including the ones
// taken AFTER the ceiling has been reached — the region the give-up threshold's
// own pins never enter. The assertion is the bound on the announced sequence,
// never an elapsed duration.
func TestEngineSupervisor_BackoffWaitsAreBoundedByTheCeiling(t *testing.T) {
	logs := runSpawnFailUntilFatalWithClampTuning(t)
	waits := relaunchWaits(t, logs)

	// Positive control: this really exercised the construction-failure path,
	// so the waits read are backoffs and not some other emission.
	if !strings.Contains(logs, "no child process was created") {
		t.Fatalf("no construction failure was ever emitted — this test graded nothing; logs:\n%s", logs)
	}
	// Positive control: the walk went all the way to the budget, so waits
	// beyond the ceiling crossing were actually taken. Without this the pin
	// could pass on a two-wait sequence that never reaches the clamp.
	if len(waits) != clampProbeWaits {
		t.Fatalf("observed %d relaunch waits (%v), want exactly %d — the walk did not take the waits that follow the ceiling crossing, so the clamp was never reached; logs:\n%s",
			len(waits), waits, clampProbeWaits, logs)
	}
	// Positive control: the sequence really climbed, rather than starting at
	// the ceiling (which is how #7123's growth arms end up clamping nothing).
	climbed := false
	for _, w := range waits {
		if w < clampProbeMax {
			climbed = true
			break
		}
	}
	if !climbed {
		t.Fatalf("every observed wait was already at the %s ceiling (%v) — the fixture never climbed, so a clamp could not have fired; logs:\n%s",
			clampProbeMax, waits, logs)
	}

	// THE PIN, direction 1 — the bound. No wait may exceed the ceiling. This
	// is what deleting the clamp, or capping to a larger value than the
	// ceiling, breaks: the waits keep doubling past the ceiling, so the
	// supervisor sleeps longer than the ceiling the crash verdict is counted
	// at (32s and 64s against a 30s ceiling at the shipped tuning).
	for i, w := range waits {
		if w > clampProbeMax {
			t.Errorf("relaunch wait %d of %d was %s, which exceeds the %s backoff ceiling (whole sequence %v): the backoff is growing past the ceiling instead of being held at it, so the supervisor sleeps longer than the ceiling its own give-up verdict is counted at — a bound violation (the wait COUNT is still bounded by maxCeilingHits; the cost is a stretched tail, 1.39x at the shipped tuning, not an unbounded one)",
				i+1, len(waits), w, clampProbeMax, waits)
		}
	}

	// THE PIN, direction 2 — the cap is the ceiling, not something smaller.
	// A clamp that fires but resets the wait (to the initial wait, say) also
	// keeps the bound; what it destroys is the backoff staying AT the ceiling
	// once it gets there.
	atCeiling := 0
	for _, w := range waits {
		if w == clampProbeMax {
			atCeiling++
		}
	}
	if atCeiling < clampProbeAtCeiling {
		t.Errorf("only %d of %d relaunch waits sat at the %s ceiling (sequence %v), want at least %d: once the backoff reaches the ceiling it must be HELD there, not reset to a smaller wait",
			atCeiling, len(waits), clampProbeMax, waits, clampProbeAtCeiling)
	}
}
