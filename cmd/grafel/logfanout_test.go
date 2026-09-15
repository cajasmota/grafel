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
	// Contains, not equality: the surviving sink also carries the one-shot
	// notice about the failed sink (see
	// TestFanoutWriter_FailedSinkIsAnnouncedOnTheSurvivors_Once).
	if !strings.HasPrefix(file.String(), "line\n") {
		t.Fatalf("file sink = %q, want it to start with the line", file.String())
	}
}

// #7050 review round 2 (N2 — BLOCKER): every test above has a FAILING first
// sink, so `break` after the first SUCCESSFUL sink survived all of them. In
// production the sinks are (os.Stderr, logFile) in that order, so on any
// healthy machine stderr would succeed, the loop would stop, and daemon.log
// would never be written for the entire run — the exact symptom this type was
// written to prevent, reachable everywhere rather than only after FreeConsole.
//
// The type's whole promise is "every sink gets the bytes". This is the test
// that holds it: a first sink that SUCCEEDS, and a later sink whose content is
// then verified.
func TestFanoutWriter_EverySinkReceivesTheBytes(t *testing.T) {
	var console, file, third bytes.Buffer

	w := newFanoutWriter(&console, &file, &third)
	if _, err := w.Write([]byte("startup: pidfile-acquire begin\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	for name, sink := range map[string]*bytes.Buffer{"console": &console, "file": &file, "third": &third} {
		if got := sink.String(); got != "startup: pidfile-acquire begin\n" {
			t.Fatalf("%s sink = %q, want the line — a successful sink must not stop the fanout", name, got)
		}
	}
}

// #7050 review round 2 (N1): errNoSinks was asserted in prose and observed by
// nothing. newFanoutWriter is variadic precisely so a future caller can pass
// none, and a silently-successful write to zero sinks is the same lie this
// type exists to stop telling.
func TestFanoutWriter_NoSinks_ReportsError(t *testing.T) {
	n, err := newFanoutWriter().Write([]byte("line\n"))
	if !errors.Is(err, errNoSinks) {
		t.Fatalf("zero-sink write returned (%d, %v), want errNoSinks", n, err)
	}
	if n != 0 {
		t.Fatalf("zero-sink write reported %d bytes written, want 0", n)
	}
}

// The inverse of #7050 must not be silent either: if daemon.log dies while the
// console lives, the durable record just stops, and a later reader cannot tell
// an empty log from a quiet daemon. The surviving sinks say so — once, not
// once per record, or a dead sink would drown the log it is reporting on.
func TestFanoutWriter_FailedSinkIsAnnouncedOnTheSurvivors_Once(t *testing.T) {
	var console bytes.Buffer
	w := newFanoutWriter(&console, &errWriter{})

	for i := 0; i < 3; i++ {
		if _, err := w.Write([]byte("record\n")); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	got := console.String()
	if n := strings.Count(got, "log fanout: a log sink failed"); n != 1 {
		t.Fatalf("failure notice appeared %d times across 3 records, want exactly 1:\n%s", n, got)
	}
	if !strings.Contains(got, "The handle is invalid") {
		t.Fatalf("notice does not carry the underlying error (which is what names the sink):\n%s", got)
	}
	if n := strings.Count(got, "record\n"); n != 3 {
		t.Fatalf("surviving sink got %d records, want 3 — the notice must not displace the log", n)
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
