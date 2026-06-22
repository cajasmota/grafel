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
)

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
