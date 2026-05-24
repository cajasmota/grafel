// Package watch — gitignore.go
//
// Thin bridge between the watcher's skip logic and the walk package's
// fully-featured gitignore parser. At AddRepo time we load the repo's
// root .gitignore once and expose a fast ShouldSkipDirGitignore helper
// that the subscribeRepo walk can consult.
//
// Design notes
//   - We reuse walk.ParseIgnoreFile so the watcher honours exactly the
//     same gitignore semantics as the indexer (no duplicate parser).
//   - Per-repo override: <repo>/.archigraph/watch.json with optional
//     include/exclude lists (see RepoWatchConfig).
//   - The cache is keyed by repo absolute path and is safe for concurrent
//     reads after the initial population in AddRepo.
package watch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/cajasmota/archigraph/internal/daemon/walk"
)

// RepoWatchConfig is the optional per-repo override stored at
// <repo>/.archigraph/watch.json. All fields are optional; the zero
// value means "no overrides".
type RepoWatchConfig struct {
	// ExcludeDirs lists additional directory basenames (beyond the global
	// SkipDirs) that should never be watched for this repo.
	ExcludeDirs []string `json:"exclude_dirs,omitempty"`
	// IncludeOnlyDirs, when non-empty, restricts subscription to exactly
	// these top-level directory names. Any top-level dir NOT in this list
	// is skipped outright. Has no effect on deeper levels (only top-level).
	IncludeOnlyDirs []string `json:"include_only_dirs,omitempty"`
}

// repoIgnoreState bundles the parsed .gitignore (may be nil/empty) and the
// optional per-repo watch config for a single repo.
type repoIgnoreState struct {
	gitignore   *walk.IgnoreFile // root .gitignore; never nil (may be no-op)
	watchCfg    RepoWatchConfig
	extraSkip   map[string]struct{} // fast set from watchCfg.ExcludeDirs
	includeOnly map[string]struct{} // fast set from watchCfg.IncludeOnlyDirs
}

// gitignoreCache is the process-wide cache of per-repo gitignore state.
// Populated by loadRepoIgnoreState; read-only after that.
type gitignoreCache struct {
	mu    sync.RWMutex
	repos map[string]*repoIgnoreState // key: absolute repo path
}

var repoIgnoreCache = &gitignoreCache{
	repos: make(map[string]*repoIgnoreState),
}

// loadRepoIgnoreState parses <repo>/.gitignore and <repo>/.archigraph/watch.json
// (both optional), caches the result, and returns the state. If repoPath was
// already cached the cached value is returned directly.
func loadRepoIgnoreState(repoPath string) *repoIgnoreState {
	repoIgnoreCache.mu.RLock()
	if s, ok := repoIgnoreCache.repos[repoPath]; ok {
		repoIgnoreCache.mu.RUnlock()
		return s
	}
	repoIgnoreCache.mu.RUnlock()

	// Parse .gitignore (non-fatal if absent).
	giPath := filepath.Join(repoPath, ".gitignore")
	ig, _ := walk.ParseIgnoreFile(repoPath, giPath, ".gitignore")
	if ig == nil {
		ig = &walk.IgnoreFile{}
	}

	// Parse per-repo watch.json override (non-fatal if absent).
	var cfg RepoWatchConfig
	cfgPath := filepath.Join(repoPath, ".archigraph", "watch.json")
	if data, err := os.ReadFile(cfgPath); err == nil {
		_ = json.Unmarshal(data, &cfg)
	}

	extraSkip := make(map[string]struct{}, len(cfg.ExcludeDirs))
	for _, d := range cfg.ExcludeDirs {
		extraSkip[d] = struct{}{}
	}
	includeOnly := make(map[string]struct{}, len(cfg.IncludeOnlyDirs))
	for _, d := range cfg.IncludeOnlyDirs {
		includeOnly[d] = struct{}{}
	}

	s := &repoIgnoreState{
		gitignore:   ig,
		watchCfg:    cfg,
		extraSkip:   extraSkip,
		includeOnly: includeOnly,
	}

	repoIgnoreCache.mu.Lock()
	repoIgnoreCache.repos[repoPath] = s
	repoIgnoreCache.mu.Unlock()
	return s
}

// evictRepoIgnoreState removes a repo from the cache. Called by RemoveRepo
// so that a subsequent re-add picks up any .gitignore changes.
func evictRepoIgnoreState(repoPath string) {
	repoIgnoreCache.mu.Lock()
	delete(repoIgnoreCache.repos, repoPath)
	repoIgnoreCache.mu.Unlock()
}

// ShouldSkipDirGitignore reports whether absDir should be excluded from the
// fsnotify subscription based on the repo's .gitignore and per-repo watch.json.
//
// repoPath must be the absolute repo root. absDir must be inside repoPath.
// relPath is absDir relative to repoPath (forward-slash, no leading slash).
//
// Returns (skip=true, reason) when the directory should be excluded.
func ShouldSkipDirGitignore(repoPath, absDir, relPath string) (bool, string) {
	s := loadRepoIgnoreState(repoPath)

	// Per-repo explicit exclude overrides.
	base := filepath.Base(absDir)
	if _, ok := s.extraSkip[base]; ok {
		return true, "watch.json:exclude_dirs"
	}

	// Per-repo include-only: if the top-level dir is not in the allow-list,
	// skip it. We detect "top-level" as a relPath with no slash separators.
	if len(s.includeOnly) > 0 {
		// Only apply at depth-1 directories directly under the repo root.
		// Deeper levels are not filtered here — they rely on the hard-skip list
		// and gitignore rules.
		if filepath.Dir(relPath) == "." {
			if _, ok := s.includeOnly[base]; !ok {
				return true, "watch.json:include_only_dirs"
			}
		}
	}

	// .gitignore match.
	if skip, line := s.gitignore.MatchDir(relPath); skip {
		return true, ".gitignore line " + strconv.Itoa(line)
	}

	return false, ""
}
