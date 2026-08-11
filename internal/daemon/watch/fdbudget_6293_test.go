package watch

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fsnotify/fsnotify"
)

// ---------------------------------------------------------------------------
// #6293 — one closed handle must be released ONCE, however many times the
// backend reports it.
//
// TestRemoveOfAWatchedDirectoryReleasesAndUnmaps failed on Windows only, and in
// the UNDER-count direction: deleting a watched subdirectory took the ledger
// from 4 to 2 where 3 is the truth. The cause is not the arithmetic, it is the
// assumption that a removal reaches grafel once.
//
// fsnotify v1.10.1's Windows backend (backend_windows.go) reports the removal
// of a watched directory TWICE, from two different watches:
//
//  1. the PARENT's ReadDirectoryChangesW read completes with
//     FILE_ACTION_REMOVED for the child's name (readEvents, :600ff), which
//     newEvent (:212-227) maps to Remove;
//  2. the directory's OWN pending read then fails with ERROR_ACCESS_DENIED —
//     "watched directory was probably removed" (readEvents :565-569, and again
//     in startRead :489-492) — and the backend sends a second event for the
//     same path carrying sysFSDELETESELF (0x400, present because AddWith asks
//     for sysFSALLEVENTS at :139). newEvent maps DELETE_SELF to Remove as well.
//
// Both events name the same absolute path and both say Remove; exactly one
// handle was closed. releaseEventClose's discriminator is the dirToRepo
// lookup, and the first event has already deleted that entry, so the second is
// taken for a FILE entry of the parent and releases perEntry() a second time.
// kqueue closes and reports once, which is why macOS never saw it.
//
// Driven with synthesised events, not with os.Remove: the double delivery is a
// Windows fact, so a filesystem-driven test cannot reproduce it anywhere else,
// and this failure has to be pinnable from a machine that cannot run Windows.
// The cost model is the injected kqueue one (newBudgetedWatcher), so the
// arithmetic under test is the per-entry arithmetic on every platform — under a
// per-watch model perEntry() is 0 and the second release is invisible rather
// than absent.
// ---------------------------------------------------------------------------

// windowsDoubleRemove is what fsnotify's Windows backend delivers for one
// deleted watched directory: the parent's FILE_ACTION_REMOVED and the
// directory's own DELETE_SELF, same path, both Remove.
func windowsDoubleRemove(t *testing.T, w *Watcher, dir string) {
	t.Helper()
	w.handleEvent(fsnotify.Event{Name: dir, Op: fsnotify.Remove})
	w.handleEvent(fsnotify.Event{Name: dir, Op: fsnotify.Remove})
}

// TestDoubleRemoveOfAWatchedDirectoryReleasesOnlyOneDescriptor is the defect.
// The second report closes nothing, so it must cost the ledger nothing.
func TestDoubleRemoveOfAWatchedDirectoryReleasesOnlyOneDescriptor(t *testing.T) {
	w, root, base := subscribedWatcher(t)
	dir := filepath.Join(root, "src")

	w.mu.Lock()
	_, watched := w.dirToRepo[dir]
	w.mu.Unlock()
	if !watched {
		t.Fatal("premise broken: src is not a watched directory")
	}

	windowsDoubleRemove(t, w, dir)

	want := base - w.fdb.model().perDir
	used, _ := w.fdb.snapshot()
	if used != want {
		t.Fatalf("ledger %d after two Remove reports of one closed directory handle, want %d: "+
			"the duplicate report released a descriptor that was never charged", used, want)
	}

	// The invariant is one release per CLOSE, not per report, so it cannot
	// depend on the count of reports. Two is all the Windows backend can send
	// for one handle — after the DELETE_SELF, deleteWatch zeroes watch.mask, so
	// startRead's own ERROR_ACCESS_DENIED branch (:489) sends nothing — but a
	// suppression spent by the first duplicate would satisfy the assertion
	// above while still releasing on any later one.
	w.handleEvent(fsnotify.Event{Name: dir, Op: fsnotify.Remove})
	if used, _ = w.fdb.snapshot(); used != want {
		t.Fatalf("ledger %d after a further Remove report for the same closed handle, want %d: "+
			"the duplicate suppression was spent rather than held", used, want)
	}
}

