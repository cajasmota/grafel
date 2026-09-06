package dashboard

// watcher_overflow_6921_test.go — #6921, the diagnostics fill.
//
// An fsnotify queue overflow drops file events permanently (fsnotify is
// edge-triggered), and the watcher goes on reporting healthy through it. The
// daemon now recovers with a full rescan, but a recovery the user cannot see is
// still the defect: "my graph does not match my tree" has to be answerable.
//
// Graded at the CALL SITE — buildDaemonDiagnostics, the method that actually
// fills the payload — rather than on a helper, so deleting the fill fails here.

import (
	"testing"
	"time"
)

// overflowingWatcher is a watcherForceRescan that reports one overflow and one
// recovery rescan. Every other method returns zeros: this test is about the
// overflow fields and nothing else.
type overflowingWatcher struct {
	overflows, rescans, coalesced uint64
	last                          time.Time
}

func (overflowingWatcher) ForceRescan()                           {}
func (overflowingWatcher) Stats() (int, int, uint64, uint64, int) { return 0, 0, 0, 0, 0 }
func (overflowingWatcher) FDBudgetStats() (int, int, int, []string) {
	return 0, 0, 0, nil
}
func (o overflowingWatcher) OverflowStats() (uint64, uint64, uint64, time.Time) {
	return o.overflows, o.rescans, o.coalesced, o.last
}

func TestDaemonDiagnosticsReportsQueueOverflows6921(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	last := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	s := &Server{watcher: overflowingWatcher{overflows: 3, rescans: 1, coalesced: 2, last: last}}

	d := s.buildDaemonDiagnostics()

	if d.WatcherOverflows != 3 {
		t.Errorf("WatcherOverflows = %d, want 3 — the count is the only signal that a window of "+
			"file events was dropped; without it an overflow is indistinguishable from a healthy watcher",
			d.WatcherOverflows)
	}
	if d.WatcherOverflowRescans != 1 {
		t.Errorf("WatcherOverflowRescans = %d, want 1 — the count says whether anything RECOVERED "+
			"the dropped events", d.WatcherOverflowRescans)
	}
	if d.WatcherLastOverflow != "2026-09-06T12:00:00Z" {
		t.Errorf("WatcherLastOverflow = %q, want RFC3339 2026-09-06T12:00:00Z", d.WatcherLastOverflow)
	}
}

// TestDaemonDiagnosticsOmitsOverflowTimeWhenNoneHappened: a watcher that has
// never overflowed must not publish a zero-value timestamp, which would read as
// "an overflow happened in year 1".
func TestDaemonDiagnosticsOmitsOverflowTimeWhenNoneHappened6921(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	s := &Server{watcher: overflowingWatcher{}}

	d := s.buildDaemonDiagnostics()

	if d.WatcherOverflows != 0 || d.WatcherOverflowRescans != 0 {
		t.Errorf("counters non-zero on a watcher that never overflowed: %+v", d)
	}
	if d.WatcherLastOverflow != "" {
		t.Errorf("WatcherLastOverflow = %q, want empty for a watcher that never overflowed",
			d.WatcherLastOverflow)
	}
}
