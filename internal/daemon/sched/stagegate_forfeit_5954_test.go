package sched

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// #5954 follow-up — the forfeit path.
//
// The gate shipped with a 15m hold-max against a group-algo pass that MEASURES
// 32–53 minutes on the real corpus, so the forfeit fired on every single run and
// the gate degraded to no-gate on exactly the corpora it exists for. Worse, the
// forfeit released the token WITHOUT stopping the forfeited stage, so the
// successor became co-resident with a still-live child — the precise condition
// the gate prevents.
//
// These tests pin both halves: the default cannot fire on legitimate work, and a
// forfeit never re-opens the gate while the forfeited stage is still resident.

// TestStageGateHoldMaxDefault_ExceedsMeasuredPassWithGrowthHeadroom asserts the
// default against the MEASUREMENT, not against the implementation's own value.
//
// Asserting `stageGateHoldMaxDefault > someRoundNumber` is how the 15m default
// survived review in the first place. The measurement is the independent
// authority here: stageGateMeasuredPassWorst carries the observed worst-case
// legitimate pass, and the default must clear it by the corpus-growth factor the
// gate is expected to absorb.
func TestStageGateHoldMaxDefault_ExceedsMeasuredPassWithGrowthHeadroom(t *testing.T) {
	// The measurement itself, from the live daemon's own logs (2026-07):
	//   held_for=53m27s  — a LEGITIMATE group-algo pass, forfeited at hold_max=15m.
	const observedWorstPass = 53*time.Minute + 27*time.Second

	if stageGateMeasuredPassWorst < observedWorstPass {
		t.Fatalf("stageGateMeasuredPassWorst (%v) is below the observed worst legitimate pass (%v) — "+
			"the constant must carry the measurement, not a smaller convenient number",
			stageGateMeasuredPassWorst, observedWorstPass)
	}
	want := time.Duration(stageGateHoldMaxGrowthFactor) * stageGateMeasuredPassWorst
	if stageGateHoldMaxDefault < want {
		t.Fatalf("stageGateHoldMaxDefault=%v cannot absorb a %dx corpus: a legitimate pass measured %v, "+
			"so the default must be >= %v or the gate forfeits every run and degrades to no-gate",
			stageGateHoldMaxDefault, stageGateHoldMaxGrowthFactor, stageGateMeasuredPassWorst, want)
	}
	// And New() must actually install it.
	s := New(Config{Workers: 1, Index: func(context.Context, string, string) error { return nil }})
	if s.cfg.StageGateHoldMax != stageGateHoldMaxDefault {
		t.Fatalf("New did not install the hold-max default: want %v got %v",
			stageGateHoldMaxDefault, s.cfg.StageGateHoldMax)
	}
	if s.cfg.StageGateForfeitGrace != stageGateForfeitGraceDefault {
		t.Fatalf("New did not install the forfeit-grace default: want %v got %v",
			stageGateForfeitGraceDefault, s.cfg.StageGateForfeitGrace)
	}
}

// forfeitTestScheduler builds a started scheduler whose group-algo pass wedges
// until either its context is cancelled (respectCtx) or the returned unwedge
// func is called.
type forfeitHarness struct {
	s         *Scheduler
	indexRuns atomic.Int32
	entered   chan struct{}
	unwedge   func()
	cancelled atomic.Bool
}

func newForfeitHarness(t *testing.T, holdMax, grace time.Duration, respectCtx bool) *forfeitHarness {
	t.Helper()
	h := &forfeitHarness{entered: make(chan struct{}, 1)}
	release := make(chan struct{})
	var once sync.Once
	h.unwedge = func() { once.Do(func() { close(release) }) }

	h.s = New(Config{
		Workers:               1,
		LinkDebounce:          time.Hour,
		GroupAlgoDebounce:     5 * time.Millisecond,
		GroupAlgoMaxWait:      time.Hour,
		StageGateRetry:        5 * time.Millisecond,
		StageGateMaxDefer:     time.Hour,
		StageGateHoldMax:      holdMax,
		StageGateForfeitGrace: grace,
		Index: func(_ context.Context, _ string, _ string) error {
			h.indexRuns.Add(1)
			return nil
		},
		Links: func(_ context.Context, _ string) error { return nil },
		GroupAlgo: func(ctx context.Context, _ string) error {
			select {
			case h.entered <- struct{}{}:
			default:
			}
			if respectCtx {
				select {
				case <-ctx.Done():
					h.cancelled.Store(true)
				case <-release:
				}
				return ctx.Err()
			}
			<-release // wedged: ignores cancellation entirely
			return nil
		},
		GroupsForRepo:      func(_ string) []string { return nil },
		MemReleaseDisabled: true,
	})
	h.s.Start()
	t.Cleanup(func() { h.unwedge(); h.s.Stop() })
	return h
}

