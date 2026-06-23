package java_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/java"
	"github.com/cajasmota/grafel/internal/types"
)

func runJava(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("java")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Test.java",
		Content:  []byte(src),
		Language: "java",
		TSTree:   tree,
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
	// IMPORTS ToIDs for the `java` external root are rewritten to
	// `ext:java:<leaf>` by resolveImportToIDs (analog of #642/#650) so
	// the resolver's external-disposition gate classifies them as
	// ExternalKnown directly.
	want := map[string]bool{"ext:java:List": false, "ext:java:Map": false}
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

// --- Issue #690: Java field CONTAINS emission ---

// TestJava_FieldContains_BareDeclaration (#690): a class with a bare field
// declaration (`private int count;`) must emit a SCOPE.Schema/field entity
// with name "<Class>.<field>" AND a CONTAINS edge from the class to the field
// via a BuildSchemaFieldStructuralRef stub.
func TestJava_FieldContains_BareDeclaration(t *testing.T) {
	src := `
class Box {
    private int count;
    String name;
}
`
	ents := runJava(t, src)
	box := javaFind(ents, "Box", "SCOPE.Component")
	if box == nil {
		t.Fatal("expected Box component")
	}
	// Field entities must be emitted with qualified names.
	if javaFind(ents, "Box.count", "SCOPE.Schema") == nil {
		t.Error("expected SCOPE.Schema entity Box.count")
	}
	if javaFind(ents, "Box.name", "SCOPE.Schema") == nil {
		t.Error("expected SCOPE.Schema entity Box.name")
	}
	// CONTAINS edges via structural-ref stubs.
	for _, field := range []string{"Box.count", "Box.name"} {
		want := "scope:schema:field:java:Test.java:" + field
		if !javaHasRel(ents, "Box", "SCOPE.Component", "CONTAINS", want) {
			t.Errorf("expected CONTAINS Box→%s (rels=%+v)", want, box.Relationships)
		}
	}
}

// TestJava_FieldContains_InitializedField (#690): a class with an initialized
// field (`String name = "default";`) must also produce a CONTAINS edge.
func TestJava_FieldContains_InitializedField(t *testing.T) {
	src := `
class Config {
    String name = "default";
    int count = 0;
}
`
	ents := runJava(t, src)
	for _, field := range []string{"Config.name", "Config.count"} {
		want := "scope:schema:field:java:Test.java:" + field
		if !javaHasRel(ents, "Config", "SCOPE.Component", "CONTAINS", want) {
			t.Errorf("expected CONTAINS Config→%s", want)
		}
	}
}

// TestJava_FieldContains_StaticField (#690): static fields must also get
// CONTAINS edges.
func TestJava_FieldContains_StaticField(t *testing.T) {
	src := `
class Constants {
    public static final int MAX = 100;
    private static String prefix = "app";
}
`
	ents := runJava(t, src)
	for _, field := range []string{"Constants.MAX", "Constants.prefix"} {
		want := "scope:schema:field:java:Test.java:" + field
		if !javaHasRel(ents, "Constants", "SCOPE.Component", "CONTAINS", want) {
			t.Errorf("expected CONTAINS Constants→%s", want)
		}
	}
}

// TestJava_FieldContains_NoRegressionMethodContains (#690): adding field
// CONTAINS must not break method CONTAINS — a class with both fields and
// methods must emit CONTAINS for all.
func TestJava_FieldContains_NoRegressionMethodContains(t *testing.T) {
	src := `
class Service {
    private Repo repo;
    private int count;
    void save() {}
    void load() {}
}
`
	ents := runJava(t, src)
	svc := javaFind(ents, "Service", "SCOPE.Component")
	if svc == nil {
		t.Fatal("expected Service component")
	}
	wantContains := map[string]bool{
		"scope:schema:field:java:Test.java:Service.repo":     false,
		"scope:schema:field:java:Test.java:Service.count":    false,
		"scope:operation:method:java:Test.java:Service.save": false,
		"scope:operation:method:java:Test.java:Service.load": false,
	}
	for _, r := range svc.Relationships {
		if r.Kind == "CONTAINS" {
			if _, ok := wantContains[r.ToID]; ok {
				wantContains[r.ToID] = true
			}
		}
	}
	for stub, seen := range wantContains {
		if !seen {
			t.Errorf("expected CONTAINS Service→%s (rels=%+v)", stub, svc.Relationships)
		}
	}
}
