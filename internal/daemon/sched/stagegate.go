package sched

import (
	"os"
	"strings"
	"time"
)

// The heavy write-stage gate (#5954).
//
// The daemon runs three heavy write-side stages, each of which materialises a
// large slice of a group's union graph:
//
//  1. the per-repo index batch (subprocess `grafel index-internal`),
//  2. the cross-repo link pass (IN-PROCESS in the engine),
//  3. the group-algorithm pass (subprocess `grafel group-algo --write`).
//
// Nothing used to make them mutually exclusive. algoSem bounds group-algo passes
// against EACH OTHER; the RSS admission ledger (tryAdmit) ledgers INDEX jobs
// only and has no knowledge that the other two exist. With a 180s group-algo
// debounce against per-repo indexes that take tens of seconds, overlap was the
// normal case — and the measured whole-machine peak was the SUM of two
// co-resident copies of the same graph (engine 1816MB + group-algo 2368MB) even
// at instants when the index child was at 0MB.
//
// The gate is a single daemon-wide token with three classes of holder:
//
//   - SHARED  — scheduler-dispatched index jobs. Their occupancy is already
//     tracked exactly by s.inflight, so the gate reads that rather than
//     duplicating a counter.
//   - EXCLUSIVE — the link pass and the group-algo pass, one at a time,
//     and never while any index job is executing.
//   - BARGE — foreground work that runs OUTSIDE the scheduler entirely (the
//     rebuild RPC's index batch and its own cross-repo link pass). It
//     registers unconditionally and never waits; exclusive stages defer to it.
//     See stagegate_barge.go — including why s.inflight alone left the single
//     largest process on the machine invisible to this gate.
//
// Neither side ever BLOCKS on the other, so the gate cannot deadlock:
//
//   - an exclusive stage whose timer fires while the machine is busy DEFERS:
//     it re-arms its own timer (stays pending, never dropped) and retries;
//   - index dispatch is held in tryAdmit while an exclusive stage holds the
//     token, and is released by the poke() that every stage release performs.
//
// Starvation (see noteStageDeferLocked) is bounded by escalating a long-running
// deferral into a DRAIN BARRIER rather than by letting the stage run anyway.
//
// THE INVARIANT, stated truthfully. At most one heavy write-side stage is
// resident at a time, with exactly two documented exceptions, both of which are
// single hand-offs rather than steady states:
//
//  1. after a FORFEIT — reapStageLocked clears a wedged exclusive holder
//     without cancelling it (it has no handle that could), so a successor may
//     become co-resident with it; and
//  2. the ONE exclusive stage already mid-flight when foreground work BARGES.
//     The barge neither cancels it (that would discard minutes of unresumable
//     work) nor waits for it (the foreground path must never pay latency), so
//     that single pair overlaps. Every exclusive stage that STARTS after the
//     barge defers until it lifts.
//
// Both exceptions are loud: `stage_gate_forfeit` (ERROR) and `stage_gate_barge`.

const (
	// stageGateRetryDefault is how long a deferred heavy stage waits before
	// re-testing the gate. Short enough to seize a natural gap between index
	// batches (a per-repo index takes tens of seconds), long enough that a
	// deferral loop costs nothing measurable.
	stageGateRetryDefault = 15 * time.Second

	// stageGateMaxDeferDefault bounds how long an exclusive stage may be held
	// off by index activity before the gate escalates to a drain barrier. It
	// deliberately mirrors groupAlgoMaxWaitDefault (300s): the gate takes
	// PRECEDENCE over that max-wait (otherwise the gate would be decorative
	// under exactly the sustained-churn conditions that produce the worst RSS
	// peaks), so it must carry an equivalent starvation budget of its own.
	stageGateMaxDeferDefault = 300 * time.Second

	// stageGateHoldMaxDefault is the longest an exclusive stage may hold the
	// token before the gate FORFEITS it and resumes index dispatch. The token
	// is held across a subprocess spawn (group-algo), so a child that wedges or
	// is SIGKILLed in a way that never returns must not wedge the daemon
	// forever. This is a safety valve, not a scheduling parameter — but it is
	// SCALE-SENSITIVE and must be tunable in the field (GRAFEL_STAGE_GATE_HOLD_
	// MAX / Config.StageGateHoldMax). A group-algo pass measures ~4 min on the
	// current corpus, so 15m is ~3.75x headroom; on a corpus ~4x larger a
	// LEGITIMATE pass would exceed it, be forfeited on every run, and silently
	// degrade the gate to no-gate precisely on the largest corpora it exists
	// for. A burst of `stage_gate_forfeit` events is that signal — raise this.
	stageGateHoldMaxDefault = 15 * time.Minute

	// stageGateDrainMaxDefault bounds the DRAIN BARRIER (the starvation guard).
	// While the barrier is up no NEW index job is dispatched, so the in-flight
	// batch drains and the starved stage can acquire the token without ever
	// overlapping. The barrier is bounded so that a stage which somehow never
	// acquires cannot block index dispatch indefinitely; it is comfortably
	// longer than a single per-repo index. Tunable via Config.StageGateDrainMax.
	stageGateDrainMaxDefault = 2 * time.Minute
)

