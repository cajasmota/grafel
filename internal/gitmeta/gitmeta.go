// Package gitmeta captures lightweight git HEAD metadata (ref name, commit
// SHA, worktree flag) for a given repository path at index time.
//
// The information is stored in the graph metadata so downstream tools
// (status, dashboard, MCP) can show which branch a graph was built from
// without re-running git. This is Phase 0 of epic #2087.
package gitmeta

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/cajasmota/grafel/internal/executil"
)

// EnvGitTimeout overrides the default external-git deadline (in seconds) used
// by the bounded runners below. A value ≤ 0 disables the cap (not recommended).
// Default when unset: DefaultGitTimeout.
const EnvGitTimeout = "GRAFEL_GIT_TIMEOUT_SECONDS"

// DefaultGitTimeout bounds any external git invocation made on a serve- or
// index-critical path. Issue #5286: a stuck `git` child (uninterruptible disk
// I/O during heavy churn) previously wedged the indexer / HEAD poller with no
// deadline. CommandContext lets us kill the child on timeout and fail-closed
// skip the repo while the daemon keeps serving. 45s is generous for a slow but
// healthy `git log`/`git diff` on a large repo, yet bounds a true hang.
const DefaultGitTimeout = 45 * time.Second

// GitTimeout returns the configured external-git deadline (DefaultGitTimeout
// unless GRAFEL_GIT_TIMEOUT_SECONDS overrides it). A non-positive override is
// clamped to DefaultGitTimeout so a typo can never re-introduce an unbounded
// call on the serve/index path.
func GitTimeout() time.Duration {
	if v := strings.TrimSpace(os.Getenv(EnvGitTimeout)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return DefaultGitTimeout
}

// RunGitBounded runs git with the given args inside dir under the configured
// GitTimeout and returns (stdout-trimmed, true) on success. On timeout or any
// error it returns ("", false) — the child is killed by CommandContext when the
// deadline fires. Unlike RunGit (2s, swallows errors) this exposes the ok flag
// so index/poller callers can fail-closed skip a repo whose git wedged, instead
// of silently treating a hang as "no output". Issue #5286.
func RunGitBounded(dir string, args ...string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), GitTimeout())
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	applyWaitDelay(cmd)
	executil.NoWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

// waitDelayGrace is how long Wait()/Output() will wait, AFTER the context
// deadline fires and the child is signalled, before the os/exec runtime
// force-closes the child's I/O pipes and returns. This is the load-bearing part
// of the #5286 fix: a stuck git can spawn a grandchild (or itself wedge in a
// U-state) that keeps the stdout pipe open, so CommandContext's SIGKILL of the
// direct child does NOT unblock Output() — Wait blocks on the inherited pipe
// indefinitely. WaitDelay caps that wait so the caller ALWAYS returns and can
// fail-closed skip the repo, even when the OS cannot reap the wedged process.
const waitDelayGrace = 3 * time.Second

// applyWaitDelay wires cmd.WaitDelay (Go 1.20+) so a wedged child whose pipes
// stay open after the deadline cannot block Wait()/Output() forever.
func applyWaitDelay(cmd *exec.Cmd) {
	cmd.WaitDelay = waitDelayGrace
}

// RunGitBoundedC is like RunGitBounded but takes the explicit subcommand form
// `git -C <dir> <args...>` (matching the indexer's existing call style) and
// returns the raw, untrimmed stdout so callers that scan line-by-line keep
// trailing structure. ok is false on timeout/error.
func RunGitBoundedC(dir string, args ...string) ([]byte, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), GitTimeout())
	defer cancel()
	full := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	applyWaitDelay(cmd)
	executil.NoWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil, false
	}
	return out, true
}

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

// IsDefaultBranch reports whether the current HEAD ref of the repository at
// repoPath is the repo's default (main) branch.
//
// Strategy:
//  1. Read HEAD symbolic ref (e.g. "main", "master", "trunk").
//  2. Compare against the remote's default branch via
//     `git symbolic-ref refs/remotes/origin/HEAD --short`.
//     This yields "origin/main" → strip the remote prefix.
//  3. Fallback heuristic: if the remote default is unavailable, treat "main",
//     "master", and "trunk" as default branch names.
//
// Returns false for detached HEAD, non-git directories, or any git error.
func IsDefaultBranch(repoPath string) bool {
	ref := RunGit(repoPath, "symbolic-ref", "--short", "HEAD")
	if ref == "" {
		return false // detached HEAD or not a git repo
	}

	// Attempt to read origin's HEAD to determine the registered default branch.
	originHead := RunGit(repoPath, "symbolic-ref", "refs/remotes/origin/HEAD", "--short")
	if originHead != "" {
		// originHead is "origin/main" — strip the remote prefix.
		parts := strings.SplitN(originHead, "/", 2)
		if len(parts) == 2 {
			return ref == parts[1]
		}
	}

	// Fallback: canonical default branch names.
	switch ref {
	case "main", "master", "trunk":
		return true
	}
	return false
}

// RunGit runs git with the given args inside dir and returns stdout trimmed.
// Returns "" on any failure. Uses a 2-second timeout consistent with Capture.
// This is the shared low-level runner used by both Capture and callers in
// other packages that need ad-hoc git queries (e.g. --git-common-dir for
// worktree resolution in internal/mcp/routing.go, PH1c of #2087).
func RunGit(dir string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	executil.NoWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Capture runs a small set of git commands against repoPath and returns the
// HEAD metadata. All git calls use a 2-second timeout; any failure (non-git
// directory, git not on PATH, etc.) returns the zero-value Info with no error.
func Capture(repoPath string) Info {
	run := func(args ...string) string {
		return RunGit(repoPath, args...)
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
