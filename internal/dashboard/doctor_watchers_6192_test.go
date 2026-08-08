package dashboard

// doctor_watchers_6192_test.go — the group doctor's watcher check told the user
// something the machine was not doing.
//
// `features.watchers: false` gates creating, rewriting and starting watcher
// units; it never deregisters one that is already installed. So a group can
// have the flag off and a unit on disk at the same time, and the doctor's
// unconditional "Disabled — changes won't trigger auto-reindex" was a claim
// about behaviour derived purely from the flag, with the actual state of the
// machine never consulted.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/install/watchers"
	"github.com/cajasmota/grafel/internal/registry"
)

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

// watchersOffConfig builds a group config with watchers disabled and one repo.
func watchersOffConfig(repoPath string) *registry.GroupConfig {
	cfg := &registry.GroupConfig{
		Name:  "grp",
		Repos: []registry.Repo{{Slug: "alpha", Path: repoPath}},
	}
	cfg.Features.Watchers = false
	return cfg
}

// 6192-E: flag off AND a unit still installed — the doctor must report the
// disagreement, not repeat the flag back at the user.
func TestBuildDoctorChecks_WatchersOffButUnitStillInstalled(t *testing.T) {
	isolateWatcherUnitDir(t)
	repo := filepath.Join(t.TempDir(), "alpha")
	cfg := watchersOffConfig(repo)

	if _, err := watchers.Write(watchers.Unit{Group: cfg.Name, Repo: repo, BinPath: "/usr/local/bin/grafel"}); err != nil {
		t.Fatalf("write unit: %v", err)
	}

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
	if !strings.Contains(c.Detail, "still installed") {
		t.Fatalf("doctor detail does not mention the surviving unit: %q", c.Detail)
	}
}

// 6192-F: flag off and NO unit on disk — the honest, quiet case. The original
// wording is correct there and must survive.
func TestBuildDoctorChecks_WatchersOffAndNoUnitInstalled(t *testing.T) {
	isolateWatcherUnitDir(t)
	cfg := watchersOffConfig(filepath.Join(t.TempDir(), "alpha"))

	c := doctorCheckByID(t, buildDoctorChecks(cfg), "watchers")
	if c.Status != "info" {
		t.Fatalf("a watchers-off group with no unit installed must stay informational: %+v", c)
	}
	if !strings.Contains(c.Detail, "won't trigger auto-reindex") {
		t.Fatalf("doctor lost the plain disabled explanation: %q", c.Detail)
	}
}

// 6192-G: the TRANSITION. A group installed with watchers ON reports ok; the
// flag is then flipped off with the unit left exactly where it was, and the
// same check must change its answer. Asserting the two states from two
// separately-built fixtures would pass against a check that read the flag and
// nothing else — which is the defect.
func TestBuildDoctorChecks_WatchersFlagFlippedWithUnitInPlace(t *testing.T) {
	isolateWatcherUnitDir(t)
	repo := filepath.Join(t.TempDir(), "alpha")
	cfg := watchersOffConfig(repo)
	cfg.Features.Watchers = true

	if _, err := watchers.Write(watchers.Unit{Group: cfg.Name, Repo: repo, BinPath: "/usr/local/bin/grafel"}); err != nil {
		t.Fatalf("write unit: %v", err)
	}
	if c := doctorCheckByID(t, buildDoctorChecks(cfg), "watchers"); c.Status != "ok" {
		t.Fatalf("an enabled group with its unit installed must be ok: %+v", c)
	}

	cfg.Features.Watchers = false // the only thing that changes

	c := doctorCheckByID(t, buildDoctorChecks(cfg), "watchers")
	if c.Status == "ok" || c.Status == "info" {
		t.Fatalf("flipping the flag with the unit in place must be reported as a disagreement: %+v", c)
	}
}
