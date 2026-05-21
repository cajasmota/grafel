// Package audit — append-only audit log for Archigraph state-changing operations.
//
// Every mutation that changes daemon or registry state (rebuild, reset, settings
// update, pattern edit, group create/delete, enrichment trigger…) appends one
// JSON line to ~/.archigraph/audit.jsonl.
//
// Schema of each line:
//
//	{
//	  "timestamp":  "2026-05-21T12:34:56.789Z",   // RFC 3339 with ms
//	  "operation":  "rebuild",                      // short snake_case verb
//	  "target":     "fixture-a",                    // group / repo / setting key / …
//	  "params":     { … },                          // optional extra context
//	  "result":     "ok" | "error",
//	  "error":      "message"                       // only present when result=="error"
//	}
//
// The file is rotated at 10 MiB (old file renamed to audit.jsonl.1, capping
// total disk use to ~20 MiB).  All I/O is non-blocking: Append enqueues to
// an internal channel; a single goroutine drains it.
package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// maxLogBytes is the rotation threshold.
	maxLogBytes = 10 * 1024 * 1024 // 10 MiB

	// auditLogFile is the base file name inside ~/.archigraph/.
	auditLogFile = "audit.jsonl"

	// logQueueDepth is the channel buffer size. Overflow events are dropped
	// rather than blocking the caller.
	logQueueDepth = 512
)

// Entry is one audit log record.
type Entry struct {
	// Timestamp is the RFC 3339 (with milliseconds) time of the operation.
	Timestamp string `json:"timestamp"`
	// Operation is a short snake_case verb, e.g. "rebuild", "settings_update".
	Operation string `json:"operation"`
	// Target identifies what was acted upon: group slug, repo slug, setting key, etc.
	Target string `json:"target,omitempty"`
	// Params holds optional structured context (request body fields, etc.).
	Params map[string]any `json:"params,omitempty"`
	// Result is "ok" or "error".
	Result string `json:"result"`
	// Error is the error message when Result == "error".
	Error string `json:"error,omitempty"`
}

// Log is a goroutine-safe, non-blocking, rotating JSONL sink.
type Log struct {
	path  string
	queue chan Entry
	once  sync.Once
	done  chan struct{}
}

// New constructs a Log that writes to path.  The background goroutine is
// started lazily on the first Append call.
func New(path string) *Log {
	return &Log{
		path:  path,
		queue: make(chan Entry, logQueueDepth),
		done:  make(chan struct{}),
	}
}

// DefaultLogPath returns ~/.archigraph/audit.jsonl.
// Returns an empty string when the home directory cannot be determined;
// callers should treat that as "disk logging disabled".
func DefaultLogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".archigraph", auditLogFile)
}

// Append enqueues e for disk write.  Returns immediately; never blocks.
// The background goroutine (started on first call) performs the actual I/O.
func (l *Log) Append(e Entry) {
	if e.Timestamp == "" {
		e.Timestamp = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	}
	if e.Result == "" {
		e.Result = "ok"
	}
	l.once.Do(l.startWorker)
	select {
	case l.queue <- e:
	default:
		// queue full — drop rather than block
	}
}

// AppendOK is a convenience wrapper for successful operations.
func (l *Log) AppendOK(operation, target string, params map[string]any) {
	l.Append(Entry{
		Operation: operation,
		Target:    target,
		Params:    params,
		Result:    "ok",
	})
}

// AppendErr is a convenience wrapper for failed operations.
func (l *Log) AppendErr(operation, target string, params map[string]any, errMsg string) {
	l.Append(Entry{
		Operation: operation,
		Target:    target,
		Params:    params,
		Result:    "error",
		Error:     errMsg,
	})
}

// Close flushes the remaining queue and stops the background goroutine.
// After Close returns, further Append calls are silently dropped.
// Safe to call even when no Append has occurred (no-op).
func (l *Log) Close() {
	// Start the worker if it hasn't been started yet, so we can close
	// the queue and wait for the done signal.
	l.once.Do(l.startWorker)
	close(l.queue)
	<-l.done
}

// Path returns the on-disk path this log writes to.
func (l *Log) Path() string { return l.path }

func (l *Log) startWorker() {
	go l.worker()
}

func (l *Log) worker() {
	defer close(l.done)

	dir := filepath.Dir(l.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
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

	written := fileSize(l.path)

	for e := range l.queue {
		line, err2 := json.Marshal(e)
		if err2 != nil {
			continue
		}
		n, _ := f.Write(append(line, '\n'))
		written += int64(n)

		if written >= maxLogBytes {
			f.Close()
			_ = os.Rename(l.path, l.path+".1")
			f, err = os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
			if err != nil {
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

// ReadHistory reads the last n entries from the JSONL log file at path.
// Supports optional filter by operation. Reads the entire file up to 50 MiB.
func ReadHistory(path string, n int, filterOp string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var entries []Entry
	start := 0
	for i := 0; i <= len(data); i++ {
		if i == len(data) || data[i] == '\n' {
			line := data[start:i]
			start = i + 1
			if len(line) == 0 {
				continue
			}
			var e Entry
			if err2 := json.Unmarshal(line, &e); err2 == nil {
				if filterOp == "" || e.Operation == filterOp {
					entries = append(entries, e)
				}
			}
		}
	}

	if n > 0 && len(entries) > n {
		entries = entries[len(entries)-n:]
	}
	return entries, nil
}
