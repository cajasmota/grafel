package main

// group_algo_cpu_budget_6108_test.go — the in-process group-algo fallback must
// run under a real, in-force CPU bound (#6108).
//
// WHAT WENT WRONG. `runGroupAlgo` logs `cap=<n>` where n is
// groupAlgoChildGOMAXPROCS — the GOMAXPROCS the group-algo CHILD would be
// spawned with. When the pass runs IN-PROCESS instead (the
// GRAFEL_SUBPROCESS_INDEXER=0 fallback) there is no child, nothing consumes
// that number, and the pass runs at the daemon's own GOMAXPROCS = every core on
// the box. Measured live: `cap=2` logged, 571.9% CPU sustained for hours inside
// the daemon serving MCP.
//
// WHY THESE ARE BEHAVIOURAL AND NOT SOURCE SCANS. Three source-scanning guards
// in this repo have already fallen to trivial mutants, and #6091 exists
// precisely because the subprocess budget had no test that observed it. These
// tests drive the REAL daemonSchedulerGroupAlgo and observe the effective
// runtime.GOMAXPROCS in force at the moment the pass body runs. Deleting the
// cap, resolving it from the foreground branch for background work, or applying
// it around nothing all fail here.

import (
	"bytes"
	"context"
	"log/slog"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph/groupalgo"

	"github.com/cajasmota/grafel/internal/daemon/sched"
	"github.com/cajasmota/grafel/internal/process"
)

// observeInProcessGroupAlgoCap runs the real daemonSchedulerGroupAlgo on the
// in-process branch and reports (the cap it asked for, the GOMAXPROCS actually
// in force inside the capped region). A zero observed cap means the fallback
// applied no bound at all.
//
// The group does not exist, so the pass body errors out promptly — that error
// is expected and ignored. What is asserted is the runtime state the body would
// have run under, which is established before the body is entered.
func observeInProcessGroupAlgoCap(t *testing.T, group string, baseline int) (askedFor, inForce int) {
	t.Helper()
	// PIN the baseline. GOMAXPROCS is settable independent of the host, so the
	// clamp is observable on a 1-core CI runner exactly as on a 12-core laptop.
	// The first draft skipped the clamp assertion when NumCPU <= cap, which
	// silently disarmed it on precisely the machines most likely to run CI.
	prevProcs := runtime.GOMAXPROCS(baseline)
	t.Cleanup(func() { runtime.GOMAXPROCS(prevProcs) })

	// Isolate all grafel state: the pass resolves the group from the registry.
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	// No operator override — assert the POLICY default.
	t.Setenv("GRAFEL_GROUP_ALGO_CPU", "")
	t.Setenv("GRAFEL_REBUILD_GOMAXPROCS", "")
	// The heartbeat is not under test here; keep it off so it cannot interleave.
	t.Setenv("GRAFEL_GROUP_ALGO_PROGRESS_INTERVAL", "0")

	// Force the in-process fallback: this is the path under test.
	prevSub := sched.SetSubprocessIndexEnabled(false)
	t.Cleanup(func() { sched.SetSubprocessIndexEnabled(prevSub) })

	prevApply := groupAlgoCapApply
	t.Cleanup(func() { groupAlgoCapApply = prevApply })
	groupAlgoCapApply = func(n int, fn func() error) error {
		askedFor = n
		// Apply the cap for real, then observe what the pass body sees.
		return process.WithGOMAXPROCSCap(n, func() error {
			inForce = runtime.GOMAXPROCS(0)
			return fn()
		})
	}

	_ = daemonSchedulerGroupAlgo(context.Background(), group)
	return askedFor, inForce
}

// TestGroupAlgoCapSeam_DefaultsToTheRealCap is the M7 guard.
//
// The cap is reached through a package var so a test can observe it. That seam
// is also the obvious way to make the whole fix vacuous: replace the default
// with a passthrough and every assertion below still passes while production
// applies no cap at all. #6091 is exactly that failure, one layer down. So pin
// the DEFAULT — the wiring is the thing #6108 is about.
func TestGroupAlgoCapSeam_DefaultsToTheRealCap(t *testing.T) {
	got := reflect.ValueOf(groupAlgoCapApply).Pointer()
	want := reflect.ValueOf(process.WithGOMAXPROCSCap).Pointer()
	if got != want {
		t.Fatalf("groupAlgoCapApply does not default to process.WithGOMAXPROCSCap — production applies no CPU bound and every seam-based test still passes (#6108 / the #6091 shape)")
	}
}

