package kotlin_test

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// field_type_refs_6912_test.go — behavioural grading for the Kotlin
// field→declared-type edge (issue #6912, kotlin arm).
//
// FIXTURE AXES. Stated because a fixture set that varies one axis thoroughly
// makes its neighbour look covered (the dominant defect on this issue — it cost
// arms G and E their first cuts).
//
//	VARIED across this file:
//	  - type EXPRESSION shape: bare, nullable, generic argument, nested
//	    generic, array element, function-type parameter, function-type return,
//	    star projection, `out`/`in` variance, dotted-qualified, dotted-qualified
//	    carrying an argument, function-type receiver, type parameter.
//	  - ANCHOR shape (the SOURCE endpoint): class-body property, primary
//	    constructor `val` parameter, object-body property, interface property,
//	    getter-only property, `by lazy` delegated property, extension property
//	    declared in a class body, and a plain (non-`val`) constructor parameter.
//	  - TARGET kind/subtype: class, data_class, interface, enum, object,
//	    typealias, import carrier, file carrier, SCOPE.Enum value-set,
//	    SCOPE.Service (Spring stereotype), SCOPE.Operation, SCOPE.Schema/field.
//	  - FILE: same-file vs another file, in both failure directions.
//	  - NOT-A-DECLARATION carriers: expect/actual, external, companion object,
//	    an `object` vs a `class`, an extension property's receiver.
//
//	HELD CONSTANT inside each test, and varied only across tests, so no single
//	row moves two axes at once: the edge Kind/`ref_kind` spelling, the owner
//	type name, and file layout.

// ktExtract runs the real Kotlin extractor over one file.
func ktExtract(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("kotlin")
	if !ok {
		t.Fatal("kotlin extractor not registered")
	}
	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path: path, Content: []byte(src), Language: "kotlin", TSTree: tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return got
}

// ktFieldTypeEdges returns every field→declared-type edge as
// "<fieldName> -> <toID>", sorted. It selects on the `ref_kind` property so a
// CONTAINS edge or any other REFERENCES producer can never be counted here.
func ktFieldTypeEdges(recs []types.EntityRecord) []string {
	var out []string
	for i := range recs {
		r := &recs[i]
		for _, rel := range r.Relationships {
			isFieldType := false
			for _, p := range rel.Properties {
				if p.K == "ref_kind" && p.V == "field_target_type" {
					isFieldType = true
				}
			}
			if !isFieldType {
				continue
			}
			out = append(out, r.Name+" -> "+rel.ToID)
		}
	}
	sort.Strings(out)
	return out
}

func ktAssertEdges(t *testing.T, got, want []string) {
	t.Helper()
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("edge set mismatch\n got: %v\nwant: %v", got, want)
	}
}

// ---------------------------------------------------------------------------
// The edge exists at all, from both anchors Kotlin mints a field from.
// ---------------------------------------------------------------------------

func TestKotlinFieldTypeRefs_BodyPropertyAndConstructorParamBothEmit(t *testing.T) {
	src := `package p

class Customer
class Line

data class Order(val buyer: Customer) {
    val line: Line = l
}
`
	recs := ktExtract(t, "Order.kt", src)
	ktAssertEdges(t, ktFieldTypeEdges(recs), []string{
		"Order.buyer -> scope:component:class:kotlin:Order.kt:Customer",
		"Order.line -> scope:component:class:kotlin:Order.kt:Line",
	})
}

func TestKotlinFieldTypeRefs_EdgeCarriesKindAndProperties(t *testing.T) {
	src := `package p

class Customer

class Order {
    val buyer: Customer = c
}
`
	recs := ktExtract(t, "Order.kt", src)
	found := 0
	for i := range recs {
		r := &recs[i]
		if r.Name != "Order.buyer" {
			continue
		}
		for _, rel := range r.Relationships {
			if rel.Kind != "REFERENCES" {
				continue
			}
			found++
			// Independent literals, not derived from the constants under test.
			props := map[string]string{}
			for _, p := range rel.Properties {
				props[p.K] = p.V
			}
			if props["ref_kind"] != "field_target_type" {
				t.Errorf("ref_kind = %q, want %q", props["ref_kind"], "field_target_type")
			}
			if props["field_name"] != "buyer" {
				t.Errorf("field_name = %q, want %q", props["field_name"], "buyer")
			}
			if props["target_type"] != "Customer" {
				t.Errorf("target_type = %q, want %q", props["target_type"], "Customer")
			}
			if rel.FromID != "" {
				t.Errorf("FromID = %q, want empty (assembly anchors on the field)", rel.FromID)
			}
		}
	}
	if found != 1 {
		t.Fatalf("REFERENCES edges on Order.buyer = %d, want 1", found)
	}
}

// ---------------------------------------------------------------------------
// The type-expression shape space, enumerated rather than sampled.
// ---------------------------------------------------------------------------

func TestKotlinFieldTypeRefs_TypeExpressionShapeSpace(t *testing.T) {
	// One target type (Order) reachable through thirteen different type
	// expressions. Every row is legal Kotlin; the ones expected to produce
	// nothing are listed with want=false and a reason.
	rows := []struct {
		label string
		decl  string
		want  bool
	}{
		{"bare", "val a: Order = x", true},
		{"nullable", "val b: Order? = x", true},
		{"generic argument", "val c: List<Order> = x", true},
		{"nested generic argument", "val d: Map<String, List<Order>> = x", true},
		{"array element", "val e: Array<Order> = x", true},
		{"out variance", "val f: Array<out Order> = x", true},
		{"function-type parameter", "val g: (Order) -> Unit = x", true},
		{"function-type return", "val h: () -> Order = x", true},
		{"suspend function type", "val i: suspend () -> Order = x", true},
		{"dotted-qualified carrying an argument", "val j: kotlin.collections.List<Order> = x", true},
		// Refused, each for a stated reason.
		{"dotted-qualified whole (no segment is taken)", "val k: com.acme.Order = x", false},
		{"star projection names nothing", "val l: List<*> = x", false},
		{"function-type receiver is not descended", "val m: Order.() -> Unit = x", false},
	}
	for _, row := range rows {
		t.Run(row.label, func(t *testing.T) {
			src := "package p\n\nclass Order\n\nclass Holder {\n    " + row.decl + "\n}\n"
			recs := ktExtract(t, "H.kt", src)
			got := ktFieldTypeEdges(recs)
			if row.want {
				if len(got) != 1 || !strings.HasSuffix(got[0], ":Order") {
					t.Fatalf("%s: got %v, want exactly one edge to Order", row.label, got)
				}
			} else if len(got) != 0 {
				t.Fatalf("%s: got %v, want no edge", row.label, got)
			}
		})
	}
}

