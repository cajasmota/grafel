// Package gitmeta — sparse.go detects whether a git repository has
// sparse-checkout enabled and, if so, which path patterns are active.
//
// Sparse-checkout (git sparse-checkout / git sparse-checkout set) limits
// the working tree to a subset of paths defined in:
//
//	<git-dir>/info/sparse-checkout          (cone mode OFF / legacy patterns)
//	<git-dir>/info/sparse-checkout          (cone mode ON — directory prefixes)
//
// The cone-mode flag lives at core.sparseCheckoutCone (bool in git-config).
//
// Detection strategy (zero git-process overhead on non-sparse repos):
//  1. Read the git-dir via `git rev-parse --git-dir` (already available via
//     RunGit — 2-second timeout, returns "" on failure).
//  2. Check git-config boolean `core.sparseCheckout`. If false/absent → not sparse.
//  3. Parse <git-dir>/info/sparse-checkout for the active pattern list.
//
// SparseInfo is intentionally lightweight — it is computed once per index
// run and passed into the walk layer as part of Options. The walker uses
// IsSparsePresent to decide whether to check each file path against the
// pattern set.
//
// Issue #2181 / epic #2175 — M4 sparse-checkout awareness.
package gitmeta

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// CoverageStatus describes the completeness of an indexed repository.
// The three-way enum is carried in graph.Document.CoverageStatus and surfaced
// as a badge in the dashboard.
//
// Values:
//
//	"full"    — normal full working tree (default / no badge shown).
//	"partial" — git sparse-checkout is active; only a subset of paths are present.
//	"sparse"  — alias for "partial" (reserved for future use; currently unused).
const (
	CoverageStatusFull    = "full"
	CoverageStatusPartial = "partial"
)

// SparseInfo carries the result of probing a repo for sparse-checkout state.
// The zero value (IsSparse=false, Patterns=nil) represents a normal full checkout.
type SparseInfo struct {
	// IsSparse is true when git sparse-checkout is enabled for this repo.
	IsSparse bool

	// Patterns is the list of raw lines from <git-dir>/info/sparse-checkout.
	// Blank lines and lines starting with '#' are excluded. May be nil when
	// IsSparse is false or the pattern file is unreadable.
	Patterns []string

	// ConeMode is true when core.sparseCheckoutCone is enabled. In cone mode
	// every pattern is treated as a directory prefix (recursive); in non-cone
	// mode standard gitignore-style globs apply.
	ConeMode bool
}

// ProbeRepo detects whether repoPath has git sparse-checkout enabled.
// Returns a zero SparseInfo (IsSparse=false) for non-git directories,
// git errors, or when sparse-checkout is disabled.
//
// All git operations use a 2-second timeout via RunGit.
func ProbeRepo(repoPath string) SparseInfo {
	// Resolve the git directory (handles worktrees correctly).
	gitDir := RunGit(repoPath, "rev-parse", "--git-dir")
	if gitDir == "" {
		return SparseInfo{}
	}
	// git rev-parse --git-dir returns a relative path when inside the repo.
	// Make it absolute relative to repoPath.
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(repoPath, gitDir)
	}

	// Check core.sparseCheckout config flag.
	enabled := RunGit(repoPath, "config", "--local", "--get", "core.sparseCheckout")
	if enabled != "true" {
		return SparseInfo{}
	}

	// Detect cone mode.
	cone := RunGit(repoPath, "config", "--local", "--get", "core.sparseCheckoutCone")
	coneMode := cone == "true"

	// Parse the sparse-checkout pattern file.
	patterns := parseSparsePatternFile(filepath.Join(gitDir, "info", "sparse-checkout"))

	return SparseInfo{
		IsSparse: true,
		Patterns: patterns,
		ConeMode: coneMode,
	}
}

// parseSparsePatternFile reads the sparse-checkout pattern file and returns
// non-empty, non-comment lines. Returns nil when the file is absent or
// unreadable (a missing file is not an error — it just means no patterns).
func parseSparsePatternFile(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var patterns []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns
}

// IsPathIncluded reports whether relPath (repo-relative forward-slash path)
// is covered by si's sparse pattern set.
//
// When IsSparse is false every path is included (full checkout).
// When Patterns is empty every path is excluded (nothing checked out).
//
// Cone mode: each non-negation pattern is a directory prefix; a file is
// included when it starts with "<pattern>/" or equals the pattern exactly,
// OR when the pattern is "/" (repo root match-all).
//
// Non-cone mode: standard gitignore-style prefix / glob semantics. For
// simplicity we use a prefix match: a file is included when one of its
// ancestor directories or the file itself matches a pattern.
func IsPathIncluded(si SparseInfo, relPath string) bool {
	if !si.IsSparse {
		return true
	}
	if len(si.Patterns) == 0 {
		return false
	}

	// Normalise: no leading slash, forward slashes only.
	rel := filepath.ToSlash(relPath)
	rel = strings.TrimPrefix(rel, "/")

	for _, pat := range si.Patterns {
		// Negation patterns (!) are not implemented here — cone mode doesn't
		// use them; non-cone repos with negation patterns are rare. Treat
		// negation lines as excluders (file is excluded when a negation matches).
		negation := strings.HasPrefix(pat, "!")
		p := strings.TrimPrefix(pat, "!")
		p = strings.TrimPrefix(p, "/")
		p = strings.TrimSuffix(p, "/")

		var match bool
		if si.ConeMode {
			// In cone mode, pat is a directory prefix.
			if p == "" || p == "*" {
				match = true // wildcard / root
			} else {
				match = rel == p ||
					strings.HasPrefix(rel, p+"/")
			}
		} else {
			// Non-cone: treat as a path prefix.
			// A pattern "services/payments" covers "services/payments/file.go".
			if p == "" || p == "*" || p == "**" {
				match = true
			} else {
				match = rel == p ||
					strings.HasPrefix(rel, p+"/")
			}
		}

		if match {
			if negation {
				return false
			}
			return true
		}
	}
	return false
}

// CoverageStatus returns CoverageStatusPartial when the SparseInfo indicates
// a sparse checkout, otherwise CoverageStatusFull. This is the string stored
// in graph.Document.CoverageStatus.
func (si SparseInfo) CoverageStatus() string {
	if si.IsSparse {
		return CoverageStatusPartial
	}
	return CoverageStatusFull
}
