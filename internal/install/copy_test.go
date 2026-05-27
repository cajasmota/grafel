package install_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/install"
	"github.com/cajasmota/archigraph/internal/install/skilllink"
)

// TestRunCopy_HappyPath verifies the complete COPY-mode install transaction:
// skills are copied, MCP is registered, install.json is written with the
// correct shape.  The daemon restart step is skipped (SkipDaemonRestart=true)
// because no real daemon is running in unit tests.
func TestRunCopy_HappyPath(t *testing.T) {
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

	result, err := install.RunCopy(opts)
	if err != nil {
		t.Fatalf("RunCopy: %v", err)
	}

	// Step 1: CLI binary identified.
	if result.CLIPath != env.fakeBin {
		t.Errorf("CLIPath = %q, want %q", result.CLIPath, env.fakeBin)
	}
	if result.CLISHA256 == "" {
		t.Error("CLISHA256 is empty")
	}

	// Step 2: skills copied.
	if len(result.SkillsInstalled) == 0 {
		t.Error("no skills reported as installed")
	}
	// Verify at least one skill file exists at the destination.
	destSkillsDir := filepath.Join(filepath.Dir(env.claudeJSON), "skills")
	for _, skillName := range result.SkillsInstalled {
		dst := filepath.Join(destSkillsDir, skillName)
		if _, err := os.Stat(dst); err != nil {
			t.Errorf("skill %s not found at destination %s: %v", skillName, dst, err)
		}
	}

	// Step 3: MCP registered.
	if len(result.MCPPaths) == 0 {
		t.Error("no MCP paths reported")
	}
	// Verify MCP entry in .claude.json.
	assertMCPRegistered(t, env.claudeJSON, env.fakeBin)

	// Step 4 (daemon): skipped.

	// Step 5: .gitignore updated (if git is available; skip the assertion if not).
	if result.GitignoreRepo != "" {
		assertGitignoreEntry(t, result.GitignoreRepo)
	} else {
		t.Log("git not available in test env or not detected; skipping .gitignore assertion")
	}

	// Step 6: install.json written.
	if result.StatePath == "" {
		t.Error("StatePath is empty")
	}
	state := readState(t, result.StatePath)
	if state.SchemaVersion != install.StateSchemaVersion {
		t.Errorf("schema_version = %d, want %d", state.SchemaVersion, install.StateSchemaVersion)
	}
	if state.InstallMode != install.ModeCopy {
		t.Errorf("install_mode = %q, want %q", state.InstallMode, install.ModeCopy)
	}
	if state.CLI.SHA256 == "" {
		t.Error("install.json: cli.sha256 is empty")
	}
	if len(state.Skills) == 0 {
		t.Error("install.json: skills map is empty")
	}
	if state.PartialInstall {
		t.Error("install.json: partial_install should be false after successful install")
	}
}

// TestRunCopy_Idempotent verifies that running install twice leaves the system
// in an equivalent state and does not error on the second run.
func TestRunCopy_Idempotent(t *testing.T) {
	env := newTestEnv(t)

	opts := install.CopyOptions{
		BinPath:           env.fakeBin,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkipDaemonRestart: true,
	}

	// First run.
	r1, err := install.RunCopy(opts)
	if err != nil {
		t.Fatalf("first RunCopy: %v", err)
	}

	// Second run — should succeed and be a no-op for skill files.
	r2, err := install.RunCopy(opts)
	if err != nil {
		t.Fatalf("second RunCopy (idempotency): %v", err)
	}

	// Both runs should report the same skills.
	if len(r1.SkillsInstalled) != len(r2.SkillsInstalled) {
		t.Errorf("idempotency: first run installed %d skills, second run %d",
			len(r1.SkillsInstalled), len(r2.SkillsInstalled))
	}

	// .gitignore should not have a duplicate entry (only check if git is available).
	if r2.GitignoreRepo != "" {
		gitignorePath := filepath.Join(r2.GitignoreRepo, ".gitignore")
		data, err := os.ReadFile(gitignorePath)
		if err != nil {
			t.Fatalf("read .gitignore: %v", err)
		}
		count := 0
		for _, line := range splitLines(string(data)) {
			if line == "/.archigraph/" {
				count++
			}
		}
		if count != 1 {
			t.Errorf(".gitignore: expected exactly 1 /.archigraph/ entry, got %d (content: %q)", count, string(data))
		}
	}
}

