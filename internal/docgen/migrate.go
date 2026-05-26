// migrate.go — archigraph docgen migrate-in-repo (issue #2216, epic #2207).
//
// Extends the existing #2193-era migrate-in-repo concept to be aware of the
// staging-dir layout introduced in #2214/#2215:
//
//   - Walks every registered repo's docs/ and <project>/.archigraph/staging/ dirs
//   - Moves in-repo docs to ~/.archigraph/docs/<group>/
//   - Backs up any existing canonical before overwriting (to <canonical>.previous-<ts>/)
//   - Idempotent: already-migrated dirs are skipped
package docgen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MigrateOptions controls RunMigrateInRepo behaviour.
type MigrateOptions struct {
	// Group scopes the migration to one group. Empty = all groups registered.
	Group string

	// HomeDir overrides the home directory lookup (for tests).
	HomeDir string

	// GroupConfigLoader returns the list of repo paths and slugs for a group.
	// Must be non-nil. Injected by the CLI and daemon to decouple this package
	// from the registry package and avoid import cycles.
	GroupConfigLoader func(group string) ([]MigrateRepo, error)

	// GroupsLoader returns all registered group names.
	// Required when Group == "" (migrate all groups).
	GroupsLoader func() ([]string, error)

	// DryRun reports what would happen without touching the filesystem.
	DryRun bool

	// Yes skips prompts (used in non-interactive / CI mode).
	// Prompt is bypassed when true; the caller may also inject ConfirmFn.
	Yes bool

	// ConfirmFn is an optional override for the confirmation prompt.
	// When nil and Yes==false, the CLI wraps this with an interactive stdin reader.
	// Returning true = proceed, false = skip.
	ConfirmFn func(msg string) bool

	// StagingOnly restricts RunMigrateInRepo to orphaned staging runs (Part B)
	// and skips in-repo docs/ scanning (Part A). Set this when the caller
	// already handles docs/ migration itself (e.g. the CLI Phase 1 handler)
	// to avoid double-processing docs/ directories with a different idempotency
	// guard.
	StagingOnly bool
}

// MigrateRepo describes a single repo within a group.
type MigrateRepo struct {
	Slug string
	Path string
}

// MigrateResult summarises what RunMigrateInRepo accomplished.
type MigrateResult struct {
	// Migrated is the list of src→dst pairs that were moved.
	Migrated []MigratePair

	// Skipped is the list of paths that were not moved (already exists / user declined).
	Skipped []string

	// Errors contains non-fatal per-path errors.
	Errors []string
}

// MigratePair records a single src→dst move.
type MigratePair struct {
	Src    string
	Dst    string
	Backup string // path of the .previous-* backup, or ""
}

