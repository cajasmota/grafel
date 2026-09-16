package csharp_test

import (
	"sort"
	"strings"
	"testing"
)

// #7041 arm A (C#) — a type parameter is a SHADOWING SCOPE for the field→
// declared-type pass. See field_type_refs.go (csVisibleTypeParameterNames) for
// the spec-derived rule, what the CST dump established, and what could NOT be
// verified on a machine with no C# compiler.
//
// ────────────────────────────────────────────────────────────────────────────
// WHAT EVERY TABLE IN THIS FILE VARIES, AND WHAT IT HOLDS CONSTANT
// ────────────────────────────────────────────────────────────────────────────
//
// Three arms of #7041 shipped a table whose held-constant axis was the one that
// mattered — kotlin held the nested declaration's modifier, java held the ANCHOR
// (every scoping row was a class field; the two record rows named types that are
// never parameters, so they graded producer liveness), rust held CANDIDATE
// MULTIPLICITY (every shadowed field had exactly one candidate, the refused one,
// which is the single shape where "drop the refused candidate" and "drop the
// whole field" are the same function). An axis a block does not name is an axis
// nobody audits. So they are named:
//
//	TestCsharpFieldTypeRefs_7041_ParameterFormSpace
//	  VARIES          the FORM of the parameter declaration — plain (G1),
//	                  two-name (G2), constrained (G3), multi-constrained (G4),
//	                  variance-annotated (G5), attributed (G9, G13); the
//	                  DECLARATION KIND carrying the list — class (G1), struct
//	                  (G6), interface (G5), record (G7, G11); the ANCHOR —
//	                  property (G1), field (G6.A, PlainS.X), record positional
//	                  (G7, G11, PlainR.X); CANDIDATE MULTIPLICITY — one
//	                  candidate (G1.Shadowed), refused-first (G12.Pair),
//	                  refused-middle (G10.M, G11.M), refused-last (G10.R,
//	                  G11.R); and the field-type SYNTAX the candidate sits in —
//	                  bare (G1), nullable (G8), array (G12.Arr), tuple
//	                  (G12.Pair), generic argument (G10, G11).
//	  HELD CONSTANT   nesting depth — every declaration in this file is
//	                  top-level, so EVERY binder here is the field's OWN
//	                  declaring type; the ascent is the whole of the next table,
//	                  and multiplicity is varied there too for that reason.
//	                  Namespace — one file-scoped namespace; namespace scope is
//	                  a SEPARATE known over-fire, still pinned.
//	                  Generic-CONSTRUCTOR position is varied only as a KEPT
//	                  candidate (`Dict` in G10/G11): a type parameter in
//	                  constructor position is the omitted illegal form, named in
//	                  field_type_refs.go rather than guessed at.
//
//	TestCsharpFieldTypeRefs_7041_NestingFormSpace
//	  VARIES          nesting DEPTH (1 and 2 levels), whether the nested
//	                  declaration carries its OWN non-empty list, the nested
//	                  declaration KIND (class · record), the `static` modifier,
//	                  the DIRECTION (ancestor-generic → refuse;
//	                  non-ancestor-generic → keep), the ANCHOR (property ·
//	                  field · record positional) and — added after review —
//	                  CANDIDATE MULTIPLICITY, at four rows and three anchors:
//	                  Inner1.Mm (refused MIDDLE, property, depth 1), Inner1.Pf
//	                  (refused FIRST, field, depth 1), Deep2.Ml (refused LAST,
//	                  property, depth 2) and R6.Mr (refused MIDDLE, RECORD
//	                  anchor, depth 1).
//	  HELD CONSTANT   parameter form — every list in this file is plain and
//	                  unconstrained, which is the previous table's axis.
//
// AN UNMET CLAIM IN THIS BLOCK IS WORSE THAN AN UNNAMED AXIS. The revision
// reviewed on #7075 said of the nesting table that candidate multiplicity was
// held constant "at most rows; the two rows that vary it are marked" — AND NO
// SUCH ROWS EXISTED. All 17 field anchors had a single bare-identifier type, so
// along the ascent "drop the refused candidate" and "drop the whole field" were
// still the same function, and mutant MX was ALIVE with both suites green. The
// block exists so a reader can tell what is graded without re-deriving it; a
// block that asserts coverage it does not have turns the audit tool into the
// thing needing an audit. Every axis named above now points at rows that exist.
//
// EVERY row is classified below by WHAT IT VARIES, under three kinds:
//
//	[REFUSE]    the field names a type parameter in scope → NO edge. Grades the
//	            permissive direction (the defect this issue is about).
//	[KEEP]      the field names a REAL same-file type at a position where that
//	            name is NOT bound as a parameter → edge MUST survive. Grades the
//	            over-refusal direction, which has NO symptom (#7056).
//	[LIVE]      the field names a real same-file type whose name is never a
//	            parameter anywhere in the file. Grades producer LIVENESS only —
//	            it does NOT grade scoping, and is never counted as a control for
//	            it. (This is java's finding: a control row grades the property it
//	            VARIES, not the property it is filed under.)
//
// NO C# COMPILER EXISTS ON THIS MACHINE (`csc`, `dotnet`, `mono`, `mcs` all
// absent). Rows whose LEGALITY is load-bearing are marked UNVERIFIED-LEGALITY
// with the reason; forms whose legality is in doubt and whose behaviour would be
// load-bearing are NOT written at all and are named as omissions in
// field_type_refs.go. EXACTLY TWO rows below rest on a legality that is not
// demonstrated — `Sn4` (a `static` nested class inside a generic one) and `G13`
// (a same-file attribute class applied to a type parameter) — and both are
// marked at the row, named in field_type_refs.go's UNVERIFIED block, and safe
// in either branch. EVERY OTHER form below is a plain generic
// class/struct/record/interface with a type parameter shadowing a same-file
// type, whose legality is not in doubt. (An earlier revision of this sentence
// said "every form", which the two marked rows contradict.)

