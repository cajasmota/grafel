// symlink_perm_6246_test.go pins the #6246 defects in this package's two
// writers.
//
// writeSettings is a verbatim copy of the function #6240 fixed in
// internal/install/mcpreg, down to the name: write `path + ".tmp"` at 0644,
// rename it over the destination. It therefore carries both of that issue's
// defects — the rename replaces the LINK INODE, and the destination inherits
// the temp file's mode, so a 0600 settings.json comes back 0644.
//
// writeNudgeScript has the SYMLINK defect only. Its 0755 is correct for a hook
// script and must NOT become "preserve whatever mode the destination has": the
// script is a file grafel owns and regenerates, and inheriting a non-executable
// mode from a stale destination would silently stop the hook from ever firing.
// A fixed mode is the right answer for a grafel-owned artefact; mode
// PRESERVATION is the right answer only for a file whose mode its owner chose.
//
// Correction to the issue text: neither destination is under ~/.claude. Both are
// project-relative — <repoRoot>/.claude/settings.json and
// <repoRoot>/.claude/grafel-grep-nudge.sh — which is why the fresh-file mode
// here stays 0644 rather than adopting mcpreg's 0600. See writeSettings.
//
// The fixtures seed 0600 and a read-only 0444. A 0644 seed cannot observe
// widening, which is why the pre-existing tests in this package were blind to
// it. The 0444 rows are the only ones with signal on Windows, where
// FILE_ATTRIBUTE_READONLY is the one mode the platform can represent
// (testsupport.AssertPerm).
package agenthooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/testsupport"
)

// requireSymlinks6246 skips when the platform cannot create a symlink at all
// (Windows without developer mode or SeCreateSymbolicLink). A capability PROBE
// rather than a runtime.GOOS check, so a Windows runner that CAN make symlinks
// still exercises these.
func requireSymlinks6246(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "target"), filepath.Join(dir, "link")); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
}

func seedFile6246(t *testing.T, path string, body []byte, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, body, perm); err != nil {
		t.Fatalf("seed %s: %v", path, err)
	}
	// os.WriteFile's perm is umask-masked and does not apply to an existing
	// file, so state the mode explicitly.
	if err := os.Chmod(path, perm); err != nil {
		t.Fatalf("chmod seed: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o666) })
}

func settingsDoc6246() map[string]any {
	return map[string]any{"model": "opus", "keep": "me"}
}

func assertContains6246(t *testing.T, path, want string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(raw), want) {
		t.Fatalf("%s does not contain %q: %s", path, want, raw)
	}
}

// ── writeSettings ────────────────────────────────────────────────────────────

// TestWriteSettings_WritesThroughSymlink is the dotfiles case: .claude/settings.json
// symlinked at a shared copy. The write must land on the target and leave the
// link a link.
func TestWriteSettings_WritesThroughSymlink(t *testing.T) {
	requireSymlinks6246(t)

	target := filepath.Join(t.TempDir(), "settings.json")
	link := filepath.Join(t.TempDir(), "settings.json")
	seedFile6246(t, target, []byte(`{"model":"opus"}`), 0o600)
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if err := writeSettings(link, settingsDoc6246()); err != nil {
		t.Fatalf("writeSettings: %v", err)
	}

	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat link: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link was replaced by a regular file — the shared settings are now detached")
	}
	assertContains6246(t, target, `"keep"`)
}

// TestWriteSettings_PreservesDestinationMode is the widening defect. 0644 is
// deliberately absent: it is the mode the buggy writer produces, so it cannot
// witness anything.
func TestWriteSettings_PreservesDestinationMode(t *testing.T) {
	for _, perm := range []os.FileMode{0o600, 0o444} {
		t.Run(perm.String(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.json")
			seedFile6246(t, path, []byte(`{"model":"opus"}`), perm)

			if err := writeSettings(path, settingsDoc6246()); err != nil {
				t.Fatalf("writeSettings: %v", err)
			}
			testsupport.AssertPerm(t, path, perm)
			assertContains6246(t, path, `"keep"`)
		})
	}
}

// TestWriteSettings_SymlinkPreservesTargetMode composes the two: the mode that
// must survive is the TARGET's, not the link's (a symlink is 0777 on Linux).
//
// The content assertion is load-bearing, not incidental. Without it the test is
// VACUOUS against the buggy writer: that writer never touches the target, so the
// target trivially still carries its seeded 0600. Being composed, this dies for
// either mutant; the independence of the two fixes is established by
// WritesThroughSymlink and PreservesDestinationMode, which do not.
func TestWriteSettings_SymlinkPreservesTargetMode(t *testing.T) {
	requireSymlinks6246(t)

	target := filepath.Join(t.TempDir(), "settings.json")
	link := filepath.Join(t.TempDir(), "settings.json")
	seedFile6246(t, target, []byte(`{"model":"opus"}`), 0o600)
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if err := writeSettings(link, settingsDoc6246()); err != nil {
		t.Fatalf("writeSettings: %v", err)
	}
	assertContains6246(t, target, `"keep"`)
	testsupport.AssertPerm(t, target, 0o600)
}

