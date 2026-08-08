// symlink_perm_6246_test.go pins the #6246 defects in `grafel register
// --write-agents-md`.
//
// upsertAgentsMDFile splices a marker-wrapped block into the user's AGENTS.md
// with the same three lines #6240 fixed in internal/install/mcpreg: write
// `path + ".tmp"` at a hardcoded 0644, rename it over the destination.
//
//  1. The rename replaces the LINK INODE. AGENTS.md is a TRACKED FILE IN THE
//     USER'S REPO, so this is not the dotfile blast radius of slice 1: a repo
//     that symlinks AGENTS.md at a shared copy has that link quietly replaced by
//     a regular file, and the user's next commit does not contain what they
//     think it does.
//  2. The destination inherits the TEMP file's 0644, so a 0444 AGENTS.md comes
//     back writable and a 0600 one comes back world-readable.
//
// There is a third, specific to this call site: the function READS with
// os.ReadFile, which FOLLOWS a symlink, and WRITES with rename, which does not.
// Read-through / write-around means the block is spliced into content taken from
// the link's target and then deposited somewhere else entirely.
//
// register_test.go's pre-existing fixtures create the file through
// upsertAgentsMDFile itself and never read a mode back, so they cannot observe
// (2). These seed 0600 and a read-only 0444 — the 0444 rows being the only ones
// with signal on Windows (testsupport.AssertPerm).
package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/atomicfile"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// requireSymlinks6246 skips when the platform cannot create a symlink at all. A
// capability PROBE rather than a runtime.GOOS check, so a Windows runner that
// CAN make symlinks still exercises these.
func requireSymlinks6246(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "target"), filepath.Join(dir, "link")); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
}

const agentsPrologue6246 = "# Contributing\n\nHouse rules, nothing to do with grafel.\n"

func seedAgentsMD6246(t *testing.T, path string, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(agentsPrologue6246), perm); err != nil {
		t.Fatalf("seed %s: %v", path, err)
	}
	// os.WriteFile's perm is umask-masked and does not apply to an existing
	// file, so state the mode explicitly.
	if err := os.Chmod(path, perm); err != nil {
		t.Fatalf("chmod seed: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o666) })
}

// assertStubAndPrologue is the CONTENT witness. Without it the symlink
// assertions below pass against the buggy writer purely because that writer
// never touches the target.
func assertStubAndPrologue(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(raw), agentsMDStartMarker) {
		t.Fatalf("%s does not carry the grafel block: %q", path, raw)
	}
	if !strings.Contains(string(raw), "# Contributing") {
		t.Fatalf("%s lost the pre-existing user content — the read side and the write "+
			"side disagreed about which path they were operating on: %q", path, raw)
	}
}

// ── Defect 2: the destination's mode must survive the write ──────────────────

func TestUpsertAgentsMDFile_PreservesDestinationMode(t *testing.T) {
	for _, perm := range []os.FileMode{0o600, 0o644, 0o444} {
		t.Run(perm.String(), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "AGENTS.md")
			seedAgentsMD6246(t, path, perm)

			if err := upsertAgentsMDFile(path, renderAgentsMDStub("g")); err != nil {
				t.Fatalf("upsertAgentsMDFile: %v", err)
			}
			testsupport.AssertPerm(t, path, perm)
			assertStubAndPrologue(t, path)
		})
	}
}

// TestUpsertAgentsMDFile_CreatesAtProjectReadableMode states the create-mode
// choice: AGENTS.md is a project file that every collaborator's checkout reads
// and that is routinely committed, so a fresh one is 0644, NOT the 0600 the
// dashboard's credential-bearing host configs use.
func TestUpsertAgentsMDFile_CreatesAtProjectReadableMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	if err := upsertAgentsMDFile(path, renderAgentsMDStub("g")); err != nil {
		t.Fatalf("upsertAgentsMDFile: %v", err)
	}
	testsupport.AssertPerm(t, path, 0o644)
	if newAgentsMDPerm&0o044 == 0 {
		t.Fatalf("newAgentsMDPerm = %04o is not group/other readable; a committed "+
			"project file must be readable by every collaborator checkout", newAgentsMDPerm)
	}
}

// ── Defect 1 + 3: symlink, and read/write path agreement ─────────────────────

