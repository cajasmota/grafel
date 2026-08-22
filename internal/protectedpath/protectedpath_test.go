package protectedpath

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMediaLibraryBundle(t *testing.T) {
	cases := map[string]bool{
		"foo.musiclibrary":         true,
		"My Library.photoslibrary": true,
		"recordings.tvlibrary":     true,
		"Old.aplibrary":            true,
		"node_modules":             false,
		"music":                    false,
	}
	for name, want := range cases {
		if got := IsMediaLibraryBundle(name); got != want {
			t.Errorf("IsMediaLibraryBundle(%q) = %v, want %v", name, got, want)
		}
	}
}

// The union table must carry BOTH former denylists. daemon/walk's list omitted
// Desktop/Documents/Downloads — exactly the iCloud "Desktop & Documents"
// folders behind #6548.
func TestUnionTable(t *testing.T) {
	for _, name := range []string{
		"Desktop", "Documents", "Downloads", "Library",
		"Movies", "Music", "Photos", "Pictures", "Public",
	} {
		if !IsProtectedHomeDir(name) {
			t.Errorf("IsProtectedHomeDir(%q) = false, want true (union of both denylists)", name)
		}
	}
	for _, name := range []string{"Projects", "src", "code", "Documentation", "Desktops"} {
		if IsProtectedHomeDir(name) {
			t.Errorf("IsProtectedHomeDir(%q) = true, want false", name)
		}
	}
}

// Walk semantics (registered-repo walk, #5296) must NOT regress: the walk
// refuses only the media classes, so an explicitly registered repo under
// ~/Documents still indexes.
func TestWalkProtected_MediaOnly(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"Music", "Photos", "Movies", "Pictures", "Library"} {
		p := filepath.Join(home, name, "sub")
		got, reason := IsWalkProtectedIn(p, home, "darwin")
		if !got {
			t.Errorf("IsWalkProtectedIn(%q) = false, want true", p)
		}
		if !strings.Contains(reason, name) {
			t.Errorf("reason %q does not name %q", reason, name)
		}
	}
	// The user pointed grafel at this repo explicitly: the walk must proceed.
	for _, name := range []string{"Documents", "Desktop", "Downloads", "Public"} {
		p := filepath.Join(home, name, "myrepo")
		if got, _ := IsWalkProtectedIn(p, home, "darwin"); got {
			t.Errorf("IsWalkProtectedIn(%q) = true; an explicitly registered repo must still index", p)
		}
	}
	// Prefix-only sibling must not match (component-wise).
	if got, _ := IsWalkProtectedIn(filepath.Join(home, "MusicStudio"), home, "darwin"); got {
		t.Error("MusicStudio must not be treated as under ~/Music")
	}
	// Off darwin the home checks do not apply.
	if got, _ := IsWalkProtectedIn(filepath.Join(home, "Music", "x"), home, "linux"); got {
		t.Error("~/Music must not be protected off darwin")
	}
	// Bundles are caught on every platform.
	if got, _ := IsWalkProtectedIn(filepath.Join(t.TempDir(), "L.photoslibrary"), home, runtime.GOOS); !got {
		t.Error("media-library bundle must be protected on any platform")
	}
}

func TestWalkProtected_SymlinkIntoMusic(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on windows")
	}
	home := t.TempDir()
	target := filepath.Join(home, "Music", "secret-repo")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "myrepo")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot symlink: %v", err)
	}
	got, reason := IsWalkProtectedIn(link, home, "darwin")
	if !got {
		t.Fatal("symlink into ~/Music must be protected")
	}
	if !strings.Contains(reason, "~/Music") {
		t.Errorf("reason = %q, want ~/Music", reason)
	}
}

