package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
	"github.com/cajasmota/archigraph/internal/types"
)

// TestGiraffe_BasicRoute covers the canonical Giraffe `choose [ ... ]` route
// list: `GET >=> route "/users" >=> handler`.
func TestGiraffe_BasicRoute(t *testing.T) {
	src := `module App
open Giraffe

let webApp =
    choose [
        GET >=> route "/users" >=> listUsers
        POST >=> route "/users" >=> createUser
    ]
`
	ids, _ := runDetect(t, "fsharp", "src/App.fs", src)
	requireContains(t, ids, []string{
		"http:GET:/users",
		"http:POST:/users",
	}, "giraffe-basic-route")
}

// TestGiraffe_RoutefTypedParam covers `routef "/users/%i"` printf-typed params,
// which canonicalise to the positional `{}` wildcard.
func TestGiraffe_RoutefTypedParam(t *testing.T) {
	src := `module App
open Giraffe

let webApp =
    choose [
        GET >=> routef "/users/%i" getUser
        DELETE >=> routef "/users/%i/posts/%s" deletePost
    ]
`
	ids, _ := runDetect(t, "fsharp", "src/App.fs", src)
	requireContains(t, ids, []string{
		"http:GET:/users/{}",
		"http:DELETE:/users/{}/posts/{}",
	}, "giraffe-routef-typed-param")
}

// TestGiraffe_SaturnRouter covers the Saturn `router { ... }` computation-
// expression DSL with both plain and `:name` colon-param paths.
func TestGiraffe_SaturnRouter(t *testing.T) {
	src := `module App
open Saturn

let apiRouter = router {
    get "/users" listUsers
    get "/users/:id" getUser
    post "/users" createUser
}
`
	ids, _ := runDetect(t, "fsharp", "src/Router.fs", src)
	requireContains(t, ids, []string{
		"http:GET:/users",
		"http:GET:/users/{id}",
		"http:POST:/users",
	}, "saturn-router")
}

// TestGiraffe_InterpolatedRouteDropped is the honest-exclusion guard: an
// interpolated path must NOT forge an endpoint.
func TestGiraffe_InterpolatedRouteDropped(t *testing.T) {
	src := `module App
open Giraffe

let webApp =
    choose [
        GET >=> route $"/users/{prefix}" >=> handler
    ]
`
	ids, _ := runDetect(t, "fsharp", "src/Dyn.fs", src)
	for _, id := range ids {
		if id == "http:GET:/" || id == "http:GET:/users" {
			t.Fatalf("interpolated route must not synthesize an endpoint; got %v", ids)
		}
	}
}

// TestGiraffe_NonWebFileIgnored is the negative guard: an F# file with no web
// marker that happens to call a `get`-like function must not be misread.
func TestGiraffe_NonWebFileIgnored(t *testing.T) {
	src := `module Cache

let lookup key =
    store |> Map.tryFind key
`
	ids, _ := runDetect(t, "fsharp", "src/Cache.fs", src)
	for _, id := range ids {
		if len(id) > 5 && id[:5] == "http:" {
			t.Fatalf("non-web F# file must not synthesize an endpoint; got %v", ids)
		}
	}
}

// TestGiraffe_CanonicalizeFormat unit-tests the routef `%fmt` → `{}` rewrite.
func TestGiraffe_CanonicalizeFormat(t *testing.T) {
	cases := map[string]string{
		"/users/%i":          "/users/{}",
		"/x/%s/%i":           "/x/{}/{}",
		"/g/%O":              "/g/{}",
		"/users":             "/users",
		"/users/:id":         "/users/{id}",
		"/pct/%%/done":       "/pct/%/done",
	}
	for in, want := range cases {
		got := httproutes.Canonicalize(httproutes.FrameworkGiraffe, in)
		if got != want {
			t.Errorf("Canonicalize(giraffe, %q) = %q; want %q", in, got, want)
		}
	}
}

// TestGiraffe_E2ERouteTestLinkage is the end-to-end RED→GREEN proof (#4749
// validation). A Giraffe route GET /users is represented as an
// http_endpoint_definition; an fsharp-testserver test_suite carrying
// `e2e_route_calls = "GET /users"` must yield a TESTS edge from the suite to
// that endpoint via the shared linkE2ERouteTestsToEndpoints pass.
func TestGiraffe_E2ERouteTestLinkage(t *testing.T) {
	def := types.EntityRecord{
		Kind:       httpEndpointDefinitionKind,
		Name:       "http:GET:/users",
		SourceFile: "src/App.fs",
		Language:   "fsharp",
		Properties: map[string]string{
			"verb":         "GET",
			"path":         "/users",
			"framework":    "giraffe",
			"pattern_type": "http_endpoint_synthesis",
		},
	}
	suite := types.EntityRecord{
		Kind:       "SCOPE.Operation",
		Subtype:    "test_suite",
		Name:       "UsersTests",
		SourceFile: "tests/UsersTests.fs",
		Language:   "fsharp",
		Properties: map[string]string{
			"framework":       "fsharp-testserver",
			"e2e_route_calls": "GET /users",
		},
	}

	merged := []types.EntityRecord{def, suite}
	resolved, stats := ResolveHTTPEndpointHandlers(merged)

	if stats.E2ERouteTestEdges < 1 {
		t.Fatalf("expected >=1 e2e route-test edge for Giraffe GET /users; got %d", stats.E2ERouteTestEdges)
	}

	found := false
	for i := range resolved {
		if resolved[i].Name != "UsersTests" {
			continue
		}
		for _, r := range resolved[i].Relationships {
			if r.Kind == string(types.RelationshipKindTests) &&
				r.Properties["match_source"] == "e2e_supertest_route" &&
				r.Properties["route"] == "/users" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected TESTS edge from UsersTests suite to GET /users endpoint")
	}
}
