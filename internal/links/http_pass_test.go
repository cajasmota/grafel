package links

import (
	"path/filepath"
	"sort"
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
	}
	for _, c := range cases {
		got := normalizePathForIndex(c.in)
		if got != c.want {
			t.Errorf("normalizePathForIndex(%q) = %q, want %q", c.in, got, c.want)
		}
	}
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
		in        string
		verb      string
		path      string
		ok        bool
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
				"properties": map[string]any{"verb": "DELETE", "path": "/inspections/{pk}", "pattern_type": "http_endpoint_synthesis"},
			},
			{
				"id": "ep-patch", "name": "http:PATCH:/inspections/{pk}", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties": map[string]any{"verb": "PATCH", "path": "/inspections/{pk}", "pattern_type": "http_endpoint_synthesis"},
			},
			{
				"id": "ep-get", "name": "http:GET:/inspections/{pk}", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties": map[string]any{"verb": "GET", "path": "/inspections/{pk}", "pattern_type": "http_endpoint_synthesis"},
			},
			{
				"id": "ep-any", "name": "http:ANY:/inspections/{pk}", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties": map[string]any{"verb": "ANY", "path": "/inspections/{pk}", "pattern_type": "http_endpoint_synthesis"},
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
				"properties": map[string]any{"verb": "PATCH", "path": "/roles/{roleId}", "pattern_type": "http_endpoint_synthesis"},
			},
			{
				"id": "ep-delete", "name": "http:DELETE:/roles/{roleId}", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties": map[string]any{"verb": "DELETE", "path": "/roles/{roleId}", "pattern_type": "http_endpoint_synthesis"},
			},
			{
				"id": "ep-any", "name": "http:ANY:/roles/{roleId}", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties": map[string]any{"verb": "ANY", "path": "/roles/{roleId}", "pattern_type": "http_endpoint_synthesis"},
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
				"properties": map[string]any{"verb": "GET", "path": "/widgets", "pattern_type": "http_endpoint_synthesis"},
			},
			{
				"id": "ep-delete", "name": "http:DELETE:/widgets", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties": map[string]any{"verb": "DELETE", "path": "/widgets", "pattern_type": "http_endpoint_synthesis"},
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