func (h *forfeitHarness) reap(now time.Time) {
	h.s.mu.Lock()
	h.s.reapStageLocked(now)
	h.s.mu.Unlock()
}

func (h *forfeitHarness) holder() string {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	return h.s.stageHolder
}

func hasEvent(s *Scheduler, kind string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.recentLog {
		if e.Kind == kind {
			return true
		}
	}
	return false
}

// TestStageGate_ForfeitDoesNotAdmitWhileForfeitedStageIsAlive is the core
// regression. The pre-fix forfeit cleared stageHolder and resumed admitting
// while the forfeited child was still resident, so links and a still-live
// group-algo became co-resident — the exact failure the gate exists to prevent,
// observed on the real corpus at held_for=53m27s.
func TestStageGate_ForfeitDoesNotAdmitWhileForfeitedStageIsAlive(t *testing.T) {
	h := newForfeitHarness(t, 20*time.Millisecond, time.Hour, false /* ignores ctx */)

	h.s.scheduleGroupAlgo("acme")
	<-h.entered

	// Drive the hold-max past its bound and reap.
	time.Sleep(40 * time.Millisecond)
	h.reap(time.Now())

	if !hasEvent(h.s, "stage_gate_forfeit") {
		t.Fatal("no stage_gate_forfeit event — the forfeit must stay loud")
	}
	if got := h.s.StageGateState().Forfeits; got != 1 {
		t.Fatalf("forfeit counter must be observable in the gate snapshot: want 1 got %d", got)
	}
	if got := h.s.Snapshot().StageForfeits; got != 1 {
		t.Fatalf("forfeit counter must be observable in Snapshot: want 1 got %d", got)
	}

	// (a) The gate stays CLOSED to a second exclusive stage while the forfeited
	// one is still alive.
	h.s.mu.Lock()
	acquired := h.s.tryAcquireStageLocked("links:acme", time.Now())
	holder := h.s.stageHolder
	h.s.mu.Unlock()
	if acquired {
		t.Fatal("a second exclusive stage acquired the token while the forfeited stage was still resident — " +
			"this is the co-residency the gate exists to prevent")
	}
	if holder != "group-algo:acme" {
		t.Fatalf("forfeited-but-alive holder must remain visible; got %q", holder)
	}

	// (b) Index dispatch is likewise still held.
	h.s.Enqueue("/repo-a")
	time.Sleep(200 * time.Millisecond)
	if n := h.indexRuns.Load(); n != 0 {
		t.Fatalf("index dispatch resumed while the forfeited stage was still resident (%d run(s))", n)
	}
}

// TestStageGate_ForfeitDoesNotCancelTheStageOrRecomputeIt: the forfeit must not
// destroy work. Releasing the token re-ran the forfeited pass (releaseStage →
// links acquires → runLinks' success path calls scheduleGroupAlgo → a FRESH
// group-algo), and cancelling it discards up to 53 minutes outright. Either way
// a 32–53 minute pass is paid twice. So a forfeited stage is left alone to
// finish, and the gate reopens on its own release.
func TestStageGate_ForfeitDoesNotCancelTheStageOrRecomputeIt(t *testing.T) {
	h := newForfeitHarness(t, 20*time.Millisecond, time.Hour, true /* respects ctx */)

	h.s.scheduleGroupAlgo("acme")
	<-h.entered

	time.Sleep(40 * time.Millisecond)
	h.reap(time.Now())

	// The stage must NOT have been cancelled: it is still running, still
	// holding, and its work is intact.
	time.Sleep(150 * time.Millisecond)
	if got := h.holder(); got != "group-algo:acme" {
		t.Fatalf("a forfeited stage was cancelled or released; holder=%q, want it left alone to finish", got)
	}
	if h.cancelled.Load() {
		t.Fatal("the forfeit cancelled the stage — up to 53 minutes of completed work discarded, " +
			"and the pass then re-armed and recomputed from scratch")
	}

	// It finishes on its own; the gate reopens then, and only then.
	h.unwedge()
	waitFor(t, 3*time.Second, func() bool { return h.holder() == "" })
	h.s.Enqueue("/repo-a")
	waitFor(t, 3*time.Second, func() bool { return h.indexRuns.Load() >= 1 })
}

