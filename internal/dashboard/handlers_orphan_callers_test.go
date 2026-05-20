package dashboard

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
)

// ─────────────────────────────────────────────────────────────────────────────
// Unit tests for collectOrphanCallers
// ─────────────────────────────────────────────────────────────────────────────

func makeOrphanGroup(entities []graph.Entity, rels []graph.Relationship) *DashGroup {
	doc := &graph.Document{
		Repo:          "frontend",
		Entities:      entities,
		Relationships: rels,
	}
	return &DashGroup{
		Name: "testgrp",
		Repos: map[string]*DashRepo{
			"frontend": {Slug: "frontend", Path: "/tmp/fake-frontend", Doc: doc},
		},
	}
}

func TestCollectOrphanCallers_NoHandlerFound(t *testing.T) {
	// A FETCHES edge whose ToID doesn't map to any http_endpoint in the group.
	entities := []graph.Entity{
		{
			ID:         "caller_fn",
			Name:       "fetchUsers",
			Kind:       "Function",
			SourceFile: "src/api/users.ts",
			StartLine:  42,
			Properties: map[string]string{},
		},
	}
	rels := []graph.Relationship{
		{
			ID:     "r1",
			FromID: "caller_fn",
			ToID:   "http:GET:/api/users",
			Kind:   "FETCHES",
			Properties: map[string]string{
				"verb": "GET",
				"path": "/api/users",
			},
		},
	}

	grp := makeOrphanGroup(entities, rels)
	rows := collectOrphanCallers(grp)

	if len(rows) != 1 {
		t.Fatalf("expected 1 orphan row, got %d", len(rows))
	}
	r := rows[0]
	if r.Reason != "no_handler_found" {
		t.Errorf("reason: want no_handler_found, got %q", r.Reason)
	}
	if r.CallerFile != "src/api/users.ts" {
		t.Errorf("caller_file: want src/api/users.ts, got %q", r.CallerFile)
	}
	if r.CallerLine != 42 {
		t.Errorf("caller_line: want 42, got %d", r.CallerLine)
	}
	if r.URLPattern != "/api/users" {
		t.Errorf("url_pattern: want /api/users, got %q", r.URLPattern)
	}
	if r.Method != "GET" {
		t.Errorf("method: want GET, got %q", r.Method)
	}
	if r.SuggestedRepairKind != "add_missing_handler" {
		t.Errorf("suggested_repair_kind: want add_missing_handler, got %q", r.SuggestedRepairKind)
	}
	if r.CallerID != "frontend::caller_fn" {
		t.Errorf("caller_id: want frontend::caller_fn, got %q", r.CallerID)
	}
}

func TestCollectOrphanCallers_DynamicBaseURL(t *testing.T) {
	// A consumer-side http_endpoint with runtime_dynamic=true is orphaned
	// because the baseURL could not be statically resolved.
	entities := []graph.Entity{
		{
			ID:         "caller_fn2",
			Name:       "postOrder",
			Kind:       "Function",
			SourceFile: "src/api/orders.ts",
			StartLine:  10,
			Properties: map[string]string{},
		},
		{
			ID:   "http:POST:/orders",
			Name: "http:POST:/orders",
			Kind: "http_endpoint",
			Properties: map[string]string{
				"pattern_type":    "http_endpoint_client_synthesis",
				"runtime_dynamic": "true",
				"verb":            "POST",
				"path":            "/orders",
			},
		},
	}
	rels := []graph.Relationship{
		{
			ID:     "r2",
			FromID: "caller_fn2",
			ToID:   "http:POST:/orders",
			Kind:   "FETCHES",
			Properties: map[string]string{
				"verb": "POST",
				"path": "/orders",
			},
		},
	}

	grp := makeOrphanGroup(entities, rels)
	rows := collectOrphanCallers(grp)

	if len(rows) != 1 {
		t.Fatalf("expected 1 orphan row, got %d", len(rows))
	}
	if rows[0].Reason != "dynamic_baseurl" {
		t.Errorf("reason: want dynamic_baseurl, got %q", rows[0].Reason)
	}
	if rows[0].SuggestedRepairKind != "annotate_baseurl" {
		t.Errorf("suggested_repair_kind: want annotate_baseurl, got %q", rows[0].SuggestedRepairKind)
	}
}

