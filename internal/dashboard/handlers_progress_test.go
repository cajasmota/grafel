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

	"github.com/cajasmota/archigraph/internal/progress"
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

	// Give the broker a moment to register both subscribers.
	time.Sleep(50 * time.Millisecond)

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

	// Allow the server goroutine to detect the disconnect and call cancel().
	time.Sleep(200 * time.Millisecond)

	stats := broker.Stats()
	if n := stats["temp-group"]; n != 0 {
		t.Errorf("broker still has %d subscriber(s) for temp-group after disconnect", n)
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
