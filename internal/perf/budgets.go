package perf

// budgets.go — performance budget tracking for archigraph (#1319)
//
// Records timed metric samples to ~/.archigraph/perf-history.jsonl and
// evaluates them against configurable budgets loaded from settings.json.
//
// Metric keys:
//   index_wall_ms      — full rebuild wall-time in milliseconds (per group)
//   query_p50_ms       — median query latency in milliseconds (per endpoint)
//   query_p95_ms       — 95th-percentile query latency in milliseconds
//   mcp_handshake_bytes — JSON payload size of a single MCP tool-list response
//   daemon_rss_mb      — resident set size of the daemon process in MiB
//   cache_hit_rate     — fraction 0.0–1.0 of graph cache hits
//
// Budget thresholds live in AppSettings.PerfBudgets (settings.json) as
// a flat string→float64 map keyed by metric name, e.g.:
//
//	"perf_budgets": {
//	  "index_wall_ms":       30000,
//	  "query_p95_ms":        500,
//	  "mcp_handshake_bytes": 65536,
//	  "daemon_rss_mb":       512,
//	  "cache_hit_rate":      0.8
//	}
//
// Status classification:
//   green  — value ≤ budget
//   yellow — value within 10% above budget
//   red    — value > budget * 1.10

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Public types
// ─────────────────────────────────────────────────────────────────────────────

// Sample is one recorded data point appended to perf-history.jsonl.
type Sample struct {
	// RecordedAt is when the measurement was taken (RFC 3339, UTC).
	RecordedAt string `json:"recorded_at"`
	// Metric is the metric key (see package comment for the canonical list).
	Metric string `json:"metric"`
	// Group is the archigraph group slug when the metric is per-group.
	// Empty for daemon-wide metrics (daemon_rss_mb, cache_hit_rate).
	Group string `json:"group,omitempty"`
	// Value is the raw measured value.
	Value float64 `json:"value"`
}

// BudgetStatus is the budget evaluation for a single (metric, group) pair.
type BudgetStatus struct {
	// Metric is the metric key.
	Metric string `json:"metric"`
	// Group is the group slug, or empty for daemon-wide metrics.
	Group string `json:"group,omitempty"`
	// Current is the most recently recorded value.
	Current float64 `json:"current"`
	// Budget is the configured threshold (0 = no budget configured).
	Budget float64 `json:"budget"`
	// Baseline is the rolling 30-run median used for trend comparison.
	Baseline float64 `json:"baseline"`
	// TrendPct is the percentage change from Baseline to Current
	// (positive = regression, negative = improvement).
	TrendPct float64 `json:"trend_pct"`
	// Status is "green" | "yellow" | "red" | "no_budget".
	Status string `json:"status"`
	// Warning is populated when Status == "red" or the regression is > 20%
	// vs Baseline.
	Warning string `json:"warning,omitempty"`
	// Sparkline contains up to 30 recent values (oldest first) for the UI.
	Sparkline []float64 `json:"sparkline,omitempty"`
}

