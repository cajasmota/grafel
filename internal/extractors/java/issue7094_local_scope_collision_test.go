// issue7094_local_scope_collision_test.go — Java local-variable receiver
// typing refuses ambiguous names instead of guessing one (issue #7094, the
// Java twin of C#'s #7072).
//
// collectLocalVarTypes builds name → type from a FLAT findAllNodes walk keyed
// by bare name, with no model of block scope. Two same-name locals in sibling
// blocks therefore both wrote the same key and one of them simply won, giving
// the loser's calls a receiver type that name never had at that site. Because
// the winner is a REAL same-file type the dotted edge BINDS, so no bind/orphan/
// dangle metric can see the error — fixtures are the only instrument, and they
// grade BOTH directions (a guard that never fires and one that always fires are
// indistinguishable on a corpus where the incidence is ~0).
//
// WHAT WAS MEASURED BEFORE THE FIX, for each fixture below:
//
//	sibling collision        o.b() on a `Customer o` emitted "Order.b"
//	untyped `var` sibling    o.b() on a factory-bound `var o` emitted "Order.b"
//	                         (in BOTH source orders)
//	leading-annotation decl   `@NonNull Customer o` emitted "Order.b"
//	enhanced-for arm         `for (Customer o : cs)` beside a local `Order o`
//	                         bound BOTH sites to Customer — "Customer.a"
//	third agreeing declarator Order/Customer/Order all emitted "Order.*"
//
// Every assertion is on the EMITTED EDGE (Relationships[].ToID for CALLS), not
// on the map's contents: a test that asserts the map lost an entry grades
// bookkeeping, not outcome.
//
// "ON COMPILABLE JAVA" HOLDS FOR EVERY FIXTURE HERE BUT ONE, and the exception
// is named rather than left to be found with a compiler (#7204): every unit in
// this file was compiled with javac 25.0.3 and all of them are accepted except
// TestJava7094_ParamsWinOverALocalOnDeliberatelyUncompilableSource, which is
// rejected ON PURPOSE and says so at the row, with the reason the input class
// it grades still matters.

package java_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

func j7094Calls(t *testing.T, src string) []types.RelationshipRecord {
	t.Helper()
	out := jcovExtract(t, "com/x/Svc.java", src)
	return jcovCalls(t, out, "Svc.run")
}

func j7094MustNotCall(t *testing.T, rels []types.RelationshipRecord, to ...string) {
	t.Helper()
	for _, bad := range to {
		if jcovHasCall(rels, bad) {
			t.Fatalf("must NOT emit CALLS %q (wrong dotted receiver binds and scores as a success); got %+v", bad, rels)
		}
	}
}

func j7094MustCall(t *testing.T, rels []types.RelationshipRecord, to ...string) {
	t.Helper()
	for _, want := range to {
		if !jcovHasCall(rels, want) {
			t.Fatalf("expected CALLS %q; got %+v", want, rels)
		}
	}
}

// Two locals named `o` in sibling blocks, different types. Neither site may
// carry a dotted receiver: the name is refused and both fall back to the bare
// leaf. This is the ALWAYS-FIRES direction's opposite number — see
// TestJava7094_SameNameSameTypeStillBinds for the other one.
func TestJava7094_SiblingCollisionRefusesBothReceivers(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
class Svc {
  void run() {
    { Order o = new Order(); o.a(); }
    { Customer o = new Customer(); o.b(); }
  }
}
`)
	// Before #7094 this emitted Order.b — a wrong receiver on a real type.
	j7094MustNotCall(t, rels, "Order.b", "Customer.a", "Order.a", "Customer.b")
	j7094MustCall(t, rels, "a", "b")
}

// Same name, SAME type is not a collision — the ledger compares types, not
// names, and refusing here would be pure recall loss. This is the
// always-fires direction: a guard that refuses every repeated name kills it.
func TestJava7094_SameNameSameTypeStillBinds(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} void b() {} }
class Svc {
  void run() {
    { Order o = new Order(); o.a(); }
    { Order o = new Order(); o.b(); }
  }
}
`)
	j7094MustCall(t, rels, "Order.a", "Order.b")
}

// HOLE 1 — an untyped `var` sibling must POISON the name, not cede it. A
// factory RHS defeats newExprClassName, so the declarator produces no type;
// before #7094 it skipped the ledger entirely and the typed sibling's `Order`
// was applied to a call site it never covered. Graded in BOTH source orders,
// since a first-writer-wins traversal makes the orders behave differently.
func TestJava7094_UntypedVarSiblingPoisonsTheName(t *testing.T) {
	typedFirst := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
class MyFactory { static Customer makeCustomer() { return new Customer(); } }
class Svc {
  void run() {
    { Order o = new Order(); o.a(); }
    { var o = MyFactory.makeCustomer(); o.b(); }
  }
}
`)
	j7094MustNotCall(t, typedFirst, "Order.b", "Order.a")
	j7094MustCall(t, typedFirst, "a", "b")

	untypedFirst := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
class MyFactory { static Customer makeCustomer() { return new Customer(); } }
class Svc {
  void run() {
    { var o = MyFactory.makeCustomer(); o.b(); }
    { Order o = new Order(); o.a(); }
  }
}
`)
	j7094MustNotCall(t, untypedFirst, "Order.b", "Order.a")
	j7094MustCall(t, untypedFirst, "a", "b")
}

