package dashboard

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/quality"
)

func TestFidelityFromBugRate(t *testing.T) {
	tests := []struct {
		name     string
		bugRate  float64
		wantFid  float64
		wantHlth string
	}{
		{"zero bug rate", 0.0, 1.0, healthHealthy},
		{"3pct bug rate", 3.0, 0.97, healthHealthy},
		{"exactly 97 boundary", 3.0, 0.97, healthHealthy},
		{"just below healthy", 3.1, 0.969, healthWarning},
		{"10pct", 10.0, 0.9, healthWarning},
		{"just below warning", 10.1, 0.899, healthDegraded},
		{"50pct", 50.0, 0.5, healthDegraded},
		{"100pct", 100.0, 0.0, healthDegraded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fid := fidelityFromBugRate(tt.bugRate)
			if fid != tt.wantFid {
				t.Errorf("fidelityFromBugRate(%.1f) = %.4f, want %.4f", tt.bugRate, fid, tt.wantFid)
			}
			_, hlth := deriveHealthFromFidelity(fid)
			if hlth != tt.wantHlth {
				t.Errorf("deriveHealthFromFidelity(%.4f) health = %q, want %q", fid, hlth, tt.wantHlth)
			}
		})
	}
}

func TestLatestGroupBugRate_NoHistory(t *testing.T) {
	dir := t.TempDir()
	bugRate, ok := latestGroupBugRate("nonexistent", dir)
	if ok {
		t.Errorf("want ok=false for missing history, got ok=true bugRate=%.2f", bugRate)
	}
	_ = bugRate
}

func TestLatestGroupBugRate_WithHistory(t *testing.T) {
	dir := t.TempDir()
	// Write two entries for "mygroup"; second is newer.
	e1 := quality.HealthEntry{
		Timestamp:   time.Now().Add(-2 * time.Hour),
		Group:       "mygroup",
		BugRate:     fptr7283(20.0),
		OrphanRate:  5.0,
		HealthScore: fptr7283(75.0),
	}
	e2 := quality.HealthEntry{
		Timestamp:   time.Now().Add(-1 * time.Hour),
		Group:       "mygroup",
		BugRate:     fptr7283(3.5),
		OrphanRate:  2.0,
		HealthScore: fptr7283(94.5),
	}
	if err := quality.AppendEntry(dir, e1); err != nil {
		t.Fatalf("AppendEntry e1: %v", err)
	}
	if err := quality.AppendEntry(dir, e2); err != nil {
		t.Fatalf("AppendEntry e2: %v", err)
	}

	bugRate, ok := latestGroupBugRate("mygroup", dir)
	if !ok {
		t.Fatal("want ok=true, got false")
	}
	if bugRate != 3.5 {
		t.Errorf("want bugRate=3.5, got %.2f", bugRate)
	}
}

func TestLatestGroupBugRate_OtherGroupIgnored(t *testing.T) {
	dir := t.TempDir()
	e := quality.HealthEntry{
		Timestamp:   time.Now(),
		Group:       "othergroup",
		BugRate:     fptr7283(15.0),
		HealthScore: fptr7283(85.0),
	}
	if err := quality.AppendEntry(dir, e); err != nil {
		t.Fatalf("AppendEntry: %v", err)
	}
	_, ok := latestGroupBugRate("mygroup", dir)
	if ok {
		t.Error("want ok=false for group with no entries, got true")
	}
}

// Make sure history files in non-existent dirs don't panic.
func TestLatestGroupBugRate_BadDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nonexistent")
	_, ok := latestGroupBugRate("any", dir)
	if ok {
		t.Error("want ok=false for bad root dir")
	}
}

// fptr7283 wraps a float literal for the pointer-typed HealthEntry fields
// (#7283 — nil is "not measured", which a bare float64 could not express).
func fptr7283(f float64) *float64 { return &f }
