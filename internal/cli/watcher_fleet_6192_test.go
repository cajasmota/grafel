package cli

// watcher_fleet_6192_test.go — `features.watchers: false` is not retroactive.
//
// The flag gates three things and only three: whether `grafel install` WRITES a
// unit (internal/install/install.go), whether ReconcileWatcherUnits rewrites and
// re-registers one (internal/install/watchersync.go), and whether
// `grafel start` re-activates one (startFleetWatchers, this package). Nothing
// deregisters a unit that is already installed and loaded, so a group whose
// flag is flipped to false keeps its watcher running — indefinitely, and today
// silently. These tests pin the surfacing of that disagreement in
// `grafel status`.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/install/watchers"
	"github.com/cajasmota/grafel/internal/registry"
)

// 6192-A: a running watcher belonging to a group with features.watchers off is
// the exact state the issue describes. `grafel status` must name it rather than
// leave the user to find it in `launchctl list`.
func TestStatus_NamesRunningWatcherOfWatchersOffGroup(t *testing.T) {
	repos := []fleetRepo{{path: "alpha", unitOnDsk: true}}
	paths := seedFleet(t, "grp", false /* features.watchers off */, repos)
	installFleetLoader(t, &fakeFleetLoader{running: map[string]bool{paths[0]: true}})

	var buf bytes.Buffer
	if err := runStatus(&buf, "", "", false); err != nil {
		t.Fatalf("runStatus: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, paths[0]) {
		t.Fatalf("status did not name the running watcher of a watchers-off group:\n%s", out)
	}
	if !strings.Contains(out, "watchers disabled") {
		t.Fatalf("status did not explain that the group has watchers disabled:\n%s", out)
	}
}

// 6192-A2: the notice must carry the way out. `grafel stop` deactivates every
// installed unit whatever the flag says, and `grafel start` restores only the
// groups whose flag is on (see the contract at the head of watcher_fleet.go),
// so that pair is what clears this state.
func TestStatus_WatchersOffNoticeNamesTheRemedy(t *testing.T) {
	repos := []fleetRepo{{path: "alpha", unitOnDsk: true}}
	paths := seedFleet(t, "grp", false, repos)
	installFleetLoader(t, &fakeFleetLoader{running: map[string]bool{paths[0]: true}})

	var buf bytes.Buffer
	if err := runStatus(&buf, "", "", false); err != nil {
		t.Fatalf("runStatus: %v", err)
	}
	out := buf.String()
	// Anchored on the notice itself. The pre-existing fleet line already says
	// "'grafel stop' stops them too", so a bare Contains(out, "grafel stop")
	// would pass against unfixed code — an assertion satisfied by text that has
	// nothing to do with the flag. The remedy is the stop/start PAIR, and it has
	// to appear inside the block that names the disagreement.
	i := strings.Index(out, "watchers disabled")
	if i < 0 {
		t.Fatalf("status did not name the disagreement at all:\n%s", out)
	}
	notice := out[i:]
	if !strings.Contains(notice, "grafel stop") || !strings.Contains(notice, "grafel start") {
		t.Fatalf("the watchers-off notice does not carry the stop/start remedy:\n%s", notice)
	}
}

// 6192-B: the notice is about a LIVE watcher. An installed-but-not-running unit
// is not the reported defect and must not produce the warning — otherwise every
// machine that has ever had watchers on warns forever, and a warning that is
// always on is a warning nobody reads.
func TestStatus_QuietWhenWatchersOffUnitIsNotRunning(t *testing.T) {
	repos := []fleetRepo{{path: "alpha", unitOnDsk: true}}
	seedFleet(t, "grp", false, repos)
	installFleetLoader(t, &fakeFleetLoader{}) // running: nothing

	var buf bytes.Buffer
	if err := runStatus(&buf, "", "", false); err != nil {
		t.Fatalf("runStatus: %v", err)
	}
	if out := buf.String(); strings.Contains(out, "watchers disabled") {
		t.Fatalf("status warned about a unit that is not running:\n%s", out)
	}
}

// 6192-C: a running watcher whose group has the flag ON is the normal, correct
// state and must stay unremarked.
func TestStatus_QuietWhenGroupHasWatchersOn(t *testing.T) {
	repos := []fleetRepo{{path: "alpha", unitOnDsk: true}}
	paths := seedFleet(t, "grp", true, repos)
	installFleetLoader(t, &fakeFleetLoader{running: map[string]bool{paths[0]: true}})

	var buf bytes.Buffer
	if err := runStatus(&buf, "", "", false); err != nil {
		t.Fatalf("runStatus: %v", err)
	}
	if out := buf.String(); strings.Contains(out, "watchers disabled") {
		t.Fatalf("status warned about a group whose watchers are enabled:\n%s", out)
	}
}

// 6192-C2: a running ORPHAN — a unit on disk that no registered group owns —
// is also non-restorable, but no group flag disagrees with it. Attributing it
// to "a group with watchers disabled" would name a group that does not exist;
// `grafel stop` reports orphans separately and with a different remedy.
func TestStatus_OrphanUnitIsNotReportedAsWatchersOff(t *testing.T) {
	seedFleet(t, "grp", true, nil)
	dir, err := watchers.UnitDir()
	if err != nil {
		t.Fatalf("UnitDir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir unit dir: %v", err)
	}
	orphan := filepath.Join(dir, "com.grafel.watcher.deletedgroup.orphan"+unitExtForTest(t))
	if err := os.WriteFile(orphan, []byte("orphan"), 0o644); err != nil {
		t.Fatalf("write orphan: %v", err)
	}
	// The orphan carries no repo path, so the fake loader keys "running" on the
	// empty string — which is exactly what an orphan unit's Repo field is.
	installFleetLoader(t, &fakeFleetLoader{running: map[string]bool{"": true}})

	var buf bytes.Buffer
	if err := runStatus(&buf, "", "", false); err != nil {
		t.Fatalf("runStatus: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "1 running") {
		t.Fatalf("precondition: the orphan should be counted as running:\n%s", out)
	}
	if strings.Contains(out, "watchers disabled") {
		t.Fatalf("an orphan was blamed on a group flag no group has:\n%s", out)
	}
}

// 6192-D: the TRANSITION, not its endpoints.
//
// The defect is not "a watchers-off group has a running unit" as a static fact;
// it is that flipping the flag while a unit is loaded changes nothing about the
// unit. Testing only the two endpoint states would pass against a status line
// that read some other input entirely. This drives the actual sequence:
// enabled + running (quiet) → flag flipped in fleet.json → same unit, same
// loader, still running (named).
func TestStatus_WarningAppearsOnlyAfterTheFlagIsFlipped(t *testing.T) {
	repos := []fleetRepo{{path: "alpha", unitOnDsk: true}}
	paths := seedFleet(t, "grp", true /* starts ENABLED */, repos)
	loader := &fakeFleetLoader{running: map[string]bool{paths[0]: true}}
	installFleetLoader(t, loader)

	var before bytes.Buffer
	if err := runStatus(&before, "", "", false); err != nil {
		t.Fatalf("runStatus (before): %v", err)
	}
	if strings.Contains(before.String(), "watchers disabled") {
		t.Fatalf("status warned before the flag was flipped:\n%s", before.String())
	}

	flipWatchersFlag(t, "grp", false)

	var after bytes.Buffer
	if err := runStatus(&after, "", "", false); err != nil {
		t.Fatalf("runStatus (after): %v", err)
	}
	out := after.String()
	if !strings.Contains(out, "watchers disabled") || !strings.Contains(out, paths[0]) {
		t.Fatalf("flipping features.watchers to false did not surface the still-running watcher:\n%s", out)
	}
	// The unit itself was never touched: no Load, no Unload. The flag is not
	// retroactive, and this is the assertion that says the fix SURFACES that
	// rather than silently changing it.
	if got := loader.sortedUnloaded(); len(got) != 0 {
		t.Fatalf("status must not deactivate anything; unloaded=%v", got)
	}
	if got := loader.sortedLoaded(); len(got) != 0 {
		t.Fatalf("status must not activate anything; loaded=%v", got)
	}
}

// flipWatchersFlag rewrites the on-disk group config with a new
// features.watchers value, which is what a fleet.json hand-edit, the dashboard
// PATCH /api/v2/groups/{group}/features handler, and the wizard all do.
func flipWatchersFlag(t *testing.T, group string, on bool) {
	t.Helper()
	cfgPath, err := registry.ConfigPathFor(group)
	if err != nil {
		t.Fatalf("config path: %v", err)
	}
	cfg, err := registry.LoadGroupConfig(cfgPath)
	if err != nil {
		t.Fatalf("load group config: %v", err)
	}
	cfg.Features.Watchers = on
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal group config: %v", err)
	}
	if err := os.WriteFile(cfgPath, b, 0o644); err != nil {
		t.Fatalf("write group config: %v", err)
	}
}
