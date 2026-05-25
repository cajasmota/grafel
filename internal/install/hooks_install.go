// hooks_install.go implements `archigraph install-hooks` (issues #2213, #2222).
//
// InstallPrePushHook writes a pre-push hook script into <repo>/.git/hooks/
// (or delegates to husky / lefthook if detected). The hook runs
// `archigraph doctor` before every push and warns on drift — it NEVER
// blocks the push.
//
// InstallGitHooks (issue #2222) extends the above to also install
// post-checkout, post-merge, and post-rewrite hooks that trigger
// ref-aware reindex via the existing daemon. All 4 hooks are managed
// idempotently and are tolerant of `archigraph` not being on PATH.
package install

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	// PrePushHookMarkerBegin / MarkerEnd delimit the archigraph-managed block.
	prePushMarkerBegin = "# >>> archigraph pre-push >>>"
	prePushMarkerEnd   = "# <<< archigraph pre-push <<<"

	// prePushHookScript is the managed block content.
	prePushHookScript = `%s
# archigraph doctor — warns on install drift but NEVER blocks the push.
if command -v archigraph >/dev/null 2>&1; then
  archigraph doctor --quick 2>/dev/null || \
    echo "archigraph: drift detected — run 'archigraph doctor' to investigate" >&2
fi
%s
`

	// post-checkout hook: fires on branch switches; signals daemon via status RPC.
	postCheckoutMarkerBegin = "# >>> archigraph post-checkout >>>"
	postCheckoutMarkerEnd   = "# <<< archigraph post-checkout <<<"
	postCheckoutHookScript  = `%s
# Only act on branch switches (arg 3 == 1), not file checkouts.
if [ "${3:-0}" = "1" ] && command -v archigraph >/dev/null 2>&1; then
  archigraph status --ref @current >/dev/null 2>&1 &
fi
%s
`

	// post-merge hook: fires after git merge / git pull.
	postMergeMarkerBegin = "# >>> archigraph post-merge >>>"
	postMergeMarkerEnd   = "# <<< archigraph post-merge <<<"
	postMergeHookScript  = `%s
# Signal the daemon to reindex after the merge; run in background so the
# hook returns immediately and never delays the user's merge workflow.
if command -v archigraph >/dev/null 2>&1; then
  archigraph status --ref @current >/dev/null 2>&1 &
fi
%s
`

	// post-rewrite hook: fires after git rebase / git commit --amend.
	postRewriteMarkerBegin = "# >>> archigraph post-rewrite >>>"
	postRewriteMarkerEnd   = "# <<< archigraph post-rewrite <<<"
	postRewriteHookScript  = `%s
# Signal the daemon to reindex after the rewrite; run in background so
# the hook returns immediately and never delays the user's workflow.
if command -v archigraph >/dev/null 2>&1; then
  archigraph status --ref @current >/dev/null 2>&1 &
fi
%s
`
)

// HookInstallOptions controls InstallPrePushHook / InstallGitHooks behaviour.
type HookInstallOptions struct {
	// RepoPath is the root of the git repository.  Defaults to os.Getwd().
	RepoPath string

	// DryRun prints actions without writing anything.
	DryRun bool

	// Force overwrites an existing pre-push hook managed block.
	Force bool
}

// InstallPrePushHook installs the archigraph pre-push hook into the repo.
// If husky or lefthook is detected in the repo, a config snippet is printed
// instead (these tools manage their own hook directory).
func InstallPrePushHook(opts HookInstallOptions) error {
	if opts.RepoPath == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("resolve working dir: %w", err)
		}
		opts.RepoPath = cwd
	}

	// ── detect hook managers ──────────────────────────────────────────────────
	if detected, name := detectHookManager(opts.RepoPath); detected {
		printHookManagerAdvice(name, opts.RepoPath)
		return nil
	}

	// ── find .git/hooks ───────────────────────────────────────────────────────
	hooksDir := filepath.Join(opts.RepoPath, ".git", "hooks")
	if _, err := os.Stat(hooksDir); err != nil {
		return fmt.Errorf("no .git/hooks directory found at %s (is this a git repo?): %w", opts.RepoPath, err)
	}

	hookPath := filepath.Join(hooksDir, "pre-push")
	block := fmt.Sprintf(prePushHookScript, prePushMarkerBegin, prePushMarkerEnd)

	if opts.DryRun {
		fmt.Fprintf(os.Stdout, "archigraph install-hooks (dry-run): would write pre-push hook to %s\n", hookPath)
		fmt.Fprintf(os.Stdout, "Block content:\n%s\n", block)
		return nil
	}

	return writeHookBlock(hookPath, block)
}

