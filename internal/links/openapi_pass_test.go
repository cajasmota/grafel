package links

import (
	"path/filepath"
	"testing"
)

// TestOpenAPISpecPass_HappyPath verifies the canonical three-repo scenario:
//   - repo "api-spec" contains an openapi_operation entity (GET /users/{id})
//   - repo "backend" contains an http_endpoint producer + IMPLEMENTS edge
//   - repo "frontend" contains an http_endpoint consumer + source_caller
//
// Expected: one openapi-spec method link from frontend::fn1 → backend::h1
// with channel=http and identifier=http:GET:/users/{id}.
func TestOpenAPISpecPass_HappyPath(t *testing.T) {
	root := fixtureRoot(t)

	// Spec repo: openapi_operation entity for GET /users/{id}.
	writeFixture(t, root, fixtureGraph{
		Repo: "api-spec",
		Entities: []map[string]any{
			{
				"id":          "op1",
				"name":        "openapi_op_get__users_{id}",
				"kind":        "openapi_operation",
				"source_file": "openapi.yaml",
				"properties": map[string]any{
					"method": "GET",
					"path":   "/users/{id}",
					"kind":   "openapi",
				},
			},
		},
		Edges: []map[string]string{},
	})

	// Backend repo: handler + producer http_endpoint.
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{
				"id":          "h1",
				"name":        "UserView",
				"kind":        "Controller",
				"source_file": "app/views.py",
			},
			{
				"id":          "ep1",
				"name":        "http:GET:/users/{id}",
				"kind":        "http_endpoint",
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

	// Frontend repo: caller function + consumer http_endpoint.
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{
				"id":          "fn1",
				"name":        "loadUser",
				"kind":        "Function",
				"source_file": "src/api.ts",
			},
			{
				"id":          "ep2",
				"name":        "http:GET:/users/{id}",
				"kind":        "http_endpoint",
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
	if _, err := RunAllPasses("goa", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "goa-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var hit *Link
	for i, l := range doc.Links {
		if l.Method == MethodOpenAPISpec {
			hit = &doc.Links[i]
			break
		}
	}
	if hit == nil {
		t.Fatalf("expected at least one openapi-spec link, got links: %+v", doc.Links)
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
	if hit.MatchQuality != openAPISpecMatchQuality {
		t.Errorf("match_quality: want %q, got %q", openAPISpecMatchQuality, hit.MatchQuality)
	}
}

// TestOpenAPISpecPass_AnyVerbProducer checks that a Django ANY-verb producer
// correctly matches a spec GET operation and a GET consumer.
func TestOpenAPISpecPass_AnyVerbProducer(t *testing.T) {
	root := fixtureRoot(t)

	writeFixture(t, root, fixtureGraph{
		Repo: "contracts",
		Entities: []map[string]any{
			{
				"id":          "op1",
				"name":        "openapi_op_post__items",
				"kind":        "openapi_operation",
				"source_file": "openapi.yaml",
				"properties": map[string]any{
					"method": "POST",
					"path":   "/items",
				},
			},
		},
		Edges: []map[string]string{},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{
				"id":          "h1",
				"name":        "ItemView",
				"kind":        "Controller",
				"source_file": "views.py",
			},
			{
				"id":          "ep1",
				"name":        "http:ANY:/items",
				"kind":        "http_endpoint",
				"source_file": "views.py",
				"properties": map[string]any{
					"verb":         "ANY",
					"path":         "/items",
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
			{
				"id":          "fn2",
				"name":        "createItem",
				"kind":        "Function",
				"source_file": "api.js",
			},
			{
				"id":          "ep2",
				"name":        "http:POST:/items",
				"kind":        "http_endpoint",
				"source_file": "api.js",
				"properties": map[string]any{
					"verb":          "POST",
					"path":          "/items",
					"framework":     "axios",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:createItem",
				},
			},
		},
		Edges: []map[string]string{},
	})

	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("goa2", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "goa2-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, l := range doc.Links {
		if l.Method == MethodOpenAPISpec && l.Source == "mobile::fn2" && l.Target == "backend::h1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected openapi-spec link mobile::fn2 → backend::h1, got %+v", doc.Links)
	}
}

// TestOpenAPISpecPass_NoMatchWhenNoSpec ensures the pass emits nothing when
// no openapi_operation entities exist in the group (pure direct-HTTP group).
func TestOpenAPISpecPass_NoMatchWhenNoSpec(t *testing.T) {
	root := fixtureRoot(t)

	// Two repos with http_endpoints but no spec.
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{
				"id":          "ep1",
				"name":        "http:GET:/foo",
				"kind":        "http_endpoint",
				"source_file": "views.py",
				"properties": map[string]any{
					"verb":         "GET",
					"path":         "/foo",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{
				"id":          "ep2",
				"name":        "http:GET:/foo",
				"kind":        "http_endpoint",
				"source_file": "api.ts",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/foo",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:fetchFoo",
				},
			},
		},
		Edges: []map[string]string{},
	})

	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("gnos", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "gnos-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range doc.Links {
		if l.Method == MethodOpenAPISpec {
			t.Errorf("expected no openapi-spec links when no spec present, got %+v", l)
		}
	}
}

// TestOpenAPISpecPass_VerbMismatch ensures the pass does NOT link a spec
// GET operation to a consumer that calls POST (different verb).
func TestOpenAPISpecPass_VerbMismatch(t *testing.T) {
	root := fixtureRoot(t)

	writeFixture(t, root, fixtureGraph{
		Repo: "contracts",
		Entities: []map[string]any{
			{
				"id":          "op1",
				"name":        "openapi_op_get__users",
				"kind":        "openapi_operation",
				"source_file": "openapi.yaml",
				"properties": map[string]any{
					"method": "GET",
					"path":   "/users",
				},
			},
		},
		Edges: []map[string]string{},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{
				"id":          "ep1",
				"name":        "http:GET:/users",
				"kind":        "http_endpoint",
				"source_file": "views.py",
				"properties": map[string]any{
					"verb":         "GET",
					"path":         "/users",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{
				"id":          "ep2",
				"name":        "http:POST:/users",
				"kind":        "http_endpoint",
				"source_file": "api.ts",
				"properties": map[string]any{
					"verb":          "POST", // mismatch with spec GET
					"path":          "/users",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:createUser",
				},
			},
		},
		Edges: []map[string]string{},
	})

	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("gverb", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "gverb-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range doc.Links {
		if l.Method == MethodOpenAPISpec {
			t.Errorf("expected no openapi-spec links on verb mismatch, got %+v", l)
		}
	}
}

// TestOpenAPISpecPass_PathParamNormalisation verifies that path parameter
// name differences ({id} vs {pk} vs :id) are normalised before matching.
func TestOpenAPISpecPass_PathParamNormalisation(t *testing.T) {
	root := fixtureRoot(t)

	// Spec uses {id} style.
	writeFixture(t, root, fixtureGraph{
		Repo: "contracts",
		Entities: []map[string]any{
			{
				"id":          "op1",
				"name":        "openapi_op_get__products_{id}",
				"kind":        "openapi_operation",
				"source_file": "openapi.yaml",
				"properties": map[string]any{
					"method": "GET",
					"path":   "/products/{id}",
				},
			},
		},
		Edges: []map[string]string{},
	})
	// Backend uses {pk} style (Django default).
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{
				"id":          "h1",
				"name":        "ProductView",
				"kind":        "Controller",
				"source_file": "views.py",
			},
			{
				"id":          "ep1",
				"name":        "http:GET:/products/{pk}",
				"kind":        "http_endpoint",
				"source_file": "views.py",
				"properties": map[string]any{
					"verb":         "GET",
					"path":         "/products/{pk}",
					"pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
		},
	})
	// Frontend uses :id Express style.
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{
				"id":          "fn1",
				"name":        "getProduct",
				"kind":        "Function",
				"source_file": "api.ts",
			},
			{
				"id":          "ep2",
				"name":        "http:GET:/products/:id",
				"kind":        "http_endpoint",
				"source_file": "api.ts",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/products/:id",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:getProduct",
				},
			},
		},
		Edges: []map[string]string{},
	})

	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("gparam", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "gparam-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, l := range doc.Links {
		if l.Method == MethodOpenAPISpec && l.Source == "frontend::fn1" && l.Target == "backend::h1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected openapi-spec link across path-param style differences; got %+v", doc.Links)
	}
}

