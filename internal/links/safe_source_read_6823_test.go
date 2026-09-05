//go:build !windows

package links

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/safeio"
)

// safe_source_read_6823_test.go — #6823.
//
// #6416 hardened the ENGINE copy of the substrate resolver against a FIFO
// planted in a scanned tree (internal/engine/http_endpoint_substrate_fold.go
// reads through internal/safeio). The near-identical read in the link pass
// was left on plain os.ReadFile, so a `mkfifo config.js` in a linked repo
// parked a group-link worker in open(2) forever. This file grades the link
// side of that.
//
// WHY EVERY TEST HERE IS WRAPPED IN A DEADLINE. The defect's symptom is a
// hang, not a failure: run against the unhardened code these bodies never
// return, and an un-bounded test would wedge the whole package suite instead
// of reporting a red. runWithin turns "blocked" into a named test failure.
// Every FIFO lives inside t.TempDir() and is removed with it.

// fifoDeadline bounds each blocking-shaped call. The hardened path decides on
// a stat(2) and returns in microseconds, so this is slack, not a measurement:
// its only job is to be shorter than "forever" and longer than any plausible
// scheduling delay on a loaded CI box.
const fifoDeadline = 10 * time.Second

// runWithin runs fn on its own goroutine and fails the test if it has not
// returned within d. A blocked goroutine is deliberately abandoned rather
// than cancelled: there is no portable way to interrupt a thread parked in
// open(2), which is the whole reason safeio's guard is a stat gate and not a
// timeout. The test binary exits at the end of the run regardless.
func runWithin(t *testing.T, d time.Duration, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s did not return within %s: the read blocked on an irregular file (#6416, reached through the link pass)", what, d)
	}
}

// mkfifo creates a FIFO at dir/name. dir MUST be a t.TempDir().
func mkfifo(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Skipf("mkfifo unsupported on this filesystem: %v", err)
	}
	return p
}

// TestReadSourceFile_FIFORefusedWithoutBlocking pins the helper itself: a
// FIFO with an indexed extension is refused as not-a-regular-file, and the
// call returns. Against plain os.ReadFile this call never returns at all.
func TestReadSourceFile_FIFORefusedWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	p := mkfifo(t, dir, "config.js")

	var (
		body []byte
		err  error
	)
	runWithin(t, fifoDeadline, "readSourceFile on a FIFO", func() {
		body, err = readSourceFile(p, maxSourceFileBytes)
	})
	if !errors.Is(err, safeio.ErrNotRegular) {
		t.Fatalf("readSourceFile(FIFO) err = %v, want safeio.ErrNotRegular", err)
	}
	if len(body) != 0 {
		t.Fatalf("readSourceFile(FIFO) returned %d bytes, want none", len(body))
	}
}

// TestReadSourceFile_SymlinkToFIFORefusedWithoutBlocking covers the shape
// #6416 named explicitly: the irregular file is behind a symlink, so a check
// on the link itself would pass it through. The helper must judge the TARGET.
func TestReadSourceFile_SymlinkToFIFORefusedWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	fifo := mkfifo(t, dir, "pipe")
	link := filepath.Join(dir, "config.js")
	if err := os.Symlink(fifo, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	var err error
	runWithin(t, fifoDeadline, "readSourceFile on a symlink to a FIFO", func() {
		_, err = readSourceFile(link, maxSourceFileBytes)
	})
	if !errors.Is(err, safeio.ErrNotRegular) {
		t.Fatalf("readSourceFile(symlink->FIFO) err = %v, want safeio.ErrNotRegular", err)
	}
}

