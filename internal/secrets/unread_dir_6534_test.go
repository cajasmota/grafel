package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

// TestScanPathReportsCompleteOnOrdinaryTrees is the permissiveness guard for
// #6534's new non-clean outcome, and it runs on every GOOS.
//
// The mutant it kills is the one the unix test cannot see: making Complete()
// return false unconditionally, or counting every walk-error callback rather
// than only the fs.ErrPermission ones. A scan that reports "incomplete" on an
// ordinary repo trains its readers to ignore the field, which puts the tool
// straight back where #6534 found it.
func TestScanPathReportsCompleteOnOrdinaryTrees(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"real.go":  "package p\n",
		"notes.md": "hello\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := ScanPath(root, 0)
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}
	if res.UnreadCount() != 0 {
		t.Errorf("an ordinary tree reported unread directories: %+v", res.Unread)
	}
	if !res.Complete() {
		t.Error("Complete() = false for an ordinary tree; the non-clean outcome " +
			"is worthless if it fires when nothing is wrong")
	}
}
