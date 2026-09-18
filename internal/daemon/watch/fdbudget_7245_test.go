//go:build !windows

package watch

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/fsnotify/fsnotify"
	"golang.org/x/sys/unix"
)

// ---------------------------------------------------------------------------
// #7245 — a FIFO or a socket created in a watched directory must not be charged
// a descriptor, because fsnotify's kqueue backend never opens one for it.
//
// addWatch (backend_kqueue.go:363-368) returns ("", nil) — SUCCESS, with no
// watch established — for os.ModeSocket or os.ModeNamedPipe. sendCreateIfNew
// (:657-664) has already emitted Create by then, so grafel is told Create and
// charges perEntry for a path that has no descriptor behind it. Nothing ever
// releases that charge: the path is unwatched, so no Remove can ever be
// reported for it. markSeen (:668) is skipped as well — addWatch handed back
// "" and that is what gets marked — so the SAME path reported again charges
// again, which makes the drift unbounded rather than one-shot.
//
// These tests are written so the release side is graded in the same run as the
// skip. A fix that simply stopped charging everything would pass the FIFO and
// socket rows and fail the regular-file rows.
//
// PLATFORM. Two facts are in play and the first version of this file conflated
// them, then the second version conflated them the other way. Both are recorded
// because the conflation is easy to make again:
//
//   - The COST MODEL is arithmetic — how many units a directory and an entry
//     each cost. newBudgetedWatcherCfg forces kqueueCostModel on every platform
//     (fdbudget_test.go:192) so ledger deltas are countable everywhere. It is
//     deliberately NOT the platform's own model, and must not be:
//     inotifyCostModel.perEntry() is 0, so matching the model per platform
//     would collapse every delta in this file to zero and the suite would go
//     vacuously green on Linux.
//
//   - The BACKEND decides which events arrive at all. That does NOT follow from
//     the cost model. In particular a backend can report a Remove for an entry
//     grafel never charged — inotify watches the DIRECTORY and reports entries
//     by name, with no filter on mode anywhere.
//
// The production fix is ungated by GOOS, because every site it touches is
// already behind perEntry() > 0 and inotifyCostModel.perEntry() is 0. The
// second fact is why it is also SYMMETRIC: skipEventOpen records the skipped
// path as already-released, so the Remove half cannot hand back a charge the
// Create half never made. That is why every expectation below is an
// unconditional `base` with no platform branch anywhere in this file.
//
// BEST-EFFORT, not identity — the same standing the pre-existing #6293 marker
// has, and it should not be described more strongly. Three sites clear a marker
// without the entry ever having been charged (forgetReleasedEntriesLocked at
// watcher.go:858 and :2284, reconcile.go:559), and two reset the map wholesale
// (watcher.go:1419, and the cap inside recordReleasedDirLocked). A clear
// between the skipped Create and a Remove would let that Remove release against
// nothing. None of it is reachable in production — it needs perEntry() > 0 on a
// backend that reports the Remove, which is only this suite's forced
// kqueueCostModel on Linux — but "identical on every backend and under every
// cost model" would be a stronger claim than the code delivers.
// TestASkippedEntryReleasesNothing is the row that pins the second half, by
// calling releaseEventClose directly — on kqueue no Remove for a FIFO ever
// arrives on its own, so that is the only way to reach it here.
// ---------------------------------------------------------------------------

// mkfifo7245 creates a FIFO inside a watched directory.
func mkfifo7245(t *testing.T, path string) {
	t.Helper()
	if err := unix.Mkfifo(path, 0o644); err != nil {
		t.Fatalf("mkfifo %s: %v", path, err)
	}
}

