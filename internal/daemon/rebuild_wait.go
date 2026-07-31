package daemon

// rebuild_wait.go — serve-side "wait until the engine actually finished OUR
// rebuild, and learn whether it SUCCEEDED" for split mode (#5790, advances epic
// #5729).
//
// In split mode Service.Rebuild enqueues a KindRebuild request onto the engine
// queue and, historically, returned immediately (fire-and-forget). Callers that
// treat err==nil as "the rebuild ran" (notably `grafel group add --index`,
// which reports "indexed": true) were therefore lied to: the ack only meant the
// enqueue landed on disk, not that the engine had rebuilt anything.
//
// awaitRebuildCompletion restores an honest "err==nil means the rebuild
// SUCCEEDED" contract by reading the engine's TERMINAL ACK — the exact ack the
// request-queue consumer (ApplyAndAckBounded, from commit 6b6f18497) writes. The
// consumer is told to KEEP that ack for a WaitForCompletion request (see
// requests.ApplyAndAckBounded's keepAck and requests_drain.go's applyBucket),
// so the waiter can read its Status:
//
//   - StatusOK    → the engine ran our rebuild to completion → nil.
//   - StatusError → the engine's rebuild returned an error, OR the request was
//     dead-lettered after maxRebuildAttempts mid-apply crashes/reaps (a real
//     risk on a memory-heavy large-monorepo rebuild that OOM-reaps) → ERROR. This
//     is the honesty fix: a failed/abandoned rebuild is NEVER reported as done.
//
// Reading the ack (rather than mere request-absence) is what makes the
// success/failure distinction possible. Per-repo classification remains the
// status plane's job (rebuild_request_status.go, wizard_split_progress.go); this
// gate answers "did the engine finish, and did it succeed?".
//
// The wait is BOUNDED and failure-aware: a never-alive engine fast-fails within
// a startup window, an engine that was live and then went stale (beyond a
// GENEROUS threshold — see rebuildWaitStaleAfter) is reported as engine-death,
// and an overall timeout is the last-resort bound so a wedged engine can never
// hang the RPC forever. net/rpc gives the handler no context, so the timeout is
// the cancellation surface — identical to the pre-existing monolith synchronous
// path (rebuildRPCTimeout), which also bounds a blocking Rebuild with no ctx.

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/requests"
	"github.com/cajasmota/grafel/internal/statusfile"
)

// waitClock is the injectable time seam so tests advance/shrink time instead of
// sleeping for real poll intervals.
type waitClock interface {
	Now() time.Time
	Sleep(d time.Duration)
}

type realWaitClock struct{}

func (realWaitClock) Now() time.Time        { return time.Now() }
func (realWaitClock) Sleep(d time.Duration) { time.Sleep(d) }

