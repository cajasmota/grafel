package rust_test

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/rust"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// #6912 arm C — the field→declared-type edge for Rust. See field_type_refs.go
// for the kind / address / same-file decisions; this file grades them.
//
// Every assertion below names FIELDS AND TARGETS. A count table cannot see a
// substitution (#6973), and recall cannot see over-firing at all — so the
// forbidden set (a named primitive field, a named `crate::`-path field, a named
// wrapper-only field, the wrapper constructors themselves) is asserted BY NAME
// beside the expected set.

const rustFTOwnerPath = "src/models.rs"

const rustFTSrc = `use std::collections::HashMap;
use std::sync::{Arc, Mutex};

pub enum OrderStatus { New, Shipped }

pub trait Shipper { fn ship(&self); }

pub type Meters = f64;

pub struct Customer { pub name: String }

pub struct Holder<T> { pub inner: T }

pub enum Event { Placed { by: Customer }, Cancelled }

pub struct Loan<'a> { pub cust: &'a Customer, pub order: &'a mut Order }

pub struct Order {
    pub buyer: Customer,
    pub status: OrderStatus,
    pub shipper: Box<dyn Shipper>,
    pub distance: Meters,
    pub watchers: Vec<Customer>,
    pub by_name: HashMap<String, Customer>,
    pub history: [Customer; 3],
    pub parent: Option<Box<Order>>,
    pub guarded: Arc<Mutex<Customer>>,
    pub pair: (Customer, Customer),
    pub nested: Holder<Customer>,
    pub callback: fn(Customer) -> i32,
    pub raw: *const Customer,
    pub quantity: i32,
    pub label: String,
    pub tags: Vec<String>,
    pub foreign: crate::other::Order,
    pub sup: super::Customer,
    pub me: Option<Self>,
}
`

// A SECOND file declaring types of the SAME bare names. Its only job is to make
// the bare-name resolver tier ambiguous, which is why the address is structural;
// the edges from src/models.rs must be identical with and without it.
const rustFTRivalPath = "src/other.rs"

const rustFTRivalSrc = `pub struct Customer { pub other: String }
pub struct Order { pub buyer: Customer }
pub enum OrderStatus { A, B }
`

func extractRustFiles(t *testing.T, files map[string]string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("rust")
	if !ok {
		t.Fatal("rust extractor not registered")
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var recs []types.EntityRecord
	for _, p := range paths {
		got, err := ext.Extract(context.Background(), extractor.FileInput{
			Path:     p,
			Content:  []byte(files[p]),
			Language: "rust",
			TSTree:   parseForTest(t, files[p]),
		})
		if err != nil {
			t.Fatalf("Extract %s: %v", p, err)
		}
		recs = append(recs, got...)
	}
	return recs
}

// rustFieldTypeRefEdges returns "<field name> -> <ToID>" for every REFERENCES
// edge carrying ref_kind=field_target_type, across ALL records, so an edge that
// escaped onto some other record is still seen. It fails on a non-empty FromID:
// the Django precedent leaves it empty so assembly anchors the edge on the field
// that carries it.
func rustFieldTypeRefEdges(t *testing.T, recs []types.EntityRecord) []string {
	t.Helper()
	var out []string
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "REFERENCES" || rustPropOf(r.Properties, "ref_kind") != "field_target_type" {
				continue
			}
			if r.FromID != "" {
				t.Errorf("%s -[REFERENCES]-> %s has FromID=%q, want empty",
					recs[i].Name, r.ToID, r.FromID)
			}
			out = append(out, recs[i].Name+" -> "+r.ToID)
		}
	}
	sort.Strings(out)
	return out
}

func rustPropOf(p types.Props, key string) string {
	for _, kv := range p {
		if kv.K == key {
			return kv.V
		}
	}
	return ""
}

