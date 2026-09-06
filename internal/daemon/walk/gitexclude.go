// Package walk — gitexclude.go
//
// #6922: git reads THREE ignore sources, not one.
//
//  1. .gitignore files in the tree      (deepest first — highest precedence)
//  2. .git/info/exclude                 (per-clone, uncommitted)
//  3. core.excludesFile                 (per-user global — lowest precedence)
//
// The walker read only the first, so a file excluded via .git/info/exclude was
// indexed by grafel and reported by `git status` at no -u level. This file
// resolves sources 2 and 3 and hands them to the walker as ordinary IgnoreFile
// values, so they go through the same parser, the same precedence stack and the
// same SkipEntry reporting as .gitignore.
package walk

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/cajasmota/grafel/internal/gitmeta"
)

// gitExcludeSourceInfo is the label used for .git/info/exclude in SkipEntry
// rules; gitExcludeSourceGlobal is the label for core.excludesFile.
const (
	gitExcludeSourceInfo   = "info/exclude"
	gitExcludeSourceGlobal = "core.excludesFile"
)

// gitExcludeIgnoreFiles returns the non-.gitignore ignore sources for the repo
// containing root, ordered LOWEST precedence first — core.excludesFile, then
// .git/info/exclude — so a caller can Push them onto an IgnoreStack before the
// tree's own .gitignore files, which must outrank both.
//
// Entries that do not exist, are empty, or contain no patterns are omitted, so
// a non-git directory yields nil and costs two bounded git calls.
//
// Patterns in both files are anchored at the git TOP-LEVEL. When root is a
// subdirectory of the top-level (a monorepo registered by package root) the
// patterns are rewritten relative to root by the same helper that handles
// inherited .grafelignore files, so an anchored `/build` in info/exclude does
// not silently become "anything named build" under the child root.
// gitExcludeIgnoreFiles returns the non-.gitignore ignore sources for the repo
// at root, ordered LOWEST precedence first — core.excludesFile, then
// .git/info/exclude — so a caller can Push them onto an IgnoreStack before the
// tree's own .gitignore files, which must outrank both.
//
// Entries that do not exist, are empty, or contain no patterns are omitted, so
// a non-git directory yields nil and costs one bounded git call.
//
// SCOPE, deliberately: patterns in both files are anchored at the git
// TOP-LEVEL, so they are applied ONLY when root IS the top-level. A walk rooted
// at a subdirectory (a monorepo registered by package root) gets neither — the
// same treatment root .gitignore already gets, which is read from root and not
// inherited from the top-level either. The one inheriting layer, .grafelignore,
// rewrites its patterns through rewriteInheritedIgnoreLine, and that rewrite
// DROPS anchoring: `/sub/x.go` under relRoot `sub` becomes the un-anchored
// `x.go`, which then matches at any depth. Reusing it here would trade an
// under-exclusion for an over-exclusion, so it is not reused. Applying the
// patterns unrewritten would mis-anchor them just as badly.
func gitExcludeIgnoreFiles(root string) []*IgnoreFile {
	// RunGit, not Capture: only the top-level is needed here and Capture costs
	// five git invocations to answer four questions this function never asks.
	topLevel := gitmeta.RunGit(root, "rev-parse", "--show-toplevel")
	if topLevel == "" {
		return nil // not a git repo (or git unavailable) — nothing to read
	}
	if !isSamePathResolved(topLevel, root) {
		return nil // see SCOPE above
	}

	var out []*IgnoreFile
	if ig := parseExcludeFile(globalExcludesPath(root), gitExcludeSourceGlobal); ig != nil {
		out = append(out, ig)
	}
	if ig := parseExcludeFile(infoExcludePath(root), gitExcludeSourceInfo); ig != nil {
		out = append(out, ig)
	}
	return out
}

// isSamePathResolved reports whether a and b name the same directory once both
// are made absolute and symlink-resolved (macOS /var → /private/var).
func isSamePathResolved(a, b string) bool {
	resolve := func(p string) string {
		abs, err := filepath.Abs(p)
		if err != nil {
			return ""
		}
		if r, err := filepath.EvalSymlinks(abs); err == nil {
			abs = r
		}
		return filepath.Clean(abs)
	}
	ra, rb := resolve(a), resolve(b)
	if ra == "" || rb == "" {
		return false
	}
	return samePath(ra, rb)
}

// parseExcludeFile reads path as gitignore syntax and returns an IgnoreFile
// anchored at the repo root, or nil when the file is absent, unreadable or pattern-free.
//
// readIgnoreFile (not os.ReadFile) because this is a NAME-CHOSEN read that runs
// before the walk, so the entry-type gate has nothing to say about it — the
// same hazard #6416 fixed for .grafelignore applies verbatim to a
// `mkfifo .git/info/exclude`.
func parseExcludeFile(path, source string) *IgnoreFile {
	if path == "" {
		return nil
	}
	b, err := readIgnoreFile(path)
	if err != nil || len(b) == 0 {
		return nil
	}
	ig, err := parseIgnoreReader("", source, strings.NewReader(string(b)))
	if err != nil || ig == nil || len(ig.patterns) == 0 {
		return nil
	}
	return ig
}

// infoExcludePath returns the absolute path of <git-common-dir>/info/exclude,
// or "" when the git dir cannot be resolved.
//
// git-common-dir, not git-dir: a linked worktree has its own git dir but shares
// the main clone's info/exclude, and git honours the shared one.
func infoExcludePath(root string) string {
	common := gitmeta.RunGit(root, "rev-parse", "--git-common-dir")
	if common == "" {
		return ""
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(root, common)
	}
	return filepath.Join(common, "info", "exclude")
}

// globalExcludesPath resolves core.excludesFile for the repo at root, falling
// back to git's own default of $XDG_CONFIG_HOME/git/ignore (then
// ~/.config/git/ignore) when the config key is unset.
//
// It is a CONFIG read, not a fixed path: a developer who points
// core.excludesFile somewhere else gets that file, which is the whole reason
// #6922 asks for the config lookup rather than a hardcoded ~/.config path.
func globalExcludesPath(root string) string {
	if p := gitmeta.RunGit(root, "config", "--get", "core.excludesFile"); p != "" {
		return expandTilde(p)
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "git", "ignore")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "git", "ignore")
}

// expandTilde expands a leading "~/" using the current user's home directory,
// matching git's own expansion of core.excludesFile.
func expandTilde(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return p
		}
		return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
	}
	return p
}
