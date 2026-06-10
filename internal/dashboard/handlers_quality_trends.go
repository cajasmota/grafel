package dashboard

// handlers_quality_trends.go — GET /api/quality/trends/{group}
//
// Returns per-metric time-series data for a group so the Quality surface
// can render inline sparklines. Each metric is returned as a separate
// series so the frontend can render individual mini-charts without
// having to fan-out multiple requests.
//
// Route registered in server.go:
//
//	GET /api/quality/trends/{group}?days=N

import (
	"net/http"
	"strconv"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/quality"
)

// TrendPoint is a single data point in a metric series.
type TrendPoint struct {
	// Timestamp is the ISO-8601 rebuild timestamp.
	Timestamp string `json:"ts"`
	// Value is the metric value at this point.
	Value float64 `json:"v"`
}

// MetricTrend holds the time series for one metric together with the
// goal threshold and the current-vs-reference delta.
type MetricTrend struct {
	// Label is the human-readable metric name.
	Label string `json:"label"`
	// Unit is the display unit ("%" or "count").
	Unit string `json:"unit"`
	// LowerIsBetter marks metrics where improvement means going down.
	LowerIsBetter bool `json:"lower_is_better"`
	// Goal is the target value (0 when no goal is set).
	Goal float64 `json:"goal,omitempty"`
	// Points is the time series, oldest first.
	Points []TrendPoint `json:"points"`
	// Latest is the most-recent value, or nil when no data.
	Latest *float64 `json:"latest,omitempty"`
	// Delta7d is the change vs. the point closest to 7 days ago.
	// Nil when insufficient history.
	Delta7d *float64 `json:"delta_7d,omitempty"`
	// Delta30d is the change vs. the point closest to 30 days ago.
	// Nil when insufficient history.
	Delta30d *float64 `json:"delta_30d,omitempty"`
}

// QualityTrendsReply is the wire shape for GET /api/quality/trends/{group}.
type QualityTrendsReply struct {
	Group string `json:"group"`
	Days  int    `json:"days"`
	// Metrics holds one series per trackable metric. It is only populated
	// when a real trend exists (≥2 recorded snapshots in the window).
	Metrics []MetricTrend `json:"metrics"`
	// HasHistory is true only when a genuine time series exists — i.e. there
	// are at least two recorded snapshots so a trend can be drawn. A freshly
	// indexed group (zero or one snapshot) reports false, and the frontend
	// shows an honest "no trend data yet" empty-state instead of a fabricated
	// or single-point line.
	HasHistory bool `json:"has_history"`
	// PointCount is the number of real snapshots in the requested window.
	// 0 = never recorded, 1 = freshly indexed (no trend yet), ≥2 = real trend.
	PointCount int `json:"point_count"`
}

// handleQualityTrends serves GET /api/quality/trends/{group}?days=N.
//
// It reads the health-history.jsonl file and builds one MetricTrend per
// trackable metric. All metrics with at least one non-nil data point are
// included; sparse metrics (e.g. coverage, cycles) simply have fewer points.
func (s *Server) handleQualityTrends(w http.ResponseWriter, r *http.Request) {
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

	reply := buildTrendsReply(group, days, entries)
	writeJSON(w, http.StatusOK, reply)
}

