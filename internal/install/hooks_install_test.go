package install_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/install"
)

// TestInstallPrePushHook_HappyPath verifies that the pre-push hook is written
// into .git/hooks/pre-push and contains the managed block.
func TestInstallPrePushHook_HappyPath(t *testing.T) {
	repoDir := makeGitRepo(t)

	opts := install.HookInstallOptions{
		RepoPath: repoDir,
	}

	if err := install.InstallPrePushHook(opts); err != nil {
		t.Fatalf("InstallPrePushHook: %v", err)
	}

	hookPath := filepath.Join(repoDir, ".git", "hooks", "pre-push")
	assertPrePushHookExists(t, hookPath)
}

// TestInstallPrePushHook_Idempotent verifies that running install-hooks twice
// does not duplicate the managed block.
func TestInstallPrePushHook_Idempotent(t *testing.T) {
	repoDir := makeGitRepo(t)

	opts := install.HookInstallOptions{RepoPath: repoDir}

	// First install.
	if err := install.InstallPrePushHook(opts); err != nil {
		t.Fatalf("first InstallPrePushHook: %v", err)
	}

	// Second install.
	if err := install.InstallPrePushHook(opts); err != nil {
		t.Fatalf("second InstallPrePushHook: %v", err)
	}

	hookPath := filepath.Join(repoDir, ".git", "hooks", "pre-push")
	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("read hook file: %v", err)
	}
	content := string(data)

	// Count occurrences of the managed block marker.
	count := strings.Count(content, "# >>> archigraph pre-push >>>")
	if count != 1 {
		t.Errorf("expected exactly 1 managed block, found %d\ncontent: %q", count, content)
	}
}

// TestInstallPrePushHook_PreservesExistingContent verifies that a pre-existing
// pre-push hook's user content is preserved and our block is appended.
func TestInstallPrePushHook_PreservesExistingContent(t *testing.T) {
	repoDir := makeGitRepo(t)

	hooksDir := filepath.Join(repoDir, ".git", "hooks")
	hookPath := filepath.Join(hooksDir, "pre-push")

	// Write a user hook.
	userContent := "#!/bin/sh\n# user's own pre-push logic\nexit 0\n"
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("create hooks dir: %v", err)
	}
	if err := os.WriteFile(hookPath, []byte(userContent), 0o755); err != nil {
		t.Fatalf("write user hook: %v", err)
	}

	opts := install.HookInstallOptions{RepoPath: repoDir}
	if err := install.InstallPrePushHook(opts); err != nil {
		t.Fatalf("InstallPrePushHook: %v", err)
	}

	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("read hook file: %v", err)
	}
	content := string(data)

	// User content must be preserved.
	if !strings.Contains(content, "user's own pre-push logic") {
		t.Errorf("user hook content was lost; got: %q", content)
	}

	// Managed block must also be present.
	if !strings.Contains(content, "# >>> archigraph pre-push >>>") {
		t.Errorf("managed block not found; got: %q", content)
	}
}

// TestInstallPrePushHook_DryRun verifies that dry-run does not create any files.
func TestInstallPrePushHook_DryRun(t *testing.T) {
	repoDir := makeGitRepo(t)

	opts := install.HookInstallOptions{
		RepoPath: repoDir,
		DryRun:   true,
	}

	if err := install.InstallPrePushHook(opts); err != nil {
		t.Fatalf("InstallPrePushHook --dry-run: %v", err)
	}

	hookPath := filepath.Join(repoDir, ".git", "hooks", "pre-push")
	if _, err := os.Stat(hookPath); err == nil {
		t.Error("dry-run should not create pre-push hook file")
	}
}

// TestInstallPrePushHook_HooksyDetection verifies that when a .husky directory
// exists, the function returns nil (prints advice instead of failing).
func TestInstallPrePushHook_HuskyDetection(t *testing.T) {
	repoDir := makeGitRepo(t)

	// Create a .husky directory to simulate husky.
	huskyDir := filepath.Join(repoDir, ".husky")
	if err := os.MkdirAll(huskyDir, 0o755); err != nil {
		t.Fatalf("create .husky: %v", err)
	}

	opts := install.HookInstallOptions{RepoPath: repoDir}

	// Should return nil (not an error) even though we detected husky.
	if err := install.InstallPrePushHook(opts); err != nil {
		t.Fatalf("InstallPrePushHook with husky: %v", err)
	}

	// The hook should NOT have been written (advice path, not file path).
	hookPath := filepath.Join(repoDir, ".git", "hooks", "pre-push")
	if _, err := os.Stat(hookPath); err == nil {
		t.Error("pre-push hook should not be written when husky is detected")
	}
}

// TestInstallPrePushHook_NoGitRepo verifies that an error is returned when
// there is no .git directory.
func TestInstallPrePushHook_NoGitRepo(t *testing.T) {
	noGitDir := t.TempDir()

	opts := install.HookInstallOptions{RepoPath: noGitDir}
	if err := install.InstallPrePushHook(opts); err == nil {
		t.Error("expected error when .git/hooks does not exist")
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

// makeGitRepo creates a temporary directory with a minimal .git structure.
func makeGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	for _, sub := range []string{
		"hooks",
		"refs",
	} {
		if err := os.MkdirAll(filepath.Join(gitDir, sub), 0o755); err != nil {
			t.Fatalf("create .git/%s: %v", sub, err)
		}
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatalf("write .git/HEAD: %v", err)
	}
	return dir
}

// assertPrePushHookExists checks that the hook file exists, is executable,
// and contains the archigraph managed block.
func assertPrePushHookExists(t *testing.T, hookPath string) {
	t.Helper()

	info, err := os.Stat(hookPath)
	if err != nil {
		t.Fatalf("pre-push hook not found at %s: %v", hookPath, err)
	}

	// Must be executable.
	if info.Mode()&0o111 == 0 {
		t.Errorf("pre-push hook at %s is not executable (mode %v)", hookPath, info.Mode())
	}

	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("read pre-push hook: %v", err)
	}
	content := string(data)

	// Must contain shebang.
	if !strings.HasPrefix(content, "#!/bin/sh") {
		t.Errorf("pre-push hook does not start with shebang; got: %q", content[:min(40, len(content))])
	}

	// Must contain managed block markers.
	if !strings.Contains(content, "# >>> archigraph pre-push >>>") {
		t.Errorf("pre-push hook missing begin marker; content: %q", content)
	}
	if !strings.Contains(content, "# <<< archigraph pre-push <<<") {
		t.Errorf("pre-push hook missing end marker; content: %q", content)
	}

	// Must reference archigraph doctor.
	if !strings.Contains(content, "archigraph doctor") {
		t.Errorf("pre-push hook does not call archigraph doctor; content: %q", content)
	}
}
