package main

import (
	"strings"
	"testing"
)

// TestAudit2704_TemplateVarResolve is the integration guard for #2704.
//
// It indexes a fixture containing JS HTTP client calls whose path is a
// template literal whose leading interpolation references a same-file
// string-literal const (`const path = "things"`). The extractor must
// substitute the constant value, producing concrete paths like
// `/things/{id}` rather than the pre-fix `{param}/{id}` placeholder.
//
// Scope of the check (matches the issue acceptance criteria):
//
//   - Leading `${path}` resolves to `/things`.
//   - Trailing `${id}` / `${nestedId}` remain as `{id}` / `{nestedId}`
//     placeholders (those are real path params, not constants).
//   - No emitted endpoint retains the leading `{param}` / `{path}` shape.
func TestAudit2704_TemplateVarResolve(t *testing.T) {
	doc := runIndexerOn(t, "testdata/audit_template_var_resolve", "audit2704", nil)

	// Collect consumer-side synthetics (http_endpoint_call) for this fixture
	// — that's what client-side HTTP wrappers emit.
	endpoints := make([]*struct {
		Verb string
		Path string
	}, 0, 8)
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Kind != "http_endpoint_call" && e.Kind != "http_endpoint" && e.Kind != "http_endpoint_definition" {
			continue
		}
		if e.Properties == nil {
			continue
		}
		endpoints = append(endpoints, &struct {
			Verb string
			Path string
		}{Verb: e.Properties["verb"], Path: e.Properties["path"]})
	}
	if len(endpoints) == 0 {
		t.Fatalf("audit_template_var_resolve: no http_endpoint(_call) entities emitted")
	}

	findBySuffix := func(verb, suffix string) bool {
		verb = strings.ToUpper(verb)
		for _, e := range endpoints {
			if strings.ToUpper(e.Verb) != verb {
				continue
			}
			if e.Path == suffix || strings.HasSuffix(e.Path, suffix) {
				return true
			}
		}
		return false
	}

	// Expected (verb, path) pairs after constant folding of `${path}`.
	cases := []struct {
		verb string
		path string
	}{
		{"GET", "/things/{id}"},
		{"PATCH", "/things/{id}"},
		{"DELETE", "/things/{id}/items/{nestedId}"},
	}
	for _, tc := range cases {
		if !findBySuffix(tc.verb, tc.path) {
			var seen []string
			for _, e := range endpoints {
				seen = append(seen, e.Verb+" "+e.Path)
			}
			t.Errorf("audit2704: missing %s %s; saw: %v", tc.verb, tc.path, seen)
		}
	}

	// Negative check: no endpoint should retain the `{param}` / `{path}`
	// leading-placeholder shape after constant folding.
	for _, e := range endpoints {
		p := e.Path
		if strings.HasPrefix(p, "/{param}") || strings.HasPrefix(p, "{param}") ||
			strings.HasPrefix(p, "/{path}") || strings.HasPrefix(p, "{path}") {
			t.Errorf("audit2704: endpoint kept unresolved leading placeholder: %s %s",
				e.Verb, p)
		}
	}
}
