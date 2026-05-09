package dart_test

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/dart"
	"github.com/cajasmota/archigraph/internal/types"
)

// runDart runs the regex-based dart extractor on src and returns the
// resulting entity records. The fixed file path is reflected in the
// expected structural-ref strings.
func runDart(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	ext, _ := extractor.Get("dart")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Test.dart",
		Content:  []byte(src),
		Language: "dart",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func dFind(ents []types.EntityRecord, name, kind string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return &ents[i]
		}
	}
	return nil
}

func dHasRel(ents []types.EntityRecord, name, kind, edgeKind, toID string) bool {
	e := dFind(ents, name, kind)
	if e == nil {
		return false
	}
	for _, r := range e.Relationships {
		if r.Kind == edgeKind && r.ToID == toID {
			return true
		}
	}
	return false
}

// TestDart_ContainsClassMethods (#369): class declarations attach one
// CONTAINS edge per method declared in the body, with structural-ref
// (Format A) shape `scope:operation:method:dart:<file>:<name>`.
func TestDart_ContainsClassMethods(t *testing.T) {
	src := `class Foo {
  void a() {
    helper();
  }
  int b(int x) {
    return x;
  }
  void c() {
    print('hi');
  }
}
`
	ents := runDart(t, src)
	foo := dFind(ents, "Foo", "SCOPE.Component")
	if foo == nil {
		t.Fatal("expected Foo component")
	}
	contains := 0
	for _, r := range foo.Relationships {
		if r.Kind == "CONTAINS" {
			contains++
		}
	}
	if contains != 3 {
		t.Errorf("expected 3 CONTAINS edges, got %d (rels=%+v)", contains, foo.Relationships)
	}
	for _, m := range []string{"a", "b", "c"} {
		want := "scope:operation:method:dart:Test.dart:" + m
		if !dHasRel(ents, "Foo", "SCOPE.Component", "CONTAINS", want) {
			t.Errorf("expected CONTAINS Foo→%s", want)
		}
	}
}

// TestDart_CallsBareName: bare invocations inside a method body produce
// CALLS edges with the leaf identifier as ToID, deduped.
func TestDart_CallsBareName(t *testing.T) {
	src := `class A {
  void caller() {
    helper();
    helper();
    print('x');
  }
  void helper() {}
}
`
	ents := runDart(t, src)
	if !dHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "helper") {
		t.Errorf("expected CALLS caller→helper")
	}
	caller := dFind(ents, "caller", "SCOPE.Operation")
	if caller == nil {
		t.Fatal("expected caller op")
	}
	n := 0
	for _, r := range caller.Relationships {
		if r.Kind == "CALLS" && r.ToID == "helper" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("expected dedup CALLS caller→helper to 1, got %d", n)
	}
	if !dHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "print") {
		t.Errorf("expected CALLS caller→print")
	}
}

// TestDart_CallsKeywordsFiltered: control-flow tokens (if/for/while/etc.)
// must NOT appear as CALLS targets even though they parenthesise an expr.
func TestDart_CallsKeywordsFiltered(t *testing.T) {
	src := `class A {
  void caller() {
    if (true) {
      print('a');
    }
    for (var i = 0; i < 3; i++) {
      doit();
    }
    while (false) {}
  }
  void doit() {}
}
`
	ents := runDart(t, src)
	caller := dFind(ents, "caller", "SCOPE.Operation")
	if caller == nil {
		t.Fatal("expected caller op")
	}
	for _, r := range caller.Relationships {
		if r.Kind != "CALLS" {
			continue
		}
		switch r.ToID {
		case "if", "for", "while", "switch", "return", "throw", "catch", "this", "super":
			t.Errorf("dart keyword %q must not be emitted as CALLS target", r.ToID)
		}
	}
	if !dHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "doit") {
		t.Error("expected CALLS caller→doit (real method call inside for-body)")
	}
	if !dHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "print") {
		t.Error("expected CALLS caller→print (real method call inside if-body)")
	}
}