// TestStageGate_ForfeitGraceIsBoundedAndLateReturnerCannotStealToken: the
// escape hatch. A stage that survives cancellation (an uninterruptible child)
// must not wedge the daemon forever — after StageGateForfeitGrace the token is
// abandoned, loudly, and index dispatch resumes. This is the ONE reachable
// condition under which the gate degrades to no-gate, so it gets its own
// event kind rather than sharing stage_gate_forfeit.
func TestStageGate_ForfeitGraceIsBoundedAndLateReturnerCannotStealToken(t *testing.T) {
	h := newForfeitHarness(t, 20*time.Millisecond, 30*time.Millisecond, false /* ignores ctx */)

	h.s.scheduleGroupAlgo("acme")
	<-h.entered

	// Past hold-max: forfeited, cancelled, still held.
	time.Sleep(40 * time.Millisecond)
	h.reap(time.Now())
	if h.holder() == "" {
		t.Fatal("token released before the forfeit grace elapsed")
	}
	// Past hold-max + grace: abandoned.
	time.Sleep(60 * time.Millisecond)
	h.reap(time.Now())

	if h.holder() != "" {
		t.Fatalf("forfeit grace did not bound the wedge — holder still %q", h.holder())
	}
	if !hasEvent(h.s, "stage_gate_forfeit_abandoned") {
		t.Fatal("abandoning a forfeited stage must emit its own loud event")
	}

	// Index dispatch resumes.
	h.s.Enqueue("/repo-a")
	waitFor(t, 3*time.Second, func() bool { return h.indexRuns.Load() >= 1 })

	// A successor takes the token...
	waitFor(t, 3*time.Second, func() bool {
		h.s.mu.Lock()
		defer h.s.mu.Unlock()
		return len(h.s.inflight) == 0
	})
	h.s.mu.Lock()
	ok := h.s.tryAcquireStageLocked("links:acme", time.Now())
	h.s.mu.Unlock()
	if !ok {
		t.Fatal("an abandoned token must be re-acquirable by a new stage")
	}
	// ...and the abandoned pass returning LATE must not steal it back.
	h.unwedge()
	time.Sleep(100 * time.Millisecond)
	if got := h.holder(); got != "links:acme" {
		t.Fatalf("a late-returning abandoned stage cleared the new holder's token (holder=%q)", got)
	}
	h.s.releaseStage("links:acme")
}

// TestStageGate_ForfeitGraceCancelsTheStage: reclaiming the token beside a
// child nobody told to die is the co-residency the gate exists to prevent, so
// the grace expiry must CANCEL first (SIGKILLing a group-algo subprocess) and
// only then release. Without this the grace would be the old broken forfeit
// with a longer fuse.
func TestStageGate_ForfeitGraceCancelsTheStage(t *testing.T) {
	h := newForfeitHarness(t, 20*time.Millisecond, 30*time.Millisecond, true /* respects ctx */)

	h.s.scheduleGroupAlgo("acme")
	<-h.entered

	time.Sleep(40 * time.Millisecond)
	h.reap(time.Now()) // tier 1: forfeit, no cancel
	if h.cancelled.Load() {
		t.Fatal("the forfeit itself cancelled the stage; only the grace expiry may")
	}
	time.Sleep(60 * time.Millisecond)
	h.reap(time.Now()) // tier 2: cancel, then reclaim

	waitFor(t, 3*time.Second, func() bool { return h.cancelled.Load() })
	if got := h.holder(); got != "" {
		t.Fatalf("token not reclaimed after the grace; holder=%q", got)
	}
}

