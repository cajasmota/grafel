package sched

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// #5954 follow-up — the FOREGROUND BARGE.
//
// The #5993 stage gate reads s.inflight for its SHARED (index) side, and
// s.inflight only tracks SCHEDULER-DISPATCHED index jobs. `grafel reset` and
// the rebuild RPC index through cmd/grafel's makeDaemonRebuildFunc → Index(...)
// DIRECTLY, never touching Enqueue, so during a rebuild len(s.inflight)==0 and
// the gate believed the machine idle: a background group-algo pass started
// alongside a multi-GB foreground index (measured peak 4258 → 5430 MB, with
// `index child 2979 MB + group-algo 1618 MB` co-resident at the peak instant).
//
// The barge closes that hole WITHOUT adding latency to the user-facing path: a
// foreground rebuild REGISTERS (never waits), and background exclusive stages
// see the registration and defer exactly as they defer for each other.

// bargeHeld reports how many foreground barge holds are currently registered.
// Reads under the scheduler lock so the race detector is satisfied.
func bargeHeld(s *Scheduler) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.stageBarge)
}

// stageDeferredFor reports whether `name` is currently recorded as deferred by
// the gate. Polling this instead of sleeping makes "the pass reached the gate
// and was turned away" an observed STATE rather than a timing assumption.
func (s *Scheduler) stageDeferredFor(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.stageDeferSince[name]
	return ok
}

