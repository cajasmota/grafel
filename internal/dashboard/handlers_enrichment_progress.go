package dashboard

// handlers_enrichment_progress.go — Per-tier enrichment progress endpoint (#1286).
//
//	GET /api/enrichments/{group}/progress
//
// Returns a live snapshot of enrichment job progress bucketed into the four
// criticality bands (critical / high / medium / low). The frontend polls this
// endpoint every 3 s while any tier has running or queued jobs and renders
// animated progress bars in the /pending surface.
//
// ETA estimation uses a rolling window of the last 30 completed jobs. When
// fewer than 2 jobs have finished the eta_seconds field is omitted so the UI
// can render "calculating…" instead of a misleading number.

import (
	"net/http"
	"time"
)

// enrichmentProgressBand is one tier in the progress response.
type enrichmentProgressBand struct {
	Band       string `json:"band"`                  // "critical" | "high" | "medium" | "low"
	Total      int    `json:"total"`                 // total jobs in this band
	Done       int    `json:"done"`                  // completed (status == "done")
	Running    int    `json:"running"`               // currently executing
	Queued     int    `json:"queued"`                // waiting in queue
	Failed     int    `json:"failed"`                // failed / cancelled
	ETASeconds *int   `json:"eta_seconds,omitempty"` // nil when not enough data
}

// enrichmentProgressResponse is the wire type for GET /api/enrichments/{group}/progress.
type enrichmentProgressResponse struct {
	Tiers        []enrichmentProgressBand `json:"tiers"`
	OverallDone  int                      `json:"overall_done"`
	OverallTotal int                      `json:"overall_total"`
	StartedAt    *time.Time               `json:"started_at,omitempty"`
}

// bandForScore returns the criticality band for a 0-100 priority score,
// matching the thresholds used by the frontend TierDef.
func bandForScore(score float64) string {
	switch {
	case score >= 80:
		return "critical"
	case score >= 60:
		return "high"
	case score >= 40:
		return "medium"
	default:
		return "low"
	}
}

// handleEnrichmentProgress — GET /api/enrichments/{group}/progress
//
// The response is always 200 (even when no jobs exist) so the frontend can
// determine "not started" vs "in progress" from the counter values alone.
func (s *Server) handleEnrichmentProgress(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	// Band order: critical first (matches UI layout).
	bandOrder := []string{"critical", "high", "medium", "low"}

	// Counters per band.
	type counts struct {
		total, done, running, queued, failed int
	}
	byBand := map[string]*counts{
		"critical": {},
		"high":     {},
		"medium":   {},
		"low":      {},
	}

	var startedAt *time.Time

	// Accumulators for rolling-window ETA.
	// completedDurations holds the duration (seconds) of the last ≤30 done jobs.
	const etaWindow = 30
	var completedDurations []float64

	if s.jobQueue != nil {
		jobs := s.jobQueue.ListForGroup(group)

		for i := range jobs {
			j := &jobs[i]

			// Assign band from the job's CriticalityBand field (set by the
			// enrichment trigger handler). Fall back to "low" when absent.
			band := j.CriticalityBand
			if _, ok := byBand[band]; !ok {
				band = "low"
			}

			c := byBand[band]
			c.total++

			switch j.Status {
			case "done":
				c.done++
				if j.StartedAt != nil && j.FinishedAt != nil {
					dur := j.FinishedAt.Sub(*j.StartedAt).Seconds()
					if dur > 0 {
						completedDurations = append(completedDurations, dur)
					}
				}
			case "running":
				c.running++
				if j.StartedAt != nil && (startedAt == nil || j.StartedAt.Before(*startedAt)) {
					t := *j.StartedAt
					startedAt = &t
				}
			case "queued":
				c.queued++
			default: // "failed", "cancelled"
				c.failed++
			}
		}
	}

	// Compute throughput from the last etaWindow completions.
	var throughputPerSec float64
	if len(completedDurations) >= 2 {
		// Keep only the last etaWindow samples.
		window := completedDurations
		if len(window) > etaWindow {
			window = window[len(window)-etaWindow:]
		}
		var totalSec float64
		for _, d := range window {
			totalSec += d
		}
		// throughput = jobs / seconds = 1 / avg_seconds_per_job
		avgSec := totalSec / float64(len(window))
		if avgSec > 0 {
			throughputPerSec = 1.0 / avgSec
		}
	}

	// Build response tiers.
	tiers := make([]enrichmentProgressBand, 0, len(bandOrder))
	var overallDone, overallTotal int

	for _, band := range bandOrder {
		c := byBand[band]
		overallTotal += c.total
		overallDone += c.done

		remaining := c.queued + c.running
		var etaSeconds *int
		if throughputPerSec > 0 && remaining > 0 {
			eta := int(float64(remaining) / throughputPerSec)
			etaSeconds = &eta
		}

		tiers = append(tiers, enrichmentProgressBand{
			Band:       band,
			Total:      c.total,
			Done:       c.done,
			Running:    c.running,
			Queued:     c.queued,
			Failed:     c.failed,
			ETASeconds: etaSeconds,
		})
	}

	writeJSON(w, http.StatusOK, enrichmentProgressResponse{
		Tiers:        tiers,
		OverallDone:  overallDone,
		OverallTotal: overallTotal,
		StartedAt:    startedAt,
	})
}
