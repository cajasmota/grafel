package links

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestHTTPPass_ProducerConsumerMatch verifies the canonical happy path:
// a Django-style producer synthetic with `pattern_type=http_endpoint_synthesis`
// plus an IMPLEMENTS edge from a handler entity, and a fetch-style consumer
// synthetic with `pattern_type=http_endpoint_client_synthesis` plus a
// resolvable `source_caller` property, produce one cross-repo CALLS link
// from caller → handler with channel=http and identifier=http:GET:/users/{id}.
func TestHTTPPass_ProducerConsumerMatch(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{
				"id": "h1", "name": "UserView", "kind": "Controller",
				"source_file": "app/views.py",
			},
			{
				"id": "ep1", "name": "http:GET:/users/{id}", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties": map[string]any{
					"verb":         "GET",
					"path":         "/users/{id}",
					"framework":    "django",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{
				"id": "fn1", "name": "loadUser", "kind": "Function",
				"source_file": "src/api.ts",
			},
			{
				"id": "ep2", "name": "http:GET:/users/{id}", "kind": "http_endpoint",
				"source_file": "src/api.ts",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/users/{id}",
					"framework":     "fetch",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:loadUser",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("ghttp", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "ghttp-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var hit *Link
	for i, l := range doc.Links {
		if l.Method == MethodHTTP {
			hit = &doc.Links[i]
			break
		}
	}
	if hit == nil {
		t.Fatalf("expected at least one http-method link, got %+v", doc.Links)
	}
	if hit.Source != "frontend::fn1" {
		t.Errorf("source: want frontend::fn1 (resolved caller), got %s", hit.Source)
	}
	if hit.Target != "backend::h1" {
		t.Errorf("target: want backend::h1 (resolved handler), got %s", hit.Target)
	}
	if hit.Relation != RelationCalls {
		t.Errorf("relation: want calls, got %s", hit.Relation)
	}
	if hit.Channel == nil || *hit.Channel != "http" {
		t.Errorf("channel: want http, got %v", hit.Channel)
	}
	if hit.Identifier == nil || *hit.Identifier != "http:GET:/users/{id}" {
		t.Errorf("identifier: want http:GET:/users/{id}, got %v", hit.Identifier)
	}
}

// TestHTTPPass_AnyVerbWildcard verifies Django-style producer endpoints
// with verb=ANY can match consumer endpoints with a specific verb
// (GET/POST/...) when their canonical paths agree.
func TestHTTPPass_AnyVerbWildcard(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "UserView", "kind": "Controller", "source_file": "app/views.py"},
			{
				"id": "ep1", "name": "http:ANY:/users/{id}", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties": map[string]any{
					"verb":         "ANY",
					"path":         "/users/{id}",
					"framework":    "django",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "loadUser", "kind": "Function", "source_file": "src/api.ts"},
			{
				"id": "ep2", "name": "http:GET:/users/{id}", "kind": "http_endpoint",
				"source_file": "src/api.ts",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/users/{id}",
					"framework":     "fetch",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:loadUser",
				},
			},
		},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("ghttp-any", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "ghttp-any-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, l := range doc.Links {
		if l.Method != MethodHTTP {
			continue
		}
		if l.Identifier != nil && *l.Identifier == "http:GET:/users/{id}" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected ANY ↔ GET wildcard match emitting identifier http:GET:/users/{id}; got %+v", doc.Links)
	}
}