// TestRustFieldTypeRefs_EmittedEdges is the positive direction, written as an
// INDEPENDENT LITERAL set (#6975) rather than derived from the production target
// index. Each ToID is spelled out so a change of address dialect, of target
// subtype, or of owning field fails here instead of passing silently.
//
// The four target SUBTYPES are each represented and each named: struct
// (Customer / Order / Holder), enum (OrderStatus), trait (Shipper) and
// type_alias (Meters).
func TestRustFieldTypeRefs_EmittedEdges(t *testing.T) {
	recs := extractRustFiles(t, map[string]string{rustFTOwnerPath: rustFTSrc})
	const p = "scope:component:class:rust:src/models.rs:"
	want := []string{
		// enum-variant struct fields are field entities too (#4854).
		"Event.Placed.by -> " + p + "Customer",
		// &'a Customer / &'a mut Order — the target is the type, not the borrow,
		// and the lifetime `'a` is not a candidate.
		"Loan.cust -> " + p + "Customer",
		"Loan.order -> " + p + "Order",
		"Order.buyer -> " + p + "Customer",
		"Order.by_name -> " + p + "Customer",
		"Order.callback -> " + p + "Customer",
		"Order.distance -> " + p + "Meters",
		"Order.guarded -> " + p + "Customer",
		"Order.history -> " + p + "Customer",
		// A USER-DEFINED generic constructor declared in this file is itself a
		// legitimate target, which is why the constructor is not blanket-dropped
		// — two rows, in source order of the type expression.
		"Order.nested -> " + p + "Customer",
		"Order.nested -> " + p + "Holder",
		"Order.parent -> " + p + "Order",
		// ONE row, not two: `(Customer, Customer)` names the same target twice
		// and the per-field dedup collapses it. A second identical row here is
		// what a dropped dedup looks like.
		"Order.pair -> " + p + "Customer",
		"Order.raw -> " + p + "Customer",
		"Order.shipper -> " + p + "Shipper",
		"Order.status -> " + p + "OrderStatus",
		"Order.watchers -> " + p + "Customer",
	}
	sort.Strings(want)
	got := rustFieldTypeRefEdges(t, recs)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("field-type edges mismatch\n got:\n%s\nwant:\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// The candidate stash is CONSUMED, not left behind. graph.go:176 copies
	// EntityRecord.Metadata onto the JSON-serialised Entity.Metadata
	// (`json:"metadata,omitempty"`), so without attachRustFieldTypeRefs's
	// delete() every Rust field entity in every indexed repo would persist a
	// `field_type_refs` array into the graph. The doc comment claims the clear;
	// this is what observes it — deleting the line is otherwise ALIVE through
	// all three suites including ./cmd/grafel/ (review of #7000).
	fieldsSeen := 0
	for i := range recs {
		if recs[i].Kind != "SCOPE.Schema" || recs[i].Subtype != "field" {
			continue
		}
		fieldsSeen++
		if v, ok := recs[i].Metadata["field_type_refs"]; ok {
			t.Errorf("%s leaked the candidate stash into Metadata: %v", recs[i].Name, v)
		}
	}
	// Positive control: the fixture's field records must actually be here, or
	// "no stash" is true because nothing was inspected. 19 Order fields + 1
	// Customer + 1 Holder + 2 Loan + 1 Event.Placed.
	if fieldsSeen != 24 {
		t.Fatalf("expected 24 field records to inspect for a leaked stash, got %d", fieldsSeen)
	}
}

// TestRustFieldTypeRefs_EdgeProperties cross-checks the two properties that are
// otherwise only claimed in prose — `field_name` and `target_type` — against an
// independent literal. `Order.by_name` is the interesting row: its declared type
// is `HashMap<String, Customer>`, so target_type is `Customer` and NOT the
// declared type string.
func TestRustFieldTypeRefs_EdgeProperties(t *testing.T) {
	recs := extractRustFiles(t, map[string]string{rustFTOwnerPath: rustFTSrc})
	var got []string
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "REFERENCES" || rustPropOf(r.Properties, "ref_kind") != "field_target_type" {
				continue
			}
			got = append(got, recs[i].Name+" field_name="+rustPropOf(r.Properties, "field_name")+
				" target_type="+rustPropOf(r.Properties, "target_type"))
		}
	}
	sort.Strings(got)
	want := []string{
		"Event.Placed.by field_name=by target_type=Customer",
		"Loan.cust field_name=cust target_type=Customer",
		"Loan.order field_name=order target_type=Order",
		"Order.buyer field_name=buyer target_type=Customer",
		"Order.by_name field_name=by_name target_type=Customer",
		"Order.callback field_name=callback target_type=Customer",
		"Order.distance field_name=distance target_type=Meters",
		"Order.guarded field_name=guarded target_type=Customer",
		"Order.history field_name=history target_type=Customer",
		"Order.nested field_name=nested target_type=Customer",
		"Order.nested field_name=nested target_type=Holder",
		"Order.pair field_name=pair target_type=Customer",
		"Order.parent field_name=parent target_type=Order",
		"Order.raw field_name=raw target_type=Customer",
		"Order.shipper field_name=shipper target_type=Shipper",
		"Order.status field_name=status target_type=OrderStatus",
		"Order.watchers field_name=watchers target_type=Customer",
	}
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("edge properties mismatch\n got:\n%s\nwant:\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestRustFieldTypeRefs_ForbiddenTargets is the negative direction, BY NAME.
// Recall is structurally blind to over-firing, and over-firing is the failure
// mode that hurts: every non-binding edge is one more `bug-extractor` endpoint
// (#6906), so a too-broad predicate makes the graph worse, not better.
func TestRustFieldTypeRefs_ForbiddenTargets(t *testing.T) {
	recs := extractRustFiles(t, map[string]string{rustFTOwnerPath: rustFTSrc})
	edges := rustFieldTypeRefEdges(t, recs)

	for _, field := range []string{
		"Order.quantity", // i32 — a NAMED primitive field
		"Order.label",    // String — declared nowhere in this file
		"Order.tags",     // Vec<String> — a NAMED wrapper-only field
		"Order.foreign",  // crate::other::Order — a NAMED crate:: path field
		"Order.sup",      // super::Customer — qualified, not descended into
		"Order.me",       // Option<Self> — Self is not a declaration
		"Customer.name",  // String
		"Holder.inner",   // T — an open type parameter
	} {
		for _, e := range edges {
			if strings.HasPrefix(e, field+" -> ") {
				t.Errorf("%s must have no field-type edge, got %q", field, e)
			}
		}
	}

	// The wrapper CONSTRUCTORS are targets in their own right if anything emits
	// to them. `Vec<Customer>` must reach Customer and never Vec.
	for _, banned := range []string{
		"Vec", "Box", "Option", "HashMap", "Arc", "Mutex", "String", "Self", "T", "i32", "f64",
	} {
		for _, e := range edges {
			if strings.HasSuffix(e, ":"+banned) {
				t.Errorf("no field-type edge may target %q, got %q", banned, e)
			}
		}
	}
}

// rustPrimitiveNames is the tree-sitter-rust `primitive_type` set, written here
// as an INDEPENDENT LITERAL (#6975): production has no such list, and that is
// the claim under test — the grammar classifies these names, so no blocklist
// exists to be compared against.
var rustPrimitiveNames = []string{
	"i8", "i16", "i32", "i64", "i128", "isize",
	"u8", "u16", "u32", "u64", "u128", "usize",
	"f32", "f64", "bool", "str", "char",
}

// TestRustFieldTypeRefs_EveryPrimitiveShadowedByASameFileStructProducesNoEdge
// is arm B's counter-example, CONSTRUCTED for Rust rather than argued away.
//
// Arm B (#6991) deleted its protobuf scalar blocklist on the argument that the
// in-file gate already dropped every scalar, and review broke it by writing
// `message string { … }`: the shadowing message IS in the gate's local set, so
// the edge was emitted and BOUND while protoc denied it. The lesson recorded
// there was that a mutant killed only by its own unit test is equally consistent
// with the fixture space lacking the case.
//
// So the case is built here. For EVERY primitive name, this file declares
// `struct <name>;` — which puts that name in the in-file target set, proven by
// the positive control below — and a field of that type. No edge may appear,
// because tree-sitter tokenises the type position as `primitive_type` and
// rustTypeRefCandidates never yields a candidate. That makes the guard the
// GRAMMAR, not a list, and it fails in the SAFE direction: if rustc resolves
// such a shadow to the struct, this pass misses an edge; it never invents one.
func TestRustFieldTypeRefs_EveryPrimitiveShadowedByASameFileStructProducesNoEdge(t *testing.T) {
	var b strings.Builder
	b.WriteString("pub struct Order { pub inner: i8 }\n")
	for _, p := range rustPrimitiveNames {
		b.WriteString("pub struct " + p + ";\n")
	}
	b.WriteString("pub struct Shadow {\n")
	for i, p := range rustPrimitiveNames {
		b.WriteString("    pub f" + string(rune('a'+i)) + ": " + p + ",\n")
	}
	b.WriteString("    pub ctrl: Order,\n}\n")
	src := b.String()

	recs := extractRustFiles(t, map[string]string{"src/prim.rs": src})

	// Positive control 1: every shadowing struct is really in the file's target
	// population, so "no edge" is not true for the trivial reason that the name
	// is undeclared.
	declared := map[string]bool{}
	for i := range recs {
		if recs[i].Kind == "SCOPE.Component" && recs[i].Subtype == "struct" {
			declared[recs[i].Name] = true
		}
	}
	for _, p := range rustPrimitiveNames {
		if !declared[p] {
			t.Fatalf("fixture did not declare `struct %s`; the shadow case is not exercised", p)
		}
	}

	// Positive control 2: a non-primitive field in the SAME struct does get its
	// edge, so the pipeline is live.
	want := "Shadow.ctrl -> scope:component:class:rust:src/prim.rs:Order"
	got := rustFieldTypeRefEdges(t, recs)
	if strings.Join(got, "\n") != want {
		t.Fatalf("a primitive field gained an edge, or the control was lost\n got:\n%s\nwant:\n%s",
			strings.Join(got, "\n"), want)
	}

	// And per member, so a single surviving primitive is named rather than
	// buried in a set diff.
	for i, p := range rustPrimitiveNames {
		field := "Shadow.f" + string(rune('a'+i))
		for _, e := range got {
			if strings.HasPrefix(e, field+" -> ") {
				t.Errorf("primitive %q gained a field-type edge: %q", p, e)
			}
		}
	}
}

// TestRustFieldTypeRefs_NonTypeIdentifiersAreNotCandidates grades the
// collector's ONE rule — it takes `type_identifier` and nothing else — at the
// two places where a type expression carries a NAME that is not a type: a
// lifetime (`&'a Customer` holds an `identifier` "a") and an array length
// (`[Order; SIZE]` holds an `identifier` "SIZE").
//
// The fixture DECLARES `struct a;` and `struct SIZE;` so both names are in the
// in-file target set. Without them the assertion would hold for the trivial
// reason that nothing by those names exists, and widening the collector to
// `identifier` — a live, production-reachable over-fire — would survive
// unnoticed. That mutant is what this test exists for.
func TestRustFieldTypeRefs_NonTypeIdentifiersAreNotCandidates(t *testing.T) {
	const src = `pub struct a;
pub struct SIZE;
pub struct Customer { pub n: u32 }
pub struct Order { pub n: u32 }

pub struct Holder<'a> {
    pub cust: &'a Customer,
    pub many: [Order; SIZE],
    pub ctrl: Customer,
}
`
	recs := extractRustFiles(t, map[string]string{"src/l.rs": src})
	// Positive control: both non-type names really are declared here.
	declared := map[string]bool{}
	for i := range recs {
		if recs[i].Kind == "SCOPE.Component" && recs[i].Subtype == "struct" {
			declared[recs[i].Name] = true
		}
	}
	if !declared["a"] || !declared["SIZE"] {
		t.Fatalf("fixture did not declare `struct a` and `struct SIZE` (%v); "+
			"the shadow case is not exercised", declared)
	}
	const p = "scope:component:class:rust:src/l.rs:"
	want := []string{
		"Holder.cust -> " + p + "Customer",
		"Holder.ctrl -> " + p + "Customer",
		"Holder.many -> " + p + "Order",
	}
	sort.Strings(want)
	if got := rustFieldTypeRefEdges(t, recs); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("a non-type identifier became a target\n got:\n%s\nwant:\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestRustFieldTypeRefs_QualifiedPathIsNotStrippedToASameFileName pins the one
// place the rule is stricter than "is it declared in this file": a qualified
// path is skipped WHOLE, so `crate::other::Order` does NOT bind to this file's
// own `Order` even though the trailing segment matches it. Descending the path
// is the permissive mutation arm B carries a forbidden row for; this is that
// row for Rust.
func TestRustFieldTypeRefs_QualifiedPathIsNotStrippedToASameFileName(t *testing.T) {
	const src = `pub trait Trait { type Assoc; }

pub struct Order { pub id: u32 }

pub struct Holder {
    pub a: crate::other::Order,
    pub b: super::Order,
    pub c: self::Order,
    pub d: <Order as Trait>::Assoc,
    pub e: Order,
}
`
	recs := extractRustFiles(t, map[string]string{"src/h.rs": src})
	want := []string{"Holder.e -> scope:component:class:rust:src/h.rs:Order"}
	if got := rustFieldTypeRefEdges(t, recs); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("qualified-path edges\n got: %v\nwant: %v", got, want)
	}
}

// TestRustFieldTypeRefs_CrossFileTypeGetsNoEdge states the deliberate cost of
// the same-file rule as an assertion rather than as prose: a field whose type is
// declared in ANOTHER file gets nothing, because pass 1 has no cross-file view
// and an edge it cannot verify would dangle.
func TestRustFieldTypeRefs_CrossFileTypeGetsNoEdge(t *testing.T) {
	recs := extractRustFiles(t, map[string]string{
		"src/order.rs":   "use crate::shipper::Shipper;\n\npub struct Order { pub via: Shipper }\n",
		"src/shipper.rs": "pub struct Shipper { pub name: String }\n",
	})
	// Positive control: the field whose type lives in the other file must exist,
	// otherwise "no edges" is trivially true for the wrong reason.
	found := false
	for i := range recs {
		if recs[i].Kind == "SCOPE.Schema" && recs[i].Subtype == "field" && recs[i].Name == "Order.via" {
			found = true
		}
	}
	if !found {
		t.Fatal("fixture produced no Order.via field; the cross-file case is not exercised")
	}
	if got := rustFieldTypeRefEdges(t, recs); len(got) != 0 {
		t.Fatalf("cross-file declared type must produce no edge, got %v", got)
	}
}

// TestRustFieldTypeRefs_SameNameInTwoFiles is the reason the address is
// structural. The rival file declares Customer / Order / OrderStatus under the
// same bare names; the edges emitted for src/models.rs must be byte-identical to
// the single-file run. #6986 measured the bare-name `Class:<Name>` form at 91.6%
// dangling for exactly this reason.
func TestRustFieldTypeRefs_SameNameInTwoFiles(t *testing.T) {
	alone := rustFieldTypeRefEdges(t, extractRustFiles(t, map[string]string{
		rustFTOwnerPath: rustFTSrc,
	}))
	withRival := rustFieldTypeRefEdges(t, extractRustFiles(t, map[string]string{
		rustFTOwnerPath: rustFTSrc,
		rustFTRivalPath: rustFTRivalSrc,
	}))
	wantExtra := "Order.buyer -> scope:component:class:rust:src/other.rs:Customer"
	var got []string
	for _, e := range withRival {
		if e != wantExtra {
			got = append(got, e)
		}
	}
	if strings.Join(got, "\n") != strings.Join(alone, "\n") {
		t.Fatalf("a same-named type in another file changed this file's edges\n got:\n%s\nwant:\n%s",
			strings.Join(got, "\n"), strings.Join(alone, "\n"))
	}
	if len(withRival) != len(alone)+1 {
		t.Fatalf("rival file's own edge missing: %v", withRival)
	}
}

// TestRustFieldTypeRefs_SameNameDeclaredTwiceInOneFile — a name declared twice
// in the SAME file is removed from the target index rather than resolved to
// either declaration, so the pass never guesses. Two inline `mod`s each
// declaring `Config` is the idiomatic Rust way to get there.
func TestRustFieldTypeRefs_SameNameDeclaredTwiceInOneFile(t *testing.T) {
	const src = `pub mod a { pub struct Config { pub x: u32 } }
pub mod b { pub struct Config { pub y: u32 } }

pub struct Holder { pub cfg: Config }
`
	recs := extractRustFiles(t, map[string]string{"src/dup.rs": src})
	configs := 0
	holderField := false
	for i := range recs {
		if recs[i].Kind == "SCOPE.Component" && recs[i].Name == "Config" {
			configs++
		}
		if recs[i].Kind == "SCOPE.Schema" && recs[i].Subtype == "field" && recs[i].Name == "Holder.cfg" {
			holderField = true
		}
	}
	if configs != 2 || !holderField {
		t.Fatalf("fixture did not produce two Config declarations and a Holder.cfg field "+
			"(configs=%d holderField=%v); the duplicate-name case is not exercised",
			configs, holderField)
	}
	for _, e := range rustFieldTypeRefEdges(t, recs) {
		if strings.HasPrefix(e, "Holder.cfg -> ") {
			t.Fatalf("Config is declared twice in this file; Holder.cfg must get no edge, got %q", e)
		}
	}
}

// TestRustFieldTypeRefs_ImplBlockIsNotATypeDeclaration is Rust-specific and it
// is load-bearing, not decorative. buildImpl emits a SCOPE.Component NAMED AFTER
// the implementing type, so `struct Order` + `impl Order` — the commonest shape
// in the language — puts two components called `Order` in one file. If the
// target index counted the impl as a declaration, the duplicate-name rule would
// delete the target and every such struct would silently lose its edges.
func TestRustFieldTypeRefs_ImplBlockIsNotATypeDeclaration(t *testing.T) {
	const src = `pub struct Customer { pub name: String }

pub struct Order { pub buyer: Customer }

impl Order {
    pub fn new(buyer: Customer) -> Self { Order { buyer } }
}

impl Customer {
    pub fn name(&self) -> &str { &self.name }
}
`
	recs := extractRustFiles(t, map[string]string{"src/i.rs": src})
	// Positive control: the file really does hold an impl component named after
	// each struct, so the collision this test guards against is present.
	impls := map[string]bool{}
	for i := range recs {
		if recs[i].Kind == "SCOPE.Component" && recs[i].Subtype == "impl" {
			impls[recs[i].Name] = true
		}
	}
	if !impls["Order"] || !impls["Customer"] {
		t.Fatalf("fixture produced no impl components named Order and Customer (%v); "+
			"the name-collision case is not exercised", impls)
	}
	want := []string{"Order.buyer -> scope:component:class:rust:src/i.rs:Customer"}
	if got := rustFieldTypeRefEdges(t, recs); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("impl blocks must not count as type declarations\n got: %v\nwant: %v", got, want)
	}
}

// TestRustFieldTypeRefs_KnownOverFire_ModuleScopeIsNotConsulted and
// TestRustFieldTypeRefs_KnownOverFire_TypeParameterShadowsSameFileType PIN A
// KNOWN DEFECT, and they are the only assertions in this file that describe
// behaviour that is WRONG.
//
// "Declared in this same file" is a FILE-scoped check with no module scope and
// no type-parameter scope behind it. Both consequences are reproduced here, and
// both are worse than a dangling edge by the same logic that chose this pass's
// address: the edge BINDS, so it never reaches `bug-extractor` and no
// disposition figure will surface it.
//
// These two tests are expected to FAIL when a follow-up fixes them. That is the
// point: they make the limitation observable at the code, so a fix has to come
// here and say so rather than changing behaviour silently.
func TestRustFieldTypeRefs_KnownOverFire_ModuleScopeIsNotConsulted(t *testing.T) {
	const src = `pub mod a { pub struct Customer { pub n: String } }
pub mod b { pub struct Order { pub buyer: Customer } }
`
	recs := extractRustFiles(t, map[string]string{"src/f.rs": src})
	want := []string{"Order.buyer -> scope:component:class:rust:src/f.rs:Customer"}
	if got := rustFieldTypeRefEdges(t, recs); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("KNOWN over-fire changed shape — if module scope is now consulted, "+
			"delete this test and say so\n got: %v\nwant: %v", got, want)
	}
}

