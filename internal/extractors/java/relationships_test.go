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

// TestJava_ImportsCarryProperties (#120): IMPORTS edges must carry the
// metadata the cross-file resolver consumes (mirroring Python #93):
// local_name, source_module, imported_name. For `import com.foo.Bar;`
// local_name="Bar", source_module="com.foo", imported_name="Bar".
func TestJava_ImportsCarryProperties(t *testing.T) {
	src := `
package x;
import com.foo.Bar;
import com.foo.Baz;
import static com.util.Helpers.staticMethod;
import com.wild.*;
class A {}
`
	ents := runJava(t, src)
	want := map[string]map[string]string{
		"com.foo.Bar": {
			"local_name":    "Bar",
			"source_module": "com.foo",
			"imported_name": "Bar",
		},
		"com.foo.Baz": {
			"local_name":    "Baz",
			"source_module": "com.foo",
			"imported_name": "Baz",
		},
		"com.util.Helpers.staticMethod": {
			"local_name":    "staticMethod",
			"source_module": "com.util.Helpers",
			"imported_name": "staticMethod",
		},
		"com.wild": {
			"source_module": "com.wild",
			"wildcard":      "1",
		},
	}
	got := map[string]map[string]string{}
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind != "IMPORTS" {
				continue
			}
			got[r.ToID] = r.Properties
		}
	}
	for to, wantProps := range want {
		gotProps, ok := got[to]
		if !ok {
			t.Errorf("expected IMPORTS edge to=%q, got=%v", to, got)
			continue
		}
		for k, v := range wantProps {
			if gotProps[k] != v {
				t.Errorf("IMPORTS to=%q prop %q: got=%q want=%q (all=%v)",
					to, k, gotProps[k], v, gotProps)
			}
		}
	}
}

// TestJava_CallsFieldReceiverDottedTarget (#120): a method invocation
// on a field whose declared type is known emits a CALLS edge with
// target "<FieldType>.<method>" — the dotted form the resolver
// indexes via byKind / byName for cross-file binding.
//
// Example pattern (Spring DI):
//
//	class OwnerController {
//	  @Autowired private OwnerRepository owners;
//	  void show(int id) { owners.findById(id); }
//	}
//
// Should emit CALLS show -> "OwnerRepository.findById".
func TestJava_CallsFieldReceiverDottedTarget(t *testing.T) {
	src := `
package x;
class OwnerRepository { Owner findById(int id) { return null; } }
class OwnerController {
  private OwnerRepository owners;
  void show(int id) { owners.findById(id); }
  void show2(int id) { this.owners.findById(id); }
}
`
	ents := runJava(t, src)
	if !javaHasRel(ents, "OwnerController.show", "SCOPE.Operation", "CALLS", "OwnerRepository.findById") {
		t.Errorf("expected CALLS show -> OwnerRepository.findById; got rels=%+v",
			javaFind(ents, "OwnerController.show", "SCOPE.Operation").Relationships)
	}
	if !javaHasRel(ents, "OwnerController.show2", "SCOPE.Operation", "CALLS", "OwnerRepository.findById") {
		t.Errorf("expected CALLS show2 -> OwnerRepository.findById (this.owners); got rels=%+v",
			javaFind(ents, "OwnerController.show2", "SCOPE.Operation").Relationships)
	}
}

// TestJava_CallsParameterReceiverDottedTarget (#120): a method
// invocation on a method parameter whose declared type is known emits
// CALLS with target "<ParamType>.<method>".
func TestJava_CallsParameterReceiverDottedTarget(t *testing.T) {
	src := `
package x;
class A {
  void run(OwnerRepository repo) { repo.findById(1); }
}
`
	ents := runJava(t, src)
	if !javaHasRel(ents, "A.run", "SCOPE.Operation", "CALLS", "OwnerRepository.findById") {
		t.Errorf("expected CALLS run -> OwnerRepository.findById from parameter receiver")
	}
}

// TestJava_CallsStaticReceiverDottedTarget (#120): when the receiver
// is a PascalCase identifier matched against the file's imports
// (e.g. `import com.foo.Helpers; Helpers.run()`), emit CALLS as
// "Helpers.run". Even without a direct match, a PascalCase receiver
// is a strong static-call signal and should be retained dotted so
// the resolver's byKind/byName can bind it.
func TestJava_CallsStaticReceiverDottedTarget(t *testing.T) {
	src := `
package x;
import com.foo.Helpers;
class A {
  void run() { Helpers.compute(); }
}
`
	ents := runJava(t, src)
	if !javaHasRel(ents, "A.run", "SCOPE.Operation", "CALLS", "Helpers.compute") {
		t.Errorf("expected CALLS run -> Helpers.compute (static receiver)")
	}
}