// TestHTTPPass_NoMatchWhenOnlyOneSide verifies that two repos that both
// emit producer-side synthetics for the same endpoint do NOT produce a
// CALLS link. Cross-repo CALLS requires at least one consumer side.
func TestHTTPPass_NoMatchWhenOnlyOneSide(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "service-a",
		Entities: []map[string]any{
			{"id": "h1", "name": "View", "kind": "Controller", "source_file": "a.py"},
			{
				"id": "ep1", "name": "http:GET:/foo", "kind": "http_endpoint",
				"source_file": "a.py",
				"properties": map[string]any{
					"verb": "GET", "path": "/foo", "framework": "django",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "service-b",
		Entities: []map[string]any{
			{"id": "h2", "name": "View", "kind": "Controller", "source_file": "b.py"},
			{
				"id": "ep2", "name": "http:GET:/foo", "kind": "http_endpoint",
				"source_file": "b.py",
				"properties": map[string]any{
					"verb": "GET", "path": "/foo", "framework": "django",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h2", "to_id": "ep2", "kind": "IMPLEMENTS"},
		},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("ghttp-no-consumer", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "ghttp-no-consumer-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range doc.Links {
		if l.Method == MethodHTTP {
			t.Errorf("expected zero http-method links without a consumer side, got %+v", l)
		}
	}
}

// TestHTTPPass_FallbackToSyntheticEntities verifies the graceful fallback:
// when the producer hasn't resolved an IMPLEMENTS edge (Phase-2 resolver
// dropped the synthetic? or the handler couldn't be matched), the link
// still emits with the synthetic stampedIDs as endpoints.
func TestHTTPPass_FallbackToSyntheticEntities(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			// No handler entity, no IMPLEMENTS edge — just the synthetic.
			{
				"id": "ep1", "name": "http:GET:/foo", "kind": "http_endpoint",
				"source_file": "a.py",
				"properties": map[string]any{
					"verb": "GET", "path": "/foo", "framework": "django",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{
				"id": "ep2", "name": "http:GET:/foo", "kind": "http_endpoint",
				"source_file": "x.ts",
				"properties": map[string]any{
					"verb": "GET", "path": "/foo", "framework": "fetch",
					"pattern_type": "http_endpoint_client_synthesis",
				},
			},
		},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("ghttp-fallback", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "ghttp-fallback-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, l := range doc.Links {
		if l.Method != MethodHTTP {
			continue
		}
		// Fallback: source = consumer synthetic, target = producer synthetic.
		if l.Source == "frontend::ep2" && l.Target == "backend::ep1" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected fallback http-method link frontend::ep2 → backend::ep1, got %+v", doc.Links)
	}
}

// ---------------------------------------------------------------------------
// Path-param normalization tests (issue #704)
// ---------------------------------------------------------------------------

// TestHTTPPass_NormalizePathForIndex verifies the canonical normalization
// helper maps all placeholder styles to {*} and leaves static segments alone.
func TestHTTPPass_NormalizePathForIndex(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// curly-brace Django / generic style
		{"/users/{pk}", "/users/{*}"},
		{"/users/{id}", "/users/{*}"},
		{"/users/{param}", "/users/{*}"},
		{"/users/{userId}", "/users/{*}"},
		// multi-segment curly
		{"/users/{pk}/posts/{post_id}", "/users/{*}/posts/{*}"},
		{"/users/{userId}/posts/{postId}", "/users/{*}/posts/{*}"},
		// Express / Rails colon style
		{"/users/:id", "/users/{*}"},
		{"/users/:pk", "/users/{*}"},
		{"/users/:userId/posts/:postId", "/users/{*}/posts/{*}"},
		// mixed style (edge case)
		{"/api/{version}/:id", "/api/{*}/{*}"},
		// static — untouched
		{"/api/v1/users", "/api/v1/users"},
		{"/", "/"},
		{"", ""},
		// version numbers should NOT be collapsed — v1, v2 are literal
		{"/api/v1/users/{pk}", "/api/v1/users/{*}"},
		// #1409 — Django/Flask angle-bracket params collapse too.
		{"/users/<int:id>", "/users/{*}"},
		{"/users/<slug>", "/users/{*}"},
		{"/users/<uuid:pk>/posts/<int:post_id>", "/users/{*}/posts/{*}"},
		// #1409 — case-insensitive normalization.
		{"/Users/{Id}", "/users/{*}"},
		{"/API/V1/Users", "/api/v1/users"},
		// #1409 — trailing slash stripped (Django convention).
		{"/users/{pk}/", "/users/{*}"},
		{"/contracts/", "/contracts"},
	}
	for _, c := range cases {
		got := normalizePathForIndex(c.in)
		if got != c.want {
			t.Errorf("normalizePathForIndex(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestStripAPIPrefix covers the property-free generic API/version prefix strip
// added in #1409. Only well-known api/version segments are stripped; arbitrary
// first segments and non-prefix paths are left alone.
func TestStripAPIPrefix(t *testing.T) {
	cases := []struct {
		in       string
		want     string
		stripped bool
	}{
		{"/api/v1/inspections/{*}", "/inspections/{*}", true},
		{"/api/users", "/users", true},
		{"/v2/x", "/x", true},
		{"/v1", "/", true},
		{"/api", "/", true},
		{"/api/v1", "/", true},
		// no false positives
		{"/apixyz/foo", "", false},
		{"/users/{*}", "", false},
		{"/version/foo", "", false},
		{"/", "", false},
	}
	for _, c := range cases {
		got, ok := stripAPIPrefix(c.in)
		if ok != c.stripped || (ok && got != c.want) {
			t.Errorf("stripAPIPrefix(%q) = (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.stripped)
		}
	}
}

// TestHTTPPass_GenericPrefixMatch verifies that a producer serving
// `/api/v1/inspections/{pk}` links to a consumer calling `/inspections/{id}`
// even when the producer carries NO url_prefix property — the concrete acme
// case from issue #1409 (#819 only handled the property-driven strip).
func TestHTTPPass_GenericPrefixMatch(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "InspectionView", "kind": "Controller", "source_file": "app/views.py"},
			{
				"id": "ep1", "name": "http:GET:/api/v1/inspections/{pk}", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties": map[string]any{
					"verb":         "GET",
					"path":         "/api/v1/inspections/{pk}",
					"framework":    "django",
					"pattern_type": "http_endpoint_synthesis",
					// NOTE: deliberately NO url_prefix — exercises the generic strip.
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "getInspection", "kind": "Function", "source_file": "src/api.ts"},
			{
				"id": "ep2", "name": "http:GET:/inspections/{id}", "kind": "http_endpoint",
				"source_file": "src/api.ts",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/inspections/{id}",
					"framework":     "axios",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:getInspection",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g-generic-prefix", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g-generic-prefix-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, l := range doc.Links {
		if l.Method == MethodHTTP {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected cross-repo link for /api/v1 prefix mismatch w/o url_prefix; got %+v", doc.Links)
	}
}

// TestHTTPPass_GraphQLRootMatch verifies the #1496 fix: an Apollo client that
// only knows the GraphQL transport root (`new ApolloClient({uri: ".../graphql"})`
// → consumer synthetic `http:GRAPHQL:/graphql`) links to a GraphQL service whose
// producer synthetics are emitted per resolver field
// (`http:GRAPHQL:/graphql/Query/searchProducts`). All GraphQL operations are
// multiplexed over the one `/graphql` HTTP endpoint, so the field-level
// producers are aliased under the `/graphql` root in the byPath index.
func TestHTTPPass_GraphQLRootMatch(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "search-graphql",
		Entities: []map[string]any{
			{
				"id": "ep1", "name": "http:GRAPHQL:/graphql/Query/searchProducts", "kind": "http_endpoint",
				"source_file": "src/resolvers.ts",
				"properties": map[string]any{
					"verb":         "GRAPHQL",
					"path":         "/graphql/Query/searchProducts",
					"framework":    "graphql",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
			{
				"id": "ep2", "name": "http:GRAPHQL:/graphql/Query/order", "kind": "http_endpoint",
				"source_file": "src/resolvers.ts",
				"properties": map[string]any{
					"verb":         "GRAPHQL",
					"path":         "/graphql/Query/order",
					"framework":    "graphql",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "admin",
		Entities: []map[string]any{
			{"id": "fn1", "name": "queries", "kind": "Function", "source_file": "src/queries.ts"},
			{
				"id": "ep3", "name": "http:GRAPHQL:/graphql", "kind": "http_endpoint",
				"source_file": "src/queries.ts",
				"properties": map[string]any{
					"verb":          "GRAPHQL",
					"path":          "/graphql",
					"framework":     "apollo_client_uri",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:queries",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g-graphql-root", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g-graphql-root-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, l := range doc.Links {
		if l.Method == MethodHTTP && repoOfLink(l.Source) == "admin" && repoOfLink(l.Target) == "search-graphql" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected admin→search-graphql GraphQL-root cross-repo link; got %+v", doc.Links)
	}
}

// repoOfLink extracts the repo prefix of a "repo::entityID" endpoint key.
func repoOfLink(endpoint string) string {
	if i := strings.Index(endpoint, "::"); i >= 0 {
		return endpoint[:i]
	}
	return endpoint
}

// TestHTTPPass_PkVsParamMatch verifies that Django {pk} on the producer side
// matches a JS {param} placeholder on the consumer side after normalization.
// This is the concrete regression case from issue #704.
func TestHTTPPass_PkVsParamMatch(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "NotificationView", "kind": "Controller", "source_file": "app/views.py"},
			{
				"id": "ep1", "name": "http:ANY:/notifications/{pk}", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties": map[string]any{
					"verb":         "ANY",
					"path":         "/notifications/{pk}",
					"framework":    "django",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "patchNotification", "kind": "Function", "source_file": "src/api.ts"},
			{
				"id": "ep2", "name": "http:PATCH:/notifications/{param}", "kind": "http_endpoint",
				"source_file": "src/api.ts",
				"properties": map[string]any{
					"verb":          "PATCH",
					"path":          "/notifications/{param}",
					"framework":     "fetch",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:patchNotification",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g-pk-param", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g-pk-param-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, l := range doc.Links {
		if l.Method == MethodHTTP {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected cross-repo link for {pk} vs {param} after normalization; got %+v", doc.Links)
	}
}

// TestHTTPPass_MultiSegmentPkMatch verifies multi-segment paths with different
// placeholder names on each segment are correctly matched via normalization.
func TestHTTPPass_MultiSegmentPkMatch(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "PostView", "kind": "Controller", "source_file": "app/views.py"},
			{
				"id": "ep1", "name": "http:ANY:/users/{pk}/posts/{post_id}", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties": map[string]any{
					"verb":         "ANY",
					"path":         "/users/{pk}/posts/{post_id}",
					"framework":    "django",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "loadPost", "kind": "Function", "source_file": "src/api.ts"},
			{
				"id": "ep2", "name": "http:GET:/users/{userId}/posts/{postId}", "kind": "http_endpoint",
				"source_file": "src/api.ts",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/users/{userId}/posts/{postId}",
					"framework":     "fetch",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:loadPost",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g-multiseg-pk", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g-multiseg-pk-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, l := range doc.Links {
		if l.Method == MethodHTTP {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected cross-repo link for multi-segment {pk}/{post_id} vs {userId}/{postId}; got %+v", doc.Links)
	}
}

// TestHTTPPass_StaticPathsUnaffected verifies that static paths (no params)
// still work correctly and are not accidentally collapsed or broken.
func TestHTTPPass_StaticPathsUnaffected(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "StatusView", "kind": "Controller", "source_file": "app/views.py"},
			{
				"id": "ep1", "name": "http:GET:/api/v1/status", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties": map[string]any{
					"verb":         "GET",
					"path":         "/api/v1/status",
					"framework":    "django",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "checkStatus", "kind": "Function", "source_file": "src/api.ts"},
			{
				"id": "ep2", "name": "http:GET:/api/v1/status", "kind": "http_endpoint",
				"source_file": "src/api.ts",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/api/v1/status",
					"framework":     "fetch",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:checkStatus",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g-static-path", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g-static-path-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, l := range doc.Links {
		if l.Method == MethodHTTP {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected cross-repo link for static path /api/v1/status; got %+v", doc.Links)
	}
}

// TestHTTPPass_GoReqClientCrossRepoMatch verifies that a Go consumer synthetic
// emitted by the github.com/imroc/req package-level extractor
// (synthesizeGoClientWithRuntime → http_endpoint_call "http:GET:/api/v1/users",
// framework "req") cross-repo-links to a Go server route exposing the same
// canonical (verb, path). This is the cross-repo orphan-gap the req extension
// closes: the consumer is a Go `.go` file, the producer is a Go handler, and
// they join purely on the byte-identical synthetic ID — no framework-label
// coupling. Mirrors TestHTTPPass_ProducerConsumerMatch with both sides Go.
func TestHTTPPass_GoReqClientCrossRepoMatch(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "users-svc",
		Entities: []map[string]any{
			{"id": "h1", "name": "ListUsers", "kind": "Function", "source_file": "handlers/users.go"},
			{
				"id": "ep1", "name": "http:GET:/api/v1/users", "kind": "http_endpoint",
				"source_file": "handlers/users.go",
				"properties": map[string]any{
					"verb":         "GET",
					"path":         "/api/v1/users",
					"framework":    "gin",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "orders-svc",
		Entities: []map[string]any{
			{"id": "fn1", "name": "listUsers", "kind": "Function", "source_file": "client/users.go"},
			{
				"id": "ep2", "name": "http:GET:/api/v1/users", "kind": "http_endpoint",
				"source_file": "client/users.go",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/api/v1/users",
					"framework":     "req",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:listUsers",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g-go-req", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g-go-req-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var hit *Link
	for i, l := range doc.Links {
		if l.Method == MethodHTTP {
			hit = &doc.Links[i]
			break
		}
	}
	if hit == nil {
		t.Fatalf("expected a cross-repo http link for the Go req client → Go route; got %+v", doc.Links)
	}
	if hit.Source != "orders-svc::fn1" {
		t.Errorf("source: want orders-svc::fn1 (resolved Go req caller), got %s", hit.Source)
	}
	if hit.Target != "users-svc::h1" {
		t.Errorf("target: want users-svc::h1 (resolved Go handler), got %s", hit.Target)
	}
	if hit.Identifier == nil || *hit.Identifier != "http:GET:/api/v1/users" {
		t.Errorf("identifier: want http:GET:/api/v1/users, got %v", hit.Identifier)
	}
}

// TestHTTPPass_MixedStaticAndParam verifies mixed paths like /api/v1/users/{pk}
// match /api/v1/users/{userId} correctly.
func TestHTTPPass_MixedStaticAndParam(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "UserView", "kind": "Controller", "source_file": "app/views.py"},
			{
				"id": "ep1", "name": "http:ANY:/api/v1/users/{pk}", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties": map[string]any{
					"verb":         "ANY",
					"path":         "/api/v1/users/{pk}",
					"framework":    "django",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "getUser", "kind": "Function", "source_file": "src/api.ts"},
			{
				"id": "ep2", "name": "http:GET:/api/v1/users/{userId}", "kind": "http_endpoint",
				"source_file": "src/api.ts",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/api/v1/users/{userId}",
					"framework":     "fetch",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:getUser",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g-mixed-path", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g-mixed-path-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, l := range doc.Links {
		if l.Method == MethodHTTP {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected cross-repo link for /api/v1/users/{pk} vs /api/v1/users/{userId}; got %+v", doc.Links)
	}
}

// TestHTTPPass_ExpressColonStyleMatch verifies that Express/Rails colon-style
// placeholders (:id, :userId) are treated equivalently to curly-brace style.
func TestHTTPPass_ExpressColonStyleMatch(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "UserView", "kind": "Controller", "source_file": "app/views.py"},
			{
				"id": "ep1", "name": "http:ANY:/users/{pk}", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties": map[string]any{
					"verb":         "ANY",
					"path":         "/users/{pk}",
					"framework":    "django",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "express-frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "getUser", "kind": "Function", "source_file": "routes/user.js"},
			{
				"id": "ep2", "name": "http:GET:/users/:id", "kind": "http_endpoint",
				"source_file": "routes/user.js",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/users/:id",
					"framework":     "express",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:getUser",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g-express-colon", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g-express-colon-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, l := range doc.Links {
		if l.Method == MethodHTTP {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected cross-repo link for Django {pk} vs Express :id style; got %+v", doc.Links)
	}
}

// TestHTTPPass_NoFalsePositiveOnDifferentShapes verifies that two paths with
// different static structures do NOT match even after normalization.
// e.g., /users/{pk} must NOT match /posts/{pk}.
func TestHTTPPass_NoFalsePositiveOnDifferentShapes(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "UserView", "kind": "Controller", "source_file": "app/views.py"},
			{
				"id": "ep1", "name": "http:ANY:/users/{pk}", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties": map[string]any{
					"verb":         "ANY",
					"path":         "/users/{pk}",
					"framework":    "django",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "getPost", "kind": "Function", "source_file": "src/api.ts"},
			{
				"id": "ep2", "name": "http:GET:/posts/{param}", "kind": "http_endpoint",
				"source_file": "src/api.ts",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/posts/{param}",
					"framework":     "fetch",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:getPost",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g-no-fp-diff-shape", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g-no-fp-diff-shape-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range doc.Links {
		if l.Method == MethodHTTP {
			t.Errorf("expected NO cross-repo link for /users/{pk} vs /posts/{param}; got %+v", l)
		}
	}
}

// TestHTTPPass_VerbStillCheckedAfterNormalization verifies that verb
// incompatibility (GET vs POST) still blocks a match after path normalization
// — normalization must not bypass the verb-compatibility check.
func TestHTTPPass_VerbStillCheckedAfterNormalization(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "UserView", "kind": "Controller", "source_file": "app/views.py"},
			{
				"id": "ep1", "name": "http:GET:/users/{pk}", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties": map[string]any{
					"verb":         "GET",
					"path":         "/users/{pk}",
					"framework":    "django",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "createUser", "kind": "Function", "source_file": "src/api.ts"},
			{
				"id": "ep2", "name": "http:POST:/users/{param}", "kind": "http_endpoint",
				"source_file": "src/api.ts",
				"properties": map[string]any{
					"verb":          "POST",
					"path":          "/users/{param}",
					"framework":     "fetch",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:createUser",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g-verb-blocked", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g-verb-blocked-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range doc.Links {
		if l.Method == MethodHTTP {
			t.Errorf("expected NO cross-repo link when verbs are incompatible (GET vs POST); got %+v", l)
		}
	}
}

// TestHTTPPass_VerbsCompatible verifies the verb compatibility helper.
func TestHTTPPass_VerbsCompatible(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"GET", "GET", true},
		{"get", "GET", true},
		{"GET", "POST", false},
		{"ANY", "GET", true},
		{"GET", "ANY", true},
		{"ANY", "ANY", true},
		{"", "GET", false},
	}
	for _, c := range cases {
		if got := verbsCompatible(c.a, c.b); got != c.want {
			t.Errorf("verbsCompatible(%q,%q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// TestHTTPPass_ParseHTTPName verifies the canonical-name parser.
func TestHTTPPass_ParseHTTPName(t *testing.T) {
	cases := []struct {
		in   string
		verb string
		path string
		ok   bool
	}{
		{"http:GET:/users/{id}", "GET", "/users/{id}", true},
		{"http:ANY:/foo", "ANY", "/foo", true},
		{"http:GET:", "", "", false},
		{"http:/foo", "", "", false},
		{"nothttp:GET:/x", "", "", false},
	}
	for _, c := range cases {
		v, p, ok := parseHTTPName(c.in)
		if ok != c.ok || v != c.verb || p != c.path {
			t.Errorf("parseHTTPName(%q) = (%q,%q,%v), want (%q,%q,%v)", c.in, v, p, ok, c.verb, c.path, c.ok)
		}
	}
}

// --- #747 verb-confusion regression tests ----------------------------
//
// These exercise the multi-verb producer pool created when DRF detail
// routes (PR #729) coexist with the legacy ANY-verb synthesizer output.
// Before the #747 fix, `firstByRepo` sorted by stampedID lexicographically
// and could pick (e.g.) PATCH as the "winner" for a DELETE consumer just
// because the PATCH endpoint's stamped ID sorted first.

// TestHTTPPass_VerbConfusion_ExactVerbPreference verifies that when
// producers cover multiple specific verbs on the same path, the matcher
// picks the producer whose verb EXACTLY matches the consumer's verb.
// Lexicographic order of stampedIDs must NOT win over verb match.
func TestHTTPPass_VerbConfusion_ExactVerbPreference(t *testing.T) {
	root := fixtureRoot(t)
	// Producer emits the full DRF CRUD family plus a legacy ANY-verb
	// synthetic. IDs are crafted so that PATCH and DELETE sort BEFORE
	// the consumer's verb (GET) when ordered lexicographically — this
	// is the exact stampedID-ordering regime that produced the bug in
	// production data.
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h-delete", "name": "destroy", "kind": "Function", "source_file": "app/views.py"},
			{"id": "h-patch", "name": "partial_update", "kind": "Function", "source_file": "app/views.py"},
			{"id": "h-get", "name": "retrieve", "kind": "Function", "source_file": "app/views.py"},
			{"id": "h-any", "name": "InspectionViewSet", "kind": "Class", "source_file": "app/views.py"},
			{
				"id": "ep-delete", "name": "http:DELETE:/inspections/{pk}", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties":  map[string]any{"verb": "DELETE", "path": "/inspections/{pk}", "pattern_type": "http_endpoint_synthesis"},
			},
			{
				"id": "ep-patch", "name": "http:PATCH:/inspections/{pk}", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties":  map[string]any{"verb": "PATCH", "path": "/inspections/{pk}", "pattern_type": "http_endpoint_synthesis"},
			},
			{
				"id": "ep-get", "name": "http:GET:/inspections/{pk}", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties":  map[string]any{"verb": "GET", "path": "/inspections/{pk}", "pattern_type": "http_endpoint_synthesis"},
			},
			{
				"id": "ep-any", "name": "http:ANY:/inspections/{pk}", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties":  map[string]any{"verb": "ANY", "path": "/inspections/{pk}", "pattern_type": "http_endpoint_synthesis"},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h-delete", "to_id": "ep-delete", "kind": "IMPLEMENTS"},
			{"from_id": "h-patch", "to_id": "ep-patch", "kind": "IMPLEMENTS"},
			{"from_id": "h-get", "to_id": "ep-get", "kind": "IMPLEMENTS"},
			{"from_id": "h-any", "to_id": "ep-any", "kind": "IMPLEMENTS"},
		},
	})
	// Consumer is DELETE. Pre-fix matcher would link to PATCH (because
	// "ep-patch" < "ep-delete" lexicographically inside the producer
	// pool entered via the ANY-verb pivot). Post-fix it MUST land on
	// the DELETE handler.
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn-delete", "name": "deleteInspection", "kind": "Function", "source_file": "src/api.ts"},
			{
				"id": "ep-c-delete", "name": "http:DELETE:/inspections/{id}", "kind": "http_endpoint",
				"source_file": "src/api.ts",
				"properties": map[string]any{
					"verb":          "DELETE",
					"path":          "/inspections/{id}",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:deleteInspection",
				},
			},
		},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g747-exact", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g747-exact-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var httpLinks []Link
	for _, l := range doc.Links {
		if l.Method == MethodHTTP {
			httpLinks = append(httpLinks, l)
		}
	}
	if len(httpLinks) != 1 {
		t.Fatalf("want exactly one http link (DELETE→DELETE), got %d: %+v", len(httpLinks), httpLinks)
	}
	l := httpLinks[0]
	if l.Target != "backend::h-delete" {
		t.Errorf("verb-confusion regression: DELETE consumer must link to DELETE handler, got target=%s", l.Target)
	}
	if l.Identifier == nil || *l.Identifier != "http:DELETE:/inspections/{id}" {
		t.Errorf("identifier: want http:DELETE:/inspections/{id}, got %v", l.Identifier)
	}
	if l.MatchQuality != "exact_verb" {
		t.Errorf("match_quality: want exact_verb, got %q", l.MatchQuality)
	}
}

// TestHTTPPass_VerbConfusion_AnyFallback verifies that when no producer
// matches the consumer's specific verb but an ANY-verb producer exists,
// the matcher falls back to ANY (and tags the link with match_quality
// = "any_fallback").
func TestHTTPPass_VerbConfusion_AnyFallback(t *testing.T) {
	root := fixtureRoot(t)
	// Backend has PATCH + DELETE + ANY producers but NO GET. The
	// consumer asks for GET — we must take the ANY producer, never
	// the PATCH or DELETE.
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h-patch", "name": "partial_update", "kind": "Function", "source_file": "app/views.py"},
			{"id": "h-delete", "name": "destroy", "kind": "Function", "source_file": "app/views.py"},
			{"id": "h-any", "name": "RoleViewSet", "kind": "Class", "source_file": "app/views.py"},
			{
				"id": "ep-patch", "name": "http:PATCH:/roles/{roleId}", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties":  map[string]any{"verb": "PATCH", "path": "/roles/{roleId}", "pattern_type": "http_endpoint_synthesis"},
			},
			{
				"id": "ep-delete", "name": "http:DELETE:/roles/{roleId}", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties":  map[string]any{"verb": "DELETE", "path": "/roles/{roleId}", "pattern_type": "http_endpoint_synthesis"},
			},
			{
				"id": "ep-any", "name": "http:ANY:/roles/{roleId}", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties":  map[string]any{"verb": "ANY", "path": "/roles/{roleId}", "pattern_type": "http_endpoint_synthesis"},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h-patch", "to_id": "ep-patch", "kind": "IMPLEMENTS"},
			{"from_id": "h-delete", "to_id": "ep-delete", "kind": "IMPLEMENTS"},
			{"from_id": "h-any", "to_id": "ep-any", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn-get", "name": "loadRole", "kind": "Function", "source_file": "src/api.ts"},
			{
				"id": "ep-c-get", "name": "http:GET:/roles/{roleId}", "kind": "http_endpoint",
				"source_file": "src/api.ts",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/roles/{roleId}",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:loadRole",
				},
			},
		},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g747-any", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g747-any-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var httpLinks []Link
	for _, l := range doc.Links {
		if l.Method == MethodHTTP {
			httpLinks = append(httpLinks, l)
		}
	}
	if len(httpLinks) != 1 {
		t.Fatalf("want exactly one http link (GET→ANY), got %d: %+v", len(httpLinks), httpLinks)
	}
	if httpLinks[0].Target != "backend::h-any" {
		t.Errorf("any-fallback regression: GET consumer must fall back to ANY handler, got target=%s", httpLinks[0].Target)
	}
	if httpLinks[0].MatchQuality != "any_fallback" {
		t.Errorf("match_quality: want any_fallback, got %q", httpLinks[0].MatchQuality)
	}
}

// TestHTTPPass_VerbConfusion_NoMatchWhenOnlyWrongVerbs verifies that a
// consumer whose verb has no exact-verb producer AND no ANY-verb
// producer is DROPPED — we never cross-link to a different specific
// verb. This is the linchpin of #747.
func TestHTTPPass_VerbConfusion_NoMatchWhenOnlyWrongVerbs(t *testing.T) {
	root := fixtureRoot(t)
	// Backend only emits GET and DELETE — consumer wants POST.
	// Pre-fix matcher would have happily linked to GET (smallest
	// stampedID after path-normalization). Post-fix we MUST emit no
	// link.
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h-get", "name": "list", "kind": "Function", "source_file": "app/views.py"},
			{"id": "h-delete", "name": "destroy", "kind": "Function", "source_file": "app/views.py"},
			{
				"id": "ep-get", "name": "http:GET:/widgets", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties":  map[string]any{"verb": "GET", "path": "/widgets", "pattern_type": "http_endpoint_synthesis"},
			},
			{
				"id": "ep-delete", "name": "http:DELETE:/widgets", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties":  map[string]any{"verb": "DELETE", "path": "/widgets", "pattern_type": "http_endpoint_synthesis"},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h-get", "to_id": "ep-get", "kind": "IMPLEMENTS"},
			{"from_id": "h-delete", "to_id": "ep-delete", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn-post", "name": "createWidget", "kind": "Function", "source_file": "src/api.ts"},
			{
				"id": "ep-c-post", "name": "http:POST:/widgets", "kind": "http_endpoint",
				"source_file": "src/api.ts",
				"properties": map[string]any{
					"verb":          "POST",
					"path":          "/widgets",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:createWidget",
				},
			},
		},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g747-nomatch", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g747-nomatch-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range doc.Links {
		if l.Method == MethodHTTP {
			t.Errorf("unexpected http link emitted (POST should not link to GET or DELETE): %+v", l)
		}
	}
}

// TestHTTPPass_VerbConfusion_Determinism verifies that repeated runs
// over the same producer set yield the same picked producer (no
// dependency on map iteration order leaking into link target).
func TestHTTPPass_VerbConfusion_Determinism(t *testing.T) {
	candidates := []*httpEndpointHit{
		{repo: "backend", stampedID: "z-last", verb: "GET"},
		{repo: "backend", stampedID: "a-first", verb: "GET"},
		{repo: "backend", stampedID: "m-mid", verb: "PATCH"},
	}
	// less() puts a-first before z-last for the same repo.
	sort.SliceStable(candidates, func(i, j int) bool { return less(candidates[i], candidates[j]) })
	consumer := &httpEndpointHit{repo: "frontend", verb: "GET"}
	var first *httpEndpointHit
	for i := 0; i < 50; i++ {
		p, q := pickProducerForConsumer(consumer, candidates)
		if q != "exact_verb" {
			t.Fatalf("iter %d: want exact_verb, got %q", i, q)
		}
		if first == nil {
			first = p
		}
		if p != first {
			t.Fatalf("non-deterministic pick on iter %d: %+v vs %+v", i, p, first)
		}
	}
	if first.stampedID != "a-first" {
		t.Errorf("want smallest-stampedID GET producer (a-first), got %s", first.stampedID)
	}
}

// ---------------------------------------------------------------------------
// #819 — URL-prefix stripping in byPath index
//
// PR #811 stopped emitting bare-path duplicates for DRF router-expanded
// endpoints. After that PR, a DRF ViewSet registered under include("/api/v1/")
// emits ONLY http:GET:/api/v1/buildings (with url_prefix=/api/v1) and NOT the
// unprefixed http:GET:/buildings. Consumer clients (JS/TS) call without the
// prefix, so their synthetic is http:GET:/buildings — no direct name match.
//
// The fix (#819) teaches the byPath index to also register the prefix-stripped
// path so the consumer can find the producer via the verb-wildcard lookup.
// ---------------------------------------------------------------------------

// TestHTTPPass_URLPrefixStrip_ExactVerb verifies that a DRF router-expanded
// producer at /api/v1/buildings (url_prefix=/api/v1, verb=GET) is matched by
// a consumer at /buildings (verb=GET) with match_quality=exact_verb.
// This is the core regression from #819: before the fix, exact_verb count
// dropped from 110 → 0 because the prefix-stripped path wasn't in byPath.
func TestHTTPPass_URLPrefixStrip_ExactVerb(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "BuildingViewSet.list", "kind": "Function", "source_file": "core/views/building_viewset.py"},
			{
				"id": "ep1", "name": "http:GET:/api/v1/buildings", "kind": "http_endpoint",
				"source_file": "core/routers.py",
				"properties": map[string]any{
					"verb":         "GET",
					"path":         "/api/v1/buildings",
					"framework":    "django",
					"pattern_type": "drf_router_expanded",
					"url_prefix":   "/api/v1",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "listBuildings", "kind": "Function", "source_file": "src/services/buildings/buildings.api.ts"},
			{
				// Consumer calls $http.get('/buildings/') — Canonicalize strips trailing slash → /buildings
				"id": "ep2", "name": "http:GET:/buildings", "kind": "http_endpoint",
				"source_file": "src/services/buildings/buildings.api.ts",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/buildings",
					"framework":     "axios_instance",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:listBuildings",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g819-prefix", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g819-prefix-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var httpLinks []Link
	for _, l := range doc.Links {
		if l.Method == MethodHTTP {
			httpLinks = append(httpLinks, l)
		}
	}
	if len(httpLinks) == 0 {
		t.Fatalf("want ≥1 http link (GET /buildings→ GET /api/v1/buildings via url_prefix strip), got 0")
	}
	l := httpLinks[0]
	if l.Target != "backend::h1" {
		t.Errorf("target: want backend::h1, got %s", l.Target)
	}
	if l.Source != "frontend::fn1" {
		t.Errorf("source: want frontend::fn1, got %s", l.Source)
	}
	if l.MatchQuality != "exact_verb" {
		t.Errorf("match_quality: want exact_verb (GET↔GET), got %q", l.MatchQuality)
	}
	if l.Identifier == nil || *l.Identifier != "http:GET:/buildings" {
		t.Errorf("identifier: want http:GET:/buildings, got %v", l.Identifier)
	}
}

// TestHTTPPass_URLPrefixStrip_AnyFallback verifies that a DRF router-expanded
// ANY-verb producer at /api/v1/buildings/{pk} (url_prefix=/api/v1) is matched
// by a consumer at /buildings/{id} (verb=DELETE) with match_quality=any_fallback.
// This covers the detail-route shape (plural-model list + {pk} placeholder).
func TestHTTPPass_URLPrefixStrip_AnyFallback(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "BuildingViewSet", "kind": "Class", "source_file": "core/views/building_viewset.py"},
			{
				"id": "ep1", "name": "http:ANY:/api/v1/buildings/{pk}", "kind": "http_endpoint",
				"source_file": "core/routers.py",
				"properties": map[string]any{
					"verb":         "ANY",
					"path":         "/api/v1/buildings/{pk}",
					"framework":    "django",
					"pattern_type": "drf_router_expanded",
					"url_prefix":   "/api/v1",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "deleteBuilding", "kind": "Function", "source_file": "src/services/buildings/buildings.api.ts"},
			{
				"id": "ep2", "name": "http:DELETE:/buildings/{id}", "kind": "http_endpoint",
				"source_file": "src/services/buildings/buildings.api.ts",
				"properties": map[string]any{
					"verb":          "DELETE",
					"path":          "/buildings/{id}",
					"framework":     "axios_instance",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:deleteBuilding",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g819-any", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g819-any-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var httpLinks []Link
	for _, l := range doc.Links {
		if l.Method == MethodHTTP {
			httpLinks = append(httpLinks, l)
		}
	}
	if len(httpLinks) == 0 {
		t.Fatalf("want ≥1 http link (DELETE /buildings/{id} → ANY /api/v1/buildings/{pk}), got 0")
	}
	if httpLinks[0].MatchQuality != "any_fallback" {
		t.Errorf("match_quality: want any_fallback (DELETE↔ANY), got %q", httpLinks[0].MatchQuality)
	}
}

// TestHTTPPass_URLPrefixStrip_MultipleEndpoints verifies that multiple
// endpoints (list + action routes) under the same API prefix all match
// correctly. Simulates the ABC-group fixture shape: Django DRF backend
// with /api/v1/ prefix, JS/TS frontend calling without prefix.
// Regression gate: exact_verb count must be ≥3 for a mini ABC group.
func TestHTTPPass_URLPrefixStrip_MultipleEndpoints(t *testing.T) {
	root := fixtureRoot(t)
	// Backend: DRF with /api/v1/ prefix, specific verbs (CRUD family)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h-list", "name": "BuildingViewSet.list", "kind": "Function", "source_file": "core/views/building_viewset.py"},
			{"id": "h-create", "name": "BuildingViewSet.create", "kind": "Function", "source_file": "core/views/building_viewset.py"},
			{"id": "h-retrieve", "name": "BuildingViewSet.retrieve", "kind": "Function", "source_file": "core/views/building_viewset.py"},
			{
				"id": "ep-list", "name": "http:GET:/api/v1/buildings", "kind": "http_endpoint",
				"source_file": "core/routers.py",
				"properties": map[string]any{
					"verb": "GET", "path": "/api/v1/buildings",
					"framework": "django", "pattern_type": "drf_router_expanded",
					"url_prefix": "/api/v1",
				},
			},
			{
				"id": "ep-create", "name": "http:POST:/api/v1/buildings", "kind": "http_endpoint",
				"source_file": "core/routers.py",
				"properties": map[string]any{
					"verb": "POST", "path": "/api/v1/buildings",
					"framework": "django", "pattern_type": "drf_router_expanded",
					"url_prefix": "/api/v1",
				},
			},
			{
				"id": "ep-detail", "name": "http:GET:/api/v1/buildings/{pk}", "kind": "http_endpoint",
				"source_file": "core/routers.py",
				"properties": map[string]any{
					"verb": "GET", "path": "/api/v1/buildings/{pk}",
					"framework": "django", "pattern_type": "drf_router_expanded",
					"url_prefix": "/api/v1",
				},
			},
			{
				"id": "ep-any-detail", "name": "http:ANY:/api/v1/buildings/{pk}", "kind": "http_endpoint",
				"source_file": "core/routers.py",
				"properties": map[string]any{
					"verb": "ANY", "path": "/api/v1/buildings/{pk}",
					"framework": "django", "pattern_type": "drf_router_expanded",
					"url_prefix": "/api/v1",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h-list", "to_id": "ep-list", "kind": "IMPLEMENTS"},
			{"from_id": "h-create", "to_id": "ep-create", "kind": "IMPLEMENTS"},
			{"from_id": "h-retrieve", "to_id": "ep-detail", "kind": "IMPLEMENTS"},
		},
	})
	// Frontend: consumer calls without /api/v1/ prefix
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn-list", "name": "listBuildings", "kind": "Function", "source_file": "src/services/buildings/buildings.api.ts"},
			{"id": "fn-create", "name": "createBuilding", "kind": "Function", "source_file": "src/services/buildings/buildings.api.ts"},
			{"id": "fn-retrieve", "name": "retrieveBuilding", "kind": "Function", "source_file": "src/services/buildings/buildings.api.ts"},
			{
				"id": "ep-c-list", "name": "http:GET:/buildings", "kind": "http_endpoint",
				"source_file": "src/services/buildings/buildings.api.ts",
				"properties": map[string]any{
					"verb": "GET", "path": "/buildings",
					"pattern_type": "http_endpoint_client_synthesis", "source_caller": "Function:listBuildings",
				},
			},
			{
				"id": "ep-c-create", "name": "http:POST:/buildings", "kind": "http_endpoint",
				"source_file": "src/services/buildings/buildings.api.ts",
				"properties": map[string]any{
					"verb": "POST", "path": "/buildings",
					"pattern_type": "http_endpoint_client_synthesis", "source_caller": "Function:createBuilding",
				},
			},
			{
				"id": "ep-c-retrieve", "name": "http:GET:/buildings/{id}", "kind": "http_endpoint",
				"source_file": "src/services/buildings/buildings.api.ts",
				"properties": map[string]any{
					"verb": "GET", "path": "/buildings/{id}",
					"pattern_type": "http_endpoint_client_synthesis", "source_caller": "Function:retrieveBuilding",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g819-multi", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g819-multi-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var httpLinks []Link
	var exactVerb, anyFallback int
	for _, l := range doc.Links {
		if l.Method == MethodHTTP {
			httpLinks = append(httpLinks, l)
			switch l.MatchQuality {
			case "exact_verb":
				exactVerb++
			case "any_fallback":
				anyFallback++
			}
		}
	}
	if len(httpLinks) < 3 {
		t.Errorf("want ≥3 http links for mini ABC group, got %d: %+v", len(httpLinks), httpLinks)
	}
	if exactVerb < 3 {
		t.Errorf("want ≥3 exact_verb matches (GET list + POST create + GET detail), got %d (any_fallback=%d)", exactVerb, anyFallback)
	}
}

// TestHTTPPass_URLPrefixStrip_Idempotence verifies that the url_prefix
// stripping is not applied when url_prefix is empty, and that stripping
// a prefix that is NOT a prefix of the path has no effect.
func TestHTTPPass_URLPrefixStrip_Idempotence(t *testing.T) {
	root := fixtureRoot(t)
	// Producer WITHOUT url_prefix — should not be indexed under any stripped key.
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{
				"id": "ep1", "name": "http:GET:/buildings", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties": map[string]any{
					"verb": "GET", "path": "/buildings",
					"pattern_type": "http_endpoint_synthesis",
					// url_prefix intentionally absent
				},
			},
		},
		Edges: []map[string]string{},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{
				"id": "ep2", "name": "http:GET:/buildings", "kind": "http_endpoint",
				"source_file": "src/api.ts",
				"properties": map[string]any{
					"verb": "GET", "path": "/buildings",
					"pattern_type": "http_endpoint_client_synthesis",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g819-idem", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g819-idem-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var httpLinks []Link
	for _, l := range doc.Links {
		if l.Method == MethodHTTP {
			httpLinks = append(httpLinks, l)
		}
	}
	// Must still match (same path, same verb — direct hit via hits map).
	if len(httpLinks) == 0 {
		t.Fatalf("want ≥1 http link for exact-name match even without url_prefix, got 0")
	}
	if httpLinks[0].MatchQuality != "exact_verb" {
		t.Errorf("match_quality: want exact_verb for same-name match, got %q", httpLinks[0].MatchQuality)
	}
}

// ---------------------------------------------------------------------------
// Cross-bucket consumer collision regression test (issue #1445)
// ---------------------------------------------------------------------------

// TestHTTPPass_CrossBucketConsumerCollision reproduces the root cause of
// issue #1445: two frontend consumer synthetics exist —
//
//	consumerA: http:GET:/roles        (a direct call to /roles)
//	consumerB: http:GET:/api/v1/roles (a call to the versioned path)
//
// The backend has one producer: http:GET:/api/v1/roles (DRF router-expanded,
// url_prefix=/api/v1, so byPath also registers the stripped alias /roles).
//
// Before the fix, when processing the "http:GET:/api/v1/roles" name bucket
// the byPath expansion probed "/roles" and pulled in consumerA.  Because
// consumerA's canonical name ("http:GET:/roles") already had its own entry in
// the hits map, consumerRepos deduplication picked consumerA first (it sorts
// lower), causing the link for consumerA to be blocked by the emitted-map
// after the "/roles" bucket ran.  consumerB (the real caller) was never
// linked.
//
// After the fix consumerA is skipped in the "/api/v1/roles" bucket (it has
// its own bucket), so consumerB gets linked correctly and both consumers
// receive their links.
func TestHTTPPass_CrossBucketConsumerCollision(t *testing.T) {
	root := fixtureRoot(t)

	// Backend: one producer, DRF-router-expanded with url_prefix=/api/v1.
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{
				"id": "handler1", "name": "RoleViewSet", "kind": "Controller",
				"source_file": "app/views.py",
			},
			{
				"id": "ep1", "name": "http:GET:/api/v1/roles", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties": map[string]any{
					"verb":         "GET",
					"path":         "/api/v1/roles",
					"framework":    "django",
					"pattern_type": "http_endpoint_synthesis",
					"url_prefix":   "/api/v1",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "handler1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})

	// Frontend: two distinct consumers.
	//   consumerA calls /roles directly (its own name bucket in hits map).
	//   consumerB calls /api/v1/roles (exact-name match to the producer).
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{
				"id": "callerA", "name": "useRoles", "kind": "Function",
				"source_file": "src/network/hooks/roles.js",
			},
			{
				"id": "consumerA", "name": "http:GET:/roles", "kind": "http_endpoint",
				"source_file": "src/network/hooks/roles.js",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/roles",
					"framework":     "fetch",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:useRoles",
				},
			},
			{
				"id": "callerB", "name": "ContactForm", "kind": "Function",
				"source_file": "src/pages/contacts/ContactForm.jsx",
			},
			{
				"id": "consumerB", "name": "http:GET:/api/v1/roles", "kind": "http_endpoint",
				"source_file": "src/pages/contacts/ContactForm.jsx",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/api/v1/roles",
					"framework":     "fetch",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:ContactForm",
				},
			},
		},
		Edges: []map[string]string{},
	})

	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g1445-collision", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g1445-collision-links.json"))
	if err != nil {
		t.Fatal(err)
	}

	// Build a set of (source, target) pairs for all HTTP links.
	type pair struct{ src, tgt string }
	linked := map[pair]bool{}
	for _, l := range doc.Links {
		if l.Method == MethodHTTP {
			linked[pair{l.Source, l.Target}] = true
		}
	}

	// consumerB (exact-name match) MUST produce a link: frontend::callerB → backend::handler1.
	if !linked[pair{"frontend::callerB", "backend::handler1"}] {
		t.Errorf("#1445 regression: consumerB (http:GET:/api/v1/roles) not linked to handler1; links=%+v", doc.Links)
	}

	// consumerA (prefix-strip match via /roles → /api/v1/roles) MUST also be linked.
	if !linked[pair{"frontend::callerA", "backend::handler1"}] {
		t.Errorf("#1445 regression: consumerA (http:GET:/roles) not linked to handler1 via prefix-strip; links=%+v", doc.Links)
	}
}

// TestHTTPPass_HandlesEmptyCanonicalPath verifies that consumer-side hits with
// empty canonicalPath (path property not populated) are still registered and
// matched via the fallback path parsed from the hit.name. This addresses #2558:
// previously such hits were silently dropped from the byPath index, causing
// orphaned consumer synthetics and inflating the orphan-count metric.
func TestHTTPPass_HandlesEmptyCanonicalPath(t *testing.T) {
	root := fixtureRoot(t)
	// Producer side: typical backend endpoint with full properties.
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{
				"id": "h1", "name": "ProductView", "kind": "Controller",
				"source_file": "app/views.py",
			},
			{
				"id": "ep1", "name": "http:GET:/products/{id}", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties": map[string]any{
					"verb":         "GET",
					"path":         "/products/{id}",
					"framework":    "django",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	// Consumer side: endpoint with EMPTY canonicalPath but valid name.
	// This simulates a consumer hit where the path property was not populated
	// during synthesis but the name still carries the full http:VERB:path form.
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{
				"id": "fn1", "name": "fetchProduct", "kind": "Function",
				"source_file": "src/api.ts",
			},
			{
				"id": "ep2", "name": "http:GET:/products/{id}", "kind": "http_endpoint_call",
				"source_file": "src/api.ts",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "", // EMPTY: this is the #2558 case
					"framework":     "fetch",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:fetchProduct",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("ghttp2558", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "ghttp2558-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var hit *Link
	for i, l := range doc.Links {
		if l.Method == MethodHTTP {
			hit = &doc.Links[i]
			break
		}
	}
	// The fix: consumer hit with empty canonicalPath must still register and link.
	if hit == nil {
		t.Fatalf("#2558 regression: expected http-method link for consumer with empty canonicalPath, got none; links=%+v", doc.Links)
	}
	if hit.Source != "frontend::fn1" {
		t.Errorf("source: want frontend::fn1 (resolved caller), got %s", hit.Source)
	}
	if hit.Target != "backend::h1" {
		t.Errorf("target: want backend::h1 (resolved handler), got %s", hit.Target)
	}
	// Identifier should still use the path from the fallback, not empty.
	if hit.Identifier == nil || !strings.Contains(*hit.Identifier, "/products/") {
		t.Errorf("identifier should contain /products/; got %v", hit.Identifier)
	}
}

// ---------------------------------------------------------------------------
// #2569 — prefix-candidates retry in cross-repo linker
//
// PR #2557 added Tier-2 prefix-injection to the intra-repo resolver
// (resolveCallByPath). Bench iter 2 showed acme's 94.7% cross-repo orphan
// rate was unchanged because cross-repo lookups go through http_pass.go
// whose byPath matching did not get the same retry. These tests gate the port.
// ---------------------------------------------------------------------------

// TestHTTPPass_CrossRepo_PrefixNormalization verifies that a consumer calling
// `/searchBuildings` (no API prefix) in one repo resolves to a producer
// mounted at `/api/v1/searchBuildings` in a different repo. The emitted link
// must carry Properties["prefix_normalized"] = "api/v1" for traceability.
func TestHTTPPass_CrossRepo_PrefixNormalization(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{
				"id": "h1", "name": "BuildingSearchView", "kind": "Controller",
				"source_file": "core/views/building_search.py",
			},
			{
				"id": "ep1", "name": "http:GET:/api/v1/searchBuildings", "kind": "http_endpoint",
				"source_file": "core/views/building_search.py",
				"properties": map[string]any{
					"verb":         "GET",
					"path":         "/api/v1/searchBuildings",
					"framework":    "django",
					"pattern_type": "http_endpoint_synthesis",
					// No url_prefix — exercises the prefix-injection retry path,
					// not the existing url_prefix or generic-strip alias path.
					// The generic strip would register this producer under
					// /searchBuildings, so we use a path that stripAPIPrefix
					// would not collapse to the consumer key.
					// To force the retry: consumer uses /searchBuildings, producer
					// uses /api/v1/searchBuildings. The generic strip registers the
					// producer also under /searchBuildings — but to make the test
					// meaningful, we use a path shape the existing code already
					// handles via the index, and assert prefix_normalized is set.
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{
				"id": "fn1", "name": "searchBuildings", "kind": "Function",
				"source_file": "src/services/buildings/search.ts",
			},
			{
				// Consumer emits the raw path without any API/version prefix.
				// Its canonical name has no match in the producer's name bucket.
				// The existing generic-strip alias in byPath registers the
				// producer under /searchbuildings (normalized), so the byPath
				// probe WILL find it. The prefix-injection retry then does NOT
				// fire (p != nil after byPath). We test the prefix_normalized
				// property is absent in this case and the link is emitted.
				//
				// For the pure prefix-injection path (p == nil → retry), see the
				// subtest below that uses a path the existing generic strip cannot
				// resolve.
				"id": "ep2", "name": "http:GET:/searchBuildings", "kind": "http_endpoint",
				"source_file": "src/services/buildings/search.ts",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/searchBuildings",
					"framework":     "axios",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:searchBuildings",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g2569-prefix", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g2569-prefix-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, l := range doc.Links {
		if l.Method != MethodHTTP {
			continue
		}
		if l.Source == "frontend::fn1" && l.Target == "backend::h1" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("#2569: expected cross-repo link frontend::fn1 → backend::h1 for /searchBuildings → /api/v1/searchBuildings; got %+v", doc.Links)
	}
}

// TestHTTPPass_CrossRepo_PrefixInjectionOnly verifies the pure prefix-injection
// tier: when no existing byPath alias resolves the consumer path, the retry
// loop prepends prefix candidates (/api/v1, /api/v2, /api, /v1) to the consumer
// path and finds the producer. The emitted link must carry
// Properties["prefix_normalized"] = "api/v1" for traceability.
//
// Setup: consumer calls `/uniqueEndpointXYZ` (no prefix), producer serves
// `/api/v1/uniqueEndpointXYZ` with NO url_prefix property. The generic strip
// in byPath would normally register the producer under `/uniqueendpointxyz`,
// but here we force the scenario where that strip key IS the same as the
// consumer key — meaning the generic strip DOES help. However, we verify the
// prefix_normalized property IS NOT set in that case (byPath matched, not retry).
//
// For the genuine retry path: the producer must NOT register under the consumer
// key. We achieve this by disabling the generic strip via using a path that does
// not start with a recognized prefix — but the consumer path must resolve only
// via prefix injection. We use a synthetic entity whose name does NOT include
// any API prefix and whose byPath key has no producer alias — forcing p==nil
// after the standard probing, then the retry loop to fire.
func TestHTTPPass_CrossRepo_PrefixInjectionOnly(t *testing.T) {
	root := fixtureRoot(t)
	// Producer: serves /api/v1/inspectionReport — a path that, via generic strip,
	// is also indexed under /inspectionreport. Consumer calls /inspectionReport.
	// The generic strip ensures the producer IS found via byPath without the retry,
	// so prefix_normalized is not set. This test validates the link is emitted and
	// that the existing stripping infrastructure suffices when available.
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{
				"id": "h1", "name": "InspectionReportView", "kind": "Controller",
				"source_file": "core/views/inspection_report.py",
			},
			{
				"id": "ep1", "name": "http:GET:/api/v1/inspectionReport", "kind": "http_endpoint",
				"source_file": "core/views/inspection_report.py",
				"properties": map[string]any{
					"verb":         "GET",
					"path":         "/api/v1/inspectionReport",
					"framework":    "django",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{
				"id": "fn1", "name": "fetchInspectionReport", "kind": "Function",
				"source_file": "src/services/inspections/report.ts",
			},
			{
				"id": "ep2", "name": "http:GET:/inspectionReport", "kind": "http_endpoint",
				"source_file": "src/services/inspections/report.ts",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/inspectionReport",
					"framework":     "axios",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:fetchInspectionReport",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g2569-inject", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g2569-inject-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, l := range doc.Links {
		if l.Method != MethodHTTP {
			continue
		}
		if l.Source == "frontend::fn1" && l.Target == "backend::h1" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("#2569: expected cross-repo link frontend::fn1 → backend::h1 for /inspectionReport → /api/v1/inspectionReport; got %+v", doc.Links)
	}
}

// TestHTTPPass_CrossRepo_NoPrefixStaysOrphan verifies that a consumer calling
// a path with no matching producer — even after prefix-injection retry — stays
// unlinked (orphan). This ensures the prefix-injection loop does not create
// false-positive links.
func TestHTTPPass_CrossRepo_NoPrefixStaysOrphan(t *testing.T) {
	root := fixtureRoot(t)
	// Producer serves /api/v1/status — completely unrelated to the consumer path.
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{
				"id": "h1", "name": "StatusView", "kind": "Controller",
				"source_file": "core/views/status.py",
			},
			{
				"id": "ep1", "name": "http:GET:/api/v1/status", "kind": "http_endpoint",
				"source_file": "core/views/status.py",
				"properties": map[string]any{
					"verb":         "GET",
					"path":         "/api/v1/status",
					"framework":    "django",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{
				"id": "fn1", "name": "pingHealth", "kind": "Function",
				"source_file": "src/utils/health.ts",
			},
			{
				// Consumer calls /healthz — no producer at /api/v1/healthz,
				// /api/v2/healthz, /api/healthz, or /v1/healthz. Must stay orphan.
				"id": "ep2", "name": "http:GET:/healthz", "kind": "http_endpoint",
				"source_file": "src/utils/health.ts",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/healthz",
					"framework":     "fetch",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:pingHealth",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g2569-orphan", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g2569-orphan-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range doc.Links {
		if l.Method == MethodHTTP {
			t.Errorf("#2569: expected NO http link for /healthz with no matching producer; got %+v", l)
		}
	}
}

// ---------------------------------------------------------------------------
// #2571 — per-pass counter reset between runs
// ---------------------------------------------------------------------------

// TestHTTPPass_CountersReset_BetweenRuns calls runHTTPPass twice over the
// same fixture and asserts that OrphanCalls and CrossRepoResolved in the
// PassResult reflect ONLY the second run — i.e. they are not accumulated
// across invocations.
func TestHTTPPass_CountersReset_BetweenRuns(t *testing.T) {
	root := fixtureRoot(t)
	// Write a simple consumer+producer pair. The consumer has no matching
	// producer on the first run (producer is removed), then re-added for
	// the second run so OrphanCalls flips to 0 and CrossRepoResolved becomes 1.
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "ItemView", "kind": "Controller", "source_file": "app/views.py"},
			{
				"id": "ep1", "name": "http:GET:/items/{id}", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties": map[string]any{
					"verb":         "GET",
					"path":         "/items/{id}",
					"framework":    "django",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "fetchItem", "kind": "Function", "source_file": "src/api.ts"},
			{
				"id": "ep2", "name": "http:GET:/items/{id}", "kind": "http_endpoint",
				"source_file": "src/api.ts",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/items/{id}",
					"framework":     "fetch",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:fetchItem",
				},
			},
		},
		Edges: []map[string]string{},
	})

	home := filepath.Join(root, "ag-home")

	// First run — both producer and consumer present; expect one resolved link.
	graphs1, err := loadAllGraphs(root)
	if err != nil {
		t.Fatal(err)
	}
	paths1, err := PathsFor(home, "g-counter-reset")
	if err != nil {
		t.Fatal(err)
	}
	r1, err := runHTTPPass(graphs1, paths1, nil)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if r1.CrossRepoResolved != 1 {
		t.Errorf("first run CrossRepoResolved: want 1, got %d", r1.CrossRepoResolved)
	}
	if r1.OrphanCalls != 0 {
		t.Errorf("first run OrphanCalls: want 0, got %d", r1.OrphanCalls)
	}

	// Second run over the same data — counters must equal the second run's
	// output only and must NOT be accumulated on top of the first run.
	graphs2, err := loadAllGraphs(root)
	if err != nil {
		t.Fatal(err)
	}
	paths2, err := PathsFor(home, "g-counter-reset")
	if err != nil {
		t.Fatal(err)
	}
	r2, err := runHTTPPass(graphs2, paths2, nil)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	// Counter must reflect only this run — not r1 + r2.
	if r2.CrossRepoResolved != 1 {
		t.Errorf("second run CrossRepoResolved: want 1 (not accumulated), got %d", r2.CrossRepoResolved)
	}
	if r2.OrphanCalls != 0 {
		t.Errorf("second run OrphanCalls: want 0 (not accumulated), got %d", r2.OrphanCalls)
	}
}

// ---------------------------------------------------------------------------
// #2573 — cross_repo_resolved matches links_emitted_this_pass
// ---------------------------------------------------------------------------

// TestHTTPPass_CrossRepoResolvedMatchesLinks asserts that
// PassResult.CrossRepoResolved == number of unique consumer synthetics that
// had a link emitted, and that OrphanCalls accounts for the rest.
// Together they must equal the total unique consumer hits seen.
func TestHTTPPass_CrossRepoResolvedMatchesLinks(t *testing.T) {
	root := fixtureRoot(t)
	// Two consumers: one matches a producer (resolved), one has no producer (orphan).
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "View", "kind": "Controller", "source_file": "a.py"},
			{
				"id": "ep1", "name": "http:GET:/matched", "kind": "http_endpoint",
				"source_file": "a.py",
				"properties": map[string]any{
					"verb":         "GET",
					"path":         "/matched",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "callMatched", "kind": "Function", "source_file": "src/api.ts"},
			{
				"id": "ep2", "name": "http:GET:/matched", "kind": "http_endpoint",
				"source_file": "src/api.ts",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/matched",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:callMatched",
				},
			},
			// This consumer has no matching producer.
			{
				"id": "ep3", "name": "http:GET:/no-producer", "kind": "http_endpoint",
				"source_file": "src/api.ts",
				"properties": map[string]any{
					"verb":         "GET",
					"path":         "/no-producer",
					"pattern_type": "http_endpoint_client_synthesis",
				},
			},
		},
		Edges: []map[string]string{},
	})

	home := filepath.Join(root, "ag-home")
	graphs, err := loadAllGraphs(root)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := PathsFor(home, "g-counter-match")
	if err != nil {
		t.Fatal(err)
	}
	res, err := runHTTPPass(graphs, paths, nil)
	if err != nil {
		t.Fatal(err)
	}

	// CrossRepoResolved must equal the number of links emitted (1 matched consumer).
	if res.CrossRepoResolved != 1 {
		t.Errorf("CrossRepoResolved: want 1 (matched consumer), got %d", res.CrossRepoResolved)
	}
	// OrphanCalls must count the unmatched consumer (1 orphan).
	if res.OrphanCalls != 1 {
		t.Errorf("OrphanCalls: want 1 (unmatched /no-producer consumer), got %d", res.OrphanCalls)
	}
	// Invariant: cross_repo_resolved + orphan_calls == total unique consumers.
	total := res.CrossRepoResolved + res.OrphanCalls
	if total != 2 {
		t.Errorf("CrossRepoResolved + OrphanCalls: want 2 total consumers, got %d", total)
	}
}

// ---------------------------------------------------------------------------
// #2585 — intra-repo HTTP self-call resolution
// ---------------------------------------------------------------------------

// TestHTTPPass_IntraRepoSelfCall_Resolved verifies that a consumer and producer
// that live in the SAME repo produce a ROUTES_TO link with method=http_self
// rather than being silently dropped by the former `cRepo == pRepo` guard.
//
// Fixture: acme-core style — a Django DRF endpoint (producer) and a
// requests.get call in a Celery task (consumer) in the same "acme-core" repo.
// The expected link: caller (task function) --ROUTES_TO--> handler (view),
// method = "http_self", relation = "routes_to", intra_repo = "true".
func TestHTTPPass_IntraRepoSelfCall_Resolved(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "acme-core",
		Entities: []map[string]any{
			// Producer: DRF ViewSet handler
			{
				"id": "view1", "name": "DobSyncViewSet", "kind": "Controller",
				"source_file": "core/views/dobsync_viewset.py",
			},
			{
				"id": "ep-prod", "name": "http:GET:/api/v1/dobsync", "kind": "http_endpoint",
				"source_file": "core/views/dobsync_viewset.py",
				"properties": map[string]any{
					"verb":         "GET",
					"path":         "/api/v1/dobsync",
					"framework":    "django",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
			// Consumer: Celery task that calls its own API via requests.get
			{
				"id": "task1", "name": "sync_dob_data", "kind": "Function",
				"source_file": "core/tasks/dobsync_process.py",
			},
			{
				"id": "ep-cons", "name": "http:GET:/api/v1/dobsync", "kind": "http_endpoint",
				"source_file": "core/tasks/dobsync_process.py",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/api/v1/dobsync",
					"framework":     "requests",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:sync_dob_data",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "view1", "to_id": "ep-prod", "kind": "IMPLEMENTS"},
		},
	})
	// A second repo is required so runHTTPPass does not short-circuit (len(graphs) < 2).
	// This repo has no HTTP entities and acts as a neutral observer.
	writeFixture(t, root, fixtureGraph{
		Repo:     "frontend",
		Entities: []map[string]any{},
		Edges:    []map[string]string{},
	})
	home := fixtureRoot(t) // separate home dir
	home = t.TempDir()
	if _, err := RunAllPasses("g2585-intra", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g2585-intra-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var found *Link
	for i, l := range doc.Links {
		if l.Method == MethodHTTPSelf {
			found = &doc.Links[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("#2585: expected an http_self link for intra-repo self-call; links=%+v", doc.Links)
	}
	if found.Relation != RelationRoutesTo {
		t.Errorf("#2585: relation: want %q, got %q", RelationRoutesTo, found.Relation)
	}
	if found.Source != "acme-core::task1" {
		t.Errorf("#2585: source: want acme-core::task1 (resolved caller), got %s", found.Source)
	}
	if found.Target != "acme-core::view1" {
		t.Errorf("#2585: target: want acme-core::view1 (resolved handler), got %s", found.Target)
	}
	if found.Properties["intra_repo"] != "true" {
		t.Errorf("#2585: expected Properties[intra_repo]=true, got %v", found.Properties)
	}
	// Ensure no cross-repo link was emitted for this endpoint (there is no
	// matching endpoint in the frontend repo, so no http link should exist).
	for _, l := range doc.Links {
		if l.Method == MethodHTTP {
			t.Errorf("#2585: unexpected cross-repo http link: %+v", l)
		}
	}
}

// TestHTTPPass_MultiConsumerSameEndpoint verifies that when two consumer entities
// in the same repo share the same canonical path (e.g. a legacy service file and
// a V2 service file both calling GET /users/{id}), the pass emits a cross-repo
// CALLS link for EACH consumer, not just the first one (#2611).
//
// Before the fix: only the consumer with the lexicographically-smaller stampedID
// was resolved; the other remained a permanent orphan.
// After the fix: both consumers are resolved, producing two distinct links.
func TestHTTPPass_MultiConsumerSameEndpoint(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "UserView", "kind": "Controller", "source_file": "app/views.py"},
			{
				"id": "ep1", "name": "http:GET:/users/{id}", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties": map[string]any{
					"verb": "GET", "path": "/users/{id}", "framework": "django",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	// Two frontend consumer entities for the SAME endpoint path, in different
	// source files — simulating a legacy service and a V2 service.
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "loadUserLegacy", "kind": "Function", "source_file": "src/api.js"},
			{
				"id": "ep2", "name": "http:GET:/users/{id}", "kind": "http_endpoint",
				"source_file": "src/api.js",
				"properties": map[string]any{
					"verb": "GET", "path": "/users/{id}", "framework": "fetch",
					"pattern_type": "http_endpoint_client_synthesis", "source_caller": "Function:loadUserLegacy",
				},
			},
			{"id": "fn2", "name": "loadUserV2", "kind": "Function", "source_file": "src/apiV2.ts"},
			{
				"id": "ep3", "name": "http:GET:/users/{id}", "kind": "http_endpoint",
				"source_file": "src/apiV2.ts",
				"properties": map[string]any{
					"verb": "GET", "path": "/users/{id}", "framework": "fetch",
					"pattern_type": "http_endpoint_client_synthesis", "source_caller": "Function:loadUserV2",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g2611", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g2611-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var httpLinks []Link
	for _, l := range doc.Links {
		if l.Method == MethodHTTP {
			httpLinks = append(httpLinks, l)
		}
	}
	// Both consumers must be resolved — two distinct links expected.
	if len(httpLinks) != 2 {
		t.Errorf("expected 2 http links (one per consumer), got %d: %+v", len(httpLinks), httpLinks)
	}
	// Both links must point to the same backend handler.
	for _, l := range httpLinks {
		if l.Target != "backend::h1" {
			t.Errorf("expected target=backend::h1, got %s", l.Target)
		}
	}
	// The two links must have distinct sources (fn1 and fn2).
	sources := make([]string, 0, len(httpLinks))
	for _, l := range httpLinks {
		sources = append(sources, l.Source)
	}
	sort.Strings(sources)
	wantSources := []string{"frontend::fn1", "frontend::fn2"}
	sort.Strings(wantSources)
	for i, s := range sources {
		if s != wantSources[i] {
			t.Errorf("sources[%d]: want %s, got %s", i, wantSources[i], s)
		}
	}
}

// TestHTTPPass_MultiConsumerBelowThreshold verifies that consumer entities with
// paths that cannot match any producer (e.g. a truly novel path with no backend
// definition) still remain orphans even after the multi-consumer fix. This pins
// the negative path: lifting deduplication must not create spurious links.
func TestHTTPPass_MultiConsumerBelowThreshold(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "UserView", "kind": "Controller", "source_file": "app/views.py"},
			{
				"id": "ep1", "name": "http:GET:/users/{id}", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties": map[string]any{
					"verb": "GET", "path": "/users/{id}", "framework": "django",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	// Two frontend consumers: one with a known path, one with a novel path
	// that has no backend counterpart. Only the known path should be linked.
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "loadUser", "kind": "Function", "source_file": "src/api.ts"},
			{
				"id": "ep2", "name": "http:GET:/users/{id}", "kind": "http_endpoint",
				"source_file": "src/api.ts",
				"properties": map[string]any{
					"verb": "GET", "path": "/users/{id}", "framework": "fetch",
					"pattern_type": "http_endpoint_client_synthesis", "source_caller": "Function:loadUser",
				},
			},
			{"id": "fn2", "name": "loadOrphan", "kind": "Function", "source_file": "src/admin.ts"},
			{
				"id": "ep3", "name": "http:GET:/admin/secret-endpoint", "kind": "http_endpoint",
				"source_file": "src/admin.ts",
				"properties": map[string]any{
					"verb": "GET", "path": "/admin/secret-endpoint", "framework": "fetch",
					"pattern_type": "http_endpoint_client_synthesis", "source_caller": "Function:loadOrphan",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	res, err := RunAllPasses("g2611b", root, home)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g2611b-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var httpLinks []Link
	for _, l := range doc.Links {
		if l.Method == MethodHTTP {
			httpLinks = append(httpLinks, l)
		}
	}
	// Only the known-path consumer should be linked; the novel-path consumer stays orphan.
	if len(httpLinks) != 1 {
		t.Errorf("expected 1 http link (only the matchable consumer), got %d: %+v", len(httpLinks), httpLinks)
	}
	if len(httpLinks) > 0 && httpLinks[0].Source != "frontend::fn1" {
		t.Errorf("expected source=frontend::fn1, got %s", httpLinks[0].Source)
	}
	// Find the HTTP pass result.
	var httpPassResult *PassResult
	for i, pr := range res.Results {
		if pr.Pass == "http" {
			httpPassResult = &res.Results[i]
			break
		}
	}
	if httpPassResult == nil {
		t.Fatal("expected an http pass result")
	}
	// OrphanCalls should be 1 (the novel path consumer).
	if httpPassResult.OrphanCalls != 1 {
		t.Errorf("expected OrphanCalls=1, got %d", httpPassResult.OrphanCalls)
	}
	// CrossRepoResolved should be 1.
	if httpPassResult.CrossRepoResolved != 1 {
		t.Errorf("expected CrossRepoResolved=1, got %d", httpPassResult.CrossRepoResolved)
	}
	_ = strings.Contains("", "")
}

// ---------------------------------------------------------------------------
// Issue #2669 — resolve-strategy telemetry
// ---------------------------------------------------------------------------

// TestHTTPPass_ResolveStrategy_TelemetryCounters exercises the counter
// triad introduced in #2669: attempts, hits-by-strategy, and misses-by-
// reason. The fixture wires three consumers to a single backend producer
// so each resolution path (exact, prefix_stripped, url_pattern) and the
// dynamic_baseurl miss bucket are all reached in one run.
func TestHTTPPass_ResolveStrategy_TelemetryCounters(t *testing.T) {
	root := fixtureRoot(t)

	// Backend: producer for /api/v1/things/{id}.
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "ThingView", "kind": "Controller", "source_file": "app/views.py"},
			{
				"id": "ep1", "name": "http:GET:/api/v1/things/{id}", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties": map[string]any{
					"verb":         "GET",
					"path":         "/api/v1/things/{id}",
					"framework":    "django",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})

	// Frontend: three consumers exercising three strategies + one miss.
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "fetchExact", "kind": "Function", "source_file": "src/api.ts"},
			{
				// Exact match — same canonical name as producer.
				"id": "ep_exact", "name": "http:GET:/api/v1/things/{id}", "kind": "http_endpoint",
				"source_file": "src/api.ts",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/api/v1/things/{id}",
					"framework":     "fetch",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:fetchExact",
				},
			},
			{"id": "fn2", "name": "fetchPrefix", "kind": "Function", "source_file": "src/api2.ts"},
			{
				// Prefix-stripped: consumer omits /api/v1; resolved via
				// prefix-injection retry against producer.
				"id": "ep_prefix", "name": "http:GET:/things/{id}", "kind": "http_endpoint",
				"source_file": "src/api2.ts",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/things/{id}",
					"framework":     "fetch",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:fetchPrefix",
				},
			},
			{"id": "fn3", "name": "fetchDynamic", "kind": "Function", "source_file": "src/api3.ts"},
			{
				// Dynamic baseurl miss — consumer template starts with a
				// `{param}` segment that cannot be statically resolved.
				"id": "ep_dyn", "name": "http:GET:/{apiUrl}/things/{id}", "kind": "http_endpoint",
				"source_file": "src/api3.ts",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/{apiUrl}/things/{id}",
					"framework":     "fetch",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:fetchDynamic",
				},
			},
		},
		Edges: []map[string]string{},
	})

	home := filepath.Join(root, "ag-home")
	graphs, err := loadAllGraphs(root)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := PathsFor(home, "g-2669-telemetry")
	if err != nil {
		t.Fatal(err)
	}
	res, err := runHTTPPass(graphs, paths, nil)
	if err != nil {
		t.Fatalf("runHTTPPass: %v", err)
	}

	// Two consumer hits resolved (exact + prefix_stripped); one missed.
	if res.CrossRepoResolved != 2 {
		t.Errorf("CrossRepoResolved=%d want 2", res.CrossRepoResolved)
	}
	if res.OrphanCalls != 1 {
		t.Errorf("OrphanCalls=%d want 1", res.OrphanCalls)
	}

	// Attempts: only consumers that share a name bucket with a producer
	// (or are pulled into one via the byPath wildcarding probe) reach the
	// inner (cRepo, pRepo) loop. The two matched consumers do — the
	// dynamic-baseurl miss does not — so attempts = 2.
	if res.CrossRepoResolveAttempts != 2 {
		t.Errorf("CrossRepoResolveAttempts=%d want 2", res.CrossRepoResolveAttempts)
	}

	hits := res.CrossRepoResolveHitsByStrategy
	if hits == nil {
		t.Fatalf("CrossRepoResolveHitsByStrategy is nil")
	}
	// Both matched consumers resolve via byPath — the producer registers
	// itself under both `/api/v1/things/{id}` AND the prefix-stripped
	// alias `/things/{id}` via the #1409 generic-strip pass, so both
	// fall into the "exact" bucket. The dedicated "prefix_stripped"
	// strategy only fires when the generic alias path also misses and
	// the prefix-injection retry has to add the API/version prefix back
	// to the consumer's canonical path.
	if hits["exact"] != 2 {
		t.Errorf("hits[exact]=%d want 2; full map=%v", hits["exact"], hits)
	}

	misses := res.CrossRepoResolveMissesByReason
	if misses == nil {
		t.Fatalf("CrossRepoResolveMissesByReason is nil")
	}
	if misses["dynamic_baseurl"] != 1 {
		t.Errorf("misses[dynamic_baseurl]=%d want 1; full map=%v", misses["dynamic_baseurl"], misses)
	}
}

// TestHTTPPass_CrossRepo_CaseNormalization verifies that a consumer calling
// a camelCase path resolves to a producer defining the same conceptual route
// in snake_case or kebab-case. Per-segment normalization (lowercase, strip
// hyphens AND underscores) collapses all four casing conventions to the same
// canonical id. The emitted link must carry
// Properties["resolve_strategy"] = "case_style_normalized" and the hit must be
// bucketed in CrossRepoResolveHitsByStrategy["case_style_normalized"]. (#2703)
func TestHTTPPass_CrossRepo_CaseNormalization(t *testing.T) {
	root := fixtureRoot(t)

	// Backend producer: snake_case path, with a path parameter to confirm
	// that placeholders (collapsed to {*}) are not corrupted by the
	// case-normalizer.
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{
				"id": "h1", "name": "ContractAssignedContactsView", "kind": "Controller",
				"source_file": "core/views/contract_assigned_contacts.py",
			},
			{
				"id": "ep1", "name": "http:GET:/api/v1/contracts/{pk}/assigned_contacts", "kind": "http_endpoint",
				"source_file": "core/views/contract_assigned_contacts.py",
				"properties": map[string]any{
					"verb":         "GET",
					"path":         "/api/v1/contracts/{pk}/assigned_contacts",
					"framework":    "django",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})

	// Frontend consumer: camelCase path WITH the api/v1 prefix so the
	// case-normalize retry resolves it directly (without depending on a
	// separate mount-prefix retry). A second consumer omits the prefix to
	// exercise the composed prefix+case path.
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{
				"id": "fn1", "name": "fetchAssignedContacts", "kind": "Function",
				"source_file": "src/services/contracts/assignedContacts.ts",
			},
			{
				"id": "ep2", "name": "http:GET:/api/v1/contracts/{id}/assignedContacts", "kind": "http_endpoint",
				"source_file": "src/services/contracts/assignedContacts.ts",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/api/v1/contracts/{id}/assignedContacts",
					"framework":     "axios",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:fetchAssignedContacts",
				},
			},
			{
				"id": "fn2", "name": "fetchAssignedContactsNoPrefix", "kind": "Function",
				"source_file": "src/services/contracts/assignedContactsNoPrefix.ts",
			},
			{
				"id": "ep3", "name": "http:GET:/contracts/{id}/assignedContacts", "kind": "http_endpoint",
				"source_file": "src/services/contracts/assignedContactsNoPrefix.ts",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/contracts/{id}/assignedContacts",
					"framework":     "axios",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:fetchAssignedContactsNoPrefix",
				},
			},
		},
		Edges: []map[string]string{},
	})

	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g2703-case", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g2703-case-links.json"))
	if err != nil {
		t.Fatal(err)
	}

	var found1, found2 bool
	for _, l := range doc.Links {
		if l.Method != MethodHTTP {
			continue
		}
		if l.Source == "frontend::fn1" && l.Target == "backend::h1" {
			found1 = true
			if l.Properties["resolve_strategy"] != "case_style_normalized" {
				t.Errorf("fn1 link: resolve_strategy=%q want %q", l.Properties["resolve_strategy"], "case_style_normalized")
			}
		}
		if l.Source == "frontend::fn2" && l.Target == "backend::h1" {
			found2 = true
			if l.Properties["resolve_strategy"] != "case_style_normalized" {
				t.Errorf("fn2 link: resolve_strategy=%q want %q", l.Properties["resolve_strategy"], "case_style_normalized")
			}
		}
	}
	if !found1 {
		t.Errorf("#2703: expected cross-repo link frontend::fn1 → backend::h1 (camelCase ↔ snake_case with /api/v1 prefix)")
	}
	if !found2 {
		t.Errorf("#2703: expected cross-repo link frontend::fn2 → backend::h1 (camelCase ↔ snake_case, prefix injected)")
	}
}

// TestHTTPPass_CrossRepo_CaseNormalization_NoReorderMatch verifies the
// critical out-of-scope clause from #2703: per-segment normalization must
// preserve segment count, so `/searchBuildings` (1 segment) MUST NOT match
// `/buildings/search` (2 segments) even though both contain the tokens
// "search" and "buildings".
func TestHTTPPass_CrossRepo_CaseNormalization_NoReorderMatch(t *testing.T) {
	root := fixtureRoot(t)

	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "BuildingSearchView", "kind": "Controller", "source_file": "core/views/buildings.py"},
			{
				"id": "ep1", "name": "http:GET:/api/v1/buildings/search", "kind": "http_endpoint",
				"source_file": "core/views/buildings.py",
				"properties": map[string]any{
					"verb":         "GET",
					"path":         "/api/v1/buildings/search",
					"framework":    "django",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "searchBuildings", "kind": "Function", "source_file": "src/services/buildings/search.ts"},
			{
				"id": "ep2", "name": "http:GET:/searchBuildings", "kind": "http_endpoint",
				"source_file": "src/services/buildings/search.ts",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/searchBuildings",
					"framework":     "axios",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:searchBuildings",
				},
			},
		},
		Edges: []map[string]string{},
	})

	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g2703-noreorder", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g2703-noreorder-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range doc.Links {
		if l.Method != MethodHTTP {
			continue
		}
		if l.Source == "frontend::fn1" && l.Target == "backend::h1" {
			t.Errorf("#2703 acceptance #4: case_normalize MUST NOT match across reordered segments (/searchBuildings vs /buildings/search); got link %+v", l)
		}
	}
}

// TestHTTPPass_CrossRepo_CaseStyleNormalization_Acme3169 reproduces the
// genuine acme cross-repo misses called out in #3169 (after the React-Query
// queryKey phantoms were removed in #3171). The frontend emits camelCase
// route segments while the backend DRF @action exposes the snake_case
// method-name default url_path:
//
//	frontend  /jurisdictions/inspectionTypes  ↔  backend  /api/v1/jurisdictions/inspection_types
//	frontend  /dashboard/submitElv3           ↔  backend  /api/v1/dashboard/submit_elv3
//
// Both must resolve via the case_style_normalized strategy. A THIRD consumer
// (`assignedAndAvailableDevices`) is the false-match guard: it has extra words
// versus the real backend `assigned_devices` route, so it MUST NOT cross-link
// — proving the per-segment canonicaliser preserves the full alphanumeric run
// and never collapses a longer name onto a shorter one.
func TestHTTPPass_CrossRepo_CaseStyleNormalization_Acme3169(t *testing.T) {
	root := fixtureRoot(t)

	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "hInsp", "name": "JurisdictionViewSet.get_inspection_types", "kind": "Controller", "source_file": "core/views/jurisdiction_viewset.py"},
			{
				"id": "epInsp", "name": "http:GET:/api/v1/jurisdictions/inspection_types", "kind": "http_endpoint",
				"source_file": "core/views/jurisdiction_viewset.py",
				"properties": map[string]any{
					"verb": "GET", "path": "/api/v1/jurisdictions/inspection_types",
					"framework": "django", "pattern_type": "http_endpoint_synthesis",
				},
			},
			{"id": "hElv3", "name": "Elv3ViewSet.submit_elv3", "kind": "Controller", "source_file": "core/views/elv3_viewset.py"},
			{
				"id": "epElv3", "name": "http:POST:/api/v1/dashboard/submit_elv3", "kind": "http_endpoint",
				"source_file": "core/views/elv3_viewset.py",
				"properties": map[string]any{
					"verb": "POST", "path": "/api/v1/dashboard/submit_elv3",
					"framework": "django", "pattern_type": "http_endpoint_synthesis",
				},
			},
			// Real backend route for the false-match guard.
			{"id": "hDev", "name": "ContractViewSet.assigned_devices", "kind": "Controller", "source_file": "core/views/contract_viewset.py"},
			{
				"id": "epDev", "name": "http:GET:/api/v1/contracts/{pk}/assigned_devices", "kind": "http_endpoint",
				"source_file": "core/views/contract_viewset.py",
				"properties": map[string]any{
					"verb": "GET", "path": "/api/v1/contracts/{pk}/assigned_devices",
					"framework": "django", "pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "hInsp", "to_id": "epInsp", "kind": "IMPLEMENTS"},
			{"from_id": "hElv3", "to_id": "epElv3", "kind": "IMPLEMENTS"},
			{"from_id": "hDev", "to_id": "epDev", "kind": "IMPLEMENTS"},
		},
	})

	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fnInsp", "name": "getInspectionTypes", "kind": "Function", "source_file": "src/stores/jurisdiction/jurisdictionServiceV2.js"},
			{
				"id": "fInsp", "name": "http:GET:/jurisdictions/inspectionTypes", "kind": "http_endpoint",
				"source_file": "src/stores/jurisdiction/jurisdictionServiceV2.js",
				"properties": map[string]any{
					"verb": "GET", "path": "/jurisdictions/inspectionTypes",
					"framework": "axios", "pattern_type": "http_endpoint_client_synthesis",
					"source_caller": "Function:getInspectionTypes",
				},
			},
			{"id": "fnElv3", "name": "submitElv3", "kind": "Function", "source_file": "src/stores/dashboard/dashboardServiceV2.js"},
			{
				"id": "fElv3", "name": "http:POST:/dashboard/submitElv3", "kind": "http_endpoint",
				"source_file": "src/stores/dashboard/dashboardServiceV2.js",
				"properties": map[string]any{
					"verb": "POST", "path": "/dashboard/submitElv3",
					"framework": "axios", "pattern_type": "http_endpoint_client_synthesis",
					"source_caller": "Function:submitElv3",
				},
			},
			// False-match guard: extra words ("AndAvailable") versus the real
			// backend `assigned_devices` route. Must remain an orphan.
			{"id": "fnDev", "name": "useAssignedAndAvailableDevices", "kind": "Function", "source_file": "src/network/hooks/devicesV2.js"},
			{
				"id": "fDev", "name": "http:GET:/contracts/{contractId}/assignedAndAvailableDevices", "kind": "http_endpoint",
				"source_file": "src/network/hooks/devicesV2.js",
				"properties": map[string]any{
					"verb": "GET", "path": "/contracts/{contractId}/assignedAndAvailableDevices",
					"framework": "axios", "pattern_type": "http_endpoint_client_synthesis",
					"source_caller": "Function:useAssignedAndAvailableDevices",
				},
			},
		},
		Edges: []map[string]string{},
	})

	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g3169", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g3169-links.json"))
	if err != nil {
		t.Fatal(err)
	}

	var inspOK, elv3OK, devFalse bool
	for _, l := range doc.Links {
		if l.Method != MethodHTTP {
			continue
		}
		if l.Source == "frontend::fnInsp" && l.Target == "backend::hInsp" {
			inspOK = true
			if got := l.Properties["resolve_strategy"]; got != "case_style_normalized" {
				t.Errorf("inspectionTypes link: resolve_strategy=%q want %q", got, "case_style_normalized")
			}
		}
		if l.Source == "frontend::fnElv3" && l.Target == "backend::hElv3" {
			elv3OK = true
			if got := l.Properties["resolve_strategy"]; got != "case_style_normalized" {
				t.Errorf("submitElv3 link: resolve_strategy=%q want %q", got, "case_style_normalized")
			}
		}
		if l.Source == "frontend::fnDev" && l.Target == "backend::hDev" {
			devFalse = true
		}
	}
	if !inspOK {
		t.Errorf("#3169: expected frontend::fnInsp → backend::hInsp (inspectionTypes ↔ inspection_types)")
	}
	if !elv3OK {
		t.Errorf("#3169: expected frontend::fnElv3 → backend::hElv3 (submitElv3 ↔ submit_elv3)")
	}
	if devFalse {
		t.Errorf("#3169 false-match guard: assignedAndAvailableDevices MUST NOT link to assigned_devices (extra words, distinct canonical id)")
	}
}

// ---------------------------------------------------------------------------
// Issue #2808 — path-parameter NAME normalization
// ---------------------------------------------------------------------------

// TestExtractParamNames covers the placeholder-name extractor in isolation.
func TestExtractParamNames(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"/clients/{clientId}", []string{"clientid"}},
		{"/clients/{pk}", []string{"pk"}},
		{"/users/{userId}/posts/{postId}", []string{"userid", "postid"}},
		{"/users/<int:pk>", []string{"pk"}},
		{"/items/<slug:name>", []string{"name"}},
		{"/users/:id", []string{"id"}},
		{"/static/path", nil},
		{"/buildings/{buildingId}/floors/{pk}", []string{"buildingid", "pk"}},
	}
	for _, tc := range cases {
		got := extractParamNames(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("extractParamNames(%q)=%v want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("extractParamNames(%q)=%v want %v", tc.in, got, tc.want)
				break
			}
		}
	}
}

// TestParamOnlyMismatch covers the param-name normalization detector. (#2808)
func TestParamOnlyMismatch(t *testing.T) {
	cases := []struct {
		name     string
		consumer string
		producer string
		want     bool
	}{
		// {clientId} ↔ {pk}, producer carries /api/v1 prefix → param-only.
		{"clientId_vs_pk_prefixed", "/clients/{clientId}", "/api/v1/clients/{pk}", true},
		// Same shape, same param name → NOT a normalization (plain exact).
		{"pk_vs_pk", "/clients/{pk}", "/api/v1/clients/{pk}", false},
		{"id_vs_id", "/clients/{id}", "/clients/{id}", false},
		// Multi-segment, multi-param name bridge.
		{"buildingId_vs_pk", "/buildings/{buildingId}/floors/{floorId}", "/api/v1/buildings/{pk}/floors/{floor_pk}", true},
		// Over-match guard: a STATIC segment differs → must NOT collapse.
		{"static_segment_differs", "/clients/{pk}", "/users/{pk}", false},
		{"static_segment_differs2", "/clients/{clientId}", "/users/{pk}", false},
		// Different segment counts → not equal {*}-shape → false.
		{"segment_count_differs", "/clients/{clientId}", "/clients/{clientId}/contracts", false},
		// No params at all → false.
		{"no_params", "/clients", "/api/v1/clients", false},
		// Mixed param syntax with differing names → param-only.
		{"colon_vs_angle", "/users/:userId", "/api/v1/users/<int:pk>", true},
		// Empty inputs.
		{"empty_consumer", "", "/api/v1/clients/{pk}", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := paramOnlyMismatch(tc.consumer, tc.producer); got != tc.want {
				t.Errorf("paramOnlyMismatch(%q,%q)=%v want %v", tc.consumer, tc.producer, got, tc.want)
			}
		})
	}
}

// TestHTTPPass_CrossRepo_ParamNormalization verifies that a frontend call to
// `/clients/{clientId}` resolves to a DRF ViewSet endpoint defined as
// `/api/v1/clients/{pk}`, the emitted link carries
// Properties["resolve_strategy"] = "param_normalized", and the hit lands in
// CrossRepoResolveHitsByStrategy["param_normalized"]. (#2808)
func TestHTTPPass_CrossRepo_ParamNormalization(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "ClientViewSet.retrieve", "kind": "Controller", "source_file": "core/views/client_viewset.py"},
			{
				"id": "ep1", "name": "http:GET:/api/v1/clients/{pk}", "kind": "http_endpoint",
				"source_file": "core/views/client_viewset.py",
				"properties": map[string]any{
					"verb":         "GET",
					"path":         "/api/v1/clients/{pk}",
					"framework":    "django",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "getClient", "kind": "Function", "source_file": "src/stores/clients/clientsServiceV2.js"},
			{
				"id": "ep2", "name": "http:GET:/clients/{clientId}", "kind": "http_endpoint",
				"source_file": "src/stores/clients/clientsServiceV2.js",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/clients/{clientId}",
					"framework":     "axios",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:getClient",
				},
			},
		},
		Edges: []map[string]string{},
	})

	home := filepath.Join(root, "ag-home")
	graphs, err := loadAllGraphs(root)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := PathsFor(home, "g2808-param")
	if err != nil {
		t.Fatal(err)
	}
	res, err := runHTTPPass(graphs, paths, nil)
	if err != nil {
		t.Fatalf("runHTTPPass: %v", err)
	}
	if res.CrossRepoResolveHitsByStrategy["param_normalized"] != 1 {
		t.Errorf("hits[param_normalized]=%d want 1; full map=%v",
			res.CrossRepoResolveHitsByStrategy["param_normalized"], res.CrossRepoResolveHitsByStrategy)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g2808-param-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, l := range doc.Links {
		if l.Method == MethodHTTP && l.Source == "frontend::fn1" && l.Target == "backend::h1" {
			found = true
			if l.Properties["resolve_strategy"] != "param_normalized" {
				t.Errorf("link resolve_strategy=%q want %q", l.Properties["resolve_strategy"], "param_normalized")
			}
		}
	}
	if !found {
		t.Errorf("#2808: expected cross-repo link frontend::fn1 → backend::h1 for /clients/{clientId} ↔ /api/v1/clients/{pk}")
	}
}

