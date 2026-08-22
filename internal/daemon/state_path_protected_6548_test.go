// Tests for the protected-path gate on canonicalizePath (#6548).
//
// Root cause: canonicalizePath is a top-down decomposition that os.ReadDir's
// EVERY ancestor of every repo path to recover each segment's on-disk casing.
// It had no protected-path check at all, so on a Mac with iCloud "Desktop &
// Documents" sync on, canonicalizing a path under ~/Documents read a
// file-provider-managed directory and the OS asked the user to approve
// «"grafel" wants to access files managed by "iCloud Drive"» — for a location
// the user never pointed grafel at.
//
// The fix: consult the single protected-path authority (internal/protectedpath)
// before each segment read and, for a protected directory, SKIP the casing
// recovery instead of reading it. The returned path is then the input path with
// the input's own casing preserved from that segment onward — the same
// deterministic degrade already used for a read error or a ReadDir timeout, and
// stable across calls because canonicalizePath is keyed and cached by the input
// string.
//
// No test here reads a real ~/Documents: the home root is injected and the
// fixture lives under t.TempDir().
package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/protectedpath"
)

// TestCanonicalizePath_SkipsProtectedDir is the #6548 regression: a path under
// ~/Documents must be canonicalized WITHOUT ever reading ~/Documents.
func TestCanonicalizePath_SkipsProtectedDir(t *testing.T) {
	home := t.TempDir()
	docs := filepath.Join(home, "Documents")
	if err := os.MkdirAll(filepath.Join(docs, "proj"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Pretend we are on macOS with this temp dir as $HOME.
	origProtected := traversalProtected
	traversalProtected = func(p string) (bool, string) {
		return protectedpath.IsTraversalProtectedIn(p, home, "darwin")
	}
	t.Cleanup(func() { traversalProtected = origProtected })

	var read []string
	origRead := readDirFunc
	readDirFunc = func(dir string) ([]os.DirEntry, error) {
		read = append(read, dir)
		return os.ReadDir(dir)
	}
	t.Cleanup(func() { readDirFunc = origRead })

	// Deliberately mis-cased leaf so we can observe whether casing recovery
	// ran (it must NOT: recovering it requires reading ~/Documents).
	input := filepath.Join(docs, "PROJ")
	got := canonicalizePath(input)

	for _, dir := range read {
		if dir == docs || strings.HasPrefix(dir, docs+string(filepath.Separator)) {
			t.Errorf("canonicalizePath read protected directory %q (#6548); reads = %v", dir, read)
		}
	}
	if got != input {
		t.Errorf("canonicalizePath(%q) = %q; a protected segment must degrade to the input casing", input, got)
	}
}

// A legitimate repo under a NON-protected home subdirectory must still get
// full casing recovery — the permissive direction would silently stop
// canonicalizing (and therefore de-duplicating) real repos.
func TestCanonicalizePath_UnprotectedPathStillCanonicalized(t *testing.T) {
	home := t.TempDir()
	// "Documentation" shares a prefix with "Documents" but is not protected.
	real := filepath.Join(home, "Projects", "Documentation", "grafel")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}

	origProtected := traversalProtected
	traversalProtected = func(p string) (bool, string) {
		return protectedpath.IsTraversalProtectedIn(p, home, "darwin")
	}
	t.Cleanup(func() { traversalProtected = origProtected })

	input := filepath.Join(home, "Projects", "DOCUMENTATION", "GRAFEL")
	got := canonicalizePath(input)
	if got != real {
		t.Errorf("canonicalizePath(%q) = %q, want %q — a legitimate path must still be canonicalized", input, got, real)
	}
}
