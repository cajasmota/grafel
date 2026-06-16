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
