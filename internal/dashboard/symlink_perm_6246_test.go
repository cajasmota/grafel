// symlink_perm_6246_test.go pins the #6246 defects in the dashboard's MCP
// setup writer.
//
// writeMCPConfig is a fully independent second implementation of the write
// internal/install/mcpreg performs, aimed at the SAME files (~/.claude.json,
// ~/.cursor/mcp.json, the Windsurf config). #6240 fixed mcpreg; this path
// bypassed that fix entirely, so a user could have the symlink-safe writer and
// the destructive one both live against one config.
//
// Both defects come out of the same three lines — write `path + ".tmp"`, rename
// it over the destination:
//
//  1. The rename replaces the LINK INODE, so a config symlinked into a dotfiles
//     repo silently detaches on the first install from the wizard.
//  2. The destination inherits the TEMP file's 0644, so a 0600 config holding an
//     OAuth token comes back world-readable with no diff to notice.
//
// Every pre-existing fixture in this package seeds 0644 and never reads the mode
// back — a mode against which widening is unobservable. These seed 0600 and a
// read-only 0444. The 0444 rows are the only ones with any signal on Windows,
// where FILE_ATTRIBUTE_READONLY is the one mode the platform can represent
// (testsupport.AssertPerm).
package dashboard

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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

// seedMCPConfig6246 writes a host config at mode perm and returns the exact
// bytes written, so a caller can assert what the backup captured rather than
// only what mode it captured it at.
func seedMCPConfig6246(t *testing.T, path string, perm os.FileMode) []byte {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := []byte(`{"mcpServers":{"other":{"command":"/opt/other/bin/other"}}}`)
	if err := os.WriteFile(path, body, perm); err != nil {
		t.Fatalf("seed %s: %v", path, err)
	}
	// os.WriteFile's perm is umask-masked and does not apply to an existing
	// file, so state the mode explicitly.
	if err := os.Chmod(path, perm); err != nil {
		t.Fatalf("chmod seed: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o666) })
	return body
}

func grafelCfg6246() map[string]any {
	return map[string]any{
		"mcpServers": map[string]any{
			"grafel": map[string]any{"command": "grafel", "args": []any{"mcp"}},
		},
	}
}

func assertGrafelWritten6246(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%s is not JSON: %v (%s)", path, err, raw)
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if _, ok := servers["grafel"]; !ok {
		t.Fatalf("no grafel entry in %s: %s", path, raw)
	}
}

// TestWriteMCPConfig_WritesThroughSymlink is the dotfiles case: ~/.cursor/mcp.json
// is a link into a dotfiles repo. The write must land on the repo copy and leave
// the link a link.
func TestWriteMCPConfig_WritesThroughSymlink(t *testing.T) {
	requireSymlinks6246(t)

	home := t.TempDir()
	dotfiles := t.TempDir()
	target := filepath.Join(dotfiles, "mcp.json")
	link := filepath.Join(home, "mcp.json")

	_ = seedMCPConfig6246(t, target, 0o600)
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if err := writeMCPConfig(link, grafelCfg6246()); err != nil {
		t.Fatalf("writeMCPConfig: %v", err)
	}

	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat link: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link was replaced by a regular file — the dotfiles repo is now detached")
	}
	assertGrafelWritten6246(t, target)
}

// TestWriteMCPConfig_DanglingSymlinkCreatesTheTarget covers the link whose
// target does not exist yet: the write must create the TARGET, not flatten the
// link.
func TestWriteMCPConfig_DanglingSymlinkCreatesTheTarget(t *testing.T) {
	requireSymlinks6246(t)

	dotfiles := t.TempDir()
	home := t.TempDir()
	target := filepath.Join(dotfiles, "mcp.json")
	link := filepath.Join(home, "mcp.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if err := writeMCPConfig(link, grafelCfg6246()); err != nil {
		t.Fatalf("writeMCPConfig: %v", err)
	}
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat link: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("dangling link was flattened into a regular file")
	}
	assertGrafelWritten6246(t, target)
}

// TestWriteMCPConfig_PreservesDestinationMode is the widening defect. 0644 is
// deliberately absent from the table: it is the mode the buggy writer produces,
// so it cannot witness anything.
func TestWriteMCPConfig_PreservesDestinationMode(t *testing.T) {
	for _, perm := range []os.FileMode{0o600, 0o444} {
		t.Run(perm.String(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "mcp.json")
			_ = seedMCPConfig6246(t, path, perm)

			if err := writeMCPConfig(path, grafelCfg6246()); err != nil {
				t.Fatalf("writeMCPConfig: %v", err)
			}
			testsupport.AssertPerm(t, path, perm)
			assertGrafelWritten6246(t, path)
		})
	}
}

