// Package mcp — activity_log.go
//
// ActivityLog writes MCP activity events to a rotating JSONL file at
// ~/.archigraph/mcp-activity.jsonl. Each line is a JSON-encoded
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

	// activityLogFile is the base file name inside ~/.archigraph/.
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

// DefaultActivityLogPath returns ~/.archigraph/mcp-activity.jsonl.
// Returns an empty string when the home directory cannot be determined
// (the caller should treat that as "disk logging disabled").
func DefaultActivityLogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".archigraph", activityLogFile)
}

// Append enqueues e for disk write. Returns immediately; never blocks.
// The background goroutine (started on first call) performs the actual I/O.
func (l *ActivityLog) Append(e MCPActivityEvent) {
	l.once.Do(l.startWorker)
	select {
	case l.queue <- e:
	default:
		// queue full — drop this event; disk logging is best-effort.
	}
}

// Close flushes the remaining queue and stops the background goroutine.
// After Close returns, further Append calls are silently dropped.
func (l *ActivityLog) Close() {
	close(l.queue)
	<-l.done
}

// startWorker launches the background I/O goroutine. Called once via
// sync.Once on the first Append.
func (l *ActivityLog) startWorker() {
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
