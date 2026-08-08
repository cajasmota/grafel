// Package watch — WatcherManager: pause/resume fsnotify subscriptions per
// (repoPath, ref) when a tier slot transitions HOT/WARM → COLD or vice-versa.
//
// PH2a of epic #2087 (#2096).
// M2 of epic #2175 (#2179): lazy subscription + per-group idle pause.
//
// Design: the daemon uses a single shared fsnotify Watcher that subscribes
// entire repo trees. "Pausing" a slot means removing that repo's directory
// tree from the fsnotify subscription and recording it in the paused set.
// "Resuming" re-adds it. Because the Watcher already owns
// AddRepo/RemoveRepo, we delegate to those APIs rather than introducing a
// second Watcher instance.
//
// A single repoPath may be referenced by multiple refs (branches). The
// fsnotify subscription is per-repo-path, not per-ref. We therefore track a
// reference count: the subscription stays alive as long as at least one ref
// for the repo is not paused. The subscription is removed only when all refs
// for a repo are paused, and restored when the first ref for that repo is
// resumed.
//
// M2 lazy subscription: the daemon boots with ZERO fsnotify subscriptions.
// SubscribeGroup wires a group's repos on the first MCP query that touches
// that group. Resume also lazily subscribes a repo whose refCount was 0 (i.e.
// it was never subscribed, not merely paused). When all refs for a repo
// become COLD (via Pause), the fsnotify subscription is removed. On the next
// query, Resume re-subscribes — making the full cycle:
//
//	boot (no subscriptions)
//	→ first query → SubscribeGroup / Resume → AddRepo
//	→ idle TTL → Pause (WARM→COLD) → RemoveRepo
//	→ re-query → Resume → AddRepo
package watch

import (
	"log/slog"
	"os"
	"sync"
	"time"
)

// Manager is the narrow interface the tier package uses to pause/resume
// per-(repoPath,ref) watcher subscriptions. Implemented by DefaultManager.
type Manager interface {
	// Pause marks (repoPath, ref) as paused. When this call causes all refs
	// for the given repoPath to become paused, the fsnotify subscription for
	// that repo tree is removed.
	Pause(repoPath, ref string)
	// Resume marks (repoPath, ref) as active. When the repo was fully
	// unsubscribed (all refs paused or never subscribed), the fsnotify
	// subscription is added via AddRepo.
	// Returns the time taken for the resume operation.
	Resume(repoPath, ref string) time.Duration
	// IsPaused reports whether (repoPath, ref) is currently paused.
	IsPaused(repoPath, ref string) bool
	// ActiveCount returns the number of (repoPath, ref) pairs currently active.
	ActiveCount() int
	// PausedCount returns the number of (repoPath, ref) pairs currently paused.
	PausedCount() int
}

// slotState tracks per-ref pause state.
type slotState struct {
	paused bool
}

// DefaultManager wraps a *Watcher and implements Manager.
// It is goroutine-safe.
type DefaultManager struct {
	watcher *Watcher
	logger  *slog.Logger

	mu sync.Mutex
	// slots maps "repoPath\x00ref" → slotState
	slots map[string]*slotState
	// refCounts maps repoPath → number of active (non-paused) refs.
	// A refCount of 0 means the repo is either paused or never subscribed.
	// The watcher tracks actual subscriptions via its own repos map.
	refCounts map[string]int
	// groups maps groupName → []repoPath (M2: for SubscribeGroup tracking).
	// Populated by SubscribeGroup; used to report SubscribedGroupCount.
	groups map[string][]string
}

// NewDefaultManager creates a DefaultManager backed by w.
// logger may be nil.
func NewDefaultManager(w *Watcher, logger *slog.Logger) *DefaultManager {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil)).With("pkg", "watch-mgr")
	}
	return &DefaultManager{
		watcher:   w,
		logger:    logger,
		slots:     make(map[string]*slotState),
		refCounts: make(map[string]int),
		groups:    make(map[string][]string),
	}
}

func slotKey(repoPath, ref string) string {
	return repoPath + "\x00" + ref
}

