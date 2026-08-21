package yaml

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/safeio"
)

// The ErrWouldBlock arm of reportHelmSkip is pinned at the reporter, not
// through the sibling-Chart.yaml read, and that limit is stated rather than
// papered over.
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
// chart file. NOT PINNED: that a would-block read reaches the reporter at all
// — that wiring is one `if err != nil { reportHelmSkip(...) }`, shared with
// the ErrNotRegular case and driven end-to-end by TestHelmChartSkipIsReported
// in helm_fifo_6416_test.go.

// TestHelmSkipReportsWouldBlock kills the mutant that drops the ErrWouldBlock
// half of reportHelmSkip's gate. Under that mutant a Chart.yaml lost to
// semaphore exhaustion silently deletes every subchart OVERRIDES edge for that
// chart.
func TestHelmSkipReportsWouldBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Chart.yaml")

	for name, err := range map[string]error{
		"bare":    safeio.ErrWouldBlock, // exactly what openWithDeadline returns
		"wrapped": fmt.Errorf("read %s: %w", path, safeio.ErrWouldBlock),
	} {
		t.Run(name, func(t *testing.T) {
			var buf strings.Builder
			restore := setHelmSkipOutput(&buf)
			defer restore()

			reportHelmSkip(path, err)

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

// TestHelmSkipIgnoresOrdinaryErrors pins the other side of the gate: the loop
// probes two chart names per values.yaml, so a missing Chart.yaml is the
// common case and must stay unannounced.
func TestHelmSkipIgnoresOrdinaryErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Chart.yaml")

	for name, err := range map[string]error{
		"not-exist":  fmt.Errorf("open %s: %w", path, os.ErrNotExist),
		"permission": &os.PathError{Op: "open", Path: path, Err: os.ErrPermission},
		"unrelated":  errors.New("some other failure"),
	} {
		t.Run(name, func(t *testing.T) {
			var buf strings.Builder
			restore := setHelmSkipOutput(&buf)
			defer restore()

			reportHelmSkip(path, err)

			if got := buf.String(); got != "" {
				t.Errorf("ordinary error %v was announced as a skip: %q", err, got)
			}
		})
	}
}