// newBargeTestScheduler builds a scheduler whose only heavy stage is a
// group-algo pass we can count. mutate lets a test tweak the config.
func newBargeTestScheduler(t *testing.T, runs *atomic.Int32, mutate func(*Config)) *Scheduler {
	t.Helper()
	cfg := Config{
		Workers:           2,
		LinkDebounce:      time.Hour, // never chain; we arm the algo pass by hand
		GroupAlgoDebounce: 5 * time.Millisecond,
		GroupAlgoMaxWait:  time.Hour,
		StageGateRetry:    5 * time.Millisecond,
		StageGateMaxDefer: time.Hour, // starvation guard not exercised here
		StageGateHoldMax:  time.Hour,
		Index:             func(_ context.Context, _ string, _ string) error { return nil },
		Links:             func(_ context.Context, _ string) error { return nil },
		GroupAlgo: func(_ context.Context, _ string) error {
			runs.Add(1)
			return nil
		},
		GroupsForRepo:      func(_ string) []string { return []string{"acme"} },
		MemReleaseDisabled: true,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	return New(cfg)
}

// TestStageGateBarge_GroupAlgoDefersWhileRebuildBarges is the core regression:
// a background group-algo pass whose timer fires while a FOREGROUND rebuild
// holds the barge must DEFER, and must run once the rebuild releases. This is
// the exact instant the measurement caught (`index child + group-algo`
// co-resident) — the scheduler-side gate could not see it because a rebuild
// never populates s.inflight.
func TestStageGateBarge_GroupAlgoDefersWhileRebuildBarges(t *testing.T) {
	var runs atomic.Int32
	s := newBargeTestScheduler(t, &runs, nil)
	s.Start()
	defer s.Stop()

	release := s.bargeForeground("rebuild:acme")
	if bargeHeld(s) != 1 {
		t.Fatalf("bargeForeground did not register a hold: stageBarge=%d", bargeHeld(s))
	}

	s.scheduleGroupAlgo("acme")

	// Give the 5ms debounce and many 5ms retry cycles a chance to fire.
	waitFor(t, time.Second, func() bool { return s.stageDeferredFor("group-algo:acme") })
	time.Sleep(50 * time.Millisecond)
	if n := runs.Load(); n != 0 {
		t.Fatalf("group-algo ran %d time(s) while a foreground rebuild held the barge; it must defer", n)
	}

	release()
	if bargeHeld(s) != 0 {
		t.Fatalf("release() left %d barge hold(s) registered", bargeHeld(s))
	}
	waitFor(t, 3*time.Second, func() bool { return runs.Load() >= 1 })
}

// TestStageGateBarge_NeverDefersEvenWhenBackgroundStageHolds pins constraint 1:
// the foreground path must NEVER wait. Even with an exclusive background stage
// mid-flight holding the token, bargeForeground returns immediately and the
// barge coexists with that one holder — the background stage is NOT cancelled
// (that would throw away minutes of work the gate has no handle to resume).
func TestStageGateBarge_NeverDefersEvenWhenBackgroundStageHolds(t *testing.T) {
	var runs atomic.Int32
	algoEntered := make(chan struct{}, 1)
	releaseAlgo := make(chan struct{})
	var once sync.Once
	letAlgoFinish := func() { once.Do(func() { close(releaseAlgo) }) }

	s := newBargeTestScheduler(t, &runs, func(c *Config) {
		c.GroupAlgo = func(_ context.Context, _ string) error {
			runs.Add(1)
			select {
			case algoEntered <- struct{}{}:
			default:
			}
			<-releaseAlgo
			return nil
		}
	})
	s.Start()
	defer func() { letAlgoFinish(); s.Stop() }()

	s.scheduleGroupAlgo("acme")
	<-algoEntered

	// The token is definitively held by the background stage right now.
	s.mu.Lock()
	holder := s.stageHolder
	s.mu.Unlock()
	if holder != "group-algo:acme" {
		t.Fatalf("stageHolder = %q, want %q — precondition for the barge test", holder, "group-algo:acme")
	}

	// Barging must return without waiting for that holder.
	done := make(chan func(), 1)
	go func() { done <- s.bargeForeground("rebuild:acme") }()
	var release func()
	select {
	case release = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("bargeForeground BLOCKED while a background stage held the token; the foreground path must never wait")
	}
	defer release()

	// The background holder keeps running — the barge does not cancel it.
	s.mu.Lock()
	holder = s.stageHolder
	nBarge := len(s.stageBarge)
	s.mu.Unlock()
	if holder != "group-algo:acme" {
		t.Fatalf("stageHolder = %q after a barge; the barge must NOT evict or cancel the in-flight background stage", holder)
	}
	if nBarge != 1 {
		t.Fatalf("stageBarge = %d, want 1 — the barge must register even when the token is held", nBarge)
	}
}

// TestStageGateBarge_ReleasedOnPanicAndIsIdempotent pins the no-wedge
// constraint at the release-closure level: the closure is installed with
// `defer` at the rebuild call site, so it fires on normal return, on error, on
// cancellation and on panic. A double release (panic-recovery defer plus the
// normal one) must not corrupt the ledger or drop a DIFFERENT rebuild's hold.
func TestStageGateBarge_ReleasedOnPanicAndIsIdempotent(t *testing.T) {
	var runs atomic.Int32
	s := newBargeTestScheduler(t, &runs, nil)
	s.Start()
	defer s.Stop()

	// A second, unrelated barge that must survive the first one's release.
	other := s.bargeForeground("rebuild:other")
	defer other()

	func() {
		defer func() { _ = recover() }()
		release := s.bargeForeground("rebuild:acme")
		defer release()
		defer release() // double release — must be a no-op, not a corruption
		panic("extractor exploded mid-rebuild")
	}()

	if got := bargeHeld(s); got != 1 {
		t.Fatalf("stageBarge = %d after a panicking rebuild released; want 1 (only the unrelated hold)", got)
	}
}

// TestStageGateBarge_NotSubjectToStageGateHoldMax is the hold-max resolution.
// A from-scratch rebuild of the current corpus takes 10–12 minutes, right at
// StageGateHoldMax (15m). If the barge were subject to that forfeit it would
// silently expire itself on exactly the LONGEST rebuilds — the ones with the
// biggest resident graph — and reintroduce the overlap it exists to remove.
// So the barge carries its own, much longer, independently tunable backstop.
//
// StageGateHoldMax is set to 1ms here: an exclusive holder would be forfeited
// almost instantly. The barge must not be.
func TestStageGateBarge_NotSubjectToStageGateHoldMax(t *testing.T) {
	var runs atomic.Int32
	s := newBargeTestScheduler(t, &runs, func(c *Config) {
		c.StageGateHoldMax = time.Millisecond
		c.StageGateBargeMax = time.Hour
	})
	s.Start()
	defer s.Stop()

	release := s.bargeForeground("rebuild:acme")
	defer release()

	s.scheduleGroupAlgo("acme")
	waitFor(t, time.Second, func() bool { return s.stageDeferredFor("group-algo:acme") })
	time.Sleep(60 * time.Millisecond) // >> StageGateHoldMax
	if n := runs.Load(); n != 0 {
		t.Fatalf("group-algo ran %d time(s): the barge was forfeited by StageGateHoldMax (%s). "+
			"A self-forfeiting barge is worse than no barge — it silently reintroduces overlap on the longest rebuilds",
			n, time.Millisecond)
	}
	if bargeHeld(s) != 1 {
		t.Fatalf("stageBarge = %d; the barge hold must survive StageGateHoldMax", bargeHeld(s))
	}
}

// TestStageGateBarge_ExpiresAfterStageGateBargeMax is the other half of the
// hold-max resolution: the barge is not immortal. If a rebuild goroutine were
// ever to vanish without running its deferred release, the backstop reclaims
// the hold so background stages resume. It is a leak backstop, not a
// scheduling parameter — hence the hour-scale default.
func TestStageGateBarge_ExpiresAfterStageGateBargeMax(t *testing.T) {
	var runs atomic.Int32
	s := newBargeTestScheduler(t, &runs, func(c *Config) {
		c.StageGateBargeMax = 20 * time.Millisecond
	})
	s.Start()
	defer s.Stop()

	release := s.bargeForeground("rebuild:acme")
	defer release() // late release must be harmless after the reap

	s.scheduleGroupAlgo("acme")
	waitFor(t, 3*time.Second, func() bool { return runs.Load() >= 1 })
	if bargeHeld(s) != 0 {
		t.Fatalf("stageBarge = %d after StageGateBargeMax elapsed; the backstop must reclaim it", bargeHeld(s))
	}
}

// TestStageGateBarge_DisabledGateDisablesTheBarge pins constraint 4:
// GRAFEL_STAGE_GATE=0 (Config.StageGateDisabled) must turn EVERYTHING off,
// including the barge — otherwise the escape hatch no longer restores the
// pre-gate baseline the wall-clock cost is measured against.
func TestStageGateBarge_DisabledGateDisablesTheBarge(t *testing.T) {
	var runs atomic.Int32
	s := newBargeTestScheduler(t, &runs, func(c *Config) { c.StageGateDisabled = true })
	s.Start()
	defer s.Stop()

	release := s.bargeForeground("rebuild:acme")
	defer release()
	if got := bargeHeld(s); got != 0 {
		t.Fatalf("stageBarge = %d with StageGateDisabled; the disable switch must cover the barge too", got)
	}

	s.scheduleGroupAlgo("acme")
	waitFor(t, 3*time.Second, func() bool { return runs.Load() >= 1 })
}

// TestStageGateBarge_ProcessBridgeFollowsSchedulerLifetime pins the plumbing
// cmd/grafel depends on: the rebuild RPC runs in a package with no scheduler
// handle, so the scheduler publishes its barge through a process-global bridge
// on Start() and withdraws it on Stop(). Before Start and after Stop the
// package-level BargeForeground must be an inert no-op — never a nil-deref.
func TestStageGateBarge_ProcessBridgeFollowsSchedulerLifetime(t *testing.T) {
	var runs atomic.Int32
	s := newBargeTestScheduler(t, &runs, nil)

	// Unstarted / no scheduler registered: inert, and safe to call+release.
	BargeForeground("rebuild:nobody")()

	s.Start()
	release := BargeForeground("rebuild:acme")
	if got := bargeHeld(s); got != 1 {
		t.Fatalf("package-level BargeForeground did not reach the started scheduler: stageBarge=%d", got)
	}
	release()
	if got := bargeHeld(s); got != 0 {
		t.Fatalf("stageBarge = %d after release via the process bridge", got)
	}

	s.Stop()
	// Withdrawn on Stop: still callable, still inert.
	BargeForeground("rebuild:after-stop")()
	if got := bargeHeld(s); got != 0 {
		t.Fatalf("stageBarge = %d after Stop; the bridge must be withdrawn", got)
	}
}

// TestStageGateBarge_BridgeWithdrawIsIdentityChecked pins the identity check in
// withdrawBargeBridge. Stop() withdraws the bridge, but a scheduler must only
// ever unpublish ITSELF: an older scheduler stopping AFTER a newer one has
// published must not tear the newer one's bridge down, or every subsequent
// rebuild silently barges into the void and the gate is back to reading an idle
// machine — the precise failure this slice exists to fix, reintroduced by a
// shutdown ordering accident.
func TestStageGateBarge_BridgeWithdrawIsIdentityChecked(t *testing.T) {
	var runs1, runs2 atomic.Int32
	s1 := newBargeTestScheduler(t, &runs1, nil)
	s2 := newBargeTestScheduler(t, &runs2, nil)

	s1.Start()
	s2.Start() // s2 is now the published bridge
	defer s2.Stop()

	// s1 stops LAST. Its withdraw must be a no-op, because it is not the
	// currently published scheduler.
	s1.Stop()

	release := BargeForeground("rebuild:acme")
	defer release()
	if got := bargeHeld(s2); got != 1 {
		t.Fatalf("stageBarge on the LIVE scheduler = %d, want 1: an older scheduler's Stop() tore down "+
			"the newer scheduler's barge bridge — every rebuild after it would register nowhere", got)
	}
	if got := bargeHeld(s1); got != 0 {
		t.Fatalf("stageBarge on the STOPPED scheduler = %d, want 0", got)
	}
}

// TestStageGateBarge_DeferralUnderBargeDoesNotRaiseDrainBarrier pins the
// escalation-suppression half of the change, and the "clock keeps running"
// claim that makes suppressing it safe.
//
// A drain barrier means "stop admitting index jobs so the in-flight batch
// drains, then the starved stage can acquire". Against a BARGE that mechanism
// is inert: the blocker is a rebuild the scheduler does not dispatch, so
// draining s.inflight cannot end it. Raising one anyway would hold scheduler
// index dispatch and log a starvation WARN every StageGateDrainMax for the
// entire 10–12 minutes of a rebuild, buying nothing.
//
// The deferral CLOCK is deliberately NOT reset, so the starvation budget is
// preserved rather than forgiven: the moment the barge lifts, an already-starved
// stage that is still blocked (here, by an index job) raises the barrier on its
// very next retry. Both halves are asserted.
func TestStageGateBarge_DeferralUnderBargeDoesNotRaiseDrainBarrier(t *testing.T) {
	var runs atomic.Int32
	indexEntered := make(chan struct{}, 1)
	releaseIndex := make(chan struct{})
	var once sync.Once
	letIndexFinish := func() { once.Do(func() { close(releaseIndex) }) }

	s := newBargeTestScheduler(t, &runs, func(c *Config) {
		// Zero-length starvation budget: EVERY deferral is instantly eligible
		// to escalate, so a surviving escalation is caught on the first retry
		// rather than depending on timing.
		c.StageGateMaxDefer = time.Nanosecond
		c.StageGateDrainMax = time.Hour
		c.Index = func(_ context.Context, _ string, _ string) error {
			select {
			case indexEntered <- struct{}{}:
			default:
			}
			<-releaseIndex
			return nil
		}
	})
	s.Start()
	defer func() { letIndexFinish(); s.Stop() }()

	release := s.bargeForeground("rebuild:acme")

	s.scheduleGroupAlgo("acme")
	waitFor(t, time.Second, func() bool { return s.stageDeferredFor("group-algo:acme") })

	// Several retry cycles at StageGateRetry=5ms, every one of them past
	// StageGateMaxDefer. No barrier may be raised while the barge is the blocker.
	time.Sleep(60 * time.Millisecond)
	s.mu.Lock()
	drainFor := s.stageDrainFor
	_, stillDeferred := s.stageDeferSince["group-algo:acme"]
	s.mu.Unlock()
	if drainFor != "" {
		t.Fatalf("drain barrier raised for %q while a foreground barge was the blocker: "+
			"draining s.inflight cannot end a rebuild, so the barrier holds index dispatch and warns for "+
			"the whole rebuild while buying nothing", drainFor)
	}
	if !stillDeferred {
		t.Fatal("group-algo stopped being deferred while the barge was held")
	}

	// Now start an index job and lift the barge. The stage is still blocked —
	// this time by index churn, which a barrier CAN resolve — and its deferral
	// clock was never reset, so the barrier must go up on the next retry.
	s.Enqueue("/slow-repo")
	<-indexEntered
	release()

	waitFor(t, 3*time.Second, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.stageDrainFor == "group-algo:acme"
	})
}
