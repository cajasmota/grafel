// issue7109_class_scope_test.go — a name bound inside a local or anonymous
// CLASS body belongs to that class's scope, not to the enclosing method's
// (issue #7109).
//
// WHAT WAS MEASURED AT 83004cbef (the parent of this change), on Java compiled
// clean by javac 25.0.3 — every row here is a wrong dotted receiver on a REAL
// same-file type, which BINDS, so bind rate / orphan rate / dangle count all
// score it as a success and it never reaches bug-extractor (#7056):
//
//	nested method PARAMETER   `new Go(){ public void go(Cust o){ o.b(); } }`
//	                          beside an outer `Order o` emitted "Order.b".
//	                          The nested formal_parameter was in NO ledger arm
//	                          at all, so the sibling local simply owned `o`.
//	nested class FIELD        `new Runnable(){ Cust o = new Cust(); … }`
//	                          emitted "Order.b" for the same reason.
//	local enum CONSTANT       `enum E { Cust; void go(){ Cust.b(); } }` beside
//	                          an outer `Order Cust` emitted "Order.b". This is
//	                          the `enum_constant` entry #7100's grammar
//	                          enumeration named and deferred to "the
//	                          anonymous/local-class-member gap" — i.e. to this
//	                          issue. It is covered here.
//	nested class LOCAL        refused on BOTH sides instead (the #7094 ledger
//	                          saw two disagreeing declarations of `o`), which
//	                          is the recall cost
//	                          TestJava7094_ClassBodyShadowingRecallRecoveredBy7109
//	                          recorded and which this change recovers.
//
// THE FIX IS A SCOPE BOUNDARY, NOT A NEW LEDGER ARM. collectLocalVarTypes now
// walks with scopedFindNodes, which stops at `class_body` / `interface_body` /
// `enum_body` / `annotation_type_body`, and javaScopeCalls recurses into each
// such body with its own ledger layered over the enclosing one.
//
// ONE CELL IS EMPTY AND IT WAS MEASURED, NOT ASSUMED: `annotation_type_body`.
// javac 25.0.3 rejects "annotation interface declaration not allowed here" in
// every place reachable from a method body — directly in the body, in a local
// class, in a local interface, and in an anonymous class body — so no
// compilable Java reaches it. The boundary entry for it was therefore DELETED
// rather than shipped with a necessarily-ALIVE mutant.
//
// TWO DIRECTIONS, AND THE SECOND IS THE DANGEROUS ONE. A boundary that stops
// the walk TOO EARLY silently re-opens #7094 / #7097 / #7099 / #7100: a nested
// statement block, a lambda body, a catch clause, a try-with-resources
// specification and a pattern label are all the SAME class scope and must keep
// refusing exactly as they did. TestJava7109_StatementScopesAreNotClassScopes
// holds that half, one sub-case per existing binder arm.
//
// LAYERING, NOT DROPPING. Java capture means an effectively-final local of the
// enclosing method IS visible by bare name inside a local/anonymous class
// body, so the inner ledger is an OVERLAY on the outer one. The conjunction
// fixture (TestJava7109_ShadowAndCaptureInTheSameBody) is the one that
// separates the three candidate implementations: a shadow and a capture in the
// SAME nested body, where "drop the inherited ledger" loses the capture and
// "inherit without overlay" keeps the wrong receiver. Either alone passes on
// two of the three.
//
// Every assertion is on the EMITTED EDGE (Relationships[].ToID for CALLS), not
// on a map's contents.
//
// EVERY FIXTURE IN THIS FILE COMPILES. Each was extracted and run through
// javac 25.0.3 (~/.sdkman/candidates/java/current/bin/javac) with zero errors
// — a legality claim about Java is executable here, and an earlier revision of
// collectLocalVarTypes' comment shipped a FALSE one (that Java forbids an
// inner block from redeclaring a name already in scope). The local
// `interface` / `record` / `enum` declarations need release 16+, which that
// compiler is.

package java_test

import "testing"

// ---------------------------------------------------------------------------
// THE HEADLINE ROW — a nested member's PARAMETER, with the receiver used
// INSIDE the nested body. Three PRs in this family shipped controls placed
// where an over-broad walk could not reach them (a method parameter of the
// OUTER method, which wins over locals; a qualified type name, which never
// collides with a bare local), so the over-refusal direction read as tested
// while grading nothing. The colliding receiver here is a BARE identifier used
// INSIDE the anonymous class body.
// ---------------------------------------------------------------------------

