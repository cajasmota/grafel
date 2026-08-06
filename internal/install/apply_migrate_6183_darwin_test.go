//go:build darwin

package install

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/install/watchers"
	"github.com/cajasmota/grafel/internal/registry"
)

// TestApply_RetiresLegacyWatcherUnit covers the #6183 migration on the install
// path.
//
// `grafel install` / `grafel update` are how most machines cross the label
// change. If Apply only wrote the new unit, the pre-#6183 plist would stay on
// disk AND stay loaded under the old label, and the new label would bootstrap
// alongside it: two watchers running for one repo, which is strictly worse than
// the collision being fixed.
//
// launchctl is stubbed — no test may mutate the developer's launchd session.
func TestApply_RetiresLegacyWatcherUnit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(home, ".grafel"))

	// No test may mutate the developer's launchd session.
	stubLaunchctlRunner(t)

	// The migration's deregistration is observed through the loader seam, not
	// through the stubbed runner: darwin's Unload probes `launchctl list`
	// first and short-circuits when the label is not loaded, which it never is
	// in a sandbox.
	fake := &fakeLoader{}
	prevLoader := newWatcherLoader
	newWatcherLoader = func() watchers.Loader { return fake }
	t.Cleanup(func() { newWatcherLoader = prevLoader })

	// The repo lives outside t.TempDir: install.Apply creates <repo>/.grafel/
	// logs asynchronously after returning, which races t.TempDir cleanup
	// (#6188). Owning the directory keeps that flake out of this test.
	repoRoot, err := os.MkdirTemp("", "grafel-6183-apply-")
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(repoRoot, "app")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for i := 0; i < 3; i++ {
			if err := os.RemoveAll(repoRoot); err == nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	})

	bin := filepath.Join(home, "bin", "grafel")
	u := watchers.Unit{Group: "g", Repo: repo, BinPath: bin}

	// A unit installed by a pre-#6183 binary.
	legacyPath, err := watchers.Write(watchers.LegacyOf(u))
	if err != nil {
		t.Fatal(err)
	}
	legacyLabel := watchers.LegacyOf(u).Label()

	cfg := &registry.GroupConfig{Name: "g", Repos: []registry.Repo{{Slug: "app", Path: repo}}}
	cfg.Features.Watchers = true

	if _, err := Apply(Options{
		Group: "g", Config: cfg, BinPath: bin,
		SkipHooks: true, SkipRulesFiles: true, SkipMCP: true,
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("Apply left the pre-#6183 unit on disk (%s): %v", legacyPath, err)
	}
	newPath, _ := watchers.UnitPath(u)
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("Apply did not install the new unit: %v", err)
	}

	// A deleted plist is not a deregistered job: launchd still holds the old
	// label. Assert the deregistration actually named it.
	bootedOutLegacy := false
	for _, l := range fake.unloaded {
		if l == legacyLabel {
			bootedOutLegacy = true
		}
		if l == u.Label() {
			t.Fatalf("Apply deregistered the NEW label %s", l)
		}
	}
	if !bootedOutLegacy {
		t.Fatalf("no deregistration named the legacy label %q; unloaded=%v", legacyLabel, fake.unloaded)
	}
}