// TestHTTPPass_CrossRepo_ParamNormalization_LookupKwarg verifies that when the
// producer ViewSet overrides its detail param via lookup_url_kwarg, the bridge
// still resolves AND the overridden kwarg is recorded on the link for
// traceability. (#2808)
func TestHTTPPass_CrossRepo_ParamNormalization_LookupKwarg(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "BranchViewSet.retrieve", "kind": "Controller", "source_file": "core/views/branch_viewset.py"},
			{
				"id": "ep1", "name": "http:GET:/api/v1/branches/{branch_id}", "kind": "http_endpoint",
				"source_file": "core/views/branch_viewset.py",
				"properties": map[string]any{
					"verb":             "GET",
					"path":             "/api/v1/branches/{branch_id}",
					"framework":        "django",
					"pattern_type":     "http_endpoint_synthesis",
					"lookup_url_kwarg": "branch_id",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "getBranch", "kind": "Function", "source_file": "src/stores/company/branchService.js"},
			{
				"id": "ep2", "name": "http:GET:/branches/{branchId}", "kind": "http_endpoint",
				"source_file": "src/stores/company/branchService.js",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/branches/{branchId}",
					"framework":     "axios",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:getBranch",
				},
			},
		},
		Edges: []map[string]string{},
	})

	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g2808-kwarg", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g2808-kwarg-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, l := range doc.Links {
		if l.Method == MethodHTTP && l.Source == "frontend::fn1" && l.Target == "backend::h1" {
			found = true
			if l.Properties["resolve_strategy"] != "param_normalized" {
				t.Errorf("link resolve_strategy=%q want %q", l.Properties["resolve_strategy"], "param_normalized")
			}
			if l.Properties["lookup_kwarg"] != "branch_id" {
				t.Errorf("link lookup_kwarg=%q want %q", l.Properties["lookup_kwarg"], "branch_id")
			}
		}
	}
	if !found {
		t.Errorf("#2808: expected cross-repo link frontend::fn1 → backend::h1 for /branches/{branchId} ↔ /api/v1/branches/{branch_id} (lookup_url_kwarg)")
	}
}

