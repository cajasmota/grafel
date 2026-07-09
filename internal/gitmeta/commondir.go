package gitmeta

import "path/filepath"

// ResolveCommonDir resolves the git common-dir for repoPath to a canonical
// absolute path. All linked worktrees of a repo — and every subdirectory of a
// single git root (e.g. the module slugs of a monorepo) — share ONE common-dir,
// so it is the correct key for deduplicating "which repos share one git root".
//
// filepath.EvalSymlinks is applied so OS-level symlinks (e.g. macOS
// /tmp → /private/tmp) do not produce spurious distinct keys. Returns "" when
// repoPath is not a git repository (or git is unavailable); callers should fall
// back to a path-based key in that case rather than dropping the entry.
func ResolveCommonDir(repoPath string) string {
	raw := RunGit(repoPath, "rev-parse", "--git-common-dir")
	if raw == "" {
		return ""
	}
	// --git-common-dir may return a relative path (e.g. ".git") for the base
	// repo; resolve it relative to the repo root.
	var abs string
	if filepath.IsAbs(raw) {
		abs = raw
	} else {
		var err error
		abs, err = filepath.Abs(filepath.Join(repoPath, raw))
		if err != nil {
			return ""
		}
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return filepath.Clean(abs) // fallback: clean without symlink resolution
	}
	return real
}
