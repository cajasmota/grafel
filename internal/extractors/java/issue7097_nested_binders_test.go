// issue7097_nested_binders_test.go — three more Java constructs bind a name in
// a nested scope and must participate in collectLocalVarTypes' ambiguity
// ledger (issue #7097, following #7094/#7095).
//
// The ledger introduced by #7094 was fed by exactly two binder forms,
// `local_variable_declaration` and `enhanced_for_statement`. A try-with-
// resources resource, a catch-clause parameter and a lambda parameter each
// bind a name too, and none of them was in the ledger — so each LOST the name
// to a same-name sibling local and the sibling's type was applied to a call
// site it never covered.
//
// MEASURED ON THIS TREE BEFORE THE CHANGE, on compilable Java (each `Order o`
// lives in its own block, so there is no JLS §6.4 conflict with the nested
// binder):
//
//	try-with-resources  `try (Customer o = new Customer()) { o.b(); }`
//	                    beside `{ Order o = …; o.a(); }`  →  "Order.b"
//	                    IN BOTH SOURCE ORDERS
//	catch parameter     `catch (MyEx o) { o.b(); }`       →  "Order.b"
//	lambda parameter    `cs.forEach(o -> o.b())`          →  "Order.b"
//
// `Order.b` is a WRONG receiver on a real same-file type: it binds, and every
// bind/orphan/dangle metric scores it as a success (#7056). Fixtures are the
// only instrument — corpus incidence for this family measured 0 — so both
// directions are graded, per construct, and every assertion is on the EMITTED
// EDGE (Relationships[].ToID for CALLS) rather than on the ledger's contents.
//
// ALSO MEASURED BEFORE THE CHANGE, and deliberately UNCHANGED after it: each
// construct ALONE emits the bare leaf `b`. None of them ever typed itself;
// they could only ever lose to a sibling. The *AloneUnchanged fixtures pin
// that, so a later change that starts typing these binders has to move a
// fixture rather than slip through.

package java_test

import "testing"

// ---------------------------------------------------------------------------
// try-with-resources
// ---------------------------------------------------------------------------

// COLLIDING — the resource must poison the name, in BOTH source orders (a
// first-writer-wins traversal makes the two orders behave differently, which
// is how #7094's `var` hole was found).
func TestJava7097_TryWithResourcesSiblingCollisionRefuses(t *testing.T) {
	localFirst := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
class Svc {
  void run() {
    { Order o = new Order(); o.a(); }
    try (Customer o = new Customer()) { o.b(); }
  }
}
`)
	j7094MustNotCall(t, localFirst, "Order.b", "Order.a", "Customer.a")
	j7094MustCall(t, localFirst, "a", "b")

	resourceFirst := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
class Svc {
  void run() {
    try (Customer o = new Customer()) { o.b(); }
    { Order o = new Order(); o.a(); }
  }
}
`)
	j7094MustNotCall(t, resourceFirst, "Order.b", "Order.a", "Customer.a")
	j7094MustCall(t, resourceFirst, "a", "b")
}

// STANDALONE CONTROL — unchanged. A resource has a declared type and COULD be
// typed, but this change does not type it: `o.b()` still emits the bare leaf.
func TestJava7097_TryWithResourcesAloneUnchanged(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Customer { void b() {} }
class Svc {
  void run() {
    try (Customer o = new Customer()) { o.b(); }
  }
}
`)
	j7094MustCall(t, rels, "b")
	j7094MustNotCall(t, rels, "Customer.b")
}

// ALWAYS-FIRES CONTROL — an EXISTING-VARIABLE resource (`try (o) { … }`,
// Java 9+) binds nothing: it names a variable declared elsewhere. The resource
// arm must not poison it, or the local's own type is lost. The grammar's
// `resource` node has no `name` field in this shape, which is what makes the
// arm a no-op here; a matcher that took the resource's text instead would
// refuse this and this fixture would go red.
func TestJava7097_ExistingVariableResourceStillBinds(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order implements AutoCloseable { void a() {} public void close() {} }
class Svc {
  void run() {
    Order o = new Order();
    try (o) { o.a(); }
  }
}
`)
	j7094MustCall(t, rels, "Order.a")
}

// THE PRICE OF NOT TYPING THE RESOURCE, observed rather than asserted in
// prose. A same-type sibling (`Order o` local, `Order o` resource) used to
// bind BOTH sites — the local's type covered the resource's call by accident,
// and it happened to be right. Poisoning with "" costs that: both sites now
// fall back to the bare leaf. This is the honest direction (the ledger refuses
// rather than guesses), and it is the fixture that must MOVE if a later change
// gives the resource its declared type — at which point the expectation here
// becomes "Order.a" and "Order.c".
func TestJava7097_TryWithResourcesSameTypeCost(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order implements AutoCloseable { void a() {} void c() {} public void close() {} }
class Svc {
  void run() {
    { Order o = new Order(); o.a(); }
    try (Order o = new Order()) { o.c(); }
  }
}
`)
	j7094MustCall(t, rels, "a", "c")
	j7094MustNotCall(t, rels, "Order.a", "Order.c")
}

// ---------------------------------------------------------------------------
// catch-clause parameter
// ---------------------------------------------------------------------------

// COLLIDING — the catch parameter must poison the name.
func TestJava7097_CatchParameterSiblingCollisionRefuses(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} }
class MyEx extends Exception { void b() {} }
class Svc {
  void run() {
    { Order o = new Order(); o.a(); }
    try { mk(); } catch (MyEx o) { o.b(); }
  }
}
`)
	j7094MustNotCall(t, rels, "Order.b", "Order.a", "MyEx.a")
	j7094MustCall(t, rels, "a", "b")
}

