package mcp

// docstate.go — documentation-state tracking for grafel_whoami (issue #734).
//
// Reads <group>/.grafel/docgen-state.json (written by /grafel-tech-docs skill)
// and computes:
//
//	documentation_state  "never_generated" | "stale" | "fresh"
//	stale_count          count of source files modified after last_docgen_at
//	suggested_action     human-readable next step for the agent to surface
//
// The file is also read by the MCP server to enrich the grafel_whoami
// response, enabling agents to proactively suggest documentation generation.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
)

// docStateCache memoizes ComputeDocState results to keep grafel_whoami off
// the per-call os.Stat walk over every unique source file in the graph (on the
// 62K-entity self-graph that is thousands of stat syscalls per call — the
// dominant whoami cost after git subprocesses, #3325 / epic #3648).
//
// Invalidation key (docStateKey): the docgen-state file's mtime+size plus a
// digest of every loaded repo's graph mtime. The graph mtime is the
// index-completion signal — Reload() advances lr.mtime when a repo's graph.fb
// is rewritten by the indexer — so a reindex always busts this cache, keeping
// stale_count correct after re-indexing. A new /grafel-tech-docs run
// rewrites docgen-state.json (mtime advances) and likewise busts it. Source
// files only become graph-visible on the next reindex, so keying on graph mtime
// is the correct freshness boundary for the stale-count semantics.
var (
	docStateMu   sync.Mutex
	docStateMemo = map[string]docStateEntry{}
)

type docStateEntry struct {
	key    docStateKey
	result DocStateResult
}

type docStateKey struct {
	docgenMtime time.Time
	docgenSize  int64
	graphDigest int64 // sum of repo graph-mtime UnixNano values (order-independent)
}

// docStateCacheKey builds the invalidation key for a group's doc state. The
// boolean is false when the key cannot be formed (no docgen state on disk), in
// which case ComputeDocState short-circuits to "never_generated" cheaply and
// caching is unnecessary.
func docStateCacheKey(groupName string, lg *LoadedGroup) (docStateKey, bool) {
	fi, err := os.Stat(docstateFilePath(groupName))
	if err != nil {
		return docStateKey{}, false
	}
	var digest int64
	for _, r := range lg.Repos {
		if r == nil {
			continue
		}
		digest += r.mtime.UnixNano()
	}
	return docStateKey{docgenMtime: fi.ModTime(), docgenSize: fi.Size(), graphDigest: digest}, true
}

// resetDocStateCacheForTest clears the memo. Test-only.
func resetDocStateCacheForTest() {
	docStateMu.Lock()
	docStateMemo = map[string]docStateEntry{}
	docStateMu.Unlock()
}

// DocgenState is the on-disk shape of docgen-state.json.
// Written by the /grafel-tech-docs skill after a successful run;
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
//  1. $GRAFEL_HOME — explicit override used in tests and custom installs.
//  2. $HOME — honored explicitly so tests using t.Setenv("HOME", tmpDir)
//     work on all platforms including Windows where os.UserHomeDir() reads
//     USERPROFILE and ignores HOME.
//  3. os.UserHomeDir() fallback (production path, uses platform conventions).
func defaultDocstateDir(group string) string {
	base := os.Getenv("GRAFEL_HOME")
	if base == "" {
		home := os.Getenv("HOME")
		if home == "" {
			var err error
			home, err = os.UserHomeDir()
			if err != nil {
				return ""
			}
		}
		base = filepath.Join(home, ".grafel")
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
// This is called by the /grafel-tech-docs skill completion path.
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
	key, ok := docStateCacheKey(groupName, lg)
	if !ok {
		// No docgen-state on disk → "never_generated"; the uncached path
		// short-circuits cheaply with no stat walk, so skip the memo.
		return computeDocStateUncached(groupName, lg)
	}

	docStateMu.Lock()
	if ent, hit := docStateMemo[groupName]; hit && ent.key == key {
		docStateMu.Unlock()
		return ent.result
	}
	docStateMu.Unlock()

	res := computeDocStateUncached(groupName, lg)

	docStateMu.Lock()
	docStateMemo[groupName] = docStateEntry{key: key, result: res}
	docStateMu.Unlock()
	return res
}

// computeDocStateUncached is the original os.Stat-walk implementation; callers
// should use ComputeDocState which memoizes it (see docStateCache).
func computeDocStateUncached(groupName string, lg *LoadedGroup) DocStateResult {
	state, err := LoadDocgenState(groupName)
	if err != nil || state == nil || state.LastDocgenAt == nil {
		return DocStateResult{
			DocumentationState: "never_generated",
			LastDocgenAt:       nil,
			StaleCount:         0,
			SuggestedAction:    "run /grafel-tech-docs",
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
		repo.forEachEntity(func(e *graph.Entity) bool {
			abs := e.SourceFile
			if !filepath.IsAbs(abs) && repo.Path != "" {
				abs = filepath.Join(repo.Path, e.SourceFile)
			}
			if seen[abs] {
				return true
			}
			seen[abs] = true

			info, err := os.Stat(abs)
			if err != nil {
				return true
			}
			if info.ModTime().After(repoDocgen) {
				perRepoStale[repoName]++
				totalStale++
			}
			return true
		})
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
		return "run /grafel-tech-docs"
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
