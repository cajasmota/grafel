package sched

import (
	"os"
	"strings"
	"sync"
	"time"
)

// foreground.go — WHO ASKED FOR THIS WORK, per group (#5954 wall-time fix).
//
// THE PROBLEM. Every heavy child the daemon spawns resolves its CPU cap inside
// its own spawn helper, from process-global env + NumCPU. That makes the cap a
// property of the CHILD, when the user's policy makes it a property of the
// CALLER:
//
//	"Cap it at max of 25% of the machine capacity … Interactive remains uncapped."
//
// The 25% half shipped (#5960); the interactive half did not reach the
// group-algo and links children. So a `grafel reset` — a human sitting there
// waiting — ran its heaviest stage on 2 cores of a 12-core box, and a full
// reindex went from ~10 minutes to 30+.
//
// WHY NOT THE BARGE. sched.BargeForeground (#5994) already marks "a foreground
// rebuild is in progress" daemon-wide, and it is the right signal for the stage
// gate. It is the WRONG signal for cap resolution, because of when it lifts.
// The rebuild's own index batch and its in-process link pass are inside the
// barge, but the stages that actually dominate the user's wall clock are NOT:
// the scheduler's cross-repo link pass and the debounced group-algo pass fire
// minutes AFTER daemonRebuildFuncCore returned and the barge released. A cap
// resolved off the barge would give the user full capacity for the two stages
// that were never the problem, and background caps for the 32–53 minute one
// that is.
//
// WHAT THIS IS INSTEAD. A per-group "the user is waiting on this group's graph"
// flag with two ways to end:
//
//   - a REFCOUNTED hold, taken for the duration of the foreground rebuild, and
//   - a LINGER after the last hold releases, which covers the follow-on stages
//     the rebuild causes but does not itself run.
//
// The linger is closed explicitly by the group-algo pass completing
// (ClearGroupForeground in runGroupAlgo) — the last stage of the work the user
// asked for — and time-bounded by foregroundLingerWindow as a backstop for the
// cases where that pass never runs (group deleted, GroupAlgo unconfigured,
// pass failing repeatedly). Without that backstop a single rebuild could leave
// a group drawing full machine capacity for background churn forever, which is
// the standing policy this change must not regress.
//
// SCOPE. This governs CPU caps only. It does not gate, defer, or reorder any
// stage; a group being "foreground" changes how much of the machine its child
// may use, never whether the child runs.

// foregroundLingerDefault is how long a group keeps foreground caps after the
// last foreground hold on it releases.
//
// It has to cover the gap between "the rebuild RPC returned" and "the last
// stage it caused was SPAWNED" — not how long those stages run, since the flag
// is read once at spawn. On the user's corpus that gap is the scheduler's link
// debounce plus the link pass (~9 min) plus the group-algo debounce (180s), so
// the window is set well clear of it rather than tight against it: the cost of
// being slightly too generous is that a background pass arriving inside the
// window runs fast, while the cost of being too tight is the exact 30-minute
// regression this file exists to fix.
const foregroundLingerDefault = 30 * time.Minute

// foregroundLingerEnv overrides foregroundLingerDefault with any Go duration
// ("90s", "10m"). Malformed or negative values are ignored and the default
// applies; "0" is honoured and means "no linger at all" (holds only).
const foregroundLingerEnv = "GRAFEL_FOREGROUND_LINGER"

func foregroundLingerWindow() time.Duration {
	if raw := strings.TrimSpace(os.Getenv(foregroundLingerEnv)); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d >= 0 {
			return d
		}
	}
	return foregroundLingerDefault
}

var (
	foregroundMu sync.Mutex
	// foregroundHolds counts live foreground holds per group. A count > 0 means
	// a user-awaited unit of work for that group is running right now.
	foregroundHolds = map[string]int{}
	// foregroundLinger is the expiry of the post-hold window per group.
	foregroundLinger = map[string]time.Time{}
	// foregroundEpochs is the identity of the CURRENT mark on each group. Every
	// MarkGroupForeground call stamps a fresh, process-monotonic epoch.
	//
	// It exists so a completion can name the mark it is completing. Without it
	// a LATE completion silently wipes a FRESHER mark: rebuild #1 marks and
	// arms a linger; its long group-algo pass starts; rebuild #2 for the same
	// group runs and re-marks; then #1's pass succeeds and clears — dropping
	// rebuild #2's mark, so #2's own follow-on link and group-algo children
	// spawn at background caps with the user still waiting on them.
	foregroundEpochs = map[string]uint64{}
	// foregroundEpochSeq is the monotonic source for the above. Never reused, so
	// two marks on the same group are always distinguishable.
	foregroundEpochSeq uint64
	// foregroundNow is the clock, swappable in tests. Read under foregroundMu.
	foregroundNow = time.Now
)

