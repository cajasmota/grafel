// doctor_binident_test.go covers classifyBinaryIdentity and the QuickOptions /
// DoctorOptions defaults that feed it.
//
// These are internal (package install) because the interesting cases —
// "SelfPath could not be resolved", "the recorded binary was deleted mid-
// upgrade", "one binary reached under two names" — are either unreachable
// through RunQuickDoctor (applyDefaults always fills SelfPath) or would need a
// re-exec to stage. Review of the first pass found four surviving mutants in
// exactly this code for that reason; each case below kills one.
package install

import (
	"os"
	"path/filepath"
	"testing"
)

func writeBin(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// TestClassifyBinaryIdentity_IdenticalPath: the textual fast path.
func TestClassifyBinaryIdentity_IdenticalPath(t *testing.T) {
	p := writeBin(t, filepath.Join(t.TempDir(), "grafel"), "bin")
	if got := classifyBinaryIdentity(p, p); got != binarySame {
		t.Errorf("classifyBinaryIdentity(same path) = %v, want binarySame", got)
	}
}

// TestClassifyBinaryIdentity_IdenticalPathButDeleted is the load-bearing half
// of the fast path, and the one a mutant that deletes `self == recorded` gets
// wrong. During an in-place upgrade there is a window where the recorded path
// does not exist; the old code was silent there (sha256File simply errored) and
// it must stay silent, not start reporting a missing install. Only the SHA
// check applies to "same path", and that check handles its own read error.
func TestClassifyBinaryIdentity_IdenticalPathButDeleted(t *testing.T) {
	p := filepath.Join(t.TempDir(), "grafel") // never created
	if got := classifyBinaryIdentity(p, p); got != binarySame {
		t.Errorf("classifyBinaryIdentity(same path, missing file) = %v, want binarySame "+
			"(the rename window of an in-place upgrade must not be reported as a relocated install)", got)
	}
}

// TestClassifyBinaryIdentity_SymlinkedBinDir: one binary reached under two
// names. This is the shape of a symlinked bin dir (/usr/local/bin ->
// /opt/homebrew/bin), a version-stamped prefix behind a stable symlink (Nix
// store, Linuxbrew Cellar, asdf/mise shims) and macOS's /tmp -> /private/tmp.
// Must be binarySame, or every such user gets a permanent false warning.
func TestClassifyBinaryIdentity_SymlinkedBinDir(t *testing.T) {
	tmp := t.TempDir()
	real := writeBin(t, filepath.Join(tmp, "Cellar", "grafel", "0.2.0", "bin", "grafel"), "bin")
	link := filepath.Join(tmp, "grafel-link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if got := classifyBinaryIdentity(link, real); got != binarySame {
		t.Errorf("classifyBinaryIdentity(symlink, target) = %v, want binarySame", got)
	}
	if got := classifyBinaryIdentity(real, link); got != binarySame {
		t.Errorf("classifyBinaryIdentity(target, symlink) = %v, want binarySame", got)
	}
}

// TestClassifyBinaryIdentity_HardLink: two names, one inode, no symlink
// involved. EvalSymlinks-based comparison gets this wrong; os.SameFile does not.
func TestClassifyBinaryIdentity_HardLink(t *testing.T) {
	tmp := t.TempDir()
	a := writeBin(t, filepath.Join(tmp, "grafel"), "bin")
	b := filepath.Join(tmp, "grafel-hardlink")
	if err := os.Link(a, b); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	if got := classifyBinaryIdentity(b, a); got != binarySame {
		t.Errorf("classifyBinaryIdentity(hard link, original) = %v, want binarySame", got)
	}
}

// TestClassifyBinaryIdentity_TwoRealInstalls: the case that must NOT be offered
// --refresh-state.
func TestClassifyBinaryIdentity_TwoRealInstalls(t *testing.T) {
	tmp := t.TempDir()
	a := writeBin(t, filepath.Join(tmp, "a", "grafel"), "install A")
	b := writeBin(t, filepath.Join(tmp, "b", "grafel"), "install B")
	if got := classifyBinaryIdentity(a, b); got != binaryDiffers {
		t.Errorf("classifyBinaryIdentity(two distinct files) = %v, want binaryDiffers", got)
	}
}

// TestClassifyBinaryIdentity_RecordedGone: a relocated install. Distinct from
// binaryDiffers because here re-recording IS the correct remedy — there is only
// one binary left, so nothing can ping-pong.
func TestClassifyBinaryIdentity_RecordedGone(t *testing.T) {
	tmp := t.TempDir()
	self := writeBin(t, filepath.Join(tmp, "new", "grafel"), "relocated")
	gone := filepath.Join(tmp, "old", "grafel")
	if got := classifyBinaryIdentity(self, gone); got != binaryRecordedMissing {
		t.Errorf("classifyBinaryIdentity(self, missing recorded) = %v, want binaryRecordedMissing", got)
	}
}

// TestClassifyBinaryIdentity_UnresolvableInputsAreSilent: with no SelfPath
// there is no comparison to make, and a warning derived from a comparison we
// could not perform is noise by construction. Kills the mutant that drops the
// empty-SelfPath guard.
func TestClassifyBinaryIdentity_UnresolvableInputsAreSilent(t *testing.T) {
	tmp := t.TempDir()
	recorded := writeBin(t, filepath.Join(tmp, "grafel"), "bin")

	if got := classifyBinaryIdentity("", recorded); got != binaryUnknown {
		t.Errorf("classifyBinaryIdentity(\"\", recorded) = %v, want binaryUnknown", got)
	}
	if got := classifyBinaryIdentity(recorded, ""); got != binaryUnknown {
		t.Errorf("classifyBinaryIdentity(self, \"\") = %v, want binaryUnknown", got)
	}
}

// TestClassifyBinaryIdentity_UnstatableSelfIsSilent: the recorded binary exists
// but we cannot stat our own path. We cannot tell whether they are the same
// file, so we must not guess "different" — that would print the two-installs
// warning off a failed comparison.
func TestClassifyBinaryIdentity_UnstatableSelfIsSilent(t *testing.T) {
	tmp := t.TempDir()
	recorded := writeBin(t, filepath.Join(tmp, "grafel"), "bin")
	missingSelf := filepath.Join(tmp, "vanished", "grafel")

	if got := classifyBinaryIdentity(missingSelf, recorded); got != binaryUnknown {
		t.Errorf("classifyBinaryIdentity(unstatable self, recorded) = %v, want binaryUnknown", got)
	}
}

// TestQuickOptions_DefaultsResolveSelfPath: production wiring. cmd/grafel and
// internal/cli both construct QuickOptions without a SelfPath, so the entire
// identity check depends on this default being populated — nothing else covers
// it, and a mutant removing it survived the first pass.
func TestQuickOptions_DefaultsResolveSelfPath(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Skipf("os.Executable unavailable: %v", err)
	}
	var o QuickOptions
	if err := o.applyDefaults(); err != nil {
		t.Fatalf("applyDefaults: %v", err)
	}
	if o.SelfPath != self {
		t.Errorf("QuickOptions.SelfPath default = %q, want %q", o.SelfPath, self)
	}
	if o.Version == "" {
		t.Error("QuickOptions.Version default must be populated")
	}
}

// TestDoctorOptions_DefaultsResolveSelfPath: same wiring for full doctor, which
// gained the identity check in this change.
func TestDoctorOptions_DefaultsResolveSelfPath(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Skipf("os.Executable unavailable: %v", err)
	}
	var o DoctorOptions
	if err := o.applyDefaults(); err != nil {
		t.Fatalf("applyDefaults: %v", err)
	}
	if o.SelfPath != self {
		t.Errorf("DoctorOptions.SelfPath default = %q, want %q", o.SelfPath, self)
	}
	if o.Version == "" {
		t.Error("DoctorOptions.Version default must be populated")
	}
}