// The rebuild* seam family (#6059).
//
// These knobs exist ONLY so tests can shrink/fake them and exercise the
// completion / timeout / engine-death paths deterministically; production never
// installs an override. They used to be plain package vars, which was a live
// data race: every one of them is read from a goroutine the test does not own —
// Service.Rebuild runs on a net/rpc serving goroutine, and awaitRebuildCompletion
// polls from that same goroutine — while a fixture's t.Cleanup writes them
// during teardown. The detector caught it as
// `TestRebuild_SingleFlight_NoConcurrentOverlap`, which is what made the
// release gate untrustworthy. The organic event is RARE and needs the FULL
// package: measured on pristine 67f8557c9, 1 failure in 36 full-package
// `-race` iterations (~3%, vs the ~12% #6056 reported), and 0 in 80 isolated
// `-run TestRebuild_SingleFlight_NoConcurrentOverlap` iterations — the reader
// is a net/rpc serving goroutine LEAKED by an earlier test in the same binary,
// so it only exists once a neighbour has run. That rarity is why the pin below
// (`TestRebuildSeams_ConcurrentOverrideAndRead`) drives the shape directly
// instead of relying on the flake: with any seam reverted to a plain var it
// fails 5/5, and 0/60 with the fix.
//
// The fix is the same shape as engineChildCommand (#6056): an atomic behind an
// accessor. Joining the serving goroutine before restoring would also be
// correct today, but it silently stops protecting the first time a fixture
// forgets; an atomic has NO plain-access form, so the protection is structural
// rather than a convention. That property is why a knob ADDED to this family
// later inherits the fix instead of inheriting the hazard — declare the
// override next to its siblings below and go through the accessor.
//
// Conventions, uniform across the family:
//   - the accessor keeps the ORIGINAL identifier (rebuildWaitInterval,
//     rebuildEngineAliveFn, ...) so the release-runbook triage note and any
//     `WARNING: DATA RACE` report stay greppable by the same names;
//   - the zero value of the override means "no override, use the default" —
//     0 for a duration, nil for a pointer seam. Setting nil/0 CLEARS rather
//     than installing a nil (#6056 F6: storing a non-nil pointer to a nil func
//     made the next resolve panic);
//   - every setter returns a restore closure and they nest LIFO. The STORAGE is
//     race-free, but set/restore itself is a non-atomic read-modify-write
//     (Load-then-Store), so the LIFO nesting property holds only for a single
//     writer. That is sound today because nothing in internal/daemon calls
//     t.Parallel(); a future parallel fixture that installs an override needs a
//     compare-and-swap or a mutex here, not just the atomic;
//   - callers that need a value more than once in one logical operation resolve
//     it ONCE into a local, so a value and the message explaining it cannot
//     disagree.
//
// None of these seams is the benign read-once-at-startup shape (contrast
// listenFn): all are resolved from a live RPC-serving goroutine, and
// rebuildWaitStaleAfter is resolved on every poll iteration of the wait loop.
const (
	// defaultRebuildWaitInterval is the poll cadence between ack checks.
	defaultRebuildWaitInterval = 500 * time.Millisecond
	// defaultRebuildWaitStartupWindow fast-fails if the engine is NEVER seen
	// live within this window after the request was enqueued (S1 in the wizard).
	defaultRebuildWaitStartupWindow = 30 * time.Second
	// defaultRebuildWaitTimeout is the last-resort overall bound. Kept equal to
	// the monolith synchronous cap so both modes bound a blocking Rebuild the
	// same, and comfortably above a multi-minute large-monorepo rebuild so a
	// healthy long rebuild never false-times-out. NOTE it tracks the DEFAULT
	// RPC cap, not the effective one: shrinking rebuildRPCTimeout in a test has
	// never shrunk this, and that stays true.
	defaultRebuildWaitTimeout = defaultRebuildRPCTimeout
	// defaultRebuildWaitStaleAfter is how long the engine-liveness heartbeat may
	// go unrefreshed before the COMPLETION WAIT treats the engine as dead. It is
	// deliberately far more generous than the shared EngineHeartbeatStaleAfter()
	// (3×5s=15s) the doctor/warming reads use (#5790 SHOULD-FIX): a memory-heavy
	// rebuild under GC/swap pressure can starve the engine's 5s heartbeat ticker
	// for tens of seconds while the rebuild itself is perfectly healthy, and a
	// false "engine stopped responding" there would report a good rebuild as
	// not-indexed. A restart, by contrast, is NOT death for us — the request is
	// durable and the new engine redrains it (crash-resume), so its terminal ack
	// still arrives; keying death on a PID change would wrongly abandon a
	// recoverable rebuild. 2 minutes tolerates realistic GC/swap stalls yet still
	// gives up long before the 2h overall cap on a truly dead engine that is not
	// coming back.
	defaultRebuildWaitStaleAfter = 2 * time.Minute
)

// engineAliveFunc reports whether the engine's liveness heartbeat is fresh.
type engineAliveFunc func() bool

// Test overrides for the family. Zero value == no override; production never
// stores anything here.
var (
	rebuildRPCTimeoutOverride        atomic.Int64 // nanoseconds; 0 == unset
	rebuildWaitIntervalOverride      atomic.Int64
	rebuildWaitStartupWindowOverride atomic.Int64
	rebuildWaitTimeoutOverride       atomic.Int64
	rebuildWaitStaleAfterOverride    atomic.Int64
	rebuildWaitClockOverride         atomic.Pointer[waitClock]
	rebuildEngineAliveOverride       atomic.Pointer[engineAliveFunc]
)

// loadDurationSeam resolves a duration seam: the override when non-zero, else
// the compiled-in default.
func loadDurationSeam(override *atomic.Int64, def time.Duration) time.Duration {
	if v := override.Load(); v != 0 {
		return time.Duration(v)
	}
	return def
}

