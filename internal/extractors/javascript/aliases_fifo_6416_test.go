//go:build unix

package javascript

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fifoDeadline bounds each subtest. A correctly-gated read returns in
// microseconds — the stat gate never opens a FIFO — so anything near this
// value is the hang, not a slow machine. It is deliberately well under the
// package test timeout so a regression FAILS rather than wedging the suite
// until the watchdog kills the binary with no attribution.
const fifoDeadline = 10 * time.Second

// mkfifoInTemp creates a named pipe inside dir, which must be a t.TempDir().
// A FIFO outside a temp dir would outlive the test and hang any other process
// that walks over it, so this helper takes the directory rather than a path
// and refuses to be pointed anywhere else by accident.
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

// TestAliasMapForFileFIFOConfigs is the #6416 regression for the JS alias
// loader.
//
// aliases.go opens six classes of config file by NAME — tsconfig.json,
// jsconfig.json, vite/webpack/metro/babel configs — from a repo root, and none
// of those reads comes from the file walker. The walker's entry-type gate
// therefore cannot protect them: `mkfifo tsconfig.json` at a repo root wedged
// AliasMapForFile (and AliasMapFor, and the cross/imports extractor that calls
// it) forever, because os.ReadFile on a FIFO waits in open(2) for a writer
// that never comes.
//
// This is a HANG, so the honest way to pin it is a deadline, not an assertion
// about a return value: the pre-fix code never produced a value to assert on.
func TestAliasMapForFileFIFOConfigs(t *testing.T) {
	names := []string{
		"tsconfig.json",
		"jsconfig.json",
		"vite.config.ts",
		"webpack.config.js",
		"metro.config.js",
		"babel.config.js",
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			mkfifoInTemp(t, dir, name)
			src := filepath.Join(dir, "a.ts")
			if err := os.WriteFile(src, []byte("export const a = 1\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			resetAliasMapCache()

			done := make(chan struct{})
			go func() {
				defer close(done)
				_ = AliasMapForFile(dir, src)
				_ = AliasMapFor(dir)
			}()
			select {
			case <-done:
			case <-time.After(fifoDeadline):
				t.Fatalf("HANG: AliasMapForFile with a FIFO named %s did not return within %s", name, fifoDeadline)
			}
		})
	}
}

// TestAliasMapFIFOSubdirConfig covers the second entry point into the same
// reads: scanSubdirAliasMap descends one level and parses a per-package
// tsconfig, so a FIFO one directory down is reached even when the repo root is
// clean.
func TestAliasMapFIFOSubdirConfig(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "packages")
	if err := os.Mkdir(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	mkfifoInTemp(t, sub, "tsconfig.json")
	resetAliasMapCache()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = AliasMapFor(root)
	}()
	select {
	case <-done:
	case <-time.After(fifoDeadline):
		t.Fatalf("HANG: AliasMapFor with a FIFO at packages/tsconfig.json did not return within %s", fifoDeadline)
	}
}

// TestAliasSkipIsReported pins the half of the fix that is not the hang.
//
// The #6416 re-review's finding F4 was that safeio.ErrNotRegular carries a
// precise reason — path plus entry kind — and that every consumer so far threw
// it away and returned nil. A FIFO named tsconfig.json silently producing no
// aliases is exactly the shape of #6338: a file the user can see, contributing
// nothing, reported nowhere. So the skip must leave a record, and that record
// is asserted here rather than assumed.
func TestAliasSkipIsReported(t *testing.T) {
	dir := t.TempDir()
	p := mkfifoInTemp(t, dir, "tsconfig.json")
	resetAliasMapCache()

	var buf strings.Builder
	restore := setAliasSkipOutput(&buf)
	defer restore()

	_ = AliasMapFor(dir)

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