// Register declares that (repoPath, ref) is tracked by the manager but does
// NOT eagerly subscribe the fsnotify watcher. M2: boot-time calls should only
// declare known slots; actual fsnotify subscription happens lazily via
// SubscribeGroup or the first Resume call when the ref transitions COLD→HOT.
//
// Register is idempotent: re-registering a known slot is a no-op. If the slot
// was previously paused, Register does NOT un-pause it — use Resume for that.
func (m *DefaultManager) Register(repoPath, ref string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := slotKey(repoPath, ref)
	if _, exists := m.slots[k]; exists {
		return // idempotent
	}
	// Mark the slot as paused=false (active) in the slot table, but do NOT
	// increment refCounts or call AddRepo. refCounts stays 0 until the first
	// Resume or SubscribeGroup call — that is the M2 lazy-subscribe signal.
	//
	// Note: this differs from the pre-M2 behavior where Register incremented
	// refCounts (assuming AddRepo had already been called by the boot path).
	// Now Register is purely declarative; fsnotify subscription is deferred.
	m.slots[k] = &slotState{paused: true} // treat as paused until subscribed
}

// SubscribeGroup lazily subscribes all repos in a named group. This is the
// M2 group-level subscription entry point: call it on the first MCP query that
// touches groupName. Repos that are already subscribed (refCount > 0) are
// skipped to preserve idempotency.
//
// SubscribeGroup records the group→repoPaths mapping so SubscribedGroupCount
// can report how many groups have live fsnotify subscriptions.
//
// Returns the number of repos newly subscribed (dirs added to fsnotify).
//
// NOTE (#6269): this is a first-subscribe path just like Resume's wasZero
// branch, and it does NOT fire the catch-up rescan that branch fires — a repo
// subscribed here still misses whatever was edited before it was subscribed.
// That is currently harmless only because SubscribeGroupWatcher in
// cmd/grafel/daemon_tier.go, the sole production entry point to this method,
// has no callers; the live subscribe path is Resume. Re-wire it and #6269
// comes back here.
func (m *DefaultManager) SubscribeGroup(groupName string, repoPaths []string) int {
	if len(repoPaths) == 0 {
		return 0
	}

	// Snapshot which repos need subscribing (those with refCount == 0).
	m.mu.Lock()
	m.groups[groupName] = repoPaths
	var toSubscribe []string
	for _, rp := range repoPaths {
		if m.refCounts[rp] == 0 {
			toSubscribe = append(toSubscribe, rp)
		}
	}
	// Pre-increment so concurrent SubscribeGroup calls for the same repo don't
	// double-add. We will decrement if AddRepo fails.
	for _, rp := range toSubscribe {
		m.refCounts[rp]++
		// Ensure the sentinel slot exists so Pause/Resume bookkeeping is correct.
		sentinelKey := slotKey(rp, "")
		if _, ok := m.slots[sentinelKey]; !ok {
			m.slots[sentinelKey] = &slotState{paused: false}
		} else {
			m.slots[sentinelKey].paused = false
		}
	}
	m.mu.Unlock()

	added := 0
	for _, rp := range toSubscribe {
		n, err := m.watcher.AddRepo(rp)
		if err != nil {
			m.logger.Error("watcher-mgr: SubscribeGroup AddRepo failed", "group", groupName, "repo", rp, "err", err)
			// Decrement on failure so the next call retries.
			m.mu.Lock()
			m.refCounts[rp]--
			m.slots[slotKey(rp, "")].paused = true
			m.mu.Unlock()
			continue
		}
		m.logger.Info("watcher-mgr: SubscribeGroup lazy subscribe", "group", groupName, "repo", rp, "dirs", n)
		added += n
	}
	return added
}

// SubscribedGroupCount returns the number of distinct groups that currently
// have at least one repo with an active fsnotify subscription (refCount > 0).
func (m *DefaultManager) SubscribedGroupCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, repos := range m.groups {
		for _, rp := range repos {
			if m.refCounts[rp] > 0 {
				count++
				break // one active repo is enough to count this group
			}
		}
	}
	return count
}

// SubscribedRepoCount returns the number of distinct repos with at least one
// active (non-paused) ref, i.e. refCount > 0. Used for observability.
func (m *DefaultManager) SubscribedRepoCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, rc := range m.refCounts {
		if rc > 0 {
			count++
		}
	}
	return count
}