const ft7041FormPath = "P.cs"

// ft7041FormSrc — the parameter-form space. `Order` is the name bound as a type
// parameter in most declarations; `Real`, `IThing`, `Keep`, `Dict` and `Mark`
// are ordinary same-file types that must stay bindable everywhere. (`Dict` is
// itself generic: its own `A`/`B` collide with nothing, and it is the KEPT
// candidate in generic-CONSTRUCTOR position for the multiplicity rows.)
const ft7041FormSrc = `namespace App;

public class Order { public string N { get; set; } }
public class Real { public string N { get; set; } }
public interface IThing { }
public class Keep { public string N { get; set; } }
public class Dict<A, B> { }

public class G1<Order> { public Order Shadowed { get; set; } public Real Ok { get; set; } }

public class G2<Order, Real> { public Order A { get; set; } public Real B { get; set; } public IThing C { get; set; } }

public class G3<T> where T : Order { public T A { get; set; } public Order B { get; set; } }

public class G4<T> where T : class, IThing, new() { public T A { get; set; } public IThing B { get; set; } }

public interface G5<in Order, out Real> { Order A { get; set; } Real B { get; set; } }

public struct G6<Order> { public Order A; public Real B; }

public record G7<Order>(Order A, Real B);

public class G8<Order> { public Order? A { get; set; } public Real? B { get; set; } }

public class G9<[System.Obsolete] Order> { public Order A { get; set; } public Real B { get; set; } }

public class G10<Order> { public Dict<Order, Real> M { get; set; } public Dict<Real, Order> R { get; set; } }

public record G11<Order>(Dict<Order, Real> M, Dict<Real, Order> R);

public class G12<Order> { public (Order, Real) Pair { get; set; } public Order[] Arr { get; set; } }

public class Mark : System.Attribute { }

public class G13<[Mark] Order> { public Order A { get; set; } public Mark M { get; set; } }

public class Plain { public Order X { get; set; } public Real Y { get; set; } }

public struct PlainS { public Order X; }

public record PlainR(Order X, Keep Y);
`

