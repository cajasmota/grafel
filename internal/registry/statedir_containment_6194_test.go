package registry

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// sandboxHomeWithSibling builds an isolated grafel home that has a *sibling*
// directory outside it, so a traversal escape has somewhere real to land.
//
// withHome (registry_test.go) points GRAFEL_HOME straight at a t.TempDir(),
// which makes "did the derived path escape?" unanswerable: everything the test
// can reach is inside the same TempDir either way. Here the state root is
// nested two levels down and the canary is a sibling of the outermost dir, so
// an escaping derivation resolves onto a path the test can stat and a contained
// one provably cannot.
//
// Returns (grafelHome, canaryDir).
func sandboxHomeWithSibling(t *testing.T) (string, string) {
	t.Helper()
	base := t.TempDir()
	osHome := filepath.Join(base, "home")
	grafelHome := filepath.Join(osHome, ".grafel")
	if err := os.MkdirAll(filepath.Join(grafelHome, "groups"), 0o755); err != nil {
		t.Fatalf("mkdir grafel home: %v", err)
	}
	canary := filepath.Join(base, "canary")
	if err := os.MkdirAll(canary, 0o755); err != nil {
		t.Fatalf("mkdir canary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(canary, "precious.txt"), []byte("do not delete"), 0o644); err != nil {
		t.Fatalf("write canary file: %v", err)
	}
	// Isolate all three home resolvers per the hard isolation gate: HomeDir()
	// reads GRAFEL_HOME first today, but the other two must never be able to
	// reach the developer's real home if that order ever changes.
	t.Setenv("HOME", osHome)
	t.Setenv("USERPROFILE", osHome)
	t.Setenv("GRAFEL_HOME", grafelHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "xdg"))
	return grafelHome, canary
}

// TestStateDirForExisting_RefusesTraversingName pins #6194.
//
// The two purge sites (install.Uninstall, daemon removeGroup) call
// os.RemoveAll on registry.StateDirFor(group). StateDirFor is a filepath.Join
// of a raw registry-supplied name, and Join collapses "..", so a grandfathered
// registry entry named "../../../canary" resolves to a directory outside the
// state root and RemoveAll deletes it.
//
// The assertion is deliberately two-sided so it cannot pass vacuously:
//  1. it first proves the OLD derivation really does escape (otherwise a
//     refusal from the new one would prove nothing about traversal), and
//  2. it then requires the new derivation to refuse.
func TestStateDirForExisting_RefusesTraversingName(t *testing.T) {
	_, canary := sandboxHomeWithSibling(t)

	// Enough ".." to climb out of <home>/.grafel/groups and into the sibling.
	const evil = "../../../canary"

	// (1) The premise: the ungated derivation escapes onto the canary.
	escaped, err := StateDirFor(evil)
	if err != nil {
		t.Fatalf("StateDirFor(%q): %v", evil, err)
	}
	if filepath.Clean(escaped) != filepath.Clean(canary) {
		t.Fatalf("test premise broken: StateDirFor(%q) = %q, want it to resolve onto the canary %q; "+
			"without a real escape this test would prove nothing", evil, escaped, canary)
	}

	// (2) The fix: the delete-side derivation must refuse.
	got, err := StateDirForExisting(evil)
	if err == nil {
		t.Fatalf("StateDirForExisting(%q) = %q, nil; want a containment refusal (#6194)", evil, got)
	}
	if got != "" {
		t.Fatalf("StateDirForExisting(%q) returned path %q alongside its error; a refusing "+
			"derivation must return no path at all, or a caller ignoring err still deletes it", evil, got)
	}
}

// TestStateDirForExisting_AllowsGrandfatheredContainedName is the other half
// of #6194 and is what makes the fix a *containment* assertion rather than
// name validation.
//
// "my/group" fails registry.ValidateGroupName (it contains a path separator)
// but is still strictly inside the state root once joined. Validating the name
// at the delete site would refuse it and resurrect the exact problem the
// read-side validation split exists to avoid: a registry entry written before
// validation existed becomes unusable. Only escape may be refused.
func TestStateDirForExisting_AllowsGrandfatheredContainedName(t *testing.T) {
	grafelHome, _ := sandboxHomeWithSibling(t)

	const grandfathered = "my/group"

	// Premise: this name is genuinely one that name-validation rejects, so the
	// test distinguishes containment from validation rather than restating it.
	if err := ValidateGroupName(grandfathered); err == nil {
		t.Fatalf("test premise broken: ValidateGroupName(%q) accepted the name, so this test no "+
			"longer distinguishes a containment check from name validation", grandfathered)
	}

	got, err := StateDirForExisting(grandfathered)
	if err != nil {
		t.Fatalf("StateDirForExisting(%q) = %v; a grandfathered-but-contained name must still "+
			"resolve, otherwise valid registries become unpurgeable (#6194)", grandfathered, err)
	}
	want := filepath.Join(grafelHome, "groups", "my", "group")
	if got != want {
		t.Fatalf("StateDirForExisting(%q) = %q, want %q", grandfathered, got, want)
	}
}

