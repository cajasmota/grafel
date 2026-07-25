package sched

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/indexstate"
)

var errStageTest = errors.New("stage failed (test)")

// #5954 heavy write-stage gate.
//
// The daemon has three heavy write stages — the per-repo index batch, the
// cross-repo link pass, and the group-algorithm pass. Each materialises a large
// slice of the group graph. Historically nothing prevented them from running
// CONCURRENTLY (the 180s group-algo debounce is shorter than a full reindex, so
// overlap was the normal case), and the whole-machine RSS peak was the SUM of
// two co-resident copies. These tests pin the invariant: at most ONE heavy
// stage executes at a time, deferral is always eventually resolved, and the
// gate is released on every exit path.

// stageProbe records, for every heavy-stage callback, whether a stage of the
// OTHER class was executing at the same moment. Index jobs may run concurrently
// with each other (worker pool + RSS admission bound that); what must never
// happen is an index job overlapping an exclusive stage, or two exclusive
// stages overlapping.
type stageProbe struct {
	indexActive     atomic.Int32
	exclusiveActive atomic.Int32
	violations      atomic.Int32

	mu    sync.Mutex
	order []string
}

func (p *stageProbe) record(ev string) {
	p.mu.Lock()
	p.order = append(p.order, ev)
	p.mu.Unlock()
}

func (p *stageProbe) events() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.order))
	copy(out, p.order)
	return out
}

// enterIndex/leaveIndex bracket an index callback.
func (p *stageProbe) enterIndex(name string) {
	if p.exclusiveActive.Load() > 0 {
		p.violations.Add(1)
	}
	p.indexActive.Add(1)
	p.record("index_start:" + name)
}

func (p *stageProbe) leaveIndex(name string) {
	p.indexActive.Add(-1)
	p.record("index_done:" + name)
}

// enterExclusive/leaveExclusive bracket a link or group-algo callback.
func (p *stageProbe) enterExclusive(name string) {
	if p.indexActive.Load() > 0 || p.exclusiveActive.Load() > 0 {
		p.violations.Add(1)
	}
	p.exclusiveActive.Add(1)
	p.record("exclusive_start:" + name)
}

func (p *stageProbe) leaveExclusive(name string) {
	p.exclusiveActive.Add(-1)
	p.record("exclusive_done:" + name)
}

func (p *stageProbe) assertNoOverlap(t *testing.T) {
	t.Helper()
	if v := p.violations.Load(); v != 0 {
		t.Fatalf("heavy write stages overlapped %d time(s); event order: %v", v, p.events())
	}
}

// TestStageGate_GroupAlgoDefersWhileIndexRunning: an armed group-algo pass whose
// timer fires while a per-repo index is in flight must DEFER — it must not start
// until the index batch has drained.
func TestStageGate_GroupAlgoDefersWhileIndexRunning(t *testing.T) {
	var probe stageProbe
	release := make(chan struct{})
	indexEntered := make(chan struct{}, 1)
	var algoRuns atomic.Int32

	s := New(Config{
		Workers:           2,
		LinkDebounce:      time.Hour, // never chain; we arm the algo pass by hand
		GroupAlgoDebounce: 5 * time.Millisecond,
		GroupAlgoMaxWait:  time.Hour,
		StageGateRetry:    5 * time.Millisecond,
		StageGateMaxDefer: time.Hour, // starvation guard NOT exercised here
		Index: func(_ context.Context, repo string, _ string) error {
			probe.enterIndex(repo)
			defer probe.leaveIndex(repo)
			select {
			case indexEntered <- struct{}{}:
			default:
			}
			<-release
			return nil
		},
		Links: func(_ context.Context, _ string) error { return nil },
		GroupAlgo: func(_ context.Context, g string) error {
			probe.enterExclusive("algo:" + g)
			defer probe.leaveExclusive("algo:" + g)
			algoRuns.Add(1)
			return nil
		},
		GroupsForRepo:      func(_ string) []string { return []string{"acme"} },
		MemReleaseDisabled: true,
	})
	var releaseOnce sync.Once
	releaseIndex := func() { releaseOnce.Do(func() { close(release) }) }
	s.Start()
	defer func() { releaseIndex(); s.Stop() }()

	s.Enqueue("/slow-repo")
	<-indexEntered

	// Arm the group-algo pass while the index is still executing.
	s.scheduleGroupAlgo("acme")

	// Give the (5ms) debounce and several retry cycles a chance to fire.
	time.Sleep(80 * time.Millisecond)
	if n := algoRuns.Load(); n != 0 {
		t.Fatalf("group-algo ran %d time(s) while an index was in flight; it must defer", n)
	}

	// Release the index; the deferred pass must now run.
	releaseIndex()
	waitFor(t, 3*time.Second, func() bool { return algoRuns.Load() >= 1 })
	probe.assertNoOverlap(t)
}

