// issue7096_qualified_type_leaf_test.go — a fully-qualified Java type reduces
// to its LEAF, not to its package root (issue #7096).
//
// THREE sites took `ids[len(ids)-1]` from a findAllNodes result while calling
// it "the rightmost type_identifier". findAllNodes is a stack DFS that pushes
// children in index order and pops from the end, so its result is in REVERSE
// source order and len-1 is the LEFTMOST segment. `com.x.XController` reduced
// to "com", and a qualified receiver was then named after its package root:
//
//	com.x.XController c = mk(); c.getCounts();  → CALLS "com.getCounts"
//	var c = new com.x.XController(svc);         → CALLS "com.com"
//	                              c.getCounts() → CALLS "com.getCounts"
//
// That is the #7056 signature — a plausible dotted target that leaves the
// resolver's bare-name class and scores as a CONFIDENT bind, so bind rate,
// orphan rate and dangle count all read it as a success. Fixtures are the only
// instrument.
//
// THE THREE SITES ARE GRADED SEPARATELY, because they are three different
// entry points into the same reduction and a fixture that only exercises one
// of them grades only that one:
//
//	javaCallTarget / object_creation_expression → the constructor edge
//	newExprClassName                            → a `var` local's type
//	leafTypeName / scoped_type_identifier       → a DECLARED local's type
//
// BOTH DIRECTIONS ARE GRADED: a qualified type must reduce to its leaf, and an
// unqualified type must be untouched (a reduction that always fires and one
// that never fires are indistinguishable on the qualified cases alone).
//
// Every assertion is on the EMITTED EDGE (Relationships[].ToID for CALLS).

package java_test

import "testing"

// SITE 3 — leafTypeName's scoped_type_identifier arm, reached by a DECLARED
// local type. This site carried the comment "`com.foo.Bar` — leaf is the
// rightmost type_identifier" while returning `com`.
func TestJava7096_DeclaredQualifiedLocalTypeBindsToLeaf(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class XController { void getCounts() {} }
class Svc {
  void run() {
    com.x.XController c = mk();
    c.getCounts();
  }
}
`)
	j7094MustNotCall(t, rels, "com.getCounts", "x.getCounts")
	j7094MustCall(t, rels, "XController.getCounts")
}

// SITE 2 — newExprClassName, reached by a `var` local bound to a qualified
// construction. Distinct from site 3: the declared type is absent, so the type
// comes from the initialiser.
func TestJava7096_VarLocalFromQualifiedNewBindsToLeaf(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class XController { void getCounts() {} }
class Svc {
  void run() {
    var c = new com.x.XController(svc);
    c.getCounts();
  }
}
`)
	j7094MustNotCall(t, rels, "com.getCounts", "x.getCounts")
	j7094MustCall(t, rels, "XController.getCounts")
}

// SITE 1 — javaCallTarget's object_creation_expression arm: the CONSTRUCTOR
// edge itself, independent of any local. `new com.x.XController(...)` emitted
// "com.com", which is neither a class nor a constructor this file declares.
func TestJava7096_QualifiedConstructorEdgeBindsToLeaf(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class XController { XController(Object s) {} }
class Svc {
  void run() {
    new com.x.XController(svc);
  }
}
`)
	j7094MustNotCall(t, rels, "com.com", "x.x")
	j7094MustCall(t, rels, "XController.XController")
}

// THE ALWAYS-FIRES DIRECTION, per site. An UNQUALIFIED type must be unaffected
// by the change: a reduction that returns the wrong child, or that strips a
// bare name, is killed here rather than by the qualified cases.
func TestJava7096_UnqualifiedTypesAreUnaffected(t *testing.T) {
	declared := j7094Calls(t, `package com.x;
class XController { void getCounts() {} }
class Svc {
  void run() {
    XController c = mk();
    c.getCounts();
  }
}
`)
	j7094MustCall(t, declared, "XController.getCounts")

	varLocal := j7094Calls(t, `package com.x;
class XController { void getCounts() {} XController(Object s) {} }
class Svc {
  void run() {
    var c = new XController(svc);
    c.getCounts();
  }
}
`)
	j7094MustCall(t, varLocal, "XController.getCounts", "XController.XController")
}

// A qualified GENERIC type still reduces to the generic's own leaf, not to its
// type argument and not to the package root: `java.util.List<XController>` is
// a List. Pins that the generic arm still descends its FIRST named child after
// the scoped arm changed underneath it.
func TestJava7096_QualifiedGenericReducesToItsOwnLeaf(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Svc {
  void run() {
    java.util.List<XController> l = mk();
    l.size();
  }
}
`)
	j7094MustNotCall(t, rels, "java.size", "util.size", "XController.size")
	j7094MustCall(t, rels, "List.size")
}

