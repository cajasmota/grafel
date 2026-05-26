// cleanup.go — archigraph docgen cleanup (issue #2216, epic #2207).
//
// Removes stale staging runs and .previous-* backups from the docgen store.
//
// Staging layout (per-project):
//
//	<project_root>/.archigraph/staging/<run_id>/
//
// Canonical layout:
//
//	~/.archigraph/docs/<group>/
//
// Backup layout created by promote:
//
//	~/.archigraph/docs/<group>.previous-<timestamp>/
//
// The run_id encodes its creation date as the first 10 characters
// (e.g. "2026-05-25-a3b4c5d6"). When parsing fails we fall back to
// the directory ModTime.
package docgen

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CleanupOptions controls RunDocgenCleanup behaviour.
type CleanupOptions struct {
	// Group scopes the cleanup to a single group. Empty = all groups.
	Group string

	// MaxAge is the age threshold for stale runs and previous-* backups.
	// Entries older than MaxAge are removed. Default: 7 days.
	MaxAge time.Duration

	// DryRun reports what would be removed without touching the filesystem.
	DryRun bool

	// HomeDir overrides the home directory lookup (for tests).
	HomeDir string

	// ProjectRoots is a list of project roots to scan for staging dirs.
	// When nil, RunDocgenCleanup attempts to discover them from the canonical
	// docs store (best-effort). In tests, inject known roots directly.
	ProjectRoots []string
}

// CleanupResult summarises what RunDocgenCleanup accomplished.
type CleanupResult struct {
	// RemovedPaths is the list of paths that were (or would be) removed.
	RemovedPaths []string

	// TotalBytes is the total disk space freed (or that would be freed).
	TotalBytes int64

	// Errors contains non-fatal per-path errors encountered during the run.
	Errors []string
}

// RunDocgenCleanup removes stale staging runs and .previous-* backups.
//
// Stale = created more than opts.MaxAge ago (default 7 days).
// Canonical docs (~/.archigraph/docs/<group>/) are never touched.
func RunDocgenCleanup(opts CleanupOptions) (*CleanupResult, error) {
	if opts.MaxAge <= 0 {
		opts.MaxAge = 7 * 24 * time.Hour
	}

	homeDir, err := resolveHomeDir(opts.HomeDir)
	if err != nil {
		return nil, fmt.Errorf("resolve home: %w", err)
	}

	result := &CleanupResult{}
	cutoff := time.Now().Add(-opts.MaxAge)

	// ── 1. Clean .previous-* backups under ~/.archigraph/docs/ ───────────────
	// homeDir is already the archigraph home (resolveHomeDir guarantees this).
	docsRoot := filepath.Join(homeDir, "docs")
	if err := cleanPreviousBackups(docsRoot, opts.Group, cutoff, opts.DryRun, result); err != nil {
		// Non-fatal: record and continue.
		result.Errors = append(result.Errors, fmt.Sprintf("scan %s: %v", docsRoot, err))
	}

	// ── 2. Clean stale staging dirs ──────────────────────────────────────────
	// When the caller provides explicit project roots (tests), use those.
	// Otherwise fall back to best-effort discovery from the docs store.
	roots := opts.ProjectRoots
	if len(roots) == 0 {
		roots = discoverProjectRoots(docsRoot)
	}

	for _, root := range roots {
		stagingBase := filepath.Join(root, ".archigraph", "staging")
		if err := cleanStagingRuns(stagingBase, opts.Group, cutoff, opts.DryRun, result); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("scan %s: %v", stagingBase, err))
		}
	}

	return result, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

// cleanPreviousBackups removes entries under docsRoot that match:
//
//	<group>.previous-<timestamp>/  (when group != "")
//	*.previous-<timestamp>/       (when group == "")
//
// and are older than cutoff.
func cleanPreviousBackups(docsRoot, group string, cutoff time.Time, dryRun bool, result *CleanupResult) error {
	entries, err := os.ReadDir(docsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()

		// Match <something>.previous-<anything>
		idx := strings.Index(name, ".previous-")
		if idx < 0 {
			continue
		}
		// Group filter.
		if group != "" {
			groupPrefix := group + ".previous-"
			if !strings.HasPrefix(name, groupPrefix) {
				continue
			}
		}

		info, err := e.Info()
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("stat %s: %v", name, err))
			continue
		}
		if info.ModTime().After(cutoff) {
			continue // fresh enough
		}

		fullPath := filepath.Join(docsRoot, name)
		size := dirSize(fullPath)

		if dryRun {
			fmt.Fprintf(os.Stderr, "archigraph docgen cleanup (dry-run): would remove %s (%s)\n",
				fullPath, humanBytes(size))
			result.RemovedPaths = append(result.RemovedPaths, fullPath)
			result.TotalBytes += size
			continue
		}

		if err := os.RemoveAll(fullPath); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("remove %s: %v", fullPath, err))
			continue
		}
		fmt.Fprintf(os.Stderr, "archigraph docgen cleanup: removed %s (%s)\n",
			fullPath, humanBytes(size))
		result.RemovedPaths = append(result.RemovedPaths, fullPath)
		result.TotalBytes += size
	}
	return nil
}

