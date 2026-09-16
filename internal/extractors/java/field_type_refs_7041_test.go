package java_test

// #7041 arm JAVA — type parameters are a SHADOWING SCOPE for the
// field→declared-type edge. This file replaces
// TestJavaFieldTypeRefs_KnownOverFire_TypeParameterShadowedByASameFileType,
// which pinned the over-fire this closes.
//
// WHAT THE DELETED PIN DID THAT THIS FILE STILL DOES. That pin carried a
// positive control — a second fixture with no colliding `class T`, asserting
// the edge was absent there — so it could tell "the over-fire is still here"
// from "the producer stopped working entirely". EVERY fixture below carries
// that control and a stronger one: each source declares a REAL same-file type
// whose field edge MUST be present in the same `want` set. A pass that stopped
// emitting altogether fails every table here.
//
// THREE CONTROL KINDS, AND THEY ARE NOT INTERCHANGEABLE. Review found this
// header claiming that "a pass that refuses too much fails a named control row"
// — true at the class-field anchor, FALSE at the record-component anchor,
// because the rows filed there graded a different property. Every "control" row
// in this file is now classified by WHAT IT VARIES, not by where it sits:
//
//   - LIVENESS. Names a type that is NEVER a parameter in its file
//     (`Box.ok => Order`). Says the producer still runs at that anchor / in
//     that form. Says NOTHING about scoping. This is the deleted pin's `H2.java`
//     idea and it is the WEAKEST of the three.
//   - SCOPING. Names a type that IS bound as a parameter somewhere in the same
//     file, at a position where it is NOT in scope, so the edge must be KEPT:
//     `Plain.t => T`, `InnerFlatN.ft => TN`, `SiblingN.st => TN`, `pf => TN`,
//     `PlainP.t => TP`, `PlainRP.t => TP`. Only these grade whether the shadow
//     set is scoped to the field's POSITION.
//   - BOUND-HARVESTING. Names a type that appears INSIDE a `type_parameters`
//     subtree without being a parameter name — a bound: `Bounded.ok => Order`,
//     `Multi.a => A`, `Multi.b => B`, `RecD.ok => OrderD`. These grade that the
//     name extraction takes DIRECT children of `type_parameter` only.
//
// The last two are the over-refusal direction — the one #7056 established no
// instrument we own can see. A control row grades the property it VARIES, not
// the property it is filed under.
//
// AUDIT OF THE EMIT-SITE AXIS, done after the review finding rather than
// assumed. This pass has exactly two emit sites for a SCOPE.Schema/field
// record: buildField (java.go:1527 — class fields AND anonymous-class fields)
// and walk's record header component (java.go:492). Both now carry a SCOPING
// control (`Plain.t`/`InnerFlatN.ft`/`SiblingN.st`/`pf`/`PlainP.t`, and
// `PlainRP.t`) AND a BOUND-HARVESTING control (`Bounded.ok`/`Multi.a`/`Multi.b`,
// and `RecD.ok`, whose record was given a bound for exactly that reason). The
// identical anchor-gated "descend from the file root" mutant is DEAD at both:
// at the class field it drops five named rows, at the record component it drops
// exactly `PlainRP.t => TP`.
//
// JAVA'S SCOPING RULE, DERIVED FROM THE JLS AND VERIFIED WITH javac 25.0.3
// (evidence in the PR body; each claim below was compiled, not recalled):
//
// JLS 8.1.2 puts a class type parameter's scope over the WHOLE body of the
// declaration that introduces it, and JLS 6.4.1 makes that a SHADOW: inside
// `class Box<T>`, the name `T` denotes the parameter and NOT a same-file
// `class T`. The static-context restriction (JLS 8.1.2: it is a compile-time
// error to refer to a type parameter of C in a static member or static nested
// class of C) does NOT reopen the outer name — javac rejects the reference
// outright:
//
//	class P {}
//	class Box<P> { static class Nested { P f; } }
//	  → error: non-static type variable P cannot be referenced from a static
//	    context                                    (NOT: resolves to class P)
//
// THIS IS WHY JAVA'S RULE IS NOT KOTLIN'S AND NOT SCALA'S. Kotlin's arm
// (kotlinVisibleTypeParameterNames) ascends ONLY through `inner` declarations,
// because a plain nested Kotlin class genuinely does not see the outer
// parameter and `T` there really does name a same-file `class T`. Scala nests
// only within instances and always captures. Java looks like Kotlin — it has
// both `static` and inner nesting — but the SHADOW is not conditioned on
// staticness at all; only the LEGALITY of the reference is. So Java ascends
// UNCONDITIONALLY through every enclosing declaration that introduces type
// parameters, and the static forms below are graded on non-compiling input
// precisely because non-compiling is what javac says they are.

