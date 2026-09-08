package java_test

// #6912 arm E — the field→declared-type edge for Java. See field_type_refs.go
// for the kind / address / same-file / allow-list / ambiguity decisions; this
// file grades them.
//
// Every assertion below names FIELDS AND TARGETS. A count table cannot see a
// substitution (#6973) and recall cannot see over-firing at all (#6926), so the
// forbidden set is asserted BY NAME beside the expected set: named primitive
// fields, a named java.lang field, a named boxed field, a named
// package-qualified field, a named nested-qualified field, the self-reference,
// the cross-file target, and the unbound type parameter.
//
// AXES. The main fixture VARIES the wrapper form (bare, array, generic
// argument, qualified-generic argument, map key AND value, wildcard,
// user-generic argument) and the TARGET'S DECLARATION FORM (class, interface,
// enum, record) and the ANCHOR (class field_declaration vs record header
// component). It HOLDS CONSTANT the file path, the declaring type (`Order`
// wherever a class field is wanted) and the target name (`Customer` wherever a
// class target is wanted), so a wrapper regression shows up as a missing row
// rather than as a changed target.
//
// The axes that cannot vary inside that fixture without also varying something
// else get their own: cross-file targets need a second FILE; the type-parameter
// over-fire needs a file that DECLARES the parameter's name; the nosql `schema`
// collision needs an @Document annotation; the Lombok duplicate-Component case
// needs a @Builder beside a hand-declared `<Class>Builder`. Each is below with
// its own comment.
//
// RECALL CEILING — what 147 corpus edges is bounded BY, so the number is not
// read as completeness. This arm can only ever emit for a field that has a
// field ENTITY, and java's field-entity population has two pre-existing holes
// (neither introduced here, neither this arm's to fix):
//
//   - A MULTI-DECLARATOR field emits only its FIRST name. `class M { Customer
//     a, b; }` yields `SCOPE.Schema/field M.a` and nothing for `b`, so `b` can
//     never carry an edge.
//   - AN INTERFACE CONSTANT emits NO field entity at all. `interface Consts {
//     Customer DEFAULT = null; }` yields no field record and no edge.
//
// A third, smaller ceiling is on the TARGET side: an `@interface` annotation
// type gets no SCOPE.Component, so an in-file annotation type can never be a
// target. All three were observed by probe, not inferred.

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

const javaFTPath = "com/example/Order.java"

const javaFTSrc = `package com.example;

import java.util.List;
import java.util.Map;

class Customer { String name; }

interface Shipper { void ship(); }

enum Status { NEW, DONE }

record Money(String currency, long amount) {}

record Invoice(Customer buyer, List<Customer> cc, long total) {}

class Holder<T> { T item; }

class Outer {}

class Inner {}

class Order {
  int quantity;
  boolean flagged;
  double weight;
  String label;
  Integer boxed;
  Customer buyer;
  Customer[] history;
  List<Customer> watchers;
  java.util.List<Customer> qualifiedWatchers;
  Map<Status, Customer> byStatus;
  List<? extends Customer> wild;
  Holder<Customer> wrapped;
  Map<Customer, Customer> both;
  Shipper ship;
  Status state;
  Money price;
  Order next;
  com.other.Customer foreign;
  Outer.Inner nested;
  Warehouse depot;
}
`

// javaFTRivalPath declares a SECOND `Customer` in a different file so the bare
// name `Customer` is ambiguous across the graph. Without it the structural
// address would be ungraded by the resolver test: a bare-name ToID would bind
// too, and the test could not tell the two dialects apart. It also supplies the
// cross-file target `Warehouse`, which must produce NO edge.
const javaFTRivalPath = "com/other/Customer.java"

const javaFTRivalSrc = `package com.other;

class Customer { String note; }

class Warehouse { String code; }
`