// TestCsharpFieldTypeRefs_7041_ParameterFormSpace is an INDEPENDENT LITERAL set
// (#6975): every surviving edge is spelled out, so an over-refusal that deletes
// a correct edge fails here by a NAMED missing row rather than by a count.
//
// PER-ROW CLASSIFICATION — what each row varies and which direction it grades.
// The rows below are the edges that must EXIST; the refused ones are listed in
// the absent table underneath, because an assertion about an edge that is gone
// cannot be written as a row of the want list.
//
//	EDGE                     KIND     VARIES
//	G1.Ok                    [LIVE]   —  (Real is never a parameter: liveness)
//	G2.C                     [LIVE]   —  (IThing is never a parameter)
//	G3.A                     [REFUSE] parameter with a constraint naming a REAL
//	                                  same-file type; `T` refused
//	G3.B                     [KEEP]   the CONSTRAINT's type `Order` — `Order` IS
//	                                  a parameter name elsewhere in this file,
//	                                  and here it is the constrained-to type. It
//	                                  must bind. Structurally it cannot be
//	                                  harvested (constraints live in a SIBLING
//	                                  type_parameter_constraints_clause, never in
//	                                  type_parameter_list) — this row is what
//	                                  proves that rather than asserting it.
//	G4.B                     [KEEP]   multi-constraint `class, IThing, new()`
//	G5.A / G5.B              [REFUSE] VARIANCE annotation (`in` / `out`)
//	G6.A                     [REFUSE] struct anchor + FIELD anchor
//	G6.B                     [KEEP]   struct anchor, unshadowed
//	G7.A                     [REFUSE] record anchor (positional parameter)
//	G7.B                     [KEEP]   record anchor, unshadowed
//	G8.A                     [REFUSE] nullable `Order?`
//	G8.B                     [KEEP]   nullable, unshadowed
//	G9.A                     [REFUSE] ATTRIBUTED parameter — the form that cost
//	                                  the scala arm its guard
//	G9.B                     [KEEP]   attributed-parameter declaration, unshadowed
//	G13.A                    [REFUSE] parameter attributed with a SAME-FILE
//	                                  attribute class
//	G13.M                    [KEEP]   THE ATTRIBUTE'S OWN NAME in field position.
//	                                  `Mark` is written inside G13's
//	                                  type_parameter_list, as `[Mark]`. A
//	                                  collector that walked the list's DESCENDANT
//	                                  identifiers instead of reading each
//	                                  type_parameter's `name` FIELD would harvest
//	                                  `Mark` as a shadowed name and delete this
//	                                  edge — the silent direction, and the exact
//	                                  shape that cost the scala arm its guard.
//	                                  G9's `[System.Obsolete]` cannot grade that:
//	                                  neither `System` nor `Obsolete` is declared
//	                                  in the file, so harvesting them is
//	                                  unobservable. UNVERIFIED-LEGALITY: `class
//	                                  Mark : System.Attribute` with `[Mark]` on a
//	                                  type parameter is legal by the default
//	                                  AttributeUsage (all targets) and the
//	                                  `…Attribute`-suffix-optional lookup rule,
//	                                  but no compiler here demonstrates it. As
//	                                  with Sn4 the assertion is safe either way —
//	                                  refusing `Order` is right if legal, and
//	                                  costless if not — and the KEEP half only
//	                                  gets stronger if the form is legal.
//	G10.M / G10.R            [REFUSE+KEEP] MULTIPLICITY: three candidates
//	                                  [Dict, Order, Real] / [Dict, Real, Order].
//	                                  Refusing `Order` must leave BOTH `Dict` and
//	                                  `Real`. Refused in MIDDLE and in LAST
//	                                  position. A whole-field refusal deletes
//	                                  these two rows entirely.
//	G11.M / G11.R            [REFUSE+KEEP] the same multiplicity at the RECORD
//	                                  anchor — java's finding was that the record
//	                                  anchor had no scoping row at all.
//	G12.Pair                 [REFUSE+KEEP] tuple `(Order, Real)`, refused FIRST
//	G12.Arr                  [REFUSE] array of the parameter; no survivor
//	Plain.X                  [KEEP]   the over-refusal control at the PROPERTY
//	                                  anchor: `Order` is a parameter in nine
//	                                  declarations of this file and NOT here.
//	Plain.Y                  [LIVE]
//	PlainS.X                 [KEEP]   the same control at the FIELD anchor
//	PlainR.X                 [KEEP]   the same control at the RECORD anchor
//	PlainR.Y                 [LIVE]
func TestCsharpFieldTypeRefs_7041_ParameterFormSpace(t *testing.T) {
	recs := extractCSFiles(t, map[string]string{ft7041FormPath: ft7041FormSrc})
	const cls = "scope:component:class:csharp:P.cs:"
	want := []string{
		"G1.Ok -> " + cls + "Real",
		"G2.C -> " + cls + "IThing",
		"G3.B -> " + cls + "Order",
		"G4.B -> " + cls + "IThing",
		"G6.B -> " + cls + "Real",
		"G7.B -> " + cls + "Real",
		"G8.B -> " + cls + "Real",
		"G9.B -> " + cls + "Real",
		"G10.M -> " + cls + "Dict",
		"G10.M -> " + cls + "Real",
		"G10.R -> " + cls + "Dict",
		"G10.R -> " + cls + "Real",
		"G11.M -> " + cls + "Dict",
		"G11.M -> " + cls + "Real",
		"G11.R -> " + cls + "Dict",
		"G11.R -> " + cls + "Real",
		"G12.Pair -> " + cls + "Real",
		"G13.M -> " + cls + "Mark",
		"Plain.X -> " + cls + "Order",
		"Plain.Y -> " + cls + "Real",
		"PlainR.X -> " + cls + "Order",
		"PlainR.Y -> " + cls + "Keep",
		"PlainS.X -> " + cls + "Order",
	}
	sort.Strings(want)
	got := fieldTypeRefEdges(t, recs)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("parameter-form-space edges mismatch\n got:\n%s\nwant:\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// The [REFUSE] direction, asserted by NAME rather than left to the set
	// comparison above, so a failure says which form leaked.
	for _, field := range []string{
		"G1.Shadowed", // plain generic class
		"G2.A",        // first of two parameters
		"G2.B",        // second of two parameters
		"G3.A",        // constrained parameter
		"G4.A",        // multi-constrained parameter
		"G5.A",        // `in` variance
		"G5.B",        // `out` variance
		"G6.A",        // struct + field anchor
		"G7.A",        // record positional parameter anchor
		"G8.A",        // nullable T?
		"G9.A",        // attributed parameter
		"G12.Arr",     // array of the parameter
		"G13.A",       // parameter attributed with a same-file attribute class
	} {
		for _, e := range got {
			if strings.HasPrefix(e, field+" -> ") {
				t.Errorf("%s names a type parameter in scope and must carry NO "+
					"field-type edge, got %q", field, e)
			}
		}
	}
}

const ft7041NestPath = "N.cs"

// ft7041NestSrc — the NESTING space. C#'s rule is derived and stated in
// field_type_refs.go; this fixture grades it in both directions. `Order`,
// `Real`, `Keep` and `Dict` are ordinary same-file types; `Order` is ALSO bound
// as a type parameter by SEVEN declarations here (N1, N2, N3, N4, N6, Inner5,
// G7n), which is what makes N5.A and Sib7.A real over-refusal controls rather
// than liveness rows. `Dict` carries the mixed-candidate rows added after
// review of #7075.
const ft7041NestSrc = `namespace App;

public class Order { public string N { get; set; } }
public class Real { public string N { get; set; } }
public class Keep { public string N { get; set; } }
public class Dict<A, B> { }

public class N1<Order> { public class Inner1 { public Order A { get; set; } public Real B { get; set; } public Dict<Order, Real> Mm { get; set; } public (Order, Real) Pf; } }

public class N2<Order> { public class Mid2 { public class Deep2 { public Order A { get; set; } public Real B { get; set; } public Dict<Real, Order> Ml { get; set; } } } }

public class N3<Order> { public class Inner3<Real> { public Order A { get; set; } public Real B { get; set; } public Keep C { get; set; } } }

public class N4<Order> { public static class Sn4 { public static Order A; public static Real B; } }

public class N5 { public Order A { get; set; } public class Inner5<Order> { public Order B { get; set; } public Real C { get; set; } } }

public class N6<Order> { public record R6(Order A, Real B, Dict<Order, Real> Mr); }

public class N7 { public class G7n<Order> { public Keep K { get; set; } } public class Sib7 { public Order A { get; set; } public Real B { get; set; } } }
`

// TestCsharpFieldTypeRefs_7041_NestingFormSpace grades the ASCENT.
//
//	EDGE / FIELD             KIND     VARIES
//	Inner1.A                 [REFUSE] depth 1 — the nested type sees the outer's
//	                                  parameter (C#'s rule; see the production
//	                                  comment for the spec citation)
//	Inner1.B                 [LIVE]
//	Inner1.Mm                [REFUSE+KEEP] MULTIPLICITY UNDER THE ASCENT.
//	                                  `Dict<Order, Real>` in a nested class:
//	                                  three candidates, the refused one in the
//	                                  MIDDLE, at the PROPERTY anchor. Both
//	                                  `Dict` and `Real` must survive. This is
//	                                  the row that separates "drop the refused
//	                                  candidate" from "drop the whole field"
//	                                  when the binder is an ANCESTOR rather than
//	                                  the field's own declaring type — the
//	                                  distinction ParameterFormSpace structurally
//	                                  cannot make, because every binder there is
//	                                  the declaring type itself.
//	Inner1.Pf                [REFUSE+KEEP] the same, refused FIRST, at the FIELD
//	                                  anchor: `(Order, Real) Pf` keeps `Real`.
//	Deep2.A                  [REFUSE] depth 2 — grades that the ascent does not
//	                                  stop after one level
//	Deep2.B                  [LIVE]
//	Deep2.Ml                 [REFUSE+KEEP] multiplicity at DEPTH 2, refused
//	                                  LAST: `Dict<Real, Order>` keeps `Dict` and
//	                                  `Real`.
//	Inner3.A                 [REFUSE] ascent PAST A NON-EMPTY NEAREST LIST. This
//	                                  is the row java found by scoring a mutant
//	                                  ALIVE: without it, every "ascent" row could
//	                                  be satisfied by reading only the nearest
//	                                  list.
//	Inner3.B                 [REFUSE] the NEAREST list, when an outer list also
//	                                  exists — grades the union, not a pick-one
//	Inner3.C                 [LIVE]
//	Sn4.A                    [REFUSE] `static` nested class.
//	                                  UNVERIFIED-LEGALITY: C# nested types are
//	                                  always static-in-the-Java-sense and §7.7
//	                                  scopes the outer parameters over the whole
//	                                  class_body, so this should compile — but
//	                                  no compiler exists here to demonstrate it.
//	                                  The assertion is SAFE EITHER WAY: if the
//	                                  form is legal, `Order` is the parameter and
//	                                  refusing is correct; if it is illegal, the
//	                                  row is not a program anyone can write and
//	                                  refusing costs nothing. It is recorded, not
//	                                  relied on as the evidence for the rule.
//	Sn4.B                    [LIVE]
//	N5.A                     [KEEP]   THE ASCENT'S OVER-REFUSAL CONTROL. `Order`
//	                                  is bound as a parameter by N5's own NESTED
//	                                  class, i.e. DESCENDANT scope. An
//	                                  implementation that searched the subtree,
//	                                  or refused a name file-wide once seen,
//	                                  deletes this edge silently.
//	Inner5.B                 [REFUSE] the same name, one level in, IS shadowed
//	Inner5.C                 [LIVE]
//	R6.A                     [REFUSE] ascent at the RECORD anchor (nested record
//	                                  in a generic class) — the anchor java had
//	                                  ungraded for scoping
//	R6.B                     [LIVE]
//	R6.Mr                    [REFUSE+KEEP] multiplicity at the RECORD anchor
//	                                  under the ascent, refused MIDDLE. Without
//	                                  this row the record anchor graded the
//	                                  ascent only in the one-candidate shape.
//	Sib7.A                   [KEEP]   the SIBLING control: the generic
//	                                  declaration is a sibling, not an ancestor,
//	                                  so nothing is shadowed
//	Sib7.B                   [LIVE]
//	G7n.K                    [LIVE]
func TestCsharpFieldTypeRefs_7041_NestingFormSpace(t *testing.T) {
	recs := extractCSFiles(t, map[string]string{ft7041NestPath: ft7041NestSrc})
	const cls = "scope:component:class:csharp:N.cs:"
	want := []string{
		"Deep2.B -> " + cls + "Real",
		"Deep2.Ml -> " + cls + "Dict",
		"Deep2.Ml -> " + cls + "Real",
		"G7n.K -> " + cls + "Keep",
		"Inner1.B -> " + cls + "Real",
		"Inner1.Mm -> " + cls + "Dict",
		"Inner1.Mm -> " + cls + "Real",
		"Inner1.Pf -> " + cls + "Real",
		"Inner3.C -> " + cls + "Keep",
		"Inner5.C -> " + cls + "Real",
		"N5.A -> " + cls + "Order",
		"R6.B -> " + cls + "Real",
		"R6.Mr -> " + cls + "Dict",
		"R6.Mr -> " + cls + "Real",
		"Sib7.A -> " + cls + "Order",
		"Sib7.B -> " + cls + "Real",
		"Sn4.B -> " + cls + "Real",
	}
	sort.Strings(want)
	got := fieldTypeRefEdges(t, recs)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("nesting-form-space edges mismatch\n got:\n%s\nwant:\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	for _, field := range []string{
		"Inner1.A", // depth 1
		"Deep2.A",  // depth 2
		"Inner3.A", // ascent past a non-empty nearest list
		"Inner3.B", // nearest list, union with the outer
		"Sn4.A",    // static nested — UNVERIFIED-LEGALITY, see above
		"Inner5.B", // shadowed one level in
		"R6.A",     // record anchor under a generic outer
	} {
		for _, e := range got {
			if strings.HasPrefix(e, field+" -> ") {
				t.Errorf("%s names a type parameter of an ENCLOSING declaration "+
					"and must carry NO field-type edge, got %q", field, e)
			}
		}
	}

	// THE MIXED-CANDIDATE ROWS, asserted per (field, TARGET) rather than per
	// field: these fields DO keep edges, so the whole-field loop above cannot
	// express them. Each names a shadowed ANCESTOR parameter alongside real
	// same-file types, so the refused candidate must go and its neighbours must
	// stay. Added after review: the previous revision graded multiplicity only
	// in ParameterFormSpace, where every binder is the field's OWN declaring
	// type — so along the ASCENT, "drop the refused candidate" and "drop the
	// whole field" were still the same function. Mutant MX (drop the whole
	// field when an ancestor OTHER than the declaring type shadows a candidate)
	// was ALIVE against the previous revision with both suites green.
	for _, forbidden := range []string{
		"Inner1.Mm -> " + cls + "Order", // refused MIDDLE, property anchor, depth 1
		"Inner1.Pf -> " + cls + "Order", // refused FIRST,  field anchor,    depth 1
		"Deep2.Ml -> " + cls + "Order",  // refused LAST,   property anchor, depth 2
		"R6.Mr -> " + cls + "Order",     // refused MIDDLE, RECORD anchor,   depth 1
	} {
		for _, e := range got {
			if e == forbidden {
				t.Errorf("mixed-candidate row leaked the shadowed candidate: %q", e)
			}
		}
	}
}

// TestCsharpFieldTypeRefs_7041_TypeParameterShadowsSameFileType replaces the
// deleted known-wrong pin
// TestCsharpFieldTypeRefs_KnownOverFire_TypeParameterShadowsSameFileType
// (field_type_refs_6912_test.go:352), which asserted the WRONG edge was present
// behind a hard `t.Fatalf`.
//
// It is java's pattern (`6d05d5fe0`), and the reason for it is that a test
// asserting only "the shadowed field has no edge" passes for the wrong reason
// the moment the producer stops producing anything at all. So there are TWO
// fixtures and every one carries a control:
//
//	COLLISION   `class Box<Customer>` beside a same-file `class Customer` —
//	            Box.Item refused, Box.Keep kept. This is the defect's own shape.
//	NO COLLISION `class Box2<T>` with NO same-file `T` — Box2.Item has no edge
//	            because nothing named `T` is declared, which is the pre-#7041
//	            reason, and Box2.Keep is kept.
//
// "the over-fire is still here" fails the first fixture's absence assertion;
// "the producer stopped working entirely" fails BOTH fixtures' Keep rows. They
// cannot be confused.
func TestCsharpFieldTypeRefs_7041_TypeParameterShadowsSameFileType(t *testing.T) {
	const collisionSrc = `namespace App.Models;

public class Customer { public string N { get; set; } }
public class Real { public string N { get; set; } }

public class Box<Customer> { public Customer Item { get; set; } public Real Keep { get; set; } }
`
	const noCollisionSrc = `namespace App.Models;

public class Real { public string N { get; set; } }

public class Box2<T> { public T Item { get; set; } public Real Keep { get; set; } }
`
	for _, tc := range []struct {
		name, path, src, absent, present string
	}{
		{"collision", "C.cs", collisionSrc, "Box.Item",
			"Box.Keep -> scope:component:class:csharp:C.cs:Real"},
		{"no-collision", "D.cs", noCollisionSrc, "Box2.Item",
			"Box2.Keep -> scope:component:class:csharp:D.cs:Real"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recs := extractCSFiles(t, map[string]string{tc.path: tc.src})
			got := fieldTypeRefEdges(t, recs)
			for _, e := range got {
				if strings.HasPrefix(e, tc.absent+" -> ") {
					t.Errorf("%s must carry NO field-type edge, got %q", tc.absent, e)
				}
			}
			var found bool
			for _, e := range got {
				if e == tc.present {
					found = true
				}
			}
			if !found {
				t.Errorf("control edge %q is MISSING — the producer is not "+
					"working, so the absence assertion above proves nothing\n got: %v",
					tc.present, got)
			}
		})
	}
}