func TestRustFieldTypeRefs_KnownOverFire_TypeParameterShadowsSameFileType(t *testing.T) {
	const src = `pub struct Customer { pub n: String }

pub struct Box2<Customer> { pub item: Customer }
`
	recs := extractRustFiles(t, map[string]string{"src/f.rs": src})
	want := []string{"Box2.item -> scope:component:class:rust:src/f.rs:Customer"}
	if got := rustFieldTypeRefEdges(t, recs); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("KNOWN over-fire changed shape — if type-parameter scope is now "+
			"consulted, delete this test and say so\n got: %v\nwant: %v", got, want)
	}
}

// TestRustFieldTypeRefs_ResolverBindsEveryEdge is the gate this whole issue
// class turns on: #6906 exists because a comment described a binding nobody
// built. Every emitted edge is driven through the PRODUCTION resolver
// (BuildIndex → ReferencesEmbedded, exactly as graph assembly does) and must
// come back REWRITTEN to a real entity ID. A dangling edge is worse than no edge
// — it is one more `bug-extractor` endpoint — so zero non-resolved dispositions
// is the assertion, not a ratio.
//
// The fixture carries the rival file (bare names ambiguous) AND an `impl Order`
// in the owner file, because the impl shares its name with the struct in the
// same file and is the Rust-specific way this address could have gone wrong.
func TestRustFieldTypeRefs_ResolverBindsEveryEdge(t *testing.T) {
	recs := extractRustFiles(t, map[string]string{
		rustFTOwnerPath: rustFTSrc + "\nimpl Order { pub fn total(&self) -> i32 { 0 } }\n",
		rustFTRivalPath: rustFTRivalSrc,
	})
	for i := range recs {
		if recs[i].Name == "" {
			continue
		}
		recs[i].ID = graph.EntityID("issue6912", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
	}
	idx := resolve.BuildIndex(recs)

	// Positive control on the address dialect itself: the BARE-name form of the
	// same target is AMBIGUOUS here, which is why it is not what we emit.
	if _, st := idx.LookupStatusHint("Customer", "REFERENCES"); st == 1 {
		t.Fatalf("the bare name %q binds in this fixture, so the ambiguity this "+
			"test controls for is not present and the structural address is ungraded", "Customer")
	}

	// A struct and its `impl` block are ONE graph node, not two: graph.EntityID
	// hashes (repo, Kind, Name, SourceFile) and both are SCOPE.Component/Order
	// in src/models.rs, so they share an ID by construction. The description
	// below therefore carries the SORTED SET of subtypes behind each ID —
	// `Order` must come back as "impl+struct" — because keying a map by ID and
	// writing the subtype into it would silently report whichever record came
	// last and read as a mis-binding.
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

	before := 0
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind == "REFERENCES" && rustPropOf(r.Properties, "ref_kind") == "field_target_type" {
				before++
			}
		}
	}
	if before == 0 {
		t.Fatal("no field-type edges to drive through the resolver")
	}

	resolve.ReferencesEmbedded(recs, idx)

	var bound []string
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "REFERENCES" || rustPropOf(r.Properties, "ref_kind") != "field_target_type" {
				continue
			}
			_, ok := idToName[r.ToID]
			if !ok {
				t.Errorf("%s -[REFERENCES]-> %q did NOT bind to an entity ID (dangling stub)",
					recs[i].Name, r.ToID)
				continue
			}
			bound = append(bound, recs[i].SourceFile+":"+recs[i].Name+" => "+describe(r.ToID))
		}
	}
	sort.Strings(bound)
	const m = "src/models.rs:"
	want := []string{
		m + "Event.Placed.by => " + m + "SCOPE.Component:Customer [struct]",
		m + "Loan.cust => " + m + "SCOPE.Component:Customer [struct]",
		m + "Loan.order => " + m + "SCOPE.Component:Order [impl+struct]",
		m + "Order.buyer => " + m + "SCOPE.Component:Customer [struct]",
		m + "Order.by_name => " + m + "SCOPE.Component:Customer [struct]",
		m + "Order.callback => " + m + "SCOPE.Component:Customer [struct]",
		m + "Order.distance => " + m + "SCOPE.Component:Meters [type_alias]",
		m + "Order.guarded => " + m + "SCOPE.Component:Customer [struct]",
		m + "Order.history => " + m + "SCOPE.Component:Customer [struct]",
		m + "Order.nested => " + m + "SCOPE.Component:Customer [struct]",
		m + "Order.nested => " + m + "SCOPE.Component:Holder [struct]",
		m + "Order.pair => " + m + "SCOPE.Component:Customer [struct]",
		m + "Order.parent => " + m + "SCOPE.Component:Order [impl+struct]",
		m + "Order.raw => " + m + "SCOPE.Component:Customer [struct]",
		m + "Order.shipper => " + m + "SCOPE.Component:Shipper [trait]",
		m + "Order.status => " + m + "SCOPE.Component:OrderStatus [enum]",
		m + "Order.watchers => " + m + "SCOPE.Component:Customer [struct]",
		"src/other.rs:Order.buyer => src/other.rs:SCOPE.Component:Customer [struct]",
	}
	sort.Strings(want)
	if strings.Join(bound, "\n") != strings.Join(want, "\n") {
		t.Fatalf("resolved endpoints mismatch\n got:\n%s\nwant:\n%s",
			strings.Join(bound, "\n"), strings.Join(want, "\n"))
	}
}
