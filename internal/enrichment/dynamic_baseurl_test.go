package enrichment

// Tests for CollectDynamicBaseURLCandidates (#708).

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// makeHTTPEndpointEntity builds a minimal graph.Entity for testing.
func makeHTTPEndpointEntity(id, path, patternType string, extraProps map[string]string) graph.Entity {
	props := map[string]string{
		"path":         path,
		"verb":         "GET",
		"pattern_type": patternType,
	}
	for k, v := range extraProps {
		props[k] = v
	}
	return graph.Entity{
		ID:         id,
		Name:       "http:GET:" + path,
		Kind:       "http_endpoint",
		SourceFile: "src/api.ts",
		Language:   "typescript",
		Properties: props,
	}
}

// ---------------------------------------------------------------------------
// TestCollectDynamicBaseURLCandidates_RuntimeDynamic
// Issue #708: consumer-side endpoint with runtime_dynamic=true (env-var
// baseURL concat) must surface as a dynamic_baseurl_endpoint candidate.
//
// Acceptance criterion: fetch(process.env.API_URL + '/users') produces a
// repair_candidate with category "cross-repo runtime".
// ---------------------------------------------------------------------------
func TestCollectDynamicBaseURLCandidates_RuntimeDynamic(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			makeHTTPEndpointEntity("id-env-1", "/users", "http_endpoint_client_synthesis", map[string]string{
				"runtime_dynamic": "true",
				"framework":       "fetch",
			}),
		},
	}

	cands := CollectDynamicBaseURLCandidates(doc)
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(cands))
	}

	c := cands[0]
	if c.Kind != KindDynamicBaseURLEndpoint {
		t.Errorf("kind: want %q, got %q", KindDynamicBaseURLEndpoint, c.Kind)
	}
	if c.SubjectID != "id-env-1" {
		t.Errorf("subject_id: want %q, got %q", "id-env-1", c.SubjectID)
	}
	if !strings.HasPrefix(c.ID, "ec:") || len(c.ID) != len("ec:")+16 {
		t.Errorf("id shape wrong: %q", c.ID)
	}
	category, _ := c.Context["category"].(string)
	if category != CategoryCrossRepoRuntime {
		t.Errorf("category: want %q, got %q", CategoryCrossRepoRuntime, category)
	}
	dynamicKind, _ := c.Context["dynamic_kind"].(string)
	if dynamicKind != "env-var-baseurl" {
		t.Errorf("dynamic_kind: want %q, got %q", "env-var-baseurl", dynamicKind)
	}
}

// ---------------------------------------------------------------------------
// TestCollectDynamicBaseURLCandidates_DynamicBaseURL
// Issue #708: consumer-side endpoint whose canonical path starts with a
// {<name>} placeholder (e.g. /${tenantId}/contracts/${contractId} →
// {tenantId}/contracts/{contractId}) must surface as a candidate.
// ---------------------------------------------------------------------------
func TestCollectDynamicBaseURLCandidates_DynamicBaseURL(t *testing.T) {
	// Use the with-leading-slash form that the synthesis pass emits
	// (fetch(`/${tenantId}/contracts/${contractId}`) → /{tenantId}/contracts/{contractId}).
	doc := &graph.Document{
		Entities: []graph.Entity{
			makeHTTPEndpointEntity("id-tenant-1", "/{tenantId}/contracts/{contractId}",
				"http_endpoint_client_synthesis", map[string]string{
					"dynamic_baseurl": "true",
					"framework":       "fetch",
				}),
		},
	}

	cands := CollectDynamicBaseURLCandidates(doc)
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(cands))
	}

	c := cands[0]
	if c.Kind != KindDynamicBaseURLEndpoint {
		t.Errorf("kind: want %q, got %q", KindDynamicBaseURLEndpoint, c.Kind)
	}
	category, _ := c.Context["category"].(string)
	if category != CategoryCrossRepoRuntime {
		t.Errorf("category: want %q, got %q", CategoryCrossRepoRuntime, category)
	}
	dynamicKind, _ := c.Context["dynamic_kind"].(string)
	if dynamicKind != "leading-path-placeholder" {
		t.Errorf("dynamic_kind: want %q, got %q", "leading-path-placeholder", dynamicKind)
	}
	// Dynamic prefix var should be extracted (strip leading `/{` to get `tenantId`).
	prefixVar, _ := c.Context["dynamic_prefix_var"].(string)
	if prefixVar != "tenantId" {
		t.Errorf("dynamic_prefix_var: want %q, got %q", "tenantId", prefixVar)
	}
	// Static suffix should strip the leading /{tenantId} segment.
	suffix, _ := c.Context["static_path_suffix"].(string)
	if suffix != "/contracts/{contractId}" {
		t.Errorf("static_path_suffix: want %q, got %q", "/contracts/{contractId}", suffix)
	}
}