func TestUpsertAgentsMDFile_WritesThroughSymlink(t *testing.T) {
	requireSymlinks6246(t)
	dir := t.TempDir()
	real := filepath.Join(dir, "shared", "AGENTS.md")
	seedAgentsMD6246(t, real, 0o600)

	link := filepath.Join(dir, "AGENTS.md")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if err := upsertAgentsMDFile(link, renderAgentsMDStub("g")); err != nil {
		t.Fatalf("upsertAgentsMDFile: %v", err)
	}

	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("upsertAgentsMDFile replaced the symlink at %s with a regular file", link)
	}
	assertStubAndPrologue(t, real)
	testsupport.AssertPerm(t, real, 0o600)
}

// TestUpsertAgentsMDFile_IsIdempotentThroughSymlink is the read/write-agreement
// case with no equivalent in the symlink test above: the SECOND run must find
// the block it wrote on the first. If the read side resolved and the write side
// did not (or vice versa), the second run reads a file that has no block and
// appends a second copy.
func TestUpsertAgentsMDFile_IsIdempotentThroughSymlink(t *testing.T) {
	requireSymlinks6246(t)
	dir := t.TempDir()
	real := filepath.Join(dir, "shared", "AGENTS.md")
	seedAgentsMD6246(t, real, 0o644)

	link := filepath.Join(dir, "AGENTS.md")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	for i := 0; i < 2; i++ {
		if err := upsertAgentsMDFile(link, renderAgentsMDStub("g")); err != nil {
			t.Fatalf("upsertAgentsMDFile run %d: %v", i, err)
		}
	}
	raw, err := os.ReadFile(real)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if n := strings.Count(string(raw), agentsMDStartMarker); n != 1 {
		t.Fatalf("block appears %d times after two runs, want 1 — the read side and the "+
			"write side disagree about which path they operate on:\n%s", n, raw)
	}
}

// TestUpsertAgentsMDFile_DanglingSymlinkCreatesTheTarget: a link at a shared
// AGENTS.md that does not exist yet must be filled in, not severed.
//
// `shared/` is deliberately NOT pre-created, and this is the point of the test.
// The first cut of it did pre-create the directory, which meant it could not
// observe that this call site had no MkdirAll while its twin in the same
// commit — rulesfiles.atomicWrite — did: `grafel register --write-agents-md`
// hard-failed with an opaque error naming a temp file. The two writers landed
// together and must not diverge silently, so the fixture matches its twin in
// internal/install/rulesfiles.
func TestUpsertAgentsMDFile_DanglingSymlinkCreatesTheTarget(t *testing.T) {
	requireSymlinks6246(t)
	dir := t.TempDir()
	real := filepath.Join(dir, "shared", "AGENTS.md") // shared/ does not exist
	link := filepath.Join(dir, "AGENTS.md")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if err := upsertAgentsMDFile(link, renderAgentsMDStub("g")); err != nil {
		t.Fatalf("upsertAgentsMDFile: %v", err)
	}
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("upsertAgentsMDFile replaced the dangling symlink at %s with a regular file", link)
	}
	raw, err := os.ReadFile(real)
	if err != nil {
		t.Fatalf("target %s was never created: %v", real, err)
	}
	if !strings.Contains(string(raw), agentsMDStartMarker) {
		t.Fatalf("target %s does not carry the grafel block: %q", real, raw)
	}
}

// TestUpsertAgentsMDFile_SymlinkCycleIsClassified.
//
// Honest note on what this does and does not prove. The buggy version ALSO
// returned an error here — os.ReadFile hits ELOOP before the writer is reached,
// the same accident that saved mcpreg's RegisterPath in #6240 — so "an error
// came back" is not on its own evidence of anything. What is new is that the
// error CLASSIFIES as ErrSymlinkChain, which can only happen if resolution ran
// before the write; and, more importantly, that the classification is produced
// by the same call whose removal is the symlink mutant. A revision that resolved
// for the write but left the read on the raw path would return a bare *PathError
// and fail here.
func TestUpsertAgentsMDFile_SymlinkCycleIsClassified(t *testing.T) {
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

	err := upsertAgentsMDFile(a, renderAgentsMDStub("g"))
	if err == nil {
		t.Fatalf("upsertAgentsMDFile silently succeeded on a symlink cycle")
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
			t.Errorf("%s was flattened into a regular file", p)
		}
	}
}