func TestJava7109_AnonClassParamOwnsItsName(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} void b() {} }
class Cust  { void b() {} }
interface Go { void go(Cust c); }
class Svc {
  void run() {
    Order o = new Order();
    o.a();
    Go g = new Go() { public void go(Cust o) { o.b(); } };
  }
}
`)
	// At 83004cbef: "Order.b" — the wrong receiver this issue reports.
	j7094MustNotCall(t, rels, "Order.b", "Cust.a")
	// The nested parameter types its own call, and the outer local keeps its.
	j7094MustCall(t, rels, "Cust.b", "Order.a")
}

func TestJava7109_LocalClassParamOwnsItsName(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} void b() {} }
class Cust  { void b() {} }
class Svc {
  void run() {
    Order o = new Order();
    o.a();
    class Inner { void go(Cust o) { o.b(); } }
  }
}
`)
	j7094MustNotCall(t, rels, "Order.b", "Cust.a")
	j7094MustCall(t, rels, "Cust.b", "Order.a")
}

func TestJava7109_LocalClassConstructorParamOwnsItsName(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} void b() {} }
class Cust  { void b() {} }
class Svc {
  void run() {
    Order o = new Order();
    o.a();
    class Inner { Inner(Cust o) { o.b(); } }
  }
}
`)
	j7094MustNotCall(t, rels, "Order.b")
	j7094MustCall(t, rels, "Cust.b", "Order.a")
}

// interface_body and class_body are DIFFERENT grammar nodes, so a boundary set
// that names only one of them leaves the other open. A local interface with a
// default method is the interface_body cell.
func TestJava7109_LocalInterfaceDefaultMethodParamOwnsItsName(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} void b() {} }
class Cust  { void b() {} }
class Svc {
  void run() {
    Order o = new Order();
    o.a();
    interface I { default void go(Cust o) { o.b(); } }
  }
}
`)
	j7094MustNotCall(t, rels, "Order.b")
	j7094MustCall(t, rels, "Cust.b", "Order.a")
}

// A record body is a `class_body`, so this shares a cell with the local-class
// row on the BOUNDARY axis — it is here because record_declaration is a
// separate declaration node and a reader would otherwise have to take the
// shared spelling on trust.
func TestJava7109_LocalRecordMethodParamOwnsItsName(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} void b() {} }
class Cust  { void b() {} }
class Svc {
  void run() {
    Order o = new Order();
    o.a();
    record R(int x) { void go(Cust o) { o.b(); } }
  }
}
`)
	j7094MustNotCall(t, rels, "Order.b")
	j7094MustCall(t, rels, "Cust.b", "Order.a")
}

// ---------------------------------------------------------------------------
// FIELD and ENUM_CONSTANT — the two binder families the class scope
// contributes itself. Without them the boundary cut alone is NOT enough: the
// inherited ledger still carries the outer local under the same name, so the
// wrong dotted receiver survives the cut. Both are therefore load-bearing, and
// each has its own mutant.
// ---------------------------------------------------------------------------

func TestJava7109_AnonClassFieldOwnsItsName(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} void b() {} }
class Cust  { void b() {} }
class Svc {
  void run() {
    Order o = new Order();
    o.a();
    Runnable r = new Runnable() {
      Cust o = new Cust();
      public void run() { o.b(); }
    };
  }
}
`)
	// At 83004cbef: "Order.b".
	j7094MustNotCall(t, rels, "Order.b")
	j7094MustCall(t, rels, "Cust.b", "Order.a")
}

