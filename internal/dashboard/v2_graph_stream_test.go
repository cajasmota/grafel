package dashboard

// v2_graph_stream_test.go — unit tests for GET /api/v2/graph/{group}/stream
// (#5446 increment 1). Verifies the SSE event ordering (connected → meta →
// chunk* → done), that the meta totals match the served graph, that nodes are
// streamed important-first when a pagerank overlay is present, that an edge is
// never streamed before both of its endpoints, that the node/edge JSON shape
// matches the full-payload endpoint, and that a cold group returns 503.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
)

// sseEvent is one parsed SSE `event:`/`data:` block.
type sseEvent struct {
	Type string
	Data string
}

// readSSE consumes the whole SSE body into an ordered slice of events.
func readSSE(t *testing.T, body string) []sseEvent {
	t.Helper()
	var out []sseEvent
	sc := bufio.NewScanner(strings.NewReader(body))
	sc.Buffer(make([]byte, 0, 1<<20), 1<<24)
	var cur sseEvent
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			cur.Type = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			cur.Data = strings.TrimPrefix(line, "data: ")
		case line == "":
			if cur.Type != "" {
				out = append(out, cur)
			}
			cur = sseEvent{}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan SSE: %v", err)
	}
	return out
}

// fetchGraphStream GETs the stream endpoint and returns the parsed events.
func fetchGraphStream(t *testing.T, ts *httptest.Server, query string) []sseEvent {
	t.Helper()
	url := ts.URL + "/api/v2/graph/testgrp/stream"
	if query != "" {
		url += "?" + query
	}
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d (want 200)", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return readSSE(t, string(b))
}

// makeStreamTestGroup builds a group of n entities with pagerank i/n (so e0 is
// lowest, e[n-1] highest) and a simple chain of relationships e0->e1->...->e[n-1].
func makeStreamTestGroup(n int) *DashGroup {
	entities := make([]graph.Entity, n)
	for i := range entities {
		pr := float64(i) / float64(n)
		entities[i] = graph.Entity{
			ID:       fmt.Sprintf("e%d", i),
			Kind:     "function",
			PageRank: &pr,
		}
	}
	rels := make([]graph.Relationship, 0, n-1)
	for i := 0; i < n-1; i++ {
		rels = append(rels, graph.Relationship{
			FromID: fmt.Sprintf("e%d", i),
			ToID:   fmt.Sprintf("e%d", i+1),
			Kind:   "CALLS",
		})
	}
	return makeGraphTestGroup(entities, rels)
}

func TestGraphStream_OrderingAndTotals(t *testing.T) {
	const n = 1800 // > 2 chunks (chunk size 750)
	grp := makeStreamTestGroup(n)
	ts := newV2GraphTestServerWithGroup(t, grp)

	events := fetchGraphStream(t, ts, "")
	if len(events) < 4 {
		t.Fatalf("want at least connected+meta+chunk+done, got %d events", len(events))
	}

	// First event must be connected, second meta, last done.
	if events[0].Type != "connected" {
		t.Errorf("first event = %q, want connected", events[0].Type)
	}
	if events[1].Type != "meta" {
		t.Errorf("second event = %q, want meta", events[1].Type)
	}
	if last := events[len(events)-1]; last.Type != "done" {
		t.Errorf("last event = %q, want done", last.Type)
	} else if last.Data != `{"done":true}` {
		t.Errorf("done payload = %q, want {\"done\":true}", last.Data)
	}

	var meta v2GraphStreamMeta
	if err := json.Unmarshal([]byte(events[1].Data), &meta); err != nil {
		t.Fatalf("meta unmarshal: %v", err)
	}
	if meta.TotalNodes != n {
		t.Errorf("meta.total_nodes = %d, want %d", meta.TotalNodes, n)
	}
	if meta.TotalEdges != n-1 {
		t.Errorf("meta.total_edges = %d, want %d", meta.TotalEdges, n-1)
	}

	// Collect chunks (everything between meta and done).
	var streamedNodes []v2GraphNode
	var streamedEdges []v2GraphEdge
	seen := map[string]int{} // node id -> order index streamed
	idx := 0
	chunkCount := 0
	for _, ev := range events[2 : len(events)-1] {
		if ev.Type != "chunk" {
			t.Fatalf("unexpected event %q between meta and done", ev.Type)
		}
		chunkCount++
		var ch v2GraphStreamChunk
		if err := json.Unmarshal([]byte(ev.Data), &ch); err != nil {
			t.Fatalf("chunk unmarshal: %v", err)
		}
		for _, nnode := range ch.Nodes {
			seen[nnode.ID] = idx
			idx++
			streamedNodes = append(streamedNodes, nnode)
		}
		// Every edge in this chunk must reference only nodes already streamed
		// (this chunk inclusive) — i.e. both endpoints seen.
		for _, e := range ch.Edges {
			if _, ok := seen[e.Source]; !ok {
				t.Errorf("edge source %q streamed before its node", e.Source)
			}
			if _, ok := seen[e.Target]; !ok {
				t.Errorf("edge target %q streamed before its node", e.Target)
			}
		}
		streamedEdges = append(streamedEdges, ch.Edges...)
	}

	if len(streamedNodes) != n {
		t.Errorf("streamed %d nodes, want %d", len(streamedNodes), n)
	}
	if len(streamedEdges) != n-1 {
		t.Errorf("streamed %d edges, want %d", len(streamedEdges), n-1)
	}
	if chunkCount < 3 {
		t.Errorf("expected at least 3 chunks for n=%d, got %d", n, chunkCount)
	}

	// Important-first: pagerank strictly descending across the streamed order.
	for i := 1; i < len(streamedNodes); i++ {
		if streamedNodes[i].PageRank > streamedNodes[i-1].PageRank {
			t.Errorf("node order not pagerank-desc at %d: %.4f after %.4f",
				i, streamedNodes[i].PageRank, streamedNodes[i-1].PageRank)
			break
		}
	}
	// The single highest-pagerank node (e1799) must be in the very first chunk.
	if got := streamedNodes[0].ID; got != "testrepo::e1799" {
		t.Errorf("first streamed node = %q, want testrepo::e1799 (highest pagerank)", got)
	}
}

