// issue7100_pattern_binders_test.go — Java's PATTERN-MATCHING binders, plus the
// varargs lambda parameter, must participate in collectLocalVarTypes' ambiguity
// ledger (issues #7100 and #7102, following #7094/#7095/#7097).
//
// #7097 named three binder constructs; two more surfaced while fixing them
// (#7100) and a third, a sub-shape of an arm #7099 was already touching, was
// found in review (#7102). #7100's closing recommendation was to stop taking
// these one pair at a time and ENUMERATE Java's binding forms against the
// grammar. That enumeration is in collectLocalVarTypes' comment; this file
// grades the four holes it found, one construct at a time.
//
// MEASURED AT 1a6a134f3, BEFORE ANY CHANGE, each construct beside a sibling
// `{ Order o = new Order(); o.a(); }` and with the nested binder's call inside
// its own scope:
//
//	instanceof pattern variable   `if (x instanceof Customer o) { o.b(); }`
//	                              → "Order.b"
//	switch type pattern, arrow    `case Customer o -> o.b();`      → "Order.b"
//	switch type pattern, colon    `case Customer o: o.b(); break;` → "Order.b"
//	record deconstruction         `if (x instanceof Pair(Customer o, Order q))`
//	                              → "Order.b"
//	varargs lambda parameter      `use((Customer... o) -> o.toString())`
//	                              → "Order.toString"
//
// `Order.b` is a WRONG receiver on a real same-file type: it binds, and bind
// rate, orphan rate and dangle count all score it as a success (#7056).
// Corpus incidence for this family measured 0, so fixtures are the ONLY
// instrument — hence both directions per construct, and every assertion on the
// EMITTED EDGE (Relationships[].ToID for CALLS) rather than on the ledger map.
//
// ALSO MEASURED BEFORE THE CHANGE and deliberately unchanged after it: each
// construct ALONE emits the bare leaf. None of them ever typed itself, so each
// could only ever LOSE to a sibling — which is the case that produces a
// confident wrong bind instead of an honest bare leaf. The *AloneUnchanged
// fixtures pin that, so a later change that starts typing these binders has to
// move a fixture rather than slip through.
//
// TWO MUTANTS ARE ALIVE AND RECORDED AS EQUIVALENT UNDER THIS SUITE, WITH A
// DEMONSTRATION EACH — not as kills, and not papered over with a manufactured
// fixture:
//
//   - `io-records-whole-expression-text-too`: additionally recording the whole
//     instanceof_expression's TEXT (`"x instanceof SubOrder q"`) cannot change
//     any edge, because the ledger is keyed by BARE NAME and looked up with a
//     receiver identifier's text. That string contains spaces, and no Java
//     identifier may (JLS §3.8), so the key is unreachable by construction. The
//     two arms of that over-firing direction that CAN collide with a real name —
//     the `left` subject and the `right` tested type — are separate mutants, and
//     both are DEAD. (In ROUND 1 this sentence claimed both DEAD while `right`
//     was in fact ALIVE against a vacuous control; see the round-2 note below.
//     It is true as of round 2, and only because the control was fixed.)
//   - `spread-records-declarator-text-not-name`: reading the
//     variable_declarator's TEXT instead of its `name` field is equivalent for a
//     spread parameter, because the two cannot differ — varargs admits no
//     initialiser, and legacy array notation on a variable-arity parameter is a
//     compile error ("legacy array notation not allowed on variable-arity
//     parameter", VERIFIED with javac 25, not argued). The over-firing arm that
//     CAN differ — recording every named child, so the declared type too — is a
//     separate mutant and is DEAD.
//
// ROUND 1 OF THIS FILE'S OWN SCORING HAD THE SAME CLASS OF FIXTURE DEFECT, and
// it is recorded rather than quietly fixed. Three always-fires mutants came back
// ALIVE — recording the instanceof `left` subject, every named child of a
// record_pattern_component, and every named child of a spread_parameter — for
// two different reasons, both in the CONTROLS, not the code: the subject control
// made the `instanceof` subject a method PARAMETER (parameters win over locals,
// so poisoning it changes nothing observable), and the two type-name controls
// wrote the shared type as `com.x.Customer`, whose recorded text is the QUALIFIED
// string and so never collides with a local called `Customer`. Both were
// rewritten — a typed local as the subject, unqualified types in the patterns —
// and every mutant was RE-SCORED from scratch, not carried.
//
// ROUND 2 FOUND THAT FIX HALF-APPLIED, WHICH IS WHY THE PARAGRAPH ABOVE IS KEPT
// RATHER THAN TIDIED. Round 1 rewrote the two RECORD-pattern type-name controls
// to unqualified types and left the instanceof and switch ones written
// `com.x.Customer`, so two always-fires mutants — instanceof also recording
// `right`, and `type_pattern` recording every named child — were still ALIVE,
// and the PR body asserted a kill for one of them. A kill claimed but not made
// is worse than an unscored mutant: it stops the next reader looking. Both
// controls now write the type BARE, both mutants are DEAD, and the whole set was
// re-scored from scratch a second time. A third vacuous control was found by
// auditing the rest of the file for the same shape rather than waiting to be
// told: TestJava7100_InstanceofWithoutBinderStillBinds named `right` and the
// whole-node text in its comment while holding no local either could collide
// with. It now holds one. THE AUDIT'S RULE, since three rounds have now hit it:
// a control is vacuous unless the fixture contains a LOCAL whose bare name is
// exactly the text the mutant would record.
//
// Every Java snippet in this file was COMPILED with javac 25 (34 of 34, zero
// errors, re-run on the round-2 tree), including the three shapes a reader is
// likeliest to doubt: a local legally obscuring a class name in an expression
// context while the pattern's type position still resolves to the type;
// `o.toString()` on a varargs parameter, which is an ARRAY; and a `var`
// record-pattern component.
//
// THE TRAP #7099 HIT AND THIS FILE AVOIDS: its first always-fires mutants came
// back ALIVE because the negative controls called the OUTER receiver outside
// the nested binder, where an over-broad walk cannot reach it — so they graded
// nothing while looking tested. Every *StillBinds control below captures the
// outer receiver INSIDE the binder's own body.

