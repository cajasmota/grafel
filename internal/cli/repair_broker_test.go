package cli

// Tests for broker-backed progress rendering (Sub-E of epic #1118).
// These tests exercise renderBrokerEvent and readSSEEvents without a live
// daemon — all transport is stubbed with in-memory readers and writers.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/progress"
)

// ---------------------------------------------------------------------------
// renderBrokerEvent — plain mode (no ANSI, no TTY)
// ---------------------------------------------------------------------------

func renderBrokerEventPlain(e progress.Event) string {
	var buf bytes.Buffer
	// plain=true, tty=false, jsonEvents=false
	renderBrokerEvent(&buf, e, map[string]int{}, false, true, false)
	return buf.String()
}

func TestRenderBrokerEvent_Scan(t *testing.T) {
	e := progress.Event{
		GroupSlug:  "mygroup",
		RepoSlug:   "core-api",
		Phase:      progress.PhaseScan,
		FilesTotal: 342,
	}
	out := renderBrokerEventPlain(e)
	if !strings.Contains(out, "core-api") {
		t.Errorf("missing slug: %q", out)
	}
	if !strings.Contains(out, "scanning") {
		t.Errorf("missing 'scanning': %q", out)
	}
	if !strings.Contains(out, "342") {
		t.Errorf("missing file count: %q", out)
	}
}

func TestRenderBrokerEvent_ExtractAST_WithFile(t *testing.T) {
	e := progress.Event{
		GroupSlug:   "g",
		RepoSlug:    "backend",
		Phase:       progress.PhaseExtractAST,
		FilesDone:   47,
		FilesTotal:  100,
		CurrentFile: "routers.py",
	}
	out := renderBrokerEventPlain(e)
	if !strings.Contains(out, "backend") {
		t.Errorf("missing slug: %q", out)
	}
	if !strings.Contains(out, "extracting_ast") {
		t.Errorf("missing phase: %q", out)
	}
	if !strings.Contains(out, "47") {
		t.Errorf("missing files_done: %q", out)
	}
	if !strings.Contains(out, "100") {
		t.Errorf("missing files_total: %q", out)
	}
	if !strings.Contains(out, "routers.py") {
		t.Errorf("missing current_file: %q", out)
	}
}

func TestRenderBrokerEvent_ExtractAST_NoFile(t *testing.T) {
	e := progress.Event{
		GroupSlug:  "g",
		RepoSlug:   "frontend",
		Phase:      progress.PhaseExtractAST,
		FilesDone:  10,
		FilesTotal: 200,
	}
	out := renderBrokerEventPlain(e)
	if !strings.Contains(out, "extracting_ast") {
		t.Errorf("missing phase: %q", out)
	}
	if !strings.Contains(out, "5%") {
		t.Errorf("missing pct (10/200=5%%): %q", out)
	}
}

func TestRenderBrokerEvent_ResolveRefs(t *testing.T) {
	e := progress.Event{
		RepoSlug: "svc",
		Phase:    progress.PhaseResolveRefs,
	}
	out := renderBrokerEventPlain(e)
	if !strings.Contains(out, "resolving_refs") {
		t.Errorf("missing phase: %q", out)
	}
}

func TestRenderBrokerEvent_Algorithms_Named(t *testing.T) {
	e := progress.Event{
		RepoSlug:      "svc",
		Phase:         progress.PhaseAlgorithms,
		AlgorithmName: "PageRank",
	}
	out := renderBrokerEventPlain(e)
	if !strings.Contains(out, "running_algorithms") {
		t.Errorf("missing phase: %q", out)
	}
	if !strings.Contains(out, "PageRank") {
		t.Errorf("missing algorithm name: %q", out)
	}
}

// TestRenderBrokerEvent_GranularPhases verifies the #5334 graph-assembly
// phases render with their friendly labels in both plain (non-TTY) and TTY
// (ANSI) modes. The label text must match the dashboard wizard's PHASE_LABEL.
func TestRenderBrokerEvent_GranularPhases(t *testing.T) {
	cases := []struct {
		phase string
		want  string
	}{
		{progress.PhaseBuildCommunities, "Building communities…"},
		{progress.PhaseComputeCentrality, "Computing centrality…"},
		{progress.PhaseDetectLinks, "Detecting cross-repo links…"},
		{progress.PhaseComputeFlows, "Computing flows…"},
		{progress.PhaseWriteGraph, "Writing graph…"},
	}
	for _, tc := range cases {
		t.Run(tc.phase, func(t *testing.T) {
			e := progress.Event{RepoSlug: "svc", Phase: tc.phase}

			// plain / non-TTY: friendly label, no ANSI escapes.
			plain := renderBrokerEventPlain(e)
			if !strings.Contains(plain, tc.want) {
				t.Errorf("plain: missing label %q in %q", tc.want, plain)
			}
			if strings.Contains(plain, "\033[") {
				t.Errorf("plain: unexpected ANSI escape in %q", plain)
			}

			// TTY: friendly label present (ANSI allowed).
			var buf bytes.Buffer
			renderBrokerEvent(&buf, e, map[string]int{}, true, false, false)
			if !strings.Contains(buf.String(), tc.want) {
				t.Errorf("tty: missing label %q in %q", tc.want, buf.String())
			}
		})
	}
}