// TestStageGate_IndexDefersWhileGroupAlgoRunning: the reciprocal direction. A
// newly enqueued repo must NOT be dispatched to a worker while an exclusive
// stage (group-algo) is executing.
func TestStageGate_IndexDefersWhileGroupAlgoRunning(t *testing.T) {
	var probe stageProbe
	algoEntered := make(chan struct{}, 1)
	releaseAlgo := make(chan struct{})
	var indexRuns atomic.Int32

	s := New(Config{
		Workers:           2,
		LinkDebounce:      time.Hour,
		GroupAlgoDebounce: 5 * time.Millisecond,
		GroupAlgoMaxWait:  time.Hour,
		StageGateRetry:    5 * time.Millisecond,
		StageGateMaxDefer: time.Hour,
		StageGateHoldMax:  time.Hour,
		Index: func(_ context.Context, repo string, _ string) error {
			probe.enterIndex(repo)
			defer probe.leaveIndex(repo)
			indexRuns.Add(1)
			return nil
		},
		Links: func(_ context.Context, _ string) error { return nil },
		GroupAlgo: func(_ context.Context, g string) error {
			probe.enterExclusive("algo:" + g)
			defer probe.leaveExclusive("algo:" + g)
			select {
			case algoEntered <- struct{}{}:
			default:
			}
			<-releaseAlgo
			return nil
		},
		GroupsForRepo:      func(_ string) []string { return []string{"acme"} },
		MemReleaseDisabled: true,
	})
	var releaseOnce sync.Once
	releaseAlgoFn := func() { releaseOnce.Do(func() { close(releaseAlgo) }) }
	s.Start()
	defer func() { releaseAlgoFn(); s.Stop() }()

	s.scheduleGroupAlgo("acme")
	<-algoEntered

	s.Enqueue("/repo-a")
	time.Sleep(80 * time.Millisecond)
	if n := indexRuns.Load(); n != 0 {
		t.Fatalf("index ran %d time(s) while a group-algo pass was in flight; it must defer", n)
	}

	releaseAlgoFn()
	waitFor(t, 3*time.Second, func() bool { return indexRuns.Load() >= 1 })
	probe.assertNoOverlap(t)
}

