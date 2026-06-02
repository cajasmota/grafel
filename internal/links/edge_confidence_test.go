package links

import (
	"os"
	"path/filepath"
	"testing"
)

// edge_confidence_test.go asserts the #3628 extraction-confidence honesty
// marker is stamped with the correct value for each synthesised edge family.
//
// The three values are asserted VALUE-by-VALUE against real end-to-end pass
// output (not len>0): a fully cross-repo-matched HTTP pair must be "resolved",
// a runtime-dynamic `${apiUrl}`-derived call must be "inferred", and a fuzzy
// string-literal match must be "heuristic".

// TestEdgeConfidence_ResolvedHTTPPair: a consumer caller and a producer handler
// matched on the same canonical (verb, path) id → confidence=resolved.
func TestEdgeConfidence_ResolvedHTTPPair(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "UserView", "kind": "Controller", "source_file": "app/views.py"},
			{
				"id": "ep1", "name": "http:GET:/users/{id}", "kind": "http_endpoint",
				"source_file": "app/views.py",
				"properties": map[string]any{
					"verb": "GET", "path": "/users/{id}",
					"framework": "django", "pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"}},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "loadUser", "kind": "Function", "source_file": "src/api.ts"},
			{
				"id": "ep2", "name": "http:GET:/users/{id}", "kind": "http_endpoint",
				"source_file": "src/api.ts",
				"properties": map[string]any{
					"verb": "GET", "path": "/users/{id}",
					"framework": "fetch", "pattern_type": "http_endpoint_client_synthesis",
					"source_caller": "Function:loadUser",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("gconf-resolved", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "gconf-resolved-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	hit := findLink(doc.Links, func(l Link) bool {
		return l.Method == MethodHTTP && l.Source == "frontend::fn1" && l.Target == "backend::h1"
	})
	if hit == nil {
		t.Fatalf("expected matched HTTP pair frontend::fn1 → backend::h1; got %+v", doc.Links)
	}
	if got := hit.Properties[EdgeConfidenceKey]; got != ConfidenceResolved {
		t.Errorf("confidence: want %q (both endpoints matched on canonical id), got %q",
			ConfidenceResolved, got)
	}
}

// TestEdgeConfidence_InferredDynamicSuffix: a `${apiUrl}/schedule/import` call
// whose static suffix uniquely matched a backend → confidence=inferred.
func TestEdgeConfidence_InferredDynamicSuffix(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "ScheduleViewSet", "kind": "Controller", "source_file": "core/views/schedule_viewset.py"},
			{
				"id": "ep1", "name": "http:POST:/api/v1/schedule/import", "kind": "http_endpoint",
				"source_file": "core/views/schedule_viewset.py",
				"properties": map[string]any{
					"verb": "POST", "path": "/api/v1/schedule/import",
					"framework": "django", "pattern_type": "http_endpoint_synthesis",
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
					"verb": "POST", "path": "/{apiUrl}/schedule/import",
					"framework": "axios", "pattern_type": "http_endpoint_client_synthesis",
					"url_kind": "dynamic_baseurl", "dynamic_baseurl": "true",
					"source_caller": "Function:importSchedule",
				},
			},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("gconf-inferred", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "gconf-inferred-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	hit := findLink(doc.Links, func(l Link) bool {
		return l.Method == MethodHTTP && l.Source == "frontend::fn1" && l.Target == "backend::h1"
	})
	if hit == nil {
		t.Fatalf("expected dynamic_suffix_match link frontend::fn1 → backend::h1; got %+v", doc.Links)
	}
	// Guard: this MUST be the runtime-dynamic strategy, otherwise the value
	// assertion below would be meaningless.
	if got := hit.Properties["resolve_strategy"]; got != "dynamic_suffix_match" {
		t.Fatalf("precondition: want resolve_strategy=dynamic_suffix_match, got %q", got)
	}
	if got := hit.Properties[EdgeConfidenceKey]; got != ConfidenceInferred {
		t.Errorf("confidence: want %q (runtime-dynamic URL-derived), got %q",
			ConfidenceInferred, got)
	}
}

// TestEdgeConfidence_HeuristicStringMatch: two repos sharing a string literal
// (an HTTP path catalog match) → confidence=heuristic.
func TestEdgeConfidence_HeuristicStringMatch(t *testing.T) {
	root := fixtureRoot(t)
	mkRepo := func(name string) {
		dir := filepath.Join(root, name, "src")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "h.go"),
			[]byte("package main\nvar p = \"/api/v1/orders/{id}\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		writeFixture(t, root, fixtureGraph{
			Repo: name,
			Entities: []map[string]any{
				{"id": name + "_e", "name": "Handler", "kind": "function", "source_file": "src/h.go"},
			},
		})
	}
	mkRepo("alpha")
	mkRepo("beta")
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("gconf-heuristic", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "gconf-heuristic-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	hit := findLink(doc.Links, func(l Link) bool {
		return l.Method == MethodString && l.Identifier != nil && *l.Identifier == "/api/v1/orders/{id}"
	})
	if hit == nil {
		t.Fatalf("expected string match link for /api/v1/orders/{id}; got %+v", doc.Links)
	}
	if got := hit.Properties[EdgeConfidenceKey]; got != ConfidenceHeuristic {
		t.Errorf("confidence: want %q (fuzzy string-literal match), got %q",
			ConfidenceHeuristic, got)
	}
}

// TestEdgeConfidence_ImportPassUnstamped documents the absence⇒resolved
// contract: AST-grounded import_pass links deliberately do NOT carry the
// marker, so a consumer treats a missing value as "resolved".
func TestEdgeConfidence_WithEdgeConfidenceSetter(t *testing.T) {
	var l Link
	l.WithEdgeConfidence(ConfidenceResolved)
	if l.Properties[EdgeConfidenceKey] != ConfidenceResolved {
		t.Fatalf("setter did not stamp marker: %+v", l.Properties)
	}
	// Idempotent re-stamp must overwrite, not append a second key.
	l.WithEdgeConfidence(ConfidenceHeuristic)
	if l.Properties[EdgeConfidenceKey] != ConfidenceHeuristic {
		t.Fatalf("setter did not overwrite: %+v", l.Properties)
	}
	if len(l.Properties) != 1 {
		t.Fatalf("setter must reuse the same key; got %d props: %+v", len(l.Properties), l.Properties)
	}
}
