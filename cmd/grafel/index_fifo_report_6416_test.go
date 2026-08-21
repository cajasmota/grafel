//go:build unix

package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestIndexRunReportsIrregularSkipsOnStderr closes the review's MEDIUM-4 gap.
//
// "The skip is always reported" is a load-bearing claim of the #6416 fix, and
// it was asserted nowhere outside internal/daemon/walk: IrregularSkipReport had
// exactly one non-test caller, so deleting that block from Indexer.Run — or
// quietly gating it behind --print-skipped — was invisible to every test.
//
// It also proves the FULL path end to end rather than the predicate: a FIFO
// planted in a real indexed repo must not wedge Run (which is #6416 itself),
// and must produce a line a user can actually see without opting in.
func TestIndexRunReportsIrregularSkipsOnStderr(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(repo, "hang.go"), 0o644); err != nil {
		t.Skipf("cannot create a fifo here: %v", err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	captured := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		captured <- string(b)
	}()

	idx := newTestIndexer(t, "fiforepo", nil, t.TempDir())
	done := make(chan error, 1)
	go func() {
		_, rerr := idx.Run(context.Background(), repo)
		done <- rerr
	}()

	var runErr error
	select {
	case runErr = <-done:
	case <-time.After(60 * time.Second):
		// Restore before failing so the harness's own output is not lost.
		os.Stderr = orig
		_ = w.Close()
		t.Fatal("Indexer.Run blocked on a fifo named hang.go — this is #6416 on the full path")
	}
	os.Stderr = orig
	_ = w.Close()
	out := <-captured

	if runErr != nil {
		t.Fatalf("Run: %v", runErr)
	}
	if !strings.Contains(out, "non-regular file") || !strings.Contains(out, "hang.go") {
		t.Errorf("the skipped fifo was never reported on stderr; got:\n%s", out)
	}
	if !strings.Contains(out, "#6416") {
		t.Errorf("the report does not point at the issue that explains it; got:\n%s", out)
	}
}