package java_test

import "testing"

// ---------------------------------------------------------------------------
// instanceof pattern variable (#7100) — instanceof_expression's `name` field
// ---------------------------------------------------------------------------

// COLLIDING, BOTH SOURCE ORDERS. The ledger is written by a stack DFS whose
// first writer wins, so the two orders are separately observable — that is how
// #7094's `var` hole was found.
func TestJava7100_InstanceofSiblingCollisionRefuses(t *testing.T) {
	localFirst := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
class Svc {
  void run(Object x) {
    { Order o = new Order(); o.a(); }
    if (x instanceof Customer o) { o.b(); }
  }
}
`)
	j7094MustNotCall(t, localFirst, "Order.b", "Order.a", "Customer.a", "Customer.b")
	j7094MustCall(t, localFirst, "a", "b")

	patternFirst := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
class Svc {
  void run(Object x) {
    if (x instanceof Customer o) { o.b(); }
    { Order o = new Order(); o.a(); }
  }
}
`)
	j7094MustNotCall(t, patternFirst, "Order.b", "Order.a", "Customer.a", "Customer.b")
	j7094MustCall(t, patternFirst, "a", "b")
}

// STANDALONE CONTROL — unchanged. The pattern variable HAS a declared type and
// could be typed; this change does not type it, so `o.b()` still emits the bare
// leaf, exactly as at 1a6a134f3.
func TestJava7100_InstanceofAloneUnchanged(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Customer { void b() {} }
class Svc {
  void run(Object x) {
    if (x instanceof Customer o) { o.b(); }
  }
}
`)
	j7094MustCall(t, rels, "b")
	j7094MustNotCall(t, rels, "Customer.b")
}

// ALWAYS-FIRES CONTROL 1 — a plain `instanceof` with NO pattern variable binds
// nothing. The grammar makes `name` OPTIONAL on instanceof_expression, so an
// arm that read the node's `right` type instead of the `name` field poisons
// `Customer` — which is a typed LOCAL here — and loses its type. The outer
// receivers are called INSIDE the if-body, so an over-broad walk cannot miss
// them.
//
// ROUND-2 AUDIT FIXED THIS FIXTURE TOO. As shipped in round 1 the only local
// was `Order o`, and the comment claimed an arm reading "the node's text, or
// its `right` type" would poison it. Neither can: the whole text is
// `x instanceof Customer` (spaces, so unreachable by a bare-name lookup) and
// `right` is `Customer`, not `o`. The control therefore graded NOTHING while
// its label read as tested — the same defect as the two qualified type-name
// controls. A second local deliberately named after the tested type makes the
// `right` arm reachable in the NO-BINDER shape specifically, which is the axis
// control 4 below does not vary. Compiles with javac 25 (a variable obscures a
// type name in an expression context; `instanceof`'s right operand is a
// type-only context).
func TestJava7100_InstanceofWithoutBinderStillBinds(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} void c() {} }
class Customer {}
class Svc {
  void run(Object x) {
    Order o = new Order();
    Order Customer = new Order();
    if (x instanceof Customer) { o.a(); Customer.c(); }
  }
}
`)
	j7094MustCall(t, rels, "Order.a", "Order.c")
}

