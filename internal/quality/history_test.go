package quality_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/quality"
)

func TestComputeHealthScore(t *testing.T) {
	tests := []struct {
		orphan float64
		bug    float64
		want   float64
	}{
		{0, 0, 100},
		{10, 5, 85},
		{60, 50, 0},  // clamp to 0
		{100, 0, 0},
	}
	for _, tc := range tests {
		got := quality.ComputeHealthScore(tc.orphan, tc.bug)
		if got != tc.want {
			t.Errorf("ComputeHealthScore(%v,%v) = %v, want %v", tc.orphan, tc.bug, got, tc.want)
		}
	}
}

func TestAppendAndReadHistory(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)

	entries := []quality.HealthEntry{
		{
			Timestamp:   now.Add(-48 * time.Hour),
			Group:       "mygroup",
			TotalEntities: 1000,
			OrphanRate:  20.0,
			BugRate:     5.0,
			HealthScore: quality.ComputeHealthScore(20.0, 5.0),
		},
		{
			Timestamp:   now.Add(-24 * time.Hour),
			Group:       "mygroup",
			TotalEntities: 1050,
			OrphanRate:  18.0,
			BugRate:     4.0,
			HealthScore: quality.ComputeHealthScore(18.0, 4.0),
		},
		{
			// Different group — should not appear in results for "mygroup".
			Timestamp:   now,
			Group:       "othergroup",
			TotalEntities: 500,
			OrphanRate:  10.0,
			BugRate:     2.0,
			HealthScore: quality.ComputeHealthScore(10.0, 2.0),
		},
	}

	for _, e := range entries {
		if err := quality.AppendEntry(root, e); err != nil {
			t.Fatalf("AppendEntry: %v", err)
		}
	}

	got, err := quality.ReadHistory(root, "mygroup", 90)
	if err != nil {
		t.Fatalf("ReadHistory: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 entries for mygroup, got %d", len(got))
	}

	// Verify content of second entry.
	if got[1].OrphanRate != 18.0 {
		t.Errorf("orphan_rate: got %v, want 18.0", got[1].OrphanRate)
	}

	// Verify the JSONL file exists at the expected path.
	path := filepath.Join(root, "health-history.jsonl")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("history file not found: %v", err)
	}
}

func TestReadHistory_FileAbsent(t *testing.T) {
	root := t.TempDir()
	entries, err := quality.ReadHistory(root, "mygroup", 7)
	if err != nil {
		t.Fatalf("expected no error when file absent, got %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestReadHistory_DayFilter(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()

	old := quality.HealthEntry{
		Timestamp:   now.AddDate(0, 0, -10),
		Group:       "g",
		TotalEntities: 100,
		OrphanRate:  15.0,
		BugRate:     3.0,
		HealthScore: quality.ComputeHealthScore(15.0, 3.0),
	}
	recent := quality.HealthEntry{
		Timestamp:   now.AddDate(0, 0, -3),
		Group:       "g",
		TotalEntities: 110,
		OrphanRate:  12.0,
		BugRate:     2.0,
		HealthScore: quality.ComputeHealthScore(12.0, 2.0),
	}

	_ = quality.AppendEntry(root, old)
	_ = quality.AppendEntry(root, recent)

	got, err := quality.ReadHistory(root, "g", 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 entry within 7 days, got %d", len(got))
	}
	if got[0].OrphanRate != 12.0 {
		t.Errorf("wrong entry returned: orphan_rate=%v", got[0].OrphanRate)
	}
}
