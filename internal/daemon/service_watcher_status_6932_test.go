package daemon

// service_watcher_status_6932_test.go — #6932 arm B, the announcement on the
// wire.
//
// The probe exists to be SEEN. A projection that reaches no surface is the same
// defect class as the watcher failures #6921/#6923/#6928 belong to: something
// the daemon knows and the operator cannot. Two levels, as in the #6921 file:
// the mapping, driven by a stand-in, and the CALL, driven by a real watcher.

import (
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/watch"
)

// budgetStub is a watcherStatusSource whose budget report can be set to
// anything, including the exceeded case, which cannot be provoked on a
// developer machine with a real kernel limit.
type budgetStub struct {
	summary string
	notes   []string
	exceeds bool
}

func (budgetStub) Stats() (int, int, uint64, uint64, int)   { return 1, 4, 0, 0, 0 }
func (budgetStub) FDBudgetStats() (int, int, int, []string) { return 0, 0, 0, nil }
func (budgetStub) OverflowStats() (uint64, uint64, uint64, time.Time) {
	return 0, 0, 0, time.Time{}
}
func (b budgetStub) InotifyBudgetReport() (string, []string, bool) {
	return b.summary, b.notes, b.exceeds
}

func TestFillWatcherStatusReportsTheInotifyBudget6932(t *testing.T) {
	var reply proto.StatusReply
	fillWatcherStatus(&reply, budgetStub{
		summary: "inotify budget: 9760 of 8192 watch descriptors — WOULD EXCEED",
		notes:   []string{"the inotify watch pool is per-UID and host-level"},
		exceeds: true,
	})

	if !strings.Contains(reply.InotifyBudgetSummary, "WOULD EXCEED") {
		t.Errorf("InotifyBudgetSummary = %q — the projection never reaches the status reply",
			reply.InotifyBudgetSummary)
	}
	if len(reply.InotifyBudgetNotes) != 1 {
		t.Errorf("InotifyBudgetNotes = %v — the caveats were dropped, leaving the number to be read "+
			"as headroom this process owns", reply.InotifyBudgetNotes)
	}
	if !reply.InotifyBudgetExceeds {
		t.Error("InotifyBudgetExceeds = false for a report that exceeds")
	}
}

// The negative half: a fitting budget must not set the flag, or a `grafel
// status` reader learns to ignore it.
func TestFillWatcherStatusDoesNotClaimExcessWhenItFits6932(t *testing.T) {
	var reply proto.StatusReply
	fillWatcherStatus(&reply, budgetStub{summary: "inotify budget: 100 of 8192 watch descriptors — fits"})
	if reply.InotifyBudgetExceeds {
		t.Error("InotifyBudgetExceeds = true for a budget that fits")
	}
	if len(reply.InotifyBudgetNotes) != 0 {
		t.Errorf("notes invented from nowhere: %v", reply.InotifyBudgetNotes)
	}
}

// The control one level up: a Service holding a REAL watcher publishes a real
// summary. Without it, the mapping above could be a helper Status never calls.
func TestStatusPublishesTheInotifyBudget6932(t *testing.T) {
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
	if !strings.Contains(reply.InotifyBudgetSummary, "inotify budget:") {
		t.Fatalf("StatusReply.InotifyBudgetSummary = %q — the probe is computed and discarded "+
			"before anyone sees it", reply.InotifyBudgetSummary)
	}
	if len(reply.InotifyBudgetNotes) == 0 {
		t.Error("a real report reached the wire with no caveats attached")
	}
}
