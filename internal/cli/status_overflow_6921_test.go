package cli

// status_overflow_6921_test.go — #6921, the print gate for fsnotify queue
// overflows.
//
// The failure being reported is silent staleness: an overflow drops an unknown
// set of file events, fsnotify never redelivers them, and the watcher keeps
// reporting healthy. `grafel status` is where a user looks when the graph does
// not match the tree, so a number that reaches StatusReply and is never printed
// is the same defect #6014 was — right on the wire, absent from the screen.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon/proto"
)

// TestPrintDaemonDetail_OverflowLinePrintsWithNoWatcherActivity is the
// regression AND the gate assertion. WatcherRepos/WatcherEvents are zero here
// on purpose: the overflow line must not be nested inside the `watcher:`
// activity block, because that block is gated on counters describing events
// that DID arrive, and an overflow is about events that did not.
func TestPrintDaemonDetail_OverflowLinePrintsWithNoWatcherActivity(t *testing.T) {
	var buf bytes.Buffer
	printDaemonDetail(&buf, proto.StatusReply{
		WatcherRepos:           0,
		WatcherEvents:          0,
		WatcherOverflows:       3,
		WatcherOverflowRescans: 1,
		WatcherLastOverflow:    "2026-09-06T12:00:00Z",
	})
	out := buf.String()

	for _, want := range []string{
		"3 fsnotify queue overflow(s)",
		"DROPPED",
		"1 full rescan(s)",
		"2026-09-06T12:00:00Z",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("`grafel status` output is missing %q — an overflow the user cannot see is "+
				"the actual defect in #6921.\ngot:\n%s", want, out)
		}
	}
}

// TestPrintDaemonDetail_NoOverflowLineWhenNoneHappened: the line is a warning,
// so it must be silent in the overwhelmingly common case. A line that always
// prints trains the reader to ignore it.
func TestPrintDaemonDetail_NoOverflowLineWhenNoneHappened(t *testing.T) {
	var buf bytes.Buffer
	printDaemonDetail(&buf, proto.StatusReply{
		WatcherRepos:  2,
		WatcherEvents: 400,
	})
	if out := buf.String(); strings.Contains(out, "overflow") {
		t.Errorf("overflow line printed for a watcher that never overflowed:\n%s", out)
	}
}
