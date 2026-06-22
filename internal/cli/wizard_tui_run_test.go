package cli

import (
	"bytes"
	"os"
	"path/filepath"
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

// TestReposForResult_Monorepo: monorepo packages map to composite slugs with a
// recorded module, matching the huh flow.
func TestReposForResult_Monorepo(t *testing.T) {
	root := t.TempDir()
	class := mustClassify(root)
	// Force a monorepo-style result.
	r := wiztui.Result{Action: wiztui.ActionMonorepo, Repos: []string{"services/auth", "packages/ui"}}
	repos := reposForResult(class, r)
	if len(repos) != 2 {
		t.Fatalf("got %d repos, want 2", len(repos))
	}
	base := filepath.Base(class.AbsPath)
	if repos[0].Slug != base+"-auth" {
		t.Errorf("slug = %q, want %q", repos[0].Slug, base+"-auth")
	}
	if len(repos[0].Modules) != 1 || repos[0].Modules[0] != "services/auth" {
		t.Errorf("modules = %v, want [services/auth]", repos[0].Modules)
	}
}