func extractJavaFT(t *testing.T, files map[string]string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("java")
	if !ok {
		t.Fatal("java extractor not registered")
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var all []types.EntityRecord
	for _, p := range paths {
		src := files[p]
		got, err := ext.Extract(context.Background(), extractor.FileInput{
			Path: p, Content: []byte(src), Language: "java", TSTree: parseForTest(t, src),
		})
		if err != nil {
			t.Fatalf("extract %s: %v", p, err)
		}
		all = append(all, got...)
	}
	return all
}

// javaFTEdges returns "<file>:<fieldEntityName> => <target_type>" for every
// field→declared-type edge in recs, across ALL records — so an edge that
// escaped onto a non-field record shows up here rather than being filtered
// away by a field-shaped query.
func javaFTEdges(recs []types.EntityRecord) []string {
	var out []string
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "REFERENCES" || r.Properties.Get("ref_kind") != "field_target_type" {
				continue
			}
			out = append(out, recs[i].SourceFile+":"+recs[i].Name+" => "+r.Properties.Get("target_type"))
		}
	}
	sort.Strings(out)
	return out
}

func javaFTWantEqual(t *testing.T, got, want []string) {
	t.Helper()
	sort.Strings(want)
	if strings.Join(got, "\n") == strings.Join(want, "\n") {
		return
	}
	t.Fatalf("field→type edge set mismatch\n got (%d):\n  %s\nwant (%d):\n  %s",
		len(got), strings.Join(got, "\n  "), len(want), strings.Join(want, "\n  "))
}

// TestJavaFieldTypeRefs_EdgeSet is the whole-set assertion: it pins every edge
// the main fixture produces AND, by being an equality rather than a
// containment, every edge it must not. Each forbidden row is called out by name
// in the comments below so a future reader can see which decision each field
// grades rather than reading an unexplained absence.
//
//	quantity/flagged/weight  — PRIMITIVES. Not a type_identifier in the
//	                           grammar at all, so never even a candidate.
//	label / boxed            — java.lang and a boxed type: ordinary
//	                           type_identifiers, refused solely by the
//	                           in-file declaration check.
//	watchers / byStatus      — the generic WRAPPERS List and Map are refused
//	                           by that same check while their ARGUMENTS bind.
//	qualifiedWatchers        — the scoped `java.util.List` is skipped whole,
//	                           yet its sibling type argument still binds.
//	both                     — Map<Customer, Customer> is ONE edge, not two.
//	next                     — the self-reference is declined.
//	foreign                  — `com.other.Customer` is NOT reduced to
//	                           `Customer`, which IS declared in this file.
//	nested                   — `Outer.Inner` takes NEITHER segment, though
//	                           both are declared in this file.
//	depot                    — `Warehouse` is declared in the OTHER file.
//	Holder.item              — the type parameter `T` binds to nothing.
//	Money.currency/amount    — a record component with no in-file target.
func TestJavaFieldTypeRefs_EdgeSet(t *testing.T) {
	recs := extractJavaFT(t, map[string]string{
		javaFTPath:      javaFTSrc,
		javaFTRivalPath: javaFTRivalSrc,
	})
	const f = "com/example/Order.java:"
	javaFTWantEqual(t, javaFTEdges(recs), []string{
		f + "Order.buyer => Customer",
		f + "Order.history => Customer",
		f + "Order.watchers => Customer",
		f + "Order.qualifiedWatchers => Customer",
		f + "Order.byStatus => Status",
		f + "Order.byStatus => Customer",
		f + "Order.wild => Customer",
		f + "Order.wrapped => Holder",
		f + "Order.wrapped => Customer",
		f + "Order.both => Customer",
		f + "Order.ship => Shipper",
		f + "Order.state => Status",
		f + "Order.price => Money",
		f + "Invoice.buyer => Customer",
		f + "Invoice.cc => Customer",
	})
}