// Inferred traversal (canonicalizePath, sibling scans) uses the FULL union:
// nothing inferred may read into Desktop/Documents/Downloads either.
func TestTraversalProtected_Union(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{
		"Desktop", "Documents", "Downloads", "Library",
		"Movies", "Music", "Photos", "Pictures", "Public",
	} {
		p := filepath.Join(home, name)
		if got, _ := IsTraversalProtectedIn(p, home, "darwin"); !got {
			t.Errorf("IsTraversalProtectedIn(%q) = false, want true", p)
		}
		if got, _ := IsTraversalProtectedIn(filepath.Join(p, "deep"), home, "darwin"); !got {
			t.Errorf("IsTraversalProtectedIn(%q/deep) = false, want true", p)
		}
	}
	// iCloud Drive proper.
	icloud := filepath.Join(home, "Library", "Mobile Documents")
	if got, _ := IsTraversalProtectedIn(icloud, home, "darwin"); !got {
		t.Error("~/Library/Mobile Documents must be protected")
	}
}

// PERMISSIVE-DIRECTION GUARD (#6548): matching a substring instead of a path
// component would silently refuse legitimate repos. These must all be allowed.
func TestTraversalProtected_NotPermissive(t *testing.T) {
	home := t.TempDir()
	allowed := []string{
		filepath.Join(home, "Projects", "Documentation"),
		filepath.Join(home, "Projects", "Documentation", "src"),
		filepath.Join(home, "Documents-old"),
		filepath.Join(home, "MyDocuments"),
		filepath.Join(home, "DesktopApps"),
		filepath.Join(home, "Downloads2"),
		filepath.Join(home, "src", "Library", "code"), // not a HOME child
		filepath.Join(home, "PublicAPI"),
		home,
		filepath.Dir(home),
	}
	for _, p := range allowed {
		if got, reason := IsTraversalProtectedIn(p, home, "darwin"); got {
			t.Errorf("IsTraversalProtectedIn(%q) = true (%s); legitimate path must not be refused", p, reason)
		}
	}
	// Off darwin nothing in the home set is protected.
	if got, _ := IsTraversalProtectedIn(filepath.Join(home, "Documents"), home, "linux"); got {
		t.Error("~/Documents must not be protected off darwin")
	}
	// No home resolvable → no home-based refusal.
	if got, _ := IsTraversalProtectedIn(filepath.Join(home, "Documents"), "", "darwin"); got {
		t.Error("with no home, nothing is home-protected")
	}
}

func TestProtectedHomeChildAndScanParent(t *testing.T) {
	home := t.TempDir()
	if !IsProtectedHomeChildIn(home, "Documents", home, "darwin") {
		t.Error("Documents under $HOME must be a protected child")
	}
	if IsProtectedHomeChildIn(filepath.Join(home, "Projects"), "Documents", home, "darwin") {
		t.Error("a folder named Documents elsewhere must not be a protected child")
	}
	if IsProtectedHomeChildIn(home, "Projects", home, "darwin") {
		t.Error("Projects must not be a protected child")
	}
	if IsProtectedHomeChildIn(home, "Documents", home, "linux") {
		t.Error("no TCC off darwin")
	}

	if !IsProtectedScanParentIn(home, home, "darwin") {
		t.Error("$HOME itself must be a protected scan parent")
	}
	if !IsProtectedScanParentIn(filepath.Join(home, "Documents", "x"), home, "darwin") {
		t.Error("inside ~/Documents must be a protected scan parent")
	}
	if IsProtectedScanParentIn(filepath.Join(home, "Projects"), home, "darwin") {
		t.Error("~/Projects must not be a protected scan parent")
	}
	// Substring-not-component guards for the scan-parent predicate too.
	for _, name := range []string{"Documentation", "Documents-old", "MyDocuments", "DesktopApps", "PublicAPI", "Downloads2"} {
		if IsProtectedScanParentIn(filepath.Join(home, name), home, "darwin") {
			t.Errorf("~/%s must not be a protected scan parent (component match, not substring)", name)
		}
	}
	if IsProtectedScanParentIn(home, home, "linux") {
		t.Error("no TCC off darwin")
	}
}
