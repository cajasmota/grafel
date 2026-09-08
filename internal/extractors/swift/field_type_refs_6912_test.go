// Package swift_test — issue #6912 arm G: the Swift field→declared-type edge.
//
// AXES THESE FIXTURES VARY, and the axes they HOLD CONSTANT. Stated because the
// dominant defect on this board is that enumerating one axis thoroughly is what
// makes its sibling look covered.
//
// VARIED
//   - TYPE-EXPRESSION SHAPE: bare, optional, implicitly-unwrapped, array,
//     dictionary-literal, generic with one and with two arguments, nested
//     generic, tuple, function type, existential (`any`), dotted. Each shape is
//     asserted against ONE target name so the shape is the only moving part.
//   - TARGET KIND: struct, class, protocol, enum (Component beside its
//     SCOPE.Enum value-set), typealias (SCOPE.Schema). These resolve through
//     two different resolver tiers and carry two different ambiguity rules.
//   - COLLIDER KIND: SCOPE.Enum (must NOT suppress a Component) and
//     SCOPE.Operation (MUST suppress an alias), each with a positive control of
//     the same declaration form and no collider, so a passing assertion cannot
//     mean "that target kind never works". The third collider — a second
//     COMPONENT-FAMILY kind, which must suppress a Component — has no
//     source-level fixture, because no Swift emit site produces one today; it is
//     graded in field_type_refs_scope_6912_test.go instead.
//   - OWNER DECLARATION FORM: `struct` and `class` (Node), plus a generic
//     `struct Box<T>`.
//
// HELD CONSTANT
//   - FILE OF THE RIVAL: every fixture here is single-file except
//     ForeignFileTypeIsNeverATarget. That is a recall statement, not a grading
//     of the same-file conjuncts — Extract sees one file at a time, so those
//     conjuncts are unreachable from this package and are graded internally in
//     field_type_refs_scope_6912_test.go, in both directions.
//   - REPO: one repo id throughout. Cross-repo name collision is not modelled
//     by this pass and is not graded here.
//   - LANGUAGE SEGMENT of the address: always "swift".
//   - FIELD MUTABILITY / ACCESS / PROPERTY WRAPPERS: `var` with no access
//     modifier, except the one `@Published` case whose subject IS the wrapper.
//     `let`, `weak`, `lazy` and access modifiers are NOT varied here: none of
//     them reaches a decision this pass makes, since candidates come from the
//     type_annotation subtree only — but that is an argument, and the fixtures
//     do not demonstrate it.
//   - NESTING: no nested type declarations. walkBody emits no entity for a type
//     declared inside a class body, so a nested type is not a candidate target
//     in any of these fixtures — that is a property of #4854's walk, not of
//     this pass, and it is not graded here.
package swift_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"

	extreg "github.com/cajasmota/grafel/internal/extractor"
)

const swFTPath = "Sources/App/Models.swift"
const swFTRivalPath = "Sources/App/Other.swift"

// swFTSrc is the main fixture. Every field below is referenced by name in at
// least one assertion.
const swFTSrc = `import Foundation

typealias Tier = Int

enum Status {
    case open
    case closed
}

protocol Shipper {
    func ship()
}

struct Customer {
    var name: String
}

class Node {
    var next: Node?
}

struct Order {
    var buyer: Customer
    var maybe: Customer?
    var forced: Customer!
    var list: [Customer]
    var dict: Dictionary<String, Customer>
    var short: [String: Customer]
    var pair: (Int, Customer)
    var fn: (Customer) -> Void
    var res: Result<Customer, Shipper>
    var deep: [Dictionary<String, Customer>]
    var both: (Customer, Customer)
    var boxed: any Shipper
    var ship: Shipper
    var status: Status
    var tier: Tier
    var count: Int
    var label: String
    var inferred = Customer()
    var computed: Customer { return Customer() }
}
`

// swFTRivalSrc declares a SECOND Customer in a different file, so the BARE name
// `Customer` is ambiguous across the graph. Without it the structural address
// would be ungraded in TestSwiftFieldTypeRefs_ResolvesToEntityIDs — a bare-name
// ToID would bind just as well.
const swFTRivalSrc = `struct Customer {
    var id: Int
}
`

