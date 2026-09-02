package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Lock mirrors the parts of grammars.lock that A2 consumes. Unknown fields are
// ignored, so the manifest can carry extra provenance without breaking parsing.
type Lock struct {
	SchemaVersion int           `json:"$schema_version"`
	Binding       Binding       `json:"binding"`
	LastVerified  string        `json:"last_verified"`
	Grammars      []GrammarSpec `json:"grammars"`
}

// Binding is the LEGACY "everything is bundled via one module" declaration.
//
// It is expected to be absent. It exists only so that reintroducing it is a
// hard error rather than a silent regression: #6749 was caused by this block
// naming github.com/smacker/go-tree-sitter — a module with zero occurrences in
// go.mod — and the checker using its pinned_date as EVERY grammar's bundled
// version. Bundled versions are now derived per grammar (see bundled.go).
type Binding struct {
	Module     string `json:"module"`
	Version    string `json:"version"`
	PinnedDate string `json:"pinned_date"`
}

// validateBinding refuses a lock that declares a bundling module the repo does
// not actually depend on. A binding naming a real go.mod module is allowed; no
// binding at all is the expected steady state.
func validateBinding(b Binding, goModPath string) error {
	if b.Module == "" {
		return nil
	}
	src, err := os.ReadFile(goModPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", goModPath, err)
	}
	for _, line := range strings.Split(string(src), "\n") {
		for _, f := range strings.Fields(stripLineComment(line)) {
			if f == b.Module {
				return nil
			}
		}
	}
	return fmt.Errorf("grammars.lock declares binding module %q (pinned_date %q) but it does not appear in %s: "+
		"a bundling module that is not a dependency cannot be any grammar's bundled version (#6749)",
		b.Module, b.PinnedDate, goModPath)
}

// GrammarSpec is one grammar-backed language's entry.
type GrammarSpec struct {
	Language              string   `json:"language"`
	Aliases               []string `json:"aliases"`
	Source                string   `json:"source"` // owner/repo on GitHub
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