// MULTI-CATCH — `catch (A | B o)` binds one name with NO single type, which is
// the reason the arm records "" rather than a leaf: there is no leftmost arm
// to prefer. It must refuse the sibling's type just like the single-type form.
func TestJava7097_MultiCatchParameterAlsoRefuses(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} }
class AEx extends Exception { void b() {} }
class BEx extends Exception { void b() {} }
class Svc {
  void run() {
    { Order o = new Order(); o.a(); }
    try { mk(); } catch (AEx | BEx o) { o.b(); }
  }
}
`)
	j7094MustNotCall(t, rels, "Order.b", "Order.a", "AEx.b", "BEx.b")
	j7094MustCall(t, rels, "a", "b")
}

// THE PRICE OF NOT TYPING THE CATCH PARAMETER — the same cost the
// try-with-resources fixture records, measured in a second place. A SAME-type
// sibling (`MyEx o` local, `MyEx o` catch parameter) used to bind BOTH sites:
// the local's type covered the catch body's call by accident, and it happened
// to be right. Poisoning with "" costs that.
//
// MEASURED ON THIS TREE, this exact source, with only java.go swapped between
// d1552ac26 and its parent:
//
//	pre-change   MyEx.a, MyEx.c
//	post-change  bare a, bare c
//
// This is the honest direction — the ledger REFUSES rather than guesses, and
// for catch the guess is not even available (`catch_type` is a union, see the
// arm's comment in java.go). It is recorded here rather than argued in prose
// because a cost nothing observes is a cost nobody can see change: this is the
// fixture that must MOVE if a later change gives a catch parameter a type, at
// which point the expectation becomes "MyEx.a" and "MyEx.c".
func TestJava7097_CatchParameterSameTypeCost(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class MyEx extends Exception { void a() {} void c() {} }
class Svc {
  void run() {
    { MyEx o = new MyEx(); o.a(); }
    try { mk(); } catch (MyEx o) { o.c(); }
  }
}
`)
	j7094MustCall(t, rels, "a", "c")
	j7094MustNotCall(t, rels, "MyEx.a", "MyEx.c")
}

// STANDALONE CONTROL — unchanged: a catch parameter alone emits the bare leaf.
func TestJava7097_CatchParameterAloneUnchanged(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class MyEx extends Exception { void b() {} }
class Svc {
  void run() {
    try { mk(); } catch (MyEx o) { o.b(); }
  }
}
`)
	j7094MustCall(t, rels, "b")
	j7094MustNotCall(t, rels, "MyEx.b")
}

// ALWAYS-FIRES CONTROL — a catch parameter named `e` must poison `e` and
// NOTHING ELSE. The outer `Order o` is CAPTURED INSIDE the catch body on
// purpose: that is what separates "poison the bound name" from "poison every
// identifier under the clause". A control that called `o.a()` outside the
// clause cannot see the difference — it was written that way first, and an
// over-broad arm that walks every `identifier` under `catch_clause` survived
// it. With the capture, that arm loses `Order.a` and this fixture goes red.
func TestJava7097_CatchParameterDoesNotPoisonOtherNames(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} }
class MyEx extends Exception {}
class Svc {
  void run() {
    Order o = new Order();
    try { mk(); } catch (MyEx e) { o.a(); }
  }
}
`)
	j7094MustCall(t, rels, "Order.a")
}

// ---------------------------------------------------------------------------
// lambda parameter
// ---------------------------------------------------------------------------

// COLLIDING — the single inferred parameter (`o -> …`, the dominant shape,
// where the grammar's `parameters` field is a bare `identifier`).
func TestJava7097_LambdaParameterSiblingCollisionRefuses(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
class Svc {
  void run(java.util.List<Customer> cs) {
    { Order o = new Order(); o.a(); }
    cs.forEach(o -> o.b());
  }
}
`)
	j7094MustNotCall(t, rels, "Order.b", "Order.a", "Customer.a")
	j7094MustCall(t, rels, "a", "b")
}

// COLLIDING — `inferred_parameters` (`(o, p) -> …`), a DIFFERENT grammar node
// from the bare-identifier shape above. Held constant: the sibling, the types,
// the call. Varied: only the parameter-list shape, so a verdict here can only
// be explained by that node.
func TestJava7097_LambdaInferredParameterListRefuses(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
class Svc {
  void run(java.util.Map<String, Customer> cs) {
    { Order o = new Order(); o.a(); }
    cs.forEach((k, o) -> o.b());
  }
}
`)
	j7094MustNotCall(t, rels, "Order.b", "Order.a", "Customer.a")
	j7094MustCall(t, rels, "a", "b")
}

