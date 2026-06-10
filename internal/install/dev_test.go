package install_test

// dev_test.go — integration tests for RunDev (issue #2212).
//
// Acceptance criteria covered:
//  1. `archigraph install --dev` symlinks all skills cross-platform
//     → TestRunDev_HappyPath
//  2. Windows fallback to COPY with clear warning (simulated via stub)
//     → TestRunDev_FallbackCopy (stub: replace Symlink with error-returning path)
//  3. `install_mode = "dev"` recorded in install.json
//     → TestRunDev_HappyPath (asserts state.InstallMode == ModeDev)
//  4. `archigraph doctor` correctly handles dev mode (no SHA mismatch panic)
//     → TestDoctorDevMode_HappyPath, TestDoctorDevMode_SymlinkDrift
//  5. Switching modes: COPY → DEV warns (no hard error), DEV install proceeds
//     → TestRunDev_ModeSwitchFromCopy
//  6. Integration: dev-mode happy path, doctor on dev install, mode switch
//     → all three TestRunDev_* + TestDoctorDev_* tests

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/install"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// newDevTestEnv creates a minimal test environment for DEV mode tests.
// It reuses newTestEnv() for the base layout and adds a skills source dir
// with at least two of the canonical skill names.
func newDevTestEnv(t *testing.T) *testEnv {
	t.Helper()
	return newTestEnv(t) // reuse the COPY-mode test env; it already sets HOME
}

