package install_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/install"
	"github.com/cajasmota/grafel/internal/install/watchers"
	"github.com/cajasmota/grafel/internal/registry"
)

// TestApply_WatcherWriteFailureIsNonFatal pins the #6185/#6186 R3 decision
// (found on round-2 review): install.Apply and internal/install/
// watchersync.go's reconcile disagreed on how to treat a per-repo
// watchers.Write failure — Apply aborted the ENTIRE multi-repo install,
// watchersync warned and continued. That inconsistency existed before this
// fix, but validateUnitFields (control characters, trailing backslash) made
// Write fail in new, plausible circumstances (a corrupted fleet.json repo
// path, hand-edited or restored from an old backup), so a single bad repo
// among many could deny watchers to every OTHER repo in the group.
//
// Decision: Apply is the path a user runs directly (onboarding, the wizard,
// `grafel install`) and the group config is already fully persisted before
// any watcher is written (see WatcherWarnings' doc comment) — the group is
// registered and will index regardless of a per-repo watcher failure. The
// same reasoning that made activation failures non-fatal (#5338) extends to
// write failures: one repo's rejected watcher unit must not deny watchers to
// every other repo in the same Apply call. This aligns Apply with
// watchersync.go's existing warn-and-continue behavior instead of leaving
// the two callers to disagree.
func TestApply_WatcherWriteFailureIsNonFatal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRAFEL_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(home, ".grafel"))

	// Never let a real launchctl run even if activation is reached for the
	// good repo.
	restore := watchers.SetLaunchctlRunnerForTest(func(args ...string) ([]byte, error) {
		return []byte(""), exec.Command("true").Run()
	})
	defer restore()

	goodRepo := t.TempDir()
	badRepo := "/tmp/bad\x00repo" // control byte -> watchers.Write rejects it
	cfg := &registry.GroupConfig{
		Name: "demo",
		Repos: []registry.Repo{
			{Slug: "bad", Path: badRepo},
			{Slug: "good", Path: goodRepo},
		},
	}
	cfg.Features.Watchers = true

	res, err := install.Apply(install.Options{
		Group:          "demo",
		Config:         cfg,
		BinPath:        "/usr/local/bin/grafel",
		SkipHooks:      true,
		SkipRulesFiles: true,
		SkipMCP:        true,
	})
	if err != nil {
		t.Fatalf("Apply should be non-fatal on a per-repo watcher write failure, got: %v", err)
	}
	foundWarning := false
	for _, w := range res.WatcherWarnings {
		if strings.Contains(w, "bad") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Fatalf("expected a WatcherWarning mentioning the bad repo, got: %v", res.WatcherWarnings)
	}
	if len(res.WatcherUnits) != 1 {
		t.Fatalf("expected the good repo's watcher unit to still be written despite the bad "+
			"repo's failure, got %d unit(s): %v", len(res.WatcherUnits), res.WatcherUnits)
	}
}
