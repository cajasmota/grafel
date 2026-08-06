package cli

// watcher_fleet_6180_test.go pins the defect behind the "`grafel stop` and
// indexing kept happening" report.
//
// `grafel install` writes ONE per-repo watcher unit per registered repo
// (internal/install/install.go, watchers.Write + Loader.Load). Those units are
// owned by launchd/systemd/schtasks, not by the daemon: each one runs
// `grafel watch <repo>`, which polls and enqueues index work on its own
// schedule. `grafel stop` (internal/cli/watcher_ctl.go runDaemonStop) only ever
// stopped the DAEMON's own OS service — the per-repo watcher fleet was never
// touched, so on a 140-repo machine 140 watcher processes carried on indexing
// after a "successful" stop, and KeepAlive/Restart respawned any that exited.
//
// These tests exercise the fleet-wide stop/start/report contract with a fake
// Loader, so no real launchctl/systemctl/schtasks is ever invoked.

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/install/watchers"
	"github.com/cajasmota/grafel/internal/registry"
)

// fakeFleetLoader records Load/Unload calls instead of shelling out.
type fakeFleetLoader struct {
	mu       sync.Mutex
	loaded   []string
	unloaded []string
	failOn   map[string]error // keyed by repo path
	running  map[string]bool  // keyed by repo path
	labels   []string         // every label acted on, incl. orphans
	// block, when non-nil, makes every Load/Unload wait on it. Used to
	// simulate a wedged `launchctl bootout` for the sweep-deadline test.
	block chan struct{}
}

func (f *fakeFleetLoader) Load(u watchers.Unit) error {
	if f.block != nil {
		<-f.block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.labels = append(f.labels, u.Label())
	if err := f.failOn[u.Repo]; err != nil {
		return err
	}
	f.loaded = append(f.loaded, u.Repo)
	return nil
}

func (f *fakeFleetLoader) Unload(u watchers.Unit) error {
	if f.block != nil {
		<-f.block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.labels = append(f.labels, u.Label())
	if err := f.failOn[u.Repo]; err != nil {
		return err
	}
	f.unloaded = append(f.unloaded, u.Repo)
	return nil
}

func (f *fakeFleetLoader) Status(u watchers.Unit) (watchers.WatcherStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return watchers.WatcherStatus{
		TaskName:  u.Label(),
		Installed: true,
		Running:   f.running[u.Repo],
	}, nil
}

// sawLabel reports whether the loader ever acted on the given label. Orphan
// units have no repo path, so the label is the only handle on them.
func (f *fakeFleetLoader) sawLabel(label string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.labels {
		if l == label {
			return true
		}
	}
	return false
}

func (f *fakeFleetLoader) sortedUnloaded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]string(nil), f.unloaded...)
	sort.Strings(out)
	return out
}

func (f *fakeFleetLoader) sortedLoaded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]string(nil), f.loaded...)
	sort.Strings(out)
	return out
}

// installFleetLoader swaps the fleet Loader constructor for the test.
func installFleetLoader(t *testing.T, f *fakeFleetLoader) {
	t.Helper()
	orig := newFleetWatcherLoader
	t.Cleanup(func() { newFleetWatcherLoader = orig })
	newFleetWatcherLoader = func() watchers.Loader { return f }
}

// fleetRepo describes one repo to seed into the fake registry.
type fleetRepo struct {
	path      string // filled in by seedFleet
	unitOnDsk bool
}

