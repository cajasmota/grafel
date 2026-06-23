// issue4682_localvar_receiver_test.go — Java local-variable receiver typing
// for test→CALLS coverage crediting (issue #4682, part of epic #4615/#4672).
//
// Generalises the TS/JS (#4680) and Python (#4716) local-receiver wins to Java.
// A JUnit unit test binds a local from a constructor and then calls a method on
// it; the call must resolve to the class method so ComputeCoverage credits the
// endpoint through test→CALLS→handler.
//
// Java already typed the EXPLICIT-DECLARED-TYPE form
// (`XController c = new XController(svc); c.getCounts()`) via
// collectLocalVarTypes + collectFieldTypes (so fixtures A and B below were
// already green from #120/#2062/#4359). The genuine RED this child closes is the
// modern `var` idiom: `var c = new XController(svc); c.getCounts()` — `var`
// carries no declared leaf type, so the local was left bare and `c.getCounts()`
// fell through to an unresolvable `var.getCounts` leaf. We now infer the type
// from a DIRECT `new ClassName(...)` initialiser (and only that shape), mirroring
// the #4680/#4716 conservatism: factory/builder/chain RHS receivers stay
// unresolved.
//
// Route-hit test-client linkage (MockMvc / WebTestClient / TestRestTemplate /
// REST Assured, fixture C in the epic) is already covered by the e2e_route_calls
// path — see internal/custom/java/issue4370_spring_e2e_route_tests_test.go and
// internal/engine/http_endpoint_e2e_testmap*.

package java_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/java"
	"github.com/cajasmota/grafel/internal/types"
)

func jcovExtract(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("java")
	if !ok {
		t.Fatal("java extractor not registered")
	}
	out, err := ext.Extract(context.Background(), extractor.FileInput{
		Path: path, Content: []byte(src), Language: "java",
		TSTree: parseForTest(t, src),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return out
}

func jcovCalls(t *testing.T, out []types.EntityRecord, name string) []types.RelationshipRecord {
	t.Helper()
	for i := range out {
		if out[i].Name == name {
			return out[i].Relationships
		}
	}
	var have []string
	for i := range out {
		have = append(have, out[i].Name)
	}
	t.Fatalf("no entity named %q; have: %v", name, have)
	return nil
}

func jcovHasCall(rels []types.RelationshipRecord, to string) bool {
	for _, r := range rels {
		if r.Kind == "CALLS" && r.ToID == to {
			return true
		}
	}
	return false
}

// FIXTURE A — explicit-declared-type local from a constructor.
// `XController c = new XController(svc); c.getCounts()` → CALLS XController.getCounts.
func TestIssue4682_LocalVarReceiver_DeclaredType(t *testing.T) {
	src := `package com.x;
class XController { public int getCounts() { return 1; } }
class XControllerTest {
  @Test
  void testGetCounts() {
    XController c = new XController(svc);
    c.getCounts();
  }
}
`
	out := jcovExtract(t, "com/x/XControllerTest.java", src)
	rels := jcovCalls(t, out, "XControllerTest.testGetCounts")
	if !jcovHasCall(rels, "XController.getCounts") {
		t.Fatalf("A: expected CALLS XController.getCounts; got %+v", rels)
	}
}

// FIXTURE A' — the `var` form is the genuine #4682 win. `var c = new XController(...)`
// must infer the local's type from the constructor so c.getCounts() resolves.
func TestIssue4682_LocalVarReceiver_VarNew(t *testing.T) {
	src := `package com.x;
class XController { public int getCounts() { return 1; } }
class XControllerTest {
  @Test
  void testGetCounts() {
    var c = new XController(svc);
    c.getCounts();
  }
}
`
	out := jcovExtract(t, "com/x/XControllerTest.java", src)
	rels := jcovCalls(t, out, "XControllerTest.testGetCounts")
	if !jcovHasCall(rels, "XController.getCounts") {
		t.Fatalf("A': expected CALLS XController.getCounts for `var` + new RHS; got %+v", rels)
	}
}

// FIXTURE B — Mockito @InjectMocks field injection. The field carries a declared
// type, so controller.getCounts() resolves via collectFieldTypes/cc.fields.
func TestIssue4682_InjectMocksField(t *testing.T) {
	src := `package com.x;
class XController { public int getCounts() { return 1; } }
class XControllerTest {
  @InjectMocks XController controller;
  @Test
  void testGetCounts() {
    controller.getCounts();
  }
}
`
	out := jcovExtract(t, "com/x/XControllerTest.java", src)
	rels := jcovCalls(t, out, "XControllerTest.testGetCounts")
	if !jcovHasCall(rels, "XController.getCounts") {
		t.Fatalf("B: expected CALLS XController.getCounts via @InjectMocks field; got %+v", rels)
	}
}

// NEGATIVE — a factory/builder receiver must NOT forge a class edge. A `var`
// local bound from `MyFactory.create()` (not a `new`) stays unresolved, so
// `c.getCounts()` is a bare leaf and no `XController.getCounts` (or any
// fabricated `<Class>.getCounts`) edge is emitted.
func TestIssue4682_NegativeFactoryReceiver(t *testing.T) {
	src := `package com.x;
class XControllerTest {
  @Test
  void testBuilder() {
    var c = MyFactory.create();
    c.getCounts();
  }
}
`
	out := jcovExtract(t, "com/x/XControllerTest.java", src)
	rels := jcovCalls(t, out, "XControllerTest.testBuilder")
	for _, r := range rels {
		if r.Kind == "CALLS" && r.ToID == "XController.getCounts" {
			t.Fatalf("negative: factory receiver must not forge XController.getCounts; got %+v", rels)
		}
		// And it must not type to the factory name either (MyFactory.getCounts).
		if r.Kind == "CALLS" && r.ToID == "MyFactory.getCounts" {
			t.Fatalf("negative: factory receiver must not type to MyFactory.getCounts; got %+v", rels)
		}
	}
}