// TestStateDirForExisting_RefusesSiblingWithSharedPrefix pins the specific
// trap a naive containment check falls into: strings.HasPrefix(dir, root)
// accepts "<root>-evil" because "<root>" is a literal prefix of it. The check
// must compare on a separator boundary (or via filepath.Rel).
func TestStateDirForExisting_RefusesSiblingWithSharedPrefix(t *testing.T) {
	grafelHome, _ := sandboxHomeWithSibling(t)
	root := filepath.Join(grafelHome, "groups")

	// "../groups-evil" resolves to a sibling of the state root whose path has
	// the root as a plain string prefix.
	const sibling = "../groups-evil"
	escaped, err := StateDirFor(sibling)
	if err != nil {
		t.Fatalf("StateDirFor: %v", err)
	}
	if !strings.HasPrefix(escaped, root) {
		t.Fatalf("test premise broken: %q is not a plain-string prefix match against root %q, so "+
			"this test does not exercise the HasPrefix trap", escaped, root)
	}
	if filepath.Dir(escaped) == root {
		t.Fatalf("test premise broken: %q is genuinely inside the root %q", escaped, root)
	}

	if got, err := StateDirForExisting(sibling); err == nil {
		t.Fatalf("StateDirForExisting(%q) = %q, nil; a prefix-only match is not containment (#6194)",
			sibling, got)
	}
}

// TestStateDirForExisting_SymlinkEscapeAndSymlinkedRoot covers the #6187 trap
// (same shape as the watcher reaper): a purely lexical containment check is
// blind to symlinks in two opposite directions.
//
//   - False NEGATIVE (the dangerous one): "esc/x" is lexically inside the
//     state root, but if <root>/esc is a symlink pointing elsewhere then
//     os.RemoveAll traverses it and deletes outside the root. A Clean+Rel
//     comparison on unresolved strings accepts this.
//   - False POSITIVE: if the state root itself is reached through a symlink
//     (a supported layout — ~/.grafel on another volume), an ordinary group
//     must still resolve. A check that resolves only one side rejects it.
//
// Both are asserted here, so neither "resolve nothing" nor "resolve too
// eagerly" passes.
func TestStateDirForExisting_SymlinkEscapeAndSymlinkedRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevation on Windows")
	}
	base := t.TempDir()
	realState := filepath.Join(base, "real-state")
	if err := os.MkdirAll(filepath.Join(realState, "groups"), 0o755); err != nil {
		t.Fatalf("mkdir real state: %v", err)
	}
	osHome := filepath.Join(base, "home")
	if err := os.MkdirAll(osHome, 0o755); err != nil {
		t.Fatalf("mkdir os home: %v", err)
	}
	// The state root is reached through a symlink.
	link := filepath.Join(osHome, ".grafel")
	if err := os.Symlink(realState, link); err != nil {
		t.Skipf("symlink unsupported here: %v", err)
	}
	// And an intermediate component inside it points back outside.
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(filepath.Join(outside, "x"), 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(realState, "groups", "esc")); err != nil {
		t.Skipf("symlink unsupported here: %v", err)
	}

	t.Setenv("HOME", osHome)
	t.Setenv("USERPROFILE", osHome)
	t.Setenv("GRAFEL_HOME", link)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "xdg"))

	// False-positive guard: the symlinked root must not break ordinary groups.
	if _, err := StateDirForExisting("ordinary"); err != nil {
		t.Fatalf("StateDirForExisting(%q) through a symlinked state root = %v, want it to resolve; "+
			"a symlinked ~/.grafel is a supported layout and must stay purgeable", "ordinary", err)
	}

	// Premise: "esc/x" really is lexically contained, so refusing it can only
	// come from resolving the symlink, not from a Clean+Rel string check.
	lexical, err := StateDirFor("esc/x")
	if err != nil {
		t.Fatalf("StateDirFor: %v", err)
	}
	rel, err := filepath.Rel(filepath.Join(link, "groups"), filepath.Clean(lexical))
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Fatalf("test premise broken: %q is not lexically contained (rel=%q, err=%v), so this "+
			"test does not exercise the symlink trap", lexical, rel, err)
	}

	// False-negative guard: it resolves outside, so it must be refused.
	if got, err := StateDirForExisting("esc/x"); err == nil {
		t.Fatalf("StateDirForExisting(%q) = %q, nil; the path is lexically contained but resolves "+
			"outside the state root through a symlink, which os.RemoveAll follows (#6194/#6187)",
			"esc/x", got)
	}
}
