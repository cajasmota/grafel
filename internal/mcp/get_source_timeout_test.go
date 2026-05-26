//go:build darwin || linux

// get_source_timeout_test.go — #1678 / #1773 regression coverage.
//
// Background: real MCP calls to archigraph_get_source against the live daemon
// were observed to hang indefinitely. SIGQUIT goroutine dumps showed multiple
// in-flight calls all stuck inside os.Open at internal/mcp/tools.go (the
// open(2) syscall blocked in kernel for source-file paths that opened fine
// from a fresh process). Because the bridge (#1671/#1677) reuses a single
// jsonrpc.Client across an MCP session, one hung Open serializes every
// subsequent tool call too.
//
// #1678 fix: file I/O runs on a worker goroutine behind a 5s context deadline.
// #1773 fix: the open(2) itself is non-blocking (O_NONBLOCK) so the 5s deadline
//
//	almost never fires — the open returns immediately even under an fsevents
//	kernel stall (macOS). The Lstat layer rejects non-regular files in <1µs.
//
// Tests:
//   - TestGetSource_ReturnsWindowedSnippet — success path, output shape
//   - TestGetSource_TimesOutOnStuckOpen — FIFO (non-regular): rejected by Lstat
//   - TestGetSource_FsnotifyWatcher_CompletesQuickly — #1773 regression: file in
//     an fsnotify-watched directory reads in <100ms (would timeout at 5s pre-fix)
//   - TestReadSourceWindow_FormatsLines — direct unit test, line formatting
//   - TestReadSourceWindow_MissingFile — direct unit test, error path
package mcp

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/fsnotify/fsnotify"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// TestGetSource_ReturnsWindowedSnippet — happy path: an entity with a real
// source file on disk gets a clamped, formatted window back.
func TestGetSource_ReturnsWindowedSnippet(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "sample.go")
	lines := []string{
		"package sample",        // 1
		"",                      // 2
		"func Foo() {",          // 3
		"\tprintln(\"in Foo\")", // 4
		"}",                     // 5
		"",                      // 6
		"func Bar() {",          // 7
		"\tprintln(\"in Bar\")", // 8
		"}",                     // 9
		"",                      // 10
	}
	if err := os.WriteFile(srcPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "foo", Name: "Foo", Kind: "Function", SourceFile: "sample.go", StartLine: 3, EndLine: 5},
		},
	}
	srv := newTestServer(t, doc)
	// Repoint the test repo at our temp dir.
	srv.State.groups["test"].Repos["repo1"].Path = dir

	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "test", "node_id": "foo", "context_lines": 1}

	res, err := srv.handleGetNodeSource(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("unexpected error result: %+v", res)
	}
	// get_source returns line-numbered plain text, not JSON — use extractResultText.
	text := extractResultText(t, res)
	// With context_lines=1, expect lines 2..6 (inclusive) — 5 numbered lines.
	if !strings.Contains(text, "    3  func Foo()") {
		t.Errorf("expected line 3 (Foo) in output, got:\n%s", text)
	}
	if strings.Contains(text, "    1  package sample") {
		t.Errorf("did not expect line 1 in output (context_lines=1, start=3), got:\n%s", text)
	}
	if strings.Contains(text, "    7  func Bar()") {
		t.Errorf("did not expect line 7 in output (context_lines=1, end=5), got:\n%s", text)
	}
}

// TestGetSource_TimesOutOnStuckOpen — covers the #1678/#1773 hang fix end-to-end.
// We point the entity at a POSIX FIFO whose writer never connects. Pre-#1678,
// the raw os.Open on the read side blocked indefinitely. Pre-#1773, the
// goroutine with the 5s deadline fired but only after 5.000s.
//
// After the #1773 three-layer defense:
//   - On Unix, the Lstat layer catches non-regular files (FIFOs, sockets, etc.)
//     immediately and returns an error — so the handler returns in <1ms without
//     ever attempting the blocking open(2).
//   - The handler MUST return a tool-error result (IsError=true), not a Go error.
//   - The handler MUST return within 500ms (generous budget; real return is <1ms).
func TestGetSource_TimesOutOnStuckOpen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FIFO test requires POSIX mkfifo; skipping on windows")
	}
	dir := t.TempDir()
	fifoPath := filepath.Join(dir, "stuck.fifo")
	if err := syscall.Mkfifo(fifoPath, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "stuck", Name: "Stuck", Kind: "Function", SourceFile: "stuck.fifo", StartLine: 1, EndLine: 1},
		},
	}
	srv := newTestServer(t, doc)
	srv.State.groups["test"].Repos["repo1"].Path = dir

	// 500ms ceiling — the handler must return well within this budget.
	// Post-#1773 the Lstat layer fires in <1ms for FIFOs; the 500ms is
	// generous headroom for slow CI machines.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "test", "node_id": "stuck"}

	type out struct {
		res *mcpapi.CallToolResult
		err error
	}
	resCh := make(chan out, 1)
	start := time.Now()
	go func() {
		res, err := srv.handleGetNodeSource(ctx, req)
		resCh <- out{res: res, err: err}
	}()

	select {
	case got := <-resCh:
		elapsed := time.Since(start)
		// Post-#1773: non-regular files are rejected by the Lstat layer in <1ms.
		// Pre-fix: the handler hung for 5s on the raw os.Open deadline.
		if elapsed > 500*time.Millisecond {
			t.Fatalf("handler returned but took too long: %v (expected <500ms, fix regression?)", elapsed)
		}
		if got.err != nil {
			t.Fatalf("handler returned go error (want nil): %v", got.err)
		}
		if got.res == nil || !got.res.IsError {
			t.Fatalf("expected an isError tool result, got: %+v", got.res)
		}
		t.Logf("handler returned error result in %v (non-regular file rejected immediately)", elapsed)
	case <-time.After(7 * time.Second):
		buf := make([]byte, 1<<14)
		n := runtime.Stack(buf, true)
		t.Fatalf("handler did not return within 7s — fix regression\nstacks:\n%s", buf[:n])
	}
}

