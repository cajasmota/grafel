package daemon

import (
	"path/filepath"
	"testing"
)

// TestWorktreeParentSeedMismatch_pins5964Bug pins the EXACT mismatch that
// killed the first clone-from-parent attempt (#3652, deleted in #65427c5a8):
// that code resolved the parent via
// StateDirForRepoRef(worktreePath, "main") — keyed by the WORKTREE's own
// absolute path (wrong store root: a worktree and its parent hash to
// different roots) AND a hardcoded ref name (wrong ref whenever the parent's
// HEAD isn't literally "main"). Both mistakes independently guarantee a
// miss.
//
// This test asserts the two dirs a correct implementation must treat as
// distinct, so a future change can't silently reintroduce either mistake:
//   - dst = StateDirForRepoRef(worktreePath, branch)   — unchanged Enqueue target
//   - src = StateDirForRepo(parentPath)                — the parent's ACTUAL
//     HEAD ref, not a hardcoded "main"
//
// and that the broken lookup (worktree path + hardcoded "main") never
// coincides with the correct src.
func TestWorktreeParentSeedMismatch_pins5964Bug(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	parentPath := filepath.Join(root, "parent-repo")
	worktreePath := filepath.Join(root, "worktree", "agent-foo")
	branch := "worktree-agent-foo"

	// The two roots the original attempt conflated: a worktree and its
	// parent hash to DIFFERENT store roots because RepoBaseDir hashes the
	// absolute repo path, not any shared git-common-dir fact.
	if RepoBaseDir(parentPath) == RepoBaseDir(worktreePath) {
		t.Fatalf("parent and worktree must hash to different store roots; got the same: %q", RepoBaseDir(parentPath))
	}

	// dst: unchanged Enqueue target, keyed by the WORKTREE path + its branch.
	dst := StateDirForRepoRef(worktreePath, branch)

	// src (correct): keyed by the PARENT path, at the parent's ACTUAL HEAD
	// ref (gitmeta.Capture on a non-existent dir returns Ref == "", which
	// RefSafeEncode maps to the "_unknown" sentinel — still a real,
	// deterministic dir, just not "main").
	src := StateDirForRepo(parentPath)

	// src (the #3652 bug): worktree path + hardcoded "main".
	buggySrc := StateDirForRepoRef(worktreePath, "main")

	if src == dst {
		t.Fatalf("src must differ from dst (would mean parent and worktree state dirs collided): %q", src)
	}
	if src == buggySrc {
		t.Fatalf("src must differ from the #3652 buggy lookup (worktree path + hardcoded main): both resolved to %q", src)
	}
	if buggySrc != dst {
		// This is the precise shape of the original bug: the buggy lookup
		// (worktree path + "main") is NOT the parent's dir at all — it's a
		// sibling ref dir under the WORKTREE's own (wrong) store root, which
		// is why it was always empty: nothing ever writes a graph there
		// under the ref name "main" for a worktree checked out on a
		// differently-named branch.
		t.Logf("buggy lookup %q != correct dst %q (expected — demonstrates the wrong root)", buggySrc, dst)
	}

	// Sanity: src is actually rooted under the parent's own hashed base dir,
	// not the worktree's.
	parentBase := RepoBaseDir(parentPath)
	if filepath.Dir(filepath.Dir(src)) != parentBase { // strip /refs/<ref>
		t.Fatalf("src %q is not under parent's base dir %q", src, parentBase)
	}
}
