package cli

// orphanrate_7292_test.go — the producer side of #7292. OrphanRate and
// TotalEntities became pointers on notifications.QualitySnapshot so the
// dashboard's test ping can say it measured neither. A rebuild DID measure
// both, and this file is the other direction: the pointer must not have turned
// a real measurement into a null on the path that actually has one.

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/notifications"
	"github.com/cajasmota/grafel/internal/quality"
)

// TestRebuildQualitySnapshot_CarriesTheMeasuredOrphanRate is the row that would
// fail if the rebuild path had been "fixed" by simply nilling the two fields.
func TestRebuildQualitySnapshot_CarriesTheMeasuredOrphanRate(t *testing.T) {
	sum := &RebuildSummary{Group: "g1", TotalEntities: 100, OrphanRate: 12.5}
	snap := rebuildQualitySnapshot("g1", sum, 87.5)

	if snap.OrphanRate == nil {
		t.Fatalf("rebuild dropped a measured orphan rate: %+v", snap)
	}
	if *snap.OrphanRate != 12.5 {
		t.Errorf("snapshot orphan_rate = %v, want 12.5", *snap.OrphanRate)
	}
	if snap.TotalEntities == nil {
		t.Fatalf("rebuild dropped a measured entity count: %+v", snap)
	}
	if *snap.TotalEntities != 100 {
		t.Errorf("snapshot total_entities = %v, want 100", *snap.TotalEntities)
	}
}

// TestRebuildQualitySnapshot_MeasuredBytes asserts what a receiver reads, not
// just what the struct holds: a real rebuild must not emit a null here.
func TestRebuildQualitySnapshot_MeasuredBytes(t *testing.T) {
	sum := &RebuildSummary{Group: "g1", TotalEntities: 100, OrphanRate: 12.5}
	body, err := json.Marshal(rebuildQualitySnapshot("g1", sum, 87.5))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(body)
	if !strings.Contains(got, `"orphan_rate":12.5`) {
		t.Errorf("rebuild snapshot does not carry the measured orphan rate:\n%s", got)
	}
	if strings.Contains(got, `"orphan_rate":null`) {
		t.Errorf("rebuild snapshot nulled a measured orphan rate:\n%s", got)
	}
	// Anchored on the value's end: a bare Contains of `"total_entities":100`
	// is satisfied by 1000, so it would survive a producer that multiplied the
	// count by ten.
	if !regexp.MustCompile(`"total_entities":100([,}])`).MatchString(got) {
		t.Errorf("rebuild snapshot does not carry the measured entity count exactly:\n%s", got)
	}
	if strings.Contains(got, `"total_entities":null`) {
		t.Errorf("rebuild snapshot nulled a measured entity count:\n%s", got)
	}
}

// TestRebuildQualitySnapshot_OrphanRatePointerIsNotShared pins that the
// snapshot owns its own float: a caller mutating the summary afterwards must
// not retroactively change a payload already built from it.
func TestRebuildQualitySnapshot_OrphanRatePointerIsNotShared(t *testing.T) {
	sum := &RebuildSummary{Group: "g1", TotalEntities: 100, OrphanRate: 12.5}
	snap := rebuildQualitySnapshot("g1", sum, 87.5)
	sum.OrphanRate = 99
	sum.TotalEntities = 7
	if snap.OrphanRate == nil || *snap.OrphanRate != 12.5 {
		t.Errorf("snapshot orphan rate followed the summary: %v", snap.OrphanRate)
	}
	if snap.TotalEntities == nil || *snap.TotalEntities != 100 {
		t.Errorf("snapshot entity count followed the summary: %v", snap.TotalEntities)
	}
}

// TestRebuildQualitySnapshot_EmptyGraphStillReportsAMeasuredZero pins a
// decision #7292 deliberately did NOT take. rebuild_summary.go computes the
// orphan rate only when TotalEntities > 0, so an empty graph leaves it at 0 —
// arguably "unmeasured" in the same sense the ping is. That was left alone
// rather than widened, and the choice was invisible to every other row here:
// nulling both fields for an empty graph passed the whole cli suite. This row
// makes the current behaviour observable, so reversing it is a decision
// somebody takes on purpose rather than a silent regression.
func TestRebuildQualitySnapshot_EmptyGraphStillReportsAMeasuredZero(t *testing.T) {
	sum := &RebuildSummary{Group: "g1", TotalEntities: 0, OrphanRate: 0}
	snap := rebuildQualitySnapshot("g1", sum, 100)

	if snap.OrphanRate == nil {
		t.Error("an empty graph nulled the orphan rate; #7292 left that field measured here")
	} else if *snap.OrphanRate != 0 {
		t.Errorf("empty-graph orphan_rate = %v, want 0", *snap.OrphanRate)
	}
	if snap.TotalEntities == nil {
		t.Error("an empty graph nulled the entity count; #7292 left that field measured here")
	} else if *snap.TotalEntities != 0 {
		t.Errorf("empty-graph total_entities = %v, want 0", *snap.TotalEntities)
	}

	// The wire, not just the struct: a receiver has to see the zeros.
	body, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(body)
	if !regexp.MustCompile(`"orphan_rate":0([,}])`).MatchString(got) {
		t.Errorf("empty-graph payload does not carry orphan_rate:0:\n%s", got)
	}
	if !regexp.MustCompile(`"total_entities":0([,}])`).MatchString(got) {
		t.Errorf("empty-graph payload does not carry total_entities:0:\n%s", got)
	}
}

// TestPreviousSnapshot_CarriesTheHistoryOrphanRate pins the claim written at
// previousSnapshot: quality.HealthEntry.OrphanRate is still a bare float64
// (#7283 defers it), so the previous run's rate is carried across as measured.
// RegressionDetected skips its orphan comparison when either side is nil, so a
// nil here would silently stop detecting orphan regressions altogether.
func TestPreviousSnapshot_CarriesTheHistoryOrphanRate(t *testing.T) {
	prev := previousSnapshot("g1", quality.HealthEntry{Group: "g1", OrphanRate: 3.5})
	if prev.OrphanRate == nil {
		t.Fatalf("previous snapshot dropped the history orphan rate: %+v", prev)
	}
	if *prev.OrphanRate != 3.5 {
		t.Errorf("previous snapshot orphan_rate = %v, want 3.5", *prev.OrphanRate)
	}
}

// TestPreviousSnapshot_OrphanRegressionIsStillDetected is the consequence of
// the row above, at the decision it feeds. Without it, "carries the rate" could
// be true while the comparison it exists for never fires.
func TestPreviousSnapshot_OrphanRegressionIsStillDetected(t *testing.T) {
	prev := previousSnapshot("g1", quality.HealthEntry{Group: "g1", OrphanRate: 3.5})
	worse := rebuildQualitySnapshot("g1", &RebuildSummary{Group: "g1", TotalEntities: 100, OrphanRate: 40}, 60)
	if !notifications.RegressionDetected(prev, worse) {
		t.Error("a 3.5% → 40% orphan rate was not reported as a regression")
	}
	// Bound: the same pair with no worsening must not report one, so the row
	// above is the comparison firing rather than RegressionDetected always
	// returning true.
	same := rebuildQualitySnapshot("g1", &RebuildSummary{Group: "g1", TotalEntities: 100, OrphanRate: 3.5}, 90)
	if notifications.RegressionDetected(prev, same) {
		t.Error("an unchanged orphan rate was reported as a regression")
	}
}
