package engine

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// TestKemal_BasicRoute covers the common Kemal controller shape:
// get "/todos" do ... end / post "/todos" do ... end.
func TestKemal_BasicRoute(t *testing.T) {
	src := `
require "kemal"

get "/todos" do |env|
  Todo.all.to_json
end

post "/todos" do |env|
  Todo.create(env.params.json)
end

Kemal.run
`
	ids, _ := runDetect(t, "crystal", "src/routes.cr", src)
	requireContains(t, ids, []string{
		"http:GET:/todos",
		"http:POST:/todos",
	}, "kemal-basic-route")
}

// TestKemal_PathParam covers the Sinatra-style `:param` dynamic segment:
// get "/todos/:id" → GET /todos/{id}.
func TestKemal_PathParam(t *testing.T) {
	src := `
require "kemal"

get "/todos/:id" do |env|
  Todo.find(env.params.url["id"]).to_json
end

delete "/todos/:id" do |env|
  Todo.delete(env.params.url["id"])
end
`
	ids, _ := runDetect(t, "crystal", "src/todos.cr", src)
	requireContains(t, ids, []string{
		"http:GET:/todos/{id}",
		"http:DELETE:/todos/{id}",
	}, "kemal-path-param")
}

// TestKemal_AmberControllerForm covers the Amber `routes` block registration
// where the verb macro carries a controller + action after the path literal:
// get "/users", UsersController, :index → GET /users.
func TestKemal_AmberControllerForm(t *testing.T) {
	src := `
Amber::Server.configure do
  routes :web do
    get "/users", UsersController, :index
    post "/users/:id", UsersController, :update
  end
end
`
	ids, _ := runDetect(t, "crystal", "config/routes.cr", src)
	requireContains(t, ids, []string{
		"http:GET:/users",
		"http:POST:/users/{id}",
	}, "amber-controller-form")
}

// TestKemal_InterpolatedRouteDropped is the honest-exclusion guard: a route
// whose path is a string interpolation with no static prefix must NOT forge an
// endpoint.
func TestKemal_InterpolatedRouteDropped(t *testing.T) {
	src := `
require "kemal"

get "#{base_path}" do |env|
  "dynamic"
end
`
	ids, _ := runDetect(t, "crystal", "src/dynamic.cr", src)
	for _, id := range ids {
		if id == "http:GET:/" || id == "http:GET:" {
			t.Fatalf("interpolated route must not synthesize an endpoint; got %v", ids)
		}
	}
}

// TestKemal_NonWebFileIgnored is the negative guard: a Crystal file that calls a
// method named `get` on a receiver (a cache/hash) but has NO web marker must not
// be misread as a route.
func TestKemal_NonWebFileIgnored(t *testing.T) {
	src := `
class Cache
  def lookup(key)
    @store.get(key)
  end
end
`
	ids, _ := runDetect(t, "crystal", "src/cache.cr", src)
	for _, id := range ids {
		if id == "http:GET:/key" {
			t.Fatalf("negative: @store.get(key) must not synthesize an endpoint; got %v", ids)
		}
	}
}

// TestKemal_E2ERouteTestLinkage is the end-to-end RED→GREEN proof (#4749
// validation). A Kemal route GET /todos is represented as an
// http_endpoint_definition; a spec-kemal test_suite carrying
// `e2e_route_calls = "GET /todos"` must yield a TESTS edge from the suite to
// that endpoint via the shared linkE2ERouteTestsToEndpoints pass.
func TestKemal_E2ERouteTestLinkage(t *testing.T) {
	def := types.EntityRecord{
		Kind:       httpEndpointDefinitionKind,
		Name:       "http:GET:/todos",
		SourceFile: "src/todos.cr",
		Language:   "crystal",
		Properties: map[string]string{
			"verb":         "GET",
			"path":         "/todos",
			"framework":    "kemal",
			"pattern_type": "http_endpoint_synthesis",
		},
	}
	suite := types.EntityRecord{
		Kind:       "SCOPE.Operation",
		Subtype:    "test_suite",
		Name:       "todos_spec",
		SourceFile: "spec/todos_spec.cr",
		Language:   "crystal",
		Properties: map[string]string{
			"framework":       "spec-kemal",
			"e2e_route_calls": "GET /todos",
		},
	}

	merged := []types.EntityRecord{def, suite}
	resolved, stats := ResolveHTTPEndpointHandlers(merged)

	if stats.E2ERouteTestEdges < 1 {
		t.Fatalf("expected >=1 e2e route-test edge for Kemal GET /todos; got %d", stats.E2ERouteTestEdges)
	}

	found := false
	for i := range resolved {
		if resolved[i].Name != "todos_spec" {
			continue
		}
		for _, r := range resolved[i].Relationships {
			if r.Kind == string(types.RelationshipKindTests) &&
				r.Properties.Get("match_source") == "e2e_supertest_route" &&
				r.Properties.Get("route") == "/todos" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected TESTS edge from todos_spec suite to GET /todos endpoint")
	}
}
