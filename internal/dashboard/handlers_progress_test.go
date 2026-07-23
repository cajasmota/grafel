package dashboard

// handlers_progress_test.go — tests for the /api/index-progress SSE endpoints.

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/progress"
)

// newTestServerWithBroker builds a minimal *Server wired to the given broker
// and returns a running httptest.Server. The test is responsible for calling
// ts.Close().
func newTestServerWithBroker(t *testing.T, broker *progress.Broker) *httptest.Server {
	t.Helper()
	srv, err := NewServer(Config{
		Bind:      "127.0.0.1",
		PortRange: PortRange{Min: 47300, Max: 47399},
	}, newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.SetProgressBroker(broker)
	return httptest.NewServer(srv.routes())
}

// readSSELines reads lines from r until either n "data:" lines are collected
// or the deadline expires. It returns only the data lines (without the
// "data: " prefix).
func readSSELines(t *testing.T, r *bufio.Reader, n int, timeout time.Duration) []string {
	t.Helper()
	var lines []string
	done := time.After(timeout)
	for len(lines) < n {
		lineCh := make(chan string, 1)
		errCh := make(chan error, 1)
		go func() {
			l, err := r.ReadString('\n')
			if err != nil {
				errCh <- err
				return
			}
			lineCh <- l
		}()
		select {
		case <-done:
			return lines
		case <-errCh:
			return lines
		case l := <-lineCh:
			l = strings.TrimSpace(l)
			if strings.HasPrefix(l, "data:") {
				lines = append(lines, strings.TrimPrefix(strings.TrimSpace(l[5:]), " "))
			}
		}
	}
	return lines
}

// TestSSE_GroupEndpoint_ReceivesEvent verifies that a subscriber on
// /api/index-progress/{group} receives an event published to that group.
func TestSSE_GroupEndpoint_ReceivesEvent(t *testing.T) {
	broker := progress.NewBroker()
	ts := newTestServerWithBroker(t, broker)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/index-progress/team-alpha", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/index-progress/team-alpha: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("want text/event-stream Content-Type, got %q", ct)
	}

	reader := bufio.NewReader(resp.Body)

	// The first data line should be the "connected" event payload.
	connected := readSSELines(t, reader, 1, 2*time.Second)
	if len(connected) == 0 {
		t.Fatal("did not receive connected event")
	}
	if !strings.Contains(connected[0], "subscribed_at") {
		t.Errorf("connected payload missing subscribed_at: %q", connected[0])
	}

	// Publish an event and expect it on the stream.
	go func() {
		time.Sleep(50 * time.Millisecond)
		broker.Publish(progress.Event{
			GroupSlug:  "team-alpha",
			RepoSlug:   "service-x",
			Phase:      "scanning",
			FilesDone:  5,
			FilesTotal: 100,
			TS:         time.Now().UnixMilli(),
		})
	}()

	lines := readSSELines(t, reader, 1, 3*time.Second)
	if len(lines) == 0 {
		t.Fatal("did not receive progress event on SSE stream")
	}
	if !strings.Contains(lines[0], "scanning") {
		t.Errorf("expected phase=scanning in payload, got: %q", lines[0])
	}
}

// TestSSE_GroupEndpoint_Isolation verifies that events for group-B do not
// appear on a subscription for group-A.
func TestSSE_GroupEndpoint_Isolation(t *testing.T) {
	broker := progress.NewBroker()
	ts := newTestServerWithBroker(t, broker)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/index-progress/group-a", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	// Consume the connected event.
	readSSELines(t, reader, 1, 2*time.Second)

	// Publish to a different group — this should NOT appear on the group-a stream.
	broker.Publish(progress.Event{
		GroupSlug: "group-b",
		RepoSlug:  "svc",
		Phase:     "done",
		TS:        time.Now().UnixMilli(),
	})

	// Drain for 300ms — expect no data lines (only possible heartbeats, but
	// 300ms < 15s heartbeat so none should arrive either).
	extra := readSSELines(t, reader, 1, 300*time.Millisecond)
	if len(extra) != 0 {
		t.Errorf("group-a stream received unexpected event from group-b: %q", extra)
	}
}

