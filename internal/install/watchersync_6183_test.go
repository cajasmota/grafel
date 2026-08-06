package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/install/watchers"
)

// TestReconcileWatcherUnits_ConvergesWithDuplicateBasenames is the #6183
// repro.
//
// Two repos in one group whose basenames slug identically used to share a
// single plist path. Reconcile then rewrote that one file to repo A's body,
// found it stale for repo B, rewrote it to B's body, and re-registered both —
// every run, forever. The measured signature in the issue is run 2 onwards
// reporting `current=0 rewritten=2 reloaded=2`.
//
// The property asserted here is convergence: after one pass that settles the
// fleet, a second pass must do nothing at all — no rewrite, no launchctl, and
// (see the loader-construction test below) no Loader constructed.
func TestReconcileWatcherUnits_ConvergesWithDuplicateBasenames(t *testing.T) {
	home := reconcileSandbox(t)
	bin := filepath.Join(home, "bin", "grafel")

	// Same basename after slugging: "my-repo" and "my_repo" both slug to
	// "my-repo" under the pre-#6183 derivation.
	a := filepath.Join(home, "x", "my-repo")
	b := filepath.Join(home, "y", "my_repo")
	registerGroup(t, home, "g", []string{a, b}, true)

	ua := watchers.Unit{Group: "g", Repo: a, BinPath: bin}
	ub := watchers.Unit{Group: "g", Repo: b, BinPath: bin}

	// Pre-existing (stale) units, as an already-installed machine has.
	writeUnitFile(t, watchers.LegacyOf(ua), "OLD BODY A\n")
	writeUnitFile(t, watchers.LegacyOf(ub), "OLD BODY B\n")

	fake := &fakeLoader{}
	prev := newWatcherLoader
	newWatcherLoader = func() watchers.Loader { return fake }
	t.Cleanup(func() { newWatcherLoader = prev })

	// Run 1: the repair pass. It is allowed to do work.
	res1, err := ReconcileWatcherUnits(ReconcileWatcherOptions{BinPath: bin})
	if err != nil {
		t.Fatal(err)
	}
	loadsAfter1 := len(fake.loaded)
	t.Logf("run 1: current=%d rewritten=%d reloaded=%d migrated=%d (cumulative Loads=%d)",
		res1.Current, len(res1.Rewritten), len(res1.Reloaded), len(res1.Migrated), loadsAfter1)

	// After run 1 BOTH repos must be watched under their own distinct unit.
	pa, err := watchers.UnitPath(ua)
	if err != nil {
		t.Skipf("unsupported OS: %v", err)
	}
	pb, _ := watchers.UnitPath(ub)
	if pa == pb {
		t.Fatalf("both repos still share one unit path: %s", pa)
	}
	for _, p := range []string{pa, pb} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected a unit at %s after reconcile: %v", p, err)
		}
	}
	if got, _ := os.ReadFile(pa); string(got) != watchers.Render(ua) {
		t.Fatalf("unit A does not carry repo A's body")
	}
	if got, _ := os.ReadFile(pb); string(got) != watchers.Render(ub) {
		t.Fatalf("unit B does not carry repo B's body")
	}

	// Runs 2 and 3: the machine is current. Reconcile must do NOTHING.
	for run := 2; run <= 3; run++ {
		res, err := ReconcileWatcherUnits(ReconcileWatcherOptions{BinPath: bin})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("run %d: current=%d rewritten=%d reloaded=%d migrated=%d (cumulative Loads=%d)",
			run, res.Current, len(res.Rewritten), len(res.Reloaded), len(res.Migrated), len(fake.loaded))
		if len(res.Rewritten) != 0 {
			t.Fatalf("run %d rewrote %d units; reconcile never converges", run, len(res.Rewritten))
		}
		if len(res.Reloaded) != 0 {
			t.Fatalf("run %d re-registered %d units on an up-to-date machine", run, len(res.Reloaded))
		}
		if len(res.Migrated) != 0 {
			t.Fatalf("run %d migrated %d units; migration is not idempotent", run, len(res.Migrated))
		}
		if res.Current != 2 {
			t.Fatalf("run %d: current=%d, want 2 (both repos have their own current unit)", run, res.Current)
		}
		if len(fake.loaded) != loadsAfter1 {
			t.Fatalf("run %d called launchctl (cumulative Loads=%d, after run 1 it was %d)",
				run, len(fake.loaded), loadsAfter1)
		}
	}
}