// TestDart_CallsReceiverChain: navigation calls `obj.method()` and
// `this.method()` resolve to ToID=method. The receiver root is recorded
// under Properties["receiver_root"] when it is a non-keyword identifier.
func TestDart_CallsReceiverChain(t *testing.T) {
	src := `class S {
  List<int> _items = [];
  void caller() {
    _items.add(1);
    this.helper();
    a.b.c.d();
  }
  void helper() {}
}
`
	ents := runDart(t, src)
	caller := dFind(ents, "caller", "SCOPE.Operation")
	if caller == nil {
		t.Fatal("expected caller op")
	}

	// _items.add → target "add", receiver_root "_items"
	var foundAdd bool
	for _, r := range caller.Relationships {
		if r.Kind == "CALLS" && r.ToID == "add" {
			foundAdd = true
			if r.Properties["receiver_root"] != "_items" {
				t.Errorf("expected receiver_root=_items, got %q", r.Properties["receiver_root"])
			}
		}
	}
	if !foundAdd {
		t.Error("expected CALLS caller→add")
	}

	// this.helper() → target "helper", receiver_root suppressed (this).
	var foundHelper bool
	for _, r := range caller.Relationships {
		if r.Kind == "CALLS" && r.ToID == "helper" {
			foundHelper = true
			if rr := r.Properties["receiver_root"]; rr != "" {
				t.Errorf("expected no receiver_root for this.helper(), got %q", rr)
			}
		}
	}
	if !foundHelper {
		t.Error("expected CALLS caller→helper for this.helper()")
	}

	// a.b.c.d() → target "d", receiver_root "a"
	var foundD bool
	for _, r := range caller.Relationships {
		if r.Kind == "CALLS" && r.ToID == "d" {
			foundD = true
			if r.Properties["receiver_root"] != "a" {
				t.Errorf("expected receiver_root=a, got %q", r.Properties["receiver_root"])
			}
		}
	}
	if !foundD {
		t.Error("expected CALLS caller→d for a.b.c.d()")
	}
}

// TestDart_ImportProperties: IMPORTS edges carry the property contract
// (local_name, source_module, imported_name) matching the Java/Python
// schema, including alias handling for `as <prefix>` imports.
func TestDart_ImportProperties(t *testing.T) {
	src := `import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'dart:convert';
import 'foo.dart';
`
	ents := runDart(t, src)

	want := map[string]struct {
		local, mod, imported string
	}{
		"package:flutter/material.dart": {"material", "package:flutter", "material"},
		"package:http/http.dart":        {"http", "package:http", "http"},
		"dart:convert":                  {"convert", "dart:convert", "convert"},
		"foo.dart":                      {"foo", "foo", "foo"},
	}
	seen := map[string]bool{}
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind != "IMPORTS" {
				continue
			}
			w, ok := want[r.ToID]
			if !ok {
				continue
			}
			seen[r.ToID] = true
			if r.FromID != "Test.dart" {
				t.Errorf("IMPORTS %s: FromID=%q want Test.dart", r.ToID, r.FromID)
			}
			if r.Properties["local_name"] != w.local {
				t.Errorf("IMPORTS %s: local_name=%q want %q", r.ToID, r.Properties["local_name"], w.local)
			}
			if r.Properties["source_module"] != w.mod {
				t.Errorf("IMPORTS %s: source_module=%q want %q", r.ToID, r.Properties["source_module"], w.mod)
			}
			if r.Properties["imported_name"] != w.imported {
				t.Errorf("IMPORTS %s: imported_name=%q want %q", r.ToID, r.Properties["imported_name"], w.imported)
			}
		}
	}
	for k := range want {
		if !seen[k] {
			t.Errorf("expected IMPORTS edge for %s", k)
		}
	}
}

// TestDart_LanguageTagged: every emitted relationship carries
// Properties["language"]="dart" so the resolver's per-language
// dynamic-pattern dispatch can fire (#90).
func TestDart_LanguageTagged(t *testing.T) {
	src := `import 'package:foo/bar.dart';

class A {
  void caller() {
    helper();
  }
  void helper() {}
}
`
	ents := runDart(t, src)
	rels := 0
	for _, e := range ents {
		for _, r := range e.Relationships {
			rels++
			if r.Properties["language"] != "dart" {
				t.Errorf("rel %s/%s missing language=dart property: %+v",
					r.Kind, r.ToID, r.Properties)
			}
		}
	}
	if rels == 0 {
		t.Fatal("expected at least one relationship for a fixture with import + class + call")
	}
}

// TestDart_TopLevelFunctionNoContainer: top-level functions have no
// enclosing class — they emit CALLS edges but no CONTAINS attachment.
func TestDart_TopLevelFunctionNoContainer(t *testing.T) {
	src := `void main() {
  doit();
}

void doit() {}
`
	ents := runDart(t, src)
	if !dHasRel(ents, "main", "SCOPE.Operation", "CALLS", "doit") {
		t.Error("expected CALLS main→doit for top-level function")
	}
	for _, e := range ents {
		if e.Kind != "SCOPE.Component" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "CONTAINS" {
				t.Errorf("unexpected CONTAINS edge from component %q (no class is declared)", e.Name)
			}
		}
	}
}
