package cli

// watcher_fleet_review_test.go pins the defects an adversarial review of the
// first fleet-stop cut found. Each test is named for the property it protects.

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/install/watchers"
)

// R1: `grafel stop` must NOT exit 0 after telling the user watchers are still
// running. The first cut discarded the fleet result entirely, so a stop that
// deactivated 0 of 140 watchers returned nil and any `&&` chain read success.
func TestRunDaemonStop_FailsWhenWatchersSurvive(t *testing.T) {
	repos := []fleetRepo{
		{path: "alpha", unitOnDsk: true},
		{path: "beta", unitOnDsk: true},
	}
	paths := seedFleet(t, "grp", true, repos)
	f := &fakeFleetLoader{failOn: map[string]error{
		paths[0]: os.ErrPermission,
		paths[1]: os.ErrPermission,
	}}
	installFleetLoader(t, f)
	stubDaemonStopSeam(t, nil)

	var buf bytes.Buffer
	err := runDaemonStop(&buf)
	if err == nil {
		t.Fatalf("runDaemonStop returned nil after 0/2 watchers stopped; output:\n%s", buf.String())
	}
	if !errors.Is(err, errWatchersNotStopped) {
		t.Fatalf("want errWatchersNotStopped, got %v", err)
	}
}

// R1b: the LAST thing the user sees must not be an unqualified success line
// while watchers are still running. On 140 repos the warnings would otherwise
// scroll above "daemon stopped".
func TestRunDaemonStop_WarningsComeAfterTheDaemonLine(t *testing.T) {
	repos := []fleetRepo{{path: "alpha", unitOnDsk: true}}
	paths := seedFleet(t, "grp", true, repos)
	f := &fakeFleetLoader{failOn: map[string]error{paths[0]: os.ErrPermission}}
	installFleetLoader(t, f)
	stubDaemonStopSeam(t, func(w io.Writer) error {
		_, _ = io.WriteString(w, "daemon stopped\n")
		return nil
	})

	var buf bytes.Buffer
	_ = runDaemonStop(&buf)
	out := buf.String()
	di := strings.Index(out, "daemon stopped")
	wi := strings.Index(out, "still running")
	if di < 0 || wi < 0 {
		t.Fatalf("missing daemon line or warning:\n%s", out)
	}
	if wi < di {
		t.Fatalf("watcher warning must come AFTER the daemon line:\n%s", out)
	}
}

// R1c: a fleet-stop failure must not prevent the daemon from being stopped.
func TestRunDaemonStop_StopsDaemonEvenWhenFleetFails(t *testing.T) {
	repos := []fleetRepo{{path: "alpha", unitOnDsk: true}}
	paths := seedFleet(t, "grp", true, repos)
	f := &fakeFleetLoader{failOn: map[string]error{paths[0]: os.ErrPermission}}
	installFleetLoader(t, f)
	called := 0
	stubDaemonStopSeam(t, func(io.Writer) error { called++; return nil })

	_ = runDaemonStop(io.Discard)
	if called != 1 {
		t.Fatalf("daemon stop called %d times, want 1", called)
	}
}

// R3: enumeration must come from DISK, not only from the registry. An orphan
// unit — left by a partially-failed watchers.Cleanup, an unreadable group
// config, a group dropped from registry.json, or a slug-derivation change —
// is a live watcher the registry cannot see.
func TestStopFleetWatchers_StopsOrphanUnitsOnDisk(t *testing.T) {
	repos := []fleetRepo{{path: "alpha", unitOnDsk: true}}
	seedFleet(t, "grp", true, repos)

	// An orphan plist for a group that is not in the registry at all.
	dir, err := watchers.UnitDir()
	if err != nil {
		t.Fatalf("UnitDir: %v", err)
	}
	orphanLabel := "com.grafel.watcher.deletedgroup.orphan"
	orphanPath := filepath.Join(dir, orphanLabel+unitExtForTest(t))
	if err := os.WriteFile(orphanPath, []byte("orphan"), 0o644); err != nil {
		t.Fatalf("write orphan: %v", err)
	}

	f := &fakeFleetLoader{}
	installFleetLoader(t, f)
	var buf bytes.Buffer
	res := stopFleetWatchers(&buf)

	if res.Total != 2 {
		t.Fatalf("Total = %d, want 2 (registered + orphan); output:\n%s", res.Total, buf.String())
	}
	if !f.sawLabel(orphanLabel) {
		t.Fatalf("orphan unit %s was never unloaded; unloaded=%v", orphanLabel, f.sortedUnloaded())
	}
}

