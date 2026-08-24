package pathboundary

import (
	"path/filepath"
	"testing"
)

// #6548 arm 3, macOS class. ~/Documents, ~/Desktop, ~/Downloads and ~/Library
// are TCC-gated only on darwin, so the ~/Documents form of the boundary is
// pinned here. The home is an INJECTED TempDir; the real ~/Documents is never
// read — reading it is the bug.

// TestClimb_RefusesProtectedHomeFolder is the killing test for the macOS class:
// climbing from a directory under ~/Documents must stop at ~/Documents without
// reading it, and must never reach $HOME above it.
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
