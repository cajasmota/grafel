// hooks_install.go implements `archigraph install-hooks` (issue #2213).
//
// InstallPrePushHook writes a pre-push hook script into <repo>/.git/hooks/
// (or delegates to husky / lefthook if detected). The hook runs
// `archigraph doctor` before every push and warns on drift — it NEVER
// blocks the push.
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
)

// HookInstallOptions controls InstallPrePushHook behaviour.
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
// If the block already exists (marked by prePushMarkerBegin/End) it is
// replaced.  Otherwise the block is appended.
// The result always starts with a sh shebang if the file was empty.
func mergeHookBlock(existing, block string) string {
	const shebang = "#!/bin/sh\n"

	// Remove any existing managed block.
	cleaned := removeHookBlock(existing)

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

// removeHookBlock strips the archigraph-managed block from content.
func removeHookBlock(content string) string {
	startIdx := indexOf(content, prePushMarkerBegin)
	if startIdx < 0 {
		return content
	}
	endIdx := indexOf(content, prePushMarkerEnd)
	if endIdx < 0 {
		// Malformed: no end marker — remove from start to end of file.
		return content[:startIdx]
	}
	// Include the newline after the end marker.
	after := endIdx + len(prePushMarkerEnd)
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
// pre-push hook via the detected hook manager.
func printHookManagerAdvice(manager, repoPath string) {
	fmt.Fprintf(os.Stdout, "Detected %s in %s.\n", manager, repoPath)
	fmt.Fprintln(os.Stdout, "")
	switch manager {
	case "husky":
		fmt.Fprintln(os.Stdout, "Add the archigraph pre-push hook to husky:")
		fmt.Fprintln(os.Stdout, "  npx husky add .husky/pre-push \"archigraph doctor --quick 2>/dev/null || echo 'archigraph: drift detected — run archigraph doctor' >&2\"")
	case "lefthook":
		fmt.Fprintln(os.Stdout, "Add to lefthook.yml:")
		fmt.Fprintln(os.Stdout, "  pre-push:")
		fmt.Fprintln(os.Stdout, "    commands:")
		fmt.Fprintln(os.Stdout, "      archigraph-doctor:")
		fmt.Fprintln(os.Stdout, "        run: archigraph doctor --quick 2>/dev/null || echo 'archigraph: drift detected' >&2")
	case "pre-commit":
		fmt.Fprintln(os.Stdout, "Add to .pre-commit-config.yaml:")
		fmt.Fprintln(os.Stdout, "  - repo: local")
		fmt.Fprintln(os.Stdout, "    hooks:")
		fmt.Fprintln(os.Stdout, "      - id: archigraph-doctor")
		fmt.Fprintln(os.Stdout, "        name: archigraph doctor")
		fmt.Fprintln(os.Stdout, "        entry: archigraph doctor --quick")
		fmt.Fprintln(os.Stdout, "        language: system")
		fmt.Fprintln(os.Stdout, "        stages: [pre-push]")
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