// ALWAYS-FIRES CONTROL 2 — a pattern variable with a DIFFERENT name must not
// disturb the outer local, whose receiver is captured INSIDE the pattern's own
// block. An arm that refused on the presence of an instanceof pattern rather
// than on a name collision kills this.
func TestJava7100_InstanceofDistinctBinderStillBinds(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
class Svc {
  void run(Object x) {
    Order o = new Order();
    if (x instanceof Customer q) { o.a(); q.b(); }
  }
}
`)
	j7094MustCall(t, rels, "Order.a", "b")
	j7094MustNotCall(t, rels, "Customer.b", "Order.b")
}

// ALWAYS-FIRES CONTROL 3 — THE INSTANCEOF SUBJECT IS NOT A BINDER.
// `instanceof_expression` has a `left` field beside its `name` field, and `left`
// is very often a typed LOCAL. An arm reading the subject — or the whole
// expression's text — instead of the `name` field poisons that local, and the
// receiver here is captured INSIDE the pattern's own block so an over-broad
// walk cannot slip past it. This is the control that was MISSING in round 1:
// the always-fires mutant came back ALIVE because nothing graded the subject.
func TestJava7100_InstanceofSubjectStillBinds(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} }
class SubOrder extends Order { void b() {} }
class Svc {
  void run() {
    Order x = new Order();
    if (x instanceof SubOrder q) { x.a(); q.b(); }
  }
}
`)
	j7094MustCall(t, rels, "Order.a", "b")
	j7094MustNotCall(t, rels, "Order.b", "SubOrder.a")
}

// ALWAYS-FIRES CONTROL 4 — THE TESTED TYPE IS NOT A BINDER. The `right` field
// holds the type being tested; an arm that recorded it would poison a local
// that happens to share the type's name. A local may legally obscure a type
// name in an expression context while the pattern's type position still
// resolves to the type (JLS §6.4.2 / §6.5.6) — VERIFIED by compiling this shape
// with javac 25, not asserted. Same shape as the record-pattern type-name
// control below, scored separately per construct.
//
// THE TYPE IS WRITTEN UNQUALIFIED AND THAT IS THE WHOLE CONTROL. Round 2 of
// this file's scoring found this fixture written `y instanceof com.x.Customer
// q`: the recorded text of a `right` holding a scoped_type_identifier is the
// QUALIFIED string `com.x.Customer`, which can never collide with a local
// called `Customer`, so the mutant this control exists to kill came back ALIVE
// while the control passed on both sides. The bare `Customer` is what makes the
// collision possible. Do not qualify it.
func TestJava7100_InstanceofTypeNameIsNotABinder(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
class Svc {
  void run(Object y) {
    Order Customer = new Order();
    if (y instanceof Customer q) { Customer.a(); q.b(); }
  }
}
`)
	j7094MustCall(t, rels, "Order.a", "b")
}

// THE PRICE OF NOT TYPING THE PATTERN VARIABLE, observed rather than described.
// A SAME-TYPE sibling (`Order o` local, `Order o` pattern) used to bind BOTH
// sites — measured `Order.a` and `Order.c` at 1a6a134f3 — because the local's
// type covered the pattern's call by accident and happened to be right.
// Poisoning with "" costs that: both fall back to the bare leaf. This is the
// fixture that must MOVE if a later change gives the pattern variable its
// declared type, at which point the expectation becomes Order.a and Order.c.
func TestJava7100_InstanceofSameTypeCost(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} void c() {} }
class Svc {
  void run(Object x) {
    { Order o = new Order(); o.a(); }
    if (x instanceof Order o) { o.c(); }
  }
}
`)
	j7094MustCall(t, rels, "a", "c")
	j7094MustNotCall(t, rels, "Order.a", "Order.c")
}