// InstallGitHooks installs all 4 archigraph-managed git hooks into the repo:
//
//   - pre-push        (from #2213) — runs `archigraph doctor --quick`
//   - post-checkout   (from #2222) — signals daemon on branch switch
//   - post-merge      (from #2222) — signals daemon after merge
//   - post-rewrite    (from #2222) — signals daemon after rebase/amend
//
// If husky, lefthook, or pre-commit is detected in the repo, advice is
// printed instead and nil is returned (not an error). All hooks are
// idempotent: re-running replaces the managed block without touching any
// user-written content.
func InstallGitHooks(opts HookInstallOptions) error {
	if opts.RepoPath == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("resolve working dir: %w", err)
		}
		opts.RepoPath = cwd
	}

	// ── detect hook managers ──────────────────────────────────────────────────
	if detected, name := detectHookManager(opts.RepoPath); detected {
		printHookManagerAdvice(name, opts.RepoPath)
		return nil
	}

	// ── find .git/hooks ───────────────────────────────────────────────────────
	hooksDir := filepath.Join(opts.RepoPath, ".git", "hooks")
	if _, err := os.Stat(hooksDir); err != nil {
		return fmt.Errorf("no .git/hooks directory found at %s (is this a git repo?): %w", opts.RepoPath, err)
	}

	type hookDef struct {
		name   string
		script string
	}

	hooks := []hookDef{
		{
			name:   "pre-push",
			script: fmt.Sprintf(prePushHookScript, prePushMarkerBegin, prePushMarkerEnd),
		},
		{
			name:   "post-checkout",
			script: fmt.Sprintf(postCheckoutHookScript, postCheckoutMarkerBegin, postCheckoutMarkerEnd),
		},
		{
			name:   "post-merge",
			script: fmt.Sprintf(postMergeHookScript, postMergeMarkerBegin, postMergeMarkerEnd),
		},
		{
			name:   "post-rewrite",
			script: fmt.Sprintf(postRewriteHookScript, postRewriteMarkerBegin, postRewriteMarkerEnd),
		},
	}

	for _, h := range hooks {
		hookPath := filepath.Join(hooksDir, h.name)
		if opts.DryRun {
			fmt.Fprintf(os.Stdout, "archigraph install-hooks (dry-run): would write %s hook to %s\n", h.name, hookPath)
			fmt.Fprintf(os.Stdout, "Block content:\n%s\n", h.script)
			continue
		}
		if err := writeHookBlock(hookPath, h.script); err != nil {
			return fmt.Errorf("write %s hook: %w", h.name, err)
		}
	}
	return nil
}

// writeHookBlock writes the archigraph managed block into hookPath.
// If the file does not exist it is created with a shebang.
// If the block already exists it is replaced (idempotent).
func writeHookBlock(hookPath, block string) error {
	// Read existing content.
	var existing string
	data, err := os.ReadFile(hookPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read hook file: %w", err)
	}
	if err == nil {
		existing = string(data)
	}

	newContent := mergeHookBlock(existing, block)

	if err := os.WriteFile(hookPath, []byte(newContent), 0o755); err != nil {
		return fmt.Errorf("write hook file %s: %w", hookPath, err)
	}
	return nil
}

// mergeHookBlock inserts or replaces the archigraph managed block in content.
// The begin/end markers are extracted from the first and last "# >>> / # <<<"
// lines of block itself so the function works for any hook type.
// The result always starts with a sh shebang if the file was empty.
func mergeHookBlock(existing, block string) string {
	const shebang = "#!/bin/sh\n"

	// Remove any existing managed block of the same type.
	markerBegin, markerEnd := extractMarkers(block)
	cleaned := removeHookBlockMarked(existing, markerBegin, markerEnd)

	if cleaned == "" {
		// New file: start with shebang.
		cleaned = shebang
	} else if cleaned == shebang {
		// Just the shebang: no trailing newline needed.
	}

	// Ensure there is exactly one blank line between the existing content and
	// our block when the file is non-trivial.
	if len(cleaned) > 0 && cleaned[len(cleaned)-1] != '\n' {
		cleaned += "\n"
	}
	return cleaned + "\n" + block
}

// extractMarkers scans block for lines of the form "# >>> … >>>" and
// "# <<< … <<<" and returns them as the begin and end markers.
// Falls back to the pre-push markers when none are found (backward compat).
func extractMarkers(block string) (begin, end string) {
	for _, line := range splitByNewline(block) {
		if len(line) > 4 && line[:4] == "# >>" {
			begin = line
		}
		if len(line) > 4 && line[:4] == "# <<" {
			end = line
		}
	}
	if begin == "" {
		begin = prePushMarkerBegin
	}
	if end == "" {
		end = prePushMarkerEnd
	}
	return
}

// removeHookBlock strips the archigraph pre-push managed block from content.
// Kept for backward compatibility with existing call sites.
func removeHookBlock(content string) string {
	return removeHookBlockMarked(content, prePushMarkerBegin, prePushMarkerEnd)
}

// removeHookBlockMarked strips the archigraph managed block delimited by
// markerBegin and markerEnd from content.
func removeHookBlockMarked(content, markerBegin, markerEnd string) string {
	startIdx := indexOf(content, markerBegin)
	if startIdx < 0 {
		return content
	}
	endIdx := indexOf(content, markerEnd)
	if endIdx < 0 {
		// Malformed: no end marker — remove from start to end of file.
		return content[:startIdx]
	}
	// Include the newline after the end marker.
	after := endIdx + len(markerEnd)
	if after < len(content) && content[after] == '\n' {
		after++
	}
	return content[:startIdx] + content[after:]
}

