package main

import (
	"errors"
	"io"
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
// detached (DETACHED_PROCESS / CREATE_NO_WINDOW / a hidden wscript wrapper)
// may have no valid stderr handle at all. Writes to it then fail on every
// record. A daemon in that state runs and serves RPC perfectly while its log
// file stays empty — the signature reported in #7050, where a provably-running
// daemon wrote zero lines to an append-only daemon.log, not even the
// `startup: pidfile-acquire begin` line emitted within milliseconds of start.
//
// The durable sink is the one that matters for diagnosis, and it must not
// depend on the health of the ephemeral one.
type fanoutWriter struct {
	sinks []io.Writer
}

// errNoSinks is reported when a fanoutWriter has nothing to write to. A
// silently-successful write to zero sinks is the same lie this type exists to
// stop telling.
var errNoSinks = errors.New("log fanout: no sinks configured")

func newFanoutWriter(sinks ...io.Writer) *fanoutWriter {
	return &fanoutWriter{sinks: sinks}
}

// Write attempts every sink and reports success if ANY of them accepted the
// bytes, returning the first error only when they ALL failed.
//
// Reporting len(p) when some sink took a short write is deliberate: this is a
// log fanout, the caller (slog) discards the result either way, and the
// alternative — propagating one sink's failure — is precisely the behaviour
// that produced an empty daemon.log.
func (f *fanoutWriter) Write(p []byte) (int, error) {
	if len(f.sinks) == 0 {
		return 0, errNoSinks
	}
	var firstErr error
	delivered := false
	for _, w := range f.sinks {
		if _, err := w.Write(p); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		delivered = true
	}
	if delivered {
		return len(p), nil
	}
	return 0, firstErr
}