// ---------------------------------------------------------------------------
// switch type pattern (#7100) — switch_label → pattern → type_pattern
// ---------------------------------------------------------------------------

// COLLIDING, ARROW LABEL, BOTH SOURCE ORDERS.
func TestJava7100_SwitchPatternSiblingCollisionRefuses(t *testing.T) {
	localFirst := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
class Svc {
  void run(Object x) {
    { Order o = new Order(); o.a(); }
    switch (x) { case Customer o -> o.b(); default -> {} }
  }
}
`)
	j7094MustNotCall(t, localFirst, "Order.b", "Order.a", "Customer.a", "Customer.b")
	j7094MustCall(t, localFirst, "a", "b")

	patternFirst := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
class Svc {
  void run(Object x) {
    switch (x) { case Customer o -> o.b(); default -> {} }
    { Order o = new Order(); o.a(); }
  }
}
`)
	j7094MustNotCall(t, patternFirst, "Order.b", "Order.a", "Customer.a", "Customer.b")
	j7094MustCall(t, patternFirst, "a", "b")
}

// COLLIDING, COLON LABEL. The two label forms have DIFFERENT parents in the
// grammar — `switch_rule` for the arrow form, `switch_block_statement_group`
// for the colon form — so a matcher routed through the label's parent covers
// only one. VARIED: the label form. HELD CONSTANT: the binder, the sibling, the
// types, the source order.
func TestJava7100_SwitchPatternColonLabelCollisionRefuses(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
class Svc {
  void run(Object x) {
    { Order o = new Order(); o.a(); }
    switch (x) { case Customer o: o.b(); break; default: break; }
  }
}
`)
	j7094MustNotCall(t, rels, "Order.b", "Order.a")
	j7094MustCall(t, rels, "a", "b")
}

// COLLIDING, GUARDED LABEL (`case Customer o when o.ok() -> …`). The guard is a
// third child of switch_label; the binder is in scope inside it. VARIED: the
// presence of a guard. HELD CONSTANT: everything else.
func TestJava7100_SwitchPatternGuardedLabelCollisionRefuses(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} boolean ok() { return true; } }
class Svc {
  void run(Object x) {
    { Order o = new Order(); o.a(); }
    switch (x) { case Customer o when o.ok() -> o.b(); default -> {} }
  }
}
`)
	j7094MustNotCall(t, rels, "Order.b", "Order.a", "Order.ok")
	j7094MustCall(t, rels, "a", "b", "ok")
}

// STANDALONE CONTROL — unchanged: bare leaf, as at 1a6a134f3.
func TestJava7100_SwitchPatternAloneUnchanged(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Customer { void b() {} }
class Svc {
  void run(Object x) {
    switch (x) { case Customer o -> o.b(); default -> {} }
  }
}
`)
	j7094MustCall(t, rels, "b")
	j7094MustNotCall(t, rels, "Customer.b")
}

// ALWAYS-FIRES CONTROL — distinct binder name, outer receiver captured INSIDE
// the switch rule's own block.
func TestJava7100_SwitchPatternDistinctBinderStillBinds(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
class Svc {
  void run(Object x) {
    Order o = new Order();
    switch (x) { case Customer q -> { o.a(); q.b(); } default -> {} }
  }
}
`)
	j7094MustCall(t, rels, "Order.a", "b")
	j7094MustNotCall(t, rels, "Customer.b", "Order.b")
}

