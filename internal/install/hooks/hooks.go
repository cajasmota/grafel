// Package hooks installs and uninstalls archigraph git hooks.
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
)

const (
	// MarkerBegin and MarkerEnd delimit the archigraph-managed block in
	// any hook file we install into. The strings are deliberately
	// distinct so a stray `>>>` in user code won't confuse the matcher.
	MarkerBegin = "# >>> archigraph managed >>>"
	MarkerEnd   = "# <<< archigraph managed <<<"
)

// HookNames are the hooks archigraph installs into.
var HookNames = []string{"post-commit", "post-merge", "post-checkout"}

// Install writes archigraph hook blocks into <repo>/.git/hooks for every
// name in HookNames. binPath is the absolute path to the archigraph
// binary that the hook should invoke.
func Install(repo, binPath string) error {
	hooksDir, err := hooksDir(repo)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return err
	}
	for _, name := range HookNames {
		path := filepath.Join(hooksDir, name)
		if err := installOne(path, BlockFor(name, binPath, repo)); err != nil {
			return fmt.Errorf("hook %s: %w", name, err)
		}
	}
	return nil
}

// Uninstall removes the archigraph block from every hook in HookNames.
// Hooks that become empty (just a shebang) are left in place; we never
// delete files we did not create.
func Uninstall(repo string) error {
	hooksDir, err := hooksDir(repo)
	if err != nil {
		return err
	}
	for _, name := range HookNames {
		path := filepath.Join(hooksDir, name)
		body, err := os.ReadFile(path)
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
func BlockFor(hookName, binPath, repo string) string {
	return fmt.Sprintf(`%s
# archigraph %s — re-index the repo after %s.
%q index %q >/dev/null 2>&1 || true
%s
`, MarkerBegin, hookName, hookName, binPath, repo, MarkerEnd)
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
	b, err := os.ReadFile(gitPath)
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
	if b, err := os.ReadFile(path); err == nil {
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