// TestReadSourceFile_RegularFileAndSymlinkToRegularFileStillRead is the
// positive control for the two tests above: a guard that refused everything
// would pass them and delete every binding in the graph. A symlink to a
// regular file must still be read, because the walker mints file entities
// for those.
func TestReadSourceFile_RegularFileAndSymlinkToRegularFileStillRead(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.js")
	if err := os.WriteFile(real, []byte("export const API = \"https://x\";\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.js")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	for _, p := range []string{real, link} {
		got, err := readSourceFile(p, maxSourceFileBytes)
		if err != nil {
			t.Fatalf("readSourceFile(%s) err = %v, want nil", filepath.Base(p), err)
		}
		if !strings.Contains(string(got), "https://x") {
			t.Fatalf("readSourceFile(%s) = %q, want the file's contents", filepath.Base(p), got)
		}
	}
}

// TestReadSourceFile_StopsAtTheByteBound pins that the bound is applied and
// that it truncates rather than erroring — the documented, deliberately
// inherited consequence of matching the engine's 1 MiB cap (#6450). It reads
// the bound from the call, not from the constant, so it grades the plumbing
// on any value.
func TestReadSourceFile_StopsAtTheByteBound(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "big.js")
	if err := os.WriteFile(p, []byte(strings.Repeat("a", 4096)), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readSourceFile(p, 100)
	if err != nil {
		t.Fatalf("readSourceFile err = %v, want nil", err)
	}
	if len(got) != 100 {
		t.Fatalf("readSourceFile with maxBytes=100 returned %d bytes, want 100 (the bound must be applied, and must truncate rather than fail)", len(got))
	}
}

// TestBuildResolver_FIFOInSourceTreeDoesNotBlock grades the CALL SITE the
// issue names — constant_propagation.go's buildResolver — not just the
// helper. The graph declares an entity in `config.js`; on disk `config.js`
// is a FIFO. buildResolver must return, and must still bind the sibling
// regular file, so the FIFO is a per-file skip rather than a lost pass.
func TestBuildResolver_FIFOInSourceTreeDoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	mkfifo(t, dir, "config.js")
	if err := os.WriteFile(filepath.Join(dir, "good.js"),
		[]byte("export const BASE = \"https://api.example.com\";\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	graphs := []repoGraph{{
		Repo:     "frontend",
		FileRoot: dir,
		Entities: []entityNode{
			{ID: "e1", Name: "BASE", Kind: "Variable", SourceFile: "config.js"},
			{ID: "e2", Name: "BASE", Kind: "Variable", SourceFile: "good.js"},
		},
	}}

	var r *Resolver
	runWithin(t, fifoDeadline, "buildResolver over a tree containing a FIFO", func() {
		r, _ = buildResolver(graphs)
	})
	if r == nil {
		t.Fatal("buildResolver returned nil; want a resolver built from the sibling regular file")
	}
	if _, ok := r.bindings["frontend"]["good.js"]; !ok {
		t.Fatalf("buildResolver dropped the sibling regular file: bindings = %v", r.bindings["frontend"])
	}
	if _, ok := r.bindings["frontend"]["config.js"]; ok {
		t.Fatal("buildResolver produced bindings for the FIFO; want it skipped")
	}
}

// TestScanRepo_NonSkippableReadErrorStillPropagates grades the OTHER arm of
// the same error block, the one the code comment calls load-bearing and that
// nothing observed before: everything that is not ErrNotRegular must still
// reach scanRepo's caller. Swallowing every read error is the #6338 shape —
// a pass that read nothing and reported a clean result — and it is a
// one-line edit away from the skip arm.
//
// It uses an unreadable regular file rather than safeio's ErrWouldBlock,
// which needs safeio's terminal state (64 abandoned opens) to produce and
// cannot be provoked in a unit test. The arm under test is the same one:
// "not ErrNotRegular" is a single branch.
func TestScanRepo_NonSkippableReadErrorStillPropagates(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "locked.go")
	if err := os.WriteFile(p, []byte("package p\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(p); err == nil {
		t.Skip("running with rights that ignore file mode (root?); cannot make a read fail this way")
	}

	if _, err := scanRepo(dir, []string{"locked.go"}, ""); err == nil {
		t.Fatal("scanRepo returned nil error on an unreadable file: a read failure that is NOT a non-regular-file skip must propagate, or the pass reports a clean repo it never read (#6338)")
	}
}

