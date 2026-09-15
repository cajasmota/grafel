package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// errWriter is a sink that always fails, standing in for the daemon's stderr
// after detachConsole() has called FreeConsole: the inherited handle no longer
// refers to a console, and every write to it returns an error.
type errWriter struct{ n int }

func (e *errWriter) Write(p []byte) (int, error) {
	e.n++
	return 0, errors.New("write /dev/stderr: The handle is invalid.")
}

// #7050: the daemon logged through io.MultiWriter(os.Stderr, logFile), and
// io.MultiWriter STOPS at the first sink that errors — it does not continue
// down the list. detachConsole() (FreeConsole, Windows-only) runs ~80 lines
// BEFORE the logger is built, so every daemon log write hits a detached stderr
// first. One failing handle there silences daemon.log for the entire run,
// while the daemon itself runs perfectly and serves RPC — which is exactly the
// "provably running, zero lines in an append-only daemon.log" signature
// reported in #7050 repro 2.
//
// The file sink must receive the bytes no matter what the console sink does.
func TestFanoutWriter_FailingSinkDoesNotSilenceTheOthers(t *testing.T) {
	stderr := &errWriter{}
	var file bytes.Buffer

	w := newFanoutWriter(stderr, &file)
	n, err := w.Write([]byte("startup: pidfile-acquire begin\n"))
	if err != nil {
		t.Fatalf("a failing console sink must not fail the write: %v", err)
	}
	if n != len("startup: pidfile-acquire begin\n") {
		t.Fatalf("n = %d, want the full write length", n)
	}
	if got := file.String(); !strings.Contains(got, "pidfile-acquire begin") {
		t.Fatalf("file sink got %q — the failing sink swallowed the line (io.MultiWriter's behaviour)", got)
	}
	if stderr.n != 1 {
		t.Fatalf("console sink was attempted %d times, want 1 — it must still be tried", stderr.n)
	}
}

// The mirror: order must not matter. A failing sink LAST must not be able to
// turn a successful file write into a reported failure either.
func TestFanoutWriter_FailingSinkLast_StillReportsSuccess(t *testing.T) {
	var file bytes.Buffer
	w := newFanoutWriter(&file, &errWriter{})
	if _, err := w.Write([]byte("line\n")); err != nil {
		t.Fatalf("one failing sink of two must not fail the write: %v", err)
	}
	if file.String() != "line\n" {
		t.Fatalf("file sink = %q, want the line", file.String())
	}
}

// When EVERY sink fails there is nothing to be optimistic about: report it,
// so a caller that does check errors is not told a lie.
func TestFanoutWriter_AllSinksFail_ReportsError(t *testing.T) {
	w := newFanoutWriter(&errWriter{}, &errWriter{})
	if _, err := w.Write([]byte("line\n")); err == nil {
		t.Fatalf("all sinks failed but the write reported success")
	}
}

// The end-to-end shape of the #7050 regression: the real logger builder, a
// dead console sink, and a file sink that must still receive the record.
func TestBuildDaemonSlogLogger_SurvivesADeadConsoleSink(t *testing.T) {
	var file bytes.Buffer
	logger := buildDaemonSlogLogger(&errWriter{}, &file)
	logger.Info("startup: pidfile-acquire begin")
	if got := file.String(); !strings.Contains(got, "pidfile-acquire begin") {
		t.Fatalf("daemon.log sink got %q, want the startup line", got)
	}
}

// Guard against the regression being reintroduced by a plain io.MultiWriter:
// this documents the behaviour we are deliberately NOT using. If this ever
// starts passing, the standard library changed and the comment above is stale.
func TestIOMultiWriter_StopsAtTheFirstFailingSink(t *testing.T) {
	var file bytes.Buffer
	if _, err := io.MultiWriter(&errWriter{}, &file).Write([]byte("line\n")); err == nil {
		t.Fatalf("io.MultiWriter reported success with a failing first sink")
	}
	if file.Len() != 0 {
		t.Fatalf("io.MultiWriter reached the second sink (%q) — premise of the fanout fix is stale", file.String())
	}
}
