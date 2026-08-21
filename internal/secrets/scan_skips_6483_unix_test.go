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

// TestScanPathSkipListStaysEmptyOnOrdinaryTrees is the permissiveness guard.
//
// The mutant it kills runs the other way: widening the classifier so an
// ordinary walk records skips (e.g. reporting the extension denylist or
// isTestFile, or dropping the errors.Is gate so every ENOENT lands in the
// list). A skip list that fires on a normal repo is worse than none — it is
// the same signal-destroying noise maxSecretSkipReports exists to prevent.
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