// indexOf returns the byte index of substr in s, or -1 if not found.
func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// detectHookManager checks whether husky or lefthook manages git hooks in the
// repo. Returns (true, name) when one is detected.
func detectHookManager(repoPath string) (bool, string) {
	// husky: presence of .husky/ directory
	if _, err := os.Stat(filepath.Join(repoPath, ".husky")); err == nil {
		return true, "husky"
	}
	// lefthook: presence of lefthook.yml or lefthook.yaml
	for _, name := range []string{"lefthook.yml", "lefthook.yaml"} {
		if _, err := os.Stat(filepath.Join(repoPath, name)); err == nil {
			return true, "lefthook"
		}
	}
	// pre-commit (Python): .pre-commit-config.yaml
	if _, err := os.Stat(filepath.Join(repoPath, ".pre-commit-config.yaml")); err == nil {
		return true, "pre-commit"
	}
	return false, ""
}

// printHookManagerAdvice prints instructions for adding the archigraph
// hooks via the detected hook manager (pre-push + post-checkout/merge/rewrite).
func printHookManagerAdvice(manager, repoPath string) {
	fmt.Fprintf(os.Stdout, "Detected %s in %s.\n", manager, repoPath)
	fmt.Fprintln(os.Stdout, "")
	switch manager {
	case "husky":
		fmt.Fprintln(os.Stdout, "Add the archigraph hooks to husky:")
		fmt.Fprintln(os.Stdout, "  npx husky add .husky/pre-push \"archigraph doctor --quick 2>/dev/null || echo 'archigraph: drift detected — run archigraph doctor' >&2\"")
		fmt.Fprintln(os.Stdout, "  npx husky add .husky/post-checkout \"if [ \\\"${3:-0}\\\" = '1' ]; then archigraph status --ref @current >/dev/null 2>&1 & fi\"")
		fmt.Fprintln(os.Stdout, "  npx husky add .husky/post-merge \"archigraph status --ref @current >/dev/null 2>&1 &\"")
		fmt.Fprintln(os.Stdout, "  npx husky add .husky/post-rewrite \"archigraph status --ref @current >/dev/null 2>&1 &\"")
	case "lefthook":
		fmt.Fprintln(os.Stdout, "Add to lefthook.yml:")
		fmt.Fprintln(os.Stdout, "  pre-push:")
		fmt.Fprintln(os.Stdout, "    commands:")
		fmt.Fprintln(os.Stdout, "      archigraph-doctor:")
		fmt.Fprintln(os.Stdout, "        run: archigraph doctor --quick 2>/dev/null || echo 'archigraph: drift detected' >&2")
		fmt.Fprintln(os.Stdout, "  post-checkout:")
		fmt.Fprintln(os.Stdout, "    commands:")
		fmt.Fprintln(os.Stdout, "      archigraph-reindex:")
		fmt.Fprintln(os.Stdout, "        run: archigraph status --ref @current >/dev/null 2>&1")
		fmt.Fprintln(os.Stdout, "  post-merge:")
		fmt.Fprintln(os.Stdout, "    commands:")
		fmt.Fprintln(os.Stdout, "      archigraph-reindex:")
		fmt.Fprintln(os.Stdout, "        run: archigraph status --ref @current >/dev/null 2>&1")
		fmt.Fprintln(os.Stdout, "  post-rewrite:")
		fmt.Fprintln(os.Stdout, "    commands:")
		fmt.Fprintln(os.Stdout, "      archigraph-reindex:")
		fmt.Fprintln(os.Stdout, "        run: archigraph status --ref @current >/dev/null 2>&1")
	case "pre-commit":
		fmt.Fprintln(os.Stdout, "Add to .pre-commit-config.yaml:")
		fmt.Fprintln(os.Stdout, "  - repo: local")
		fmt.Fprintln(os.Stdout, "    hooks:")
		fmt.Fprintln(os.Stdout, "      - id: archigraph-doctor")
		fmt.Fprintln(os.Stdout, "        name: archigraph doctor")
		fmt.Fprintln(os.Stdout, "        entry: archigraph doctor --quick")
		fmt.Fprintln(os.Stdout, "        language: system")
		fmt.Fprintln(os.Stdout, "        stages: [pre-push]")
		fmt.Fprintln(os.Stdout, "  Note: pre-commit does not support post-checkout/merge/rewrite hooks.")
		fmt.Fprintln(os.Stdout, "        Run 'archigraph install-hooks --force' to install those directly.")
	}
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Or run 'archigraph install-hooks --force' to install directly into .git/hooks/ instead.")
}

// IsDoctorQuickFlagSupported is a helper that checks whether the
// `archigraph doctor --quick` flag exists in the installed binary.
// Used by tests; not part of the public API.
func IsDoctorQuickFlagSupported(binPath string) bool {
	cmd := exec.Command(binPath, "doctor", "--help")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	for _, line := range splitByNewline(string(out)) {
		if contains(line, "--quick") {
			return true
		}
	}
	return false
}

func splitByNewline(s string) []string {
	var lines []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func contains(s, substr string) bool {
	return indexOf(s, substr) >= 0
}
