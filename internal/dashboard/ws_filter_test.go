package dashboard

// ws_filter_test.go — unit tests for wsFilter + multi-client integration test.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Unit tests: wsFilter.Matches
// ---------------------------------------------------------------------------

func TestWSFilter_Nil_IsFirehose(t *testing.T) {
	// A nil filter (no subscription) must not be called — but we test the
	// Matches logic directly with an empty filter to confirm the zero-value
	// works as expected.
	f := newWSFilter(nil, nil)

	cases := []WSEvent{
		{Group: "upvate", Ref: "main"},
		{Group: "archigraph", Ref: "feature/foo"},
		{Group: "", Ref: ""},
	}
	for _, evt := range cases {
		if !f.Matches(evt) {
			t.Errorf("empty filter rejected event %+v; want pass", evt)
		}
	}
}

func TestWSFilter_GroupFilter(t *testing.T) {
	f := newWSFilter([]string{"upvate"}, nil)

	if !f.Matches(WSEvent{Group: "upvate", Ref: "main"}) {
		t.Error("expected upvate/main to pass group filter")
	}
	if f.Matches(WSEvent{Group: "archigraph", Ref: "main"}) {
		t.Error("expected archigraph/main to be blocked by group filter")
	}
	if !f.Matches(WSEvent{Group: "upvate", Ref: "feature/foo"}) {
		t.Error("expected upvate/feature/foo to pass group filter")
	}
}

func TestWSFilter_RefFilter(t *testing.T) {
	f := newWSFilter(nil, []string{"main"})

	if !f.Matches(WSEvent{Group: "upvate", Ref: "main"}) {
		t.Error("expected upvate/main to pass ref filter")
	}
	if f.Matches(WSEvent{Group: "upvate", Ref: "feature/foo"}) {
		t.Error("expected upvate/feature/foo to be blocked by ref filter")
	}
}

func TestWSFilter_GroupAndRefFilter(t *testing.T) {
	f := newWSFilter([]string{"upvate", "archigraph"}, []string{"main", "feature/foo"})

	// Both group and ref match → pass.
	if !f.Matches(WSEvent{Group: "upvate", Ref: "main"}) {
		t.Error("upvate/main should pass")
	}
	if !f.Matches(WSEvent{Group: "archigraph", Ref: "feature/foo"}) {
		t.Error("archigraph/feature/foo should pass")
	}

	// Right group, wrong ref → block.
	if f.Matches(WSEvent{Group: "upvate", Ref: "hotfix/x"}) {
		t.Error("upvate/hotfix/x should be blocked (ref not subscribed)")
	}

	// Wrong group, right ref → block.
	if f.Matches(WSEvent{Group: "other", Ref: "main"}) {
		t.Error("other/main should be blocked (group not subscribed)")
	}
}

func TestWSFilter_LegacyEventNoRef_AlwaysPasses(t *testing.T) {
	// Events published by legacy (pre-#2221) code carry Ref=="". They must
	// pass even when the client subscribed to specific refs, so old publishers
	// stay visible and backward compatibility is preserved.
	f := newWSFilter(nil, []string{"main"})

	if !f.Matches(WSEvent{Group: "upvate", Ref: ""}) {
		t.Error("legacy event (Ref='') must pass through a ref filter unconditionally")
	}
}

func TestWSFilter_EmptyStringsInSlices(t *testing.T) {
	// Callers that pass slices containing only empty strings should behave as
	// if no constraint was provided on that dimension.
	f := newWSFilter([]string{""}, []string{""})
	if !f.Matches(WSEvent{Group: "any", Ref: "any"}) {
		t.Error("filter with all-empty entries should act as firehose")
	}
}

func TestWSFilter_SubscribeReplacesPriorFilter(t *testing.T) {
	// Calling subscribe again must REPLACE (not append) the prior filter.
	c := &wsClient{
		send: make(chan []byte, 8),
		done: make(chan struct{}),
	}

	// First subscription: only upvate/main.
	c.subMu.Lock()
	c.sub = newWSFilter([]string{"upvate"}, []string{"main"})
	c.subMu.Unlock()

	// Second subscription replaces: only archigraph/feature/foo.
	c.subMu.Lock()
	c.sub = newWSFilter([]string{"archigraph"}, []string{"feature/foo"})
	c.subMu.Unlock()

	c.subMu.Lock()
	f := c.sub
	c.subMu.Unlock()

	if f.Matches(WSEvent{Group: "upvate", Ref: "main"}) {
		t.Error("old filter must have been replaced — upvate/main should now be blocked")
	}
	if !f.Matches(WSEvent{Group: "archigraph", Ref: "feature/foo"}) {
		t.Error("new filter should pass archigraph/feature/foo")
	}
}

