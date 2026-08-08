// symlink_perm_6246_test.go pins the #6246 defects in this package's writer.
//
// atomicWrite was the fourth independent copy of the three lines #6240 fixed in
// internal/install/mcpreg — write `path + ".tmp"` at a hardcoded 0644, rename it
// over the destination — and it carries both of that shape's defects:
//
//  1. The rename replaces the LINK INODE. Unlike slice 1's dotfiles, the
//     destinations here are TRACKED FILES IN THE USER'S GIT REPO — CLAUDE.md,
//     AGENTS.md, .cursorrules, .github/copilot-instructions.md. A monorepo or a
//     shared-docs setup that symlinks CLAUDE.md at one canonical copy detaches on
//     the first `grafel install`, and the user's next commit silently does not
//     contain what they think it does: the link they committed has become a
//     regular file, and the canonical copy they keep editing is no longer read.
//  2. The destination inherits the TEMP file's 0644. A rules file deliberately
//     held at 0600 comes back world-readable; a 0444 one comes back writable.
//
// Every pre-existing fixture in this package seeds the default mode and never
// reads a mode back, so none of them can observe (2) at all — which is exactly
// why 40 mcpreg tests were blind to the same defect. The fixtures here seed 0600
// and a read-only 0444. The 0444 rows are the only ones with any signal on
// Windows, where FILE_ATTRIBUTE_READONLY is the one mode the platform can
// represent (testsupport.AssertPerm).
package rulesfiles

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/atomicfile"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// requireSymlinks6246 skips when the platform cannot create a symlink at all
// (Windows without developer mode or the SeCreateSymbolicLink privilege). A
// capability PROBE rather than a runtime.GOOS check, so a Windows runner that
// CAN make symlinks still exercises these.
func requireSymlinks6246(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "target"), filepath.Join(dir, "link")); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
}

const userPrologue6246 = "# House rules\n\nNothing to do with grafel.\n"

// seedRulesFile6246 writes user content at mode perm and returns the bytes.
func seedRulesFile6246(t *testing.T, path string, perm os.FileMode) []byte {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := []byte(userPrologue6246)
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

// assertBlockAndPrologue is the CONTENT witness. Without it a symlink assertion
// passes against the buggy writer purely because that writer never touches the
// target — the trap slice 1 hit twice.
func assertBlockAndPrologue(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(raw), StartMarker) {
		t.Fatalf("%s does not carry the grafel block: %q", path, raw)
	}
	if !strings.Contains(string(raw), "# House rules") {
		t.Fatalf("%s lost the pre-existing user content: %q", path, raw)
	}
}

// ── Defect 2: the destination's mode must survive the write ──────────────────

func TestWriteTargets_PreservesDestinationMode(t *testing.T) {
	for _, perm := range []os.FileMode{0o600, 0o644, 0o444} {
		t.Run(perm.String(), func(t *testing.T) {
			root := t.TempDir()
			file := filepath.Join(root, "AGENTS.md")
			seedRulesFile6246(t, file, perm)

			if _, err := WriteTargets(root, WriteOptions{GroupName: "g", Logger: io.Discard},
				[]string{"AGENTS.md"}); err != nil {
				t.Fatalf("WriteTargets: %v", err)
			}
			testsupport.AssertPerm(t, file, perm)
			assertBlockAndPrologue(t, file)
		})
	}
}

// TestWriteTargets_CreatesAtProjectReadableMode states the create-mode choice:
// these are files a user commits and every collaborator's checkout reads, so a
// fresh one is 0644 — NOT the 0600 the dashboard's credential-bearing host
// configs use.
//
// The group/other-readable assertion is what gives this test teeth. An
// AssertPerm against newRulesFilePerm ALONE is vacuous: it moves with the
// constant, so flipping the constant to 0600 leaves it green — which is exactly
// what the first cut of this test did.
func TestWriteTargets_CreatesAtProjectReadableMode(t *testing.T) {
	root := t.TempDir()
	if _, err := WriteTargets(root, WriteOptions{GroupName: "g", Logger: io.Discard},
		[]string{"AGENTS.md"}); err != nil {
		t.Fatalf("WriteTargets: %v", err)
	}
	testsupport.AssertPerm(t, filepath.Join(root, "AGENTS.md"), 0o644)
	if newRulesFilePerm&0o044 == 0 {
		t.Fatalf("newRulesFilePerm = %04o is not group/other readable; a committed "+
			"project file must be readable by every collaborator checkout", newRulesFilePerm)
	}
}