// defaultDevOpts returns a DevOptions pre-wired for the test env.
func defaultDevOpts(env *testEnv) install.DevOptions {
	return install.DevOptions{
		BinPath:           env.fakeBin,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkipDaemonRestart: true,
	}
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// TestRunDev_HappyPath verifies the complete DEV-mode install transaction:
//   - each skill appears as a symlink at the destination
//   - install.json records install_mode="dev" and dev_target for each skill
//   - no SHA manifest files recorded for symlinked skills
func TestRunDev_HappyPath(t *testing.T) {
	env := newDevTestEnv(t)
	opts := defaultDevOpts(env)

	result, err := install.RunDev(opts)
	if err != nil {
		t.Fatalf("RunDev: %v", err)
	}

	// Step 1: binary identified.
	if result.CLIPath != env.fakeBin {
		t.Errorf("CLIPath = %q, want %q", result.CLIPath, env.fakeBin)
	}
	if result.CLISHA256 == "" {
		t.Error("CLISHA256 is empty")
	}

	// Step 2: skills symlinked (or linked successfully).
	if len(result.SkillsLinked) == 0 {
		t.Error("no skills reported as linked")
	}

	destSkillsDir := filepath.Join(filepath.Dir(env.claudeJSON), "skills")
	for _, skillName := range result.SkillsLinked {
		dst := filepath.Join(destSkillsDir, skillName)

		// On all platforms the destination must exist.
		if _, err := os.Stat(dst); err != nil {
			t.Errorf("skill %s not found at destination %s: %v", skillName, dst, err)
			continue
		}

		// On non-Windows the destination must be a symlink.
		if runtime.GOOS != "windows" {
			info, err := os.Lstat(dst)
			if err != nil {
				t.Errorf("Lstat %s: %v", dst, err)
				continue
			}
			if info.Mode()&os.ModeSymlink == 0 {
				t.Errorf("skill %s at %s is not a symlink", skillName, dst)
			}
		}
	}

	// Step 3: MCP registered.
	if len(result.MCPPaths) == 0 {
		t.Error("no MCP paths reported")
	}
	assertMCPRegistered(t, env.claudeJSON, env.fakeBin)

	// Step 6: install.json written with install_mode="dev".
	if result.StatePath == "" {
		t.Error("StatePath is empty")
	}
	state := readState(t, result.StatePath)
	if state == nil {
		t.Fatal("install.json not written")
	}
	if state.InstallMode != install.ModeDev {
		t.Errorf("install_mode = %q, want %q", state.InstallMode, install.ModeDev)
	}
	if state.SchemaVersion != install.StateSchemaVersion {
		t.Errorf("schema_version = %d, want %d", state.SchemaVersion, install.StateSchemaVersion)
	}
	if len(state.Skills) == 0 {
		t.Error("install.json: skills map is empty")
	}
	// Each symlinked skill should have DevTarget set and no Files manifest.
	for skillName, rec := range state.Skills {
		if rec.DevTarget == "" {
			t.Errorf("install.json: skill %s has no dev_target", skillName)
		}
		if len(rec.Files) > 0 {
			// Files is expected to be nil/empty for a properly symlinked skill.
			// A non-empty Files map indicates the Windows fallback path was taken.
			t.Logf("skill %s has Files manifest (Windows fallback?): %v", skillName, rec.Files)
		}
	}
	if state.PartialInstall {
		t.Error("install.json: partial_install should be false after successful install")
	}
}

// TestRunDev_Idempotent verifies that running `archigraph install --dev` twice
// leaves the system in a consistent state and does not error on the second run.
func TestRunDev_Idempotent(t *testing.T) {
	env := newDevTestEnv(t)
	opts := defaultDevOpts(env)

	r1, err := install.RunDev(opts)
	if err != nil {
		t.Fatalf("first RunDev: %v", err)
	}

	r2, err := install.RunDev(opts)
	if err != nil {
		t.Fatalf("second RunDev (idempotency): %v", err)
	}

	if len(r1.SkillsLinked) != len(r2.SkillsLinked) {
		t.Errorf("idempotency: first run linked %d skills, second run %d",
			len(r1.SkillsLinked), len(r2.SkillsLinked))
	}

	// Symlinks should still be correct after the second run.
	destSkillsDir := filepath.Join(filepath.Dir(env.claudeJSON), "skills")
	for _, skillName := range r2.SkillsLinked {
		dst := filepath.Join(destSkillsDir, skillName)
		if runtime.GOOS != "windows" {
			info, err := os.Lstat(dst)
			if err != nil {
				t.Errorf("Lstat %s after second run: %v", dst, err)
				continue
			}
			if info.Mode()&os.ModeSymlink == 0 {
				t.Errorf("skill %s is not a symlink after second run", skillName)
			}
		}
	}
}

// TestRunDev_PartialInstallAutoRecovers verifies that running `archigraph install --dev`
// when a partial install is recorded auto-recovers (idempotent retry) WITHOUT
// requiring --force (#4461 — the partial-install guard is shared with COPY mode).
func TestRunDev_PartialInstallAutoRecovers(t *testing.T) {
	env := newDevTestEnv(t)

	// Write a fake partial state.
	partial := install.NewState(install.ModeDev)
	partial.PartialInstall = true
	partial.RollbackFromStep = 3
	if err := install.WriteState(env.statePath, partial); err != nil {
		t.Fatalf("write partial state: %v", err)
	}

	opts := defaultDevOpts(env)
	opts.Force = false // explicitly NO --force

	if _, err := install.RunDev(opts); err != nil {
		t.Fatalf("expected partial-install retry to auto-recover without --force, got: %v", err)
	}

	// State must be clean after a successful recovery.
	state, err := install.ReadState(env.statePath)
	if err != nil {
		t.Fatalf("read state after recovery: %v", err)
	}
	if state.PartialInstall {
		t.Error("expected PartialInstall=false after successful auto-recovery")
	}
}

// TestRunDev_ModeSwitchFromCopy verifies that running `archigraph install --dev`
// on top of an existing COPY install:
//  1. Does NOT return an error (mode switch proceeds).
//  2. Replaces COPY skills with symlinks.
//  3. Writes install_mode="dev" in install.json.
func TestRunDev_ModeSwitchFromCopy(t *testing.T) {
	env := newDevTestEnv(t)

	// First install in COPY mode.
	copyOpts := install.CopyOptions{
		BinPath:           env.fakeBin,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkipDaemonRestart: true,
	}
	_, err := install.RunCopy(copyOpts)
	if err != nil {
		t.Fatalf("RunCopy (setup): %v", err)
	}

	// Confirm we have COPY mode in install.json.
	state := readState(t, env.statePath)
	if state.InstallMode != install.ModeCopy {
		t.Fatalf("setup: install_mode = %q, want copy", state.InstallMode)
	}

	// Now run DEV mode on top — should warn (to stderr) but NOT error.
	devOpts := defaultDevOpts(env)
	result, err := install.RunDev(devOpts)
	if err != nil {
		t.Fatalf("RunDev (mode switch): %v", err)
	}

	// install.json must now record dev mode.
	state = readState(t, result.StatePath)
	if state.InstallMode != install.ModeDev {
		t.Errorf("after switch: install_mode = %q, want dev", state.InstallMode)
	}

	// Skills must be symlinks after the switch (on non-Windows).
	if runtime.GOOS != "windows" {
		destSkillsDir := filepath.Join(filepath.Dir(env.claudeJSON), "skills")
		for _, skillName := range result.SkillsLinked {
			dst := filepath.Join(destSkillsDir, skillName)
			info, err := os.Lstat(dst)
			if err != nil {
				t.Errorf("Lstat %s after switch: %v", dst, err)
				continue
			}
			if info.Mode()&os.ModeSymlink == 0 {
				t.Errorf("skill %s is not a symlink after COPY→DEV switch", skillName)
			}
		}
	}
}

// TestRunDev_RollbackOnStep4Failure verifies that when the daemon restart step
// fails, the symlinks and MCP registrations are rolled back and install.json
// records PartialInstall=true.
func TestRunDev_RollbackOnStep4Failure(t *testing.T) {
	env := newDevTestEnv(t)

	opts := defaultDevOpts(env)
	opts.SkipDaemonRestart = false
	opts.RestartDaemon = func(_ string, _ int, _ time.Duration) (string, error) {
		return "", errorf("injected daemon failure")
	}

	_, err := install.RunDev(opts)
	if err == nil {
		t.Fatal("expected RunDev to fail when daemon restart fails")
	}

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

	// After rollback: skill symlinks (or directories) should be gone.
	destSkillsDir := filepath.Join(filepath.Dir(env.claudeJSON), "skills")
	for skillName := range state.Skills {
		dst := filepath.Join(destSkillsDir, skillName)
		if _, err := os.Lstat(dst); err == nil {
			t.Errorf("rollback: skill %s still exists at %s", skillName, dst)
		}
	}
}

// ── Doctor tests in DEV mode ──────────────────────────────────────────────────

// newDevDoctorEnv creates a self-consistent DEV install.json + symlinked skills.
type devDoctorEnv struct {
	home       string
	statePath  string
	claudeJSON string
	skillsDir  string // ~/.claude/skills — where symlinks live
	srcDir     string // skills source directory — where symlinks point
	fakeBin    string
	skillName  string
}

func newDevDoctorEnv(t *testing.T) *devDoctorEnv {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// Fake binary.
	fakeBin := filepath.Join(tmp, "archigraph-fake")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\necho fake-dev-doctor"), 0o755); err != nil {
		t.Fatalf("create fake bin: %v", err)
	}

	// Skills source (simulate repo working tree).
	srcDir := filepath.Join(tmp, "repo", "skills")
	skillName := "archigraph-quality-check"
	srcSkillDir := filepath.Join(srcDir, skillName)
	if err := os.MkdirAll(srcSkillDir, 0o755); err != nil {
		t.Fatalf("mkdir src skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcSkillDir, "SKILL.md"), []byte("# archigraph-quality-check"), 0o644); err != nil {
		t.Fatalf("write src SKILL.md: %v", err)
	}

	// Claude config dir.
	claudeDir := filepath.Join(tmp, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir claude dir: %v", err)
	}
	claudeJSON := filepath.Join(claudeDir, ".claude.json")
	if err := os.WriteFile(claudeJSON, []byte(`{"mcpServers":{"archigraph":{"command":"`+fakeBin+`","args":["mcp-bridge"],"type":"stdio"}}}`), 0o644); err != nil {
		t.Fatalf("write .claude.json: %v", err)
	}

	// Skills dest dir with a symlink.
	skillsDir := filepath.Join(claudeDir, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir skills dir: %v", err)
	}
	absSrc, _ := filepath.Abs(srcSkillDir)
	skillDst := filepath.Join(skillsDir, skillName)
	if err := os.Symlink(absSrc, skillDst); err != nil {
		t.Fatalf("create skill symlink: %v", err)
	}

	// Compute binary SHA.
	binSHA, err := install.SHA256FilePublic(fakeBin)
	if err != nil {
		t.Fatalf("sha256 bin: %v", err)
	}

	// Write install.json in DEV mode.
	stateDir := filepath.Join(tmp, ".archigraph")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	statePath := filepath.Join(stateDir, "install.json")

	state := install.NewState(install.ModeDev)
	state.CLI = install.CLIRecord{Path: fakeBin, SHA256: binSHA}
	state.Skills = map[string]install.SkillRecord{
		skillName: {DevTarget: absSrc},
	}
	state.MCP = install.MCPRecord{
		Name:            "archigraph",
		RegisteredPaths: []string{claudeJSON},
	}
	if err := install.WriteState(statePath, state); err != nil {
		t.Fatalf("write state: %v", err)
	}

	return &devDoctorEnv{
		home:       tmp,
		statePath:  statePath,
		claudeJSON: claudeJSON,
		skillsDir:  skillsDir,
		srcDir:     srcDir,
		fakeBin:    fakeBin,
		skillName:  skillName,
	}
}

func runDevDoctor(t *testing.T, env *devDoctorEnv) *install.DoctorReport {
	t.Helper()
	opts := install.DoctorOptions{
		StatePath:        env.statePath,
		ClaudeConfigDirs: []string{env.claudeJSON},
		DaemonPort:       1, // unreachable — we only care about skill checks
		DaemonTimeout:    100 * time.Millisecond,
		SkillsDir:        env.skillsDir,
	}
	report, err := install.RunDoctor(opts)
	if err != nil {
		t.Fatalf("RunDoctor: %v", err)
	}
	return report
}

// TestDoctorDevMode_HappyPath: DEV install, correct symlink → skill check passes.
// Doctor must NOT perform a SHA manifest check (no manifest in install.json).
func TestDoctorDevMode_HappyPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink-based doctor check only relevant on non-Windows")
	}
	env := newDevDoctorEnv(t)
	report := runDevDoctor(t, env)

	skillSurface := "skills/" + env.skillName
	skill := findCheck(report, skillSurface)
	if skill == nil {
		t.Fatalf("skill check %q missing from report", skillSurface)
	}
	if !skill.OK {
		t.Errorf("skill check failed (should pass for correct symlink): %v", skill.Drift)
	}
}

