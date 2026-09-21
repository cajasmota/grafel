// Package quality — health-score history storage.
//
// After every rebuild, a single JSONL line is appended to
// ~/.grafel/health-history.jsonl so users can see whether graph
// quality is improving or degrading over time.
//
// File format: one JSON object per line, newest entries at the end.
// All floats are 0–100 percentages.
package quality

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"
)

// HealthEntry is one measurement recorded after a rebuild.
type HealthEntry struct {
	// Timestamp is when the rebuild completed (RFC 3339).
	Timestamp time.Time `json:"timestamp"`
	// Group is the grafel group name.
	Group string `json:"group"`
	// TotalEntities is the total entity count across all repos in the group.
	TotalEntities int `json:"total_entities"`
	// TotalFlows is the number of process-flow entities in the group.
	TotalFlows int `json:"total_flows,omitempty"`
	// TotalEndpoints is the number of http_endpoint entities in the group.
	TotalEndpoints int `json:"total_endpoints,omitempty"`
	// OrphanRate is the percentage of entities with no incoming relationship (0–100).
	OrphanRate float64 `json:"orphan_rate"`
	// BugRate is the percentage of IMPORTS edges whose target did not resolve
	// to an addressable entity (0–100) — NOT, as this comment said until
	// #7283, a repair-candidate rate over entities.
	//
	// Omitted (null) when nothing was measured: no repo's graph could be
	// scanned, or the repos that were scanned carry no IMPORTS edge at all.
	// A bare float64 could not say that — 0.0 meant both "every import
	// resolved" and "nobody counted", and the second reads as a perfect
	// score everywhere this file is consumed (see internal/quality/audit's
	// BugRate, which carries the counts for exactly this reason).
	//
	// NOTE for readers of an old file: rows written before #7283 stored a
	// literal `"bug_rate":0` in both states and decode as a measured zero.
	// Absence, not zero, is the unmeasured signal, and only from #7283 on.
	BugRate *float64 `json:"bug_rate,omitempty"`
	// HealthScore is a composite quality score (0–100, higher is better).
	// Computed as max(0, 100 - OrphanRate - BugRate).
	//
	// Omitted (null) when BugRate is, because ComputeHealthScore has no
	// unknown state: handing it a 0 for an unmeasured bug rate does not
	// produce an uncertain score, it produces an INFLATED one (#7283).
	HealthScore *float64 `json:"health_score,omitempty"`
	// CoveragePct is the test-coverage percentage (0–100) measured from
	// Test-entity → production-entity edges. Omitted when not available.
	CoveragePct *float64 `json:"coverage_pct,omitempty"`
	// Cycles is the total number of import cycles detected. Omitted when
	// cycle detection was not run.
	Cycles *int `json:"cycles,omitempty"`
	// AuthUncovered is the number of HTTP endpoints with no auth annotation.
	// Omitted when not available.
	AuthUncovered *int `json:"auth_uncovered,omitempty"`
	// Secrets is the total number of hardcoded-secret findings. Omitted when
	// the secret scan was not run.
	Secrets *int `json:"secrets,omitempty"`
	// RecallPct is the enrichment recall percentage when available (0–100).
	// Omitted (null) when not measured.
	RecallPct *float64 `json:"recall_pct,omitempty"`
}

// HealthScore computes a composite score given orphan and bug rates.
// The score is clamped to [0, 100].
func ComputeHealthScore(orphanRate, bugRate float64) float64 {
	s := 100.0 - orphanRate - bugRate
	if s < 0 {
		return 0
	}
	if s > 100 {
		return 100
	}
	return s
}

// historyFilename returns the path to the JSONL history file for the given
// daemon root. If root is empty the default ~/.grafel directory is used.
func historyFilename(root string) (string, error) {
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("quality/history: cannot locate home dir: %w", err)
		}
		root = filepath.Join(home, ".grafel")
	}
	return filepath.Join(root, "health-history.jsonl"), nil
}

// AppendEntry appends a single HealthEntry to the history JSONL file stored
// under root (typically the value of daemon.Layout.Root, i.e. ~/.grafel).
// The directory is created when it does not exist. A newline is always written
// after the JSON so the file remains valid JSONL.
func AppendEntry(root string, e HealthEntry) error {
	path, err := historyFilename(root)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("quality/history: mkdir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("quality/history: open: %w", err)
	}
	defer f.Close()

	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("quality/history: marshal: %w", err)
	}
	line = append(line, '\n')
	_, err = f.Write(line)
	return err
}

