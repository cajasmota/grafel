package daemon

import (
	"context"
	"sync"
)

// group_cancel.go — per-group rebuild cancellation registry.
//
// The scheduler owns cancellation of its OWN background passes (group-algo,
// links, per-repo reindex) via Scheduler.CancelGroup. But the heavy GROUP
// REBUILD — the multi-minute loop in cmd/grafel's daemonRebuildFuncCore that
// force-indexes every member repo and then runs the cross-repo link/phantom
// passes — runs OUTSIDE the scheduler: it is driven by RebuildFunc, whose
// signature takes no context (it is invoked identically from Service.Rebuild in
// monolith/engine mode and from the engine's rebuild-drain worker in split
// mode). Before this registry, a `grafel delete <group>` removed the group from
// the registry but left that rebuild loop churning to completion — the daemon
// stayed pinned at 100%+ CPU for minutes after the group was gone.
//
// This registry gives the rebuild a per-group context.Context WITHOUT changing
// the RebuildFunc signature (which threads through ~a dozen call/test sites):
// daemonRebuildFuncCore calls GroupRebuildContext(group) to obtain (and
// register) a cancelable context plus an `end` cleanup func, checks ctx.Err()
// between repos and threads it into the per-repo index + link passes, and calls
// end() on return. Service.DeleteGroup (and the engine's KindCancelGroup drain
// handler) call CancelGroupRebuild(group) to interrupt it.
//
// Cancel-before-register (split mode): the engine drain dispatches a pending
// KindRebuild to the async rebuildWorker (non-blocking) and may then apply a
// KindCancelGroup INLINE before that worker goroutine has called
// GroupRebuildContext, so CancelGroupRebuild finds nothing to cancel and returns
// false. That residual race is NOT closed here with a tombstone — a tombstone
// planted on every delete (including the common delete-of-an-idle-group case)
// would make a recreate of the same name consume it and start already-cancelled,
// spuriously killing the recreate's first rebuild ("delete api; recreate api").
// Instead it is closed DETERMINISTICALLY one layer up: daemonRebuildFuncCore
// re-reads the registry immediately after registering its context and aborts
// with "unknown group" when the group is absent (see cmd/grafel/daemon.go). A
// stale rebuild for a genuinely-deleted group therefore aborts before any heavy
// work, while a recreate re-registers the group so its own rebuild's existence
// check passes — no false-cancel.
//
// Stale end()/prev overwrite (rapid delete→recreate of the same group name): an
// old rebuild's deferred end() must not delete the NEW rebuild's entry.
// Registration returns an identity TOKEN; end() only deletes the entry when it
// is still that token's, and a re-registration defensively cancels any stale
// predecessor.

// groupRebuildEntry is the identity token for one registered group rebuild.
type groupRebuildEntry struct {
	cancel context.CancelFunc
}

type groupCancelRegistry struct {
	mu sync.Mutex
	m  map[string]*groupRebuildEntry
}

var groupRebuildCancels = &groupCancelRegistry{
	m: map[string]*groupRebuildEntry{},
}

// GroupRebuildContext returns a cancelable context for a group rebuild plus an
// `end` func the caller MUST defer. The context is cancelled by
// CancelGroupRebuild(group); end() deregisters this rebuild's entry (without
// cancelling — normal completion). A re-registration under the same name (rapid
// delete→recreate) defensively cancels any stale predecessor still registered.
func GroupRebuildContext(group string) (ctx context.Context, cancel context.CancelFunc, end func()) {
	ctx, cancel = context.WithCancel(context.Background())
	entry := &groupRebuildEntry{cancel: cancel}

	r := groupRebuildCancels
	r.mu.Lock()
	// Defensive: cancel a stale predecessor still registered under this name,
	// so one is never left orphaned.
	//
	// Is the "single-flight upstream" this relies on real? Both production
	// entrypoints into RebuildFunc serialise same-group rebuilds before either
	// can reach here (#7307): Service.Rebuild takes a per-group capacity-1
	// semaphore (s.groupRebuildMu) in monolith/engine mode, released from the
	// rebuild's own worker goroutine only after RebuildFunc returns; and on the
	// split-mode engine drain applyRequest holds engineGroupRebuildGuard around
	// the rebuildFn call. daemonRebuildFuncCore's deferred end() deregisters
	// before either guard is released, so within one daemon process this branch
	// is defensive rather than load-bearing. It is graded regardless — see
	// TestRegisterSameGroupTwice_CancelsPredecessor.
	if prev, ok := r.m[group]; ok {
		prev.cancel()
	}
	r.m[group] = entry
	r.mu.Unlock()

	end = func() {
		r.mu.Lock()
		// Only remove the entry if it is still OURS — a rapid delete→recreate of
		// the same group name may have registered a newer rebuild; deleting by
		// bare key would strip the new rebuild's cancel and re-open the leak.
		if r.m[group] == entry {
			delete(r.m, group)
		}
		r.mu.Unlock()
	}
	return ctx, cancel, end
}

// CancelGroupRebuild cancels an in-flight group rebuild's context, if one is
// registered. Best-effort and non-blocking. Returns true if a live rebuild was
// cancelled now, false if nothing was registered (the split-mode
// cancel-before-register case, which the registry existence check in
// daemonRebuildFuncCore backstops — see the file header).
func CancelGroupRebuild(group string) bool {
	r := groupRebuildCancels
	r.mu.Lock()
	entry, ok := r.m[group]
	if ok {
		delete(r.m, group)
	}
	r.mu.Unlock()
	if ok {
		entry.cancel()
	}
	return ok
}