// TestGraphStream_ShapeMatchesFullPayload asserts the streamed node/edge JSON is
// the same shape (same fields) as the non-streaming /api/v2/graph endpoint, so
// the frontend can switch with no data-model change.
func TestGraphStream_ShapeMatchesFullPayload(t *testing.T) {
	grp := makeStreamTestGroup(10)
	ts := newV2GraphTestServerWithGroup(t, grp)

	// Full payload.
	full := fetchV2Graph(t, ts, "full")
	fullNodeByID := map[string]v2GraphNode{}
	for _, nnode := range full.Nodes {
		fullNodeByID[nnode.ID] = nnode
	}
	fullEdgeSet := map[string]bool{}
	for _, e := range full.Edges {
		fullEdgeSet[e.Source+"|"+e.Target+"|"+e.Kind] = true
	}

	// Streamed.
	events := fetchGraphStream(t, ts, "")
	var streamNodes []v2GraphNode
	var streamEdges []v2GraphEdge
	for _, ev := range events {
		if ev.Type != "chunk" {
			continue
		}
		var ch v2GraphStreamChunk
		if err := json.Unmarshal([]byte(ev.Data), &ch); err != nil {
			t.Fatalf("chunk unmarshal: %v", err)
		}
		streamNodes = append(streamNodes, ch.Nodes...)
		streamEdges = append(streamEdges, ch.Edges...)
	}

	if len(streamNodes) != len(full.Nodes) {
		t.Fatalf("node count mismatch: stream %d vs full %d", len(streamNodes), len(full.Nodes))
	}
	for _, sn := range streamNodes {
		fn, ok := fullNodeByID[sn.ID]
		if !ok {
			t.Errorf("streamed node %q absent from full payload", sn.ID)
			continue
		}
		// Compare the full struct: same fields, same values (shape match).
		if sn != fn {
			t.Errorf("node %q differs between stream and full payload:\n stream=%+v\n full=%+v", sn.ID, sn, fn)
		}
	}
	if len(streamEdges) != len(full.Edges) {
		t.Errorf("edge count mismatch: stream %d vs full %d", len(streamEdges), len(full.Edges))
	}
	for _, se := range streamEdges {
		if !fullEdgeSet[se.Source+"|"+se.Target+"|"+se.Kind] {
			t.Errorf("streamed edge %+v absent from full payload", se)
		}
	}
}

// TestGraphStream_NoOverlayFallsBackToDegree verifies that when no pagerank
// overlay is present (all pagerank == 0) ordering falls back to degree desc.
func TestGraphStream_NoOverlayFallsBackToDegree(t *testing.T) {
	// star: hub e0 connected to e1..e4; no pagerank set on any entity.
	entities := []graph.Entity{
		{ID: "e0", Kind: "function"},
		{ID: "e1", Kind: "function"},
		{ID: "e2", Kind: "function"},
		{ID: "e3", Kind: "function"},
		{ID: "e4", Kind: "function"},
	}
	rels := []graph.Relationship{
		{FromID: "e0", ToID: "e1", Kind: "CALLS"},
		{FromID: "e0", ToID: "e2", Kind: "CALLS"},
		{FromID: "e0", ToID: "e3", Kind: "CALLS"},
		{FromID: "e0", ToID: "e4", Kind: "CALLS"},
	}
	grp := makeGraphTestGroup(entities, rels)
	ts := newV2GraphTestServerWithGroup(t, grp)

	events := fetchGraphStream(t, ts, "")
	var first v2GraphNode
	got := false
	for _, ev := range events {
		if ev.Type != "chunk" {
			continue
		}
		var ch v2GraphStreamChunk
		if err := json.Unmarshal([]byte(ev.Data), &ch); err != nil {
			t.Fatalf("chunk unmarshal: %v", err)
		}
		if len(ch.Nodes) > 0 {
			first = ch.Nodes[0]
			got = true
			break
		}
	}
	if !got {
		t.Fatal("no node chunk received")
	}
	// Hub e0 has degree 4, others degree 1 → hub must stream first.
	if first.ID != "testrepo::e0" {
		t.Errorf("first node = %q, want testrepo::e0 (highest degree, no overlay)", first.ID)
	}
}

// TestGraphStream_ColdGroupReturns503 verifies a not-warm group yields the
// not-loaded signal rather than force-loading from disk.
func TestGraphStream_ColdGroupReturns503(t *testing.T) {
	st := newFakeStore()
	st.groups["testgrp"] = GroupSummary{
		Name: "testgrp", ConfigPath: "/tmp/testgrp.json", Repos: []string{"testrepo"},
	}
	cfg := DefaultConfig()
	srv, err := NewServer(cfg, st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	// Deliberately do NOT populate srv.graphs.entries → cold.
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/v2/graph/testgrp/stream")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("cold group status = %d, want 503", resp.StatusCode)
	}
	var env struct {
		OK    bool `json:"ok"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode err body: %v", err)
	}
	if env.OK || env.Error.Code != "unavailable" {
		t.Errorf("cold group body = %+v, want ok=false code=unavailable", env)
	}
	// Give the background warm goroutine a beat so it doesn't race t.Cleanup.
	time.Sleep(10 * time.Millisecond)
}
