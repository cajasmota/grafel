package walk

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile creates a file (and parent dirs) with trivial content.
func mkSrc(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func contains(files []string, want string) bool {
	for _, f := range files {
		if f == want {
			return true
		}
	}
	return false
}

// A fixture with a foo.musiclibrary bundle + a heavy node_modules → both
// SKIPPED; real source still indexed.
func TestWalkRepo_SkipsMediaBundleAndNodeModules(t *testing.T) {
	root := t.TempDir()
	mkSrc(t, filepath.Join(root, "src", "main.go"))
	mkSrc(t, filepath.Join(root, "foo.musiclibrary", "track.go"))
	mkSrc(t, filepath.Join(root, "node_modules", "pkg", "index.go"))

	var skipLog bytes.Buffer
	files, skipped, err := WalkRepo(root, &Options{PrintSkipped: &skipLog})
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}
	if !contains(files, "src/main.go") {
		t.Errorf("expected src/main.go to be indexed, got %v", files)
	}
	if contains(files, "foo.musiclibrary/track.go") {
		t.Errorf("media-library bundle content must not be indexed: %v", files)
	}
	if contains(files, "node_modules/pkg/index.go") {
		t.Errorf("node_modules content must not be indexed: %v", files)
	}

	var sawBundle bool
	for _, s := range skipped {
		if strings.Contains(s.Rule, "media-library") {
			sawBundle = true
		}
	}
	if !sawBundle {
		t.Errorf("expected a media-library skip entry, got %+v", skipped)
	}
}

// A repo root that resolves (via symlink) into a fake ~/Music-like dir →
// REFUSED with WARN, not walked.
func TestWalkRepo_RefusesRootInMediaBundle(t *testing.T) {
	// Use a media-library bundle as the root directly — IsProtectedPath
	// catches it on every platform, so this is portable.
	root := filepath.Join(t.TempDir(), "Mine.photoslibrary")
	mkSrc(t, filepath.Join(root, "private", "data.go"))

	var skipLog bytes.Buffer
	files, skipped, err := WalkRepo(root, &Options{PrintSkipped: &skipLog})
	if err == nil {
		t.Fatalf("expected WalkRepo to refuse protected root, got nil error")
	}
	if len(files) != 0 {
		t.Errorf("protected root must yield no files, got %v", files)
	}
	if len(skipped) == 0 || !strings.Contains(skipped[0].Rule, "protected") {
		t.Errorf("expected a protected skip entry, got %+v", skipped)
	}
	if !strings.Contains(skipLog.String(), "WARN") {
		t.Errorf("expected a WARN in skip log, got %q", skipLog.String())
	}
}

// Watch-dir cap: a tree exceeding the cap → WARN + capped/skipped.
func TestWalkRepo_DirCapWarnsAndSkips(t *testing.T) {
	t.Setenv("GRAFEL_WATCH_DIR_CAP", "5")
	root := t.TempDir()
	// Create many directories, each with a source file.
	for i := 0; i < 30; i++ {
		mkSrc(t, filepath.Join(root, "d"+pad(i), "f.go"))
	}

	var skipLog bytes.Buffer
	files, _, err := WalkRepo(root, &Options{PrintSkipped: &skipLog})
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}
	if !strings.Contains(skipLog.String(), "WARN") || !strings.Contains(skipLog.String(), "cap") {
		t.Errorf("expected a dir-cap WARN, got %q", skipLog.String())
	}
	// We should have collected fewer files than the 30 we created.
	if len(files) >= 30 {
		t.Errorf("dir-cap did not reduce the walked set: got %d files", len(files))
	}
}

// A normal small code repo → fully walked (no false skips).
func TestWalkRepo_NormalRepoFullyWalked(t *testing.T) {
	root := t.TempDir()
	want := []string{
		"main.go",
		"internal/a/a.go",
		"internal/b/b.go",
		"cmd/tool/main.go",
	}
	for _, rel := range want {
		mkSrc(t, filepath.Join(root, filepath.FromSlash(rel)))
	}

	files, _, err := WalkRepo(root, nil)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}
	for _, w := range want {
		if !contains(files, w) {
			t.Errorf("expected %q to be walked, got %v", w, files)
		}
	}
}

func pad(i int) string {
	s := ""
	if i < 10 {
		s = "0"
	}
	return s + tccItoa(i)
}

func tccItoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
