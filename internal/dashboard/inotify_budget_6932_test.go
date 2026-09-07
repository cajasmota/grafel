package dashboard

// inotify_budget_6932_test.go — #6932 arm B, the /diagnostics half of the
// announcement. Graded at the CALL SITE (buildDaemonDiagnostics), so deleting
// the fill fails here rather than leaving a struct field nobody populates.

import (
	"strings"
	"testing"
	"time"
)

type budgetWatcher struct {
	summary string
	notes   []string
	exceeds bool
}

func (budgetWatcher) ForceRescan()                             {}
func (budgetWatcher) Stats() (int, int, uint64, uint64, int)   { return 1, 4, 0, 0, 0 }
func (budgetWatcher) FDBudgetStats() (int, int, int, []string) { return 0, 0, 0, nil }
func (budgetWatcher) OverflowStats() (uint64, uint64, uint64, time.Time) {
	return 0, 0, 0, time.Time{}
}
func (b budgetWatcher) InotifyBudgetReport() (string, []string, bool) {
	return b.summary, b.notes, b.exceeds
}

func TestDaemonDiagnosticsReportsTheInotifyBudget6932(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	s := &Server{watcher: budgetWatcher{
		summary: "inotify budget: 9760 of 8192 watch descriptors — WOULD EXCEED",
		notes:   []string{"the inotify watch pool is per-UID and host-level"},
		exceeds: true,
	}}

	d := s.buildDaemonDiagnostics()

	if !strings.Contains(d.InotifyBudgetSummary, "WOULD EXCEED") {
		t.Errorf("InotifyBudgetSummary = %q — /diagnostics does not report the probe at all",
			d.InotifyBudgetSummary)
	}
	if len(d.InotifyBudgetNotes) != 1 {
		t.Errorf("InotifyBudgetNotes = %v — the caveats that keep the number honest were dropped",
			d.InotifyBudgetNotes)
	}
	if !d.InotifyBudgetExceeds {
		t.Error("InotifyBudgetExceeds = false for a report that exceeds")
	}
}
