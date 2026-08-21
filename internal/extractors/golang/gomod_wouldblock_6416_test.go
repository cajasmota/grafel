package golang

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/safeio"
)

// The ErrWouldBlock arm of reportGoModSkip is pinned at the reporter, not
// through readGoMod, and that limit is stated rather than papered over.
//
// safeio.ErrWouldBlock has exactly two producers, both inside
// openWithDeadline: the 64-slot semaphore acquire timing out, and an open(2)
// worker still parked past DefaultTimeout. Neither is reachable from `go test`
// on a healthy filesystem — nonBlockingOpen passes O_NONBLOCK, so the one
// hanging object a test can create (syscall.Mkfifo) opens IMMEDIATELY and is
// then refused by fstat as ErrNotRegular; the genuine producers are hung
// network/FUSE mounts and root-owned device nodes.
//
// PINNED: given an ErrWouldBlock, the reporter announces it and names the
// go.mod. NOT PINNED: that a would-block read reaches the reporter at all —
// that wiring is readGoMod's single `if err != nil { reportGoModSkip(...) }`,
// shared with the ErrNotRegular case and driven end-to-end by
// TestGoModSkipIsReported in gomod_fifo_6416_test.go.

// TestGoModSkipReportsWouldBlock kills the mutant that drops the
// ErrWouldBlock half of reportGoModSkip's gate. Under that mutant a go.mod
// lost to semaphore exhaustion takes every in-tree Go import edge in the repo
// with it, silently.
func TestGoModSkipReportsWouldBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "go.mod")

	for name, err := range map[string]error{
		"bare":    safeio.ErrWouldBlock, // exactly what openWithDeadline returns
		"wrapped": fmt.Errorf("read %s: %w", path, safeio.ErrWouldBlock),
	} {
		t.Run(name, func(t *testing.T) {
			var buf strings.Builder
			restore := setGoModSkipOutput(&buf)
			defer restore()

			reportGoModSkip(path, err)

			got := buf.String()
			if got == "" {
				t.Fatalf("ErrWouldBlock skip of %s was reported NOWHERE", path)
			}
			if !strings.Contains(got, path) {
				t.Errorf("skip report does not name the path %q; got %q", path, got)
			}
			if !strings.Contains(got, "would block") {
				t.Errorf("skip report does not say WHY (would block); got %q", got)
			}
			if !strings.Contains(got, "#6416") {
				t.Errorf("skip report does not cite the issue; got %q", got)
			}
		})
	}
}

// TestGoModSkipIgnoresOrdinaryErrors pins the other side of the gate: a tree
// of Go files with no go.mod above them is ordinary and must stay unannounced,
// or the report stops being a signal.
func TestGoModSkipIgnoresOrdinaryErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "go.mod")

	for name, err := range map[string]error{
		"not-exist":  fmt.Errorf("open %s: %w", path, os.ErrNotExist),
		"permission": &os.PathError{Op: "open", Path: path, Err: os.ErrPermission},
		"unrelated":  errors.New("some other failure"),
	} {
		t.Run(name, func(t *testing.T) {
			var buf strings.Builder
			restore := setGoModSkipOutput(&buf)
			defer restore()

			reportGoModSkip(path, err)

			if got := buf.String(); got != "" {
				t.Errorf("ordinary error %v was announced as a skip: %q", err, got)
			}
		})
	}
}
