package install_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/install"
	"github.com/cajasmota/grafel/internal/install/skilllink"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// TestRunCopy_EmbeddedSkillsLandAndUninstallRemoves is the install-acceptance
// test for #5503: on a binary-only install (a released tarball with NO skills/
// directory next to the binary — the macOS symptom), `grafel install` must
// still copy the bundled skills into the Claude skills dir, and `grafel
// uninstall` must remove them again.
//
// The whole test runs against an isolated HOME (t.Setenv + GuardRealHome) so it
// never touches the developer's real ~/.claude or ~/.grafel.
func TestRunCopy_EmbeddedSkillsLandAndUninstallRemoves(t *testing.T) {
	tmp := t.TempDir()

	// Place the fake binary in its OWN isolated subtree with NO skills/ dir in
	// any ancestor, so on-disk discovery (sibling/one-up/ancestor) all miss and
	// the install is forced down the embedded-skills fallback — exactly the
	// released-tarball scenario from #5503.
	binDir := filepath.Join(tmp, "opt", "grafel", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("create bin dir: %v", err)
	}
	fakeBin := filepath.Join(binDir, "grafel")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\necho fake"), 0o755); err != nil {
		t.Fatalf("write fake bin: %v", err)
	}

	// A fresh, empty Claude config so skills + MCP have a destination.
	claudeDir := filepath.Join(tmp, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("create claude dir: %v", err)
	}
	claudeJSON := filepath.Join(claudeDir, ".claude.json")
	if err := os.WriteFile(claudeJSON, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write .claude.json: %v", err)
	}

	stateDir := filepath.Join(tmp, ".grafel")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("create state dir: %v", err)
	}
	statePath := filepath.Join(stateDir, "install.json")

	// Isolate every home-dir-derived path to tmp, then fail closed if the
	// redirect somehow resolved to the real user home.
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "cfg"))
	t.Setenv("GRAFEL_DAEMON_ROOT", stateDir)
	t.Setenv("GRAFEL_HOME", stateDir)
	// Ensure no env override points discovery at a real on-disk skills tree.
	t.Setenv("GRAFEL_SKILLS_DIR", "")
	testsupport.GuardRealHome(t)

	// Sanity: discovery must resolve to the materialised embedded cache, NOT an
	// on-disk repo tree — proving the fallback is what's being exercised.
	src := skilllink.DiscoverSkillsDir(fakeBin, "")
	if src == "" {
		t.Fatal("DiscoverSkillsDir returned empty even with embedded fallback")
	}
	wantCache := filepath.Join(tmp, ".grafel", "skills-cache")
	if src != wantCache {
		t.Fatalf("expected discovery to use embedded cache %s, got %s", wantCache, src)
	}

	opts := install.CopyOptions{
		BinPath:           fakeBin,
		SkillsSourceDir:   "", // force discovery → embedded fallback
		ClaudeConfigDirs:  []string{claudeJSON},
		StatePath:         statePath,
		WorkingDir:        tmp,
		SkipDaemonRestart: true,
		NoHooks:           true,
	}

	result, err := install.RunCopy(opts)
	if err != nil {
		t.Fatalf("RunCopy: %v", err)
	}
	if len(result.SkillsInstalled) == 0 {
		t.Fatal("install reported zero skills installed; embedded skills did not land (#5503)")
	}

	// Assert every canonical skill physically exists under the resolved Claude
	// skills dir with its SKILL.md.
	skillsDest := skilllink.ClaudeSkillsDirForConfig(claudeJSON)
	for _, name := range skilllink.SkillNames {
		skillMd := filepath.Join(skillsDest, name, "SKILL.md")
		if _, err := os.Stat(skillMd); err != nil {
			t.Errorf("after install, expected skill file %s to exist: %v", skillMd, err)
		}
	}

	// ── uninstall must remove every skill it installed ───────────────────────
	if _, err := install.RunUninstall(install.UninstallOptions{
		StatePath:      statePath,
		SkipDaemonStop: true,
	}); err != nil {
		t.Fatalf("RunUninstall: %v", err)
	}
	for _, name := range skilllink.SkillNames {
		skillDir := filepath.Join(skillsDest, name)
		if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
			t.Errorf("after uninstall, expected skill %s to be gone, but stat err=%v", skillDir, err)
		}
	}
}
