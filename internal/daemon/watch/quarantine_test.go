package watch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeClock is a manually-advanced clock for deterministic quarantine tests.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// newTestTracker builds a tracker with a fixed config and a fake clock so the
// detect/quarantine/heal logic is exercised without real timing, env, or fs
// dependence beyond the temp repo dir for persistence.
func newTestTracker(t *testing.T) (*QuarantineTracker, *fakeClock) {
	t.Helper()
	clk := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	q := &QuarantineTracker{
		cfg: quarantineConfig{
			threshold: 10,
			window:    2 * time.Minute,
			healQuiet: 15 * time.Minute,
		},
		now:         clk.now,
		churn:       make(map[string]map[string]*dirChurn),
		quarantined: make(map[string]map[string]QuarantineReason),
		loaded:      make(map[string]bool),
	}
	return q, clk
}

// pump fires n Observe calls for the same path and returns how many were
// dropped (quarantined).
func pump(q *QuarantineTracker, repo, path string, n int) int {
	dropped := 0
	for i := 0; i < n; i++ {
		if q.Observe(repo, path) {
			dropped++
		}
	}
	return dropped
}

func TestQuarantine_ChurnTripsQuarantine(t *testing.T) {
	repo := t.TempDir()
	q, _ := newTestTracker(t)
	buildFile := filepath.Join(repo, "app", "build", "out.o")

	// Threshold is 10. The 10th event should quarantine; from then on events
	// under that dir are dropped.
	drops := pump(q, repo, buildFile, 9)
	if drops != 0 {
		t.Fatalf("9 events should not quarantine yet, got %d drops", drops)
	}
	if q.Observe(repo, buildFile) != true {
		t.Fatalf("10th event should trip quarantine and be dropped")
	}
	// Subsequent events are dropped without further accounting.
	if !q.Observe(repo, buildFile) {
		t.Fatalf("post-quarantine event should be dropped")
	}

	list := q.List(repo)
	if len(list) != 1 || list[0].Rel != "app/build" {
		t.Fatalf("expected app/build quarantined, got %+v", list)
	}
	if list[0].Signal != "churn" {
		t.Fatalf("expected churn signal, got %q", list[0].Signal)
	}
}

func TestQuarantine_NormalEditBurstDoesNotQuarantine(t *testing.T) {
	repo := t.TempDir()
	q, clk := newTestTracker(t)
	src := filepath.Join(repo, "src", "service.go")

	// A realistic human edit burst: a handful of saves, well under threshold.
	if drops := pump(q, repo, src, 6); drops != 0 {
		t.Fatalf("edit burst should not quarantine, got %d drops", drops)
	}
	// Even spread across several windows, as long as no single window crosses
	// the threshold, it never quarantines.
	for i := 0; i < 5; i++ {
		clk.advance(3 * time.Minute) // each save in a fresh window
		if q.Observe(repo, src) {
			t.Fatalf("spread-out edits should never quarantine")
		}
	}
	if len(q.List(repo)) != 0 {
		t.Fatalf("normal source dir must not be quarantined: %+v", q.List(repo))
	}
}

func TestQuarantine_WindowRollResetsCount(t *testing.T) {
	repo := t.TempDir()
	q, clk := newTestTracker(t)
	p := filepath.Join(repo, "gen", "x.ts")

	// 9 events, then roll the window past 2m, then 9 more: never reaches 10
	// within a single window → no quarantine.
	pump(q, repo, p, 9)
	clk.advance(3 * time.Minute)
	if drops := pump(q, repo, p, 9); drops != 0 {
		t.Fatalf("window should have rolled; no quarantine expected, got %d", drops)
	}
	if len(q.List(repo)) != 0 {
		t.Fatalf("expected no quarantine after window roll")
	}
}

func TestQuarantine_SelfHealAfterQuiet(t *testing.T) {
	repo := t.TempDir()
	q, clk := newTestTracker(t)
	p := filepath.Join(repo, "build", "a.o")

	pump(q, repo, p, 10) // quarantine
	if len(q.List(repo)) != 1 {
		t.Fatalf("expected quarantine")
	}

	// Not quiet long enough → no heal.
	clk.advance(10 * time.Minute)
	if healed := q.Sweep(); len(healed) != 0 {
		t.Fatalf("should not heal before quiet window, got %+v", healed)
	}
	if len(q.List(repo)) != 1 {
		t.Fatalf("still quarantined expected")
	}

	// Past the 15m quiet window → heal.
	clk.advance(6 * time.Minute)
	healed := q.Sweep()
	if len(healed[repo]) != 1 || healed[repo][0] != "build" {
		t.Fatalf("expected build healed, got %+v", healed)
	}
	if len(q.List(repo)) != 0 {
		t.Fatalf("expected un-quarantined after heal")
	}

	// After heal, the dir is observed fresh again (events not dropped).
	if q.Observe(repo, p) {
		t.Fatalf("healed dir should accept events again")
	}
}