// HOLE 2, AS FAR AS JAVA HAS ONE. C#'s second hole was a DECLARED type
// leafTypeName cannot reduce (a `ref` local), which skipped the ledger. NO JAVA
// INSTANCE OF THAT SHAPE WAS FOUND — leafTypeName's switch covers every
// _unannotated_type variant the grammar produces (type_identifier, void_type,
// integral_type, floating_point_type, boolean_type, generic_type, array_type,
// scoped_type_identifier), and a LEADING annotation is hoisted into the
// declaration's modifiers rather than producing an `annotated_type` type field.
// Measured: `@NonNull Customer o`, `final Customer o`, `Customer @NonNull [] o`,
// `List<@A Customer> o` and `@NonNull var o` all reduce to a non-empty leaf.
//
// So this fixture grades what it actually is — a leading-annotation declaration
// is an ORDINARY collision and must be refused like any other, not waved
// through — and NOT an empty-declType poisoning, which no fixture here reaches.
// collectLocalVarTypes still routes an empty declType into the ledger rather
// than skipping it; that arm is defensive and, as of this change, UNGRADED,
// because nothing was found that can reach it.
func TestJava7094_LeadingAnnotationIsAnOrdinaryCollision(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
@interface NonNull {}
class Order { void a() {} }
class Customer { void b() {} }
class Svc {
  Customer mk() { return new Customer(); }
  void run() {
    { Order o = new Order(); o.a(); }
    { @NonNull Customer o = mk(); o.b(); }
  }
}
`)
	j7094MustNotCall(t, rels, "Order.b", "Order.a", "Customer.b")
	j7094MustCall(t, rels, "a", "b")
}

// The enhanced-for arm is in the SAME ledger. It used to run after the
// declaration walk and write `out` unconditionally, so the loop variable
// overwrote a colliding local and BOTH sites bound to the loop's type —
// measured as "Customer.a" for the Order block's call.
func TestJava7094_EnhancedForArmParticipatesInTheLedger(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
class Svc {
  void run(java.util.List<Customer> cs) {
    { Order o = new Order(); o.a(); }
    for (Customer o : cs) { o.b(); }
  }
}
`)
	j7094MustNotCall(t, rels, "Customer.a", "Order.b", "Order.a", "Customer.b")
	j7094MustCall(t, rels, "a", "b")
}

// An uncontested enhanced-for loop variable must still bind — the ledger only
// refuses disagreement, it does not disable the arm.
func TestJava7094_EnhancedForAloneStillBinds(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Customer { void b() {} }
class Svc {
  void run(java.util.List<Customer> cs) {
    for (Customer o : cs) { o.b(); }
  }
}
`)
	j7094MustCall(t, rels, "Customer.b")
}

// STICKINESS — why the ledger is two-stage. A one-stage delete-on-collision
// against the published map forgets that the name ever disagreed, so a third
// declarator agreeing with the first finds no entry, sees no conflict, and
// RESURRECTS the wrong binding for the middle one. With the ambiguous set the
// refusal survives the agreeing redeclaration.
func TestJava7094_AmbiguityIsStickyAcrossAnAgreeingRedeclaration(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} void c() {} }
class Customer { void b() {} }
class Svc {
  void run() {
    { Order o = new Order(); o.a(); }
    { Customer o = new Customer(); o.b(); }
    { Order o = new Order(); o.c(); }
  }
}
`)
	j7094MustNotCall(t, rels, "Order.b", "Order.a", "Order.c", "Customer.b")
	j7094MustCall(t, rels, "a", "b", "c")
}

