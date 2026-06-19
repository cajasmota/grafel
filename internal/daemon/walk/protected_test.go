package walk

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIsMediaLibraryBundle(t *testing.T) {
	cases := map[string]bool{
		"foo.musiclibrary":            true,
		"My Library.photoslibrary":    true,
		"Photos Library.photoslibrary": true,
		"recordings.tvlibrary":        true,
		"Old.aplibrary":               true,
		"node_modules":                false,
		"src":                         false,
		"music":                       false, // not a bundle suffix
	}
	for name, want := range cases {
		if got := IsMediaLibraryBundle(name); got != want {
			t.Errorf("IsMediaLibraryBundle(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestIsProtectedPath_MediaBundleAnyPlatform(t *testing.T) {
	// Bundle by name must be caught on every platform.
	p := filepath.Join(t.TempDir(), "Photos Library.photoslibrary")
	protected, reason := isProtectedPathWithHome(p, "/some/home", runtime.GOOS)
	if !protected {
		t.Fatalf("expected media bundle %q to be protected", p)
	}
	if !strings.Contains(reason, "media-library bundle") {
		t.Errorf("reason = %q, want media-library bundle", reason)
	}
}

func TestIsProtectedPath_HomeMusicDarwin(t *testing.T) {
	home := t.TempDir()
	music := filepath.Join(home, "Music")
	if err := os.MkdirAll(filepath.Join(music, "iTunes"), 0o755); err != nil {
		t.Fatal(err)
	}

	// On darwin, a path under ~/Music is protected.
	got, reason := isProtectedPathWithHome(filepath.Join(music, "iTunes"), home, "darwin")
	if !got {
		t.Fatalf("expected ~/Music/iTunes to be protected on darwin")
	}
	if !strings.Contains(reason, "~/Music") {
		t.Errorf("reason = %q, want ~/Music", reason)
	}

	// A sibling that merely shares a prefix must NOT be protected.
	sibling := filepath.Join(home, "MusicStudio")
	if got, _ := isProtectedPathWithHome(sibling, home, "darwin"); got {
		t.Errorf("MusicStudio must not be treated as under ~/Music")
	}

	// Off darwin, ~/Music carries no special meaning.
	if got, _ := isProtectedPathWithHome(filepath.Join(music, "iTunes"), home, "linux"); got {
		t.Errorf("~/Music must not be protected off darwin")
	}
}

func TestIsProtectedPath_SymlinkIntoMusic(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on windows")
	}
	home := t.TempDir()
	music := filepath.Join(home, "Music")
	target := filepath.Join(music, "secret-repo")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	// A repo path that is actually a symlink into ~/Music must be refused
	// after symlink resolution.
	link := filepath.Join(t.TempDir(), "myrepo")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	got, reason := isProtectedPathWithHome(link, home, "darwin")
	if !got {
		t.Fatalf("symlink into ~/Music must be protected, got false")
	}
	if !strings.Contains(reason, "~/Music") {
		t.Errorf("reason = %q, want ~/Music", reason)
	}
}

func TestWatchDirCap_EnvOverride(t *testing.T) {
	t.Setenv("GRAFEL_WATCH_DIR_CAP", "42")
	if got := WatchDirCap(); got != 42 {
		t.Errorf("WatchDirCap() = %d, want 42", got)
	}
	t.Setenv("GRAFEL_WATCH_DIR_CAP", "")
	if got := WatchDirCap(); got != DefaultWatchDirCap {
		t.Errorf("WatchDirCap() default = %d, want %d", got, DefaultWatchDirCap)
	}
	t.Setenv("GRAFEL_WATCH_DIR_CAP", "not-a-number")
	if got := WatchDirCap(); got != DefaultWatchDirCap {
		t.Errorf("WatchDirCap() bad value = %d, want default %d", got, DefaultWatchDirCap)
	}
}
