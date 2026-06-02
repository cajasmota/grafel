package main

import (
	"strings"
	"testing"
)

// TestAudit2709_SubscriptEnumeration is the integration guard for #2709.
//
// It indexes a fixture containing a JS HTTP client call whose URL is a
// template literal interpolating `${TYPES[t]}` where TYPES is a same-file
// `const` flat object literal with string-literal values. The extractor
// must enumerate ONE consumer-side synthetic per known map value (rather
// than dropping the subscript expression as `{param}`), and each enumerated
// entity must carry a `polymorphic_subscript` property identifying the
// source `<ident>[<keyExpr>]` expression so downstream consumers can tell
// a discovered set apart from a literal route.
//
// The fixture also includes a static-key subscript (`TYPES["a"]`) to guard
// against regressions in the direct-substitution path.
func TestAudit2709_SubscriptEnumeration(t *testing.T) {
	doc := runIndexerOn(t, "testdata/audit_subscript_enum", "audit2709", nil)

	type ep struct {
		Verb       string
		Path       string
		PolySubscr string
	}
	endpoints := make([]ep, 0, 8)
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Kind != "http_endpoint_call" && e.Kind != "http_endpoint" &&
			e.Kind != "http_endpoint_definition" {
			continue
		}
		if e.Properties == nil {
			continue
		}
		endpoints = append(endpoints, ep{
			Verb:       strings.ToUpper(e.Properties["verb"]),
			Path:       e.Properties["path"],
			PolySubscr: e.Properties["polymorphic_subscript"],
		})
	}
	if len(endpoints) == 0 {
		t.Fatalf("audit_subscript_enum: no http_endpoint(_call) entities emitted")
	}

	findPoly := func(verb, suffix string) *ep {
		verb = strings.ToUpper(verb)
		for i := range endpoints {
			e := &endpoints[i]
			if e.Verb != verb {
				continue
			}
			if e.Path == suffix || strings.HasSuffix(e.Path, suffix) {
				return e
			}
		}
		return nil
	}

	// Dynamic-key enumeration: both /alpha/x AND /beta/x must be emitted,
	// each tagged with polymorphic_subscript = "TYPES[t]".
	for _, suffix := range []string{"/alpha/x", "/beta/x"} {
		got := findPoly("GET", suffix)
		if got == nil {
			var seen []string
			for _, e := range endpoints {
				seen = append(seen, e.Verb+" "+e.Path)
			}
			t.Errorf("audit2709: missing GET %s; saw: %v", suffix, seen)
			continue
		}
		if got.PolySubscr != "TYPES[t]" {
			t.Errorf("audit2709: GET %s expected polymorphic_subscript=\"TYPES[t]\", got %q",
				suffix, got.PolySubscr)
		}
	}

	// Static-key substitution: /alpha/y must be emitted WITHOUT the
	// polymorphic_subscript marker (it's a literal, not an enumeration).
	got := findPoly("GET", "/alpha/y")
	if got == nil {
		var seen []string
		for _, e := range endpoints {
			seen = append(seen, e.Verb+" "+e.Path)
		}
		t.Errorf("audit2709: missing GET /alpha/y; saw: %v", seen)
	} else if got.PolySubscr != "" {
		t.Errorf("audit2709: static-key GET /alpha/y must NOT carry polymorphic_subscript, got %q",
			got.PolySubscr)
	}

	// Negative check: nothing should retain the unresolved `{param}` shape
	// in the segment that the enumerator was supposed to expand.
	for _, e := range endpoints {
		if strings.Contains(e.Path, "/{param}/x") || strings.Contains(e.Path, "/{param}/y") {
			t.Errorf("audit2709: endpoint retained unexpanded subscript placeholder: %s %s",
				e.Verb, e.Path)
		}
	}
}
