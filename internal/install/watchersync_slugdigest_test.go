package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/install/watchers"
)

// windowsClean is filepath.Clean's Windows behaviour, spelled for a host that
// is not Windows. watchers.SetNativeCleanForTest takes it so the SUPERSEDED
// label derivation — which is host-native by definition — is reachable from
// darwin and linux, and so the reconcile wiring that retires it is exercised
// by the two CI legs that are not Windows.
func windowsClean(s string) string {
	return strings.ReplaceAll(filepath.Clean(s), "/", `\`)
}

// TestReconcileWatcherUnits_RetiresThePreSlashNormalisationUnit.
//
// pathDigest now hashes the slash-normalised path. On Windows that renames
// every installed unit, and before this wiring existed NOTHING in the tree
// could derive the old name: planWatcherUnits stats the current label and the
// pre-#6183 label, so the machine's actual unit matched neither, the repo was
// counted Absent, and reconcile did nothing. The stale `.xml` stayed — and with
// it the REGISTERED SCHEDULED TASK, which loader_windows.go names by Label()
// and which therefore outlives the file — while `grafel install` wrote the new
// label alongside. Two live watchers per repo: the #6197 orphan mode, and the
// outcome MigrateLegacyUnit's doc calls strictly worse than the collision it
// repairs.
//
// This is the mutant test for the migration: delete the nativeExists probe in
// planWatcherUnits or the MigrateNativeDigestUnit call beside it and this
// fails, on every platform.
func TestReconcileWatcherUnits_RetiresThePreSlashNormalisationUnit(t *testing.T) {
	home := reconcileSandbox(t)
	restore := watchers.SetNativeCleanForTest(windowsClean)
	t.Cleanup(restore)

	bin := filepath.Join(home, "bin", "grafel")
	repo := filepath.Join(home, "x", "api")
	registerGroup(t, home, "g", []string{repo}, true)

	u := watchers.Unit{Group: "g", Repo: repo, BinPath: bin}
	current, err := watchers.UnitPath(u)
	if err != nil {
		t.Skipf("unsupported OS: %v", err)
	}
	old := watchers.NativeDigestOf(u)
	oldPath, err := watchers.NativeDigestUnitPath(u)
	if err != nil {
		t.Fatal(err)
	}
	if oldPath == current {
		t.Fatalf("precondition: the superseded path must differ from the current one (%s)", current)
	}

	// The exact state a Windows machine is in the moment it upgrades across the
	// normalisation: a unit under the old label, nothing under the new one.
	if err := os.MkdirAll(filepath.Dir(oldPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPath, []byte("superseded unit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fl := &fakeLoader{}
	prev := newWatcherLoader
	newWatcherLoader = func() watchers.Loader { return fl }
	t.Cleanup(func() { newWatcherLoader = prev })

	res, err := ReconcileWatcherUnits(ReconcileWatcherOptions{BinPath: bin})
	if err != nil {
		t.Fatal(err)
	}

	if res.Absent != 0 {
		t.Fatalf("the repo was counted Absent (%d): reconcile cannot see the unit it actually has, "+
			"so the old one is left installed and a new one is written beside it", res.Absent)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("the superseded unit file survived at %s: %v", oldPath, err)
	}
	if _, err := os.Stat(current); err != nil {
		t.Fatalf("no unit was written under the current label: %v", err)
	}
	// Deleting the file does not deregister the job — on Windows the scheduled
	// task is registered under the LABEL and survives the .xml.
	var sawOld bool
	for _, l := range fl.unloaded {
		if l == old.Label() {
			sawOld = true
		}
	}
	if !sawOld {
		t.Fatalf("the superseded label %s was never deregistered; unloaded=%v — "+
			"the scheduled task keeps spawning `grafel watch` forever", old.Label(), fl.unloaded)
	}
	if len(res.Migrated) != 1 || res.Migrated[0] != oldPath {
		t.Errorf("Migrated = %v, want [%s]", res.Migrated, oldPath)
	}
}

// TestReconcileWatcherUnits_RetiresThePreSlashNormalisationUnitWhenWatchersAreDisabled
// is the same retirement in the branch where nobody is looking.
//
// A group with Features.Watchers=false takes its own arm of the reconcile loop
// and `continue`s before anything is installed. #6183 established why that arm
// still has to RETIRE: a superseded unit for a registered repo stays on disk and
// stays loaded, and re-enabling watchers later would add the new label beside it
// rather than over it. One derivation later the stakes are higher and the
// visibility is lower — on Windows the orphan is a registered SCHEDULED TASK
// spawning `grafel watch` forever, in a group where every signal that would
// surface it is switched off.
//
// The legacy arm of this branch is pinned by
// TestReconcileWatcherUnits_RetiresLegacyUnitWhenWatchersAreDisabled; this is
// its counterpart, and the mutant test for the second wiring site.
func TestReconcileWatcherUnits_RetiresThePreSlashNormalisationUnitWhenWatchersAreDisabled(t *testing.T) {
	home := reconcileSandbox(t)
	restore := watchers.SetNativeCleanForTest(windowsClean)
	t.Cleanup(restore)

	bin := filepath.Join(home, "bin", "grafel")
	repo := filepath.Join(home, "repos", "api")
	registerGroup(t, home, "g", []string{repo}, false) // watchers OFF

	u := watchers.Unit{Group: "g", Repo: repo, BinPath: bin}
	current, err := watchers.UnitPath(u)
	if err != nil {
		t.Skipf("unsupported OS: %v", err)
	}
	old := watchers.NativeDigestOf(u)
	oldPath, err := watchers.NativeDigestUnitPath(u)
	if err != nil {
		t.Fatal(err)
	}
	if oldPath == current {
		t.Fatalf("precondition: the superseded path must differ from the current one (%s)", current)
	}
	if err := os.MkdirAll(filepath.Dir(oldPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPath, []byte("superseded unit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fl := &fakeLoader{}
	prev := newWatcherLoader
	newWatcherLoader = func() watchers.Loader { return fl }
	t.Cleanup(func() { newWatcherLoader = prev })

	res, err := ReconcileWatcherUnits(ReconcileWatcherOptions{BinPath: bin})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("the superseded unit survived at %s in a watchers-disabled group: %v", oldPath, err)
	}
	var sawOld bool
	for _, l := range fl.unloaded {
		if l == old.Label() {
			sawOld = true
		}
	}
	if !sawOld {
		t.Fatalf("the superseded label %s was never deregistered; unloaded=%v — the scheduled "+
			"task outlives the file, in the group least likely to be watched", old.Label(), fl.unloaded)
	}
	if len(res.Migrated) != 1 || res.Migrated[0] != oldPath {
		t.Errorf("Migrated = %v, want [%s]", res.Migrated, oldPath)
	}
	// And nothing may be installed: watchers are off, and that is the whole
	// meaning of the flag.
	if _, err := os.Stat(current); !os.IsNotExist(err) {
		t.Fatalf("reconcile installed a watcher for a group with watchers disabled")
	}
	if len(res.Reloaded) != 0 {
		t.Fatalf("Reloaded = %v, want nothing registered", res.Reloaded)
	}
}

// TestReconcileWatcherUnits_LeavesTheLiveUnitAloneWhereDerivationsCoincide is
// the other half: with the host's native clean in force (the real production
// case off Windows) the superseded derivation IS the current one, and a
// migration that did not notice would boot out and delete the live unit on
// every reconcile.
func TestReconcileWatcherUnits_LeavesTheLiveUnitAloneWhereDerivationsCoincide(t *testing.T) {
	home := reconcileSandbox(t)
	bin := filepath.Join(home, "bin", "grafel")
	repo := filepath.Join(home, "x", "api")
	registerGroup(t, home, "g", []string{repo}, true)

	u := watchers.Unit{Group: "g", Repo: repo, BinPath: bin}
	if watchers.NativeDigestOf(u).Label() != u.Label() {
		t.Skip("this host's native clean differs from the slash form; covered by the migration test")
	}
	path := writeUnitFile(t, u, watchers.Render(u))

	fl := &fakeLoader{}
	prev := newWatcherLoader
	newWatcherLoader = func() watchers.Loader { return fl }
	t.Cleanup(func() { newWatcherLoader = prev })

	res, err := ReconcileWatcherUnits(ReconcileWatcherOptions{BinPath: bin})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("reconcile deleted the live unit: %v", err)
	}
	if len(res.Migrated) != 0 {
		t.Fatalf("Migrated = %v; a coincident derivation must retire nothing", res.Migrated)
	}
	if res.Current != 1 {
		t.Errorf("Current = %d, want 1 (an up-to-date unit is left alone)", res.Current)
	}
}
