package daemon

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/install/watchers"
	"github.com/cajasmota/grafel/internal/registry"
)

// recordingLoader observes Unload without any launchctl/systemctl/schtasks
// call. The real darwin loader probes `launchctl list` and short-circuits when
// the label is not loaded — which under a sandboxed HOME it never is — so a
// stubbed command runner alone could not show WHICH unit was named, and the
// assertion that matters here is exactly which unit was named.
type recordingLoader struct{ unloaded *[]watchers.Unit }

func (l recordingLoader) Load(watchers.Unit) error { return nil }
func (l recordingLoader) Unload(u watchers.Unit) error {
	*l.unloaded = append(*l.unloaded, u)
	return nil
}
func (l recordingLoader) Status(watchers.Unit) (watchers.WatcherStatus, error) {
	return watchers.WatcherStatus{}, nil
}

// sandboxWatcherHome redirects grafel's home, the OS unit directory and the
// systemd/XDG directory into a temp tree, and stubs every service-manager
// invocation. Nothing this test does may reach ~/.grafel or the developer's
// live launchd session.
func sandboxWatcherHome(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root) // os.UserHomeDir on Windows
	t.Setenv("GRAFEL_HOME", filepath.Join(root, ".grafel"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	t.Cleanup(watchers.StubServiceCallsForTest())

	// Premise: the sandbox actually took. If HomeDir still pointed at the real
	// ~/.grafel every assertion below would be reading the developer's machine.
	got, err := registry.HomeDir()
	if err != nil {
		t.Fatalf("registry.HomeDir: %v", err)
	}
	if got != filepath.Join(root, ".grafel") {
		t.Fatalf("sandbox premise broken: registry.HomeDir() = %q, want it under %q", got, root)
	}
	return root
}

// registerRepo writes a registry + group config naming repoPath, and returns
// the group name.
func registerRepo(t *testing.T, repoPath string) string {
	t.Helper()
	home, err := registry.HomeDir()
	if err != nil {
		t.Fatalf("HomeDir: %v", err)
	}
	const group = "g"
	cfgPath := filepath.Join(home, "groups", group, "fleet.json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfg := map[string]any{
		"name":  group,
		"repos": []map[string]any{{"name": filepath.Base(repoPath), "path": repoPath}},
	}
	writeJSON(t, cfgPath, cfg)
	writeJSON(t, filepath.Join(home, "registry.json"), map[string]any{
		"groups": []map[string]any{{"name": group, "config_path": cfgPath}},
	})

	// Premise: the registry we just wrote is the one the resolver will read.
	refs, err := registry.Groups()
	if err != nil || len(refs) != 1 || refs[0].Name != group {
		t.Fatalf("fixture premise broken: registry.Groups() = %v, err = %v", refs, err)
	}
	return group
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// #6187: the production unloader finds the registered repo's unit and
// deregisters exactly that unit from the OS scheduler.
func TestWatcherUnitUnloader_UnloadsTheRegisteredRepoUnit(t *testing.T) {
	sandboxWatcherHome(t)
	repo := t.TempDir()
	group := registerRepo(t, repo)

	// Install a unit file for the repo — the resolver only deregisters units
	// it can see on disk.
	want := watchers.Unit{Group: group, Repo: repo, BinPath: "/anything"}
	unitPath, err := watchers.Write(want)
	if err != nil {
		t.Fatalf("write unit: %v", err)
	}
	if _, err := os.Stat(unitPath); err != nil {
		t.Fatalf("fixture premise broken: unit file %s must exist: %v", unitPath, err)
	}

	var unloaded []watchers.Unit
	prev := newWatcherUnitLoader
	newWatcherUnitLoader = func() watchers.Loader { return recordingLoader{&unloaded} }
	t.Cleanup(func() { newWatcherUnitLoader = prev })

	makeWatcherUnitUnloader(quietLogger())(repo)

	if len(unloaded) != 1 {
		t.Fatalf("unloaded %d units, want exactly 1: %v", len(unloaded), unloaded)
	}
	// Label is the identity launchctl/systemd/schtasks key off, so that is what
	// must match — not the struct, whose BinPath the resolver fills from
	// os.Executable().
	if got := unloaded[0].Label(); got != want.Label() {
		t.Fatalf("unloaded label = %q, want %q", got, want.Label())
	}
}

// A repo with no unit file installed is left entirely alone: there is no
// registration to repair, and calling the OS scheduler for a unit that was
// never written can only produce noise.
func TestWatcherUnitUnloader_NoUnitFileNoCall(t *testing.T) {
	sandboxWatcherHome(t)
	repo := t.TempDir()
	registerRepo(t, repo)

	var unloaded []watchers.Unit
	prev := newWatcherUnitLoader
	newWatcherUnitLoader = func() watchers.Loader { return recordingLoader{&unloaded} }
	t.Cleanup(func() { newWatcherUnitLoader = prev })

	makeWatcherUnitUnloader(quietLogger())(repo)

	if len(unloaded) != 0 {
		t.Fatalf("unloaded = %v, want none (no unit file was ever installed)", unloaded)
	}
}

// A repo that is not in the registry at all is never acted on. The reaper can
// see `grafel watch` processes for paths grafel does not manage; the unloader
// must not guess a group for them — and in particular must not deregister SOME
// OTHER repo's unit just because that one happened to be resolvable.
//
// The registered repo therefore gets a REAL, INSTALLED unit. Without it the
// unit-file existence check alone would reject every candidate and the test
// would pass with the repo-path match deleted.
func TestWatcherUnitUnloader_UnregisteredRepoNoCall(t *testing.T) {
	sandboxWatcherHome(t)
	registered := t.TempDir()
	group := registerRepo(t, registered)
	stranger := t.TempDir()

	if filepath.Clean(stranger) == filepath.Clean(registered) {
		t.Fatal("fixture premise broken: the two repo paths must differ")
	}
	decoy, err := watchers.Write(watchers.Unit{Group: group, Repo: registered, BinPath: "/anything"})
	if err != nil {
		t.Fatalf("write decoy unit: %v", err)
	}
	if _, err := os.Stat(decoy); err != nil {
		t.Fatalf("fixture premise broken: the registered repo must have an installed unit to be mis-matched against: %v", err)
	}

	var unloaded []watchers.Unit
	prev := newWatcherUnitLoader
	newWatcherUnitLoader = func() watchers.Loader { return recordingLoader{&unloaded} }
	t.Cleanup(func() { newWatcherUnitLoader = prev })

	makeWatcherUnitUnloader(quietLogger())(stranger)

	if len(unloaded) != 0 {
		t.Fatalf("unloaded = %v, want none for an unregistered repo", unloaded)
	}
}