// TestInProcessGroupAlgo_BackgroundRunsUnderTheBackgroundCPUCap is the direct
// regression for the observed defect.
func TestInProcessGroupAlgo_BackgroundRunsUnderTheBackgroundCPUCap(t *testing.T) {
	want := sched.GroupAlgoGOMAXPROCS() // BACKGROUND resolution
	askedFor, inForce := observeInProcessGroupAlgoCap(t, "no-such-group-6108", want+4)

	if askedFor == 0 {
		t.Fatalf("the in-process group-algo fallback applied NO CPU cap — the scheduler logs cap=%d and then runs the pass at the daemon's full GOMAXPROCS (#6108)", want)
	}
	if askedFor != want {
		t.Errorf("in-process group-algo cap = %d, want the background cap %d — the number the scheduler logs and the number it enforces must be the same number (#6108)", askedFor, want)
	}
	if inForce != want {
		t.Errorf("GOMAXPROCS in force during the in-process group-algo pass = %d, want %d — a serial pass on %d Ps lets the GC fill every idle P with mark workers, which is where the measured 571.9%% came from (#6108)",
			inForce, want, inForce)
	}
}

// TestInProcessGroupAlgo_ForegroundIsNotThrottled pins the other half of the
// policy: work a human is waiting on is deliberately UNCAPPED.
//
// GIVING THIS TEETH. The foreground resolution is the host core count, which
// equals the ambient GOMAXPROCS, so "inForce == baseline" would hold whether or
// not the never-raise rule worked. So the baseline is pinned BELOW the
// foreground cap: a clamp would show as a drop, and a naive max() that took the
// resolved cap literally would show as a RISE. Only leaving the runtime alone
// passes.
func TestInProcessGroupAlgo_ForegroundIsNotThrottled(t *testing.T) {
	const group = "fg-group-6108"
	release := sched.MarkGroupForeground(group)
	t.Cleanup(release)

	const baseline = 2
	askedFor, inForce := observeInProcessGroupAlgoCap(t, group, baseline)

	if want := sched.ForegroundReindexGOMAXPROCS(); askedFor != want {
		t.Errorf("foreground in-process group-algo cap = %d, want %d (the foreground resolution)", askedFor, want)
	}
	if inForce != baseline {
		t.Errorf("GOMAXPROCS during a FOREGROUND in-process group-algo pass = %d, want the untouched %d — user-awaited work must be left exactly as it was, neither throttled to the background budget nor widened to the resolved cap (#5954)", inForce, baseline)
	}
}

// TestInProcessGroupAlgo_ForegroundDoesNotQueueBehindABackgroundRegion is the
// H1 regression at the call site.
//
// A foreground pass resolves a cap that lowers nothing, so it must not merely
// leave the VALUE alone — it must not WAIT. With the cap primitive serialising
// every region, a user-awaited rebuild's follow-on pass would block for the
// whole lifetime of an in-flight background sweep: minutes to hours,
// uncancellable, and invisible to any assertion about the cap integer.
func TestInProcessGroupAlgo_ForegroundDoesNotQueueBehindABackgroundRegion(t *testing.T) {
	const group = "fg-nonblocking-6108"
	release := sched.MarkGroupForeground(group)
	t.Cleanup(release)

	prevProcs := runtime.GOMAXPROCS(8)
	t.Cleanup(func() { runtime.GOMAXPROCS(prevProcs) })

	// A background region, held open for the duration.
	entered, hold, bgDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(bgDone)
		_ = process.WithGOMAXPROCSCap(1, func() error { close(entered); <-hold; return nil })
	}()
	<-entered
	defer func() { close(hold); <-bgDone }()

	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GRAFEL_GROUP_ALGO_CPU", "")
	t.Setenv("GRAFEL_GROUP_ALGO_PROGRESS_INTERVAL", "0")
	prevSub := sched.SetSubprocessIndexEnabled(false)
	t.Cleanup(func() { sched.SetSubprocessIndexEnabled(prevSub) })

	done := make(chan struct{})
	go func() { defer close(done); _ = daemonSchedulerGroupAlgo(context.Background(), group) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a FOREGROUND in-process group-algo pass blocked behind a live background capped region — the user's rebuild serialises behind background churn (#6108 H1)")
	}
}

