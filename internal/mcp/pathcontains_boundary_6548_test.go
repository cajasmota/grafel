package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/testsupport"
)

// #6548 arm 3 — pathContains' fallback "resolve the deepest existing ancestor"
// loop (internal/mcp/routing.go pathContains) was the last UNBOUNDED climb on
// the MCP hot path: no depth cap, no home stop, root-only, one
// filepath.EvalSymlinks per level. It runs on every cwd→repo containment check,
// unprompted, whenever the cwd does not exist on disk yet.
//
// The fixture is entirely inside t.TempDir() with an injected home; nothing
// under the developer's real home is read.

// fakeMissingHome6548 isolates the environment and points HOME at a path
// INSIDE the sandbox that does not exist. An existing home resolves on the
// first probe and hides the boundary entirely, so the boundary is only
// observable when nothing at or below $HOME resolves.
func fakeMissingHome6548(t *testing.T) (root, home string) {
	t.Helper()
	root = testsupport.IsolateHome(t)
	home = filepath.Join(root, "link", "home", "u")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return root, home
}

// symlinkedRoot6548 builds <root>/real and a <root>/link symlink to it, so the
// resolved and unresolved spellings of the same path differ on every platform
// (without one, a lexical prefix check passes and the boundary is unobservable).
func symlinkedRoot6548(t *testing.T, root string) string {
	t.Helper()
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", real, err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	return link
}

// TestPathContains_StopsAtHome is the killing test for the pathContains site.
func TestPathContains_StopsAtHome(t *testing.T) {
	root, home := fakeMissingHome6548(t)
	link := symlinkedRoot6548(t, root)

	// A non-existent child, lexically inside $HOME. Resolving it requires
	// climbing ABOVE $HOME to <root>/link — which is exactly what the boundary
	// forbids.
	child := filepath.Join(home, "w", "x")

	if pathContains(link, child) {
		t.Fatalf("pathContains climbed past $HOME (site: internal/mcp/routing.go pathContains): it resolved %q by walking up to %q, above home %q", child, link, home)
	}
}

// TestPathContains_ExistingPathsStillContained is the permissive-direction
// guard: bounding the fallback loop must not break the ordinary answer, where
// both sides exist and resolve on the first probe.
func TestPathContains_ExistingPathsStillContained(t *testing.T) {
	root, home := fakeMissingHome6548(t)
	_ = home
	ancestor := filepath.Join(root, "src")
	child := filepath.Join(ancestor, "repo", "pkg")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if !pathContains(ancestor, child) {
		t.Fatalf("pathContains lost a plain containment answer: %q must contain %q", ancestor, child)
	}
	if pathContains(child, ancestor) {
		t.Fatalf("pathContains inverted: %q must NOT contain %q", child, ancestor)
	}
}

// TestPathContains_MissingChildUnderExistingAncestorStillResolves — the other
// permissive-direction guard: the fallback loop must still do its job when the
// deepest existing ancestor is reachable WITHOUT crossing the boundary.
func TestPathContains_MissingChildUnderExistingAncestorStillResolves(t *testing.T) {
	root, _ := fakeMissingHome6548(t)
	link := symlinkedRoot6548(t, root)

	// Outside $HOME entirely, so the home stop never applies; the child does
	// not exist and must still be recognised as contained by <root>/link.
	child := filepath.Join(link, "not", "created", "yet")
	if !pathContains(link, child) {
		t.Fatalf("pathContains stopped resolving a missing child under an existing ancestor: %q must contain %q", link, child)
	}
}
