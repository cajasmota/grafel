package install_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/install"
)

// TestEnsureGitignore_CreatesFile verifies that .gitignore is created if it
// does not exist, with the archigraph entry and a comment.
func TestEnsureGitignore_CreatesFile(t *testing.T) {
	repoRoot := t.TempDir()

	gitignorePath, err := install.EnsureGitignore(repoRoot)
	if err != nil {
		t.Fatalf("EnsureGitignore: %v", err)
	}

	if gitignorePath == "" {
		t.Error("gitignorePath is empty")
	}

	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}

	content := string(data)
	if !bytes.Contains(data, []byte("/.archigraph/")) {
		t.Errorf(".gitignore does not contain /.archigraph/; content: %q", content)
	}
}

// TestEnsureGitignore_Idempotent verifies that calling EnsureGitignore
// multiple times does not duplicate the entry.
func TestEnsureGitignore_Idempotent(t *testing.T) {
	repoRoot := t.TempDir()

	// First call creates the file.
	path1, err := install.EnsureGitignore(repoRoot)
	if err != nil {
		t.Fatalf("first EnsureGitignore: %v", err)
	}

	data1, err := os.ReadFile(path1)
	if err != nil {
		t.Fatalf("read .gitignore after first call: %v", err)
	}

	// Count occurrences of the entry in the first version.
	count1 := bytes.Count(data1, []byte("/.archigraph/"))
	if count1 != 1 {
		t.Errorf("after first call, entry appears %d times, want 1", count1)
	}

	// Second call should be idempotent (no modification).
	path2, err := install.EnsureGitignore(repoRoot)
	if err != nil {
		t.Fatalf("second EnsureGitignore: %v", err)
	}

	if path1 != path2 {
		t.Errorf("paths differ: %q vs %q", path1, path2)
	}

	data2, err := os.ReadFile(path2)
	if err != nil {
		t.Fatalf("read .gitignore after second call: %v", err)
	}

	// Entry should still appear exactly once.
	count2 := bytes.Count(data2, []byte("/.archigraph/"))
	if count2 != 1 {
		t.Errorf("after second call, entry appears %d times, want 1", count2)
	}

	// Content should be identical.
	if !bytes.Equal(data1, data2) {
		t.Errorf("content changed after second call:\nfirst:\n%s\nsecond:\n%s", data1, data2)
	}
}

// TestEnsureGitignore_ExistingEntry verifies that if the entry already
// exists (possibly with whitespace), it is not duplicated.
func TestEnsureGitignore_ExistingEntry(t *testing.T) {
	repoRoot := t.TempDir()
	gitignorePath := filepath.Join(repoRoot, ".gitignore")

	// Create a .gitignore with the entry already present (with leading/trailing space).
	existingContent := "node_modules/\n  /.archigraph/  \nfoo/\n"
	if err := os.WriteFile(gitignorePath, []byte(existingContent), 0o644); err != nil {
		t.Fatalf("write existing .gitignore: %v", err)
	}

	returnedPath, err := install.EnsureGitignore(repoRoot)
	if err != nil {
		t.Fatalf("EnsureGitignore: %v", err)
	}

	data, err := os.ReadFile(returnedPath)
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}

	// Entry should appear exactly once (trimmed version should match).
	if !bytes.Contains(data, []byte("/.archigraph/")) {
		t.Error("entry not found in .gitignore")
	}
	count := bytes.Count(data, []byte("/.archigraph/"))
	if count != 1 {
		t.Errorf("entry appears %d times, want 1; content:\n%s", count, string(data))
	}
}

// TestEnsureGitignore_PreservesExistingContent verifies that the file's
// existing content is preserved when appending the entry.
func TestEnsureGitignore_PreservesExistingContent(t *testing.T) {
	repoRoot := t.TempDir()
	gitignorePath := filepath.Join(repoRoot, ".gitignore")

	existingContent := "node_modules/\nvenv/\n"
	if err := os.WriteFile(gitignorePath, []byte(existingContent), 0o644); err != nil {
		t.Fatalf("write existing .gitignore: %v", err)
	}

	_, err := install.EnsureGitignore(repoRoot)
	if err != nil {
		t.Fatalf("EnsureGitignore: %v", err)
	}

	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}

	content := string(data)

	// Check that original entries are preserved.
	if !bytes.Contains(data, []byte("node_modules/")) {
		t.Error("node_modules/ was lost")
	}
	if !bytes.Contains(data, []byte("venv/")) {
		t.Error("venv/ was lost")
	}

	// Check that the new entry was added.
	if !bytes.Contains(data, []byte("/.archigraph/")) {
		t.Error("/.archigraph/ was not added")
	}

	// Verify the structure: existing content, then new entry.
	if !bytes.HasPrefix(data, []byte("node_modules/")) {
		t.Errorf("existing content was reordered; content: %q", content)
	}
}

