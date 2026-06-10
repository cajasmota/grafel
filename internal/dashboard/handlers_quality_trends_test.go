package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/quality"
)

// TestBuildTrendsReply_empty verifies that an empty entry slice yields
// an empty metrics slice (not nil) so the frontend can handle it uniformly,
// and reports has_history=false / point_count=0 (no fabricated series).
func TestBuildTrendsReply_empty(t *testing.T) {
	reply := buildTrendsReply("mygroup", 30, nil)
	if reply.Group != "mygroup" {
		t.Errorf("group: got %q, want %q", reply.Group, "mygroup")
	}
	if len(reply.Metrics) != 0 {
		t.Errorf("expected 0 metrics for empty input, got %d", len(reply.Metrics))
	}
	if reply.HasHistory {
		t.Errorf("has_history: got true, want false for 0 snapshots")
	}
	if reply.PointCount != 0 {
		t.Errorf("point_count: got %d, want 0", reply.PointCount)
	}
}

// TestBuildTrendsReply_singleSnapshot verifies the freshly-indexed case: a
// single recorded snapshot is NOT a trend, so we emit zero metrics and report
// has_history=false / point_count=1. This is the honest empty-state that
// replaces the previously confusing single-point "insufficient history" cards
// (and any fabricated sawtooth). Ticket #4506.
func TestBuildTrendsReply_singleSnapshot(t *testing.T) {
	now := time.Now().UTC()
	covPct := 65.0
	cycles := 3
	secrets := 2
	entries := []quality.HealthEntry{
		{
			Timestamp:     now,
			Group:         "g",
			TotalEntities: 500,
			OrphanRate:    10.0,
			BugRate:       2.0,
			HealthScore:   88.0,
			CoveragePct:   &covPct,
			Cycles:        &cycles,
			Secrets:       &secrets,
		},
	}

	reply := buildTrendsReply("g", 30, entries)

	if len(reply.Metrics) != 0 {
		t.Errorf("expected 0 metrics for a single snapshot (no trend), got %d", len(reply.Metrics))
	}
	if reply.HasHistory {
		t.Errorf("has_history: got true, want false for a single snapshot")
	}
	if reply.PointCount != 1 {
		t.Errorf("point_count: got %d, want 1", reply.PointCount)
	}
	// Must never be nil so the frontend can iterate uniformly.
	if reply.Metrics == nil {
		t.Error("Metrics must be a non-nil empty slice")
	}
}

// TestBuildTrendsReply_coreMetrics verifies that the three always-present
// metrics (health_score, orphan_rate, bug_rate) are present and correct.
func TestBuildTrendsReply_coreMetrics(t *testing.T) {
	now := time.Now().UTC()
	entries := []quality.HealthEntry{
		{
			Timestamp:     now.Add(-48 * time.Hour),
			Group:         "g",
			TotalEntities: 1000,
			OrphanRate:    20.0,
			BugRate:       5.0,
			HealthScore:   75.0,
		},
		{
			Timestamp:     now.Add(-24 * time.Hour),
			Group:         "g",
			TotalEntities: 1050,
			OrphanRate:    18.0,
			BugRate:       4.5,
			HealthScore:   77.5,
		},
	}

	reply := buildTrendsReply("g", 30, entries)

	// Two real snapshots → a genuine trend.
	if !reply.HasHistory {
		t.Errorf("has_history: got false, want true for 2 snapshots")
	}
	if reply.PointCount != 2 {
		t.Errorf("point_count: got %d, want 2", reply.PointCount)
	}

	// At minimum the three core metrics must be present.
	if len(reply.Metrics) < 3 {
		t.Fatalf("expected at least 3 metrics, got %d", len(reply.Metrics))
	}

	// Find health_score metric.
	var hsMt *MetricTrend
	for i := range reply.Metrics {
		if reply.Metrics[i].Label == "Health score" {
			hsMt = &reply.Metrics[i]
			break
		}
	}
	if hsMt == nil {
		t.Fatal("Health score metric not found")
	}
	if len(hsMt.Points) != 2 {
		t.Errorf("expected 2 points for Health score, got %d", len(hsMt.Points))
	}
	if hsMt.Latest == nil || *hsMt.Latest != 77.5 {
		t.Errorf("latest health score: got %v, want 77.5", hsMt.Latest)
	}
	if hsMt.LowerIsBetter {
		t.Errorf("health score should have LowerIsBetter=false")
	}
}

