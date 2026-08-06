package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/install/watchers"
	"github.com/cajasmota/grafel/internal/registry"
)

// fakeLoader records Load calls instead of shelling out to launchctl.
type fakeLoader struct {
	loaded []string
	// unloaded records deregistrations by LABEL — #6183's migration is only
	// correct if it boots out the OLD label, which the repo path cannot show.
	unloaded []string
	// statused counts Status queries — Apply asks for one on the SAME loader it
	// activated with, so a second construction would be another escape hatch.
	statused int
	fail     map[string]error
}

func (f *fakeLoader) Load(u watchers.Unit) error {
	if err, ok := f.fail[u.Repo]; ok {
		return err
	}
	f.loaded = append(f.loaded, u.Repo)
	return nil
}
func (f *fakeLoader) Unload(u watchers.Unit) error {
	f.unloaded = append(f.unloaded, u.Label())
	return nil
}
func (f *fakeLoader) Status(u watchers.Unit) (watchers.WatcherStatus, error) {
	f.statused++
	return watchers.WatcherStatus{TaskName: u.Label()}, nil
}

// reconcileSandbox redirects HOME, GRAFEL_HOME and XDG_CONFIG_HOME into a temp
// dir so unit paths, the registry and group configs are all sandboxed. Nothing
// here ever invokes launchctl or touches the real ~/Library/LaunchAgents.
func reconcileSandbox(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("GRAFEL_HOME", filepath.Join(dir, ".grafel"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	return dir
}

// registerGroup writes a group config with watchers enabled and adds it to the
// registry, returning the repo paths.
func registerGroup(t *testing.T, home, group string, repos []string, watchersOn bool) {
	t.Helper()
	cfgDir, err := registry.ConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := registry.GroupConfig{Name: group}
	cfg.Features.Watchers = watchersOn
	for _, r := range repos {
		if err := os.MkdirAll(r, 0o755); err != nil {
			t.Fatal(err)
		}
		cfg.Repos = append(cfg.Repos, registry.Repo{Slug: filepath.Base(r), Path: r})
	}
	p := filepath.Join(cfgDir, group+".fleet.json")
	b, _ := json.Marshal(cfg)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := registry.AddGroup(group, p); err != nil {
		t.Fatal(err)
	}
}

func writeUnitFile(t *testing.T, u watchers.Unit, body string) string {
	t.Helper()
	p, err := watchers.UnitPath(u)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestReconcileWatcherUnits_RepairsStaleAndSkipsCurrent is the core of #6179
// F1. Existing installs carry pre-fix unit files; something on the upgrade path
// has to rewrite them, and it must not re-register the ones that are already
// correct — every re-registration is a launchctl bootout+bootstrap that macOS
// posts a Background Items notification for, and 140 of those at once is the
// burst the issue reports.
func TestReconcileWatcherUnits_RepairsStaleAndSkipsCurrent(t *testing.T) {
	home := reconcileSandbox(t)
	bin := filepath.Join(home, "bin", "grafel")

	stale := filepath.Join(home, "repos", "stale")
	current := filepath.Join(home, "repos", "current")
	absent := filepath.Join(home, "repos", "absent")
	registerGroup(t, home, "g", []string{stale, current, absent}, true)

	staleUnit := watchers.Unit{Group: "g", Repo: stale, BinPath: bin}
	currentUnit := watchers.Unit{Group: "g", Repo: current, BinPath: bin}

	// A pre-fix unit body, and an already-correct one.
	stalePath := writeUnitFile(t, staleUnit, "OLD UNIT BODY - unconditional KeepAlive\n")
	currentPath := writeUnitFile(t, currentUnit, watchers.Render(currentUnit))

	fake := &fakeLoader{}
	prev := newWatcherLoader
	newWatcherLoader = func() watchers.Loader { return fake }
	t.Cleanup(func() { newWatcherLoader = prev })

	res, err := ReconcileWatcherUnits(ReconcileWatcherOptions{BinPath: bin})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Rewritten) != 1 || res.Rewritten[0] != stalePath {
		t.Errorf("Rewritten = %v, want exactly [%s]", res.Rewritten, stalePath)
	}
	if res.Current != 1 {
		t.Errorf("Current = %d, want 1 (the already-correct unit must not be rewritten)", res.Current)
	}
	if res.Absent != 1 {
		t.Errorf("Absent = %d, want 1 (a repo with no unit file must be left alone)", res.Absent)
	}
	if len(fake.loaded) != 1 || fake.loaded[0] != stale {
		t.Errorf("re-registered %v, want only the stale repo %q — re-registering a "+
			"unit whose content did not change is a Background Items notification "+
			"for no benefit (#6179)", fake.loaded, stale)
	}

	// The stale file must now carry the current template...
	got, err := os.ReadFile(stalePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != watchers.Render(staleUnit) {
		t.Errorf("stale unit was not rewritten to the current template:\n%s", got)
	}
	// ...and the already-correct one must be byte-identical, untouched.
	if b, _ := os.ReadFile(currentPath); string(b) != watchers.Render(currentUnit) {
		t.Errorf("current unit was modified")
	}
	// No unit invented for the repo that had none.
	absentPath, _ := watchers.UnitPath(watchers.Unit{Group: "g", Repo: absent, BinPath: bin})
	if _, err := os.Stat(absentPath); !os.IsNotExist(err) {
		t.Errorf("reconcile created a unit for a repo that had none: %s", absentPath)
	}
}

// TestReconcileWatcherUnits_NoOpWhenAllCurrent proves the operation is free on
// an up-to-date machine — no writes, and critically no loader constructed at
// all, so nothing shells out to launchctl.
func TestReconcileWatcherUnits_NoOpWhenAllCurrent(t *testing.T) {
	home := reconcileSandbox(t)
	bin := filepath.Join(home, "bin", "grafel")
	repo := filepath.Join(home, "repos", "a")
	registerGroup(t, home, "g", []string{repo}, true)
	u := watchers.Unit{Group: "g", Repo: repo, BinPath: bin}
	writeUnitFile(t, u, watchers.Render(u))

	constructed := 0
	prev := newWatcherLoader
	newWatcherLoader = func() watchers.Loader {
		constructed++
		return &fakeLoader{}
	}
	t.Cleanup(func() { newWatcherLoader = prev })

	res, err := ReconcileWatcherUnits(ReconcileWatcherOptions{BinPath: bin})
	if err != nil {
		t.Fatal(err)
	}
	if constructed != 0 {
		t.Errorf("constructed a Loader %d times on an up-to-date machine; reconcile must "+
			"not touch the service manager when nothing changed", constructed)
	}
	if len(res.Rewritten) != 0 || res.Current != 1 {
		t.Errorf("Rewritten=%v Current=%d, want no rewrites and 1 current", res.Rewritten, res.Current)
	}
}

// TestReconcileWatcherUnits_SkipsGroupsWithWatchersDisabled: a group that opted
// out of watchers must not have units written or loaded for it.
func TestReconcileWatcherUnits_SkipsGroupsWithWatchersDisabled(t *testing.T) {
	home := reconcileSandbox(t)
	bin := filepath.Join(home, "bin", "grafel")
	repo := filepath.Join(home, "repos", "off")
	registerGroup(t, home, "g", []string{repo}, false)
	writeUnitFile(t, watchers.Unit{Group: "g", Repo: repo, BinPath: bin}, "OLD BODY\n")

	fake := &fakeLoader{}
	prev := newWatcherLoader
	newWatcherLoader = func() watchers.Loader { return fake }
	t.Cleanup(func() { newWatcherLoader = prev })

	res, err := ReconcileWatcherUnits(ReconcileWatcherOptions{BinPath: bin})
	if err != nil {
		t.Fatal(err)
	}
	if res.Examined != 0 || len(res.Rewritten) != 0 || len(fake.loaded) != 0 {
		t.Errorf("watchers-disabled group was reconciled: %+v loaded=%v", res, fake.loaded)
	}
}

// TestReconcileWatcherUnits_OneBadUnitDoesNotStopTheRest: at 140 repos a single
// failing re-registration must not abandon the other 139 stale plists.
func TestReconcileWatcherUnits_OneBadUnitDoesNotStopTheRest(t *testing.T) {
	home := reconcileSandbox(t)
	bin := filepath.Join(home, "bin", "grafel")
	bad := filepath.Join(home, "repos", "bad")
	good := filepath.Join(home, "repos", "good")
	registerGroup(t, home, "g", []string{bad, good}, true)
	writeUnitFile(t, watchers.Unit{Group: "g", Repo: bad, BinPath: bin}, "OLD BODY\n")
	writeUnitFile(t, watchers.Unit{Group: "g", Repo: good, BinPath: bin}, "OLD BODY\n")

	fake := &fakeLoader{fail: map[string]error{bad: os.ErrPermission}}
	prev := newWatcherLoader
	newWatcherLoader = func() watchers.Loader { return fake }
	t.Cleanup(func() { newWatcherLoader = prev })

	res, err := ReconcileWatcherUnits(ReconcileWatcherOptions{BinPath: bin})
	if err != nil {
		t.Fatalf("one bad unit must not fail the whole reconcile: %v", err)
	}
	if len(res.Rewritten) != 2 {
		t.Errorf("Rewritten = %v, want both files rewritten regardless of load failure", res.Rewritten)
	}
	if len(res.Reloaded) != 1 {
		t.Errorf("Reloaded = %v, want only the good one", res.Reloaded)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "re-register") {
		t.Errorf("Warnings = %v, want one re-register warning", res.Warnings)
	}
}