// ALWAYS-FIRES CONTROL 2 — THE PATTERN'S TYPE IS NOT A BINDER. `type_pattern`
// holds the declared type and the binder as two sibling children; only the
// `identifier` one is the binder. An arm that recorded BOTH would poison a
// local sharing the type's name.
//
// UNQUALIFIED ON PURPOSE, same round-2 finding as the instanceof control above:
// written `case com.x.Customer q` the type child is a scoped_type_identifier
// whose text never collides with a local `Customer`, so the record-ALL-named-
// children mutant was ALIVE against a passing control. Compiles with javac 25.
func TestJava7100_SwitchPatternTypeNameIsNotABinder(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
class Svc {
  void run(Object y) {
    Order Customer = new Order();
    switch (y) { case Customer q -> { Customer.a(); q.b(); } default -> {} }
  }
}
`)
	j7094MustCall(t, rels, "Order.a", "b")
}

// THE PRICE OF NOT TYPING THE SWITCH PATTERN — same shape as the instanceof
// cost fixture, scored separately so one construct's regression cannot hide
// behind another's.
func TestJava7100_SwitchPatternSameTypeCost(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} void c() {} }
class Svc {
  void run(Object x) {
    { Order o = new Order(); o.a(); }
    switch (x) { case Order o -> o.c(); default -> {} }
  }
}
`)
	j7094MustCall(t, rels, "a", "c")
	j7094MustNotCall(t, rels, "Order.a", "Order.c")
}

// ---------------------------------------------------------------------------
// record deconstruction pattern (#7100, found by the enumeration — named in
// neither issue) — record_pattern_body → record_pattern_component
// ---------------------------------------------------------------------------

// COLLIDING, via `instanceof` (the `pattern` field, which is the alternative to
// `name`), BOTH SOURCE ORDERS.
func TestJava7100_RecordPatternInstanceofCollisionRefuses(t *testing.T) {
	localFirst := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
record Pair(Customer c, Order d) {}
class Svc {
  void run(Object x) {
    { Order o = new Order(); o.a(); }
    if (x instanceof Pair(Customer o, Order q)) { o.b(); }
  }
}
`)
	j7094MustNotCall(t, localFirst, "Order.b", "Order.a", "Customer.a", "Customer.b")
	j7094MustCall(t, localFirst, "a", "b")

	patternFirst := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
record Pair(Customer c, Order d) {}
class Svc {
  void run(Object x) {
    if (x instanceof Pair(Customer o, Order q)) { o.b(); }
    { Order o = new Order(); o.a(); }
  }
}
`)
	j7094MustNotCall(t, patternFirst, "Order.b", "Order.a", "Customer.a", "Customer.b")
	j7094MustCall(t, patternFirst, "a", "b")
}

// COLLIDING, via a SWITCH label. VARIED: which construct carries the record
// pattern (instanceof above vs switch here). HELD CONSTANT: the pattern itself,
// the sibling, the types, the source order — so a differing verdict can only be
// the carrier.
func TestJava7100_RecordPatternSwitchCollisionRefuses(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
record Pair(Customer c, Order d) {}
class Svc {
  void run(Object x) {
    { Order o = new Order(); o.a(); }
    switch (x) { case Pair(Customer o, Order q) -> o.b(); default -> {} }
  }
}
`)
	j7094MustNotCall(t, rels, "Order.b", "Order.a")
	j7094MustCall(t, rels, "a", "b")
}

// COLLIDING, NESTED pattern. `Pair(Point(Customer o, …), …)` puts the binder two
// record_pattern levels down; the flat descendant walk reaches it, and this
// fixture is what says so. VARIED: nesting depth. HELD CONSTANT: the carrier,
// the sibling, the types.
func TestJava7100_RecordPatternNestedCollisionRefuses(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
record Point(Customer c, Order d) {}
record Pair(Point p, Order q) {}
class Svc {
  void run(Object x) {
    { Order o = new Order(); o.a(); }
    if (x instanceof Pair(Point(Customer o, Order q), Order r)) { o.b(); }
  }
}
`)
	j7094MustNotCall(t, rels, "Order.b", "Order.a")
	j7094MustCall(t, rels, "a", "b")
}

