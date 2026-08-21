// Tests for #6505's second decision: which scanFile errors may keep the
// findings collected before the failure.
//
// No build tag and no syscall — this is pure predicate coverage, so Windows CI
// exercises it too, alongside the end-to-end TestScanPathReportsOverlongLineAsSkip.
package secrets

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"testing"

	"github.com/cajasmota/grafel/internal/safeio"
)

// TestKeepPartialFindingsIsExclusiveToErrTooLong pins the PERMISSIVE direction
// of the predicate, which no end-to-end test can see.
//
// Widening it to `return true` is invisible through ScanPath: the two safeio
// sentinels come from a failed OPEN, so scanFile returned a nil slice and
// appending it is a no-op. The full package suite stays green under that mutant
// — verified — which is exactly why the decision is asserted here rather than
// left to the walk's comment.
//
// The mid-read I/O arm is the one where the widening would actually change an
// answer: a truncated or erroring read can leave ff non-empty while the byte
// stream, and therefore every line number in it, is in doubt. No portable
// fixture provokes that through os/filepath, so it is asserted at the level the
// decision is made.
func TestKeepPartialFindingsIsExclusiveToErrTooLong(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"bufio.ErrTooLong", bufio.ErrTooLong, true},
		{"wrapped ErrTooLong", fmt.Errorf("scan %s: %w", "creds.go", bufio.ErrTooLong), true},

		// Failed opens: ff is empty, so keeping it is a no-op end-to-end.
		// Asserted anyway — a `return true` mutant must die somewhere, and the
		// invariant being protected is "only a partial READ keeps findings",
		// not "only cases where it happens to matter today".
		{"safeio.ErrNotRegular", safeio.ErrNotRegular, false},
		{"safeio.ErrWouldBlock", safeio.ErrWouldBlock, false},
		{"wrapped ErrNotRegular", fmt.Errorf("open: %w", safeio.ErrNotRegular), false},

		// Mid-read failures: ff CAN be non-empty and the line numbers are
		// untrustworthy, so these must drop it.
		{"io.ErrUnexpectedEOF", io.ErrUnexpectedEOF, false},
		{"fs.ErrPermission", fs.ErrPermission, false},
		{"os.ErrClosed", os.ErrClosed, false},
		{"generic I/O error", errors.New("input/output error"), false},

		// bufio's other sentinel: a negative advance is a scanner bug, not a
		// clean partial read.
		{"bufio.ErrAdvanceTooFar", bufio.ErrAdvanceTooFar, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := keepPartialFindings(tc.err); got != tc.want {
				t.Errorf("keepPartialFindings(%v) = %v, want %v: only a PARTIAL "+
					"READ leaves the collected findings and their line numbers "+
					"trustworthy", tc.err, got, tc.want)
			}
		})
	}
}

// TestKeepPartialFindingsRejectsNil guards the degenerate input. The walk only
// calls it inside `if err != nil`, but a predicate that answered true for nil
// would silently invert its own contract if that guard ever moved.
func TestKeepPartialFindingsRejectsNil(t *testing.T) {
	if keepPartialFindings(nil) {
		t.Error("keepPartialFindings(nil) = true; nil is not a partial read")
	}
}