// An enum's fields and methods sit one level deeper than its constants, inside
// `enum_body_declarations`, but belong to the SAME class scope. A field arm
// that reads only the enum_body's immediate children misses this.
func TestJava7109_LocalEnumFieldOwnsItsName(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} void b() {} }
class Cust  { void b() {} }
class Svc {
  void run() {
    Order o = new Order();
    o.a();
    enum E {
      ONE;
      Cust o = new Cust();
      void go() { o.b(); }
    }
  }
}
`)
	j7094MustNotCall(t, rels, "Order.b")
	j7094MustCall(t, rels, "Cust.b", "Order.a")
}

// A local enum's METHOD PARAMETER. Its members sit in `enum_body_declarations`,
// one level below the enum_body, and the member walk has to see through that
// node to give the method its own parameter frame. A mutant that makes
// `enum_body_declarations` opaque (falling to the default arm, which has NO
// parameter frame) is ALIVE against a parameterless enum method, so this row
// exists specifically to grade that transparency.
func TestJava7109_LocalEnumMethodParamOwnsItsName(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} void b() {} }
class Cust  { void b() {} }
class Svc {
  void run() {
    Order o = new Order();
    o.a();
    enum E { ONE; void go(Cust o) { o.b(); } }
  }
}
`)
	j7094MustNotCall(t, rels, "Order.b")
	j7094MustCall(t, rels, "Cust.b", "Order.a")
}

// THE `enum_constant` CELL. #7100's grammar enumeration named enum_constant
// explicitly as "the one entry that binds a VALUE name rather than a type
// name" and deferred it to this issue as "a strict sub-case of the
// anonymous/local-class-member gap". It is covered here.
//
// The constant is POISONED rather than typed: its type is the local enum,
// which has no entity (walk returns at method_declaration and never emits the
// members of a method-local class), so typing it would fabricate a target.
// What remains is receiverTypeName's PascalCase static-call arm, which yields
// "Cust.b" — a target that DANGLES rather than binding to the wrong real type.
// That is the direction #7094 chose explicitly ("a fabricated `var.b` at least
// dangles, while `Order.b` on a real Order does not"), so the assertion that
// matters is the NEGATIVE one.
func TestJava7109_LocalEnumConstantOwnsItsName(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} void b() {} }
class Svc {
  void run() {
    Order Cust = new Order();
    Cust.a();
    enum E { Cust; void b() {} void go() { Cust.b(); } }
  }
}
`)
	// At 83004cbef: "Order.b" — a wrong receiver on a real same-file type.
	j7094MustNotCall(t, rels, "Order.b")
	j7094MustCall(t, rels, "Order.a")
}

// An instance initialiser block is a direct child of the class body with no
// parameter frame of its own — the member walk's default arm.
func TestJava7109_AnonClassInitialiserBlockOwnsItsName(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} void b() {} }
class Cust  { void b() {} }
class Svc {
  void run() {
    Order o = new Order();
    o.a();
    Runnable r = new Runnable() {
      { Cust o = new Cust(); o.b(); }
      public void run() {}
    };
  }
}
`)
	j7094MustNotCall(t, rels, "Order.b")
	j7094MustCall(t, rels, "Cust.b", "Order.a")
}

// A RECORD COMPONENT is the one class-scope binder that does not live inside
// the body: the grammar hangs it off the record_declaration as
// `parameters: formal_parameters`, a SIBLING of the class_body. So the
// boundary cut alone does not reach it — MEASURED at 9fe0b2be5, with the
// boundary already in place, this still emitted "Order.b".
func TestJava7109_LocalRecordComponentOwnsItsName(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} void b() {} }
class Cust  { void b() {} }
class Svc {
  void run() {
    Order o = new Order();
    o.a();
    record R(Cust o) { void go() { o.b(); } }
  }
}
`)
	j7094MustNotCall(t, rels, "Order.b")
	j7094MustCall(t, rels, "Cust.b", "Order.a")
}

// The COMPACT constructor form of the same binder. It has no `parameters`
// field of its own — the component it refers to is the record's — so it is
// graded separately from the accessor form above rather than assumed to share
// its fate.
func TestJava7109_LocalRecordCompactConstructorSeesTheComponent(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} void b() {} }
class Cust  { void b() {} }
class Svc {
  void run() {
    Order o = new Order();
    o.a();
    record R(Cust o) { R { o.b(); } }
  }
}
`)
	j7094MustNotCall(t, rels, "Order.b")
	j7094MustCall(t, rels, "Cust.b", "Order.a")
}

