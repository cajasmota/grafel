// Package gitmeta captures lightweight git HEAD metadata (ref name, commit
// SHA, worktree flag) for a given repository path at index time.
//
// The information is stored in the graph metadata so downstream tools
// (status, dashboard, MCP) can show which branch a graph was built from
// without re-running git. This is Phase 0 of epic #2087.
package gitmeta

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cajasmota/grafel/internal/executil"
	"github.com/cajasmota/grafel/internal/pathboundary"
)

// HasGitDirInTree walks dir upward looking for a .git file or directory,
// indicating an enclosing git repository. It returns true if .git is found
// anywhere from dir up to the climb boundary, false otherwise. This is a fast,
// subprocess-free check (no `git` invocation) that correctly recognises a
// module subdirectory of a single-.git monorepo as being inside a git repo.
//
// The climb is bounded by pathboundary.Climb: it stops at $HOME when dir is
// inside it, at the filesystem root otherwise, and at a depth backstop either
// way. Before #6548 the only stop was the root, so a dir with no .git anywhere
// above it Stat'd $HOME, /Users and / on every call.
func HasGitDirInTree(dir string) bool {
	return pathboundary.Climb(dir, func(cur string) bool {
		_, err := os.Stat(filepath.Join(cur, ".git"))
		return err == nil
	})
}

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

// gitStatus classifies WHY an external git invocation produced no usable
// output. Empty stdout on its own is ambiguous — it is what you get both when
// git ran and reported "this is not a git repository" and when git could not be
// run at all — and that ambiguity is the root of #6181: a transient fork
// failure was memoized as a durable fact about the repository.
type gitStatus int

const (
	// gitOK: git ran and exited 0. The output is the answer.
	gitOK gitStatus = iota
	// gitAnswered: git ran to completion and exited non-zero. This is still a
	// real answer ABOUT THE REPO — "not a git repository", "detached HEAD has no
	// symbolic ref", "no such ref" — and it is reproducible, so a caller may
	// safely memoize what it derived from it.
	gitAnswered
	// gitUnavailable: git could not be run to completion — the 2s deadline
	// fired, fork failed (EAGAIN under load), the binary is not on PATH, or a
	// pipe broke. This says nothing about the repository, only about the moment.
	// A caller MUST NOT memoize anything derived from it (#6181).
	gitUnavailable
)

// runGitFn is the seam through which every git invocation in this package is
// made. It exists so tests can inject a gitUnavailable outcome deterministically
// — you cannot reliably starve a machine into a fork failure, and the bug it
// guards (#6181) is precisely the load-correlated case. Unexported and
// test-only; production always uses runGitReal.
//
// NOT SYNCHRONISED. Both this and gitCallTimeout are plain globals that tests
// write mid-run, which is only safe because no test in this package calls
// t.Parallel(). If you add one, these need a mutex or a per-call injection
// point first — `go test -race` is green today by that accident, not by design.
var runGitFn = runGitReal

// gitCallTimeout is RunGit's deadline. A var, not a const, only so a test can
// shrink it and exercise the timeout branch in milliseconds rather than
// seconds. Production never changes it. See runGitFn on synchronisation.
var gitCallTimeout = 2 * time.Second

// runGitStatus runs git and reports both stdout and why it failed, if it did.
func runGitStatus(dir string, args ...string) (string, gitStatus) {
	return runGitFn(dir, args...)
}

func runGitReal(dir string, args ...string) (string, gitStatus) {
	ctx, cancel := context.WithTimeout(context.Background(), gitCallTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	executil.NoWindow(cmd)
	out, err := cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(out)), gitOK
	}
	// Deadline first: CommandContext kills the child on timeout. Strictly
	// redundant with the signal check below — a killed child is a signalled
	// child — but it is cheaper and it documents the intent at the point where
	// the timeout is set.
	if ctx.Err() != nil {
		return "", gitUnavailable
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		// *exec.ExitError collapses two different meanings, and the axis that
		// separates them is signalled-vs-exited — NOT deadline-vs-not, which is
		// only a subset of it. A git killed by the OOM killer, macOS jetsam, or a
		// SIGTERM propagated to the daemon's process group never got to answer,
		// and arrives here with ctx.Err() == nil. Classifying that as a durable
		// fact about the repository is #6181 verbatim, and it is MORE
		// load-correlated than the timeout: the OOM killer fires under exactly
		// the memory pressure that makes forks fail.
		//
		// ExitCode() returns -1 when the process was terminated by a signal, on
		// every platform — no syscall import, correct on Windows.
		if ee.ExitCode() < 0 {
			return "", gitUnavailable
		}
		// A real non-zero exit (128 "not a git repository", 1 "no such ref") is a
		// reproducible answer about the repo and stays cacheable.
		return "", gitAnswered
	}
	// Everything else — exec.ErrNotFound, EAGAIN on fork, ENOMEM, pipe errors —
	// means git never got to speak.
	return "", gitUnavailable
}

// RunGit runs git with the given args inside dir and returns stdout trimmed.
// Returns "" on any failure. Uses a 2-second timeout consistent with Capture.
// This is the shared low-level runner used by both Capture and callers in
// other packages that need ad-hoc git queries (e.g. --git-common-dir for
// worktree resolution in internal/mcp/routing.go, PH1c of #2087).
//
// It deliberately collapses gitAnswered and gitUnavailable into "": callers
// outside this package do not memoize on the result, so the distinction buys
// them nothing. Anything that CACHES a git-derived value must use runGitStatus
// and refuse to cache a gitUnavailable outcome (#6181).
func RunGit(dir string, args ...string) string {
	out, _ := runGitStatus(dir, args...)
	return out
}

// Capture runs a small set of git commands against repoPath and returns the
// HEAD metadata. All git calls use a 2-second timeout; any failure (non-git
// directory, git not on PATH, etc.) returns the zero-value Info with no error.
func Capture(repoPath string) Info {
	info, _ := captureStatus(repoPath)
	return info
}

// captureStatus is Capture plus a trust flag. trusted is false when ANY of the
// git invocations could not be run to completion, which means the returned Info
// describes the moment rather than the repository and must not be memoized
// (#6181). A repo that git ran against and reported on — including "not a git
// repository" and a detached HEAD's empty symbolic-ref — is trusted, so the
// negative result stays cacheable and the cache keeps its value.
func captureStatus(repoPath string) (info Info, trusted bool) {
	trusted = true
	run := func(args ...string) string {
		out, st := runGitStatus(repoPath, args...)
		if st == gitUnavailable {
			trusted = false
		}
		return out
	}

	// Sanity-check: is this a git repo at all?
	topLevel := run("rev-parse", "--show-toplevel")
	if topLevel == "" {
		return Info{}, trusted
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
	}, trusted
}
