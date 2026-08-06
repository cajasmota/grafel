package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/install/watchers"
)

// --- F2: an interrupted first migration pass must not strand the sibling ----

// TestReconcileWatcherUnits_InterruptedMigrationLeavesTheSiblingAbsentAndSaysSo.
//
// Repos that collided under the pre-#6183 label shared ONE unit file. Within a
// single pass that is fully handled: phase 1 stats the shared file before
// anything is retired, so every member records legacyExists=true and every
// member gets its own unit (see ConvergesWithDuplicateBasenames).
//
// A pass KILLED partway through — ^C, a closed lid, an OOM; a 140-repo fleet
// gives that room — is different. The survivor is left with no unit under
// either label, and reconcile will not create it. It cannot: that on-disk state
// is byte-for-byte identical to a watcher the user deliberately booted out and
// deleted, and re-creating those is a bug of its own (see
// DoesNotResurrectADeliberatelyRemovedUnit).
//
// So the contract is: report it, do not guess. The repo is counted Absent, the
// summary names the count and points at `grafel install`, and reconcile stays a
// pure read — no rewrite, no launchctl, nothing that would drift.
func TestReconcileWatcherUnits_InterruptedMigrationLeavesTheSiblingAbsentAndSaysSo(t *testing.T) {
	home := reconcileSandbox(t)
	bin := filepath.Join(home, "bin", "grafel")
	a := filepath.Join(home, "x", "my-repo")
	b := filepath.Join(home, "y", "my_repo")
	registerGroup(t, home, "g", []string{a, b}, true)

	ua := watchers.Unit{Group: "g", Repo: a, BinPath: bin}
	ub := watchers.Unit{Group: "g", Repo: b, BinPath: bin}
	pa, err := watchers.UnitPath(ua)
	if err != nil {
		t.Skipf("unsupported OS: %v", err)
	}
	pb, _ := watchers.UnitPath(ub)

	// The exact state an interrupted pass leaves behind: the shared legacy unit
	// is gone, repo A has its new unit, repo B has nothing at all.
	writeUnitFile(t, ua, watchers.Render(ua))
	if _, err := os.Stat(pb); !os.IsNotExist(err) {
		t.Fatalf("precondition: repo B must have no unit")
	}
	legacyPath, _ := watchers.LegacyUnitPath(ua)
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("precondition: the shared legacy unit must already be retired")
	}

	prev := newWatcherLoader
	newWatcherLoader = func() watchers.Loader {
		t.Fatalf("constructed a Loader: an interrupted migration is reported, not repaired")
		return nil
	}
	t.Cleanup(func() { newWatcherLoader = prev })

	res, err := ReconcileWatcherUnits(ReconcileWatcherOptions{BinPath: bin})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pb); !os.IsNotExist(err) {
		t.Fatalf("reconcile re-created repo B's unit; it cannot distinguish this from a " +
			"watcher the user deliberately removed")
	}
	if res.Absent != 1 {
		t.Fatalf("Absent = %d, want 1 — the operator's only signal that repo B is unwatched",
			res.Absent)
	}
	if len(res.Rewritten) != 0 || len(res.Reloaded) != 0 {
		t.Fatalf("rewritten=%d reloaded=%d, want a pure read",
			len(res.Rewritten), len(res.Reloaded))
	}
	if _, err := os.Stat(pa); err != nil {
		t.Fatalf("repo A lost its unit: %v", err)
	}
	// And Apply is the documented remedy: it always writes, so it recovers B.
	if res.Current != 1 {
		t.Fatalf("Current = %d, want 1", res.Current)
	}
}

// TestReconcileWatcherUnits_DoesNotCreateUnitsForNonCollidingAbsentRepos is the
// bound on the recovery above. A repo that simply has no unit — never installed,
// or deliberately booted out by the user — must STILL be left alone. Only the
// collision recovery is in scope; resurrecting units nobody asked for is what
// ReconcileWatcherUnits exists not to do.
func TestReconcileWatcherUnits_DoesNotCreateUnitsForNonCollidingAbsentRepos(t *testing.T) {
	home := reconcileSandbox(t)
	bin := filepath.Join(home, "bin", "grafel")
	a := filepath.Join(home, "x", "alpha")
	b := filepath.Join(home, "y", "beta") // distinct basenames: never collided
	registerGroup(t, home, "g", []string{a, b}, true)

	ua := watchers.Unit{Group: "g", Repo: a, BinPath: bin}
	pa, err := watchers.UnitPath(ua)
	if err != nil {
		t.Skipf("unsupported OS: %v", err)
	}
	writeUnitFile(t, ua, watchers.Render(ua))

	prev := newWatcherLoader
	newWatcherLoader = func() watchers.Loader {
		t.Fatalf("constructed a Loader: nothing here is stale")
		return nil
	}
	t.Cleanup(func() { newWatcherLoader = prev })

	res, err := ReconcileWatcherUnits(ReconcileWatcherOptions{BinPath: bin})
	if err != nil {
		t.Fatal(err)
	}
	pb, _ := watchers.UnitPath(watchers.Unit{Group: "g", Repo: b, BinPath: bin})
	if _, err := os.Stat(pb); !os.IsNotExist(err) {
		t.Fatalf("reconcile created a unit for a repo that never had one (%s)", pb)
	}
	if res.Absent != 1 {
		t.Fatalf("Absent = %d, want 1", res.Absent)
	}
	if _, err := os.Stat(pa); err != nil {
		t.Fatalf("repo A's unit was disturbed: %v", err)
	}
}