// An INTERFACE field is a `constant_declaration`, a DIFFERENT node kind from
// `field_declaration` with an identical shape — so the field arm that covers
// class and enum bodies cannot see it, and at 9fe0b2be5 this still emitted
// "Order.b" with the boundary in place. Node kind DERIVED from a parse dump.
func TestJava7109_LocalInterfaceConstantOwnsItsName(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} void b() {} }
class Cust  { void b() {} }
class Svc {
  void run() {
    Order o = new Order();
    o.a();
    interface I { Cust o = new Cust(); default void go() { o.b(); } }
  }
}
`)
	j7094MustNotCall(t, rels, "Order.b")
	j7094MustCall(t, rels, "Cust.b", "Order.a")
}

// A MULTI-DECLARATOR interface constant binds EVERY name, not just the first.
// The outer collision is on the SECOND declarator, so a first-only reading
// leaves `q` to the outer `Order q` and emits "Order.b".
func TestJava7109_MultiDeclaratorInterfaceConstantBindsEveryName(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} void b() {} }
class Cust  { void b() {} void c() {} }
class Svc {
  void run() {
    Order q = new Order();
    q.a();
    interface I { Cust p = new Cust(), q = new Cust(); default void go() { p.c(); q.b(); } }
  }
}
`)
	j7094MustNotCall(t, rels, "Order.b")
	j7094MustCall(t, rels, "Cust.c", "Cust.b", "Order.a")
}

// A local enum's CONSTRUCTOR parameter. Unlike the three rows above this one
// already worked once the boundary was in place — the member walk's
// constructor arm reaches it through `enum_body_declarations` — and it is
// recorded so the cell is occupied rather than argued.
func TestJava7109_LocalEnumConstructorParamOwnsItsName(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} void b() {} }
class Cust  { void b() {} }
class Svc {
  void run() {
    Order o = new Order();
    o.a();
    enum E { ONE(null); E(Cust o) { o.b(); } }
  }
}
`)
	j7094MustNotCall(t, rels, "Order.b")
	j7094MustCall(t, rels, "Cust.b", "Order.a")
}

// ---------------------------------------------------------------------------
// THE CONJUNCTION. A shadow and a capture in the SAME nested body. This is the
// row that separates the implementations; each half alone is passed by a wrong
// one.
// ---------------------------------------------------------------------------

func TestJava7109_ShadowAndCaptureInTheSameBody(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} void b() {} }
class Cust  { void b() {} }
class Note  { void n() {} }
interface Go { void go(Cust c); }
class Svc {
  void run() {
    Order o = new Order();
    Note note = new Note();
    o.a();
    Go g = new Go() { public void go(Cust o) { o.b(); note.n(); } };
  }
}
`)
	// "inherit the outer ledger without overlaying the inner scope" keeps this:
	j7094MustNotCall(t, rels, "Order.b")
	// "drop the inherited ledger at the boundary" loses this:
	j7094MustCall(t, rels, "Note.n")
	j7094MustCall(t, rels, "Cust.b", "Order.a")
}

// Capture with NO shadow anywhere. An effectively-final outer local used bare
// inside an anonymous class body bound correctly at 83004cbef (the flat walk
// reached it by accident) and must keep doing so — dropping it would be a
// recall regression dressed as a fix.
func TestJava7109_CapturedOuterLocalStillBindsInside(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Svc {
  void run() {
    Order o = new Order();
    Runnable r = new Runnable() { public void run() { o.a(); } };
  }
}
`)
	j7094MustCall(t, rels, "Order.a")
}

// Nesting is not one level deep. An anonymous class inside an anonymous class
// must get its own frame, and the outermost local must still be captured
// through both.
func TestJava7109_NestedAnonClassesEachGetTheirOwnFrame(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} void b() {} }
class Cust  { void b() {} }
class Note  { void n() {} }
class Svc {
  void run() {
    Order o = new Order();
    Note note = new Note();
    o.a();
    Runnable r = new Runnable() { public void run() {
      Runnable s = new Runnable() { public void run() { Cust o = new Cust(); o.b(); note.n(); } };
    } };
  }
}
`)
	j7094MustNotCall(t, rels, "Order.b")
	j7094MustCall(t, rels, "Cust.b", "Order.a", "Note.n")
}

// ---------------------------------------------------------------------------
// THE PERMISSIVE DIRECTION — a boundary that stops the walk TOO EARLY. Each
// sub-case is an existing binder arm whose refusal must survive: these are all
// the SAME class scope, so a boundary on `block`, `lambda_expression`,
// `catch_clause`, `resource_specification` or a pattern node would re-open
// #7094 / #7097 / #7099 / #7100 while every row above stayed green.
//
// Each sub-case is a #7094-shaped collision: the sibling blocks are separate,
// so there is no JLS §6.4 conflict and all of them compile.
// ---------------------------------------------------------------------------