// storeDurationSeam installs a duration override (d == 0 clears it) and returns
// a restore closure that puts back whatever was there before, so overrides nest
// LIFO.
func storeDurationSeam(override *atomic.Int64, d time.Duration) (restore func()) {
	prev := override.Load()
	override.Store(int64(d))
	return func() { override.Store(prev) }
}

// rebuildWaitInterval resolves the poll cadence between ack checks.
func rebuildWaitInterval() time.Duration {
	return loadDurationSeam(&rebuildWaitIntervalOverride, defaultRebuildWaitInterval)
}

// rebuildWaitStartupWindow resolves the never-became-live fast-fail window.
func rebuildWaitStartupWindow() time.Duration {
	return loadDurationSeam(&rebuildWaitStartupWindowOverride, defaultRebuildWaitStartupWindow)
}

// rebuildWaitTimeout resolves the last-resort overall wait bound.
func rebuildWaitTimeout() time.Duration {
	return loadDurationSeam(&rebuildWaitTimeoutOverride, defaultRebuildWaitTimeout)
}

// rebuildWaitStaleAfter resolves the completion-wait heartbeat staleness bound.
func rebuildWaitStaleAfter() time.Duration {
	return loadDurationSeam(&rebuildWaitStaleAfterOverride, defaultRebuildWaitStaleAfter)
}

// rebuildWaitClock resolves the production clock, or a test's fake.
func rebuildWaitClock() waitClock {
	if c := rebuildWaitClockOverride.Load(); c != nil {
		return *c
	}
	return realWaitClock{}
}

// rebuildEngineAliveFn reports whether the engine's liveness heartbeat is
// fresh (within rebuildWaitStaleAfter). It has the shape `func() bool` so it
// can still be passed straight to awaitRebuildAck. Production resolves to
// defaultRebuildEngineAlive, which reads the engine-liveness sidecar.
func rebuildEngineAliveFn() bool {
	if fn := rebuildEngineAliveOverride.Load(); fn != nil {
		return (*fn)()
	}
	return defaultRebuildEngineAlive()
}

// setRebuildWaitIntervalForTest installs a poll-cadence override (0 clears) and
// returns a restore closure. Production must never call this — as with every
// setter below.
func setRebuildWaitIntervalForTest(d time.Duration) (restore func()) {
	return storeDurationSeam(&rebuildWaitIntervalOverride, d)
}

// setRebuildWaitStartupWindowForTest installs a startup-window override (0
// clears) and returns a restore closure.
//
// ASYMMETRY, deliberate (#6059 review F3): awaitRebuildAck still honours
// startupWindow == 0 as "disable the never-became-live fast-fail", but this
// setter can no longer produce that, because 0 is the family's clear sentinel
// and resolves back to defaultRebuildWaitStartupWindow. awaitRebuildAck takes
// its bounds as parameters and is a pure function, so a direct unit test can
// still pass 0 — the branch is kept for that caller rather than deleted. What is
// lost is only the ability to reach it THROUGH the seam, which no test does.
func setRebuildWaitStartupWindowForTest(d time.Duration) (restore func()) {
	return storeDurationSeam(&rebuildWaitStartupWindowOverride, d)
}

// setRebuildWaitTimeoutForTest installs an overall-bound override (0 clears)
// and returns a restore closure.
func setRebuildWaitTimeoutForTest(d time.Duration) (restore func()) {
	return storeDurationSeam(&rebuildWaitTimeoutOverride, d)
}

// setRebuildWaitStaleAfterForTest installs a staleness-bound override (0
// clears) and returns a restore closure.
func setRebuildWaitStaleAfterForTest(d time.Duration) (restore func()) {
	return storeDurationSeam(&rebuildWaitStaleAfterOverride, d)
}

// setRebuildWaitClockForTest installs a fake clock and returns a restore
// closure. c == nil CLEARS the override (the seam falls back to realWaitClock)
// rather than installing a nil clock whose next method call would panic.
func setRebuildWaitClockForTest(c waitClock) (restore func()) {
	prev := rebuildWaitClockOverride.Load()
	if c == nil {
		rebuildWaitClockOverride.Store(nil)
	} else {
		next := c
		rebuildWaitClockOverride.Store(&next)
	}
	return func() { rebuildWaitClockOverride.Store(prev) }
}