// buildTrendsReply constructs the full trends payload from a slice of
// HealthEntry values. The caller must pass entries sorted oldest-first.
func buildTrendsReply(group string, days int, entries []quality.HealthEntry) QualityTrendsReply {
	// A trend requires at least two recorded snapshots. With zero or one
	// snapshot there is no honest time series to draw, so we return an empty
	// payload with has_history=false rather than emitting single-point series
	// (which the UI would render as "insufficient history" cards) or any
	// synthetic/fabricated line. The frontend renders its honest empty-state
	// off has_history / an empty metrics list.
	if len(entries) < 2 {
		return QualityTrendsReply{
			Group:      group,
			Days:       days,
			Metrics:    []MetricTrend{},
			HasHistory: false,
			PointCount: len(entries),
		}
	}

	// Build per-metric point lists. We always emit the core metrics
	// (health_score, orphan_rate, bug_rate). Extended metrics are only
	// emitted when at least one entry has a non-nil value.

	type extractor struct {
		label         string
		unit          string
		lowerIsBetter bool
		goal          float64
		fn            func(e quality.HealthEntry) *float64
	}

	ptrF := func(v float64) *float64 { return &v }

	extractors := []extractor{
		{
			label:         "Health score",
			unit:          "%",
			lowerIsBetter: false,
			goal:          90,
			fn:            func(e quality.HealthEntry) *float64 { return ptrF(e.HealthScore) },
		},
		{
			label:         "Orphan rate",
			unit:          "%",
			lowerIsBetter: true,
			goal:          5,
			fn:            func(e quality.HealthEntry) *float64 { return ptrF(e.OrphanRate) },
		},
		{
			label:         "Bug rate",
			unit:          "%",
			lowerIsBetter: true,
			goal:          3,
			fn:            func(e quality.HealthEntry) *float64 { return ptrF(e.BugRate) },
		},
		{
			label:         "Test coverage",
			unit:          "%",
			lowerIsBetter: false,
			goal:          80,
			fn:            func(e quality.HealthEntry) *float64 { return e.CoveragePct },
		},
		{
			label:         "Import cycles",
			unit:          "count",
			lowerIsBetter: true,
			goal:          0,
			fn: func(e quality.HealthEntry) *float64 {
				if e.Cycles == nil {
					return nil
				}
				v := float64(*e.Cycles)
				return &v
			},
		},
		{
			label:         "Auth-uncovered endpoints",
			unit:          "count",
			lowerIsBetter: true,
			goal:          0,
			fn: func(e quality.HealthEntry) *float64 {
				if e.AuthUncovered == nil {
					return nil
				}
				v := float64(*e.AuthUncovered)
				return &v
			},
		},
		{
			label:         "Secret findings",
			unit:          "count",
			lowerIsBetter: true,
			goal:          0,
			fn: func(e quality.HealthEntry) *float64 {
				if e.Secrets == nil {
					return nil
				}
				v := float64(*e.Secrets)
				return &v
			},
		},
	}

	var metrics []MetricTrend
	for _, ex := range extractors {
		var points []TrendPoint
		for _, e := range entries {
			v := ex.fn(e)
			if v == nil {
				continue
			}
			points = append(points, TrendPoint{
				Timestamp: e.Timestamp.UTC().Format("2006-01-02T15:04:05Z"),
				Value:     *v,
			})
		}
		// Skip metrics that have no data at all.
		if len(points) == 0 {
			continue
		}

		mt := MetricTrend{
			Label:         ex.label,
			Unit:          ex.unit,
			LowerIsBetter: ex.lowerIsBetter,
			Goal:          ex.goal,
			Points:        points,
		}

		latest := points[len(points)-1].Value
		mt.Latest = &latest

		// Compute deltas vs ~7d and ~30d reference windows.
		if d := refDelta(entries, ex.fn, 7); d != nil {
			mt.Delta7d = d
		}
		if d := refDelta(entries, ex.fn, 30); d != nil {
			mt.Delta30d = d
		}

		metrics = append(metrics, mt)
	}
	if metrics == nil {
		metrics = []MetricTrend{}
	}

	return QualityTrendsReply{
		Group:      group,
		Days:       days,
		Metrics:    metrics,
		HasHistory: true,
		PointCount: len(entries),
	}
}

// refDelta returns (latest − reference) for a metric, where reference is
// the entry closest to daysAgo days before the most-recent timestamp.
// Returns nil when there are fewer than 2 data points or when the metric
// has no value at the reference point.
func refDelta(entries []quality.HealthEntry, fn func(quality.HealthEntry) *float64, daysAgo int) *float64 {
	// Find the latest entry that has a value.
	var latestVal *float64
	for i := len(entries) - 1; i >= 0; i-- {
		if v := fn(entries[i]); v != nil {
			latestVal = v
			break
		}
	}
	if latestVal == nil {
		return nil
	}

	// Reference: find last entry older than daysAgo.
	windowMS := int64(daysAgo) * 24 * 60 * 60 * 1000
	latestTs := entries[len(entries)-1].Timestamp.UnixMilli()
	cutoff := latestTs - windowMS

	var refVal *float64
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Timestamp.UnixMilli() <= cutoff {
			if v := fn(entries[i]); v != nil {
				refVal = v
			}
			break
		}
	}
	if refVal == nil {
		return nil
	}

	delta := *latestVal - *refVal
	return &delta
}
