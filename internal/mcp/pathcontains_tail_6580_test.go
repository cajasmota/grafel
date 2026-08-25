package mcp

import (
	"path/filepath"
	"runtime"
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

// TestResolveDeepestExistingChild_RootTerminatingClimbStaysClean pins that the
// accumulation produces a CLEANED path, not merely one carrying the right
// components. A climb that resolves at the filesystem root re-joins onto "/",
// so concatenating the tail instead of using filepath.Join yields
// "//gone/a/b" — same components, doubled separator. No other test in the
// package exercises a root-terminating climb.
//
// Nothing is created here: /grafel6580-does-not-exist is only ever stat'd, and
// the walk stops at "/".
func TestResolveDeepestExistingChild_RootTerminatingClimbStaysClean(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX root spelling; Windows roots are drive-qualified")
	}
	fakeMissingHome6548(t) // keeps the injected home clear of this climb

	// Outside the injected $HOME, so the climb runs all the way to "/", which
	// is the only level that resolves.
	child := "/grafel6580-does-not-exist/a/b"

	got, ok := resolveDeepestExistingChild(child)
	if !ok {
		t.Fatalf("resolveDeepestExistingChild(%q) reported nothing resolved; \"/\" must resolve", child)
	}
	if got != child {
		t.Fatalf("resolveDeepestExistingChild(%q) = %q, want %q", child, got, child)
	}
	if cleaned := filepath.Clean(got); got != cleaned {
		t.Fatalf("resolveDeepestExistingChild(%q) returned an uncleaned path %q (cleans to %q): the accumulated tail must be re-joined with filepath.Join, not concatenated", child, got, cleaned)
	}
}

// TestResolveDeepestExistingChild_TrailingSeparatorDoesNotDuplicate covers the
// one input shape the walk's Dir/Base partition does not hold for.
// Dir(".../yet/") is ".../yet", so without the Clean on entry the seed and the
// first failing level BOTH contribute "yet" and it lands in the result twice —
// a wrong answer that looks entirely plausible. Unreachable from the three
// call sites, which all pass Abs+Clean paths, but the helper is independently
// callable.
func TestResolveDeepestExistingChild_TrailingSeparatorDoesNotDuplicate(t *testing.T) {
	root, _ := fakeMissingHome6548(t)
	link := symlinkedRoot6548(t, root)

	linkReal, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", link, err)
	}
	want := filepath.Join(linkReal, "not", "created", "yet")

	child := filepath.Join(link, "not", "created", "yet") + string(filepath.Separator)
	got, ok := resolveDeepestExistingChild(child)
	if !ok {
		t.Fatalf("resolveDeepestExistingChild(%q) reported nothing resolved", child)
	}
	if got != want {
		t.Fatalf("resolveDeepestExistingChild(%q) = %q, want %q (a trailing separator must not duplicate the last component)", child, got, want)
	}
}
