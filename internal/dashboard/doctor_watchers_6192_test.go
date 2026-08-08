package dashboard

// doctor_watchers_6192_test.go — the group doctor's watcher check told the user
// something the machine was not doing.
//
// `features.watchers: false` gates creating, rewriting and starting watcher
// units; it does not deregister one that is already installed. So a group can
// have the flag off and a live watcher at the same time, and the doctor's
// unconditional "Disabled — changes won't trigger auto-reindex" was a claim
// about behaviour derived purely from the flag, with the machine never
// consulted. The mirror was wrong the same way: flag on reported "Enabled
// across N repos" whether or not a single unit existed.
//
// The check keys on the SAME predicate `grafel status` uses — a unit the OS
// reports as RUNNING — so the two surfaces cannot disagree about one machine.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/install/watchers"
	"github.com/cajasmota/grafel/internal/registry"
)

// fakeDoctorLoader reports a fixed running-set, keyed by repo path, and records
// nothing else: the doctor must never load or unload anything.
type fakeDoctorLoader struct {
	running  map[string]bool
	mutated  bool
	statuses int
}

func (f *fakeDoctorLoader) Load(watchers.Unit) error   { f.mutated = true; return nil }
func (f *fakeDoctorLoader) Unload(watchers.Unit) error { f.mutated = true; return nil }
func (f *fakeDoctorLoader) Status(u watchers.Unit) (watchers.WatcherStatus, error) {
	f.statuses++
	return watchers.WatcherStatus{TaskName: u.Label(), Installed: true, Running: f.running[u.Repo]}, nil
}

// installDoctorLoader swaps the doctor's Loader constructor for the test, so no
// real launchctl/systemctl is ever consulted.
func installDoctorLoader(t *testing.T, f *fakeDoctorLoader) {
	t.Helper()
	orig := newDoctorWatcherLoader
	t.Cleanup(func() { newDoctorWatcherLoader = orig })
	newDoctorWatcherLoader = func() watchers.Loader { return f }
}

// isolateWatcherUnitDir points the home env at a temp dir so unit files are
// written to and read from a sandbox, never the developer's real
// ~/Library/LaunchAgents (or systemd user dir).
func isolateWatcherUnitDir(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GRAFEL_HOME", filepath.Join(home, ".grafel"))
}