// TestReconcileWatcherUnits_MigratesLegacyLabelAndDeregistersIt is the
// migration half of #6183 seen from reconcile: the pre-fix unit is booted out
// under its OLD label and deleted, and the repo ends up with exactly one unit.
func TestReconcileWatcherUnits_MigratesLegacyLabelAndDeregistersIt(t *testing.T) {
	home := reconcileSandbox(t)
	bin := filepath.Join(home, "bin", "grafel")
	repo := filepath.Join(home, "repos", "api")
	registerGroup(t, home, "g", []string{repo}, true)

	u := watchers.Unit{Group: "g", Repo: repo, BinPath: bin}
	legacyPath := writeUnitFile(t, watchers.LegacyOf(u), watchers.Render(watchers.LegacyOf(u)))
	newPath, err := watchers.UnitPath(u)
	if err != nil {
		t.Skipf("unsupported OS: %v", err)
	}

	fake := &fakeLoader{}
	prev := newWatcherLoader
	newWatcherLoader = func() watchers.Loader { return fake }
	t.Cleanup(func() { newWatcherLoader = prev })

	res, err := ReconcileWatcherUnits(ReconcileWatcherOptions{BinPath: bin})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Migrated) != 1 || res.Migrated[0] != legacyPath {
		t.Fatalf("Migrated = %v, want [%s]", res.Migrated, legacyPath)
	}
	if res.Absent != 0 {
		t.Fatalf("a repo whose unit exists under the legacy label was reported Absent")
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy unit file survived: %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("repo lost its watcher: %v", err)
	}
	// The OLD label must have been deregistered. Deleting the plist without a
	// bootout leaves the job loaded in launchd under a name nothing can derive
	// any more, and the new label bootstraps alongside it: two watchers.
	legacyLabel := watchers.LegacyOf(u).Label()
	found := false
	for _, l := range fake.unloaded {
		if l == legacyLabel {
			found = true
		}
		if l == u.Label() {
			t.Fatalf("deregistered the NEW label %s", l)
		}
	}
	if !found {
		t.Fatalf("legacy label %s was never deregistered; unloaded=%v", legacyLabel, fake.unloaded)
	}
	if len(res.Reloaded) != 1 {
		t.Fatalf("Reloaded = %v, want the new unit registered exactly once", res.Reloaded)
	}
}

// TestReconcileWatcherUnits_DoesNotTouchUnregisteredUnits pins the blast
// radius. Migration derives the old label from a REGISTERED (group, repo), so
// it can only ever name a unit belonging to that repo. Units for groups or
// repos that are not in the registry — a group installed by another checkout, a
// repo the user removed by hand — are left exactly as they are, and non-grafel
// LaunchAgents are never even looked at.
func TestReconcileWatcherUnits_DoesNotTouchUnregisteredUnits(t *testing.T) {
	home := reconcileSandbox(t)
	bin := filepath.Join(home, "bin", "grafel")
	registered := filepath.Join(home, "repos", "api")
	registerGroup(t, home, "g", []string{registered}, true)

	u := watchers.Unit{Group: "g", Repo: registered, BinPath: bin}
	if _, err := watchers.UnitPath(u); err != nil {
		t.Skipf("unsupported OS: %v", err)
	}
	writeUnitFile(t, watchers.LegacyOf(u), "OLD BODY\n")

	// Units nothing in the registry points at.
	strayRepo := watchers.Unit{Group: "g", Repo: filepath.Join(home, "repos", "gone"), BinPath: bin}
	strayGroup := watchers.Unit{Group: "othergroup", Repo: registered, BinPath: bin}
	strayLegacyRepo := writeUnitFile(t, watchers.LegacyOf(strayRepo), "STRAY REPO\n")
	strayCurrentGroup := writeUnitFile(t, strayGroup, "STRAY GROUP\n")

	// And something that is not ours at all.
	unitDir, err := watchers.UnitDir()
	if err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(unitDir, "com.example.somebodyelse.plist")
	if err := os.WriteFile(foreign, []byte("FOREIGN\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fake := &fakeLoader{}
	prev := newWatcherLoader
	newWatcherLoader = func() watchers.Loader { return fake }
	t.Cleanup(func() { newWatcherLoader = prev })

	if _, err := ReconcileWatcherUnits(ReconcileWatcherOptions{BinPath: bin}); err != nil {
		t.Fatal(err)
	}

	for name, p := range map[string]string{
		"stray repo":  strayLegacyRepo,
		"stray group": strayCurrentGroup,
		"foreign":     foreign,
	} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("%s unit was removed: %v", name, err)
		}
		if len(b) == 0 {
			t.Fatalf("%s unit was rewritten", name)
		}
	}
	for _, l := range fake.unloaded {
		if l != watchers.LegacyOf(u).Label() {
			t.Fatalf("deregistered an unrelated unit: %s", l)
		}
	}
}