// TestEnsureGitignore_NoNewlineAtEnd verifies that when the existing file
// doesn't end with a newline, one is inserted before appending.
func TestEnsureGitignore_NoNewlineAtEnd(t *testing.T) {
	repoRoot := t.TempDir()
	gitignorePath := filepath.Join(repoRoot, ".gitignore")

	// File without trailing newline.
	existingContent := "node_modules/"
	if err := os.WriteFile(gitignorePath, []byte(existingContent), 0o644); err != nil {
		t.Fatalf("write existing .gitignore: %v", err)
	}

	_, err := install.EnsureGitignore(repoRoot)
	if err != nil {
		t.Fatalf("EnsureGitignore: %v", err)
	}

	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}

	content := string(data)

	// Check structure: original line, newline, new entry, newline.
	expected := "node_modules/\n/.archigraph/\n"
	if content != expected {
		t.Errorf("content mismatch:\ngot:      %q\nexpected: %q", content, expected)
	}
}

// TestEnsureGitignore_EmptyFile verifies that an empty .gitignore file
// is populated with the entry (without a leading newline).
func TestEnsureGitignore_EmptyFile(t *testing.T) {
	repoRoot := t.TempDir()
	gitignorePath := filepath.Join(repoRoot, ".gitignore")

	// Create an empty file.
	if err := os.WriteFile(gitignorePath, []byte(""), 0o644); err != nil {
		t.Fatalf("write empty .gitignore: %v", err)
	}

	_, err := install.EnsureGitignore(repoRoot)
	if err != nil {
		t.Fatalf("EnsureGitignore: %v", err)
	}

	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}

	content := string(data)

	// Should be just the entry with a trailing newline, no leading newline.
	expected := "/.archigraph/\n"
	if content != expected {
		t.Errorf("content mismatch:\ngot:      %q\nexpected: %q", content, expected)
	}
}

// TestDetectGitRepo_InRepo verifies that DetectGitRepo returns the repo root
// when called from within a git repository.
func TestDetectGitRepo_InRepo(t *testing.T) {
	// Use the test env helper from copy_test.go (indirectly by creating a git repo).
	tmp := t.TempDir()
	gitRepo := filepath.Join(tmp, "myrepo")
	if err := os.MkdirAll(gitRepo, 0o755); err != nil {
		t.Fatalf("create git repo dir: %v", err)
	}

	// Initialise the repo by passing the path directly to git init — avoids
	// os.Chdir which holds the process CWD on the TempDir and causes
	// "The process cannot access the file because it is being used by
	// another process" cleanup failures on Windows.
	out, err := exec.Command("git", "init", "-q", gitRepo).CombinedOutput()
	if err != nil {
		// Git not available — skip this test.
		t.Skipf("git init failed: %v: %s", err, out)
	}

	// Call DetectGitRepo from within the repo.
	root, ok := install.DetectGitRepo(gitRepo)
	if !ok {
		t.Fatal("DetectGitRepo returned false, expected true")
	}

	if root == "" {
		t.Error("DetectGitRepo returned empty root")
	}

	// The returned root should be an absolute path.
	if !filepath.IsAbs(root) {
		t.Errorf("returned root is not absolute: %q", root)
	}
}

// TestDetectGitRepo_NotInRepo verifies that DetectGitRepo returns false
// when called from outside a git repository.
func TestDetectGitRepo_NotInRepo(t *testing.T) {
	notAGitRepo := t.TempDir()

	root, ok := install.DetectGitRepo(notAGitRepo)
	if ok {
		t.Errorf("DetectGitRepo returned true for non-git dir, expected false; root=%q", root)
	}

	if root != "" {
		t.Errorf("DetectGitRepo returned non-empty root for non-git dir: %q", root)
	}
}

// TestIntegration_RunCopy_GitignoreIdempotent tests that RunCopy can be
// called multiple times and the .gitignore remains consistent.
func TestIntegration_RunCopy_GitignoreIdempotent(t *testing.T) {
	env := newTestEnv(t)

	opts := install.CopyOptions{
		BinPath:           env.fakeBin,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkipDaemonRestart: true,
		DryRun:            false,
	}

	// First install.
	result1, err := install.RunCopy(opts)
	if err != nil {
		t.Fatalf("first RunCopy: %v", err)
	}

	gitignorePath := filepath.Join(result1.GitignoreRepo, ".gitignore")
	data1, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("read .gitignore after first install: %v", err)
	}

	// Count the entry in the first version.
	count1 := bytes.Count(data1, []byte("/.archigraph/"))
	if count1 != 1 {
		t.Errorf("after first install, entry appears %d times, want 1", count1)
	}

	// Second install with a different state file path (so it's a fresh run).
	opts.StatePath = filepath.Join(filepath.Dir(env.statePath), "install2.json")

	_, err = install.RunCopy(opts)
	if err != nil {
		t.Fatalf("second RunCopy: %v", err)
	}

	data2, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("read .gitignore after second install: %v", err)
	}

	// Entry should still appear exactly once.
	count2 := bytes.Count(data2, []byte("/.archigraph/"))
	if count2 != 1 {
		t.Errorf("after second install, entry appears %d times, want 1", count2)
	}

	// Content should be identical.
	if !bytes.Equal(data1, data2) {
		t.Errorf(".gitignore changed between installs:\nfirst:\n%s\nsecond:\n%s", data1, data2)
	}
}