func TestQuarantine_OngoingChurnDelaysHeal(t *testing.T) {
	repo := t.TempDir()
	q, clk := newTestTracker(t)
	p := filepath.Join(repo, "build", "a.o")
	pump(q, repo, p, 10)

	// A still-thrashing dir keeps emitting events; each refreshes lastEvent so
	// the quiet timer never elapses.
	for i := 0; i < 5; i++ {
		clk.advance(10 * time.Minute)
		q.Observe(repo, p) // dropped, but refreshes lastEvent
		if healed := q.Sweep(); len(healed) != 0 {
			t.Fatalf("dir still churning must not heal, iter %d: %+v", i, healed)
		}
	}
	if len(q.List(repo)) != 1 {
		t.Fatalf("expected still quarantined")
	}
}

func TestQuarantine_AncestorDropsDescendants(t *testing.T) {
	repo := t.TempDir()
	q, _ := newTestTracker(t)
	dir := filepath.Join(repo, "out")
	pump(q, repo, filepath.Join(dir, "a.js"), 10) // quarantine "out"

	// An event in a nested subdir of the quarantined dir is dropped too.
	if !q.Observe(repo, filepath.Join(dir, "nested", "deep", "b.js")) {
		t.Fatalf("event under quarantined ancestor should be dropped")
	}
	if !q.IsQuarantined(repo, filepath.Join(dir, "nested", "x")) {
		t.Fatalf("nested path should report quarantined via ancestor")
	}
}

func TestQuarantine_PersistAndReload(t *testing.T) {
	repo := t.TempDir()
	q, _ := newTestTracker(t)
	pump(q, repo, filepath.Join(repo, "build", "a.o"), 10)

	// File should exist on disk.
	data, err := os.ReadFile(quarantinePath(repo))
	if err != nil {
		t.Fatalf("expected persisted quarantine file: %v", err)
	}
	var f quarantineFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if len(f.Dirs) != 1 || f.Dirs[0].Rel != "build" {
		t.Fatalf("unexpected persisted content: %+v", f)
	}

	// A fresh tracker over the same repo loads the persisted set on first touch.
	q2 := NewQuarantineTracker(nil)
	if !q2.IsQuarantined(repo, filepath.Join(repo, "build", "z.o")) {
		t.Fatalf("reloaded tracker should honour persisted quarantine")
	}
}

func TestQuarantine_ManualUnquarantine(t *testing.T) {
	repo := t.TempDir()
	q, _ := newTestTracker(t)
	pump(q, repo, filepath.Join(repo, "build", "a.o"), 10)

	if !q.Unquarantine(repo, "build") {
		t.Fatalf("expected manual un-quarantine to remove entry")
	}
	if len(q.List(repo)) != 0 {
		t.Fatalf("expected empty after manual override")
	}
	if q.Unquarantine(repo, "nope") {
		t.Fatalf("un-quarantining a non-quarantined dir should return false")
	}
}

func TestQuarantine_DisabledIsNoOp(t *testing.T) {
	repo := t.TempDir()
	q := &QuarantineTracker{
		cfg:         quarantineConfig{disabled: true},
		now:         time.Now,
		churn:       make(map[string]map[string]*dirChurn),
		quarantined: make(map[string]map[string]QuarantineReason),
		loaded:      make(map[string]bool),
	}
	for i := 0; i < 100; i++ {
		if q.Observe(repo, filepath.Join(repo, "build", "a.o")) {
			t.Fatalf("disabled tracker must never drop")
		}
	}
}

func TestQuarantine_NilReceiverSafe(t *testing.T) {
	var q *QuarantineTracker
	if q.Observe("/repo", "/repo/x") {
		t.Fatalf("nil tracker Observe must be a no-op")
	}
	if q.IsQuarantined("/repo", "/repo/x") {
		t.Fatalf("nil tracker IsQuarantined must be false")
	}
	if q.Sweep() != nil {
		t.Fatalf("nil tracker Sweep must return nil")
	}
}

func TestQuarantine_RepoRootNotQuarantined(t *testing.T) {
	repo := t.TempDir()
	q, _ := newTestTracker(t)
	// Events for a file directly at the repo root (dir == ".") must never
	// quarantine the root.
	for i := 0; i < 50; i++ {
		if q.Observe(repo, filepath.Join(repo, "main.go")) {
			t.Fatalf("repo-root events must never be quarantined")
		}
	}
	if len(q.List(repo)) != 0 {
		t.Fatalf("root must not appear in quarantine set")
	}
}
