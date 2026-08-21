package javascript

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/safeio"
)

// The ErrWouldBlock arm of reportAliasSkip is pinned here rather than through
// AliasMapFor, and the reason is worth stating plainly instead of hiding
// behind a helper that looks like an end-to-end test.
//
// WHAT THIS FILE DOES NOT DO. It does not drive a real read that returns
// safeio.ErrWouldBlock. That error has exactly two producers, both inside
// openWithDeadline: the semaphore acquire timing out, and an open(2) worker
// still parked past DefaultTimeout. Neither can be provoked from this package
// on a healthy filesystem:
//
//   - openSlots is an unexported 64-deep channel, so exhausting it would need
//     64 concurrently-abandoned opens, and a slot is only abandoned by an
//     open(2) that hangs. On unix, nonBlockingOpen passes O_NONBLOCK, which is
//     precisely the flag that makes a FIFO — the one hanging object a test can
//     create with syscall.Mkfifo — open IMMEDIATELY. A FIFO is then refused by
//     fstat as ErrNotRegular, never as ErrWouldBlock.
//   - The remaining real producers are hung network/FUSE mounts and device
//     nodes needing root. Both are out of reach of `go test`, and simulating
//     one by sleeping past the 5s DefaultTimeout would cost 5s+ per case for a
//     path the test could not observe any better than it does below.
//
// So what IS pinned: given an ErrWouldBlock, the reporter announces it and
// names the file. What is NOT pinned: that a would-block read actually reaches
// the reporter. That wiring — readAliasConfig routing every safeio.ReadFile
// error to reportAliasSkip, with no per-error-class filtering in between — is
// one line, is shared with the ErrNotRegular case, and IS driven end-to-end by
// TestAliasSkipIsReported in aliases_fifo_6416_test.go. This file pins the
// gate that line's error then has to survive.

// TestAliasSkipReportsWouldBlock kills the mutant that drops the
// ErrWouldBlock half of reportAliasSkip's gate. Under that mutant a skip
// caused by semaphore exhaustion or an abandoned open is discarded in silence,
// which is #6338's shape and the exact failure mode the round-2 review found
// in internal/secrets.
func TestAliasSkipReportsWouldBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tsconfig.json")

	cases := map[string]error{
		// Exactly what openWithDeadline returns: bare, with no path in it.
		"bare": safeio.ErrWouldBlock,
		// And through a wrapper, since errors.Is is the contract, not ==.
		"wrapped": fmt.Errorf("read %s: %w", path, safeio.ErrWouldBlock),
	}
	for name, err := range cases {
		t.Run(name, func(t *testing.T) {
			var buf strings.Builder
			restore := setAliasSkipOutput(&buf)
			defer restore()

			reportAliasSkip(path, err)

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

// TestAliasSkipIgnoresOrdinaryErrors pins the other side of the same gate: the
// report is a signal, and it stays one only because the overwhelmingly common
// "this repo has no vite.config.ts" is NOT announced. A mutant that widens the
// gate to report everything dies here.
func TestAliasSkipIgnoresOrdinaryErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vite.config.ts")

	for name, err := range map[string]error{
		"not-exist": fmt.Errorf("open %s: %w", path, os.ErrNotExist),
		"permission": &os.PathError{
			Op: "open", Path: path, Err: os.ErrPermission,
		},
		"unrelated": errors.New("some other failure"),
	} {
		t.Run(name, func(t *testing.T) {
			var buf strings.Builder
			restore := setAliasSkipOutput(&buf)
			defer restore()

			reportAliasSkip(path, err)

			if got := buf.String(); got != "" {
				t.Errorf("ordinary error %v was announced as a skip: %q", err, got)
			}
		})
	}
}