// TestBuildResolver_OneMiBBoundIsObservable makes the byte bound's COST a
// measured fact rather than a claim in a comment. A binding declared inside
// the first megabyte of a file is found; the identical binding declared past
// the megabyte mark is not, because safeio truncates at the bound and the
// sniffer never sees it. That is the capability loss #6450 recorded on the
// engine twin, now inherited here deliberately.
//
// It also grades the CALL SITE's bound argument in both directions: raising
// the production bound makes the second half find the binding, and removing
// it (maxBytes <= 0, i.e. unbounded) does the same.
func TestBuildResolver_OneMiBBoundIsObservable(t *testing.T) {
	const decl = "export const BASE = \"https://api.example.com\";\n"
	pad := strings.Repeat("// pad\n", 1024)

	write := func(t *testing.T, dir, name string, before int) {
		t.Helper()
		var b strings.Builder
		for b.Len() < before {
			b.WriteString(pad)
		}
		b.WriteString(decl)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(b.String()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	resolve := func(t *testing.T, dir string) bool {
		t.Helper()
		r, _ := buildResolver([]repoGraph{{
			Repo:     "frontend",
			FileRoot: dir,
			Entities: []entityNode{{ID: "e1", Name: "BASE", Kind: "Variable", SourceFile: "config.js"}},
		}})
		if r == nil {
			return false
		}
		_, ok := r.bindings["frontend"]["config.js"]["BASE"]
		return ok
	}

	small := t.TempDir()
	write(t, small, "config.js", 4096)
	if !resolve(t, small) {
		t.Fatal("binding declared well inside the bound was not found; the fixture, not the bound, is wrong")
	}

	big := t.TempDir()
	write(t, big, "config.js", int(maxSourceFileBytes)+4096)
	if resolve(t, big) {
		t.Fatalf("binding declared past the %d-byte bound WAS found: the production call site is no longer applying maxSourceFileBytes", maxSourceFileBytes)
	}
}

// TestScanRepo_FIFOIsSkippedAndDoesNotAbortTheRepo grades the string pass's
// read. That site differs from the substrate ones in a way that matters: a
// read error there propagates out of scanRepo and zeroes the repo's whole
// extraction set (the #5523 shape). So hardening it is not enough — the
// refusal has to be a per-file skip. This asserts both halves: the call
// returns, AND the sibling regular file's literals survive.
func TestScanRepo_FIFOIsSkippedAndDoesNotAbortTheRepo(t *testing.T) {
	dir := t.TempDir()
	mkfifo(t, dir, "pipe.go")
	if err := os.WriteFile(filepath.Join(dir, "good.go"),
		[]byte("package p\n\nconst Q = \"orders.created.v1\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var (
		exs []Extraction
		err error
	)
	runWithin(t, fifoDeadline, "scanRepo over a tree containing a FIFO", func() {
		exs, err = scanRepo(dir, []string{"pipe.go", "good.go"}, "")
	})
	if err != nil {
		t.Fatalf("scanRepo err = %v; a FIFO must be a per-file skip, not a pass-wide abort", err)
	}
	found := false
	for _, e := range exs {
		if e.File == "good.go" {
			found = true
		}
		if e.File == "pipe.go" {
			t.Fatal("scanRepo produced an extraction from the FIFO")
		}
	}
	if !found {
		t.Fatalf("scanRepo lost the sibling regular file's extractions: %+v", exs)
	}
}

// TestScanRepo_ReadsPastTheFirstBytesOfAFile pins stringScanMaxFileBytes in
// the only direction that can regress silently: downwards. The string pass
// discards any file over 4 MiB by its own check, so a bound at or above that
// is behaviour-preserving and a bound below it starts truncating ordinary
// files and losing literals with no error. A literal parked well past the
// first hundred bytes must still be extracted.
//
// It does NOT pin the bound upwards — raising it changes nothing, because the
// pass's own 4 MiB discard still fires.
func TestScanRepo_ReadsPastTheFirstBytesOfAFile(t *testing.T) {
	dir := t.TempDir()
	body := "package p\n\n" + strings.Repeat("// filler filler filler\n", 64) +
		"const Q = \"orders.created.v1\"\n"
	if len(body) < 1024 {
		t.Fatalf("fixture is only %d bytes; it must be long enough to be truncated by a too-small bound", len(body))
	}
	if err := os.WriteFile(filepath.Join(dir, "late.go"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	exs, err := scanRepo(dir, []string{"late.go"}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range exs {
		if strings.Contains(e.Value, "orders.created.v1") {
			return
		}
	}
	t.Fatalf("literal declared %d bytes into the file was not extracted: the string pass's read bound is truncating ordinary files (%+v)", len(body), exs)
}