// TestDoubleRemoveStillUnmapsTheDirectory guards the other half of
// releaseEventClose: suppressing the duplicate must not cost the unmapping,
// which is what stops events being routed to a path fsnotify no longer
// watches.
func TestDoubleRemoveStillUnmapsTheDirectory(t *testing.T) {
	w, root, _ := subscribedWatcher(t)
	dir := filepath.Join(root, "src")

	windowsDoubleRemove(t, w, dir)

	w.mu.Lock()
	_, stillWatched := w.dirToRepo[dir]
	w.mu.Unlock()
	if stillWatched {
		t.Fatal("removed directory is still in dirToRepo")
	}
}

// TestRenameThenDeleteSelfOfAWatchedDirectoryReleasesOnce is the same double
// delivery for a directory moved away rather than deleted: the parent reports
// FILE_ACTION_RENAMED_OLD_NAME (Rename) and the directory's own read still
// fails with ERROR_ACCESS_DENIED (Remove). The two events differ in Op, so a
// fix keyed on the operation rather than on the path would leave half the
// drift.
func TestRenameThenDeleteSelfOfAWatchedDirectoryReleasesOnce(t *testing.T) {
	w, root, base := subscribedWatcher(t)
	dir := filepath.Join(root, "src")

	w.handleEvent(fsnotify.Event{Name: dir, Op: fsnotify.Rename})
	w.handleEvent(fsnotify.Event{Name: dir, Op: fsnotify.Remove})

	used, _ := w.fdb.snapshot()
	if want := base - w.fdb.model().perDir; used != want {
		t.Fatalf("ledger %d after Rename+DELETE_SELF for one closed directory handle, want %d",
			used, want)
	}
}

// TestARecreatedDirectoryIsReleasableAgain is the counterweight: suppressing
// the duplicate must not be permanent. A directory deleted and created again is
// a NEW open, and its next removal must release again. A fix that remembers
// "already released" forever would leak the recreated directory's descriptor on
// every churn cycle — the over-count this ledger exists to avoid, arrived at
// from the other side.
func TestARecreatedDirectoryIsReleasableAgain(t *testing.T) {
	w, root, base := subscribedWatcher(t)
	dir := filepath.Join(root, "src")

	windowsDoubleRemove(t, w, dir)
	afterFirst, _ := w.fdb.snapshot()

	// Drive the real Create path: chargeEventOpen pays for the new directory's
	// own descriptor and subscribeDirRecursive re-publishes it into dirToRepo.
	// Nothing on disk changes — src is still there, only the ledger was told it
	// went away — so no genuine event can arrive alongside and be counted too.
	w.handleEvent(fsnotify.Event{Name: dir, Op: fsnotify.Create})
	w.mu.Lock()
	_, watched := w.dirToRepo[dir]
	w.mu.Unlock()
	if !watched {
		t.Fatal("premise broken: the recreated directory was not re-subscribed")
	}
	afterResubscribe, _ := w.fdb.snapshot()
	if afterResubscribe <= afterFirst {
		t.Fatalf("premise broken: re-subscribing charged nothing (%d -> %d)", afterFirst, afterResubscribe)
	}

	w.handleEvent(fsnotify.Event{Name: dir, Op: fsnotify.Remove})
	used, _ := w.fdb.snapshot()
	if want := afterResubscribe - w.fdb.model().perDir; used != want {
		t.Fatalf("ledger %d after removing the RECREATED directory, want %d: "+
			"the duplicate-suppression outlived the descriptor it stood for", used, want)
	}
	if used < base-w.fdb.model().perDir {
		t.Fatalf("ledger %d has drifted below the one-directory-gone baseline %d",
			used, base-w.fdb.model().perDir)
	}
}