// A NESTED type (`Outer.Inner`) is the worst spelling of this bug: BOTH
// segments are plausible same-file names, so the old reduction produced
// "Outer.ping" — a real type, a real-looking edge, and the wrong one.
func TestJava7096_NestedTypeBindsToInnerNotOuter(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Outer { static class Inner { void ping() {} } void ping() {} }
class Svc {
  void run() {
    Outer.Inner i = mk();
    i.ping();
  }
}
`)
	j7094MustNotCall(t, rels, "Outer.ping")
	j7094MustCall(t, rels, "Inner.ping")
}

// The leaf is read as the scoped node's LAST NAMED CHILD, which the grammar
// (`seq(qualifier, '.', repeat(annotation), type_identifier)`) makes correct
// even when the trailing segment carries a type annotation: the annotations
// sit BEFORE the identifier, so the identifier is still last. A reduction that
// took a FIXED index instead — `NamedChild(1)`, which is the identifier for
// `com.x.Bar` and the ANNOTATION the moment one appears — is killed here, and
// only here.
func TestJava7096_AnnotatedQualifiedSegmentStillBindsToLeaf(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class XController { void getCounts() {} }
class Svc {
  void run() {
    com.x.@NonNull XController c = mk();
    c.getCounts();
  }
}
`)
	j7094MustNotCall(t, rels, "com.getCounts", "x.getCounts", "NonNull.getCounts")
	j7094MustCall(t, rels, "XController.getCounts")
}

// THE #7095 INTERACTION, and the direct regression pin this bug made
// unwritable. #7095 refuses two same-name locals whose TYPES disagree. One
// spelling qualified and one bare is the SAME type, but the qualified one
// reduced to "com", so the ledger saw `XController` vs `com`, called it a
// collision and refused BOTH receivers. Fixing the reduction makes both
// spellings agree, so both calls bind — a FALSE refusal disappearing.
func TestJava7096_SameTypeTwoSpellingsIsNotACollision(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class XController { void a() {} void b() {} }
class Svc {
  void run() {
    { XController c = mk(); c.a(); }
    { com.x.XController c = mk(); c.b(); }
  }
}
`)
	j7094MustCall(t, rels, "XController.a", "XController.b")
}

// The refusal itself must survive: two DIFFERENT types, one of them spelled
// qualified, is still a real collision and both receivers stay refused. Pins
// that the leaf reduction did not turn #7095's ledger into a no-op.
func TestJava7096_QualifiedSpellingOfADifferentTypeStillCollides(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
class Svc {
  void run() {
    { Order o = mk(); o.a(); }
    { com.x.Customer o = mk(); o.b(); }
  }
}
`)
	j7094MustNotCall(t, rels, "Order.b", "Customer.a", "Order.a", "Customer.b")
	j7094MustCall(t, rels, "a", "b")
}

// ---------------------------------------------------------------------------
// leafTypeName's OTHER CALLERS (round 2).
//
// leafTypeName has EIGHT non-recursive callers. Sites 1-3 above grade three of
// them; the remaining five reach the SAME scoped_type_identifier arm from
// field types, parameter types, the enhanced-for arm, javaSuperclassNames,
// javaSuperInterfaceNames and javaInjectFieldTypes, and every one of them
// emitted the package root for a qualified type:
//
//	extends com.x.Base          EXTENDS    -> "com"
//	implements com.x.Iface      IMPLEMENTS -> "com"
//	@Inject com.x.Repo repo     REFERENCES -> "com"
//	com.x.Repo repo; repo.l()   CALLS      -> "com.load"
//	void run(com.x.Param p)     CALLS      -> "com.ping"
//	for (com.x.Item i : is)     CALLS      -> "com.use"
//
// Measured: reverting javaSuperclassNames ALONE to the old
// findAllNodes(...)[len-1] passed the ENTIRE Java suite with zero --- FAIL
// lines while emitting `EXTENDS -> com`. So the #7096 defect could return on
// any of these five surfaces under a green suite. That is the same argument
// that makes sites 1/2/3 worth grading separately, applied to the callers.
//
// EACH SURFACE IS ITS OWN TEST on its own fixture: six assertions in one test
// fail together, and assertions that only fail together grade none of them.