// ---------------------------------------------------------------------------
// TestCollectDynamicBaseURLCandidates_ProducerSideSkipped
// Producer-side http_endpoint synthetics must never appear as candidates —
// they ARE the targets, not the callers.
// ---------------------------------------------------------------------------
func TestCollectDynamicBaseURLCandidates_ProducerSideSkipped(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			// Producer side — must be skipped even if path starts with {.
			makeHTTPEndpointEntity("id-prod-1", "/{version}/users",
				"http_endpoint_synthesis", map[string]string{
					"dynamic_baseurl": "true",
				}),
			// Non-http_endpoint entity — must be skipped.
			{
				ID:   "id-func-1",
				Kind: "function",
				Name: "fetchUsers",
				Properties: map[string]string{
					"runtime_dynamic": "true",
				},
			},
		},
	}

	cands := CollectDynamicBaseURLCandidates(doc)
	if len(cands) != 0 {
		t.Fatalf("expected 0 candidates, got %d: %+v", len(cands), cands)
	}
}

// ---------------------------------------------------------------------------
// TestCollectDynamicBaseURLCandidates_StaticConsumerSkipped
// A consumer-side endpoint with a plain static path (no runtime_dynamic,
// no dynamic_baseurl) must not produce a candidate — it's the normal case.
// ---------------------------------------------------------------------------
func TestCollectDynamicBaseURLCandidates_StaticConsumerSkipped(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			makeHTTPEndpointEntity("id-static-1", "/users/{id}",
				"http_endpoint_client_synthesis", nil),
		},
	}

	cands := CollectDynamicBaseURLCandidates(doc)
	if len(cands) != 0 {
		t.Fatalf("expected 0 candidates, got %d", len(cands))
	}
}

// ---------------------------------------------------------------------------
// TestCollectDynamicBaseURLCandidates_BothSignals
// A file with both runtime_dynamic and dynamic_baseurl entities emits one
// candidate per entity, both with category "cross-repo runtime".
// ---------------------------------------------------------------------------
func TestCollectDynamicBaseURLCandidates_BothSignals(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			makeHTTPEndpointEntity("id-env-2", "/items",
				"http_endpoint_client_synthesis", map[string]string{
					"runtime_dynamic": "true",
				}),
			makeHTTPEndpointEntity("id-tenant-2", "/{tenantId}/orders",
				"http_endpoint_client_synthesis", map[string]string{
					"dynamic_baseurl": "true",
				}),
			// Static consumer — must not appear.
			makeHTTPEndpointEntity("id-static-2", "/products",
				"http_endpoint_client_synthesis", nil),
		},
	}

	cands := CollectDynamicBaseURLCandidates(doc)
	if len(cands) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(cands))
	}
	for _, c := range cands {
		if c.Kind != KindDynamicBaseURLEndpoint {
			t.Errorf("unexpected kind %q", c.Kind)
		}
		cat, _ := c.Context["category"].(string)
		if cat != CategoryCrossRepoRuntime {
			t.Errorf("category: want %q, got %q", CategoryCrossRepoRuntime, cat)
		}
	}
}

// ---------------------------------------------------------------------------
// TestCollectDynamicBaseURLCandidates_Idempotent
// Running the collector twice on the same document must return identical
// candidate IDs (deterministic / idempotent across index runs).
// ---------------------------------------------------------------------------
func TestCollectDynamicBaseURLCandidates_Idempotent(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			makeHTTPEndpointEntity("id-idem-1", "/orders/{id}",
				"http_endpoint_client_synthesis", map[string]string{
					"runtime_dynamic": "true",
				}),
		},
	}

	first := CollectDynamicBaseURLCandidates(doc)
	second := CollectDynamicBaseURLCandidates(doc)
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("expected 1 candidate each run, got %d / %d", len(first), len(second))
	}
	if first[0].ID != second[0].ID {
		t.Errorf("candidate IDs differ across runs: %q vs %q", first[0].ID, second[0].ID)
	}
}

// ---------------------------------------------------------------------------
// TestStaticPathSuffix
// Unit test for the staticPathSuffix helper.
// ---------------------------------------------------------------------------
func TestStaticPathSuffix(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		// No-leading-slash placeholder forms.
		{"{tenantId}/contracts/{id}", "/contracts/{id}"},
		{"{param}/users/{id}", "/users/{id}"},
		{"{x}", "/"},
		{"{tenantId}", "/"},
		// Leading-slash placeholder forms (what the synthesis pass emits).
		{"/{tenantId}/contracts/{id}", "/contracts/{id}"},
		{"/{param}/users/{id}", "/users/{id}"},
		{"/{x}", "/"},
		{"/{tenantId}", "/"},
		// Already-static paths (no leading param) — must pass through.
		{"/users/{id}", "/users/{id}"},
		{"/api/v1/items", "/api/v1/items"},
	}
	for _, tc := range cases {
		got := staticPathSuffix(tc.input)
		if got != tc.want {
			t.Errorf("staticPathSuffix(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