// TestOpenAPISpecPass_CoexistsWithHTTPPass verifies that when BOTH the
// http pass (P4) and the openapi-spec pass (P5) fire for the same
// consumer→producer pair, both links appear in the output (they have
// distinct method values and therefore distinct IDs).
func TestOpenAPISpecPass_CoexistsWithHTTPPass(t *testing.T) {
	root := fixtureRoot(t)

	writeFixture(t, root, fixtureGraph{
		Repo: "contracts",
		Entities: []map[string]any{
			{
				"id":          "op1",
				"name":        "openapi_op_get__items",
				"kind":        "openapi_operation",
				"source_file": "openapi.yaml",
				"properties": map[string]any{
					"method": "GET",
					"path":   "/items",
				},
			},
		},
		Edges: []map[string]string{},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{
				"id":          "h1",
				"name":        "ItemsView",
				"kind":        "Controller",
				"source_file": "views.py",
			},
			{
				"id":          "ep1",
				"name":        "http:GET:/items",
				"kind":        "http_endpoint",
				"source_file": "views.py",
				"properties": map[string]any{
					"verb":         "GET",
					"path":         "/items",
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
				"id":          "fn1",
				"name":        "fetchItems",
				"kind":        "Function",
				"source_file": "api.ts",
			},
			{
				"id":          "ep2",
				"name":        "http:GET:/items",
				"kind":        "http_endpoint",
				"source_file": "api.ts",
				"properties": map[string]any{
					"verb":          "GET",
					"path":          "/items",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:fetchItems",
				},
			},
		},
		Edges: []map[string]string{},
	})

	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("gcoex", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "gcoex-links.json"))
	if err != nil {
		t.Fatal(err)
	}

	var hasHTTP, hasOASpec bool
	for _, l := range doc.Links {
		if l.Source == "frontend::fn1" && l.Target == "backend::h1" {
			switch l.Method {
			case MethodHTTP:
				hasHTTP = true
			case MethodOpenAPISpec:
				hasOASpec = true
			}
		}
	}
	if !hasHTTP {
		t.Errorf("expected a method=http link from P4 pass; got %+v", doc.Links)
	}
	if !hasOASpec {
		t.Errorf("expected a method=openapi-spec link from P5 pass; got %+v", doc.Links)
	}
}
