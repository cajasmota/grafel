package mcp

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestActivityLogCloseWithoutAppend ensures Close does not deadlock when the
// background worker was never started (no Append ever called). Before #5264 the
// fix, Close blocked forever on <-l.done because the worker — which closes the
// done channel — only starts on the first Append.
func TestActivityLogCloseWithoutAppend(t *testing.T) {
	l := NewActivityLog(filepath.Join(t.TempDir(), ".grafel", activityLogFile))
	done := make(chan struct{})
	go func() {
		l.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close() blocked when worker was never started")
	}
}

// TestActivityLogCloseIdempotent ensures Close can be called multiple times
// (and is nil-safe) without panicking on the already-closed queue channel.
func TestActivityLogCloseIdempotent(t *testing.T) {
	var nilLog *ActivityLog
	nilLog.Close() // must not panic

	l := NewActivityLog(filepath.Join(t.TempDir(), ".grafel", activityLogFile))
	l.Append(MCPActivityEvent{ToolName: "test"})
	l.Close()
	l.Close() // second call must be a no-op, not a double-close panic
}

// TestActivityLogCloseReleasesHandle is the #5264 regression: after Close the
// log file must be removable (on Windows an open handle blocks unlink; this
// verifies the worker has released its handle by the time Close returns).
func TestActivityLogCloseReleasesHandle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".grafel", activityLogFile)
	l := NewActivityLog(path)
	l.Append(MCPActivityEvent{ToolName: "test"})
	l.Close()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected log file to exist after Close: %v", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("could not remove dir after Close (handle still open?): %v", err)
	}
}

// TestActivityLogAppendAfterClose ensures Append after Close does not panic by
// sending on the closed queue (it must be silently dropped).
func TestActivityLogAppendAfterClose(t *testing.T) {
	l := NewActivityLog(filepath.Join(t.TempDir(), ".grafel", activityLogFile))
	l.Append(MCPActivityEvent{ToolName: "first"})
	l.Close()
	// Append after Close: startWorker is a no-op (started==true) and the
	// select default path drops the event rather than sending on the closed
	// queue. Must not panic.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Append after Close panicked: %v", r)
		}
	}()
	l.Append(MCPActivityEvent{ToolName: "after-close"})
}

// TestBrokerCloseLogIdempotent ensures the broker's CloseLog is safe with and
// without an attached log, and is idempotent.
func TestBrokerCloseLogIdempotent(t *testing.T) {
	b := NewMCPActivityBroker()
	b.CloseLog() // no log attached — must be a no-op

	l := NewActivityLog(filepath.Join(t.TempDir(), ".grafel", activityLogFile))
	b.SetLog(l)
	b.Publish(MCPActivityEvent{ToolName: "test"})
	b.CloseLog()
	b.CloseLog() // detached + already closed — must not panic
}

// TestBrokerCloseLogReleasesHandle is the #5264 regression at the broker level —
// it exercises the EXACT call the daemon's graceful-stop cleanup makes
// (MCPActivityBroker.CloseLog, via daemonShutdownCleanup → closeDaemonActivityLog).
// The handle on mcp-activity.jsonl is opened lazily on the first published event
// (mirroring the selftest's grafel_stats call). After CloseLog returns the file
// must be immediately deletable — on Windows an open handle blocks unlink, which
// was the failing teardown layer. The selftest daemon previously never invoked
// this cleanup, so the handle leaked; this asserts the close path itself fully
// releases the handle synchronously.
func TestBrokerCloseLogReleasesHandle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".grafel", activityLogFile)

	b := NewMCPActivityBroker()
	b.SetLog(NewActivityLog(path))
	// Publishing opens the disk handle lazily on the worker's first write — the
	// same trigger as an MCP grafel_stats Append in the selftest.
	b.Publish(MCPActivityEvent{ToolName: "grafel_stats"})

	// Graceful-stop cleanup: this is what daemonShutdownCleanup invokes.
	b.CloseLog()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected activity log to exist after Publish+CloseLog: %v", err)
	}
	// The teardown that #5264 fixes: removing the isolated root. Must succeed
	// once the handle is released (would fail on Windows if it leaked).
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("could not remove root after CloseLog (handle still open?): %v", err)
	}
}
