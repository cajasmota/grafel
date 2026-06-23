package scala_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/scala"
	"github.com/cajasmota/grafel/internal/types"
)

func runScala(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("scala")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Test.scala",
		Content:  []byte(src),
		Language: "scala",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func scalaFind(ents []types.EntityRecord, name, kind string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return &ents[i]
		}
	}
	return nil
}

func scalaHasRel(ents []types.EntityRecord, name, kind, edgeKind, toID string) bool {
	e := scalaFind(ents, name, kind)
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

// TestScala_ContainsClassMethods (#379): class with N methods → N
// CONTAINS edges keyed on the canonical Format A structural-ref.
func TestScala_ContainsClassMethods(t *testing.T) {
	src := `class Foo {
  def a() = {}
  def b(x: Int) = {}
  def c() = {}
}
`
	ents := runScala(t, src)
	foo := scalaFind(ents, "Foo", "SCOPE.Component")
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
		want := "scope:operation:method:scala:Test.scala:" + m
		if !scalaHasRel(ents, "Foo", "SCOPE.Component", "CONTAINS", want) {
			t.Errorf("expected CONTAINS Foo→%s", want)
		}
	}
}

// TestScala_ContainsObjectAndTraitMethods (#379): object and trait
// containers also emit CONTAINS for their declared functions
// (function_declaration in trait bodies has no block).
func TestScala_ContainsObjectAndTraitMethods(t *testing.T) {
	src := `object O {
  def make(): Int = 1
}
trait T {
  def t1(): Int
  def t2(): String
}
`
	ents := runScala(t, src)
	if !scalaHasRel(ents, "O", "SCOPE.Component", "CONTAINS",
		"scope:operation:method:scala:Test.scala:make") {
		t.Errorf("expected CONTAINS O→make")
	}
	for _, m := range []string{"t1", "t2"} {
		want := "scope:operation:method:scala:Test.scala:" + m
		if !scalaHasRel(ents, "T", "SCOPE.Component", "CONTAINS", want) {
			t.Errorf("expected CONTAINS T→%s", want)
		}
	}
}

// TestScala_CallsBareName (#379): bare-name method invocation in a
// function body emits a CALLS edge with the bare callee name. Dedup is
// applied per (target).
func TestScala_CallsBareName(t *testing.T) {
	src := `class A {
  def caller() = {
    helper()
    helper()
    println("x")
  }
  def helper() = {}
}
`
	ents := runScala(t, src)
	if !scalaHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "helper") {
		t.Errorf("expected CALLS caller→helper")
	}
	if !scalaHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "println") {
		t.Errorf("expected CALLS caller→println")
	}
	caller := scalaFind(ents, "caller", "SCOPE.Operation")
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

// TestScala_CallsReceiverFieldDottedTarget (#379): when the receiver
// of a field_expression is a val/var/class-parameter with a known
// declared type, the CALLS target is "<Type>.<method>" and Properties
// carries `receiver_type=<Type>`.
func TestScala_CallsReceiverFieldDottedTarget(t *testing.T) {
	src := `class C(val repo: Repo) {
  val helper: Helper = new Helper()
  def f(p: Param) = {
    repo.find()
    helper.run()
    p.go()
  }
}
`
	ents := runScala(t, src)
	caller := scalaFind(ents, "f", "SCOPE.Operation")
	if caller == nil {
		t.Fatal("expected operation f")
	}
	want := map[string]string{
		"Repo.find":  "Repo",
		"Helper.run": "Helper",
		"Param.go":   "Param",
	}
	got := map[string]string{}
	for _, r := range caller.Relationships {
		if r.Kind != "CALLS" {
			continue
		}
		got[r.ToID] = r.Properties["receiver_type"]
	}
	for to, recv := range want {
		v, ok := got[to]
		if !ok {
			t.Errorf("expected CALLS %s, got=%v", to, got)
			continue
		}
		if v != recv {
			t.Errorf("CALLS %s receiver_type=%q want=%q", to, v, recv)
		}
	}
}

// TestScala_CallsKeywordsFiltered (#379): scala keywords / special
// identifiers (`this`, `super`, `new`) must NOT be emitted as CALLS
// targets — they are not real call sites.
func TestScala_CallsKeywordsFiltered(t *testing.T) {
	src := `class A {
  def caller() = {
    this
    super.toString()
    helper()
  }
  def helper() = {}
}
`
	ents := runScala(t, src)
	caller := scalaFind(ents, "caller", "SCOPE.Operation")
	if caller == nil {
		t.Fatal("expected caller operation")
	}
	for _, r := range caller.Relationships {
		if r.Kind != "CALLS" {
			continue
		}
		switch r.ToID {
		case "this", "super", "new":
			t.Errorf("scala keyword %q must not be emitted as CALLS target", r.ToID)
		}
	}
}

