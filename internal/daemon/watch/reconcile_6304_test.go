package watch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ---------------------------------------------------------------------------
// #6304 — fsnotify silently abandons a directory listing.
//
// Two flavours of test here, and the split is deliberate.
//
//   - The DARWIN tests reproduce the real kernel condition. They do not wait for
//     a rare scheduling accident: an entry with mode 0000 makes
//     sendCreateIfNew's internalWatch fail with EACCES every single time, and
//     dirChange's response to EACCES is `return nil` — the abandonment, on
//     demand. Everything after that entry in readdir order is neither watched
//     nor reported. These are the tests that pin the PRODUCTION consequences:
//     the ledger deficit and, more importantly, the edits that stop arriving.
//
//   - The PORTABLE tests drive the reconciler against a deficit produced by the
//     testEvents seam (the loop goroutine is pointed at channels the test owns,
//     so no Create the backend emits is ever charged). They pin the ledger
//     arithmetic, the budget refusal, the quarantine interaction and the
//     no-deficit no-op on every platform, with the injected kqueue cost model.
//
// The CI failure this came from has never been reproduced by re-running the
// suite — 52 attempts — so neither kind of test is allowed to depend on timing
// to FAIL. Each one either drives reconcileOnce directly or waits on a signal
// that cannot arrive at all unless the repair happened.
//
// WHAT IS NOT COVERED OFF DARWIN, stated plainly. The portable tests model the
// LEDGER half only: they assert that the sweep charges what it opens, attributes
// it, refuses when the budget is gone and leaves a settled tree alone. They
// cannot assert the half that matters more — that an edit under a recovered file
// reaches the sink again — because that needs a backend which takes a descriptor
// per entry and a listing that abandons, and only kqueue does both. On Linux and
// Windows CI these tests verify the arithmetic and nothing about the repair's
// effect on watching.
// ---------------------------------------------------------------------------