// Pause marks (repoPath, ref) paused. If this is the last active ref for
// the repo, the fsnotify subscription is removed.
func (m *DefaultManager) Pause(repoPath, ref string) {
	start := time.Now()
	m.mu.Lock()
	k := slotKey(repoPath, ref)
	st, known := m.slots[k]
	if !known {
		st = &slotState{}
		m.slots[k] = st
	}
	if st.paused {
		m.mu.Unlock()
		return // already paused — idempotent
	}
	st.paused = true
	m.refCounts[repoPath]--
	removeRepo := m.refCounts[repoPath] <= 0
	if removeRepo {
		m.refCounts[repoPath] = 0
	}
	m.mu.Unlock()

	if removeRepo {
		m.watcher.RemoveRepo(repoPath)
		m.logger.Info("watcher-mgr: paused — fsnotify unsubscribed",
			"repo", repoPath, "ref", ref, "elapsed", time.Since(start).Round(time.Microsecond))
	} else {
		m.logger.Info("watcher-mgr: paused — other refs still active", "ref", ref, "repo", repoPath)
	}
}

// Resume marks (repoPath, ref) active. If the repo was fully unsubscribed
// (refCount == 0, either because all refs were paused or because it was never
// subscribed at boot), the fsnotify subscription is added via AddRepo.
//
// M2: this is also the lazy-subscribe path for the first cold-wake of a slot
// that was registered at boot via RegisterCold (S1) but never subscribed to
// fsnotify. The refCount check handles both the re-subscribe and first-subscribe
// cases identically.
//
// Returns the time taken for the resume.
func (m *DefaultManager) Resume(repoPath, ref string) time.Duration {
	start := time.Now()
	m.mu.Lock()
	k := slotKey(repoPath, ref)
	st, known := m.slots[k]
	if !known {
		st = &slotState{paused: true}
		m.slots[k] = st
	}
	if !st.paused {
		m.mu.Unlock()
		return time.Since(start) // already active — idempotent
	}
	st.paused = false
	wasZero := m.refCounts[repoPath] == 0
	m.refCounts[repoPath]++
	m.mu.Unlock()

	if wasZero {
		// Subscribe (or re-subscribe after idle unsubscription).
		n, err := m.watcher.AddRepo(repoPath)
		elapsed := time.Since(start)
		if err != nil {
			// Nothing got subscribed, so undo this call's bookkeeping: the
			// increment above and the un-pause. SubscribeGroup already does
			// exactly this on its own AddRepo failure (see its decrement and
			// paused=true above); Resume did not, and the asymmetry is a leak.
			//
			// Without the undo, refCounts stays at 1 while the watcher holds no
			// entry for the repo (AddRepo removes it again on the FD-budget
			// path). Every later Resume then reads wasZero == false, takes the
			// "subscription already active" path, and never retries AddRepo —
			// so the repo is permanently unwatched AND permanently reported as
			// watched by ActiveCount/IsPaused. Restoring both fields makes the
			// next Resume retry the subscription.
			m.mu.Lock()
			if m.refCounts[repoPath] > 0 {
				m.refCounts[repoPath]--
			}
			st.paused = true
			m.mu.Unlock()
			m.logger.Error("watcher-mgr: resume AddRepo failed",
				"repo", repoPath, "ref", ref, "err", err, "elapsed", elapsed.Round(time.Microsecond))
		} else {
			m.logger.Info("watcher-mgr: resumed",
				"repo", repoPath, "ref", ref, "dirs", n, "elapsed", elapsed.Round(time.Microsecond))
			// #6269: the subscription was absent until this call, so no event
			// was delivered for anything edited in the meantime — AddRepo
			// starts the stream, it does not replay it. Ask for a full
			// reconciliation of the repo so those edits are picked up.
			//
			// Only on this branch: the refcount-bump path below did not
			// re-subscribe, so its subscription never lapsed and there is
			// nothing to catch up on.
			//
			// RescanRepo dispatches the sink on its own goroutine — see the
			// latency note on its declaration.
			m.watcher.RescanRepo(repoPath)
		}
		return elapsed
	}
	m.logger.Info("watcher-mgr: resumed — subscription already active", "ref", ref, "repo", repoPath)
	return time.Since(start)
}

// IsPaused reports whether (repoPath, ref) is currently paused.
func (m *DefaultManager) IsPaused(repoPath, ref string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.slots[slotKey(repoPath, ref)]
	if !ok {
		return false
	}
	return st.paused
}

// ActiveCount returns the number of slot entries that are not paused.
func (m *DefaultManager) ActiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, st := range m.slots {
		if !st.paused {
			n++
		}
	}
	return n
}

// PausedCount returns the number of slot entries that are paused.
func (m *DefaultManager) PausedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, st := range m.slots {
		if st.paused {
			n++
		}
	}
	return n
}