// TestJavaFieldTypeRefs_EdgeProperties pins the three properties on one edge
// against INDEPENDENT LITERALS rather than against the constants under test, so
// a near-miss in the ref_kind spelling (`field_target_types`) fails here.
// field_name is the field's LEAF name, not the `<Owner>.<field>` entity Name.
func TestJavaFieldTypeRefs_EdgeProperties(t *testing.T) {
	recs := extractJavaFT(t, map[string]string{javaFTPath: javaFTSrc})
	var found int
	for i := range recs {
		if recs[i].Name != "Order.buyer" {
			continue
		}
		for _, r := range recs[i].Relationships {
			if r.Kind != "REFERENCES" {
				continue
			}
			found++
			if got := r.Properties.Get("ref_kind"); got != "field_target_type" {
				t.Errorf("ref_kind = %q, want field_target_type", got)
			}
			if got := r.Properties.Get("field_name"); got != "buyer" {
				t.Errorf("field_name = %q, want buyer (leaf, not Order.buyer)", got)
			}
			if got := r.Properties.Get("target_type"); got != "Customer" {
				t.Errorf("target_type = %q, want Customer", got)
			}
			if got := r.ToID; got != "scope:component:class:java:com/example/Order.java:Customer" {
				t.Errorf("ToID = %q, want the file-scoped component structural ref", got)
			}
			if r.FromID != "" {
				t.Errorf("FromID = %q, want empty so assembly anchors on the field record", r.FromID)
			}
		}
	}
	if found != 1 {
		t.Fatalf("Order.buyer carries %d REFERENCES edges, want exactly 1", found)
	}
}

// TestJavaFieldTypeRefs_PrimitiveFieldsProduceNoEdge enumerates Java's eight
// primitives against an INDEPENDENT literal list rather than trusting the main
// fixture's three, and pairs each with a same-named DECLARED CLASS so the test
// cannot pass merely because the name is undeclared.
//
// This is the conjunct where Java differs from Go most sharply. Go's `int` is a
// shadowable identifier and arm D had to pin the shadowing case; Java's
// primitives are keywords with their own node types, so `class int {}` does not
// even parse and the primitive can never be a candidate. Declaring `class Int`
// (capitalised) alongside is the closest constructible neighbour and it MUST get
// its edge — the positive control that separates "primitives are refused" from
// "this fixture emits nothing".
func TestJavaFieldTypeRefs_PrimitiveFieldsProduceNoEdge(t *testing.T) {
	const path = "Prim.java"
	src := `class Int {}
class Prim {
  byte a; short b; int c; long d; float e; double f; boolean g; char h;
  Int ok;
}
`
	recs := extractJavaFT(t, map[string]string{path: src})
	javaFTWantEqual(t, javaFTEdges(recs), []string{path + ":Prim.ok => Int"})

	// Independent restatement: no edge anywhere carries a primitive target.
	for _, p := range []string{"byte", "short", "int", "long", "float", "double", "boolean", "char"} {
		for _, e := range javaFTEdges(recs) {
			if strings.HasSuffix(e, " => "+p) {
				t.Errorf("primitive %q became a target: %s", p, e)
			}
		}
	}
}

// TestJavaFieldTypeRefs_BoxedAndJavaLangTypesProduceNoEdge grades the OTHER
// half of the primitive story: the boxed types and java.lang names ARE
// candidates and are refused only by the in-file declaration check. The second
// file declares `class Integer` locally, which Java permits and which then
// genuinely means THAT type in field position — so the same source text yields
// no edge in one file and an edge in the other. That pair is what proves the
// refusal is the declaration check and not a name blocklist.
func TestJavaFieldTypeRefs_BoxedAndJavaLangTypesProduceNoEdge(t *testing.T) {
	recs := extractJavaFT(t, map[string]string{"A.java": `class A {
  Integer n; String s; Object o; Boolean b;
}
`})
	javaFTWantEqual(t, javaFTEdges(recs), nil)

	shadowed := extractJavaFT(t, map[string]string{"B.java": `class Integer { int v; }
class B {
  Integer n; String s;
}
`})
	javaFTWantEqual(t, javaFTEdges(shadowed), []string{"B.java:B.n => Integer"})
}

