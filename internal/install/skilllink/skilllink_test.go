package skilllink

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// toSlash is a test-local helper that normalises a path to forward slashes so
// that TestClaudeSkillsDirForConfig can use hard-coded Unix-style want strings
// on all platforms (including Windows, where filepath.Join returns backslashes).
func toSlash(p string) string { return filepath.ToSlash(p) }

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
			name:            "empty skills source dir falls through to defaults",
			skillsSourceDir: "",
			envPath:         "",
			expectFound:     false, // won't find anything in a temp dir
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

// TestClaudeSkillsDirForConfig table-drives the path-derivation helper.
//
// The Claude Code conventions covered here:
//
//   - HOME/.claude.json (primary, file directly in HOME)
//     → HOME/.claude/skills
//   - HOME/.claude-X/.claude.json (sidecar profile, file inside a dir
//     whose basename already starts with ".claude")
//     → HOME/.claude-X/skills
//   - Hypothetical flat sidecar HOME/.claude-X.json
//     → HOME/.claude-X/skills
//   - Non-".json" inputs return "".
func TestClaudeSkillsDirForConfig(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "primary config in HOME",
			in:   "/home/u/.claude.json",
			want: "/home/u/.claude/skills",
		},
		{
			name: "sidecar config inside .claude-personal dir",
			in:   "/home/u/.claude-personal/.claude.json",
			want: "/home/u/.claude-personal/skills",
		},
		{
			name: "sidecar config inside .claude-extra dir",
			in:   "/home/u/.claude-extra/.claude.json",
			want: "/home/u/.claude-extra/skills",
		},
		{
			name: "config in a non-Claude parent dir",
			in:   "/abs/path/.claude.json",
			want: "/abs/path/.claude/skills",
		},
		{
			name: "hypothetical flat sidecar (~/.claude-personal.json)",
			in:   "/home/u/.claude-personal.json",
			want: "/home/u/.claude-personal/skills",
		},
		{
			name: "no .json suffix → empty string",
			in:   "/home/u/.claude",
			want: "",
		},
		{
			name: "empty string → empty string",
			in:   "",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClaudeSkillsDirForConfig(tt.in)
			// Normalise to forward slashes before comparing so the test passes
			// on Windows (where filepath.Join returns backslash-separated paths)
			// while still exercising the correct derivation logic on all platforms.
			if toSlash(got) != tt.want {
				t.Errorf("ClaudeSkillsDirForConfig(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestInstallSkillsInClaudeConfigs_MultipleConfigs verifies that the install
// loop places symlinks in EVERY detected config dir's skills/ subdir, using
// the correct derivation (primary vs sidecar layout).
func TestInstallSkillsInClaudeConfigs_MultipleConfigs(t *testing.T) {
	dir := t.TempDir()

	// Source skills.
	skillsDir := filepath.Join(dir, "src-skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, skillName := range SkillNames {
		skillPath := filepath.Join(skillsDir, skillName)
		if err := os.MkdirAll(skillPath, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skillPath, "skill.yaml"), []byte("name: "+skillName), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Three configs covering the primary + two sidecar profiles.
	primaryCfg := filepath.Join(dir, ".claude.json")
	personalCfg := filepath.Join(dir, ".claude-personal", ".claude.json")
	extraCfg := filepath.Join(dir, ".claude-extra", ".claude.json")
	for _, p := range []string{filepath.Dir(personalCfg), filepath.Dir(extraCfg)} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	out := &bytes.Buffer{}
	installed := InstallSkillsInClaudeConfigs(out, "", skillsDir, []string{primaryCfg, personalCfg, extraCfg})

	if len(installed) != 3 {
		t.Fatalf("expected 3 installed dirs, got %d: %v\noutput:\n%s", len(installed), installed, out.String())
	}

	wantDirs := map[string]string{
		"primary":  filepath.Join(dir, ".claude", "skills"),
		"personal": filepath.Join(dir, ".claude-personal", "skills"),
		"extra":    filepath.Join(dir, ".claude-extra", "skills"),
	}
	for label, want := range wantDirs {
		info, err := os.Stat(want)
		if err != nil {
			t.Fatalf("%s: skills dir %s missing: %v", label, want, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s: %s is not a directory", label, want)
		}
		for _, skillName := range SkillNames {
			dst := filepath.Join(want, skillName)
			linkInfo, err := os.Lstat(dst)
			if err != nil {
				t.Errorf("%s: symlink not created for %s at %s: %v", label, skillName, dst, err)
				continue
			}
			if linkInfo.Mode()&os.ModeSymlink == 0 {
				t.Errorf("%s: %s is not a symlink", label, dst)
				continue
			}
			target, err := os.Readlink(dst)
			if err != nil {
				t.Errorf("%s: readlink %s: %v", label, dst, err)
				continue
			}
			expected := filepath.Join(skillsDir, skillName)
			if target != expected {
				t.Errorf("%s: symlink target mismatch for %s: got %q want %q", label, skillName, target, expected)
			}
		}
	}
}

// TestInstallSkillsInClaudeConfigs_PrunesOrphans verifies that stale symlinks
// for renamed/retired skills are removed before the current skill set is
// installed.  Regular directories (manual installs) are left intact.
func TestInstallSkillsInClaudeConfigs_PrunesOrphans(t *testing.T) {
	dir := t.TempDir()

	// Source skills (current set).
	skillsDir := filepath.Join(dir, "src-skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, skillName := range SkillNames {
		skillPath := filepath.Join(skillsDir, skillName)
		if err := os.MkdirAll(skillPath, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Pre-seed the destination skills dir with three stale orphan symlinks
	// (renamed/retired skills) plus one manual install (regular directory).
	primaryCfg := filepath.Join(dir, ".claude.json")
	skillsSubdir := ClaudeSkillsDirForConfig(primaryCfg)
	if err := os.MkdirAll(skillsSubdir, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := []string{"archigraph-quality-check", "archigraph-repair", "generate-docs"}
	dummyTarget := filepath.Join(dir, "dummy-target")
	if err := os.MkdirAll(dummyTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range stale {
		if err := os.Symlink(dummyTarget, filepath.Join(skillsSubdir, name)); err != nil {
			t.Fatalf("seed orphan symlink %s: %v", name, err)
		}
	}
	manualSkill := filepath.Join(skillsSubdir, "my-custom-skill")
	if err := os.MkdirAll(manualSkill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manualSkill, "marker"), []byte("manual"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Run install.
	out := &bytes.Buffer{}
	installed := InstallSkillsInClaudeConfigs(out, "", skillsDir, []string{primaryCfg})
	if len(installed) != 1 {
		t.Fatalf("expected 1 installed dir, got %d: %v", len(installed), installed)
	}

	// Orphan symlinks must be gone.
	for _, name := range stale {
		_, err := os.Lstat(filepath.Join(skillsSubdir, name))
		if !os.IsNotExist(err) {
			t.Errorf("orphan symlink %s should have been pruned (err=%v)", name, err)
		}
	}

	// Manual install (regular dir) must be untouched.
	if _, err := os.Stat(filepath.Join(manualSkill, "marker")); err != nil {
		t.Errorf("manual install was clobbered: %v", err)
	}

	// All current SkillNames must be present as symlinks.
	for _, name := range SkillNames {
		info, err := os.Lstat(filepath.Join(skillsSubdir, name))
		if err != nil {
			t.Errorf("current skill %s missing after install: %v", name, err)
			continue
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("current skill %s is not a symlink", name)
		}
	}

	// The output should mention each pruned orphan so the user knows what
	// happened (one line per orphan).
	outStr := out.String()
	for _, name := range stale {
		if !strings.Contains(outStr, name) {
			t.Errorf("output should mention pruned orphan %s; got:\n%s", name, outStr)
		}
	}
}

func TestInstallSkillsIdempotent(t *testing.T) {
	dir := t.TempDir()

	// Create source skills directory.
	skillsDir := filepath.Join(dir, "src-skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, skillName := range SkillNames {
		skillPath := filepath.Join(skillsDir, skillName)
		if err := os.MkdirAll(skillPath, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Primary config (file directly in tmp/).
	primaryCfg := filepath.Join(dir, ".claude.json")
	skillsSubdir := ClaudeSkillsDirForConfig(primaryCfg)

	// First install.
	out1 := &bytes.Buffer{}
	installed1 := InstallSkillsInClaudeConfigs(out1, "", skillsDir, []string{primaryCfg})
	if len(installed1) != 1 {
		t.Fatalf("first install: expected 1 installed dir, got %d", len(installed1))
	}

	// Verify a symlink was created.
	skillPath1 := filepath.Join(skillsSubdir, SkillNames[0])
	if _, err := os.Lstat(skillPath1); err != nil {
		t.Fatal(err)
	}

	// Re-run install (should be idempotent).
	out2 := &bytes.Buffer{}
	installed2 := InstallSkillsInClaudeConfigs(out2, "", skillsDir, []string{primaryCfg})
	if len(installed2) != 1 {
		t.Fatalf("second install: expected 1 installed dir, got %d", len(installed2))
	}

	// Symlinks still point at the source.
	target, err := os.Readlink(skillPath1)
	if err != nil {
		t.Fatal(err)
	}
	expectedTarget := filepath.Join(skillsDir, SkillNames[0])
	if target != expectedTarget {
		t.Errorf("symlink target mismatch after re-install: expected %q, got %q", expectedTarget, target)
	}

	if !stringContains(out1.String(), "Skills linked in:") {
		t.Errorf("first install didn't report success: %s", out1.String())
	}
	if !stringContains(out2.String(), "Skills linked in:") {
		t.Errorf("second install didn't report success: %s", out2.String())
	}
}

func TestInstallSkillsSkipsManualInstall(t *testing.T) {
	dir := t.TempDir()

	// Source skills.
	skillsDir := filepath.Join(dir, "src-skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, skillName := range SkillNames {
		skillPath := filepath.Join(skillsDir, skillName)
		if err := os.MkdirAll(skillPath, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	primaryCfg := filepath.Join(dir, ".claude.json")
	skillsSubdir := ClaudeSkillsDirForConfig(primaryCfg)
	if err := os.MkdirAll(skillsSubdir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Manually-installed skill at the first canonical name (regular dir).
	manualSkillPath := filepath.Join(skillsSubdir, SkillNames[0])
	if err := os.MkdirAll(manualSkillPath, 0o755); err != nil {
		t.Fatal(err)
	}

	out := &bytes.Buffer{}
	_ = InstallSkillsInClaudeConfigs(out, "", skillsDir, []string{primaryCfg})

	outStr := out.String()
	if !stringContains(outStr, "Skills linked in:") {
		t.Errorf("should report partial success: %s", outStr)
	}
	if !stringContains(outStr, "exists as directory") && !stringContains(outStr, "manual install") {
		t.Errorf("should warn about manual install: %s", outStr)
	}

	// Manual skill must NOT have been replaced with a symlink.
	info, err := os.Lstat(filepath.Join(skillsSubdir, SkillNames[0]))
	if err != nil {
		t.Fatalf("manual skill was deleted: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("manual skill was replaced with a symlink")
	}

	// Other skills should be symlinks.
	for _, skillName := range SkillNames[1:] {
		linkInfo, err := os.Lstat(filepath.Join(skillsSubdir, skillName))
		if err != nil {
			t.Fatalf("symlink not created for %s: %v", skillName, err)
		}
		if linkInfo.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s is not a symlink", skillName)
		}
	}
}

func TestRemoveSkillsFromClaudeConfigs(t *testing.T) {
	dir := t.TempDir()

	// Source skills.
	skillsDir := filepath.Join(dir, "src-skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, skillName := range SkillNames {
		skillPath := filepath.Join(skillsDir, skillName)
		if err := os.MkdirAll(skillPath, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	primaryCfg := filepath.Join(dir, ".claude.json")

	out := &bytes.Buffer{}
	InstallSkillsInClaudeConfigs(out, "", skillsDir, []string{primaryCfg})

	out2 := &bytes.Buffer{}
	removed := RemoveSkillsFromClaudeConfigs(out2, []string{primaryCfg})

	if len(removed) != 1 {
		t.Fatalf("expected 1 removed dir, got %d", len(removed))
	}

	skillsSubdir := ClaudeSkillsDirForConfig(primaryCfg)
	for _, skillName := range SkillNames {
		_, err := os.Lstat(filepath.Join(skillsSubdir, skillName))
		if !os.IsNotExist(err) {
			t.Fatalf("symlink not removed for %s", skillName)
		}
	}

	if !stringContains(out2.String(), "Skills removed from:") {
		t.Errorf("should report removal: %s", out2.String())
	}
}

func TestRemoveSkillsIdempotent(t *testing.T) {
	dir := t.TempDir()

	primaryCfg := filepath.Join(dir, ".claude.json")

	// Remove should succeed silently (idempotent) even if no skills exist.
	out := &bytes.Buffer{}
	removed := RemoveSkillsFromClaudeConfigs(out, []string{primaryCfg})

	if len(removed) != 0 {
		t.Fatalf("expected 0 removed dirs, got %d", len(removed))
	}

	out2 := &bytes.Buffer{}
	removed2 := RemoveSkillsFromClaudeConfigs(out2, []string{primaryCfg})
	if len(removed2) != 0 {
		t.Fatalf("expected 0 removed dirs on second call, got %d", len(removed2))
	}
}

func TestValidateSkillSymlinks(t *testing.T) {
	dir := t.TempDir()

	skillsDir := filepath.Join(dir, "src-skills")
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
			if err := os.MkdirAll(filepath.Join(skillsSubdir3, skillName), 0o755); err != nil {
				t.Fatal(err)
			}
		} else {
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
	return strings.Contains(s, substr)
}