// ReadHistory reads up to the most recent maxDays days of entries for the
// given group from the JSONL file stored under root. Entries outside the time
// window or belonging to other groups are skipped. An empty slice (not an
// error) is returned when the file does not exist yet.
func ReadHistory(root, group string, maxDays int) ([]HealthEntry, error) {
	path, err := historyFilename(root)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("quality/history: open: %w", err)
	}
	defer f.Close()

	cutoff := time.Now().UTC().AddDate(0, 0, -maxDays)

	var entries []HealthEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e HealthEntry
		if err := json.Unmarshal(line, &e); err != nil {
			// Skip malformed lines rather than aborting.
			continue
		}
		if e.Group != group {
			continue
		}
		// #7301 — a numeric field outside the range its producers can emit
		// is corruption, and skipping the line is what this loop already
		// does with a line that does not parse. Not clamped: substituting
		// an invented number is the defect class #7283/#7287/#7292 were
		// about. Consumers fail in both directions on such a value — a
		// negative prev lowers RegressionDetected's bar, and an
		// out-of-range rate trips CheckBudgets — so one bad line can
		// fabricate both a regression and a budget breach.
		if outOfRangeField(&e) != "" {
			continue
		}
		if maxDays > 0 && e.Timestamp.Before(cutoff) {
			continue
		}
		entries = append(entries, e)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("quality/history: scan: %w", err)
	}
	return entries, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Range validation (#7301)
// ─────────────────────────────────────────────────────────────────────────────

// noUpperBound is the hi of a field that is a count rather than a percentage:
// counts have a floor at zero and no ceiling this package can justify.
var noUpperBound = math.Inf(1)

// numericRange describes one numeric field of HealthEntry and the values
// ReadHistory will accept for it.
//
// Every range here is taken from the field's producers, not from the doc
// comment on the struct:
//
//   - orphan_rate: 100*orphans/entities, where the orphans are counted among
//     the entities. Three producers: cmd/grafel/rebuild_history.go,
//     cmd/grafel/quality_corpus.go, and internal/cli/repair.go via
//     RebuildSummary.OrphanRate (internal/cli/rebuild_summary.go), whose
//     OrphanEntities are counted in a second loop over the same per-repo
//     entities that produced TotalEntities.
//   - bug_rate: audit.BugRate.Pct, whose numerator is TotalImports-Resolved
//     and whose denominator is TotalImports, with Resolved only ever
//     incremented alongside Total (internal/quality/audit/bugrate.go).
//   - health_score: all three producers clamp — ComputeHealthScore above
//     (used by cmd/grafel/rebuild_history.go and internal/cli/repair.go) and
//     CompositeScoreFromPcts via clamp100 (internal/quality/composite.go,
//     reaching the record as Composite.Score in cmd/grafel/quality_corpus.go).
//   - coverage_pct: 100*CoveredProduction/TotalProduction, where the covered
//     set is a subset of the production set (internal/graph/coverage.go).
//   - recall_pct: no in-tree producer; the [0,100] range is the one its field
//     comment states.
//   - the counts: cardinalities of scans, so >= 0.
type numericRange struct {
	// name is the JSON field name, used only in tests.
	name string
	lo   float64
	hi   float64
	// value reports the field's value and whether it was measured at all. A
	// nil pointer field is "not measured" (#7283/#7287/#7292) — that is not
	// an out-of-range value and must survive the read.
	value func(e *HealthEntry) (v float64, measured bool)
}

func bare(v float64) (float64, bool) { return v, true }

func fromFloatPtr(p *float64) (float64, bool) {
	if p == nil {
		return 0, false
	}
	return *p, true
}

func fromIntPtr(p *int) (float64, bool) {
	if p == nil {
		return 0, false
	}
	return float64(*p), true
}

// numericRanges is the full set of numeric fields ReadHistory range-checks.
var numericRanges = []numericRange{
	{name: "total_entities", lo: 0, hi: noUpperBound, value: func(e *HealthEntry) (float64, bool) { return bare(float64(e.TotalEntities)) }},
	{name: "total_flows", lo: 0, hi: noUpperBound, value: func(e *HealthEntry) (float64, bool) { return bare(float64(e.TotalFlows)) }},
	{name: "total_endpoints", lo: 0, hi: noUpperBound, value: func(e *HealthEntry) (float64, bool) { return bare(float64(e.TotalEndpoints)) }},
	{name: "orphan_rate", lo: 0, hi: 100, value: func(e *HealthEntry) (float64, bool) { return bare(e.OrphanRate) }},
	{name: "bug_rate", lo: 0, hi: 100, value: func(e *HealthEntry) (float64, bool) { return fromFloatPtr(e.BugRate) }},
	{name: "health_score", lo: 0, hi: 100, value: func(e *HealthEntry) (float64, bool) { return fromFloatPtr(e.HealthScore) }},
	{name: "coverage_pct", lo: 0, hi: 100, value: func(e *HealthEntry) (float64, bool) { return fromFloatPtr(e.CoveragePct) }},
	{name: "recall_pct", lo: 0, hi: 100, value: func(e *HealthEntry) (float64, bool) { return fromFloatPtr(e.RecallPct) }},
	{name: "cycles", lo: 0, hi: noUpperBound, value: func(e *HealthEntry) (float64, bool) { return fromIntPtr(e.Cycles) }},
	{name: "auth_uncovered", lo: 0, hi: noUpperBound, value: func(e *HealthEntry) (float64, bool) { return fromIntPtr(e.AuthUncovered) }},
	{name: "secrets", lo: 0, hi: noUpperBound, value: func(e *HealthEntry) (float64, bool) { return fromIntPtr(e.Secrets) }},
}

// outOfRangeField returns the JSON name of the first numeric field of e whose
// value falls outside the range its producers can emit, or "" when every
// measured field is in range.
//
// The comparison is written as a rejected-unless-inside test rather than
// `v < lo || v > hi` so that a NaN — which compares false against everything —
// is rejected rather than accepted.
func outOfRangeField(e *HealthEntry) string {
	for _, r := range numericRanges {
		v, measured := r.value(e)
		if !measured {
			continue
		}
		if !(v >= r.lo && v <= r.hi) {
			return r.name
		}
	}
	return ""
}
