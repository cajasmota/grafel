//go:build unix

package golang

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// gomodFIFODeadline bounds each subtest. A correctly-gated read returns in
// microseconds — safeio's stat gate never opens a FIFO — so anything near this
// value is the hang, not a slow machine. It is deliberately well under the
// package test timeout so a regression FAILS with attribution rather than
// wedging the suite until the watchdog kills the binary.
const gomodFIFODeadline = 10 * time.Second

// mkfifoInTemp creates a named pipe inside dir, which must be a t.TempDir().
// A FIFO outside a temp dir would outlive the test and hang any other process
// that walks over it, so this helper takes the DIRECTORY rather than a full
// path and cannot be pointed elsewhere by accident.
func mkfifoInTemp(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Fatalf("mkfifo %s: %v", p, err)
	}
	// t.TempDir's RemoveAll unlinks a FIFO without opening it, so cleanup is
	// already correct; this is belt-and-braces against a future refactor that
	// stops using t.TempDir.
	t.Cleanup(func() { _ = os.Remove(p) })
	return p
}

// mustReturn runs fn and fails the test if it has not returned within
// gomodFIFODeadline. Every call into the readers below needs this, not just
// the liveness tests: under the pre-fix code a bare call parks in open(2)
// forever, which would wedge the whole test binary until the -timeout watchdog
// killed it with no attribution AND leave the t.TempDir cleanup unrun — which
// leaks a FIFO onto a shared machine.
func mustReturn(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(gomodFIFODeadline):
		t.Fatalf("HANG: %s did not return within %s", what, gomodFIFODeadline)
	}
}

// TestGoModFIFODoesNotHang is the #6416 regression for the two go.mod readers.
//
// These are the highest-severity remaining sites in the issue because they are
// UNCONDITIONAL: goModuleRoot and goModuleReplaces both join the literal name
// "go.mod" onto the repo root, and the Go extractor calls them for every Go
// file it sees. `mkfifo go.mod` at the root of any Go repository therefore
// wedged the extraction worker forever — os.Open waits in open(2) for a writer
// that never comes — with no walker gate in between, because neither path came
// from the walker.
//
// This is a HANG, so the honest way to pin it is a deadline, not an assertion
// about a return value: the pre-fix code never produced a value to assert on.
func TestGoModFIFODoesNotHang(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(string)
	}{
		{"goModuleRoot", func(root string) { _ = goModuleRoot(root) }},
		{"goModuleReplaces", func(root string) { _ = goModuleReplaces(root) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			mkfifoInTemp(t, dir, "go.mod")

			mustReturn(t, tc.name+" with a FIFO named go.mod", func() { tc.call(dir) })
		})
	}
}

// TestGoModFIFOReturnsEmpty pins the values, not just the liveness: a refused
// go.mod must degrade to the same "absent go.mod" behaviour the package doc
// already promises, not to a partial or fabricated module name.
func TestGoModFIFOReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	mkfifoInTemp(t, dir, "go.mod")

	var (
		name string
		reps []goReplace
	)
	mustReturn(t, "go.mod FIFO readers", func() {
		name = goModuleRoot(dir)
		reps = goModuleReplaces(dir)
	})
	if name != "" {
		t.Errorf("goModuleRoot = %q, want \"\" for a refused go.mod", name)
	}
	if len(reps) != 0 {
		t.Errorf("goModuleReplaces = %+v, want none for a refused go.mod", reps)
	}
}

// TestGoModSkipIsReported pins the half of the fix that is not the hang.
//
// The #6416 re-review's standing finding is that safeio.ErrNotRegular carries a
// precise reason — path plus entry kind — and that consumers kept throwing it
// away with a bare `return ""`. A FIFO named go.mod silently producing no
// module root means every in-tree import in the repo resolves to an external
// ext: node instead of a file entity; that is a large, visible degradation with
// no stated cause, which is exactly #6338's shape. So the skip must leave a
// record, and the record is asserted here rather than assumed.
func TestGoModSkipIsReported(t *testing.T) {
	dir := t.TempDir()
	p := mkfifoInTemp(t, dir, "go.mod")

	var buf strings.Builder
	restore := setGoModSkipOutput(&buf)
	defer restore()

	mustReturn(t, "goModuleRoot", func() { _ = goModuleRoot(dir) })

	got := buf.String()
	if !strings.Contains(got, p) {
		t.Fatalf("skip report does not name the path %q; got %q", p, got)
	}
	if !strings.Contains(got, "named-pipe") {
		t.Fatalf("skip report does not say WHY (named-pipe); got %q", got)
	}
	if !strings.Contains(got, "#6416") {
		t.Fatalf("skip report does not cite the issue; got %q", got)
	}
}

// TestGoModMissingIsNotReported guards the other half of the convention: a
// plain ENOENT is the ordinary case for a directory with no go.mod, and
// announcing it would bury the FIFO signal under noise.
func TestGoModMissingIsNotReported(t *testing.T) {
	dir := t.TempDir()

	var buf strings.Builder
	restore := setGoModSkipOutput(&buf)
	defer restore()

	mustReturn(t, "goModuleRoot/goModuleReplaces", func() {
		_ = goModuleRoot(dir)
		_ = goModuleReplaces(dir)
	})

	if got := buf.String(); got != "" {
		t.Fatalf("an absent go.mod was reported as a skip: %q", got)
	}
}
