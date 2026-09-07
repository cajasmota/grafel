package cli

// status_inotify_6932_test.go — #6932 arm B, the print gate for the inotify
// budget probe.
//
// A number on the wire that is never printed is the #6014 defect. The caveats
// are printed WITH the number rather than summarised away, because the number
// alone reads as headroom the daemon owns — and the pool is per-UID and shared
// with every other process running as the same user.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon/proto"
)

func TestPrintDaemonDetail_PrintsInotifyBudgetWithItsCaveats(t *testing.T) {
	var buf bytes.Buffer
	printDaemonDetail(&buf, proto.StatusReply{
		// Zero watcher activity on purpose: the budget line must not be
		// nested inside the `watcher:` block, whose gate is about events that
		// arrived and says nothing about what the watch set costs.
		InotifyBudgetSummary: "inotify budget: 9760 of 8192 watch descriptors (projected, 9760 dirs across 10 repos) — WOULD EXCEED",
		InotifyBudgetNotes:   []string{"the inotify watch pool is per-UID and host-level", "demand is PROJECTED from a directory walk"},
		InotifyBudgetExceeds: true,
	})
	out := buf.String()

	for _, want := range []string{
		"9760 of 8192 watch descriptors",
		"WOULD EXCEED",
		"per-UID and host-level",
		"PROJECTED from a directory walk",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("`grafel status` output is missing %q — a budget the user cannot see is no "+
				"improvement on a watcher nobody can see failing.\ngot:\n%s", want, out)
		}
	}
}

// A daemon whose reply carries no probe (an older daemon, or one with no
// watcher) prints nothing rather than an empty line or a fabricated zero.
func TestPrintDaemonDetail_NoInotifyLineWithoutAProbe(t *testing.T) {
	var buf bytes.Buffer
	printDaemonDetail(&buf, proto.StatusReply{WatcherRepos: 2, WatcherEvents: 400})
	if out := buf.String(); strings.Contains(out, "inotify") {
		t.Errorf("inotify budget line printed with no probe in the reply:\n%s", out)
	}
}