import "testing"

// ---------------------------------------------------------------------------
// TABLE 1 — THE TYPE-PARAMETER FORM SPACE.
//
// VARIED: the shape of the type_parameters list — arity (1, 2), a bound
// (`extends Order`), an intersection bound (`extends A & B`), a recursive
// bound (`extends Comparable<T>`), a TYPE_PARAMETER annotation, and the
// position the parameter is USED in (bare, array, generic argument, wildcard
// argument, qualified-generic argument). Plus the no-parameter case.
//
// HELD CONSTANT (and attacked by the other tables in this file): the declaring
// form is always `class`; the anchor is always a class `field_declaration`; the
// file is one path; the colliding name is always `T` except where arity
// demands `K`/`V`; the control type is always `Order`.
//
// EVERY ROW IS BOTH DIRECTIONS. The parameter-typed field must produce NO
// edge; a named field typed by a REAL same-file declaration in the SAME class
// must produce one. `Plain.t => T` is the over-refusal control for the whole
// table: `class T` is a real type and a NON-generic class referring to it must
// keep its edge.
//
// THIS SOURCE COMPILES (javac 25.0.3, rc=0). The bound types `Order`, `A`, `B`
// are real declarations, so a collector that harvested BOUND names instead of
// PARAMETER names would delete `Bounded.ok`, `Multi.a` and `Multi.b`.
const javaFT7041FormsPath = "Forms.java"

const javaFT7041FormsSrc = `import java.lang.annotation.ElementType;
import java.lang.annotation.Target;

class Order {}
class A {}
interface B {}
class T {}
class K {}
class V {}

@Target(ElementType.TYPE_PARAMETER) @interface NonNull {}

class Box<T> { T item; Order ok; }
class Pair<K, V> { K key; V val; Order ok; }
class Bounded<T extends Order> { T t; Order ok; }
class Multi<T extends A & B> { T t; A a; B b; }
class Recur<T extends Comparable<T>> { T t; Order ok; }
class Anno<@NonNull T> { T t; Order ok; }
class Wrapped<T> {
  T[] arr;
  java.util.List<T> list;
  java.util.List<? extends T> wild;
  java.util.List<? extends Order> okWild;
}
class Plain { T t; Order ok; }
`

func TestJavaFieldTypeRefs_7041_TypeParameterFormSpace(t *testing.T) {
	recs := extractJavaFT(t, map[string]string{javaFT7041FormsPath: javaFT7041FormsSrc})
	const f = javaFT7041FormsPath + ":"
	javaFTWantEqual(t, javaFTEdges(recs), []string{
		// REFUSED (no row): Box.item, Pair.key, Pair.val, Bounded.t,
		// Multi.t, Recur.t, Anno.t, Wrapped.arr, Wrapped.list,
		// Wrapped.wild — every one is a type parameter of its own
		// declaring class, and every one of those names IS declared in
		// this file, so each would bind before this fix.
		f + "Box.ok => Order",
		f + "Pair.ok => Order",
		// The BOUND is not the parameter: `T extends Order` refuses T and
		// keeps Order addressable.
		f + "Bounded.ok => Order",
		// An intersection bound names two real same-file types; both stay
		// addressable in field position.
		f + "Multi.a => A",
		f + "Multi.b => B",
		// A recursive bound mentions its own parameter inside the bound;
		// the name collected is still only the parameter's.
		f + "Recur.ok => Order",
		// A TYPE_PARAMETER annotation sits before the name in the CST;
		// the name is still found and the annotation is not a candidate.
		f + "Anno.ok => Order",
		// A wildcard's bound is a real type and still binds, while the
		// same position holding the parameter does not.
		f + "Wrapped.okWild => Order",
		// THE OVER-REFUSAL CONTROL. `Plain` declares no parameters, so
		// `T` here really is `class T` and the edge MUST survive.
		f + "Plain.t => T",
		f + "Plain.ok => Order",
	})
}

