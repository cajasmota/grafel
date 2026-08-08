//go:build darwin

package install_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/install"
	"github.com/cajasmota/grafel/internal/install/watchers"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// TestApply_WatcherActivationFailureIsNonFatal verifies the #5338 fix: when the
// OS watcher loader fails to activate a unit (here a forced launchctl failure),
// install.Apply does NOT abort — it records a WatcherWarning and returns nil so
// the wizard completes and the (already-saved) group still indexes.
func TestApply_WatcherActivationFailureIsNonFatal(t *testing.T) {
	testsupport.IsolateHome(t)

	// Force every launchctl invocation to fail with a non-err-5 (real) error so
	// the loader gives up immediately and returns a fatal-looking error that
	// Apply must downgrade to a warning.
	restore := watchers.SetLaunchctlRunnerForTest(func(args ...string) ([]byte, error) {
		return []byte("boom"), exec.Command("sh", "-c", "exit 1").Run()
	})
	defer restore()

	repo := t.TempDir()
	cfg := &registry.GroupConfig{
		Name:  "demo",
		Repos: []registry.Repo{{Slug: "r", Path: repo}},
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
		t.Fatalf("Apply should be non-fatal on watcher activation failure, got: %v", err)
	}
	if len(res.WatcherWarnings) == 0 {
		t.Fatal("expected a WatcherWarning to be recorded")
	}
	if !strings.Contains(res.WatcherWarnings[0], "still registered and will index") {
		t.Fatalf("warning should reassure the group is registered, got: %q", res.WatcherWarnings[0])
	}
}