// COLLIDING — `formal_parameters` (`(Customer o) -> …`), the third and only
// TYPED lambda shape, and again a distinct grammar node. It is recorded as ""
// like the other two: this change removes a wrong bind, it does not add
// typing.
func TestJava7097_LambdaTypedParameterListRefuses(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
class Svc {
  void run(java.util.List<Customer> cs) {
    { Order o = new Order(); o.a(); }
    cs.forEach((Customer o) -> o.b());
  }
}
`)
	j7094MustNotCall(t, rels, "Order.b", "Order.a", "Customer.a")
	j7094MustCall(t, rels, "a", "b")
}

// STANDALONE CONTROL — unchanged: a lambda parameter alone emits the bare leaf.
func TestJava7097_LambdaParameterAloneUnchanged(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Customer { void b() {} }
class Svc {
  void run(java.util.List<Customer> cs) {
    cs.forEach(o -> o.b());
  }
}
`)
	j7094MustCall(t, rels, "b")
	j7094MustNotCall(t, rels, "Customer.b")
}

// THE PRICE OF NOT TYPING THE LAMBDA PARAMETER, inferred shape — a same-type
// sibling (`Customer o` local, `o -> …` lambda parameter) used to bind BOTH
// sites by accident.
//
// MEASURED ON THIS TREE, this exact source, with only java.go swapped between
// d1552ac26 and its parent:
//
//	pre-change   Customer.a, Customer.c
//	post-change  bare a, bare c
//
// Honest direction again: for THIS shape the grammar carries no type at all,
// so "" is the only entry there is and the loss is unavoidable rather than
// chosen. The fixture exists so the loss is observed; it moves only if a
// change starts INFERRING a lambda parameter's type from the functional
// interface, which is a different (and much larger) project than reading a
// declared one.
func TestJava7097_LambdaInferredParameterSameTypeCost(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Customer { void a() {} void c() {} }
class Svc {
  void run(java.util.List<Customer> cs) {
    { Customer o = new Customer(); o.a(); }
    cs.forEach(o -> o.c());
  }
}
`)
	j7094MustCall(t, rels, "a", "c")
	j7094MustNotCall(t, rels, "Customer.a", "Customer.c")
}

// THE PRICE OF NOT TYPING THE LAMBDA PARAMETER, TYPED shape — kept as its own
// fixture rather than folded into the inferred one above, for the same reason
// the three collision fixtures are separate: `formal_parameters` is a DIFFERENT
// grammar node handled by a DIFFERENT arm of the switch, so a verdict on one
// says nothing about the other. It also has a different MOVE CONDITION, which
// is the whole job of these cost fixtures: here the type `Customer` is
// literally present in the source, so this row can move the moment someone
// reads `formal_parameter`'s `type` field — the inferred row above cannot move
// without whole-program functional-interface inference. One fixture covering
// both would go red for two unrelated reasons and could not say which.
//
// MEASURED ON THIS TREE, this exact source, with only java.go swapped between
// d1552ac26 and its parent:
//
//	pre-change   Customer.a, Customer.c
//	post-change  bare a, bare c
//
// This is the least defensible of the three costs and it is recorded as such:
// the ledger refuses a type it could have read. That is deliberate — typing
// the binder is a recall ADDITION with its own grading obligation, not part of
// removing a wrong bind — and THIS fixture is what must move when that
// addition lands, at which point the expectation becomes "Customer.a" and
// "Customer.c".
func TestJava7097_LambdaTypedParameterSameTypeCost(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Customer { void a() {} void c() {} }
class Svc {
  void run(java.util.List<Customer> cs) {
    { Customer o = new Customer(); o.a(); }
    cs.forEach((Customer o) -> o.c());
  }
}
`)
	j7094MustCall(t, rels, "a", "c")
	j7094MustNotCall(t, rels, "Customer.a", "Customer.c")
}

// ALWAYS-FIRES CONTROL — a lambda parameter named `p` must poison `p` and
// NOTHING ELSE. As in the catch control, the outer `Order o` is CAPTURED IN
// THE LAMBDA BODY: capturing is what distinguishes an arm that reads the
// `parameters` field from one that walks every `identifier` under the
// lambda_expression. Without the capture the over-broad arm survives — it did,
// on the first draft of this fixture.
func TestJava7097_LambdaParameterDoesNotPoisonOtherNames(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
class Svc {
  void run(java.util.List<Customer> cs) {
    Order o = new Order();
    cs.forEach(p -> o.a());
  }
}
`)
	j7094MustCall(t, rels, "Order.a")
}
