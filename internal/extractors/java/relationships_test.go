package java_test

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/java"
	"github.com/cajasmota/archigraph/internal/types"
)

func runJava(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("java")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Test.java",
		Content:  []byte(src),
		Language: "java",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func javaFind(ents []types.EntityRecord, name, kind string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return &ents[i]
		}
	}
	return nil
}

func javaHasRel(ents []types.EntityRecord, name, kind, edgeKind, toID string) bool {
	e := javaFind(ents, name, kind)
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

// TestJava_ContainsClassMethods (#41): class with N methods → N CONTAINS edges.
func TestJava_ContainsClassMethods(t *testing.T) {
	src := `
class Foo {
  void a() {}
  void b(int x) {}
  void c() {}
}
`
	ents := runJava(t, src)
	foo := javaFind(ents, "Foo", "SCOPE.Component")
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
		t.Errorf("expected 3 CONTAINS edges from Foo, got %d (rels=%+v)", contains, foo.Relationships)
	}
	// Issue #144 — CONTAINS targets are structural-ref stubs (Format A)
	// keyed on the source file. The trailing :<name> segment carries the
	// dotted "Outer.member" form (issue #65).
	for _, m := range []string{"Foo.a", "Foo.b", "Foo.c"} {
		want := "scope:operation:method:java:Test.java:" + m
		if !javaHasRel(ents, "Foo", "SCOPE.Component", "CONTAINS", want) {
			t.Errorf("expected CONTAINS Foo→%s", want)
		}
	}
}

// TestJava_CallsBareName (#41): method calling another method → CALLS edge with stub.
func TestJava_CallsBareName(t *testing.T) {
	src := `
class A {
  void caller() { helper(); helper(); System.out.println("x"); }
  void helper() {}
}
`
	ents := runJava(t, src)
	if !javaHasRel(ents, "A.caller", "SCOPE.Operation", "CALLS", "helper") {
		t.Errorf("expected CALLS caller→helper")
	}
	if !javaHasRel(ents, "A.caller", "SCOPE.Operation", "CALLS", "println") {
		t.Errorf("expected CALLS caller→println (selector trailing)")
	}
	caller := javaFind(ents, "A.caller", "SCOPE.Operation")
	n := 0
	for _, r := range caller.Relationships {
		if r.Kind == "CALLS" && r.ToID == "helper" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("expected dedup CALLS caller→helper to 1, got %d", n)
	}
}

// TestJava_Imports (#41): import declarations emit IMPORTS relationships.
func TestJava_Imports(t *testing.T) {
	src := `
package x;
import java.util.List;
import java.util.Map;
class A {}
`
	ents := runJava(t, src)
	want := map[string]bool{"java.util.List": false, "java.util.Map": false}
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				if _, ok := want[r.ToID]; ok {
					want[r.ToID] = true
				}
			}
		}
	}
	for k, ok := range want {
		if !ok {
			t.Errorf("expected IMPORTS edge for %q", k)
		}
	}
}