// stageGateDisabledFromEnv reports whether GRAFEL_STAGE_GATE explicitly turns
// the gate off ("0"/"false"/"off"). Any other value (including unset) leaves it
// enabled — the gate is the default because peak RSS on a 16GB laptop is the
// binding constraint, not reindex wall-clock.
func stageGateDisabledFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GRAFEL_STAGE_GATE"))) {
	case "0", "false", "off", "no":
		return true
	}
	return false
}

// stageGateDurationFromEnv resolves a Go duration from env, falling back to def.
func stageGateDurationFromEnv(key string, def time.Duration) time.Duration {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return def
}

// stopped reports whether Stop() has been signalled. A deferred stage re-arms a
// retry timer, so every fire path checks this first: without it a timer firing
// during shutdown could arm a successor that outlives the scheduler.
func (s *Scheduler) stopped() bool {
	select {
	case <-s.stop:
		return true
	default:
		return false
	}
}

// reapStageLocked expires a wedged token holder and a stale drain barrier. MUST
// be called with s.mu held. It is the "no lost work, no wedge" half of the gate:
// every blocking-ish piece of state has a bounded lifetime.
//
// THE NO-OVERLAP INVARIANT IS CONDITIONAL ON THIS FUNCTION NOT FIRING. The
// forfeit below clears stageHolder WITHOUT cancelling the wedged stage — the
// gate has no cancel handle for it, and even if it did, a stage that has stopped
// responding for StageGateHoldMax is unlikely to honour one. So after a forfeit a
// second exclusive stage CAN become co-resident with the first. That is the
// deliberate trade: a transient overlap is strictly better than a permanently
// wedged daemon that never indexes again. The identity check in releaseStage is
// the other half — a late-returning forfeited stage must not clear the NEW
// holder's token. Everything else in the gate holds unconditionally; this is the
// one documented exception, and it is loud (ERROR + stage_gate_forfeit event).
func (s *Scheduler) reapStageLocked(now time.Time) {
	// Foreground barges expire on their OWN, much longer bound — never on
	// StageGateHoldMax. See stageGateBargeMaxDefault for why the two liveness
	// guarantees differ and why sharing the bound would be actively harmful.
	s.reapBargeLocked(now)
	if s.stageHolder != "" && s.cfg.StageGateHoldMax > 0 &&
		now.Sub(s.stageHeldSince) > s.cfg.StageGateHoldMax {
		held := now.Sub(s.stageHeldSince).Truncate(time.Second)
		s.logEventLocked("stage_gate_forfeit", "",
			s.stageHolder+": held the heavy-stage token for "+held.String()+" — forfeiting so index dispatch resumes (#5954)")
		s.logger.Error("sched: heavy write-stage token forfeited after hold-max — a stage or its subprocess never returned",
			"stage", s.stageHolder, "held_for", held, "hold_max", s.cfg.StageGateHoldMax)
		s.stageHolder = ""
		s.stageHeldSince = time.Time{}
	}
	// Expire a stale drain barrier — but ONLY once the batch it was raised to
	// drain has actually cleared. Expiring while index jobs are still executing
	// would re-open dispatch in the one state the barrier exists to prevent, and
	// the starved stage would immediately re-raise it on its next retry anyway
	// (measured: latency-to-first-run is unchanged either way, because the
	// re-raise wins the race). Gating on an empty inflight map turns that race
	// into a guarantee. The barrier is still bounded: with dispatch held, the
	// in-flight batch drains within one index duration.
	if s.stageDrainFor != "" && now.After(s.stageDrainUntil) && len(s.inflight) == 0 {
		s.logEventLocked("stage_gate_drain_expired", "",
			s.stageDrainFor+": drain barrier expired without acquiring — resuming index dispatch (#5954)")
		s.stageDrainFor = ""
		s.stageDrainUntil = time.Time{}
	}
}

