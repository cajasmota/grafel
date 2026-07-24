// localvar_receiver_4749_test.go — Groovy local-variable receiver typing for
// test→CALLS coverage crediting (#4749, the Groovy slice of the coverage-linkage
// tail epic #4749/#4615; the JVM analog of Java #4682 / Kotlin).
//
// A Groovy unit test binds a local from a constructor and then calls a method on
// it; the call must resolve to the CLASS method so ComputeCoverage credits the
// endpoint through test→CALLS→handler. Without local-var typing the receiver `c`
// is a bare lowercase identifier and `c.index()` degrades to the unresolvable
// leaf `index`.
//
// Conservatism mirrors Java #4682: ONLY a direct `new ClassName(...)`
// initialiser types the local — a factory/builder RHS stays untyped and forges
// no class edge.

package groovy_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// hasCall reports whether e carries a CALLS edge to toID. When recv is non-empty
// it also requires receiver_type == recv.
func hasCall(e *types.EntityRecord, toID, recv string) bool {
	for _, r := range e.Relationships {
		if r.Kind != "CALLS" || r.ToID != toID {
			continue
		}
		if recv == "" {
			return true
		}
		if r.Properties.Get("receiver_type") == recv {
			return true
		}
	}
	return false
}

// FIXTURE A — `def c = new FooController(); c.index()` → CALLS FooController.index.
func TestIssue4749_GroovyLocalVarReceiver_DefNew(t *testing.T) {
	src := `class FooController { def index() { return "ok" } }
class FooControllerTest {
    def testIndex() {
        def c = new FooController()
        c.index()
    }
}`
	ents := runGroovy(t, src)
	caller := gFind(ents, "testIndex", "SCOPE.Operation")
	if caller == nil {
		t.Fatal("expected operation testIndex")
	}
	if !hasCall(caller, "FooController.index", "FooController") {
		t.Fatalf("A: expected CALLS testIndex→FooController.index (receiver_type=FooController); got %+v", caller.Relationships)
	}
	// The `new FooController()` constructor must NOT leak a bare `FooController`
	// CALLS edge.
	for _, r := range caller.Relationships {
		if r.Kind == "CALLS" && r.ToID == "FooController" {
			t.Fatalf("A: constructor call must not emit a bare FooController CALLS edge; got %+v", caller.Relationships)
		}
	}
	// And it must NOT leave a bare `index` leaf alongside the typed edge.
	for _, r := range caller.Relationships {
		if r.Kind == "CALLS" && r.ToID == "index" {
			t.Fatalf("A: typed receiver must replace the bare leaf `index`; got %+v", caller.Relationships)
		}
	}
}

// FIXTURE A' — explicit declared type `FooController c = new FooController()`.
func TestIssue4749_GroovyLocalVarReceiver_DeclaredType(t *testing.T) {
	src := `class FooController { def index() { return "ok" } }
class FooControllerTest {
    def testIndex() {
        FooController c = new FooController()
        c.index()
    }
}`
	ents := runGroovy(t, src)
	caller := gFind(ents, "testIndex", "SCOPE.Operation")
	if caller == nil {
		t.Fatal("expected operation testIndex")
	}
	if !hasCall(caller, "FooController.index", "FooController") {
		t.Fatalf("A': expected CALLS testIndex→FooController.index; got %+v", caller.Relationships)
	}
}

// NEGATIVE — a factory/builder receiver must NOT forge a class edge. `def c =
// MyFactory.create()` leaves the local untyped, so `c.index()` is a bare leaf
// `index` and no `<Class>.index` edge is emitted.
func TestIssue4749_GroovyLocalVarReceiver_NegativeFactory(t *testing.T) {
	src := `class FooControllerTest {
    def testBuilder() {
        def c = MyFactory.create()
        c.index()
    }
}`
	ents := runGroovy(t, src)
	caller := gFind(ents, "testBuilder", "SCOPE.Operation")
	if caller == nil {
		t.Fatal("expected operation testBuilder")
	}
	for _, r := range caller.Relationships {
		if r.Kind != "CALLS" {
			continue
		}
		if r.ToID == "FooController.index" || r.ToID == "MyFactory.index" {
			t.Fatalf("negative: factory receiver must not forge a typed index edge; got %+v", caller.Relationships)
		}
	}
	// The bare leaf `index` is the honest unresolved result.
	if !hasCall(caller, "index", "") {
		t.Fatalf("negative: expected bare CALLS testBuilder→index; got %+v", caller.Relationships)
	}
}