// TestStageGate_TwoLinkPassesDoNotOverlap: two distinct groups whose link
// debounce timers fire together must serialise — one runs, the other defers.
func TestStageGate_TwoLinkPassesDoNotOverlap(t *testing.T) {
	var probe stageProbe
	var linkRuns atomic.Int32
	gate := make(chan struct{})
	var once sync.Once

	s := New(Config{
		Workers:           2,
		LinkDebounce:      5 * time.Millisecond,
		GroupAlgoDebounce: time.Hour, // don't chain an algo pass into this test
		GroupAlgoMaxWait:  time.Hour,
		StageGateRetry:    5 * time.Millisecond,
		StageGateMaxDefer: time.Hour,
		StageGateHoldMax:  time.Hour,
		Index:             func(_ context.Context, _ string, _ string) error { return nil },
		Links: func(_ context.Context, g string) error {
			probe.enterExclusive("links:" + g)
			defer probe.leaveExclusive("links:" + g)
			linkRuns.Add(1)
			// The FIRST pass holds the gate long enough that the second group's
			// timer definitely fires underneath it.
			once.Do(func() { <-gate })
			return nil
		},
		GroupAlgo:          func(_ context.Context, _ string) error { return nil },
		GroupsForRepo:      func(_ string) []string { return nil },
		MemReleaseDisabled: true,
	})
	s.Start()
	defer func() { s.Stop() }()

	s.scheduleLinks("groupA")
	s.scheduleLinks("groupB")

	waitFor(t, 2*time.Second, func() bool { return probe.exclusiveActive.Load() == 1 })
	time.Sleep(60 * time.Millisecond)
	if n := probe.exclusiveActive.Load(); n != 1 {
		t.Fatalf("expected exactly 1 in-flight link pass while the gate is held, got %d", n)
	}
	close(gate)
	waitFor(t, 3*time.Second, func() bool { return linkRuns.Load() >= 2 })
	probe.assertNoOverlap(t)
}

// TestStageGate_DeferredStageRunsUnderSustainedChurn is the constraint-1 test:
// the gate takes precedence over the group-algo max-wait, so a busy daemon could
// defer the pass forever. The gate therefore carries its OWN bounded starvation
// guard: after StageGateMaxDefer of continuous deferral it stops admitting NEW
// index jobs, lets the in-flight batch drain, and then runs — WITHOUT ever
// overlapping. This test drives sustained index churn (there is never a natural
// idle gap) and asserts the pass still runs, with zero overlap.
func TestStageGate_DeferredStageRunsUnderSustainedChurn(t *testing.T) {
	var probe stageProbe
	var algoRuns atomic.Int32
	inflightAtAlgoEntry := make(chan int, 4)

	var s *Scheduler
	s = New(Config{
		Workers:           2,
		LinkDebounce:      time.Hour,
		GroupAlgoDebounce: 5 * time.Millisecond,
		GroupAlgoMaxWait:  time.Hour,
		StageGateRetry:    5 * time.Millisecond,
		StageGateMaxDefer: 120 * time.Millisecond,
		StageGateHoldMax:  time.Hour,
		Index: func(_ context.Context, repo string, _ string) error {
			probe.enterIndex(repo)
			defer probe.leaveIndex(repo)
			time.Sleep(10 * time.Millisecond)
			return nil
		},
		Links: func(_ context.Context, _ string) error { return nil },
		GroupAlgo: func(_ context.Context, g string) error {
			probe.enterExclusive("algo:" + g)
			defer probe.leaveExclusive("algo:" + g)
			s.mu.Lock()
			n := len(s.inflight)
			s.mu.Unlock()
			select {
			case inflightAtAlgoEntry <- n:
			default:
			}
			algoRuns.Add(1)
			return nil
		},
		GroupsForRepo:      func(_ string) []string { return nil },
		MemReleaseDisabled: true,
	})
	s.Start()

	// Sustained churn: keep re-enqueuing so the index batch is (almost) never
	// empty. This is the condition under which the pre-gate max-wait would fire
	// ANYWAY and the gate would be decorative.
	stopChurn := make(chan struct{})
	churnDone := make(chan struct{})
	go func() {
		defer close(churnDone)
		i := 0
		for {
			select {
			case <-stopChurn:
				return
			default:
			}
			s.Enqueue("/churn-" + itoa(int64(i%6)))
			i++
			time.Sleep(2 * time.Millisecond)
		}
	}()

	s.scheduleGroupAlgo("acme")

	waitFor(t, 5*time.Second, func() bool { return algoRuns.Load() >= 1 })
	close(stopChurn)
	<-churnDone
	s.Stop()

	if n := <-inflightAtAlgoEntry; n != 0 {
		t.Fatalf("group-algo started with %d index job(s) still in flight — the starvation guard must DRAIN, not overlap", n)
	}
	probe.assertNoOverlap(t)
}