// cleanStagingRuns removes entries under stagingBase that are older than cutoff.
// Each entry is a <run_id>/ directory. The run creation date is extracted from
// the run_id prefix (YYYY-MM-DD). When parsing fails we fall back to ModTime.
//
// When group != "", only run_ids that belong to the given group are removed.
// Since staging dirs are not group-labelled on disk, we use the presence of a
// ".group" marker file written by start_run. When absent we fall back to
// checking the in-memory registry (best-effort; always clean when no registry).
func cleanStagingRuns(stagingBase, group string, cutoff time.Time, dryRun bool, result *CleanupResult) error {
	entries, err := os.ReadDir(stagingBase)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		runID := e.Name()
		fullPath := filepath.Join(stagingBase, runID)

		// Determine the group this run belongs to (from .group marker or skip).
		if group != "" {
			runGroup := readRunGroupMarker(fullPath)
			if runGroup != "" && runGroup != group {
				continue
			}
			// If no marker exists and a group filter is active, we cannot
			// determine ownership — skip conservatively.
			if runGroup == "" {
				continue
			}
		}

		// Determine age: parse date prefix from run_id, fall back to ModTime.
		createdAt := parseRunIDDate(runID)
		if createdAt.IsZero() {
			info, err := e.Info()
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("stat %s: %v", runID, err))
				continue
			}
			createdAt = info.ModTime()
		}

		if createdAt.After(cutoff) {
			continue // still fresh
		}

		size := dirSize(fullPath)

		if dryRun {
			fmt.Fprintf(os.Stderr, "archigraph docgen cleanup (dry-run): would remove staging %s (%s)\n",
				fullPath, humanBytes(size))
			result.RemovedPaths = append(result.RemovedPaths, fullPath)
			result.TotalBytes += size
			continue
		}

		if err := os.RemoveAll(fullPath); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("remove staging %s: %v", fullPath, err))
			continue
		}
		fmt.Fprintf(os.Stderr, "archigraph docgen cleanup: removed staging %s (%s)\n",
			fullPath, humanBytes(size))
		result.RemovedPaths = append(result.RemovedPaths, fullPath)
		result.TotalBytes += size
	}
	return nil
}

// readRunGroupMarker reads ~/.archigraph/staging/<run_id>/.group if present.
// Returns "" when the file does not exist or cannot be read.
func readRunGroupMarker(runDir string) string {
	data, err := os.ReadFile(filepath.Join(runDir, ".group"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// parseRunIDDate extracts the date from a run_id like "2026-05-25-a3b4c5d6".
// Returns zero time when the format does not match.
func parseRunIDDate(runID string) time.Time {
	if len(runID) < 10 {
		return time.Time{}
	}
	t, err := time.Parse("2006-01-02", runID[:10])
	if err != nil {
		return time.Time{}
	}
	return t
}

// discoverProjectRoots attempts to enumerate project roots that may have
// staging dirs. This is a best-effort heuristic: we look for any
// ".archigraph/staging" dir under the user's home directory, capped to
// avoid runaway traversal.
//
// In practice, the daemon knows the registered project roots; this fallback
// is used by the standalone CLI cleanup command when no roots are injected.
func discoverProjectRoots(docsRoot string) []string {
	// Derive the archigraph home from docsRoot (parent of docs/).
	archigraphHome := filepath.Dir(docsRoot)
	if archigraphHome == "." || archigraphHome == "" {
		return nil
	}

	// Walk one level up to find project-level .archigraph/staging dirs.
	// We limit to the parent of archigraphHome (usually ~/) as a safety cap.
	homeDir := filepath.Dir(archigraphHome)
	if homeDir == "." || homeDir == "" {
		return nil
	}

	var roots []string
	// Walk up to 3 levels below homeDir, looking for .archigraph/staging.
	_ = filepath.WalkDir(homeDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			return nil
		}
		// Avoid descending into the archigraph home itself (not a project).
		if path == archigraphHome {
			return filepath.SkipDir
		}
		// Cap depth: count path separators relative to homeDir.
		rel, relErr := filepath.Rel(homeDir, path)
		if relErr != nil {
			return nil
		}
		depth := strings.Count(rel, string(filepath.Separator))
		if depth > 4 {
			return filepath.SkipDir
		}
		// Check for .archigraph/staging.
		staging := filepath.Join(path, ".archigraph", "staging")
		if info, statErr := os.Stat(staging); statErr == nil && info.IsDir() {
			roots = append(roots, path)
			return filepath.SkipDir // don't recurse into the project
		}
		return nil
	})
	return roots
}

// dirSize returns the total size in bytes of all files in dir. Returns 0 on error.
func dirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total
}

// humanBytes formats a byte count as a human-readable string.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// resolveHomeDir returns the archigraph home directory using the same
// convention as registry.HomeDir:
//   - override if explicitly provided (used by tests and the daemon)
//   - $ARCHIGRAPH_HOME if the environment variable is set
//   - ~/.archigraph otherwise
//
// The returned path IS the archigraph home (e.g. ~/.archigraph), NOT the raw
// OS home directory, so callers must NOT append an extra ".archigraph" segment.
func resolveHomeDir(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	if env := os.Getenv("ARCHIGRAPH_HOME"); env != "" {
		return env, nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".archigraph"), nil
}
