package main

import (
	"strings"
	"testing"
)

// TestAudit2708_ArrowFnTemplateInlining is the integration guard for #2708.
//
// It indexes a fixture containing JS HTTP-client calls whose path is a
// template literal that interpolates a same-file arrow-function factory
// (`const base = (a, b) => `+"`"+`/api/v1/${a}/${b}/branches`+"`"+`). The
// extractor must inline the factory body, producing concrete paths with
// named placeholders (`/api/v1/{companyType}/{companyId}/branches/...`)
// rather than the pre-fix `{param}/{param}/...` opacity.
//
// Scope of the check:
//
//   - Each call site materialises a path that begins with the arrow-fn's
//     literal segment (`/api/v1/`).
//   - The arrow-fn's parameter names (`companyType`, `companyId`) appear
//     as `{companyType}` / `{companyId}` placeholders.
//   - No emitted endpoint retains the leading `{param}` opacity from the
//     unresolved factory call.
func TestAudit2708_ArrowFnTemplateInlining(t *testing.T) {
	doc := runIndexerOn(t, "testdata/audit_arrowfn_template", "audit2708", nil)

	type ep struct {
		Verb string
		Path string
	}
	var endpoints []ep
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Kind != "http_endpoint_call" && e.Kind != "http_endpoint" && e.Kind != "http_endpoint_definition" {
			continue
		}
		if e.Properties == nil {
			continue
		}
		endpoints = append(endpoints, ep{Verb: e.Properties["verb"], Path: e.Properties["path"]})
	}
	if len(endpoints) == 0 {
		t.Fatalf("audit_arrowfn_template: no http_endpoint(_call) entities emitted")
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

	cases := []struct {
		verb string
		path string
	}{
		{"GET", "/api/v1/{companyType}/{companyId}/branches"},
		{"GET", "/api/v1/{companyType}/{companyId}/branches/{branchId}"},
		{"POST", "/api/v1/{companyType}/{companyId}/branches/{branchId}/set_active"},
	}
	for _, tc := range cases {
		if !findBySuffix(tc.verb, tc.path) {
			var seen []string
			for _, e := range endpoints {
				seen = append(seen, e.Verb+" "+e.Path)
			}
			t.Errorf("audit2708: missing %s %s; saw: %v", tc.verb, tc.path, seen)
		}
	}

	// Negative: no endpoint should retain the leading `{param}` opacity
	// from the unresolved factory call.
	for _, e := range endpoints {
		if strings.HasPrefix(e.Path, "/{param}") || strings.HasPrefix(e.Path, "{param}") {
			t.Errorf("audit2708: endpoint kept unresolved leading {param}: %s %s", e.Verb, e.Path)
		}
	}
}
