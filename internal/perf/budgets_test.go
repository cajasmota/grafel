package perf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecordAndSamples(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(filepath.Join(dir, "perf-history.jsonl"))

	if err := rec.Record("index_wall_ms", "mygroup", 12345.0); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := rec.Record("index_wall_ms", "mygroup", 9000.0); err != nil {
		t.Fatalf("Record: %v", err)
	}

	samples := rec.Samples("index_wall_ms", "mygroup")
	if len(samples) != 2 {
		t.Fatalf("want 2 samples, got %d", len(samples))
	}
	if samples[0].Value != 12345.0 {
		t.Errorf("want 12345, got %f", samples[0].Value)
	}
	if samples[1].Value != 9000.0 {
		t.Errorf("want 9000, got %f", samples[1].Value)
	}
}

func TestRecordPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "perf-history.jsonl")

	rec1 := NewRecorder(path)
	_ = rec1.Record("daemon_rss_mb", "", 200.0)

	// New recorder reads existing history.
	rec2 := NewRecorder(path)
	samples := rec2.Samples("daemon_rss_mb", "")
	if len(samples) != 1 {
		t.Fatalf("want 1 sample after reload, got %d", len(samples))
	}
	if samples[0].Value != 200.0 {
		t.Errorf("want 200, got %f", samples[0].Value)
	}
}

func TestEvaluateGreen(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(filepath.Join(dir, "h.jsonl"))
	// Record well below the 30000 ms budget.
	for i := 0; i < 5; i++ {
		_ = rec.Record("index_wall_ms", "g1", 10000.0)
	}
	ev := NewEvaluator(rec, nil)
	bs := ev.Evaluate("index_wall_ms", "g1")
	if bs.Status != "green" {
		t.Errorf("want green, got %s (current=%.0f budget=%.0f)", bs.Status, bs.Current, bs.Budget)
	}
	if bs.Warning != "" {
		t.Errorf("want no warning, got %q", bs.Warning)
	}
}

func TestEvaluateRed(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(filepath.Join(dir, "h.jsonl"))
	// Record well above the 30000 ms budget.
	for i := 0; i < 5; i++ {
		_ = rec.Record("index_wall_ms", "g1", 50000.0)
	}
	ev := NewEvaluator(rec, nil)
	bs := ev.Evaluate("index_wall_ms", "g1")
	if bs.Status != "red" {
		t.Errorf("want red, got %s", bs.Status)
	}
	if bs.Warning == "" {
		t.Error("want warning for red status")
	}
}

func TestEvaluateYellow(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(filepath.Join(dir, "h.jsonl"))
	// Record just above budget but within 10% window (30000 * 1.05 = 31500).
	for i := 0; i < 5; i++ {
		_ = rec.Record("index_wall_ms", "g1", 31500.0)
	}
	ev := NewEvaluator(rec, nil)
	bs := ev.Evaluate("index_wall_ms", "g1")
	if bs.Status != "yellow" {
		t.Errorf("want yellow, got %s", bs.Status)
	}
}

func TestEvaluateCacheHitRateInverted(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(filepath.Join(dir, "h.jsonl"))
	// cache_hit_rate budget = 0.8; current = 0.5 → red.
	_ = rec.Record("cache_hit_rate", "", 0.5)
	ev := NewEvaluator(rec, nil)
	bs := ev.Evaluate("cache_hit_rate", "")
	if bs.Status != "red" {
		t.Errorf("want red for low cache_hit_rate, got %s", bs.Status)
	}

	// current = 0.95 → green.
	rec2 := NewRecorder(filepath.Join(dir, "h2.jsonl"))
	_ = rec2.Record("cache_hit_rate", "", 0.95)
	ev2 := NewEvaluator(rec2, nil)
	bs2 := ev2.Evaluate("cache_hit_rate", "")
	if bs2.Status != "green" {
		t.Errorf("want green for high cache_hit_rate, got %s", bs2.Status)
	}
}

