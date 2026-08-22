//go:build darwin

package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/testsupport"
)

// #6548 — discoverCandidates falls back to filepath.Dir(cwd) when --parent is
// absent, then os.ReadDir's that parent and os.Stat's every child's .git. From
// $HOME/<x> that enumerates all of $HOME, firing macOS TCC prompts — while the
// same operation in internal/install/detect (siblingGitRepos) IS gated by
// isProtectedScanParent. This closes that asymmetry.
//
// All fixtures live under t.TempDir(); $HOME is an injected fake.

func mkdirW6548(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	return path
}

// fakeHomeW6548 isolates the whole environment and re-points the home at a
// nested dir inside the sandbox. Returns the home path.
func fakeHomeW6548(t *testing.T) string {
	t.Helper()
	root := testsupport.IsolateHome(t)
	home := mkdirW6548(t, filepath.Join(root, "u"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("GRAFEL_HOME", filepath.Join(home, ".grafel-store"))
	return home
}

// TestDiscoverCandidates_RefusesInferredHomeParent is the killing test: with no
// --parent, a cwd directly under $HOME must NOT cause $HOME to be enumerated.
func TestDiscoverCandidates_RefusesInferredHomeParent(t *testing.T) {
	home := fakeHomeW6548(t)

	repo := mkdirW6548(t, filepath.Join(home, "proj"))
	mkdirW6548(t, filepath.Join(repo, ".git"))
	// A sibling repo that an ungated scan would happily enumerate.
	mkdirW6548(t, filepath.Join(home, "other", ".git"))

	t.Chdir(repo)

	got, err := discoverCandidates(io.Discard, wizardOptions{})
	if err == nil {
		t.Fatalf("discoverCandidates enumerated $HOME (site: internal/cli/wizard.go discoverCandidates): got %v, want a refusal error", got)
	}
	if !strings.Contains(err.Error(), "--parent") {
		t.Fatalf("refusal error should tell the user to pass --parent/--repos explicitly, got: %v", err)
	}
}

// TestDiscoverCandidates_ExplicitParentStaysExempt — the rule constrains
// INFERRED traversal only. An explicitly supplied --parent, even $HOME itself,
// is an instruction the user gave and must still be scanned.
func TestDiscoverCandidates_ExplicitParentStaysExempt(t *testing.T) {
	home := fakeHomeW6548(t)

	mkdirW6548(t, filepath.Join(home, "proj", ".git"))

	got, err := discoverCandidates(io.Discard, wizardOptions{ParentDir: home})
	if err != nil {
		t.Fatalf("explicit --parent must stay exempt, got error: %v", err)
	}
	if len(got) != 1 || filepath.Base(got[0]) != "proj" {
		t.Fatalf("explicit --parent scan: got %v, want [%s]", got, filepath.Join(home, "proj"))
	}
}

// TestDiscoverCandidates_InferredNonProtectedParentStillScans — the
// permissive-direction guard: the ordinary case (a repo under a normal
// workspace dir) must keep auto-discovering siblings. A gate that refused any
// inferred parent would break the wizard's whole zero-flag path.
func TestDiscoverCandidates_InferredNonProtectedParentStillScans(t *testing.T) {
	home := fakeHomeW6548(t)

	ws := mkdirW6548(t, filepath.Join(home, "src"))
	mkdirW6548(t, filepath.Join(ws, "a", ".git"))
	mkdirW6548(t, filepath.Join(ws, "b", ".git"))

	t.Chdir(filepath.Join(ws, "a"))

	got, err := discoverCandidates(io.Discard, wizardOptions{})
	if err != nil {
		t.Fatalf("inferred non-protected parent must still scan, got error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("inferred scan of %s: got %v, want 2 repos", ws, got)
	}
}
