package mcp

// metrics.go — MCP session metrics aggregation + daily rollup persistence (#2192).
//
// Design overview:
//
//	SessionMetrics wraps Telemetry and adds:
//	  - Per-tool latency sample reservoir (capped at 1000 samples) for p50/p95.
//	  - Session-level tracking: session ID, start/end time, per-tool error rate.
//	  - Daily rollup serialised to ~/.archigraph/metrics/mcp-YYYY-MM-DD.jsonl.
//
// Integration:
//	  - server.go creates a SessionMetrics alongside Telemetry (or replaces it).
//	  - wrap() calls SessionMetrics.Record(tool, elapsed, isErr) after each call.
//	  - handleMCPMetrics reads SessionMetrics.CurrentStats() + last-N-days rollups.
//
// Rollup file format (JSON-lines, one object per call batch written at flush):
//
//	{"date":"2026-05-27","session_id":"...","tool":"archigraph_find","calls":42,
//	 "errors":1,"p50_ms":12,"p95_ms":87,"flushed_at":"2026-05-27T..."}
//
// Persistence is best-effort: write failures are silently ignored so a
// read-only home directory doesn't crash the daemon.

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// maxLatencySamples is the reservoir cap per tool. At 1 000 samples the p95
// estimate is accurate to ±1% with high probability for any call distribution.
const maxLatencySamples int = 1000

// sessionMetricsTool holds per-tool metrics for the current session.
type sessionMetricsTool struct {
	Calls    int     // total invocations
	Errors   int     // invocations that returned isError=true
	samples  []int64 // latency samples in milliseconds (reservoir-sampled)
	p50Cache int64   // cached after sort; reset on next record
	p95Cache int64
	dirty    bool // true when new samples arrived since last percentile calc
}

// record adds one observation. Reservoir sampling keeps the size bounded.
func (s *sessionMetricsTool) record(elapsedMS int64, isErr bool) {
	s.Calls++
	if isErr {
		s.Errors++
	}
	// Reservoir sampling (Algorithm R): keep at most maxLatencySamples samples
	// with uniform probability.
	n := len(s.samples)
	if n < maxLatencySamples {
		s.samples = append(s.samples, elapsedMS)
	} else {
		// Replace a random existing sample with probability cap/total.
		idx := rand.Intn(s.Calls)
		if idx < maxLatencySamples {
			s.samples[idx] = elapsedMS
		}
	}
	s.dirty = true
}

// percentiles returns (p50, p95) in milliseconds. Reuses cached values when
// no new samples have arrived since the last call.
func (s *sessionMetricsTool) percentiles() (p50, p95 int64) {
	if len(s.samples) == 0 {
		return 0, 0
	}
	if !s.dirty {
		return s.p50Cache, s.p95Cache
	}
	cp := make([]int64, len(s.samples))
	copy(cp, s.samples)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	n := len(cp)
	s.p50Cache = cp[(n-1)*50/100]
	s.p95Cache = cp[(n-1)*95/100]
	s.dirty = false
	return s.p50Cache, s.p95Cache
}

// SessionMetrics is the top-level session-scoped metrics collector.
type SessionMetrics struct {
	mu         sync.Mutex
	sessionID  string
	startedAt  time.Time
	tools      map[string]*sessionMetricsTool
	metricsDir string // path to ~/.archigraph/metrics/; empty = no rollup
}

// NewSessionMetrics creates a SessionMetrics. metricsDir is the directory for
// daily rollup files; pass "" to disable persistence (useful in tests).
func NewSessionMetrics(sessionID, metricsDir string) *SessionMetrics {
	return &SessionMetrics{
		sessionID:  sessionID,
		startedAt:  time.Now(),
		tools:      make(map[string]*sessionMetricsTool),
		metricsDir: metricsDir,
	}
}

// Record adds one tool observation. Called from wrap() after each handler returns.
func (m *SessionMetrics) Record(tool string, elapsed time.Duration, isErr bool) {
	ms := elapsed.Milliseconds()
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.tools[tool]
	if s == nil {
		s = &sessionMetricsTool{}
		m.tools[tool] = s
	}
	s.record(ms, isErr)
}

// ToolStat is the exported shape for one tool's current-session metrics.
type ToolStat struct {
	Tool     string  `json:"tool"`
	Calls    int     `json:"calls"`
	Errors   int     `json:"errors"`
	ErrorPct float64 `json:"error_pct"` // 0–100
	P50MS    int64   `json:"p50_ms"`
	P95MS    int64   `json:"p95_ms"`
}