// RELABELLED, NOT REMOVED AND NOT REPAIRED: this row was
// `TestJava7094_ParamsWinOverALocalOfTheSameName`. The old name is written here
// so a grep for it lands on this note, and the new name carries the caveat the
// old one hid — THIS IS THE ONE FIXTURE IN THIS FILE WHOSE SOURCE DOES NOT
// COMPILE, DELIBERATELY, and that is stated at the row rather than left for the
// next reader to discover with a compiler (issue #7204).
//
// It pins the `params win over locals` line in javaOverlayLedger by making a
// parameter and a local of method `run` share the name `o`. Its own comment
// conceded the source did not compile and said the answer was "DERIVED FROM THE
// JLS §6.4 AND UNDERIVED BY EXECUTION — no javac in this environment". There IS
// a javac in this environment (25.0.3), and it rejects that fixture:
//
//	com/x/Svc.java:7: error: variable o is already defined in method run(Order)
//
// The conclusion the old comment did not draw is that the shape is not merely
// uncompiled but UNREACHABLE, so the row grades the merge on input the
// extractor can only meet in broken source. That is not a reason to delete the
// row, but it IS a reason it may not claim to be JLS-derived evidence about
// legal Java, which is what the old comment did and what #7204 was filed for.
//
// WHY UNREACHABLE, ENUMERATED RATHER THAN ARGUED. javaOverlayLedger's `top` is
// the enclosing member's formal parameters and its `mid` is
// collectLocalVarTypes over THAT member's scope root. For the ordering between
// them to decide anything, one name must be in both. Every binder family that
// feeds `mid` was compiled against a method parameter of the same name with
// javac 25.0.3, and every one is rejected with "variable o is already defined
// in method run(...)":
//
//	nested block local, flat-sibling local, enhanced-for variable,
//	try-with-resources resource, catch parameter, instanceof type pattern,
//	switch case pattern, lambda parameter, lambda-body local, for-init variable
//
// The only shadowings javac ACCEPTS put the inner binder inside a class body —
// an anonymous-class method parameter, or a local class's own local. Since
// #7109 collectLocalVarTypes stops at exactly that boundary, neither reaches
// `mid`: each becomes its own scope whose ledger takes the outer one as `base`
// and its own parameters as `top`. So `mid` ∩ `top` = ∅ in every compilable
// Java program, and inverting the two loops is an EQUIVALENT mutant on
// compilable Java. Verified by execution, not just derived: inverting the two
// loops fails THIS ROW and nothing else in the package, and with this row
// muted the inverted form is green everywhere.
//
// SO WHY KEEP IT, given the file's header promises compilable Java. Because
// "unreachable in legal Java" is not "unreachable". grafel indexes partial,
// mid-edit and generated sources, and on those `mid` ∩ `top` IS non-empty —
// that is precisely the population in which a parameter and a local can share
// a name. There the ordering still decides which layer types the receiver, and
// the wrong layer yields a dotted target on a REAL same-file type: it binds,
// and bind/orphan/dangle all score it as a success (#7056), the same signature
// this whole file exists to catch. Delete the row and inverting the loops is
// silently green across the entire suite; keep it and the decision is pinned.
//
// WHAT THE ROW MAY AND MAY NOT BE READ AS. It pins a CHOICE, not a language
// rule: on input javac rejects, the JLS answers nothing, so there is no
// "correct" layer to prefer. The choice is justified rather than arbitrary —
// the local is the declaration javac rejects, so the author's repair will
// rename or remove IT, leaving the parameter's binding the one the eventual
// legal program keeps — but that is a bet about repairs, not a derivation.
// Whoever changes the ordering deliberately should update this row, not treat
// its red as proof of a bug.
//
// WHAT ELSE GRADES THE PARAMS LAYER, so this row is not its only guard. The
// layer's PRESENCE — dropping it entirely, e.g. by weakening
// javaOverlayLedger's shortcut to `if len(mid) == 0` so a params-only ledger
// returns `base` — is killed by TestJava7096_QualifiedParamTypeReceiverBindsToLeaf
// and TestJava_CallsParameterReceiverDottedTarget, both of which compile. Its
// precedence over the INHERITED layer, which is the only precedence a LEGAL
// program can exercise, is killed by TestJava7109_AnonClassParamOwnsItsName and
// its siblings. This row is the only grader of the mid-vs-top ORDERING, and it
// can only be that on non-compilable input.
//
// This row was NOT rewritten into a legal shape because every legal shape it
// could take is already a fixture: an anonymous-class or local-class parameter
// shadowing an outer binder is TestJava7109_AnonClassParamOwnsItsName /
// TestJava7109_LocalClassParamOwnsItsName. A duplicate would read as coverage
// without adding any, and would NOT grade the ordering — a nested scope's
// parameters arrive as `top` over an INHERITED `base`, never over `mid`.
//
// grafel:fixture-does-not-compile — machine-readable opt-out for the
// compile-every-Java-fixture guard #7204 prices, whose requirement 3 is exactly
// such a marker. PROPOSED HERE, NOT YET A CONVENTION: nothing reads it today,
// and if the guard lands with a different spelling this line moves.
func TestJava7094_ParamsWinOverALocalOnDeliberatelyUncompilableSource(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} void b() {} }
class Customer { void b() {} }
class Svc {
  void run(Order o) {
    o.a();
    { Customer o = new Customer(); o.b(); }
  }
}
`)
	j7094MustCall(t, rels, "Order.a", "Order.b")
	j7094MustNotCall(t, rels, "Customer.b")
}

// NEVER-FIRES control — an ordinary single declaration, and a nested block
// binding a DIFFERENT name, must be untouched by the refusal.
func TestJava7094_UncontestedDeclarationsUnaffected(t *testing.T) {
	single := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Svc {
  void run() {
    Order o = new Order();
    o.a();
  }
}
`)
	j7094MustCall(t, single, "Order.a")

	distinct := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