// TestAFileReplacingARemovedDirectoryIsStillReleasable is the same
// counterweight for the path that never goes back through dirToRepo: the
// deleted directory's name comes back as a FILE. Its Create charges perEntry(),
// so its Remove must release perEntry() — a duplicate-suppression keyed only on
// re-subscription would swallow that release and understate the ledger for
// good.
//
// Driven through the filesystem and waitLedger, the idiom of
// fdbudget_6268_test.go, because the point is the interaction between the
// suppression and events grafel really receives. The one synthesised event is
// the Windows duplicate, which no other platform delivers; on Windows it is
// simply a third report of a handle already closed, and must be as free as the
// second.
func TestAFileReplacingARemovedDirectoryIsStillReleasable(t *testing.T) {
	w, root, base := subscribedWatcher(t)
	dir := filepath.Join(root, "src")

	if err := os.Remove(filepath.Join(dir, "b.go")); err != nil {
		t.Fatalf("remove file: %v", err)
	}
	waitLedger(t, w, base-1, "the only file in a watched subdirectory deleted")
	if err := os.Remove(dir); err != nil {
		t.Fatalf("remove dir: %v", err)
	}
	waitLedger(t, w, base-2, "a watched subdirectory deleted")

	// The Windows DELETE_SELF duplicate, injected on every platform.
	w.handleEvent(fsnotify.Event{Name: dir, Op: fsnotify.Remove})
	waitLedger(t, w, base-2, "a duplicate removal report for a directory already released")

	// The name comes back as a regular file: a real, fresh descriptor.
	if err := os.WriteFile(dir, []byte("package p\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	waitLedger(t, w, base-1, "a file created with the removed directory's name")

	if err := os.Remove(dir); err != nil {
		t.Fatalf("remove replacement file: %v", err)
	}
	waitLedger(t, w, base-2, "the replacement file deleted")
}

// ---------------------------------------------------------------------------
// The other direction: a marker that outlives the descriptor it stands for
// swallows a REAL release, which is the same under-count arriving by another
// road — and on macOS, where the budget is actually enforced, it would be a
// permanent over-count instead of a Windows-only contract violation. The two
// tests below are the two ways a path is charged WITHOUT a Create event ever
// reaching chargeEventOpen, which is the whole population a per-path,
// event-driven clear cannot see.
// ---------------------------------------------------------------------------

// TestAReAddedRepoCanStillReleaseANameThatWasAWatchedDirectory: a store rescan
// (RemoveRepo + AddRepo) re-charges every entry from subscribeRepo's LISTING.
// No Create event is involved, so a marker keyed to that path must already be
// gone by the time the re-added file is removed.
func TestAReAddedRepoCanStillReleaseANameThatWasAWatchedDirectory(t *testing.T) {
	w, root, base := subscribedWatcher(t)
	dir := filepath.Join(root, "src")

	if err := os.Remove(filepath.Join(dir, "b.go")); err != nil {
		t.Fatalf("remove file: %v", err)
	}
	waitLedger(t, w, base-1, "the only file in a watched subdirectory deleted")
	if err := os.Remove(dir); err != nil {
		t.Fatalf("remove dir: %v", err)
	}
	waitLedger(t, w, base-2, "a watched subdirectory deleted")
	w.handleEvent(fsnotify.Event{Name: dir, Op: fsnotify.Remove}) // the Windows duplicate

	w.RemoveRepo(root)
	waitLedger(t, w, 0, "the repo unsubscribed")

	// The name comes back as a FILE while nothing is watched, so its descriptor
	// is opened by the re-subscription's listing and reported by nothing.
	if err := os.WriteFile(dir, []byte("package p\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := w.AddRepo(root); err != nil {
		t.Fatalf("re-AddRepo: %v", err)
	}
	// root, plus its three entries a.go, node_modules and the new src file.
	const reAdded = 4
	waitLedger(t, w, reAdded, "the repo re-added with a file where the subdirectory was")

	if err := os.Remove(dir); err != nil {
		t.Fatalf("remove replacement file: %v", err)
	}
	waitLedger(t, w, reAdded-1, "the re-added file deleted")
}

// TestARecreatedParentRelistingClearsTheMarker is the same hole without any
// RemoveRepo: a watched directory is deleted and comes back holding a FILE with
// a deleted SUBdirectory's name. subscribeDirRecursive's listing charges that
// file, and fsnotify's own listing absorbs it silently — markSeen stops
// sendCreateIfNew ever reporting it (backend_kqueue.go:612, :657) — so the
// charge happens with no event at all.
//
// The parent is assembled outside the watched tree and renamed in, so it
// appears with its entry already inside rather than racing the listing.
func TestARecreatedParentRelistingClearsTheMarker(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg", "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for rel, body := range map[string]string{
		"a.go":         "package p\n",
		"pkg/sub/y.go": "package p\n",
	} {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	w := newBudgetedWatcher(t, 10000)
	if _, err := w.AddRepo(root); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	base, _ := w.fdb.snapshot()
	// dirs root, pkg, sub; entries a.go and y.go.
	if base != 5 {
		t.Fatalf("premise broken: subscription charged %d, want 5", base)
	}

	if err := os.RemoveAll(filepath.Join(root, "pkg", "sub")); err != nil {
		t.Fatalf("rm sub: %v", err)
	}
	waitLedger(t, w, base-2, "a watched subdirectory and its file deleted")
	if err := os.RemoveAll(filepath.Join(root, "pkg")); err != nil {
		t.Fatalf("rm pkg: %v", err)
	}
	waitLedger(t, w, base-3, "the parent of that subdirectory deleted")

	staging := filepath.Join(t.TempDir(), "pkg")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatalf("mkdir staging: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staging, "sub"), []byte("package p\n"), 0o644); err != nil {
		t.Fatalf("write staging entry: %v", err)
	}
	if err := os.Rename(staging, filepath.Join(root, "pkg")); err != nil {
		t.Fatalf("rename in: %v", err)
	}
	// The directory's own descriptor (chargeEventOpen) and the file inside it
	// (the listing).
	waitLedger(t, w, base-1, "the parent recreated holding a file with the subdirectory's name")

	if err := os.Remove(filepath.Join(root, "pkg", "sub")); err != nil {
		t.Fatalf("rm the replacement file: %v", err)
	}
	waitLedger(t, w, base-2, "the replacement file deleted")
}

// TestOnAPerWatchModelAMarkerDoesNotOutliveTheDescriptor is the only test in
// the package driving the model Windows and Linux actually run
// (inotifyCostModel), and it exists because chargeEventOpen returns early when
// perEntry() is 0: a clear placed after that return is unreachable on exactly
// the platform this whole issue is about, and every marker there is write-only
// until the cap resets it. Asserted on the state rather than on the ledger,
// because with perEntry() == 0 no ledger reading can distinguish the two.
func TestOnAPerWatchModelAMarkerDoesNotOutliveTheDescriptor(t *testing.T) {
	root := makePrunedTree(t)
	w := newInotifyWatcher(t, 10000)
	if _, err := w.AddRepo(root); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	base, _ := w.fdb.snapshot()
	dir := filepath.Join(root, "src")

	windowsDoubleRemove(t, w, dir)
	if used, _ := w.fdb.snapshot(); used != base-w.fdb.model().perDir {
		t.Fatalf("ledger %d after two Remove reports, want %d", used, base-w.fdb.model().perDir)
	}

	// src is still on disk, so this is the Create of a path being watched
	// again — the moment the marker stands for nothing.
	w.handleEvent(fsnotify.Event{Name: dir, Op: fsnotify.Create})
	w.mu.Lock()
	n := len(w.fdReleasedDirs)
	w.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d released-dir marker(s) survived the path being charged again: "+
			"on a per-watch model they are write-only state, cleared by nothing until the cap", n)
	}
}
