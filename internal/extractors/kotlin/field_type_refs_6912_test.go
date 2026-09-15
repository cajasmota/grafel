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
	// pinned this as a KNOWN OVER-FIRE; Kotlin refuses it, because the
	// declaration's `type_parameters` list is in hand at the emit site.
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
	// Each row declares one target construct and one field typed by it.
	rows := []struct {
		label string
		decl  string
		want  bool
	}{
		{"class", "class Tgt", true},
		{"data class", "data class Tgt(val n: Int)", true},
		{"interface", "interface Tgt", true},
		{"object", "object Tgt", true},
		{"enum class", "enum class Tgt { A, B }", true},
		{"annotation class", "annotation class Tgt", true},
		{"expect class", "expect class Tgt", true},
		{"external class", "external class Tgt", true},
		{"typealias", "typealias Tgt = String", true},
		{"undeclared (nothing named Tgt)", "class Other", false},
		{"top-level function", "fun Tgt(): Int = 1", false},
	}
	for _, row := range rows {
		t.Run(row.label, func(t *testing.T) {
			src := "package p\n\n" + row.decl + "\n\nclass Holder {\n    val f: Tgt = x\n}\n"
			recs := ktExtract(t, "H.kt", src)
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