// setRebuildEngineAliveForTest installs a liveness-probe override and returns a
// restore closure. fn == nil CLEARS the override (the seam falls back to
// defaultRebuildEngineAlive); see #6056 F6 for why storing a non-nil pointer to
// a nil func is the trap this branch avoids.
func setRebuildEngineAliveForTest(fn func() bool) (restore func()) {
	prev := rebuildEngineAliveOverride.Load()
	if fn == nil {
		rebuildEngineAliveOverride.Store(nil)
	} else {
		next := engineAliveFunc(fn)
		rebuildEngineAliveOverride.Store(&next)
	}
	return func() { rebuildEngineAliveOverride.Store(prev) }
}

// defaultRebuildEngineAlive reads the ambient engine-liveness heartbeat (the
// same sidecar the wizard/doctor use) and reports whether it is fresh within the
// GENEROUS completion-wait threshold rebuildWaitStaleAfter — NOT the tight
// shared EngineHeartbeatStaleAfter() default (see rebuildWaitStaleAfter's doc).
func defaultRebuildEngineAlive() bool {
	root := ""
	if layout, err := DefaultLayout(); err == nil {
		root = layout.Root
	}
	f, err := statusfile.Read(EngineLivenessStatusKey(root))
	if err != nil {
		return false
	}
	return time.Since(f.HeartbeatAt) <= rebuildWaitStaleAfter()
}

// awaitRebuildCompletion blocks until the engine writes the terminal ack for the
// KindRebuild request <id> under dir, then returns nil on a StatusOK ack and an
// error on a StatusError/dead-letter ack — or a bounded failure (engine-death,
// never-alive, timeout). It is the serve-side glue Service.Rebuild calls when
// WaitForCompletion is set in split mode. The kept ack (keepAck) is consumed on
// the way out so it does not linger on disk.
func (s *Service) awaitRebuildCompletion(dir, id string) error {
	// Best-effort consume/cleanup of the kept ack regardless of outcome (a
	// no-op if the wait timed out before any ack was written).
	defer func() { _ = requests.DeleteAck(dir, id) }()
	readAck := func() (requests.Ack, bool, error) { return requests.ReadAck(dir, id) }
	// Each seam is resolved ONCE here, into awaitRebuildAck's parameters, so a
	// single wait uses one consistent set of bounds (#6059).
	return awaitRebuildAck(readAck, rebuildEngineAliveFn, rebuildWaitClock(), rebuildWaitInterval(), rebuildWaitStartupWindow(), rebuildWaitTimeout())
}

// awaitRebuildAck is the pure completion loop (no I/O of its own beyond the
// injected closures), so it is unit-testable with fakes. It returns:
//   - nil                        — a StatusOK terminal ack appeared: engine ran our rebuild OK.
//   - "group rebuild failed" err — a StatusError/dead-letter ack appeared: honest failure.
//   - "stopped responding" err   — engine was live then went stale (death).
//   - "never became live" err    — engine was never live within startupWindow.
//   - "timed out" err            — overall bound elapsed with no terminal ack.
func awaitRebuildAck(readAck func() (requests.Ack, bool, error), alive func() bool, clk waitClock, interval, startupWindow, timeout time.Duration) error {
	start := clk.Now()
	sawAlive := false
	for {
		if ack, ok, err := readAck(); err == nil && ok {
			if ack.Status == requests.StatusError {
				reason := ack.Err
				if reason == "" {
					reason = "engine reported an error"
				}
				return fmt.Errorf("group rebuild failed: %s", reason)
			}
			return nil // StatusOK → engine finished our rebuild successfully
		}
		if alive() {
			sawAlive = true
		} else if sawAlive {
			return fmt.Errorf("index engine stopped responding before the group rebuild finished")
		}
		elapsed := clk.Now().Sub(start)
		// startupWindow == 0 disables the fast-fail. Reachable only by a direct
		// caller of this function: setRebuildWaitStartupWindowForTest treats 0 as
		// the family's CLEAR sentinel, so it cannot install 0 (#6059 review F3).
		if !sawAlive && startupWindow > 0 && elapsed >= startupWindow {
			return fmt.Errorf("index engine never became live within %s; is the daemon/engine running?", startupWindow)
		}
		if elapsed >= timeout {
			return fmt.Errorf("timed out after %s waiting for the group rebuild to finish", timeout)
		}
		clk.Sleep(interval)
	}
}
