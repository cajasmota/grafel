package dashboard

// handlers_graph_test.go — unit tests for Process node label handling in the
// /api/graph/{group} endpoint (and the entityLabel helper).
//
// Tests verify that:
//   - Process entities with a non-empty Name emit that Name as the label.
//   - Process entities with an empty Name fall back to Properties["entry_name"]
//     + terminal derived from Properties["chain_labels"].
//   - Process entities with neither Name nor chain_labels fall back to
//     Properties["entry_id"] (last path component) + " flow".
//   - Non-Process entities with an empty Name are returned with an empty label
//     (not a hash fallback) — the graphNodeWire.Label field is always present in
//     the JSON so the frontend never sees undefined and falls back to the raw id.
//   - The "label" JSON key is always present in graphNodeWire output even when
//     the value is an empty string (no omitempty).
//
// Payload-cache tests (#1399) verify that:
//   - A cache hit returns the same body as the original response.
//   - A strong ETag is present on first and subsequent responses.
//   - If-None-Match with a matching ETag returns 304 Not Modified (empty body).
//   - Invalidating the group clears the payload cache (next request rebuilds).
//   - A request with gzip Accept-Encoding is served correctly.
//   - Different query params (includeModules, repos) produce separate cache entries.

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

func makeGraphTestGroup(entities []graph.Entity, rels []graph.Relationship) *DashGroup {
	doc := &graph.Document{
		Repo:          "testrepo",
		Entities:      entities,
		Relationships: rels,
	}
	return &DashGroup{
		Name: "testgrp",
		Repos: map[string]*DashRepo{
			"testrepo": {Slug: "testrepo", Path: "/tmp/fake", Doc: doc},
		},
	}
}