func swFTExtract(t *testing.T, files map[string]string) []types.EntityRecord {
	t.Helper()
	var out []types.EntityRecord
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		out = append(out, swExtract(t, files[p], p)...)
	}
	return out
}

func swFTOne(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	return swExtract(t, src, swFTPath)
}

// swFTTargetsOf returns the sorted target_type values of the field-type edges
// carried by the field entity named `<owner>.<field>`.
func swFTTargetsOf(recs []types.EntityRecord, dotted string) []string {
	var out []string
	for i := range recs {
		r := &recs[i]
		if r.Kind != "SCOPE.Schema" || r.Subtype != "field" || r.Name != dotted {
			continue
		}
		for _, rel := range r.Relationships {
			if rel.Kind == "REFERENCES" && rel.Properties.Get("ref_kind") == "field_target_type" {
				out = append(out, rel.Properties.Get("target_type"))
			}
		}
	}
	sort.Strings(out)
	return out
}

func swFTFieldExists(t *testing.T, recs []types.EntityRecord, dotted string) {
	t.Helper()
	for i := range recs {
		if recs[i].Kind == "SCOPE.Schema" && recs[i].Subtype == "field" && recs[i].Name == dotted {
			return
		}
	}
	t.Fatalf("fixture premise broken: no field entity named %q — any assertion "+
		"about its edges would pass vacuously", dotted)
}

func swFTWant(t *testing.T, recs []types.EntityRecord, dotted string, want ...string) {
	t.Helper()
	swFTFieldExists(t, recs, dotted)
	got := swFTTargetsOf(recs, dotted)
	if want == nil {
		want = []string{}
	}
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("%s targets = %v, want %v", dotted, got, want)
	}
}

// ---------------------------------------------------------------------------
// The trap this arm exists to close.
// ---------------------------------------------------------------------------

// TestSwiftFieldTypeRefs_FieldTypePropertyIsLossyForGenerics pins the reason
// this arm reads the AST rather than the `field_type` property, as a live
// expectation instead of a comment. If someone later fixes firstDescendantText
// so the property carries the whole type, this test fails and points at the
// header block that says why the property was not the input.
func TestSwiftFieldTypeRefs_FieldTypePropertyIsLossyForGenerics(t *testing.T) {
	recs := swFTOne(t, swFTSrc)
	prop := func(dotted string) string {
		for i := range recs {
			if recs[i].Kind == "SCOPE.Schema" && recs[i].Subtype == "field" && recs[i].Name == dotted {
				return recs[i].Properties["field_type"]
			}
		}
		t.Fatalf("no field entity %q", dotted)
		return ""
	}
	for _, c := range []struct{ field, want string }{
		{"Order.dict", "Dictionary"},
		{"Order.res", "Result"},
		{"Order.buyer", "Customer"},
	} {
		if got := prop(c.field); got != c.want {
			t.Errorf("field_type[%s] = %q, want %q — the premise of this arm's "+
				"design changed; re-read field_type_refs.go's header before "+
				"editing this test", c.field, got, c.want)
		}
	}
	// And the consequence: the edge is NOT to the wrapper.
	swFTWant(t, recs, "Order.dict", "Customer")
	swFTWant(t, recs, "Order.res", "Customer", "Shipper")
}

// ---------------------------------------------------------------------------
// Type-expression shapes.
// ---------------------------------------------------------------------------

func TestSwiftFieldTypeRefs_TypeExpressionShapes(t *testing.T) {
	recs := swFTOne(t, swFTSrc)
	for _, c := range []struct {
		field string
		want  []string
	}{
		{"Order.buyer", []string{"Customer"}},
		{"Order.maybe", []string{"Customer"}},
		{"Order.forced", []string{"Customer"}},
		{"Order.list", []string{"Customer"}},
		{"Order.dict", []string{"Customer"}},
		{"Order.short", []string{"Customer"}},
		{"Order.pair", []string{"Customer"}},
		{"Order.fn", []string{"Customer"}},
		{"Order.deep", []string{"Customer"}},
		// One target, not two: the same name twice in one type expression is
		// one relation. Without the per-field dedupe this row is [Customer
		// Customer] and the graph carries a duplicate edge.
		{"Order.both", []string{"Customer"}},
		{"Order.boxed", []string{"Shipper"}},
		{"Order.ship", []string{"Shipper"}},
	} {
		swFTWant(t, recs, c.field, c.want...)
	}
}

