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
	"reflect"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
)

func TestParseGraphRequestOptions(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v2/graph/g?repos=b,a&filter_kind=function&include_external=true&view=modules&ref=release&lod=high", nil)
	got := parseGraphRequestOptions(req)
	want := graphRequestOptions{
		FilterKind: "function", ReposParam: "b,a", IncludeExternal: true,
		IncludeModules: true, Ref: "release", LOD: "high",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("options = %#v, want %#v", got, want)
	}

	includeReq := httptest.NewRequest(http.MethodGet, "/api/v2/graph/g?include=modules", nil)
	if got := parseGraphRequestOptions(includeReq); !got.IncludeModules {
		t.Fatalf("include=modules was ignored: %#v", got)
	}
}

func TestLodLimitsAreFinite(t *testing.T) {
	tests := map[string][2]int{
		"low":      {500, 4_000},
		"overview": {500, 4_000},
		"mid":      {3_000, 24_000},
		"normal":   {3_000, 24_000},
		"":         {3_000, 24_000},
		"high":     {20_000, 120_000},
		"full":     {50_000, 250_000},
	}
	for lod, want := range tests {
		nodeCap, edgeCap := lodLimits(lod)
		if nodeCap != want[0] || edgeCap != want[1] {
			t.Fatalf("lod=%q limits=(%d,%d), want=(%d,%d)", lod, nodeCap, edgeCap, want[0], want[1])
		}
	}
}

func TestCollectCappedGraphEdgesMatchesDeterministicOrdering(t *testing.T) {
	nodes := []v2GraphNode{
		{ID: "a", PageRank: 0.9}, {ID: "b", PageRank: 0.8}, {ID: "c", PageRank: 0.2}, {ID: "d", PageRank: 0.1},
	}
	edges := []v2GraphEdge{
		{Source: "c", Target: "d", Kind: "CALLS"},
		{Source: "a", Target: "c", Kind: "CALLS"},
		{Source: "a", Target: "b", Kind: "CALLS"},
	}
	got, truncated := collectCappedGraphEdges(nodes, 2, func(yield func(v2GraphEdge)) {
		for _, edge := range edges {
			yield(edge)
		}
	})
	if !truncated || !reflect.DeepEqual(got, []v2GraphEdge{
		{Source: "a", Target: "b", Kind: "CALLS"},
		{Source: "a", Target: "c", Kind: "CALLS"},
	}) {
		t.Fatalf("edges = %#v truncated=%v", got, truncated)
	}
}

func TestBuildV2GraphMetadataReportsEdgesDroppedByNodeThinning(t *testing.T) {
	grp := graphMetadataTestGroup()
	got := (&Server{}).buildV2GraphWithLimits(
		[]*DashRepo{grp.Repos["testrepo"]}, grp, "", false, false, 2, 100,
	)

	if got.TotalEdgeCount != 3 || len(got.Edges) != 1 {
		t.Fatalf("edge counts = total:%d served:%d, want total:3 served:1", got.TotalEdgeCount, len(got.Edges))
	}
	if !got.NodeTruncated || !got.EdgeTruncated {
		t.Fatalf("truncation metadata = node:%v edge:%v", got.NodeTruncated, got.EdgeTruncated)
	}
	if !reflect.DeepEqual(got.Edges, []v2GraphEdge{{Source: "testrepo::a", Target: "testrepo::b", Kind: "CALLS"}}) {
		t.Fatalf("edges = %#v", got.Edges)
	}
}

func TestBuildV2GraphMetadataReportsEdgeCapWithoutNodeThinning(t *testing.T) {
	grp := graphMetadataTestGroup()
	got := (&Server{}).buildV2GraphWithLimits(
		[]*DashRepo{grp.Repos["testrepo"]}, grp, "", false, false, 10, 2,
	)

	if got.TotalEdgeCount != 3 || len(got.Edges) != 2 {
		t.Fatalf("edge counts = total:%d served:%d, want total:3 served:2", got.TotalEdgeCount, len(got.Edges))
	}
	if got.NodeTruncated || !got.EdgeTruncated {
		t.Fatalf("truncation metadata = node:%v edge:%v", got.NodeTruncated, got.EdgeTruncated)
	}
	if got.Limits.EdgeCap != 2 {
		t.Fatalf("edge cap = %d, want 2", got.Limits.EdgeCap)
	}
}

func graphMetadataTestGroup() *DashGroup {
	ranks := []float64{0.9, 0.8, 0.2, 0.1}
	ids := []string{"a", "b", "c", "d"}
	entities := make([]graph.Entity, len(ids))
	for index, id := range ids {
		rank := ranks[index]
		entities[index] = graph.Entity{ID: id, Kind: "function", PageRank: &rank}
	}
	return makeGraphTestGroup(entities, []graph.Relationship{
		{FromID: "a", ToID: "b", Kind: "CALLS"},
		{FromID: "a", ToID: "c", Kind: "CALLS"},
		{FromID: "c", ToID: "d", Kind: "CALLS"},
	})
}

