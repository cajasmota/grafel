package sched

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

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