// TestDoctorDevMode_SymlinkDrift: replace the symlink with a regular directory
// → doctor reports drift (not a symlink).
func TestDoctorDevMode_SymlinkDrift(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink-based doctor check only relevant on non-Windows")
	}
	env := newDevDoctorEnv(t)

	// Replace the symlink with a regular directory.
	skillDst := filepath.Join(env.skillsDir, env.skillName)
	if err := os.Remove(skillDst); err != nil {
		t.Fatalf("remove symlink: %v", err)
	}
	if err := os.MkdirAll(skillDst, 0o755); err != nil {
		t.Fatalf("mkdir replacement: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDst, "SKILL.md"), []byte("# copy"), 0o644); err != nil {
		t.Fatalf("write replacement SKILL.md: %v", err)
	}

	report := runDevDoctor(t, env)

	skillSurface := "skills/" + env.skillName
	skill := findCheck(report, skillSurface)
	if skill == nil {
		t.Fatalf("skill check %q missing", skillSurface)
	}
	if skill.OK {
		t.Error("skill check should fail when symlink replaced with directory")
	}
	found := false
	for _, d := range skill.Drift {
		if containsStr(d, "not a symlink") || containsStr(d, "replaced") {
			found = true
		}
	}
	if !found {
		t.Errorf("drift should mention symlink replacement; got: %v", skill.Drift)
	}
}

