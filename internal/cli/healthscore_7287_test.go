package cli

// healthscore_7287_test.go — the producer side of #7287. rebuildQualitySnapshot
// is handed a health score its caller computed with ComputeHealthScore from
// sum.BugRate.Pct(), which is 0 for an unmeasured rate. Passing that through
// put a score nobody measured on the wire and into a Slack title.
//
// TestRebuildQualitySnapshot_UnmeasuredIsNull (doctor_bugrate_7271_test.go)
// already covered the bug rate at this seam but passed 80 as a literal
// healthScore and asserted nothing about the field, so the score started from
// zero coverage.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/notifications"
	"github.com/cajasmota/grafel/internal/quality"
	"github.com/cajasmota/grafel/internal/quality/audit"
)

func fptr7287(v float64) *float64 { return &v }

// TestPreviousSnapshot_CarriesMeasurednessAcross covers the other side of the
// comparison. Before #7287 the health score went through a
// `if prevEntry.HealthScore != nil { prevSnap.HealthScore = * }` guard, which
// left a 0 in the field for an unmeasured previous run — a perfect score, in a
// snapshot that otherwise says nothing was measured.
//
// For the bug rate the flattening also changes a decision: RegressionDetected
// skips a metric when either side is nil, so a 0 there makes the previous run a
// flawless baseline and reports a regression that did not happen. That
// consequence is asserted separately below. The health score changes no
// decision today (see TestRegressionDetected_IgnoresTheHealthScore in
// internal/notifications); it is pinned here as snapshot faithfulness.
func TestPreviousSnapshot_CarriesMeasurednessAcross(t *testing.T) {
	t.Run("unmeasured stays nil", func(t *testing.T) {
		got := previousSnapshot("g1", quality.HealthEntry{Group: "g1", OrphanRate: 3.5})
		if got.HealthScore != nil {
			t.Errorf("previous health_score = %v for an entry that recorded none, want nil", *got.HealthScore)
		}
		if got.BugRate != nil {
			t.Errorf("previous bug_rate = %v, want nil", *got.BugRate)
		}
		if got.OrphanRate != 3.5 || got.Group != "g1" {
			t.Errorf("control: the rest of the snapshot is still mapped: %+v", got)
		}
	})

	t.Run("measured carries across", func(t *testing.T) {
		got := previousSnapshot("g1", quality.HealthEntry{
			Group: "g1", OrphanRate: 3.5,
			BugRate: fptr7287(10), HealthScore: fptr7287(86.5),
		})
		if got.HealthScore == nil || *got.HealthScore != 86.5 {
			t.Errorf("previous health_score = %v, want 86.5", got.HealthScore)
		}
		if got.BugRate == nil || *got.BugRate != 10 {
			t.Errorf("previous bug_rate = %v, want 10", got.BugRate)
		}
	})
}

// TestPreviousSnapshot_NoRegressionFromAnUnmeasuredPrevious is the consequence
// the mapping exists for, asserted at the decision it feeds rather than at the
// field. A previous run that measured no bug rate must not be read as a
// flawless baseline that the current run then "regresses" from.
func TestPreviousSnapshot_NoRegressionFromAnUnmeasuredPrevious(t *testing.T) {
	prev := previousSnapshot("g1", quality.HealthEntry{Group: "g1", OrphanRate: 3.5})
	curr := notifications.QualitySnapshot{Group: "g1", OrphanRate: 3.5, BugRate: fptr7287(40), HealthScore: fptr7287(56.5)}

	if notifications.RegressionDetected(prev, curr) {
		t.Error("a previous run that measured nothing was treated as a baseline to regress from")
	}

	// Positive control: with a measured previous bug rate, the same current
	// snapshot IS a regression — so the row above is a real skip, not a
	// RegressionDetected that never fires.
	prevMeasured := previousSnapshot("g1", quality.HealthEntry{
		Group: "g1", OrphanRate: 3.5, BugRate: fptr7287(1), HealthScore: fptr7287(95.5),
	})
	if !notifications.RegressionDetected(prevMeasured, curr) {
		t.Error("control: a measured 1% → 40% bug rate must be a regression; the row above proves nothing otherwise")
	}
}