func TestKotlinFieldTypeRefs_QualifiedTypeIsNotStrippedToASameFileName(t *testing.T) {
	// `com.acme.Order` must NOT bind to the same-file `class Order`. Arm D
	// found exactly this silently-wrong binding in Go's resolveTypeReferences.
	src := `package p

class Order

class Holder {
    val o: com.acme.Order = x
}
`
	recs := ktExtract(t, "H.kt", src)
	ktAssertEdges(t, ktFieldTypeEdges(recs), nil)
}

func TestKotlinFieldTypeRefs_NestedQualifiedTypeTakesNeitherSegment(t *testing.T) {
	// Both segments of `Outer.Inner` are plausible same-file names; taking
	// either is a guess between two wrong answers.
	src := `package p

class Outer
class Inner

class Holder {
    val o: Outer.Inner = x
}
`
	recs := ktExtract(t, "H.kt", src)
	ktAssertEdges(t, ktFieldTypeEdges(recs), nil)
}

func TestKotlinFieldTypeRefs_OneEdgePerTargetEvenWhenNamedTwice(t *testing.T) {
	src := `package p

class Order

class Holder {
    val m: Map<Order, Order> = x
}
`
	recs := ktExtract(t, "H.kt", src)
	ktAssertEdges(t, ktFieldTypeEdges(recs), []string{
		"Holder.m -> scope:component:class:kotlin:H.kt:Order",
	})
}

func TestKotlinFieldTypeRefs_TypeParameterIsNeverATargetEvenWhenShadowed(t *testing.T) {
	// `class Holder<T>` declares T as a type parameter. A file that ALSO
	// declares `class T` must not make `val item: T` bind to it. Java's arm
	// pinned this as a KNOWN OVER-FIRE; Kotlin refuses it for the type
	// parameters VISIBLE at the declaration, because it has the
	// `type_parameters` list in hand at the emit site (see the nesting-form
	// table below for exactly which enclosing lists count as visible).
	//
	// THIS TEST GRADES THE PRIMARY-CONSTRUCTOR ANCHOR ONLY. The body-property
	// anchor is a SECOND call site passing the same shadow set and it is
	// graded by its own test below — removing the filter from one anchor left
	// the other's test green (review finding F2, mutant MR-1b), which is the
	// mutually-masking-guards failure applied to this very guard.
	src := `package p

class T
class Order

class Holder<T>(val item: T, val real: Order)
`
	recs := ktExtract(t, "H.kt", src)
	ktAssertEdges(t, ktFieldTypeEdges(recs), []string{
		// `real: Order` is the positive control: the pass IS emitting here,
		// so the absent `item -> T` edge is a refusal and not a silent no-op.
		"Holder.real -> scope:component:class:kotlin:H.kt:Order",
	})
}

func TestKotlinFieldTypeRefs_TypeParameterIsNeverATargetFromABodyProperty(t *testing.T) {
	// The BODY-PROPERTY anchor of the same guard, graded separately from the
	// primary-constructor anchor above. `buildProperty` receives the shadow
	// set through its own argument at its own call site (kotlin.go's
	// class-body arm), so a mutant that drops it there — MR-1b in the review
	// — must fail HERE and nowhere else.
	//
	// Held constant against the constructor row: the same shadow name `T`,
	// the same rival `class T`, the same positive control. Only the ANCHOR
	// varies, which is the axis the constructor row holds fixed.
	src := `package p

class T
class Order

class Holder<T> {
    val item: T = q
    val real: Order = z
}
`
	recs := ktExtract(t, "H.kt", src)
	ktAssertEdges(t, ktFieldTypeEdges(recs), []string{
		"Holder.real -> scope:component:class:kotlin:H.kt:Order",
	})
}