// allowIndexAdmitLocked reports whether a NEW index job may be dispatched to a
// worker. It is false while an exclusive heavy stage holds the token, and while
// a starved stage has raised the drain barrier. MUST be called with s.mu held.
//
// Interactive/user-initiated work is NOT held by this gate. The synchronous
// Index RPC (Service.Index without Async) calls s.index directly and never
// touches the scheduler queue; the foreground group-rebuild
// (daemonRebuildFuncCore) calls the extractor directly for the same reason. The
// only caller routed through this gate is Index{Async:true} — background,
// already-debounced, fire-and-forget reindexing.
//
// The foreground rebuild is covered from the OTHER side, by the BARGE (#5954
// follow-up, stagegate_barge.go): daemonRebuildFuncCore registers a barge for
// its whole duration — its direct index batch AND its own in-process cross-repo
// link pass, the SCOPE LIMIT this comment previously flagged as uncovered — so
// background exclusive stages defer around it. The rebuild still never waits and
// is still never held by THIS function.
//
// One bounded interaction, for the reader tracing a stall: if a drain barrier
// is ALREADY up when a rebuild barges, the starved stage can no longer acquire
// (tryAcquireStageLocked's barge check precedes the branch that clears the
// barrier on acquisition), so the barrier cannot be cleared that way. It is
// still bounded, and NOT by the barge's lifetime: reapStageLocked expires it
// once now > stageDrainUntil AND the in-flight batch has cleared, so index
// dispatch is held for at most StageGateDrainMax (2m), not the 10–12 minutes of
// a rebuild.
//
// SCOPE LIMIT — a barge does not hold index ADMISSION. Scheduler-dispatched
// index jobs may still be dispatched during a foreground rebuild; they remain
// bounded by the RSS admission ledger, and same-repo collisions by repolock's
// foreground claim. Holding admission for the 10–12 minutes of a rebuild would
// starve the watcher queue for the whole window, which the measurement does not
// ask for. Also uncovered: the synchronous `grafel index` RPC (daemonIndexFunc),
// which is single-repo and an order of magnitude smaller than a group rebuild.
func (s *Scheduler) allowIndexAdmitLocked(now time.Time) bool {
	if s.cfg.StageGateDisabled {
		return true
	}
	s.reapStageLocked(now)
	return s.stageHolder == "" && s.stageDrainFor == ""
}

// stageBusyLocked reports whether the gate itself represents work in progress
// (an exclusive stage running, or a drain barrier waiting for the index batch to
// clear). The dead-man switch consults this: its job is to detect "queue
// non-empty and NOTHING is happening", and under the gate something IS happening
// — force-admitting an index there would re-create the exact overlap the gate
// exists to remove. Both states are individually time-bounded (StageGateHoldMax,
// StageGateDrainMax), so the dead-man's own guarantee survives.
// MUST be called with s.mu held.
func (s *Scheduler) stageBusyLocked(now time.Time) bool {
	if s.cfg.StageGateDisabled {
		return false
	}
	s.reapStageLocked(now)
	return s.stageHolder != "" || s.stageDrainFor != ""
}

// tryAcquireStageLocked attempts to take the EXCLUSIVE heavy-stage token for
// `name`. It never blocks. It succeeds only when no other exclusive stage holds
// the token AND no index job is executing. MUST be called with s.mu held.
//
// A drain barrier raised by a DIFFERENT stage also blocks acquisition, so the
// starved stage that paid for the barrier is the one that gets through it.
func (s *Scheduler) tryAcquireStageLocked(name string, now time.Time) bool {
	if s.cfg.StageGateDisabled {
		return true
	}
	s.reapStageLocked(now)
	// #5954 barge: foreground work (a rebuild's index batch and its own link
	// pass) runs entirely outside the scheduler, so s.inflight below cannot see
	// it. A registered barge holds background stages off for its duration.
	if s.bargeHeldLocked() {
		return false
	}
	if s.stageHolder != "" {
		return false
	}
	if len(s.inflight) > 0 {
		return false
	}
	if s.stageDrainFor != "" && s.stageDrainFor != name {
		return false
	}
	if since, ok := s.stageDeferSince[name]; ok {
		waited := now.Sub(since).Truncate(time.Millisecond)
		s.logEventLocked("stage_gate_admit", "",
			name+": acquired the heavy-stage token after deferring for "+waited.String()+" (#5954)")
		s.logger.Info("sched: heavy write-stage resumed after deferral",
			"stage", name, "deferred_for", waited)
		delete(s.stageDeferSince, name)
	}
	if s.stageDrainFor == name {
		s.stageDrainFor = ""
		s.stageDrainUntil = time.Time{}
	}
	s.stageHolder = name
	s.stageHeldSince = now
	return true
}