// ---------------------------------------------------------------------------
// TABLE 2 — THE DECLARING FORM, which table 1 holds constant at `class`.
//
// VARIED: the form that CARRIES the type parameters and the ANCHOR that
// consumes them — a `class` with a field_declaration, and a `record` with a
// header component (walk's record_declaration arm is a completely separate
// emit site, so a fix wired only into buildField passes table 1 whole).
//
// HELD CONSTANT: one parameter named `TD`, one control type `OrderD`, one file
// — AND, stated plainly because a review found this table implying otherwise,
// THE DIRECTION. This table varies the anchor for the REFUSAL direction only
// (`BoxD.item`, `RecD.item`). Its `OrderD` rows are LIVENESS controls: `OrderD`
// is never a type parameter in this file, so they say the producer still runs
// at each anchor — they say nothing about whether the shadow set is SCOPED to
// the field's position. The over-refusal direction at the record anchor is
// varied in TABLE 5 (`PlainRP.t => TP`), which is where the missing grader was
// added; do not read this table as covering it.
//
// THE FORMS THAT CANNOT APPEAR ARE STATED, NOT OMITTED. javac 25.0.3:
//
//	enum ED<P> { X }            → error: enums cannot be generic
//	@interface AnnD<P> { … }    → error: annotation interface cannot be generic
//
// so `enum` and `@interface` have no generic+field combination to grade at
// all. A generic INTERFACE can be written, but an interface constant emits no
// field entity in this extractor (the recall ceiling stated in
// field_type_refs_6912_test.go's header), so `IFD.CONST` produces no row in
// either direction — asserted here by its absence from `want` rather than left
// unexamined.
//
// THIS SOURCE COMPILES (javac 25.0.3, rc=0).
func TestJavaFieldTypeRefs_7041_DeclaringFormSpace(t *testing.T) {
	const path = "Decls.java"
	recs := extractJavaFT(t, map[string]string{path: `class OrderD {}
class TD {}
class BoxD<TD> { TD item; OrderD ok; }
record RecD<TD extends OrderD>(TD item, OrderD ok) {}
interface IFD<TD> { OrderD CONST = null; }
enum ED { X }
@interface AnnD { String value(); }
`})
	const f = path + ":"
	javaFTWantEqual(t, javaFTEdges(recs), []string{
		// class anchor: parameter refused, LIVENESS control kept.
		f + "BoxD.ok => OrderD",
		// RECORD HEADER COMPONENT anchor — the second emit site.
		// RecD.item is the parameter and must be refused there too.
		// `RecD` was given the bound `<TD extends OrderD>` in review so
		// this row is also the BOUND-HARVESTING control at the record
		// anchor: `OrderD` sits inside the `type_parameters` subtree
		// without being a parameter name, so a collector taking
		// DESCENDANTS of `type_parameter` deletes this edge here as well
		// as the three at the class anchor in table 1.
		f + "RecD.ok => OrderD",
		// IFD.CONST: no field entity exists, so no row in either
		// direction. ED and AnnD cannot be generic at all.
	})
}

