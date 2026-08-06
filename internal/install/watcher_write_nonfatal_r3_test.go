package install_test

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/install"
	"github.com/cajasmota/grafel/internal/install/watchers"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/testsupport"
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
// Nothing in this test is macOS-specific: the property is Apply's per-repo
// warn-and-continue, and watchers.Write rejects a control byte in Unit.Repo on
// every platform (validateUnitFields, not the plist renderer). It nevertheless
// used the darwin-only watchers.SetLaunchctlRunnerForTest seam and a bare
// t.Setenv("HOME", ...), so it did not COMPILE on linux or windows —
// `GOOS=linux go vet ./internal/install/...` failed on origin/main too. That was
// noted verbatim in #6197's PR body as "pre-existing and unrelated" and left in
// place; it is fixed here rather than tagged, because tagging would concede
// coverage the test never needed to give up.
//
// The two portable spellings both already exist in this package:
//   - testsupport.IsolateHome sets HOME *and* %USERPROFILE% (os.UserHomeDir
//     reads the latter on Windows, which watchers.UnitDir goes through) and
//     asserts the redirect took effect. See guidance_test.go for the same call.
//   - watchers.StubServiceCallsForTest is the documented single cross-platform
//     stub — it short-circuits launchctl, systemctl AND schtasks. The darwin
//     helper in launchctl_stub_darwin_test.go delegates to it for that reason.
func TestApply_WatcherWriteFailureIsNonFatal(t *testing.T) {
	testsupport.IsolateHome(t)

	// Never let a real service manager run even if activation is reached for
	// the good repo.
	t.Cleanup(watchers.StubServiceCallsForTest())

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
