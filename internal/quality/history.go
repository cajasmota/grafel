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
