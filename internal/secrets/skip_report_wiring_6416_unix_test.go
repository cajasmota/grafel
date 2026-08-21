//go:build unix

package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// TestScanPathReportsFifoSkip is the wiring half: a REAL refused file, through
// the real ScanPath walk, reaching the real reporter.
//
// The unit tests next door call reportSecretScanSkip directly, which cannot
// tell whether anything ever calls it — dropping the reportSecretScanSkip line
// from scanFile leaves every one of them green. This test is what kills that
// mutant, and it is the one that reproduces the user-visible bug: before the
// fix, ScanPath returned "no findings" for this tree and said nothing at all
// about creds.go.
func TestScanPathReportsFifoSkip(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "real.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(root, "creds.go")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("cannot create a fifo here: %v", err)
	}

	var buf strings.Builder
	restore := setSecretSkipOutput(&buf)
	defer restore()

	if _, err := ScanPath(root, 0); err != nil {
		t.Fatalf("ScanPath: %v", err)
	}

	got := buf.String()
	if got == "" {
		t.Fatalf("a FIFO named creds.go was skipped by the scan and reported NOWHERE; "+
			"the scan claimed a clean result for %s", root)
	}
	if !strings.Contains(got, fifo) {
		t.Errorf("skip report does not name the skipped file %q; got %q", fifo, got)
	}
	if !strings.Contains(got, "#6416") {
		t.Errorf("skip report does not cite the issue; got %q", got)
	}
}

// TestScanPathStaysSilentOnOrdinaryTrees is the noise guard on the wiring: an
// ordinary repo must produce no skip output whatsoever, or the warning stops
// being a signal on the very first scan a user runs.
func TestScanPathStaysSilentOnOrdinaryTrees(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "real.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf strings.Builder
	restore := setSecretSkipOutput(&buf)
	defer restore()

	if _, err := ScanPath(root, 0); err != nil {
		t.Fatalf("ScanPath: %v", err)
	}
	if got := buf.String(); got != "" {
		t.Errorf("an ordinary tree produced skip output: %q", got)
	}
}