func TestCollectOrphanCallers_TemplateLiteral(t *testing.T) {
	// A FETCHES edge whose path contains a template-literal placeholder.
	entities := []graph.Entity{
		{
			ID:         "caller_fn3",
			Name:       "getUser",
			Kind:       "Function",
			SourceFile: "src/api/user.ts",
			StartLine:  7,
			Properties: map[string]string{},
		},
	}
	rels := []graph.Relationship{
		{
			ID:     "r3",
			FromID: "caller_fn3",
			ToID:   "http:GET:/api/users/${userId}",
			Kind:   "FETCHES",
			Properties: map[string]string{
				"verb": "GET",
				"path": "/api/users/${userId}",
			},
		},
	}

	grp := makeOrphanGroup(entities, rels)
	rows := collectOrphanCallers(grp)

	if len(rows) != 1 {
		t.Fatalf("expected 1 orphan row, got %d", len(rows))
	}
	if rows[0].Reason != "template_literal" {
		t.Errorf("reason: want template_literal, got %q", rows[0].Reason)
	}
	if rows[0].SuggestedRepairKind != "resolve_template_url" {
		t.Errorf("suggested_repair_kind: want resolve_template_url, got %q", rows[0].SuggestedRepairKind)
	}
}

func TestCollectOrphanCallers_ResolvedEdgeNotOrphan(t *testing.T) {
	// A FETCHES edge whose ToID resolves to a producer-side http_endpoint — NOT orphan.
	entities := []graph.Entity{
		{
			ID:         "caller_fn4",
			Name:       "fetchHealth",
			Kind:       "Function",
			SourceFile: "src/api/health.ts",
			StartLine:  3,
			Properties: map[string]string{},
		},
		{
			ID:   "http:GET:/health",
			Name: "http:GET:/health",
			Kind: "http_endpoint",
			Properties: map[string]string{
				"pattern_type": "http_endpoint_synthesis", // producer side
				"verb":         "GET",
				"path":         "/health",
			},
		},
	}
	rels := []graph.Relationship{
		{
			ID:     "r4",
			FromID: "caller_fn4",
			ToID:   "http:GET:/health",
			Kind:   "FETCHES",
			Properties: map[string]string{
				"verb": "GET",
				"path": "/health",
			},
		},
	}

	grp := makeOrphanGroup(entities, rels)
	rows := collectOrphanCallers(grp)

	if len(rows) != 0 {
		t.Errorf("expected 0 orphan rows for resolved edge, got %d", len(rows))
	}
}

func TestCollectOrphanCallers_Deduplication(t *testing.T) {
	// Two FETCHES edges with the same (caller_id, url_pattern) must produce only one row.
	entities := []graph.Entity{
		{
			ID:         "caller_dup",
			Name:       "callTwice",
			Kind:       "Function",
			SourceFile: "src/dup.ts",
			StartLine:  5,
			Properties: map[string]string{},
		},
	}
	rels := []graph.Relationship{
		{ID: "rdup1", FromID: "caller_dup", ToID: "http:GET:/dup", Kind: "FETCHES",
			Properties: map[string]string{"verb": "GET", "path": "/dup"}},
		{ID: "rdup2", FromID: "caller_dup", ToID: "http:GET:/dup", Kind: "FETCHES",
			Properties: map[string]string{"verb": "GET", "path": "/dup"}},
	}

	grp := makeOrphanGroup(entities, rels)
	rows := collectOrphanCallers(grp)

	if len(rows) != 1 {
		t.Errorf("expected 1 deduplicated row, got %d", len(rows))
	}
}

func TestCollectOrphanCallers_EmptyGroup(t *testing.T) {
	grp := &DashGroup{
		Name:  "empty",
		Repos: map[string]*DashRepo{},
	}
	rows := collectOrphanCallers(grp)
	if len(rows) != 0 {
		t.Errorf("expected 0 rows for empty group, got %d", len(rows))
	}
}

