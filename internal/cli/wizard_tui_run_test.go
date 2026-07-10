package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/cli/wiztui"
)

// TestWizardUseTUI_NonTTYFallsBack: a non-*os.File writer (e.g. a bytes.Buffer
// used in tests / pipes) must NOT launch the full-screen TUI.
func TestWizardUseTUI_NonTTYFallsBack(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	if wizardUseTUI(&bytes.Buffer{}) {
		t.Error("wizardUseTUI(bytes.Buffer) = true; want false (not a terminal)")
	}
}

// TestWizardUseTUI_DumbTermFallsBack: $TERM=dumb forces the line-based flow.
func TestWizardUseTUI_DumbTermFallsBack(t *testing.T) {
	t.Setenv("TERM", "dumb")
	if wizardUseTUI(os.Stdout) {
		t.Error("wizardUseTUI with TERM=dumb = true; want false")
	}
}

// TestWizardUseTUI_EmptyTermFallsBack: an empty $TERM forces the line-based flow.
func TestWizardUseTUI_EmptyTermFallsBack(t *testing.T) {
	t.Setenv("TERM", "")
	if wizardUseTUI(os.Stdout) {
		t.Error("wizardUseTUI with empty TERM = true; want false")
	}
}

// TestWizardUseTUI_OptOutEnv: GRAFEL_NO_TUI disables the TUI even on a TTY.
func TestWizardUseTUI_OptOutEnv(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("GRAFEL_NO_TUI", "1")
	if wizardUseTUI(os.Stdout) {
		t.Error("GRAFEL_NO_TUI did not disable the TUI")
	}
}

// TestReposForResult_Monorepo: a monorepo action must map to EXACTLY ONE
// registry.Repo rooted at the monorepo path, with the chosen packages
// recorded as Modules — NOT one flattened repo per package (D2/D3: flattening
// breaks the graph model and makes hooksDir stat a non-existent <pkg>/.git).
// This mirrors `monorepo add`, which appends packages into r.Modules on a
// single Repo (internal/cli/monorepo.go newMonorepoAddCmd).
func TestReposForResult_Monorepo(t *testing.T) {
	root := t.TempDir()
	class := mustClassify(root)
	// Force a monorepo-style result with multiple chosen packages.
	r := wiztui.Result{Action: wiztui.ActionMonorepo, Repos: []string{"services/auth", "packages/ui"}}
	repos := reposForResult(class, r)
	if len(repos) != 1 {
		t.Fatalf("got %d repos, want 1 (single repo with Modules, not one per package)", len(repos))
	}
	got := repos[0]
	if got.Path != class.AbsPath {
		t.Errorf("path = %q, want monorepo root %q", got.Path, class.AbsPath)
	}
	base := filepath.Base(class.AbsPath)
	if got.Slug != base {
		t.Errorf("slug = %q, want root-based slug %q", got.Slug, base)
	}
	wantModules := []string{"packages/ui", "services/auth"}
	gotModules := append([]string(nil), got.Modules...)
	sort.Strings(gotModules)
	if !reflect.DeepEqual(gotModules, wantModules) {
		t.Errorf("modules = %v, want %v", gotModules, wantModules)
	}
}