// TestGroupAlgoProgress_EmitsPhaseAndCPU: the pass must report progress while
// it runs. ~4 hours of total silence after "starting" is what made #6108
// undiagnosable from outside — there was no way to distinguish slow progress
// from a wedge, and no CPU number next to the cap the line claimed.
func TestGroupAlgoProgress_EmitsPhaseAndCPU(t *testing.T) {
	t.Setenv("GRAFEL_GROUP_ALGO_PROGRESS_INTERVAL", "20ms")

	var mu sync.Mutex
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&syncWriter{mu: &mu, w: buf}, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	groupalgo.ResetPhaseHistory()
	groupalgo.SetPhase(groupalgo.PhaseRunningAlgorithms)
	t.Cleanup(groupalgo.ResetPhaseHistory)

	stop := startGroupAlgoProgress(context.Background(), "gProg", "in-process")
	deadline := time.Now().Add(5 * time.Second)
	var out string
	for time.Now().Before(deadline) {
		mu.Lock()
		out = buf.String()
		mu.Unlock()
		if strings.Contains(out, "group-algo: progress") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	stop()

	if !strings.Contains(out, "group-algo: progress") {
		t.Fatalf("a running group-algo pass emitted no progress line — slow and wedged remain indistinguishable (#6108):\n%s", out)
	}
	for _, field := range []string{"group=gProg", "mode=in-process", "elapsed=", "phase=" + groupalgo.PhaseRunningAlgorithms} {
		if !strings.Contains(out, field) {
			t.Errorf("progress line is missing %q:\n%s", field, out)
		}
	}
	if !strings.Contains(out, "cpu_pct=") {
		t.Errorf("progress line carries no cpu_pct — `cap=2` beside the real draw is the single field that would have made #6108 self-evident:\n%s", out)
	}
}

// TestGroupAlgoProgress_StopsWithThePass: the heartbeat must not outlive the
// pass, or a daemon accumulates one ticker goroutine per group-algo run.
//
// #6134 — WHY THIS IS ASSERTED WITHOUT A SETTLING SLEEP, AND WHY THE PRODUCT
// CHANGED. This test used to sleep 30ms after stop() before taking its
// baseline count, and compared that baseline against a count 80ms later. It
// failed under host load (1 -> 2 lines) and passed in isolation, which reads
// like a tight test. It was not: the 30ms sleep was hiding a genuine race, and
// on a loaded machine the race simply outlived the sleep.
//
// stop() closed a channel and returned. The heartbeat goroutine selected over
// {done, ctx.Done(), ticker}. A Go select chooses UNIFORMLY AT RANDOM among
// ready cases, so once a tick was pending, a closed `done` won only half the
// time — and the goroutine had no obligation to have reached the select at all.
// A line could therefore be emitted AFTER stop() returned, reporting elapsed
// time and a phase for a pass that had already ended. That is the #6047 class
// (progress reported after the work it describes has finished), not a test
// artifact: the wizard could print a heartbeat for a finished pass.
//
// startGroupAlgoProgress now makes stop() a real barrier — it re-checks the
// stop signal after a tick wins the select, and waits for the goroutine to
// exit before returning. So the guarantee is exact and needs no settling
// window: once stop() has RETURNED, the line count is final forever. Sampling
// immediately after stop() is what actually tests that guarantee; sleeping
// first tests a weaker one and is what made this load-sensitive.
func TestGroupAlgoProgress_StopsWithThePass(t *testing.T) {
	t.Setenv("GRAFEL_GROUP_ALGO_PROGRESS_INTERVAL", "10ms")

	var mu sync.Mutex
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&syncWriter{mu: &mu, w: buf}, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	stop := startGroupAlgoProgress(context.Background(), "gStop", "child")
	time.Sleep(60 * time.Millisecond)
	stop()

	// No settling sleep: the count is taken the instant stop() returns.
	mu.Lock()
	settled := strings.Count(buf.String(), "group-algo: progress")
	mu.Unlock()

	// Fixture validity: if the heartbeat never emitted anything, "no further
	// lines" is vacuously true and this test proves nothing.
	if settled == 0 {
		t.Fatalf("heartbeat emitted no progress line in 60ms at a 10ms interval — "+
			"the no-further-lines assertion below would be vacuous:\n%s", buf.String())
	}

	// Several more intervals must produce nothing.
	time.Sleep(120 * time.Millisecond)
	mu.Lock()
	after := strings.Count(buf.String(), "group-algo: progress")
	mu.Unlock()
	if after != settled {
		t.Errorf("progress heartbeat kept ticking after stop() RETURNED (%d -> %d lines) — "+
			"the pass is over and the heartbeat is still reporting on it (#6134/#6047)", settled, after)
	}
	// Calling stop twice must not panic on a double close, and must still return.
	stop()
}

// TestGroupAlgoProgress_StopIsABarrier is the direct, load-independent pin on
// the #6134 guarantee, separate from the line-count test above: stop() must not
// return until the heartbeat goroutine has actually exited. Asserted by having
// the log sink record whether any write happens after stop() returns, over many
// stop/start cycles — the race is probabilistic per cycle (a select coin-flip),
// so a single cycle is not evidence either way.
func TestGroupAlgoProgress_StopIsABarrier(t *testing.T) {
	t.Setenv("GRAFEL_GROUP_ALGO_PROGRESS_INTERVAL", "1ms")

	for i := range 40 {
		var mu sync.Mutex
		buf := &bytes.Buffer{}
		var stopped bool
		var lateWrite bool
		sink := &lateDetectWriter{mu: &mu, w: buf, stopped: &stopped, late: &lateWrite}

		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(sink, nil)))

		stop := startGroupAlgoProgress(context.Background(), "gBarrier", "child")
		// Long enough at the 1ms floor for the ticker to be firing steadily, so
		// stop() genuinely races a pending tick rather than arriving before the
		// first one.
		time.Sleep(20 * time.Millisecond)
		stop()
		mu.Lock()
		stopped = true
		mu.Unlock()

		// Give any goroutine that survived stop() ample room to emit.
		time.Sleep(10 * time.Millisecond)

		mu.Lock()
		late := lateWrite
		out := buf.String()
		mu.Unlock()
		slog.SetDefault(prev)

		if late {
			t.Fatalf("cycle %d: the heartbeat wrote a line AFTER stop() returned — stop() is not a "+
				"barrier, so a finished pass can still report progress (#6134):\n%s", i, out)
		}
	}
}

