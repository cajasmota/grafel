package install_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/install"
)

// #6168 follow-up (S1): the pristine sidecar snapshot must never outlive the
// install run that took it.
//
// backupOnce (mcpreg.go:397-402) REFUSES to overwrite an existing sidecar, so
// whatever snapshot is on disk at the start of a run is the one a rollback in
// that run will write back. If a run ends without clearing its sidecar — which
// every step-4 failure does, and a saturated #6167 daemon makes that the COMMON
// ending — the next run inherits a snapshot that is arbitrarily stale. A step-3
// mid-loop failure in that later run then restores weeks-old content over the
// user's live config.
//
// This is the invariant the original "so the next install snapshots fresh"
// comment was protecting. It was load-bearing, not cargo.

// failingRestart is the injected step-4 failure used to end a run without
// committing.
func failingRestart(_ string, _ int, _ time.Duration) (string, error) {
	return "", fmt.Errorf("injected daemon restart failure (#6168 S1)")
}

// malformedTarget writes an unparseable config so RegisterPath fails on it,
// aborting step 3 part-way through the target list.
func malformedTarget(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "broken", ".claude.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte("{ not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

// TestStaleSidecar_AbsentSentinelDoesNotDeleteTheUsersConfig is the
// catastrophic case.
//
// Run 1 is a first-ever install: no .claude.json exists, so the sidecar is the
// GRAFEL_BACKUP_ORIGINAL_ABSENT sentinel. Step 4 fails, so the run never
// commits. If the sentinel is left on disk, run 2's step-3 rollback takes
// RestoreSnapshot's sentinel branch — os.Remove(path) — and deletes the config
// file the user has since filled with real state: every foreign MCP server and
// all unrelated Claude Code keys.
func TestStaleSidecar_AbsentSentinelDoesNotDeleteTheUsersConfig(t *testing.T) {
	env := newTestEnv(t)

	// Run 1: first-ever install — the config must NOT exist beforehand.
	if err := os.Remove(env.claudeJSON); err != nil {
		t.Fatalf("remove seed config: %v", err)
	}

	run1 := install.CopyOptions{
		BinPath:           env.fakeBin,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkipDaemonRestart: false,
		RestartDaemon:     failingRestart,
	}
	if _, err := install.RunCopy(run1); err == nil {
		t.Fatal("run 1: expected a step-4 failure")
	}

	// Between runs the user keeps using Claude Code: the config now holds real
	// state that exists nowhere else.
	live := map[string]any{
		"mcpServers": map[string]any{
			"playwright": map[string]any{"command": "/bin/playwright", "type": "stdio"},
		},
		"projects":       map[string]any{"/home/u/work": map[string]any{"allowedTools": []any{}}},
		"oauthAccount":   map[string]any{"emailAddress": "u@example.com"},
		"numStartups":    41,
		"userIDOverride": "keep-me",
	}
	b, err := json.MarshalIndent(live, "", "  ")
	if err != nil {
		t.Fatalf("marshal live config: %v", err)
	}
	if err := os.WriteFile(env.claudeJSON, b, 0o644); err != nil {
		t.Fatalf("write live config: %v", err)
	}

	// Run 2 aborts inside step 3, triggering the mid-loop restore.
	run2 := run1
	run2.Force = true // run 1 left PartialInstall=true
	run2.SkipDaemonRestart = true
	run2.RestartDaemon = nil
	run2.ClaudeConfigDirs = []string{env.claudeJSON, malformedTarget(t, filepath.Dir(env.claudeJSON))}
	if _, err := install.RunCopy(run2); err == nil {
		t.Fatal("run 2: expected a step-3 failure")
	}

	// The user's config must still be there. Deleting it is unrecoverable.
	raw, err := os.ReadFile(env.claudeJSON)
	if err != nil {
		t.Fatalf("the user's .claude.json was DELETED by a stale absent-sentinel snapshot: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("parse config after run 2: %v (content: %s)", err, raw)
	}
	if got["userIDOverride"] != "keep-me" {
		t.Errorf("unrelated Claude Code state lost: userIDOverride = %v, want \"keep-me\" (content: %s)", got["userIDOverride"], raw)
	}
	servers, _ := got["mcpServers"].(map[string]any)
	if _, ok := servers["playwright"]; !ok {
		t.Errorf("foreign MCP server 'playwright' lost (content: %s)", raw)
	}
}

// TestStaleSidecar_DoesNotResurrectAnOldSnapshotOverLiveContent is the normal
// (non-sentinel) case: a server the user added BETWEEN runs is silently
// destroyed when run 2 restores run 1's snapshot.
func TestStaleSidecar_DoesNotResurrectAnOldSnapshotOverLiveContent(t *testing.T) {
	env := newTestEnv(t)

	// Pre-existing config with one foreign server.
	seed := `{
  "mcpServers": {
    "alpha": {
      "command": "/bin/alpha",
      "type": "stdio"
    }
  }
}`
	if err := os.WriteFile(env.claudeJSON, []byte(seed), 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	run1 := install.CopyOptions{
		BinPath:           env.fakeBin,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkipDaemonRestart: false,
		RestartDaemon:     failingRestart,
	}
	if _, err := install.RunCopy(run1); err == nil {
		t.Fatal("run 1: expected a step-4 failure")
	}

	// Between runs the user adds a SECOND MCP server by hand.
	cur := mcpServersOf(t, env.claudeJSON)
	if cur == nil {
		cur = map[string]any{}
	}
	cur["beta"] = map[string]any{"command": "/bin/beta", "type": "stdio"}
	doc := map[string]any{"mcpServers": cur}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(env.claudeJSON, b, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	run2 := run1
	run2.Force = true
	run2.SkipDaemonRestart = true
	run2.RestartDaemon = nil
	run2.ClaudeConfigDirs = []string{env.claudeJSON, malformedTarget(t, filepath.Dir(env.claudeJSON))}
	if _, err := install.RunCopy(run2); err == nil {
		t.Fatal("run 2: expected a step-3 failure")
	}

	after := mcpServersOf(t, env.claudeJSON)
	if after == nil {
		t.Fatal("mcpServers key gone after run 2")
	}
	if _, ok := after["beta"]; !ok {
		t.Errorf("foreign server 'beta', added between runs, was destroyed by run 1's stale snapshot (servers: %v)", after)
	}
	if _, ok := after["alpha"]; !ok {
		t.Errorf("foreign server 'alpha' lost (servers: %v)", after)
	}
}

// TestStaleSidecar_NoSidecarSurvivesAStep4Failure states the invariant
// directly: no run may leave a sidecar behind for the next run to inherit.
func TestStaleSidecar_NoSidecarSurvivesAStep4Failure(t *testing.T) {
	env := newTestEnv(t)
	sidecar := env.claudeJSON + ".grafel.bak"

	opts := install.CopyOptions{
		BinPath:           env.fakeBin,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkipDaemonRestart: false,
		RestartDaemon:     failingRestart,
	}
	if _, err := install.RunCopy(opts); err == nil {
		t.Fatal("expected a step-4 failure")
	}

	// The failed run may leave one (it is still the in-flight snapshot), but
	// the NEXT run must not inherit it: starting step 3 clears stale sidecars.
	next := opts
	next.Force = true
	next.SkipDaemonRestart = true
	next.RestartDaemon = nil
	if _, err := install.RunCopy(next); err != nil {
		t.Fatalf("second RunCopy: %v", err)
	}
	if _, err := os.Stat(sidecar); err == nil {
		t.Errorf("sidecar %s survived a committed install", sidecar)
	}
}