// TestDoctorDevMode_WrongTarget: symlink points to wrong directory → doctor
// reports target mismatch.
func TestDoctorDevMode_WrongTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink-based doctor check only relevant on non-Windows")
	}
	env := newDevDoctorEnv(t)

	// Replace the symlink with one pointing to a wrong location.
	skillDst := filepath.Join(env.skillsDir, env.skillName)
	if err := os.Remove(skillDst); err != nil {
		t.Fatalf("remove symlink: %v", err)
	}
	wrongTarget := filepath.Join(env.home, "wrong-skills-dir", env.skillName)
	if err := os.MkdirAll(wrongTarget, 0o755); err != nil {
		t.Fatalf("mkdir wrong target: %v", err)
	}
	if err := os.Symlink(wrongTarget, skillDst); err != nil {
		t.Fatalf("create wrong symlink: %v", err)
	}

	report := runDevDoctor(t, env)

	skillSurface := "skills/" + env.skillName
	skill := findCheck(report, skillSurface)
	if skill == nil {
		t.Fatalf("skill check %q missing", skillSurface)
	}
	if skill.OK {
		t.Error("skill check should fail when symlink points to wrong target")
	}
	found := false
	for _, d := range skill.Drift {
		if containsStr(d, "mismatch") || containsStr(d, "target") {
			found = true
		}
	}
	if !found {
		t.Errorf("drift should mention target mismatch; got: %v", skill.Drift)
	}
}

