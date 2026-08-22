// classify.go — shared path classification for the index wizard (#5336).
//
// ClassifyPath is the SINGLE SOURCE OF TRUTH used by both the CLI wizard
// (internal/cli/wizard.go) and the dashboard scan/inspect handler
// (internal/dashboard/v2_wizard.go) so the two surfaces agree on what a folder
// IS and what action makes sense for it.
//
// The motivating bug: the old CLI wizard always scanned filepath.Dir(cwd) — the
// PARENT of the cwd — for sibling git repos. For a "container" folder like
//
//	ivivo/
//	  backend/   (.git)
//	  frontend/  (.git)
//
// running the wizard from inside `ivivo` listed ivivo's UNRELATED siblings and
// missed backend+frontend entirely. ClassifyPath instead understands the folder
// you point it at: it reports whether the folder itself is a git repo, its
// immediate CHILD git repos (the ivivo case), its monorepo packages, and — when
// the folder is itself a repo — its SIBLING git repos. A suggested action is
// derived from that so the wizard can pre-place its cursor sensibly.

package detect

import (
	"os"
	"path/filepath"
	"sort"
)

// SuggestedAction is the wizard action ClassifyPath recommends for a path.
type SuggestedAction string

const (
	// ActionSingle — the path is a single git repo with no child repos and is
	// not a monorepo. Index it as one unit.
	ActionSingle SuggestedAction = "single"
	// ActionGroup — the path is a container of multiple git repos (the ivivo
	// case: child git repos), OR a git repo that has sibling git repos. Index a
	// group of related repos.
	ActionGroup SuggestedAction = "group"
	// ActionMonorepo — the path is a monorepo (workspace manifest or container
	// layout) with multiple package roots. Index selected packages.
	ActionMonorepo SuggestedAction = "monorepo"
	// ActionNone — the path is empty / not a repo / nothing indexable found.
	ActionNone SuggestedAction = ""
)

// Classification is the result of ClassifyPath. All repo/package lists are
// names/paths RELATIVE to the appropriate anchor, documented per field, so both
// the CLI and the dashboard can render and resolve them consistently.
type Classification struct {
	// AbsPath is the resolved absolute path that was classified.
	AbsPath string

	// IsGitRepo reports whether AbsPath itself contains a .git entry.
	IsGitRepo bool

	// ChildGitRepos are the names of AbsPath's immediate child directories that
	// are themselves git repos (relative to AbsPath). This is the ivivo case:
	// classifying ivivo/ yields ["backend","frontend"]. Sorted, nil when none.
	ChildGitRepos []string

	// SiblingGitRepos are the ABSOLUTE paths of the OTHER git repos that live
	// alongside AbsPath in its parent directory (excluding AbsPath itself).
	// Populated only when AbsPath is itself a git repo — it answers "this repo
	// plus its siblings" for the group action. Sorted, nil when none.
	SiblingGitRepos []string

	// Monorepo is the detected monorepo layout (KindNone when not a monorepo).
	Monorepo MonorepoKind

	// Packages are the repo-relative package roots when AbsPath is a monorepo.
	// Sorted, nil when not a monorepo.
	Packages []string

	// Stack is the dominant detected stack of AbsPath ("go","node",…).
	Stack string

	// Suggested is the recommended wizard action derived from the above.
	Suggested SuggestedAction
}

// ClassifyPath inspects absPath (which need not be absolute; it is resolved)
// and returns a Classification describing how it should be indexed. It performs
// NO writes. The precedence for the suggested action is:
//
//  1. Container of child git repos (ivivo: backend+frontend) → group.
//  2. Monorepo (workspace manifest or services/apps/… layout) → monorepo.
//  3. Git repo WITH siblings → group; a lone git repo → single.
//  4. Nothing indexable → none.
//
// Note that ChildGitRepos and Packages are mutually exclusive in the suggested
// action: a folder full of independent git repos is treated as a group, not a
// monorepo, even if a child happens to carry a workspace manifest. Both lists
// are still populated for callers that want to present alternatives.
func ClassifyPath(path string) (Classification, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Classification{}, err
	}
	c := Classification{AbsPath: abs}

	c.IsGitRepo = dirHasGit(abs)
	c.ChildGitRepos = childGitRepoNames(abs)

	mono, err := DetectMonorepo(abs)
	if err == nil {
		c.Monorepo = mono.Kind
		c.Packages = mono.Packages
	}

	if c.IsGitRepo {
		c.SiblingGitRepos = siblingGitRepos(abs)
		c.Stack = Stack(abs)
	}

	c.Suggested = suggestAction(c)
	return c, nil
}

// suggestAction derives the recommended action from an otherwise-populated
// Classification (see ClassifyPath's precedence doc).
func suggestAction(c Classification) SuggestedAction {
	switch {
	case len(c.ChildGitRepos) > 0:
		return ActionGroup
	case c.IsGitRepo:
		// A repo that is also a monorepo: prefer the monorepo action so the user
		// gets per-package selection (the more specific intent).
		if c.Monorepo != KindNone && len(c.Packages) > 0 {
			return ActionMonorepo
		}
		if len(c.SiblingGitRepos) > 0 {
			return ActionGroup
		}
		return ActionSingle
	case c.Monorepo != KindNone && len(c.Packages) > 0:
		return ActionMonorepo
	default:
		return ActionNone
	}
}

// dirHasGit reports whether dir exists, is a directory, and contains a .git
// entry (file or dir — covering worktrees and submodules).
func dirHasGit(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// childGitRepoNames returns the names of dir's immediate child directories that
// are git repos, sorted. Returns nil when none (so callers can test with len).
func childGitRepoNames(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Never Stat INTO a macOS TCC-protected child of $HOME (Documents/
		// Downloads/Pictures/Music/…): probing each child's .git here fired a
		// batch of permission prompts when the classified dir was $HOME itself
		// (v0.1.8 bug). No-op off darwin.
		if isProtectedHomeChild(dir, e.Name()) {
			continue
		}
		if dirHasGit(filepath.Join(dir, e.Name())) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// siblingGitRepos returns the absolute paths of the OTHER git repos in repo's
// parent directory (excluding repo itself), sorted. Returns nil when none.
func siblingGitRepos(repo string) []string {
	parent := filepath.Dir(repo)
	// Skip when the parent is the home dir or a macOS TCC-protected folder:
	// enumerating it and probing each child's .git reads INTO protected folders
	// and fires a permission prompt during normal wizard use (v0.1.8 bug). A
	// repo cloned straight into ~ has no meaningful "siblings" to offer anyway.
	if isProtectedScanParent(parent) {
		return nil
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil
	}
	self := filepath.Base(repo)
	var out []string
	for _, e := range entries {
		if !e.IsDir() || e.Name() == self {
			continue
		}
		full := filepath.Join(parent, e.Name())
		if dirHasGit(full) {
			out = append(out, full)
		}
	}
	sort.Strings(out)
	return out
}

// IsProtectedScanParent is the exported form of the guard above, for callers
// outside this package that perform the same "ReadDir a parent and Stat every
// child's .git" scan on an INFERRED parent directory — currently the wizard's
// discoverCandidates fallback to filepath.Dir(cwd) (#6548). It reports whether
// enumerating parent would read $HOME itself or a macOS TCC-protected folder.
//
// There is deliberately ONE denylist behind this (protected_darwin.go); callers
// must not grow a second copy. Off darwin it is false — there is no TCC.
func IsProtectedScanParent(parent string) bool { return isProtectedScanParent(parent) }
