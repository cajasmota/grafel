package pathboundary

import (
	"path/filepath"
	"testing"
)

// #6548 arm 3 — the climb must consult protectedpath.IsTraversalProtected and
// refuse to READ a protected directory, not merely stop at $HOME and the root.
// #6549 built the table and #6550 built the bounded loop; until this file went
// green they never met, and a climb whose ancestor chain passed through
// ~/Library/Mobile Documents or a *.photoslibrary bundle still visited it.
//
// Every fixture is a TempDir with an INJECTED home (ClimbWithHome). The media
// -library-bundle class is platform-independent in protectedpath, so the
// killing test here runs everywhere; the macOS-only ~/Documents class is
// pinned in protected_climb_6548_darwin_test.go.

// TestClimb_RefusesProtectedAncestor is the killing test for the Climb site:
// a *.photoslibrary bundle on the ancestor chain is a boundary. The climb must
// neither visit it nor anything above it.
func TestClimb_RefusesProtectedAncestor(t *testing.T) {
	root := t.TempDir()
	home := mk(t, filepath.Join(root, "home", "u"))
	bundle := mk(t, filepath.Join(root, "lib", "My.photoslibrary"))
	start := mk(t, filepath.Join(bundle, "album", "deep"))

	seen := visited(t, start, home)

	for _, d := range seen {
		if samePath(d, bundle) {
			t.Fatalf("Climb VISITED a protected directory (site: internal/pathboundary/pathboundary.go Climb): %q is a media-library bundle and IsTraversalProtected says so; visited %v", d, seen)
		}
		if samePath(d, filepath.Join(root, "lib")) || samePath(d, root) {
			t.Fatalf("Climb climbed PAST a protected directory (site: internal/pathboundary/pathboundary.go Climb): reached %q above the protected bundle %q; visited %v", d, bundle, seen)
		}
	}
	// And it really did run: the two levels below the bundle are still visited.
	if len(seen) != 2 {
		t.Fatalf("climb inside the protected tree should visit exactly the 2 levels below the bundle, visited %v", seen)
	}
}

// TestClimb_ProtectedAncestorMakesTheClimbFail — a marker above the protected
// boundary must be unreachable, i.e. the refusal is observable through Climb's
// own return value, not only through the visit log.
func TestClimb_ProtectedAncestorMakesTheClimbFail(t *testing.T) {
	root := t.TempDir()
	home := mk(t, filepath.Join(root, "home", "u"))
	bundle := mk(t, filepath.Join(root, "lib", "Shared.musiclibrary"))
	start := mk(t, filepath.Join(bundle, "x"))

	marker := filepath.Join(root, "lib")
	found := ClimbWithHome(start, home, func(dir string) bool { return samePath(dir, marker) || samePath(dir, bundle) })
	if found {
		t.Fatalf("Climb reached a protected directory or beyond (site: internal/pathboundary/pathboundary.go Climb): bundle %q must end the climb", bundle)
	}
}

// TestClimb_ProtectedRefusalIsTheOutermostDirectoryOnly is the
// permissive-direction guard on the GRANULARITY, and the explicit-path
// exemption in miniature: a climb that STARTS inside a protected tree — the
// user pointed grafel at a repo there — must still resolve its own ancestors
// up to the protected root. Refusing every at-or-under path would break that
// repo; refusing none would read the protected directory itself.
func TestClimb_ProtectedRefusalIsTheOutermostDirectoryOnly(t *testing.T) {
	root := t.TempDir()
	home := mk(t, filepath.Join(root, "home", "u"))
	bundle := mk(t, filepath.Join(root, "lib", "My.photoslibrary"))
	repo := mk(t, filepath.Join(bundle, "repo"))
	start := mk(t, filepath.Join(repo, "pkg", "sub"))

	found := ClimbWithHome(start, home, func(dir string) bool { return samePath(dir, repo) })
	if !found {
		t.Fatalf("the protected refusal cut a climb that STARTS inside the protected tree: %q was never visited from %q (visited %v). Only the outermost protected directory is the boundary", repo, start, visited(t, start, home))
	}
}

// TestClimb_UnprotectedClimbIsUnaffected — the too-strict mutant guard: adding
// the protected check must not shorten an ordinary climb.
func TestClimb_UnprotectedClimbIsUnaffected(t *testing.T) {
	root := t.TempDir()
	home := mk(t, filepath.Join(root, "home", "u"))
	start := mk(t, filepath.Join(home, "work", "repo", "pkg"))

	seen := visited(t, start, home)
	if len(seen) != 4 || !samePath(seen[len(seen)-1], home) {
		t.Fatalf("an ordinary climb must still run start→$HOME: visited %v, want 4 levels ending at %q", seen, home)
	}
}
