//go:build unix

package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

// TestScanPathCountsUnreadableDirectory is the #6534 defect, stated as a test.
//
// Before #6534 the walk-error callback returned nil for every entry the walk
// itself could not read. One EACCES directory is N unread FILES, and the
// caller got findings=0, skipped=[] — byte-identical to a genuinely clean
// tree. Users act on a clean secrets scan, so "found nothing" and "looked at
// nothing" rendering the same verdict is the worst failure mode this tool
// has.
//
// Killing mutant: restore `return nil` in ScanPath's walk-error arm. Nothing
// else in this package dies.
func TestScanPathCountsUnreadableDirectory(t *testing.T) {
	root, sub := unreadableSubdirTree(t)

	res, err := ScanPath(root, 0)
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}

	// Precondition: the key is genuinely unreachable, so the scan really does
	// come back with nothing found. That is what makes the count load-bearing.
	if len(res.Findings) != 0 {
		t.Fatalf("precondition failed: the key under %s was read; findings=%+v", sub, res.Findings)
	}

	if res.UnreadCount() != 1 {
		t.Fatalf("UnreadCount() = %d, want 1; a directory that cannot be opened "+
			"must be COUNTED, not silently dropped (%+v)", res.UnreadCount(), res.Unread)
	}
	if res.Complete() {
		t.Error("Complete() = true for a tree with an unopenable subtree; " +
			"'found nothing' and 'looked at nothing' must not render identically")
	}
	if got, want := res.Unread[0].Rel, "sub"; got != want {
		t.Errorf("Unread[0].Rel = %q, want %q", got, want)
	}
	if got := res.Unread[0].Path; got != sub {
		t.Errorf("Unread[0].Path = %q, want %q", got, sub)
	}

	// The existing four-reason vocabulary is per-FILE and stays closed: an
	// unreadable directory names no file, so it must not leak into Skipped.
	if len(res.Skipped) != 0 {
		t.Errorf("an unreadable directory leaked into the per-file skip vocabulary: %+v", res.Skipped)
	}
}

// TestScanPathDoesNotCountUnreadableFileAsUnreadDir is the permissiveness
// guard on the file-vs-directory gate.
//
// TestScanPathDoesNotReportUnreadableFileAsSkip already pins that a mode-000
// FILE stays out of Skipped. This pins that #6534 did not smuggle it into
// Unread instead: an unreadable file is one file, already covered by the
// deliberate ENOENT/EACCES exclusion, and counting it would contradict the
// sibling test rather than extend it.
//
// Killing mutant: drop the `d.IsDir()` half of the walk-error gate.
func TestScanPathDoesNotCountUnreadableFileAsUnreadDir(t *testing.T) {
	requireModeBitsHonoured(t)
	root := t.TempDir()
	locked := filepath.Join(root, "locked.go")
	if err := os.WriteFile(locked, []byte("package p\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	if f, err := os.Open(locked); err == nil {
		f.Close()
		t.Skip("this filesystem ignores mode 0o000")
	}

	res, err := ScanPath(root, 0)
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}
	if res.UnreadCount() != 0 {
		t.Errorf("an unreadable FILE was counted as an unread directory: %+v", res.Unread)
	}
	if !res.Complete() {
		t.Error("Complete() = false for a tree whose only oddity is one unreadable file")
	}
}

