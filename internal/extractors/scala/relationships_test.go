package scala_test

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/scala"
	"github.com/cajasmota/archigraph/internal/types"
)

func runScala(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("scala")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Test.scala",
		Content:  []byte(src),
		Language: "scala",
		Tree:     tree,
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
