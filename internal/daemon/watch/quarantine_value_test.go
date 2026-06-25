package watch

// quarantine_value_test.go — Q4 value-based detection (T2, #5619).
//
// These tests exercise the value detector deterministically (fake clock,
// injected cost via RecordIndexCost, injected usage via NoteUsage, temp repo for
// persistence). They verify the T2 contract:
//   - expensive ∧ unused  → quarantined (signal="value");
//   - expensive ∧ used    → NOT quarantined (recent use vetoes it);
//   - cheap   ∧ unused    → NOT quarantined (under the cost threshold);
//   - pinned dir          → never (Sweep skips pins; a later use recovers via Q3);
//   - recovers on later use (Q3 Recover) and re-evaluates cleanly.

import (
	"path/filepath"
	"testing"
	"time"
)

// newValueTracker builds a tracker with a fixed value config + fake clock, so
// the expensive/unused logic runs without real timing, env, or fsnotify. It
// keeps the churn config from newTestTracker so both detectors coexist.
func newValueTracker(t *testing.T) (*QuarantineTracker, *fakeClock) {
	t.Helper()
	q, clk := newTestTracker(t)
	q.value = &valueDetector{
		cfg: valueConfig{
			costThreshold: 1000,
			unusedGrace:   24 * time.Hour,
		},
		byDir: make(map[string]map[string]*dirValue),
	}
	return q, clk
}

func isQuarantined(q *QuarantineTracker, repo, rel string) bool {
	for _, r := range q.List(repo) {
		if r.Rel == rel {
			return true
		}
	}
	return false
}

func TestValue_ExpensiveAndUnusedIsQuarantined(t *testing.T) {
	repo := t.TempDir()
	q, clk := newValueTracker(t)
	heavy := filepath.Join(repo, "vendor", "huge", "bundle.js")

	// Record an expensive index pass (over the 1000 threshold). Not yet
	// quarantined: the unused grace (24h) has not elapsed since first-seen.
	if q.RecordIndexCost(repo, heavy, 5000) {
		t.Fatalf("expensive dir must NOT quarantine before the unused grace elapses")
	}
	if isQuarantined(q, repo, "vendor/huge") {
		t.Fatalf("dir quarantined too early")
	}

	// Let a full day of no usage pass, then another index pass observes it: now
	// it is expensive AND unused → quarantine.
	clk.advance(25 * time.Hour)
	if !q.RecordIndexCost(repo, heavy, 5000) {
		t.Fatalf("expensive ∧ unused dir must be quarantined after the grace period")
	}
	got := q.List(repo)
	if len(got) != 1 || got[0].Rel != "vendor/huge" || got[0].Signal != "value" {
		t.Fatalf("want vendor/huge quarantined with signal=value, got %+v", got)
	}
}

func TestValue_ExpensiveButUsedIsNotQuarantined(t *testing.T) {
	repo := t.TempDir()
	q, clk := newValueTracker(t)
	heavy := filepath.Join(repo, "gen", "schema.ts")

	if q.RecordIndexCost(repo, heavy, 5000) {
		t.Fatalf("not expected to quarantine on first record")
	}
	// A day passes, but the dir IS queried right before re-evaluation — recent
	// use must veto the value quarantine no matter how expensive.
	clk.advance(25 * time.Hour)
	q.NoteUsage(repo, heavy)
	if q.RecordIndexCost(repo, heavy, 5000) {
		t.Fatalf("expensive but recently-USED dir must NOT be quarantined")
	}
	if isQuarantined(q, repo, "gen") {
		t.Fatalf("used dir must not be quarantined")
	}
}

func TestValue_CheapAndUnusedIsNotQuarantined(t *testing.T) {
	repo := t.TempDir()
	q, clk := newValueTracker(t)
	cheap := filepath.Join(repo, "src", "util.go")

	// Cheap (under the 1000 threshold). Even after a long unused stretch it must
	// never be quarantined — the cost gate excludes it categorically.
	if q.RecordIndexCost(repo, cheap, 50) {
		t.Fatalf("cheap dir must not quarantine")
	}
	clk.advance(72 * time.Hour)
	if q.RecordIndexCost(repo, cheap, 50) {
		t.Fatalf("cheap ∧ unused dir must NOT be quarantined")
	}
	if isQuarantined(q, repo, "src") {
		t.Fatalf("cheap dir must never be quarantined")
	}
}

func TestValue_PinnedDirIsNeverValueQuarantinedAway(t *testing.T) {
	repo := t.TempDir()
	q, clk := newValueTracker(t)
	heavy := filepath.Join(repo, "vendor", "pinned", "x.js")

	// Drive it into a value quarantine, then operator-pins it.
	q.RecordIndexCost(repo, heavy, 5000)
	clk.advance(25 * time.Hour)
	if !q.RecordIndexCost(repo, heavy, 5000) {
		t.Fatalf("setup: expected value quarantine")
	}
	if !q.Pin(repo, "vendor/pinned") {
		t.Fatalf("setup: pin should change state")
	}

	// A query under the pinned dir must NOT auto-recover it (operator override).
	rel, recovered := q.Recover(repo, heavy)
	if recovered || rel != "" {
		t.Fatalf("pinned value-quarantine must not auto-recover, got (%q,%v)", rel, recovered)
	}
	if got := q.List(repo); len(got) != 1 || !got[0].Pinned {
		t.Fatalf("pinned dir must stay quarantined+pinned, got %+v", got)
	}

	// And the quiet-window Sweep must never auto-heal a pinned dir either.
	clk.advance(48 * time.Hour)
	q.Sweep()
	if !isQuarantined(q, repo, "vendor/pinned") {
		t.Fatalf("Sweep must never auto-heal a pinned value-quarantine")
	}
}

