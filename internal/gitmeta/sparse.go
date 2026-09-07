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
//  2. Check git-config boolean `core.sparseCheckout` with git's own scope
//     resolution (worktree → local → global → system). Since git 2.32 the
//     value lives in the WORKTREE scope, which `--local` cannot see (#6964).
//     If false/absent → not sparse.
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
	"bytes"
	"path"
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

	// ConeMode is true when core.sparseCheckoutCone is enabled.
	//
	// It does NOT select a matching algorithm. Cone mode is a restriction on
	// what git WRITES — directory prefixes expressed as `/*`, `!/*/`, `/dir/`
	// — and those lines are ordinary gitignore-syntax patterns, so
	// IsPathIncluded answers both modes with one matcher (#6964). The flag is
	// reported because callers surface it, and because it is the only way to
	// tell a cone repo from a non-cone one after the fact.
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

	// Check the core.sparseCheckout config flag.
	//
	// SCOPE (#6964): this deliberately passes NO scope flag. `git config --get`
	// resolves worktree → local → global → system, which is exactly how git
	// itself resolves core.sparseCheckout, so the probe agrees with the working
	// tree git actually produced:
	//
	//   * git >= 2.32 — `git sparse-checkout init/set` turns on
	//     extensions.worktreeConfig and writes core.sparseCheckout /
	//     core.sparseCheckoutCone into the WORKTREE scope
	//     (<git-dir>/config.worktree). `--local` does NOT read that scope, so
	//     the previous `--local --get` returned empty and every sparse repo
	//     created by a modern git was probed as "not sparse" — measured on git
	//     2.50.1 (Apple Git-155), where `--local --get` exits 1 with no output
	//     while `--get` returns "true".
	//   * git < 2.32 — the same keys are written to the LOCAL scope
	//     (<git-dir>/config). `--get` reads local too, so this spelling is
	//     strictly wider than the one it replaces: nothing that used to be
	//     detected stops being detected.
	//   * a value set by hand in --local (or inherited from --global/--system)
	//     is honoured, because git honours it — a core.sparseCheckout=true in
	//     the global config really does give a sparse working tree, verified by
	//     driving `git read-tree -mu HEAD` under GIT_CONFIG_GLOBAL.
	//
	// `--worktree --get` was rejected: it is a hard error (exit 1) on any repo
	// that has not enabled extensions.worktreeConfig, i.e. on every non-sparse
	// repo and on every repo made sparse by git < 2.32.
	//
	// --bool normalises git's boolean spellings, so `core.sparseCheckout = 1`
	// (or "yes"/"on") is read as true the way git reads it, instead of falling
	// through the old literal `== "true"` comparison.
	enabled := RunGit(repoPath, "config", "--bool", "--get", "core.sparseCheckout")
	if enabled != "true" {
		return SparseInfo{}
	}

	// Detect cone mode. Same scope reasoning as above.
	cone := RunGit(repoPath, "config", "--bool", "--get", "core.sparseCheckoutCone")
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
	// readGitMetaFile, not os.Open: open(2) is what blocks on a FIFO, so
	// scanning rather than slurping made no difference to #6416. The pattern
	// file is name-chosen ("info/sparse-checkout" under the git dir) and is
	// read before any walk, so no entry-type gate sits in front of it.
	data, err := readGitMetaFile(path, maxSparsePatternBytes)
	if err != nil {
		return nil
	}

	var patterns []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns
}