func TestRegressionWarning(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(filepath.Join(dir, "h.jsonl"))
	budgets := map[string]float64{
		"query_p95_ms": 1_000_000, // enormous budget — won't trigger red
	}
	// Baseline of 100 ms then sudden jump to 200 ms (100% regression > 20%).
	for i := 0; i < 10; i++ {
		_ = rec.Record("query_p95_ms", "", 100.0)
	}
	_ = rec.Record("query_p95_ms", "", 200.0)

	ev := NewEvaluator(rec, budgets)
	bs := ev.Evaluate("query_p95_ms", "")
	if bs.Warning == "" {
		t.Errorf("want regression warning, got none (trend=%.1f%%)", bs.TrendPct)
	}
}

func TestSparklineLen(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(filepath.Join(dir, "h.jsonl"))
	for i := 0; i < 50; i++ {
		_ = rec.Record("index_wall_ms", "g", float64(i))
	}
	ev := NewEvaluator(rec, nil)
	bs := ev.Evaluate("index_wall_ms", "g")
	if len(bs.Sparkline) != 30 {
		t.Errorf("want sparkline len 30, got %d", len(bs.Sparkline))
	}
}

func TestEvaluateAll(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(filepath.Join(dir, "h.jsonl"))
	_ = rec.Record("index_wall_ms", "g1", 5000.0)
	_ = rec.Record("query_p95_ms", "", 200.0)
	_ = rec.Record("daemon_rss_mb", "", 300.0)

	ev := NewEvaluator(rec, nil)
	statuses := ev.EvaluateAll()
	if len(statuses) == 0 {
		t.Error("want at least one status from EvaluateAll")
	}
}

func TestNoBudgetConfigured(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(filepath.Join(dir, "h.jsonl"))
	_ = rec.Record("custom_metric", "g", 999.0)
	// No budget for custom_metric.
	ev := NewEvaluator(rec, map[string]float64{})
	bs := ev.Evaluate("custom_metric", "g")
	if bs.Status != "no_budget" {
		t.Errorf("want no_budget, got %s", bs.Status)
	}
}

func TestNoSamples(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(filepath.Join(dir, "h.jsonl"))
	ev := NewEvaluator(rec, nil)
	bs := ev.Evaluate("index_wall_ms", "g")
	if bs.Status != "no_budget" {
		// no samples means no current value — status should be no_budget
		t.Errorf("want no_budget for empty sample set, got %s", bs.Status)
	}
	if bs.Current != 0 {
		t.Errorf("want current=0 for empty, got %f", bs.Current)
	}
}

func TestMedian(t *testing.T) {
	tests := []struct {
		vals []float64
		want float64
	}{
		{[]float64{3, 1, 2}, 2},
		{[]float64{1, 2, 3, 4}, 2.5},
		{[]float64{5}, 5},
		{nil, 0},
	}
	for _, tc := range tests {
		got := median(tc.vals)
		if got != tc.want {
			t.Errorf("median(%v) = %f, want %f", tc.vals, got, tc.want)
		}
	}
}

func TestRingCapEnforced(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(filepath.Join(dir, "h.jsonl"))
	for i := 0; i < ringCap+50; i++ {
		_ = rec.Record("index_wall_ms", "g", float64(i))
	}
	rec.mu.Lock()
	n := len(rec.ring)
	rec.mu.Unlock()
	if n > ringCap {
		t.Errorf("ring exceeded cap: len=%d cap=%d", n, ringCap)
	}
}

// TestHistPathCreated ensures the directory is auto-created.
func TestHistPathCreated(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b", "c", "perf-history.jsonl")
	rec := NewRecorder(nested)
	if err := rec.Record("index_wall_ms", "g", 1.0); err != nil {
		t.Fatalf("Record in nested dir: %v", err)
	}
	if _, err := os.Stat(nested); err != nil {
		t.Errorf("history file not created: %v", err)
	}
}