func TestKotlinFieldTypeRefs_NestingFormSpace(t *testing.T) {
	// THE NESTING-FORM SPACE, enumerated in BOTH directions (review finding
	// F1). Kotlin scoping is not "the immediate declaration's list": an
	// `inner class` — and only an `inner class` — captures its enclosing
	// CLASS's type parameters, so a field typed by one of them must be
	// refused even though the shadow name appears nowhere on the inner
	// declaration itself. Every other nesting form does NOT capture, and
	// refusing there would silently DELETE a correct edge — a missing edge
	// has no symptom, which is how arm cpp's equivalent refusal was wrong in
	// both directions at once (#7057).
	//
	// So every row below carries `real: Order`, a real same-file type, as a
	// positive control: a row asserting only an absence would pass if the
	// pass emitted nothing at all.
	//
	// VARIED:
	//   - the nesting RELATIONSHIP (inner / plain nested / object / local, and
	//     what sits between a declaration and the generic class above it);
	//   - whether the enclosing declaration is GENERIC;
	//   - the nested declaration's own **`class_modifier`**. `inner` is one
	//     member of that node type; PROBED against the grammar, the others
	//     that a nested declaration can carry are exactly `data`, `sealed`,
	//     `annotation` and `value` — all four are rows. This axis was HELD
	//     CONSTANT in the first cut of this table (every row was bare `class`
	//     or `inner class`) and a mutant that treated ANY `class_modifier` as
	//     `inner` survived the whole suite while silently deleting `data class
	//     M(val y: T)`'s correct edge — CK-1. Enumerating one axis thoroughly
	//     is what makes its sibling look covered.
	//   - the nested declaration's SUBTYPE where it is not a modifier at all:
	//     `enum class` puts a bare `enum` node directly under
	//     class_declaration, NOT under `modifiers`, so it is OUTSIDE CK-1's
	//     blast radius and does not kill it. Probed rather than assumed — the
	//     first draft of this comment listed `enum` as a class_modifier
	//     sibling and it is not one. The row stays because it varies the
	//     nested subtype and is the only nested row whose target mints TWO
	//     records, but it is not part of the CK-1 enumeration.
	//   - the ENCLOSING declaration's modifier, one row, in the refuse
	//     direction. The code never READS it (a `sealed class` is still a
	//     `class_declaration`), so no mutant distinguishes that row; it is
	//     here because the held-constant list is what made CK-1 invisible on
	//     reading, and "bare `class` outer" was on it.
	//
	// HELD CONSTANT: the shadow name (`T`), the rival declaration (`class T`),
	// the file, and the anchor — primary-constructor `val`, except the rows
	// that say otherwise, which are the body-property anchor and the two
	// modifier rows (`value`, `enum`) whose form cannot carry two constructor
	// properties.
	//
	// NOT ROWS, having been checked rather than assumed: `abstract` / `open`
	// are an `inheritance_modifier` and `private` a `visibility_modifier` —
	// different CST node types, outside CK-1's blast radius, and nothing in
	// this pass reads them.
	//
	// Two fixtures could not be written the way the others are, and both
	// would have asserted NOTHING if they had been: a bare
	// `enum class M { A, B }` mints no field at all, and a `value class`
	// takes exactly one constructor property. Both rows use body properties
	// instead, which moves the anchor — said here rather than left to be
	// noticed.
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{
			// CAPTURES — the defect F1 named. `Inner` is inner, so `T` here
			// is `Outer`'s type parameter, not `class T`.
			name: "inner class in a generic outer, constructor anchor",
			src: `class T
class Order
class Outer<T> {
    inner class Inner(val x: T, val real: Order)
}`,
			want: []string{"Inner.real -> scope:component:class:kotlin:N.kt:Order"},
		},
		{
			// CAPTURES — same, through the body-property anchor, and with the
			// shadow name spelled as an ordinary type name (`Key`) to show
			// this is not an exotic spelling.
			name: "inner class in a generic outer, body-property anchor",
			src: `class Key
class Order
class Cache<Key> {
    inner class Entry {
        val k: Key = q
        val real: Order = z
    }
}`,
			want: []string{"Entry.real -> scope:component:class:kotlin:N.kt:Order"},
		},
		{
			// CAPTURES — two levels of `inner`. `C` captures `B`'s scope and
			// `B`, being inner too, captures `A`'s.
			name: "inner class inside an inner class, generic grandparent",
			src: `class T
class Order
class A<T> {
    inner class B {
        inner class C(val x: T, val real: Order)
    }
}`,
			want: []string{"C.real -> scope:component:class:kotlin:N.kt:Order"},
		},
		{
			// DOES NOT CAPTURE — the outer declares no type parameters at
			// all, so `T` is the same-file `class T` and the edge is CORRECT.
			// This row is what fails if the walk adds the enclosing list
			// unconditionally rather than reading it.
			name: "inner class in a NON-generic outer",
			src: `class T
class Order
class Outer {
    inner class Inner(val x: T, val real: Order)
}`,
			want: []string{
				"Inner.real -> scope:component:class:kotlin:N.kt:Order",
				"Inner.x -> scope:component:class:kotlin:N.kt:T",
			},
		},
		{
			// DOES NOT CAPTURE — a nested class WITHOUT `inner` has no access
			// to the outer's type parameters (Kotlin rejects `val x: T`
			// there as an unresolved reference to the parameter), so `T`
			// names the same-file class and the edge is CORRECT. The
			// over-refusal direction.
			name: "plain nested class in a generic outer",
			src: `class T
class Order
class Outer<T> {
    class Nested(val x: T, val real: Order)
}`,
			want: []string{
				"Nested.real -> scope:component:class:kotlin:N.kt:Order",
				"Nested.x -> scope:component:class:kotlin:N.kt:T",
			},
		},
		{
			// DOES NOT CAPTURE — same, nested inside a generic INTERFACE
			// rather than a generic class. The enclosing declaration's node
			// type is the same (`class_declaration`), so this row grades that
			// the rule keys on `inner`, not on the enclosing subtype.
			name: "plain nested class in a generic interface",
			src: `class T
class Order
interface I<T> {
    class N(val x: T, val real: Order)
}`,
			want: []string{
				"N.real -> scope:component:class:kotlin:N.kt:Order",
				"N.x -> scope:component:class:kotlin:N.kt:T",
			},
		},
		{
			// DOES NOT CAPTURE — an `object` declaration cannot take type
			// parameters and does not capture the enclosing class's, so its
			// body property's `T` is the same-file class. CORRECT edge.
			name: "object declaration inside a generic class",
			src: `class T
class Order
class Outer<T> {
    object Obj {
        val x: T = q
        val real: Order = z
    }
}`,
			want: []string{
				"Obj.real -> scope:component:class:kotlin:N.kt:Order",
				"Obj.x -> scope:component:class:kotlin:N.kt:T",
			},
		},
		{
			// DOES NOT CAPTURE ACROSS AN OBJECT — `P` is inner, but the
			// declaration it is inner TO is an `object`, which holds no type
			// parameters and captures none of `A`'s. Walking to the nearest
			// enclosing CLASS instead of the nearest enclosing DECLARATION
			// would over-refuse here and delete a correct edge.
			name: "inner class inside an object inside a generic class",
			src: `class T
class Order
class A<T> {
    object O {
        inner class P(val z: T, val real: Order)
    }
}`,
			want: []string{
				"P.real -> scope:component:class:kotlin:N.kt:Order",
				"P.z -> scope:component:class:kotlin:N.kt:T",
			},
		},
		{
			// DOES NOT CAPTURE ACROSS A NON-INNER NESTED CLASS — the chain
			// STOPS at `M`, which is not inner. `N` sees `M`'s parameters
			// (none), never `A`'s.
			name: "inner class inside a plain nested class inside a generic class",
			src: `class T
class Order
class A<T> {
    class M {
        inner class N(val y: T, val real: Order)
    }
}`,
			want: []string{
				"N.real -> scope:component:class:kotlin:N.kt:Order",
				"N.y -> scope:component:class:kotlin:N.kt:T",
			},
		},
		{
			// CAPTURES, and the inner's OWN list shadows the outer's. The
			// refusal is right for either reason, so this row is here to pin
			// that a union never loses the own list — a walk that REPLACED
			// the own set with the captured one would still refuse `x`, so
			// the row also carries `w: W`, the inner's SECOND own parameter,
			// which only the own list can refuse.
			name: "inner class with its own shadowing type parameter",
			src: `class T
class W
class Order
class A<T> {
    inner class B<T, W>(val x: T, val w: W, val real: Order)
}`,
			want: []string{"B.real -> scope:component:class:kotlin:N.kt:Order"},
		},

		// ------------------------------------------------------------------
		// The nested declaration's own class_modifier. `inner` is ONE member
		// of that node type; the four rows below are the other members a
		// nested declaration can carry, probed against the grammar, and NONE
		// of them captures — `data class M` inside `class A<T>` cannot see
		// `T`, so `val y: T` names the same-file `class T` and the edge is
		// CORRECT. Every row is therefore in the KEPT direction, which is the
		// direction with no symptom when it breaks. CK-1 — "treat any
		// class_modifier as inner" — dies on all four.
		//
		// The set is enumerated rather than represented by `data` alone: a
		// hand-picked representative is how the first cut of this table came
		// to hold the whole axis constant, and each row costs four lines.
		// ------------------------------------------------------------------
		{
			name: "data class nested in a generic outer",
			src: `class T
class Order
class A<T> {
    data class M(val y: T, val real: Order)
}`,
			want: []string{
				"M.real -> scope:component:class:kotlin:N.kt:Order",
				"M.y -> scope:component:class:kotlin:N.kt:T",
			},
		},
		{
			name: "sealed class nested in a generic outer",
			src: `class T
class Order
class A<T> {
    sealed class M(val y: T, val real: Order)
}`,
			want: []string{
				"M.real -> scope:component:class:kotlin:N.kt:Order",
				"M.y -> scope:component:class:kotlin:N.kt:T",
			},
		},
		{
			name: "annotation class nested in a generic outer",
			src: `class T
class Order
class A<T> {
    annotation class M(val y: T, val real: Order)
}`,
			want: []string{
				"M.real -> scope:component:class:kotlin:N.kt:Order",
				"M.y -> scope:component:class:kotlin:N.kt:T",
			},
		},
		{
			// A value class takes exactly ONE constructor property, so its
			// positive control has to be a body property. The anchor moves
			// with it, and that is said rather than left to be noticed.
			name: "value class nested in a generic outer",
			src: `class T
class Order
class A<T> {
    value class M(val y: T) {
        val real: Order get() = z
    }
}`,
			want: []string{
				"M.real -> scope:component:class:kotlin:N.kt:Order",
				"M.y -> scope:component:class:kotlin:N.kt:T",
			},
		},
		{
			// NOT a class_modifier row: `enum class` puts a bare `enum` node
			// directly under class_declaration rather than under `modifiers`,
			// so this row does NOT kill CK-1 and is not claimed to. It earns
			// its place on a different axis — it is the only nested row whose
			// target mints TWO records (a SCOPE.Enum value-set and a
			// SCOPE.Component), so it shows the component-family half of the
			// ambiguity rule holding up under a nested declaration.
			//
			// A bare `enum class M { A, B }` mints NO field, so this fixture
			// uses body properties; without them the row would assert nothing.
			name: "enum class nested in a generic outer",
			src: `class T
class Order
class A<T> {
    enum class M {
        A, B;
        val y: T get() = q
        val real: Order get() = z
    }
}`,
			want: []string{
				"M.real -> scope:component:class:kotlin:N.kt:Order",
				"M.y -> scope:component:class:kotlin:N.kt:T",
			},
		},
		{
			// The ENCLOSING declaration's modifier, in the refuse direction.
			// A `sealed class` is still a class_declaration carrying a
			// type_parameters list, so `N` captures `T` exactly as it does
			// under a bare `class` — asserted rather than assumed, because
			// "the outer is always a bare class" was on the held-constant
			// list that hid CK-1.
			name: "inner class in a generic SEALED outer",
			src: `class T
class Order
sealed class A<T> {
    inner class N(val y: T, val real: Order)
}`,
			want: []string{"N.real -> scope:component:class:kotlin:N.kt:Order"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			recs := ktExtract(t, "N.kt", "package p\n\n"+c.src+"\n")
			ktAssertEdges(t, ktFieldTypeEdges(recs), c.want)
		})
	}
}

