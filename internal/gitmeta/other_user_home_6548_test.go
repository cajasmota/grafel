package gitmeta

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/pathboundary"
)

// #6548 requirement 3, NON-VACUITY. The boundary is only real if it refuses on
// a REAL traversal path — extracting a predicate and unit-testing it does not
// pin the call site (#6533). HasGitDirInTree is a production climber that does
// an os.Stat of <ancestor>/.git per level, so its answer is a direct
// observation of WHAT WAS READ: the .git exists, and the only way to report
// false is to never have looked.
//
// Site named by these tests: internal/gitmeta/gitmeta.go HasGitDirInTree, via
// pathboundary.Climb.

func mkdir(t *testing.T, p string) string {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
	return p
}

// TestHasGitDirInTree_RefusesAnotherUsersHome — a git checkout sitting in
// another user's home must not be discovered, even though it is right there.
func TestHasGitDirInTree_RefusesAnotherUsersHome(t *testing.T) {
	users := mkdir(t, filepath.Join(t.TempDir(), "Users"))
	alice := mkdir(t, filepath.Join(users, "alice"))
	bob := mkdir(t, filepath.Join(users, "bob"))

	restore := pathboundary.OverrideHomeReferences(alice, alice)
	defer restore()

	repo := mkdir(t, filepath.Join(bob, "proj"))
	mkdir(t, filepath.Join(repo, ".git"))
	start := mkdir(t, filepath.Join(repo, "internal", "pkg"))

	if HasGitDirInTree(start) {
		t.Fatalf("HasGitDirInTree read another user's home (site: internal/gitmeta/gitmeta.go HasGitDirInTree): found %q while the current user's home is %q",
			filepath.Join(repo, ".git"), alice)
	}

	// UNDER-FIRING CONTROL: the identical layout inside the CURRENT user's own
	// home must still resolve. A boundary that refuses unconditionally passes
	// the assertion above and fails this one.
	ownRepo := mkdir(t, filepath.Join(alice, "proj"))
	mkdir(t, filepath.Join(ownRepo, ".git"))
	ownStart := mkdir(t, filepath.Join(ownRepo, "internal", "pkg"))
	if !HasGitDirInTree(ownStart) {
		t.Fatalf("HasGitDirInTree failed to find %q inside the current user's own home — the boundary refuses everything",
			filepath.Join(ownRepo, ".git"))
	}
}

// TestHasGitDirInTree_SiblingHomeIsNotAPrefixMatch — /Users/alice2 is not
// inside /Users/alice, on a real traversal path.
func TestHasGitDirInTree_SiblingHomeIsNotAPrefixMatch(t *testing.T) {
	users := mkdir(t, filepath.Join(t.TempDir(), "Users"))
	alice := mkdir(t, filepath.Join(users, "alice"))
	alice2 := mkdir(t, filepath.Join(users, "alice2"))

	restore := pathboundary.OverrideHomeReferences(alice, alice)
	defer restore()

	repo := mkdir(t, filepath.Join(alice2, "proj"))
	mkdir(t, filepath.Join(repo, ".git"))

	if HasGitDirInTree(mkdir(t, filepath.Join(repo, "sub"))) {
		t.Fatalf("prefix comparison on a real traversal path: %q was treated as inside %q", alice2, alice)
	}
}