// TestJavaFieldTypeRefs_QualifiedTypeIsNotStrippedToASameFileName is the
// correctness fix arm D found in Go and this arm inherits deliberately. The
// field is declared `com.other.Customer` and the SAME FILE declares `Customer`;
// reducing the qualified name to its trailing segment binds the field to the
// WRONG entity, silently, because a bound edge never reaches bug-extractor.
//
// The same-file `Customer` is exercised by a POSITIVE CONTROL field in the same
// class, so the test distinguishes "the qualified form is skipped" from
// "`Customer` is not a target here at all".
func TestJavaFieldTypeRefs_QualifiedTypeIsNotStrippedToASameFileName(t *testing.T) {
	recs := extractJavaFT(t, map[string]string{"Q.java": `class Customer {}
class Q {
  com.other.Customer foreign;
  Customer local;
}
`})
	javaFTWantEqual(t, javaFTEdges(recs), []string{"Q.java:Q.local => Customer"})
}

// TestJavaFieldTypeRefs_NestedQualifiedTypeTakesNeitherSegment is the Java-only
// half of that ruling, and the reason a "take the last segment" heuristic is
// worse here than in Go. `Outer.Inner` is a nested-type reference whose BOTH
// segments name a type declared in this file, so a reduction is a guess between
// two wrong answers rather than one. Both are exercised as positive controls by
// bare-named fields, so the absence is specific to the qualified form.
func TestJavaFieldTypeRefs_NestedQualifiedTypeTakesNeitherSegment(t *testing.T) {
	recs := extractJavaFT(t, map[string]string{"N.java": `class Outer {}
class Inner {}
class N {
  Outer.Inner nested;
  Outer o;
  Inner i;
}
`})
	javaFTWantEqual(t, javaFTEdges(recs), []string{
		"N.java:N.o => Outer",
		"N.java:N.i => Inner",
	})
}

// TestJavaFieldTypeRefs_QualifiedGenericStillBindsItsArgument pins the
// interaction the two rules have, which is the one place skipping a node whole
// could have cost a real edge. In `java.util.List<Order>` the type_arguments
// node is a SIBLING of the scoped_type_identifier under generic_type, not a
// child of it, so skipping the qualified wrapper leaves the argument reachable.
// Asserted with the unqualified form beside it: both must yield exactly the
// argument, and the qualified form must NOT additionally yield `List`.
func TestJavaFieldTypeRefs_QualifiedGenericStillBindsItsArgument(t *testing.T) {
	recs := extractJavaFT(t, map[string]string{"G.java": `import java.util.List;
class Order {}
class List {}
class G {
  java.util.List<Order> qualified;
  List<Order> bare;
}
`})
	javaFTWantEqual(t, javaFTEdges(recs), []string{
		"G.java:G.qualified => Order",
		"G.java:G.bare => Order",
		"G.java:G.bare => List",
	})
}

// TestJavaFieldTypeRefs_SelfReferenceProducesNoEdge — a field whose declared
// type is its own declaring type. The CONTAINS edge already relates the two
// records; arms C and D decline it identically. The sibling field proves the
// target is otherwise reachable.
func TestJavaFieldTypeRefs_SelfReferenceProducesNoEdge(t *testing.T) {
	recs := extractJavaFT(t, map[string]string{"S.java": `class Node {
  Node next;
  Node[] children;
  Leaf leaf;
}
class Leaf {}
`})
	javaFTWantEqual(t, javaFTEdges(recs), []string{"S.java:Node.leaf => Leaf"})
}