// ---------------------------------------------------------------------------
// TABLE 3 — NESTING, COMPILING FORMS.
//
// VARIED: where the field sits relative to the declaration that introduces the
// parameter — the declaring class itself, an inner class, an anonymous class in
// an instance-field initializer, an inner class that REDECLARES the name, an
// inner class with its OWN differently-named list that still uses the outer's,
// a generic inner class of a NON-generic outer, a non-generic inner class of a
// non-generic outer, and a SIBLING declaration outside the generic class.
//
// HELD CONSTANT: the parameter name is always `TN`, the control type always
// `OrderN`, one file, `class` throughout.
//
// THE SCOPING CONTROLS ARE `InnerFlatN.ft`, `SiblingN.st` AND `pf`. All three
// sit OUTSIDE any declaration that binds `TN`, so `TN` there really is
// `class TN` and the edges MUST survive — javac agrees (rc=0). An ascent that
// ran past the declaration boundary, or that refused a name globally once it
// had seen it anywhere in the file, deletes exactly those three rows.
//
// `pf` WAS ADDED IN REVIEW and is the anonymous-class-field twin of the other
// two. Before it, the only anonymous-anchor row was `aok => OrderN`, whose type
// is never a parameter in this file — so it graded producer LIVENESS at that
// anchor and not the SCOPING of the shadow set, the same conflation the review
// found at the record anchor (see table 5). Unlike the record component, an
// anonymous class's field goes through the SAME emit site as a class field
// (buildField, java.go:1527), so this row is not an independent grader of a
// third call site; what it does grade is that the ascent crosses an
// `object_creation_expression` + `class_body` boundary and still stops at the
// enclosing declaration — `PlainOuterN` binds nothing, so `pf` keeps its edge
// while `af`, in the identical position inside `OuterN<TN>`, does not.
//
// THIS SOURCE COMPILES (javac 25.0.3, rc=0) — including the anonymous class in
// the instance-field initializer and the inner class that shadows `TN` again.
func TestJavaFieldTypeRefs_7041_NestingFormSpace_Compiling(t *testing.T) {
	const path = "NestOk.java"
	recs := extractJavaFT(t, map[string]string{path: `class OrderN {}
class TN {}
class OuterN<TN> {
  TN own;
  OrderN ok;
  Runnable anon = new Runnable() { TN af; OrderN aok; public void run() {} };
  class InnerN { TN it; OrderN iok; }
  class RedeclareN<TN> { TN rt; OrderN rok; }
  class BothN<UN> { TN bt; UN bu; OrderN bok; }
}
class PlainOuterN {
  Runnable panon = new Runnable() { TN pf; OrderN pok; public void run() {} };
  class InnerGenN<TN> { TN gt; OrderN gok; }
  class InnerFlatN { TN ft; OrderN fok; }
}
class SiblingN { TN st; OrderN sok; }
`})
	const f = path + ":"
	javaFTWantEqual(t, javaFTEdges(recs), []string{
		// The declaring class itself: OuterN.own refused.
		f + "OuterN.ok => OrderN",
		// An ANONYMOUS class in an instance-field initializer does emit
		// field entities in this extractor, with BARE names (no owner).
		// It is lexically inside OuterN, so TN is the parameter there:
		// `af` refused, `aok` kept (LIVENESS at this anchor).
		f + "aok => OrderN",
		// SCOPING at the anonymous anchor — the same position inside a
		// NON-generic outer. `pf` MUST keep its edge; `pok` is the
		// liveness control beside it.
		f + "pf => TN",
		f + "pok => OrderN",
		// INNER class of a generic class — captures, per JLS 8.1.2, and
		// javac compiles it. `it` refused.
		f + "InnerN.iok => OrderN",
		// An inner class REDECLARING the name: `rt` is RedeclareN's own
		// parameter. Refused by the immediate list alone, but the row
		// that matters is the control beside it.
		f + "RedeclareN.rok => OrderN",
		// AN INNER CLASS WITH ITS OWN, DIFFERENTLY-NAMED LIST that still
		// uses the OUTER's parameter. This row exists because the
		// redeclaring row above does NOT grade the ascent: there, the
		// immediate list already carries `TN`, so an implementation that
		// stopped at the first non-empty list would pass it. `BothN.bt`
		// is the input that separates them — a mutant returning as soon
		// as it has collected anything survives the whole table without
		// it (scored ALIVE, then graded).
		f + "BothN.bok => OrderN",
		// A generic inner class of a NON-generic outer: `gt` refused by
		// its own list.
		f + "InnerGenN.gok => OrderN",
		// CONTROL — a non-generic inner class of a non-generic outer, in
		// a file where `TN` is used as a parameter elsewhere. The edge
		// MUST survive.
		f + "InnerFlatN.ft => TN",
		f + "InnerFlatN.fok => OrderN",
		// CONTROL — a top-level sibling of the generic class.
		f + "SiblingN.st => TN",
		f + "SiblingN.sok => OrderN",
	})
}