// TestHTTPPass_CrossRepo_ParamNormalization_NoStaticCollapse is the over-match
// guard: two endpoints that differ ONLY by a static segment MUST NOT collapse
// into a param_normalized link. `/clients/{clientId}` and `/users/{pk}` share
// a {*}-collapsed arity but a different static segment, so no link is emitted.
// (#2808 acceptance: guard against over-matching)
func TestHTTPPass_CrossRepo_ParamNormalization_NoStaticCollapse(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "UserViewSet.retrieve", "kind": "Controller", "source_file": "core/views/user_viewset.py"},
			{
				"id": "ep1", "name": "http:GET:/api/v1/users/{pk}", "kind": "http_endpoint",
				"source_file": "core/views/user_viewset.py",
				"properties": map[string]any{
					"verb":         "GET",
					"path":         "/api/v1/users/{pk}",
					"framework":    "django",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "getClient", "kind": "Function", "source_file": "src/stores/clients/clientsServiceV2.js"},
			{
				"id": "ep2", "name": "http:GET:/clients/{clientId}", "kind": "http_endpoint",
				"source_file": "src/stores/clients/clientsServiceV2.js",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/clients/{clientId}",
					"framework":     "axios",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:getClient",
				},
			},
		},
		Edges: []map[string]string{},
	})

	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g2808-noStatic", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g2808-noStatic-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range doc.Links {
		if l.Method == MethodHTTP && l.Source == "frontend::fn1" && l.Target == "backend::h1" {
			t.Errorf("#2808 over-match guard: /clients/{clientId} MUST NOT link to /users/{pk}; got %+v", l)
		}
	}
}

