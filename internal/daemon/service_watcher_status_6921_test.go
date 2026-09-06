package daemon

// service_watcher_status_6921_test.go — #6921, the status-reply fill.
//
// Two levels, deliberately:
//
//	fillWatcherStatus  — the mapping, driven by a stand-in whose counters can be
//	                     set to anything. A queue overflow cannot be provoked
//	                     from this package (handleOverflow is unexported) and
//	                     cannot be provoked AT ALL on macOS, where fsnotify's
//	                     kqueue backend never raises ErrEventOverflow.
//	Service.Status     — the CALL, driven by a real *watch.Watcher. Extracting
//	                     the mapping would otherwise leave Status free to stop
//	                     calling it, with every mapping assertion still green.

import (
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/watch"
)

// stubWatcherStatus is a watcherStatusSource with fixed counters.
type stubWatcherStatus struct {
	overflows, rescans, coalesced uint64
	last                          time.Time
}

func (stubWatcherStatus) Stats() (int, int, uint64, uint64, int)   { return 2, 9, 400, 3, 1 }
func (stubWatcherStatus) FDBudgetStats() (int, int, int, []string) { return 10, 20, 1, nil }
func (s stubWatcherStatus) OverflowStats() (uint64, uint64, uint64, time.Time) {
	return s.overflows, s.rescans, s.coalesced, s.last
}

func TestFillWatcherStatusReportsQueueOverflows6921(t *testing.T) {
	last := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	var reply proto.StatusReply
	fillWatcherStatus(&reply, stubWatcherStatus{overflows: 4, rescans: 2, coalesced: 2, last: last})

	if reply.WatcherOverflows != 4 || reply.WatcherOverflowRescans != 2 {
		t.Errorf("overflow counters = (%d, %d), want (4, 2) — without them `grafel status` cannot "+
			"distinguish a watcher that dropped a window of events from a healthy one",
			reply.WatcherOverflows, reply.WatcherOverflowRescans)
	}
	if reply.WatcherLastOverflow != "2026-09-06T12:00:00Z" {
		t.Errorf("WatcherLastOverflow = %q, want RFC3339 2026-09-06T12:00:00Z", reply.WatcherLastOverflow)
	}
	// The pre-existing fields must survive the extraction untouched.
	if reply.WatcherRepos != 2 || reply.WatcherDirs != 9 || reply.WatcherEvents != 400 ||
		reply.WatcherDropped != 3 || reply.WatcherUnwatched != 1 ||
		reply.WatcherFDUsed != 10 || reply.WatcherFDLimit != 20 {
		t.Errorf("a pre-existing watcher field was dropped by the extraction: %+v", reply)
	}
}

// TestFillWatcherStatusOmitsOverflowTimeWhenNoneHappened: a watcher that never
// overflowed must not publish a zero-value timestamp, which renders as an
// overflow in year 1.
func TestFillWatcherStatusOmitsOverflowTimeWhenNoneHappened6921(t *testing.T) {
	var reply proto.StatusReply
	fillWatcherStatus(&reply, stubWatcherStatus{})
	if reply.WatcherLastOverflow != "" {
		t.Errorf("WatcherLastOverflow = %q, want empty", reply.WatcherLastOverflow)
	}
}

// TestStatusCallsTheWatcherFill is the control one level up: a Service holding
// a REAL watcher with one subscribed repo must report it. This is what stops
// the extraction above from being a helper nobody calls.
func TestStatusCallsTheWatcherFill6921(t *testing.T) {
	w, err := watch.NewWatcher(time.Hour, func(string, bool) {}, nil)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	t.Cleanup(w.Stop)
	if _, err := w.AddRepo(t.TempDir()); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}

	s := &Service{watcher: w, startedAt: time.Now()}
	var reply proto.StatusReply
	if err := s.Status(&proto.StatusArgs{}, &reply); err != nil {
		t.Fatalf("Status: %v", err)
	}
	if reply.WatcherRepos != 1 {
		t.Fatalf("StatusReply.WatcherRepos = %d, want 1 — Service.Status is not reporting the "+
			"watcher at all, so every watcher counter (overflows included) is computed and "+
			"then discarded before anyone sees it", reply.WatcherRepos)
	}
}