// TestReconcileWatcherUnits_LegacyProbeConstructsNoLoader guards the property
// #6179 exists to protect. Reconcile now stats a second path per repo to find
// pre-#6183 units; that probe must stay a pure read. On a migrated, up-to-date
// machine there must be no Loader and therefore no launchctl at all.
func TestReconcileWatcherUnits_LegacyProbeConstructsNoLoader(t *testing.T) {
	home := reconcileSandbox(t)
	bin := filepath.Join(home, "bin", "grafel")
	a := filepath.Join(home, "x", "api")
	b := filepath.Join(home, "y", "api")
	registerGroup(t, home, "g", []string{a, b}, true)

	for _, r := range []string{a, b} {
		u := watchers.Unit{Group: "g", Repo: r, BinPath: bin}
		if _, err := watchers.UnitPath(u); err != nil {
			t.Skipf("unsupported OS: %v", err)
		}
		writeUnitFile(t, u, watchers.Render(u))
	}

	prev := newWatcherLoader
	newWatcherLoader = func() watchers.Loader {
		t.Fatalf("constructed a Loader on an up-to-date machine")
		return nil
	}
	t.Cleanup(func() { newWatcherLoader = prev })

	res, err := ReconcileWatcherUnits(ReconcileWatcherOptions{BinPath: bin})
	if err != nil {
		t.Fatal(err)
	}
	if res.Current != 2 || len(res.Rewritten) != 0 || len(res.Migrated) != 0 {
		t.Fatalf("current=%d rewritten=%d migrated=%d, want 2/0/0",
			res.Current, len(res.Rewritten), len(res.Migrated))
	}
}

// TestReconcileWatcherUnits_MigrationResetsWatchStarts answers the question a
// label change raises for the crash-loop detector: are its records orphaned
// too?
//
// They are not. WatchStartsPath is keyed by REPO PATH (<repo>/.grafel/
// watch-starts.json), not by label, so renaming the unit leaves the history
// exactly where the watcher will look for it. That is the good outcome and the
// bad one at once: the record survives, and the migration's bootstrap is itself
// a counted start, so an over-threshold history would make the freshly
// registered watcher give up on its first tick (#6179 F4-a). Migration must
// therefore clear it, like every other re-registration path.
func TestReconcileWatcherUnits_MigrationResetsWatchStarts(t *testing.T) {
	home := reconcileSandbox(t)
	bin := filepath.Join(home, "bin", "grafel")
	repo := filepath.Join(home, "repos", "api")
	registerGroup(t, home, "g", []string{repo}, true)

	u := watchers.Unit{Group: "g", Repo: repo, BinPath: bin}
	if _, err := watchers.UnitPath(u); err != nil {
		t.Skipf("unsupported OS: %v", err)
	}
	writeUnitFile(t, watchers.LegacyOf(u), "OLD BODY\n")

	starts := watchers.WatchStartsPath(repo)
	if err := os.MkdirAll(filepath.Dir(starts), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(starts, []byte(`{"starts":[1,2,3,4,5,6]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// The record's location does not mention the label at all.
	if strings.Contains(starts, u.Label()) || strings.Contains(starts, watchers.LegacyOf(u).Label()) {
		t.Fatalf("watch-starts path is label-derived (%s); a rename would orphan it", starts)
	}

	fake := &fakeLoader{}
	prev := newWatcherLoader
	newWatcherLoader = func() watchers.Loader { return fake }
	t.Cleanup(func() { newWatcherLoader = prev })

	if _, err := ReconcileWatcherUnits(ReconcileWatcherOptions{BinPath: bin}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(starts); !os.IsNotExist(err) {
		t.Fatalf("migration re-registered without clearing the crash-loop history: %v", err)
	}
}