// TestScala_ImportsCarryProperties (#379): IMPORTS edges must carry the
// metadata the cross-file resolver consumes (mirroring Python #93 and
// Java #120): local_name, source_module, imported_name.
func TestScala_ImportsCarryProperties(t *testing.T) {
	src := `
import scala.collection.mutable
import scala.util.{Try, Success, Failure}
import scala.collection.mutable._

class A {}
`
	ents := runScala(t, src)
	want := map[string]map[string]string{
		"scala.collection.mutable": {
			"local_name":    "mutable",
			"source_module": "scala.collection",
			"imported_name": "mutable",
		},
		"scala.util.Try": {
			"local_name":    "Try",
			"source_module": "scala.util",
			"imported_name": "Try",
		},
		"scala.util.Success": {
			"local_name":    "Success",
			"source_module": "scala.util",
			"imported_name": "Success",
		},
		"scala.util.Failure": {
			"local_name":    "Failure",
			"source_module": "scala.util",
			"imported_name": "Failure",
		},
	}
	got := map[string]map[string]string{}
	wildcardSeen := false
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind != "IMPORTS" {
				continue
			}
			got[r.ToID] = r.Properties
			if r.Properties["wildcard"] == "1" && r.ToID == "scala.collection.mutable" {
				wildcardSeen = true
			}
		}
	}
	for to, wantProps := range want {
		gotProps, ok := got[to]
		if !ok {
			// Wildcard import covers `scala.collection.mutable` ToID
			// and would shadow the plain import. The plain
			// `import scala.collection.mutable` is still emitted as a
			// separate edge — but both resolve to the same ToID, so
			// only one survives in the map. Skip if wildcard seen.
			if to == "scala.collection.mutable" && wildcardSeen {
				continue
			}
			t.Errorf("expected IMPORTS edge to=%q", to)
			continue
		}
		for k, v := range wantProps {
			if to == "scala.collection.mutable" && wildcardSeen {
				continue // wildcard shape — different props
			}
			if gotProps[k] != v {
				t.Errorf("IMPORTS to=%q prop %q: got=%q want=%q (all=%v)",
					to, k, gotProps[k], v, gotProps)
			}
		}
	}
	if !wildcardSeen {
		t.Errorf("expected wildcard IMPORTS edge for scala.collection.mutable._")
	}
}

// --- Issue #690: Scala field CONTAINS emission ---

// TestScala_FieldContains_ValDeclaration (#690): a class with body `val`
// members must emit SCOPE.Schema/field entities and CONTAINS edges.
func TestScala_FieldContains_ValDeclaration(t *testing.T) {
	src := `class Service {
  val name: String = "default"
  var count: Int = 0
  def run() = {}
}
`
	ents := runScala(t, src)
	svc := scalaFind(ents, "Service", "SCOPE.Component")
	if svc == nil {
		t.Fatal("expected Service component")
	}
	// Field entities must exist.
	for _, fn := range []string{"Service.name", "Service.count"} {
		if scalaFind(ents, fn, "SCOPE.Schema") == nil {
			t.Errorf("expected SCOPE.Schema entity %s", fn)
		}
	}
	// CONTAINS edges via structural-ref stubs.
	wantContains := map[string]bool{
		"scope:schema:field:scala:Test.scala:Service.name":  false,
		"scope:schema:field:scala:Test.scala:Service.count": false,
		"scope:operation:method:scala:Test.scala:run":       false,
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

// TestScala_FieldContains_CaseClassParams (#690): case class parameters are
// structural fields — each must produce a SCOPE.Schema/field entity and a
// CONTAINS edge.
func TestScala_FieldContains_CaseClassParams(t *testing.T) {
	src := `case class Order(id: Int, total: Double)
`
	ents := runScala(t, src)
	order := scalaFind(ents, "Order", "SCOPE.Component")
	if order == nil {
		t.Fatal("expected Order component")
	}
	for _, field := range []string{"Order.id", "Order.total"} {
		if scalaFind(ents, field, "SCOPE.Schema") == nil {
			t.Errorf("expected SCOPE.Schema entity %s", field)
		}
		want := "scope:schema:field:scala:Test.scala:" + field
		if !scalaHasRel(ents, "Order", "SCOPE.Component", "CONTAINS", want) {
			t.Errorf("expected CONTAINS Order→%s (rels=%+v)", want, order.Relationships)
		}
	}
}

// TestScala_FieldContains_ObjectMembers (#690): object member vals also get
// CONTAINS edges.
func TestScala_FieldContains_ObjectMembers(t *testing.T) {
	src := `object Config {
  val timeout: Int = 30
  val prefix: String = "app"
  def load() = {}
}
`
	ents := runScala(t, src)
	cfg := scalaFind(ents, "Config", "SCOPE.Component")
	if cfg == nil {
		t.Fatal("expected Config component")
	}
	for _, field := range []string{"Config.timeout", "Config.prefix"} {
		want := "scope:schema:field:scala:Test.scala:" + field
		if !scalaHasRel(ents, "Config", "SCOPE.Component", "CONTAINS", want) {
			t.Errorf("expected CONTAINS Config→%s (rels=%+v)", want, cfg.Relationships)
		}
	}
}

// TestScala_FieldContains_NoRegressionMethodContains (#690): adding field
// CONTAINS must not disrupt method CONTAINS. A class with both fields and
// methods must emit CONTAINS for all.
func TestScala_FieldContains_NoRegressionMethodContains(t *testing.T) {
	src := `class Repo {
  val db: DB = null
  def find() = {}
  def save() = {}
}
`
	ents := runScala(t, src)
	repo := scalaFind(ents, "Repo", "SCOPE.Component")
	if repo == nil {
		t.Fatal("expected Repo component")
	}
	wantContains := map[string]bool{
		"scope:schema:field:scala:Test.scala:Repo.db":  false,
		"scope:operation:method:scala:Test.scala:find": false,
		"scope:operation:method:scala:Test.scala:save": false,
	}
	for _, r := range repo.Relationships {
		if r.Kind == "CONTAINS" {
			if _, ok := wantContains[r.ToID]; ok {
				wantContains[r.ToID] = true
			}
		}
	}
	for stub, seen := range wantContains {
		if !seen {
			t.Errorf("expected CONTAINS Repo→%s (rels=%+v)", stub, repo.Relationships)
		}
	}
}