// R3b: an orphan must be counted by status too, or status under-reports what
// is running after a stop.
func TestStatusCountsOrphanUnits(t *testing.T) {
	seedFleet(t, "grp", true, []fleetRepo{{path: "alpha", unitOnDsk: true}})
	dir, _ := watchers.UnitDir()
	orphan := filepath.Join(dir, "com.grafel.watcher.deletedgroup.orphan"+unitExtForTest(t))
	if err := os.WriteFile(orphan, []byte("orphan"), 0o644); err != nil {
		t.Fatalf("write orphan: %v", err)
	}
	installFleetLoader(t, &fakeFleetLoader{})

	var buf bytes.Buffer
	if err := runStatus(&buf, "", "", false); err != nil {
		t.Fatalf("runStatus: %v", err)
	}
	if !strings.Contains(buf.String(), "2 installed") {
		t.Fatalf("status did not count the orphan unit:\n%s", buf.String())
	}
}

// R4: `grafel restart` on a service-installed root took an early-return branch
// that never touched the fleet, so `stop` then `restart` left watchers
// persistently disabled with the daemon up and no hint of the way out.
func TestRunDaemonRestart_RestoresTheFleetOnServiceRoot(t *testing.T) {
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
	serviceRestartForThisRoot = func(io.Writer) error { return nil }

	var buf bytes.Buffer
	if err := runDaemonRestart(&buf); err != nil {
		t.Fatalf("runDaemonRestart: %v", err)
	}
	if got := f.sortedLoaded(); len(got) != 1 || got[0] != paths[0] {
		t.Fatalf("restart did not restore the fleet: loaded=%v out=%q", got, buf.String())
	}
}

// R5: stop deactivates units whose group has watchers=false, but start will
// not bring them back. That is the intended asymmetry — but it is a one-way
// door, so stop must SAY so rather than claim to be reversible.
func TestStopFleetWatchers_NamesUnitsStartWillNotRestore(t *testing.T) {
	repos := []fleetRepo{{path: "alpha", unitOnDsk: true}}
	paths := seedFleet(t, "grp", false /* watchers disabled */, repos)
	installFleetLoader(t, &fakeFleetLoader{})

	var buf bytes.Buffer
	stopFleetWatchers(&buf)
	out := buf.String()
	if !strings.Contains(out, paths[0]) {
		t.Fatalf("stop did not name the one-way-door unit:\n%s", out)
	}
	if !strings.Contains(out, "grafel install") {
		t.Fatalf("stop did not tell the user how to get it back:\n%s", out)
	}
}

// R5b: the remedy printed for a TRUE orphan was wrong. `grafel install` only
// writes units for REGISTERED repos, so telling the owner of a unit whose group
// is not in the registry to run `grafel install` sends them somewhere that
// cannot help — in exactly the case the message was added to cover.
func TestStopFleetWatchers_OrphanRemedyIsNotGrafelInstall(t *testing.T) {
	seedFleet(t, "grp", true, []fleetRepo{{path: "alpha", unitOnDsk: true}})
	dir, _ := watchers.UnitDir()
	orphanLabel := "com.grafel.watcher.deletedgroup.orphan"
	if err := os.WriteFile(filepath.Join(dir, orphanLabel+unitExtForTest(t)), []byte("x"), 0o644); err != nil {
		t.Fatalf("write orphan: %v", err)
	}
	installFleetLoader(t, &fakeFleetLoader{})

	var buf bytes.Buffer
	stopFleetWatchers(&buf)
	out := buf.String()

	// The orphan must be named...
	oi := strings.Index(out, orphanLabel)
	if oi < 0 {
		t.Fatalf("orphan not named in stop output:\n%s", out)
	}
	// ...and the line that names it must not be under a "run grafel install"
	// heading. Split the output at the orphan section and check its remedy.
	orphanSection := out[oi:]
	if strings.Contains(orphanSection, "grafel install") {
		t.Fatalf("orphan remedy points at 'grafel install', which cannot restore an "+
			"unregistered unit:\n%s", out)
	}
	if !strings.Contains(out, "no registered group owns") {
		t.Fatalf("stop does not explain that the orphan has no owning group:\n%s", out)
	}
}

