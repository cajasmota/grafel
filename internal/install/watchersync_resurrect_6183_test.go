package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/install/watchers"
)

// TestReconcileWatcherUnits_DoesNotResurrectADeliberatelyRemovedUnit.
//
// A user who boots out a watcher and deletes its plist has expressed an
// intention. ReconcileWatcherUnits must respect it: a repo with no unit under
// either label is left alone, which is the rule that makes reconcile safe to
// run unconditionally on every upgrade.
//
// The collision recovery added for #6183 F2 briefly broke that rule, and only
// for repos whose directory name happened to match a sibling's. Its in-scope
// gate accepted `pl.exists` — the CURRENT-label unit — as evidence that a
// shared legacy unit had just been retired. But `pl.exists` is true for every
// ordinary, fully-migrated repo forever after, so once two repos in a group
// shared a legacy label and either had any unit, the other was re-created and
// re-bootstrapped on EVERY reconcile: the #6183 non-convergence signature
// restored, with a Background Items notification per `grafel update`, and the
// answer to "will reconcile install a watcher I never asked for" made to depend
// on whether an unrelated repo shares my directory name.
//
// There is no predicate that separates the two cases, which is why the gate had
// to go rather than be narrowed: "interrupted mid-migration" and "user deleted
// this on purpose" are byte-for-byte identical on disk — sibling has a current
// unit, this repo has none, no legacy unit anywhere. The recovery for the first
// is `Absent` in the summary telling the operator to run `grafel install`.
func TestReconcileWatcherUnits_DoesNotResurrectADeliberatelyRemovedUnit(t *testing.T) {
	home := reconcileSandbox(t)
	bin := filepath.Join(home, "bin", "grafel")

	// A fresh, modern install: no legacy unit has ever existed here. The two
	// repos collide only under the OLD scheme.
	a := filepath.Join(home, "x", "api")
	b := filepath.Join(home, "y", "api")
	registerGroup(t, home, "g", []string{a, b}, true)

	ua := watchers.Unit{Group: "g", Repo: a, BinPath: bin}
	ub := watchers.Unit{Group: "g", Repo: b, BinPath: bin}
	pa, err := watchers.UnitPath(ua)
	if err != nil {
		t.Skipf("unsupported OS: %v", err)
	}
	pb, _ := watchers.UnitPath(ub)
	if watchers.LegacyOf(ua).Label() != watchers.LegacyOf(ub).Label() {
		t.Fatalf("precondition: the pair must collide under the legacy label")
	}
	legacyPath, _ := watchers.LegacyUnitPath(ua)
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("precondition: no legacy unit may exist")
	}

	// A is installed and current. B was deliberately booted out and deleted.
	writeUnitFile(t, ua, watchers.Render(ua))

	fake := &fakeLoader{}
	prev := newWatcherLoader
	newWatcherLoader = func() watchers.Loader { return fake }
	t.Cleanup(func() { newWatcherLoader = prev })

	for run := 1; run <= 3; run++ {
		res, err := ReconcileWatcherUnits(ReconcileWatcherOptions{BinPath: bin})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("run %d: current=%d rewritten=%d reloaded=%d migrated=%d absent=%d (cumulative Loads=%d)",
			run, res.Current, len(res.Rewritten), len(res.Reloaded), len(res.Migrated),
			res.Absent, len(fake.loaded))

		if _, err := os.Stat(pb); !os.IsNotExist(err) {
			t.Fatalf("run %d resurrected a unit the user deliberately removed (%s)", run, pb)
		}
		if res.Absent != 1 {
			t.Fatalf("run %d: Absent = %d, want 1 (repo B, reported not re-created)", run, res.Absent)
		}
		if len(res.Rewritten) != 0 || len(res.Reloaded) != 0 {
			t.Fatalf("run %d: rewritten=%d reloaded=%d on an up-to-date machine — #6179's "+
				"whole property", run, len(res.Rewritten), len(res.Reloaded))
		}
		if len(fake.loaded) != 0 {
			t.Fatalf("run %d: cumulative launchctl Loads=%d, want 0", run, len(fake.loaded))
		}
		if res.Current != 1 {
			t.Fatalf("run %d: Current = %d, want 1 (repo A untouched)", run, res.Current)
		}
	}
	if _, err := os.Stat(pa); err != nil {
		t.Fatalf("repo A's unit was disturbed: %v", err)
	}
}

// TestReconcileWatcherUnits_CollidingAndNonCollidingAbsentReposAreTreatedAlike
// pins the symmetry directly. Whether reconcile creates a watcher must not
// depend on whether some other repo happens to share this one's directory name.
func TestReconcileWatcherUnits_CollidingAndNonCollidingAbsentReposAreTreatedAlike(t *testing.T) {
	home := reconcileSandbox(t)
	bin := filepath.Join(home, "bin", "grafel")

	installed := filepath.Join(home, "x", "api")
	colliding := filepath.Join(home, "y", "api")    // shares a legacy label with installed
	nonColliding := filepath.Join(home, "z", "web") // shares nothing
	registerGroup(t, home, "g", []string{installed, colliding, nonColliding}, true)

	ui := watchers.Unit{Group: "g", Repo: installed, BinPath: bin}
	if _, err := watchers.UnitPath(ui); err != nil {
		t.Skipf("unsupported OS: %v", err)
	}
	writeUnitFile(t, ui, watchers.Render(ui))

	prev := newWatcherLoader
	newWatcherLoader = func() watchers.Loader {
		t.Fatalf("constructed a Loader: nothing is stale and nothing should be created")
		return nil
	}
	t.Cleanup(func() { newWatcherLoader = prev })

	res, err := ReconcileWatcherUnits(ReconcileWatcherOptions{BinPath: bin})
	if err != nil {
		t.Fatal(err)
	}
	pc, _ := watchers.UnitPath(watchers.Unit{Group: "g", Repo: colliding, BinPath: bin})
	pn, _ := watchers.UnitPath(watchers.Unit{Group: "g", Repo: nonColliding, BinPath: bin})
	_, cErr := os.Stat(pc)
	_, nErr := os.Stat(pn)
	if os.IsNotExist(cErr) != os.IsNotExist(nErr) {
		t.Fatalf("colliding and non-colliding absent repos treated differently: "+
			"colliding created=%v, non-colliding created=%v", cErr == nil, nErr == nil)
	}
	if cErr == nil {
		t.Fatalf("reconcile created a unit for a repo that has none (%s)", pc)
	}
	if res.Absent != 2 {
		t.Fatalf("Absent = %d, want 2", res.Absent)
	}
}
