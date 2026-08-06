package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/install/watchers"
)

// TestUninstall_RemovesLegacyWatcherUnit closes the last window the #6183 label
// change opens.
//
// Uninstall is the only code that runs when a group stops being registered, and
// it is the LAST moment at which the pre-#6183 label can be derived at all —
// afterwards nothing knows the repo path, so nothing can name the old unit. A
// legacy plist left here would stay on disk and stay loaded forever, relaunching
// a watcher for a group grafel no longer has a config for.
func TestUninstall_RemovesLegacyWatcherUnit(t *testing.T) {
	home := reconcileSandbox(t)
	bin := filepath.Join(home, "bin", "grafel")
	repo := filepath.Join(home, "repos", "app")
	registerGroup(t, home, "g", []string{repo}, true)

	u := watchers.Unit{Group: "g", Repo: repo, BinPath: bin}
	if _, err := watchers.UnitPath(u); err != nil {
		t.Skipf("unsupported OS: %v", err)
	}
	legacyPath := writeUnitFile(t, watchers.LegacyOf(u), watchers.Render(watchers.LegacyOf(u)))
	currentPath := writeUnitFile(t, u, watchers.Render(u))

	// TODO(#6183-seam): assert WHICH label was deregistered once Uninstall
	// builds its Loader through the newWatcherLoader seam. It still calls
	// watchers.NewLoader() directly, so the deregistrations are invisible from
	// here and a mutant that deletes the legacy Unload survives. The seam
	// routing lands with fix/grafel-stop-leaves-indexing; the coordinator is
	// filing the tracking issue. Cleanup's equivalent IS asserted by label in
	// watchers/migrate_6183_test.go.
	// Stub the runner so nothing this test does can reach a real launchctl in
	// the meantime; darwin's Unload only probes `launchctl list` (read-only)
	// before short-circuiting on a label that is not loaded.
	stubLaunchctlRunner(t)

	if err := Uninstall("g", false); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	for name, p := range map[string]string{"legacy": legacyPath, "current": currentPath} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("Uninstall left the %s unit on disk (%s): %v", name, p, err)
		}
	}
}