// j7096ClassRels returns "KIND:ToID" tokens for the SCOPE.Component entity
// named `class`, which is where EXTENDS / IMPLEMENTS / REFERENCES land.
func j7096ClassRels(t *testing.T, class, src string) []string {
	t.Helper()
	out := jcovExtract(t, "com/x/"+class+".java", src)
	for _, ent := range out {
		if ent.Name == class && ent.Kind == "SCOPE.Component" {
			var rels []string
			for _, r := range ent.Relationships {
				rels = append(rels, r.Kind+":"+r.ToID)
			}
			return rels
		}
	}
	t.Fatalf("no SCOPE.Component entity named %q", class)
	return nil
}

func j7096MustRel(t *testing.T, rels []string, want string) {
	t.Helper()
	for _, r := range rels {
		if r == want {
			return
		}
	}
	t.Fatalf("expected edge %q; got %v", want, rels)
}

func j7096MustNotRel(t *testing.T, rels []string, bad ...string) {
	t.Helper()
	for _, b := range bad {
		for _, r := range rels {
			if r == b {
				t.Fatalf("must NOT emit %q (package root as a type); got %v", b, rels)
			}
		}
	}
}

// SURFACE 4 — javaSuperclassNames.
func TestJava7096_QualifiedSuperclassBindsToLeaf(t *testing.T) {
	rels := j7096ClassRels(t, "Svc", `package com.x;
class Svc extends com.x.Base {}
`)
	j7096MustNotRel(t, rels, "EXTENDS:com", "EXTENDS:x")
	j7096MustRel(t, rels, "EXTENDS:Base")
}

// SURFACE 5 — javaSuperInterfaceNames.
func TestJava7096_QualifiedInterfaceBindsToLeaf(t *testing.T) {
	rels := j7096ClassRels(t, "Svc", `package com.x;
class Svc implements com.x.Iface {}
`)
	j7096MustNotRel(t, rels, "IMPLEMENTS:com", "IMPLEMENTS:x")
	j7096MustRel(t, rels, "IMPLEMENTS:Iface")
}

// SURFACE 6 — javaInjectFieldTypes.
func TestJava7096_QualifiedInjectedFieldBindsToLeaf(t *testing.T) {
	rels := j7096ClassRels(t, "Svc", `package com.x;
class Svc {
  @Inject com.x.Repo repo;
}
`)
	j7096MustNotRel(t, rels, "REFERENCES:com", "REFERENCES:x")
	j7096MustRel(t, rels, "REFERENCES:Repo")
}

// SURFACE 7 — collectFieldTypes, reached through a FIELD receiver.
func TestJava7096_QualifiedFieldTypeReceiverBindsToLeaf(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Svc {
  com.x.Repo repo;
  void run() { repo.load(); }
}
`)
	j7094MustNotCall(t, rels, "com.load", "x.load")
	j7094MustCall(t, rels, "Repo.load")
}

// SURFACE 8 — collectParamTypes, reached through a PARAMETER receiver.
func TestJava7096_QualifiedParamTypeReceiverBindsToLeaf(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Svc {
  void run(com.x.Param p) { p.ping(); }
}
`)
	j7094MustNotCall(t, rels, "com.ping", "x.ping")
	j7094MustCall(t, rels, "Param.ping")
}

// SURFACE 9 — the enhanced-for arm of collectLocalVarTypes.
func TestJava7096_QualifiedEnhancedForTypeBindsToLeaf(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Svc {
  void run(java.util.List<com.x.Item> items) {
    for (com.x.Item i : items) { i.use(); }
  }
}
`)
	j7094MustNotCall(t, rels, "com.use", "x.use")
	j7094MustCall(t, rels, "Item.use")
}