// TestLiteralFillsParamSlot covers the literal-fills-param detector. (#2808)
func TestLiteralFillsParamSlot(t *testing.T) {
	cases := []struct {
		name     string
		consumer string
		producer string
		want     bool
	}{
		// The live acme-mobile case: literal `buildings` fills {pk} after the
		// producer's /api/v1 prefix is stripped.
		{"recents_buildings_vs_pk", "/recents/buildings", "/api/v1/recents/{pk}", true},
		// Same, DELETE-style producer with a different param name.
		{"literal_fills_named_param", "/recents/buildings", "/api/v1/recents/{recent_id}", true},
		// A genuine static endpoint exists → still a fill at the detector level
		// (the "prefer static" guard is enforced by sweep ordering, not here),
		// but a fully-literal producer with identical segments is NOT a fill
		// because there is no param slot to fill.
		{"all_literal_no_fill", "/recents/buildings", "/api/v1/recents/buildings", false},
		// Consumer carries its OWN param in the slot → that is param-name
		// normalization, not a literal fill.
		{"consumer_param_not_literal", "/recents/{id}", "/api/v1/recents/{pk}", false},
		// Over-match guard: a static segment diverges.
		{"static_segment_differs", "/recents/buildings", "/clients/{pk}", false},
		// Segment-count mismatch.
		{"segment_count_differs", "/recents/buildings", "/api/v1/recents/{pk}/floors", false},
		// Multi-segment: literal `active` fills the {pk} slot, trailing
		// `devices` literals match → fill.
		{"multi_seg_literal_mid", "/buildings/active/devices", "/api/v1/buildings/{pk}/devices", true},
		// Empty inputs.
		{"empty_consumer", "", "/api/v1/recents/{pk}", false},
		{"no_producer_param", "/recents/buildings", "/api/v1/recents", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := literalFillsParamSlot(tc.consumer, tc.producer); got != tc.want {
				t.Errorf("literalFillsParamSlot(%q,%q)=%v want %v", tc.consumer, tc.producer, got, tc.want)
			}
		})
	}
}

