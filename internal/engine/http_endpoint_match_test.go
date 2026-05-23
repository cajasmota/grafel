package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func TestNormalizeEndpointPath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/users/{pk}", "/users/{*}"},
		{"/users/:id", "/users/{*}"},
		{"/users/<int:id>", "/users/{*}"},
		{"/Users/{userId}/", "/users/{*}"},
		{"/users/{userId}/posts/{postId}", "/users/{*}/posts/{*}"},
		{"/api/v1/inspections/{pk}/create-deficiencies", "/api/v1/inspections/{*}/create-deficiencies"},
		{"/inspections/{id}/create-deficiencies/", "/inspections/{*}/create-deficiencies"},
		{"/", "/"},
	}
	for _, c := range cases {
		if got := normalizeEndpointPath(c.in); got != c.want {
			t.Errorf("normalizeEndpointPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStripEndpointAPIPrefix(t *testing.T) {
	cases := []struct {
		in       string
		want     string
		stripped bool
	}{
		{"/api/v1/inspections/{*}/create-deficiencies", "/inspections/{*}/create-deficiencies", true},
		{"/api/users", "/users", true},
		{"/v2/things", "/things", true},
		{"/api", "/", true},
		{"/inspections/{*}/create-deficiencies", "", false}, // no prefix
		{"/apiary/bees", "", false},                         // not a real api prefix segment
	}
	for _, c := range cases {
		got, ok := stripEndpointAPIPrefix(c.in)
		if ok != c.stripped || (ok && got != c.want) {
			t.Errorf("stripEndpointAPIPrefix(%q) = (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.stripped)
		}
	}
}

func TestVerbsMatchCompat(t *testing.T) {
	yes := [][2]string{{"POST", "ANY"}, {"ANY", "GET"}, {"GET", "GET"}, {"", "POST"}, {"delete", "DELETE"}}
	no := [][2]string{{"DELETE", "PATCH"}, {"GET", "POST"}, {"PUT", "GET"}}
	for _, c := range yes {
		if !verbsMatchCompat(c[0], c[1]) {
			t.Errorf("verbsMatchCompat(%q,%q) = false, want true", c[0], c[1])
		}
	}
	for _, c := range no {
		if verbsMatchCompat(c[0], c[1]) {
			t.Errorf("verbsMatchCompat(%q,%q) = true, want false", c[0], c[1])
		}
	}
}

// TestResolveStructuralFallbackAndRetarget exercises the #1615 path: a call
// synthetic that differs from its definition on API prefix + param-token name +
// trailing slash + verb (ANY) must (a) resolve via the structural fallback and
// (b) have its inbound caller→call-synthetic FETCHES edge retargeted at the
// definition so it stops counting as an orphan.
func TestResolveStructuralFallbackAndRetarget(t *testing.T) {
	merged := []types.EntityRecord{
		// Producer handler + its definition synthetic (mounted under /api/v1, {pk}, ANY verb).
		{
			Kind: "SCOPE.Operation", Name: "InspectionViewSet.create_deficiency", SourceFile: "views.py",
		},
		{
			Kind: httpEndpointDefinitionKind,
			Name: "http:ANY:/api/v1/inspections/{pk}/create-deficiencies",
			Properties: map[string]string{
				"path": "/api/v1/inspections/{pk}/create-deficiencies", "verb": "ANY",
				"source_handler": "SCOPE.Operation:InspectionViewSet.create_deficiency",
			},
			SourceFile: "views.py",
		},
		// Consumer caller + its call synthetic (no prefix, {id}, trailing slash, POST verb).
		{
			Kind: "SCOPE.Operation", Name: "createDeficiencies", SourceFile: "api.ts",
		},
		{
			Kind: httpEndpointCallKind,
			Name: "http:POST:/inspections/{id}/create-deficiencies/",
			Properties: map[string]string{
				"path": "/inspections/{id}/create-deficiencies/", "verb": "POST",
				"source_caller": "SCOPE.Operation:createDeficiencies",
			},
			SourceFile: "api.ts",
		},
	}

	out, stats := ResolveHTTPEndpointHandlers(merged)

	if stats.CallsLinked < 1 {
		t.Fatalf("expected the call synthetic to resolve via structural fallback, calls_linked=%d", stats.CallsLinked)
	}
	if stats.CallerEdgesRetargeted < 1 {
		t.Fatalf("expected the inbound caller FETCHES edge to be retargeted, caller_edges_retargeted=%d", stats.CallerEdgesRetargeted)
	}

	// The caller entity's FETCHES edge must now point at the DEFINITION stub,
	// not the call-synthetic stub.
	defStub := httpEndpointDefinitionKind + ":http:ANY:/api/v1/inspections/{pk}/create-deficiencies"
	found := false
	for _, e := range out {
		if e.Name != "createDeficiencies" {
			continue
		}
		for _, rel := range e.Relationships {
			if string(rel.Kind) == fetchesEdgeKind && rel.ToID == defStub {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("caller createDeficiencies has no FETCHES edge retargeted to %q", defStub)
	}
}

// TestResolveStructuralFallbackVerbGuard ensures a call with an incompatible
// specific verb does NOT resolve to a definition of a different specific verb
// (false-match guard).
func TestResolveStructuralFallbackVerbGuard(t *testing.T) {
	merged := []types.EntityRecord{
		{
			Kind:       httpEndpointDefinitionKind,
			Name:       "http:GET:/me-email-templates",
			Properties: map[string]string{"path": "/me-email-templates", "verb": "GET"},
			SourceFile: "views.py",
		},
		{
			Kind:       httpEndpointCallKind,
			Name:       "http:DELETE:/me-email-templates",
			Properties: map[string]string{"path": "/me-email-templates", "verb": "DELETE"},
			SourceFile: "api.ts",
		},
	}
	_, stats := ResolveHTTPEndpointHandlers(merged)
	if stats.CallsLinked != 0 {
		t.Errorf("DELETE call must not resolve to a GET-only definition, calls_linked=%d", stats.CallsLinked)
	}
	if stats.CallsUnresolved != 1 {
		t.Errorf("expected the DELETE call to stay unresolved, calls_unresolved=%d", stats.CallsUnresolved)
	}
}