class Svc {
  void run(boolean x) {
    Order o = new Order();
    o.a();
    if (x) { Customer c = new Customer(); c.b(); }
  }
}
`)
	j7094MustCall(t, distinct, "Order.a", "Customer.b")
}

// A multi-declarator declaration binds every name to the declared type, and
// that is unchanged by the ledger.
func TestJava7094_MultiDeclaratorUnaffected(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} void b() {} }
class Svc {
  void run() {
    Order p = new Order(), q = new Order();
    p.a();
    q.b();
  }
}
`)
	j7094MustCall(t, rels, "Order.a", "Order.b")
}

// THE COST OF REFUSING, RECORDED AS A FIXTURE RATHER THAN AS PROSE — AND THEN
// RECOVERED. This test was `TestJava7094_ClassBodyShadowingCostsRecall`; it is
// renamed rather than deleted, and the old name is written here so a grep for
// it lands on this note.
//
// An earlier revision of collectLocalVarTypes' comment claimed Java forbids an
// inner block from redeclaring a name already in scope, so "a compilable
// program cannot contain that case". FALSE: JLS §6.4 restricts redeclaration
// only within the DIRECTLY ENCLOSING method, constructor or initializer block.
// A local or anonymous CLASS BODY is a new class scope and may legally shadow.
// Calls inside such a body are attributed to the ENCLOSING METHOD entity, so
// the flat walk reached across the class boundary and poisoned the outer name.
//
// WHAT THIS TEST USED TO ASSERT, and why the change is not a regression: it
// asserted that `Order.a` and `Customer.b` were BOTH lost — a recall cost paid
// so a wrong receiver could not be guessed. Its own comment named the fix and
// predicted this moment verbatim:
//
//	"It exists so the cost is OBSERVED and moves when the behaviour moves: a
//	 real block-scoped symbol table would stop at the class boundary and
//	 `Order.a` would come back, at which point this test fails and is updated
//	 to assert the recovery."
//
// #7109 built that boundary (for class scopes — see
// issue7109_class_scope_test.go). So this row is REWRITTEN to assert the
// recovery it anticipated, not removed: the refusal was never the goal, the
// absence of a scope model was the reason for it. Both receivers come back,
// because the inner declaration now lives in the inner class's own ledger and
// no longer disagrees with the outer one.
//
// The recovery is BROADER than the prediction — `Customer.b` returns as well
// as `Order.a`, because the inner scope gets a real ledger rather than just
// being excluded from the outer one. That is direction 2 as #7109 specified it
// ("that body gets its own ledger seeded from its own parameters and locals").
func TestJava7094_ClassBodyShadowingRecallRecoveredBy7109(t *testing.T) {
	// Anonymous class body shadowing the enclosing method's local.
	anon := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
class Svc {
  void run() {
    Order o = new Order();
    o.a();
    Runnable r = new Runnable() {
      public void run() { Customer o = new Customer(); o.b(); }
    };
  }
}
`)
	// WAS: j7094MustCall(t, anon, "a", "b") plus a MustNotCall on all four
	// dotted forms — i.e. both receivers lost.
	j7094MustCall(t, anon, "Order.a", "Customer.b")
	// The CROSSED pairings stay forbidden: shadowing must not swap the types,
	// only scope them.
	j7094MustNotCall(t, anon, "Order.b", "Customer.a")

	// Local (named) class body — same shape, same recovery.
	local := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
class Svc {
  void run() {
    Order o = new Order();
    o.a();
    class Inner { void go() { Customer o = new Customer(); o.b(); } }
  }
}
`)
	j7094MustCall(t, local, "Order.a", "Customer.b")
	j7094MustNotCall(t, local, "Order.b", "Customer.a")

	// CONTROL — rename the inner variable. This bound both receivers BEFORE
	// #7109 too, which is what made it the proof that the loss above was
	// caused by cross-class-boundary poisoning and not by the fixture. It is
	// kept because it is now the row that shows the boundary did not COST
	// anything either: the no-collision case is unchanged.
	control := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
class Svc {
  void run() {
    Order o = new Order();
    o.a();
    Runnable r = new Runnable() {
      public void run() { Customer c = new Customer(); c.b(); }
    };
  }
}
`)
	j7094MustCall(t, control, "Order.a", "Customer.b")
}