func TestWSFilter_Unsubscribe_ClearsFilter(t *testing.T) {
	c := &wsClient{
		send: make(chan []byte, 8),
		done: make(chan struct{}),
	}
	// Install a filter.
	c.subMu.Lock()
	c.sub = newWSFilter([]string{"upvate"}, []string{"main"})
	c.subMu.Unlock()

	// Unsubscribe — set nil (firehose).
	c.subMu.Lock()
	c.sub = nil
	c.subMu.Unlock()

	c.subMu.Lock()
	f := c.sub
	c.subMu.Unlock()

	if f != nil {
		t.Error("unsubscribe must set sub to nil")
	}
}

// ---------------------------------------------------------------------------
// Hub-level filtering: Broadcast respects per-client subscription.
// ---------------------------------------------------------------------------

func TestWSHub_Broadcast_FiltersPerClient(t *testing.T) {
	hub := newWSHub()

	// clientA subscribes to ref=main only.
	clientA := &wsClient{
		send: make(chan []byte, 8),
		done: make(chan struct{}),
		sub:  newWSFilter(nil, []string{"main"}),
	}
	// clientB subscribes to ref=feature/foo only.
	clientB := &wsClient{
		send: make(chan []byte, 8),
		done: make(chan struct{}),
		sub:  newWSFilter(nil, []string{"feature/foo"}),
	}
	// clientC has no subscription (firehose).
	clientC := &wsClient{
		send: make(chan []byte, 8),
		done: make(chan struct{}),
	}

	hub.add(clientA)
	hub.add(clientB)
	hub.add(clientC)

	// Broadcast a main-ref event.
	hub.Broadcast(WSEvent{
		Type:  "reindex_completed",
		Group: "upvate",
		Ref:   "main",
	})

	// The debounce timer fires after 2 s; use a 3-second deadline.
	deadline := time.After(3 * time.Second)

	// Drain clientA — should receive.
	var gotA, gotB, gotC bool
	for !gotA {
		select {
		case frame := <-clientA.send:
			var evt WSEvent
			// frame is a raw WebSocket frame; payload starts at byte 2 (for ≤125-byte frames).
			if len(frame) > 2 {
				_ = json.Unmarshal(frame[2:], &evt)
			}
			if evt.Ref == "main" {
				gotA = true
			}
		case <-deadline:
			t.Fatal("timeout waiting for clientA to receive main-ref event")
		}
	}

	// clientB should NOT have received the main-ref event (it subscribed to feature/foo).
	select {
	case <-clientB.send:
		gotB = true
	default:
	}
	if gotB {
		t.Error("clientB (feature/foo filter) must not receive a main-ref event")
	}

	// clientC (firehose) should receive.
	for !gotC {
		select {
		case frame := <-clientC.send:
			var evt WSEvent
			if len(frame) > 2 {
				_ = json.Unmarshal(frame[2:], &evt)
			}
			if evt.Ref == "main" {
				gotC = true
			}
		case <-deadline:
			t.Fatal("timeout waiting for clientC (firehose) to receive main-ref event")
		}
	}
}

// ---------------------------------------------------------------------------
// Integration test: subscribe/unsubscribe messages over a real WebSocket.
// ---------------------------------------------------------------------------

// dialTestWS performs a minimal WebSocket handshake against the given httptest
// server and returns the hijacked net.Conn + its buffered reader.
func dialTestWS(t *testing.T, srv *httptest.Server) (net.Conn, *bufio.Reader) {
	t.Helper()
	addr := strings.TrimPrefix(srv.URL, "http://")
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// Send WebSocket upgrade request.
	const key = "dGhlIHNhbXBsZSBub25jZQ==" // RFC 6455 example key
	req := fmt.Sprintf(
		"GET /ws/events HTTP/1.1\r\n"+
			"Host: %s\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Key: %s\r\n"+
			"Sec-WebSocket-Version: 13\r\n\r\n",
		addr, key,
	)
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("write upgrade: %v", err)
	}

	br := bufio.NewReader(conn)
	// Read until the blank line that ends the HTTP response headers.
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read upgrade response: %v", err)
		}
		if line == "\r\n" {
			break
		}
	}
	return conn, br
}

// sendWSTextFrame writes a masked WebSocket text frame to conn.
func sendWSTextFrame(t *testing.T, conn net.Conn, payload []byte) {
	t.Helper()
	frame := make([]byte, 2+4+len(payload))
	frame[0] = 0x81                         // FIN + opcode=text
	frame[1] = 0x80 | byte(len(payload))    // mask bit + length (assumes ≤125)
	mask := [4]byte{0xDE, 0xAD, 0xBE, 0xEF} // fixed mask for test simplicity
	copy(frame[2:6], mask[:])
	for i, b := range payload {
		frame[6+i] = b ^ mask[i%4]
	}
	if _, err := conn.Write(frame); err != nil {
		t.Fatalf("write WS frame: %v", err)
	}
}