// TestRunCopy_RollbackOnStep4Failure verifies that when the daemon restart
// step fails, the skills and MCP registrations are rolled back and
// install.json records PartialInstall=true.
//
// We inject a stub DaemonRestartFunc that always returns an error.
func TestRunCopy_RollbackOnStep4Failure(t *testing.T) {
	env := newTestEnv(t)

	opts := install.CopyOptions{
		BinPath:           env.fakeBin,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkipDaemonRestart: false, // let step 4 run and fail
		// Inject a stub that always fails (simulating a daemon restart error).
		RestartDaemon: func(_ string, _ int, _ time.Duration) (string, error) {
			return "", fmt.Errorf("injected daemon restart failure for testing")
		},
	}

	_, err := install.RunCopy(opts)
	if err == nil {
		t.Fatal("expected RunCopy to fail when daemon restart fails, but it succeeded")
	}

	// After rollback: install.json should record the partial state.
	state := readState(t, env.statePath)
	if state == nil {
		t.Fatal("install.json was not written after rollback")
	}
	if !state.PartialInstall {
		t.Error("install.json: partial_install should be true after rollback")
	}
	if state.RollbackFromStep == 0 {
		t.Error("install.json: rollback_from_step should be non-zero after rollback")
	}

	// After rollback: skills should have been removed.
	destSkillsDir := filepath.Join(filepath.Dir(env.claudeJSON), "skills")
	for skillName := range state.Skills {
		dst := filepath.Join(destSkillsDir, skillName)
		if _, err := os.Stat(dst); err == nil {
			t.Errorf("rollback: skill %s still exists at %s after rollback", skillName, dst)
		}
	}
}

// TestRunCopy_PartialInstallGuard verifies that running install when a
// partial install is already recorded returns an error unless --force is set.
func TestRunCopy_PartialInstallGuard(t *testing.T) {
	env := newTestEnv(t)

	// Write a fake partial state.
	partial := install.NewState(install.ModeCopy)
	partial.PartialInstall = true
	partial.RollbackFromStep = 4
	if err := install.WriteState(env.statePath, partial); err != nil {
		t.Fatalf("write partial state: %v", err)
	}

	opts := install.CopyOptions{
		BinPath:           env.fakeBin,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkipDaemonRestart: true,
		Force:             false, // no --force
	}

	_, err := install.RunCopy(opts)
	if err == nil {
		t.Fatal("expected RunCopy to refuse when PartialInstall=true and Force=false")
	}

	// With --force it should proceed.
	opts.Force = true
	_, err = install.RunCopy(opts)
	if err != nil {
		t.Fatalf("RunCopy with --force: %v", err)
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

// testEnv holds all temp paths needed by a test.
type testEnv struct {
	// fakeBin is a minimal executable (just enough to hash).
	fakeBin string
	// skillsSourceDir is a temp dir with two fake skill subdirs.
	skillsSourceDir string
	// claudeJSON is the path to a fresh .claude.json for MCP registration.
	claudeJSON string
	// statePath is the path where install.json should be written.
	statePath string
	// gitRepo is a temp dir initialised as a git repo (for .gitignore step).
	gitRepo string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	tmp := t.TempDir()

	// Create a fake binary (just a file with some bytes).
	fakeBin := filepath.Join(tmp, "archigraph-fake")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\necho fake"), 0o755); err != nil {
		t.Fatalf("create fake bin: %v", err)
	}

	// Create a fake skills source dir populated with every canonical skill name
	// so the fixture stays in sync with skilllink.SkillNames automatically.
	skillsSourceDir := filepath.Join(tmp, "skills")
	for _, name := range skilllink.SkillNames {
		skillDir := filepath.Join(skillsSourceDir, name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatalf("create skill dir %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# "+name), 0o644); err != nil {
			t.Fatalf("write SKILL.md: %v", err)
		}
	}

	// Create a claude config dir with an empty .claude.json.
	claudeDir := filepath.Join(tmp, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("create claude dir: %v", err)
	}
	claudeJSON := filepath.Join(claudeDir, ".claude.json")
	if err := os.WriteFile(claudeJSON, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write .claude.json: %v", err)
	}

	// install.json path.
	stateDir := filepath.Join(tmp, ".archigraph")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("create state dir: %v", err)
	}
	statePath := filepath.Join(stateDir, "install.json")

	// Create a git repo so .gitignore detection succeeds.
	// We run `git init` to get a real git repo that git rev-parse can see.
	gitRepo := filepath.Join(tmp, "myrepo")
	if err := os.MkdirAll(gitRepo, 0o755); err != nil {
		t.Fatalf("create git repo dir: %v", err)
	}
	{
		// Try to init a real git repo; if git is not available, fall back
		// to creating a minimal .git directory that works for most OS git
		// versions (git rev-parse --show-toplevel only needs .git/HEAD).
		out, gerr := exec.Command("git", "-C", gitRepo, "init", "-q").CombinedOutput()
		if gerr != nil {
			// Fallback: create a minimal .git tree manually.
			t.Logf("git init failed (%v: %s); creating minimal .git manually", gerr, out)
			gitDir := filepath.Join(gitRepo, ".git")
			if err := os.MkdirAll(filepath.Join(gitDir, "refs"), 0o755); err != nil {
				t.Fatalf("create .git/refs: %v", err)
			}
			if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
				t.Fatalf("write .git/HEAD: %v", err)
			}
		}
	}

	// Override HOME so home-dir dependent paths go to tmp.
	t.Setenv("HOME", tmp)

	return &testEnv{
		fakeBin:         fakeBin,
		skillsSourceDir: skillsSourceDir,
		claudeJSON:      claudeJSON,
		statePath:       statePath,
		gitRepo:         gitRepo,
	}
}