// R5c: the watchers=false case keeps the `grafel install` remedy, which IS
// correct there — the repo is registered, install re-writes and re-loads it.
func TestStopFleetWatchers_DisabledGroupRemedyIsGrafelInstall(t *testing.T) {
	paths := seedFleet(t, "grp", false, []fleetRepo{{path: "alpha", unitOnDsk: true}})
	installFleetLoader(t, &fakeFleetLoader{})

	var buf bytes.Buffer
	stopFleetWatchers(&buf)
	out := buf.String()
	if !strings.Contains(out, paths[0]) || !strings.Contains(out, "grafel install") {
		t.Fatalf("a registered repo whose group has watchers off must still be pointed at "+
			"'grafel install':\n%s", out)
	}
}

// R6: the sweep must be bounded. `launchctl bootout` blocks until the job
// exits and `grafel watch` drains on SIGTERM, so a wedged watcher could hang
// `grafel stop` forever with no output.
func TestStopFleetWatchers_BoundedByDeadline(t *testing.T) {
	repos := []fleetRepo{{path: "alpha", unitOnDsk: true}}
	paths := seedFleet(t, "grp", true, repos)

	orig := fleetSweepTimeout
	fleetSweepTimeout = 150 * time.Millisecond
	t.Cleanup(func() { fleetSweepTimeout = orig })

	f := &fakeFleetLoader{block: make(chan struct{})}
	t.Cleanup(func() { close(f.block) })
	installFleetLoader(t, f)

	done := make(chan fleetWatcherResult, 1)
	var buf bytes.Buffer
	go func() { done <- stopFleetWatchers(&buf) }()

	select {
	case res := <-done:
		if len(res.Failures) != 1 {
			t.Fatalf("a wedged unit must be reported as not stopped, got %v", res.Failures)
		}
		if !strings.Contains(res.Failures[0], paths[0]) {
			t.Fatalf("failure does not name the wedged repo: %q", res.Failures[0])
		}
	case <-time.After(10 * time.Second):
		t.Fatal("stopFleetWatchers hung past its deadline")
	}
}

// unitExtForTest returns the current platform's watcher-unit file extension by
// asking watchers.UnitPath, so the test does not hard-code a per-OS suffix.
func unitExtForTest(t *testing.T) string {
	t.Helper()
	p, err := watchers.UnitPath(watchers.Unit{Group: "g", Repo: "/r"})
	if err != nil {
		t.Fatalf("UnitPath: %v", err)
	}
	return filepath.Ext(p)
}

// isolateWatcherFleet points HOME / XDG_CONFIG_HOME / GRAFEL_HOME at temp
// directories so any code path that enumerates the watcher fleet sees an EMPTY
// fleet instead of the developer's real 140 registered repos and their real
// ~/Library/LaunchAgents.
//
// This is not belt-and-braces. `grafel start` now restores the fleet, so any
// test that drives runDaemonStartOpts/runDaemonRestart without this would call
// Loader.Load on the developer's REAL watcher units — i.e. `launchctl enable`
// + `bootstrap` against live jobs. The fail-closed guard in
// internal/install/watchers/test_isolation_guard.go catches that and panics
// (it caught exactly this in TestRunDaemonStartOpts_RoutesToServiceRestart_
// WhenInstalled, one call short of re-enabling a real user watcher), but the
// guard is the backstop; isolating the home is the fix.
func isolateWatcherFleet(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GRAFEL_HOME", filepath.Join(home, ".grafel"))
}

// stubDaemonStopSeam replaces the OS-service stop with a stub so runDaemonStop
// never touches a real service manager.
func stubDaemonStopSeam(t *testing.T, fn func(io.Writer) error) {
	t.Helper()
	// No isolateWatcherFleet here on purpose: every caller runs seedFleet
	// first, which already isolates HOME *and* seeds a registry. Re-isolating
	// would clobber that seed and silently reduce these tests to empty fleets.
	origInstalled := serviceInstalledForThisRoot
	origStop := serviceStopForThisRoot
	t.Cleanup(func() {
		serviceInstalledForThisRoot = origInstalled
		serviceStopForThisRoot = origStop
	})
	serviceInstalledForThisRoot = func() bool { return true }
	if fn == nil {
		fn = func(io.Writer) error { return nil }
	}
	serviceStopForThisRoot = fn
}