func TestV2GraphLoDMetadataReportsAppliedLimits(t *testing.T) {
	const n = 600
	ts := makeV2GraphTestServer(t, n)
	data := fetchV2Graph(t, ts, "low")
	if data.Limits.NodeCap != 500 || data.Limits.EdgeCap != 4_000 {
		t.Fatalf("limits = %#v", data.Limits)
	}
	if !data.NodeTruncated || data.TotalNodeCount != n || len(data.Nodes) != 500 {
		t.Fatalf("metadata = %#v nodes=%d", data, len(data.Nodes))
	}
}

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

// TestV2GraphServedDegree_MatchesEdges (Issue #1597) verifies node.degree
// reflects only the SERVED edges, not the full-graph degree. A node whose only
// relationship is to a filtered-out neighbour (here: a CONTAINS edge to a node
// excluded by include_modules=false) must report degree 0, not 1, so it never
// claims connectivity the canvas does not render.
func TestV2GraphServedDegree_MatchesEdges(t *testing.T) {
	prHi, prLo := 0.9, 0.1
	entities := []graph.Entity{
		{ID: "fn", Kind: "function", PageRank: &prHi},
		// "mod" is a Module entity; excluded by default (include_modules=false).
		{ID: "mod", Kind: "Module", PageRank: &prLo},
	}
	rels := []graph.Relationship{
		// fn's only edge is a CONTAINS to the (filtered-out) module.
		{FromID: "mod", ToID: "fn", Kind: "CONTAINS"},
	}
	grp := makeGraphTestGroup(entities, rels)
	ts := newV2GraphTestServerWithGroup(t, grp)
	data := fetchV2Graph(t, ts, "full")

	var fn *v2GraphNode
	for i := range data.Nodes {
		if data.Nodes[i].ID == "testrepo::fn" {
			fn = &data.Nodes[i]
		}
	}
	if fn == nil {
		t.Fatalf("fn node not served; nodes=%v", data.Nodes)
	}
	if fn.Degree != 0 {
		t.Errorf("served degree = %d, want 0 (its only edge is to a filtered-out module)", fn.Degree)
	}
	// Cross-check: no served edge touches fn.
	for _, e := range data.Edges {
		if e.Source == "testrepo::fn" || e.Target == "testrepo::fn" {
			t.Errorf("unexpected served edge touching fn: %+v", e)
		}
	}
}

// TestV2GraphThinning_ConnectivityPreserved (Issue #1597) builds a star graph
// where a high-pagerank hub connects to many low-pagerank leaves. Plain
// top-by-pagerank thinning would keep the hub but could keep leaves whose only
// neighbour (the hub) survives — the failure mode is a kept node rendering with
// zero served edges. We assert that every kept node that HAS edges in the full
// graph retains at least one served edge.
func TestV2GraphThinning_ConnectivityPreserved(t *testing.T) {
	const n = 700 // > overview cap 500
	entities := make([]graph.Entity, 0, n)
	rels := make([]graph.Relationship, 0, n)
	// Pair up nodes: e(2k) <-> e(2k+1). Give each pair adjacent pageranks so
	// the thinner that ignores connectivity tends to split pairs.
	for i := 0; i < n; i++ {
		pr := float64(i) / float64(n)
		entities = append(entities, graph.Entity{ID: fmt.Sprintf("e%d", i), Kind: "function", PageRank: &pr})
	}
	for k := 0; k+1 < n; k += 2 {
		rels = append(rels, graph.Relationship{
			FromID: fmt.Sprintf("e%d", k), ToID: fmt.Sprintf("e%d", k+1), Kind: "CALLS",
		})
	}
	grp := makeGraphTestGroup(entities, rels)
	ts := newV2GraphTestServerWithGroup(t, grp)
	data := fetchV2Graph(t, ts, "overview")

	served := map[string]int{}
	for _, e := range data.Edges {
		served[e.Source]++
		served[e.Target]++
	}
	isolatedKept := 0
	for _, nd := range data.Nodes {
		if served[nd.ID] == 0 {
			isolatedKept++
		}
		// node.degree must equal served degree.
		if nd.Degree != served[nd.ID] {
			t.Errorf("node %s: degree=%d, served=%d (must match)", nd.ID, nd.Degree, served[nd.ID])
		}
	}
	// Every node in this fixture has a real edge, so a connectivity-preserving
	// thinner should leave (near) zero isolated kept nodes. Allow a small slack
	// for the fixed-budget repair pass but require it to be far below the naive
	// failure rate.
	if isolatedKept > len(data.Nodes)/10 {
		t.Errorf("connectivity not preserved: %d/%d kept nodes are isolated", isolatedKept, len(data.Nodes))
	}
}