// TestSwiftFieldTypeRefs_InferredAndComputedProducesNothing — `var inferred =
// Customer()` has no type_annotation and the initializer is not read; a
// computed property is not a stored member and has no field entity at all.
func TestSwiftFieldTypeRefs_InferredAndComputedProducesNothing(t *testing.T) {
	recs := swFTOne(t, swFTSrc)
	swFTWant(t, recs, "Order.inferred")
	for i := range recs {
		if recs[i].Kind == "SCOPE.Schema" && recs[i].Subtype == "field" && recs[i].Name == "Order.computed" {
			t.Fatal("Order.computed has a field entity — the #4854 computed-property " +
				"exclusion changed and this test's premise with it")
		}
	}
}

// ---------------------------------------------------------------------------
// Dotted types — both directions of the wrong binding.
// ---------------------------------------------------------------------------

// TestSwiftFieldTypeRefs_DottedTypeBindsNeitherHeadNorTail constructs the two
// wrong bindings a dotted user_type invites and asserts NEITHER is made, with a
// positive control in the same file so the assertion cannot mean "no edge is
// ever emitted here".
func TestSwiftFieldTypeRefs_DottedTypeBindsNeitherHeadNorTail(t *testing.T) {
	src := `struct Foundation {
    var v: Int
}

struct Data {
    var v: Int
}

struct Inner {
    var v: Int
}

struct Customer {
    var v: Int
}

struct Order {
    var qualified: Foundation.Data
    var nested: Customer.Inner
    var meta: Customer.Type
    var generic: Foundation.Array<Customer>
    var control: Customer
}
`
	recs := swFTOne(t, src)
	swFTWant(t, recs, "Order.qualified") // not Foundation (head), not Data (tail)
	swFTWant(t, recs, "Order.nested")    // not Customer (head), not Inner (tail)
	swFTWant(t, recs, "Order.meta")      // metatype: the documented price
	// Type ARGUMENTS are still descended into even under a dotted head.
	swFTWant(t, recs, "Order.generic", "Customer")
	// Positive control: the same file, the same target name, undotted.
	swFTWant(t, recs, "Order.control", "Customer")
}

// ---------------------------------------------------------------------------
// Primitives / stdlib names, both directions.
// ---------------------------------------------------------------------------

func TestSwiftFieldTypeRefs_PrimitiveFieldsProduceNoEdge(t *testing.T) {
	recs := swFTOne(t, swFTSrc)
	swFTWant(t, recs, "Order.count")
	swFTWant(t, recs, "Order.label")
}

// TestSwiftFieldTypeRefs_ShadowedStdlibNameIsATarget is the other direction: a
// file that DECLARES `struct Int` means that type in field position, and a
// primitive blocklist would refuse the one edge that should exist. This is the
// case that makes the absence of a blocklist a decision rather than an
// omission.
func TestSwiftFieldTypeRefs_ShadowedStdlibNameIsATarget(t *testing.T) {
	src := `struct Int {
    var v: Bool
}

struct Order {
    var n: Int
}
`
	recs := swFTOne(t, src)
	swFTWant(t, recs, "Order.n", "Int")
}

// ---------------------------------------------------------------------------
// The two ambiguity tiers — each with a positive control.
// ---------------------------------------------------------------------------

