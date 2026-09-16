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
class Order { void a() {} }
class Svc {
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

// PINS `params win over locals` at extractCallRelationships' merge. The
// precedence line was previously unexercised by any fixture — this one makes a
// parameter and a local genuinely share a name, so inverting the merge fails.
// (Java forbids a local shadowing a parameter, so this is not a compilable
// program; tree-sitter parses it and the merge still has to answer, so the
// answer is pinned where the code claims it. DERIVED FROM THE JLS §6.4 AND
// UNDERIVED BY EXECUTION — no javac in this environment.)
func TestJava7094_ParamsWinOverALocalOfTheSameName(t *testing.T) {
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