// TestStageGate_ReleasedOnStageError: a stage whose callback returns an error
// must release the token, or every later heavy stage defers forever. Asserted
// via a SECOND exclusive stage acquiring afterwards — with StageGateHoldMax set
// to an hour, a leaked token would never be reaped inside the test window.
func TestStageGate_ReleasedOnStageError(t *testing.T) {
	algoEntered := make(chan struct{}, 1)
	var linkRuns atomic.Int32

	s := New(Config{
		Workers:           1,
		LinkDebounce:      5 * time.Millisecond,
		GroupAlgoDebounce: 5 * time.Millisecond,
		GroupAlgoMaxWait:  time.Hour,
		StageGateRetry:    5 * time.Millisecond,
		StageGateMaxDefer: time.Hour,
		StageGateHoldMax:  time.Hour,
		Index:             func(_ context.Context, _ string, _ string) error { return nil },
		Links: func(_ context.Context, _ string) error {
			linkRuns.Add(1)
			return nil
		},
		GroupAlgo: func(_ context.Context, _ string) error {
			select {
			case algoEntered <- struct{}{}:
			default:
			}
			return errStageTest
		},
		GroupsForRepo:      func(_ string) []string { return nil },
		MemReleaseDisabled: true,
	})
	s.Start()
	defer s.Stop()

	s.scheduleGroupAlgo("acme")
	<-algoEntered

	s.scheduleLinks("acme")
	waitFor(t, 3*time.Second, func() bool { return linkRuns.Load() >= 1 })
}

// TestStageGate_ReleasedOnCancel: a cancelled stage (group delete / shutdown)
// must release the gate too.
func TestStageGate_ReleasedOnCancel(t *testing.T) {
	entered := make(chan struct{}, 1)
	s := New(Config{
		Workers:           1,
		LinkDebounce:      time.Hour,
		GroupAlgoDebounce: 5 * time.Millisecond,
		GroupAlgoMaxWait:  time.Hour,
		StageGateRetry:    5 * time.Millisecond,
		StageGateMaxDefer: time.Hour,
		StageGateHoldMax:  time.Hour,
		Index:             func(_ context.Context, _ string, _ string) error { return nil },
		Links:             func(_ context.Context, _ string) error { return nil },
		GroupAlgo: func(ctx context.Context, _ string) error {
			select {
			case entered <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return ctx.Err()
		},
		GroupsForRepo:      func(_ string) []string { return nil },
		MemReleaseDisabled: true,
	})
	s.Start()
	defer s.Stop()

	s.scheduleGroupAlgo("acme")
	<-entered
	s.CancelGroup("acme")

	waitFor(t, 3*time.Second, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.stageHolder == ""
	})
}

