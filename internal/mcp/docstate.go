package mcp

// docstate.go — documentation-state tracking for archigraph_whoami (issue #734).
//
// Reads <group>/.archigraph/docgen-state.json (written by /generate-docs skill)
// and computes:
//
//	documentation_state  "never_generated" | "stale" | "fresh"
//	stale_count          count of source files modified after last_docgen_at
//	suggested_action     human-readable next step for the agent to surface
//
// The file is also read by the MCP server to enrich the archigraph_whoami
// response, enabling agents to proactively suggest documentation generation.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DocgenState is the on-disk shape of docgen-state.json.
// Written by the /generate-docs skill after a successful run;
// read here by the MCP server and daemon helpers.
type DocgenState struct {
	// LastDocgenAt is the RFC3339 timestamp of the last /generate-docs run.
	// Null / zero time means documentation has never been generated.
	LastDocgenAt *time.Time `json:"last_docgen_at"`

	// LastDocgenCommit is the git HEAD at the time of the last run (optional).
	// Useful for staleness reasoning beyond mtime.
	LastDocgenCommit string `json:"last_docgen_commit,omitempty"`

	// GeneratedPaths is the list of doc files produced in the last run.
	GeneratedPaths []string `json:"generated_paths,omitempty"`

	// PerRepo holds per-repo timestamps (populated when partial regeneration
	// was performed). Keys are repo names matching the registry entry.
	PerRepo map[string]*time.Time `json:"per_repo,omitempty"`
}

// DocStateResult is the computed documentation state for one group.
type DocStateResult struct {
	// DocumentationState is "never_generated", "stale", or "fresh".
	DocumentationState string `json:"documentation_state"`
	// LastDocgenAt is the timestamp of the last doc-gen run, or nil.
	LastDocgenAt *time.Time `json:"last_docgen_at"`
	// StaleCount is the number of source files changed since the last run.
	StaleCount int `json:"stale_count"`
	// SuggestedAction is the human-readable next step.
	SuggestedAction string `json:"suggested_action"`
	// PerRepoStale maps repo name → stale file count for detailed reporting.
	PerRepoStale map[string]int `json:"per_repo_stale,omitempty"`
}

// defaultDocstateDir returns the per-group docstate directory.
//
// Priority order:
//  1. $ARCHIGRAPH_HOME — explicit override used in tests and custom installs.
//     On Windows, os.UserHomeDir() reads USERPROFILE (not HOME), so tests that
//     only set HOME would silently write to the wrong location. ARCHIGRAPH_HOME
//     sidesteps this platform difference entirely.
//  2. $HOME/.archigraph (Unix) / %USERPROFILE%/.archigraph (Windows) via
//     os.UserHomeDir().
func defaultDocstateDir(group string) string {
	base := os.Getenv("ARCHIGRAPH_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".archigraph")
	}
	return filepath.Join(base, "groups", group)
}

// docstateFilePath returns the full path to docgen-state.json for a group.
func docstateFilePath(group string) string {
	return filepath.Join(defaultDocstateDir(group), "docgen-state.json")
}

// LoadDocgenState reads docgen-state.json for a group.
// Returns nil (not error) when the file does not exist (never_generated).
func LoadDocgenState(group string) (*DocgenState, error) {
	path := docstateFilePath(group)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("docstate: read %s: %w", path, err)
	}
	var st DocgenState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("docstate: parse %s: %w", path, err)
	}
	return &st, nil
}

// SaveDocgenState writes docgen-state.json atomically (tmp + rename).
// This is called by the /generate-docs skill completion path.
func SaveDocgenState(group string, st DocgenState) error {
	dir := defaultDocstateDir(group)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("docstate: mkdir %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("docstate: marshal: %w", err)
	}
	path := filepath.Join(dir, "docgen-state.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("docstate: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("docstate: rename: %w", err)
	}
	return nil
}

// ComputeDocState computes the documentation state for a loaded group.
//   - Reads docgen-state.json for the group.
//   - Walks every source file in every loaded repo to count stale files
//     (mtime > last_docgen_at, per-repo and group-wide).
//   - Counts per-repo per-repo stale files separately for detailed surfacing.
func ComputeDocState(groupName string, lg *LoadedGroup) DocStateResult {
	state, err := LoadDocgenState(groupName)
	if err != nil || state == nil || state.LastDocgenAt == nil {
		return DocStateResult{
			DocumentationState: "never_generated",
			LastDocgenAt:       nil,
			StaleCount:         0,
			SuggestedAction:    "run /generate-docs",
		}
	}

	lastDocgen := *state.LastDocgenAt
	perRepoStale := map[string]int{}
	totalStale := 0

	for repoName, repo := range lg.Repos {
		if repo == nil || repo.Doc == nil || repo.Path == "" {
			continue
		}

		// Use per-repo timestamp when available (partial regeneration).
		repoDocgen := lastDocgen
		if state.PerRepo != nil {
			if rt, ok := state.PerRepo[repoName]; ok && rt != nil {
				repoDocgen = *rt
			}
		}

		// Walk source files for this repo.
		seen := map[string]bool{}
		for i := range repo.Doc.Entities {
			e := &repo.Doc.Entities[i]
			abs := e.SourceFile
			if !filepath.IsAbs(abs) && repo.Path != "" {
				abs = filepath.Join(repo.Path, e.SourceFile)
			}
			if seen[abs] {
				continue
			}
			seen[abs] = true

			info, err := os.Stat(abs)
			if err != nil {
				continue
			}
			if info.ModTime().After(repoDocgen) {
				perRepoStale[repoName]++
				totalStale++
			}
		}
	}

	result := DocStateResult{
		LastDocgenAt: state.LastDocgenAt,
		StaleCount:   totalStale,
	}
	if len(perRepoStale) > 0 {
		result.PerRepoStale = perRepoStale
	}

	if totalStale > 0 {
		result.DocumentationState = "stale"
		result.SuggestedAction = fmt.Sprintf("refresh docs — %d file(s) changed since last generation", totalStale)
	} else {
		result.DocumentationState = "fresh"
		result.SuggestedAction = "none — graph is healthy"
	}
	return result
}

// composeSuggestedAction selects the highest-priority suggested_action given the
// full state picture. Priority: stale docs > pattern candidates > residuals > healthy.
func composeSuggestedAction(docState DocStateResult, candidateCount, residualCount int) string {
	// Docs-first: if never generated or stale, that dominates.
	if docState.DocumentationState == "never_generated" {
		return "run /generate-docs"
	}
	if docState.DocumentationState == "stale" {
		return fmt.Sprintf("refresh docs — %d file(s) changed since last generation", docState.StaleCount)
	}
	// Secondary: pattern candidates.
	if candidateCount > 0 {
		return fmt.Sprintf("review %d pending pattern candidate(s)", candidateCount)
	}
	// Tertiary: residual repairs.
	if residualCount > 0 {
		return fmt.Sprintf("review %d pending repair candidate(s)", residualCount)
	}
	return "none — graph is healthy"
}