// seedFleet builds an isolated HOME + GRAFEL_HOME + XDG_CONFIG_HOME with a
// registry containing one group, and writes a watcher unit file for each repo
// flagged unitOnDsk. It returns the absolute repo paths in declaration order.
func seedFleet(t *testing.T, group string, watchersEnabled bool, repos []fleetRepo) []string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GRAFEL_HOME", filepath.Join(home, ".grafel"))
	t.Setenv(daemon.EnvRoot, filepath.Join(home, ".grafel"))

	cfg := &registry.GroupConfig{Name: group}
	cfg.Features.Watchers = watchersEnabled
	paths := make([]string, 0, len(repos))
	for i := range repos {
		p := filepath.Join(home, "repos", group, repos[i].path)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("mkdir repo: %v", err)
		}
		repos[i].path = p
		paths = append(paths, p)
		cfg.Repos = append(cfg.Repos, registry.Repo{Slug: filepath.Base(p), Path: p})
	}

	cfgPath, err := registry.ConfigPathFor(group)
	if err != nil {
		t.Fatalf("config path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(cfgPath, b, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	regPath, err := registry.RegistryPath()
	if err != nil {
		t.Fatalf("registry path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(regPath), 0o755); err != nil {
		t.Fatalf("mkdir registry dir: %v", err)
	}
	rb, _ := json.MarshalIndent(registry.Registry{
		Version: 1,
		Groups:  []registry.GroupRef{{Name: group, ConfigPath: cfgPath}},
	}, "", "  ")
	if err := os.WriteFile(regPath, rb, 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	for _, r := range repos {
		if !r.unitOnDsk {
			continue
		}
		u := watchers.Unit{Group: group, Repo: r.path, BinPath: "/usr/local/bin/grafel"}
		if _, err := watchers.Write(u); err != nil {
			t.Fatalf("write unit for %s: %v", r.path, err)
		}
	}
	return paths
}

// TestStopFleetWatchers_UnloadsEveryInstalledUnit is the core defect: a stop
// must deactivate every per-repo watcher unit that is installed on disk.
func TestStopFleetWatchers_UnloadsEveryInstalledUnit(t *testing.T) {
	repos := []fleetRepo{
		{path: "alpha", unitOnDsk: true},
		{path: "beta", unitOnDsk: true},
		{path: "gamma", unitOnDsk: false}, // no unit — nothing to stop
	}
	paths := seedFleet(t, "grp", true, repos)
	f := &fakeFleetLoader{}
	installFleetLoader(t, f)

	var buf bytes.Buffer
	res := stopFleetWatchers(&buf)

	if res.Total != 2 {
		t.Fatalf("Total = %d, want 2 (only installed units count)", res.Total)
	}
	if len(res.Failures) != 0 {
		t.Fatalf("unexpected failures: %v", res.Failures)
	}
	got := f.sortedUnloaded()
	want := []string{paths[0], paths[1]}
	sort.Strings(want)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("unloaded = %v, want %v", got, want)
	}
	if !strings.Contains(buf.String(), "watcher") {
		t.Fatalf("stop output does not mention watchers:\n%s", buf.String())
	}
}

// TestStopFleetWatchers_IgnoresFeatureFlag: a group whose fleet config has
// features.watchers=false can still have units on disk from an earlier install
// (the flag is not retroactive). Stop must mean stop for those too.
func TestStopFleetWatchers_IgnoresFeatureFlag(t *testing.T) {
	repos := []fleetRepo{{path: "alpha", unitOnDsk: true}}
	paths := seedFleet(t, "grp", false /* watchers disabled in config */, repos)
	f := &fakeFleetLoader{}
	installFleetLoader(t, f)

	res := stopFleetWatchers(io.Discard)
	if res.Total != 1 {
		t.Fatalf("Total = %d, want 1", res.Total)
	}
	if got := f.sortedUnloaded(); len(got) != 1 || got[0] != paths[0] {
		t.Fatalf("unloaded = %v, want [%s]", got, paths[0])
	}
}

// TestStopFleetWatchers_ReportsWhatItCouldNotStop: silence is what produced
// the report. A watcher that fails to deactivate must be named.
func TestStopFleetWatchers_ReportsWhatItCouldNotStop(t *testing.T) {
	repos := []fleetRepo{
		{path: "alpha", unitOnDsk: true},
		{path: "beta", unitOnDsk: true},
	}
	paths := seedFleet(t, "grp", true, repos)
	f := &fakeFleetLoader{failOn: map[string]error{paths[1]: os.ErrPermission}}
	installFleetLoader(t, f)

	var buf bytes.Buffer
	res := stopFleetWatchers(&buf)

	if len(res.Failures) != 1 {
		t.Fatalf("Failures = %v, want exactly 1", res.Failures)
	}
	if !strings.Contains(res.Failures[0], paths[1]) {
		t.Fatalf("failure does not name the repo: %q", res.Failures[0])
	}
	out := buf.String()
	if !strings.Contains(out, paths[1]) || !strings.Contains(out, "still running") {
		t.Fatalf("stop output must name what it could not stop:\n%s", out)
	}
}

// TestRunDaemonStop_StopsTheWatcherFleet wires the fleet stop into the actual
// `grafel stop` entry point. Before the fix runDaemonStop only stopped the
// daemon's own service.
func TestRunDaemonStop_StopsTheWatcherFleet(t *testing.T) {
	repos := []fleetRepo{{path: "alpha", unitOnDsk: true}}
	paths := seedFleet(t, "grp", true, repos)
	f := &fakeFleetLoader{}
	installFleetLoader(t, f)

	origInstalled := serviceInstalledForThisRoot
	origStop := serviceStopForThisRoot
	t.Cleanup(func() {
		serviceInstalledForThisRoot = origInstalled
		serviceStopForThisRoot = origStop
	})
	serviceInstalledForThisRoot = func() bool { return true }
	serviceStopForThisRoot = func(w io.Writer) error { return nil }

	var buf bytes.Buffer
	if err := runDaemonStop(&buf); err != nil {
		t.Fatalf("runDaemonStop: %v", err)
	}
	if got := f.sortedUnloaded(); len(got) != 1 || got[0] != paths[0] {
		t.Fatalf("grafel stop did not stop the watcher fleet: unloaded=%v", got)
	}
}

// TestRunDaemonStart_RestoresTheWatcherFleet: a stop that turns watchers off
// is only acceptable if start turns them back on.
func TestRunDaemonStart_RestoresTheWatcherFleet(t *testing.T) {
	repos := []fleetRepo{{path: "alpha", unitOnDsk: true}}
	paths := seedFleet(t, "grp", true, repos)
	f := &fakeFleetLoader{}
	installFleetLoader(t, f)

	origInstalled := serviceInstalledForThisRoot
	origRestart := serviceRestartForThisRoot
	t.Cleanup(func() {
		serviceInstalledForThisRoot = origInstalled
		serviceRestartForThisRoot = origRestart
	})
	serviceInstalledForThisRoot = func() bool { return true }
	serviceRestartForThisRoot = func(w io.Writer) error { return nil }

	var buf bytes.Buffer
	if err := runDaemonStartOpts(&buf, 0, false); err != nil {
		t.Fatalf("runDaemonStartOpts: %v", err)
	}
	if got := f.sortedLoaded(); len(got) != 1 || got[0] != paths[0] {
		t.Fatalf("grafel start did not restore the watcher fleet: loaded=%v", got)
	}
}

// TestStartFleetWatchers_RespectsDisabledFeature: start must not resurrect
// watchers for a group the user turned watchers off for. (Stop is unconditional;
// start is not — that asymmetry is deliberate.)
func TestStartFleetWatchers_RespectsDisabledFeature(t *testing.T) {
	repos := []fleetRepo{{path: "alpha", unitOnDsk: true}}
	seedFleet(t, "grp", false, repos)
	f := &fakeFleetLoader{}
	installFleetLoader(t, f)

	res := startFleetWatchers(io.Discard)
	if res.Total != 0 {
		t.Fatalf("Total = %d, want 0 (feature disabled)", res.Total)
	}
	if got := f.sortedLoaded(); len(got) != 0 {
		t.Fatalf("start resurrected watchers for a watchers=false group: %v", got)
	}
}

// TestStatusReportsRunningWatchers: `grafel status` must tell the truth about
// the fleet — if watchers are still running after a stop, status must say so.
func TestStatusReportsRunningWatchers(t *testing.T) {
	repos := []fleetRepo{
		{path: "alpha", unitOnDsk: true},
		{path: "beta", unitOnDsk: true},
	}
	paths := seedFleet(t, "grp", true, repos)
	f := &fakeFleetLoader{running: map[string]bool{paths[0]: true}}
	installFleetLoader(t, f)

	var buf bytes.Buffer
	if err := runStatus(&buf, "", "", false); err != nil {
		t.Fatalf("runStatus: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Watchers:") {
		t.Fatalf("status has no Watchers section:\n%s", out)
	}
	if !strings.Contains(out, "1 running") {
		t.Fatalf("status does not report the running watcher count:\n%s", out)
	}
}

// TestDarwinWatcherUnloadIsPersistent: on macOS, `launchctl bootout` only
// clears the CURRENT login session — the plist stays in ~/Library/LaunchAgents
// and RunAtLoad fires again at next login. The daemon's own service already
// pairs bootout with `launchctl disable` for exactly this reason (#6044,
// internal/daemon/service/launchd_darwin.go Unload). The per-repo watcher
// loader must do the same, and Load must re-enable.
func TestDarwinWatcherUnloadIsPersistent(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only: launchctl disable/enable semantics")
	}
	var calls []string
	restore := watchers.SetLaunchctlRunnerForTest(func(args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		return nil, nil
	})
	t.Cleanup(restore)

	home := t.TempDir()
	t.Setenv("HOME", home)
	u := watchers.Unit{Group: "grp", Repo: filepath.Join(home, "alpha"), BinPath: "/usr/local/bin/grafel"}
	if _, err := watchers.Write(u); err != nil {
		t.Fatalf("write unit: %v", err)
	}

	if err := watchers.NewLoader().Unload(u); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, "bootout") {
		t.Fatalf("Unload did not bootout:\n%s", joined)
	}
	if !strings.Contains(joined, "disable") {
		t.Fatalf("Unload did not persist the stop (no `launchctl disable`):\n%s", joined)
	}

	calls = nil
	if err := watchers.NewLoader().Load(u); err != nil {
		t.Fatalf("Load: %v", err)
	}
	joined = strings.Join(calls, "\n")
	if !strings.Contains(joined, "enable") {
		t.Fatalf("Load did not clear a persisted disable (no `launchctl enable`):\n%s", joined)
	}
	ei := strings.Index(joined, "enable")
	bi := strings.Index(joined, "bootstrap")
	if bi >= 0 && ei > bi {
		t.Fatalf("`launchctl enable` must precede bootstrap:\n%s", joined)
	}
}