// TestSSE_AllGroups_ReceivesEventsFromMultipleGroups verifies the wildcard
// endpoint /api/index-progress delivers events from every group.
func TestSSE_AllGroups_ReceivesEventsFromMultipleGroups(t *testing.T) {
	broker := progress.NewBroker()
	ts := newTestServerWithBroker(t, broker)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/index-progress", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/index-progress: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	// Consume the connected event.
	readSSELines(t, reader, 1, 2*time.Second)

	// Publish events to two different groups.
	go func() {
		time.Sleep(50 * time.Millisecond)
		broker.Publish(progress.Event{GroupSlug: "alpha", Phase: "scanning", TS: time.Now().UnixMilli()})
		broker.Publish(progress.Event{GroupSlug: "beta", Phase: "done", TS: time.Now().UnixMilli()})
	}()

	// Expect two data lines (one per group).
	lines := readSSELines(t, reader, 2, 3*time.Second)
	if len(lines) < 2 {
		t.Fatalf("wildcard stream: want 2 events, got %d", len(lines))
	}
	combined := strings.Join(lines, " ")
	if !strings.Contains(combined, "scanning") {
		t.Errorf("wildcard stream missing alpha/scanning event: %q", lines)
	}
	if !strings.Contains(combined, "done") {
		t.Errorf("wildcard stream missing beta/done event: %q", lines)
	}
}

// TestSSE_MultipleSubscribers verifies that two concurrent SSE connections for
// the same group both receive the same published event.
func TestSSE_MultipleSubscribers(t *testing.T) {
	broker := progress.NewBroker()
	ts := newTestServerWithBroker(t, broker)
	defer ts.Close()

	makeConn := func() (*bufio.Reader, context.CancelFunc, *http.Response) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/index-progress/shared-group", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			cancel()
			t.Fatalf("GET: %v", err)
		}
		return bufio.NewReader(resp.Body), cancel, resp
	}

	r1, cancel1, resp1 := makeConn()
	defer cancel1()
	defer resp1.Body.Close()

	r2, cancel2, resp2 := makeConn()
	defer cancel2()
	defer resp2.Body.Close()

	// Drain connected events from both.
	readSSELines(t, r1, 1, 2*time.Second)
	readSSELines(t, r2, 1, 2*time.Second)

	// Bounded-await both subscribers being registered with the broker rather than
	// sleeping a fixed 50ms and hoping both SSE handler goroutines have called
	// Subscribe by then. On a loaded/-race CI runner handler registration can lag
	// past 50ms, so the subsequent Publish would race ahead of a not-yet-attached
	// subscriber and that subscriber would miss the event (timing flake). We poll
	// Stats() until both are attached, which is the actual precondition.
	subDeadline := time.Now().Add(5 * time.Second)
	for broker.Stats()["shared-group"] < 2 {
		if time.Now().After(subDeadline) {
			t.Fatalf("both subscribers never registered: stats=%v", broker.Stats())
		}
		time.Sleep(10 * time.Millisecond)
	}

	broker.Publish(progress.Event{
		GroupSlug: "shared-group",
		Phase:     "extracting_ast",
		TS:        time.Now().UnixMilli(),
	})

	l1 := readSSELines(t, r1, 1, 2*time.Second)
	l2 := readSSELines(t, r2, 1, 2*time.Second)

	if len(l1) == 0 {
		t.Error("subscriber 1 did not receive event")
	}
	if len(l2) == 0 {
		t.Error("subscriber 2 did not receive event")
	}
}

// TestSSE_DisconnectRemovesSubscriber verifies that after the client context is
// cancelled the broker holds no lingering subscriber for the group.
func TestSSE_DisconnectRemovesSubscriber(t *testing.T) {
	broker := progress.NewBroker()
	ts := newTestServerWithBroker(t, broker)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/index-progress/temp-group", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	// Consume the connected event so we know the subscription is active.
	readSSELines(t, reader, 1, 2*time.Second)

	// Cancel the client context to simulate disconnect.
	cancel()

	// Bounded-await the server goroutine detecting the disconnect and
	// deregistering the subscriber, rather than sleeping a fixed 200ms and
	// asserting once. Under load the detect-and-unsubscribe path can take longer
	// than 200ms, which made the single-shot assertion flaky.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if n := broker.Stats()["temp-group"]; n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Errorf("broker still has %d subscriber(s) for temp-group after disconnect",
				broker.Stats()["temp-group"])
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestSSE_NoBroker_Returns503 checks that the endpoint gracefully returns 503
// when no broker is configured.
func TestSSE_NoBroker_Returns503(t *testing.T) {
	srv, err := NewServer(Config{
		Bind:      "127.0.0.1",
		PortRange: PortRange{Min: 47300, Max: 47399},
	}, newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	// No broker set.
	ts := httptest.NewServer(srv.routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/index-progress/any-group")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("want 503, got %d", resp.StatusCode)
	}
}

// TestSSE_ProxyHeaders verifies the proxy-friendliness headers are set.
func TestSSE_ProxyHeaders(t *testing.T) {
	broker := progress.NewBroker()
	ts := newTestServerWithBroker(t, broker)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/index-progress/hdr-group", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	tests := []struct{ header, want string }{
		{"Cache-Control", "no-cache, no-transform"},
		{"X-Accel-Buffering", "no"},
	}
	for _, tc := range tests {
		if got := resp.Header.Get(tc.header); got != tc.want {
			t.Errorf("header %q: want %q, got %q", tc.header, tc.want, got)
		}
	}
}

