package dashboard

// handlers_perf.go — Performance budget monitor REST surface (#1319)
//
// Exposes recorded metric samples and budget evaluations so the Diagnostics
// surface can show green / yellow / red budget status with sparklines.
//
// Routes registered in server.go:
//
//	GET  /api/perf/budgets        — current vs baseline for all metrics
//	POST /api/perf/record         — record a one-off sample (CLI / daemon hooks)

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/cajasmota/archigraph/internal/perf"
	"github.com/cajasmota/archigraph/internal/registry"
)

// ─────────────────────────────────────────────────────────────────────────────
// Wire shapes
// ─────────────────────────────────────────────────────────────────────────────

// PerfBudgetsReply is the wire shape for GET /api/perf/budgets.
type PerfBudgetsReply struct {
	// CheckedAt is the RFC 3339 timestamp of when this snapshot was generated.
	CheckedAt string `json:"checked_at"`
	// Statuses holds one entry per (metric, group) pair that has recorded data,
	// plus entries for all daemon-wide metrics regardless of recorded data.
	Statuses []perf.BudgetStatus `json:"statuses"`
	// HistoryPath is the absolute path of the JSONL history file being read.
	HistoryPath string `json:"history_path"`
	// AnyWarning is true when at least one status is yellow or red.
	AnyWarning bool `json:"any_warning"`
}

// PerfRecordRequest is the wire shape for POST /api/perf/record.
type PerfRecordRequest struct {
	Metric string  `json:"metric"`
	Group  string  `json:"group,omitempty"`
	Value  float64 `json:"value"`
}

// PerfRecordReply is the wire shape for POST /api/perf/record.
type PerfRecordReply struct {
	Recorded bool   `json:"recorded"`
	Metric   string `json:"metric"`
	Group    string `json:"group,omitempty"`
	Value    float64 `json:"value"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Handlers
// ─────────────────────────────────────────────────────────────────────────────

// handlePerfBudgets — GET /api/perf/budgets
//
// Evaluates all tracked metrics against configured budgets and returns
// current / baseline / trend / sparkline data. Safe to poll every 30 s.
func (s *Server) handlePerfBudgets(w http.ResponseWriter, _ *http.Request) {
	rec, ev, histPath := s.perfComponents()

	_ = rec // rec is used indirectly through ev

	statuses := ev.EvaluateAll()

	// Also snapshot daemon RSS right now so it's always fresh.
	rss := getRSSMB()
	if err := rec.Record("daemon_rss_mb", "", rss); err == nil {
		// Refresh the daemon_rss_mb status with the live reading.
		for i, st := range statuses {
			if st.Metric == "daemon_rss_mb" && st.Group == "" {
				statuses[i] = ev.Evaluate("daemon_rss_mb", "")
			}
		}
	}

	anyWarning := false
	for _, st := range statuses {
		if st.Status == "yellow" || st.Status == "red" || st.Warning != "" {
			anyWarning = true
			break
		}
	}

	writeJSON(w, http.StatusOK, PerfBudgetsReply{
		CheckedAt:   time.Now().UTC().Format(time.RFC3339),
		Statuses:    statuses,
		HistoryPath: histPath,
		AnyWarning:  anyWarning,
	})
}

// handlePerfRecord — POST /api/perf/record
//
// Accepts a single metric sample from external callers (CLI, daemon hooks).
// Returns 400 on invalid JSON or missing metric name.
func (s *Server) handlePerfRecord(w http.ResponseWriter, r *http.Request) {
	var req PerfRecordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Metric == "" {
		writeErr(w, http.StatusBadRequest, "metric field required")
		return
	}

	rec, _, _ := s.perfComponents()
	if err := rec.Record(req.Metric, req.Group, req.Value); err != nil {
		writeErr(w, http.StatusInternalServerError, "record: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, PerfRecordReply{
		Recorded: true,
		Metric:   req.Metric,
		Group:    req.Group,
		Value:    req.Value,
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ─────────────────────────────────────────────────────────────────────────────

// perfComponents lazily constructs (or returns cached) perf.Recorder and
// perf.Evaluator wired to the archigraph home directory's history file.
// It reads the current settings to pick up any configured perf_budgets.
func (s *Server) perfComponents() (*perf.Recorder, *perf.Evaluator, string) {
	homeDir, _ := registry.HomeDir()
	histPath := homeDir + "/perf-history.jsonl"

	// Build or reuse the recorder. A simple approach: always construct a fresh
	// recorder that picks up new lines via its pre-loaded ring. Because NewRecorder
	// reads the file on construction, production callers should cache this on the
	// Server — see perfRecorder field below. For correctness without the cache we
	// just re-read; the file is small.
	s.perfMu.Lock()
	if s.perfRecorder == nil {
		s.perfRecorder = perf.NewRecorder(histPath)
	}
	rec := s.perfRecorder
	s.perfMu.Unlock()

	// Load per-user budget overrides from settings.json.
	var budgets map[string]float64
	if settings, err := loadSettings(); err == nil && len(settings.PerfBudgets) > 0 {
		budgets = settings.PerfBudgets
	}

	ev := perf.NewEvaluator(rec, budgets)
	return rec, ev, histPath
}