// TestBuildTrendsReply_extendedMetrics verifies that sparse extended metrics
// (coverage, cycles, secrets) are included only when data exists.
func TestBuildTrendsReply_extendedMetrics(t *testing.T) {
	now := time.Now().UTC()
	covPct0 := 60.0
	cycles0 := 4
	secrets0 := 3
	covPct := 65.0
	cycles := 3
	secrets := 2

	// Need ≥2 snapshots for a real trend to be emitted.
	entries := []quality.HealthEntry{
		{
			Timestamp:     now.Add(-48 * time.Hour),
			Group:         "g",
			TotalEntities: 480,
			OrphanRate:    11.0,
			BugRate:       2.5,
			HealthScore:   86.5,
			CoveragePct:   &covPct0,
			Cycles:        &cycles0,
			Secrets:       &secrets0,
		},
		{
			Timestamp:     now.Add(-24 * time.Hour),
			Group:         "g",
			TotalEntities: 500,
			OrphanRate:    10.0,
			BugRate:       2.0,
			HealthScore:   88.0,
			CoveragePct:   &covPct,
			Cycles:        &cycles,
			Secrets:       &secrets,
		},
	}

	reply := buildTrendsReply("g", 30, entries)

	metricsMap := make(map[string]*MetricTrend, len(reply.Metrics))
	for i := range reply.Metrics {
		metricsMap[reply.Metrics[i].Label] = &reply.Metrics[i]
	}

	if mt, ok := metricsMap["Test coverage"]; !ok {
		t.Error("Test coverage metric missing")
	} else if mt.Latest == nil || *mt.Latest != 65.0 {
		t.Errorf("coverage latest: got %v, want 65.0", mt.Latest)
	}

	if mt, ok := metricsMap["Import cycles"]; !ok {
		t.Error("Import cycles metric missing")
	} else if mt.Latest == nil || *mt.Latest != 3 {
		t.Errorf("cycles latest: got %v, want 3", mt.Latest)
	}

	if mt, ok := metricsMap["Secret findings"]; !ok {
		t.Error("Secret findings metric missing")
	} else if mt.Latest == nil || *mt.Latest != 2 {
		t.Errorf("secrets latest: got %v, want 2", mt.Latest)
	}
}

// TestBuildTrendsReply_sparseMetricExcluded verifies that a metric is not
// included in the reply when all entries have nil for that metric.
func TestBuildTrendsReply_sparseMetricExcluded(t *testing.T) {
	now := time.Now().UTC()
	// Two snapshots so a trend is emitted, but with no extended metrics.
	entries := []quality.HealthEntry{
		{
			Timestamp:     now.Add(-24 * time.Hour),
			Group:         "g",
			TotalEntities: 480,
			OrphanRate:    11.0,
			BugRate:       2.5,
			HealthScore:   86.5,
			// CoveragePct, Cycles, Secrets all nil → should be excluded.
		},
		{
			Timestamp:     now,
			Group:         "g",
			TotalEntities: 500,
			OrphanRate:    10.0,
			BugRate:       2.0,
			HealthScore:   88.0,
			// CoveragePct, Cycles, Secrets all nil → should be excluded.
		},
	}

	reply := buildTrendsReply("g", 30, entries)

	for _, mt := range reply.Metrics {
		switch mt.Label {
		case "Test coverage", "Import cycles", "Auth-uncovered endpoints", "Secret findings":
			t.Errorf("metric %q should be absent when all values are nil, but was included", mt.Label)
		}
	}
}

// TestBuildTrendsReply_delta verifies that the 7d and 30d deltas are computed
// correctly.
func TestBuildTrendsReply_delta(t *testing.T) {
	now := time.Now().UTC()
	entries := []quality.HealthEntry{
		{
			Timestamp:     now.AddDate(0, 0, -35),
			Group:         "g",
			TotalEntities: 900,
			OrphanRate:    25.0,
			BugRate:       6.0,
			HealthScore:   69.0, // old entry (>30d ago)
		},
		{
			Timestamp:     now.AddDate(0, 0, -8),
			Group:         "g",
			TotalEntities: 950,
			OrphanRate:    22.0,
			BugRate:       5.0,
			HealthScore:   73.0, // ~7-30d ago
		},
		{
			Timestamp:     now,
			Group:         "g",
			TotalEntities: 1000,
			OrphanRate:    20.0,
			BugRate:       4.5,
			HealthScore:   80.0, // latest
		},
	}

	reply := buildTrendsReply("g", 60, entries)

	var hsMt *MetricTrend
	for i := range reply.Metrics {
		if reply.Metrics[i].Label == "Health score" {
			hsMt = &reply.Metrics[i]
			break
		}
	}
	if hsMt == nil {
		t.Fatal("Health score metric missing")
	}

	// delta30d should be 80 - 69 = 11
	if hsMt.Delta30d == nil {
		t.Fatal("Delta30d is nil, want 11.0")
	}
	if *hsMt.Delta30d != 11.0 {
		t.Errorf("Delta30d: got %v, want 11.0", *hsMt.Delta30d)
	}

	// delta7d: the entry at -8d is the closest entry older than 7d from now
	// (cutoff = now - 7d; -8d <= cutoff), so delta7d = 80 - 73 = 7
	if hsMt.Delta7d == nil {
		t.Fatal("Delta7d is nil, want 7.0")
	}
	if *hsMt.Delta7d != 7.0 {
		t.Errorf("Delta7d: got %v, want 7.0", *hsMt.Delta7d)
	}
}

// TestHandleQualityTrends_daysValidation checks that the handler returns 400
// for an invalid ?days value.
func TestHandleQualityTrends_daysValidation(t *testing.T) {
	srv, err := NewServer(DefaultConfig(), newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/quality/trends/mygroup?days=bad", nil)
	req.SetPathValue("group", "mygroup")
	rr := httptest.NewRecorder()

	srv.handleQualityTrends(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", rr.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] == "" {
		t.Error("expected non-empty error message")
	}
}