// RunMigrateInRepo migrates in-repo docs and staging dirs into the canonical
// ~/.archigraph/docs/<group>/ layout.
func RunMigrateInRepo(opts MigrateOptions) (*MigrateResult, error) {
	if opts.GroupConfigLoader == nil {
		return nil, fmt.Errorf("MigrateOptions.GroupConfigLoader must be set")
	}

	homeDir, err := resolveHomeDir(opts.HomeDir)
	if err != nil {
		return nil, fmt.Errorf("resolve home: %w", err)
	}

	// Resolve the group list.
	var groups []string
	if opts.Group != "" {
		groups = []string{opts.Group}
	} else {
		if opts.GroupsLoader == nil {
			return nil, fmt.Errorf("MigrateOptions.GroupsLoader must be set when Group is empty")
		}
		gs, err := opts.GroupsLoader()
		if err != nil {
			return nil, fmt.Errorf("list groups: %w", err)
		}
		groups = gs
	}

	result := &MigrateResult{}
	confirm := opts.ConfirmFn
	if confirm == nil {
		confirm = func(_ string) bool { return opts.Yes }
	}

	for _, group := range groups {
		repos, err := opts.GroupConfigLoader(group)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("load group %q config: %v", group, err))
			continue
		}

		// homeDir is already the archigraph home (resolveHomeDir guarantees
		// this convention) — do NOT append ".archigraph".
		canonicalBase := filepath.Join(homeDir, "docs", group)

		for _, repo := range repos {
			if repo.Path == "" {
				continue
			}

			// ── A. In-repo docs/ (or doc/) ────────────────────────────────
			// Skipped when StagingOnly is set — the CLI Phase-1 handler owns
			// docs/ migration and has already applied the idempotency guard.
			if !opts.StagingOnly {
				for _, sub := range []string{"docs", "doc"} {
					srcDir := filepath.Join(repo.Path, sub)
					info, statErr := os.Stat(srcDir)
					if statErr != nil || !info.IsDir() {
						continue
					}
					if !looksLikeDocgenOutput(srcDir) {
						continue
					}
					slug := repo.Slug
					if slug == "" {
						slug = filepath.Base(repo.Path)
					}
					dstDir := filepath.Join(canonicalBase, slug)
					if err := migrateDir(srcDir, dstDir, opts.DryRun, confirm, result); err != nil {
						result.Errors = append(result.Errors, fmt.Sprintf("migrate %s → %s: %v", srcDir, dstDir, err))
					}
				}
			}

			// ── B. In-repo .archigraph/staging/ runs ──────────────────────
			// Any staging run under the project root that was never promoted
			// (e.g. aborted runs) is moved to a timestamped subdir under the
			// canonical docs path so nothing is silently lost.
			stagingBase := filepath.Join(repo.Path, ".archigraph", "staging")
			stagingInfo, statErr := os.Stat(stagingBase)
			if statErr == nil && stagingInfo.IsDir() {
				entries, readErr := os.ReadDir(stagingBase)
				if readErr == nil {
					for _, e := range entries {
						if !e.IsDir() {
							continue
						}
						runID := e.Name()
						srcRun := filepath.Join(stagingBase, runID)
						// Destination: <canonical>/.staging-recovered/<run_id>/
						dstRun := filepath.Join(canonicalBase, ".staging-recovered", runID)
						if err := migrateDir(srcRun, dstRun, opts.DryRun, confirm, result); err != nil {
							result.Errors = append(result.Errors,
								fmt.Sprintf("migrate staging run %s → %s: %v", srcRun, dstRun, err))
						}
					}
				}
			}
		}
	}

	return result, nil
}

// migrateDir moves src to dst. If dst already exists it is backed up to
// <dst>.previous-<timestamp>/ before the move. Idempotent: if src does not
// exist, this is a no-op (returns nil).
func migrateDir(src, dst string, dryRun bool, confirm func(string) bool, result *MigrateResult) error {
	if _, err := os.Stat(src); err != nil {
		return nil // src gone — already migrated
	}

	msg := fmt.Sprintf("migrate %s → %s", src, dst)
	if !confirm(msg) {
		result.Skipped = append(result.Skipped, src)
		return nil
	}

	if dryRun {
		fmt.Fprintf(os.Stderr, "archigraph docgen migrate-in-repo (dry-run): would move %s → %s\n", src, dst)
		result.Migrated = append(result.Migrated, MigratePair{Src: src, Dst: dst})
		return nil
	}

	// Ensure parent directory.
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create parent for %s: %w", dst, err)
	}

	// Back up existing canonical if present.
	backup := ""
	if _, err := os.Stat(dst); err == nil {
		ts := time.Now().UTC().Format("20060102T150405Z")
		// Derive the group+slug from dst — the backup lives as a sibling.
		backup = dst + ".previous-" + ts
		if renErr := os.Rename(dst, backup); renErr != nil {
			return fmt.Errorf("backup existing canonical %s → %s: %w", dst, backup, renErr)
		}
	}

	// Move src → dst.
	if err := os.Rename(src, dst); err != nil {
		// Attempt to restore backup on failure.
		if backup != "" {
			_ = os.Rename(backup, dst)
		}
		return fmt.Errorf("move %s → %s: %w", src, dst, err)
	}

	fmt.Fprintf(os.Stderr, "archigraph docgen migrate-in-repo: moved %s → %s\n", src, dst)
	result.Migrated = append(result.Migrated, MigratePair{Src: src, Dst: dst, Backup: backup})
	return nil
}

// looksLikeDocgenOutput checks whether dir contains known archigraph docgen
// marker files (same heuristic as internal/cli.isDocgenOutput, duplicated here
// to avoid an import cycle).
func looksLikeDocgenOutput(dir string) bool {
	markers := []string{".plan.md", ".inventory.json", ".metadata.json"}
	for _, m := range markers {
		if _, err := os.Stat(filepath.Join(dir, m)); err == nil {
			return true
		}
	}
	// Also treat any directory that clearly looks like tier output as docgen.
	// Tier output directories start with ".tier" or ".llm-cache".
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() && (strings.HasPrefix(e.Name(), ".tier") || e.Name() == ".llm-cache") {
			return true
		}
	}
	return false
}
