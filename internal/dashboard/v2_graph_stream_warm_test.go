package dashboard

// v2_graph_stream_warm_test.go — tests for the BOUNDED warm-during-stream
// behaviour (#48). A cold large group must no longer answer the stream endpoint
// with a bare 503 (which makes the browser eventually give up and fall back to
// the uncapped, blocking full-payload blob). Instead the stream endpoint keeps
// the SSE connection open, emits `warming` heartbeats while a bounded blocking
// warm proceeds, and then streams the graph — or surfaces a genuine failure /
// timeout as a distinguishable `error` event.

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/registry"
)

// --- unit: waitForWarmGroup ---------------------------------------------------

func TestWaitForWarmGroup_EmitsWarmingThenReady(t *testing.T) {
	// Cold for the first few polls, then a background warm "completes".
	var polls int
	warmAt := 3
	grp := &DashGroup{Name: "g"}
	cached := func() (*DashGroup, bool) {
		polls++
		if polls >= warmAt {
			return grp, true
		}
		return nil, false
	}
	noErr := func() (error, bool) { return nil, false }

	var heartbeats int
	got, err, outcome := waitForWarmGroup(cached, noErr, func(time.Duration) { heartbeats++ }, 2*time.Second, 1*time.Millisecond)
	if outcome != warmReady {
		t.Fatalf("outcome = %v, want warmReady", outcome)
	}
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if got != grp {
		t.Fatalf("returned group %p, want %p", got, grp)
	}
	if heartbeats < 1 {
		t.Fatalf("expected at least one warming heartbeat, got %d", heartbeats)
	}
}

func TestWaitForWarmGroup_SurfacesFailure(t *testing.T) {
	cold := func() (*DashGroup, bool) { return nil, false }
	sentinel := errors.New("config file does not exist")
	failed := func() (error, bool) { return sentinel, true }

	got, err, outcome := waitForWarmGroup(cold, failed, nil, 2*time.Second, 1*time.Millisecond)
	if outcome != warmFailed {
		t.Fatalf("outcome = %v, want warmFailed", outcome)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want sentinel", err)
	}
	if got != nil {
		t.Fatalf("group = %v, want nil on failure", got)
	}
}

func TestWaitForWarmGroup_TimesOut(t *testing.T) {
	cold := func() (*DashGroup, bool) { return nil, false }
	noErr := func() (error, bool) { return nil, false }

	start := time.Now()
	got, _, outcome := waitForWarmGroup(cold, noErr, nil, 40*time.Millisecond, 5*time.Millisecond)
	if outcome != warmTimedOut {
		t.Fatalf("outcome = %v, want warmTimedOut", outcome)
	}
	if got != nil {
		t.Fatalf("group = %v, want nil on timeout", got)
	}
	// 5s, not 500ms. The assertion that matters is outcome == warmTimedOut
	// above; this bound only catches waitForWarmGroup ignoring its deadline
	// entirely (i.e. never returning). 500ms is 12x a 40ms deadline, which a
	// contended runner can close; 5s cannot be reached by a poll loop that
	// honours 40ms at all.
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("timeout took %v — waitForWarmGroup ignored its 40ms deadline", elapsed)
	}
}

// --- handler: a cold-but-warmable group streams instead of 503ing ------------

