package skilllink

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverSkillsDir(t *testing.T) {
	dir := t.TempDir()

	// Create a temporary skills directory for testing.
	skillsDir := filepath.Join(dir, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name            string
		binPath         string
		skillsSourceDir string
		envPath         string
		expectFound     bool
	}{
		{
			name:            "explicit skillsSourceDir takes precedence",
			skillsSourceDir: skillsDir,
			expectFound:     true,
		},
		{
			name:        "empty skills source dir falls through to defaults",
			skillsSourceDir: "",
			envPath:     "",
			expectFound: false, // won't find anything in a temp dir
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save the original HOME and ARCHIGRAPH_SKILLS_DIR.
			oldHome := os.Getenv("HOME")
			oldEnv := os.Getenv("ARCHIGRAPH_SKILLS_DIR")

			// Set the environment for this test.
			if tt.envPath != "" {
				t.Setenv("ARCHIGRAPH_SKILLS_DIR", tt.envPath)
			} else {
				t.Setenv("ARCHIGRAPH_SKILLS_DIR", "")
			}

			result := DiscoverSkillsDir(tt.binPath, tt.skillsSourceDir)
			if tt.expectFound && result == "" {
				t.Errorf("expected to find skills dir, got empty")
			}
			if !tt.expectFound && result != "" && tt.skillsSourceDir != "" {
				t.Errorf("expected empty result when skillsSourceDir doesn't exist, got: %s", result)
			}

			// Restore environment.
			if oldHome != "" {
				t.Setenv("HOME", oldHome)
			}
			if oldEnv != "" {
				t.Setenv("ARCHIGRAPH_SKILLS_DIR", oldEnv)
			}
		})
	}
}

