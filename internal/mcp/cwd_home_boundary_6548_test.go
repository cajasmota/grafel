package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/pathboundary"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// #6548 — groupFromCWD / repoFromCWD climbed to the filesystem root when the
// marker they seek is absent, walking through $HOME, /Users and / on the way.
// A macOS user was shown an iCloud Drive consent prompt as a result.
//
// The fixtures below are built entirely under t.TempDir(); the "home" they use
// is an injected fake. No test here reads the developer's real home directory.

// fakeHomeUnder fully isolates the environment with testsupport.IsolateHome
// (HOME, USERPROFILE, GRAFEL_HOME, XDG, daemon root) and then re-points the
// home at a NESTED directory inside that sandbox, so a fixture can plant a
// marker ABOVE the home and still stay inside a TempDir. It returns
// (sandboxRoot, home).
func fakeHomeUnder(t *testing.T, sub ...string) (string, string) {
	t.Helper()
	root := testsupport.IsolateHome(t)
	home := mkdir(t, filepath.Join(append([]string{root}, sub...)...))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("GRAFEL_HOME", filepath.Join(home, ".grafel-store"))
	return root, home
}

// mkdirAll + WriteFile helpers keep the fixtures readable.
func mkdir(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	return path
}

func writeGroupMarker(t *testing.T, dir, group string) {
	t.Helper()
	mkdir(t, filepath.Join(dir, ".grafel"))
	body := []byte(`{"group":"` + group + `"}`)
	if err := os.WriteFile(filepath.Join(dir, ".grafel", "group.json"), body, 0o644); err != nil {
		t.Fatalf("write group.json: %v", err)
	}
}

// TestGroupFromCWD_StopsAtHome is the killing test for the groupFromCWD site:
// a group.json planted ABOVE the home directory must never be reached.
func TestGroupFromCWD_StopsAtHome(t *testing.T) {
	root, home := fakeHomeUnder(t, "home", "u")

	// Marker sits ABOVE $HOME — a climber with no home boundary finds it.
	writeGroupMarker(t, root, "above-home")

	start := mkdir(t, filepath.Join(home, "work", "repo", "pkg"))
	if got := groupFromCWD(start); got != "" {
		t.Fatalf("groupFromCWD climbed past $HOME (site: internal/mcp/routing.go groupFromCWD): got %q, want \"\" (marker at %s is above home %s)", got, root, home)
	}
}

// TestGroupFromCWD_FindsMarkerAtHome is the permissive-direction guard: the
// boundary must stop AT $HOME, not before it, and must not treat any
// home-looking ancestor as the boundary.
func TestGroupFromCWD_FindsMarkerAtHome(t *testing.T) {
	_, home := fakeHomeUnder(t, "home", "u")

	writeGroupMarker(t, home, "at-home")

	start := mkdir(t, filepath.Join(home, "work", "repo", "pkg"))
	if got := groupFromCWD(start); got != "at-home" {
		t.Fatalf("groupFromCWD stopped too early: got %q, want %q (marker lives at $HOME %s)", got, "at-home", home)
	}
}

// TestGroupFromCWD_OutsideHomeStillClimbs guards the other permissive failure:
// a start path that is NOT inside any home (a /var/folders temp dir, an /opt or
// /srv checkout) must keep climbing. A boundary that fired on every ancestor
// would silently stop such a repo from resolving its group.
//
// #6548 requirement 3 (owner decision 2026-09-02) INVERTED half of this test:
// it used to start under <root>/Users/someone — a SIBLING of the current
// user's home — and require the marker above the Users level to be found.
// That case is now a refusal and is asserted as one below.
func TestGroupFromCWD_OutsideHomeStillClimbs(t *testing.T) {
	root, home := fakeHomeUnder(t, "Users", "me")
	// $HOME is no longer consulted by the other-user-home class (it can be
	// laundered), so the fixture's home is injected through the seam.
	t.Cleanup(pathboundary.OverrideHomeReferences(home))

	writeGroupMarker(t, root, "workspace-root")

	start := mkdir(t, filepath.Join(root, "srv", "ci", "src", "deep", "pkg"))
	if got := groupFromCWD(start); got != "workspace-root" {
		t.Fatalf("groupFromCWD stopped before a legitimate marker outside every home: got %q, want %q", got, "workspace-root")
	}

	// The inverted case: the same marker, reached from another user's home,
	// must be unreachable — the climb never enters /Users/someone at all.
	sibling := mkdir(t, filepath.Join(root, "Users", "someone", "src", "deep", "pkg"))
	if got := groupFromCWD(sibling); got != "" {
		t.Fatalf("groupFromCWD climbed out of another user's home %s (site: internal/mcp/routing.go groupFromCWD): got %q, want \"\"",
			filepath.Join(root, "Users", "someone"), got)
	}
}

// TestRepoFromCWD_StopsAtHome is the killing test for the repoFromCWD site.
func TestRepoFromCWD_StopsAtHome(t *testing.T) {
	root, home := fakeHomeUnder(t, "home", "u")

	mkdir(t, filepath.Join(root, ".grafel"))

	start := mkdir(t, filepath.Join(home, "work", "repo", "pkg"))
	if got := repoFromCWD(start); got != "" {
		t.Fatalf("repoFromCWD climbed past $HOME (site: internal/mcp/routing.go repoFromCWD): got %q, want \"\"", got)
	}
}

// TestRepoFromCWD_FindsMarkerAtHome — permissive-direction guard for repoFromCWD.
func TestRepoFromCWD_FindsMarkerAtHome(t *testing.T) {
	_, home := fakeHomeUnder(t, "home", "u")

	mkdir(t, filepath.Join(home, ".grafel"))

	start := mkdir(t, filepath.Join(home, "work", "repo", "pkg"))
	if got := repoFromCWD(start); got != filepath.Base(home) {
		t.Fatalf("repoFromCWD stopped too early: got %q, want %q", got, filepath.Base(home))
	}
}
