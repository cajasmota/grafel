// Package hooks installs and uninstalls grafel git hooks.
//
// All managed regions are wrapped in marker comments so that pre-existing
// hook scripts written by the user are preserved across install/upgrade
// /uninstall cycles. Idempotent: re-installing replaces the marker block;
// uninstall removes the block and leaves the file otherwise untouched.
package hooks

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cajasmota/grafel/internal/safeio"
)

const (
	// MarkerBegin and MarkerEnd delimit the grafel-managed block in
	// any hook file we install into. The strings are deliberately
	// distinct so a stray `>>>` in user code won't confuse the matcher.
	MarkerBegin = "# >>> grafel managed >>>"
	MarkerEnd   = "# <<< grafel managed <<<"
)

// HookNames are the hooks grafel installs into.
var HookNames = []string{"post-commit", "post-merge", "post-checkout"}

// Install writes grafel hook blocks into <repo>/.git/hooks for every
// name in HookNames. binPath is the absolute path to the grafel
// binary that the hook should invoke. When group is non-empty, the
// post-commit hook also refreshes the group's cross-repo links file by
// invoking `grafel links pass <group>` after re-indexing.
func Install(repo, binPath string, group ...string) error {
	hooksDir, err := hooksDir(repo)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return err
	}
	g := ""
	if len(group) > 0 {
		g = group[0]
	}
	for _, name := range HookNames {
		path := filepath.Join(hooksDir, name)
		if err := installOne(path, BlockFor(name, binPath, repo, g)); err != nil {
			return fmt.Errorf("hook %s: %w", name, err)
		}
	}
	return nil
}

// Uninstall removes the grafel block from every hook in HookNames.
// Hooks that become empty (just a shebang) are left in place; we never
// delete files we did not create.
func Uninstall(repo string) error {
	hooksDir, err := hooksDir(repo)
	if err != nil {
		return err
	}
	for _, name := range HookNames {
		path := filepath.Join(hooksDir, name)
		body, err := safeio.ReadFileReported(path, safeio.FollowSymlinks, safeio.MaxConfigFileBytes)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return err
		}
		stripped := stripBlock(string(body))
		if stripped == string(body) {
			continue
		}
		if err := os.WriteFile(path, []byte(stripped), 0o755); err != nil {
			return err
		}
	}
	return nil
}

// BlockFor returns the managed hook body for a single hook name.
//
// The reindex is enqueued ASYNCHRONOUSLY (`index --async`, #3366): the
// daemon coalesces it onto its debounced scheduler and ACKs immediately,
// so git writes (commit/checkout/merge) are never blocked waiting on a
// full reindex. Because the call returns instantly we do NOT background it
// with `&` — and `|| true` keeps the hook a no-op when the daemon is down.
//
// Rebase guard: when a rebase is in progress (`rebase-merge`/`rebase-apply`
// directories exist) the hook does nothing. A rebase replays N commits and
// would otherwise fire post-commit N times back-to-back; the final HEAD is
// reindexed by the post-checkout/post-merge that completes the rebase, or
// by the next ordinary commit.
//
// Cross-repo links: `grafel links pass <group>` is run ONLY by the
// post-merge hook (and only when group is non-empty). A merge is the point
// at which cross-repo wiring can change; post-commit/post-checkout skip it
// to keep ordinary writes cheap.
//
// The generated script resolves the repo path at runtime via
// `git rev-parse --show-toplevel` so that hooks shared through
// .git/hooks across worktrees index the worktree that fired the
// hook, not the hardcoded registered main-repo path.
func BlockFor(hookName, binPath, repo string, group ...string) string {
	g := ""
	if len(group) > 0 {
		g = group[0]
	}
	// Links pass only on post-merge, and only when we know the group.
	linksLine := ""
	if hookName == "post-merge" && g != "" {
		linksLine = fmt.Sprintf("  %q links pass %q >/dev/null 2>&1 || true\n", binPath, g)
	}
	return fmt.Sprintf(`%s
# grafel %s — enqueue a debounced async reindex of the repo (or worktree).
# Skipped mid-rebase so replaying N commits doesn't fire N reindexes (#3366).
_ag_repo="$(git rev-parse --show-toplevel 2>/dev/null)"
_ag_rbm="$(git rev-parse --git-path rebase-merge 2>/dev/null)"
_ag_rba="$(git rev-parse --git-path rebase-apply 2>/dev/null)"
if [ -n "$_ag_repo" ] && [ ! -d "$_ag_rbm" ] && [ ! -d "$_ag_rba" ]; then
  %q index --async "$_ag_repo" >/dev/null 2>&1 || true
%sfi
%s
`, MarkerBegin, hookName, binPath, linksLine, MarkerEnd)
}

func hooksDir(repo string) (string, error) {
	// Honor a `.git` file (worktrees) by reading `gitdir:`.
	gitPath := filepath.Join(repo, ".git")
	fi, err := os.Stat(gitPath)
	if err != nil {
		return "", err
	}
	if fi.IsDir() {
		return filepath.Join(gitPath, "hooks"), nil
	}
	b, err := safeio.ReadFileReported(gitPath, safeio.FollowSymlinks, safeio.MaxConfigFileBytes)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "gitdir:") {
			gd := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
			if !filepath.IsAbs(gd) {
				gd = filepath.Join(repo, gd)
			}
			return filepath.Join(gd, "hooks"), nil
		}
	}
	return "", fmt.Errorf("could not resolve git hooks directory for %s", repo)
}

func installOne(path, block string) error {
	existing := ""
	if b, err := safeio.ReadFileReported(path, safeio.FollowSymlinks, safeio.MaxConfigFileBytes); err == nil {
		existing = string(b)
	}
	stripped := stripBlock(existing)
	if stripped == "" {
		stripped = "#!/bin/sh\n"
	} else if !strings.HasSuffix(stripped, "\n") {
		stripped += "\n"
	}
	out := stripped + block
	return os.WriteFile(path, []byte(out), 0o755)
}

// stripBlock removes any marker-delimited block from body. If multiple
// blocks exist (a corrupted file from earlier versions) all are removed.
func stripBlock(body string) string {
	for {
		begin := strings.Index(body, MarkerBegin)
		if begin == -1 {
			return body
		}
		end := strings.Index(body[begin:], MarkerEnd)
		if end == -1 {
			// Unterminated block — strip from begin to EOF.
			return strings.TrimRight(body[:begin], "\n") + "\n"
		}
		end = begin + end + len(MarkerEnd)
		// Consume trailing newline if present.
		if end < len(body) && body[end] == '\n' {
			end++
		}
		body = body[:begin] + body[end:]
	}
}
