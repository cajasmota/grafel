// Package indexstate is a tiny, dependency-free, process-global record of
// whether the daemon currently has a reindex in flight. It exists so the
// in-daemon MCP server (internal/mcp) can surface an `is_indexing` flag in
// grafel_stats without holding a reference to the scheduler (internal/daemon/
// sched) — wiring the live scheduler into the MCP server would create an
// import cycle, since internal/mcp imports internal/daemon for layout paths.
//
// Both the scheduler (writer) and the MCP stats handler (reader) import this
// leaf package. The scheduler calls Set(n) under its lock whenever the number
// of in-flight index jobs changes; readers call Snapshot() for a lock-free,
// race-free view.
//
// Motivation: the dogfooding report (P5) asked for a way to query indexing
// state via grafel_stats instead of polling `ps aux` for hot grafel processes.
package indexstate

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// inFlight is the current number of in-flight index jobs. startedUnixNano is
// the wall-clock start of the CURRENT busy period (set on the 0→>0 edge,
// cleared to 0 on the >0→0 edge). Both are package-global atomics so a reader
// in another package observes a consistent value without any lock.
var (
	inFlight        atomic.Int64
	startedUnixNano atomic.Int64
	// groupAlgoInFlight counts in-flight GROUP-algorithm passes (#5349 A3).
	// Tracked separately from index jobs so is_indexing reflects a background
	// group-algo pass without conflating it with a reactive reindex count.
	groupAlgoInFlight atomic.Int64

	// indexConcActive / indexConcQueued mirror the daemon-wide index-concurrency
	// gate (#5493): how many module/repo index operations are running right now
	// vs. waiting for a free slot. They let `grafel status` / grafel_index_status
	// surface "indexing N, queued M" so a 30-module group draining 2-at-a-time is
	// visible rather than looking stalled. Written by the gate, read lock-free.
	indexConcActive atomic.Int64
	indexConcQueued atomic.Int64
	indexConcCap    atomic.Int64
)

// SetIndexConcurrency publishes the daemon-wide index-concurrency gate's current
// active/queued counts and its configured cap (#5493). Called by the gate on
// every acquire/release. Negative values are clamped to 0. Safe to call from any
// goroutine.
func SetIndexConcurrency(active, queued, cap int) {
	if active < 0 {
		active = 0
	}
	if queued < 0 {
		queued = 0
	}
	if cap < 0 {
		cap = 0
	}
	indexConcActive.Store(int64(active))
	indexConcQueued.Store(int64(queued))
	indexConcCap.Store(int64(cap))
}

// IndexConcurrency is a point-in-time view of the daemon-wide index-concurrency
// gate (#5493).
type IndexConcurrency struct {
	// Active is the number of index operations currently holding a gate slot.
	Active int
	// Queued is the number of index operations waiting for a slot to free.
	Queued int
	// Cap is the configured concurrency limit (GRAFEL_INDEX_CONCURRENCY).
	Cap int
}

// GetIndexConcurrency returns the current gate counts. Lock-free; safe from an
// MCP request handler.
func GetIndexConcurrency() IndexConcurrency {
	return IndexConcurrency{
		Active: int(indexConcActive.Load()),
		Queued: int(indexConcQueued.Load()),
		Cap:    int(indexConcCap.Load()),
	}
}

// Per-repo index freshness (#5433). The scheduler is the single writer; it
// publishes a defensive copy of its per-repo state under its own lock via
// SetRepoStates. The in-daemon MCP server (grafel_index_status) is the reader
// and calls RepoStates lock-free-for-the-caller — a single mutex guards the
// stored slice so a reader never observes a torn write. This is the same
// import-cycle-avoiding bridge pattern as the global is_indexing flag above:
// scheduler (sched) and MCP both import this leaf package, neither imports the
// other.
const (
	// StateCurrent — the repo is fully indexed and idle (no queued/in-flight
	// work and no coalesced dirty marker). An agent gating on its own repo
	// should treat current (ideally with indexed_ref == head_ref) as "ready".
	StateCurrent = "current"
	// StateQueued — an index for this repo is enqueued but not yet running.
	StateQueued = "queued"
	// StateIndexing — an index for this repo is running right now.
	StateIndexing = "indexing"
	// StateDirty — a reindex is in flight AND further changes arrived during
	// it, so a follow-up reindex is already pending (#5138 coalescing).
	StateDirty = "dirty"
)