// noteStageDeferLocked records that `name` could not acquire the token, and
// returns how long to wait before re-testing. MUST be called with s.mu held.
//
// Starvation resolution (the max-wait tension). scheduleGroupAlgo's #5450
// max-wait bounds DEBOUNCE starvation by firing the timer promptly under
// churn; the gate then refuses to let that firing RUN while an index batch is
// executing. Deferral deliberately takes precedence over the max-wait —
// otherwise the gate would be bypassed exactly when it matters. The replacement
// bound is not "eventually run anyway" (that would reintroduce the overlap);
// it is a DRAIN BARRIER: after StageGateMaxDefer of continuous deferral the
// gate stops admitting NEW index jobs, so the in-flight batch drains within one
// index duration and the starved stage then acquires cleanly. Progress is
// therefore guaranteed, and the no-overlap invariant is never traded away.
func (s *Scheduler) noteStageDeferLocked(name string, now time.Time) time.Duration {
	first, ok := s.stageDeferSince[name]
	if !ok {
		first = now
		s.stageDeferSince[name] = now
		s.logEventLocked("stage_gate_defer", "",
			name+": deferring — "+s.stageBlockReasonLocked()+" (#5954)")
		s.logger.Info("sched: deferring heavy write stage to avoid a co-resident graph copy",
			"stage", name, "reason", s.stageBlockReasonLocked(), "retry_in", s.cfg.StageGateRetry)
	}
	// A deferral caused by a FOREGROUND BARGE must not escalate to a drain
	// barrier. The barrier's whole mechanism is "stop admitting index jobs so
	// the in-flight batch drains" — against a barge that is inert, because the
	// blocker is a rebuild the scheduler does not dispatch and draining
	// s.inflight cannot end it. Escalating anyway would hold scheduler index
	// dispatch and emit a starvation WARN every StageGateDrainMax for the whole
	// 10–12 minutes of a rebuild, buying nothing. The deferral CLOCK keeps
	// running, so if index churn is what blocks the stage once the barge lifts,
	// the barrier is raised immediately on the next retry — the starvation
	// budget is preserved, only the useless escalation is suppressed.
	if waited := now.Sub(first); waited >= s.cfg.StageGateMaxDefer && s.stageDrainFor == "" && !s.bargeHeldLocked() {
		s.stageDrainFor = name
		s.stageDrainUntil = now.Add(s.cfg.StageGateDrainMax)
		s.logEventLocked("stage_gate_drain", "",
			name+": deferred "+waited.Truncate(time.Second).String()+" — holding index dispatch so the batch drains (#5954)")
		s.logger.Warn("sched: heavy write stage starved by index churn — raising drain barrier",
			"stage", name, "deferred_for", waited.Truncate(time.Second), "max_defer", s.cfg.StageGateMaxDefer)
	}
	return s.cfg.StageGateRetry
}

// stageBlockReasonLocked renders why the token is currently unavailable.
// MUST be called with s.mu held.
func (s *Scheduler) stageBlockReasonLocked() string {
	switch {
	case s.bargeHeldLocked():
		return "foreground " + strings.Join(s.bargeNamesLocked(), ",") + " is barging"
	case s.stageHolder != "":
		return "stage " + s.stageHolder + " holds the token"
	case len(s.inflight) > 0:
		return itoa(int64(len(s.inflight))) + " index job(s) in flight"
	case s.stageDrainFor != "":
		return "stage " + s.stageDrainFor + " owns the drain barrier"
	default:
		return "unknown"
	}
}

// releaseStage hands the exclusive token back and wakes admission. Always
// invoked via `defer` at the acquisition site, so it also runs when the stage
// returns an error, is cancelled, or panics.
func (s *Scheduler) releaseStage(name string) {
	s.mu.Lock()
	if s.stageHolder == name {
		held := time.Since(s.stageHeldSince).Truncate(time.Millisecond)
		s.stageHolder = ""
		s.stageHeldSince = time.Time{}
		s.logEventLocked("stage_gate_release", "", name+" held "+held.String())
	}
	s.mu.Unlock()
	// Capacity has freed: let the admission loop dispatch queued index jobs.
	s.poke()
}