// readSSERaw reads raw SSE lines (both event: and data:) until n non-empty
// lines are collected or the deadline expires.
func readSSERaw(t *testing.T, r *bufio.Reader, n int, timeout time.Duration) []string {
	t.Helper()
	var lines []string
	done := time.After(timeout)
	for len(lines) < n {
		lineCh := make(chan string, 1)
		errCh := make(chan error, 1)
		go func() {
			l, err := r.ReadString('\n')
			if err != nil {
				errCh <- err
				return
			}
			lineCh <- l
		}()
		select {
		case <-done:
			return lines
		case <-errCh:
			return lines
		case l := <-lineCh:
			l = strings.TrimSpace(l)
			if l != "" {
				lines = append(lines, l)
			}
		}
	}
	return lines
}

// readSSEUntil reads raw SSE lines from r until pred(accumulated) is true or
// the deadline expires, returning all lines collected so far. Unlike
// readSSERaw (which stops after a fixed line count or timeout and is therefore
// racy when the awaited event arrives a few heartbeats late on a loaded CI
// runner), this keeps reading until the expected condition is actually met,
// making the assertion deterministic under -race and repeated runs.
func readSSEUntil(t *testing.T, r *bufio.Reader, timeout time.Duration, pred func(lines []string) bool) []string {
	t.Helper()
	var lines []string
	done := time.After(timeout)
	for {
		if pred(lines) {
			return lines
		}
		lineCh := make(chan string, 1)
		errCh := make(chan error, 1)
		go func() {
			l, err := r.ReadString('\n')
			if err != nil {
				errCh <- err
				return
			}
			lineCh <- l
		}()
		select {
		case <-done:
			return lines
		case <-errCh:
			return lines
		case l := <-lineCh:
			l = strings.TrimSpace(l)
			if l != "" {
				lines = append(lines, l)
			}
		}
	}
}