// TestGraphStream_ColdGroupWarmsAndStreams registers a group with a VALID
// config (so the background warm succeeds) but leaves the cache cold. The
// stream request must stay a 200 SSE stream — connected → warming* → meta →
// done — and never return the bare 503 that used to trip the blob fallback.
func TestGraphStream_ColdGroupWarmsAndStreams(t *testing.T) {
	// Shrink the warm cadence so the test is fast and deterministically emits
	// at least one `warming` heartbeat before the warm completes.
	restore := setStreamWarmTimingForTest(2*time.Second, 2*time.Millisecond)
	defer restore()

	archHome := t.TempDir()
	daemonRoot := t.TempDir()
	t.Setenv("GRAFEL_HOME", archHome)
	t.Setenv("GRAFEL_DAEMON_ROOT", daemonRoot)

	configDir := filepath.Join(archHome, "configs")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir configs: %v", err)
	}
	// A structurally valid config listing one repo whose graph.fb does not
	// exist. loadGroupForRef records the per-repo load miss but returns a
	// (repo-less) group with NO top-level error, so the warm SUCCEEDS.
	configPath := filepath.Join(configDir, "testgrp.fleet.json")
	// Build the fixture via json.Marshal rather than string-concatenating the
	// path into a raw JSON literal: on Windows filepath.Join produces
	// backslash-separated paths, and `\n`/`\t`-style sequences inside an
	// unescaped path (e.g. "...\norepo" containing a literal `\n`) form an
	// invalid JSON escape when pasted straight into a string literal.
	// json.Marshal escapes the path correctly regardless of platform.
	cfg := struct {
		Name  string `json:"name"`
		Repos []struct {
			Slug string `json:"slug"`
			Path string `json:"path"`
		} `json:"repos"`
	}{
		Name: "testgrp",
		Repos: []struct {
			Slug string `json:"slug"`
			Path string `json:"path"`
		}{
			{Slug: "testrepo", Path: filepath.Join(daemonRoot, "norepo")},
		},
	}
	body, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config fixture: %v", err)
	}
	if err := os.WriteFile(configPath, body, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := registry.AddGroup("testgrp", configPath); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}

	st := newFakeStore()
	srv, err := NewServer(DefaultConfig(), st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	// Cache is deliberately cold — no entries injected.

	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/v2/graph/testgrp/stream")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cold group status = %d, want 200 (SSE stream, not a 503)", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	events := readSSE(t, string(b))
	if len(events) == 0 {
		t.Fatalf("no SSE events received")
	}
	if events[0].Type != "connected" {
		t.Fatalf("first event = %q, want connected", events[0].Type)
	}
	// The stream must terminate cleanly with `done` (the warm succeeded) and
	// must include a `meta` event. It must NOT surface an `error`.
	sawMeta, sawDone, sawError := false, false, false
	for _, ev := range events {
		switch ev.Type {
		case "meta":
			sawMeta = true
		case "done":
			sawDone = true
		case "error":
			sawError = true
		}
	}
	if sawError {
		t.Fatalf("cold-but-warmable group surfaced an error event: %+v", events)
	}
	if !sawMeta {
		t.Fatalf("no meta event; events=%+v", events)
	}
	if !sawDone {
		t.Fatalf("no done event; events=%+v", events)
	}
}

// TestGraphStream_ColdUnregisterableGroupSurfacesError verifies the #5722
// contract still holds under the new warm-during-stream flow: a group that can
// never warm (not registered) resolves to a distinguishable SSE `error` event
// on a 200 stream — NOT an eternal 503, and NOT a spurious success.
func TestGraphStream_ColdUnregisterableGroupSurfacesError(t *testing.T) {
	restore := setStreamWarmTimingForTest(2*time.Second, 2*time.Millisecond)
	defer restore()

	st := newFakeStore()
	st.groups["testgrp"] = GroupSummary{
		Name: "testgrp", ConfigPath: "/tmp/testgrp.json", Repos: []string{"testrepo"},
	}
	srv, err := NewServer(DefaultConfig(), st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	// testgrp is NOT in the registry package, so the background warm fails.

	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/v2/graph/testgrp/stream")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 SSE so EventSource can read the error", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	events := readSSE(t, string(b))
	var errEv *sseEvent
	for i := range events {
		if events[i].Type == "error" {
			errEv = &events[i]
			break
		}
	}
	if errEv == nil {
		t.Fatalf("no error event for an unregisterable cold group; events=%+v", events)
	}
	var payload struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(errEv.Data), &payload); err != nil {
		t.Fatalf("error payload unmarshal: %v", err)
	}
	if payload.Code == "" || payload.Message == "" {
		t.Fatalf("error payload incomplete: %+v", payload)
	}
	time.Sleep(10 * time.Millisecond)
}

// TestHighLodNodeCapKnob documents the single high/full LoD cap knob and its
// current unlimited value, so a change to a finite cap is caught here.
func TestHighLodNodeCapKnob(t *testing.T) {
	if highLodNodeCap != 0 {
		t.Fatalf("highLodNodeCap = %d; if you intentionally switched high LoD to a finite cap, update this test", highLodNodeCap)
	}
	if got := lodNodeCap("high"); got != highLodNodeCap {
		t.Fatalf("lodNodeCap(high) = %d, want highLodNodeCap=%d", got, highLodNodeCap)
	}
	if got := lodNodeCap("full"); got != highLodNodeCap {
		t.Fatalf("lodNodeCap(full) = %d, want highLodNodeCap=%d", got, highLodNodeCap)
	}
}