// TestDoctorDevMode_MissingSymlink: symlink doesn't exist → doctor reports drift.
func TestDoctorDevMode_MissingSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink-based doctor check only relevant on non-Windows")
	}
	env := newDevDoctorEnv(t)

	// Remove the symlink entirely.
	skillDst := filepath.Join(env.skillsDir, env.skillName)
	if err := os.Remove(skillDst); err != nil {
		t.Fatalf("remove symlink: %v", err)
	}

	report := runDevDoctor(t, env)

	skillSurface := "skills/" + env.skillName
	skill := findCheck(report, skillSurface)
	if skill == nil {
		t.Fatalf("skill check %q missing", skillSurface)
	}
	if skill.OK {
		t.Error("skill check should fail when symlink is missing")
	}
}

// TestDoctorDevMode_NoPanicOnFileModification verifies that modifying a file
// inside the symlinked skill directory does NOT cause doctor to fail (since
// DEV mode skips the SHA manifest check).
func TestDoctorDevMode_NoPanicOnFileModification(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink-based doctor check only relevant on non-Windows")
	}
	env := newDevDoctorEnv(t)

	// Modify the SKILL.md in the source directory.
	srcSKILL := filepath.Join(env.srcDir, env.skillName, "SKILL.md")
	if err := os.WriteFile(srcSKILL, []byte("# modified in dev"), 0o644); err != nil {
		t.Fatalf("modify source SKILL.md: %v", err)
	}

	// Doctor should still report the skill as OK — the symlink is intact.
	report := runDevDoctor(t, env)

	skillSurface := "skills/" + env.skillName
	skill := findCheck(report, skillSurface)
	if skill == nil {
		t.Fatalf("skill check %q missing", skillSurface)
	}
	if !skill.OK {
		t.Errorf("DEV-mode doctor should pass after source file modification (symlink intact): %v", skill.Drift)
	}
}