// ---------------------------------------------------------------------------
// The SOURCE endpoint: which records are anchors.
// ---------------------------------------------------------------------------

func TestKotlinFieldTypeRefs_AnchorShapeSpace(t *testing.T) {
	src := `package p

class Order
class Recv

class Holder(val ctorProp: Order, plain: Order) {
    val body: Order = x
    val computed: Order get() = x
    val delegated: Order by lazy { x }
    val Recv.extension: Order get() = x
}

interface Iface {
    val abstractProp: Order
}

object Singleton {
    val objProp: Order = x
}
`
	recs := ktExtract(t, "H.kt", src)
	ktAssertEdges(t, ktFieldTypeEdges(recs), []string{
		"Holder.ctorProp -> scope:component:class:kotlin:H.kt:Order",
		"Holder.body -> scope:component:class:kotlin:H.kt:Order",
		"Holder.computed -> scope:component:class:kotlin:H.kt:Order",
		"Holder.delegated -> scope:component:class:kotlin:H.kt:Order",
		"Holder.extension -> scope:component:class:kotlin:H.kt:Order",
		"Iface.abstractProp -> scope:component:class:kotlin:H.kt:Order",
		"Singleton.objProp -> scope:component:class:kotlin:H.kt:Order",
	})
	// `plain: Order` is a constructor parameter with NO val/var binding, so
	// it is not a property and the extractor mints no field entity for it.
	for i := range recs {
		if recs[i].Name == "Holder.plain" {
			t.Fatalf("a non-val constructor parameter must not be a field entity")
		}
	}
}