// MarkGroupForeground records that a user-awaited unit of work for group has
// started, and returns its release closure. Use it with `defer` at the top of
// any user-initiated rebuild of a group.
//
// Reentrant (refcounted) and idempotent on release. An empty group name is a
// no-op with a no-op release, so callers need no guards.
func MarkGroupForeground(group string) (release func()) {
	if group == "" {
		return func() {}
	}
	foregroundMu.Lock()
	foregroundHolds[group]++
	foregroundEpochSeq++
	foregroundEpochs[group] = foregroundEpochSeq
	foregroundMu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			foregroundMu.Lock()
			defer foregroundMu.Unlock()
			if n := foregroundHolds[group]; n > 1 {
				foregroundHolds[group] = n - 1
				return
			}
			delete(foregroundHolds, group)
			// Last hold out arms the linger so the stages this rebuild causes
			// but does not run — the scheduler's link pass and the debounced
			// group-algo pass — are still spawned at foreground caps.
			if w := foregroundLingerWindow(); w > 0 {
				foregroundLinger[group] = foregroundNow().Add(w)
			} else {
				delete(foregroundLinger, group)
			}
		})
	}
}

// GroupIsForeground reports whether work for group should resolve FOREGROUND
// caps: a hold is live, or the post-hold linger has not expired.
//
// Cheap enough to call on every spawn.
func GroupIsForeground(group string) bool {
	_, fg := GroupForegroundState(group)
	return fg
}

// GroupForegroundState reports whether group should resolve FOREGROUND caps,
// and the epoch of the mark that says so (0 when it should not).
//
// Capture the epoch when work is SPAWNED and hand it back to
// ClearGroupForeground when that work completes, so a slow pass reporting in
// late cannot clear a mark that a newer rebuild has since installed.
//
// Every read sweeps ALL expired linger entries, not just this group's. Per-key
// reaping would leak any group that is rebuilt once and then deleted or
// renamed — nothing would ever look it up again to expire it. The map holds one
// entry per recently-rebuilt group, so a full pass is trivial and this is
// genuinely bounded rather than bounded-in-the-comment-only.
func GroupForegroundState(group string) (epoch uint64, foreground bool) {
	if group == "" {
		return 0, false
	}
	foregroundMu.Lock()
	defer foregroundMu.Unlock()
	sweepExpiredLingerLocked()
	if foregroundHolds[group] > 0 {
		return foregroundEpochs[group], true
	}
	if _, ok := foregroundLinger[group]; ok {
		return foregroundEpochs[group], true
	}
	return 0, false
}

// sweepExpiredLingerLocked drops every linger window that has passed, and the
// epoch of any group that is left with neither a hold nor a linger. MUST be
// called with foregroundMu held.
func sweepExpiredLingerLocked() {
	now := foregroundNow()
	for g, exp := range foregroundLinger {
		if !now.Before(exp) {
			delete(foregroundLinger, g)
		}
	}
	for g := range foregroundEpochs {
		if foregroundHolds[g] > 0 {
			continue
		}
		if _, ok := foregroundLinger[g]; ok {
			continue
		}
		delete(foregroundEpochs, g)
	}
}

// ClearGroupForeground ends the post-hold linger for group, but ONLY if epoch
// is still the current mark. Called when the last stage of the user-awaited
// work — the group-algo pass — completes: the graph the user asked for exists,
// so anything that runs from here on is background churn again.
//
// The epoch check is what makes a late completion safe. A pass spawned under
// rebuild #1 can finish long after rebuild #2 has re-marked the group; without
// the check it would clear #2's mark and send #2's own follow-on children to
// background caps while the user is still waiting on them.
//
// It also deliberately does NOT cancel live holds. A hold means a foreground
// rebuild is running right now; only its own release may end it.
func ClearGroupForeground(group string, epoch uint64) {
	foregroundMu.Lock()
	defer foregroundMu.Unlock()
	if foregroundHolds[group] > 0 {
		return
	}
	if foregroundEpochs[group] != epoch {
		return
	}
	delete(foregroundLinger, group)
	delete(foregroundEpochs, group)
}

// ForgetGroupForeground drops all foreground state for group unconditionally.
// For teardown — a group delete — where no epoch can still be current because
// the group itself is gone. Called from Scheduler.CancelGroup, which is the
// path Service.DeleteGroup invokes.
func ForgetGroupForeground(group string) {
	foregroundMu.Lock()
	defer foregroundMu.Unlock()
	delete(foregroundHolds, group)
	delete(foregroundLinger, group)
	delete(foregroundEpochs, group)
}

// resetForegroundGroups drops all foreground state. Test-only seam — production
// state is per-group and self-expiring.
func resetForegroundGroups() {
	foregroundMu.Lock()
	defer foregroundMu.Unlock()
	foregroundHolds = map[string]int{}
	foregroundLinger = map[string]time.Time{}
	foregroundEpochs = map[string]uint64{}
}

// setForegroundClockForTest swaps the clock and returns a restore func. Test-only.
func setForegroundClockForTest(now func() time.Time) (restore func()) {
	foregroundMu.Lock()
	prev := foregroundNow
	foregroundNow = now
	foregroundMu.Unlock()
	return func() {
		foregroundMu.Lock()
		foregroundNow = prev
		foregroundMu.Unlock()
	}
}
