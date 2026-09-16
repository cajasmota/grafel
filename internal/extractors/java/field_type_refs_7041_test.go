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
// emitting altogether fails every table here, and a pass that refuses too much
// fails the named control row rather than silently deleting an edge — which is
// the direction #7056 established no instrument we own can see.
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
// HELD CONSTANT: one parameter named `TD`, one control type `OrderD`, one file.
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
record RecD<TD>(TD item, OrderD ok) {}
interface IFD<TD> { OrderD CONST = null; }
enum ED { X }
@interface AnnD { String value(); }
`})
	const f = path + ":"
	javaFTWantEqual(t, javaFTEdges(recs), []string{
		// class anchor: parameter refused, control kept.
		f + "BoxD.ok => OrderD",
		// RECORD HEADER COMPONENT anchor — the second emit site.
		// RecD.item is the parameter and must be refused there too.
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
// THE CONTROLS ARE THE LAST THREE ROWS. `InnerFlatN.ft` and `SiblingN.st` are
// OUTSIDE any declaration that binds `TN`, so `TN` there really is `class TN`
// and the edges MUST survive — javac agrees (a sibling class referring to the
// top-level type compiles, rc=0). An ascent that ran past the declaration
// boundary, or that refused a name globally once it had seen it anywhere in the
// file, deletes exactly those two rows.
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
		// It is lexically inside OuterN, so TN is the parameter there.
		f + "aok => OrderN",
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
// VARIED: the static-context form — an explicit `static` nested class, a nested
// record (implicitly static, JLS 8.10.1), a member class of a generic interface
// (implicitly static, JLS 9.5), a static class nested two levels down, and a
// nested class inside a static nested class.
//
// HELD CONSTANT: the parameter name `TS`, the control type `OrderS`, one file.
//
// The controls are the `OrderS` rows: they prove the producer still runs inside
// every one of these forms, so a row's absence means refusal and not silence.
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
// TABLE 5 — THE POSITION OF THE COLLIDING DECLARATION, which every table above
// holds constant at "before the generic one".
//
// VARIED: the colliding `class TP` is declared AFTER the generic class that
// shadows its name, and after the plain class that legitimately references it.
// This is the axis the two-pass design (stash at walk, attach after) exists
// for, and holding it constant everywhere else would leave it ungraded.
//
// HELD CONSTANT: single parameter, class anchor, one file, control `OrderP`.
//
// THIS SOURCE COMPILES (javac 25.0.3, rc=0) — Java has no forward-declaration
// requirement.
func TestJavaFieldTypeRefs_7041_CollidingDeclarationComesLast(t *testing.T) {
	const path = "Later.java"
	recs := extractJavaFT(t, map[string]string{path: `class BoxP<TP> { TP item; OrderP ok; }
class PlainP { TP t; OrderP ok; }
class TP {}
class OrderP {}
`})
	const f = path + ":"
	javaFTWantEqual(t, javaFTEdges(recs), []string{
		f + "BoxP.ok => OrderP",
		f + "PlainP.t => TP",
		f + "PlainP.ok => OrderP",
	})
}
