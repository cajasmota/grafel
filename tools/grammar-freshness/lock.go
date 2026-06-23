package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// Lock mirrors the parts of grammars.lock that A2 consumes. Unknown fields are
// ignored, so the manifest can carry extra provenance without breaking parsing.
type Lock struct {
	SchemaVersion int           `json:"$schema_version"`
	Binding       Binding       `json:"binding"`
	LastVerified  string        `json:"last_verified"`
	Grammars      []GrammarSpec `json:"grammars"`
}

// Binding records the smacker snapshot the grammars are bundled from.
type Binding struct {
	Module     string `json:"module"`
	Version    string `json:"version"`
	PinnedDate string `json:"pinned_date"`
}

// GrammarSpec is one grammar-backed language's entry.
type GrammarSpec struct {
	Language              string   `json:"language"`
	Aliases               []string `json:"aliases"`
	Source                string   `json:"source"` // owner/repo on GitHub
	BundledVia            string   `json:"bundled_via"`
	UpstreamLatestRelease string   `json:"upstream_latest_release"`
	UpstreamLatestCommit  string   `json:"upstream_latest_commit_date"`
	HighValue             bool     `json:"high_value"`
	BackfillC3            string   `json:"backfill_c3"`
}

// loadLock reads and parses grammars.lock.
func loadLock(path string) (*Lock, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var l Lock
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(l.Grammars) == 0 {
		return nil, fmt.Errorf("%s: no grammars found", path)
	}
	return &l, nil
}