// doctorCheckByID returns the check with the given ID, failing if absent.
func doctorCheckByID(t *testing.T, checks []v2DoctorCheck, id string) v2DoctorCheck {
	t.Helper()
	for _, c := range checks {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no doctor check with id %q in %+v", id, checks)
	return v2DoctorCheck{}
}

// groupWithOneRepo builds a config with one repo and the given flag value.
func groupWithOneRepo(repoPath string, watchersOn bool) *registry.GroupConfig {
	cfg := &registry.GroupConfig{
		Name:  "grp",
		Repos: []registry.Repo{{Slug: "alpha", Path: repoPath}},
	}
	cfg.Features.Watchers = watchersOn
	return cfg
}

// writeUnitFor writes the repo's watcher unit into the sandboxed unit dir.
func writeUnitFor(t *testing.T, cfg *registry.GroupConfig, repoPath string) {
	t.Helper()
	u := watchers.Unit{Group: cfg.Name, Repo: repoPath, BinPath: "/usr/local/bin/grafel"}
	if _, err := watchers.Write(u); err != nil {
		t.Fatalf("write unit: %v", err)
	}
}

// 6192-E: flag off AND the unit still RUNNING — the state the issue reports.
// The doctor must report the machine, not read the flag back to the user.
func TestBuildDoctorChecks_WatchersOffButUnitStillRunning(t *testing.T) {
	isolateWatcherUnitDir(t)
	repo := filepath.Join(t.TempDir(), "alpha")
	cfg := groupWithOneRepo(repo, false)
	writeUnitFor(t, cfg, repo)
	f := &fakeDoctorLoader{running: map[string]bool{repo: true}}
	installDoctorLoader(t, f)

	c := doctorCheckByID(t, buildDoctorChecks(cfg), "watchers")
	if c.Status == "info" {
		t.Fatalf("doctor reported the flag and not the machine: %+v", c)
	}
	// The wire contract, not a synonym of it: DoctorCheck.status in
	// webui-v2/src/data/types.ts is the union "ok" | "warning" | "info" |
	// "error", so a check emitting "warn" would render as an unknown status.
	if c.Status != "warning" {
		t.Fatalf("status %q is not in the DoctorCheck union the WebUI declares", c.Status)
	}
	if !strings.Contains(c.Detail, "still running") {
		t.Fatalf("doctor detail does not describe the surviving watcher: %q", c.Detail)
	}
	if f.mutated {
		t.Fatal("the doctor loaded or unloaded a unit; it is a read-only check")
	}
}

// 6192-E2: the two surfaces must agree about one machine. `grafel status` goes
// quiet once the watcher is no longer running (its notice is gated on liveness),
// so a doctor keyed on the unit FILE would keep warning after the user followed
// the remedy — a permanent disagreement between two surfaces added together.
// Same predicate, same answer.
func TestBuildDoctorChecks_WatchersOffUnitInstalledButNotRunning(t *testing.T) {
	isolateWatcherUnitDir(t)
	repo := filepath.Join(t.TempDir(), "alpha")
	cfg := groupWithOneRepo(repo, false)
	writeUnitFor(t, cfg, repo) // file present…
	installDoctorLoader(t, &fakeDoctorLoader{})

	c := doctorCheckByID(t, buildDoctorChecks(cfg), "watchers")
	if c.Status != "info" {
		t.Fatalf("a stopped watcher must not warn — 'grafel status' does not: %+v", c)
	}
	if !strings.Contains(c.Detail, "won't trigger auto-reindex") {
		t.Fatalf("doctor lost the plain disabled explanation: %q", c.Detail)
	}
}

// 6192-F: flag off and no unit at all — the honest, quiet case.
func TestBuildDoctorChecks_WatchersOffAndNoUnitInstalled(t *testing.T) {
	isolateWatcherUnitDir(t)
	cfg := groupWithOneRepo(filepath.Join(t.TempDir(), "alpha"), false)
	f := &fakeDoctorLoader{}
	installDoctorLoader(t, f)

	c := doctorCheckByID(t, buildDoctorChecks(cfg), "watchers")
	if c.Status != "info" {
		t.Fatalf("a watchers-off group with no unit installed must stay informational: %+v", c)
	}
	if f.statuses != 0 {
		t.Fatalf("the doctor queried the service manager for a repo with no unit file (%d calls)", f.statuses)
	}
}

// 6192-G: the TRANSITION. A group installed with watchers ON and its unit
// running reports ok; the flag alone is then flipped and the same check must
// change its answer, with the unit untouched. Two separately-built fixtures
// would pass against a check that read the flag and nothing else — the defect.
func TestBuildDoctorChecks_WatchersFlagFlippedWithUnitRunning(t *testing.T) {
	isolateWatcherUnitDir(t)
	repo := filepath.Join(t.TempDir(), "alpha")
	cfg := groupWithOneRepo(repo, true)
	writeUnitFor(t, cfg, repo)
	installDoctorLoader(t, &fakeDoctorLoader{running: map[string]bool{repo: true}})

	if c := doctorCheckByID(t, buildDoctorChecks(cfg), "watchers"); c.Status != "ok" {
		t.Fatalf("an enabled group with its unit running must be ok: %+v", c)
	}

	cfg.Features.Watchers = false // the only thing that changes

	if c := doctorCheckByID(t, buildDoctorChecks(cfg), "watchers"); c.Status != "warning" {
		t.Fatalf("flipping the flag with the watcher live must be reported as a disagreement: %+v", c)
	}
}

// 6192-H: the MIRROR. Flag on with no unit installed is the same defect in the
// other direction — "Enabled across N repos" is a claim about the machine
// derived from the flag, and nothing is watching anything.
func TestBuildDoctorChecks_WatchersOnButNoUnitInstalled(t *testing.T) {
	isolateWatcherUnitDir(t)
	cfg := groupWithOneRepo(filepath.Join(t.TempDir(), "alpha"), true)
	installDoctorLoader(t, &fakeDoctorLoader{})

	c := doctorCheckByID(t, buildDoctorChecks(cfg), "watchers")
	if c.Status != "warning" {
		t.Fatalf("watchers on with 0 of 1 units installed must not report ok: %+v", c)
	}
	if !strings.Contains(c.Detail, "grafel install") {
		t.Fatalf("the enabled-but-uninstalled detail must name the remedy that writes units: %q", c.Detail)
	}
}

// 6192-H2: flag on with the unit installed is the ordinary healthy state and
// must stay "ok" — the mirror fix must not turn every enabled group into a
// warning. Liveness is deliberately NOT required here: an enabled unit that is
// momentarily not running is launchd's business, and `grafel status` already
// reports installed-vs-running for the whole fleet.
func TestBuildDoctorChecks_WatchersOnWithUnitInstalledIsOK(t *testing.T) {
	isolateWatcherUnitDir(t)
	repo := filepath.Join(t.TempDir(), "alpha")
	cfg := groupWithOneRepo(repo, true)
	writeUnitFor(t, cfg, repo)
	installDoctorLoader(t, &fakeDoctorLoader{})

	c := doctorCheckByID(t, buildDoctorChecks(cfg), "watchers")
	if c.Status != "ok" {
		t.Fatalf("an enabled group with its unit installed must be ok: %+v", c)
	}
}
