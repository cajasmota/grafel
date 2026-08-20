//go:build unix

package vbnet

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/types"
)

// TestFifoSiblingDoesNotBlockExtraction is a LIVENESS test for #6327 S7a.
//
// siblingPath resolves the anchor from a directory listing and typeDecls then
// os.ReadFile's it. A FIFO named `Widget.vb` is a perfectly good directory
// entry, and opening one blocks in open(2) until a writer appears — forever, if
// none ever does. That deadlocks the indexing worker that picked up
// `Widget.Designer.vb`, and it bypasses the walker's own vetting because
// partial.go reads a path the walker never handed it.
//
// The round-1 argument for dropping the entry-type filter — "a non-regular file
// simply fails the read and declares nothing" — is true of a directory and
// FALSE of a FIFO, which does not fail. Hence the filter.
//
// The extraction runs on its own goroutine against a deadline so a regression
// FAILS the suite instead of hanging it; a hung goroutine is leaked
// deliberately, since there is no way to interrupt a blocking open.
func TestFifoSiblingDoesNotBlockExtraction(t *testing.T) {
	for _, tc := range []struct{ name, kind string }{
		{"a bare fifo", "fifo"},
		{"a symlink to a fifo", "symlink"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			designer := "Partial Class Widget\n    Private Sub InitializeComponent()\n    End Sub\nEnd Class\n"
			if err := os.WriteFile(filepath.Join(root, "Widget.Designer.vb"), []byte(designer), 0o644); err != nil {
				t.Fatal(err)
			}
			fifo := filepath.Join(root, "Widget.vb")
			if tc.kind == "symlink" {
				fifo = filepath.Join(root, "real_fifo")
			}
			if err := syscall.Mkfifo(fifo, 0o644); err != nil {
				t.Skipf("cannot create a fifo here: %v", err)
			}
			if tc.kind == "symlink" {
				if err := os.Symlink(fifo, filepath.Join(root, "Widget.vb")); err != nil {
					t.Skipf("symlinks unavailable here: %v", err)
				}
			}

			done := make(chan []types.EntityRecord, 1)
			go func() { done <- extractVBNet(designer, "Widget.Designer.vb", root) }()

			select {
			case recs := <-done:
				for _, r := range recs {
					if r.Kind == "SCOPE.Component" && r.Name == "Widget" && r.SourceFile != "Widget.Designer.vb" {
						t.Errorf("re-anchored onto the fifo %q", r.SourceFile)
					}
				}
			case <-time.After(5 * time.Second):
				t.Fatal("extraction blocked on opening a fifo named Widget.vb; the anchor must be a readable regular file")
			}
		})
	}
}