// DefaultBudgets returns the out-of-the-box threshold map.
func DefaultBudgets() map[string]float64 {
	return map[string]float64{
		"index_wall_ms":       30_000, // 30 s
		"query_p50_ms":        100,    // 100 ms
		"query_p95_ms":        500,    // 500 ms
		"mcp_handshake_bytes": 65_536, // 64 KiB
		"daemon_rss_mb":       512,    // 512 MiB
		"cache_hit_rate":      0.8,    // 80% (note: lower = worse, inverted)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Recorder — append-only JSONL sink
// ─────────────────────────────────────────────────────────────────────────────

// Recorder appends metric samples to a JSONL file and maintains an in-process
// ring buffer for fast read-back without re-scanning the whole file.
type Recorder struct {
	mu   sync.Mutex
	path string
	ring []Sample // bounded in-memory buffer, up to ringCap entries
}

const ringCap = 500

// NewRecorder creates a Recorder targeting histPath. The file is created
// (along with parent directories) the first time Record is called.
func NewRecorder(histPath string) *Recorder {
	r := &Recorder{path: histPath}
	// Best-effort pre-load of existing history so the sparklines work
	// immediately without a cold-start gap.
	_ = r.loadHistory()
	return r
}

// Record appends a new sample for (metric, group, value).
func (r *Recorder) Record(metric, group string, value float64) error {
	s := Sample{
		RecordedAt: time.Now().UTC().Format(time.RFC3339),
		Metric:     metric,
		Group:      group,
		Value:      value,
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	// Append to ring.
	r.ring = append(r.ring, s)
	if len(r.ring) > ringCap {
		r.ring = r.ring[len(r.ring)-ringCap:]
	}

	// Append to JSONL file.
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return fmt.Errorf("perf: mkdir %s: %w", filepath.Dir(r.path), err)
	}
	f, err := os.OpenFile(r.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("perf: open %s: %w", r.path, err)
	}
	defer f.Close()
	b, _ := json.Marshal(s)
	_, err = fmt.Fprintf(f, "%s\n", b)
	return err
}

// Samples returns a snapshot of the in-memory ring, filtered to
// (metric, group). group=="" matches all groups.
func (r *Recorder) Samples(metric, group string) []Sample {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Sample
	for _, s := range r.ring {
		if s.Metric != metric {
			continue
		}
		if group != "" && s.Group != group {
			continue
		}
		out = append(out, s)
	}
	return out
}

// loadHistory reads the last ringCap lines from the JSONL file into ring.
// Called once during construction; errors are silently ignored (empty history
// is a valid starting state).
func (r *Recorder) loadHistory() error {
	f, err := os.Open(r.path)
	if err != nil {
		return nil // file not found is OK
	}
	defer f.Close()

	var samples []Sample
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var s Sample
		if json.Unmarshal(sc.Bytes(), &s) == nil {
			samples = append(samples, s)
		}
	}
	// Keep only the last ringCap.
	if len(samples) > ringCap {
		samples = samples[len(samples)-ringCap:]
	}
	r.ring = samples
	return sc.Err()
}

// ─────────────────────────────────────────────────────────────────────────────
// Evaluator — budget comparison + status
// ─────────────────────────────────────────────────────────────────────────────

// Evaluator computes BudgetStatus values from a Recorder + a budget map.
type Evaluator struct {
	rec     *Recorder
	budgets map[string]float64 // metric → threshold
}

// NewEvaluator creates an Evaluator. budgets may be nil (uses DefaultBudgets).
func NewEvaluator(rec *Recorder, budgets map[string]float64) *Evaluator {
	if budgets == nil {
		budgets = DefaultBudgets()
	}
	return &Evaluator{rec: rec, budgets: budgets}
}

// Evaluate returns BudgetStatus for the given (metric, group) pair.
// group="" is used for daemon-wide metrics.
func (e *Evaluator) Evaluate(metric, group string) BudgetStatus {
	samples := e.rec.Samples(metric, group)
	bs := BudgetStatus{
		Metric: metric,
		Group:  group,
		Budget: e.budgets[metric],
		Status: "no_budget",
	}

	if len(samples) == 0 {
		return bs
	}

	// Current = most recent value.
	bs.Current = samples[len(samples)-1].Value

	// Baseline = rolling 30-run median.
	windowSize := 30
	if len(samples) < windowSize {
		windowSize = len(samples)
	}
	window := samples[len(samples)-windowSize:]
	vals := make([]float64, len(window))
	for i, s := range window {
		vals[i] = s.Value
	}
	bs.Baseline = median(vals)

	// Trend % vs baseline.
	if bs.Baseline != 0 {
		bs.TrendPct = (bs.Current - bs.Baseline) / bs.Baseline * 100
	}

	// Sparkline — up to 30 most recent values.
	sparkLen := len(samples)
	if sparkLen > 30 {
		sparkLen = 30
	}
	bs.Sparkline = make([]float64, sparkLen)
	for i, s := range samples[len(samples)-sparkLen:] {
		bs.Sparkline[i] = s.Value
	}

	// Budget evaluation.
	// cache_hit_rate is an inverted metric — higher is better.
	if bs.Budget == 0 {
		bs.Status = "no_budget"
	} else if metric == "cache_hit_rate" {
		// For cache_hit_rate: green if current >= budget.
		switch {
		case bs.Current >= bs.Budget:
			bs.Status = "green"
		case bs.Current >= bs.Budget*0.90:
			bs.Status = "yellow"
		default:
			bs.Status = "red"
			bs.Warning = fmt.Sprintf("cache hit rate %.1f%% is below budget %.1f%%",
				bs.Current*100, bs.Budget*100)
		}
	} else {
		// For all other metrics: green if current <= budget.
		warnThresh := bs.Budget * 1.10
		switch {
		case bs.Current <= bs.Budget:
			bs.Status = "green"
		case bs.Current <= warnThresh:
			bs.Status = "yellow"
		default:
			bs.Status = "red"
			bs.Warning = fmt.Sprintf("%s %.0f exceeds budget %.0f (%.0f%%)",
				metric, bs.Current, bs.Budget,
				(bs.Current-bs.Budget)/bs.Budget*100)
		}
	}

	// Regression warning — independent of budget: flag > 20% increase vs baseline.
	if bs.Warning == "" && bs.TrendPct > 20 && metric != "cache_hit_rate" {
		bs.Warning = fmt.Sprintf("%s regressed %.0f%% vs 30-run baseline (%.0f → %.0f)",
			metric, bs.TrendPct, bs.Baseline, bs.Current)
	}
	if bs.Warning == "" && metric == "cache_hit_rate" && bs.TrendPct < -20 {
		bs.Warning = fmt.Sprintf("cache hit rate dropped %.0f%% vs 30-run baseline (%.1f%% → %.1f%%)",
			-bs.TrendPct, bs.Baseline*100, bs.Current*100)
	}

	return bs
}

// EvaluateAll evaluates all known metrics across all groups that have data.
func (e *Evaluator) EvaluateAll() []BudgetStatus {
	// Collect distinct (metric, group) pairs from the ring.
	type key struct{ metric, group string }
	seen := map[key]struct{}{}
	e.rec.mu.Lock()
	for _, s := range e.rec.ring {
		seen[key{s.Metric, s.Group}] = struct{}{}
	}
	e.rec.mu.Unlock()

	// Always include daemon-wide metrics.
	for m := range e.budgets {
		if m == "cache_hit_rate" || m == "daemon_rss_mb" {
			seen[key{m, ""}] = struct{}{}
		}
	}

	keys := make([]key, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	// Stable sort for deterministic output.
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].metric != keys[j].metric {
			return keys[i].metric < keys[j].metric
		}
		return keys[i].group < keys[j].group
	})

	out := make([]BudgetStatus, 0, len(keys))
	for _, k := range keys {
		out = append(out, e.Evaluate(k.metric, k.group))
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func median(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	cp := make([]float64, len(vals))
	copy(cp, vals)
	sort.Float64s(cp)
	n := len(cp)
	if n%2 == 0 {
		return (cp[n/2-1] + cp[n/2]) / 2
	}
	return cp[n/2]
}
