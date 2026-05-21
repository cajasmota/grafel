// Package quality — health-score history storage.
//
// After every rebuild, a single JSONL line is appended to
// ~/.archigraph/health-history.jsonl so users can see whether graph
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
	// Group is the archigraph group name.
	Group string `json:"group"`
	// TotalEntities is the total entity count across all repos in the group.
	TotalEntities int `json:"total_entities"`
	// OrphanRate is the percentage of entities with no incoming relationship (0–100).
	OrphanRate float64 `json:"orphan_rate"`
	// BugRate is the percentage of entities that are repair candidates (0–100).
	// Zero when not applicable.
	BugRate float64 `json:"bug_rate"`
	// HealthScore is a composite quality score (0–100, higher is better).
	// Computed as max(0, 100 - OrphanRate - BugRate).
	HealthScore float64 `json:"health_score"`
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
// daemon root. If root is empty the default ~/.archigraph directory is used.
func historyFilename(root string) (string, error) {
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("quality/history: cannot locate home dir: %w", err)
		}
		root = filepath.Join(home, ".archigraph")
	}
	return filepath.Join(root, "health-history.jsonl"), nil
}

// AppendEntry appends a single HealthEntry to the history JSONL file stored
// under root (typically the value of daemon.Layout.Root, i.e. ~/.archigraph).
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
