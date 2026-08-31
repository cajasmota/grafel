// classify.go — linked-worktree detection (issue #3680).
//
// # Why
//
// grafel dogfoods itself: the `grafel` repo is an indexed group, so
// the daemon's fsnotify watcher catches every `git worktree add` and would
// cold-index the new worktree as a SEPARATE repo, keyed under
// hash(worktreePath) with its OWN ~100MB full graph store. The rewrite agent
// creates many agent-worktrees, so this proliferates stores and blows the RSS
// admission budget.
//
// A linked git worktree has a `.git` FILE (not a directory) whose single line
// is:
//
//	gitdir: <primary>/.git/worktrees/<name>
//
// From that pointer we can recover the PRIMARY repo's checkout path. When the
// primary is an already-indexed root repo, the worktree should NOT be onboarded
// as a brand-new root repo with a cold full clone — it shares the primary's
// graph (the daemon already tracks the worktree as an ephemeral WorktreeChild
// and overlays only its diff). A real standalone repo (`.git` is a DIRECTORY)
// is still classified as a root and indexed normally.
package worktree

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/cajasmota/grafel/internal/safeio"
)

// RootKind classifies a candidate repo root for onboarding decisions.
type RootKind int8

const (
	// RootKindStandalone is a normal repository: `.git` is a directory.
	// Onboard and index it as its own root repo.
	RootKindStandalone RootKind = iota
	// RootKindLinkedWorktree is a git linked worktree: `.git` is a file
	// containing a `gitdir:` pointer into a primary repo's
	// .git/worktrees/<name>. Do NOT cold-index it as a new root repo; it
	// shares the primary's graph (tracked as an ephemeral WorktreeChild).
	RootKindLinkedWorktree
	// RootKindUnknown is returned when the path has no `.git` entry, is not a
	// directory, or the `.git` file cannot be parsed. Callers should treat it
	// conservatively (do not onboard as a new root).
	RootKindUnknown
)

// String returns the lowercase JSON-safe kind name.
func (k RootKind) String() string {
	switch k {
	case RootKindStandalone:
		return "standalone"
	case RootKindLinkedWorktree:
		return "linked_worktree"
	default:
		return "unknown"
	}
}

// RootClassification is the result of ClassifyRoot.
type RootClassification struct {
	// Kind is the structural classification of the candidate path.
	Kind RootKind
	// PrimaryRepoPath is the absolute checkout path of the primary repo that
	// owns this worktree. Set only when Kind == RootKindLinkedWorktree and the
	// primary checkout could be resolved; "" otherwise.
	PrimaryRepoPath string
	// GitDir is the resolved absolute gitdir the `.git` file points at
	// (<primary>/.git/worktrees/<name>). Set only for linked worktrees.
	GitDir string
}

// ClassifyRoot inspects candidatePath's `.git` entry and classifies it.
//
//   - `.git` is a directory                       → RootKindStandalone.
//   - `.git` is a file with a valid gitdir pointer
//     of the shape  <common>/worktrees/<name>     → RootKindLinkedWorktree,
//     with PrimaryRepoPath resolved to the primary checkout (the parent of
//     the common `.git` dir).
//   - anything else (missing `.git`, unparsable
//     pointer, malformed shape)                   → RootKindUnknown.
//
// ClassifyRoot performs only cheap filesystem reads (one Stat + at most one
// small ReadFile); it never shells out to git, so it is safe to call on the
// onboarding hot path.
func ClassifyRoot(candidatePath string) RootClassification {
	gitPath := filepath.Join(candidatePath, ".git")
	fi, err := os.Lstat(gitPath)
	if err != nil {
		return RootClassification{Kind: RootKindUnknown}
	}
	if fi.IsDir() {
		// Normal repository — `.git` is a directory.
		return RootClassification{Kind: RootKindStandalone}
	}

	gitdir := readGitdirPointer(gitPath)
	if gitdir == "" {
		return RootClassification{Kind: RootKindUnknown}
	}

	// A linked worktree's gitdir is <common>/worktrees/<name>. Verify the
	// shape so we don't misclassify a submodule (gitdir: <common>/modules/<name>)
	// or any other non-worktree pointer as a worktree.
	parent := filepath.Dir(gitdir) // <common>/worktrees
	if filepath.Base(parent) != "worktrees" {
		return RootClassification{Kind: RootKindUnknown}
	}
	commonGitDir := filepath.Dir(parent) // <common>  (== <primary>/.git)
	primary := primaryFromCommonGitDir(commonGitDir)

	return RootClassification{
		Kind:            RootKindLinkedWorktree,
		PrimaryRepoPath: primary,
		GitDir:          gitdir,
	}
}

// readGitdirPointer reads a `.git` FILE and returns the absolute path the
// `gitdir:` line points at, or "" when the file is unreadable / malformed.
// Relative pointers (git can write `gitdir: ../.git/worktrees/x`) are resolved
// against the directory containing the `.git` file.
func readGitdirPointer(gitFilePath string) string {
	data, err := safeio.ReadFileReported(gitFilePath, safeio.FollowSymlinks, safeio.MaxConfigFileBytes)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	rest, ok := strings.CutPrefix(line, "gitdir:")
	if !ok {
		return ""
	}
	gitdir := strings.TrimSpace(rest)
	if gitdir == "" {
		return ""
	}
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(filepath.Dir(gitFilePath), gitdir)
	}
	return filepath.Clean(gitdir)
}

// primaryFromCommonGitDir derives the primary checkout path from the common
// git directory (<primary>/.git). The common case is `<primary>/.git`, so the
// primary is its parent. When the common dir is a bare repo (no trailing
// `.git` component) we return it as-is — callers key the primary by path and a
// bare repo is still a usable parent key.
func primaryFromCommonGitDir(commonGitDir string) string {
	if commonGitDir == "" || commonGitDir == "." {
		return ""
	}
	if filepath.Base(commonGitDir) == ".git" {
		return filepath.Dir(commonGitDir)
	}
	return commonGitDir
}

// IsLinkedWorktreeOf reports whether candidatePath is a linked worktree whose
// primary checkout is one of the already-indexed primary repo paths in
// indexedPrimaries. This is the onboarding-gate predicate: when it returns
// true, the daemon must NOT onboard candidatePath as a new root repo (which
// would trigger a cold full-store clone) — the worktree shares the primary's
// graph and is tracked as an ephemeral WorktreeChild instead.
//
// Paths are compared after canonicalisation (normPath + symlink resolution) so
// separator/case quirks on git's porcelain output and OS symlink aliases
// (e.g. macOS /var → /private/var) do not cause false negatives.
func IsLinkedWorktreeOf(candidatePath string, indexedPrimaries []string) bool {
	c := ClassifyRoot(candidatePath)
	if c.Kind != RootKindLinkedWorktree || c.PrimaryRepoPath == "" {
		return false
	}
	want := canonPath(c.PrimaryRepoPath)
	for _, p := range indexedPrimaries {
		if canonPath(p) == want {
			return true
		}
	}
	return false
}

// canonPath canonicalises p for cross-source path comparison: it normalises
// separators/case via normPath and then resolves symlinks when possible.
// EvalSymlinks failures (e.g. the path no longer exists) fall back to the
// normalised path so the comparison still works on a best-effort basis.
func canonPath(p string) string {
	n := normPath(p)
	if resolved, err := filepath.EvalSymlinks(n); err == nil {
		return resolved
	}
	return n
}
