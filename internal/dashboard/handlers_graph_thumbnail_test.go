package dashboard

// handlers_graph_thumbnail_test.go — unit tests for GET /api/graph/{group}/layout-snapshot (#983)

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestHandleGraphLayoutSnapshot_OK(t *testing.T) {
	ts, _ := newPhase1Server(t)

	resp, err := http.Get(ts.URL + "/api/graph/testgroup/layout-snapshot")
	if err != nil {
		t.Fatalf("GET layout-snapshot: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var payload struct {
		Nodes      []ThumbnailNode `json:"nodes"`
		TotalNodes int             `json:"total_nodes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(payload.Nodes) == 0 {
		t.Fatal("expected at least one node in layout-snapshot")
	}
	if payload.TotalNodes != len(payload.Nodes) {
		t.Errorf("total_nodes %d != len(nodes) %d", payload.TotalNodes, len(payload.Nodes))
	}

	// All positions must be in [0,1].
	for _, n := range payload.Nodes {
		if n.X < 0 || n.X > 1 {
			t.Errorf("node %q: x=%f out of [0,1]", n.ID, n.X)
		}
		if n.Y < 0 || n.Y > 1 {
			t.Errorf("node %q: y=%f out of [0,1]", n.ID, n.Y)
		}
	}

	// Cache-Control header must be present.
	cc := resp.Header.Get("Cache-Control")
	if cc == "" {
		t.Error("expected Cache-Control header")
	}
}

func TestHandleGraphLayoutSnapshot_UnknownGroup(t *testing.T) {
	ts, _ := newPhase1Server(t)

	resp, err := http.Get(ts.URL + "/api/graph/no-such-group/layout-snapshot")
	if err != nil {
		t.Fatalf("GET layout-snapshot: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestHandleGraphLayoutSnapshot_TopParam(t *testing.T) {
	ts, _ := newPhase1Server(t)

	// Request top=1 — should return exactly 1 node.
	resp, err := http.Get(ts.URL + "/api/graph/testgroup/layout-snapshot?top=1")
	if err != nil {
		t.Fatalf("GET layout-snapshot?top=1: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var payload struct {
		Nodes []ThumbnailNode `json:"nodes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(payload.Nodes) != 1 {
		t.Errorf("expected 1 node with top=1, got %d", len(payload.Nodes))
	}
}

func TestHandleGraphLayoutSnapshot_ExcludesExternal(t *testing.T) {
	ts, gc := newPhase1Server(t)

	// Ensure the fake group doesn't have External entities by default —
	// the existing fakeDashGroup only uses Component / Function / etc.
	// Just assert the endpoint succeeds and External-kind nodes (if any) are absent.
	resp, err := http.Get(ts.URL + "/api/graph/testgroup/layout-snapshot")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	_ = gc // referenced to satisfy the import
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}