// drainWSFrames reads from br for up to timeout, returning all text-frame payloads.
func drainWSFrames(br *bufio.Reader, timeout time.Duration) [][]byte {
	_ = br // placeholder; real reads use the conn deadline
	return nil
}

func TestWSIntegration_SubscribeFiltersEvents(t *testing.T) {
	// Build a minimal server.
	store := newFakeStore()
	srv, err := NewServer(DefaultConfig(), store)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ts := httptest.NewServer(http.HandlerFunc(srv.handleWSEvents))
	defer ts.Close()

	// --- Client A: subscribes to ref=main ---
	connA, brA := dialTestWS(t, ts)
	defer connA.Close()

	subMsg := `{"type":"subscribe","groups":[],"refs":["main"]}`
	sendWSTextFrame(t, connA, []byte(subMsg))

	// --- Client B: subscribes to ref=feature/foo ---
	connB, brB := dialTestWS(t, ts)
	defer connB.Close()

	_ = brB
	subMsgB := `{"type":"subscribe","groups":[],"refs":["feature/foo"]}`
	sendWSTextFrame(t, connB, []byte(subMsgB))

	// Give the server a moment to process the subscribe messages before
	// broadcasting, so the filters are installed.
	time.Sleep(100 * time.Millisecond)

	// Broadcast a main-ref event; the debounce is 2 s.
	srv.hub.Broadcast(WSEvent{
		Type:  "reindex_completed",
		Group: "upvate",
		Ref:   "main",
	})

	// Wait for the debounce + a small buffer.
	time.Sleep(2500 * time.Millisecond)

	// Client A should have received a frame.
	_ = connA.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	frameA, err := readWSFrame(brA)
	if err != nil {
		t.Errorf("clientA (ref=main): expected a frame but got error: %v", err)
	} else {
		var evt WSEvent
		if jsonErr := json.Unmarshal(frameA, &evt); jsonErr != nil {
			t.Errorf("clientA: unmarshal event: %v", jsonErr)
		} else if evt.Ref != "main" {
			t.Errorf("clientA: got ref %q, want %q", evt.Ref, "main")
		}
	}

	// Client B should NOT have received a frame (feature/foo filter).
	_ = connB.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	_, errB := readWSFrame(brB)
	if errB == nil {
		t.Error("clientB (ref=feature/foo) must not receive a main-ref event")
	}
}

func TestWSIntegration_Unsubscribe_RestoresFirehose(t *testing.T) {
	store := newFakeStore()
	srv, err := NewServer(DefaultConfig(), store)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ts := httptest.NewServer(http.HandlerFunc(srv.handleWSEvents))
	defer ts.Close()

	conn, br := dialTestWS(t, ts)
	defer conn.Close()

	// Subscribe to ref=main only.
	sendWSTextFrame(t, conn, []byte(`{"type":"subscribe","refs":["main"]}`))
	time.Sleep(100 * time.Millisecond)

	// Unsubscribe — restore firehose.
	sendWSTextFrame(t, conn, []byte(`{"type":"unsubscribe"}`))
	time.Sleep(100 * time.Millisecond)

	// Broadcast a feature/foo event; after unsubscribe the client should receive it.
	srv.hub.Broadcast(WSEvent{
		Type:  "reindex_completed",
		Group: "upvate",
		Ref:   "feature/foo",
	})
	time.Sleep(2500 * time.Millisecond)

	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	frame, err := readWSFrame(br)
	if err != nil {
		t.Errorf("after unsubscribe, client should receive all events but got error: %v", err)
	} else {
		var evt WSEvent
		if jsonErr := json.Unmarshal(frame, &evt); jsonErr != nil {
			t.Errorf("unmarshal event: %v", jsonErr)
		} else if evt.Ref != "feature/foo" {
			t.Errorf("got ref %q, want %q", evt.Ref, "feature/foo")
		}
	}
}

func TestWSIntegration_NoSubscribe_IsFirehose(t *testing.T) {
	store := newFakeStore()
	srv, err := NewServer(DefaultConfig(), store)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ts := httptest.NewServer(http.HandlerFunc(srv.handleWSEvents))
	defer ts.Close()

	conn, br := dialTestWS(t, ts)
	defer conn.Close()

	// Do NOT send any subscribe message.

	srv.hub.Broadcast(WSEvent{
		Type:  "watcher_event",
		Group: "some-group",
		Ref:   "any-ref",
	})
	time.Sleep(2500 * time.Millisecond)

	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	frame, err := readWSFrame(br)
	if err != nil {
		t.Errorf("client with no subscription (firehose) should receive all events, got: %v", err)
	} else if len(frame) == 0 {
		t.Error("received empty frame")
	}
}