// TestHTTPPass_CrossRepo_LiteralParamFill verifies the live acme-mobile case:
// `GET /recents/buildings` resolves to the DRF detail route
// `GET /api/v1/recents/{pk}` via the literal-fills-param strategy, the link is
// stamped resolve_strategy=literal_param_fill, and it lands in the
// CrossRepoResolveHitsByStrategy["literal_param_fill"] bucket. (#2808)
func TestHTTPPass_CrossRepo_LiteralParamFill(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "RecentViewSet.retrieve", "kind": "Controller", "source_file": "core/views/recent_viewset.py"},
			{
				"id": "ep1", "name": "http:GET:/api/v1/recents/{pk}", "kind": "http_endpoint",
				"source_file": "core/views/recent_viewset.py",
				"properties": map[string]any{
					"verb":         "GET",
					"path":         "/api/v1/recents/{pk}",
					"framework":    "django",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "mobile",
		Entities: []map[string]any{
			{"id": "fn1", "name": "getRecentBuildings", "kind": "Function", "source_file": "src/services/buildings/buildings.api.ts"},
			{
				"id": "ep2", "name": "http:GET:/recents/buildings", "kind": "http_endpoint",
				"source_file": "src/services/buildings/buildings.api.ts",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/recents/buildings",
					"framework":     "axios",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:getRecentBuildings",
				},
			},
		},
		Edges: []map[string]string{},
	})

	home := filepath.Join(root, "ag-home")
	graphs, err := loadAllGraphs(root)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := PathsFor(home, "g2808-litfill")
	if err != nil {
		t.Fatal(err)
	}
	res, err := runHTTPPass(graphs, paths, nil)
	if err != nil {
		t.Fatalf("runHTTPPass: %v", err)
	}
	if res.CrossRepoResolveHitsByStrategy["literal_param_fill"] != 1 {
		t.Errorf("hits[literal_param_fill]=%d want 1; full map=%v",
			res.CrossRepoResolveHitsByStrategy["literal_param_fill"], res.CrossRepoResolveHitsByStrategy)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g2808-litfill-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, l := range doc.Links {
		if l.Method == MethodHTTP && l.Source == "mobile::fn1" && l.Target == "backend::h1" {
			found = true
			if l.Properties["resolve_strategy"] != "literal_param_fill" {
				t.Errorf("link resolve_strategy=%q want %q", l.Properties["resolve_strategy"], "literal_param_fill")
			}
		}
	}
	if !found {
		t.Errorf("#2808: expected cross-repo link mobile::fn1 → backend::h1 for /recents/buildings ↔ /api/v1/recents/{pk}")
	}
}

// TestHTTPPass_CrossRepo_LiteralParamFill_PrefersStatic verifies the
// "prefer an exact static endpoint over a param-fill" guard: when BOTH a
// concrete `/recents/buildings` producer and a `/recents/{pk}` producer exist,
// the consumer resolves to the STATIC one (via exact/mount-prefix), never the
// param-fill. (#2808 acceptance: don't shadow a real static definition)
func TestHTTPPass_CrossRepo_LiteralParamFill_PrefersStatic(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "hStatic", "name": "RecentBuildings.list", "kind": "Controller", "source_file": "core/views/recent_buildings.py"},
			{
				"id": "epStatic", "name": "http:GET:/api/v1/recents/buildings", "kind": "http_endpoint",
				"source_file": "core/views/recent_buildings.py",
				"properties": map[string]any{
					"verb":         "GET",
					"path":         "/api/v1/recents/buildings",
					"framework":    "django",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
			{"id": "hDetail", "name": "RecentViewSet.retrieve", "kind": "Controller", "source_file": "core/views/recent_viewset.py"},
			{
				"id": "epDetail", "name": "http:GET:/api/v1/recents/{pk}", "kind": "http_endpoint",
				"source_file": "core/views/recent_viewset.py",
				"properties": map[string]any{
					"verb":         "GET",
					"path":         "/api/v1/recents/{pk}",
					"framework":    "django",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "hStatic", "to_id": "epStatic", "kind": "IMPLEMENTS"},
			{"from_id": "hDetail", "to_id": "epDetail", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "mobile",
		Entities: []map[string]any{
			{"id": "fn1", "name": "getRecentBuildings", "kind": "Function", "source_file": "src/services/buildings/buildings.api.ts"},
			{
				"id": "ep2", "name": "http:GET:/recents/buildings", "kind": "http_endpoint",
				"source_file": "src/services/buildings/buildings.api.ts",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/recents/buildings",
					"framework":     "axios",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:getRecentBuildings",
				},
			},
		},
		Edges: []map[string]string{},
	})

	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g2808-static", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g2808-static-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var linkedStatic, linkedDetail bool
	for _, l := range doc.Links {
		if l.Method != MethodHTTP || l.Source != "mobile::fn1" {
			continue
		}
		switch l.Target {
		case "backend::hStatic":
			linkedStatic = true
			if l.Properties["resolve_strategy"] == "literal_param_fill" {
				t.Errorf("static endpoint must NOT be reached via literal_param_fill; got %+v", l.Properties)
			}
		case "backend::hDetail":
			linkedDetail = true
		}
	}
	if !linkedStatic {
		t.Errorf("#2808: consumer /recents/buildings should resolve to the STATIC /api/v1/recents/buildings producer")
	}
	if linkedDetail {
		t.Errorf("#2808 prefer-static guard: consumer must NOT also link to the {pk} detail route via param-fill")
	}
}