// SessionSnapshot is the full current-session snapshot returned by the MCP tool.
type SessionSnapshot struct {
	SessionID     string     `json:"session_id"`
	StartedAt     time.Time  `json:"started_at"`
	UptimeSeconds int        `json:"uptime_seconds"`
	TotalCalls    int        `json:"total_calls"`
	TotalErrors   int        `json:"total_errors"`
	Tools         []ToolStat `json:"tools"`
}

// Snapshot returns a point-in-time snapshot of current-session metrics.
func (m *SessionMetrics) Snapshot() SessionSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	snap := SessionSnapshot{
		SessionID:     m.sessionID,
		StartedAt:     m.startedAt,
		UptimeSeconds: int(time.Since(m.startedAt).Seconds()),
	}

	names := make([]string, 0, len(m.tools))
	for k := range m.tools {
		names = append(names, k)
	}
	sort.Strings(names)

	for _, name := range names {
		s := m.tools[name]
		p50, p95 := s.percentiles()
		errPct := 0.0
		if s.Calls > 0 {
			errPct = float64(s.Errors) / float64(s.Calls) * 100
		}
		snap.Tools = append(snap.Tools, ToolStat{
			Tool:     name,
			Calls:    s.Calls,
			Errors:   s.Errors,
			ErrorPct: errPct,
			P50MS:    p50,
			P95MS:    p95,
		})
		snap.TotalCalls += s.Calls
		snap.TotalErrors += s.Errors
	}
	return snap
}

// RollupRecord is one line in the daily rollup JSONL file.
type RollupRecord struct {
	Date      string    `json:"date"`
	SessionID string    `json:"session_id"`
	Tool      string    `json:"tool"`
	Calls     int       `json:"calls"`
	Errors    int       `json:"errors"`
	P50MS     int64     `json:"p50_ms"`
	P95MS     int64     `json:"p95_ms"`
	FlushAt   time.Time `json:"flushed_at"`
}

// FlushRollup writes per-tool rollup records for the current session to the
// daily JSONL file (~/.archigraph/metrics/mcp-YYYY-MM-DD.jsonl). It is
// best-effort: any I/O error is silently ignored.
func (m *SessionMetrics) FlushRollup() {
	if m.metricsDir == "" {
		return
	}
	snap := m.Snapshot()
	if len(snap.Tools) == 0 {
		return
	}
	date := time.Now().UTC().Format("2006-01-02")
	path := filepath.Join(m.metricsDir, fmt.Sprintf("mcp-%s.jsonl", date))
	if err := os.MkdirAll(m.metricsDir, 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	now := time.Now().UTC()
	enc := json.NewEncoder(f)
	for _, ts := range snap.Tools {
		rec := RollupRecord{
			Date:      date,
			SessionID: snap.SessionID,
			Tool:      ts.Tool,
			Calls:     ts.Calls,
			Errors:    ts.Errors,
			P50MS:     ts.P50MS,
			P95MS:     ts.P95MS,
			FlushAt:   now,
		}
		_ = enc.Encode(rec) // best-effort; ignore errors
	}
}

// ReadRollups reads the last nDays of daily rollup files and returns all records,
// newest first. nDays must be >= 1; values > 30 are capped at 30 to avoid
// scanning excessive history.
func ReadRollups(metricsDir string, nDays int) ([]RollupRecord, error) {
	if nDays < 1 {
		nDays = 1
	}
	if nDays > 30 {
		nDays = 30
	}
	var out []RollupRecord
	now := time.Now().UTC()
	for i := 0; i < nDays; i++ {
		day := now.AddDate(0, 0, -i)
		date := day.Format("2006-01-02")
		path := filepath.Join(metricsDir, fmt.Sprintf("mcp-%s.jsonl", date))
		recs, err := readRollupFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return out, fmt.Errorf("read rollup %s: %w", date, err)
		}
		out = append(out, recs...)
	}
	return out, nil
}

func readRollupFile(path string) ([]RollupRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []RollupRecord
	dec := json.NewDecoder(f)
	for dec.More() {
		var r RollupRecord
		if err := dec.Decode(&r); err != nil {
			break // corrupt line; stop reading this file
		}
		out = append(out, r)
	}
	return out, nil
}