func TestValue_RecoversOnLaterUse(t *testing.T) {
	repo := t.TempDir()
	q, clk := newValueTracker(t)
	heavy := filepath.Join(repo, "build", "out", "app.js")

	q.RecordIndexCost(repo, heavy, 5000)
	clk.advance(25 * time.Hour)
	if !q.RecordIndexCost(repo, heavy, 5000) {
		t.Fatalf("setup: expected value quarantine")
	}

	// The dir later proves real — its content is queried. Q3 Recover lifts the
	// value quarantine immediately, same as for a churn quarantine.
	rel, recovered := q.Recover(repo, heavy)
	if !recovered || rel != "build/out" {
		t.Fatalf("query under a value-quarantined dir must recover it, got (%q,%v)", rel, recovered)
	}
	if len(q.List(repo)) != 0 {
		t.Fatalf("value-quarantined dir must be gone after recover: %+v", q.List(repo))
	}
}

func TestValue_UsageWithinGraceDefersQuarantine(t *testing.T) {
	repo := t.TempDir()
	q, clk := newValueTracker(t)
	heavy := filepath.Join(repo, "data", "blob.json")

	q.RecordIndexCost(repo, heavy, 5000)
	// 12h in (inside the 24h grace) the dir is queried → resets the unused clock.
	clk.advance(12 * time.Hour)
	q.NoteUsage(repo, heavy)
	// 12h more (24h since first-seen, but only 12h since last use): still used.
	clk.advance(12 * time.Hour)
	if q.RecordIndexCost(repo, heavy, 5000) {
		t.Fatalf("a use within grace must reset the unused clock and defer quarantine")
	}
	// A further full day with no use → now genuinely unused → quarantine.
	clk.advance(25 * time.Hour)
	if !q.RecordIndexCost(repo, heavy, 5000) {
		t.Fatalf("after a full unused day post-use the dir must be quarantined")
	}
}

func TestValue_AncestorUsageProtectsExpensiveParent(t *testing.T) {
	repo := t.TempDir()
	q, clk := newValueTracker(t)
	parent := filepath.Join(repo, "pkg", "core.go") // cost attributed to "pkg"
	child := filepath.Join(repo, "pkg", "sub", "leaf.go")

	q.RecordIndexCost(repo, parent, 5000)
	clk.advance(25 * time.Hour)
	// A query touches a CHILD under pkg → marks pkg (and pkg/sub) used.
	q.NoteUsage(repo, child)
	if q.RecordIndexCost(repo, parent, 5000) {
		t.Fatalf("usage under a child must protect the expensive parent from value-quarantine")
	}
	if isQuarantined(q, repo, "pkg") {
		t.Fatalf("pkg must not be quarantined while its children are queried")
	}
}

func TestValue_PersistsAcrossReload(t *testing.T) {
	repo := t.TempDir()
	q, clk := newValueTracker(t)
	heavy := filepath.Join(repo, "vendor", "big", "x.js")

	q.RecordIndexCost(repo, heavy, 5000)
	clk.advance(25 * time.Hour)
	if !q.RecordIndexCost(repo, heavy, 5000) {
		t.Fatalf("setup: expected value quarantine")
	}

	// A fresh tracker reads the persisted set — the value quarantine survives.
	q2, _ := newValueTracker(t)
	got := q2.List(repo)
	if len(got) != 1 || got[0].Rel != "vendor/big" || got[0].Signal != "value" {
		t.Fatalf("value quarantine must persist across reload, got %+v", got)
	}
}

func TestValue_DisabledAndNilSafe(t *testing.T) {
	var nilq *QuarantineTracker
	if nilq.RecordIndexCost("/repo", "/repo/x/y.go", 9999) {
		t.Fatalf("nil receiver RecordIndexCost must be a safe no-op")
	}
	nilq.NoteUsage("/repo", "/repo/x/y.go") // must not panic

	repo := t.TempDir()
	// Whole-tracker kill switch (Q1) disables T2 too.
	q, clk := newValueTracker(t)
	q.cfg.disabled = true
	clk.advance(72 * time.Hour)
	if q.RecordIndexCost(repo, filepath.Join(repo, "v", "x.js"), 9999) {
		t.Fatalf("disabled tracker must not value-quarantine")
	}

	// T2-only kill switch leaves the churn detector alone but disables value.
	q2, clk2 := newValueTracker(t)
	q2.value.cfg.disabled = true
	q2.RecordIndexCost(repo, filepath.Join(repo, "w", "x.js"), 9999)
	clk2.advance(72 * time.Hour)
	if q2.RecordIndexCost(repo, filepath.Join(repo, "w", "x.js"), 9999) {
		t.Fatalf("value-disabled tracker must not value-quarantine")
	}
}

func TestValue_OutsideRepoAndZeroCostAreNoOps(t *testing.T) {
	repo := t.TempDir()
	q, clk := newValueTracker(t)
	clk.advance(72 * time.Hour)

	if q.RecordIndexCost(repo, "/elsewhere/x.js", 9999) {
		t.Fatalf("path outside repo must be a no-op")
	}
	if q.RecordIndexCost(repo, filepath.Join(repo, "a", "x.js"), 0) {
		t.Fatalf("zero cost must be a no-op")
	}
	if len(q.List(repo)) != 0 {
		t.Fatalf("no quarantines expected, got %+v", q.List(repo))
	}
}