// TestSwiftFieldTypeRefs_EnumValueSetDoesNotSuppressItsComponent is arm F's
// rule for Swift. `enum Status` emits a SCOPE.Component AND a same-named
// SCOPE.Enum value-set in the SAME FILE. SCOPE.Enum trims to "Enum", which is
// in no kind family, so lookupLocationKind resolves the Component before
// ambigLocation is consulted — and arm D's all-kinds rule would refuse EVERY
// Swift enum target.
//
// The two-record premise is asserted FIRST so the test cannot pass because the
// value-set stopped being emitted.
func TestSwiftFieldTypeRefs_EnumValueSetDoesNotSuppressItsComponent(t *testing.T) {
	recs := swFTOne(t, swFTSrc)
	var comp, vset int
	for i := range recs {
		if recs[i].Name != "Status" || recs[i].SourceFile != swFTPath {
			continue
		}
		switch recs[i].Kind {
		case "SCOPE.Component":
			comp++
		case "SCOPE.Enum":
			vset++
		}
	}
	if comp != 1 || vset != 1 {
		t.Fatalf("premise broken: Status has %d SCOPE.Component and %d SCOPE.Enum "+
			"records in %s; this test only grades the rule when BOTH exist",
			comp, vset, swFTPath)
	}
	swFTWant(t, recs, "Order.status", "Status")
}

// The suppression half of the component tier — a rival that IS in the component
// address family must suppress — has no source-level fixture: no Swift emit site
// produces a second component-family kind for one name in one file today, and
// writing Swift that "would" is fabricating an example. It is graded at the
// function level instead, in
// TestSwiftFieldTypeRefs_TheTwoCollisionScansAreIndependent
// (field_type_refs_scope_6912_test.go), which also pins that the two scans
// suppress DIFFERENT things.