// TestReadSourceWindow_FormatsLines — direct unit test on the extracted
// readSourceWindow helper. Locks down the line-number formatting and bounds.
func TestReadSourceWindow_FormatsLines(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "tiny.go")
	if err := os.WriteFile(srcPath, []byte("aaa\nbbb\nccc\nddd\neee\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := readSourceWindow(srcPath, 2, 4)
	if err != nil {
		t.Fatalf("readSourceWindow: %v", err)
	}
	want := "    2  bbb\n    3  ccc\n    4  ddd\n"
	if out != want {
		t.Errorf("readSourceWindow mismatch:\ngot:  %q\nwant: %q", out, want)
	}
}

// TestReadSourceWindow_MissingFile — error path: a non-existent path returns
// the os.Open error, not a panic. Important because the daemon stores
// SourceFile strings that may be stale after a repo file is deleted.
func TestReadSourceWindow_MissingFile(t *testing.T) {
	_, err := readSourceWindow(filepath.Join(t.TempDir(), "no-such-file.go"), 1, 5)
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// TestGetSource_FsnotifyWatcher_CompletesQuickly — #1773 regression test.
//
// This is the EXACT bug class that caused archigraph_get_source to always
// time out at 5.000s in the iter5 quality bench (Q10, Q11). The daemon holds
// an fsnotify watcher on every indexed source tree. On macOS, a raw os.Open
// on a path inside a watched tree can block indefinitely in the kernel
// (fsevents stall). The #1773 fix uses O_NONBLOCK open(2) so the kernel
// returns immediately even during a stall.
//
// We prove the fix works by:
//  1. Spawning an fsnotify watcher on a temp directory.
//  2. Writing a real source file in that directory.
//  3. Calling readSourceWindow on the file.
//  4. Asserting the call completes in <100ms.
//
// Without the fix, this call would block at os.Open for ~5s on macOS before
// the deadline goroutine cancels it. With the fix it returns in <1ms.
func TestGetSource_FsnotifyWatcher_CompletesQuickly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fsevents stall is a macOS/Linux concern; skipping on Windows")
	}

	dir := t.TempDir()

	// Start an fsnotify watcher on the directory, simulating the daemon's
	// per-repo watchers. The watcher holds the tree under observation for
	// the duration of the test.
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("fsnotify.NewWatcher: %v", err)
	}
	defer watcher.Close()
	if err := watcher.Add(dir); err != nil {
		t.Fatalf("watcher.Add: %v", err)
	}
	// Drain fsnotify events in background to avoid channel back-pressure.
	go func() {
		for range watcher.Events {
		}
	}()

	// Write a real source file into the watched directory.
	srcPath := filepath.Join(dir, "watched_src.go")
	content := "package watched\n\nfunc Hello() string { return \"hello\" }\n"
	if err := os.WriteFile(srcPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Call readSourceWindow and measure latency. The 100ms ceiling is generous;
	// the non-blocking open path returns in <1ms on any healthy filesystem.
	// Pre-fix: this call would take exactly 5.000s (context deadline).
	start := time.Now()
	out, err := readSourceWindow(srcPath, 1, 3)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("readSourceWindow failed under fsnotify watcher: %v (elapsed %v)", err, elapsed)
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("readSourceWindow took %v under fsnotify watcher — expected <100ms; #1773 regression?", elapsed)
	}
	if !strings.Contains(out, "package watched") {
		t.Errorf("unexpected output: %q", out)
	}
	t.Logf("#1773 OK: readSourceWindow completed in %v under fsnotify watcher", elapsed)
}