// TestStageGate_ReleasedOnPanic drives the REAL production path: a GroupAlgo
// callback that panics unwinds through fireGroupAlgo, whose `defer
// s.releaseStage(name)` must still hand the token back. Asserted on
// fireGroupAlgo's own goroutine (the timer path would take the panic to the
// whole process, which is pre-existing behaviour and not what this pins).
//
// Mutation guard: removing the `defer s.releaseStage(name)` from fireGroupAlgo
// fails this test.
func TestStageGate_ReleasedOnPanic(t *testing.T) {
	s := New(Config{
		Workers:           1,
		LinkDebounce:      time.Hour,
		GroupAlgoDebounce: time.Hour,
		GroupAlgoMaxWait:  time.Hour,
		StageGateHoldMax:  time.Hour,
		Index:             func(_ context.Context, _ string, _ string) error { return nil },
		Links:             func(_ context.Context, _ string) error { return nil },
		GroupAlgo: func(_ context.Context, _ string) error {
			panic("group-algo exploded")
		},
		GroupsForRepo:      func(_ string) []string { return nil },
		MemReleaseDisabled: true,
	})

	panicked := make(chan any, 1)
	go func() {
		defer func() { panicked <- recover() }()
		s.fireGroupAlgo("acme")
	}()

	select {
	case r := <-panicked:
		if r == nil {
			t.Fatal("expected the GroupAlgo panic to propagate out of fireGroupAlgo")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("fireGroupAlgo never returned")
	}

	s.mu.Lock()
	holder := s.stageHolder
	s.mu.Unlock()
	if holder != "" {
		t.Fatalf("stage token still held by %q after a panicking group-algo pass", holder)
	}
}

// TestStageGate_HoldMaxForfeitsAndLateReturnerCannotStealToken pins the one
// branch where the no-overlap invariant is deliberately given up. A stage that
// holds the token past StageGateHoldMax is FORFEITED — index dispatch must
// resume (a permanently wedged daemon is worse than a transient overlap) — and
// when the wedged stage eventually returns, its releaseStage must NOT clear the
// token of whoever holds it by then.
func TestStageGate_HoldMaxForfeitsAndLateReturnerCannotStealToken(t *testing.T) {
	var indexRuns atomic.Int32
	releaseAlgo := make(chan struct{})
	algoEntered := make(chan struct{}, 1)

	s := New(Config{
		Workers:           1,
		LinkDebounce:      time.Hour,
		GroupAlgoDebounce: 5 * time.Millisecond,
		GroupAlgoMaxWait:  time.Hour,
		StageGateRetry:    5 * time.Millisecond,
		StageGateMaxDefer: time.Hour,
		StageGateHoldMax:  20 * time.Millisecond, // wedge threshold, for the test
		Index: func(_ context.Context, _ string, _ string) error {
			indexRuns.Add(1)
			return nil
		},
		Links: func(_ context.Context, _ string) error { return nil },
		GroupAlgo: func(_ context.Context, _ string) error {
			select {
			case algoEntered <- struct{}{}:
			default:
			}
			<-releaseAlgo // wedged: holds the token past StageGateHoldMax
			return nil
		},
		GroupsForRepo:      func(_ string) []string { return nil },
		MemReleaseDisabled: true,
	})
	var releaseOnce sync.Once
	releaseAlgoFn := func() { releaseOnce.Do(func() { close(releaseAlgo) }) }
	s.Start()
	defer func() { releaseAlgoFn(); s.Stop() }()

	s.scheduleGroupAlgo("acme")
	<-algoEntered

	// (a) Index dispatch must resume once the hold-max forfeits the token,
	// even though the wedged pass is still running.
	s.Enqueue("/repo-a")
	waitFor(t, 3*time.Second, func() bool { return indexRuns.Load() >= 1 })

	// (b) A NEW exclusive stage takes the token after the forfeit...
	waitFor(t, 3*time.Second, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.inflight) == 0
	})
	s.mu.Lock()
	if !s.tryAcquireStageLocked("links:acme", time.Now()) {
		s.mu.Unlock()
		t.Fatal("a forfeited token must be re-acquirable by a new stage")
	}
	s.mu.Unlock()

	// ...and the wedged pass returning LATE must not steal it back.
	releaseAlgoFn()
	time.Sleep(100 * time.Millisecond)

	s.mu.Lock()
	holder := s.stageHolder
	s.mu.Unlock()
	if holder != "links:acme" {
		t.Fatalf("a late-returning forfeited stage cleared the new holder's token (holder=%q, want %q)",
			holder, "links:acme")
	}
	s.releaseStage("links:acme")
}

