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

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
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
	e := &graph.Entity{
		ID:   "proc:abc123",
		Name: "handleOrder → writeDB",
		Kind: "SCOPE.Process",
		Properties: map[string]string{
			"entry_name":   "handleOrder",
			"chain_labels": "handleOrder → callService → writeDB",
		},
	}
	// When Name is set, entityLabel returns it unchanged — no Properties lookup.
	if got := entityLabel(e); got != "handleOrder → writeDB" {
		t.Errorf("entityLabel = %q, want 'handleOrder → writeDB'", got)
	}
}

func TestEntityLabel_ProcessEmptyName_FallsBackToEntryName(t *testing.T) {
	e := &graph.Entity{
		ID:   "proc:deadbeef01234567",
		Name: "", // empty — simulates older graph data
		Kind: "SCOPE.Process",
		Properties: map[string]string{
			"entry_name":   "processPayment",
			"entry_id":     "testrepo::SCOPE.Function:processPayment",
			"chain_labels": "processPayment → chargeCard → emitEvent → notify",
		},
	}
	got := entityLabel(e)
	// Should derive "processPayment → notify" from entry_name + last chain segment.
	if got != "processPayment → notify" {
		t.Errorf("entityLabel = %q, want 'processPayment → notify'", got)
	}
}

func TestEntityLabel_ProcessEmptyNameNoChainLabels_FallsBackToEntryNameFlow(t *testing.T) {
	e := &graph.Entity{
		ID:   "proc:deadbeef01234567",
		Name: "",
		Kind: "SCOPE.Process",
		Properties: map[string]string{
			"entry_name": "syncInventory",
		},
	}
	got := entityLabel(e)
	if got != "syncInventory flow" {
		t.Errorf("entityLabel = %q, want 'syncInventory flow'", got)
	}
}

func TestEntityLabel_ProcessEmptyNameNoEntryName_FallsBackToEntryID(t *testing.T) {
	e := &graph.Entity{
		ID:   "proc:deadbeef01234567",
		Name: "",
		Kind: "SCOPE.Process",
		Properties: map[string]string{
			"entry_id": "testrepo::SCOPE.Function:auditLog",
		},
	}
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
		Properties: map[string]string{
			"entry_name":   "handleSubmit",
			"chain_labels": "handleSubmit → callRepo → writeDB",
		},
	}
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
		Properties: map[string]string{
			"entry_name":   "processOrder",
			"chain_labels": "processOrder → validateCart → chargeCard → notify",
		},
	}
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
