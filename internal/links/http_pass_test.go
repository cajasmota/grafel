package links

import (
	"path/filepath"
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
