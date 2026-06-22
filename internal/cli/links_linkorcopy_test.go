package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLinkOrCopyFile_Symlinks verifies the happy path: when os.Symlink
// succeeds (Linux/mac, or Windows with the privilege), dst is a symlink to src
// and no bytes are copied.
func TestLinkOrCopyFile_Symlinks(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "graph.fb")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "staged", "graph.fb")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := linkOrCopyFile(src, dst); err != nil {
		t.Fatalf("linkOrCopyFile: %v", err)
	}
	// Where symlinks are permitted dst should be a symlink; the content must
	// resolve to the source regardless.
	if fi, err := os.Lstat(dst); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(dst)
		if err != nil || target != src {
			t.Errorf("symlink target = %q (err %v), want %q", target, err, src)
		}
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "payload" {
		t.Errorf("dst content = %q (err %v), want %q", got, err, "payload")
	}
}

// TestLinkOrCopyFile_FallsBackToCopy simulates the Windows-without-privilege
// case where os.Symlink fails: linkOrCopyFile must fall back to a byte copy so
// the cross-repo staging still completes (bug 5). We force the symlink to fail
// by pre-creating dst as a regular file (os.Symlink returns EEXIST); the
// fallback then truncates and copies, leaving a real (non-symlink) file whose
// bytes match src.
func TestLinkOrCopyFile_FallsBackToCopy(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "graph.json")
	if err := os.WriteFile(src, []byte("real-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "graph.json.dst")
	// Pre-create dst so os.Symlink(src, dst) fails with EEXIST, exercising the
	// copy fallback deterministically on every platform.
	if err := os.WriteFile(dst, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := linkOrCopyFile(src, dst); err != nil {
		t.Fatalf("linkOrCopyFile fallback: %v", err)
	}

	fi, err := os.Lstat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("expected a copied regular file, got a symlink")
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "real-bytes" {
		t.Errorf("dst content = %q (err %v), want %q", got, err, "real-bytes")
	}
}
