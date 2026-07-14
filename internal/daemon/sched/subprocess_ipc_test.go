package sched

import (
	"bytes"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/progress"
)

// TestStdoutProgressPublisher_RoundTrip is the make-or-break fidelity test for
// the wizard bars: a sequence of per-module progress events marshalled by the
// child's StdoutProgressPublisher must round-trip through parseSubprocessStdout
// into the parent's publisher byte-faithfully — every Event field preserved,
// per module.
func TestStdoutProgressPublisher_RoundTrip(t *testing.T) {
	in := []progress.Event{
		{GroupSlug: "g", RepoSlug: "svc", Phase: progress.PhaseExtractAST, Module: "services/auth", FilesDone: 3, FilesTotal: 10, EntitiesSoFar: 42, BytesSeen: 999, CurrentFile: "services/auth/login.go", PhaseStartedAtMS: 111, TS: 1000},
		{GroupSlug: "g", RepoSlug: "svc", Phase: progress.PhaseExtractAST, Module: "packages/ui", FilesDone: 7, FilesTotal: 7, EntitiesSoFar: 88, CurrentFile: "packages/ui/button.tsx", PhaseStartedAtMS: 222, TS: 1001},
		{GroupSlug: "g", RepoSlug: "svc", Phase: progress.PhaseComputeCentrality, AlgorithmName: "PageRank", FilesDone: 10, FilesTotal: 10, EntitiesSoFar: 90, TS: 1002},
		{GroupSlug: "g", RepoSlug: "svc", Phase: progress.PhaseDone, FilesDone: 10, FilesTotal: 10, EntitiesSoFar: 90, TS: 1003},
	}

	var buf bytes.Buffer
	pub := NewStdoutProgressPublisher(&buf)
	for _, e := range in {
		pub.Publish(e)
	}

	got := &progress.SliceCollector{}
	last := parseSubprocessStdout(&buf, got, 0, nil)

	if len(got.Events) != len(in) {
		t.Fatalf("republished %d events, want %d", len(got.Events), len(in))
	}
	for i := range in {
		if !reflect.DeepEqual(got.Events[i], in[i]) {
			t.Errorf("event %d not byte-faithful:\n got  %+v\n want %+v", i, got.Events[i], in[i])
		}
	}
	// Progress lines are not lifecycle events — lastEvent stays zero-valued.
	if last.Event != "" {
		t.Errorf("lastEvent.Event = %q, want empty (progress lines are not lifecycle events)", last.Event)
	}
}

// TestParseSubprocessStdout_MixedStream verifies the parent correctly demuxes a
// realistic interleaved stream: lifecycle lines drive the returned lastEvent,
// progress lines are republished, junk is ignored.
func TestParseSubprocessStdout_MixedStream(t *testing.T) {
	// Build the stream the way the child does: start line, two module ticks, a
	// non-JSON stray line, then the done line.
	var buf bytes.Buffer
	buf.WriteString(`{"event":"index_start","repo":"/r","ref":"main"}` + "\n")
	pub := NewStdoutProgressPublisher(&buf)
	pub.Publish(progress.Event{RepoSlug: "svc", Module: "a", Phase: progress.PhaseExtractAST, FilesDone: 1, FilesTotal: 2})
	pub.Publish(progress.Event{RepoSlug: "svc", Module: "b", Phase: progress.PhaseExtractAST, FilesDone: 2, FilesTotal: 2})
	buf.WriteString("not json at all\n")
	buf.WriteString(`{"event":"index_done","repo":"/r","ref":"main"}` + "\n")

	got := &progress.SliceCollector{}
	last := parseSubprocessStdout(&buf, got, 0, nil)

	if last.Event != "index_done" || last.Repo != "/r" || last.Ref != "main" {
		t.Errorf("lastEvent = %+v, want index_done for /r@main", last)
	}
	if len(got.Events) != 2 {
		t.Fatalf("republished %d progress events, want 2", len(got.Events))
	}
	if got.Events[0].Module != "a" || got.Events[1].Module != "b" {
		t.Errorf("per-module order lost: got %q,%q want a,b", got.Events[0].Module, got.Events[1].Module)
	}
}

// TestParseSubprocessStdout_ErrorEvent verifies a child index_error line is
// surfaced as the returned lastEvent so RunSubprocessIndex can return it.
func TestParseSubprocessStdout_ErrorEvent(t *testing.T) {
	stream := strings.Join([]string{
		`{"event":"index_start","repo":"/r"}`,
		`{"event":"index_error","repo":"/r","error":"boom"}`,
	}, "\n") + "\n"

	last := parseSubprocessStdout(strings.NewReader(stream), nil, 0, nil)
	if last.Event != "index_error" || last.Error != "boom" {
		t.Errorf("lastEvent = %+v, want index_error with error=boom", last)
	}
}

// TestParseSubprocessStdout_OversizedLineDrainsToEOF is the hang guard: a single
// stdout line above the 1 MiB scanner cap makes Scan() abort early
// (bufio.ErrTooLong). The drain MUST NOT return while the child is still
// writing — it must (a) surface the scanner error (never silently swallowed) and
// (b) keep consuming the pipe to EOF so the child never blocks on a full stdout
// pipe (which would wedge cmd.Wait forever). A pathological oversized line
// degrades to dropped progress, not a deadlock.
func TestParseSubprocessStdout_OversizedLineDrainsToEOF(t *testing.T) {
	var stream bytes.Buffer
	// A valid lifecycle line first.
	stream.WriteString(`{"event":"index_start","repo":"/r"}` + "\n")
	// An oversized line (> 1 MiB, no interior newline) → bufio.ErrTooLong.
	stream.Write(bytes.Repeat([]byte("x"), 1500*1024))
	stream.WriteByte('\n')
	// More bytes AFTER the oversized line — these represent the child still
	// writing. If the drain returned early they would stay unread and the child
	// would block. The drain must consume them.
	tail := `{"event":"index_done","repo":"/r"}` + "\n"
	stream.WriteString(tail)

	r := bytes.NewReader(stream.Bytes())

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Guard against a real hang: run the drain with a hard deadline.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = parseSubprocessStdout(r, nil, 1234, logger)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("parseSubprocessStdout did not return — oversized line caused a hang")
	}

	// The whole stream was consumed to EOF (no early return leaving the child
	// blocked): the reader must be fully drained.
	if r.Len() != 0 {
		t.Errorf("reader not drained to EOF: %d bytes remain (child would block on a full pipe)", r.Len())
	}
	// The scanner error must be surfaced, not swallowed.
	logged := logBuf.String()
	if !strings.Contains(logged, "scan aborted") || !strings.Contains(logged, "too long") {
		t.Errorf("scanner error not surfaced in log; got: %q", logged)
	}
}

// TestParseSubprocessStdout_NilPublisherDropsProgress verifies the scheduler
// path (ProgressPub nil) tolerates progress lines without panicking and still
// tracks lifecycle events.
func TestParseSubprocessStdout_NilPublisherDropsProgress(t *testing.T) {
	var buf bytes.Buffer
	pub := NewStdoutProgressPublisher(&buf)
	pub.Publish(progress.Event{RepoSlug: "svc", Module: "a", Phase: progress.PhaseExtractAST})
	buf.WriteString(`{"event":"index_done","repo":"/r"}` + "\n")

	last := parseSubprocessStdout(&buf, nil, 0, nil)
	if last.Event != "index_done" {
		t.Errorf("lastEvent = %+v, want index_done", last)
	}
}
