//go:build unix

package testsupport

// fifo_unix.go — the shared half of a #6416/#6478 blocking-open regression
// test.
//
// Every one of these tests has the same shape and the same two hazards, and
// #6478 routes 27 call sites across fourteen packages. Fourteen copies of the
// same helper is how the ORIGINAL defect happened — internal/safeio's own doc
// comment says it exists "rather than a fourth hand-rolled copy of the same
// dance" — so the helper lives here once.
//
// Hazard one: a FIFO outside a temp directory outlives the test and hangs any
// other process that walks over it, on a machine that is usually shared.
// MkfifoInTemp therefore takes the ROOT temp directory separately from the
// relative path and cannot be pointed elsewhere by accident.
//
// Hazard two: under the pre-fix code the call under test parks in open(2)
// FOREVER. A bare call would wedge the whole test binary until the -timeout
// watchdog killed it — with no attribution, and with the t.TempDir cleanup
// unrun, which leaks the FIFO. MustReturn runs the call on its own goroutine
// and fails with a named deadline instead.

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// FIFODeadline bounds a call that must not block. A correctly-gated read
// returns in microseconds — safeio's stat gate never opens a FIFO at all — so
// anything near this value is the hang, not a slow machine. It matches
// internal/licenses/licenses_fifo_6416_test.go and is deliberately well under
// the default package test timeout, so a regression FAILS with attribution
// rather than wedging the suite until the watchdog kills the binary.
const FIFODeadline = 10 * time.Second

// MkfifoInTemp creates a named pipe at dir/rel..., where dir must be a
// t.TempDir().
func MkfifoInTemp(t *testing.T, dir string, rel ...string) string {
	t.Helper()
	p := filepath.Join(append([]string{dir}, rel...)...)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
	}
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Fatalf("mkfifo %s: %v", p, err)
	}
	// t.TempDir's RemoveAll unlinks a FIFO without opening it, so cleanup is
	// already correct; this is belt-and-braces against a future refactor that
	// stops using t.TempDir.
	t.Cleanup(func() { _ = os.Remove(p) })
	return p
}

// MustReturn runs fn and fails the test if it has not returned within
// FIFODeadline.
//
// A hang is pinned by a DEADLINE, not by an assertion about a return value:
// the pre-fix code never produced a value to assert on. The goroutine is
// deliberately abandoned rather than waited for — that is safeio's own stated
// guarantee ("the deadline does not rescue the worker, it rescues the
// CALLER"), and blocking on it here would reproduce the hang inside the guard.
func MustReturn(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(FIFODeadline):
		t.Fatalf("HANG: %s did not return within %s", what, FIFODeadline)
	}
}