func TestRenderBrokerEvent_Materialize(t *testing.T) {
	e := progress.Event{
		RepoSlug: "svc",
		Phase:    progress.PhaseMaterialize,
	}
	out := renderBrokerEventPlain(e)
	if !strings.Contains(out, "materializing") {
		t.Errorf("missing phase: %q", out)
	}
}

func TestRenderBrokerEvent_Done_WithEntities(t *testing.T) {
	e := progress.Event{
		RepoSlug:      "svc",
		Phase:         progress.PhaseDone,
		EntitiesSoFar: 4821,
	}
	out := renderBrokerEventPlain(e)
	if !strings.Contains(out, "done") {
		t.Errorf("missing 'done': %q", out)
	}
	if !strings.Contains(out, "4,821") {
		t.Errorf("missing entity count: %q", out)
	}
}

func TestRenderBrokerEvent_Done_NoEntities(t *testing.T) {
	e := progress.Event{
		RepoSlug: "svc",
		Phase:    progress.PhaseDone,
	}
	out := renderBrokerEventPlain(e)
	if !strings.Contains(out, "done") {
		t.Errorf("missing 'done': %q", out)
	}
}

func TestRenderBrokerEvent_Error(t *testing.T) {
	e := progress.Event{
		RepoSlug: "broken",
		Phase:    progress.PhaseError,
		Error:    "permission denied",
	}
	out := renderBrokerEventPlain(e)
	if !strings.Contains(out, "error") {
		t.Errorf("missing 'error': %q", out)
	}
	if !strings.Contains(out, "permission denied") {
		t.Errorf("missing error message: %q", out)
	}
}

func TestRenderBrokerEvent_FallsBackToGroupSlug(t *testing.T) {
	// When RepoSlug is empty, GroupSlug is used.
	e := progress.Event{
		GroupSlug: "mygroup",
		RepoSlug:  "",
		Phase:     progress.PhaseScan,
	}
	out := renderBrokerEventPlain(e)
	if !strings.Contains(out, "mygroup") {
		t.Errorf("expected group slug as fallback: %q", out)
	}
}

// ---------------------------------------------------------------------------
// renderBrokerEvent — JSON mode
// ---------------------------------------------------------------------------

func TestRenderBrokerEvent_JSONMode(t *testing.T) {
	e := progress.Event{
		GroupSlug:   "g",
		RepoSlug:    "api",
		Phase:       progress.PhaseExtractAST,
		FilesDone:   5,
		FilesTotal:  50,
		CurrentFile: "main.go",
	}
	var buf bytes.Buffer
	renderBrokerEvent(&buf, e, map[string]int{}, false, false, true)

	var m map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, buf.String())
	}
	if m["phase"] != progress.PhaseExtractAST {
		t.Errorf("phase = %v, want %q", m["phase"], progress.PhaseExtractAST)
	}
	if m["repo_slug"] != "api" {
		t.Errorf("repo_slug = %v, want 'api'", m["repo_slug"])
	}
	if m["current_file"] != "main.go" {
		t.Errorf("current_file = %v, want 'main.go'", m["current_file"])
	}
}

// ---------------------------------------------------------------------------
// ANSI color — TTY mode
// ---------------------------------------------------------------------------

func TestRenderBrokerEvent_ANSIColors_Done(t *testing.T) {
	e := progress.Event{
		RepoSlug: "svc",
		Phase:    progress.PhaseDone,
	}
	var buf bytes.Buffer
	// tty=true, plain=false, jsonEvents=false
	renderBrokerEvent(&buf, e, map[string]int{}, true, false, false)
	out := buf.String()
	// Green ANSI code for done.
	if !strings.Contains(out, ansiGreen) {
		t.Errorf("expected green ANSI for done: %q", out)
	}
	if !strings.Contains(out, ansiReset) {
		t.Errorf("expected ANSI reset: %q", out)
	}
}

