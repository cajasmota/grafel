// Regression test for issue #6178, links.PathsFor half: PathsFor's own
// empty-string fallback used to call os.UserHomeDir() directly and never
// consulted GRAFEL_HOME. That made every "" caller of PathsFor (not just
// the internal/cli link-pass call sites, which now resolve the home
// explicitly and don't hit this branch at all) silently target the real
// home under an isolated GRAFEL_HOME — e.g. internal/mcp's
// grafel_payload_drift reader, which calls links.PathsFor("", group)
// directly.
//
// This test exercises PathsFor in isolation (no cli, no registry group,
// no staged graphs) so a regression here fails even if every other layer
// of the #6178 fix is intact — code-review finding #2 on the original fix
// noted this half had zero standalone coverage anywhere in the repo.
package links

import (
	"path/filepath"
	"testing"
)

// TestPathsFor_EmptyHomeHonorsGRAFELHomeEnv_6178 sets GRAFEL_HOME and HOME
// to two DIFFERENT temp dirs (the shape a real isolated run uses — only
// GRAFEL_HOME is overridden) and asserts PathsFor("", group) resolves
// under GRAFEL_HOME, not under the HOME-derived fallback.
func TestPathsFor_EmptyHomeHonorsGRAFELHomeEnv_6178(t *testing.T) {
	sandboxHome := t.TempDir()
	grafelHome := t.TempDir()
	t.Setenv("HOME", sandboxHome)
	t.Setenv("USERPROFILE", sandboxHome) // os.UserHomeDir() reads this on Windows
	t.Setenv("GRAFEL_HOME", grafelHome)

	paths, err := PathsFor("", "issue6178-paths-unit-test")
	if err != nil {
		t.Fatalf("PathsFor: %v", err)
	}

	wantLinks := filepath.Join(grafelHome, "groups", "issue6178-paths-unit-test-links.json")
	if paths.Links != wantLinks {
		t.Fatalf("Links = %q, want %q (GRAFEL_HOME=%s)", paths.Links, wantLinks, grafelHome)
	}

	// Must NOT have resolved via the HOME-derived fallback — that would
	// mean the "" branch called os.UserHomeDir() instead of honoring
	// GRAFEL_HOME.
	homeFallback := filepath.Join(sandboxHome, ".grafel", "groups", "issue6178-paths-unit-test-links.json")
	if paths.Links == homeFallback {
		t.Fatalf("regression: PathsFor(\"\", ...) resolved via HOME (%s) instead of GRAFEL_HOME (%s)", sandboxHome, grafelHome)
	}
}

// TestPathsFor_ExplicitHomeUnaffectedByGRAFELHomeEnv_6178 is the mirror
// case: when a caller passes an explicit grafelHome (as the internal/cli
// link-pass call sites now do), PathsFor must use exactly that value and
// ignore GRAFEL_HOME/HOME entirely — the "" branch is a fallback, not a
// silent override of an explicit argument.
func TestPathsFor_ExplicitHomeUnaffectedByGRAFELHomeEnv_6178(t *testing.T) {
	sandboxHome := t.TempDir()
	envHome := t.TempDir()
	explicitHome := t.TempDir()
	t.Setenv("HOME", sandboxHome)
	t.Setenv("USERPROFILE", sandboxHome)
	t.Setenv("GRAFEL_HOME", envHome)

	paths, err := PathsFor(explicitHome, "issue6178-paths-unit-test-explicit")
	if err != nil {
		t.Fatalf("PathsFor: %v", err)
	}
	want := filepath.Join(explicitHome, "groups", "issue6178-paths-unit-test-explicit-links.json")
	if paths.Links != want {
		t.Fatalf("Links = %q, want %q (explicit grafelHome must win over GRAFEL_HOME env)", paths.Links, want)
	}
}
