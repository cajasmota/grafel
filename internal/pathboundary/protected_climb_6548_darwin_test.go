package pathboundary

import (
	"os"
	"path/filepath"
	"testing"
)

// #6548 arm 3, macOS class. ~/Documents, ~/Desktop, ~/Downloads and ~/Library
// are TCC-gated only on darwin, so the ~/Documents form of the boundary is
// pinned here. The home is an INJECTED TempDir; the real ~/Documents is never
// read — reading it is the bug.

// TestClimb_RefusesProtectedHomeFolder is the killing test for the macOS TCC
// class: climbing from a directory under ~/Documents must stop at ~/Documents
// without reading it, and must never reach $HOME above it.
func TestClimb_RefusesProtectedHomeFolder(t *testing.T) {
	root := t.TempDir()
	home := mk(t, filepath.Join(root, "home", "u"))
	docs := mk(t, filepath.Join(home, "Documents"))
	start := mk(t, filepath.Join(docs, "proj", "pkg"))

	seen := visited(t, start, home)
	for _, d := range seen {
		if samePath(d, docs) {
			t.Fatalf("Climb VISITED ~/Documents (site: internal/pathboundary/pathboundary.go Climb): %q is IsTraversalProtected and is exactly the folder iCloud \"Desktop & Documents\" sync makes file-provider managed; visited %v", d, seen)
		}
		if samePath(d, home) {
			t.Fatalf("Climb climbed PAST ~/Documents up to $HOME (site: internal/pathboundary/pathboundary.go Climb): visited %v", seen)
		}
	}
	if len(seen) != 2 {
		t.Fatalf("climb should visit exactly proj/pkg and proj below ~/Documents, visited %v", seen)
	}
}

// TestClimb_RepoUnderDocumentsStillResolvesItsOwnAncestors is the
// explicit-path exemption: a repo the user deliberately keeps in ~/Documents
// must keep resolving its own markers. Only ~/Documents itself is the boundary.
func TestClimb_RepoUnderDocumentsStillResolvesItsOwnAncestors(t *testing.T) {
	root := t.TempDir()
	home := mk(t, filepath.Join(root, "home", "u"))
	repo := mk(t, filepath.Join(home, "Documents", "proj"))
	start := mk(t, filepath.Join(repo, "pkg", "sub"))

	if !ClimbWithHome(start, home, func(dir string) bool { return samePath(dir, repo) }) {
		t.Fatalf("a repo under ~/Documents lost its own ancestor climb: %q unreachable from %q (visited %v). The exemption at pathboundary.go doc lines 39-41 requires this to work", repo, start, visited(t, start, home))
	}
}

// TestClimb_UnprotectedHomeFolderIsNotABoundary — too-strict guard: only the
// denylisted names are protected. ~/src must not become a boundary.
func TestClimb_UnprotectedHomeFolderIsNotABoundary(t *testing.T) {
	root := t.TempDir()
	home := mk(t, filepath.Join(root, "home", "u"))
	start := mk(t, filepath.Join(home, "src", "repo"))

	if !ClimbWithHome(start, home, func(dir string) bool { return samePath(dir, home) }) {
		t.Fatalf("$HOME became unreachable through an UNPROTECTED folder ~/src: visited %v", visited(t, start, home))
	}
}

// TestClimb_RefusesICloudDriveOutright — review finding on arm 3's first cut:
// an "outermost directory of a protected tree" rule applied uniformly makes
// ~/Library the refusal point, which DISARMS the iCloud entry nested inside it.
// A climb starting in ~/Library/Mobile Documents then still visited
// com~apple~CloudDocs and Mobile Documents themselves — the exact read behind
// «"grafel" wants to access files managed by "iCloud Drive"».
//
// ~/Library is classMedia in protectedpath's table, and the media class is
// refused AT OR UNDER, so nothing inside iCloud Drive is ever visited.
func TestClimb_RefusesICloudDriveOutright(t *testing.T) {
	root := t.TempDir()
	home := mk(t, filepath.Join(root, "home", "u"))
	icloud := mk(t, filepath.Join(home, "Library", "Mobile Documents", "com~apple~CloudDocs"))
	start := mk(t, filepath.Join(icloud, "notes", "proj"))

	seen := visited(t, start, home)
	if len(seen) != 0 {
		t.Fatalf("Climb READ inside iCloud Drive (site: internal/pathboundary/pathboundary.go Climb): visited %v. ~/Library is classMedia and must be refused at-or-under, so a climb starting inside ~/Library/Mobile Documents visits nothing at all", seen)
	}
}

// TestClimb_RefusesMediaBundleUnderPictures — the second half of the same
// finding. protectedpath's own doc says classMedia is refused EVEN FOR AN
// EXPLICITLY REGISTERED REPO ROOT (#5296: descending into a *.photoslibrary
// pops the Photos prompt), while classTCC gets the explicit-path exemption. A
// uniform outermost rule silently extended the TCC exemption to media: the
// bundle was visited and only ~/Pictures stopped the climb.
func TestClimb_RefusesMediaBundleUnderPictures(t *testing.T) {
	root := t.TempDir()
	home := mk(t, filepath.Join(root, "home", "u"))
	bundle := mk(t, filepath.Join(home, "Pictures", "Mine.photoslibrary"))
	start := mk(t, filepath.Join(bundle, "originals", "0"))

	seen := visited(t, start, home)
	if len(seen) != 0 {
		t.Fatalf("Climb READ inside a media-library bundle under ~/Pictures (site: internal/pathboundary/pathboundary.go Climb): visited %v. classMedia is refused at-or-under — the explicit-path exemption is classTCC only", seen)
	}
}

// TestClimb_RefusesSymlinkIntoProtectedTree kills the mutant that survived the
// first cut: replacing the classifier's Inside(dir, home) gate with a purely
// lexical containment check deletes the symlink arm, and a link OUTSIDE $HOME
// resolving into ~/Documents becomes freely climbable. Catching exactly that is
// why protectedpath resolves symlinks at all.
func TestClimb_RefusesSymlinkIntoProtectedTree(t *testing.T) {
	root := t.TempDir()
	home := mk(t, filepath.Join(root, "home", "u"))
	docs := mk(t, filepath.Join(home, "Documents"))
	work := mk(t, filepath.Join(root, "work"))

	link := filepath.Join(work, "link")
	if err := os.Symlink(docs, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	seen := visited(t, link, home)
	for _, d := range seen {
		if samePath(d, link) {
			t.Fatalf("Climb visited %q, a symlink OUTSIDE $HOME resolving to the protected %q (site: internal/pathboundary/pathboundary.go classifyDir): a lexical-only containment gate misses this entirely; visited %v", d, docs, seen)
		}
	}
	if len(seen) != 0 {
		t.Fatalf("Climb continued past a symlink into a protected tree: visited %v, want nothing", seen)
	}
}

// TestClimb_SymlinkIntoAnUnprotectedTreeStillClimbs — permissive-direction
// guard on the symlink arm: resolving links must not turn every link into a
// boundary.
func TestClimb_SymlinkIntoAnUnprotectedTreeStillClimbs(t *testing.T) {
	root := t.TempDir()
	home := mk(t, filepath.Join(root, "home", "u"))
	target := mk(t, filepath.Join(home, "src", "repo"))
	work := mk(t, filepath.Join(root, "work"))

	link := filepath.Join(work, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if !ClimbWithHome(link, home, func(dir string) bool { return samePath(dir, work) }) {
		t.Fatalf("a symlink into an UNPROTECTED tree became a boundary: %q unreachable from %q (visited %v)", work, link, visited(t, link, home))
	}
}