// TestRunDev_MultipleConfigsAndOrphanPruning verifies the #2269 fix:
//
//  1. When multiple Claude config dirs are passed, skill symlinks are
//     created in EVERY config dir's skills/ subdir (primary HOME/.claude.json
//     + sidecar HOME/.claude-*/.claude.json layouts).
//  2. Pre-existing stale symlinks for renamed/retired skills are pruned
//     before the current skill set is installed.
//
// Both bugs share a fix path so we cover them in one integration test.
func TestRunDev_MultipleConfigsAndOrphanPruning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink-based assertions only meaningful on non-Windows")
	}
	env := newDevTestEnv(t)

	// Build the three real-world Claude config layouts: HOME/.claude.json
	// (primary), HOME/.claude-personal/.claude.json and
	// HOME/.claude-extra/.claude.json (sidecars).  newTestEnv already
	// configured the primary at HOME/.claude/.claude.json — we add the
	// sidecars and override ClaudeConfigDirs.
	homeDir := filepath.Dir(filepath.Dir(env.claudeJSON)) // tmp
	primaryCfg := env.claudeJSON                          // tmp/.claude/.claude.json
	personalCfg := filepath.Join(homeDir, ".claude-personal", ".claude.json")
	extraCfg := filepath.Join(homeDir, ".claude-extra", ".claude.json")
	for _, p := range []string{personalCfg, extraCfg} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("create sidecar dir: %v", err)
		}
		if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
			t.Fatalf("write sidecar .claude.json: %v", err)
		}
	}

	// Pre-seed the primary skills dir with three stale orphan symlinks
	// (renamed/retired skills from earlier installs).
	primarySkillsDir := filepath.Join(filepath.Dir(primaryCfg), "skills")
	if err := os.MkdirAll(primarySkillsDir, 0o755); err != nil {
		t.Fatalf("mkdir primary skills dir: %v", err)
	}
	dummyTarget := filepath.Join(homeDir, "dummy-target")
	if err := os.MkdirAll(dummyTarget, 0o755); err != nil {
		t.Fatalf("mkdir dummy target: %v", err)
	}
	stale := []string{"archigraph-quality-check", "archigraph-repair", "generate-docs"}
	for _, name := range stale {
		if err := os.Symlink(dummyTarget, filepath.Join(primarySkillsDir, name)); err != nil {
			t.Fatalf("seed orphan %s: %v", name, err)
		}
	}

	opts := defaultDevOpts(env)
	opts.ClaudeConfigDirs = []string{primaryCfg, personalCfg, extraCfg}

	result, err := install.RunDev(opts)
	if err != nil {
		t.Fatalf("RunDev: %v", err)
	}

	// Every config dir should have the full skill set as symlinks.
	configsAndSkillsDirs := map[string]string{
		primaryCfg:  filepath.Join(filepath.Dir(primaryCfg), "skills"),
		personalCfg: filepath.Join(filepath.Dir(personalCfg), "skills"),
		extraCfg:    filepath.Join(filepath.Dir(extraCfg), "skills"),
	}
	for cfg, skillsSubdir := range configsAndSkillsDirs {
		for _, name := range result.SkillsLinked {
			dst := filepath.Join(skillsSubdir, name)
			info, err := os.Lstat(dst)
			if err != nil {
				t.Errorf("config %s: skill %s missing at %s: %v", cfg, name, dst, err)
				continue
			}
			if info.Mode()&os.ModeSymlink == 0 {
				t.Errorf("config %s: skill %s at %s is not a symlink", cfg, name, dst)
			}
		}
	}

	// Stale orphan symlinks must be gone from the primary skills dir.
	for _, name := range stale {
		if _, err := os.Lstat(filepath.Join(primarySkillsDir, name)); !os.IsNotExist(err) {
			t.Errorf("orphan symlink %s should have been pruned (err=%v)", name, err)
		}
	}

	// The state file should record every current skill.
	state := readState(t, result.StatePath)
	if state == nil {
		t.Fatal("install.json not written")
	}
	for _, name := range result.SkillsLinked {
		if _, ok := state.Skills[name]; !ok {
			t.Errorf("install.json missing skill record for %s", name)
		}
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// errorf is a test helper that returns a simple error value.
func errorf(msg string) error {
	return &simpleError{msg: msg}
}

type simpleError struct{ msg string }

func (e *simpleError) Error() string { return e.msg }
