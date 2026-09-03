package gitmeta

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/pathboundary"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// #6548 — HasGitDirInTree climbed to the filesystem root when no .git exists
// anywhere above the start path, walking through $HOME on the way. Fixtures are
// built under t.TempDir() with an injected fake home; the real home is never read.

// fakeHomeUnder6548 isolates the whole environment (testsupport.IsolateHome)
// and then re-points the home at a NESTED dir inside that sandbox, so a fixture
// can plant a marker ABOVE the home without leaving TempDir. Returns
// (sandboxRoot, home).
func fakeHomeUnder6548(t *testing.T, sub ...string) (string, string) {
	t.Helper()
	root := testsupport.IsolateHome(t)
	home := mkdir6548(t, filepath.Join(append([]string{root}, sub...)...))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("GRAFEL_HOME", filepath.Join(home, ".grafel-store"))
	return root, home
}

func mkdir6548(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	return path
}

// TestHasGitDirInTree_StopsAtHome is the killing test for the gitmeta site.
func TestHasGitDirInTree_StopsAtHome(t *testing.T) {
	root, home := fakeHomeUnder6548(t, "home", "u")

	// A .git ABOVE $HOME must be invisible to the climb.
	mkdir6548(t, filepath.Join(root, ".git"))

	start := mkdir6548(t, filepath.Join(home, "work", "repo", "pkg"))
	if HasGitDirInTree(start) {
		t.Fatalf("HasGitDirInTree climbed past $HOME (site: internal/gitmeta/gitmeta.go HasGitDirInTree): found .git at %s above home %s", root, home)
	}
}

// TestHasGitDirInTree_FindsGitAtHome — permissive-direction guard: a repo cloned
// straight into $HOME must still be recognised.
func TestHasGitDirInTree_FindsGitAtHome(t *testing.T) {
	_, home := fakeHomeUnder6548(t, "home", "u")

	mkdir6548(t, filepath.Join(home, ".git"))

	start := mkdir6548(t, filepath.Join(home, "pkg", "sub"))
	if !HasGitDirInTree(start) {
		t.Fatalf("HasGitDirInTree stopped too early: .git at $HOME %s was not found from %s", home, start)
	}
}

// TestHasGitDirInTree_OutsideHomeStillClimbs — a start path outside every home
// keeps climbing; a boundary that fired on any ancestor at all would break
// repos under /opt, /var/folders or a CI checkout root.
//
// #6548 requirement 3 (owner decision 2026-09-02) INVERTED half of this test.
// It used to start under <root>/Users/someone — a sibling of the current
// user's home — and assert the .git above the Users level was still found.
// That case is now a refusal, and is asserted as one below; the
// still-climbs half moves to a non-home root, which is what it was really for.
func TestHasGitDirInTree_OutsideHomeStillClimbs(t *testing.T) {
	root, home := fakeHomeUnder6548(t, "Users", "me")
	// $HOME is no longer consulted by the other-user-home class (it can be
	// laundered), so the fixture's home is injected through the seam.
	t.Cleanup(pathboundary.OverrideHomeReferences(home))

	mkdir6548(t, filepath.Join(root, ".git"))
	start := mkdir6548(t, filepath.Join(root, "srv", "ci", "src", "deep"))
	if !HasGitDirInTree(start) {
		t.Fatalf("HasGitDirInTree stopped before a legitimate .git outside every home (start %s)", start)
	}

	// The inverted case: the same .git, reached from a SIBLING HOME, must not
	// be found — the climb never enters /Users/someone at all.
	sibling := mkdir6548(t, filepath.Join(root, "Users", "someone", "src", "deep"))
	if HasGitDirInTree(sibling) {
		t.Fatalf("HasGitDirInTree climbed out of another user's home %s (site: internal/gitmeta/gitmeta.go HasGitDirInTree)",
			filepath.Join(root, "Users", "someone"))
	}
}

// TestHasGitDirInTree_StartingExactlyAtHomeStopsThere pins the boundary
// end-to-end for the one start path that IS the boundary: running a grafel
// command with the working directory set to the user's home. If a climb that
// begins at $HOME were exempt from the home stop, this call would Stat $HOME,
// then /Users (or /home), then / — the exact traversal #6548 exists to stop,
// one case short.
func TestHasGitDirInTree_StartingExactlyAtHomeStopsThere(t *testing.T) {
	root, home := fakeHomeUnder6548(t, "home", "u")

	// A .git ABOVE $HOME must stay invisible even when $HOME is the start.
	mkdir6548(t, filepath.Join(root, ".git"))

	if HasGitDirInTree(home) {
		t.Fatalf("HasGitDirInTree climbed past $HOME when the START path IS $HOME: found .git at %s above home %s", root, home)
	}
}

// TestHasGitDirInTree_StartingAtHomeFindsGitAtHome — permissive-direction guard:
// bounding the climb at the start directory must not skip visiting it. A repo
// cloned straight into $HOME is still recognised when $HOME is the cwd.
func TestHasGitDirInTree_StartingAtHomeFindsGitAtHome(t *testing.T) {
	_, home := fakeHomeUnder6548(t, "home", "u")

	mkdir6548(t, filepath.Join(home, ".git"))

	if !HasGitDirInTree(home) {
		t.Fatalf("HasGitDirInTree missed .git AT $HOME %s when starting there", home)
	}
}
