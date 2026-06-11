package scala_test

import (
	"testing"

	_ "github.com/cajasmota/archigraph/internal/extractors/scala"
	"github.com/cajasmota/archigraph/internal/types"
)

// scalaCallRecv returns the receiver_type stamped on a CALLS edge from `fn`
// (SCOPE.Operation) to `toID`, plus whether such an edge exists.
func scalaCallRecv(ents []types.EntityRecord, fn, toID string) (string, bool) {
	e := scalaFind(ents, fn, "SCOPE.Operation")
	if e == nil {
		return "", false
	}
	for _, r := range e.Relationships {
		if r.Kind == "CALLS" && r.ToID == toID {
			return r.Properties["receiver_type"], true
		}
	}
	return "", false
}

// #4749 — local-variable receiver typing (the Scala slice of epic #4615). A unit
// test that constructs a controller and calls its method
// (`val c = new FooController(...); c.create()`) must resolve to a
// `FooController.create` CALLS target carrying receiver_type=FooController, so the
// shared coverage linkage credits the controller method the unit test exercises.
func TestScalaLocalVarCtorReceiver_4749(t *testing.T) {
	src := `package controllers
object UserControllerSpec {
  def test() = {
    val c = new UserController(svc)
    c.create()
  }
}`
	ents := runScala(t, src)
	recv, ok := scalaCallRecv(ents, "test", "UserController.create")
	if !ok {
		t.Fatalf("expected CALLS test→UserController.create, rels=%+v",
			func() interface{} { e := scalaFind(ents, "test", "SCOPE.Operation"); if e != nil { return e.Relationships }; return nil }())
	}
	if recv != "UserController" {
		t.Fatalf("receiver_type=%q want UserController", recv)
	}
}

// Explicit type annotation seeds the local even when the RHS is a factory.
func TestScalaLocalVarAnnotatedReceiver_4749(t *testing.T) {
	src := `package controllers
object OrderControllerSpec {
  def test() = {
    val c: OrderController = makeController()
    c.update()
  }
}`
	ents := runScala(t, src)
	recv, ok := scalaCallRecv(ents, "test", "OrderController.update")
	if !ok {
		t.Fatalf("expected CALLS test→OrderController.update")
	}
	if recv != "OrderController" {
		t.Fatalf("receiver_type=%q want OrderController", recv)
	}
}

// Negative: an UNTYPED factory local (`val c = makeController()`, no annotation,
// no `new`) stays bare — the method call must NOT acquire a fabricated
// receiver_type. (#4683-style negative case.)
func TestScalaLocalVarUntypedFactoryStaysBare_4749(t *testing.T) {
	src := `package controllers
object FooSpec {
  def test() = {
    val c = makeController()
    c.run()
  }
}`
	ents := runScala(t, src)
	// The bare-name target "run" should exist; a dotted "X.run" must NOT.
	if recv, ok := scalaCallRecv(ents, "test", "run"); !ok || recv != "" {
		t.Fatalf("untyped factory local should yield bare CALLS test→run (recv empty); ok=%v recv=%q", ok, recv)
	}
}