func TestKotlinFieldTypeRefs_ExtensionPropertyReceiverIsNotTheDeclaredType(t *testing.T) {
	// `val Recv.p: Order` declares a property OF TYPE Order whose receiver is
	// Recv. The receiver is a sibling of the variable_declaration in the CST,
	// so a capture that walked the whole property_declaration would emit an
	// edge to Recv — a confidently wrong target of exactly the shape that cost
	// swift half its edges (#7047).
	src := `package p

class Recv
class Order

class Holder {
    val Recv.p: Order get() = x
}
`
	recs := ktExtract(t, "H.kt", src)
	ktAssertEdges(t, ktFieldTypeEdges(recs), []string{
		"Holder.p -> scope:component:class:kotlin:H.kt:Order",
	})
}

func TestKotlinFieldTypeRefs_ConstructorParameterDefaultValueIsNotScanned(t *testing.T) {
	// The default expression sits after `=` in the same class_parameter node.
	// Scanning it would collect Line from `emptyList<Line>()` — a real type
	// mention, but NOT the field's declared type, so the edge would claim
	// something false.
	src := `package p

class Line
class Order

class Holder(val xs: Order = build<Line>())
`
	recs := ktExtract(t, "H.kt", src)
	ktAssertEdges(t, ktFieldTypeEdges(recs), []string{
		"Holder.xs -> scope:component:class:kotlin:H.kt:Order",
	})
}

func TestKotlinFieldTypeRefs_UntypedPropertyProducesNothing(t *testing.T) {
	src := `package p

class Order

class Holder {
    val inferred = Order()
}
`
	recs := ktExtract(t, "H.kt", src)
	ktAssertEdges(t, ktFieldTypeEdges(recs), nil)
}

// ---------------------------------------------------------------------------
// The TARGET allow-list, and the not-a-declaration audit.
// ---------------------------------------------------------------------------

func TestKotlinFieldTypeRefs_TargetKindSpace(t *testing.T) {
	// Each row declares one target construct and one field typed by it, and
	// names the Kind/Subtype the extractor ACTUALLY mints for it.
	//
	// That column exists because the source spelling and the extracted subtype
	// are two different axes, and the labels alone conflated them: `annotation
	// class` / `expect class` / `external class` — and `sealed`, `value` and
	// `abstract`, probed identically — all land on SCOPE.Component/**class**,
	// byte-identical to the plain `class` row (review, §8). Those rows vary the
	// SOURCE SPELLING and nothing else; `wantKind` says so per row, so a label
	// can no longer claim a distinction the content does not carry. The rows
	// that genuinely move the extracted subtype are class / data_class /
	// interface / object / enum / type_alias — six, not eleven.
	rows := []struct {
		label string
		decl  string
		// wantRecords is EVERY record the extractor mints named `Tgt`, as
		// "Kind/Subtype" in emission order — asserted so the row's claim
		// about WHICH kind it exercises is observed rather than asserted in
		// a comment. Empty = mints nothing named Tgt, which is itself the
		// premise of that row's absent edge.
		wantRecords []string
		want        bool
	}{
		{"class", "class Tgt", []string{"SCOPE.Component/class"}, true},
		{"data class", "data class Tgt(val n: Int)", []string{"SCOPE.Component/data_class"}, true},
		{"interface", "interface Tgt", []string{"SCOPE.Component/interface"}, true},
		{"object", "object Tgt", []string{"SCOPE.Component/object"}, true},
		// The enum row is the one that mints TWO records; the edge binding
		// anyway is the component-family half of the ambiguity rule.
		{"enum class", "enum class Tgt { A, B }", []string{"SCOPE.Enum/enum", "SCOPE.Component/enum"}, true},
		{"typealias", "typealias Tgt = String", []string{"SCOPE.Schema/type_alias"}, true},
		// Source-spelling rows: same extracted subtype as `class` above.
		{"annotation class (extracted as class)", "annotation class Tgt", []string{"SCOPE.Component/class"}, true},
		{"expect class (extracted as class)", "expect class Tgt", []string{"SCOPE.Component/class"}, true},
		{"external class (extracted as class)", "external class Tgt", []string{"SCOPE.Component/class"}, true},
		{"sealed class (extracted as class)", "sealed class Tgt", []string{"SCOPE.Component/class"}, true},
		{"value class (extracted as class)", "value class Tgt(val n: Int)", []string{"SCOPE.Component/class"}, true},
		{"abstract class (extracted as class)", "abstract class Tgt", []string{"SCOPE.Component/class"}, true},
		{"undeclared (nothing named Tgt)", "class Other", nil, false},
		{"top-level function", "fun Tgt(): Int = 1", []string{"SCOPE.Operation/function"}, false},
	}
	for _, row := range rows {
		t.Run(row.label, func(t *testing.T) {
			src := "package p\n\n" + row.decl + "\n\nclass Holder {\n    val f: Tgt = x\n}\n"
			recs := ktExtract(t, "H.kt", src)
			var gotRecords []string
			for i := range recs {
				if recs[i].Name == "Tgt" {
					gotRecords = append(gotRecords, recs[i].Kind+"/"+recs[i].Subtype)
				}
			}
			if strings.Join(gotRecords, ",") != strings.Join(row.wantRecords, ",") {
				t.Fatalf("%s: extractor minted Tgt as %v, row claims %v",
					row.label, gotRecords, row.wantRecords)
			}
			got := ktFieldTypeEdges(recs)
			if row.want {
				ktAssertEdges(t, got, []string{
					"Holder.f -> scope:component:class:kotlin:H.kt:Tgt",
				})
			} else if len(got) != 0 {
				t.Fatalf("%s: got %v, want no edge", row.label, got)
			}
		})
	}
}

