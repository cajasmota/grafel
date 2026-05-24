// Package gitmeta captures lightweight git HEAD metadata (ref name, commit
// SHA, worktree flag) for a given repository path at index time.
//
// The information is stored in the graph metadata so downstream tools
// (status, dashboard, MCP) can show which branch a graph was built from
// without re-running git. This is Phase 0 of epic #2087.
package gitmeta

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// Info holds the git HEAD metadata captured at index time.
type Info struct {
	// Ref is the branch/tag name ("main", "feat/X"). Empty for a detached HEAD.
	Ref string
	// SHA is the abbreviated (12-char) commit hash, or "" if not a git repo.
	SHA string
	// IsWorktree is true when repoPath is a linked worktree (not the main
	// checkout). Determined by comparing git-dir vs git-common-dir.
	IsWorktree bool
	// TopLevel is the output of git rev-parse --show-toplevel, or "" if not
	// a git repo.
	TopLevel string
}

// Capture runs a small set of git commands against repoPath and returns the
// HEAD metadata. All git calls use a 2-second timeout; any failure (non-git
// directory, git not on PATH, etc.) returns the zero-value Info with no error.
func Capture(repoPath string) Info {
	run := func(args ...string) string {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = repoPath
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}

	// Sanity-check: is this a git repo at all?
	topLevel := run("rev-parse", "--show-toplevel")
	if topLevel == "" {
		return Info{}
	}

	// Abbreviated SHA (12 chars matches GitHub's default).
	sha := run("rev-parse", "--short=12", "HEAD")

	// Symbolic ref — fails for detached HEAD; that's fine, Ref stays "".
	ref := run("symbolic-ref", "--short", "HEAD")

	// Worktree detection: linked worktree ↔ git-dir != git-common-dir.
	gitDir := run("rev-parse", "--git-dir")
	gitCommonDir := run("rev-parse", "--git-common-dir")
	isWorktree := gitDir != "" && gitCommonDir != "" && gitDir != gitCommonDir

	return Info{
		Ref:        ref,
		SHA:        sha,
		IsWorktree: isWorktree,
		TopLevel:   topLevel,
	}
}
