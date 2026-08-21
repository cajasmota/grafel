package licenses

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/safeio"
)

// The ErrWouldBlock arm of reportLicenseSkip is pinned at the reporter, not
// through readLicenseFile, and that limit is stated rather than papered over.
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
// file. NOT PINNED: that a would-block read reaches the reporter at all — that
// wiring is readLicenseFile's single `if err != nil { reportLicenseSkip(...) }`,
// shared with the ErrNotRegular case and driven end-to-end by
// TestLicenseSkipIsReported in licenses_fifo_6416_test.go.

// TestLicenseSkipReportsWouldBlock kills the mutant that drops the
// ErrWouldBlock half of reportLicenseSkip's gate. Under that mutant a LICENSE
// lost to semaphore exhaustion silently downgrades the project license to
// "Unknown", which changes the compatibility verdict for every dependency in
// the repo with nothing on stderr to explain it.
func TestLicenseSkipReportsWouldBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "LICENSE")

	for name, err := range map[string]error{
		"bare":    safeio.ErrWouldBlock, // exactly what openWithDeadline returns
		"wrapped": fmt.Errorf("read %s: %w", path, safeio.ErrWouldBlock),
	} {
		t.Run(name, func(t *testing.T) {
			var buf strings.Builder
			restore := setLicenseSkipOutput(&buf)
			defer restore()

			reportLicenseSkip(path, err)

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

// TestLicenseSkipIgnoresOrdinaryErrors pins the other side of the gate. This
// package probes eight filenames at every repo root and expects most of them
// to be absent, so ENOENT is not merely uninteresting here — announcing it
// would emit several lines for every healthy repo and bury the signal.
func TestLicenseSkipIgnoresOrdinaryErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "LICENSE")

	for name, err := range map[string]error{
		"not-exist":  fmt.Errorf("open %s: %w", path, os.ErrNotExist),
		"permission": &os.PathError{Op: "open", Path: path, Err: os.ErrPermission},
		"unrelated":  errors.New("some other failure"),
	} {
		t.Run(name, func(t *testing.T) {
			var buf strings.Builder
			restore := setLicenseSkipOutput(&buf)
			defer restore()

			reportLicenseSkip(path, err)

			if got := buf.String(); got != "" {
				t.Errorf("ordinary error %v was announced as a skip: %q", err, got)
			}
		})
	}
}