// mksock7245 creates a bound unix-domain socket inside a watched directory and
// closes the listener, leaving the socket file behind. It fails the test rather
// than skipping: a bind that cannot be performed is a broken premise, not an
// excused row.
func mksock7245(t *testing.T, path string) {
	t.Helper()
	// sun_path is 104 bytes on darwin. t.TempDir() under /var/folders leaves
	// ample room, but say so out loud rather than reading a bind failure as a
	// property of the watcher.
	if len(path) >= 100 {
		t.Fatalf("premise broken: socket path %d bytes, too long to bind: %s", len(path), path)
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen unix %s: %v", path, err)
	}
	// Close, not Listener.Close-with-unlink: SetUnlinkOnClose(false) keeps the
	// socket file on disk, which is the entry this test is about.
	ul, ok := l.(*net.UnixListener)
	if !ok {
		t.Fatalf("premise broken: net.Listen(\"unix\") returned %T", l)
	}
	ul.SetUnlinkOnClose(false)
	if err := ul.Close(); err != nil {
		t.Fatalf("close unix listener: %v", err)
	}
	if fi, err := os.Lstat(path); err != nil {
		t.Fatalf("premise broken: socket file not on disk after bind: %v", err)
	} else if fi.Mode()&os.ModeSocket == 0 {
		t.Fatalf("premise broken: %s is mode %v, not a socket", path, fi.Mode())
	}
}

// sentinel7245 creates a regular file whose name sorts AFTER the unwatchable
// entry the caller just made, waits for its charge, and removes it again,
// leaving the ledger where it found it.
//
// This is the positive control that stops every absence assertion in this file
// from being vacuous. Both backends give the ordering, by different means:
// kqueue's dirChange (backend_kqueue.go:625-650) lists the whole directory and
// reports every unseen entry in readdir order, so a Create for a later-sorting
// name proves the earlier-sorting FIFO was walked past in the same listing;
// inotify queues one event per operation in the order the operations happened,
// so the FIFO's IN_CREATE precedes the sentinel's. Without this, "the ledger is
// still at base" is equally true of a fix that works and of an event that has
// not arrived yet.
func sentinel7245(t *testing.T, w *Watcher, dir string, base int, what string) {
	t.Helper()
	p := filepath.Join(dir, "zz-sentinel.go")
	if err := os.WriteFile(p, []byte("package p\n"), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}
	waitLedger(t, w, base+1, "the sentinel regular file after "+what)
	if err := os.Remove(p); err != nil {
		t.Fatalf("remove sentinel: %v", err)
	}
	waitLedger(t, w, base, "the sentinel's charge released after "+what)
}