func TestJava7109_StatementScopesAreNotClassScopes(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"nested statement block", `package com.x;
class Order { void a() {} }
class Cust  { void b() {} }
class Svc {
  void run() {
    { Order o = new Order(); o.a(); }
    { Cust o = new Cust(); o.b(); }
  }
}
`},
		{"lambda parameter", `package com.x;
class Order { void a() {} }
class Cust  { void b() {} }
class Svc {
  void run(java.util.List<Cust> cs) {
    { Order o = new Order(); o.a(); }
    cs.forEach(o -> o.b());
  }
}
`},
		{"catch parameter", `package com.x;
class Order { void a() {} }
class Cust extends Exception { void b() {} }
class Svc {
  void run() {
    { Order o = new Order(); o.a(); }
    try { throw new Cust(); } catch (Cust o) { o.b(); }
  }
}
`},
		{"try-with-resources", `package com.x;
class Order { void a() {} }
class Cust implements AutoCloseable { void b() {} public void close() {} }
class Svc {
  void run() throws Exception {
    { Order o = new Order(); o.a(); }
    try (Cust o = new Cust()) { o.b(); }
  }
}
`},
		{"instanceof pattern variable", `package com.x;
class Order { void a() {} }
class Cust  { void b() {} }
class Svc {
  void run(Object x) {
    { Order o = new Order(); o.a(); }
    if (x instanceof Cust o) { o.b(); }
  }
}
`},
		{"switch type pattern", `package com.x;
class Order { void a() {} }
class Cust  { void b() {} }
class Svc {
  void run(Object x) {
    { Order o = new Order(); o.a(); }
    switch (x) { case Cust o -> o.b(); default -> {} }
  }
}
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rels := j7094Calls(t, tc.src)
			// `o` disagrees within ONE class scope, so BOTH sites are
			// refused and fall back to the bare leaf. Unchanged by #7109.
			j7094MustNotCall(t, rels, "Order.a", "Cust.b", "Order.b", "Cust.a")
			j7094MustCall(t, rels, "a", "b")
		})
	}
}

// A distinct name in the nested body is not a collision on either side of the
// boundary: each binds its own type. The never-fires direction for the
// collision machinery, INSIDE a class scope.
func TestJava7109_DistinctNamesBindOnBothSidesOfTheBoundary(t *testing.T) {
	rels := j7094Calls(t, `package com.x;
class Order { void a() {} }
class Cust  { void b() {} }
interface Go { void go(Cust c); }
class Svc {
  void run() {
    Order o = new Order();
    o.a();
    Go g = new Go() { public void go(Cust c) { c.b(); } };
  }
}
`)
	j7094MustCall(t, rels, "Order.a", "Cust.b")
	j7094MustNotCall(t, rels, "Order.b", "Cust.a")
}

// STANDALONE — the nested member with NO outer sibling at all.
//
// THIS ROW CHANGES, and the change is asserted rather than tolerated. The
// grading note for this issue asked that "the nested-class member alone must
// behave exactly as it does today"; MEASURED at 83004cbef it emitted the BARE
// leaf `b`, because a nested formal_parameter was in no ledger arm and so
// typed nothing. Direction 2 as specified — "that body gets its own ledger
// seeded from its own parameters and locals, reusing the machinery
// collectParamTypes already has" — necessarily types it. The two requirements
// are not jointly satisfiable, and the scope instruction is the one with a
// stated mechanism, so the standalone case is a RECALL ADDITION here: `Cust.b`.
func TestJava7109_StandaloneNestedMemberNowTypesItsOwnParam(t *testing.T) {
	anon := j7094Calls(t, `package com.x;
class Cust  { void b() {} }
interface Go { void go(Cust c); }
class Svc {
  void run() {
    Go g = new Go() { public void go(Cust o) { o.b(); } };
  }
}
`)
	// At 83004cbef: bare `b`. Now typed by the nested body's own ledger.
	j7094MustCall(t, anon, "Cust.b")

	local := j7094Calls(t, `package com.x;
class Cust  { void b() {} }
class Svc {
  void run() {
    class Inner { void go(Cust o) { o.b(); } }
  }
}
`)
	j7094MustCall(t, local, "Cust.b")
}
