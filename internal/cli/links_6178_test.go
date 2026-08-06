// Regression test for issue #6178: the `grafel links` command
// (runLinksForGroup) and its RunLinksForGroup hook both passed "" as the
// grafelHome argument to links.RunAllPasses, and PathsFor's own
// empty-string fallback consults os.UserHomeDir() directly rather than
// GRAFEL_HOME. In an isolated run (GRAFEL_HOME set, HOME left alone —
// the shape every scratch-root workflow uses) the link *store* landed
// under GRAFEL_HOME (StoreDir honors the override) but
// <group>-links.json landed in the REAL ~/.grafel/groups/, silently,
// next to unrelated groups' state.
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/registry"
)

// TestRunLinksForGroup_HonorsGRAFELHomeEnv_6178 reproduces #6178 without
// ever touching the real user's home directory. HOME is redirected to a
// throwaway sandbox (so nothing here can write into a real ~/.grafel even
// if the fix regresses), and GRAFEL_HOME is pointed at a SEPARATE temp
// dir — the arrangement a real isolated run uses (only GRAFEL_HOME set).
// If the fallback ever consults os.UserHomeDir() instead of GRAFEL_HOME,
// the links file lands under the sandboxed HOME's .grafel instead of
// GRAFEL_HOME, which this test catches.
//
// It also asserts, read-only via os.Stat (never a write), that nothing
// landed at the process's REAL home, captured before any env override —
// belt-and-braces documentation of the invariant, not the assertion that
// actually detects the bug (see the comment at that check below for why).
func TestRunLinksForGroup_HonorsGRAFELHomeEnv_6178(t *testing.T) {
	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir (pre-override): %v", err)
	}

	sandboxHome := t.TempDir()
	grafelHome := t.TempDir()
	t.Setenv("HOME", sandboxHome)
	// #6178 code-review finding #3: os.UserHomeDir() reads USERPROFILE on
	// Windows, not HOME. internal/daemon/paths_windows.go exists, so
	// Windows is a supported target; without this, a regressed build on a
	// Windows dev box would fall through the (unoverridden) real
	// %USERPROFILE% here instead of the sandbox.
	t.Setenv("USERPROFILE", sandboxHome)
	t.Setenv("GRAFEL_HOME", grafelHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(sandboxHome, ".config"))

	const group = "issue6178-regression-group-do-not-create"
	repo := filepath.Join(sandboxHome, "repos", "alpha")
	makeRepo(t, repo)

	cfg := &registry.GroupConfig{Name: group}
	cfg.Repos = []registry.Repo{{Slug: "alpha", Path: repo, Stack: registry.StackList{"go"}}}
	cfgPath, err := registry.ConfigPathFor(group)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	if err := registry.AddGroup(group, cfgPath); err != nil {
		t.Fatal(err)
	}

	// Minimal per-repo graph.json so the link pass has something to
	// stage and load (empty entity/relationship set is enough).
	gpath := daemon.GraphPathForRepo(repo)
	if err := os.MkdirAll(filepath.Dir(gpath), 0o755); err != nil {
		t.Fatal(err)
	}
	const doc = `{"version":1,"repo":"alpha","entities":[],"relationships":[]}`
	if err := os.WriteFile(gpath, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	if err := runLinksForGroup(cmd, group); err != nil {
		t.Fatalf("runLinksForGroup: %v\noutput:\n%s", err, out.String())
	}

	wantLinks := filepath.Join(grafelHome, "groups", group+"-links.json")
	if _, err := os.Stat(wantLinks); err != nil {
		t.Fatalf("expected links file under GRAFEL_HOME at %s: %v\noutput:\n%s", wantLinks, err, out.String())
	}

	// Belt-and-braces, not the load-bearing assertion: since this test
	// overrides HOME/USERPROFILE before any code under test runs, a fully
	// reverted build writes into the SANDBOXED home, never the real one —
	// so this os.Stat cannot actually observe a regression on its own; the
	// sandboxFallback check below is what proves the bug is caught.
	// os.Stat is read-only regardless, so keeping it costs nothing and
	// documents the invariant this whole test exists to protect: no run
	// of this suite may ever touch the operator's real ~/.grafel.
	realLinks := filepath.Join(realHome, ".grafel", "groups", group+"-links.json")
	if _, statErr := os.Stat(realLinks); statErr == nil {
		t.Fatalf("regression: links file leaked into the REAL home at %s", realLinks)
	} else if !os.IsNotExist(statErr) {
		t.Fatalf("unexpected error statting real home path %s: %v", realLinks, statErr)
	}

	// This is the assertion that actually catches the #6178 regression:
	// nothing may have landed under the sandboxed HOME's .grafel — that
	// would mean the fallback used os.UserHomeDir() (HOME) instead of
	// consulting GRAFEL_HOME, which is a different dir in this test.
	sandboxFallback := filepath.Join(sandboxHome, ".grafel", "groups", group+"-links.json")
	if _, statErr := os.Stat(sandboxFallback); statErr == nil {
		t.Fatalf("regression: links file landed at HOME-derived fallback %s instead of GRAFEL_HOME %s", sandboxFallback, grafelHome)
	}
}