// TestStageGate_ForfeitLogMessageSaysWhatItDid: a forfeit means the epic's
// central mechanism just intervened. The message must name the remedy, not read
// as a shrug.
func TestStageGate_ForfeitLogMessageSaysWhatItDid(t *testing.T) {
	h := newForfeitHarness(t, 20*time.Millisecond, time.Hour, false)
	h.s.scheduleGroupAlgo("acme")
	<-h.entered
	time.Sleep(40 * time.Millisecond)
	h.reap(time.Now())

	h.s.mu.Lock()
	var msg string
	for _, e := range h.s.recentLog {
		if e.Kind == "stage_gate_forfeit" {
			msg = e.Msg
		}
	}
	h.s.mu.Unlock()
	if msg == "" {
		t.Fatal("no stage_gate_forfeit event recorded")
	}
	// The message must say what the gate DID (held the line), not read as a
	// shrug that leaves the reader guessing whether the gate is still binding.
	for _, want := range []string{"FORFEITED", "CLOSED", "not"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("forfeit message must be unambiguous about the gate's state (missing %q): %q", want, msg)
		}
	}
}

// TestMemRelease_PendingStageDoesNotBlockRelease: busyLocked conflated "a stage
// is WAITING" with "heavy work is in flight", so a permanently-deferred stage
// starved FreeOSMemory — measured: 8+ hours without a single heap release, and
// an idle floor that was mostly an un-returned watermark. A stage that is merely
// pending is not doing work.
func TestMemRelease_PendingStageDoesNotBlockRelease(t *testing.T) {
	cases := []struct {
		name  string
		setup func(s *Scheduler)
	}{
		{"deferred stage", func(s *Scheduler) { s.stageDeferSince["group-algo:acme"] = time.Now() }},
		{"drain barrier", func(s *Scheduler) { s.stageDrainFor = "group-algo:acme" }},
		{"pending group-algo", func(s *Scheduler) { s.groupAlgoPending["acme"] = true }},
		{"pending links", func(s *Scheduler) { s.linkPending["acme"] = true }},
		{"queued job", func(s *Scheduler) { s.pendingQ = append(s.pendingQ, "/repo-a") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var freed atomic.Int32
			s := newMemTestScheduler(10*time.Second, func() { freed.Add(1) })
			s.mu.Lock()
			tc.setup(s)
			s.mu.Unlock()

			base := time.Now()
			s.maybeReleaseMemory(base)
			s.maybeReleaseMemory(base.Add(11 * time.Second))
			if got := freed.Load(); got != 1 {
				t.Fatalf("a merely-pending stage blocked the heap release; want 1 got %d", got)
			}
		})
	}
}

// TestMemRelease_InFlightWorkStillBlocksRelease is the other half: releasing the
// heap underneath genuinely in-flight work is what busyLocked was protecting
// against, and splitting it must not regress that.
func TestMemRelease_InFlightWorkStillBlocksRelease(t *testing.T) {
	cases := []struct {
		name  string
		setup func(s *Scheduler)
	}{
		{"index in flight", func(s *Scheduler) { s.inflight["/repo"] = 1 }},
		{"exclusive stage holding", func(s *Scheduler) { s.stageHolder = "group-algo:acme" }},
		{"foreground barge", func(s *Scheduler) {
			s.stageBarge = map[int64]bargeHold{1: {name: "rebuild:acme", since: time.Now()}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var freed atomic.Int32
			s := newMemTestScheduler(10*time.Second, func() { freed.Add(1) })
			s.mu.Lock()
			tc.setup(s)
			s.mu.Unlock()

			base := time.Now()
			s.maybeReleaseMemory(base)
			s.maybeReleaseMemory(base.Add(11 * time.Second))
			s.maybeReleaseMemory(base.Add(60 * time.Second))
			if got := freed.Load(); got != 0 {
				t.Fatalf("released the heap under in-flight work; want 0 got %d", got)
			}
		})
	}
}