// TestWriteSettings_CreatesProjectReadableFile pins the deliberate NON-change:
// a settings.json this package invents stays 0644, unlike the 0600 mcpreg chose
// in #6240. The destination is <repoRoot>/.claude/settings.json — a
// project-scoped file that is routinely committed and read by every collaborator
// checkout — and it carries hook commands, not credentials. Narrowing it to 0600
// would be a behaviour change with no security argument behind it.
func TestWriteSettings_CreatesProjectReadableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := writeSettings(path, settingsDoc6246()); err != nil {
		t.Fatalf("writeSettings: %v", err)
	}
	testsupport.AssertPerm(t, path, 0o644)
}

// TestWriteSettings_SymlinkCycleFailsLoudly: an unresolvable chain is an error
// and leaves the user's link intact. Answering with the last hop reached is not
// a safe fallback — that path is itself a symlink, so the writer renames over it
// and flattens a link while reporting success.
func TestWriteSettings_SymlinkCycleFailsLoudly(t *testing.T) {
	requireSymlinks6246(t)

	dir := t.TempDir()
	a := filepath.Join(dir, "a.json")
	b := filepath.Join(dir, "b.json")
	if err := os.Symlink(b, a); err != nil {
		t.Fatalf("symlink a: %v", err)
	}
	if err := os.Symlink(a, b); err != nil {
		t.Fatalf("symlink b: %v", err)
	}

	if err := writeSettings(a, settingsDoc6246()); err == nil {
		t.Fatalf("a symlink cycle reported success")
	}
	fi, err := os.Lstat(a)
	if err != nil {
		t.Fatalf("lstat a: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the cycle flattened the user's link into a regular file")
	}
}

// ── writeNudgeScript ─────────────────────────────────────────────────────────

// TestWriteNudgeScript_WritesThroughSymlink is the symlink half, which the
// script shares with settings.json.
func TestWriteNudgeScript_WritesThroughSymlink(t *testing.T) {
	requireSymlinks6246(t)

	target := filepath.Join(t.TempDir(), "grafel-grep-nudge.sh")
	link := filepath.Join(t.TempDir(), "grafel-grep-nudge.sh")
	seedFile6246(t, target, []byte("#!/bin/sh\nexit 0\n"), 0o755)
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if err := writeNudgeScript(link); err != nil {
		t.Fatalf("writeNudgeScript: %v", err)
	}
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat link: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link was replaced by a regular file")
	}
	assertContains6246(t, target, "#!/bin/sh")
}

// TestWriteNudgeScript_IsAlwaysExecutable is the half that must NOT change. The
// script is grafel-owned and grafel-generated, so its mode is grafel's to state:
// 0755 unconditionally, including over a destination left non-executable by an
// earlier version or a stray chmod. This is the row that fails if someone
// "consistently" applies mode preservation to every site in this issue — the
// hook would then be installed, referenced from settings.json, and never able to
// run.
func TestWriteNudgeScript_IsAlwaysExecutable(t *testing.T) {
	for _, seed := range []os.FileMode{0o600, 0o444} {
		t.Run("over "+seed.String(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "grafel-grep-nudge.sh")
			seedFile6246(t, path, []byte("stale\n"), seed)

			if err := writeNudgeScript(path); err != nil {
				t.Fatalf("writeNudgeScript: %v", err)
			}
			testsupport.AssertPerm(t, path, 0o755)
		})
	}

	t.Run("fresh", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "grafel-grep-nudge.sh")
		if err := writeNudgeScript(path); err != nil {
			t.Fatalf("writeNudgeScript: %v", err)
		}
		testsupport.AssertPerm(t, path, 0o755)
	})
}

// TestWriteNudgeScript_SymlinkCycleFailsLoudly mirrors the settings case.
func TestWriteNudgeScript_SymlinkCycleFailsLoudly(t *testing.T) {
	requireSymlinks6246(t)

	dir := t.TempDir()
	a := filepath.Join(dir, "a.sh")
	b := filepath.Join(dir, "b.sh")
	if err := os.Symlink(b, a); err != nil {
		t.Fatalf("symlink a: %v", err)
	}
	if err := os.Symlink(a, b); err != nil {
		t.Fatalf("symlink b: %v", err)
	}

	if err := writeNudgeScript(a); err == nil {
		t.Fatalf("a symlink cycle reported success")
	}
	fi, err := os.Lstat(a)
	if err != nil {
		t.Fatalf("lstat a: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the cycle flattened the user's link into a regular file")
	}
}

// ── end to end ───────────────────────────────────────────────────────────────

// TestInstallClaudeCode_KeepsSymlinkedSettings exercises the public entry point
// rather than the two writers directly: the damage #6246 describes happens on a
// plain `grafel install`, not on a call nobody makes.
func TestInstallClaudeCode_KeepsSymlinkedSettings(t *testing.T) {
	requireSymlinks6246(t)

	repo := t.TempDir()
	shared := t.TempDir()
	target := filepath.Join(shared, "settings.json")
	seedFile6246(t, target, []byte(`{"model":"opus"}`), 0o600)

	if err := os.MkdirAll(filepath.Join(repo, ".claude"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	link := filepath.Join(repo, SettingsRelPath)
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if _, err := installClaudeCode(repo); err != nil {
		t.Fatalf("installClaudeCode: %v", err)
	}

	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat link: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("grafel install detached the symlinked settings.json")
	}
	testsupport.AssertPerm(t, target, 0o600)
	assertContains6246(t, target, Marker)
}