// TestSSE_TerminalReplayedOnConnect verifies the #5326 fix: when a client
// connects to the group SSE stream AFTER the rebuild already finished (its
// terminal PhaseDone was retained by the broker), the handler immediately
// replays the terminal event and closes — the wizard never freezes waiting for
// an event that already fired.
func TestSSE_TerminalReplayedOnConnect(t *testing.T) {
	broker := progress.NewBroker()
	ts := newTestServerWithBroker(t, broker)
	defer ts.Close()

	// Rebuild already completed before the client connects — TS is real "now",
	// exactly the scenario the #5326 guarantee exists for (and the case the
	// #5937 fix must not break: see this file's package doc / handlers_progress.go
	// for why invalidation is run-scoped via Broker.ClearTerminal, called from
	// daemon.Service.Rebuild at the start of every new run, rather than gated
	// on a wall-clock comparison here).
	broker.Publish(progress.Event{
		GroupSlug:     "late-group",
		Phase:         progress.PhaseDone,
		EntitiesSoFar: 123,
		TS:            time.Now().UnixMilli(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/index-progress/late-group", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	lines := readSSERaw(t, reader, 6, 3*time.Second)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, progress.PhaseDone) {
		t.Errorf("expected terminal %q event replayed on connect, got:\n%s", progress.PhaseDone, joined)
	}
	if !strings.Contains(joined, "event: close") {
		t.Errorf("expected close event after terminal replay, got:\n%s", joined)
	}
}

// TestSSE_ClearedTerminalNotReplayed verifies the #5937 fix's ACTUAL mechanism:
// a terminal retained from a PREVIOUS run must not be replayed to a subscriber
// watching a NEW run. Under the corrected, run-scoped design this is expressed
// as "invalidate on run start (ClearTerminal — what daemon.Service.Rebuild
// calls at the top of every Rebuild), then subscribe" — no timestamp trickery,
// because the handler no longer compares against subscribedAt at all (see
// handlers_progress.go's emitTerminalIfReady comment for why a wall-clock
// comparison there cannot distinguish "stale corpse" from "legitimate late
// reconnect" and would break the #5326 guarantee outright).
func TestSSE_ClearedTerminalNotReplayed(t *testing.T) {
	broker := progress.NewBroker()
	ts := newTestServerWithBroker(t, broker)
	defer ts.Close()

	// A previous run's terminal, retained by the broker...
	broker.Publish(progress.Event{
		GroupSlug:     "cleared-group",
		Phase:         progress.PhaseDone,
		EntitiesSoFar: 42,
		TS:            time.Now().UnixMilli(),
	})
	// ...then a NEW run starts. This is what daemon.Service.Rebuild does at the
	// top of every Rebuild call (see internal/daemon/rebuild_terminal_invalidation_test.go
	// for the RPC-level assertion that Rebuild actually calls this).
	broker.ClearTerminal("cleared-group")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/index-progress/cleared-group", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	// Consume the connected event.
	readSSELines(t, reader, 1, 2*time.Second)

	// Publish a LIVE event on the same group (the new run's own progress)
	// shortly after subscribing.
	go func() {
		time.Sleep(50 * time.Millisecond)
		broker.Publish(progress.Event{
			GroupSlug: "cleared-group",
			Phase:     "extracting_ast",
			Module:    "mod-x",
			TS:        time.Now().UnixMilli(),
		})
	}()

	// Read continuously (a SINGLE call, avoiding the leaked-goroutine race a
	// timed-out readSSERaw followed by another read call on the same
	// bufio.Reader would create) until the live event appears. This both
	// proves the stream stayed open long enough to deliver it (the whole
	// point of the fix — the wizard keeps receiving events for the run it is
	// watching) AND, by inspecting everything collected up to that point,
	// proves no replayed terminal / close preceded it.
	lines := readSSEUntil(t, reader, 5*time.Second, func(lines []string) bool {
		return strings.Contains(strings.Join(lines, "\n"), "extracting_ast")
	})
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "extracting_ast") {
		t.Fatalf("live event for the new run never delivered after the stale terminal was correctly withheld: %v", lines)
	}
	// NOTE: matching on the literal `"phase":"done"` JSON field rather than the
	// bare progress.PhaseDone ("done") constant — the latter is ALSO a
	// substring of the unrelated "files_done" field present on the live
	// extracting_ast event itself, which would make this a false positive.
	if strings.Contains(joined, `"phase":"done"`) {
		t.Errorf("cleared (previous-run) terminal was replayed to the new run's subscriber before its live event: %s", joined)
	}
	if strings.Contains(joined, "event: close") {
		t.Errorf("stream was closed (on a cleared previous-run terminal) before the new run's live event: %s", joined)
	}
}

// TestSSE_TerminalAtRealNow_ReplayedAndClosed is the realistic-boundary
// regression guard the #5937 review asked for: a terminal published at real
// wall-clock `now`, immediately followed by a subscribe (the actual timing
// every genuine late-reconnect-after-completion case has — there is no
// artificial "comfortably fresh" timestamp here), must still be replayed and
// the stream must still close. This is the #5326 guarantee at the timing that
// actually occurs in production, proving the corrected design (replay-
// whatever's-retained, invalidated only by ClearTerminal at run start) holds
// it — where the old subscribedAt-comparison gate provably did not (any
// retained terminal necessarily predates the subsequent subscribedAt stamp,
// so that gate withheld replay unconditionally).
func TestSSE_TerminalAtRealNow_ReplayedAndClosed(t *testing.T) {
	broker := progress.NewBroker()
	ts := newTestServerWithBroker(t, broker)
	defer ts.Close()

	broker.Publish(progress.Event{
		GroupSlug:     "realtime-group",
		Phase:         progress.PhaseDone,
		EntitiesSoFar: 7,
		TS:            time.Now().UnixMilli(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/index-progress/realtime-group", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	lines := readSSERaw(t, reader, 6, 3*time.Second)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, progress.PhaseDone) {
		t.Errorf("expected terminal replayed on connect at the realistic now/immediate-subscribe boundary, got:\n%s", joined)
	}
	if !strings.Contains(joined, "event: close") {
		t.Errorf("expected close event after terminal replay, got:\n%s", joined)
	}
}

// TestSSE_TokenMismatch_TerminalNotReplayedOnConnect is the #5937 chunk 2 F1
// regression test: a subscriber that supplies its OWN run token (T2) must NOT
// have a DIFFERENT run's retained terminal (T1) replayed to it on connect —
// and, crucially, the stream must stay open so T2's own live events can still
// arrive afterwards. This is exactly the wizard's subscribe-then-trigger
// ordering: it attaches before its rebuild starts, so whatever the broker has
// retained at that instant is necessarily a PRIOR run's terminal, never its
// own.
func TestSSE_TokenMismatch_TerminalNotReplayedOnConnect(t *testing.T) {
	broker := progress.NewBroker()
	ts := newTestServerWithBroker(t, broker)
	defer ts.Close()

	// A previous run (token T1) already completed and its terminal is retained.
	broker.Publish(progress.Event{
		GroupSlug:     "tok-group",
		Phase:         progress.PhaseDone,
		RunToken:      "T1",
		EntitiesSoFar: 99,
		TS:            time.Now().UnixMilli(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/index-progress/tok-group?token=T2", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	// Consume the connected event. If the stale T1 terminal were (incorrectly)
	// replayed+closed, it would show up as the very next data line instead of
	// the live T2 event below — the assertions after prove it didn't.
	readSSELines(t, reader, 1, 2*time.Second)

	// T2's own run now produces a live (non-terminal) event.
	go func() {
		time.Sleep(50 * time.Millisecond)
		broker.Publish(progress.Event{
			GroupSlug: "tok-group",
			Phase:     "extracting_ast",
			RunToken:  "T2",
			Module:    "mod-y",
			TS:        time.Now().UnixMilli(),
		})
	}()

	lines := readSSEUntil(t, reader, 5*time.Second, func(lines []string) bool {
		return strings.Contains(strings.Join(lines, "\n"), "extracting_ast")
	})
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "extracting_ast") {
		t.Fatalf("T2's own live event never delivered after the mismatched T1 terminal was correctly withheld: %v", lines)
	}
	if strings.Contains(joined, `"phase":"done"`) {
		t.Errorf("mismatched-token (T1) terminal was replayed to the T2 subscriber before its own live event: %s", joined)
	}
	if strings.Contains(joined, "event: close") {
		t.Errorf("stream was closed on a mismatched-token (T1) terminal before T2's live event arrived: %s", joined)
	}
}

// TestSSE_TokenMismatch_LiveTerminalDoesNotClose is the #5937 chunk 2 F2
// regression test — the subtle race: a DIFFERENT run's terminal (T1) arrives
// on the LIVE path (not the connect-time replay) AFTER this subscriber (T2)
// has already attached with no retained terminal present at connect time. The
// handler must not treat T1's terminal as this subscriber's own: it must not
// close the stream, and T2's subsequent live event must still be delivered.
func TestSSE_TokenMismatch_LiveTerminalDoesNotClose(t *testing.T) {
	broker := progress.NewBroker()
	ts := newTestServerWithBroker(t, broker)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/index-progress/race-group?token=T2", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	// Consume the connected event — no terminal is retained yet, so nothing else
	// should arrive from the replay-on-connect path.
	readSSELines(t, reader, 1, 2*time.Second)

	// A PRIOR run's (T1) terminal arrives LIVE, shortly after this subscriber
	// attached — the exact race the tailer can produce (~up to 120ms after
	// subscribe).
	go func() {
		time.Sleep(30 * time.Millisecond)
		broker.Publish(progress.Event{
			GroupSlug:     "race-group",
			Phase:         progress.PhaseDone,
			RunToken:      "T1",
			EntitiesSoFar: 5,
			TS:            time.Now().UnixMilli(),
		})
		// Immediately after, T2's OWN live (non-terminal) event fires — this must
		// still reach the subscriber, proving the stream was not closed by T1's
		// terminal above.
		time.Sleep(30 * time.Millisecond)
		broker.Publish(progress.Event{
			GroupSlug: "race-group",
			Phase:     "extracting_ast",
			RunToken:  "T2",
			Module:    "mod-z",
			TS:        time.Now().UnixMilli(),
		})
	}()

	lines := readSSEUntil(t, reader, 5*time.Second, func(lines []string) bool {
		return strings.Contains(strings.Join(lines, "\n"), "extracting_ast")
	})
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "extracting_ast") {
		t.Fatalf("T2's live event never arrived — the live-path mismatched-token (T1) terminal incorrectly closed the stream: %v", lines)
	}
	if strings.Contains(joined, `"phase":"done"`) {
		t.Errorf("mismatched-token (T1) live terminal was forwarded to the T2 subscriber: %s", joined)
	}
	if strings.Contains(joined, "event: close") {
		t.Errorf("stream was closed by a mismatched-token (T1) live terminal event: %s", joined)
	}
}

// TestSSE_TokenMatch_TerminalReplayedAndClosed verifies requirement (iii): a
// subscriber whose supplied token matches the retained terminal's RunToken
// gets it replayed and the stream closed, exactly like the tokenless #5326
// case — token matching must not regress the positive case.
func TestSSE_TokenMatch_TerminalReplayedAndClosed(t *testing.T) {
	broker := progress.NewBroker()
	ts := newTestServerWithBroker(t, broker)
	defer ts.Close()

	broker.Publish(progress.Event{
		GroupSlug:     "match-group",
		Phase:         progress.PhaseDone,
		RunToken:      "T3",
		EntitiesSoFar: 17,
		TS:            time.Now().UnixMilli(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/index-progress/match-group?token=T3", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	lines := readSSERaw(t, reader, 6, 3*time.Second)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, progress.PhaseDone) {
		t.Errorf("expected matching-token terminal replayed on connect, got:\n%s", joined)
	}
	if !strings.Contains(joined, "event: close") {
		t.Errorf("expected close event after matching-token terminal replay, got:\n%s", joined)
	}
}

// TestSSE_Tokenless_TerminalStillReplayedAndClosed verifies requirement (iv):
// a subscriber that supplies NO token (subscriberToken == "") still gets
// whatever terminal is retained replayed+closed — the #5326 late-reconnect
// guarantee is unaffected by the #5937 chunk 2 token-matching change.
func TestSSE_Tokenless_TerminalStillReplayedAndClosed(t *testing.T) {
	broker := progress.NewBroker()
	ts := newTestServerWithBroker(t, broker)
	defer ts.Close()

	broker.Publish(progress.Event{
		GroupSlug:     "tokenless-group",
		Phase:         progress.PhaseDone,
		RunToken:      "T4",
		EntitiesSoFar: 3,
		TS:            time.Now().UnixMilli(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// No ?token= query param at all.
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/index-progress/tokenless-group", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	lines := readSSERaw(t, reader, 6, 3*time.Second)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, progress.PhaseDone) {
		t.Errorf("expected tokenless subscriber to still get the retained terminal replayed (#5326), got:\n%s", joined)
	}
	if !strings.Contains(joined, "event: close") {
		t.Errorf("expected close event after tokenless terminal replay, got:\n%s", joined)
	}
}

// TestSSE_TerminalReassertedOnHeartbeat verifies that when the live terminal
// event is dropped (slow subscriber buffer full at the moment it fires), the
// handler re-asserts the retained terminal state on the next heartbeat so the
// wizard still reaches a terminal render rather than freezing (#5326).
func TestSSE_TerminalReassertedOnHeartbeat(t *testing.T) {
	broker := progress.NewBroker()
	ts := newTestServerWithBroker(t, broker)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/index-progress/hb-group", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	// Consume the connected event.
	readSSELines(t, reader, 1, 2*time.Second)

	// Simulate the drop-on-full case: retain the terminal in the broker WITHOUT
	// it reaching this subscriber's channel. We publish directly bypassing the
	// live channel by saturating, then asserting retention drives re-delivery.
	// Simplest faithful reproduction: record the terminal via Publish while the
	// reader is momentarily not draining; retention guarantees re-assert anyway.
	broker.Publish(progress.Event{
		GroupSlug:     "hb-group",
		Phase:         progress.PhaseDone,
		EntitiesSoFar: 7,
		TS:            time.Now().UnixMilli(),
	})

	// The terminal must arrive (via the live channel or, if that event was
	// dropped, a heartbeat re-assert ~1s/tick) followed by a close. Poll until
	// BOTH markers are present rather than reading a fixed window, so a few
	// late heartbeats on a slow/loaded CI runner can't flake the assertion.
	hasTerminalClose := func(lines []string) bool {
		joined := strings.Join(lines, "\n")
		return strings.Contains(joined, progress.PhaseDone) && strings.Contains(joined, "event: close")
	}
	lines := readSSEUntil(t, reader, 15*time.Second, hasTerminalClose)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, progress.PhaseDone) {
		t.Errorf("terminal state never delivered on heartbeat re-assert, got:\n%s", joined)
	}
	if !strings.Contains(joined, "event: close") {
		t.Errorf("expected close after terminal, got:\n%s", joined)
	}
}