// COLLIDING, `var` COMPONENT. `Pair(var o, …)` is the one component spelling
// with no other coverage in this file, and it is the spelling that decides
// whether patternBinderName's "first `identifier` child" reading is correct:
// collectLocalVarTypes' comment claims `var` arrives as `type_identifier "var"`
// and so "does not shift which child is the binder". That is a universal claim
// about the grammar, and this fixture is what OBSERVES it — were `var` an
// `identifier` child instead, patternBinderName would return "var", the binder
// `o` would never be poisoned, and `o.b()` would come out `Order.b` on the
// sibling's type. VARIED: the component's type spelling (`var` vs an explicit
// type_identifier). HELD CONSTANT: the carrier, the arity, the sibling, the
// receiver. Compiles with javac 25.
func TestJava7100_RecordPatternVarComponentCollisionRefuses(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
record Pair(Customer c, Order d) {}
class Svc {
  void run(Object x) {
    { Order o = new Order(); o.a(); }
    if (x instanceof Pair(var o, Order q)) { o.b(); q.a(); }
  }
}
`)
	j7094MustNotCall(t, rels, "Order.b", "Order.a")
	j7094MustCall(t, rels, "a", "b")
}

// STANDALONE CONTROL — unchanged: bare leaf.
func TestJava7100_RecordPatternAloneUnchanged(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
record Pair(Customer c, Order d) {}
class Svc {
  void run(Object x) {
    if (x instanceof Pair(Customer o, Order q)) { o.b(); }
  }
}
`)
	j7094MustCall(t, rels, "b")
	j7094MustNotCall(t, rels, "Customer.b")
}

// ALWAYS-FIRES CONTROL 1 — distinct binder names, outer receiver captured
// INSIDE the pattern's own block. Both components are graded, so an arm that
// poisoned every name it walked past kills this.
func TestJava7100_RecordPatternDistinctBindersStillBind(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
record Pair(Customer c, Order d) {}
class Svc {
  void run(Object x) {
    Order o = new Order();
    if (x instanceof Pair(Customer q, Order r)) { o.a(); q.b(); r.a(); }
  }
}
`)
	j7094MustCall(t, rels, "Order.a", "a", "b")
	j7094MustNotCall(t, rels, "Customer.b", "Order.b")
}

// ALWAYS-FIRES CONTROL 2 — THE RECORD'S TYPE NAME IS NOT A BINDER. A
// `record_pattern` node carries the record's type as its OWN `identifier` child
// (`Pair`), so an arm matching `record_pattern` rather than
// `record_pattern_component` would poison the name `Pair`. A local variable may
// legally be called `Pair`, and here it is: if that name were poisoned,
// `Pair.a()` would lose its receiver. This is the fixture that pins the choice
// of matched node, not merely the choice of child.
func TestJava7100_RecordPatternTypeNameIsNotABinder(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { void b() {} }
record Pair(Customer c, Order d) {}
class Svc {
  void run(Object x) {
    Order Pair = new Order();
    if (x instanceof Pair(Customer q, Order r)) { Pair.a(); }
  }
}
`)
	j7094MustCall(t, rels, "Order.a")
}