// TestWriteMCPConfig_SymlinkPreservesTargetMode is the two defects composed: the
// mode that must survive is the TARGET's, not the link's (a symlink is 0777 on
// Linux).
//
// The content assertion is load-bearing, not incidental. Without it the test is
// VACUOUS against the buggy writer: that writer never touches the target at all,
// so the target trivially still has the 0600 it was seeded with. Being composed,
// this one dies for either mutant — the independence of the two fixes is
// established by WritesThroughSymlink and PreservesDestinationMode, which do
// not.
func TestWriteMCPConfig_SymlinkPreservesTargetMode(t *testing.T) {
	requireSymlinks6246(t)

	target := filepath.Join(t.TempDir(), "mcp.json")
	link := filepath.Join(t.TempDir(), "mcp.json")
	_ = seedMCPConfig6246(t, target, 0o600)
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if err := writeMCPConfig(link, grafelCfg6246()); err != nil {
		t.Fatalf("writeMCPConfig: %v", err)
	}
	assertGrafelWritten6246(t, target)
	testsupport.AssertPerm(t, target, 0o600)
}

// TestWriteMCPConfig_CreatesPrivateConfig pins the deliberate behaviour change:
// a host config the DASHBOARD invents is 0600, not 0644. These files carry
// credentials — ~/.claude.json holds an OAuth token — and at minimum enumerate
// every project on the machine. It matches what mcpreg chose in #6240 for the
// very same files; a second writer creating them wider would make the pair
// incoherent.
func TestWriteMCPConfig_CreatesPrivateConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := writeMCPConfig(path, grafelCfg6246()); err != nil {
		t.Fatalf("writeMCPConfig: %v", err)
	}
	testsupport.AssertPerm(t, path, 0o600)
}

// TestWriteMCPConfig_SymlinkCycleFailsLoudly: an unresolvable chain must be an
// error and must leave the user's link intact. Answering with the last hop
// reached is not a safe fallback — that path is itself a symlink, so the caller
// renames over it and flattens a link while reporting success.
func TestWriteMCPConfig_SymlinkCycleFailsLoudly(t *testing.T) {
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

	if err := writeMCPConfig(a, grafelCfg6246()); err == nil {
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

// TestWriteMCPConfig_BackupIsAVerbatimCopy asserts the thing the `<path>.bak`
// sidecar exists to do, which nothing in the tree asserted before: that it holds
// the bytes it is a backup OF.
//
// This is not hypothetical rigour. Deleting the io.Copy outright — leaving a
// zero-byte `.bak` with a flawless 0600 — left the ENTIRE internal/dashboard
// suite green, mode assertions included. copyFile now carries a deferred Close
// plus an explicit Close, a Remove and a Chmod: four things a later edit can
// reorder into a silently truncated backup. This sidecar is the only rollback
// material the wizard path leaves behind, for a file that holds an OAuth token.
// A mode without content is a backup that restores nothing.
func TestWriteMCPConfig_BackupIsAVerbatimCopy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	want := seedMCPConfig6246(t, path, 0o600)

	if err := writeMCPConfig(path, grafelCfg6246()); err != nil {
		t.Fatalf("writeMCPConfig: %v", err)
	}
	got, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("backup is not a verbatim copy of the original\n got  (%d bytes) %q\n want (%d bytes) %q",
			len(got), got, len(want), want)
	}
}

// TestWriteMCPConfig_BackupPreservesMode: the `<path>.bak` copy is a verbatim
// copy of a file that may hold an OAuth token. os.Create made it 0666&^umask,
// so backing up a 0600 config published its contents to every local account —
// the same widening defect one line above the one this issue names.
//
// Content is asserted separately, by TestWriteMCPConfig_BackupIsAVerbatimCopy —
// see there for why the two properties need two tests.
func TestWriteMCPConfig_BackupPreservesMode(t *testing.T) {
	for _, perm := range []os.FileMode{0o600, 0o444} {
		t.Run(perm.String(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "mcp.json")
			_ = seedMCPConfig6246(t, path, perm)

			if err := writeMCPConfig(path, grafelCfg6246()); err != nil {
				t.Fatalf("writeMCPConfig: %v", err)
			}
			testsupport.AssertPerm(t, path+".bak", perm)
			t.Cleanup(func() { _ = os.Chmod(path+".bak", 0o666) })
		})
	}
}