// TestJavaFieldTypeRefs_ForwardDeclaredTargetBinds is why the candidates are
// stashed during the walk instead of becoming edges at the emit site: the target
// is declared BELOW the field that references it, so the in-file declaration set
// is complete only after walk returns. A mutant that emitted at the emit site
// would produce nothing here.
func TestJavaFieldTypeRefs_ForwardDeclaredTargetBinds(t *testing.T) {
	recs := extractJavaFT(t, map[string]string{"F.java": `class Order {
  Customer buyer;
}
class Customer {}
`})
	javaFTWantEqual(t, javaFTEdges(recs), []string{"F.java:Order.buyer => Customer"})
}

// TestJavaFieldTypeRefs_AllFourComponentSubtypesAreTargets grades the
// allow-list's ADMIT direction one subtype at a time. Java routes class,
// interface, enum and record through the SAME buildComponent call with only the
// Subtype varying, so a guard written on Kind alone would look correct and a
// guard that dropped one subtype would be invisible without this row-per-subtype
// assertion.
func TestJavaFieldTypeRefs_AllFourComponentSubtypesAreTargets(t *testing.T) {
	recs := extractJavaFT(t, map[string]string{"T.java": `class C {}
interface I { void x(); }
enum E { A, B }
record R(int v) {}
class Host {
  C c; I i; E e; R r;
}
`})
	javaFTWantEqual(t, javaFTEdges(recs), []string{
		"T.java:Host.c => C",
		"T.java:Host.i => I",
		"T.java:Host.e => E",
		"T.java:Host.r => R",
	})
}

// TestJavaFieldTypeRefs_RecordComponentIsAnAnchor grades the SECOND emit site.
// A record header component is emitted by a completely separate code path from
// buildField (walk's record_declaration arm), so an arm that wired only
// buildField would still pass every class-field test above.
func TestJavaFieldTypeRefs_RecordComponentIsAnAnchor(t *testing.T) {
	recs := extractJavaFT(t, map[string]string{"R.java": `class Customer {}
record Invoice(Customer buyer, String note, int total) {}
`})
	javaFTWantEqual(t, javaFTEdges(recs), []string{"R.java:Invoice.buyer => Customer"})
}

// TestJavaFieldTypeRefs_KnownOverFire_TypeParameterShadowedByASameFileType pins
// a case this pass gets WRONG, so the wrongness is a graded fact rather than a
// silent one. `class Holder<T>` uses `T` in field position; a file that also
// declares `class T {}` gets an edge that asserts something false. A real fix —
// threading the enclosing declaration's type_parameters list to the field — is
// EXPECTED to break this test, and breaking it is the signal that the fix
// landed.
//
// The control is the same source WITHOUT `class T {}`, which must produce
// nothing: that separates "the type parameter is mistaken for a type" from
// "type parameters produce edges unconditionally".
func TestJavaFieldTypeRefs_KnownOverFire_TypeParameterShadowedByASameFileType(t *testing.T) {
	overfire := extractJavaFT(t, map[string]string{"H.java": `class T {}
class Holder<T> { T item; }
`})
	javaFTWantEqual(t, javaFTEdges(overfire), []string{"H.java:Holder.item => T"})

	clean := extractJavaFT(t, map[string]string{"H2.java": `class Holder<T> { T item; }`})
	javaFTWantEqual(t, javaFTEdges(clean), nil)
}

// TestJavaFieldTypeRefs_NoSqlModelSchemaIsNeverATarget grades the allow-list
// against a REAL same-file competitor rather than a synthetic one.
// nosql_model.go emits a `SCOPE.Schema`/`schema` entity named after the ANNOTATED
// CLASS, in the same file, so `@Document class Customer` yields two records
// named `Customer`: the admitted SCOPE.Component and a SCOPE.Schema that is not
// a legal target.
//
// Two things must hold together and are asserted separately: the field still
// binds (the SCOPE.Schema must not make the name ambiguous — it is outside the
// component address family), and the edge's ToID addresses the component space.
func TestJavaFieldTypeRefs_NoSqlModelSchemaIsNeverATarget(t *testing.T) {
	const path = "Doc.java"
	src := `import org.springframework.data.mongodb.core.mapping.Document;

@Document
class Customer { String name; }

class Order { Customer buyer; }
`
	recs := extractJavaFT(t, map[string]string{path: src})
	// Premise: the competitor record actually exists in this fixture.
	var schemaNamed int
	for i := range recs {
		if recs[i].Kind == "SCOPE.Schema" && recs[i].Subtype == "schema" && recs[i].Name == "Customer" {
			schemaNamed++
		}
	}
	if schemaNamed == 0 {
		t.Skip("nosql model pass did not fire on this fixture; the collision it grades is absent")
	}
	javaFTWantEqual(t, javaFTEdges(recs), []string{path + ":Order.buyer => Customer"})
}

