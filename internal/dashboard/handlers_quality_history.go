package dashboard

// handlers_quality_history.go — GET /api/quality/history/{group}
//
// Returns the time-series health-score history for a group so the Quality
// surface can render a trend line.

import (
	"net/http"
	"strconv"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/quality"
)

// handleQualityHistory — GET /api/quality/history/{group}?days=N
//
// Query params:
//
//	days — how many days of history to return (default 30, max 365)
//
// Response body:
//
//	{
//	  "group":   "mygroup",
//	  "days":    30,
//	  "entries": [ { "timestamp", "group", "total_entities", "orphan_rate",
//	                 "bug_rate", "health_score", "recall_pct?" } … ]
//	}
func (s *Server) handleQualityHistory(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	days := 30
	if raw := r.URL.Query().Get("days"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			writeErr(w, http.StatusBadRequest, "days must be a positive integer")
			return
		}
		if n > 365 {
			n = 365
		}
		days = n
	}

	layout, err := daemon.DefaultLayout()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "cannot locate daemon root: "+err.Error())
		return
	}

	entries, err := quality.ReadHistory(layout.Root, group, days)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "reading history: "+err.Error())
		return
	}
	if entries == nil {
		entries = []quality.HealthEntry{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"group":   group,
		"days":    days,
		"entries": entries,
	})
}
