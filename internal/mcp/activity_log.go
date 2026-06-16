// Package mcp — activity_log.go
//
// ActivityLog writes MCP activity events to a rotating JSONL file at
// ~/.grafel/mcp-activity.jsonl. Each line is a JSON-encoded
// MCPActivityEvent. The log file is rotated when it exceeds maxLogBytes
// (the previous file is renamed with a .1 suffix, capping disk usage to
// 2 × maxLogBytes). All disk I/O is non-blocking: Append queues events to
// an internal channel; a single goroutine drains it and flushes to disk.
package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

const (
	// maxLogBytes is the rotation threshold for the activity JSONL file.
	// At 10 MiB the file is renamed .1 and a new file is started.
	maxLogBytes = 10 * 1024 * 1024 // 10 MiB

	// activityLogFile is the base file name inside ~/.grafel/.
	activityLogFile = "mcp-activity.jsonl"

	// logQueueDepth is the internal channel buffer. Events that overflow
	// the queue are dropped rather than blocking the caller.
	logQueueDepth = 512
)

// ActivityLog is a goroutine-safe, non-blocking rotating JSONL sink.
type ActivityLog struct {
	path  string
	queue chan MCPActivityEvent
	once  sync.Once
	done  chan struct{}

	// started records whether the background worker has been launched. Close
	// must not block on the worker's done channel if Append was never called
	// (the worker — and therefore the open file handle — never came into
	// existence). Guarded by startMu.
	startMu   sync.Mutex
	started   bool
	closed    bool
	closeOnce sync.Once
}

// NewActivityLog constructs an ActivityLog that writes to path. The
// background goroutine is started lazily on the first Append call.
func NewActivityLog(path string) *ActivityLog {
	return &ActivityLog{
		path:  path,
		queue: make(chan MCPActivityEvent, logQueueDepth),
		done:  make(chan struct{}),
	}
}

// DefaultActivityLogPath returns ~/.grafel/mcp-activity.jsonl.
// Returns an empty string when the home directory cannot be determined
// (the caller should treat that as "disk logging disabled").
func DefaultActivityLogPath() string {
	// Prefer $HOME so tests using t.Setenv("HOME", tmpDir) work on Windows
	// where os.UserHomeDir() reads USERPROFILE and ignores HOME.
	home := os.Getenv("HOME")
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return ""
		}
	}
	return filepath.Join(home, ".grafel", activityLogFile)
}

// Append enqueues e for disk write. Returns immediately; never blocks.
// The background goroutine (started on first call) performs the actual I/O.
// Safe to call after Close: the event is silently dropped (sending on the
// closed queue would otherwise panic, which select+default does NOT prevent).
func (l *ActivityLog) Append(e MCPActivityEvent) {
	l.startMu.Lock()
	closed := l.closed
	l.startMu.Unlock()
	if closed {
		return
	}
	l.once.Do(l.startWorker)
	defer func() {
		// Tolerate a Close that raced between the closed-check above and the
		// send below: sending on a closed channel panics; recover and drop.
		_ = recover()
	}()
	select {
	case l.queue <- e:
	default:
		// queue full — drop this event; disk logging is best-effort.
	}
}

// Close flushes the remaining queue, stops the background goroutine and waits
// for it to release the open file handle. It is idempotent and nil-safe, and
// it does not block when the worker was never started (no Append ever ran), so
// it is always safe to call on daemon shutdown.
//
// Closing the file handle here is what makes the isolated-root teardown work on
// Windows: Windows refuses to unlink a file while a handle is open, whereas
// Unix tolerates it. The daemon's graceful-stop path must call Close before any
// caller removes ~/.grafel.
func (l *ActivityLog) Close() {
	if l == nil {
		return
	}
	l.closeOnce.Do(func() {
		l.startMu.Lock()
		started := l.started
		// Mark as closed so any concurrent startWorker becomes a no-op and the
		// worker goroutine is never launched after Close, and so Append drops
		// events instead of sending on the closed queue.
		l.started = true
		l.closed = true
		l.startMu.Unlock()

		close(l.queue)
		if started {
			// Worker is running (or about to drain the now-closed queue); wait
			// for it to flush and Close the file handle.
			<-l.done
		}
	})
}

// startWorker launches the background I/O goroutine. Called once via
// sync.Once on the first Append. If Close has already run, it is a no-op so no
// new file handle is opened after shutdown.
func (l *ActivityLog) startWorker() {
	l.startMu.Lock()
	if l.started {
		l.startMu.Unlock()
		return
	}
	l.started = true
	l.startMu.Unlock()
	go l.worker()
}

func (l *ActivityLog) worker() {
	defer close(l.done)

	// Ensure the directory exists. Failure is non-fatal: we just log nothing.
	dir := filepath.Dir(l.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		// drain the queue without writing.
		for range l.queue {
		}
		return
	}

	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		for range l.queue {
		}
		return
	}

	enc := json.NewEncoder(f)
	written := fileSize(l.path)

	for e := range l.queue {
		line, err2 := json.Marshal(e)
		if err2 != nil {
			continue
		}
		n, _ := f.Write(append(line, '\n'))
		written += int64(n)
		_ = enc // enc is used for alternative approach; keep for future

		if written >= maxLogBytes {
			f.Close()
			rotated := l.path + ".1"
			_ = os.Rename(l.path, rotated)
			f, err = os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
			if err != nil {
				// can't reopen — drain silently.
				for range l.queue {
				}
				return
			}
			written = 0
		}
	}
	f.Close()
}

// fileSize returns the current byte size of path, or 0 on error.
func fileSize(path string) int64 {
	if fi, err := os.Stat(path); err == nil {
		return fi.Size()
	}
	return 0
}

// ---------------------------------------------------------------------------
// History reader
// ---------------------------------------------------------------------------

// ReadHistory reads the last n events from the JSONL log file at path.
// It reads the entire file (up to 50 MiB) and returns the tail. Intended
// only for the /api/mcp-activity/history endpoint — not a hot path.
func ReadHistory(path string, n int) ([]MCPActivityEvent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var events []MCPActivityEvent
	// Split on newlines and decode each line.
	start := 0
	for i := 0; i <= len(data); i++ {
		if i == len(data) || data[i] == '\n' {
			line := data[start:i]
			start = i + 1
			if len(line) == 0 {
				continue
			}
			var e MCPActivityEvent
			if err2 := json.Unmarshal(line, &e); err2 == nil {
				events = append(events, e)
			}
		}
	}

	if n > 0 && len(events) > n {
		events = events[len(events)-n:]
	}
	return events, nil
}