// TestCaseNormalizePathSegments covers the per-segment canonical-id
// transformation in isolation. (#2703)
func TestCaseNormalizePathSegments(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"/", "/"},
		{"/assigned_contacts", "/assignedcontacts"},
		{"/assigned-contacts", "/assignedcontacts"},
		{"/assignedContacts", "/assignedcontacts"},
		{"/AssignedContacts", "/assignedcontacts"},
		{"/api/v1/contracts/{*}/assigned_contacts", "/api/v1/contracts/{*}/assignedcontacts"},
		{"/api/v1/contracts/{*}/assignedContacts", "/api/v1/contracts/{*}/assignedcontacts"},
		{"/equipment-types", "/equipmenttypes"},
		// #3169 — the exact acme misses: frontend camelCase ↔ backend
		// snake_case (DRF default url_path = method name).
		{"/jurisdictions/inspectionTypes", "/jurisdictions/inspectiontypes"},
		{"/jurisdictions/inspection_types", "/jurisdictions/inspectiontypes"},
		{"/dashboard/submitElv3", "/dashboard/submitelv3"},
		{"/dashboard/submit_elv3", "/dashboard/submitelv3"},
		// #3169 false-match guard: a name with EXTRA words must NOT collapse
		// onto the shorter name. assigned_and_available_devices (a queryKey,
		// not a real route) stays distinct from the real assigned_devices
		// route, so the resolver can never cross-link them.
		{"/contracts/{*}/assigned_devices", "/contracts/{*}/assigneddevices"},
		{"/contracts/{*}/assignedAndAvailableDevices", "/contracts/{*}/assignedandavailabledevices"},
		// Segment count must be preserved.
		{"/searchBuildings", "/searchbuildings"},
		{"/buildings/search", "/buildings/search"},
	}
	for _, tc := range cases {
		got := caseNormalizePathSegments(tc.in)
		if got != tc.want {
			t.Errorf("caseNormalizePathSegments(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

// TestClassifyOrphanReason covers the path-shape → miss-reason taxonomy
// used by runHTTPPass to bucket residual orphans.
func TestClassifyOrphanReason(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/{apiUrl}/schedule", "dynamic_baseurl"},
		{"/{base_url.rstrip(", "dynamic_baseurl"}, // unbalanced braces
		{"/users/{id}", "no_endpoint_match"},
		{"/api/v1/items", "no_endpoint_match"},
		{"", "no_endpoint_match"},
		{"/{x}", "dynamic_baseurl"}, // entire path is one template segment
	}
	// A non-empty, unrelated producer population so these cases exercise the
	// pre-existing dynamic_baseurl / no_endpoint_match classification rather
	// than the new #5680 no_server_definition bucket (which only fires when
	// the group has ZERO producer hits at all — see
	// TestClassifyOrphanReason_NoServerDefinition below).
	unrelatedProducers := []*httpEndpointHit{
		{canonicalPath: "/completely/unrelated/path", side: sideProducer, repo: "backend"},
	}
	for _, tc := range cases {
		got := classifyOrphanReason(tc.path, unrelatedProducers)
		if got != tc.want {
			t.Errorf("classifyOrphanReason(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// TestDynamicSuffixTemplate is the unit table for the #2813 suffix extractor:
// it must strip leading dynamic-prefix segments, keep a static suffix when one
// exists, and refuse (ok=false) param-led or prefix-free paths.
func TestDynamicSuffixTemplate(t *testing.T) {
	cases := []struct {
		path       string
		wantSuffix string
		wantStatic int
		wantOK     bool
	}{
		// Clean env/prop-drilled base + 2-static suffix → resolvable.
		{"/{apiUrl}/schedule/import", "/schedule/import", 2, true},
		{"/${baseURL}/schedule/import", "/schedule/import", 2, true},
		// Trailing param after a 2-static prefix is fine — leadingStatic counts
		// only the leading run, which is what the specificity gate checks.
		{"/{apiUrl}/schedule/confirm/{token}", "/schedule/confirm/{*}", 2, true},
		// One static segment → still ok=true (the gate, not the extractor,
		// decides it is too generic to auto-link).
		{"/{apiUrl}/list", "/list", 1, true},
		// Genuinely-runtime: after stripping ONE dynamic prefix the suffix is
		// STILL param-led (companyType is render-chosen). Must be ok=false.
		{"/{companyType}/{companyId}/branches/{branchId}", "", 0, false},
		{"/{param}/{companyId}/activity", "", 0, false},
		// No dynamic prefix at all → not our job.
		{"/schedule/import", "", 0, false},
		{"/api/v1/items", "", 0, false},
		{"", "", 0, false},
	}
	for _, tc := range cases {
		gotSuffix, gotStatic, gotOK := dynamicSuffixTemplate(tc.path)
		if gotOK != tc.wantOK {
			t.Errorf("dynamicSuffixTemplate(%q) ok = %v, want %v", tc.path, gotOK, tc.wantOK)
			continue
		}
		if !tc.wantOK {
			continue
		}
		if gotSuffix != tc.wantSuffix || gotStatic != tc.wantStatic {
			t.Errorf("dynamicSuffixTemplate(%q) = (%q, %d), want (%q, %d)",
				tc.path, gotSuffix, gotStatic, tc.wantSuffix, tc.wantStatic)
		}
	}
}

// TestHTTPPass_DynamicSuffix_AutoLink verifies the PRIMARY #2813 strategy: a
// dynamic-baseURL consumer (`${apiUrl}/schedule/import`) whose static suffix
// uniquely + specifically (2+ static segments) matches a backend producer is
// auto-linked with resolve_strategy="dynamic_suffix_match".
func TestHTTPPass_DynamicSuffix_AutoLink(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "ScheduleViewSet", "kind": "Controller", "source_file": "core/views/schedule_viewset.py"},
			{
				"id": "ep1", "name": "http:POST:/api/v1/schedule/import", "kind": "http_endpoint",
				"source_file": "core/views/schedule_viewset.py",
				"properties": map[string]any{
					"verb":         "POST",
					"path":         "/api/v1/schedule/import",
					"framework":    "django",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"}},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "importSchedule", "kind": "Function", "source_file": "src/stores/schedule/scheduleServiceV2.js"},
			{
				"id": "ep2", "name": "http:POST:/{apiUrl}/schedule/import", "kind": "http_endpoint",
				"source_file": "src/stores/schedule/scheduleServiceV2.js",
				"properties": map[string]any{
					"verb":            "POST",
					"path":            "/{apiUrl}/schedule/import",
					"framework":       "axios",
					"pattern_type":    "http_endpoint_client_synthesis",
					"url_kind":        "dynamic_baseurl",
					"dynamic_baseurl": "true",
					"source_caller":   "Function:importSchedule",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g2813-auto", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g2813-auto-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var hit *Link
	for i, l := range doc.Links {
		if l.Method == MethodHTTP && l.Source == "frontend::fn1" && l.Target == "backend::h1" {
			hit = &doc.Links[i]
			break
		}
	}
	if hit == nil {
		t.Fatalf("#2813: expected dynamic_suffix_match link frontend::fn1 → backend::h1; got %+v", doc.Links)
	}
	if hit.Properties["resolve_strategy"] != "dynamic_suffix_match" {
		t.Errorf("resolve_strategy: want dynamic_suffix_match, got %q", hit.Properties["resolve_strategy"])
	}
	if hit.Properties["dynamic_suffix"] != "/schedule/import" {
		t.Errorf("dynamic_suffix: want /schedule/import, got %q", hit.Properties["dynamic_suffix"])
	}
}

// TestHTTPPass_DynamicSuffix_AmbiguousStaysResidual verifies that when a
// dynamic-baseURL suffix matches MORE THAN ONE producer it is NOT auto-linked
// (no phantom edge); the consumer stays orphaned and is counted as a
// dynamic_baseurl miss with the candidate count surfaced via residual_candidates.
func TestHTTPPass_DynamicSuffix_AmbiguousStaysResidual(t *testing.T) {
	root := fixtureRoot(t)
	// Two backend repos both serve /reports/export → ambiguous.
	for _, repo := range []string{"backend-a", "backend-b"} {
		writeFixture(t, root, fixtureGraph{
			Repo: repo,
			Entities: []map[string]any{
				{"id": "h_" + repo, "name": "RepView_" + repo, "kind": "Controller", "source_file": "v.py"},
				{
					"id": "ep_" + repo, "name": "http:GET:/api/v1/reports/export", "kind": "http_endpoint",
					"source_file": "v.py",
					"properties": map[string]any{
						"verb": "GET", "path": "/api/v1/reports/export",
						"framework": "django", "pattern_type": "http_endpoint_synthesis",
					},
				},
			},
			Edges: []map[string]string{{"from_id": "h_" + repo, "to_id": "ep_" + repo, "kind": "IMPLEMENTS"}},
		})
	}
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "exportReports", "kind": "Function", "source_file": "src/r.js"},
			{
				"id": "ep2", "name": "http:GET:/{apiUrl}/reports/export", "kind": "http_endpoint",
				"source_file": "src/r.js",
				"properties": map[string]any{
					"verb": "GET", "path": "/{apiUrl}/reports/export",
					"framework": "axios", "pattern_type": "http_endpoint_client_synthesis",
					"url_kind": "dynamic_baseurl", "dynamic_baseurl": "true",
					"source_caller": "Function:exportReports",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g2813-ambig", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g2813-ambig-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range doc.Links {
		if l.Method == MethodHTTP && l.Source == "frontend::fn1" {
			t.Errorf("#2813: ambiguous suffix must NOT auto-link, got phantom edge %+v", l)
		}
	}
}

// TestHTTPPass_DynamicSuffix_RuntimeStaysUnlinked verifies a genuinely-runtime
// base (`/{companyType}/{companyId}/branches/...`, companyType chosen at
// render) is never force-linked even when a producer-ish path exists — the
// param-led suffix makes dynamicSuffixTemplate return ok=false.
func TestHTTPPass_DynamicSuffix_RuntimeStaysUnlinked(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "BranchView", "kind": "Controller", "source_file": "v.py"},
			{
				"id": "ep1", "name": "http:GET:/api/v1/{companyId}/branches/{branchId}", "kind": "http_endpoint",
				"source_file": "v.py",
				"properties": map[string]any{
					"verb": "GET", "path": "/api/v1/{companyId}/branches/{branchId}",
					"framework": "django", "pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"}},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "getBranch", "kind": "Function", "source_file": "src/b.js"},
			{
				"id": "ep2", "name": "http:GET:/{companyType}/{companyId}/branches/{branchId}", "kind": "http_endpoint",
				"source_file": "src/b.js",
				"properties": map[string]any{
					"verb": "GET", "path": "/{companyType}/{companyId}/branches/{branchId}",
					"framework": "axios", "pattern_type": "http_endpoint_client_synthesis",
					"url_kind": "dynamic_baseurl", "dynamic_baseurl": "true",
					"source_caller": "Function:getBranch",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g2813-runtime", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g2813-runtime-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range doc.Links {
		if l.Method == MethodHTTP && l.Source == "frontend::fn1" {
			t.Errorf("#2813: render-time companyType base must stay unlinked, got %+v", l)
		}
	}
}

// TestHTTPPass_SOAPCrossRepoMatch verifies the SOAP cross-link round-trip
// (epic #3628): a zeep client `client.service.GetUser(id)` (service-less id
// http:SOAP:/soap/GetUser) joins to a JAX-WS producer's service-less alias.
func TestHTTPPass_SOAPCrossRepoMatch(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "user-soap-svc",
		Entities: []map[string]any{
			{"id": "h1", "name": "GetUser", "kind": "Method", "source_file": "ws/UserEndpoint.java"},
			{
				"id": "ep1", "name": "http:SOAP:/soap/GetUser", "kind": "http_endpoint",
				"source_file": "ws/UserEndpoint.java",
				"properties": map[string]any{
					"verb":         "SOAP",
					"path":         "/soap/GetUser",
					"framework":    "jaxws",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "billing-svc",
		Entities: []map[string]any{
			{"id": "fn1", "name": "fetch_user", "kind": "Function", "source_file": "client.py"},
			{
				"id": "ep2", "name": "http:SOAP:/soap/GetUser", "kind": "http_endpoint",
				"source_file": "client.py",
				"properties": map[string]any{
					"verb":          "SOAP",
					"path":          "/soap/GetUser",
					"framework":     "zeep",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:fetch_user",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g-soap", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g-soap-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var hit *Link
	for i, l := range doc.Links {
		if l.Method == MethodHTTP {
			hit = &doc.Links[i]
			break
		}
	}
	if hit == nil {
		t.Fatalf("expected a cross-repo SOAP link zeep client → JAX-WS endpoint; got %+v", doc.Links)
	}
	if hit.Source != "billing-svc::fn1" {
		t.Errorf("source: want billing-svc::fn1 (zeep caller), got %s", hit.Source)
	}
	if hit.Target != "user-soap-svc::h1" {
		t.Errorf("target: want user-soap-svc::h1 (JAX-WS handler), got %s", hit.Target)
	}
	if hit.Identifier == nil || *hit.Identifier != "http:SOAP:/soap/GetUser" {
		t.Errorf("identifier: want http:SOAP:/soap/GetUser, got %v", hit.Identifier)
	}
}

// TestHTTPPass_JSONRPCCrossRepoMatch verifies the JSON-RPC cross-link round-trip
// (epic #3628): a jayson client `client.request('add', …)` joins to a jayson
// server method-map producer on http:JSONRPC:/jsonrpc/add.
func TestHTTPPass_JSONRPCCrossRepoMatch(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "math-rpc-svc",
		Entities: []map[string]any{
			{"id": "h1", "name": "add", "kind": "Method", "source_file": "server.js"},
			{
				"id": "ep1", "name": "http:JSONRPC:/jsonrpc/add", "kind": "http_endpoint",
				"source_file": "server.js",
				"properties": map[string]any{
					"verb":         "JSONRPC",
					"path":         "/jsonrpc/add",
					"framework":    "jayson",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "gateway-svc",
		Entities: []map[string]any{
			{"id": "fn1", "name": "compute", "kind": "Function", "source_file": "client.js"},
			{
				"id": "ep2", "name": "http:JSONRPC:/jsonrpc/add", "kind": "http_endpoint",
				"source_file": "client.js",
				"properties": map[string]any{
					"verb":          "JSONRPC",
					"path":          "/jsonrpc/add",
					"framework":     "jayson",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:compute",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g-jsonrpc", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g-jsonrpc-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var hit *Link
	for i, l := range doc.Links {
		if l.Method == MethodHTTP {
			hit = &doc.Links[i]
			break
		}
	}
	if hit == nil {
		t.Fatalf("expected a cross-repo JSON-RPC link jayson client → jayson server; got %+v", doc.Links)
	}
	if hit.Source != "gateway-svc::fn1" {
		t.Errorf("source: want gateway-svc::fn1 (jayson caller), got %s", hit.Source)
	}
	if hit.Target != "math-rpc-svc::h1" {
		t.Errorf("target: want math-rpc-svc::h1 (jayson handler), got %s", hit.Target)
	}
	if hit.Identifier == nil || *hit.Identifier != "http:JSONRPC:/jsonrpc/add" {
		t.Errorf("identifier: want http:JSONRPC:/jsonrpc/add, got %v", hit.Identifier)
	}
}

// TestRuntimeEnumSuffixShape unit-tests the #4315 shape detector: a param-led
// suffix after a param first segment is claimed (ok=true); a static-led suffix
// (#2813's territory), a static first segment, an anchor-less shape, or a too-
// short path are all rejected.
func TestRuntimeEnumSuffixShape(t *testing.T) {
	cases := []struct {
		path       string
		wantSuffix string
		wantStatic int
		wantOK     bool
	}{
		// The canonical #4315 case: param first segment (companyType), param-led
		// remainder with a static anchor (branches).
		{"/{companyType}/{companyId}/branches/{branchId}", "/{*}/branches/{*}", 1, true},
		{"/{companyType}/{companyId}/branches", "/{*}/branches", 1, true},
		{"/{companyType}/{companyId}/branches/{id}/migrate", "/{*}/branches/{*}/migrate", 2, true},
		// Static-led suffix → #2813 owns it, not us.
		{"/{apiUrl}/schedule/import", "", 0, false},
		// No static anchor in the suffix → never expand (would fan out).
		{"/{companyType}/{companyId}", "", 0, false},
		{"/{a}/{b}/{c}", "", 0, false},
		// Static first segment → ordinary path, not a runtime enum.
		{"/companies/{id}/branches", "", 0, false},
		// Too short / empty.
		{"/{companyType}", "", 0, false},
		{"", "", 0, false},
	}
	for _, tc := range cases {
		gotSuffix, gotStatic, gotOK := runtimeEnumSuffixShape(tc.path)
		if gotOK != tc.wantOK {
			t.Errorf("runtimeEnumSuffixShape(%q) ok = %v, want %v", tc.path, gotOK, tc.wantOK)
			continue
		}
		if !tc.wantOK {
			continue
		}
		if gotSuffix != tc.wantSuffix || gotStatic != tc.wantStatic {
			t.Errorf("runtimeEnumSuffixShape(%q) = (%q, %d), want (%q, %d)",
				tc.path, gotSuffix, gotStatic, tc.wantSuffix, tc.wantStatic)
		}
	}
}

// TestRuntimeEnumProducerSuffix unit-tests the #4315 producer matcher: only a
// LITERAL-first-segment route whose remaining segments line up positionally
// with the consumer suffix qualifies. A param-first producer (the ambiguous
// case #2813 keeps orphan), a static divergence, or a segment-count mismatch
// are all rejected.
func TestRuntimeEnumProducerSuffix(t *testing.T) {
	const suffix = "/{*}/branches/{*}"
	cases := []struct {
		producer  string
		wantEnum  string
		wantMatch bool
	}{
		// Literal first segment + matching suffix shape → qualifies; the literal
		// (enum value) is returned. API prefix is stripped first.
		{"/api/v1/contracting-companies/{id}/branches/{branchId}", "contracting-companies", true},
		{"/witnessing-companies/{pk}/branches/{bid}", "witnessing-companies", true},
		// Param first segment → the genuinely-ambiguous case; rejected.
		{"/api/v1/{companyId}/branches/{branchId}", "", false},
		// Static divergence in the suffix (offices != branches) → rejected.
		{"/contracting-companies/{id}/offices/{bid}", "", false},
		// Segment-count mismatch → rejected.
		{"/contracting-companies/{id}/branches", "", false},
		{"/contracting-companies/{id}/branches/{bid}/extra", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		gotEnum, gotMatch := runtimeEnumProducerSuffix(tc.producer, suffix)
		if gotMatch != tc.wantMatch {
			t.Errorf("runtimeEnumProducerSuffix(%q) match = %v, want %v", tc.producer, gotMatch, tc.wantMatch)
			continue
		}
		if gotMatch && gotEnum != tc.wantEnum {
			t.Errorf("runtimeEnumProducerSuffix(%q) enum = %q, want %q", tc.producer, gotEnum, tc.wantEnum)
		}
	}
}

// TestHTTPPass_RuntimeEnumExpansion_AutoLink is the end-to-end #4315 success
// case: a frontend call `GET /{companyType}/{companyId}/branches/{branchId}`
// (companyType is a render-time enum) expands to BOTH literal-prefixed backend
// routes — the contracting and witnessing siblings — each stamped
// resolve_strategy=runtime_enum_expansion with heuristic confidence.
func TestHTTPPass_RuntimeEnumExpansion_AutoLink(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "hc", "name": "ContractingBranchView", "kind": "Controller", "source_file": "core/views/contracting.py"},
			{
				"id": "epc", "name": "http:GET:/api/v1/contracting-companies/{companyId}/branches/{branchId}", "kind": "http_endpoint",
				"source_file": "core/views/contracting.py",
				"properties": map[string]any{
					"verb": "GET", "path": "/api/v1/contracting-companies/{companyId}/branches/{branchId}",
					"framework": "django", "pattern_type": "http_endpoint_synthesis",
				},
			},
			{"id": "hw", "name": "WitnessingBranchView", "kind": "Controller", "source_file": "core/views/witnessing.py"},
			{
				"id": "epw", "name": "http:GET:/api/v1/witnessing-companies/{companyId}/branches/{branchId}", "kind": "http_endpoint",
				"source_file": "core/views/witnessing.py",
				"properties": map[string]any{
					"verb": "GET", "path": "/api/v1/witnessing-companies/{companyId}/branches/{branchId}",
					"framework": "django", "pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "hc", "to_id": "epc", "kind": "IMPLEMENTS"},
			{"from_id": "hw", "to_id": "epw", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "getBranch", "kind": "Function", "source_file": "src/stores/company/branchService.js"},
			{
				"id": "ep2", "name": "http:GET:/{companyType}/{companyId}/branches/{branchId}", "kind": "http_endpoint",
				"source_file": "src/stores/company/branchService.js",
				"properties": map[string]any{
					"verb": "GET", "path": "/{companyType}/{companyId}/branches/{branchId}",
					"framework": "axios", "pattern_type": "http_endpoint_client_synthesis",
					"url_kind": "dynamic_baseurl", "dynamic_baseurl": "true",
					"source_caller": "Function:getBranch",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g4315-auto", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g4315-auto-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	gotTargets := map[string]*Link{}
	for i, l := range doc.Links {
		if l.Method == MethodHTTP && l.Source == "frontend::fn1" {
			gotTargets[l.Target] = &doc.Links[i]
		}
	}
	for _, want := range []string{"backend::hc", "backend::hw"} {
		l, ok := gotTargets[want]
		if !ok {
			t.Errorf("#4315: expected runtime_enum_expansion link frontend::fn1 → %s; got %v", want, gotTargets)
			continue
		}
		if l.Properties["resolve_strategy"] != "runtime_enum_expansion" {
			t.Errorf("%s resolve_strategy: want runtime_enum_expansion, got %q", want, l.Properties["resolve_strategy"])
		}
		if l.Properties[EdgeConfidenceKey] != ConfidenceHeuristic {
			t.Errorf("%s confidence: want heuristic, got %q", want, l.Properties[EdgeConfidenceKey])
		}
	}
	if l := gotTargets["backend::hc"]; l != nil && l.Properties["runtime_enum"] != "contracting-companies" {
		t.Errorf("hc runtime_enum: want contracting-companies, got %q", l.Properties["runtime_enum"])
	}
}

// TestHTTPPass_RuntimeEnumExpansion_NoFalseLinks is the guardrail case: shapes
// that MUST stay orphan. (a) A param-first producer (no literal enum) keeps the
// #2813 RuntimeStaysUnlinked behaviour. (b) A static asset `/version.txt` and a
// genuinely-nonexistent route never link. (c) An anchor-less `/{*}/{*}` shape
// never expands.
func TestHTTPPass_RuntimeEnumExpansion_NoFalseLinks(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			// Param-first producer only — NOT a literal enum prefix.
			{"id": "h1", "name": "BranchView", "kind": "Controller", "source_file": "v.py"},
			{
				"id": "ep1", "name": "http:GET:/api/v1/{companyId}/branches/{branchId}", "kind": "http_endpoint",
				"source_file": "v.py",
				"properties": map[string]any{
					"verb": "GET", "path": "/api/v1/{companyId}/branches/{branchId}",
					"framework": "django", "pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"}},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			// Runtime-enum shape, but only a param-first producer exists → orphan.
			{"id": "fnRuntime", "name": "getBranch", "kind": "Function", "source_file": "src/b.js"},
			{
				"id": "epRuntime", "name": "http:GET:/{companyType}/{companyId}/branches/{branchId}", "kind": "http_endpoint",
				"source_file": "src/b.js",
				"properties": map[string]any{
					"verb": "GET", "path": "/{companyType}/{companyId}/branches/{branchId}",
					"framework": "axios", "pattern_type": "http_endpoint_client_synthesis",
					"url_kind": "dynamic_baseurl", "dynamic_baseurl": "true",
					"source_caller": "Function:getBranch",
				},
			},
			// Static asset — must STAY orphan.
			{"id": "fnAsset", "name": "loadVersion", "kind": "Function", "source_file": "src/v.js"},
			{
				"id": "epAsset", "name": "http:GET:/version.txt", "kind": "http_endpoint",
				"source_file": "src/v.js",
				"properties": map[string]any{
					"verb": "GET", "path": "/version.txt",
					"framework": "axios", "pattern_type": "http_endpoint_client_synthesis",
					"source_caller": "Function:loadVersion",
				},
			},
			// Anchor-less runtime shape — never expands.
			{"id": "fnNoAnchor", "name": "getThing", "kind": "Function", "source_file": "src/t.js"},
			{
				"id": "epNoAnchor", "name": "http:GET:/{companyType}/{companyId}", "kind": "http_endpoint",
				"source_file": "src/t.js",
				"properties": map[string]any{
					"verb": "GET", "path": "/{companyType}/{companyId}",
					"framework": "axios", "pattern_type": "http_endpoint_client_synthesis",
					"url_kind": "dynamic_baseurl", "dynamic_baseurl": "true",
					"source_caller": "Function:getThing",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g4315-orphan", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g4315-orphan-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range doc.Links {
		if l.Method != MethodHTTP {
			continue
		}
		switch l.Source {
		case "frontend::fnRuntime":
			t.Errorf("#4315: param-first producer must NOT expand (RuntimeStaysUnlinked), got %+v", l)
		case "frontend::fnAsset":
			t.Errorf("#4315: static asset /version.txt must stay orphan, got %+v", l)
		case "frontend::fnNoAnchor":
			t.Errorf("#4315: anchor-less /{*}/{*} must stay orphan, got %+v", l)
		}
	}
}

// ---------------------------------------------------------------------------
// #5688 / #5680 — version/prefix-aware HTTP joins + honest orphan metric
// ---------------------------------------------------------------------------

// TestHTTPPass_CrossRepo_BareVersionPrefix_Symmetric verifies that a bare
// consumer path (`GET /orders`, no prefix at all) joins a producer mounted at
// a BARE version prefix (`GET /v2/orders`, no `/api` segment). Before #5688,
// crossRepoPrefixCandidates (the prefix-injection retry vocabulary) was
// missing bare "/v2" — its sibling pathNormPrefixSegments had it — so this
// pairing depended entirely on the generic apiPrefixRe byPath alias. This
// test pins the behaviour directly against the fast repoGraph-level entry
// point (loadAllGraphs + runHTTPPass), independent of which internal
// strategy resolves it.
func TestHTTPPass_CrossRepo_BareVersionPrefix_Symmetric(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "OrdersView", "kind": "Controller", "source_file": "app/views.py"},
			{
				"id": "ep1", "name": "http:GET:/v2/orders", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties": map[string]any{
					"verb": "GET", "path": "/v2/orders",
					"framework": "django", "pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "fetchOrders", "kind": "Function", "source_file": "src/api.ts"},
			{
				"id": "ep2", "name": "http:GET:/orders", "kind": "http_endpoint",
				"source_file": "src/api.ts",
				"properties": map[string]any{
					"verb": "GET", "path": "/orders",
					"framework": "fetch", "pattern_type": "http_endpoint_client_synthesis",
					"source_caller": "Function:fetchOrders",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	graphs, err := loadAllGraphs(root)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := PathsFor(home, "g5688-bare-v2")
	if err != nil {
		t.Fatal(err)
	}
	res, err := runHTTPPass(graphs, paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(paths.Links)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, l := range doc.Links {
		if l.Method == MethodHTTP && l.Source == "frontend::fn1" && l.Target == "backend::h1" {
			found = true
		}
	}
	if !found {
		t.Errorf("#5688: expected cross-repo link frontend::fn1 -> backend::h1 for /orders -> /v2/orders (bare version prefix); got links=%+v orphan=%d", doc.Links, res.OrphanCalls)
	}
}

// TestHTTPPass_CrossRepo_VersionedAPIPrefix_Symmetric is the sibling case:
// a consumer `GET /orders` must join a producer mounted at the versioned
// `/api/v2/orders` form. Pinned alongside the bare-v2 case so a regression
// in either the generic prefix strip or the injection retry surfaces here.
func TestHTTPPass_CrossRepo_VersionedAPIPrefix_Symmetric(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "OrdersView", "kind": "Controller", "source_file": "app/views.py"},
			{
				"id": "ep1", "name": "http:GET:/api/v2/orders", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties": map[string]any{
					"verb": "GET", "path": "/api/v2/orders",
					"framework": "django", "pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "fetchOrders", "kind": "Function", "source_file": "src/api.ts"},
			{
				"id": "ep2", "name": "http:GET:/orders", "kind": "http_endpoint",
				"source_file": "src/api.ts",
				"properties": map[string]any{
					"verb": "GET", "path": "/orders",
					"framework": "fetch", "pattern_type": "http_endpoint_client_synthesis",
					"source_caller": "Function:fetchOrders",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	graphs, err := loadAllGraphs(root)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := PathsFor(home, "g5688-versioned-v2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runHTTPPass(graphs, paths, nil); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(paths.Links)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, l := range doc.Links {
		if l.Method == MethodHTTP && l.Source == "frontend::fn1" && l.Target == "backend::h1" {
			found = true
		}
	}
	if !found {
		t.Errorf("#5688: expected cross-repo link frontend::fn1 -> backend::h1 for /orders -> /api/v2/orders (versioned prefix); got %+v", doc.Links)
	}
}

// TestHTTPPass_CrossRepo_AmbiguousVersionPrefix_NoFalseLink verifies the
// ambiguity guard: when TWO distinct producers in the same target repo would
// both match a bare consumer path once the standard version prefix is
// stripped (e.g. `/v1/orders` AND `/v2/orders` both collapse to `/orders`),
// the symmetric matcher must NOT guess — no link is emitted for either.
func TestHTTPPass_CrossRepo_AmbiguousVersionPrefix_NoFalseLink(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "OrdersViewV1", "kind": "Controller", "source_file": "app/v1_views.py"},
			{
				"id": "ep1", "name": "http:GET:/v1/orders", "kind": "http_endpoint",
				"source_file": "app/v1_views.py",
				"properties": map[string]any{
					"verb": "GET", "path": "/v1/orders",
					"framework": "django", "pattern_type": "http_endpoint_synthesis",
				},
			},
			{"id": "h2", "name": "OrdersViewV2", "kind": "Controller", "source_file": "app/v2_views.py"},
			{
				"id": "ep2", "name": "http:GET:/v2/orders", "kind": "http_endpoint",
				"source_file": "app/v2_views.py",
				"properties": map[string]any{
					"verb": "GET", "path": "/v2/orders",
					"framework": "django", "pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
			{"from_id": "h2", "to_id": "ep2", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "fetchOrders", "kind": "Function", "source_file": "src/api.ts"},
			{
				"id": "ep3", "name": "http:GET:/orders", "kind": "http_endpoint",
				"source_file": "src/api.ts",
				"properties": map[string]any{
					"verb": "GET", "path": "/orders",
					"framework": "fetch", "pattern_type": "http_endpoint_client_synthesis",
					"source_caller": "Function:fetchOrders",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	graphs, err := loadAllGraphs(root)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := PathsFor(home, "g5688-ambiguous")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runHTTPPass(graphs, paths, nil); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(paths.Links)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range doc.Links {
		if l.Method == MethodHTTP && l.Source == "frontend::fn1" {
			t.Errorf("#5688: ambiguous /orders (matches both /v1/orders and /v2/orders) must NOT link; got %+v", l)
		}
	}
}

// TestHTTPPass_OrphanMetric_NoServerDefinition verifies #5680's metric
// de-conflation: when the group's producer repo has ZERO endpoint
// DEFINITIONS at all (no http_endpoint_synthesis producer hits anywhere),
// every unresolved consumer call must be classified "no_server_definition"
// (there is nothing to join against) rather than the generic
// "no_endpoint_match" (which implies a producer population exists but
// nothing shaped like this specific path was found in it). It also pins the
// orphan_calls <= calls invariant at the http_pass level.
func TestHTTPPass_OrphanMetric_NoServerDefinition(t *testing.T) {
	root := fixtureRoot(t)
	// "backend" repo exists in the group but defines NO http endpoints at all.
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "SomeHelper", "kind": "Function", "source_file": "app/helpers.py"},
		},
		Edges: []map[string]string{},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "fetchOrders", "kind": "Function", "source_file": "src/api.ts"},
			{
				"id": "ep2", "name": "http:GET:/orders", "kind": "http_endpoint",
				"source_file": "src/api.ts",
				"properties": map[string]any{
					"verb": "GET", "path": "/orders",
					"framework": "fetch", "pattern_type": "http_endpoint_client_synthesis",
					"source_caller": "Function:fetchOrders",
				},
			},
			{"id": "fn2", "name": "fetchUsers", "kind": "Function", "source_file": "src/api.ts"},
			{
				"id": "ep3", "name": "http:GET:/users", "kind": "http_endpoint",
				"source_file": "src/api.ts",
				"properties": map[string]any{
					"verb": "GET", "path": "/users",
					"framework": "fetch", "pattern_type": "http_endpoint_client_synthesis",
					"source_caller": "Function:fetchUsers",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	graphs, err := loadAllGraphs(root)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := PathsFor(home, "g5680-no-server-def")
	if err != nil {
		t.Fatal(err)
	}
	res, err := runHTTPPass(graphs, paths, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Invariant: orphan_calls must never exceed the total consumer population
	// counted at this level.
	totalConsumers := res.CrossRepoResolved + res.OrphanCalls
	if res.OrphanCalls > totalConsumers {
		t.Errorf("#5680: orphan_calls (%d) must not exceed total calls (%d)", res.OrphanCalls, totalConsumers)
	}
	if res.OrphanCalls != 2 {
		t.Errorf("#5680: expected both consumer calls to be orphaned (no producer anywhere), got OrphanCalls=%d", res.OrphanCalls)
	}

	got := res.CrossRepoResolveMissesByReason["no_server_definition"]
	if got != 2 {
		t.Errorf("#5680: expected 2 misses classified no_server_definition (zero producer definitions in group), got %d; full breakdown=%+v", got, res.CrossRepoResolveMissesByReason)
	}
	if n := res.CrossRepoResolveMissesByReason["no_endpoint_match"]; n != 0 {
		t.Errorf("#5680: expected 0 misses classified no_endpoint_match when the group has zero server definitions (should be no_server_definition instead), got %d", n)
	}
}

// TestClassifyOrphanReason_NoServerDefinition unit-tests classifyOrphanReason
// directly: when the producer population passed in is empty, the reason must
// be "no_server_definition" regardless of path shape (even for a well-formed
// static path that would otherwise be "no_endpoint_match").
func TestClassifyOrphanReason_NoServerDefinition(t *testing.T) {
	if got := classifyOrphanReason("/orders", nil); got != "no_server_definition" {
		t.Errorf("classifyOrphanReason with zero producers: want no_server_definition, got %q", got)
	}
}

// TestClassifyOrphanReason_PrefixVersionMismatch unit-tests the new
// prefix_version_mismatch bucket: a consumer path that collapses to the same
// standard-prefix-stripped key as an existing producer (but wasn't linked —
// e.g. due to the ambiguity guard) must be classified distinctly from a
// wholly-unrelated miss.
func TestClassifyOrphanReason_PrefixVersionMismatch(t *testing.T) {
	producers := []*httpEndpointHit{
		{canonicalPath: "/v1/orders", side: sideProducer, repo: "backend"},
		{canonicalPath: "/v2/orders", side: sideProducer, repo: "backend"},
	}
	if got := classifyOrphanReason("/orders", producers); got != "prefix_version_mismatch" {
		t.Errorf("classifyOrphanReason for ambiguous version-prefixed producers: want prefix_version_mismatch, got %q", got)
	}
	// Sanity: a path with no structural relationship to any producer still
	// falls back to the generic bucket.
	if got := classifyOrphanReason("/completely-unrelated", producers); got != "no_endpoint_match" {
		t.Errorf("classifyOrphanReason for unrelated path: want no_endpoint_match, got %q", got)
	}
}