// ALWAYS-FIRES CONTROL 3 — A COMPONENT'S TYPE IS NOT A BINDER EITHER. Control 2
// above pins the record_pattern's OWN identifier (`Pair`); this one pins the
// per-component type, which is a DIFFERENT child of a different node. An arm
// that recorded every named child of a record_pattern_component — rather than
// its first `identifier` — poisons the component type's name, and nothing in
// controls 1 or 2 observes that. VARIED: which name is shared with a local
// (the record's type vs a component's type). HELD CONSTANT: the carrier, the
// pattern's arity, the outer local's type.
func TestJava7100_RecordPatternComponentTypeIsNotABinder(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} void c() {} }
class Customer { void b() {} }
record Pair(Customer c, Order d) {}
class Svc {
  void run(Object y) {
    Order Customer = new Order();
    if (y instanceof Pair(Customer q, Order r)) { Customer.a(); q.b(); r.c(); }
  }
}
`)
	j7094MustCall(t, rels, "Order.a", "b", "c")
}

// THE PRICE OF NOT TYPING A RECORD PATTERN COMPONENT — scored separately from
// the instanceof and switch cost fixtures.
func TestJava7100_RecordPatternSameTypeCost(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} void c() {} }
record Pair(Order d) {}
class Svc {
  void run(Object x) {
    { Order o = new Order(); o.a(); }
    if (x instanceof Pair(Order o)) { o.c(); }
  }
}
`)
	j7094MustCall(t, rels, "a", "c")
	j7094MustNotCall(t, rels, "Order.a", "Order.c")
}

// ---------------------------------------------------------------------------
// varargs lambda parameter (#7102) — formal_parameters → spread_parameter →
// variable_declarator{name}
// ---------------------------------------------------------------------------

// COLLIDING, PURE VARARGS, BOTH SOURCE ORDERS. #7099 believed its lambda arm
// covered every binder `formal_parameters` can hold; `spread_parameter` has no
// `name` field and its variable_declarator is NOT reachable from the
// local_variable_declaration walk, so this name still ceded to the sibling —
// measured `Order.toString` both before and after #7099. The probe method is
// `toString` because a varargs receiver is an ARRAY: only `Object` methods are
// callable on it, so a method declared on `Customer` alone would not compile —
// which is why an override like `toString` is the realistic shape here (#7102).
// Every Java snippet in this file was COMPILED with javac 25 before use.
func TestJava7102_VarargsLambdaSiblingCollisionRefuses(t *testing.T) {
	localFirst := j7094Calls(t, `package com.x;
class Order { void a() {} public String toString() { return "o"; } }
class Customer { public String toString() { return "c"; } }
interface VC { void go(Customer... cs); }
class Svc {
  void use(VC f) {}
  void run() {
    { Order o = new Order(); o.a(); }
    use((Customer... o) -> o.toString());
  }
}
`)
	j7094MustNotCall(t, localFirst, "Order.toString", "Order.a", "Customer.toString")
	j7094MustCall(t, localFirst, "a", "toString")

	lambdaFirst := j7094Calls(t, `package com.x;
class Order { void a() {} public String toString() { return "o"; } }
class Customer { public String toString() { return "c"; } }
interface VC { void go(Customer... cs); }
class Svc {
  void use(VC f) {}
  void run() {
    use((Customer... o) -> o.toString());
    { Order o = new Order(); o.a(); }
  }
}
`)
	j7094MustNotCall(t, lambdaFirst, "Order.toString", "Order.a", "Customer.toString")
	j7094MustCall(t, lambdaFirst, "a", "toString")
}

