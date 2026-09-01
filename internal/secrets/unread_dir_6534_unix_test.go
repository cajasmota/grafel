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