func TestCollectOrphanCallers_NonFetchesEdgesIgnored(t *testing.T) {
	// CALLS and QUERIES edges should not contribute orphan rows.
	entities := []graph.Entity{
		{ID: "fn1", Name: "fn1", Kind: "Function", SourceFile: "src/a.go", StartLine: 1},
	}
	rels := []graph.Relationship{
		{ID: "rc1", FromID: "fn1", ToID: "missing_target", Kind: "CALLS"},
		{ID: "rc2", FromID: "fn1", ToID: "missing_target2", Kind: "QUERIES"},
	}

	grp := makeOrphanGroup(entities, rels)
	rows := collectOrphanCallers(grp)

	if len(rows) != 0 {
		t.Errorf("expected 0 rows (non-FETCHES edges must be ignored), got %d", len(rows))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Unit tests for classification helpers
// ─────────────────────────────────────────────────────────────────────────────

func TestContainsTemplatePlaceholder(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"/api/users/${userId}", true},
		{"/api/users/{id}/profile", true},
		{"/api/users", false},
		{"/{tenantId}/contracts", false}, // leading placeholder (no $) — handled as dynamic_baseurl
		{"{param}/users/{id}", false},    // leading placeholder
		{"/api/{version}/users", true},   // mid-path placeholder
	}
	for _, tc := range cases {
		got := containsTemplatePlaceholder(tc.url)
		if got != tc.want {
			t.Errorf("containsTemplatePlaceholder(%q): want %v, got %v", tc.url, tc.want, got)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Integration smoke: HTTP endpoint returns correct JSON shape
// ─────────────────────────────────────────────────────────────────────────────

func newOrphanTestServer(t *testing.T, grp *DashGroup) *httptest.Server {
	t.Helper()
	st := newFakeStore()
	st.groups["mygrp"] = GroupSummary{
		Name:       "mygrp",
		ConfigPath: "/tmp/mygrp.json",
		Repos:      []string{"frontend"},
	}
	cfg := DefaultConfig()
	srv, err := NewServer(cfg, st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.graphs.mu.Lock()
	srv.graphs.entries["mygrp"] = &cacheEntry{group: grp, loadedAt: time.Now()}
	srv.graphs.mu.Unlock()

	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)
	return ts
}

func TestHandleOrphanCallers_HTTPSmoke(t *testing.T) {
	entities := []graph.Entity{
		{
			ID:         "caller_smoke",
			Name:       "callAPI",
			Kind:       "Function",
			SourceFile: "src/api/smoke.ts",
			StartLine:  99,
			Properties: map[string]string{},
		},
	}
	rels := []graph.Relationship{
		{
			ID:     "rsmoke",
			FromID: "caller_smoke",
			ToID:   "http:DELETE:/smoke",
			Kind:   "FETCHES",
			Properties: map[string]string{
				"verb": "DELETE",
				"path": "/smoke",
			},
		},
	}
	grp := makeOrphanGroup(entities, rels)
	grp.Name = "mygrp"

	ts := newOrphanTestServer(t, grp)

	resp, err := http.Get(ts.URL + "/api/paths/mygrp/orphan-callers")
	if err != nil {
		t.Fatalf("GET orphan-callers: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}

	b, _ := io.ReadAll(resp.Body)
	var body struct {
		OrphanCallers []OrphanCallerRow `json:"orphan_callers"`
		Total         int               `json:"total"`
	}
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatalf("decode response: %v\nbody: %s", err, b)
	}

	if body.Total != 1 {
		t.Errorf("total: want 1, got %d", body.Total)
	}
	if len(body.OrphanCallers) != 1 {
		t.Fatalf("orphan_callers len: want 1, got %d", len(body.OrphanCallers))
	}

	row := body.OrphanCallers[0]
	if row.Method != "DELETE" {
		t.Errorf("method: want DELETE, got %q", row.Method)
	}
	if row.CallerFile != "src/api/smoke.ts" {
		t.Errorf("caller_file: want src/api/smoke.ts, got %q", row.CallerFile)
	}
	if row.CallerLine != 99 {
		t.Errorf("caller_line: want 99, got %d", row.CallerLine)
	}
}

func TestHandleOrphanCallers_UnknownGroup(t *testing.T) {
	grp := makeOrphanGroup(nil, nil)
	grp.Name = "mygrp"
	ts := newOrphanTestServer(t, grp)

	resp, err := http.Get(ts.URL + "/api/paths/nosuchgroup/orphan-callers")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: want 404, got %d", resp.StatusCode)
	}
}

func TestHandleOrphanCallers_EmptyResult(t *testing.T) {
	// A group with no FETCHES edges returns an empty list (not null).
	grp := makeOrphanGroup([]graph.Entity{
		{ID: "e1", Name: "e1", Kind: "Function", SourceFile: "src/x.go"},
	}, []graph.Relationship{
		{ID: "r1", FromID: "e1", ToID: "e2", Kind: "CALLS"},
	})
	grp.Name = "mygrp"
	ts := newOrphanTestServer(t, grp)

	resp, err := http.Get(ts.URL + "/api/paths/mygrp/orphan-callers")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	var body map[string]any
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	callers, ok := body["orphan_callers"]
	if !ok {
		t.Fatal("orphan_callers key missing")
	}
	arr, ok := callers.([]any)
	if !ok || len(arr) != 0 {
		t.Errorf("expected empty array, got %v", callers)
	}
}
