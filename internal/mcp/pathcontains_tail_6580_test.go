package mcp

import (
	"path/filepath"
	"testing"
)

// #6580 — pathContains' fallback rebuilt the child path as
// filepath.Join(resolved, filepath.Base(below)). filepath.Base keeps only the
// LAST component, so a climb that ascended more than one level silently
// dropped every intermediate component: link/not/created/yet came back as
// <root>/real/not.
//
// TestPathContains_MissingChildUnderExistingAncestorStillResolves cannot see
// this: it asserts that resolution SUCCEEDS, not what it resolves TO. This
// test asserts the resolved VALUE.
//
// The fixture is entirely inside t.TempDir() with an injected home; nothing
// under the developer's real home is read.
func TestResolveDeepestExistingChild_PreservesEveryClimbedComponent(t *testing.T) {
	root, _ := fakeMissingHome6548(t)
	link := symlinkedRoot6548(t, root)

	// Outside $HOME entirely, so the home stop never applies. Only <root>/link
	// exists, so the climb ascends several levels before anything resolves.
	child := filepath.Join(link, "not", "created", "yet")

	linkReal, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", link, err)
	}
	want := filepath.Join(linkReal, "not", "created", "yet")

	got, ok := resolveDeepestExistingChild(child)
	if !ok {
		t.Fatalf("resolveDeepestExistingChild(%q) reported nothing resolved; <root>/link exists and must resolve", child)
	}
	if got != want {
		t.Fatalf("resolveDeepestExistingChild(%q) dropped components consumed during the climb:\n got: %q\nwant: %q", child, got, want)
	}
}

// TestResolveDeepestExistingChild_SingleLevelClimbUnchanged is the
// permissive-direction guard: accumulating the tail must not disturb the
// common case where the child's own parent already resolves.
func TestResolveDeepestExistingChild_SingleLevelClimbUnchanged(t *testing.T) {
	root, _ := fakeMissingHome6548(t)
	link := symlinkedRoot6548(t, root)

	child := filepath.Join(link, "yet")
	linkReal, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", link, err)
	}
	want := filepath.Join(linkReal, "yet")

	got, ok := resolveDeepestExistingChild(child)
	if !ok {
		t.Fatalf("resolveDeepestExistingChild(%q) reported nothing resolved", child)
	}
	if got != want {
		t.Fatalf("resolveDeepestExistingChild(%q) = %q, want %q", child, got, want)
	}
}

// TestResolveDeepestExistingChild_NothingResolvedKeepsSpelling pins the
// contract the caller depends on: when the climb is stopped before anything
// resolves, the reported value is the unresolved spelling and ok is false, so
// pathContains leaves childNorm alone.
func TestResolveDeepestExistingChild_NothingResolvedKeepsSpelling(t *testing.T) {
	_, home := fakeMissingHome6548(t)

	// Inside the (non-existent) injected $HOME: the climb reaches the home
	// boundary with nothing resolved.
	child := filepath.Join(home, "w", "x")

	got, ok := resolveDeepestExistingChild(child)
	if ok {
		t.Fatalf("resolveDeepestExistingChild(%q) claimed it resolved to %q, but nothing at or below the injected home %q exists", child, got, home)
	}
	if got != child {
		t.Fatalf("resolveDeepestExistingChild(%q) = %q; an unresolved climb must return the spelling it was given", child, got)
	}
}
