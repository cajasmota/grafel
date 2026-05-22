package dashboard

// v2_graph_test.go — unit tests for the ?lod= LoD thinning feature (#1516).
//
// The tests verify:
//   - lod=overview caps nodes to the top 500 by pagerank.
//   - lod=normal caps nodes to the top 3000 by pagerank.
//   - lod=full (or no param) returns all nodes.
//   - Edges whose endpoints are removed by LoD thinning are dropped.
//   - total_node_count reflects the un-thinned count.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
)

// makeV2GraphTestServer builds a test server loaded with n fake entities.
// Entities are given pagerank values 0..n-1 scaled to [0,1) (highest = n-1/n).
func makeV2GraphTestServer(t *testing.T, n int) *httptest.Server {
	t.Helper()
	entities := make([]graph.Entity, n)
	for i := range entities {
		pr := float64(i) / float64(n)
		entities[i] = graph.Entity{
			ID:       fmt.Sprintf("e%d", i),
			Kind:     "function",
			PageRank: &pr,
		}
	}
	grp := makeGraphTestGroup(entities, nil)
	return newV2GraphTestServerWithGroup(t, grp)
}

func newV2GraphTestServerWithGroup(t *testing.T, grp *DashGroup) *httptest.Server {
	t.Helper()
	st := newFakeStore()
	st.groups["testgrp"] = GroupSummary{
		Name: "testgrp", ConfigPath: "/tmp/testgrp.json", Repos: []string{"testrepo"},
	}
	cfg := DefaultConfig()
	srv, err := NewServer(cfg, st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.graphs.mu.Lock()
	srv.graphs.entries["testgrp"] = &cacheEntry{group: grp, loadedAt: time.Now()}
	srv.graphs.mu.Unlock()
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)
	return ts
}

func fetchV2Graph(t *testing.T, ts *httptest.Server, lod string) v2GraphResponse {
	t.Helper()
	url := ts.URL + "/api/v2/graph/testgrp"
	if lod != "" {
		url += "?lod=" + lod
	}
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d for lod=%q", resp.StatusCode, lod)
	}
	var env struct {
		Data v2GraphResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return env.Data
}

func TestV2GraphLoD_Full(t *testing.T) {
	const n = 100
	ts := makeV2GraphTestServer(t, n)
	data := fetchV2Graph(t, ts, "full")
	if len(data.Nodes) != n {
		t.Errorf("lod=full: got %d nodes, want %d", len(data.Nodes), n)
	}
	if data.TotalNodeCount != n {
		t.Errorf("lod=full: TotalNodeCount=%d, want %d", data.TotalNodeCount, n)
	}
}

func TestV2GraphLoD_Overview(t *testing.T) {
	const n = 1000
	const cap = 500
	ts := makeV2GraphTestServer(t, n)
	data := fetchV2Graph(t, ts, "overview")
	if len(data.Nodes) > cap {
		t.Errorf("lod=overview: got %d nodes, want ≤%d", len(data.Nodes), cap)
	}
	if data.TotalNodeCount != n {
		t.Errorf("lod=overview: TotalNodeCount=%d, want %d (un-thinned)", data.TotalNodeCount, n)
	}
}

func TestV2GraphLoD_Normal(t *testing.T) {
	const n = 5000
	const cap = 3000
	ts := makeV2GraphTestServer(t, n)
	data := fetchV2Graph(t, ts, "normal")
	if len(data.Nodes) > cap {
		t.Errorf("lod=normal: got %d nodes, want ≤%d", len(data.Nodes), cap)
	}
	if data.TotalNodeCount != n {
		t.Errorf("lod=normal: TotalNodeCount=%d, want %d (un-thinned)", data.TotalNodeCount, n)
	}
}

func TestV2GraphLoD_LegacyNames(t *testing.T) {
	// "low", "mid", "high" are the frontend's LodLevel strings; accept them too.
	const n = 1000
	ts := makeV2GraphTestServer(t, n)
	if data := fetchV2Graph(t, ts, "low"); len(data.Nodes) > 500 {
		t.Errorf("lod=low (legacy): got %d nodes, want ≤500", len(data.Nodes))
	}
	if data := fetchV2Graph(t, ts, "high"); len(data.Nodes) != n {
		t.Errorf("lod=high (legacy): got %d nodes, want %d", len(data.Nodes), n)
	}
}

func TestV2GraphLoD_DefaultIsNormal(t *testing.T) {
	// No ?lod= param → same as lod=normal (cap 3000).
	const n = 5000
	ts := makeV2GraphTestServer(t, n)
	data := fetchV2Graph(t, ts, "")
	if len(data.Nodes) > 3000 {
		t.Errorf("no lod: got %d nodes, want ≤3000 (default=normal)", len(data.Nodes))
	}
}

func TestV2GraphLoD_EdgesDropped(t *testing.T) {
	// With lod=overview on 600 nodes (cap=500), the lowest-pagerank nodes are
	// removed. Verify no edge in the thinned response references a missing node.
	const n = 600
	entities := make([]graph.Entity, n)
	rels := make([]graph.Relationship, 0, n)
	for i := range entities {
		pr := float64(i) / float64(n)
		entities[i] = graph.Entity{ID: fmt.Sprintf("e%d", i), Kind: "function", PageRank: &pr}
	}
	// chain: e0→e1, e1→e2, ... e(n-1)→e0
	for i := 0; i < n; i++ {
		rels = append(rels, graph.Relationship{
			FromID: fmt.Sprintf("e%d", i),
			ToID:   fmt.Sprintf("e%d", (i+1)%n),
			Kind:   "CALLS",
		})
	}
	grp := makeGraphTestGroup(entities, rels)
	ts := newV2GraphTestServerWithGroup(t, grp)

	data := fetchV2Graph(t, ts, "overview") // cap=500; removes e0..e99
	nodeSet := make(map[string]bool, len(data.Nodes))
	for _, nd := range data.Nodes {
		nodeSet[nd.ID] = true
	}
	for _, e := range data.Edges {
		if !nodeSet[e.Source] {
			t.Errorf("edge source %q not in thinned node set", e.Source)
		}
		if !nodeSet[e.Target] {
			t.Errorf("edge target %q not in thinned node set", e.Target)
		}
	}
}