// lateDetectWriter flags any write that lands after the test marked the pass
// stopped. Unlike a count comparison it cannot be satisfied by a write that
// merely happens to arrive between two samples.
type lateDetectWriter struct {
	mu      *sync.Mutex
	w       *bytes.Buffer
	stopped *bool
	late    *bool
}

func (s *lateDetectWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if *s.stopped {
		*s.late = true
	}
	return s.w.Write(p)
}

// TestGroupAlgoProgress_Disabled: "0" turns the heartbeat off entirely.
func TestGroupAlgoProgress_Disabled(t *testing.T) {
	t.Setenv("GRAFEL_GROUP_ALGO_PROGRESS_INTERVAL", "0")

	var mu sync.Mutex
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&syncWriter{mu: &mu, w: buf}, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	stop := startGroupAlgoProgress(context.Background(), "gOff", "in-process")
	time.Sleep(50 * time.Millisecond)
	stop()
	mu.Lock()
	defer mu.Unlock()
	if strings.Contains(buf.String(), "group-algo: progress") {
		t.Errorf("GRAFEL_GROUP_ALGO_PROGRESS_INTERVAL=0 must disable the heartbeat:\n%s", buf.String())
	}
}

type syncWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// TestGroupAlgoProgressEvery_OverrideParsing covers the two knob defects review
// caught: "0s" (a valid zero duration) silently fell back to the 60s default
// while bare "0" disabled, and "1ns" was accepted and spun a core logging.
func TestGroupAlgoProgressEvery_OverrideParsing(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want time.Duration
	}{
		{"", groupAlgoProgressInterval},
		{"0", 0},
		{"0s", 0},
		{"0ms", 0},
		{"-5s", 0},
		{"1ns", groupAlgoProgressMin},
		{"1ms", groupAlgoProgressMin},
		{"30s", 30 * time.Second},
		{"not-a-duration", groupAlgoProgressInterval},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			t.Setenv("GRAFEL_GROUP_ALGO_PROGRESS_INTERVAL", tc.raw)
			if got := groupAlgoProgressEvery(); got != tc.want {
				t.Errorf("groupAlgoProgressEvery(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

// TestGroupAlgoProgress_NoLineIsCommittedInsideTheStopWindow is the test that
// pins the IN-LOOP RE-CHECK, as opposed to the barrier.
//
// #6134 — WHY THE OTHER TWO TESTS ARE NOT ENOUGH. Both of them sample at or
// after stop() RETURNS, so neither can observe a line committed *during* stop().
// Mutation-tested: removing the `<-exited` barrier kills them, but removing the
// re-check leaves them fully green — the extra line is written while stop() is
// still waiting, and by the time stop() returns the goroutine is gone and the
// count is stable. So the half of the fix that actually prevents a finished pass
// from reporting was, until this test, unpinned.
//
// HOW THE RACE IS MADE DETERMINISTIC RATHER THAN SAMPLED. A rate-based check
// ("count writes inside the stop window") has an irreducible false positive: a
// tick body that was already in flight when the stop signal was set is a
// legitimate line, and no external observer can distinguish it. This test
// removes the ambiguity by driving the goroutine into a known position instead:
//
//  1. A gated sink blocks the goroutine INSIDE its first log write.
//  2. The test then sleeps past the interval, so the ticker has a tick queued
//     (time.Ticker buffers exactly one).
//  3. stop() is called from another goroutine; it closes `done` and blocks on
//     the barrier. `done` is now closed BEFORE the goroutine re-enters select.
//  4. The sink is released. The goroutine finishes write #1 and loops into a
//     select where BOTH `done` and the pending tick are ready — the exact
//     coin-flip the re-check exists for.
//
// With the re-check, no second line is possible, by construction. Without it,
// select picks the tick about half the time and emits one. Repeated cycles make
// a surviving mutant vanishingly unlikely while keeping false positives at
// exactly zero — the legitimate in-flight line is write #1, which is expected
// and counted.
func TestGroupAlgoProgress_NoLineIsCommittedInsideTheStopWindow(t *testing.T) {
	t.Setenv("GRAFEL_GROUP_ALGO_PROGRESS_INTERVAL", "10ms")

	const cycles = 24
	for i := range cycles {
		sink := &gatedWriter{
			entered: make(chan struct{}),
			release: make(chan struct{}),
		}
		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(sink, nil)))

		stop := startGroupAlgoProgress(context.Background(), "gWindow", "child")

		// (1) Park the goroutine inside its first write.
		select {
		case <-sink.entered:
		case <-time.After(5 * time.Second):
			slog.SetDefault(prev)
			close(sink.release)
			stop()
			t.Fatalf("cycle %d: heartbeat never emitted a first line — the fixture cannot "+
				"position the goroutine and the assertion below would be vacuous", i)
		}

		// (2) Queue a tick behind it.
		time.Sleep(30 * time.Millisecond)

		// (3) Close `done` while the goroutine is still parked in the write.
		stopReturned := make(chan struct{})
		go func() {
			stop()
			close(stopReturned)
		}()
		time.Sleep(20 * time.Millisecond) // let stop() reach close(done)

		// (4) Release: the goroutine now re-enters select with both ready.
		close(sink.release)

		select {
		case <-stopReturned:
		case <-time.After(5 * time.Second):
			slog.SetDefault(prev)
			t.Fatalf("cycle %d: stop() did not return after the sink was released", i)
		}
		slog.SetDefault(prev)

		if got := sink.count(); got != 1 {
			t.Fatalf("cycle %d: %d lines written, want exactly 1 — a progress line was committed "+
				"AFTER the stop signal was set and while stop() was still running. The pass is "+
				"over and the heartbeat reported on it anyway (#6134/#6047); the in-loop re-check "+
				"is what prevents this, and the barrier alone does not.", i, got)
		}
	}
}

// gatedWriter blocks the FIRST write until release is closed, so a test can pin
// the heartbeat goroutine at a known point in its loop. Later writes pass
// straight through and are counted — they are the ones that must not happen.
type gatedWriter struct {
	mu      sync.Mutex
	writes  int
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *gatedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.writes++
	first := w.writes == 1
	w.mu.Unlock()
	if first {
		w.once.Do(func() { close(w.entered) })
		<-w.release
	}
	return len(p), nil
}

func (w *gatedWriter) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writes
}