// TestStageGate_DeferredGroupAlgoStaysVisibleToIndexState: constraint 3. An MCP
// consumer polling grafel_stats must not see the daemon go quiet while a heavy
// stage is merely DEFERRED — it would read a stale overlay believing the daemon
// is idle. The deferred case gets the same indexstate treatment as the running
// case.
func TestStageGate_DeferredGroupAlgoStaysVisibleToIndexState(t *testing.T) {
	s := New(Config{
		Workers:            1,
		LinkDebounce:       time.Hour,
		GroupAlgoDebounce:  time.Hour,
		GroupAlgoMaxWait:   time.Hour,
		StageGateRetry:     time.Hour, // no retry inside the test window
		StageGateMaxDefer:  time.Hour,
		StageGateHoldMax:   time.Hour,
		Index:              func(_ context.Context, _ string, _ string) error { return nil },
		Links:              func(_ context.Context, _ string) error { return nil },
		GroupAlgo:          func(_ context.Context, _ string) error { return nil },
		GroupsForRepo:      func(_ string) []string { return nil },
		MemReleaseDisabled: true,
	})

	before := indexstate.Get().GroupAlgoInFlight

	// indexstate is PROCESS-GLOBAL: a t.Fatal below would otherwise leak this
	// test's GroupAlgoBegin into every later test in the binary.
	t.Cleanup(func() {
		s.mu.Lock()
		delete(s.inflight, "/busy")
		s.markGroupAlgoDeferredLocked("acme", false)
		s.mu.Unlock()
	})

	// Simulate an index batch in flight so the fire attempt must defer.
	s.mu.Lock()
	s.inflight["/busy"] = 1
	s.mu.Unlock()

	s.fireGroupAlgo("acme")

	snap := indexstate.Get()
	if snap.GroupAlgoInFlight <= before {
		t.Fatalf("a DEFERRED group-algo pass must stay visible in indexstate (got %d, was %d)",
			snap.GroupAlgoInFlight, before)
	}
	if !snap.Busy {
		t.Fatal("indexstate must report Busy while a heavy stage is deferred")
	}

	s.mu.Lock()
	pending := s.groupAlgoPending["acme"]
	s.mu.Unlock()
	if !pending {
		t.Fatal("a deferred group-algo pass must remain PENDING (re-armed), not dropped")
	}

	// Clean up: cancelling the group must also drop the deferred-state marker so
	// indexstate does not leak a permanently-busy daemon.
	s.mu.Lock()
	delete(s.inflight, "/busy")
	s.mu.Unlock()
	s.CancelGroup("acme")

	waitFor(t, 2*time.Second, func() bool { return indexstate.Get().GroupAlgoInFlight <= before })
}

// TestStageGate_DisabledRestoresLegacyOverlap: the gate must be switchable off
// so the wall-clock cost can be measured against the pre-gate behaviour.
func TestStageGate_DisabledRestoresLegacyOverlap(t *testing.T) {
	algoEntered := make(chan struct{}, 1)
	releaseAlgo := make(chan struct{})
	var indexRuns atomic.Int32

	s := New(Config{
		Workers:           2,
		LinkDebounce:      time.Hour,
		GroupAlgoDebounce: 5 * time.Millisecond,
		GroupAlgoMaxWait:  time.Hour,
		StageGateDisabled: true,
		Index: func(_ context.Context, _ string, _ string) error {
			indexRuns.Add(1)
			return nil
		},
		Links: func(_ context.Context, _ string) error { return nil },
		GroupAlgo: func(_ context.Context, _ string) error {
			select {
			case algoEntered <- struct{}{}:
			default:
			}
			<-releaseAlgo
			return nil
		},
		GroupsForRepo:      func(_ string) []string { return nil },
		MemReleaseDisabled: true,
	})
	s.Start()
	defer func() { close(releaseAlgo); s.Stop() }()

	s.scheduleGroupAlgo("acme")
	<-algoEntered
	s.Enqueue("/repo-a")
	// With the gate disabled the index must NOT be held back by the in-flight
	// group-algo pass (legacy, overlapping behaviour).
	waitFor(t, 3*time.Second, func() bool { return indexRuns.Load() >= 1 })
}
