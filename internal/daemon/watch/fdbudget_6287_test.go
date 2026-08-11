package watch

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fsnotify/fsnotify"
)

// ---------------------------------------------------------------------------
// #6287 — the ledger arithmetic in fdbudget_6268_test.go was measured against a
// fixture the product mutates while the measurement is running.
//
// TestInterleavedDirectoryFillsAcrossReposAreNotDoubleCharged read 662 against
// a want of 660 on Linux and passed on macOS. The two extra descriptors were
// not a double charge and not the internalWatch-failure residual documented
// above chargeEventOpen: they were one honest charge per repo for a directory
// the QUARANTINE TRACKER created inside each repo, mid-test, when the churn the
// test generates crossed the threshold.
//
// The chain is: Observe trips -> quarantineLocked -> persistLocked ->
// os.MkdirAll(<repo>/.grafel) -> a new entry appears in a watched directory ->
// fsnotify opens a descriptor for it and reports Create -> chargeEventOpen
// charges it (correctly: skipped directories are charged, that is #6268 (C)) ->
// ShouldSkipPath drops the event before anything subscribes to it, and nothing
// ever removes the directory, so the charge stays. Staying is right: so does
// the descriptor.
//
// Every test below drives handleEvent with a synthesised event rather than
// waiting for a real one. That is deliberate and is the only reason a
// Linux-only failure was reproducible and pinnable from macOS: there is no
// polling, no deadline, and no dependence on how a backend batches. It is also
// safe in the way fdbudget_6268_test.go's header warns a synthetic event
// generally is not — each path used here is one NOTHING creates on disk, so no
// genuine event for it can ever arrive alongside and be counted twice.
// ---------------------------------------------------------------------------

// chargedByHandleEvent runs one event through the real handleEvent and reports
// what it did to the ledger. handleEvent, not chargeEventOpen: the ORDER of the
// ledger bookkeeping against the skip and quarantine filters is a property of
// handleEvent alone, and a test that calls the helpers directly cannot observe
// it — moving the quarantine filter above the bookkeeping would leave such a
// test perfectly green.
func chargedByHandleEvent(t *testing.T, w *Watcher, path string, op fsnotify.Op) int {
	t.Helper()
	before, _ := w.fdb.snapshot()
	w.handleEvent(fsnotify.Event{Name: path, Op: op})
	after, _ := w.fdb.snapshot()
	return after - before
}

// TestQuarantineTrippingCreatesADirectoryInsideTheRepo is the first link, and
// the one that makes this a fixture defect rather than a ledger defect: the
// product writes into the repo. Driven straight through Observe — no watcher,
// nothing subscribed, no events; this is purely about the tracker's side
// effect on the filesystem.
func TestQuarantineTrippingCreatesADirectoryInsideTheRepo(t *testing.T) {
	root := makePrunedTree(t)
	q := NewQuarantineTracker(nil)
	if q.cfg.disabled {
		t.Skip("quarantine disabled by env for this process")
	}
	if _, err := os.Stat(filepath.Join(root, ".grafel")); err == nil {
		t.Fatal("premise broken: the fixture already contains .grafel")
	}

	churned := filepath.Join(root, "src", "b.go")
	for i := 0; i < q.cfg.threshold; i++ {
		q.Observe(root, churned)
	}
	if !q.IsQuarantined(root, churned) {
		t.Fatalf("premise broken: %d observations did not reach the churn threshold of %d",
			q.cfg.threshold, q.cfg.threshold)
	}

	fi, err := os.Stat(filepath.Join(root, ".grafel"))
	if err != nil {
		t.Fatalf("tripping the quarantine did not create <repo>/.grafel: %v — "+
			"if persistence moved out of the repo, the interleaved ledger test can stop "+
			"disabling the tracker", err)
	}
	if !fi.IsDir() {
		t.Fatal("<repo>/.grafel is not a directory")
	}
}

// TestQuarantineStateDirectoryIsChargedOncePerRepo is the second link, and it
// reproduces the Linux signature exactly: TWO repos, one `.grafel` each, +2 on
// a ledger that predicted +0. Two repos rather than one path charged twice,
// because those are genuinely different — a second repo resolves through a
// different repoForLocked result into a different fdReserved bucket, and a test
// that charged one path twice would not show that.
//
// The charge is CORRECT and this test asserts it as correct. What it pins is
// that the interleaved test's arithmetic cannot predict it, because WHEN the
// tracker trips depends on event batching.
func TestQuarantineStateDirectoryIsChargedOncePerRepo(t *testing.T) {
	repoA := makePrunedTree(t)
	repoB := makePrunedTree(t)
	w := newBudgetedWatcher(t, 10000)
	for _, r := range []string{repoA, repoB} {
		if _, err := w.AddRepo(r); err != nil {
			t.Fatalf("premise broken: AddRepo(%s): %v", r, err)
		}
	}
	base, _ := w.fdb.snapshot()
	per := w.fdb.model().perEntry()

	for _, r := range []string{repoA, repoB} {
		state := filepath.Join(r, ".grafel")
		if !ShouldSkipPath(state) {
			t.Fatal("premise broken: <repo>/.grafel is not on the skip list, so handleEvent " +
				"would subscribe to it and the charge would not stand alone")
		}
		if got := chargedByHandleEvent(t, w, state, fsnotify.Create); got != per {
			t.Fatalf("the quarantine state directory in %s charged %d, want %d", r, got, per)
		}
	}

	used, _ := w.fdb.snapshot()
	if used != base+2*per {
		t.Fatalf("ledger reads %d, want %d — this is the +2 the interleaved test saw on Linux",
			used, base+2*per)
	}

	// Attributed per repo, so RemoveRepo hands each back separately. A single
	// path charged twice would land entirely in one bucket and look identical
	// on the global ledger.
	w.mu.Lock()
	a, b := w.fdReserved[repoA], w.fdReserved[repoB]
	w.mu.Unlock()
	if a != b {
		t.Fatalf("charges attributed unevenly: repoA holds %d, repoB %d", a, b)
	}

	// Nothing in the flow gives it back: the directory is never removed, so no
	// Remove is ever reported for it, so releaseEventClose is never reached.
	// Unrelated churn elsewhere must not quietly return it either.
	churn := filepath.Join(repoA, "src", "churn.go")
	if got := chargedByHandleEvent(t, w, churn, fsnotify.Create); got != per {
		t.Fatalf("an ordinary Create charged %d, want %d", got, per)
	}
	if got := chargedByHandleEvent(t, w, churn, fsnotify.Remove); got != -per {
		t.Fatalf("an ordinary Remove released %d, want %d", -got, per)
	}
	if used, _ := w.fdb.snapshot(); used != base+2*per {
		t.Fatalf("ledger reads %d after a full churn cycle, want the two state directories "+
			"still charged at %d", used, base+2*per)
	}
}