// abandonedListingTree builds a repo whose src/ holds `blockers` unreadable
// entries and nothing else. "0" sorts before "z", and os.ReadDir returns entries
// sorted by name, so every dirChange over src/ reaches a blocker before any file
// created later: internalWatch fails with EACCES, dirChange returns nil, and the
// rest of the listing is abandoned. Deterministically, on every listing, with no
// dependence on scheduling.
//
// The blockers exist BEFORE the subscription on purpose. watchDirectoryFiles
// tolerates EACCES and marks the entry seen anyway (backend_kqueue.go:603-612),
// so each blocker is reported exactly never; planted afterwards they would be
// seen by nothing, and every later dirChange would re-report the same blocker as
// a Create for the life of the watcher.
//
// THE PHANTOM CHARGE. grafel's own chargeDir counts every non-directory entry of
// a directory it subscribes, blockers included, so the baseline reading taken
// after AddRepo carries one descriptor per blocker for descriptors fsnotify never
// opened. That is a pre-existing over-count on the subscribe path, not this
// change's, and it is why the repair's ledger movement is `recovered - blockers`:
// dirEntries already counts the blockers, and repairDir charges the difference
// between that tally and what it actually managed to watch. What the tests assert
// as the real invariant is the end state — dirEntries equals the number of
// entries fsnotify holds.
func abandonedListingTree(t *testing.T, blockers int) (root, dir string) {
	t.Helper()
	root = t.TempDir()
	dir = filepath.Join(root, "src")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for i := 0; i < blockers; i++ {
		p := filepath.Join(dir, fmt.Sprintf("0blocker%d.bin", i))
		if err := os.WriteFile(p, []byte("x"), 0o000); err != nil {
			t.Fatalf("write blocker: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return root, dir
}

func requireKqueueAbandonment(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("dirChange/sendCreateIfNew is the kqueue backend; #6304 is a kqueue defect")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: open(2) ignores mode 0000, so the listing never abandons")
	}
}

// sinkReached polls the shared sinkRecorder (resume_catchup_6269_test.go) for a
// call count, and REPORTS rather than fails: both outcomes are assertions here —
// the broken state is "no call ever arrives", the fixed state is "one does".
func sinkReached(r *sinkRecorder, want int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if r.count() >= want {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// TestAbandonedListingLeavesEntriesUnwatchedAndUncharged is the premise, and it
// is the failure the issue describes, reproduced rather than inferred. It
// asserts the BROKEN state — no repair is run — so it goes on passing after the
// fix and fails loudly the day fsnotify stops abandoning listings, at which
// point the reconciler's reason to exist should be re-examined.
func TestAbandonedListingLeavesEntriesUnwatchedAndUncharged(t *testing.T) {
	requireKqueueAbandonment(t)
	root, dir := abandonedListingTree(t, 1)
	rec := &sinkRecorder{}
	w := newReconcileWatcher(t, rec.sink)
	if _, err := w.AddRepo(root); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	base, _ := w.fdb.snapshot()

	const n = 5
	writeNumbered(t, dir, n)
	// Long enough for any Create that WAS going to arrive: the same 10x margin
	// over the debounce the rest of the package uses.
	time.Sleep(750 * time.Millisecond)

	if used, _ := w.fdb.snapshot(); used != base {
		t.Fatalf("premise not reproduced: ledger moved %d -> %d after creating %d files. "+
			"The listing was NOT abandoned, so this test is no longer exercising #6304", base, used, n)
	}
	if got := rec.count(); got != 0 {
		t.Fatalf("premise not reproduced: sink fired %d times for files whose Creates were never delivered", got)
	}

	// The consequence that matters: those files are not watched, so editing one
	// produces no event at all and no re-index is ever armed.
	editInPlace(t, filepath.Join(dir, "z2.go"))
	if sinkReached(rec, 1, 750*time.Millisecond) {
		t.Fatalf("premise not reproduced: an edit under an abandoned listing DID reach the sink")
	}
}

// TestReconcileReWatchesAndChargesAnAbandonedListing is the fix, against the
// real kernel condition. Both consequences in one run: the ledger comes back to
// what the process should hold, and — the part that matters in production —
// edits under the recovered files start arming a re-index again.
//
// Against the unfixed product this fails at the ledger assertion: nothing
// re-lists a subscribed directory, so `used` never moves off base.
func TestReconcileReWatchesAndChargesAnAbandonedListing(t *testing.T) {
	requireKqueueAbandonment(t)
	root, dir := abandonedListingTree(t, 1)
	const blockers = 1
	rec := &sinkRecorder{}
	w := newReconcileWatcher(t, rec.sink)
	if _, err := w.AddRepo(root); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	base, _ := w.fdb.snapshot()

	const n = 5
	writeNumbered(t, dir, n)
	time.Sleep(500 * time.Millisecond)
	if used, _ := w.fdb.snapshot(); used != base {
		t.Fatalf("premise not reproduced: ledger moved %d -> %d, so no listing was abandoned", base, used)
	}

	// The first pass only RECORDS the deficit — see scanDir. A directory whose
	// Creates are still in the channel looks identical to one whose listing was
	// abandoned, and acting on the first sighting is what makes a sweep fight
	// the event stream.
	if repaired := w.reconcileOnce(); repaired != 0 {
		t.Fatalf("the first sweep repaired %d directories; a deficit must be confirmed before it is acted on", repaired)
	}
	if used, _ := w.fdb.snapshot(); used != base {
		t.Fatalf("an unconfirmed deficit moved the ledger %d -> %d", base, used)
	}
	if repaired := w.reconcileOnce(); repaired != 1 {
		t.Fatalf("the confirming sweep repaired %d directories, want exactly 1 (%s)", repaired, dir)
	}
	// base already carries `blocker` descriptors for an entry fsnotify never
	// opened (see plantBlocker), and the repair charges only what it opened —
	// the n readable files, minus the blocker's phantom charge that dirEntries
	// already counts. The end state is the one that matters and it is exact:
	// grafel's charge for src's entries equals the n descriptors fsnotify holds,
	// asserted directly below.
	if used, _ := w.fdb.snapshot(); used != base+n-blockers {
		t.Fatalf("after reconcile the ledger reads %d, want %d — the %d recovered entries "+
			"hold descriptors the ledger must account for", used, base+n-blockers, n)
	}
	w.mu.Lock()
	tally := w.dirEntries[dir]
	w.mu.Unlock()
	if tally != n {
		t.Fatalf("dirEntries[src] = %d, want %d: the tally must equal the entries fsnotify "+
			"actually holds, not the entries the listing named", tally, n)
	}
	assertLedgerMatchesPerRepo(t, w, "after reconciling an abandoned listing")

	// Consequence (2): the files are watched again.
	before := rec.count()
	editInPlace(t, filepath.Join(dir, "z2.go"))
	if !sinkReached(rec, before+1, 3*time.Second) {
		t.Fatal("an edit under a reconciled directory still did not arm a re-index — " +
			"the ledger was repaired but the watch was not, which is the worse half of #6304")
	}
}

// TestReconcileIsIdempotentOnARepairedDirectory pins the other half of the
// contract: having repaired a directory, the sweep must leave it alone. A
// reconciler that re-lists unconditionally would charge the same entries on
// every pass — a monotonic overstatement that refuses repos the process can
// afford, which is the defect #6180 exists to prevent — and would thrash a
// directory it has no reason to touch.
func TestReconcileIsIdempotentOnARepairedDirectory(t *testing.T) {
	requireKqueueAbandonment(t)
	root, dir := abandonedListingTree(t, 1)
	const blockers = 1
	w := newReconcileWatcher(t, func(string, bool) {})
	if _, err := w.AddRepo(root); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	base, _ := w.fdb.snapshot()
	writeNumbered(t, dir, 5)
	time.Sleep(500 * time.Millisecond)
	w.reconcileOnce()
	if repaired := w.reconcileOnce(); repaired != 1 {
		// Not a Skip. The abandonment here is caused by a permission error, not
		// by a scheduling accident, so "it did not happen this run" is a broken
		// premise rather than bad luck — and a Skip would let this file go green
		// having asserted nothing.
		t.Fatalf("premise broken: the confirming sweep repaired %d directories, want 1", repaired)
	}
	settled, _ := w.fdb.snapshot()
	if settled != base+5-blockers {
		t.Fatalf("premise: ledger %d after repair, want %d", settled, base+5-blockers)
	}
	for i := 0; i < 3; i++ {
		if repaired := w.reconcileOnce(); repaired != 0 {
			t.Fatalf("pass %d repaired %d directories, want 0 — a repaired directory was re-listed", i+2, repaired)
		}
		if used, _ := w.fdb.snapshot(); used != settled {
			t.Fatalf("pass %d moved the ledger %d -> %d; a no-op sweep must be ledger-neutral", i+2, settled, used)
		}
	}
}

// ---------------------------------------------------------------------------
// Portable: the reconciler's arithmetic and its refusals, on every platform.
// ---------------------------------------------------------------------------

// deficientWatcher subscribes makePrunedTree with the loop goroutine pointed at
// channels the TEST owns, so nothing the backend reports is ever charged, then
// creates n files under src/. The result is a directory holding entries grafel
// has neither charged nor recorded — the state an abandoned listing leaves
// behind, produced without needing the kernel to co-operate.
//
// The test owning the channels is not decoration: fsnotify's own goroutine both
// sends on and closes the backend's channels, and a second sender racing that
// close is a data race rather than a simulation (#6287).
func deficientWatcher(t *testing.T, n int) (w *Watcher, root, dir string, base int) {
	t.Helper()
	root = makePrunedTree(t)
	w = withheldEventsWatcher(t, Config{FDBudget: 1_000_000, disableQuarantine: true})
	if _, err := w.AddRepo(root); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	base, _ = w.fdb.snapshot()
	dir = filepath.Join(root, "src")
	writeNumbered(t, dir, n)
	time.Sleep(50 * time.Millisecond)
	if used, _ := w.fdb.snapshot(); used != base {
		t.Fatalf("premise broken: the ledger moved %d -> %d, so the loop goroutine is "+
			"charging events the test meant to withhold", base, used)
	}
	return w, root, dir, base
}

// TestReconcileChargesExactlyTheMissingEntries is the ledger arithmetic. Note
// what it is NOT: a bound. The entries recovered are counted, and the ledger
// must move by exactly that many descriptors — the same exact-equality contract
// that caught #6287's over-count and #6293's double release.
func TestReconcileChargesExactlyTheMissingEntries(t *testing.T) {
	const n = 7
	w, _, _, base := deficientWatcher(t, n)

	if repaired := w.reconcileOnce(); repaired != 0 {
		t.Fatalf("the first sweep repaired %d directories, want 0 — a deficit is acted on only once confirmed", repaired)
	}
	if repaired := w.reconcileOnce(); repaired != 1 {
		t.Fatalf("the confirming sweep repaired %d directories, want exactly 1", repaired)
	}
	if used, _ := w.fdb.snapshot(); used != base+n {
		t.Fatalf("ledger reads %d after reconcile, want %d", used, base+n)
	}
	assertLedgerMatchesPerRepo(t, w, "after a reconcile repair")

	if repaired := w.reconcileOnce(); repaired != 0 {
		t.Fatalf("a second pass repaired %d directories, want 0", repaired)
	}
	if used, _ := w.fdb.snapshot(); used != base+n {
		t.Fatalf("a second pass moved the ledger to %d, want it to stay at %d", used, base+n)
	}
}

// TestReconcileLeavesAFullyAccountedTreeAlone is the negative control. Without
// it, a reconciler that simply re-listed and re-charged everything it swept
// would pass every test above.
func TestReconcileLeavesAFullyAccountedTreeAlone(t *testing.T) {
	w, root, base := subscribedWatcher(t)
	_ = root
	for i := 0; i < 4; i++ {
		if repaired := w.reconcileOnce(); repaired != 0 {
			t.Fatalf("pass %d repaired %d directories in a tree nothing has touched, want 0", i+1, repaired)
		}
	}
	if used, _ := w.fdb.snapshot(); used != base {
		t.Fatalf("sweeping an untouched tree moved the ledger %d -> %d", base, used)
	}
}

// TestReconcileRefusesToRepairPastTheBudget. The repair opens descriptors that
// nothing has opened yet — unlike the event path, which records opens that have
// already happened and therefore cannot refuse. This one can, and must: a
// watcher at its ceiling stays partially blind rather than pushing the process
// toward EMFILE, which is the whole premise of the package.
func TestReconcileRefusesToRepairPastTheBudget(t *testing.T) {
	const n = 7
	w, _, _, base := deficientWatcher(t, n)

	// Take the remaining headroom away from underneath the reconciler.
	_, limit := w.fdb.snapshot()
	if !w.fdb.reserve(limit - base) {
		t.Fatalf("premise broken: could not fill the budget (used %d of %d)", base, limit)
	}
	full, _ := w.fdb.snapshot()

	w.reconcileOnce() // confirm the deficit
	if repaired := w.reconcileOnce(); repaired != 0 {
		t.Fatalf("reconcileOnce repaired %d directories with a full budget, want 0", repaired)
	}
	if used, _ := w.fdb.snapshot(); used != full {
		t.Fatalf("a refused repair moved the ledger %d -> %d; a refusal must cost nothing", full, used)
	}

	// And it is a refusal, not a permanent give-up: freeing the budget lets the
	// next sweep repair the same directory.
	// No re-confirmation needed: a refusal leaves the deficit recorded, so the
	// very next sweep acts on it.
	w.fdb.release(limit - base)
	if repaired := w.reconcileOnce(); repaired != 1 {
		t.Fatalf("with the budget freed again the sweep repaired %d directories, want 1", repaired)
	}
	if used, _ := w.fdb.snapshot(); used != base+n {
		t.Fatalf("ledger reads %d after the deferred repair, want %d", used, base+n)
	}
}

// TestReconcileSkipsQuarantinedDirectories. quarantine.go exists because
// directories can churn pathologically; a sweep that re-listed one on a timer
// would be exactly the thrash it was built to stop, and the tracker is dropping
// that directory's events anyway.
func TestReconcileSkipsQuarantinedDirectories(t *testing.T) {
	const n = 7
	root := makePrunedTree(t)
	w := withheldEventsWatcher(t, Config{FDBudget: 1_000_000})
	if _, err := w.AddRepo(root); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	base, _ := w.fdb.snapshot()
	dir := filepath.Join(root, "src")
	writeNumbered(t, dir, n)
	time.Sleep(50 * time.Millisecond)

	if w.quarantine == nil {
		t.Fatal("premise broken: no quarantine tracker on this watcher")
	}
	for i := 0; i < defaultChurnThreshold+5; i++ {
		w.quarantine.Observe(root, filepath.Join(dir, "churn.tmp"))
	}
	if !w.quarantine.IsQuarantined(root, filepath.Join(dir, "_")) {
		t.Fatalf("premise broken: %d observations did not quarantine %s, so this test "+
			"would go green without exercising the skip at all", defaultChurnThreshold+5, dir)
	}
	got, _ := w.fdb.snapshot()

	w.reconcileOnce()
	if repaired := w.reconcileOnce(); repaired != 0 {
		t.Fatalf("reconcileOnce repaired %d quarantined directories, want 0", repaired)
	}
	if used, _ := w.fdb.snapshot(); used != got {
		t.Fatalf("a skipped directory moved the ledger %d -> %d", got, used)
	}
	_ = base
}

// TestReconcileSweepsEveryDirectoryAcrossCycles. The sweep is bounded per PASS,
// not per cycle, so a watcher holding more directories than reconcileBatch must
// still come round to all of them. Without this a large repo could hold a
// permanently blind directory that the sweep never reached.
func TestReconcileVisitsEveryDirectoryAcrossPasses(t *testing.T) {
	root := makeWideTree(t, reconcileBatch+40)
	w := withheldEventsWatcher(t, Config{FDBudget: 1_000_000, disableQuarantine: true})
	if _, err := w.AddRepo(root); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	base, _ := w.fdb.snapshot()

	// Starve the LAST directory in nothing in particular: every directory gets
	// one extra file, so whichever order the cycle happens to take, a pass that
	// stopped early would leave some of them unrepaired.
	w.mu.Lock()
	dirs := make([]string, 0, len(w.dirToRepo))
	for d := range w.dirToRepo {
		dirs = append(dirs, d)
	}
	w.mu.Unlock()
	for _, d := range dirs {
		if err := os.WriteFile(filepath.Join(d, "extra.go"), []byte("package p\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	time.Sleep(50 * time.Millisecond)
	if used, _ := w.fdb.snapshot(); used != base {
		t.Fatalf("premise broken: ledger moved %d -> %d without the loop charging anything", base, used)
	}

	// Passes, not directories: a pass confirms some and acts on others, and it
	// is bounded in ENTRIES acted on (maxRepairEntriesPerPass), so the number of
	// passes a full recovery needs is a property of the tree. The assertion is
	// that it terminates having reached every directory, not that it does so in
	// any particular number of passes.
	total := 0
	const maxPasses = 200
	passes := 0
	for ; passes < maxPasses && total < len(dirs); passes++ {
		total += w.reconcileOnce()
	}
	if total != len(dirs) {
		t.Fatalf("the sweep repaired %d of %d directories across %d passes — "+
			"some directory is never reached", total, len(dirs), passes)
	}
	if used, _ := w.fdb.snapshot(); used != base+len(dirs) {
		t.Fatalf("ledger reads %d, want %d (one recovered entry per directory)", used, base+len(dirs))
	}
	assertLedgerMatchesPerRepo(t, w, "after a full reconcile cycle over a wide tree")
}

// ---------------------------------------------------------------------------

// withheldEventsWatcher points the loop goroutine at channels the TEST owns, so
// nothing the real backend reports is ever charged — the deficit an abandoned
// listing leaves behind, on any platform, without needing the kernel to
// co-operate. The backend itself stays real: reconcileDir's fs.Remove/fs.Add
// must act on a live watch set for the arithmetic to mean anything.
//
// closeBackend is wrapped so the drain has an exit. Stop closes the real
// backend, which closes ITS channels — but the loop is draining these instead,
// and a loop with no exit makes every one of these tests pay the full
// watcherStopTimeout at cleanup. The test owns these channels, so it is the test
// that may close them (#6287).
//
// WITHHOLDING FROM THE LOOP IS NOT THE SAME AS LEAVING THE BACKEND UNREAD, and
// #6380 is what conflating the two costs. Re-pointing w.events at ev leaves the
// REAL backend's Events/Errors with no reader anywhere in the process. On
// Windows that is a deadlock, not a leak: fsnotify's single I/O goroutine is
// both the only sender on those channels and the serialisation point for every
// Add and Remove, so the moment it parks in sendEvent the next fs.Add never
// gets its reply — AddRepo hung for the full 15-minute test timeout over a
// 296-directory fixture, alongside "fsnotify: queue or buffer overflow". This
// is the third way into the invariant at watcher.go:172-185, and the only one
// that is purely a property of a harness.
//
// So the real channels get a discard sink for the lifetime of the test. That
// costs these tests nothing, because what they actually require is that no
// Create the backend reports is ever CHARGED — and charging happens on the loop
// goroutine, which is reading ev/errs, not these. Drained rather than buffered
// deliberately: the volume is one event per file per directory, so any fixed
// buffer is a size that silently becomes wrong the next time a fixture grows.
func withheldEventsWatcher(t *testing.T, cfg Config) *Watcher {
	t.Helper()
	ev := make(chan fsnotify.Event)
	errs := make(chan error)
	cfg.testEvents, cfg.testErrors = ev, errs
	// The AUTOMATIC sweep is off here, and only here. These tests drive
	// reconcileOnce by hand and count what it returns; a second sweeper running
	// on its own schedule repairs some of the same directories and those repairs
	// are counted by nobody, so the totals come up short about one run in six.
	// This is not the ledger suite's pin that #6307 removed — every pre-existing
	// ledger test still runs the sweep at its production cadence. It is the
	// difference between a test that observes the sweep and a test that races it.
	cfg.reconcileInterval = -1
	w := newBudgetedWatcherCfg(t, cfg)
	// Read once, under the lock, rather than per receive: HeartbeatInterval is
	// an hour here so restartBackend never fires, and binding the sink to THIS
	// generation keeps it from straddling two if that ever changes.
	w.mu.Lock()
	fw := w.fs
	w.mu.Unlock()
	drained := discardBackendChannels(fw.Events, fw.Errors)

	realClose := w.closeBackend
	w.closeBackend = func() error {
		err := realClose()
		// A backend closes its own channels as the last act of Close, which is
		// what ends the sink. Bounded, and an assertion rather than a wait: an
		// unbounded receive here would trade the hang this fixes for another.
		select {
		case <-drained:
		case <-time.After(5 * time.Second):
			t.Errorf("the backend discard sink outlived Close by 5s — fsnotify did not " +
				"close its Events/Errors, so the sink is a leaked goroutine")
		}
		close(ev)
		close(errs)
		return err
	}
	return w
}

// discardBackendChannels reads a backend's Events and Errors and throws both
// away, until each is closed. It exists so withheldEventsWatcher can withhold
// events from the WATCHER without withholding them from the process — see the
// comment above, and the invariant at watcher.go:172-185.
//
// The returned channel closes when both inputs are closed and the goroutine has
// returned, so a caller can assert the sink does not outlive the backend.
func discardBackendChannels(ev <-chan fsnotify.Event, errs <-chan error) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Nil-out on close rather than returning: a nil channel is never ready
		// in a select, so the sink keeps serving whichever side is still open.
		// Windows closes these from inside Close, after teardown sends that are
		// still arriving on the other one.
		for ev != nil || errs != nil {
			select {
			case _, ok := <-ev:
				if !ok {
					ev = nil
				}
			case _, ok := <-errs:
				if !ok {
					errs = nil
				}
			}
		}
	}()
	return done
}

func newReconcileWatcher(t *testing.T, sink EventSink) *Watcher {
	t.Helper()
	w, err := NewWatcherConfig(Config{
		Debounce:          50 * time.Millisecond,
		BulkThreshold:     10000,
		HeartbeatInterval: time.Hour,
		reconcileInterval: time.Hour, // driven explicitly; never on a timer
		FDBudget:          1_000_000,
		fdCost:            kqueueCostModel,
		disableQuarantine: true,
	}, sink, nil)
	if err != nil {
		t.Fatalf("NewWatcherConfig: %v", err)
	}
	t.Cleanup(w.Stop)
	return w
}

func writeNumbered(t *testing.T, dir string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		p := filepath.Join(dir, fmt.Sprintf("z%d.go", i))
		if err := os.WriteFile(p, []byte("package p\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
}

func editInPlace(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("package p // edited\n"), 0o644); err != nil {
		t.Fatalf("edit %s: %v", path, err)
	}
}

// TestRemoveRepoClosesEntryWatchesTheRepairOpened is the descriptor-leak guard
// on the repair itself. fs.Add marks a path "added by the user", and fsnotify
// deliberately leaves user-added paths alone when it unwatches a directory's
// contents — so the fs.Remove(dir) that every teardown path in watcher.go uses
// does NOT close the entry watches reconcileDir took. Leaking descriptors in the
// package built to bound them would be a poor trade for a repair.
//
// WatchList reports exactly the user-added paths, which is what makes the leak
// observable rather than inferred.
func TestRemoveRepoClosesEntryWatchesTheRepairOpened(t *testing.T) {
	const n = 7
	w, root, dir, _ := deficientWatcher(t, n)
	w.reconcileOnce()
	if w.reconcileOnce() != 1 {
		t.Fatal("premise broken: the deficient directory was not repaired")
	}

	inDir := func() []string {
		var out []string
		for _, p := range w.fs.WatchList() {
			if filepath.Dir(p) == dir {
				out = append(out, p)
			}
		}
		return out
	}
	if got := len(inDir()); got != n+1 {
		t.Fatalf("after the repair fsnotify holds %d user watches under %s, want %d "+
			"(the %d recovered entries plus the pre-existing one)", got, dir, n+1, n)
	}

	w.RemoveRepo(root)
	if got := inDir(); len(got) != 0 {
		t.Fatalf("RemoveRepo left %d entry watches open under %s: %v — "+
			"fs.Remove of the directory does not reach user-added entry watches", len(got), dir, got)
	}
	if used, _ := w.fdb.snapshot(); used != 0 {
		t.Fatalf("RemoveRepo returned the repo to a ledger reading of %d, want 0", used)
	}
}

// TestReconcileChargesOnlyEntriesItCouldActuallyWatch is the case the repair
// meets FIRST in the wild, because an unreadable entry is what makes dirChange
// abandon a listing in the first place. Charging from the listing rather than
// from what was actually opened gets it wrong twice over: it puts a descriptor
// on the ledger that does not exist and that nothing will ever release — an
// unwatched path emits no Remove — and it raises dirEntries to the full listing
// count, so `want <= have` holds forever and the sweep never comes back. The
// directory ends up permanently blind AND permanently over-charged, and
// TestReconcileIsIdempotentOnARepairedDirectory would call that a success:
// idempotence is not correctness.
//
// Two blockers, so the assertion cannot be satisfied by an off-by-one.
func TestReconcileChargesOnlyEntriesItCouldActuallyWatch(t *testing.T) {
	requireKqueueAbandonment(t)
	const (
		blockers = 2
		readable = 3
	)
	root, dir := abandonedListingTree(t, blockers)
	w := newReconcileWatcher(t, func(string, bool) {})
	if _, err := w.AddRepo(root); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	base, _ := w.fdb.snapshot()

	writeNumbered(t, dir, readable)
	time.Sleep(500 * time.Millisecond)

	w.reconcileOnce()
	w.reconcileOnce()
	if used, _ := w.fdb.snapshot(); used != base+readable-blockers {
		t.Fatalf("ledger reads %d after the repair, want %d — only the %d readable entries "+
			"hold a descriptor, and base already carries %d phantom charges for the blockers; "+
			"the unreadable entries hold nothing and nothing would ever release a charge "+
			"made for them", used, base+readable-blockers, readable, blockers)
	}
	assertLedgerMatchesPerRepo(t, w, "after repairing a directory with unreadable entries")

	// And the directory must stay eligible. A repair that wrote the unreadable
	// entries off as watched would leave want <= have true forever.
	w.mu.Lock()
	_, stillPending := w.dirDeficit[dir]
	have := w.dirEntries[dir]
	w.mu.Unlock()
	if !stillPending {
		t.Fatalf("the directory was written off as repaired while two of its entries are "+
			"still unwatched (dirEntries=%d) — the sweep will never come back to it", have)
	}
	if have != readable {
		t.Fatalf("dirEntries[src] = %d, want %d: the tally must count what is watched, "+
			"not what the listing named", have, readable)
	}

	// Make them readable and the next confirmed sweep finishes the job.
	for i := 0; i < blockers; i++ {
		if err := os.Chmod(filepath.Join(dir, fmt.Sprintf("0blocker%d.bin", i)), 0o644); err != nil {
			t.Fatalf("chmod: %v", err)
		}
	}
	if repaired := w.reconcileOnce(); repaired != 1 {
		t.Fatalf("with the entries readable the sweep repaired %d directories, want 1", repaired)
	}
	if used, _ := w.fdb.snapshot(); used != base+readable {
		t.Fatalf("ledger reads %d, want %d once every entry is watchable", used, base+readable)
	}
	w.mu.Lock()
	_, pendingNow := w.dirDeficit[dir]
	w.mu.Unlock()
	if pendingNow {
		t.Fatal("a fully repaired directory is still recorded as deficient")
	}
}

// TestReconcileForgetsTheDeficitOfARemovedRepo is the leak guard on dirDeficit.
// Every other path-keyed map in watcher.go is pruned when a directory stops
// being watched; a record that outlives its directory is one dead key per
// directory the daemon ever loses.
func TestReconcileForgetsTheDeficitOfARemovedRepo(t *testing.T) {
	const n = 7
	w, root, dir, _ := deficientWatcher(t, n)
	w.reconcileOnce() // records the deficit, does not act on it
	w.mu.Lock()
	_, recorded := w.dirDeficit[dir]
	w.mu.Unlock()
	if !recorded {
		t.Fatal("premise broken: the first sweep did not record a deficit for src/")
	}

	w.RemoveRepo(root)
	w.mu.Lock()
	n2 := len(w.dirDeficit)
	w.mu.Unlock()
	if n2 != 0 {
		t.Fatalf("RemoveRepo left %d deficit records behind; they are keyed by a path "+
			"nothing will visit again", n2)
	}
}

// TestReconcileIntervalIsOperatorOverridable is the kill-switch. The sweep is on
// by default because the defect it repairs is silent, permanent and
// user-facing — but a site that hits a pathological interaction with it must be
// able to turn it off without a rebuild, exactly as GRAFEL_QUARANTINE_SWEEP_SEC
// does for the directly comparable loop.
func TestReconcileIntervalIsOperatorOverridable(t *testing.T) {
	cfg := Config{}
	if got := cfg.reconcile(); got != defaultReconcileInterval {
		t.Fatalf("with no override the cadence is %v, want the default %v", got, defaultReconcileInterval)
	}
	if got := (&Config{reconcileInterval: time.Millisecond}).reconcile(); got != minReconcileInterval {
		t.Fatalf("a sub-floor interval gave %v, want it raised to %v", got, minReconcileInterval)
	}
	t.Setenv(reconcileEnv, "45")
	if got := cfg.reconcile(); got != 45*time.Second {
		t.Fatalf("%s=45 gave %v, want 45s", reconcileEnv, got)
	}
	// A configured interval does not win over an operator who has spoken.
	cfg.reconcileInterval = 2 * time.Hour
	if got := cfg.reconcile(); got != 45*time.Second {
		t.Fatalf("%s=45 gave %v against an explicit Config, want 45s", reconcileEnv, got)
	}
	for _, off := range []string{"0", "-1"} {
		t.Setenv(reconcileEnv, off)
		if got := cfg.reconcile(); got >= 0 {
			t.Fatalf("%s=%s gave %v, want a negative value (disabled)", reconcileEnv, off, got)
		}
	}
}

// TestReconcileLoopStopsWithTheWatcher pins the sweep into the shutdown
// contract. It is counted in loopWG, so a Stop that returns while a sweep is
// still reading a directory or waiting on a repair handoff would be a Stop that
// returns with work outstanding against a backend it has just closed.
func TestReconcileLoopStopsWithTheWatcher(t *testing.T) {
	w := newBudgetedWatcherCfgNoCleanup(t, Config{FDBudget: 1_000_000, reconcileInterval: minReconcileInterval})
	root := makePrunedTree(t)
	if _, err := w.AddRepo(root); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	done := make(chan struct{})
	go func() { defer close(done); w.Stop() }()
	select {
	case <-done:
	case <-time.After(watcherStopTimeout + 2*time.Second):
		t.Fatal("Stop did not return with a sweep running at a 1ms cadence")
	}
}

// shortRepairBounds shrinks the sweep's work bounds for the duration of a test,
// so the bounds can be observed without building a tree big enough to hit the
// production ones.
func shortRepairBounds(t *testing.T, perRepair, perPass int) {
	t.Helper()
	pr, pp := maxRepairEntries, maxRepairEntriesPerPass
	maxRepairEntries, maxRepairEntriesPerPass = perRepair, perPass
	t.Cleanup(func() { maxRepairEntries, maxRepairEntriesPerPass = pr, pp })
}

// TestReconcileRefusesADirectoryTooWideToRepair. A partial repair cannot be
// accounted: the charge is derived from "every entry was attempted", and half an
// attempt makes that count a fiction — the same reasoning that makes the charge
// come from len(added) rather than from the listing. So an oversized directory is
// refused loudly and kept eligible, not repaired halfway.
func TestReconcileRefusesADirectoryTooWideToRepair(t *testing.T) {
	const n = 7
	shortRepairBounds(t, 3, 4096)
	w, _, dir, base := deficientWatcher(t, n)

	for i := 0; i < 3; i++ {
		if repaired := w.reconcileOnce(); repaired != 0 {
			t.Fatalf("pass %d repaired %d directories holding more entries than the cap, want 0", i+1, repaired)
		}
	}
	if used, _ := w.fdb.snapshot(); used != base {
		t.Fatalf("a refused directory moved the ledger %d -> %d", base, used)
	}
	w.mu.Lock()
	_, pending := w.dirDeficit[dir]
	w.mu.Unlock()
	if !pending {
		t.Fatal("an oversized directory was forgotten rather than kept eligible")
	}

	// Raise the ceiling and the very next confirmed sweep acts on it.
	shortRepairBounds(t, 4096, 4096)
	if repaired := w.reconcileOnce(); repaired != 1 {
		t.Fatalf("with the ceiling raised the sweep repaired %d directories, want 1", repaired)
	}
	if used, _ := w.fdb.snapshot(); used != base+n {
		t.Fatalf("ledger reads %d, want %d", used, base+n)
	}
}

// TestReconcileBoundsTheWorkOnePassPutsOnTheLoop. The repair runs on the
// goroutine that drains fsnotify, so what a pass may hand it is bounded in the
// unit the work is done in — one fs.Add per entry — and not in directories,
// which says nothing about size. Without the bound one pass over a wide tree
// hands the drain every repair at once.
func TestReconcileBoundsTheWorkOnePassPutsOnTheLoop(t *testing.T) {
	shortRepairBounds(t, 4096, 4)
	root := makeWideTree(t, 12)
	w := withheldEventsWatcher(t, Config{FDBudget: 1_000_000, disableQuarantine: true})
	if _, err := w.AddRepo(root); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	base, _ := w.fdb.snapshot()
	w.mu.Lock()
	dirs := make([]string, 0, len(w.dirToRepo))
	for d := range w.dirToRepo {
		dirs = append(dirs, d)
	}
	w.mu.Unlock()
	for _, d := range dirs {
		if err := os.WriteFile(filepath.Join(d, "extra.go"), []byte("package p\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	time.Sleep(50 * time.Millisecond)

	w.reconcileOnce() // confirms every directory; acts on none

	// Every directory is now deficient by at least one entry, so a budget of
	// maxRepairEntriesPerPass admits at most that many of them in a pass — the
	// bound is in entries, and no directory here holds fewer than one. Without
	// the bound a single pass hands the drain all of them at once.
	first := w.reconcileOnce()
	if first > maxRepairEntriesPerPass {
		t.Fatalf("one pass repaired %d directories against a %d-entry budget, want at most %d",
			first, maxRepairEntriesPerPass, maxRepairEntriesPerPass)
	}
	if first >= len(dirs) {
		t.Fatalf("one pass repaired all %d directories; the per-pass bound is not held", len(dirs))
	}

	// And the rest are not lost: they keep their confirmed deficit and are
	// picked up by later passes.
	total := first
	for i := 0; i < 50 && total < len(dirs); i++ {
		total += w.reconcileOnce()
	}
	if total != len(dirs) {
		t.Fatalf("only %d of %d directories were reached after the bounded passes", total, len(dirs))
	}
	if used, _ := w.fdb.snapshot(); used != base+len(dirs) {
		t.Fatalf("ledger reads %d, want %d (one recovered entry per directory)", used, base+len(dirs))
	}
}

// TestRepairRechecksQuarantineBeforeActing. The scan and the repair are separate
// steps and the tracker can quarantine a directory between them. Driven
// directly, because the window is a few instructions wide and cannot be pinned
// by interleaving alone.
func TestRepairRechecksQuarantineBeforeActing(t *testing.T) {
	const n = 7
	root := makePrunedTree(t)
	w := withheldEventsWatcher(t, Config{FDBudget: 1_000_000})
	if _, err := w.AddRepo(root); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	base, _ := w.fdb.snapshot()
	dir := filepath.Join(root, "src")
	writeNumbered(t, dir, n)
	time.Sleep(50 * time.Millisecond)

	entries := make([]string, 0, n+1)
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range ents {
		if !e.IsDir() {
			entries = append(entries, filepath.Join(dir, e.Name()))
		}
	}
	for i := 0; i < defaultChurnThreshold+5; i++ {
		w.quarantine.Observe(root, filepath.Join(dir, "churn.tmp"))
	}
	if !w.quarantine.IsQuarantined(root, filepath.Join(dir, "_")) {
		t.Fatalf("premise broken: %d observations did not quarantine %s", defaultChurnThreshold+5, dir)
	}

	res := w.repairDir(dir, w.fdb.model(), entries)
	if res.charged != 0 || res.settled {
		t.Fatalf("repairDir acted on a directory quarantined after the scan: %+v", res)
	}
	if used, _ := w.fdb.snapshot(); used != base {
		t.Fatalf("a quarantined repair moved the ledger %d -> %d", base, used)
	}
}

// TestReconcileDoesNotChargeTheCreatesItsOwnRepairProvokes is the interference
// test, and it is deterministic: no cadence, no sleep, no racing the scheduler.
//
// An entry the repair Add()s was never markSeen'd by fsnotify, so the
// directory's next dirChange reports it as a Create even though the descriptor
// already exists (backend_kqueue.go:657) — and any Create still sitting in the
// channel when the repair runs arrives afterwards too. Both are charges for
// descriptors the repair has already paid for. The pre-charge markers are what
// stop them landing twice, and this test drives exactly that sequence by owning
// the channel the loop drains and delivering the Creates by hand.
//
// The sentinel at the end is the synchronisation: the loop is FIFO, so a Create
// for a path the markers do NOT cover cannot be processed before the ones queued
// ahead of it. When the ledger moves by that one descriptor, every event before
// it has been handled.
func TestReconcileDoesNotChargeTheCreatesItsOwnRepairProvokes(t *testing.T) {
	const n = 7
	w, root, dir, base := deficientWatcher(t, n)
	w.reconcileOnce()
	if repaired := w.reconcileOnce(); repaired != 1 {
		t.Fatalf("premise broken: the confirming sweep repaired %d directories, want 1", repaired)
	}
	if used, _ := w.fdb.snapshot(); used != base+n {
		t.Fatalf("premise broken: ledger %d after the repair, want %d", used, base+n)
	}
	repaired, _ := w.fdb.snapshot()

	// Every entry the repair touched, reported as a Create — what fsnotify does
	// on the directory's next NOTE_WRITE, and what a Create already queued at
	// repair time looks like on arrival.
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		w.cfg.testEvents <- fsnotify.Event{Name: filepath.Join(dir, e.Name()), Op: fsnotify.Create}
	}
	// Sentinel: a path no marker covers, so it must be charged exactly once.
	sentinel := filepath.Join(dir, "sentinel.go")
	if err := os.WriteFile(sentinel, []byte("package p\n"), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}
	w.cfg.testEvents <- fsnotify.Event{Name: sentinel, Op: fsnotify.Create}
	deadline := time.Now().Add(5 * time.Second)
	used := 0
	for time.Now().Before(deadline) {
		if used, _ = w.fdb.snapshot(); used >= repaired+1 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if used != repaired+1 {
		t.Fatalf("replaying the repair's own Creates moved the ledger %d -> %d; only the one "+
			"sentinel descriptor is new, the rest were charged by the repair and must be "+
			"suppressed", repaired, used)
	}
	assertLedgerMatchesPerRepo(t, w, "after replaying a repair's provoked Creates")

	// And the tally moved with the sentinel's charge. If it had not, the next
	// sweep would read the directory as short and charge the whole of it again.
	w.mu.Lock()
	have := w.dirEntries[dir]
	w.mu.Unlock()
	if have != len(ents)+1 {
		t.Fatalf("dirEntries[src] = %d, want %d", have, len(ents)+1)
	}
	for i := 0; i < 3; i++ {
		if repairedAgain := w.reconcileOnce(); repairedAgain != 0 {
			t.Fatalf("sweep %d repaired %d directories that are fully accounted, want 0", i+1, repairedAgain)
		}
	}
	if after, _ := w.fdb.snapshot(); after != used {
		t.Fatalf("later sweeps moved the ledger %d -> %d over an unchanged directory", used, after)
	}
	_ = root
}

// ---------------------------------------------------------------------------
// The Windows contract (#6307), pinned portably.
//
// fsnotify's Windows backend serialises Add and Remove through its single I/O
// goroutine — `w.input <- in; return <-in.reply` (backend_windows.go:141-146,
// :162-167) — and that same goroutine is the only sender on Events and Errors,
// through a sendEvent/sendError that blocks until grafel receives (:69-91). An
// Add or Remove therefore completes ONLY while grafel's loop goroutine is free
// to drain. Two ways to break that, and this change made both:
//
//   - hold w.mu across the call, and a loop parked in chargeEventOpen waiting
//     for that same mutex can never drain. This is what fired: the #6304 refusal
//     unwind folded fs.Remove into the critical section, and the Windows job
//     wedged for the full 15-minute test timeout on a pre-existing test.
//   - make the call FROM the loop goroutine, which then cannot drain because it
//     is inside the call. That was repairDir's original placement.
//
// The kqueue and inotify backends do this work in the caller's goroutine and
// depend on nothing, so neither mistake is observable off Windows. The seams
// below model the dependency, which makes both failures reproducible on every
// platform in milliseconds instead of on one platform in fifteen minutes.
// ---------------------------------------------------------------------------

// windowsShapedBackend replaces the Add/Remove seams with stand-ins that
// complete only if the loop goroutine keeps draining — the Windows backend's
// actual contract. Two probes, not one: the first proves the loop RECEIVED, the
// second proves it finished handling the first and came back for more. A Create
// is used rather than a Chmod precisely because handleEvent takes w.mu for it
// and a Chmod returns before it does, so only a Create catches a caller holding
// the lock.
//
// The probe path lies outside every watched directory, so chargeEventOpen takes
// the lock, finds no owning repo and charges nothing.
func windowsShapedBackend(t *testing.T, w *Watcher) {
	t.Helper()
	probe := filepath.Join(t.TempDir(), "probe.go")
	drain := func(what, name string) error {
		for i := 0; i < 2; i++ {
			select {
			case w.cfg.testEvents <- fsnotify.Event{Name: probe, Op: fsnotify.Create}:
			case <-time.After(2 * time.Second):
				return fmt.Errorf("%s(%s): the event drain made no progress, so this call "+
					"would never return on Windows", what, name)
			}
		}
		return nil
	}
	w.fsAdd = func(name string) error { return drain("Add", name) }
	w.fsRemove = func(name string) error { return drain("Remove", name) }
}

// lockAssertingBackend lives in removerepo_6309_test.go: the RemoveRepo half of
// this deadlock class was split out of #6307 and landed as #6310, and main's
// helper is the same stand-in with a record of what was called. This file uses
// it rather than redeclaring it.

// TestRemoveRepoAfterRepairDoesNotCallTheBackendUnderTheLock covers the
// teardown path once the repair has taken entry watches of its own, so
// RemoveRepo has more than the directory itself to drop. The bare RemoveRepo
// case is TestRemoveRepoDoesNotCallTheBackendUnderTheLock in
// removerepo_6309_test.go.
func TestRemoveRepoAfterRepairDoesNotCallTheBackendUnderTheLock(t *testing.T) {
	const n = 7
	w, root, _, _ := deficientWatcher(t, n)
	w.reconcileOnce()
	if repaired := w.reconcileOnce(); repaired != 1 {
		t.Fatalf("premise broken: the confirming sweep repaired %d directories, want 1", repaired)
	}
	// Installed after the repair so the repair's own entry watches are real.
	_ = lockAssertingBackend(t, w)
	w.RemoveRepo(root)
}

// TestRefusedSubscriptionDoesNotCallTheBackendUnderTheLock is the path that
// actually wedged Windows CI: subscribeRepo's refusal unwind.
func TestRefusedSubscriptionDoesNotCallTheBackendUnderTheLock(t *testing.T) {
	root := makeTree(t, 4)
	dirs, files := countTree(t, root)
	w := withheldEventsWatcher(t, Config{FDBudget: dirs + files - 1})
	_ = lockAssertingBackend(t, w)
	if _, err := w.AddRepo(root); !isFDBudgetError(err) {
		t.Fatalf("premise broken: AddRepo err = %v, want a budget refusal so the unwind runs", err)
	}
}

// TestRepairNeverBlocksTheEventDrain is the repair's half of the contract. It
// fails if repairDir is moved back onto the loop goroutine, and it fails if the
// Adds are made under w.mu.
func TestRepairNeverBlocksTheEventDrain(t *testing.T) {
	const n = 7
	w, _, dir, _ := deficientWatcher(t, n)
	windowsShapedBackend(t, w)

	done := make(chan int, 1)
	go func() {
		w.reconcileOnce()
		done <- w.reconcileOnce()
	}()
	select {
	case repaired := <-done:
		if repaired != 1 {
			t.Fatalf("the sweep repaired %d directories against a backend that needs the "+
				"drain to run, want 1 — the Adds did not complete", repaired)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the sweep never returned: the repair is blocking the goroutine the " +
			"backend depends on, which is the Windows deadlock")
	}
	w.mu.Lock()
	have := w.dirEntries[dir]
	w.mu.Unlock()
	if have != n+1 {
		t.Fatalf("dirEntries[src] = %d, want %d", have, n+1)
	}
}

// TestDiscardBackendChannelsKeepsReceivingPastAnyBuffer is the regression guard
// on the harness fix for #6380. withheldEventsWatcher points the LOOP goroutine
// at channels the test owns, which leaves the real backend's own Events/Errors
// with no reader at all. On Windows that is a deadlock and not a leak — see the
// invariant at watcher.go:172-185 — so the harness runs a discard sink over the
// real channels for the lifetime of the test, and this pins that the sink keeps
// receiving without bound and then goes away.
//
// The channels here are UNBUFFERED on purpose: a send on an unbuffered channel
// completes only if something actually received it, so this test cannot pass by
// accident against a sink that has stopped.
func TestDiscardBackendChannelsKeepsReceivingPastAnyBuffer(t *testing.T) {
	ev := make(chan fsnotify.Event)
	errs := make(chan error)
	done := discardBackendChannels(ev, errs)

	// reconcileBatch+40 is the directory count of
	// TestReconcileVisitsEveryDirectoryAcrossPasses, the fixture that wedged
	// Windows CI for the full 15-minute timeout. Nothing about the number is
	// load-bearing except that it is larger than any buffer the harness could
	// plausibly have picked instead of draining.
	const n = reconcileBatch + 40
	for i := 0; i < n; i++ {
		select {
		case ev <- fsnotify.Event{Name: "x.go", Op: fsnotify.Create}:
		case <-time.After(5 * time.Second):
			t.Fatalf("the discard sink stopped receiving events after %d of %d; on Windows "+
				"fsnotify's I/O goroutine parks in sendEvent right here and every later "+
				"fs.Add deadlocks waiting for a reply it can never send", i, n)
		}
		select {
		case errs <- errors.New("fsnotify: queue or buffer overflow"):
		case <-time.After(5 * time.Second):
			t.Fatalf("the discard sink stopped receiving errors after %d of %d", i, n)
		}
	}

	// Closing is how a real backend ends its sends (backend_kqueue.go:441-443).
	// One channel at a time, because the sink must survive the first close: on
	// Windows, Close's own teardown keeps sending on one after the other is done.
	close(ev)
	select {
	case <-done:
		t.Fatal("the discard sink returned while the Errors channel was still open")
	case <-time.After(50 * time.Millisecond):
	}
	select {
	case errs <- errors.New("teardown"):
	case <-time.After(5 * time.Second):
		t.Fatal("the discard sink stopped receiving errors once the Events channel closed")
	}
	close(errs)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the discard sink outlived both of its channels — one leaked goroutine per test")
	}
}
