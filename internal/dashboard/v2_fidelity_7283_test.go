package dashboard

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/quality"
)

// v2_fidelity_7283_test.go — an unmeasured bug rate must not reach the
// fidelity derivation as a zero (#7283).

// writeHistoryEntries7283 writes entries to a throwaway history root.
func writeHistoryEntries7283(t *testing.T, entries ...quality.HealthEntry) string {
	t.Helper()
	root := t.TempDir()
	for _, e := range entries {
		if err := quality.AppendEntry(root, e); err != nil {
			t.Fatalf("AppendEntry: %v", err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "health-history.jsonl")); err != nil {
		t.Fatalf("history file not written: %v", err)
	}
	return root
}

// TestLatestGroupBugRate_UnmeasuredEntryIsNotZero pins the read side of the
// record. An entry the rebuild wrote when no repo's graph could be scanned
// carries no bug rate; returning (0, true) for it would render the group at
// fidelity 1.0 / healthy off a measurement that never happened.
//
// Two entries, not one: the most-recent-wins selection must still be the thing
// being exercised, so the unmeasured row has to displace a measured one.
func TestLatestGroupBugRate_UnmeasuredEntryIsNotZero(t *testing.T) {
	base := time.Now().UTC().Add(-48 * time.Hour)
	measured := 20.0
	health := 75.0
	root := writeHistoryEntries7283(t,
		quality.HealthEntry{
			Timestamp: base, Group: "g", TotalEntities: 100,
			OrphanRate: 5, BugRate: &measured, HealthScore: &health,
		},
		quality.HealthEntry{
			// Every repo's scan failed: no bug rate, no health score.
			Timestamp: base.Add(time.Hour), Group: "g",
		},
	)

	// Control: the measured row alone IS readable, so a false "not ok" below
	// cannot come from the plumbing.
	ctlRoot := writeHistoryEntries7283(t, quality.HealthEntry{
		Timestamp: base, Group: "g", TotalEntities: 100,
		OrphanRate: 5, BugRate: &measured, HealthScore: &health,
	})
	if got, ok := latestGroupBugRate("g", ctlRoot); !ok || got != 20.0 {
		t.Fatalf("control: latestGroupBugRate = (%v, %v), want (20, true)", got, ok)
	}

	got, ok := latestGroupBugRate("g", root)
	if ok {
		t.Errorf("latestGroupBugRate = (%v, true) for an entry that recorded no bug rate; "+
			"fidelity would be %v and health %q off zero measurements",
			got, fidelityFromBugRate(got), mustHealth(fidelityFromBugRate(got)))
	}
}

func mustHealth(f float64) string {
	_, h := deriveHealthFromFidelity(f)
	return h
}

// TestQualityTrends_UnmeasuredEntryLeavesAGap pins the series rendering: an
// unmeasured entry contributes no point to the bug-rate or health-score
// series, rather than a 0 / 100 point. The machinery already exists for
// Cycles and AuthUncovered; this is the same shape.
func TestQualityTrends_UnmeasuredEntryLeavesAGap(t *testing.T) {
	base := time.Now().UTC().Add(-72 * time.Hour)
	b1, h1 := 2.0, 88.0
	b2, h2 := 4.0, 76.0
	entries := []quality.HealthEntry{
		{Timestamp: base, Group: "g", TotalEntities: 10, OrphanRate: 10, BugRate: &b1, HealthScore: &h1},
		{Timestamp: base.Add(time.Hour), Group: "g", TotalEntities: 10, OrphanRate: 20},
		{Timestamp: base.Add(2 * time.Hour), Group: "g", TotalEntities: 10, OrphanRate: 20, BugRate: &b2, HealthScore: &h2},
	}

	reply := buildTrendsReply("g", 30, entries)

	byLabel := map[string][]float64{}
	for _, m := range reply.Metrics {
		var vs []float64
		for _, p := range m.Points {
			vs = append(vs, p.Value)
		}
		byLabel[m.Label] = vs
	}

	// Orphan rate is measured on all three entries — the positive control that
	// proves the series builder saw every entry.
	if got := byLabel["Orphan rate"]; len(got) != 3 {
		t.Fatalf("Orphan rate points = %v, want 3 (the series builder must have seen all entries)", got)
	}
	for _, label := range []string{"Bug rate", "Health score"} {
		got := byLabel[label]
		if len(got) != 2 {
			t.Errorf("%s points = %v, want 2 — the unmeasured entry must leave a gap, not a fabricated point", label, got)
		}
	}
	if got := byLabel["Bug rate"]; len(got) == 2 && (got[0] != 2.0 || got[1] != 4.0) {
		t.Errorf("Bug rate points = %v, want [2 4]", got)
	}
	if got := byLabel["Health score"]; len(got) == 2 && (got[0] != 88.0 || got[1] != 76.0) {
		t.Errorf("Health score points = %v, want [88 76]", got)
	}
}
