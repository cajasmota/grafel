package watch

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ---------------------------------------------------------------------------
// #6268 — the ledger must track what fsnotify's kqueue backend actually opens
// and closes, not grafel's own idea of what it subscribed.
//
// The oracle used throughout this file is the formula derived by reading
// fsnotify v1.10.1 backend_kqueue.go:
//
//	opens = |dirs grafel Add()ed| + |entries of those dirs grafel did NOT Add()|
//
// Justification, from that file:
//   - addWatch (:397-399) opens one descriptor per path, unless it is already
//     watched.
//   - addWatch (:418-433) calls watchDirectoryFiles only when the request
//     carries NOTE_WRITE, which only Add()/AddWith supplies (noteAllEvents,
//     :345); internalWatch's subdirectory branch (:677) passes
//     info.dirFlags|NOTE_DELETE|NOTE_RENAME, so watchDirectoryFiles does not
//     recurse.
//   - watchDirectoryFiles (:582-616) therefore opens exactly one descriptor for
//     every entry of the dir — files AND subdirectories, one level deep.
//
// So a pruned subdirectory still costs its own descriptor (charged by the
// parent's watchDirectoryFiles) but none of its entries.
// ---------------------------------------------------------------------------

// makePrunedTree builds:
//
//	root/a.go
//	root/src/b.go
//	root/node_modules/x.js
//
// node_modules is on SkipDirs, so grafel Add()s root and src but not
// node_modules. fsnotify still opens a descriptor for node_modules itself when
// it lists root.
func makePrunedTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"src", "node_modules"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	files := map[string]string{
		"a.go":              "package p\n",
		"src/b.go":          "package p\n",
		"node_modules/x.js": "module.exports={}\n",
		"node_modules/y.js": "module.exports={}\n",
	}
	for rel, body := range files {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

// TestPrunedTreePremise asserts the fixture is the shape the arithmetic below
// assumes, using nothing from the production watcher. Without this, every
// number in this file could be right for the wrong reason.
func TestPrunedTreePremise(t *testing.T) {
	root := makePrunedTree(t)
	if !ShouldSkipDir("node_modules") {
		t.Fatal("fixture premise broken: node_modules is not on the skip list, so nothing is pruned")
	}
	if ShouldSkipDir("src") {
		t.Fatal("fixture premise broken: src is skipped, so it is not an Add()ed dir")
	}
	ents, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(ents) != 3 {
		t.Fatalf("fixture premise broken: root has %d entries, want 3 (a.go, src, node_modules)", len(ents))
	}
}

// prunedTreeKqueueOpens is the oracle for makePrunedTree under the kqueue
// model, computed by hand from the formula above:
//
//	Add()ed dirs:        root, src                              = 2
//	entries of root not Add()ed:  a.go, node_modules            = 2
//	entries of src not Add()ed:   b.go                          = 1
//
// node_modules/x.js and node_modules/y.js cost nothing: watchDirectoryFiles is
// never run for node_modules, because grafel never Add()s it and the parent's
// listing does not recurse.
const prunedTreeKqueueOpens = 5

// --- (C) skipped directories are charged their own descriptor ---------------

// TestSkippedDirectoryIsChargedItsOwnDescriptor is the (C) defect. Before the
// fix grafel charged 4 (2 dirs + 2 files under them) for a tree on which
// fsnotify opens 5 descriptors: the pruned node_modules directory has already
// been opened by root's watchDirectoryFiles by the time the walk decides to
// prune it. Pruning saves a directory's ENTRIES, never its own descriptor.
func TestSkippedDirectoryIsChargedItsOwnDescriptor(t *testing.T) {
	root := makePrunedTree(t)
	w := newBudgetedWatcher(t, prunedTreeKqueueOpens)

	added, err := w.AddRepo(root)
	if err != nil {
		t.Fatalf("AddRepo with a budget of exactly the true cost failed: %v", err)
	}
	if added != 2 {
		t.Fatalf("subscribed %d dirs, want 2 (root + src); the fixture is not being pruned as assumed", added)
	}
	used, _ := w.fdb.snapshot()
	if used != prunedTreeKqueueOpens {
		t.Fatalf("charged %d descriptors, want %d — the pruned directory's own descriptor is unaccounted",
			used, prunedTreeKqueueOpens)
	}
}

// TestSkippedDirectoryPushesASubscriptionOverBudget is the other half of (C):
// under-charging is not a cosmetic drift, it lets a repo through that does not
// fit. A budget one short of the true cost must refuse.
func TestSkippedDirectoryPushesASubscriptionOverBudget(t *testing.T) {
	root := makePrunedTree(t)
	w := newBudgetedWatcher(t, prunedTreeKqueueOpens-1)

	if _, err := w.AddRepo(root); err == nil {
		t.Fatalf("AddRepo accepted a tree costing %d descriptors on a budget of %d",
			prunedTreeKqueueOpens, prunedTreeKqueueOpens-1)
	} else if !isFDBudgetError(err) {
		t.Fatalf("AddRepo failed with %v, want an FD-budget refusal", err)
	}
	if used, _ := w.fdb.snapshot(); used != 0 {
		t.Fatalf("refusal left %d descriptors on the ledger, want 0", used)
	}
}

// --- (A) event-time opens / (B) event-time closes ---------------------------
//
// These drive the REAL sequence: a live fsnotify watcher over a real tree, real
// filesystem mutations, and the watcher's own loop goroutine delivering the
// events. Nothing calls handleEvent directly for an event the filesystem can
// produce — a synthetic one would be double-counted alongside the genuine one
// the loop is already processing, and asserting a release helper in isolation
// would not show that the charge and the release ever meet.
//
// ONE exemption, at the end of
// TestInterleavedDirectoryFillsAcrossReposAreNotDoubleCharged: the DUPLICATE
// report of a close. No filesystem can be asked for a second report of one
// close on demand — how many a backend emits is a property of the host — so
// that one is synthesised, against a path the test has finished measuring, and
// asserted as a delta rather than against the ledger's absolute value. The rule
// above still holds for everything a real mutation can produce.
//
// The ledger arithmetic is the injected kqueue model (newBudgetedWatcher), so
// the numbers are the macOS ones on every platform; the events themselves come
// from whatever backend the host has, and Create/Remove are emitted by kqueue
// and inotify alike.

// waitLedger waits until the ledger reads want and HOLDS it for
// ledgerStableReads consecutive polls, or fails with the last reading.
//
// The wait exists because the event is delivered by the watcher's loop
// goroutine, not by the call that mutated the filesystem. The hold matters for
// a different reason: an over-charging watcher passes THROUGH the right value
// on its way to the wrong one, so a helper that returned on the first matching
// poll would report success for a ledger that then kept climbing. That is not
// hypothetical — it is how a double-charge of only some files in a burst
// escapes detection.
func waitLedger(t *testing.T, w *Watcher, want int, what string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	used, held := -1, 0
	for time.Now().Before(deadline) {
		used, _ = w.fdb.snapshot()
		if used == want {
			if held++; held >= ledgerStableReads {
				return
			}
		} else {
			held = 0
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%s: ledger read %d (held %d of %d polls), want a settled %d",
		what, used, held, ledgerStableReads, want)
}

// ledgerStableReads is how many consecutive polls must agree before a ledger
// reading counts as settled — ~150ms at the 5ms poll interval, comfortably
// longer than the gap between two events of one filesystem burst.
const ledgerStableReads = 30

// The hold a drained reading must survive, for a reading that is NOT being
// compared against a known target.
//
// It has to be long because it is the only evidence the burst has drained.
// waitLedger knows the value it is waiting for, so a plateau at some other
// value cannot satisfy it; waitLedgerAtMost accepts a RANGE, and a mid-drain
// plateau inside that range would end the wait with events still in flight —
// after which the next phase's filesystem work interleaves with the last of
// this phase's and the test measures a shape it did not build.
//
// The hold is SELF-SCALING, and that is the point. A fixed hold sets a
// wall-clock floor while the thing it has to beat — the longest gap between two
// events of one burst — is CPU- and IO-anchored and grows with runner slowness.
// A constant that clears the gap 2.6x here would clear it barely 1x on a runner
// 3x slower, silently, with no test change in between; the hosted runners are
// routinely 2-4x slower than this box for a filesystem-plus-event workload. So
// instead of guessing a constant, the wait MEASURES the longest gap it has
// already seen in this same call and demands a multiple of it. A slow runner
// stretches the gaps and stretches the required hold with them, in proportion.
//
// Measured at b55736417 on this box, the longest gap before the ledger moved
// again was 785ms across the test's four bursts (345ms, 785ms, 680ms, 270ms in
// one representative run), so the demanded hold is ~3.1s and the margin ~4x by
// construction on any runner rather than only on this one.
//
// The floor covers the phases that produce no gaps worth measuring — the two
// removal bursts drain with a longest gap of one poll — where there is nothing
// to scale from.
const (
	ledgerHoldFloor     = 1500 * time.Millisecond
	ledgerHoldFactor    = 4
	ledgerPollEvery     = 5 * time.Millisecond
	ledgerDrainDeadline = 90 * time.Second
)

// drainLedger polls until the ledger has held one value for the self-scaling
// hold described above, and returns that value. gapOK, when non-nil, is
// consulted on every reading: a reading it rejects resets the hold instead of
// counting toward it, which is how waitLedgerAtMost keeps a plateau above its
// ceiling from ever being mistaken for a drained one.
//
// Returns the last reading and whether it drained before the deadline.
func drainLedger(t *testing.T, w *Watcher, accept func(int) bool) (int, bool) {
	t.Helper()
	deadline := time.Now().Add(ledgerDrainDeadline)
	last, longestGap := -1, time.Duration(0)
	runStart := time.Now()
	for time.Now().Before(deadline) {
		used, _ := w.fdb.snapshot()
		switch {
		case accept != nil && !accept(used):
			// Not a candidate for "drained" at all: restart the hold, and do
			// NOT let this run widen longestGap — an over-ceiling plateau is
			// the failure being looked for, not evidence about event timing.
			last, runStart = used, time.Now()
		case used != last:
			// A change ends the current run. Only a run that ENDS is a gap
			// between two events; the trailing run is the drain itself and
			// must never inflate the bar it has to clear.
			if gap := time.Since(runStart); last != -1 && gap > longestGap {
				longestGap = gap
			}
			last, runStart = used, time.Now()
		default:
			hold := ledgerHoldFloor
			if scaled := time.Duration(ledgerHoldFactor) * longestGap; scaled > hold {
				hold = scaled
			}
			// Clamped, or the scaling turns a slow run into a GUARANTEED
			// failure instead of a longer one: past a single measured gap of
			// ledgerDrainDeadline/ledgerHoldFactor there is not enough time
			// left on the deadline to satisfy the hold at all, and the wait
			// fails however healthy the ledger is. A run that slow has already
			// lost the drain-vs-plateau argument; failing it for the reason it
			// is actually failing beats failing it on arithmetic.
			if hold > ledgerDrainDeadline/3 {
				hold = ledgerDrainDeadline / 3
			}
			if time.Since(runStart) >= hold {
				return used, true
			}
		}
		time.Sleep(ledgerPollEvery)
	}
	return last, false
}

// waitLedgerAtMost waits for the ledger to drain to a value at or below
// ceiling and HOLD it, or fails with the last reading.
//
// One-sided, and that is the point (#6304). fsnotify's backends abandon a
// directory listing at the first entry they cannot stat or open, and
// readEvents discards that error (backend_kqueue.go:506), so an arbitrary tail
// of a burst's Creates is never reported and never charged. Those events do
// not arrive late — they never arrive — so a wait for the exact count of files
// WRITTEN can only time out, which is how this test reddened the gate at
// 636/660 and 606/660 (`held 0 of 30 polls`). Under-running the ceiling is
// therefore accepted. Exceeding it is not: the ceiling is what fsnotify opens
// when it drops NOTHING, so a reading above it charges a descriptor that was
// never opened.
//
// A plateau above the ceiling is treated as "still draining", not as a
// failure, so an over-charge is reported by the deadline rather than by a
// lucky poll — and a burst that is merely slow is never mistaken for one.
// backendPairsEveryOpenWithAClose reports whether the host's backend can be
// relied on to report the close of every descriptor whose open it reported.
// Only on such a backend is the ABSOLUTE ledger value a function of the
// filesystem operations a test performed, and only there can a test assert one.
//
// kqueue and inotify lose a whole path's lifetime or nothing of it: #6304's
// abandoned listing means the entry is never reported, so it is never charged
// AND never released, and it cancels out of both sides of any count.
//
// fsnotify's Windows backend loses INDIVIDUAL events, silently and in either
// direction, which does not cancel:
//
//   - backend_windows.go:556-563, ERROR_MORE_DATA — "the i/o succeeded but the
//     buffer is full. In theory we should be building up a full packet. In
//     practice we can get away with just carrying on." The tail of that packet
//     is discarded with no error raised at all.
//   - backend_windows.go:565-570, ERROR_ACCESS_DENIED — a watched directory was
//     removed, so the backend sends DELETE_SELF and calls deleteWatch. Anything
//     still buffered for that directory dies with the watch.
//
// A lost Remove strands a charge and reads ABOVE the truth; a lost Create loses
// one and reads BELOW it. Neither is a grafel defect and neither is
// distinguishable, from the ledger alone, from the defects this test exists to
// catch. Observed on CI run 31750096959: this test's teardown read 35 against a
// base of 10 — 25 charges stranded, in a package otherwise green on that runner
// and fully green on Ubuntu. See the note on the test itself.
// An ALLOW-LIST, not "not Windows". The property is a claim about a specific
// backend and there are five: inotify, kqueue, ReadDirectoryChangesW, fen
// (Solaris/illumos, backend_fen.go) and the stub in backend_other.go. Only the
// first two have been read for it. Naming the platforms that were reasoned
// about, rather than the one that was ruled out, means a port to a backend
// nobody has looked at inherits "assume nothing" instead of silently inheriting
// a guarantee: the absolute-value assertions switch off there, the DELTA ones
// stay on, and the test still fails for every defect it exists to catch.
func backendPairsEveryOpenWithAClose() bool {
	switch runtime.GOOS {
	case "linux", "darwin", "freebsd", "netbsd", "openbsd", "dragonfly":
		return true // inotify and kqueue: read, and reasoned about above
	default:
		return false // windows, solaris, illumos, and anything new
	}
}

// waitLedgerAtMost returns the drained reading, so a caller can also hold it
// against the phase BEFORE it — see the delta bounds in the interleaved test,
// which are the half of this that survives a backend losing events.
func waitLedgerAtMost(t *testing.T, w *Watcher, ceiling int, what string) int {
	t.Helper()
	used, drained := drainLedger(t, w, func(v int) bool { return v <= ceiling })
	if !drained {
		t.Fatalf("%s: ledger read %d, want it drained to at most %d — "+
			"a reading above that charges descriptors fsnotify never opened",
			what, used, ceiling)
	}
	return used
}

// subscribedWatcher builds a watcher over makePrunedTree with a budget far
// larger than the tree, subscribes it, and returns the watcher, the root, and
// the ledger reading immediately after subscription.
func subscribedWatcher(t *testing.T) (w *Watcher, root string, base int) {
	t.Helper()
	root = makePrunedTree(t)
	w = newBudgetedWatcher(t, 10000)
	if _, err := w.AddRepo(root); err != nil {
		t.Fatalf("premise broken: AddRepo failed: %v", err)
	}
	base, _ = w.fdb.snapshot()
	if base != prunedTreeKqueueOpens {
		t.Fatalf("premise broken: subscription charged %d, want %d", base, prunedTreeKqueueOpens)
	}
	return w, root, base
}

// TestCreateEventChargesTheDescriptorFsnotifyOpened is the (A) defect and the
// EMFILE path. On a Write to a watched directory fsnotify runs dirChange ->
// sendCreateIfNew -> internalWatch (backend_kqueue.go:622-670), which opens a
// descriptor for the new FILE before the Create event is ever delivered to
// grafel. Before the fix grafel charged only when the new entry was a
// directory, so a git checkout landing 20k files in watched dirs opened 20k
// descriptors the ledger never saw.
func TestCreateEventChargesTheDescriptorFsnotifyOpened(t *testing.T) {
	w, root, base := subscribedWatcher(t)

	p := filepath.Join(root, "new.go")
	if err := os.WriteFile(p, []byte("package p\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	waitLedger(t, w, base+1, "one new file in a watched directory")
}

// TestCreateEventIsChargedEvenWhenTheEventIsFiltered: the skip filters decide
// whether to REINDEX, not whether fsnotify opened a descriptor.
// sendCreateIfNew calls internalWatch for every new entry of a watched dir
// regardless of its extension, so a *.log file — dropped by ShouldSkipPath
// (skip.go:196-199) — still costs a descriptor. Charging after the filters
// would leave exactly the churn-heavy paths unaccounted.
func TestCreateEventIsChargedEvenWhenTheEventIsFiltered(t *testing.T) {
	w, root, base := subscribedWatcher(t)

	p := filepath.Join(root, "build.log")
	if !ShouldSkipPath(p) {
		t.Fatal("premise broken: build.log is not filtered, so this test does not exercise the filtered path")
	}
	if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	waitLedger(t, w, base+1, "a filtered file in a watched directory")

	// The filter still did its job: a dropped event must not be a watched dir.
	w.mu.Lock()
	_, subscribed := w.dirToRepo[p]
	w.mu.Unlock()
	if subscribed {
		t.Fatal("a filtered path was subscribed as a directory")
	}
}

// TestCreateEventForASkippedDirectoryIsCharged: a directory grafel refuses to
// subscribe still had its descriptor opened by fsnotify's sendCreateIfNew ->
// internalWatch -> addWatch (backend_kqueue.go:656-681). This is (C) again, at
// event time.
func TestCreateEventForASkippedDirectoryIsCharged(t *testing.T) {
	w, root, base := subscribedWatcher(t)

	if !ShouldSkipDir("dist") {
		t.Fatal("premise broken: dist is not on the skip list")
	}
	p := filepath.Join(root, "dist")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	waitLedger(t, w, base+1, "a skipped directory created in a watched directory")

	w.mu.Lock()
	_, subscribed := w.dirToRepo[p]
	w.mu.Unlock()
	if subscribed {
		t.Fatal("a skipped directory was subscribed — the charge must not come from subscribing it")
	}
}

// TestCreateEventForASubscribedDirectoryIsChargedExactlyOnce guards the
// composition of (A) with the pre-existing subscribeDirRecursive charge. The
// new directory's descriptor is opened once — by sendCreateIfNew; grafel's
// subsequent Add() finds it alreadyWatching (backend_kqueue.go:358) and opens
// nothing new — so it must be charged once, not once by the event path and
// again by the recursive subscribe. Its two files cost one each, whichever of
// watchDirectoryFiles or a later sendCreateIfNew reaches them first.
func TestCreateEventForASubscribedDirectoryIsChargedExactlyOnce(t *testing.T) {
	w, root, base := subscribedWatcher(t)

	// Built outside the watched tree and moved in atomically. mkdir-then-write
	// would race chargeDir's listing against fsnotify's watchDirectoryFiles for
	// the same directory (see chargeDir's note), and the point of this test is
	// the double-charge, not that window.
	staged := filepath.Join(t.TempDir(), "pkg")
	if err := os.MkdirAll(staged, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, n := range []string{"c.go", "d.go"} {
		if err := os.WriteFile(filepath.Join(staged, n), []byte("package p\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	dir := filepath.Join(root, "pkg")
	if err := os.Rename(staged, dir); err != nil {
		t.Fatalf("rename into the watched tree: %v", err)
	}

	// fsnotify opens three descriptors: pkg itself, via sendCreateIfNew ->
	// internalWatch, whose subdirectory branch does NOT list the directory
	// (backend_kqueue.go:677 omits NOTE_WRITE, and the watchDir predicate at
	// :419 requires it); then c.go and d.go, via the watchDirectoryFiles that
	// grafel's own Add does trigger.
	waitLedger(t, w, base+3, "a new subscribed directory holding two files")

	w.mu.Lock()
	_, subscribed := w.dirToRepo[dir]
	w.mu.Unlock()
	if !subscribed {
		t.Fatal("premise broken: the new directory was not subscribed, so double-charging is not what is measured")
	}
}

// TestRemoveEventReleasesTheDescriptorFsnotifyClosed is the (B) defect.
// backend_kqueue.go:500-503 calls w.remove on any Rename/Remove event, and
// remove closes the descriptor (:320) without telling grafel. Before the fix
// the charge stayed forever, so a long-lived daemon in a churny repo overstated
// usage monotonically and refused repos it could afford.
func TestRemoveEventReleasesTheDescriptorFsnotifyClosed(t *testing.T) {
	w, root, base := subscribedWatcher(t)

	if err := os.Remove(filepath.Join(root, "a.go")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	waitLedger(t, w, base-1, "a watched file deleted")
}

// TestRenameReleasesTheDescriptor: backend_kqueue.go:500 tests Rename OR
// Remove, so a rename closes the descriptor the same way. Covered separately
// because a fix handling only Remove leaves half the drift.
//
// The rename target is outside the watched tree, so the move cannot be
// re-charged as a Create in the same directory and confuse the reading.
func TestRenameReleasesTheDescriptor(t *testing.T) {
	w, root, base := subscribedWatcher(t)

	dst := filepath.Join(t.TempDir(), "renamed.go")
	if err := os.Rename(filepath.Join(root, "a.go"), dst); err != nil {
		t.Fatalf("rename: %v", err)
	}
	waitLedger(t, w, base-1, "a watched file renamed out of the tree")
}

// TestRemoveOfAWatchedDirectoryReleasesAndUnmaps: a removed directory costs
// perDir, and leaving it in dirToRepo would keep routing events for a path
// fsnotify no longer watches. Driven in two steps so each release is
// attributable rather than inferred from a single aggregate.
func TestRemoveOfAWatchedDirectoryReleasesAndUnmaps(t *testing.T) {
	w, root, base := subscribedWatcher(t)

	dir := filepath.Join(root, "src")
	w.mu.Lock()
	_, watched := w.dirToRepo[dir]
	w.mu.Unlock()
	if !watched {
		t.Fatal("premise broken: src is not a watched directory")
	}

	if err := os.Remove(filepath.Join(dir, "b.go")); err != nil {
		t.Fatalf("remove file: %v", err)
	}
	waitLedger(t, w, base-1, "the only file in a watched subdirectory deleted")

	if err := os.Remove(dir); err != nil {
		t.Fatalf("remove dir: %v", err)
	}
	waitLedger(t, w, base-2, "a watched subdirectory deleted")

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		w.mu.Lock()
		_, stillWatched := w.dirToRepo[dir]
		w.mu.Unlock()
		if !stillWatched {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("removed directory is still in dirToRepo")
}

// TestChurnReturnsTheLedgerToItsSubscriptionBaseline drives the whole (A)+(B)
// composition rather than either half: create N files, then delete them, and
// the ledger must land exactly where the subscription left it. Asserting only
// that Create charges, or only that Remove releases, would pass against a fix
// whose two halves disagree about the amount.
func TestChurnReturnsTheLedgerToItsSubscriptionBaseline(t *testing.T) {
	w, root, base := subscribedWatcher(t)

	// n is kept under defaultChurnThreshold/2 (quarantine.go:68) on purpose:
	// 2n reindex-arming events in one directory would trip the quarantine
	// tracker, which persists its state by creating <repo>/.grafel
	// (quarantine.go:652). That directory is a real new entry of a watched
	// directory, so fsnotify opens a real descriptor for it and the ledger
	// correctly grows by one — a true charge, but not the one this test is
	// about. The premise assertion below fails loudly if that stops holding.
	const n = 12
	paths := make([]string, 0, n)
	for i := 0; i < n; i++ {
		p := filepath.Join(root, "src", "churn"+string(rune('a'+i))+".go")
		if err := os.WriteFile(p, []byte("package p\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		paths = append(paths, p)
	}
	waitLedger(t, w, base+n, "n files created in a watched directory")

	for _, p := range paths {
		if err := os.Remove(p); err != nil {
			t.Fatalf("remove: %v", err)
		}
	}
	waitLedger(t, w, base, "the same files deleted again")

	if _, err := os.Stat(filepath.Join(root, ".grafel")); err == nil {
		t.Fatal("premise broken: the churn tripped the quarantine tracker, which created .grafel — " +
			"the ledger reading now includes a charge this test is not measuring")
	}
}

// TestEventChargesAreReturnedByRemoveRepo: descriptors opened at event time
// belong to the owning repo, so unregistering it must hand them back. Without
// per-repo attribution the churn charge would leak on unregister.
func TestEventChargesAreReturnedByRemoveRepo(t *testing.T) {
	w, root, base := subscribedWatcher(t)

	for _, n := range []string{"e.go", "f.go", "g.go"} {
		if err := os.WriteFile(filepath.Join(root, n), []byte("package p\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	waitLedger(t, w, base+3, "three files created in a watched directory")

	w.RemoveRepo(root)
	if used, _ := w.fdb.snapshot(); used != 0 {
		t.Fatalf("RemoveRepo left %d descriptors on the ledger, want 0", used)
	}
}

// TestEventOpenIsChargedEvenWhenTheBudgetIsFull: fsnotify has ALREADY opened
// the descriptor by the time grafel sees the Create, so the charge cannot be
// refused — it can only be recorded. Recording it is what closes the EMFILE
// path: an overdrawn ledger refuses the NEXT subscription instead of pretending
// there is headroom.
func TestEventOpenIsChargedEvenWhenTheBudgetIsFull(t *testing.T) {
	root := makePrunedTree(t)
	w := newBudgetedWatcher(t, prunedTreeKqueueOpens) // exactly full after AddRepo
	if _, err := w.AddRepo(root); err != nil {
		t.Fatalf("premise broken: AddRepo failed: %v", err)
	}
	base, limit := w.fdb.snapshot()
	if base != limit {
		t.Fatalf("premise broken: ledger is %d/%d, want it exactly full", base, limit)
	}

	if err := os.WriteFile(filepath.Join(root, "overflow.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	waitLedger(t, w, base+1, "an unrefusable open past the limit")

	// And the overdraft must be visible to the next subscription.
	other := makePrunedTree(t)
	if _, err := w.AddRepo(other); !isFDBudgetError(err) {
		t.Fatalf("second AddRepo against an overdrawn ledger returned %v, want an FD-budget refusal", err)
	}
}

// --- (D) the cost model must not be per-Watcher while the ledger is global ---

// TestCostModelInjectionRequiresAPrivateLedger closes (D). Config.fdCost was a
// per-Watcher override while fdb defaults to the process-wide sharedFDBudget,
// so two Watchers could charge one ledger with two different arithmetics and
// neither `used` nor `limit` would mean anything. The cost model now lives on
// the ledger, and asking for a custom model without a private ledger is a
// construction error rather than a silent inconsistency.
func TestCostModelInjectionRequiresAPrivateLedger(t *testing.T) {
	_, err := NewWatcherConfig(Config{
		Debounce: time.Hour,
		fdCost:   kqueueCostModel,
		// FDBudget deliberately omitted: that selects the shared ledger.
	}, func(string, bool) {}, nil)
	if err == nil {
		t.Fatal("a custom cost model on the process-wide ledger was accepted")
	}
}

// TestLedgerCarriesItsOwnCostModel: the arithmetic a charge is computed with
// must come from the ledger being charged, so every consumer of one ledger
// agrees. This is the structural half of (D) — the error above is only the
// guard rail.
func TestLedgerCarriesItsOwnCostModel(t *testing.T) {
	w := newBudgetedWatcher(t, 10000)
	if got := w.fdb.model(); got != kqueueCostModel {
		t.Fatalf("watcher ledger model = %+v, want the injected kqueue model", got)
	}
	if got := sharedFDBudget().model(); got != defaultCostModel() {
		t.Fatalf("shared ledger model = %+v, want the platform default %+v", got, defaultCostModel())
	}
	// The injected model must actually reach the ledger, not merely be
	// accepted and replaced by the platform default. Asserted with the model
	// this platform does NOT default to, so the check cannot pass because the
	// two happen to coincide here.
	other := kqueueCostModel
	if defaultCostModel() == other {
		other = inotifyCostModel
	}
	if got := newFDBudgetCost(100, other).model(); got != other {
		t.Fatalf("ledger built with %+v reports model %+v", other, got)
	}
}

// TestEventReleaseIsDeductedFromTheOwningRepo: releasing an event-time charge
// from the global ledger is only half of it. The per-repo tally that RemoveRepo
// hands back must come down too, or unregistering a churny repo returns
// descriptors it no longer holds — freeing budget that a DIFFERENT repo is
// still using, which is how the ledger ends up under-stating the process.
//
// Two repos are needed to see it: with one repo the over-release is hidden by
// the ledger's clamp at zero.
func TestEventReleaseIsDeductedFromTheOwningRepo(t *testing.T) {
	w, root, _ := subscribedWatcher(t)

	quiet := makePrunedTree(t)
	if _, err := w.AddRepo(quiet); err != nil {
		t.Fatalf("premise broken: second AddRepo failed: %v", err)
	}
	twoRepos, _ := w.fdb.snapshot()
	if twoRepos != 2*prunedTreeKqueueOpens {
		t.Fatalf("premise broken: two trees charged %d, want %d", twoRepos, 2*prunedTreeKqueueOpens)
	}

	// Churn only the first repo: charge, then release, the same descriptors.
	//
	// n is kept under defaultChurnThreshold/2 for the same reason
	// TestChurnReturnsTheLedgerToItsSubscriptionBaseline keeps it there
	// (quarantine.go:68): 2n reindex-arming events in one directory would trip
	// the quarantine tracker, which persists its decision by creating
	// <repo>/.grafel (quarantine.go:652) — a real new entry of a watched
	// directory, for which fsnotify opens a real descriptor and the ledger
	// correctly grows by one PER REPO. That charge is honest; it is simply not
	// predictable from this test's arithmetic. Measured at 8768e5c42 the cycle
	// produces 24 events against a threshold of 40, so the margin is 1.7x and
	// not the 3.3x the /2 suggests. The premise assertion at the end says so
	// out loud if it ever stops holding (#6287, #6308).
	const n = 12
	paths := make([]string, 0, n)
	for i := 0; i < n; i++ {
		p := filepath.Join(root, "src", "r"+string(rune('a'+i))+".go")
		if err := os.WriteFile(p, []byte("package p\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		paths = append(paths, p)
	}
	waitLedger(t, w, twoRepos+n, "n files created under the first repo")
	for _, p := range paths {
		if err := os.Remove(p); err != nil {
			t.Fatalf("remove: %v", err)
		}
	}
	waitLedger(t, w, twoRepos, "the same files deleted again")

	// Unregistering the churny repo must return exactly its own subscription,
	// leaving the untouched repo's descriptors on the ledger.
	w.RemoveRepo(root)
	used, _ := w.fdb.snapshot()
	if used != prunedTreeKqueueOpens {
		t.Fatalf("after removing the churny repo the ledger reads %d, want %d "+
			"(the other repo's subscription) — the per-repo tally still held the released churn",
			used, prunedTreeKqueueOpens)
	}

	// Premise, checked last so a failure above is not blamed on it, and the
	// same guard the two sibling tests with this shape already carry: nothing
	// in this test may have written into the repos it is measuring. If the
	// churn tripped the quarantine tracker, <repo>/.grafel exists, fsnotify
	// opened a descriptor for it, and every reading above includes a charge
	// this test's arithmetic does not predict (#6287, #6308). This changes no
	// assertion — it names the confounder instead of leaving it to be
	// rediscovered from an unexplained ledger value.
	for _, r := range []string{root, quiet} {
		if _, err := os.Stat(filepath.Join(r, ".grafel")); err == nil {
			t.Fatalf("premise broken: %s/.grafel exists — the churn tripped the quarantine tracker, "+
				"which persisted state into the repo under measurement, and the ledger readings "+
				"above include a charge this test cannot predict", r)
		}
	}
}

// TestRacyDirectoryFillIsNotDoubleCharged pins the ordering chargeDir uses: the
// directory listing is taken BEFORE w.fs.Add, so grafel's charge set is a
// subset of what fsnotify's watchDirectoryFiles then opens. A directory is
// created and immediately filled — the `git checkout` shape — so the files land
// while the Create event for the directory is still in flight.
//
// fsnotify opens exactly 1 + n descriptors for this: the directory, via
// sendCreateIfNew, and one per file, via the watchDirectoryFiles that grafel's
// Add triggers (backend_kqueue.go:430) or via a later sendCreateIfNew for any
// file that arrives after it. Listing AFTER the Add charges the second group
// twice — once by grafel's own walk, once by the Create it also receives.
func TestRacyDirectoryFillIsNotDoubleCharged(t *testing.T) {
	w, root, base := subscribedWatcher(t)

	dir := filepath.Join(root, "pkg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	const n = 12
	for i := 0; i < n; i++ {
		p := filepath.Join(dir, "z"+string(rune('a'+i))+".go")
		if err := os.WriteFile(p, []byte("package p\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	waitLedger(t, w, base+1+n, "a directory created and immediately filled with 12 files")
}

// ---------------------------------------------------------------------------
// The per-watch (inotify) platform. Three of the charge sites this change adds
// — prune, chargeEventOpen, releaseEventClose — are gated on
// fdCostModel.perEntry() rather than on a build tag, and the comment on
// perEntry (fdbudget.go) promises they therefore "vanish" on a backend that
// spends a watch per directory and nothing per entry. Nothing asserted that.
//
// It cannot be asserted by running on Linux from here, but it does not need to
// be: Config.fdCost injects the model, and perEntry() is what the production
// code branches on. Mutating perEntry() to return perDir is invisible under
// kqueueCostModel{1,1} and changes every one of those three sites under
// inotifyCostModel{1,0}.
// ---------------------------------------------------------------------------

// newInotifyWatcher is newBudgetedWatcher with the per-watch cost model.
func newInotifyWatcher(t *testing.T, budget int) *Watcher {
	t.Helper()
	w, err := NewWatcherConfig(Config{
		Debounce:          time.Hour,
		BulkThreshold:     10000,
		HeartbeatInterval: time.Hour,
		FDBudget:          budget,
		fdCost:            inotifyCostModel,
	}, func(string, bool) {}, nil)
	if err != nil {
		t.Fatalf("NewWatcherConfig: %v", err)
	}
	t.Cleanup(w.Stop)
	return w
}

// TestPerWatchPlatformChargesDirsOnly: on a per-watch backend a subscription
// costs one per subscribed DIRECTORY and nothing else. In particular the
// pruned-directory charge of (C) must not appear: nothing opened a descriptor
// for node_modules there, because nothing opens descriptors per entry at all.
func TestPerWatchPlatformChargesDirsOnly(t *testing.T) {
	root := makePrunedTree(t)
	w := newInotifyWatcher(t, 10000)

	added, err := w.AddRepo(root)
	if err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if added != 2 {
		t.Fatalf("subscribed %d dirs, want 2 (root + src)", added)
	}
	used, _ := w.fdb.snapshot()
	if used != 2 {
		t.Fatalf("per-watch subscription charged %d, want 2 — one per subscribed directory, "+
			"nothing per file and nothing for the pruned directory", used)
	}
}

// TestPerWatchPlatformDoesNotChargeEventOpens: (A) and (B) are kqueue
// artefacts. A file appearing in, or vanishing from, a watched directory costs
// a per-watch backend nothing, so neither the charge nor the release may fire.
func TestPerWatchPlatformDoesNotChargeEventOpens(t *testing.T) {
	root := makePrunedTree(t)
	w := newInotifyWatcher(t, 10000)
	if _, err := w.AddRepo(root); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	base, _ := w.fdb.snapshot()
	if base != 2 {
		t.Fatalf("premise broken: subscription charged %d, want 2", base)
	}

	p := filepath.Join(root, "src", "added.go")
	if err := os.WriteFile(p, []byte("package p\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	waitLedger(t, w, base, "a file created on a per-watch backend")

	if err := os.Remove(p); err != nil {
		t.Fatalf("remove: %v", err)
	}
	waitLedger(t, w, base, "the same file deleted on a per-watch backend")

	// A new directory, though, IS a new watch and must still be charged — the
	// gate must switch off the per-entry terms, not the per-directory one.
	dir := filepath.Join(root, "pkg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	waitLedger(t, w, base+1, "a directory created on a per-watch backend")
}

// ---------------------------------------------------------------------------
// The walk and the loop goroutine overlap.
//
// AddRepo releases w.mu before calling subscribeRepo, so the tree walk runs
// concurrently with event delivery. From the moment the walk publishes its
// first directory into dirToRepo, a Create underneath it is charged by
// chargeEventOpen and attributed to that repo — while the walk is still
// accumulating its own total. Whatever the walk does with fdReserved at the end
// must therefore preserve those charges.
// ---------------------------------------------------------------------------

// assertLedgerMatchesPerRepo checks the invariant that makes RemoveRepo and
// Stop correct: every descriptor on the global ledger is attributed to exactly
// one repo. A charge that reaches `used` but not fdReserved can never be handed
// back, which is failure mode (B) by another route.
//
// BOTH halves are read under ONE acquisition of w.mu, and that is load-bearing
// rather than tidiness (#6308). The global ledger and the per-repo tally are
// one fact recorded twice, and every site that moves them — chargeEventOpen,
// releaseEventClose, RemoveRepo, the refusal unwind — moves both inside this
// lock precisely so no observer can see them disagree. Reading `used` outside
// the lock and `fdReserved` inside it samples two different instants: any
// charge that lands in between is counted by the second read and not the
// first, and the helper reports a mismatch for a ledger that was never
// inconsistent. The gap is nanoseconds on an idle machine and milliseconds on
// a loaded one — long enough for thousands of charges — which is why this
// failed a few percent of the time on CI and not at all locally.
//
// Measured at 8768e5c42 on the shape TestEventChargesDuringTheWalkSurvives...
// drives, sampling continuously while events were still being delivered: the
// split read disagreed 5,968 times against 6,511 events delivered (worst
// divergence: one descriptor), while this single-acquisition read disagreed 0
// times in ~300 million samples across twelve runs. The assertion itself is
// unchanged — still exact equality, still fatal — only the reading is taken at
// one instant instead of two.
//
// w.mu then fdb.mu is the ordering chargeEventOpen already uses, so nesting
// them here introduces no new lock order.
func assertLedgerMatchesPerRepo(t *testing.T, w *Watcher, what string) {
	t.Helper()
	w.mu.Lock()
	used, _ := w.fdb.snapshot()
	sum := 0
	for _, n := range w.fdReserved {
		sum += n
	}
	w.mu.Unlock()
	if sum != used {
		t.Fatalf("%s: ledger holds %d descriptors but only %d are attributed to a repo — "+
			"the unattributed %d can never be released", what, used, sum, used-sum)
	}
}

// makeWideTree builds a tree with nDirs subdirectories, each holding one file,
// so the walk takes long enough for events to be delivered while it runs.
func makeWideTree(t *testing.T, nDirs int) string {
	t.Helper()
	root := t.TempDir()
	for i := 0; i < nDirs; i++ {
		d := filepath.Join(root, fmt.Sprintf("d%04d", i))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(d, "f.go"), []byte("package p\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	return root
}

// TestEventChargesDuringTheWalkSurviveTheWalksBookkeeping is the clobber. The
// walk used to finish with `fdReserved[abs] = reserved`, an assignment, which
// discarded every event-time charge accumulated since the first directory was
// published — leaving those descriptors on the global ledger with no repo to
// release them.
func TestEventChargesDuringTheWalkSurviveTheWalksBookkeeping(t *testing.T) {
	root := makeWideTree(t, 400)
	w := newBudgetedWatcher(t, 1_000_000)

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := w.AddRepo(root); err != nil {
			t.Errorf("AddRepo: %v", err)
		}
	}()

	// Churn the repo root while the walk is in flight. The root is the first
	// directory the walk publishes, so these land as charged Create events
	// during the walk on any run where the timing overlaps at all.
	for i := 0; ; i++ {
		select {
		case <-done:
			goto walked
		default:
		}
		p := filepath.Join(root, fmt.Sprintf("live%04d.go", i))
		if err := os.WriteFile(p, []byte("package p\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
walked:
	<-done

	// Give any in-flight events time to be charged, then check the invariant.
	time.Sleep(300 * time.Millisecond)
	assertLedgerMatchesPerRepo(t, w, "after a walk that overlapped live events")

	// And the strong end-to-end consequence: unregistering must empty the
	// ledger. Anything the walk dropped from the per-repo tally is stranded.
	w.RemoveRepo(root)
	if used, _ := w.fdb.snapshot(); used != 0 {
		t.Fatalf("RemoveRepo left %d descriptors stranded on the ledger, want 0", used)
	}
}

// TestRefusalReturnsEventChargesToo: the refusal branch unwinds by calling
// fs.Remove on every directory it subscribed, and remove(name, true) also drops
// every internal watch inside those directories (backend_kqueue.go:325-333) —
// including the ones opened for files that arrived as events during the walk.
// So the refusal must hand back the event charges as well as its own, or a
// REFUSED subscription poisons the budget it was protecting.
func TestRefusalReturnsEventChargesToo(t *testing.T) {
	root := makeWideTree(t, 400)
	// Enough to get well into the walk — so directories are published and
	// events are charged — but nowhere near enough to finish it.
	w := newBudgetedWatcher(t, 300)

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := w.AddRepo(root); !isFDBudgetError(err) {
			t.Errorf("AddRepo returned %v, want an FD-budget refusal", err)
		}
	}()
	for i := 0; ; i++ {
		select {
		case <-done:
			goto walked
		default:
		}
		p := filepath.Join(root, fmt.Sprintf("live%04d.go", i))
		if err := os.WriteFile(p, []byte("package p\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
walked:
	<-done

	time.Sleep(300 * time.Millisecond)
	if used, _ := w.fdb.snapshot(); used != 0 {
		t.Fatalf("a refused subscription left %d descriptors on the ledger, want 0", used)
	}
	assertLedgerMatchesPerRepo(t, w, "after a refusal that overlapped live events")
}

// TestInterleavedDirectoryFillsAcrossReposAreNotDoubleCharged covers the
// LIFETIME of the pre-charge set, which TestRacyDirectoryFillIsNotDoubleCharged
// only covers for a single directory in isolation.
//
// The loop goroutine is FIFO across every repo, so with two repos both gaining
// directories the Create for one repo's new directory is routinely queued ahead
// of the entries still pending from the other's. If subscribeDirRecursive
// assigned its set over the watcher's instead of merging into it, those pending
// paths would lose their marker and their Creates would be charged a second
// time — the over-count the mechanism exists to prevent, on an ordinary
// two-repo `git checkout`.
//
// The second half deletes and recreates every file: a marker consumed by a
// Remove must not go on suppressing the charge for a path that later comes
// back, or the ledger drifts down instead.
//
// WHAT THIS TEST DOES NOT GUARANTEE (#6304). It does not prove that every file
// it created was seen, or that the ledger reached any particular figure while
// the churn was in flight. fsnotify's backends abandon a directory listing at
// the first entry they cannot stat or open, and readEvents discards that error
// (backend_kqueue.go:506), so an arbitrary tail of one burst's Creates is never
// reported and never charged. That loss is a real product defect, deferred as
// #6304 and measured at up to ~89% of Creates in a hot directory; it is not
// this test's subject and this test cannot be made to fail for it without
// failing for the platform instead. It used to try: the mid-flight assertions
// named an exact figure that counted every file WRITTEN rather than every event
// DELIVERED, so a drop reddened the gate (636/660 and 606/660 with `held 0 of
// 30 polls` — never arriving, not arriving late) for a reason that had nothing
// to do with double-charging.
//
// What it does guarantee is the property it was built for, stated so that a
// missing event cannot break it: EVERY DESCRIPTOR IS CHARGED ONCE AND RELEASED
// ONCE. The teardown deletes every file AND every directory this test created,
// so whatever fsnotify did open it has now closed, and the ledger must come
// back to EXACTLY the subscription baseline. That equality is untouched by
// drops — an entry that was never opened is neither charged nor released, so it
// cancels — and it is broken in the right direction by each defect that matters:
//
//   - a path charged twice (the fdPreCharged suppression ignored, #6268) is
//     released once, so the teardown lands ABOVE base;
//   - a charge for something fsnotify never opened (#6287's .grafel, which the
//     teardown does not delete because the test does not know it is there)
//     lands ABOVE base;
//   - a charge suppressed by a marker that outlived its descriptor is released
//     anyway on the teardown, which lands BELOW base.
//
// TWO further things it does not guarantee, both of which make the ledger read
// ABOVE the truth and neither of which is distinguishable from a double charge:
//
//  1. On a backend that loses INDIVIDUAL events rather than whole paths, a lost
//     Remove strands a charge forever. This is not hypothetical: CI run
//     31750096959 read 35 against a base of 10 on Windows — 25 stranded — with
//     the rest of the package green there and everything green on Ubuntu. The
//     absolute-value assertions are therefore scoped to backends that pair
//     their opens with their closes; see backendPairsEveryOpenWithAClose. The
//     assertions that run everywhere are the ones exact in DELTAS.
//  2. Even on those backends, subscribeDirRecursive pre-charges every entry its
//     own os.ReadDir sees, while fsnotify's watchDirectoryFiles may abandon its
//     listing of that same directory partway. Entries in the difference are
//     charged by grafel, never opened by fsnotify, and so never produce a
//     Remove: they leak upward exactly as a double charge does. Exposure here
//     is narrow — only entries already present when a churned directory is
//     subscribed, and these directories are created EMPTY — and modelled at +1,
//     but it is a real hole in the teardown's exactness rather than a bound
//     this test enforces.
//
// The over-RELEASE (#6293) is reached twice over. The teardown deletes 50
// watched directories, and — contrary to the "kqueue closes and reports once"
// reading in fdbudget_6293_test.go — the duplicate close report is NOT only a
// Windows fact: instrumenting the suppression branch at 4833026a4 counted 48
// duplicate reports across those 50 removals on macOS, so with the suppression
// gone the teardown lands at 0 against a base of 10. That is a strong kill but
// a measured one, and the count is a property of the host. So the same
// invariant is ALSO pinned synthetically at the end — two Remove reports for
// one watched directory, one release — against a ledger the teardown has just
// settled to a known value. That half holds on any platform and does not
// depend on how the backend batches.
//
// The mid-flight readings are kept but made one-sided: drain, then assert the
// ledger is at most what fsnotify could have opened. Under-running that ceiling
// is #6304; exceeding it is a double charge, and on a host that drops nothing
// the ceiling is still exact — measured at 4833026a4 the fill and the refill
// both land on it to the descriptor.
//
// The quarantine tracker is detached for the duration (#6287). Unlike
// TestChurnReturnsTheLedgerToItsSubscriptionBaseline, this test CANNOT stay
// under defaultChurnThreshold: 12 files created, deleted and recreated is 40+
// reindex-arming events in a single directory by construction, which is the
// churn threshold. Tripping it makes the tracker persist its decision by
// creating <repo>/.grafel (quarantine.go:652) — a real new entry of a watched
// directory, for which fsnotify opens a real descriptor and the ledger
// correctly grows by one PER REPO. That charge is honest; it is simply not
// predictable from this test's arithmetic, because WHEN the 40th event lands
// depends on how the backend batches, and the product is mutating the fixture
// it is being measured against. On Linux the 40th event landed in the recreate
// phase and the final assertion read 662 against a want of 660 — two repos,
// two .grafel directories — while macOS never reached the threshold inside the
// test's ~0.5s of churn and stayed green. The premise assertion at the end
// fails loudly if the tracker ever gets back in.
//
// Disabled at CONSTRUCTION rather than assigned over afterwards: the loop
// goroutine reads w.quarantine on every event, so a later write to it would be
// an unsynchronised one — and a disabled tracker is a configuration production
// actually has (GRAFEL_QUARANTINE_DISABLE), which nil is not.
func TestInterleavedDirectoryFillsAcrossReposAreNotDoubleCharged(t *testing.T) {
	repoA := makePrunedTree(t)
	repoB := makePrunedTree(t)
	// This test's arithmetic is the only one in this file that a real macOS run
	// has been observed to miss: fsnotify's dirChange abandoned two directories'
	// listings mid-burst, so 24 of the 660 descriptors were never opened, never
	// charged and — the part that matters off CI — never watched again. Nothing
	// re-listed a subscribed directory, so the reading stayed 24 short for the
	// whole 10-second deadline (`held 0 of 30 polls`).
	//
	// The assertion is unchanged and still an exact equality: 660, not "at
	// least". What changed is that the product now has a way to get there, at
	// the production sweep cadence — this test sets no interval, exactly like
	// every other test in the file (#6304).
	w := newBudgetedWatcherCfg(t, Config{
		FDBudget:          1_000_000,
		disableQuarantine: true,
	})
	for _, r := range []string{repoA, repoB} {
		if _, err := w.AddRepo(r); err != nil {
			t.Fatalf("premise broken: AddRepo(%s): %v", r, err)
		}
	}
	base, _ := w.fdb.snapshot()

	const (
		rounds       = 25
		filesPerDir  = 12
		perDirectory = 1 + filesPerDir // the directory plus its files
	)
	var dirs []string
	for r := 0; r < rounds; r++ {
		for i, repo := range []string{repoA, repoB} {
			d := filepath.Join(repo, fmt.Sprintf("p%d%02d", i, r))
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			dirs = append(dirs, d)
		}
		// Filled after both directories exist, so the two subscribeDirRecursive
		// calls and both sets of pending entry Creates interleave.
		for i := 0; i < filesPerDir; i++ {
			for _, d := range dirs[len(dirs)-2:] {
				p := filepath.Join(d, fmt.Sprintf("z%02d.go", i))
				if err := os.WriteFile(p, []byte("package p\n"), 0o644); err != nil {
					t.Fatalf("write: %v", err)
				}
			}
		}
	}
	filled := base + len(dirs)*perDirectory
	filledUsed := waitLedgerAtMost(t, w, filled, fmt.Sprintf("%d interleaved directory fills across two repos", len(dirs)))
	assertLedgerMatchesPerRepo(t, w, "after interleaved directory fills")

	// Delete every file, then put it back. Only a marker spent by a Remove
	// lets that path's next Create be charged again.
	removeFiles := func(stage string) {
		for _, d := range dirs {
			for i := 0; i < filesPerDir; i++ {
				if err := os.Remove(filepath.Join(d, fmt.Sprintf("z%02d.go", i))); err != nil {
					t.Fatalf("%s remove: %v", stage, err)
				}
			}
		}
	}
	// From here on every phase follows a burst of REMOVALS, so a backend that
	// loses individual events can strand a charge and read above the truth. The
	// readings are still taken — the drain is what separates the phases, and the
	// DELTAS between them are asserted everywhere — but the ABSOLUTE value is
	// only asserted where it means something.
	atMostWhereMeaningful := func(ceiling int, what string) int {
		t.Helper()
		if backendPairsEveryOpenWithAClose() {
			return waitLedgerAtMost(t, w, ceiling, what)
		}
		used, drained := drainLedger(t, w, nil)
		if !drained {
			t.Fatalf("%s: ledger never stopped moving, last reading %d", what, used)
		}
		return used
	}

	// The bounds that survive a backend losing events, because they compare two
	// readings taken around ONE burst instead of one reading against a count of
	// files. Anything stranded or lost BEFORE the burst sits in both readings and
	// cancels; only what the burst itself did is left.
	//
	// This is what keeps the recreate phase — the marker-suppression family, and
	// the only package-wide guard for it — alive on a backend where the absolute
	// ceilings are switched off. Without it, a charge that is right on first
	// sight and doubled on RE-creation passes the whole package there.
	//
	// One-sided, in the direction each burst can honestly move:
	//   - a removal burst can release at most one descriptor per file removed. A
	//     lost Remove releases fewer, which passes; releasing MORE means a
	//     descriptor came off the ledger that no close paid for.
	//   - a creation burst can charge at most one per file created. A lost Create
	//     charges fewer, which passes; charging MORE is the double charge.
	totalFiles := len(dirs) * filesPerDir

	removeFiles("first")
	// Only the directories can still be charged, and only those whose own
	// Create was delivered.
	afterRemovals := atMostWhereMeaningful(base+len(dirs), "every file deleted again, directories kept")
	if released := filledUsed - afterRemovals; released > totalFiles {
		t.Fatalf("removing %d files released %d descriptors (%d -> %d): the excess came off the "+
			"ledger without a close behind it", totalFiles, released, filledUsed, afterRemovals)
	}

	for _, d := range dirs {
		for i := 0; i < filesPerDir; i++ {
			p := filepath.Join(d, fmt.Sprintf("z%02d.go", i))
			if err := os.WriteFile(p, []byte("package p\n"), 0o644); err != nil {
				t.Fatalf("rewrite: %v", err)
			}
		}
	}
	afterRecreate := atMostWhereMeaningful(filled, "every file recreated in place")
	if charged := afterRecreate - afterRemovals; charged > totalFiles {
		t.Fatalf("recreating %d files charged %d descriptors (%d -> %d): a path that came back "+
			"after a release was charged more than once for it",
			totalFiles, charged, afterRemovals, afterRecreate)
	}

	// Tear the whole churn back down — every file, then every directory — and on
	// a backend that pairs its opens with its closes the ledger must return to
	// the subscription baseline to the descriptor.
	//
	// Exact where it runs, because it counts CLOSES against CHARGES rather than
	// files against expectations: a Create fsnotify never delivered (#6304)
	// produced no descriptor and so produces no Remove, and drops out of both
	// sides. A Create delivered twice, or a charge for a path this test never
	// created (#6287), does not.
	//
	// The removals are DRAINED before the directories go: a directory's removal
	// tears down its watch, and on Windows anything still buffered for it dies
	// there (backend_windows.go:565-570). Draining first means there is nothing
	// buffered to lose. This costs one extra wait and removes a whole class of
	// ordering-dependent residue on every platform.
	removeFiles("teardown")
	atMostWhereMeaningful(base+len(dirs), "every file deleted for the last time")
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := os.Remove(dirs[i]); err != nil {
			t.Fatalf("teardown rmdir: %v", err)
		}
	}
	if backendPairsEveryOpenWithAClose() {
		waitLedger(t, w, base, "every created file and directory removed again")
	} else if used, drained := drainLedger(t, w, nil); !drained {
		t.Fatalf("teardown: ledger never stopped moving, last reading %d", used)
	}

	// Every descriptor still on the ledger is attributed to a repo. Holds on
	// every backend: it compares two records of the SAME events, so an event
	// that was never delivered is missing from both.
	assertLedgerMatchesPerRepo(t, w, "after the teardown")

	// The over-release half of "exactly once", pinned deterministically: ONE
	// closed directory handle reported TWICE must release ONE descriptor
	// (#6293; fdbudget_6293_test.go names the two Windows completion paths the
	// duplicate comes from, and the teardown above shows kqueue produces it as
	// well). Synthesised rather than provoked, because how many duplicates a
	// backend emits for 50 removals is a property of the host, and this
	// invariant is not.
	//
	// Driven against a directory this test never deleted, and the premise check
	// below proves it: if the target were one of the churned directories, a
	// dropped Create (#6304) would leave it out of dirToRepo and the FIRST
	// report would take the file branch, which is not the shape being measured.
	// Done late because it unmaps the directory.
	//
	// Asserted as DELTAS from whatever the ledger happens to read now, never
	// against base. The absolute value is not knowable on a backend that loses
	// events, and it does not need to be: "the second report moved the ledger by
	// zero" is the whole invariant.
	dup := filepath.Join(repoA, "src")
	w.mu.Lock()
	_, watched := w.dirToRepo[dup]
	w.mu.Unlock()
	if !watched {
		t.Fatalf("premise broken: %s is not a watched directory, so two Remove reports for it "+
			"do not stand for one closed directory handle", dup)
	}
	// Premise: a release of perDir has to be observable at all. Both cost models
	// carry perDir 1 today (fdbudget.go:60,64), but a model with perDir 0 would
	// make BOTH assertions below pass against a release path that does nothing.
	perDir := w.fdb.model().perDir
	if perDir <= 0 {
		t.Fatalf("premise broken: the ledger's cost model charges %d per directory, so a "+
			"directory release moves nothing and neither assertion below can fail", perDir)
	}
	// The first report is the PREMISE — a real close, really released — and only
	// the second is the assertion. Split that way round because an over-release
	// earlier in this test drives the ledger to its clamp at zero, and "want -1"
	// is a much worse diagnosis of that than saying so.
	beforeDup, _ := w.fdb.snapshot()
	if beforeDup < perDir {
		t.Fatalf("premise broken: the ledger holds %d before the duplicate-report check, fewer than "+
			"the %d a directory release hands back — there is nothing left to release, which is itself "+
			"what an over-release earlier in this test looks like once the ledger clamps at zero",
			beforeDup, perDir)
	}
	w.handleEvent(fsnotify.Event{Name: dup, Op: fsnotify.Remove})
	closed, _ := w.fdb.snapshot()
	if closed != beforeDup-perDir {
		t.Fatalf("premise broken: one Remove report for a watched directory moved the ledger from "+
			"%d to %d, want %d — the first report has to be a real release for the second to be a duplicate",
			beforeDup, closed, beforeDup-perDir)
	}
	w.handleEvent(fsnotify.Event{Name: dup, Op: fsnotify.Remove})
	if used, _ := w.fdb.snapshot(); used != closed {
		t.Fatalf("a second Remove report for the same closed handle moved the ledger to %d, want %d: "+
			"the duplicate released a descriptor that was never charged", used, closed)
	}

	// Exact on every backend, for the same reason: unregistering every repo must
	// empty the ledger completely. RemoveRepo hands back the per-repo tally
	// however that tally was arrived at, so a lost event cannot make this fail —
	// while a descriptor on the global ledger that no repo owns can never be
	// handed back at all, which is failure mode (B).
	for _, r := range []string{repoA, repoB} {
		w.RemoveRepo(r)
	}
	if used, _ := w.fdb.snapshot(); used != 0 {
		t.Fatalf("unregistering both repos left %d descriptors on the ledger, want 0", used)
	}

	// Premise, checked last so a failure above is not blamed on it: nothing in
	// this test may have written into the repos it is measuring. The same guard
	// as TestChurnReturnsTheLedgerToItsSubscriptionBaseline, and the reason the
	// tracker is detached above (#6287).
	for _, r := range []string{repoA, repoB} {
		if _, err := os.Stat(filepath.Join(r, ".grafel")); err == nil {
			t.Fatalf("premise broken: %s/.grafel exists — something persisted state into the repo "+
				"under measurement, and the ledger reading now includes a charge this test cannot predict", r)
		}
	}
}