// TestJavaFieldTypeRefs_ResolvesToEntityIDs is the assertion that makes "a
// non-binding edge is worse than no edge" a graded property rather than a stated
// intention. It drives the REAL resolver and requires every emitted edge to bind
// to an entity ID; a dangling stub is kept verbatim and classified
// bug-extractor, so a mis-addressed edge would be a full hit.
//
// THE ENUM ROW IS THE POINT. `Order.state => Status` binds even though the file
// carries BOTH a SCOPE.Component/enum and a SCOPE.Enum value-set named `Status`.
// Arm D's rule — count kinds across EVERY same-file record — would have refused
// that edge and every other same-file enum target in Java, which is the
// over-strictness filed as #7038. This test is what says the narrower Java rule
// is correct on the resolver's own terms rather than merely cheaper: it does not
// assert the emit-site decision, it asserts the BINDING.
//
// The class rows beside it are the positive control: without them a rule that
// emitted nothing at all would also pass "no edge dangled".
func TestJavaFieldTypeRefs_ResolvesToEntityIDs(t *testing.T) {
	recs := extractJavaFT(t, map[string]string{
		javaFTPath:      javaFTSrc,
		javaFTRivalPath: javaFTRivalSrc,
	})
	for i := range recs {
		if recs[i].ID == "" {
			recs[i].ID = graph.EntityID("issue6912", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
		}
	}
	idx := resolve.BuildIndex(recs)

	before := len(javaFTEdges(recs))
	if before == 0 {
		t.Fatal("no field-type edges to drive through the resolver")
	}

	// Positive control on the address dialect: the BARE name must be ambiguous
	// here, otherwise this test does not distinguish the structural address from
	// a bare-name ToID, which would pass too.
	if _, st := idx.LookupStatusHint("Customer", "REFERENCES"); st == 1 {
		t.Fatalf("the bare name %q binds in this fixture, so the ambiguity this "+
			"test controls for is absent and the structural address is ungraded",
			"Customer")
	}

	idToName := map[string]string{}
	idToSubs := map[string]map[string]bool{}
	for i := range recs {
		id := recs[i].ID
		idToName[id] = recs[i].SourceFile + ":" + recs[i].Kind + ":" + recs[i].Name
		if idToSubs[id] == nil {
			idToSubs[id] = map[string]bool{}
		}
		idToSubs[id][recs[i].Subtype] = true
	}
	describe := func(id string) string {
		subs := make([]string, 0, len(idToSubs[id]))
		for s := range idToSubs[id] {
			subs = append(subs, s)
		}
		sort.Strings(subs)
		return idToName[id] + " [" + strings.Join(subs, "+") + "]"
	}

	resolve.ReferencesEmbedded(recs, idx)

	var bound []string
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "REFERENCES" || r.Properties.Get("ref_kind") != "field_target_type" {
				continue
			}
			if _, ok := idToName[r.ToID]; !ok {
				t.Errorf("%s:%s -[REFERENCES]-> %q did NOT bind to an entity id "+
					"(dangling stub → bug-extractor)",
					recs[i].SourceFile, recs[i].Name, r.ToID)
				continue
			}
			bound = append(bound, recs[i].SourceFile+":"+recs[i].Name+" => "+describe(r.ToID))
		}
	}
	sort.Strings(bound)
	const f = "com/example/Order.java:"
	javaFTWantEqual(t, bound, []string{
		f + "Order.buyer => " + f + "SCOPE.Component:Customer [class]",
		f + "Order.history => " + f + "SCOPE.Component:Customer [class]",
		f + "Order.watchers => " + f + "SCOPE.Component:Customer [class]",
		f + "Order.qualifiedWatchers => " + f + "SCOPE.Component:Customer [class]",
		f + "Order.byStatus => " + f + "SCOPE.Component:Status [enum]",
		f + "Order.byStatus => " + f + "SCOPE.Component:Customer [class]",
		f + "Order.wild => " + f + "SCOPE.Component:Customer [class]",
		f + "Order.wrapped => " + f + "SCOPE.Component:Holder [class]",
		f + "Order.wrapped => " + f + "SCOPE.Component:Customer [class]",
		f + "Order.both => " + f + "SCOPE.Component:Customer [class]",
		f + "Order.ship => " + f + "SCOPE.Component:Shipper [interface]",
		f + "Order.state => " + f + "SCOPE.Component:Status [enum]",
		f + "Order.price => " + f + "SCOPE.Component:Money [record]",
		f + "Invoice.buyer => " + f + "SCOPE.Component:Customer [class]",
		f + "Invoice.cc => " + f + "SCOPE.Component:Customer [class]",
	})
}