// RepoState is one repo's index-freshness slice, mirrored from the scheduler.
type RepoState struct {
	// Path is the repo's on-disk path (the scheduler's map key).
	Path string
	// State is one of the State* constants.
	State string
	// IndexedRef is the git ref the last completed index ran against, or empty
	// if the repo has never been indexed in this daemon's lifetime.
	IndexedRef string
	// HeadRef is the ref captured at the latest enqueue (the ref the pending /
	// in-flight work targets), or empty when nothing is pending.
	HeadRef string
	// Dirty is true when a coalesced follow-up reindex is pending (#5138).
	Dirty bool
}

var (
	repoStatesMu sync.RWMutex
	repoStates   []RepoState
)

// SetRepoStates replaces the published per-repo index-state snapshot. The
// scheduler calls this under its own lock immediately after publishing the
// global in-flight count; it passes a freshly built slice (not an alias of any
// internal map) so the stored value is safe to hand to readers. Idempotent and
// safe to call from any goroutine.
func SetRepoStates(states []RepoState) {
	cp := make([]RepoState, len(states))
	copy(cp, states)
	repoStatesMu.Lock()
	repoStates = cp
	repoStatesMu.Unlock()
}

// RepoStates returns a copy of the current per-repo index-state snapshot,
// sorted by Path for determinism. Lock-free for the caller's downstream use
// (it gets its own slice). Safe to call from an MCP request handler.
func RepoStates() []RepoState {
	repoStatesMu.RLock()
	out := make([]RepoState, len(repoStates))
	copy(out, repoStates)
	repoStatesMu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// GroupAlgoBegin records the start of a group-algorithm pass. Safe to call from
// any goroutine; balanced by GroupAlgoEnd (deferred at the call site).
func GroupAlgoBegin() {
	if prev := groupAlgoInFlight.Add(1); prev == 1 && inFlight.Load() == 0 {
		// First activity of an otherwise-idle daemon — stamp the busy-period
		// start so indexing_started_at is populated for a pure group-algo pass.
		startedUnixNano.CompareAndSwap(0, time.Now().UnixNano())
	}
}

// GroupAlgoEnd records the completion of a group-algorithm pass. Clamped at 0.
func GroupAlgoEnd() {
	if n := groupAlgoInFlight.Add(-1); n < 0 {
		groupAlgoInFlight.Store(0)
	}
}

// Set records the current number of in-flight index jobs. It is idempotent and
// safe to call from any goroutine. On the transition into a busy period
// (previous count 0, new count > 0) it stamps the start time; on the
// transition back to idle it clears the stamp. A negative n is clamped to 0.
func Set(n int) {
	if n < 0 {
		n = 0
	}
	prev := inFlight.Swap(int64(n))
	switch {
	case prev == 0 && n > 0:
		startedUnixNano.Store(time.Now().UnixNano())
	case n == 0:
		startedUnixNano.Store(0)
	}
}

// Snapshot is a point-in-time view of the indexing state.
type Snapshot struct {
	// IsIndexing is true when at least one index job OR a group-algorithm pass
	// is in flight.
	IsIndexing bool
	// InFlight is the number of index jobs currently running.
	InFlight int
	// GroupAlgoInFlight is the number of group-algorithm passes currently
	// running (#5349 A3).
	GroupAlgoInFlight int
	// StartedAt is the wall-clock start of the current busy period, or the
	// zero Time when idle.
	StartedAt time.Time
}

// Get returns the current indexing state. Lock-free and safe to call from any
// goroutine, including an MCP request handler.
func Get() Snapshot {
	n := inFlight.Load()
	ga := groupAlgoInFlight.Load()
	s := Snapshot{
		IsIndexing:        n > 0 || ga > 0,
		InFlight:          int(n),
		GroupAlgoInFlight: int(ga),
	}
	if started := startedUnixNano.Load(); started > 0 {
		s.StartedAt = time.Unix(0, started)
	}
	return s
}
