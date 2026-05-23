// get_source_timeout_test.go — #1678 regression coverage.
//
// Background: real MCP calls to archigraph_get_source against the live daemon
// were observed to hang indefinitely. SIGQUIT goroutine dumps showed multiple
// in-flight calls all stuck inside os.Open at internal/mcp/tools.go (the
// open(2) syscall blocked in kernel for source-file paths that opened fine
// from a fresh process). Because the bridge (#1671/#1677) reuses a single
// jsonrpc.Client across an MCP session, one hung Open serializes every
// subsequent tool call too.
//
// Fix: the handler now performs file I/O on a worker goroutine and selects on
// a context deadline (5s). A stuck Open surfaces as a tool error instead of
// wedging the bridge.
//
// These tests exercise both the success path (verifies refactor preserves the
// existing output shape — windowed snippet, hard-cap, formatted lines) and
// the timeout path (verifies a slow-Open scenario returns a clean error
// instead of hanging the test).
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
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// TestGetSource_ReturnsWindowedSnippet — happy path: an entity with a real
// source file on disk gets a clamped, formatted window back.
func TestGetSource_ReturnsWindowedSnippet(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "sample.go")
	lines := []string{
		"package sample",            // 1
		"",                          // 2
		"func Foo() {",              // 3
		"\tprintln(\"in Foo\")",     // 4
		"}",                         // 5
		"",                          // 6
		"func Bar() {",              // 7
		"\tprintln(\"in Bar\")",     // 8
		"}",                         // 9
		"",                          // 10
	}
	if err := os.WriteFile(srcPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "foo", Name: "Foo", Kind: "Function", SourceFile: "sample.go", StartLine: 3, EndLine: 5},
		},
	}
	srv := newTestServerWithDoc(t, doc)
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
	tc, ok := res.Content[0].(mcpapi.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}
	// With context_lines=1, expect lines 2..6 (inclusive) — 5 numbered lines.
	if !strings.Contains(tc.Text, "    3  func Foo()") {
		t.Errorf("expected line 3 (Foo) in output, got:\n%s", tc.Text)
	}
	if strings.Contains(tc.Text, "    1  package sample") {
		t.Errorf("did not expect line 1 in output (context_lines=1, start=3), got:\n%s", tc.Text)
	}
	if strings.Contains(tc.Text, "    7  func Bar()") {
		t.Errorf("did not expect line 7 in output (context_lines=1, end=5), got:\n%s", tc.Text)
	}
}

// TestGetSource_TimesOutOnStuckOpen — covers the #1678 hang fix end-to-end.
// We point the entity at a POSIX FIFO whose writer never connects; os.Open on
// the read side of a FIFO blocks indefinitely until a writer opens it. Before
// the fix the handler would never return. After the fix the context deadline
// fires (or, when the caller's own context cancels, that wins) and the
// handler returns a clean tool-error result within a bounded interval.
//
// To keep CI fast we override the handler's internal 5s deadline by passing a
// caller context that we cancel after 500ms — the handler's select picks up
// whichever deadline fires first.
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
	srv := newTestServerWithDoc(t, doc)
	srv.State.groups["test"].Repos["repo1"].Path = dir

	// Caller cancels at 500ms — well under the handler's 5s internal deadline.
	// Whichever fires first, the handler MUST return promptly.
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
		// The handler must return well before the FIFO ever unblocks (which
		// would need a writer to connect — nothing does). Anything under
		// ~5.5s is acceptable; in practice the caller-context cancel fires at
		// ~500ms and we return shortly after.
		if elapsed > 5500*time.Millisecond {
			t.Fatalf("handler returned but took too long: %v", elapsed)
		}
		if got.err != nil {
			t.Fatalf("handler returned go error: %v", got.err)
		}
		if got.res == nil || !got.res.IsError {
			t.Fatalf("expected an isError result describing the timeout, got: %+v", got.res)
		}
		tc, _ := got.res.Content[0].(mcpapi.TextContent)
		if !strings.Contains(tc.Text, "timed out") {
			t.Errorf("expected 'timed out' in error message, got: %s", tc.Text)
		}
		t.Logf("handler returned timeout error after %v: %s", elapsed, tc.Text)
	case <-time.After(7 * time.Second):
		// Open never released the goroutine; the bug is back.
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