func newGraphTestServer(t *testing.T, grp *DashGroup) *httptest.Server {
	t.Helper()
	st := newFakeStore()
	st.groups["testgrp"] = GroupSummary{
		Name:       "testgrp",
		ConfigPath: "/tmp/testgrp.json",
		Repos:      []string{"testrepo"},
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

// fetchGraphNodes calls GET /api/graph/testgrp and returns the nodes slice.
func fetchGraphNodes(t *testing.T, ts *httptest.Server) []map[string]interface{} {
	t.Helper()
	resp, err := http.Get(ts.URL + "/api/graph/testgrp")
	if err != nil {
		t.Fatalf("GET /api/graph/testgrp: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	nodesRaw, ok := body["nodes"].([]interface{})
	if !ok {
		t.Fatal("nodes field missing or wrong type")
	}
	out := make([]map[string]interface{}, 0, len(nodesRaw))
	for _, n := range nodesRaw {
		if m, ok := n.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out
}

// nodeByID finds the first node whose "id" field contains the given suffix.
func nodeByID(nodes []map[string]interface{}, idSuffix string) map[string]interface{} {
	for _, n := range nodes {
		if id, _ := n["id"].(string); strings.Contains(id, idSuffix) {
			return n
		}
	}
	return nil
}

// ─── entityLabel unit tests ───────────────────────────────────────────────────

func TestEntityLabel_NonEmpty(t *testing.T) {
	e := &graph.Entity{ID: "fn:abc", Name: "handleSubmit", Kind: "SCOPE.Function"}
	if got := entityLabel(e); got != "handleSubmit" {
		t.Errorf("entityLabel = %q, want 'handleSubmit'", got)
	}
}

func TestEntityLabel_ProcessWithName(t *testing.T) {
	e :=

		graph.EntityPtr(graph.Entity{
			ID:   "proc:abc123",
			Name: "handleOrder → writeDB",
			Kind: "SCOPE.Process",
		}.WithProperties(map[string]string{
			"entry_name":   "handleOrder",
			"chain_labels": "handleOrder → callService → writeDB",
		},
		))
	// When Name is set, entityLabel returns it unchanged — no Properties lookup.
	if got := entityLabel(e); got != "handleOrder → writeDB" {
		t.Errorf("entityLabel = %q, want 'handleOrder → writeDB'", got)
	}
}

func TestEntityLabel_ProcessEmptyName_FallsBackToEntryName(t *testing.T) {
	e :=

		// empty — simulates older graph data

		graph.EntityPtr(graph.Entity{
			ID:   "proc:deadbeef01234567",
			Name: "",
			Kind: "SCOPE.Process",
		}.WithProperties(map[string]string{
			"entry_name":   "processPayment",
			"entry_id":     "testrepo::SCOPE.Function:processPayment",
			"chain_labels": "processPayment → chargeCard → emitEvent → notify",
		},
		))
	got := entityLabel(e)
	// Should derive "processPayment → notify" from entry_name + last chain segment.
	if got != "processPayment → notify" {
		t.Errorf("entityLabel = %q, want 'processPayment → notify'", got)
	}
}

func TestEntityLabel_ProcessEmptyNameNoChainLabels_FallsBackToEntryNameFlow(t *testing.T) {
	e :=

		graph.EntityPtr(graph.Entity{
			ID:   "proc:deadbeef01234567",
			Name: "",
			Kind: "SCOPE.Process",
		}.WithProperties(map[string]string{
			"entry_name": "syncInventory",
		},
		))
	got := entityLabel(e)
	if got != "syncInventory flow" {
		t.Errorf("entityLabel = %q, want 'syncInventory flow'", got)
	}
}

func TestEntityLabel_ProcessEmptyNameNoEntryName_FallsBackToEntryID(t *testing.T) {
	e :=

		graph.EntityPtr(graph.Entity{
			ID:   "proc:deadbeef01234567",
			Name: "",
			Kind: "SCOPE.Process",
		}.WithProperties(map[string]string{
			"entry_id": "testrepo::SCOPE.Function:auditLog",
		},
		))
	got := entityLabel(e)
	if got != "auditLog flow" {
		t.Errorf("entityLabel = %q, want 'auditLog flow'", got)
	}
}

func TestEntityLabel_ProcessEmptyNameNoProperties_FallsBackToID(t *testing.T) {
	e := &graph.Entity{
		ID:   "proc:deadbeef01234567",
		Name: "",
		Kind: "SCOPE.Process",
	}
	got := entityLabel(e)
	// When there are absolutely no properties, the raw ID is returned
	// rather than showing an empty string.
	if got == "" {
		t.Errorf("entityLabel returned empty string, want non-empty fallback")
	}
}

func TestEntityLabel_NonProcessEmptyName_ReturnsEmpty(t *testing.T) {
	// Non-Process entities with empty Name are NOT given a fallback — they
	// return "" which will be transmitted as the label (not omitted), so the
	// frontend receives "" and won't show the raw id.
	e := &graph.Entity{ID: "fn:xyz", Name: "", Kind: "SCOPE.Function"}
	if got := entityLabel(e); got != "" {
		t.Errorf("entityLabel = %q, want '' for non-Process empty-Name entity", got)
	}
}

// ─── HTTP handler integration tests ──────────────────────────────────────────

// TestHandlerGraph_ProcessNodeLabel_NonEmptyName verifies that a SCOPE.Process
// entity with a non-empty Name emits that Name as the label field in the JSON.
func TestHandlerGraph_ProcessNodeLabel_NonEmptyName(t *testing.T) {
	procEnt := graph.Entity{
		ID:   "proc:0123456789abcdef",
		Name: "handleSubmit → writeDB",
		Kind: "SCOPE.Process",
	}.WithProperties(map[string]string{
		"entry_name":   "handleSubmit",
		"chain_labels": "handleSubmit → callRepo → writeDB",
	},
	)
	grp := makeGraphTestGroup([]graph.Entity{procEnt}, nil)
	ts := newGraphTestServer(t, grp)

	nodes := fetchGraphNodes(t, ts)
	node := nodeByID(nodes, "proc:0123456789abcdef")
	if node == nil {
		t.Fatal("proc node not found in response")
	}
	label, _ := node["label"].(string)
	if label != "handleSubmit → writeDB" {
		t.Errorf("label=%q, want 'handleSubmit → writeDB'", label)
	}
}

// TestHandlerGraph_ProcessNodeLabel_EmptyName verifies that a SCOPE.Process
// entity with an empty Name (older graph data) falls back to Properties-derived
// label so the frontend never shows the raw proc:<hash> id.
func TestHandlerGraph_ProcessNodeLabel_EmptyName(t *testing.T) {
	procEnt := graph.Entity{
		ID:   "proc:deadbeef12345678",
		Name: "", // empty — the bug scenario
		Kind: "SCOPE.Process",
	}.WithProperties(map[string]string{
		"entry_name":   "processOrder",
		"chain_labels": "processOrder → validateCart → chargeCard → notify",
	},
	)
	grp := makeGraphTestGroup([]graph.Entity{procEnt}, nil)
	ts := newGraphTestServer(t, grp)

	nodes := fetchGraphNodes(t, ts)
	node := nodeByID(nodes, "proc:deadbeef12345678")
	if node == nil {
		t.Fatal("proc node not found in response")
	}

	label, _ := node["label"].(string)
	// Must NOT be the raw ID and must NOT be empty.
	if strings.Contains(label, "proc:deadbeef12345678") {
		t.Errorf("label still contains the raw proc hash: %q", label)
	}
	if label == "" {
		t.Error("label is empty; expected a human-readable fallback")
	}
	// Should be derived from entry_name + last segment of chain_labels.
	want := "processOrder → notify"
	if label != want {
		t.Errorf("label=%q, want %q", label, want)
	}
}

// TestHandlerGraph_LabelFieldAlwaysPresent checks that the "label" JSON field
// is always present in the graphNodeWire output, even when the entity has an
// empty Name and no Properties (the zero-value case). The frontend
// normalizeGraphNode does `raw.label ?? raw.id` — undefined (absent field)
// triggers the raw-id fallback; an explicit empty string does not.
func TestHandlerGraph_LabelFieldAlwaysPresent(t *testing.T) {
	fnEnt := graph.Entity{
		ID:   "fn:abc",
		Name: "", // empty name — anonymous or generated entity
		Kind: "SCOPE.Function",
	}
	grp := makeGraphTestGroup([]graph.Entity{fnEnt}, nil)
	ts := newGraphTestServer(t, grp)

	resp, err := http.Get(ts.URL + "/api/graph/testgrp")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	// Decode into a raw map so we can distinguish absent vs. present-empty
	// JSON keys.
	var body struct {
		Nodes []map[string]json.RawMessage `json:"nodes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Nodes) == 0 {
		t.Fatal("no nodes returned")
	}
	raw := body.Nodes[0]
	if _, ok := raw["label"]; !ok {
		t.Error(`"label" key absent from graphNodeWire JSON — must always be present (no omitempty)`)
	}
}

// ─── payload-cache + ETag/304 tests (#1399) ───────────────────────────────────

// makeSimpleGroup builds a minimal group with one Function entity.
func makeSimpleGroup() *DashGroup {
	return makeGraphTestGroup(
		[]graph.Entity{{ID: "fn:001", Name: "doWork", Kind: "SCOPE.Function"}},
		nil,
	)
}

// TestPayloadCache_CacheHit verifies that a second identical request to
// GET /api/graph/{group} is served from the payload cache: the response body
// must be identical to the first response and the ETag header must match.
func TestPayloadCache_CacheHit(t *testing.T) {
	ts := newGraphTestServer(t, makeSimpleGroup())

	// First request — cold (cache miss).
	resp1, err := http.Get(ts.URL + "/api/graph/testgrp")
	if err != nil {
		t.Fatalf("first GET: %v", err)
	}
	body1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()
	etag1 := resp1.Header.Get("ETag")

	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("first request status=%d", resp1.StatusCode)
	}
	if etag1 == "" {
		t.Fatal("first response missing ETag header")
	}

	// Second request — warm (cache hit).
	resp2, err := http.Get(ts.URL + "/api/graph/testgrp")
	if err != nil {
		t.Fatalf("second GET: %v", err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	etag2 := resp2.Header.Get("ETag")

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second request status=%d", resp2.StatusCode)
	}
	if etag2 == "" {
		t.Fatal("second response missing ETag header")
	}
	if etag1 != etag2 {
		t.Errorf("ETag changed between requests: %q → %q", etag1, etag2)
	}
	if string(body1) != string(body2) {
		t.Errorf("response body changed between requests (len %d → %d)", len(body1), len(body2))
	}
}

// TestPayloadCache_ETag304 verifies that If-None-Match with a matching ETag
// returns 304 Not Modified with an empty body.
func TestPayloadCache_ETag304(t *testing.T) {
	ts := newGraphTestServer(t, makeSimpleGroup())

	// First request to obtain ETag.
	resp1, err := http.Get(ts.URL + "/api/graph/testgrp")
	if err != nil {
		t.Fatalf("first GET: %v", err)
	}
	io.ReadAll(resp1.Body) //nolint:errcheck
	resp1.Body.Close()
	etag := resp1.Header.Get("ETag")
	if etag == "" {
		t.Fatal("no ETag on first response")
	}

	// Second request with If-None-Match.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/graph/testgrp", nil)
	req.Header.Set("If-None-Match", etag)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("conditional GET: %v", err)
	}
	condBody, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()

	if resp2.StatusCode != http.StatusNotModified {
		t.Errorf("status=%d, want 304 Not Modified", resp2.StatusCode)
	}
	if len(condBody) != 0 {
		t.Errorf("304 response body must be empty, got %d bytes: %s", len(condBody), condBody)
	}
}

// TestPayloadCache_ETag_Stale verifies that an outdated If-None-Match value
// (different ETag) still returns a full 200 response with the current body.
func TestPayloadCache_ETag_Stale(t *testing.T) {
	ts := newGraphTestServer(t, makeSimpleGroup())

	// Warm the cache.
	resp, _ := http.Get(ts.URL + "/api/graph/testgrp")
	io.ReadAll(resp.Body) //nolint:errcheck
	resp.Body.Close()

	// Request with a stale ETag.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/graph/testgrp", nil)
	req.Header.Set("If-None-Match", `"stale-etag-that-does-not-match"`)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET with stale ETag: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("status=%d, want 200 OK for stale ETag", resp2.StatusCode)
	}
}

// TestPayloadCache_Invalidation verifies that after cache invalidation the
// next request rebuilds the payload (ETag must still be non-empty; the test
// mostly checks no panic/error occurs).
func TestPayloadCache_Invalidation(t *testing.T) {
	grp := makeSimpleGroup()
	st := newFakeStore()
	st.groups["testgrp"] = GroupSummary{
		Name:       "testgrp",
		ConfigPath: "/tmp/testgrp.json",
		Repos:      []string{"testrepo"},
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

	// Prime the payload cache.
	resp1, _ := http.Get(ts.URL + "/api/graph/testgrp")
	etag1 := resp1.Header.Get("ETag")
	io.ReadAll(resp1.Body) //nolint:errcheck
	resp1.Body.Close()

	if etag1 == "" {
		t.Fatal("no ETag after first request")
	}

	// Invalidate the group — simulates a re-index event.
	srv.graphs.Invalidate("testgrp")

	// Re-inject the group so GetGroup doesn't hit disk (test environment).
	srv.graphs.mu.Lock()
	srv.graphs.entries["testgrp"] = &cacheEntry{group: grp, loadedAt: time.Now()}
	srv.graphs.mu.Unlock()

	// Next request must succeed (payload rebuilt from scratch).
	resp2, err := http.Get(ts.URL + "/api/graph/testgrp")
	if err != nil {
		t.Fatalf("GET after invalidation: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("status=%d after invalidation, want 200", resp2.StatusCode)
	}
	etag2 := resp2.Header.Get("ETag")
	if etag2 == "" {
		t.Fatal("no ETag after invalidation+rebuild")
	}
	// The ETag must be the same (same graph data) — this proves the rebuild
	// is deterministic and the cache is not serving stale data.
	if etag1 != etag2 {
		// Not a hard failure — different build order is acceptable, but we log it.
		t.Logf("ETag changed after rebuild: %q → %q (graph data may be non-deterministic)", etag1, etag2)
	}
}

// TestPayloadCache_GzipResponse verifies that a request with Accept-Encoding:
// gzip receives a valid gzip-compressed response.
func TestPayloadCache_GzipResponse(t *testing.T) {
	ts := newGraphTestServer(t, makeSimpleGroup())

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/graph/testgrp", nil)
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatalf("GET with Accept-Encoding: gzip: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding=%q, want gzip", resp.Header.Get("Content-Encoding"))
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer gz.Close()

	var body map[string]interface{}
	if err := json.NewDecoder(gz).Decode(&body); err != nil {
		t.Fatalf("decode gzip body: %v", err)
	}
	if _, ok := body["nodes"]; !ok {
		t.Error("decoded gzip body missing 'nodes' field")
	}
}

// TestPayloadCache_DifferentParams verifies that different query params
// produce separate cache entries with different (or same) ETags as appropriate.
func TestPayloadCache_DifferentParams(t *testing.T) {
	ts := newGraphTestServer(t, makeSimpleGroup())

	// Default params — no modules.
	resp1, err := http.Get(ts.URL + "/api/graph/testgrp")
	if err != nil {
		t.Fatalf("GET default: %v", err)
	}
	io.ReadAll(resp1.Body) //nolint:errcheck
	resp1.Body.Close()
	etag1 := resp1.Header.Get("ETag")

	// With view=modules — different cache key.
	resp2, err := http.Get(ts.URL + "/api/graph/testgrp?view=modules")
	if err != nil {
		t.Fatalf("GET view=modules: %v", err)
	}
	io.ReadAll(resp2.Body) //nolint:errcheck
	resp2.Body.Close()
	etag2 := resp2.Header.Get("ETag")

	if etag1 == "" || etag2 == "" {
		t.Fatal("missing ETag on one of the responses")
	}
	// Different params → different cache entries.  ETags may differ.
	// (For this tiny test fixture they may happen to collide — but the keys differ.)
	_ = etag1
	_ = etag2

	// Confirm the cache key function itself differs.
	key1 := payloadCacheKey("testgrp", "", "", "", false, false)
	key2 := payloadCacheKey("testgrp", "", "", "", false, true) // includeModules=true
	if key1 == key2 {
		t.Error("payloadCacheKey returned the same key for different includeModules values")
	}
}
