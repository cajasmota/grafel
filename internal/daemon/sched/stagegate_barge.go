package sched

import (
	"sync"
	"time"
)

// The FOREGROUND BARGE (#5954 follow-up to the #5993 stage gate).
//
// WHY THE GATE ALONE WAS NOT ENOUGH. The gate's SHARED (index) side reads
// s.inflight, which tracks SCHEDULER-DISPATCHED index jobs only. But the path a
// user actually takes for a from-scratch index — `grafel reset`, the rebuild
// RPC — runs through cmd/grafel's makeDaemonRebuildFunc → indexFn → Index(...)
// DIRECTLY. It never calls scheduler.Enqueue, so throughout a 10–12 minute
// rebuild len(s.inflight) == 0 and the gate concludes the machine is idle. A
// background group-algo pass then starts alongside the largest process on the
// box. Measured after deploying the gate: peak went UP, 4258 → 5430 MB, and the
// peak instant showed `index child 2979 MB + group-algo 1618 MB` co-resident.
// The gate was correct for scheduler-driven churn (git hooks, watchers, async
// enqueue) and simply did not bind the path that runs.
//
// THE SHAPE. Foreground work must never WAIT — a user watching a rebuild must
// not pay latency for a background pass — but it must REGISTER so background
// stages yield around it. So:
//
//   - a foreground rebuild BARGES: bargeForeground records a hold and returns
//     immediately, unconditionally, whatever else is running;
//   - background exclusive stages (scheduler link pass, group-algo) see the
//     hold in tryAcquireStageLocked and DEFER, exactly as they defer for each
//     other — re-arming their own timer, staying pending, losing no work.
//
// THE INVARIANT, stated honestly. At most one heavy write-side stage is
// resident at a time, EXCEPT:
//
//  1. after a forfeit — reapStageLocked clears a wedged exclusive holder
//     without cancelling it (see its doc), so a successor can overlap it; and
//  2. the ONE background exclusive stage that was already mid-flight at the
//     instant a rebuild barged. The barge does not cancel it (the gate has no
//     handle that could resume the lost work) and does not wait for it. That
//     one hand-off overlaps; every background stage that STARTS after the barge
//     defers until the barge lifts.
//
// Both exceptions are single-hand-off, not steady-state. Case 2 costs at most
// one background pass' worth of overlap at the START of a rebuild, versus the
// pre-barge behaviour of a background pass being free to start at ANY point
// across the whole rebuild — including its heaviest minutes.
//
// SCOPE. The barge holds off EXCLUSIVE stages only; it does not gate index
// admission. Scheduler-dispatched index jobs remain bounded by the RSS
// admission ledger, and same-repo collisions by repolock's foreground claim —
// both unchanged. Blocking admission for the 10–12 minutes of a rebuild would
// be a much larger behavioural change than the measurement asks for, and it
// would starve the watcher queue for the whole window.

const (
	// stageGateBargeMaxDefault is the barge's leak backstop.
	//
	// THE HOLD-MAX RESOLUTION. A foreground rebuild takes 10–12 minutes on the
	// current corpus — right at StageGateHoldMax (15m). A barge subject to that
	// forfeit would expire itself mid-rebuild on exactly the LONGEST rebuilds,
	// silently reintroducing the overlap it exists to remove, on the runs with
	// the largest resident graph. A self-forfeiting barge is worse than no
	// barge. So the barge is NOT subject to StageGateHoldMax; it carries its own
	// bound, and that bound is deliberately an order of magnitude looser.
	//
	// It can afford to be, because the two holders have different liveness
	// guarantees. StageGateHoldMax must be tight: an exclusive stage holds the
	// token ACROSS A SUBPROCESS SPAWN, and a child that is SIGKILLed in a way
	// the parent never observes wedges INDEX DISPATCH — the daemon's core job.
	// A barge is held by in-process Go code with `defer release()` at the call
	// site, so it unwinds on return, error, cancellation and panic alike; and a
	// leaked barge cannot wedge indexing at all, only delay background link and
	// group-algo passes. That is a strictly weaker failure mode, so an hour-scale
	// backstop is the right trade: long enough that no legitimate rebuild can
	// reach it, short enough that a leak self-heals without a daemon restart.
	//
	// This is a BACKSTOP, not a scheduling parameter. A `stage_gate_barge_expired`
	// event means a rebuild goroutine vanished without unwinding — investigate
	// it, do not simply raise this. Env: GRAFEL_STAGE_GATE_BARGE_MAX.
	stageGateBargeMaxDefault = 60 * time.Minute
)

// bargeHold is one live foreground registration: who, and since when. Holds are
// keyed by a monotonic id rather than by name so that concurrent rebuilds of
// different groups each get their own entry, a release cannot drop somebody
// else's hold, and a double release is a harmless no-op (deleting an absent id).
type bargeHold struct {
	name  string
	since time.Time
}