func TestKotlinFieldTypeRefs_CompanionObjectIsNeverATarget(t *testing.T) {
	// A companion object is NOT a declaration of a nameable type: an
	// UNNAMED one is addressed as `Outer.Companion`, and a NAMED one as
	// `Outer.Named`. Neither is spelled by a bare `type_identifier`, and the
	// extractor mints no Component for either (walk has no companion_object
	// case), so a field typed `Companion` or `Named` finds no target.
	src := `package p

class Outer {
    companion object Named {
        val n: Int = 1
    }
}

class Holder {
    val a: Named = x
    val b: Companion = y
}
`
	recs := ktExtract(t, "H.kt", src)
	ktAssertEdges(t, ktFieldTypeEdges(recs), nil)
	for i := range recs {
		if recs[i].Name == "Named" || recs[i].Name == "Companion" {
			t.Fatalf("companion object minted an entity named %q — the "+
				"refusal above rests on it minting none", recs[i].Name)
		}
	}
}

func TestKotlinFieldTypeRefs_UnmintedDeclarationForms(t *testing.T) {
	// Two Kotlin forms that DO declare something and are unreachable to this
	// pass anyway. They are named because the not-a-declaration audit in
	// field_type_refs.go is presented as an enumeration, and an enumeration
	// with silent omissions is the same prose defect as an unqualified claim.
	// Both are RECALL floors (a missing edge), not over-fires, and both are
	// pre-existing extractor behaviour this arm neither introduced nor widened.
	//
	// Each half asserts its PREMISE — the wrong kind, the absent entity — so
	// it cannot pass vacuously if the extractor later starts minting them.
	src := `package p

class Order

fun interface Cb {
    fun go()
}

fun outer() {
    class Local(val o: Order)
}

class Holder {
    val c: Cb = x
    val real: Order = y
}
`
	recs := ktExtract(t, "H.kt", src)
	ktAssertEdges(t, ktFieldTypeEdges(recs), []string{
		// The positive control: the pass IS emitting in this file, so the
		// two absences below are refusals and not a dead pass.
		"Holder.real -> scope:component:class:kotlin:H.kt:Order",
	})
	// Both premises are ABSENCES of a record, asserted directly, so neither
	// half can pass because some other rule already caused the missing edge.
	// `Cb` is checked by NAME and not by kind: the extractor mints nothing
	// named Cb at all (probed — the review's description of it as a
	// SCOPE.Operation named Cb, and therefore a collider, is not what the
	// extractor does), and `go` surfaces BARE because no parentType is in
	// scope inside a `fun interface` body.
	sawBareMember := false
	for i := range recs {
		switch recs[i].Name {
		case "Cb":
			t.Fatalf("fun interface minted an entity named Cb (%s/%s) — the "+
				"missing `c -> Cb` edge rests on it minting none",
				recs[i].Kind, recs[i].Subtype)
		case "Local", "Local.o":
			t.Fatalf("local class minted %q (%s/%s) — this arm's silence "+
				"about local classes rests on them minting nothing",
				recs[i].Name, recs[i].Kind, recs[i].Subtype)
		case "go":
			sawBareMember = true
		}
	}
	if !sawBareMember {
		t.Fatal("expected the fun interface's member `go` to surface bare — " +
			"if it stopped surfacing, the fixture no longer exercises the form")
	}
}

func TestKotlinFieldTypeRefs_ImportCarrierIsNeverATarget(t *testing.T) {
	// A single-segment import (`import Order`, legal for a root-package type)
	// mints a SCOPE.Component/import whose Name is exactly `Order` — the Kotlin
	// analogue of Go's import placeholder, which was the one record arm D's
	// allow-list genuinely had to stop. Nothing else in this file declares
	// Order, so any edge here targets the carrier.
	src := `package p

import Order

class Holder {
    val o: Order = x
}
`
	recs := ktExtract(t, "H.kt", src)
	ktAssertEdges(t, ktFieldTypeEdges(recs), nil)
	// The premise: the carrier record really is named `Order`.
	carrier := false
	for i := range recs {
		if recs[i].Kind == "SCOPE.Component" && recs[i].Subtype == "import" && recs[i].Name == "Order" {
			carrier = true
		}
	}
	if !carrier {
		t.Fatal("no import carrier named Order — this test would pass vacuously")
	}
}

func TestKotlinFieldTypeRefs_DottedImportCarrierCannotCollide(t *testing.T) {
	// The ordinary import shape namespaces itself: buildImport keeps the FULL
	// dotted path as the Name, which no bare type_identifier can equal. Pinned
	// so a change to that rule surfaces here rather than as wrong edges.
	src := `package p

import com.acme.Order
`
	recs := ktExtract(t, "H.kt", src)
	for i := range recs {
		if recs[i].Subtype == "import" && !strings.Contains(recs[i].Name, ".") {
			t.Fatalf("import carrier Name %q is bare; it can now collide with a type name", recs[i].Name)
		}
	}
}