// TestScanPathCountsUnreadableRoot covers the case the sibling test cannot
// reach: the scan root ITSELF is unopenable.
//
// It was found by probing filepath.WalkDir rather than by reading it. When
// LSTAT(root) fails, WalkDir calls fn ONCE with d == nil and then returns
// whatever fn returned — and this callback returns nil, so WalkDir returns
// nil too. Before #6534's d == nil arm, ScanPath handed a caller an empty
// ScanResult and a nil error for a repo it had not read one byte of: the
// #6534 defect with the root standing in for the subtree, and strictly worse
// because NOTHING was scanned.
//
// The root is caged by making its PARENT non-searchable, which is what makes
// the lstat itself fail; chmod 0o000 on the root directory takes the ReadDir
// path instead and is covered above.
//
// Killing mutant: narrow the gate back to `d != nil && d.IsDir() && ...`.
func TestScanPathCountsUnreadableRoot(t *testing.T) {
	requireModeBitsHonoured(t)
	cage := t.TempDir()
	root := filepath.Join(cage, "repo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "package p\n\nconst k = \"AKIAIOSFODNN7EXAMPLE\"\n"
	if err := os.WriteFile(filepath.Join(root, "creds.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cage, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(cage, 0o755) })
	if _, err := os.Lstat(root); err == nil {
		t.Skip("this filesystem still permits lstat through a mode-000 parent")
	}

	res, err := ScanPath(root, 0)
	if err != nil {
		// A caller that checks err is already safe; the defect is the path
		// where err is nil, which is what WalkDir actually does here.
		t.Skipf("ScanPath surfaced the error directly (%v); the silent-clean "+
			"path this test guards is not reachable on this platform", err)
	}
	if len(res.Findings) != 0 {
		t.Fatalf("precondition failed: the caged repo was read; findings=%+v", res.Findings)
	}
	if res.UnreadCount() != 1 {
		t.Fatalf("UnreadCount() = %d, want 1: ScanPath returned a nil error and an "+
			"empty result for a repo it never opened — a completely unscanned "+
			"repo must not render as clean (%+v)", res.UnreadCount(), res.Unread)
	}
	if res.Complete() {
		t.Error("Complete() = true for a scan that read nothing at all")
	}
	if got := res.Unread[0].Path; got != root {
		t.Errorf("Unread[0].Path = %q, want the root %q", got, root)
	}
}

// TestScanPathUnreadableFileNeverReachesTheWalkErrorArm is the honest record
// of an EQUIVALENT MUTANT, in the shape #6737 established.
//
// Dropping `d.IsDir()` from ScanPath's walk-error gate leaves this package
// green, and that is CORRECT rather than a hole: fs.WalkDirFunc's contract
// enumerates exactly two cases in which fn sees a non-nil err — root Stat
// failure (d nil) and a directory's ReadDir failure (d describes the
// DIRECTORY) — so `d != nil && !d.IsDir()` cannot co-occur with an error.
// The conjunct is defence in depth against a future walk change, NOT a live
// discriminator, and no fixture can kill its removal today.
//
// What this test CAN pin is the premise that makes the argument true: a
// mode-000 file lstats fine and never reaches the walk-error arm at all; it
// fails later, inside scanFile. If that premise ever breaks — a walker that
// stats eagerly, an fs.FS-backed walk — this test goes red and the conjunct
// stops being decorative, which is exactly when the next reader needs to know.
func TestScanPathUnreadableFileNeverReachesTheWalkErrorArm(t *testing.T) {
	requireModeBitsHonoured(t)
	root := t.TempDir()
	locked := filepath.Join(root, "locked.go")
	if err := os.WriteFile(locked, []byte("package p\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	if f, err := os.Open(locked); err == nil {
		f.Close()
		t.Skip("this filesystem ignores mode 0o000")
	}

	var sawErrorEntry []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil && d != nil && !d.IsDir() {
			sawErrorEntry = append(sawErrorEntry, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir: %v", err)
	}
	if len(sawErrorEntry) != 0 {
		t.Errorf("WalkDir called the error arm with a NON-DIRECTORY entry (%v); "+
			"the d.IsDir() conjunct in ScanPath is no longer defence in depth "+
			"but a live discriminator, and dropping it is no longer an "+
			"equivalent mutant — the comment there must be updated", sawErrorEntry)
	}
}

// requireModeBitsHonoured skips when mode bits cannot make anything
// unreadable — running as root, or on a filesystem that ignores them.
func requireModeBitsHonoured(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores mode bits, so 0o000 is still readable")
	}
}

// unreadableSubdirTree builds <root>/sub/creds.go holding a real-looking AWS
// key, then chmods sub to 0o000. Cleanup restores 0o755 so t.TempDir can
// remove the tree.
func unreadableSubdirTree(t *testing.T) (root, sub string) {
	t.Helper()
	requireModeBitsHonoured(t)
	root = t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "real.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub = filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "package p\n\nconst k = \"AKIAIOSFODNN7EXAMPLE\"\n"
	if err := os.WriteFile(filepath.Join(sub, "creds.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sub, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sub, 0o755) })
	if f, err := os.Open(sub); err == nil {
		f.Close()
		t.Skip("this filesystem ignores mode 0o000 on directories")
	}
	return root, sub
}