// TestJavaFieldTypeRefs_LombokBuilderDuplicateComponentIsOneNode is the
// PRODUCTION-REACHABLE grader for the count-kinds-not-records rule, and it
// exists because an earlier revision of this arm described the shape wrongly.
//
// synthesizeLombokEntities emits a synthesized SCOPE.Component/class for
// `@Builder` named `<Class>Builder` (lombok.go:438) — never the annotated
// class's own name, which is what the earlier description claimed. So the
// collision is constructible only like this: `@Builder class Order` synthesizes
// `OrderBuilder` into this file, and the file ALSO declares `class OrderBuilder`
// by hand. Two SCOPE.Component/class records, one Name, one file — and
// therefore ONE graph node, since graph.EntityID excludes Subtype and both
// records carry the same Kind.
//
// A field typed `OrderBuilder` must still get its edge. A rule counting RECORDS
// deletes it, which is #7038's defect reached from java source rather than from
// a hand-built record set. The fixture asserts the premise (two records, one
// name) before asserting the edge, so it cannot pass vacuously if Lombok stops
// synthesizing.
func TestJavaFieldTypeRefs_LombokBuilderDuplicateComponentIsOneNode(t *testing.T) {
	const path = "Order.java"
	recs := extractJavaFT(t, map[string]string{path: `import lombok.Builder;

@Builder
class Order {
  String id;
}

class OrderBuilder {
  String stage;
}

class Host {
  OrderBuilder ob;
  Order order;
}
`})
	// Premise: TWO SCOPE.Component records named OrderBuilder in this file,
	// one synthesized and one declared. Without both, the rule this test
	// grades is not exercised and the assertion below would be vacuous.
	var synthesized, declared int
	for i := range recs {
		if recs[i].Kind != "SCOPE.Component" || recs[i].Name != "OrderBuilder" ||
			recs[i].SourceFile != path {
			continue
		}
		if recs[i].Properties["synthesized_from"] != "" {
			synthesized++
		} else {
			declared++
		}
	}
	if synthesized != 1 || declared != 1 {
		t.Fatalf("premise absent: %d synthesized + %d declared OrderBuilder "+
			"components, want 1 + 1 — the duplicate-record rule is not exercised",
			synthesized, declared)
	}
	javaFTWantEqual(t, javaFTEdges(recs), []string{
		path + ":Host.ob => OrderBuilder",
		path + ":Host.order => Order",
	})
}