// ---------------------------------------------------------------------------
// TABLE 4 — NESTING, STATIC-CONTEXT FORMS. THIS SOURCE DOES NOT COMPILE, AND
// THAT IS THE FINDING.
//
// Java is the language where a reader expects the Kotlin answer: a `static`
// nested class does not see the outer class's type parameters, so `T` inside
// one should mean the same-file `class T` and the edge should be KEPT.
// **That is wrong, and javac 25.0.3 says so.** Each form below was compiled in
// isolation with a top-level type of the parameter's name present:
//
//	class P {}
//	class Box<P> { static class Nested { P f = new P(); } }
//	  → error: non-static type variable P cannot be referenced from a static
//	    context                                              (twice: type and ctor)
//
//	class P2 {} class Box<P2> { record R(P2 x) {} }        → same error
//	class P3 {} interface Box<P3> { class C { P3 f; } }    → same error
//	class P4 {} class Box<P4> { static P4 f; }             → same error
//	           class Box<P>  { class Mid { static class DeepStatic { P g; } } }
//	                                                        → same error
//
// The parameter still SHADOWS (JLS 6.4.1); only the reference is illegal. So
// there is no valid program in which these names denote the same-file type, and
// refusing them deletes no correct edge. Java therefore ascends
// UNCONDITIONALLY — no `static` check, no implicitly-static carve-out for
// nested records/interfaces/enums — and this table pins that the extractor's
// answer on such input is refusal rather than a wrong binding.
//
// VARIED — FOUR forms, one per fixture declaration, counted against the source
// rather than described from memory: an explicit `static` nested class
// (`NestedS`), a nested record (implicitly static, JLS 8.10.1 — `NRecS`), an
// inner class nested inside a `static` nested class (`DeepS.DeeperS`), and a
// member class of a generic interface (implicitly static, JLS 9.5 — `CInIS`).
// An earlier revision of this comment listed FIVE, adding the inner-then-static
// shape (`class Mid { static class Deep { … } }`, javac row 6 above) which is
// NOT in the fixture. It is refused by the same union ascent and is not
// separately distinguishable, so the label was claiming more than the content
// rather than hiding a gap — corrected here rather than papered over.
//
// HELD CONSTANT: the parameter name `TS`, the control type `OrderS`, one file,
// and — stated, not implied — THE DIRECTION. The `OrderS` rows are LIVENESS
// controls only: `OrderS` is never a parameter in this file, so they prove the
// producer still runs inside every one of these forms (a row's absence means
// refusal, not silence) and nothing more. The SCOPING direction for the class
// and record anchors is graded in tables 3 and 5.
func TestJavaFieldTypeRefs_7041_NestingFormSpace_StaticContext(t *testing.T) {
	const path = "NestStatic.java"
	recs := extractJavaFT(t, map[string]string{path: `class OrderS {}
class TS {}
class OuterS<TS> {
  static class NestedS { TS ns; OrderS nok; }
  record NRecS(TS nr, OrderS nrok) {}
  static class DeepS { class DeeperS { TS ds; OrderS dok; } }
}
interface IFaceS<TS> { class CInIS { TS ci; OrderS cok; } }
`})
	const f = path + ":"
	javaFTWantEqual(t, javaFTEdges(recs), []string{
		f + "NestedS.nok => OrderS",
		f + "NRecS.nrok => OrderS",
		f + "DeeperS.dok => OrderS",
		f + "CInIS.cok => OrderS",
	})
}