func TestRenderBrokerEvent_ANSIColors_Error(t *testing.T) {
	e := progress.Event{
		RepoSlug: "svc",
		Phase:    progress.PhaseError,
		Error:    "crash",
	}
	var buf bytes.Buffer
	renderBrokerEvent(&buf, e, map[string]int{}, true, false, false)
	out := buf.String()
	if !strings.Contains(out, ansiRed) {
		t.Errorf("expected red ANSI for error: %q", out)
	}
}

func TestRenderBrokerEvent_ANSIColors_InProgress(t *testing.T) {
	e := progress.Event{
		RepoSlug: "svc",
		Phase:    progress.PhaseExtractAST,
	}
	var buf bytes.Buffer
	renderBrokerEvent(&buf, e, map[string]int{}, true, false, false)
	out := buf.String()
	if !strings.Contains(out, ansiYellow) {
		t.Errorf("expected yellow ANSI for in-progress: %q", out)
	}
}

func TestColorize_Plain_NoANSI(t *testing.T) {
	text := "hello"
	out := colorize(text, ansiGreen, false, true)
	if out != text {
		t.Errorf("colorize plain should be identity: got %q", out)
	}
}

func TestColorize_TTY_AddsColor(t *testing.T) {
	text := "hello"
	out := colorize(text, ansiGreen, true, false)
	if !strings.HasPrefix(out, ansiGreen) {
		t.Errorf("colorize TTY should add prefix: got %q", out)
	}
	if !strings.HasSuffix(out, ansiReset) {
		t.Errorf("colorize TTY should add reset suffix: got %q", out)
	}
}

// ---------------------------------------------------------------------------
// readSSEEvents — SSE parser
// ---------------------------------------------------------------------------

func TestReadSSEEvents_ParsesNameAndData(t *testing.T) {
	raw := "event: progress\ndata: {\"phase\":\"scanning\"}\n\n" +
		"event: heartbeat\ndata: {}\n\n"

	ch := make(chan sseEvent, 8)
	ctx := context.Background()
	go func() {
		readSSEEvents(ctx, strings.NewReader(raw), ch)
		close(ch)
	}()

	var events []sseEvent
	for ev := range ch {
		events = append(events, ev)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d: %+v", len(events), events)
	}
	if events[0].name != "progress" {
		t.Errorf("event[0].name = %q, want 'progress'", events[0].name)
	}
	if !strings.Contains(events[0].data, "scanning") {
		t.Errorf("event[0].data missing 'scanning': %q", events[0].data)
	}
	if events[1].name != "heartbeat" {
		t.Errorf("event[1].name = %q, want 'heartbeat'", events[1].name)
	}
}

func TestReadSSEEvents_EmptyLinesAsBoundaries(t *testing.T) {
	// Three events separated by blank lines.
	raw := "event: connected\ndata: {}\n\n" +
		"event: progress\ndata: {\"phase\":\"done\"}\n\n" +
		"event: close\ndata: {}\n\n"

	ch := make(chan sseEvent, 8)
	ctx := context.Background()
	go func() {
		readSSEEvents(ctx, strings.NewReader(raw), ch)
		close(ch)
	}()

	count := 0
	for range ch {
		count++
	}
	if count != 3 {
		t.Errorf("expected 3 events, got %d", count)
	}
}

func TestReadSSEEvents_ContextCancellation(t *testing.T) {
	// Infinite stream — context cancels after first event.
	raw := "event: progress\ndata: {}\n\n" +
		"event: progress\ndata: {}\n\n" +
		"event: progress\ndata: {}\n\n"

	ch := make(chan sseEvent, 1) // capacity 1 so second event blocks
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Use a slow reader by wrapping with bufio and a scanner.
		readSSEEvents(ctx, strings.NewReader(raw), ch)
	}()

	// Drain first event and cancel.
	<-ch
	cancel()

	// goroutine should exit without hanging.
	select {
	case <-done:
	case <-waitFor(50):
		// Context cancellation check fires only at event boundaries,
		// so we may get a second event before the goroutine notices.
	}
}

// waitFor returns a channel that closes after ~n*10ms for testing timeouts.
func waitFor(steps int) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(strings.NewReader(strings.Repeat("x\n", steps*1000)))
		for scanner.Scan() {
		}
		close(ch)
	}()
	return ch
}