// TestARegularFileIsStillChargedAndReleased is the discrimination control for
// the whole file, stated on its own so a blanket "charge nothing" cannot pass
// as a fix for #7245. It is deliberately NOT folded into the FIFO test: if the
// two shared a body, a failure would not say which half broke.
func TestARegularFileIsStillChargedAndReleased(t *testing.T) {
	w, root, base := subscribedWatcher(t)

	p := filepath.Join(root, "regular.go")
	if err := os.WriteFile(p, []byte("package p\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	waitLedger(t, w, base+1, "a regular file in a watched directory")

	if err := os.Remove(p); err != nil {
		t.Fatalf("remove: %v", err)
	}
	waitLedger(t, w, base, "the regular file's charge after its Remove")
}

// TestAFifoDoesNotStrandAnFDCharge is the #7245 defect for the named-pipe half
// of addWatch's early return. Before the fix the ledger reads base+1 for the
// life of the process; the FIFO's removal reports nothing, because the path was
// never watched.
func TestAFifoDoesNotStrandAnFDCharge(t *testing.T) {
	w, root, base := subscribedWatcher(t)

	p := filepath.Join(root, "aa-pipe")
	mkfifo7245(t, p)
	sentinel7245(t, w, root, base, "the FIFO was created")

	// Nothing is charged while the FIFO is on disk. The sentinel above proves
	// the FIFO's own event has already been processed, so this is an assertion
	// rather than a race the test happens to win.
	waitLedger(t, w, base, "a live FIFO in a watched directory")

	if err := os.Remove(p); err != nil {
		t.Fatalf("remove fifo: %v", err)
	}
	sentinel7245(t, w, root, base, "the FIFO was removed")
	waitLedger(t, w, base, "the ledger after a FIFO create/remove cycle")
}

// TestSocketChargeNotStranded is the other half of the same early
// return. A FIFO-only fixture would leave the socket arm of
// `fi.Mode()&os.ModeSocket` ungraded, and the two are separate mode bits tested
// by separate operands.
func TestSocketChargeNotStranded(t *testing.T) {
	w, root, base := subscribedWatcher(t)

	p := filepath.Join(root, "s")
	mksock7245(t, p)
	sentinel7245(t, w, root, base, "the socket was created")
	waitLedger(t, w, base, "a live socket in a watched directory")

	if err := os.Remove(p); err != nil {
		t.Fatalf("remove socket: %v", err)
	}
	sentinel7245(t, w, root, base, "the socket was removed")
	waitLedger(t, w, base, "the ledger after a socket create/remove cycle")
}

// TestARecreatedFifoDoesNotChargeTwice pins the unbounded half of the defect.
// addWatch returns ("", nil), so sendCreateIfNew calls markSeen("") and the
// FIFO's own path is never marked seen — meaning the SAME path is reported as a
// Create again and again. Before the fix each report added another charge; the
// drift is per-report, not per-path, which is why the issue calls it unbounded.
//
// Two full cycles, and the assertion is the ledger reading at the end: a fix
// that charged once and suppressed the rest would still be a leak of one.
func TestARecreatedFifoDoesNotChargeTwice(t *testing.T) {
	w, root, base := subscribedWatcher(t)

	p := filepath.Join(root, "aa-pipe")
	for i := range 2 {
		mkfifo7245(t, p)
		sentinel7245(t, w, root, base, "FIFO cycle create")
		// Exactly base on the SECOND cycle too. addWatch handed "" back, so
		// markSeen never marked the FIFO's own path and the entry is reported
		// as a Create again — before the fix, charged again.
		waitLedger(t, w, base, "a live FIFO on cycle")
		if err := os.Remove(p); err != nil {
			t.Fatalf("remove fifo (cycle %d): %v", i, err)
		}
		sentinel7245(t, w, root, base, "FIFO cycle remove")
	}
	waitLedger(t, w, base, "the ledger after two FIFO create/remove cycles")
}

// TestPreexistingFifoSock covers the listing arm of the
// same fact. subscribeDirRecursive's chargeDirRecording counts every
// non-directory entry, but fsnotify's watchDirectoryFiles -> internalWatch ->
// addWatch (backend_kqueue.go:594, :365-368) opens nothing for a FIFO either,
// so a FIFO present at subscribe time is charged for a descriptor that does not
// exist — and, markSeen having been given "", it is reported as a Create on the
// directory's next change and charged all over again.
//
// The ledger reading is the whole assertion: prunedTreeKqueueOpens is what the
// tree costs, and a FIFO added to it must not move that number.
func TestPreexistingFifoSock(t *testing.T) {
	root := makePrunedTree(t)
	mkfifo7245(t, filepath.Join(root, "aa-pipe"))
	mksock7245(t, filepath.Join(root, "s"))

	w := newBudgetedWatcher(t, 10000)
	if _, err := w.AddRepo(root); err != nil {
		t.Fatalf("premise broken: AddRepo failed: %v", err)
	}
	used, _ := w.fdb.snapshot()
	want := prunedTreeKqueueOpens
	if used != want {
		t.Fatalf("subscription over a tree holding a FIFO and a socket charged %d, want %d — "+
			"fsnotify opened no descriptor for either (#7245)", used, want)
	}
	// And the reconcile sweep must agree with that tally, or it will "repair"
	// the two entries it thinks are missing and charge them right back. Driven
	// explicitly, twice, because a deficit must be seen on one sweep and
	// confirmed on the next before repairDir acts.
	w.reconcileOnce()
	w.reconcileOnce()
	waitLedger(t, w, want, "the ledger after two reconcile sweeps over a FIFO and a socket")
}

// TestASymlinkToAFifoIsStillCharged is the permissive-direction guard on the
// mode test itself, and it is the reason handleEvent confirms with os.Lstat.
// os.Stat FOLLOWS symlinks and would report ModeNamedPipe for this path;
// fsnotify does not. internalWatch calls addWatch with listDir=true, which
// skips the readlink branch (:371) and Lstats the symlink itself — mode
// ModeSymlink, neither socket nor pipe — so unix.Open(name) runs, follows the
// link and DOES open a descriptor. Charging it is correct.
//
// The release side of a symlink-to-FIFO is NOT asserted, and deliberately:
// removing the symlink does not unlink the FIFO inode the descriptor is
// registered against, so no NOTE_DELETE arrives. That is the pre-existing
// behaviour of every symlinked entry, unchanged by #7245 and out of its scope.
func TestASymlinkToAFifoIsStillCharged(t *testing.T) {
	// The reconcile sweep is DISABLED here, and that is the whole point of the
	// row. `ab-link` is a symlink, so scanDir lists it, reads a deficit and
	// repairDir Add()s and charges it — within about four seconds, whatever
	// handleEvent decided. With the sweep on, this test passes against a
	// handleEvent that skips the symlink, which grades nothing. Off, the only
	// site that can move the ledger is the Create path under test.
	root := makePrunedTree(t)
	w := newBudgetedWatcherCfg(t, Config{FDBudget: 10000, reconcileInterval: -1})
	if _, err := w.AddRepo(root); err != nil {
		t.Fatalf("premise broken: AddRepo failed: %v", err)
	}
	base, _ := w.fdb.snapshot()
	if base != prunedTreeKqueueOpens {
		t.Fatalf("premise broken: subscription charged %d, want %d", base, prunedTreeKqueueOpens)
	}

	target := filepath.Join(root, "aa-pipe")
	mkfifo7245(t, target)
	sentinel7245(t, w, root, base, "the FIFO target was created")

	link := filepath.Join(root, "ab-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	waitLedger(t, w, base+1, "a symlink pointing at a FIFO")

	// This test costs ~10s of wall clock and logs "event loop did not finish
	// draining after Close". Once fsnotify has a descriptor registered on a
	// FIFO — which is exactly what this test arranges, and what makes the row
	// meaningful — Stop waits out its full drain timeout. Unlinking the symlink
	// and the FIFO first does not help; the descriptor is already open. That is
	// a pre-existing property of watching a FIFO, unchanged by #7245, recorded
	// here so the next reader does not take the stall for a regression.
}

// TestASkippedEntryReleasesNothing pins the half of the skip that this
// platform cannot reach on its own, and it is the reason skipEventOpen exists
// rather than the skip being a bare `if`.
//
// Not charging a path and not releasing it are two separate decisions made by
// two separate functions, and only the first follows from addWatch's early
// return. Whether a Remove is REPORTED for the path is a property of the
// backend: kqueue cannot report one, because the path is unwatched, but a
// backend that watches the directory instead of the entry reports the entry's
// removal by name and knows nothing about what grafel charged. releaseEventClose
// would then hand back a descriptor that was never charged — the #6268
// under-count, reached from the direction #7245 opens. That is not a
// hypothetical: it is what an ungated, asymmetric skip does under this suite's
// forced kqueueCostModel on Linux.
//
// Since no such report can arrive here, it is delivered by hand. Calling
// releaseEventClose directly is the planted violation: on the fix it releases
// nothing, and with skipEventOpen's marker removed it takes the ledger BELOW
// base, which is a failure no amount of waiting would produce naturally.
//
// The regular-file half is REDUNDANT, and is kept as documentation of the
// discrimination rather than as a load-bearing row. The justification first
// written here — that without it the test would pass against a
// releaseEventClose that had stopped releasing anything — is false, and is
// the class of claim the header rewrite above exists to remove: with
// releaseEventClose stopped entirely this test fails three lines earlier, at
// sentinel7245, which needs an ordinary release to reach base again. No mutant
// found so far is caught by the control alone.
func TestASkippedEntryReleasesNothing(t *testing.T) {
	w, root, base := subscribedWatcher(t)

	pipe := filepath.Join(root, "aa-pipe")
	mkfifo7245(t, pipe)
	sentinel7245(t, w, root, base, "the FIFO was created")
	waitLedger(t, w, base, "a live FIFO before the hand-delivered Remove")

	// The violation. One Remove report for a path that was never charged.
	w.releaseEventClose(pipe)
	if used, _ := w.fdb.snapshot(); used != base {
		t.Fatalf("a Remove report for a never-charged FIFO moved the ledger to %d, want %d — "+
			"releasing against a charge that was never made is the #6268 under-count (#7245)", used, base)
	}

	// Control: the same call on a path that WAS charged must still release it,
	// or the row above is satisfied by a releaseEventClose that does nothing.
	reg := filepath.Join(root, "regular.go")
	if err := os.WriteFile(reg, []byte("package p\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	waitLedger(t, w, base+1, "a regular file before the hand-delivered Remove")
	w.releaseEventClose(reg)
	if used, _ := w.fdb.snapshot(); used != base {
		t.Fatalf("a Remove report for a charged regular file left the ledger at %d, want %d — "+
			"the skip must not have stopped ordinary releases", used, base)
	}
}

// TestPerWatchFifoMarker is the sibling of
// TestOnAPerWatchModelAMarkerDoesNotOutliveTheDescriptor in fdbudget_6293_test.go,
// and it lives here rather than beside it only because that file has no build
// tag and is compiled on Windows, where unix.Mkfifo does not exist.
//
// #6293 established that a released-dir marker must not outlive the descriptor
// it stands for: on a per-watch model nothing clears one until the cap resets
// it, so a survivor is write-only state. Its test recreates the path as a
// DIRECTORY, which reaches chargeEventOpen, whose pre-return
// forgetReleasedDirLocked is the clear. #7245 introduced a path that does NOT
// reach chargeEventOpen at all — a FIFO or socket, routed to skipEventOpen —
// and so re-opened exactly that hole for the case where the marker's prior
// owner was a watched directory and the path comes back unwatchable.
//
// The assertion is on the marker state, not the ledger, for the reason the
// #6293 test gives: with perEntry() == 0 no ledger reading can tell the two
// apart. The regular-file row is the control — it is the path #6293 already
// covered, so if it ever fails the failure is not about FIFOs.
//
// The name is kept SHORT deliberately, and so are the subtest names: t.TempDir()
// embeds both, and the socket row binds inside it against a 104-byte sun_path
// on darwin. A more descriptive name pushed it to 117 bytes and the row failed
// on its own premise check. mksock7245 reports that as a broken premise rather
// than skipping, so the budget cannot be lost silently.
func TestPerWatchFifoMarker(t *testing.T) {
	for _, tc := range []struct {
		name     string
		recreate func(t *testing.T, path string)
	}{
		{"fifo", func(t *testing.T, path string) { mkfifo7245(t, path) }},
		{"socket", func(t *testing.T, path string) { mksock7245(t, path) }},
		{"regular", func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte("package p\n"), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := makePrunedTree(t)
			w := newInotifyWatcher(t, 10000)
			if _, err := w.AddRepo(root); err != nil {
				t.Fatalf("AddRepo: %v", err)
			}
			dir := filepath.Join(root, "src")

			// Two Remove reports for a watched directory: one release, and a
			// marker recorded so the second releases nothing (#6293).
			windowsDoubleRemove(t, w, dir)
			w.mu.Lock()
			before := len(w.fdReleasedDirs)
			w.mu.Unlock()
			if before == 0 {
				t.Fatal("premise broken: no released-dir marker was recorded, so this test " +
					"cannot observe whether the Create clears one")
			}

			// The path comes back under the same name, as something that is not
			// a directory. This is the moment the marker stands for nothing.
			if err := os.RemoveAll(dir); err != nil {
				t.Fatalf("rm dir: %v", err)
			}
			tc.recreate(t, dir)
			w.handleEvent(fsnotify.Event{Name: dir, Op: fsnotify.Create})

			w.mu.Lock()
			after := len(w.fdReleasedDirs)
			w.mu.Unlock()
			if after != 0 {
				t.Fatalf("markers before=%d after=%d: %d released-dir marker(s) survived the path "+
					"being recreated. On a per-watch model that is write-only state cleared by "+
					"nothing until the cap resets it — the #6293 defect, re-opened for a path "+
					"that never reaches chargeEventOpen (#7245)", before, after, after)
			}
		})
	}
}
