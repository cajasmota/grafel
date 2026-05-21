package dashboard

// handlers_mcp_activity_test.go — tests for GET /api/mcp-activity/stream
// and GET /api/mcp-activity/history.

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/mcp"
)

// newTestServerWithActivityBroker wires an MCPActivityBroker into a minimal
// dashboard server and returns a running httptest.Server.
func newTestServerWithActivityBroker(t *testing.T, broker *mcp.MCPActivityBroker) *httptest.Server {
	t.Helper()
	srv, err := NewServer(Config{
		Bind:      "127.0.0.1",
		PortRange: PortRange{Min: 47300, Max: 47399},
	}, newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.SetMCPActivityBroker(broker)
	return httptest.NewServer(srv.routes())
}

// TestMCPActivityStreamConnectedEvent verifies that a subscriber immediately
// receives the "connected" SSE event.
func TestMCPActivityStreamConnectedEvent(t *testing.T) {
	broker := mcp.NewMCPActivityBroker()
	ts := newTestServerWithActivityBroker(t, broker)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/mcp-activity/stream", nil)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /api/mcp-activity/stream: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q; want text/event-stream", ct)
	}

	// readSSELines returns only the "data:" payloads. The connected event's
	// data payload contains "subscribed_at".
	lines := readSSELines(t, bufio.NewReader(resp.Body), 1, 2*time.Second)
	if len(lines) == 0 {
		t.Fatalf("received no SSE data lines")
	}
	if !strings.Contains(lines[0], "subscribed_at") {
		t.Errorf("first data line = %q; want subscribed_at field (connected event)", lines[0])
	}
}

// TestMCPActivityStreamEventDelivery publishes an event and verifies a
// subscriber receives it.
func TestMCPActivityStreamEventDelivery(t *testing.T) {
	broker := mcp.NewMCPActivityBroker()
	ts := newTestServerWithActivityBroker(t, broker)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/mcp-activity/stream", nil)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /api/mcp-activity/stream: %v", err)
	}
	defer resp.Body.Close()

	// Drain the connected event first.
	r := bufio.NewReader(resp.Body)
	readSSELines(t, r, 2, 2*time.Second)

	// Publish an activity event.
	go func() {
		time.Sleep(50 * time.Millisecond)
		broker.Publish(mcp.MCPActivityEvent{
			ToolName:        "archigraph_search_entities",
			ReturnedNodeIDs: []string{"svc:auth", "svc:billing"},
		})
	}()

	lines := readSSELines(t, r, 2, 2*time.Second)
	found := false
	for _, l := range lines {
		if strings.Contains(l, "archigraph_search_entities") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("did not receive mcp_activity event; lines: %v", lines)
	}
}

// TestMCPActivityStreamMultipleSubscribers verifies two simultaneous
// subscribers both receive the same published event.
func TestMCPActivityStreamMultipleSubscribers(t *testing.T) {
	broker := mcp.NewMCPActivityBroker()
	ts := newTestServerWithActivityBroker(t, broker)
	defer ts.Close()

	subscribe := func() (*bufio.Reader, func()) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = cancel // context will expire naturally
		req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/mcp-activity/stream", nil)
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("subscribe: %v", err)
		}
		r := bufio.NewReader(resp.Body)
		readSSELines(t, r, 2, 2*time.Second) // drain connected
		return r, func() { resp.Body.Close() }
	}

	r1, close1 := subscribe()
	r2, close2 := subscribe()
	defer close1()
	defer close2()

	time.Sleep(30 * time.Millisecond)
	broker.Publish(mcp.MCPActivityEvent{ToolName: "archigraph_find"})

	for i, r := range []*bufio.Reader{r1, r2} {
		lines := readSSELines(t, r, 2, 2*time.Second)
		found := false
		for _, l := range lines {
			if strings.Contains(l, "archigraph_find") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("subscriber %d did not receive event; lines: %v", i+1, lines)
		}
	}
}

// TestMCPActivityStreamNoBroker verifies that 503 is returned when no broker
// is configured.
func TestMCPActivityStreamNoBroker(t *testing.T) {
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

	resp, err := ts.Client().Get(ts.URL + "/api/mcp-activity/stream")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d; want 503", resp.StatusCode)
	}
}

// TestMCPActivityHistory verifies the history endpoint reads from the JSONL log.
func TestMCPActivityHistory(t *testing.T) {
	srv, err := NewServer(Config{
		Bind:      "127.0.0.1",
		PortRange: PortRange{Min: 47300, Max: 47399},
	}, newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// Write two events to a temp JSONL file.
	dir := t.TempDir()
	logPath := filepath.Join(dir, "mcp-activity.jsonl")
	events := []mcp.MCPActivityEvent{
		{ToolName: "archigraph_find", Timestamp: 1000},
		{ToolName: "archigraph_inspect", Timestamp: 2000},
	}
	f, _ := os.Create(logPath)
	enc := json.NewEncoder(f)
	for _, e := range events {
		_ = enc.Encode(e)
	}
	f.Close()

	srv.SetMCPActivityLog(logPath)
	ts := httptest.NewServer(srv.routes())
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/api/mcp-activity/history?limit=10")
	if err != nil {
		t.Fatalf("GET history: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; want 200", resp.StatusCode)
	}

	var body struct {
		Events []mcp.MCPActivityEvent `json:"events"`
		Count  int                    `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Count != 2 {
		t.Errorf("count = %d; want 2", body.Count)
	}
	if len(body.Events) < 2 {
		t.Fatalf("events len = %d; want 2", len(body.Events))
	}
	if body.Events[0].ToolName != "archigraph_find" {
		t.Errorf("events[0].ToolName = %q; want archigraph_find", body.Events[0].ToolName)
	}
}
