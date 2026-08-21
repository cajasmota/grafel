//go:build unix

package secrets

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestScanPathReturnsFifoSkipToCaller is the #6483 half of the #6416 wiring
// test next door.
//
// TestScanPathReportsFifoSkip asserts the same tree produces a line on
// secretSkipOut. That assertion is structurally unable to fail when the skip
// is dropped from the RETURN VALUE, because it reads the stderr buffer and
// never looks at what ScanPath handed back — which is the whole defect: the
// MCP tool and the dashboard handler are given an unqualified "clean" while a
// file in the tree was never opened.
//
// The killing mutant is: delete the Skipped assignment from ScanPath's
// returned ScanResult while leaving reportSecretScanSkip in place. Every
// pre-#6483 test in this package stays green; only this one dies.
func TestScanPathReturnsFifoSkipToCaller(t *testing.T) {
	root := t.TempDir() // never outside TempDir: a stray FIFO hangs other walks
	if err := os.WriteFile(filepath.Join(root, "real.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(root, "creds.go")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("cannot create a fifo here: %v", err)
	}

	res, err := ScanPath(root, 0)
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}

	if len(res.Skipped) == 0 {
		t.Fatalf("ScanPath reported no skips for %s, but creds.go is a FIFO that "+
			"was never opened; the caller cannot tell 'clean' from 'not read'", root)
	}
	var got *Skip
	for i := range res.Skipped {
		if res.Skipped[i].Path == fifo {
			got = &res.Skipped[i]
		}
	}
	if got == nil {
		t.Fatalf("skip list does not name %q; got %+v", fifo, res.Skipped)
	}
	if got.Reason != SkipNotRegular {
		t.Errorf("reason = %q, want %q", got.Reason, SkipNotRegular)
	}
	if got.Kind != "named-pipe" {
		t.Errorf("kind = %q, want %q", got.Kind, "named-pipe")
	}
	if got.Rel != "creds.go" {
		t.Errorf("rel = %q, want %q", got.Rel, "creds.go")
	}
}

// TestScanPathSkipListStaysEmptyOnOrdinaryTrees is the permissiveness guard
// for the WALK's filters.
//
// The mutant it kills runs the other way from the FIFO test above: widening
// the walk so an ordinary tree records skips — reporting the extension
// denylist, or isTestFile. A skip list that fires on a normal repo is worse
// than none: it is the same signal-destroying noise maxSecretSkipReports
// exists to prevent.
//
// It does NOT observe classifyScanSkip's errors.Is gate. On a tree of
// ordinary files scanFile returns no error at all, so the gate is unreached
// in both directions. TestScanPathDoesNotReportUnreadableFileAsSkip below is
// the case that pins it.
func TestScanPathSkipListStaysEmptyOnOrdinaryTrees(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"real.go":      "package p\n",
		"notes.md":     "hello\n",
		"real_test.go": "package p\n",
		"logo.png":     "\x89PNG\r\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	res, err := ScanPath(root, 0)
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}
	if len(res.Skipped) != 0 {
		t.Errorf("an ordinary tree reported skips: %+v", res.Skipped)
	}
}

// TestScanPathDoesNotReportUnreadableFileAsSkip is the case that makes the
// permissiveness claim above actually true.
//
// TestScanPathSkipListStaysEmptyOnOrdinaryTrees cannot observe the errors.Is
// gate in classifyScanSkip at all: on an ordinary tree scanFile returns no
// error, so the gate is never reached in either direction. A mode-000 file
// DOES reach it, with an error (EACCES) that is none of the sentinels.
//
// Killing mutant: replace `case errors.Is(err, safeio.ErrNotRegular)` with
// `case err != nil`. Every ENOENT, EACCES and ErrTooLong then becomes a
// reported not_regular skip, and a normal repo grows a skip list of files
// that were simply deleted mid-walk — the signal-destroying noise the closed
// vocabulary exists to prevent.
func TestScanPathDoesNotReportUnreadableFileAsSkip(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores mode bits, so 0o000 is still readable")
	}
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
	if len(res.Skipped) != 0 {
		t.Errorf("an unreadable-but-ordinary file was reported as a skip: %+v; "+
			"EACCES/ENOENT are deliberately excluded from the vocabulary", res.Skipped)
	}
}

// TestScanPathNamesFifoKindThroughSymlink pins the os.Stat re-stat inside
// classifyScanSkip.
//
// mode there comes from WalkDir's lstat, which for a symlink reports
// ModeSymlink — safeio.Kind(mode) would name the skip "symlink". safeio.Open
// FOLLOWED the link before refusing it, so the entry type the user needs to
// see is the target's: "named-pipe".
//
// Killing mutant: delete the os.Stat block and keep `kind := safeio.Kind(mode)`.
// Kind flips to "symlink" and this test dies; nothing else in the suite moves.
func TestScanPathNamesFifoKindThroughSymlink(t *testing.T) {
	root := t.TempDir() // never outside TempDir: a stray FIFO hangs other walks
	fifo := filepath.Join(root, "pipe")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("cannot create a fifo here: %v", err)
	}
	link := filepath.Join(root, "creds.go")
	if err := os.Symlink(fifo, link); err != nil {
		t.Skipf("cannot symlink here: %v", err)
	}

	res, err := ScanPath(root, 0)
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}
	var got *Skip
	for i := range res.Skipped {
		if res.Skipped[i].Rel == "creds.go" {
			got = &res.Skipped[i]
		}
	}
	if got == nil {
		t.Fatalf("no skip named creds.go; got %+v", res.Skipped)
	}
	if got.Kind != "named-pipe" {
		t.Errorf("kind = %q, want %q: the walk's lstat mode says \"symlink\", but "+
			"safeio refused the TARGET, and that is what the caller must be told",
			got.Kind, "named-pipe")
	}
}