// IsPathIncluded reports whether relPath (a repo-relative, forward-slash path
// to a FILE) is covered by si's sparse pattern set.
//
// When IsSparse is false every path is included (full checkout).
// When Patterns is empty every path is excluded (nothing checked out).
//
// # Semantics (#6964)
//
// git's sparse-checkout pattern file uses gitignore syntax with the sense
// inverted: a matching pattern INCLUDES, a matching negation (`!`) EXCLUDES,
// and the LAST pattern that matches wins. A pattern that matches a DIRECTORY
// carries its verdict down to everything beneath it, until some deeper path
// matches a pattern of its own.
//
// So the decision walks the path prefix by prefix — "a", "a/b", "a/b/c.go" —
// evaluating the whole pattern list against each prefix and letting the last
// match set the running verdict. A prefix that matches nothing inherits its
// parent's verdict; the file's verdict is whatever survives to the leaf. That
// is what makes cone mode work: git writes
//
//	/*
//	!/*/
//	/src/
//
// where `/*` includes every root entry, `!/*/` (directory-only) takes back
// every root DIRECTORY, and `/src/` puts src back. Before #6964 the code
// stripped the leading slash, saw `*`, returned "included" for the very first
// pattern and never reached the `/src/` restriction — every cone-mode sparse
// repo was indexed in full, silently. The old comment claimed cone mode does
// not use negation patterns; `!/*/` is the second line git itself writes.
//
// A directory that ends up excluded does NOT prune its subtree: git allows a
// later pattern to re-include below it (`/*`, `!/*/`, `/services/payments/`
// checks out services/payments while leaving services/orders absent). This
// matches measured git 2.50.1 behaviour, not gitignore's "cannot re-include"
// rule.
//
// ConeMode is informational only. Cone patterns are a subset of the general
// syntax, so one matcher answers both; the flag is still reported for the
// coverage badge and for callers that want to know how the repo was made.
//
// Cost: the pattern list is parsed on each call rather than cached, because
// SparseInfo is a plain value that callers construct and mutate directly. The
// list is a handful of short lines and parsing is a few string ops per line.
func IsPathIncluded(si SparseInfo, relPath string) bool {
	if !si.IsSparse {
		return true
	}
	if len(si.Patterns) == 0 {
		return false
	}

	// Normalise: forward slashes, no leading slash, no empty components.
	rel := strings.TrimPrefix(filepath.ToSlash(relPath), "/")
	comps := splitPathComponents(rel)
	if len(comps) == 0 {
		return false
	}

	pats := make([]sparsePattern, 0, len(si.Patterns))
	for _, raw := range si.Patterns {
		if p, ok := parseSparsePattern(raw); ok {
			pats = append(pats, p)
		}
	}
	if len(pats) == 0 {
		return false
	}

	included := false
	for i := 1; i <= len(comps); i++ {
		prefix := comps[:i]
		isDir := i < len(comps) // only the final component is the file itself
		for _, p := range pats {
			if p.matches(prefix, isDir) {
				included = !p.negate
			}
		}
	}
	return included
}

// sparsePattern is one parsed line of a sparse-checkout / gitignore pattern
// file.
type sparsePattern struct {
	// negate is true for a leading '!' — the line EXCLUDES what it matches.
	negate bool
	// dirOnly is true for a trailing '/' — the line matches directories only.
	dirOnly bool
	// anchored is true when the pattern is rooted at the repo top. Per
	// gitignore rules that is any pattern with a leading slash, or with a
	// slash anywhere other than at the end.
	anchored bool
	// segs are the slash-separated glob segments. A "**" segment matches zero
	// or more path components; every other segment is matched with path.Match,
	// whose '*' and '?' never cross a '/' and which honours '\' escapes and
	// '[...]' classes.
	segs []string
}

// parseSparsePattern parses one raw line. ok is false for a line that cannot
// select anything (empty, or "/" alone).
func parseSparsePattern(raw string) (sparsePattern, bool) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return sparsePattern{}, false
	}

	var p sparsePattern
	if strings.HasPrefix(line, "!") {
		p.negate = true
		line = line[1:]
	} else if strings.HasPrefix(line, `\!`) || strings.HasPrefix(line, `\#`) {
		// gitignore escape for a literal leading '!' or '#'.
		line = line[1:]
	}
	if strings.HasSuffix(line, "/") {
		p.dirOnly = true
	}

	trimmed := strings.Trim(line, "/")
	if trimmed == "" {
		// "/" or "!/" — selects the repo root itself, nothing beneath a name.
		return sparsePattern{}, false
	}

	// Anchored when rooted with a leading slash, or when a slash appears
	// anywhere before the (optional) trailing one.
	p.anchored = strings.HasPrefix(line, "/") || strings.Contains(trimmed, "/")
	p.segs = splitPathComponents(trimmed)
	if len(p.segs) == 0 {
		return sparsePattern{}, false
	}
	return p, true
}

// matches reports whether the pattern selects the path made of comps, where
// isDir says whether that path is a directory.
func (p sparsePattern) matches(comps []string, isDir bool) bool {
	if p.dirOnly && !isDir {
		return false
	}
	if p.anchored {
		return globSegments(p.segs, comps)
	}
	// Unanchored: the pattern may start at any component, but must still run
	// to the end of the path.
	for i := range comps {
		if globSegments(p.segs, comps[i:]) {
			return true
		}
	}
	return false
}

// globSegments matches pattern segments against path segments from the start,
// consuming both entirely. "**" matches zero or more path segments.
func globSegments(pat, comps []string) bool {
	if len(pat) == 0 {
		return len(comps) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(comps); i++ {
			if globSegments(pat[1:], comps[i:]) {
				return true
			}
		}
		return false
	}
	if len(comps) == 0 {
		return false
	}
	// path.Match, not filepath.Match: sparse patterns are always
	// forward-slash and '\' is an escape character on every platform.
	ok, err := path.Match(pat[0], comps[0])
	if err != nil || !ok {
		return false
	}
	return globSegments(pat[1:], comps[1:])
}

// splitPathComponents splits a forward-slash path, dropping empty components
// so "a//b/" yields ["a", "b"].
func splitPathComponents(s string) []string {
	parts := strings.Split(s, "/")
	out := parts[:0]
	for _, c := range parts {
		if c != "" {
			out = append(out, c)
		}
	}
	return out
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
