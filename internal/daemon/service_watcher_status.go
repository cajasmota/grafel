package daemon

// service_watcher_status.go — the watcher half of Service.Status' reply.
//
// Extracted from Status (#6921) so the mapping can be driven by a stand-in
// watcher. Status itself needs a live *watch.Watcher to have counters worth
// reading, and the one condition that matters most here — a queue overflow —
// cannot be provoked from outside package watch on any platform, and cannot be
// provoked at all on macOS, where the kqueue backend never raises it.
//
// The extraction moves the mapping out of Status but NOT the decision to call
// it: Status still owns the `s.watcher != nil` gate, and a test that builds a
// Service around a real Watcher pins that gate independently — otherwise this
// would be the classic extraction that pins a helper and leaves the call site
// free to skip it.

import (
	"time"

	"github.com/cajasmota/grafel/internal/daemon/proto"
)

// watcherStatusSource is the read-only subset of *watch.Watcher that
// Service.Status reports. Narrow by construction: everything here is a counter
// snapshot, so a stand-in is a struct of fields rather than a simulation.
type watcherStatusSource interface {
	// Stats returns (repos, dirs, totalEvents, dropped, unwatched).
	Stats() (int, int, uint64, uint64, int)
	// FDBudgetStats returns (used, limit, unwatched, unwatchedRepos) (#6180).
	FDBudgetStats() (int, int, int, []string)
	// OverflowStats returns (overflows, rescans, coalesced, last) (#6921).
	OverflowStats() (uint64, uint64, uint64, time.Time)
}

// fillWatcherStatus copies the watcher's counters into the status reply.
func fillWatcherStatus(reply *proto.StatusReply, w watcherStatusSource) {
	repos, dirs, events, dropped, unwatched := w.Stats()
	reply.WatcherRepos = repos
	reply.WatcherDirs = dirs
	reply.WatcherEvents = events
	reply.WatcherDropped = dropped
	// #6180: surface repos that are NOT watched for want of file
	// descriptors, plus the ledger, so `grafel status` says so out loud.
	reply.WatcherUnwatched = unwatched
	fdUsed, fdLimit, _, _ := w.FDBudgetStats()
	reply.WatcherFDUsed = fdUsed
	reply.WatcherFDLimit = fdLimit
	// #6921: an fsnotify queue overflow drops an unknown set of file events,
	// and fsnotify is edge-triggered, so nothing redelivers them. The watcher
	// recovers with a full rescan of every subscribed repo — but the user must
	// be able to SEE that it happened, because a watcher that overflowed and a
	// watcher that is healthy look identical from outside.
	overflows, overflowRescans, _, lastOverflow := w.OverflowStats()
	reply.WatcherOverflows = overflows
	reply.WatcherOverflowRescans = overflowRescans
	if !lastOverflow.IsZero() {
		reply.WatcherLastOverflow = lastOverflow.UTC().Format(time.RFC3339)
	}
}