func readState(t *testing.T, path string) *install.State {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read state %s: %v", path, err)
	}
	var s install.State
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("parse state %s: %v", path, err)
	}
	return &s
}

func assertMCPRegistered(t *testing.T, claudeJSON, binPath string) {
	t.Helper()
	data, err := os.ReadFile(claudeJSON)
	if err != nil {
		t.Fatalf("read .claude.json: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse .claude.json: %v", err)
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if servers == nil {
		t.Error(".claude.json: mcpServers not found")
		return
	}
	entry, ok := servers["archigraph"]
	if !ok {
		t.Error(".claude.json: archigraph entry not found in mcpServers")
		return
	}
	entryMap, _ := entry.(map[string]any)
	if entryMap == nil {
		t.Error(".claude.json: archigraph entry is not an object")
		return
	}
	if cmd, _ := entryMap["command"].(string); cmd != binPath {
		t.Errorf(".claude.json: archigraph.command = %q, want %q", cmd, binPath)
	}
}

func assertGitignoreEntry(t *testing.T, repoRoot string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	for _, line := range splitLines(string(data)) {
		if line == "/.archigraph/" {
			return
		}
	}
	t.Errorf(".gitignore does not contain /.archigraph/; content: %q", string(data))
}

// TestRunCopy_MissingSkillsDirectory verifies that when the skills directory
// cannot be discovered, the error message includes the current working directory
// and suggests using --skills-source-dir.
func TestRunCopy_MissingSkillsDirectory(t *testing.T) {
	env := newTestEnv(t)

	opts := install.CopyOptions{
		BinPath:           env.fakeBin,
		SkillsSourceDir:   "/nonexistent/skills",
		ClaudeConfigDirs:  []string{env.claudeJSON},
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkipDaemonRestart: true,
	}

	_, err := install.RunCopy(opts)
	if err == nil {
		t.Fatal("expected RunCopy to fail when skills directory is missing")
	}

	errMsg := err.Error()
	// Check that the error message includes the actionable hints.
	if !strings.Contains(errMsg, "no skills/ directory found") {
		t.Errorf("error should mention missing skills directory; got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "--skills-source-dir") {
		t.Errorf("error should suggest --skills-source-dir flag; got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "/skills") {
		t.Errorf("error should show path suffix /skills; got: %s", errMsg)
	}
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