func TestKotlinFieldTypeRefs_FileCarrierIsNeverATarget(t *testing.T) {
	// The #577 file entity is a SCOPE.Component whose Name is the file PATH.
	// With a single-segment path it looks bare, but it keeps the `.kt`
	// extension and a type_identifier cannot contain a dot.
	src := `package p

class Holder {
    val f: Order = x
}
`
	recs := ktExtract(t, "Order.kt", src)
	ktAssertEdges(t, ktFieldTypeEdges(recs), nil)
}

func TestKotlinFieldTypeRefs_FieldIsNeverATarget(t *testing.T) {
	// A field entity's Name is dotted (`Holder.inner`), so it cannot be
	// spelled by a bare type_identifier. Refused by kind anyway.
	src := `package p

class Holder {
    val inner: Int = 1
    val f: Holder = x
}
`
	recs := ktExtract(t, "H.kt", src)
	// Holder is a real same-file class, so the self-reference IS emitted.
	ktAssertEdges(t, ktFieldTypeEdges(recs), []string{
		"Holder.f -> scope:component:class:kotlin:H.kt:Holder",
	})
}

func TestKotlinFieldTypeRefs_SelfReferentialFieldGetsAnEdge(t *testing.T) {
	// `class Node { val next: Node? }`. Arms D and F suppress the self case to
	// stay consistent with an adjacent declared-type edge they already ship;
	// Kotlin ships none, so there is nothing to stay consistent with, and the
	// CONTAINS edge runs owner→field, not field→owner. Arms A, C and G emit it.
	src := `package p

class Node {
    val next: Node? = null
}
`
	recs := ktExtract(t, "N.kt", src)
	ktAssertEdges(t, ktFieldTypeEdges(recs), []string{
		"Node.next -> scope:component:class:kotlin:N.kt:Node",
	})
}

// ---------------------------------------------------------------------------
// Same-file only — THREE conjuncts, graded one at a time, in both directions.
// ---------------------------------------------------------------------------

func TestKotlinFieldTypeRefs_OtherFileRecordsAreNeverTargets(t *testing.T) {
	// Conjunct A (pass 2, the target scan). A record from another file must
	// not become a target: the direction in which a stub is wrongly EMITTED.
	recs := ktExtract(t, "H.kt", `package p

class Holder {
    val o: Order = x
}
`)
	// Order is declared in a DIFFERENT file, so it is not in this record set
	// at all — the extractor is per-file. The unit test below drives the
	// cross-file record set directly; this row pins the integration floor.
	ktAssertEdges(t, ktFieldTypeEdges(recs), nil)
}

func TestKotlinFieldTypeRefs_StashIsDeletedFromEveryRecord(t *testing.T) {
	// The stash is scratch state; it must never reach the graph as entity
	// metadata, on an anchor or on a record the pass declined.
	src := `package p

class Order

class Holder {
    val o: Order = x
    val n: NoSuchType = y
}
`
	recs := ktExtract(t, "H.kt", src)
	for i := range recs {
		for _, k := range []string{"field_type_refs", "field_type_refs_owner"} {
			if _, ok := recs[i].Metadata[k]; ok {
				t.Fatalf("%s still carries the %q stash", recs[i].Name, k)
			}
		}
		// And no record is left holding an EMPTY Metadata map it did not have
		// before this pass existed — `n: NoSuchType` is the row that would.
		if recs[i].Metadata != nil && len(recs[i].Metadata) == 0 {
			t.Fatalf("%s was left with an empty Metadata map", recs[i].Name)
		}
	}
}

// ---------------------------------------------------------------------------
// The ambiguity rule: count the kinds the tier that resolves THIS address
// weighs. Kotlin has three same-file same-name rivals OUTSIDE that family.
// ---------------------------------------------------------------------------

func TestKotlinFieldTypeRefs_EnumValueSetSiblingDoesNotRefuseTheEnumTarget(t *testing.T) {
	// `enum class Status` mints BOTH a SCOPE.Component/enum and a SCOPE.Enum
	// value-set of the same name in the same file. SCOPE.Enum is not in the
	// component address family, so it never enters the tier that resolves a
	// component-space ref. Arm D's all-kinds rule would refuse every Kotlin
	// enum target.
	src := `package p

enum class Status { OPEN, CLOSED }

class Order {
    val s: Status = Status.OPEN
}
`
	recs := ktExtract(t, "O.kt", src)
	ktAssertEdges(t, ktFieldTypeEdges(recs), []string{
		"Order.s -> scope:component:class:kotlin:O.kt:Status",
	})
	// Premise: both records really exist, so the test is not vacuous.
	var vs, comp bool
	for i := range recs {
		if recs[i].Name != "Status" {
			continue
		}
		if recs[i].Kind == "SCOPE.Enum" {
			vs = true
		}
		if recs[i].Kind == "SCOPE.Component" {
			comp = true
		}
	}
	if !vs || !comp {
		t.Fatalf("premise missing: value-set=%v component=%v", vs, comp)
	}
}

func TestKotlinFieldTypeRefs_ConstGroupValueSetSiblingDoesNotRefuseTheObjectTarget(t *testing.T) {
	// An object whose body holds two or more `const val` string constants
	// mints a SCOPE.Enum value-set NAMED AFTER THE OBJECT — a second
	// same-name, same-file rival kind that has nothing to do with enums.
	src := `package p

object Pages {
    const val HOME = "home"
    const val ABOUT = "about"
}

class Nav {
    val p: Pages = Pages
}
`
	recs := ktExtract(t, "N.kt", src)
	ktAssertEdges(t, ktFieldTypeEdges(recs), []string{
		"Nav.p -> scope:component:class:kotlin:N.kt:Pages",
	})
	vs := false
	for i := range recs {
		if recs[i].Name == "Pages" && recs[i].Kind == "SCOPE.Enum" {
			vs = true
		}
	}
	if !vs {
		t.Fatal("premise missing: no const-group value-set named Pages")
	}
}

