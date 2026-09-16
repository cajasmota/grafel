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
//	  VARIES          the FORM of the parameter declaration (plain, two-name,
//	                  constrained, multi-constrained, variance-annotated,
//	                  attributed), the DECLARATION KIND carrying the list
//	                  (class / struct / interface / record), the ANCHOR
//	                  (property · field · record positional parameter) and
//	                  CANDIDATE MULTIPLICITY (1 candidate · refused-first ·
//	                  refused-middle · refused-last), and the field-type SYNTAX
//	                  the candidate sits in (bare, nullable, array, tuple,
//	                  generic argument, generic constructor position).
//	  HELD CONSTANT   nesting depth (top-level declarations only — that axis is
//	                  the whole of the next table) and namespace (one
//	                  file-scoped namespace; namespace scope is a SEPARATE
//	                  known over-fire, still pinned).
//
//	TestCsharpFieldTypeRefs_7041_NestingFormSpace
//	  VARIES          nesting DEPTH (1 and 2 levels), whether the nested
//	                  declaration carries its OWN non-empty list, the nested
//	                  declaration KIND (class · record), the `static` modifier,
//	                  and the DIRECTION (ancestor-generic → refuse;
//	                  non-ancestor-generic → keep).
//	  HELD CONSTANT   parameter form (plain, unconstrained — graded by the table
//	                  above) and candidate multiplicity at most rows; the two
//	                  rows that vary it are marked.
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
// field_type_refs.go. Every form used below is a plain generic
// class/struct/record/interface with a type parameter shadowing a same-file
// type, whose legality is not in doubt.

const ft7041FormPath = "P.cs"

// ft7041FormSrc — the parameter-form space. `Order` is the name bound as a type
// parameter in most declarations; `Real`, `IThing` and `Keep` are ordinary
// same-file types that must stay bindable everywhere.
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
// field_type_refs.go; this fixture grades it in both directions.
const ft7041NestSrc = `namespace App;

public class Order { public string N { get; set; } }
public class Real { public string N { get; set; } }
public class Keep { public string N { get; set; } }

public class N1<Order> { public class Inner1 { public Order A { get; set; } public Real B { get; set; } } }

public class N2<Order> { public class Mid2 { public class Deep2 { public Order A { get; set; } public Real B { get; set; } } } }

public class N3<Order> { public class Inner3<Real> { public Order A { get; set; } public Real B { get; set; } public Keep C { get; set; } } }

public class N4<Order> { public static class Sn4 { public static Order A; public static Real B; } }

public class N5 { public Order A { get; set; } public class Inner5<Order> { public Order B { get; set; } public Real C { get; set; } } }

public class N6<Order> { public record R6(Order A, Real B); }

public class N7 { public class G7n<Order> { public Keep K { get; set; } } public class Sib7 { public Order A { get; set; } public Real B { get; set; } } }
`

// TestCsharpFieldTypeRefs_7041_NestingFormSpace grades the ASCENT.
//
//	EDGE / FIELD             KIND     VARIES
//	Inner1.A                 [REFUSE] depth 1 — the nested type sees the outer's
//	                                  parameter (C#'s rule; see the production
//	                                  comment for the spec citation)
//	Inner1.B                 [LIVE]
//	Deep2.A                  [REFUSE] depth 2 — grades that the ascent does not
//	                                  stop after one level
//	Deep2.B                  [LIVE]
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
		"G7n.K -> " + cls + "Keep",
		"Inner1.B -> " + cls + "Real",
		"Inner3.C -> " + cls + "Keep",
		"Inner5.C -> " + cls + "Real",
		"N5.A -> " + cls + "Order",
		"R6.B -> " + cls + "Real",
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
