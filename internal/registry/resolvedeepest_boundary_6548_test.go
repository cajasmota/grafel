package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/testsupport"
)

// #6548 arm 3 — resolveDeepestExisting (internal/registry/registry.go), reached
// from PathContainedUnder on every registry path validation, was an unbounded
// ascend-until-something-resolves loop: no depth cap, no home stop, one
// filepath.EvalSymlinks per level, running in the daemon unprompted.
//
// The fixture lives entirely under t.TempDir() with an injected home. Nothing
// under the developer's real home is read.

// missingHome6548 points HOME at a path inside the sandbox that does not
// exist: an existing home resolves on the first probe and hides the boundary.
func missingHome6548(t *testing.T) (root, home string) {
	t.Helper()
	root = testsupport.IsolateHome(t)
	home = filepath.Join(root, "link", "home", "u")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return root, home
}

// symlinkedRoot6548 builds <root>/real plus a <root>/link symlink to it so the
// resolved and unresolved spellings differ; without that the boundary is
// invisible to a string comparison.
func symlinkedRoot6548(t *testing.T, root string) string {
	t.Helper()
	target := filepath.Join(root, "real")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", target, err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	return link
}

// TestResolveDeepestExisting_StopsAtHome is the killing test for the registry site.
func TestResolveDeepestExisting_StopsAtHome(t *testing.T) {
	root, home := missingHome6548(t)
	symlinkedRoot6548(t, root)

	// Nothing at or below $HOME exists, so resolving p requires climbing ABOVE
	// $HOME to <root>/link — the traversal the boundary forbids. With the
	// boundary in force nothing resolves and p is returned unchanged, which is
	// resolveDeepestExisting's documented "nothing resolved" contract.
	p := filepath.Join(home, "w", "x")
	if got := resolveDeepestExisting(p); got != p {
		t.Fatalf("resolveDeepestExisting climbed past $HOME (site: internal/registry/registry.go resolveDeepestExisting): got %q, want %q unchanged (home %q)", got, p, home)
	}
}

// TestResolveDeepestExisting_StillResolvesBelowTheBoundary is the
// permissive-direction guard: the loop must keep doing its job — replacing the
// longest existing ancestor with its symlink-resolved form and re-appending the
// missing tail — whenever that ancestor is reachable without crossing $HOME.
func TestResolveDeepestExisting_StillResolvesBelowTheBoundary(t *testing.T) {
	root, _ := missingHome6548(t)
	link := symlinkedRoot6548(t, root)

	p := filepath.Join(link, "not", "created", "yet")
	got := resolveDeepestExisting(p)
	want := filepath.Join(filepath.Join(root, "real"), "not", "created", "yet")
	// The sandbox root itself may sit behind a symlink (/var → /private/var on
	// macOS), so compare against the resolved form of the target.
	if resolvedTarget, err := filepath.EvalSymlinks(filepath.Join(root, "real")); err == nil {
		want = filepath.Join(resolvedTarget, "not", "created", "yet")
	}
	if got != want {
		t.Fatalf("resolveDeepestExisting stopped resolving below the boundary: got %q, want %q", got, want)
	}
}

// TestPathContainedUnder_StillWorksInsideTheStore — the boundary must not
// change the answer for the ordinary case this predicate exists for: a state
// directory under an existing root, whether or not it exists yet.
func TestPathContainedUnder_StillWorksInsideTheStore(t *testing.T) {
	root, _ := missingHome6548(t)
	store := filepath.Join(root, "store")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if !PathContainedUnder(store, filepath.Join(store, "groups", "g1")) {
		t.Fatalf("PathContainedUnder lost a plain containment answer under %q", store)
	}
	if PathContainedUnder(store, filepath.Join(root, "elsewhere")) {
		t.Fatalf("PathContainedUnder said an outside path is contained under %q", store)
	}
	if PathContainedUnder(store, store) {
		t.Fatalf("PathContainedUnder must not report the root as contained in itself")
	}
}