// ---------------------------------------------------------------------------
// TABLE 5 — THE POSITION OF THE COLLIDING DECLARATION, and THE EMIT SITE OF
// THE OVER-REFUSAL CONTROL.
//
// VARIED, axis 1: the colliding `class TP` is declared AFTER the generic class
// that shadows its name, and after the declarations that legitimately reference
// it. This is the axis the two-pass design (stash at walk, attach after) exists
// for, and holding it constant everywhere else would leave it ungraded.
//
// VARIED, axis 2 — ADDED IN REVIEW, and the reason this table is no longer
// "class anchor" HELD CONSTANT: the SCOPING direction is graded at BOTH emit
// sites. `PlainRP.t => TP` is a RECORD HEADER COMPONENT (java.go:492) naming a
// type parameter that is bound elsewhere in the file but NOT in this record's
// ancestry, so the edge must be KEPT — the record-anchor twin of
// `PlainP.t => TP`.
//
// WHY THAT ROW EXISTS AND WHAT WAS WRONG WITHOUT IT. Table 2 varies the anchor
// for the REFUSAL direction (`RecD.item`, and `NRecS.nr` in table 4) and, before
// this row, held it constant for the OVER-REFUSAL direction: every row grading
// the SCOPING of the shadow set — a name that IS a parameter somewhere in the
// file whose edge must nonetheless survive — sat on a class field. The two
// record-anchor rows that looked like controls (`RecD.ok`, `NRecS.nrok`) name
// types that are never a parameter in their files, so they grade producer
// LIVENESS, not scoping. Different property. A reviewer applied the identical
// "descend from the file root instead of ascending from this field" mutation at
// each anchor: DEAD at the class field (four named rows), ALIVE at the record
// component — a correct edge silently deleted with the whole suite green, the
// direction #7056 says no instrument we own can see. A control row grades the
// property it VARIES, not the property it is filed under.
//
// HELD CONSTANT: single parameter name `TP`, one file, control `OrderP`, and
// the colliding declaration's kind (`class`).
//
// THIS SOURCE COMPILES (javac 25.0.3, rc=0) — Java has no forward-declaration
// requirement, and a record component may name a top-level type declared later.
func TestJavaFieldTypeRefs_7041_CollidingDeclarationComesLast(t *testing.T) {
	const path = "Later.java"
	recs := extractJavaFT(t, map[string]string{path: `class BoxP<TP> { TP item; OrderP ok; }
record BoxRP<TP>(TP item, OrderP ok) {}
class PlainP { TP t; OrderP ok; }
record PlainRP(TP t, OrderP ok) {}
class TP {}
class OrderP {}
`})
	const f = path + ":"
	javaFTWantEqual(t, javaFTEdges(recs), []string{
		// REFUSED at both anchors: BoxP.item (class field) and
		// BoxRP.item (record component) are their declaration's own
		// parameter, and `class TP` — declared LAST — would have been
		// their target.
		f + "BoxP.ok => OrderP",
		f + "BoxRP.ok => OrderP",
		// SCOPING CONTROL, class field anchor.
		f + "PlainP.t => TP",
		f + "PlainP.ok => OrderP",
		// SCOPING CONTROL, RECORD COMPONENT anchor. `PlainRP` declares
		// no parameters, so `TP` here is `class TP` and the edge MUST
		// survive. This is the row the review's MA mutant deletes.
		f + "PlainRP.t => TP",
		f + "PlainRP.ok => OrderP",
	})
}