// bargeForeground registers a foreground hold on the heavy write-stage gate and
// returns its release closure. It NEVER blocks and NEVER fails: the returned
// closure is safe to call more than once and safe to call after the backstop
// has already reclaimed the hold.
//
// Call it with `defer release()` at the top of the foreground unit of work, so
// the hold spans everything that materialises a big slice of the group graph
// (for a rebuild: the whole per-repo index batch AND the cross-repo link pass)
// and unwinds on every exit path.
//
// With the gate disabled (GRAFEL_STAGE_GATE=0 / Config.StageGateDisabled) this
// records nothing and returns an inert closure — the escape hatch must restore
// the pre-gate baseline exactly, including on this path.
func (s *Scheduler) bargeForeground(name string) (release func()) {
	if s.cfg.StageGateDisabled {
		return func() {}
	}
	now := time.Now()

	s.mu.Lock()
	s.stageBargeSeq++
	id := s.stageBargeSeq
	if s.stageBarge == nil {
		s.stageBarge = map[int64]bargeHold{}
	}
	s.stageBarge[id] = bargeHold{name: name, since: now}
	s.logEventLocked("stage_gate_barge", "",
		name+": foreground work registered — background heavy stages will defer until it completes (#5954)")
	s.mu.Unlock()

	s.logger.Info("sched: foreground work barged the heavy write-stage gate",
		"holder", name, "barge_max", s.cfg.StageGateBargeMax)

	var once sync.Once
	return func() {
		once.Do(func() { s.releaseBarge(id) })
	}
}

// releaseBarge drops one foreground hold and wakes the gate so a deferred
// background stage can retry promptly rather than sitting out the rest of its
// StageGateRetry window.
func (s *Scheduler) releaseBarge(id int64) {
	s.mu.Lock()
	if h, ok := s.stageBarge[id]; ok {
		held := time.Since(h.since).Truncate(time.Millisecond)
		delete(s.stageBarge, id)
		s.logEventLocked("stage_gate_barge_release", "", h.name+" barged for "+held.String())
	}
	s.mu.Unlock()
	// Capacity has freed: let the admission loop and any deferred stage move.
	s.poke()
}

// reapBargeLocked expires holds older than StageGateBargeMax. MUST be called
// with s.mu held. See stageGateBargeMaxDefault for why this bound is an order
// of magnitude looser than StageGateHoldMax rather than shared with it.
func (s *Scheduler) reapBargeLocked(now time.Time) {
	if s.cfg.StageGateBargeMax <= 0 {
		return
	}
	for id, h := range s.stageBarge {
		if now.Sub(h.since) <= s.cfg.StageGateBargeMax {
			continue
		}
		held := now.Sub(h.since).Truncate(time.Second)
		delete(s.stageBarge, id)
		s.logEventLocked("stage_gate_barge_expired", "",
			h.name+": foreground barge held "+held.String()+" — reclaiming so background stages resume (#5954)")
		s.logger.Error("sched: foreground barge exceeded its backstop — a foreground unit of work never unwound its release",
			"holder", h.name, "held_for", held, "barge_max", s.cfg.StageGateBargeMax)
	}
}

// bargeHeldLocked reports whether any foreground hold is live. MUST be called
// with s.mu held, and AFTER reapBargeLocked (reapStageLocked does both).
func (s *Scheduler) bargeHeldLocked() bool { return len(s.stageBarge) > 0 }

// bargeNamesLocked renders the live holders for Snapshot/observability. MUST be
// called with s.mu held.
func (s *Scheduler) bargeNamesLocked() []string {
	if len(s.stageBarge) == 0 {
		return nil
	}
	out := make([]string, 0, len(s.stageBarge))
	for _, h := range s.stageBarge {
		out = append(out, h.name)
	}
	return out
}

// --- process bridge ---------------------------------------------------------
//
// cmd/grafel's rebuild path has no scheduler handle: daemon.Config carries a
// Rebuild func INTO internal/daemon, which is where the scheduler is
// constructed — the dependency runs the wrong way to thread one back out, and
// restructuring the daemon to fix that is out of proportion to a single
// advisory hold. So the scheduler publishes its barge here on Start() and
// withdraws it on Stop(); the rebuild reaches it through the package-level
// BargeForeground below. Same shape as repolock.DefaultRegistry, which the
// rebuild already uses for the analogous cross-path claim.
//
// Withdrawal is identity-checked so a scheduler stopping after a newer one
// started (test suites do this) cannot unpublish the live one.

var (
	bargeBridgeMu sync.Mutex
	bargeBridge   *Scheduler
)

func publishBargeBridge(s *Scheduler) {
	bargeBridgeMu.Lock()
	bargeBridge = s
	bargeBridgeMu.Unlock()
}

func withdrawBargeBridge(s *Scheduler) {
	bargeBridgeMu.Lock()
	if bargeBridge == s {
		bargeBridge = nil
	}
	bargeBridgeMu.Unlock()
}

// BargeForeground registers foreground work with the running daemon's heavy
// write-stage gate and returns its release closure. Use it with `defer` at the
// top of any unit of work that materialises a large slice of a group graph
// OUTSIDE the scheduler — today, the rebuild RPC's index batch and its own
// cross-repo link pass.
//
// It never blocks and never fails. With no scheduler running (CLI one-shots,
// watcher-less daemons, tests) it is inert and the returned closure is a no-op,
// so callers need no nil checks and no build-mode branches.
func BargeForeground(name string) (release func()) {
	bargeBridgeMu.Lock()
	s := bargeBridge
	bargeBridgeMu.Unlock()
	if s == nil {
		return func() {}
	}
	return s.bargeForeground(name)
}
