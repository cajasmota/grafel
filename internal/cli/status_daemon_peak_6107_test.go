package cli

// status_daemon_peak_6107_test.go — #6107, the same shape as #6014 next door:
// a number that is right on the wire and absent on the screen.
//
// LastPeakMB reaches proto.IndexedRepoState and was never rendered. That became
// load-bearing when the 30-day TTL was removed from RSSHistory: a recorded peak
// now has no automatic way to clear, so the only remedy for a bad entry is an
// operator deleting it by hand — which requires being able to see it first.
// LastPeakSrc rides along because the peak survives runs that measured nothing,
// so without the src a value carried over from an older full index is
// indistinguishable from a fresh one.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon/proto"
)

func TestPrintDaemonDetail_RendersLastPeakWithSource(t *testing.T) {
	var buf bytes.Buffer
	printDaemonDetail(&buf, proto.StatusReply{
		IndexedRepos: []proto.IndexedRepoState{{
			Path:        "/repo-a",
			LastPeakMB:  1477,
			LastPeakSrc: "child_maxrss",
		}},
	})
	out := buf.String()

	if !strings.Contains(out, "last_peak=1477MB") {
		t.Errorf("`grafel status` does not show the measured peak that governs this repo's "+
			"admission. With no TTL there is no automatic way for a wrong entry to clear, so "+
			"an invisible number is also an uncorrectable one.\ngot:\n%s", out)
	}
	if !strings.Contains(out, "child_maxrss") {
		t.Errorf("peak rendered without its source: a peak carried over from an older full "+
			"index reads identically to a fresh one.\ngot:\n%s", out)
	}
}

// TestPrintDaemonDetail_NoPeakLineWhenUnmeasured keeps the renderer honest in
// the same way the completion line is: absent rather than zero. A repo that has
// only ever been reindexed incrementally has no peak, and printing
// "last_peak=0MB" would assert it costs no memory.
func TestPrintDaemonDetail_NoPeakLineWhenUnmeasured(t *testing.T) {
	var buf bytes.Buffer
	printDaemonDetail(&buf, proto.StatusReply{
		IndexedRepos: []proto.IndexedRepoState{{Path: "/repo-b"}},
	})
	if out := buf.String(); strings.Contains(out, "last_peak") {
		t.Errorf("rendered a peak for a repo that never had one measured:\n%s", out)
	}
}