// TestSwiftFieldTypeRefs_AliasShadowedByAnOperationWouldDangleUnderArmFsRule is
// the alias tier, and it grades the DEVIATION rather than the agreement: a
// top-level `func Tier()` beside `typealias Tier` is Schema + Operation. Neither
// is in the component address family, so arm F's component-scoped count sees
// ONE kind and would admit the target — and the edge would then dangle, because
// an alias target is resolved by the kind-agnostic ambigLocation/byLocation
// pair, which counts every kind.
//
// The dangle is not asserted from the emit decision; the real resolver is
// driven and asked whether such an address binds.
func TestSwiftFieldTypeRefs_AliasShadowedByAnOperationWouldDangleUnderArmFsRule(t *testing.T) {
	src := `typealias Tier = Int

func Tier() {
}

typealias Grade = Int

struct Order {
    var t: Tier
    var g: Grade
}
`
	recs := swFTOne(t, src)

	// Premise: both kinds really are emitted for Tier, and only one for Grade.
	kinds := map[string]map[string]bool{}
	for i := range recs {
		if recs[i].SourceFile != swFTPath || recs[i].Name == "" {
			continue
		}
		if kinds[recs[i].Name] == nil {
			kinds[recs[i].Name] = map[string]bool{}
		}
		kinds[recs[i].Name][recs[i].Kind] = true
	}
	if len(kinds["Tier"]) != 2 {
		t.Fatalf("premise broken: Tier carries kinds %v, want exactly two "+
			"(SCOPE.Schema + SCOPE.Operation)", kinds["Tier"])
	}
	if len(kinds["Grade"]) != 1 {
		t.Fatalf("premise broken: Grade carries kinds %v, want exactly one", kinds["Grade"])
	}
	// Neither collider kind is in the component address family — which is
	// exactly why arm F's rule would not see this collision.
	for k := range kinds["Tier"] {
		if k == "SCOPE.Component" {
			t.Fatalf("premise broken: %q is a component-family kind, so this "+
				"collision IS visible to arm F's rule and the test grades nothing", k)
		}
	}

	swFTWant(t, recs, "Order.t")          // refused
	swFTWant(t, recs, "Order.g", "Grade") // positive control: same form, no collider

	// And the reason: the address arm F's rule would have emitted does not bind.
	for i := range recs {
		if recs[i].ID == "" {
			recs[i].ID = graph.EntityID("issue6912", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
		}
	}
	idx := resolve.BuildIndex(recs)
	probe := []types.EntityRecord{{
		ID: "probe", Kind: "SCOPE.Schema", Subtype: "field", Name: "Probe.t",
		SourceFile: swFTPath,
		Relationships: []types.RelationshipRecord{
			{FromID: "probe", ToID: extreg.BuildComponentStructuralRef("swift", swFTPath, "Tier"), Kind: "REFERENCES"},
			{FromID: "probe", ToID: extreg.BuildComponentStructuralRef("swift", swFTPath, "Grade"), Kind: "REFERENCES"},
		},
	}}
	resolve.ReferencesEmbedded(probe, idx)
	ids := map[string]bool{}
	for i := range recs {
		ids[recs[i].ID] = true
	}
	if ids[probe[0].Relationships[0].ToID] {
		t.Errorf("the shadowed alias address BOUND to %q — arm F's rule would not "+
			"have dangled here and this arm's alias tier is unmotivated; re-derive "+
			"before deleting it", probe[0].Relationships[0].ToID)
	}
	if !ids[probe[0].Relationships[1].ToID] {
		t.Errorf("the UNSHADOWED alias address did not bind (%q) — the negative "+
			"result above is then about aliases in general, not about the collision",
			probe[0].Relationships[1].ToID)
	}
}

// TestSwiftFieldTypeRefs_UnshadowedAliasIsATarget — the recall half of the
// alias tier, on the main fixture. Arm F's Component-only allow-list would drop
// this edge entirely; arm D measured that shape at 19% of Go's edges.
func TestSwiftFieldTypeRefs_UnshadowedAliasIsATarget(t *testing.T) {
	recs := swFTOne(t, swFTSrc)
	swFTWant(t, recs, "Order.tier", "Tier")
}

// ---------------------------------------------------------------------------
// The two same-file conjuncts, in OPPOSITE failure directions.
// ---------------------------------------------------------------------------

// The two SourceFile conjuncts themselves are graded in
// field_type_refs_scope_6912_test.go, an INTERNAL test: Extract is called once
// per file, so from out here neither conjunct can ever fire and a mutant that
// deletes either survives this whole file. What IS graded here is the
// end-to-end recall statement that follows.
//
// TestSwiftFieldTypeRefs_ForeignFileTypeIsNeverATarget — a type declared only in
// another file yields nothing, with a same-file control in the same fixture.
// This is the honest floor of a same-file rule, asserted rather than described.
func TestSwiftFieldTypeRefs_ForeignFileTypeIsNeverATarget(t *testing.T) {
	src := `struct Order {
    var buyer: Customer
    var ship: Shipper
}

protocol Shipper {
    func ship()
}
`
	recs := swFTExtract(t, map[string]string{
		swFTPath:      src,
		swFTRivalPath: swFTRivalSrc, // declares Customer, other file
	})
	swFTWant(t, recs, "Order.buyer")           // cross-file: nothing
	swFTWant(t, recs, "Order.ship", "Shipper") // same-file control
}

// ---------------------------------------------------------------------------
// Scoping, shadowing and the records that must never be targets.
// ---------------------------------------------------------------------------

// TestSwiftFieldTypeRefs_TypeParameterShadowedByASameFileTypeGetsNoEdge — arms
// A and D both ship this as a KNOWN-WRONG over-fire. Swift refuses it, because
// the owning declaration node is already in hand at the collection site.
func TestSwiftFieldTypeRefs_TypeParameterShadowedByASameFileTypeGetsNoEdge(t *testing.T) {
	src := `struct T {
    var v: Int
}

struct Customer {
    var v: Int
}

struct Box<T> {
    var item: T
    var owner: Customer
}
`
	recs := swFTOne(t, src)
	swFTWant(t, recs, "Box.item")              // T is the parameter, not the struct
	swFTWant(t, recs, "Box.owner", "Customer") // control: same owner, real type
}

// TestSwiftFieldTypeRefs_PropertyWrapperTypeIsNotACandidate — `@Published var s:
// Customer` shapes `Published` as a user_type under a modifiers>attribute
// sibling of the type_annotation. A same-file `struct Published` is declared so
// the assertion can actually fail if candidates were collected from the whole
// property_declaration.
func TestSwiftFieldTypeRefs_PropertyWrapperTypeIsNotACandidate(t *testing.T) {
	src := `struct Published {
    var v: Int
}

struct Customer {
    var v: Int
}

class Model {
    @Published var s: Customer
}
`
	recs := swFTOne(t, src)
	swFTWant(t, recs, "Model.s", "Customer")
}

// TestSwiftFieldTypeRefs_ImportCarrierNameCannotCollide pins the upstream
// protection this pass RELIES on rather than re-implements: buildImport
// namespaces the carrier as `<file>::import::<module>` (#492), so it cannot
// equal a bare type_identifier. Go's import placeholder carried a bare name and
// was the one record its allow-list genuinely had to stop; if this namespacing
// is ever removed, this test is where it surfaces.
func TestSwiftFieldTypeRefs_ImportCarrierNameCannotCollide(t *testing.T) {
	src := `import Customer

struct Order {
    var c: Customer
}
`
	recs := swFTOne(t, src)
	saw := false
	for i := range recs {
		if recs[i].Kind == "SCOPE.Component" && recs[i].Subtype == "module" {
			saw = true
			if !strings.Contains(recs[i].Name, "::import::") {
				t.Errorf("import carrier Name = %q — it no longer carries the #492 "+
					"namespacing, so a bare `import Foo` can now collide with a "+
					"same-file `struct Foo` and this pass needs an explicit guard",
					recs[i].Name)
			}
		}
	}
	if !saw {
		t.Fatal("premise broken: no import carrier entity emitted")
	}
	// `Customer` is imported and NOT declared here, so no edge.
	swFTWant(t, recs, "Order.c")
}

// TestSwiftFieldTypeRefs_SelfReferentialFieldGetsAnEdge — the deliberate
// departure from arm D, which suppresses the self case only because Go ships an
// adjacent struct-anchored DEPENDS_ON that declines it. Swift has no such edge;
// arms A and C, which likewise have none, emit it.
func TestSwiftFieldTypeRefs_SelfReferentialFieldGetsAnEdge(t *testing.T) {
	recs := swFTOne(t, swFTSrc)
	swFTWant(t, recs, "Node.next", "Node")
}

// TestSwiftFieldTypeRefs_ExtensionBesideItsTypeIsStillOneNode is the SOURCE-LEVEL
// form of #7038's direction, and it is not a contrived shape: `extension Foo`
// beside `struct Foo` is ordinary Swift, and it is what the corpus measurement
// found. The walk emits a SECOND SCOPE.Component for the extension (subtype
// defaults to "class"), so the name carries TWO RECORDS and ONE graph node —
// EntityID hashes (repo, Kind, Name, SourceFile) with Subtype excluded.
//
// Arm C's record-counting rule would refuse the target here. Measured on the
// corpus that is 5 of 38 edges (13.2%), every one of them an extension in the
// declaring file. The two-record premise is asserted FIRST so the test cannot
// pass because the extension stopped producing a record.
func TestSwiftFieldTypeRefs_ExtensionBesideItsTypeIsStillOneNode(t *testing.T) {
	src := `struct Customer {
    var name: String
}

extension Customer {
    static let empty = Customer(name: "")
}

struct Order {
    var buyer: Customer
}
`
	recs := swFTOne(t, src)
	records, kinds := 0, map[string]bool{}
	for i := range recs {
		if recs[i].Name == "Customer" && recs[i].SourceFile == swFTPath {
			records++
			kinds[recs[i].Kind] = true
		}
	}
	if records < 2 || len(kinds) != 1 {
		t.Fatalf("premise broken: Customer has %d records across kinds %v; this "+
			"test only grades the record-vs-kind distinction when there are two "+
			"records under ONE kind", records, kinds)
	}
	swFTWant(t, recs, "Order.buyer", "Customer")
}

// TestSwiftFieldTypeRefs_StashIsClearedFromMetadata — the candidate stash is an
// internal handoff between the walk and the attach pass. Leaving it on the
// record would ship extractor scratch state into the graph as entity metadata.
func TestSwiftFieldTypeRefs_StashIsClearedFromMetadata(t *testing.T) {
	recs := swFTOne(t, swFTSrc)
	saw := false
	for i := range recs {
		if recs[i].Kind == "SCOPE.Schema" && recs[i].Subtype == "field" {
			saw = true
		}
		if recs[i].Metadata == nil {
			continue
		}
		if v, ok := recs[i].Metadata["field_type_refs"]; ok {
			t.Errorf("%s still carries the field_type_refs stash: %v", recs[i].Name, v)
		}
	}
	if !saw {
		t.Fatal("no field records in fixture — the absence of a stash is vacuous")
	}
}

// ---------------------------------------------------------------------------
// The whole thing, through the real resolver.
// ---------------------------------------------------------------------------

// TestSwiftFieldTypeRefs_ResolvesToEntityIDs drives every emitted edge through
// resolve.BuildIndex → resolve.ReferencesEmbedded and asserts EACH ONE BINDS,
// naming the entity it lands on. #6912's hard requirement is that a non-binding
// edge is worse than no edge: a dangling stub is kept verbatim and classified
// bug-extractor, so a mis-addressed edge is a full hit on the disposition
// denominator.
//
// The rival file is indexed too, so the BARE name `Customer` is ambiguous
// across the graph — asserted explicitly, because if it were not, a bare-name
// ToID would pass here and the structural address would be ungraded.
func TestSwiftFieldTypeRefs_ResolvesToEntityIDs(t *testing.T) {
	recs := swFTExtract(t, map[string]string{
		swFTPath:      swFTSrc,
		swFTRivalPath: swFTRivalSrc,
	})
	for i := range recs {
		if recs[i].ID == "" {
			recs[i].ID = graph.EntityID("issue6912", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
		}
	}
	idx := resolve.BuildIndex(recs)

	before := 0
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind == "REFERENCES" && r.Properties.Get("ref_kind") == "field_target_type" {
				before++
			}
		}
	}
	if before == 0 {
		t.Fatal("no field-type edges to drive through the resolver")
	}

	if _, st := idx.LookupStatusHint("Customer", "REFERENCES"); st == 1 {
		t.Fatalf("the bare name %q binds in this fixture, so the ambiguity this "+
			"test controls for is absent and the structural address is ungraded",
			"Customer")
	}

	idToName := map[string]string{}
	for i := range recs {
		idToName[recs[i].ID] = recs[i].SourceFile + ":" + recs[i].Kind + "/" +
			recs[i].Subtype + ":" + recs[i].Name
	}

	resolve.ReferencesEmbedded(recs, idx)

	var bound []string
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "REFERENCES" || r.Properties.Get("ref_kind") != "field_target_type" {
				continue
			}
			name, ok := idToName[r.ToID]
			if !ok {
				t.Errorf("%s:%s -[REFERENCES]-> %q did NOT bind to an entity id "+
					"(dangling stub → bug-extractor)", recs[i].SourceFile, recs[i].Name, r.ToID)
				continue
			}
			bound = append(bound, recs[i].Name+" => "+name)
		}
	}
	sort.Strings(bound)

	const m = swFTPath + ":"
	want := []string{
		"Node.next => " + m + "SCOPE.Component/class:Node",
		"Order.both => " + m + "SCOPE.Component/struct:Customer",
		"Order.boxed => " + m + "SCOPE.Component/protocol:Shipper",
		"Order.buyer => " + m + "SCOPE.Component/struct:Customer",
		"Order.deep => " + m + "SCOPE.Component/struct:Customer",
		"Order.dict => " + m + "SCOPE.Component/struct:Customer",
		"Order.fn => " + m + "SCOPE.Component/struct:Customer",
		"Order.forced => " + m + "SCOPE.Component/struct:Customer",
		"Order.list => " + m + "SCOPE.Component/struct:Customer",
		"Order.maybe => " + m + "SCOPE.Component/struct:Customer",
		"Order.pair => " + m + "SCOPE.Component/struct:Customer",
		"Order.res => " + m + "SCOPE.Component/protocol:Shipper",
		"Order.res => " + m + "SCOPE.Component/struct:Customer",
		"Order.ship => " + m + "SCOPE.Component/protocol:Shipper",
		"Order.short => " + m + "SCOPE.Component/struct:Customer",
		"Order.status => " + m + "SCOPE.Component/enum:Status",
		"Order.tier => " + m + "SCOPE.Schema/type_alias:Tier",
	}
	sort.Strings(want)
	if strings.Join(bound, "\n") != strings.Join(want, "\n") {
		t.Errorf("bound edges:\n%s\nwant:\n%s", strings.Join(bound, "\n"), strings.Join(want, "\n"))
	}
}
