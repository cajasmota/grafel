package main

import (
	"errors"
	"fmt"
	"io"
	"sync"
)

// fanoutWriter writes every Write to every sink, and — unlike io.MultiWriter —
// a sink that fails does not suppress the sinks after it.
//
// Why this exists (#7050): the daemon logs through
// buildDaemonSlogLogger(<console sink>, <daemon.log file>). io.MultiWriter
// "stops and returns the error" at the first sink that fails and never reaches
// the rest, and slog discards the handler's write error, so ONE failing sink
// silences daemon.log for the whole run with nothing anywhere to say why.
//
// That sink is not hypothetical on Windows. detachConsole() calls FreeConsole
// before the logger is built (runDaemon, daemon.go), and a daemon launched
// detached (DETACHED_PROCESS / CREATE_NO_WINDOW) may have no valid stderr
// handle at all. Writes to it then fail on every record. A daemon in that state
// runs and serves RPC perfectly while its log file stays empty — the signature
// reported in #7050, where a provably-running daemon wrote zero lines to an
// append-only daemon.log, not even the `startup: pidfile-acquire begin` line
// emitted within milliseconds of start.
//
// The durable sink is the one that matters for diagnosis, and it must not
// depend on the health of the ephemeral one.
type fanoutWriter struct {
	mu sync.Mutex
	// sinks is written to in order. Every sink is attempted on every Write,
	// including one that failed before: a handle can come back (a full disk
	// that is emptied), and a sink that is permanently dead costs one failing
	// syscall per record — far cheaper than deciding to stop writing logs.
	sinks []*fanoutSink
	// emittingNotice guards the failure notice below against recursing through
	// this same Write.
	emittingNotice bool
}

type fanoutSink struct {
	w io.Writer
	// reported records that this sink's first failure has already been
	// announced on the surviving sinks, so a permanently dead sink produces
	// one notice, not one per log record.
	reported bool
}

// errNoSinks is reported when a fanoutWriter has nothing to write to. A
// silently-successful write to zero sinks is the same lie this type exists to
// stop telling.
var errNoSinks = errors.New("log fanout: no sinks configured")

func newFanoutWriter(sinks ...io.Writer) *fanoutWriter {
	f := &fanoutWriter{sinks: make([]*fanoutSink, 0, len(sinks))}
	for _, w := range sinks {
		f.sinks = append(f.sinks, &fanoutSink{w: w})
	}
	return f
}

// Write attempts EVERY sink — never stopping at the first success or the first
// failure — and reports success if any of them accepted the bytes, returning
// the first error only when they all failed.
//
// The first time a sink fails, the failure is announced on the sinks that are
// still working. Without that, the inverse of #7050 is silent: if daemon.log
// dies while stderr lives, the durable record simply stops, and a later reader
// cannot tell an empty log from a quiet daemon. The error itself names the
// sink for the only kind that matters here — os.File write errors are
// *PathError and carry the path.
//
// Short writes are not treated as failures. Every production sink is an
// *os.File, whose Write converts a short write into io.ErrShortWrite and
// returns it as an error, so a partial write cannot reach the success path
// unnoticed; returning len(p) is then accurate for the callers we have. A
// future sink that can short-write without erroring would need this revisited.
func (f *fanoutWriter) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.sinks) == 0 {
		return 0, errNoSinks
	}
	var firstErr error
	delivered := false
	newlyFailed := make([]error, 0, len(f.sinks))
	for _, s := range f.sinks {
		if _, err := s.w.Write(p); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			if !s.reported {
				s.reported = true
				newlyFailed = append(newlyFailed, err)
			}
			continue
		}
		delivered = true
	}
	if delivered {
		f.noticeLocked(newlyFailed)
		return len(p), nil
	}
	return 0, firstErr
}

// noticeLocked announces each newly-failed sink on the sinks that still work.
// Caller holds f.mu. It is a no-op while a notice is already being emitted, so
// a sink that fails during the announcement cannot recurse.
func (f *fanoutWriter) noticeLocked(failures []error) {
	if len(failures) == 0 || f.emittingNotice {
		return
	}
	f.emittingNotice = true
	defer func() { f.emittingNotice = false }()
	for _, err := range failures {
		line := fmt.Sprintf("log fanout: a log sink failed and its output is being lost; the remaining sinks continue: %v\n", err)
		for _, s := range f.sinks {
			if s.reported {
				continue // this is one of the dead ones.
			}
			_, _ = s.w.Write([]byte(line))
		}
	}
}