// COLLIDING, MIXED SHAPE, THE SPREAD PARAMETER COLLIDING.
// `(Customer a, Object... o) -> …` puts a `formal_parameter` and a
// `spread_parameter` in the SAME formal_parameters node, so a fixture holding
// only the pure-varargs form leaves the mixed one ungraded. VARIED: whether a
// plain formal parameter sits beside the spread one. HELD CONSTANT: which
// parameter collides (the spread one), the sibling, the source order.
func TestJava7102_MixedVarargsSpreadCollisionRefuses(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} public String toString() { return "o"; } }
class Customer { public String toString() { return "c"; } }
interface VM { void go(Customer a, Object... rest); }
class Svc {
  void use(VM f) {}
  void run() {
    { Order o = new Order(); o.a(); }
    use((Customer a, Object... o) -> o.toString());
  }
}
`)
	j7094MustNotCall(t, rels, "Order.toString", "Order.a")
	j7094MustCall(t, rels, "a", "toString")
}

// NEGATIVE CONTROL ON THE RESTRUCTURE — the mixed shape with the PLAIN formal
// parameter colliding. #7099 already refused this, and adding the
// `spread_parameter` branch beside it must not change that: the switch's
// `formal_parameter` branch still has to run for a node that also holds a
// spread. VARIED: which of the two parameters collides. HELD CONSTANT: the
// mixed node itself, the sibling, the source order. Measured identical before
// and after this change.
func TestJava7102_MixedVarargsFormalStillRefuses(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} void b() {} }
class Customer { void b() {} }
interface VM { void go(Customer a, Object... rest); }
class Svc {
  void use(VM f) {}
  void run() {
    { Order o = new Order(); o.a(); }
    use((Customer o, Object... q) -> o.b());
  }
}
`)
	j7094MustNotCall(t, rels, "Order.b", "Order.a")
	j7094MustCall(t, rels, "a", "b")
}

// STANDALONE CONTROL — unchanged. Measured bare `toString` at 1a6a134f3 and still
// bare: a varargs lambda parameter never typed itself, so it could only ever
// lose to a sibling.
func TestJava7102_VarargsLambdaAloneUnchanged(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Customer { public String toString() { return "c"; } }
interface VC { void go(Customer... cs); }
class Svc {
  void use(VC f) {}
  void run() {
    use((Customer... o) -> o.toString());
  }
}
`)
	j7094MustCall(t, rels, "toString")
	j7094MustNotCall(t, rels, "Customer.toString")
}

// ALWAYS-FIRES CONTROL — distinct binder name, outer receiver captured INSIDE
// the lambda body (the exact place #7099's first round got wrong).
func TestJava7102_VarargsLambdaDistinctBinderStillBinds(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { public String toString() { return "c"; } }
interface VC { void go(Customer... cs); }
class Svc {
  void use(VC f) {}
  void run() {
    Order o = new Order();
    use((Customer... q) -> o.a());
  }
}
`)
	j7094MustCall(t, rels, "Order.a")
}

// ALWAYS-FIRES CONTROL 2 — THE SPREAD PARAMETER'S TYPE IS NOT A BINDER.
// `spread_parameter` holds its declared type and its variable_declarator as
// sibling children; only the declarator's `name` is the binder. An arm that
// recorded every named child poisons the type's name, and the distinct-binder
// control above cannot see that.
func TestJava7102_VarargsTypeNameIsNotABinder(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Customer { public String toString() { return "c"; } }
interface VC { void go(Customer... cs); }
class Svc {
  void use(VC f) {}
  void run() {
    Order Customer = new Order();
    use((Customer... q) -> Customer.a());
  }
}
`)
	j7094MustCall(t, rels, "Order.a")
}

// THE SAME-TYPE CASE IS DIFFERENT HERE, AND THAT IS THE POINT. For the three
// pattern constructs above, a same-type sibling used to bind CORRECTLY by
// accident, so poisoning costs real recall (the *SameTypeCost fixtures). A
// varargs parameter declared `Order... o` is an `Order[]`, NOT an `Order`: the
// bind it used to get — measured `Order.toString` at 1a6a134f3 — was WRONG even
// with a same-type sibling, because only `Object` methods are callable on the
// array. So this fixture records a recall LOSS THAT IS NOT A COST, and unlike
// the three above it must NOT move when someone starts typing binders: typing
// a spread parameter to its element type would reinstate the wrong edge.
func TestJava7102_VarargsSameTypeSiblingWasAlsoWrong(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} public String toString() { return "o"; } }
interface VO { void go(Order... os); }
class Svc {
  void use(VO f) {}
  void run() {
    { Order o = new Order(); o.a(); }
    use((Order... o) -> o.toString());
  }
}
`)
	j7094MustCall(t, rels, "a", "toString")
	j7094MustNotCall(t, rels, "Order.a", "Order.toString")
}