// ── Defect 1: a symlinked rules file must survive as a symlink ───────────────

// TestWriteTargets_WritesThroughSymlink is the monorepo case: CLAUDE.md in each
// package symlinked at one canonical copy. The install must update the canonical
// copy, not sever every package from it.
func TestWriteTargets_WritesThroughSymlink(t *testing.T) {
	requireSymlinks6246(t)
	root := t.TempDir()
	real := filepath.Join(root, "docs", "canonical.md")
	seedRulesFile6246(t, real, 0o600)

	link := filepath.Join(root, "CLAUDE.md")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if _, err := WriteTargets(root, WriteOptions{GroupName: "g", Logger: io.Discard},
		[]string{"CLAUDE.md"}); err != nil {
		t.Fatalf("WriteTargets: %v", err)
	}

	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("WriteTargets replaced the symlink at %s with a regular file — "+
			"the user's next commit no longer contains the link they wrote", link)
	}
	assertBlockAndPrologue(t, real)
	testsupport.AssertPerm(t, real, 0o600)
}

// TestWriteTargets_DanglingSymlinkCreatesTheTarget covers a rules file linked at
// a canonical copy that does not exist yet (a fresh clone of the docs repo, or a
// path the user set up ahead of time). The create must land on the TARGET.
//
// It also pins the MkdirAll moving onto the RESOLVED directory: the link's own
// directory always exists, the target's need not.
func TestWriteTargets_DanglingSymlinkCreatesTheTarget(t *testing.T) {
	requireSymlinks6246(t)
	root := t.TempDir()
	real := filepath.Join(root, "docs", "canonical.md") // docs/ does not exist
	link := filepath.Join(root, "CLAUDE.md")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if _, err := WriteTargets(root, WriteOptions{GroupName: "g", Logger: io.Discard},
		[]string{"CLAUDE.md"}); err != nil {
		t.Fatalf("WriteTargets: %v", err)
	}

	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("WriteTargets replaced the dangling symlink at %s with a regular file", link)
	}
	raw, err := os.ReadFile(real)
	if err != nil {
		t.Fatalf("target %s was never created: %v", real, err)
	}
	if !strings.Contains(string(raw), StartMarker) {
		t.Fatalf("target %s does not carry the grafel block: %q", real, raw)
	}
	testsupport.AssertPerm(t, real, 0o644)
}

// ── An unresolvable chain must fail, not flatten ─────────────────────────────

// TestAtomicWrite_SymlinkCycleIsAnError drives atomicWrite DIRECTLY, and
// deliberately so.
//
// Every public entry point in this package (upsert, upsertPlain, RemoveTargets)
// reads the destination with os.ReadFile before it ever writes, and a link cycle
// makes that read fail with ELOOP first — so a cycle test driven through
// WriteTargets would pass against the buggy writer for a reason that has nothing
// to do with the writer. That is the vacuity trap, and mcpreg hit the same one
// (#6240: only RestoreSnapshot, which writes with no prior read, was live).
//
// atomicWrite is the unit that must refuse, because it is the unit that would
// otherwise rename over a mid-cycle path — itself a symlink — flattening one of
// the user's links into a regular file and returning nil.
func TestAtomicWrite_SymlinkCycleIsAnError(t *testing.T) {
	requireSymlinks6246(t)
	dir := t.TempDir()
	a := filepath.Join(dir, "AGENTS.md")
	b := filepath.Join(dir, "AGENTS.other.md")
	if err := os.Symlink(b, a); err != nil {
		t.Fatalf("symlink a: %v", err)
	}
	if err := os.Symlink(a, b); err != nil {
		t.Fatalf("symlink b: %v", err)
	}

	err := atomicWrite(a, []byte("block"))
	if err == nil {
		t.Fatalf("atomicWrite silently succeeded on a symlink cycle")
	}
	if !errors.Is(err, atomicfile.ErrSymlinkChain) {
		t.Fatalf("error does not classify as ErrSymlinkChain: %v", err)
	}
	for _, p := range []string{a, b} {
		fi, lerr := os.Lstat(p)
		if lerr != nil {
			t.Fatalf("lstat %s: %v", p, lerr)
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s was flattened into a regular file by a write that should have refused", p)
		}
	}
}