// TestRebuildQualitySnapshot_UncomputedHealthScoreIsNull is the forbidden row
// at the producer: the caller's number must not survive when there was no bug
// rate to compute it from.
func TestRebuildQualitySnapshot_UncomputedHealthScoreIsNull(t *testing.T) {
	sum := &RebuildSummary{Group: "g1", TotalEntities: 100, OrphanRate: 12.5}

	// 87.5 is exactly what recordHealthHistory computes for this summary:
	// ComputeHealthScore(12.5, sum.BugRate.Pct()) with Pct() returning 0. It is
	// passed here rather than an arbitrary literal so this row rejects the real
	// production value, not a value only a test would produce.
	snap := rebuildQualitySnapshot("g1", sum, 87.5)

	if snap.HealthScore != nil {
		t.Errorf("snapshot health_score = %v for a rebuild that measured no bug rate, want null", *snap.HealthScore)
	}
	if snap.BugRate != nil {
		t.Errorf("control: bug_rate = %v, want null", *snap.BugRate)
	}
	if snap.OrphanRate != 12.5 || snap.TotalEntities != 100 {
		t.Errorf("control: the rest of the snapshot is still built: %+v", snap)
	}
}

// TestRebuildQualitySnapshot_MeasuredHealthScoreSurvives is the other
// direction. Without it, `snap.HealthScore = nil` unconditionally passes the
// row above.
func TestRebuildQualitySnapshot_MeasuredHealthScoreSurvives(t *testing.T) {
	sum := &RebuildSummary{
		Group: "g1", TotalEntities: 100, OrphanRate: 12.5,
		BugRate: audit.BugRate{TotalImports: 4, ResolvedImports: 3},
	}

	snap := rebuildQualitySnapshot("g1", sum, 62.5)

	if snap.HealthScore == nil {
		t.Fatalf("snapshot dropped a computed health score: %+v", snap)
	}
	if *snap.HealthScore != 62.5 {
		t.Errorf("snapshot health_score = %v, want 62.5", *snap.HealthScore)
	}
	if snap.BugRate == nil || *snap.BugRate != 25.0 {
		t.Errorf("control: bug_rate = %v, want 25", snap.BugRate)
	}
}

// TestRebuildQualitySnapshot_ParityWithTheHistoryRecord pins the claim the
// comment in recordHealthHistory makes: the webhook snapshot and the persisted
// HealthEntry go null on the same condition. Before #7287 they disagreed — the
// record omitted the score and the snapshot shipped it — and the comment that
// said so was the only thing observing it.
//
// Both sides are derived from the same summary here, and the pair is checked in
// both states, so a fix that nulls one surface and not the other fails.
func TestRebuildQualitySnapshot_ParityWithTheHistoryRecord(t *testing.T) {
	for _, tc := range []struct {
		name       string
		bugRate    audit.BugRate
		wantScored bool
	}{
		{"unmeasured", audit.BugRate{}, false},
		{"measured", audit.BugRate{TotalImports: 4, ResolvedImports: 3}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv(daemon.EnvRoot, root)
			sum := &RebuildSummary{Group: "g1", TotalEntities: 100, OrphanRate: 12.5, BugRate: tc.bugRate}

			// The record side is observed, not transcribed: recordHealthHistory
			// writes the history file and the entry is read back off disk.
			recordHealthHistory("g1", sum)
			entries, err := quality.ReadHistory(root, "g1", 3650)
			if err != nil {
				t.Fatalf("ReadHistory: %v", err)
			}
			if len(entries) != 1 {
				t.Fatalf("got %d history entries, want 1", len(entries))
			}
			recordScored := entries[0].HealthScore != nil
			snapScored := rebuildQualitySnapshot("g1", sum, 87.5).HealthScore != nil

			if recordScored != tc.wantScored {
				t.Fatalf("premise broken: history record scored = %v, want %v", recordScored, tc.wantScored)
			}
			if snapScored != recordScored {
				t.Errorf("webhook snapshot scored = %v but the history record scored = %v; "+
					"the two surfaces disagree about whether anything was measured", snapScored, recordScored)
			}
		})
	}
}

// TestRebuildQualitySnapshot_EmittedBytes asserts what a webhook consumer
// actually receives. The struct-level rows above would stay green if the JSON
// tag grew an `,omitempty` and the key vanished from the body — and a consumer
// that defaults a missing key to 0 lands back on the original defect.
func TestRebuildQualitySnapshot_EmittedBytes(t *testing.T) {
	sum := &RebuildSummary{Group: "g1", TotalEntities: 100, OrphanRate: 12.5}

	body, err := json.Marshal(rebuildQualitySnapshot("g1", sum, 87.5))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(body)
	if !strings.Contains(got, `"health_score":null`) {
		t.Errorf("emitted snapshot does not carry an explicit null health_score:\n%s", got)
	}
	if strings.Contains(got, `"health_score":87.5`) {
		t.Errorf("emitted snapshot carries the uncomputed score:\n%s", got)
	}
	if !strings.Contains(got, `"bug_rate":null`) || !strings.Contains(got, `"orphan_rate":12.5`) {
		t.Fatalf("control: the rest of the snapshot did not serialise:\n%s", got)
	}
}