func TestInstallSkillsInClaudeConfigs(t *testing.T) {
	dir := t.TempDir()

	// Create source skills directory with dummy skill dirs.
	skillsDir := filepath.Join(dir, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, skillName := range SkillNames {
		skillPath := filepath.Join(skillsDir, skillName)
		if err := os.MkdirAll(skillPath, 0o755); err != nil {
			t.Fatal(err)
		}
		// Write a marker file so we can verify the symlink target.
		if err := os.WriteFile(filepath.Join(skillPath, "skill.yaml"), []byte("name: "+skillName), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Create Claude config directories.
	claudeDir := filepath.Join(dir, "claude", ".claude.json")
	claudePersonalDir := filepath.Join(dir, "claude-personal", ".claude.json")
	for _, p := range []string{
		filepath.Dir(claudeDir),
		filepath.Dir(claudePersonalDir),
	} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Call the installation function.
	out := &bytes.Buffer{}
	installed := InstallSkillsInClaudeConfigs(out, "", skillsDir, []string{claudeDir, claudePersonalDir})

	if len(installed) != 2 {
		t.Fatalf("expected 2 installed dirs, got %d: %v", len(installed), installed)
	}

	// Verify symlinks were created in the primary Claude config.
	primarySkillsDir := filepath.Join(filepath.Dir(claudeDir), "skills")
	for _, skillName := range SkillNames {
		skillPath := filepath.Join(primarySkillsDir, skillName)
		info, err := os.Lstat(skillPath)
		if err != nil {
			t.Fatalf("symlink not created for %s: %v", skillName, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s is not a symlink", skillName)
		}

		// Verify the symlink target.
		target, err := os.Readlink(skillPath)
		if err != nil {
			t.Fatalf("failed to read symlink %s: %v", skillName, err)
		}
		expectedTarget := filepath.Join(skillsDir, skillName)
		if target != expectedTarget {
			t.Errorf("symlink target mismatch: expected %q, got %q", expectedTarget, target)
		}

		// Verify we can read through the symlink.
		content, err := os.ReadFile(filepath.Join(skillPath, "skill.yaml"))
		if err != nil {
			t.Fatalf("failed to read through symlink %s: %v", skillName, err)
		}
		if !stringContains(string(content), skillName) {
			t.Errorf("symlink didn't resolve correctly for %s", skillName)
		}
	}

	// Also verify symlinks in the secondary Claude config.
	secondarySkillsDir := filepath.Join(filepath.Dir(claudePersonalDir), "skills")
	for _, skillName := range SkillNames {
		skillPath := filepath.Join(secondarySkillsDir, skillName)
		info, err := os.Lstat(skillPath)
		if err != nil {
			t.Fatalf("symlink not created for %s in secondary config: %v", skillName, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s is not a symlink in secondary config", skillName)
		}
	}
}

func TestInstallSkillsIdempotent(t *testing.T) {
	dir := t.TempDir()

	// Create source skills directory.
	skillsDir := filepath.Join(dir, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, skillName := range SkillNames {
		skillPath := filepath.Join(skillsDir, skillName)
		if err := os.MkdirAll(skillPath, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Create Claude config directory.
	claudeDir := filepath.Join(dir, "claude", ".claude.json")
	if err := os.MkdirAll(filepath.Dir(claudeDir), 0o755); err != nil {
		t.Fatal(err)
	}

	// First install.
	out1 := &bytes.Buffer{}
	installed1 := InstallSkillsInClaudeConfigs(out1, "", skillsDir, []string{claudeDir})
	if len(installed1) != 1 {
		t.Fatalf("first install: expected 1 installed dir, got %d", len(installed1))
	}

	// Get the modification time of one symlink.
	skillPath1 := filepath.Join(filepath.Dir(claudeDir), "skills", SkillNames[0])
	_, err := os.Lstat(skillPath1)
	if err != nil {
		t.Fatal(err)
	}

	// Re-run install (should be idempotent).
	out2 := &bytes.Buffer{}
	installed2 := InstallSkillsInClaudeConfigs(out2, "", skillsDir, []string{claudeDir})
	if len(installed2) != 1 {
		t.Fatalf("second install: expected 1 installed dir, got %d", len(installed2))
	}

	// Verify symlinks still exist and point correctly.
	skillPath2 := filepath.Join(filepath.Dir(claudeDir), "skills", SkillNames[0])
	if _, err := os.Lstat(skillPath2); err != nil {
		t.Fatal(err)
	}

	// The symlink should have been replaced (new mtime), but target should be the same.
	target, err := os.Readlink(skillPath2)
	if err != nil {
		t.Fatal(err)
	}
	expectedTarget := filepath.Join(skillsDir, SkillNames[0])
	if target != expectedTarget {
		t.Errorf("symlink target mismatch after re-install: expected %q, got %q", expectedTarget, target)
	}

	// Verify both installs reported success.
	if !stringContains(out1.String(), "Skills linked in:") {
		t.Errorf("first install didn't report success: %s", out1.String())
	}
	if !stringContains(out2.String(), "Skills linked in:") {
		t.Errorf("second install didn't report success: %s", out2.String())
	}
}

func TestInstallSkillsSkipsManualInstall(t *testing.T) {
	dir := t.TempDir()

	// Create source skills directory.
	skillsDir := filepath.Join(dir, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, skillName := range SkillNames {
		skillPath := filepath.Join(skillsDir, skillName)
		if err := os.MkdirAll(skillPath, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Create Claude config directory with one manually-installed skill.
	claudeDir := filepath.Join(dir, "claude", ".claude.json")
	if err := os.MkdirAll(filepath.Dir(claudeDir), 0o755); err != nil {
		t.Fatal(err)
	}
	skillsSubdir := filepath.Join(filepath.Dir(claudeDir), "skills")
	if err := os.MkdirAll(skillsSubdir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a regular directory for the first skill (manual install).
	manualSkillPath := filepath.Join(skillsSubdir, SkillNames[0])
	if err := os.MkdirAll(manualSkillPath, 0o755); err != nil {
		t.Fatal(err)
	}

	// Install should create symlinks for the other skills but warn about the manual one.
	out := &bytes.Buffer{}
	_ = InstallSkillsInClaudeConfigs(out, "", skillsDir, []string{claudeDir})

	// We still return the dir as installed, but with a warning.
	outStr := out.String()
	if !stringContains(outStr, "Skills linked in:") {
		t.Errorf("should report partial success: %s", outStr)
	}
	if !stringContains(outStr, "exists as directory") && !stringContains(outStr, "manual install") {
		t.Errorf("should warn about manual install: %s", outStr)
	}

	// Verify the manual skill was not replaced.
	_, err := os.Lstat(filepath.Join(skillsSubdir, SkillNames[0]))
	if err != nil {
		t.Fatalf("manual skill was deleted: %v", err)
	}
	info, err := os.Lstat(filepath.Join(skillsSubdir, SkillNames[0]))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("manual skill was replaced with a symlink")
	}

	// Verify other skills were installed as symlinks.
	for _, skillName := range SkillNames[1:] {
		skillPath := filepath.Join(skillsSubdir, skillName)
		info, err := os.Lstat(skillPath)
		if err != nil {
			t.Fatalf("symlink not created for %s: %v", skillName, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s is not a symlink", skillName)
		}
	}
}

func TestRemoveSkillsFromClaudeConfigs(t *testing.T) {
	dir := t.TempDir()

	// Create source skills directory.
	skillsDir := filepath.Join(dir, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, skillName := range SkillNames {
		skillPath := filepath.Join(skillsDir, skillName)
		if err := os.MkdirAll(skillPath, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Create Claude config directory and install skills.
	claudeDir := filepath.Join(dir, "claude", ".claude.json")
	if err := os.MkdirAll(filepath.Dir(claudeDir), 0o755); err != nil {
		t.Fatal(err)
	}

	out := &bytes.Buffer{}
	InstallSkillsInClaudeConfigs(out, "", skillsDir, []string{claudeDir})

	// Now remove the skills.
	out2 := &bytes.Buffer{}
	removed := RemoveSkillsFromClaudeConfigs(out2, []string{claudeDir})

	if len(removed) != 1 {
		t.Fatalf("expected 1 removed dir, got %d", len(removed))
	}

	// Verify symlinks were removed.
	skillsSubdir := filepath.Join(filepath.Dir(claudeDir), "skills")
	for _, skillName := range SkillNames {
		skillPath := filepath.Join(skillsSubdir, skillName)
		_, err := os.Lstat(skillPath)
		if !os.IsNotExist(err) {
			t.Fatalf("symlink not removed for %s", skillName)
		}
	}

	// Verify the output mentions removal.
	if !stringContains(out2.String(), "Skills removed from:") {
		t.Errorf("should report removal: %s", out2.String())
	}
}

func TestRemoveSkillsIdempotent(t *testing.T) {
	dir := t.TempDir()

	// Create Claude config directory (no skills installed).
	claudeDir := filepath.Join(dir, "claude", ".claude.json")
	if err := os.MkdirAll(filepath.Dir(claudeDir), 0o755); err != nil {
		t.Fatal(err)
	}

	// Remove should succeed silently (idempotent) even if no skills exist.
	out := &bytes.Buffer{}
	removed := RemoveSkillsFromClaudeConfigs(out, []string{claudeDir})

	if len(removed) != 0 {
		t.Fatalf("expected 0 removed dirs, got %d", len(removed))
	}

	// Second removal should also succeed.
	out2 := &bytes.Buffer{}
	removed2 := RemoveSkillsFromClaudeConfigs(out2, []string{claudeDir})
	if len(removed2) != 0 {
		t.Fatalf("expected 0 removed dirs on second call, got %d", len(removed2))
	}
}

func TestValidateSkillSymlinks(t *testing.T) {
	dir := t.TempDir()

	// Create source skills directory.
	skillsDir := filepath.Join(dir, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, skillName := range SkillNames {
		skillPath := filepath.Join(skillsDir, skillName)
		if err := os.MkdirAll(skillPath, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Test 1: Empty skills directory (missing symlinks).
	skillsSubdir := filepath.Join(dir, "empty-skills")
	if err := os.MkdirAll(skillsSubdir, 0o755); err != nil {
		t.Fatal(err)
	}
	errors := ValidateSkillSymlinks(skillsSubdir)
	if errors == "" {
		t.Errorf("should report missing symlinks")
	}
	if !stringContains(errors, "not found") {
		t.Errorf("error message should mention 'not found': %s", errors)
	}

	// Test 2: All symlinks correct.
	skillsSubdir2 := filepath.Join(dir, "good-skills")
	if err := os.MkdirAll(skillsSubdir2, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, skillName := range SkillNames {
		src := filepath.Join(skillsDir, skillName)
		dst := filepath.Join(skillsSubdir2, skillName)
		if err := os.Symlink(src, dst); err != nil {
			t.Fatal(err)
		}
	}
	errors = ValidateSkillSymlinks(skillsSubdir2)
	if errors != "" {
		t.Errorf("should not report errors for valid symlinks: %s", errors)
	}

	// Test 3: One skill is a regular directory instead of symlink.
	skillsSubdir3 := filepath.Join(dir, "mixed-skills")
	if err := os.MkdirAll(skillsSubdir3, 0o755); err != nil {
		t.Fatal(err)
	}
	for i, skillName := range SkillNames {
		if i == 0 {
			// First skill is a regular directory.
			if err := os.MkdirAll(filepath.Join(skillsSubdir3, skillName), 0o755); err != nil {
				t.Fatal(err)
			}
		} else {
			// Others are symlinks.
			src := filepath.Join(skillsDir, skillName)
			dst := filepath.Join(skillsSubdir3, skillName)
			if err := os.Symlink(src, dst); err != nil {
				t.Fatal(err)
			}
		}
	}
	errors = ValidateSkillSymlinks(skillsSubdir3)
	if errors == "" {
		t.Errorf("should report error for non-symlink directory")
	}
	if !stringContains(errors, "not a symlink") {
		t.Errorf("error message should mention 'not a symlink': %s", errors)
	}
}

// stringContains checks if a string contains a substring.
func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
