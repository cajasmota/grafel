package install_test

// doctor_test.go — integration tests for RunDoctor and RunQuickDoctor (#2211).
//
// Tests cover every row from the acceptance table:
//   - Happy path: clean install state → ok=true
//   - CLI SHA tamper: rename binary → CLI check fails
//   - Skill tamper: modify a skill file → skills check reports drift
//   - Daemon offline: no daemon running → daemon check fails cleanly (no panic)
//   - MCP drift: remove archigraph entry → MCP check fails
//   - Gitignore missing entry → gitignore check warns
//   - Stale staging dirs → staging check reports info
//   - Quick mode: tampered state → exits silently with warning (doesn't block)
//   - JSON output: schema_version=1, stable schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/install"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// doctorTestEnv sets up a self-consistent install.json + skills on disk.
type doctorTestEnv struct {
	home       string
	statePath  string
	claudeJSON string
	skillsDir  string // ~/.claude/skills
	fakeBin    string
	gitRepo    string

	// skillName is the single skill installed in this env.
	skillName string
}

func newDoctorTestEnv(t *testing.T) *doctorTestEnv {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// Fake binary.
	fakeBin := filepath.Join(tmp, "archigraph-fake")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\necho fake-doctor-test"), 0o755); err != nil {
		t.Fatalf("create fake bin: %v", err)
	}

	// Claude config dir + .claude.json.
	claudeDir := filepath.Join(tmp, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir claude dir: %v", err)
	}
	claudeJSON := filepath.Join(claudeDir, ".claude.json")

	// Skills dir.
	skillsDir := filepath.Join(claudeDir, "skills")
	skillName := "archigraph-quality-check"
	skillDir := filepath.Join(skillsDir, skillName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# archigraph-quality-check"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	// Compute SHA for the binary.
	binSHA, err := install.SHA256FilePublic(fakeBin)
	if err != nil {
		t.Fatalf("sha256 bin: %v", err)
	}

	// Build skill manifest.
	manifest, err := install.BuildManifestPublic(skillDir)
	if err != nil {
		t.Fatalf("build skill manifest: %v", err)
	}

	// Git repo with .gitignore.
	gitRepo := filepath.Join(tmp, "myrepo")
	if err := os.MkdirAll(filepath.Join(gitRepo, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gitRepo, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatalf("write .git/HEAD: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gitRepo, ".gitignore"), []byte("/.archigraph/\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}

	// MCP registration in .claude.json.
	mcpDoc := map[string]any{
		"mcpServers": map[string]any{
			"archigraph": map[string]any{
				"command": fakeBin,
				"args":    []string{"mcp-bridge"},
				"type":    "stdio",
			},
		},
	}
	mcpBytes, _ := json.MarshalIndent(mcpDoc, "", "  ")
	if err := os.WriteFile(claudeJSON, mcpBytes, 0o644); err != nil {
		t.Fatalf("write .claude.json: %v", err)
	}

	// Write install.json.
	state := install.NewState(install.ModeCopy)
	state.CLI = install.CLIRecord{Path: fakeBin, SHA256: binSHA}
	state.Skills = map[string]install.SkillRecord{
		skillName: {Files: manifest},
	}
	state.MCP = install.MCPRecord{
		Name:            "archigraph",
		RegisteredPaths: []string{claudeJSON},
	}
	state.DaemonVersion = "v1.0.0-test"
	state.Gitignore = install.GitignoreRecord{Repos: []string{gitRepo}}

	stateDir := filepath.Join(tmp, ".archigraph")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	statePath := filepath.Join(stateDir, "install.json")
	if err := install.WriteState(statePath, state); err != nil {
		t.Fatalf("write state: %v", err)
	}

	return &doctorTestEnv{
		home:       tmp,
		statePath:  statePath,
		claudeJSON: claudeJSON,
		skillsDir:  skillsDir,
		fakeBin:    fakeBin,
		gitRepo:    gitRepo,
		skillName:  skillName,
	}
}

func runDoctor(t *testing.T, env *doctorTestEnv, port int) *install.DoctorReport {
	t.Helper()
	opts := install.DoctorOptions{
		StatePath:        env.statePath,
		ClaudeConfigDirs: []string{env.claudeJSON},
		DaemonPort:       port,
		DaemonTimeout:    200 * time.Millisecond,
		SkillsDir:        env.skillsDir,
	}
	report, err := install.RunDoctor(opts)
	if err != nil {
		t.Fatalf("RunDoctor error: %v", err)
	}
	return report
}

// findCheck returns the CheckResult for the given surface, or nil.
func findCheck(report *install.DoctorReport, surface string) *install.CheckResult {
	for i := range report.Checks {
		if report.Checks[i].Surface == surface {
			return &report.Checks[i]
		}
	}
	return nil
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// TestDoctorHappyPath: clean install state, daemon down, all file checks pass.
// Daemon will be unreachable (we use port 0 or a closed port) — the daemon
// check is Critical so OK will be false, but CLI + skills + MCP should pass.
// We verify those surfaces individually.
func TestDoctorHappyPath_NoLiveDaemon(t *testing.T) {
	env := newDoctorTestEnv(t)
	// Use port 1 — guaranteed unreachable without root.
	report := runDoctor(t, env, 1)

	// CLI check must pass.
	cli := findCheck(report, "cli")
	if cli == nil {
		t.Fatal("cli check missing from report")
	}
	if !cli.OK {
		t.Errorf("cli check failed: %v", cli.Drift)
	}

	// Skills check must pass.
	skillSurface := "skills/" + env.skillName
	skill := findCheck(report, skillSurface)
	if skill == nil {
		t.Fatalf("skill check %q missing from report", skillSurface)
	}
	if !skill.OK {
		t.Errorf("skill check failed: %v", skill.Drift)
	}

	// MCP check must pass.
	mcp := findCheck(report, "mcp")
	if mcp == nil {
		t.Fatal("mcp check missing from report")
	}
	if !mcp.OK {
		t.Errorf("mcp check failed: %v", mcp.Drift)
	}

	// Gitignore check must pass.
	gitSurface := "gitignore/" + filepath.Base(env.gitRepo)
	git := findCheck(report, gitSurface)
	if git == nil {
		t.Fatalf("gitignore check %q missing from report", gitSurface)
	}
	if !git.OK {
		t.Errorf("gitignore check failed: %v", git.Drift)
	}

	// Daemon check must be present (and will be not-OK since no daemon).
	daemon := findCheck(report, "daemon")
	if daemon == nil {
		t.Fatal("daemon check missing from report")
	}
}

// TestDoctorCLITamper: modify the binary → CLI check reports SHA mismatch.
func TestDoctorCLITamper(t *testing.T) {
	env := newDoctorTestEnv(t)

	// Overwrite the binary with different content.
	if err := os.WriteFile(env.fakeBin, []byte("#!/bin/sh\necho tampered"), 0o755); err != nil {
		t.Fatalf("tamper binary: %v", err)
	}

	report := runDoctor(t, env, 1)

	cli := findCheck(report, "cli")
	if cli == nil {
		t.Fatal("cli check missing")
	}
	if cli.OK {
		t.Error("cli check should fail after binary tamper")
	}
	if len(cli.Drift) == 0 {
		t.Error("cli check should report drift")
	}
	// #4463: a SHA-only drift is a Warning, not Critical — the daemon is still
	// usable, so it must not read as a broken install. (The overall report.OK in
	// this test env is independently false because no daemon is running on the
	// probe port; the CLI severity is what matters here.)
	if cli.Severity != install.SeverityWarning {
		t.Errorf("cli severity = %q, want warning", cli.Severity)
	}
}

// TestDoctorMissingCLIRecordCritical: an install.json with no CLI record (a
// genuine partial/corrupt install) must still be Critical (#4463 keeps Critical
// for the missing-record case; only SHA drift was downgraded).
func TestDoctorMissingCLIRecordCritical(t *testing.T) {
	env := newDoctorTestEnv(t)

	// Blank out the CLI record in install.json.
	state, err := install.ReadState(env.statePath)
	if err != nil || state == nil {
		t.Fatalf("read state: %v", err)
	}
	state.CLI = install.CLIRecord{}
	if err := install.WriteState(env.statePath, state); err != nil {
		t.Fatalf("write state: %v", err)
	}

	report := runDoctor(t, env, 1)
	cli := findCheck(report, "cli")
	if cli == nil {
		t.Fatal("cli check missing")
	}
	if cli.Severity != install.SeverityCritical {
		t.Errorf("cli severity = %q, want critical for missing CLI record", cli.Severity)
	}
	if report.OK {
		t.Error("report.OK should be false when CLI record is missing")
	}
}

// TestDoctorSkillTamper: modify a skill file → skills check reports drift.
func TestDoctorSkillTamper(t *testing.T) {
	env := newDoctorTestEnv(t)

	// Overwrite SKILL.md in the installed skill directory.
	skillMD := filepath.Join(env.skillsDir, env.skillName, "SKILL.md")
	if err := os.WriteFile(skillMD, []byte("# tampered content"), 0o644); err != nil {
		t.Fatalf("tamper SKILL.md: %v", err)
	}

	report := runDoctor(t, env, 1)

	skillSurface := "skills/" + env.skillName
	skill := findCheck(report, skillSurface)
	if skill == nil {
		t.Fatalf("skill check %q missing", skillSurface)
	}
	if skill.OK {
		t.Error("skill check should fail after tamper")
	}
	// Must mention the drifted file.
	found := false
	for _, d := range skill.Drift {
		if containsStr(d, "SKILL.md") {
			found = true
		}
	}
	if !found {
		t.Errorf("skill drift should mention SKILL.md; got: %v", skill.Drift)
	}

	// Overall: Critical drift → not OK.
	if report.OK {
		t.Error("report.OK should be false after skill tamper")
	}
}

// TestDoctorSkillMissingFile: remove a skill file → doctor reports it missing.
func TestDoctorSkillMissingFile(t *testing.T) {
	env := newDoctorTestEnv(t)

	// Remove SKILL.md from the installed skill.
	skillMD := filepath.Join(env.skillsDir, env.skillName, "SKILL.md")
	if err := os.Remove(skillMD); err != nil {
		t.Fatalf("remove SKILL.md: %v", err)
	}

	report := runDoctor(t, env, 1)

	skillSurface := "skills/" + env.skillName
	skill := findCheck(report, skillSurface)
	if skill == nil {
		t.Fatalf("skill check %q missing", skillSurface)
	}
	if skill.OK {
		t.Error("skill check should fail when file removed")
	}
	found := false
	for _, d := range skill.Drift {
		if containsStr(d, "SKILL.md") || containsStr(d, "missing") {
			found = true
		}
	}
	if !found {
		t.Errorf("skill drift should mention SKILL.md missing; got: %v", skill.Drift)
	}
}

// TestDoctorDaemonOffline: daemon not running → check reports unreachable cleanly (no panic).
func TestDoctorDaemonOffline(t *testing.T) {
	env := newDoctorTestEnv(t)

	// Port 1 is non-routable without root — will fail immediately.
	report := runDoctor(t, env, 1)

	daemon := findCheck(report, "daemon")
	if daemon == nil {
		t.Fatal("daemon check missing")
	}
	if daemon.OK {
		t.Error("daemon check should fail when daemon is unreachable")
	}
	if daemon.Severity != install.SeverityCritical {
		t.Errorf("daemon severity = %q, want critical", daemon.Severity)
	}
	if len(daemon.Drift) == 0 {
		t.Error("daemon check should report drift message")
	}

	// report.OK can be false (daemon critical) — that's expected.
	// The key invariant is no panic.
}

// TestDoctorMCPDrift: remove the archigraph entry from .claude.json → MCP check fails.
func TestDoctorMCPDrift(t *testing.T) {
	env := newDoctorTestEnv(t)

	// Overwrite .claude.json without the archigraph MCP entry.
	noMCP := map[string]any{"mcpServers": map[string]any{}}
	b, _ := json.MarshalIndent(noMCP, "", "  ")
	if err := os.WriteFile(env.claudeJSON, b, 0o644); err != nil {
		t.Fatalf("overwrite .claude.json: %v", err)
	}

	report := runDoctor(t, env, 1)

	mcp := findCheck(report, "mcp")
	if mcp == nil {
		t.Fatal("mcp check missing")
	}
	if mcp.OK {
		t.Error("mcp check should fail when entry removed")
	}
}

// TestDoctorGitignoreMissing: remove /.archigraph/ from .gitignore → warning.
func TestDoctorGitignoreMissing(t *testing.T) {
	env := newDoctorTestEnv(t)

	// Overwrite .gitignore without the archigraph entry.
	if err := os.WriteFile(filepath.Join(env.gitRepo, ".gitignore"), []byte("node_modules/\n"), 0o644); err != nil {
		t.Fatalf("overwrite .gitignore: %v", err)
	}

	report := runDoctor(t, env, 1)

	gitSurface := "gitignore/" + filepath.Base(env.gitRepo)
	git := findCheck(report, gitSurface)
	if git == nil {
		t.Fatalf("gitignore check %q missing", gitSurface)
	}
	if git.OK {
		t.Error("gitignore check should fail when entry missing")
	}
	if git.Severity != install.SeverityWarning {
		t.Errorf("gitignore severity = %q, want warning", git.Severity)
	}
}

// TestDoctorStaleStagingDirs: create old staging dirs → staging check reports info.
func TestDoctorStaleStagingDirs(t *testing.T) {
	env := newDoctorTestEnv(t)

	// Create a staging dir that is older than 7 days.
	stagingDir := filepath.Join(env.home, ".archigraph", "staging", "run-old")
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		t.Fatalf("mkdir staging: %v", err)
	}
	// Set mtime to 8 days ago.
	oldTime := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(stagingDir, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	report := runDoctor(t, env, 1)

	staging := findCheck(report, "staging")
	if staging == nil {
		t.Fatal("staging check missing — expected it to be present when stale dirs exist")
	}
	if staging.OK {
		t.Error("staging check should be not-OK when stale dirs exist")
	}
	if staging.Severity != install.SeverityInfo {
		t.Errorf("staging severity = %q, want info", staging.Severity)
	}
	if len(staging.Drift) == 0 {
		t.Error("staging drift should list stale dirs")
	}
}

// TestDoctorMissingInstallJSON: no install.json → single critical check returned.
func TestDoctorMissingInstallJSON(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	opts := install.DoctorOptions{
		StatePath:     filepath.Join(tmp, ".archigraph", "install.json"),
		DaemonPort:    1,
		DaemonTimeout: 100 * time.Millisecond,
	}
	report, err := install.RunDoctor(opts)
	if err != nil {
		t.Fatalf("RunDoctor: %v", err)
	}
	if report.OK {
		t.Error("report.OK should be false when install.json missing")
	}
	if len(report.Checks) == 0 {
		t.Error("should have at least one check")
	}
	// The first check should mention install.json.
	if report.Checks[0].Surface != "install.json" {
		t.Errorf("first check surface = %q, want install.json", report.Checks[0].Surface)
	}
}

// TestDoctorJSONOutput: --json output is valid JSON with schema_version=1.
func TestDoctorJSONOutput(t *testing.T) {
	env := newDoctorTestEnv(t)
	report := runDoctor(t, env, 1)

	// Marshal to JSON.
	b, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	// Unmarshal back.
	var decoded install.DoctorReport
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if decoded.SchemaVersion != install.DoctorSchemaVersion {
		t.Errorf("schema_version = %d, want %d", decoded.SchemaVersion, install.DoctorSchemaVersion)
	}
	if len(decoded.Checks) == 0 {
		t.Error("checks array is empty in JSON output")
	}
	for _, c := range decoded.Checks {
		if c.Surface == "" {
			t.Error("check has empty surface in JSON output")
		}
	}
}

// TestDoctorJSONSchema: verify required fields are present and stable.
func TestDoctorJSONSchema(t *testing.T) {
	env := newDoctorTestEnv(t)
	report := runDoctor(t, env, 1)

	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, field := range []string{"schema_version", "ok", "checks"} {
		if _, ok := raw[field]; !ok {
			t.Errorf("JSON output missing field %q", field)
		}
	}
}

// TestDoctorQuickMode_Tampered: quick-doctor with a tampered binary prints
// a one-line warning and returns nil (does not block).
func TestDoctorQuickMode_Tampered(t *testing.T) {
	env := newDoctorTestEnv(t)

	// Tamper the binary.
	if err := os.WriteFile(env.fakeBin, []byte("#!/bin/sh\necho tampered"), 0o755); err != nil {
		t.Fatalf("tamper binary: %v", err)
	}

	var buf bytes.Buffer
	opts := install.QuickOptions{
		StatePath:     env.statePath,
		DaemonPort:    1,
		DaemonTimeout: 100 * time.Millisecond,
		Out:           &buf,
	}

	err := install.RunQuickDoctor(opts)
	if err != nil {
		t.Errorf("RunQuickDoctor must not return error (quick mode never blocks): %v", err)
	}

	// Must have printed something (SHA mismatch warning).
	if buf.Len() == 0 {
		t.Error("quick-doctor should print warning when binary is tampered")
	}

	output := buf.String()
	if len(output) > 200 {
		t.Errorf("quick-doctor output too long (%d bytes) — should be a single line", len(output))
	}
	// Must be a single line (no extra newlines).
	lines := countNonEmptyLines(output)
	if lines > 1 {
		t.Errorf("quick-doctor printed %d lines, want 1: %q", lines, output)
	}
	// #4463: the wording must not read as "broken". The old "reinstall
	// recommended" phrasing alarmed users on every status/rebuild.
	if strings.Contains(output, "reinstall recommended") {
		t.Errorf("quick-doctor SHA-drift wording should be non-alarming, got: %q", output)
	}
	if !strings.Contains(output, "still usable") {
		t.Errorf("quick-doctor SHA-drift message should reassure the daemon is usable, got: %q", output)
	}
}

// TestDoctorQuickMode_Clean: quick-doctor with matching state prints nothing
// and returns nil (daemon unreachable is noted but still returns nil).
func TestDoctorQuickMode_NoInstall(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	var buf bytes.Buffer
	opts := install.QuickOptions{
		StatePath:     filepath.Join(tmp, ".archigraph", "install.json"),
		DaemonPort:    1,
		DaemonTimeout: 100 * time.Millisecond,
		Out:           &buf,
	}

	err := install.RunQuickDoctor(opts)
	if err != nil {
		t.Errorf("RunQuickDoctor with no install.json must not error: %v", err)
	}
	// No install.json → silent exit.
	if buf.Len() != 0 {
		t.Errorf("quick-doctor should be silent when no install.json: %q", buf.String())
	}
}

// TestDoctorRenderReport_Coloured: RenderReport writes ANSI-coloured output.
func TestDoctorRenderReport_Coloured(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	report := &install.DoctorReport{
		SchemaVersion: 1,
		OK:            false,
		Checks: []install.CheckResult{
			{Surface: "cli", OK: true},
			{Surface: "daemon", OK: false, Severity: install.SeverityCritical, Drift: []string{"unreachable"}},
		},
		Remediation: "Run: archigraph install",
	}

	var buf bytes.Buffer
	install.RenderReport(&buf, report)
	output := buf.String()

	if !containsStr(output, "cli") {
		t.Error("output missing 'cli'")
	}
	if !containsStr(output, "daemon") {
		t.Error("output missing 'daemon'")
	}
	if !containsStr(output, "unreachable") {
		t.Error("output missing drift text")
	}
	if !containsStr(output, "archigraph install") {
		t.Error("output missing remediation hint")
	}
}

// TestDoctorRenderReport_NoColor: RenderReport respects NO_COLOR.
func TestDoctorRenderReport_NoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	report := &install.DoctorReport{
		SchemaVersion: 1,
		OK:            true,
		Checks: []install.CheckResult{
			{Surface: "cli", OK: true},
		},
	}

	var buf bytes.Buffer
	install.RenderReport(&buf, report)
	output := buf.String()

	// Should not contain ANSI escape codes.
	if containsStr(output, "\033[") {
		t.Error("output should not contain ANSI codes when NO_COLOR=1")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}

func countNonEmptyLines(s string) int {
	count := 0
	for _, line := range splitByNewline(s) {
		if len(line) > 0 {
			count++
		}
	}
	return count
}

func splitByNewline(s string) []string {
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

// TestDoctorQuickMode_Timing: RunQuickDoctor (no live daemon) should complete in <200ms.
// This is a soft timing test — it only fails if dramatically over budget.
func TestDoctorQuickMode_Timing(t *testing.T) {
	env := newDoctorTestEnv(t)

	var buf bytes.Buffer
	opts := install.QuickOptions{
		StatePath:     env.statePath,
		DaemonPort:    1, // unreachable
		DaemonTimeout: 100 * time.Millisecond,
		Out:           &buf,
	}

	start := time.Now()
	if err := install.RunQuickDoctor(opts); err != nil {
		t.Errorf("RunQuickDoctor: %v", err)
	}
	elapsed := time.Since(start)

	// Budget: 200ms (100ms daemon timeout + SHA + overhead).
	// We use 200ms because CI machines may be slow.
	if elapsed > 200*time.Millisecond {
		t.Errorf("RunQuickDoctor took %s, want <200ms", elapsed)
	}
	_ = fmt.Sprintf("quick-doctor elapsed: %s", elapsed)
}
