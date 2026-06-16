package install

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestRemoveBinary_RemovesNonRunningBinary verifies the entry function deletes a
// plain (non-running) binary via the normal path on every OS.
func TestRemoveBinary_RemovesNonRunningBinary(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "grafel-copy")
	if err := os.WriteFile(bin, []byte("not the running process"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := removeBinary(bin); err != nil {
		t.Fatalf("removeBinary: %v", err)
	}
	if _, err := os.Stat(bin); !os.IsNotExist(err) {
		t.Fatalf("binary still present after removeBinary; stat err = %v", err)
	}
}

// TestIsRunningExecutable verifies the running-exe detection: the test binary's
// own os.Executable() matches, an arbitrary other file does not.
func TestIsRunningExecutable(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Skipf("os.Executable unavailable: %v", err)
	}
	if !isRunningExecutable(self) {
		t.Errorf("isRunningExecutable(self=%s) = false, want true", self)
	}

	other := filepath.Join(t.TempDir(), "other")
	if err := os.WriteFile(other, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if isRunningExecutable(other) {
		t.Errorf("isRunningExecutable(other=%s) = true, want false", other)
	}
}

// TestSameFile checks identity detection across symlinks and distinct files.
func TestSameFile(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	if err := os.WriteFile(a, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if sameFile(a, b) {
		t.Errorf("sameFile(a,b) = true for distinct files")
	}
	if !sameFile(a, a) {
		t.Errorf("sameFile(a,a) = false, want true")
	}

	// A symlink to a resolves to the same file.
	link := filepath.Join(dir, "link")
	if err := os.Symlink(a, link); err == nil {
		if !sameFile(a, link) {
			t.Errorf("sameFile(a, link->a) = false, want true")
		}
	}
}

// TestRenamedAsidePath checks the orphan path is a unique sibling in the same
// directory and embeds the pid.
func TestRenamedAsidePath(t *testing.T) {
	bin := filepath.FromSlash("/opt/grafel/bin/grafel.exe")
	pid := 4242
	got := renamedAsidePath(bin, pid)

	if filepath.Dir(got) != filepath.Dir(bin) {
		t.Errorf("aside dir = %s, want same dir as bin %s", filepath.Dir(got), filepath.Dir(bin))
	}
	if got == bin {
		t.Errorf("aside path equals original bin path")
	}
	if !strings.Contains(filepath.Base(got), strconv.Itoa(pid)) {
		t.Errorf("aside base %q does not embed pid %d", filepath.Base(got), pid)
	}
	if !strings.HasSuffix(got, ".old") {
		t.Errorf("aside path %q does not end in .old", got)
	}
	// Different pids yield different paths (no collision under concurrent uninstall).
	if renamedAsidePath(bin, 1) == renamedAsidePath(bin, 2) {
		t.Errorf("aside paths collide across pids")
	}
}

// TestCanonicalPath checks absolute + clean normalisation is applied.
func TestCanonicalPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "..", "file")
	got := canonicalPath(p)
	want := caseFold(filepath.Join(dir, "file"))
	if got != want {
		t.Errorf("canonicalPath(%q) = %q, want %q", p, got, want)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("canonicalPath result %q is not absolute", got)
	}
}
