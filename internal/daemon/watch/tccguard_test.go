package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A media-library bundle anywhere in the tree must never be subscribed.
func TestShouldSkipDir_MediaLibraryBundle(t *testing.T) {
	cases := map[string]bool{
		"foo.musiclibrary":             true,
		"Photos Library.photoslibrary": true,
		"recordings.tvlibrary":         true,
		"src":                          false,
		"node_modules":                 true,
	}
	for base, want := range cases {
		if got := ShouldSkipDir(base); got != want {
			t.Errorf("ShouldSkipDir(%q) = %v, want %v", base, got, want)
		}
	}
}

// AddRepo must refuse a repo root that is a media-library bundle (the
// cross-platform-catchable arm of the protected-path guard).
func TestAddRepo_RefusesMediaLibraryRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Family.photoslibrary")
	if err := os.MkdirAll(filepath.Join(root, "originals"), 0o755); err != nil {
		t.Fatal(err)
	}
	w, err := newTestWatcher(50*time.Millisecond, func(string, bool) {})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	n, err := w.AddRepo(root)
	if err == nil {
		t.Fatalf("expected AddRepo to refuse media-library root, got nil error (n=%d)", n)
	}
	if n != 0 {
		t.Errorf("expected 0 dirs subscribed, got %d", n)
	}
}

// The watch-dir cap bounds the number of subscribed directories on a tree
// that blows past the cap.
func TestSubscribeRepo_DirCap(t *testing.T) {
	t.Setenv("GRAFEL_WATCH_DIR_CAP", "5")
	root := t.TempDir()
	for i := 0; i < 40; i++ {
		d := filepath.Join(root, "pkg", itoa3(i))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	w, err := newTestWatcher(50*time.Millisecond, func(string, bool) {})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	n, err := w.AddRepo(root)
	if err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	// The cap is 5; we must not register all 40+ directories. Allow a small
	// margin since the cap is checked as added >= cap before walking deeper.
	if n > 10 {
		t.Errorf("watch-dir cap not enforced: subscribed %d dirs, want <= 10", n)
	}
}

func itoa3(i int) string {
	if i == 0 {
		return "000"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	for len(b) < 3 {
		b = append([]byte{'0'}, b...)
	}
	return string(b)
}