// --- F3: legacy units that are orphaned while still perfectly derivable ------

// TestReconcileWatcherUnits_RetiresLegacyUnitWhenWatchersAreDisabled.
//
// A group with Features.Watchers=false was `continue`d before any repo was
// visited, so turning watchers off in fleet.json left a pre-#6183 plist on disk
// and loaded — for a repo that is STILL REGISTERED and whose legacy label is
// therefore still derivable. That is not the unnameable-orphan class the
// registry-derivation bound is about; it is an orphan we can name and simply
// were not naming.
//
// Retiring it is also what "watchers: false" already means everywhere else.
func TestReconcileWatcherUnits_RetiresLegacyUnitWhenWatchersAreDisabled(t *testing.T) {
	home := reconcileSandbox(t)
	bin := filepath.Join(home, "bin", "grafel")
	repo := filepath.Join(home, "repos", "api")
	registerGroup(t, home, "g", []string{repo}, false) // watchers OFF

	u := watchers.Unit{Group: "g", Repo: repo, BinPath: bin}
	if _, err := watchers.UnitPath(u); err != nil {
		t.Skipf("unsupported OS: %v", err)
	}
	legacyPath := writeUnitFile(t, watchers.LegacyOf(u), watchers.Render(watchers.LegacyOf(u)))

	fake := &fakeLoader{}
	prev := newWatcherLoader
	newWatcherLoader = func() watchers.Loader { return fake }
	t.Cleanup(func() { newWatcherLoader = prev })

	res, err := ReconcileWatcherUnits(ReconcileWatcherOptions{BinPath: bin})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy unit survived in a watchers-disabled group: %v", err)
	}
	if len(res.Migrated) != 1 {
		t.Fatalf("Migrated = %v, want the legacy unit reported", res.Migrated)
	}
	// It must NOT be replaced: watchers are off.
	newPath, _ := watchers.UnitPath(u)
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Fatalf("reconcile installed a watcher for a group with watchers disabled")
	}
	if len(res.Reloaded) != 0 {
		t.Fatalf("Reloaded = %v, want nothing registered", res.Reloaded)
	}
	// Deregistered under the OLD label.
	if len(fake.unloaded) != 1 || fake.unloaded[0] != watchers.LegacyOf(u).Label() {
		t.Fatalf("unloaded = %v, want [%s]", fake.unloaded, watchers.LegacyOf(u).Label())
	}

	// Idempotent: nothing left to do, so no Loader at all.
	newWatcherLoader = func() watchers.Loader {
		t.Fatalf("second pass constructed a Loader")
		return nil
	}
	if _, err := ReconcileWatcherUnits(ReconcileWatcherOptions{BinPath: bin}); err != nil {
		t.Fatal(err)
	}
}

// TestReconcileWatcherUnits_CollidingPairWithNothingInstalledStaysAbsent is the
// bound on the crash recovery.
//
// The recovery keys off "two registered repos in one group share a legacy
// label". That condition is readable from the registry alone — which is what
// makes it survive a crash — but it is therefore ALSO true on a machine where
// no watcher was ever installed. Acting on it there would have reconcile
// installing and bootstrapping watchers nobody asked for, on every upgrade,
// which is the exact behaviour #6179 exists to prevent.
//
// So the collision only puts a repo in scope when a sibling actually has a unit
// on disk. Nothing installed means nothing to recover.
func TestReconcileWatcherUnits_CollidingPairWithNothingInstalledStaysAbsent(t *testing.T) {
	home := reconcileSandbox(t)
	bin := filepath.Join(home, "bin", "grafel")
	a := filepath.Join(home, "x", "my-repo")
	b := filepath.Join(home, "y", "my_repo") // same legacy slug as a
	registerGroup(t, home, "g", []string{a, b}, true)

	ua := watchers.Unit{Group: "g", Repo: a, BinPath: bin}
	pa, err := watchers.UnitPath(ua)
	if err != nil {
		t.Skipf("unsupported OS: %v", err)
	}
	pb, _ := watchers.UnitPath(watchers.Unit{Group: "g", Repo: b, BinPath: bin})
	if watchers.LegacyOf(ua).Label() != watchers.LegacyOf(watchers.Unit{Group: "g", Repo: b, BinPath: bin}).Label() {
		t.Fatalf("precondition: the two repos must share a legacy label")
	}

	prev := newWatcherLoader
	newWatcherLoader = func() watchers.Loader {
		t.Fatalf("constructed a Loader: nothing is installed, so nothing can be stale")
		return nil
	}
	t.Cleanup(func() { newWatcherLoader = prev })

	res, err := ReconcileWatcherUnits(ReconcileWatcherOptions{BinPath: bin})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{pa, pb} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("reconcile installed a watcher nobody asked for: %s", p)
		}
	}
	if res.Absent != 2 {
		t.Fatalf("Absent = %d, want 2", res.Absent)
	}
	if len(res.Rewritten) != 0 || len(res.Reloaded) != 0 || len(res.Migrated) != 0 {
		t.Fatalf("rewritten=%d reloaded=%d migrated=%d, want all zero",
			len(res.Rewritten), len(res.Reloaded), len(res.Migrated))
	}
}
