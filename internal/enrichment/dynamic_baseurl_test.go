package enrichment

// Tests for CollectDynamicBaseURLCandidates (#708).

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// synthesisedKindFor mirrors the kind selection in
// internal/engine/http_endpoint_synthesis.go (#1217): the synthesis pass
// stamps http_endpoint_call on consumer-side synthetics and
// http_endpoint_definition on producer-side ones. The pre-#1217
// "http_endpoint" kind is no longer emitted by any current code path, so a
// fixture that hardcodes it cannot exercise the production filter (#6449).
func synthesisedKindFor(patternType string) string {
	if patternType == "http_endpoint_client_synthesis" {
		return string(types.EntityKindHTTPEndpointCall)
	}
	return string(types.EntityKindHTTPEndpointDefinition)
}

// makeHTTPEndpointEntity builds a minimal graph.Entity for testing, using the
// kind the current synthesis path actually emits for the given pattern_type.
func makeHTTPEndpointEntity(id, path, patternType string, extraProps map[string]string) graph.Entity {
	return makeHTTPEndpointEntityOfKind(id, path, patternType,
		synthesisedKindFor(patternType), extraProps)
}

// makeHTTPEndpointEntityOfKind is makeHTTPEndpointEntity with an explicit
// kind, for back-compat coverage of graphs indexed before the #1217 split.
func makeHTTPEndpointEntityOfKind(id, path, patternType, kind string, extraProps map[string]string) graph.Entity {
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
		Kind:       kind,
		SourceFile: "src/api.ts",
		Language:   "typescript",
	}.WithProperties(props)
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
			graph.Entity{
				ID:   "id-func-1",
				Kind: "function",
				Name: "fetchUsers",
			}.WithProperties(map[string]string{
				"runtime_dynamic": "true",
			},
			),
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

// ---------------------------------------------------------------------------
// TestCollectDynamicBaseURLCandidates_PostSplitCallKind
// Issue #6449: the collector gated on the raw pre-#1217 kind literal
// "http_endpoint", while the synthesis pass has minted consumer-side
// entities as "http_endpoint_call" since the split. On every graph indexed
// after #1217 the collector therefore returned zero candidates, severing the
// runtime_dynamic → repair-queue feed (#732 / ADR-0015).
//
// This test pins the kind literal directly rather than going through the
// helper so that a future fixture change cannot silently re-vacuum it.
// ---------------------------------------------------------------------------
func TestCollectDynamicBaseURLCandidates_PostSplitCallKind(t *testing.T) {
	// Drift guard. This pins the literal that the SYNTHESIS PASS actually
	// stamps: since #6449, internal/engine/http_endpoint_synthesis.go derives
	// httpEndpointCallKind from types.EntityKindHTTPEndpointCall rather than
	// re-declaring its own literal, so producer and consumer cannot disagree
	// and this one assertion covers both. If it ever drifts, the fixture below
	// is vacuous and #6449 has recurred.
	if got := synthesisedKindFor("http_endpoint_client_synthesis"); got != "http_endpoint_call" {
		t.Fatalf("consumer synthesis kind drifted: got %q, want %q", got, "http_endpoint_call")
	}

	doc := &graph.Document{
		Entities: []graph.Entity{
			makeHTTPEndpointEntityOfKind("id-call-1", "/users",
				"http_endpoint_client_synthesis", "http_endpoint_call",
				map[string]string{
					"runtime_dynamic": "true",
					"framework":       "fetch",
				}),
		},
	}

	cands := CollectDynamicBaseURLCandidates(doc)
	if len(cands) != 1 {
		t.Fatalf("http_endpoint_call consumer produced %d candidates, want 1 — "+
			"the collector is gating on a stale kind literal (#6449)", len(cands))
	}
	if cands[0].SubjectID != "id-call-1" {
		t.Errorf("subject_id: want %q, got %q", "id-call-1", cands[0].SubjectID)
	}
}

// ---------------------------------------------------------------------------
// TestCollectDynamicBaseURLCandidates_LegacyKindStillAccepted
// Graphs indexed before the #1217 split still carry the legacy
// "http_endpoint" kind. types.IsHTTPEndpointKind covers all three kinds, so
// those pre-split graphs must keep producing candidates.
// ---------------------------------------------------------------------------
func TestCollectDynamicBaseURLCandidates_LegacyKindStillAccepted(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			makeHTTPEndpointEntityOfKind("id-legacy-1", "/users",
				"http_endpoint_client_synthesis", "http_endpoint",
				map[string]string{"runtime_dynamic": "true"}),
		},
	}

	cands := CollectDynamicBaseURLCandidates(doc)
	if len(cands) != 1 {
		t.Fatalf("legacy-kind consumer produced %d candidates, want 1", len(cands))
	}
}

// ---------------------------------------------------------------------------
// TestCollectDynamicBaseURLCandidates_DefinitionKindSkipped
// The collector has TWO independent gates — kind and pattern_type — and each
// must be pinned on its own. Every other case in this file varies only the
// kind while holding a consumer pattern_type, or vice versa, so a mutant that
// deletes either gate entirely can slip through the other one.
//
// This case holds pattern_type at the CONSUMER value so the pattern_type gate
// cannot do the rejecting, and varies only the kind. The sole reason each
// entity below must be skipped is the kind filter, which gives that filter an
// upper bound: widening it to accept every kind fails here.
// ---------------------------------------------------------------------------
func TestCollectDynamicBaseURLCandidates_DefinitionKindSkipped(t *testing.T) {
	// Every entity carries a consumer pattern_type AND a live dynamic signal,
	// so it would qualify in full were it not for its kind.
	for _, kind := range []string{
		"http_endpoint_definition", // post-#1217 producer kind
		"function",
		"Service",
		"SCOPE.Operation",
		"", // unkinded entity
	} {
		t.Run("kind="+kind, func(t *testing.T) {
			doc := &graph.Document{
				Entities: []graph.Entity{
					makeHTTPEndpointEntityOfKind("id-def-1", "/{version}/users",
						"http_endpoint_client_synthesis", kind,
						map[string]string{
							"dynamic_baseurl": "true",
							"runtime_dynamic": "true",
						}),
				},
			}

			if cands := CollectDynamicBaseURLCandidates(doc); len(cands) != 0 {
				t.Fatalf("kind %q passed the kind filter: got %d candidates, want 0 — "+
					"the kind gate has no upper bound", kind, len(cands))
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestCollectDynamicBaseURLCandidates_ProducerPatternTypeSkipped
// The mirror of the case above: kind is held at the CONSUMER value so the
// kind gate cannot do the rejecting, and only pattern_type varies. Pins the
// pattern_type gate independently.
// ---------------------------------------------------------------------------
func TestCollectDynamicBaseURLCandidates_ProducerPatternTypeSkipped(t *testing.T) {
	for _, patternType := range []string{
		"http_endpoint_synthesis", // producer side
		"openapi_spec",
		"", // unstamped
	} {
		t.Run("pattern_type="+patternType, func(t *testing.T) {
			doc := &graph.Document{
				Entities: []graph.Entity{
					makeHTTPEndpointEntityOfKind("id-pt-1", "/{version}/users",
						patternType, "http_endpoint_call",
						map[string]string{
							"dynamic_baseurl": "true",
							"runtime_dynamic": "true",
						}),
				},
			}

			if cands := CollectDynamicBaseURLCandidates(doc); len(cands) != 0 {
				t.Fatalf("pattern_type %q passed the pattern_type filter: got %d candidates, want 0",
					patternType, len(cands))
			}
		})
	}
}