// TestQuarantineFilterRunsAfterTheLedgerBookkeeping positively excludes the
// other candidate explanation for a drift of exactly two on a run with exactly
// two quarantines: that the quarantine filter itself skips a charge.
//
// It drives handleEvent, which is where that ordering lives (the ledger
// bookkeeping at the top, the quarantine filter well below it). The same
// directory is measured twice — once before it is quarantined and once after —
// so the assertion is the reviewer-proof one: a quarantined directory's events
// must move the ledger by the SAME amount an unquarantined one's do. Hoisting
// the filter above the bookkeeping fails this; asserting on the helpers
// directly would not.
func TestQuarantineFilterRunsAfterTheLedgerBookkeeping(t *testing.T) {
	root := makePrunedTree(t)
	// A second ordinary directory, so the quarantined and unquarantined
	// measurements are two live directories of one repo rather than one
	// directory before and after — no window in which a stale reading could be
	// mistaken for a match.
	if err := os.MkdirAll(filepath.Join(root, "lib"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "lib", "c.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	w := newBudgetedWatcher(t, 10000)
	if w.quarantine == nil || w.quarantine.cfg.disabled {
		t.Skip("quarantine disabled by env for this process")
	}

	// Quarantined BEFORE the repo is subscribed, on purpose. Tripping the
	// tracker writes <repo>/.grafel, and doing that under a live subscription
	// would deliver a real Create asynchronously into the very ledger these
	// deltas are measuring. Nothing is watched yet, so nothing is reported.
	quarantined := filepath.Join(root, "src", "churn.go")
	ordinary := filepath.Join(root, "lib", "churn.go")
	for i := 0; i < w.quarantine.cfg.threshold; i++ {
		w.quarantine.Observe(root, quarantined)
	}
	if !w.quarantine.IsQuarantined(root, quarantined) {
		t.Fatal("premise broken: src is not quarantined, so this proves nothing")
	}
	if w.quarantine.IsQuarantined(root, ordinary) {
		t.Fatal("premise broken: lib is quarantined too, so there is nothing to compare against")
	}
	for _, p := range []string{quarantined, ordinary} {
		if ShouldSkipPath(p) {
			t.Fatalf("premise broken: %s is on the static skip list, so the quarantine "+
				"filter is never reached and this proves nothing", p)
		}
	}

	if _, err := w.AddRepo(root); err != nil {
		t.Fatalf("premise broken: AddRepo: %v", err)
	}
	_, _, _, droppedBefore, _ := w.Stats()

	// Neither path is ever created on disk, so no genuine event for either can
	// arrive alongside these synthesised ones.
	openOrd := chargedByHandleEvent(t, w, ordinary, fsnotify.Create)
	closeOrd := chargedByHandleEvent(t, w, ordinary, fsnotify.Remove)
	if openOrd <= 0 || closeOrd != -openOrd {
		t.Fatalf("premise broken: an unquarantined Create/Remove pair moved the ledger "+
			"by %d/%d, which is not a matched pair", openOrd, closeOrd)
	}

	if got := chargedByHandleEvent(t, w, quarantined, fsnotify.Create); got != openOrd {
		t.Fatalf("a Create under a QUARANTINED directory charged %d, want %d — the same as "+
			"the unquarantined case. The quarantine filter is running BEFORE the ledger "+
			"bookkeeping, so churny directories stop being accounted", got, openOrd)
	}
	if got := chargedByHandleEvent(t, w, quarantined, fsnotify.Remove); got != closeOrd {
		t.Fatalf("a Remove under a QUARANTINED directory released %d, want %d — the same as "+
			"the unquarantined case. The quarantine filter is running BEFORE the ledger "+
			"bookkeeping, so the charge would outlive the descriptor", got, closeOrd)
	}

	// And the filter IS doing its job — otherwise the two measurements could
	// agree simply because nothing is ever quarantined.
	if _, _, _, droppedAfter, _ := w.Stats(); droppedAfter == droppedBefore {
		t.Fatal("premise broken: no event was dropped, so the quarantine filter never ran")
	}
}
