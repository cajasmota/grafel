//go:build !windows

package watch

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// ---------------------------------------------------------------------------
// #7245 — a FIFO or a socket created in a watched directory must not be charged
// a descriptor, because fsnotify's kqueue backend never opens one for it.
//
// addWatch (backend_kqueue.go:365-368) returns ("", nil) — SUCCESS, with no
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
// Platform note: the claim is a fact about the kqueue backend. On Linux the
// inotify backend takes no per-entry descriptor at all and reports Remove for a
// FIFO through the directory's own watch, so the charge and the release are
// symmetric there and the code must keep charging — see
// backendSkipsPipesAndSockets. Every assertion below is written in terms of the
// SETTLED ledger across a create/remove pair, which holds on both: on kqueue
// because nothing was charged, on inotify because what was charged was
// released. Only the mid-sequence "still at base while the FIFO exists" rows
// are kqueue-only, and they are guarded.
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
// from being vacuous. fsnotify's dirChange (backend_kqueue.go:625-650) lists
// the whole directory and reports every unseen entry in readdir order, so a
// Create delivered for a later-sorting name proves the earlier-sorting FIFO or
// socket was walked past in the same listing (or in an earlier one). Without
// it, "the ledger is still at base" is equally true of a fix that works and of
// an event that has not arrived yet.
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

	if backendSkipsPipesAndSockets {
		// kqueue opened nothing, so nothing may be charged while the FIFO is
		// still on disk. The sentinel above proves the listing that contains
		// the FIFO has been processed, so this row is reachable.
		waitLedger(t, w, base, "a live FIFO in a watched directory")
	}

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

	if backendSkipsPipesAndSockets {
		waitLedger(t, w, base, "a live socket in a watched directory")
	}

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
		if backendSkipsPipesAndSockets {
			waitLedger(t, w, base, "a live FIFO on cycle")
		}
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
	if !backendSkipsPipesAndSockets {
		want += 2 // inotify arithmetic: the entries are counted as they always were
	}
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