func TestKotlinFieldTypeRefs_SpringServiceSiblingDoesNotRefuseTheClassTarget(t *testing.T) {
	// A Spring stereotype mints a SCOPE.Service named after the class, in the
	// same file. SCOPE.Service is deliberately NOT in componentKindFamily
	// (refs.go:2347), so it cannot make a component-space ref ambiguous.
	src := `package p

@Service
class Billing

class Order {
    val b: Billing = x
}
`
	recs := ktExtract(t, "O.kt", src)
	ktAssertEdges(t, ktFieldTypeEdges(recs), []string{
		"Order.b -> scope:component:class:kotlin:O.kt:Billing",
	})
	svc := false
	for i := range recs {
		if recs[i].Kind == "SCOPE.Service" && recs[i].Name == "Billing" {
			svc = true
		}
	}
	if !svc {
		t.Fatal("premise missing: no SCOPE.Service named Billing")
	}
}

// ---------------------------------------------------------------------------
// The typealias target resolves through a DIFFERENT tier, so it counts
// differently.
// ---------------------------------------------------------------------------

func TestKotlinFieldTypeRefs_TypeAliasTargetIsRefusedWhenAnyKindCollides(t *testing.T) {
	// A SCOPE.Schema/type_alias is invisible to lookupLocationKind, so its ref
	// falls through to ambigLocation, which counts EVERY kind. A same-named
	// top-level function is enough to make it ambiguous — and that is a shape
	// Kotlin writes routinely (a typealias beside a factory function of the
	// same name).
	src := `package p

typealias Handler = (String) -> Unit

fun Handler(): Int = 1

class Holder {
    val h: Handler = x
}
`
	recs := ktExtract(t, "H.kt", src)
	ktAssertEdges(t, ktFieldTypeEdges(recs), nil)
	// Premise: the alias and the operation both exist with that name.
	var alias, op bool
	for i := range recs {
		if recs[i].Name != "Handler" {
			continue
		}
		if recs[i].Kind == "SCOPE.Schema" {
			alias = true
		}
		if recs[i].Kind == "SCOPE.Operation" {
			op = true
		}
	}
	if !alias || !op {
		t.Fatalf("premise missing: alias=%v operation=%v", alias, op)
	}
}

func TestKotlinFieldTypeRefs_ComponentTargetSurvivesTheSameCollision(t *testing.T) {
	// The mirror of the row above, holding everything constant but the target
	// kind: a same-named top-level function does NOT refuse a COMPONENT
	// target, because the component tier never weighs SCOPE.Operation. Without
	// this row the split rule looks like one rule.
	src := `package p

class Handler

fun Handler(): Int = 1

class Holder {
    val h: Handler = x
}
`
	recs := ktExtract(t, "H.kt", src)
	ktAssertEdges(t, ktFieldTypeEdges(recs), []string{
		"Holder.h -> scope:component:class:kotlin:H.kt:Handler",
	})
}

// ---------------------------------------------------------------------------
// The resolver actually binds what this pass emits.
// ---------------------------------------------------------------------------

func TestKotlinFieldTypeRefs_ResolverBindsEveryEdge(t *testing.T) {
	src := `package p

class Customer
interface Shipper
enum class Status { OPEN }
object Registry
typealias Handler = (String) -> Unit

class Order(val buyer: Customer) {
    val shipper: Shipper = s
    val status: Status = Status.OPEN
    val registry: Registry = Registry
    val handler: Handler = h
}
`
	recs := ktExtract(t, "Order.kt", src)
	edges := ktFieldTypeEdges(recs)
	if len(edges) != 5 {
		t.Fatalf("got %d edges, want 5: %v", len(edges), edges)
	}
	bound, dangling := ktResolveFieldTypeEdges(t, recs)
	if dangling != 0 {
		t.Fatalf("%d of %d edges dangle after the real resolver", dangling, bound+dangling)
	}
	if bound != 5 {
		t.Fatalf("resolver bound %d edges, want 5", bound)
	}
}

func TestKotlinFieldTypeRefs_AnnotationOnAPropertyIsNotItsDeclaredType(t *testing.T) {
	// A property's annotations sit BEFORE the colon, and tree-sitter-kotlin
	// spells an annotation's name with a `user_type` — the same node the
	// candidate scanner collects from. Without the post-colon window, an
	// annotated property in a file that also declares the annotation class
	// would emit `o -> Marker`: a real declaration, a real same-file target,
	// and not the field's type. This is the distinguishing input for that
	// conjunct, and annotated properties are the common Kotlin shape.
	src := `package p

annotation class Marker
class Order

class Holder {
    @Marker val o: Order = x
}
`
	recs := ktExtract(t, "H.kt", src)
	ktAssertEdges(t, ktFieldTypeEdges(recs), []string{
		"Holder.o -> scope:component:class:kotlin:H.kt:Order",
	})
}

func TestKotlinFieldTypeRefs_AnnotationOnAConstructorParamIsNotItsDeclaredType(t *testing.T) {
	// The same conjunct on the OTHER anchor: a class_parameter carries its
	// annotations as a `modifiers` child, also before the colon.
	src := `package p

annotation class Marker
class Order

class Holder(@Marker val o: Order)
`
	recs := ktExtract(t, "H.kt", src)
	ktAssertEdges(t, ktFieldTypeEdges(recs), []string{
		"Holder.o -> scope:component:class:kotlin:H.kt:Order",
	})
}
