package rust_test

import (
	"strings"
	"testing"
)

// #7041 arm C (rust) — a TYPE PARAMETER is not a same-file type declaration.
//
// Before this arm, `pub struct Box2<Customer> { pub item: Customer }` beside a
// real `pub struct Customer` emitted `Box2.item -> Customer`. That edge BINDS,
// so nothing downstream can see it is wrong (#7056): right name, right file,
// admissible kind and subtype, and the orphan metric of #6726 actively improves
// when it fires. It was pinned as known-wrong by
// TestRustFieldTypeRefs_KnownOverFire_TypeParameterShadowsSameFileType, a hard
// t.Fatalf, which this arm DELETES — its own message said to.
//
// WHY THE RULE IS "THE DECLARATION'S OWN <…> AND NOTHING ELSE", derived from
// rustc 1.98.1 rather than ported from a sibling arm (kotlin gates on `inner`,
// java ascends unconditionally, scala always accumulates — all three differ,
// and two corrections on #7041 came from asserting a language rule from
// memory). The programs and the compiler's answers:
//
//	pub struct Customer { pub n: String }
//	pub struct Box2<Customer> { pub item: Customer }
//	pub fn mk() -> Box2<i32> { Box2 { item: 5 } }        // COMPILES
//
// so the parameter shadows the struct inside its own declaration: `item` is the
// parameter, and `Box2<i32>.item` is an `i32`. And the nesting direction:
//
//	pub fn f<T>() { struct S { x: T } … }
//	  error[E0401]: can't use generic parameters from outer item
//	  note: nested items are independent from their parent item for everything
//	        except for privacy and name resolution
//
//	pub fn f<T>() { mod m { pub struct S { pub x: T } } }
//	  error[E0425]: cannot find type `T` in this scope
//
//	pub struct T { pub n: u8 }
//	pub fn f<T>() { struct S { x: T } … }
//	  error[E0401] AGAIN — rustc does NOT fall back to the top-level `struct T`
//
//	impl<T> Box3<T> { pub fn g() { struct S { x: T } … } }   // error[E0401]
//	impl<T> Box3<T> { pub fn get(self) -> T { self.item } }  // COMPILES
//	impl<T> Tr for Box4<T> { type A = T; }                   // COMPILES
//
// A nested ITEM therefore never sees an enclosing item's type parameters, so
// this pass reads the field's OWN owning declaration and does not ascend. The
// third program is the one that settles it: there is NO legal Rust in which a
// nested item's bare name means the top-level type while an enclosing generic
// binds that name — exactly java's situation, and the reason "in scope" and
// "bindable here" must be asked separately.
//
// THE GRAMMAR SPELLINGS ARE FROM A CST DUMP, not from memory. The scala arm
// shipped a guard switching on two node types that do not exist in its grammar
// (#7064), so every string the collector matches is named here with the source
// that produces it, and every one of them is graded by a row below that turns
// RED when it is corrupted:
//
//	type_parameters                struct Box<T> { … }         ← the list itself
//	type_identifier                <T>                         ← a bare parameter
//	constrained_type_parameter     <T: Order>   field "left"   ← bounded
//	optional_type_parameter        <T = Order>  field "name"   ← defaulted
//
// and the ones deliberately NOT matched, each because matching them would
// refuse a REAL type and silently delete a correct edge — the over-refusal
// direction, which #7056 established has no symptom at all:
//
//	trait_bounds       <T: Order>       `Order` is a real trait
//	default_type       <T = Real>       `Real` is a real struct  ← cpp's shipped bug
//	lifetime           <'a, T>          `'a` binds `a` in a separate namespace
//	const_parameter    <const N: Alias> `Alias` is a real type_alias, and `N`
//	                                    lives in the VALUE namespace so a field
//	                                    typed `Order` beside `<const Order: usize>`
//	                                    still means the STRUCT (compiled: it does)
//	where_clause       where T: Order   a SIBLING of type_parameters, never read
//
// `<T: Bnd = Order>` nests: optional_type_parameter/name is a
// constrained_type_parameter, not a type_identifier. Taking "the first
// type_identifier child" would collect NOTHING there and leave the over-fire
// live for that form — cpp's other direction. The collector recurses; row
// `default_with_bound` grades it.

const rustTPPath = "src/g.rs"
const rustTPRef = "scope:component:class:rust:src/g.rs:"

// rustTPPreamble is held constant across every row so that a row's own source
// is the only thing that varies. It declares the four target FORMS this pass
// admits (struct, enum via `Ce` in the rows that need it, trait, type_alias)
// plus `Real`, the positive control every row carries: a field typed `Real`
// must keep its edge in EVERY fixture, so no row can pass by the producer
// having stopped producing.
const rustTPPreamble = `pub struct Order { pub n: u8 }
pub struct Real { pub n: u8 }
pub trait Bnd {}
pub trait Bnd2 {}
pub type Alias = usize;
`

type rustTPCase struct {
	name string
	src  string
	want []string
}

// TestRustFieldTypeRefs_7041_TypeParameterFormSpace enumerates the Rust
// type-parameter form space and grades EVERY row in BOTH directions: the
// shadowed field must emit NOTHING, and a real same-file type named in the same
// fixture must still bind. `want` is the COMPLETE sorted set of
// ref_kind=field_target_type edges for the fixture, so an extra edge fails it
// as loudly as a missing one.
//
// VARIED / HELD-CONSTANT — every row is classified by the axis it VARIES, not
// by the heading it sits under. A control row grades the property it varies,
// and nothing else (#7041 trap (c): this mistake has shipped on five
// consecutive PRs, twice inside a fix for it).
//
//	ANCHOR ROW (every axis at its anchor value): `plain`.
//	  anchor                 = named-struct field
//	  parameter form         = a bare `<Order>`
//	  owner form             = top-level `struct`
//	  collider form          = `struct`, declared BEFORE the generic (preamble)
//	  candidate multiplicity = ONE candidate, and it is the refused one
//	  serde attributes       = none
//
//	VARIES the PARAMETER FORM — which spellings enter the shadow set:
//	  two_params, bounded, multi_bound, where_clause, lifetime,
//	  const_param_type, const_param_name, default, default_with_bound
//	VARIES the SHADOWED FIELD'S CANDIDATE MULTIPLICITY — what the shadow set
//	is allowed to REMOVE from a field, i.e. per-candidate vs per-field:
//	  mixed_candidates_struct, mixed_candidates_enum_variant
//	VARIES the ANCHOR (which producer makes the field record):
//	  enum_variant_field, tuple_struct_field, union_field,
//	  mixed_candidates_enum_variant (varies anchor AND multiplicity together,
//	  deliberately: the enum producer computes its own shadow set)
//	VARIES the OWNER'S NESTING (which item the declaration sits in):
//	  nested_in_mod, nested_in_fn
//	VARIES the COLLIDER'S DECLARING FORM:
//	  collider_trait, collider_enum, collider_type_alias
//	VARIES the COLLIDER'S POSITION relative to the generic declaration:
//	  collider_declared_after
//	VARIES the DECLARATION the parameter belongs to (scope leakage):
//	  sibling_declaration_keeps_edge
//	GRADES PRODUCER LIVENESS ONLY — varies nothing about scoping, because its
//	parameter name is never a same-file type. NOT a scoping control and not
//	counted as one anywhere:
//	  non_colliding_param
//
// STILL HELD CONSTANT, named so the next reader can attack it rather than
// inherit the blind spot:
//
//   - SERDE ATTRIBUTES on the shadowed field. No row carries `#[serde(rename)]`
//     or `#[serde(skip)]`. This axis cannot exercise the guard: both are
//     handled in rustFieldsFromList BEFORE the stash and act on the field's
//     WIRE NAME, never on its declared type, and a skipped field `continue`s
//     before any candidate is collected. It is therefore recorded as unable
//     to distinguish, not as covered.
//   - THE NUMBER OF DECLARATIONS IN THE FILE beyond two. `mixed_candidates_-
//     struct` carries two generic declarations (`Holder`, `G22`) and
//     `sibling_declaration_keeps_edge` one generic and one not, which is what
//     grades per-declaration scoping; a third would vary nothing new.
//
// MULTIPLICITY WAS THE AXIS THIS BLOCK ORIGINALLY FAILED TO NAME, and the
// omission is why the gap was invisible: with every shadowed field spelled as
// one bare candidate, "drop the refused candidate" and "drop the whole field"
// are the same function, so a whole-field refusal deleted correct edges at
// both anchors with the suite green. The portable lesson is the one this
// family keeps relearning — a control row grades the property it VARIES, not
// the property it is filed under, and an axis a block does not NAME is an
// axis nobody audits.
//
// Every row also carries the liveness control inside itself (the `ctl` / `b` /
// `ali` / `m` fields), which is what distinguishes "the refusal works" from
// "the producer died" — the distinction the deleted pin could not make.
func TestRustFieldTypeRefs_7041_TypeParameterFormSpace(t *testing.T) {
	cases := []rustTPCase{
		{
			name: "plain",
			src:  "pub struct G1<Order> { pub item: Order, pub ctl: Real }",
			want: []string{"G1.ctl -> " + rustTPRef + "Real"},
		},
		{
			name: "two_params",
			src:  "pub struct G2<Order, Real> { pub a: Order, pub b: Real, pub c: Alias }",
			want: []string{"G2.c -> " + rustTPRef + "Alias"},
		},
		{
			// `Bnd` is a REAL trait. Harvesting the bound would delete G3.b.
			name: "bounded",
			src:  "pub struct G3<Order: Bnd> { pub item: Order, pub b: Box<dyn Bnd>, pub ctl: Real }",
			want: []string{
				"G3.b -> " + rustTPRef + "Bnd",
				"G3.ctl -> " + rustTPRef + "Real",
			},
		},
		{
			name: "multi_bound",
			src:  "pub struct G4<Order: Bnd + Bnd2> { pub item: Order, pub b1: Box<dyn Bnd>, pub b2: Box<dyn Bnd2> }",
			want: []string{
				"G4.b1 -> " + rustTPRef + "Bnd",
				"G4.b2 -> " + rustTPRef + "Bnd2",
			},
		},
		{
			// where_clause is a SIBLING of type_parameters in the CST, so the
			// parameter must still be refused from the list alone, and the
			// clause's `Bnd` must survive.
			name: "where_clause",
			src:  "pub struct G5<Order> where Order: Bnd { pub item: Order, pub b: Box<dyn Bnd>, pub ctl: Real }",
			want: []string{
				"G5.b -> " + rustTPRef + "Bnd",
				"G5.ctl -> " + rustTPRef + "Real",
			},
		},
		{
			// A lifetime is not a type name. `struct a` is a real same-file
			// type and `'a` must not refuse it — compiled: `struct L<'a> { y: a }`.
			name: "lifetime",
			src: "pub struct a { pub n: u8 }\n" +
				"pub struct G6<'a, Order> { pub item: &'a Order, pub y: a, pub ctl: &'a Real }",
			want: []string{
				"G6.ctl -> " + rustTPRef + "Real",
				"G6.y -> " + rustTPRef + "a",
			},
		},
		{
			// `const N: Alias` is LEGAL (Alias = usize) and `Alias` is a real
			// same-file type_alias. Harvesting a const parameter's TYPE would
			// delete G7.ali.
			name: "const_param_type",
			src:  "pub struct G7<const N: Alias> { pub item: [u8; N], pub ali: Alias, pub ctl: Real }",
			want: []string{
				"G7.ali -> " + rustTPRef + "Alias",
				"G7.ctl -> " + rustTPRef + "Real",
			},
		},
		{
			// A const parameter binds in the VALUE namespace: `struct
			// C<const Order: usize> { x: Order }` COMPILES beside `struct
			// Order`, and `x` is the STRUCT. Refusing the const parameter's
			// NAME would delete G8.x.
			name: "const_param_name",
			src:  "pub struct G8<const Order: usize> { pub x: Order, pub ctl: Real }",
			want: []string{
				"G8.ctl -> " + rustTPRef + "Real",
				"G8.x -> " + rustTPRef + "Order",
			},
		},
		{
			// cpp's exact shipped bug: harvesting the DEFAULT. `Real` is real.
			name: "default",
			src:  "pub struct G9<Order = Real> { pub item: Order, pub ctl: Real }",
			want: []string{"G9.ctl -> " + rustTPRef + "Real"},
		},
		{
			// optional_type_parameter/name is a constrained_type_parameter
			// here, not a type_identifier. "First type_identifier child" would
			// collect nothing and leave the over-fire live.
			// `impl Bnd for Real {}` is here because rustc requires the
			// default to satisfy the bound (E0277) — the row is legal Rust,
			// checked with rustc, not merely plausible. It also costs nothing:
			// subtype "impl" is not in the target set.
			name: "default_with_bound",
			src: "impl Bnd for Real {}\n" +
				"pub struct G10<Order: Bnd = Real> { pub item: Order, pub b: Box<dyn Bnd>, pub ctl: Real }",
			want: []string{
				"G10.b -> " + rustTPRef + "Bnd",
				"G10.ctl -> " + rustTPRef + "Real",
			},
		},
		{
			// VARIES THE ANCHOR: the field record comes from an enum variant,
			// a different producer (emitRustEnumVariantFields) reached through
			// a different owner node.
			name: "enum_variant_field",
			src:  "pub enum G11<Order> { V { item: Order, ctl: Real } }",
			want: []string{"G11.V.ctl -> " + rustTPRef + "Real"},
		},
		{
			// VARIES THE SHADOWED FIELD'S CANDIDATE MULTIPLICITY, the axis
			// every other row holds constant at ONE — and the only axis on
			// which the refusal being per-CANDIDATE rather than per-FIELD is
			// observable at all. Each of `m`, `p`, `q` and `h` names the
			// parameter `Order` AND a real same-file type in the SAME declared
			// type, so "drop the refused candidate" and "drop the whole field"
			// stop being the same behaviour here. Without this row a
			// whole-field refusal silently deletes four correct edges with the
			// rust and resolve suites green — the direction #7056 established
			// has no symptom, one level up from where the rest of this table
			// attacks it (those rows grade which names ENTER the shadow set;
			// this one grades what the shadow set may REMOVE from a field).
			//
			// `q` puts the REAL type FIRST and the parameter second, so the
			// position of the refused candidate within the list is varied too.
			// `h` makes the surviving candidate the generic CONSTRUCTOR
			// (`Holder`, itself a same-file generic declaration), not an
			// argument. `v: Vec<Order>` is the row's PERMISSIVE half: every
			// candidate is either unmodelled or refused, so it must emit
			// NOTHING — applying the refusal only to single-candidate fields
			// would put this issue's own wrong-and-BINDING edge straight back.
			//
			// The shape is ordinary, not exotic: `Vec<T>`, `HashMap<K, Real>`,
			// `Option<T>`, `(T, Real)` are how a generic container's fields are
			// normally spelled. The corpus was already exercising it — this
			// PR's own refusal listing names `just/src/recipe.rs:Recipe.dependencies`,
			// whose source line is `pub(crate) dependencies: Vec<D>,`.
			name: "mixed_candidates_struct",
			src: "use std::collections::HashMap;\n" +
				"pub struct Holder<X> { pub x: X }\n" +
				"pub struct G22<Order> { pub m: HashMap<Order, Real>, pub v: Vec<Order>, " +
				"pub p: (Order, Real), pub q: (Real, Order), pub h: Holder<Order>, pub ctl: Real }",
			want: []string{
				"G22.ctl -> " + rustTPRef + "Real",
				"G22.h -> " + rustTPRef + "Holder",
				"G22.m -> " + rustTPRef + "Real",
				"G22.p -> " + rustTPRef + "Real",
				"G22.q -> " + rustTPRef + "Real",
			},
		},
		{
			// VARIES CANDIDATE MULTIPLICITY *AT THE ENUM ANCHOR*. The two
			// anchors reach the stash site through different producers
			// (emitRustStructFields vs emitRustEnumVariantFields), each
			// computing the shadow set from a different owner node, so a
			// per-field refusal has to be graded at both. Closing it on the
			// struct alone would leave the enum producer ungraded on this
			// axis — the same mistake one level down.
			name: "mixed_candidates_enum_variant",
			src: "use std::collections::HashMap;\n" +
				"pub enum G23<Order> { V { m: HashMap<Order, Real>, v: Vec<Order>, " +
				"p: (Order, Real), ctl: Real } }",
			want: []string{
				"G23.V.ctl -> " + rustTPRef + "Real",
				"G23.V.m -> " + rustTPRef + "Real",
				"G23.V.p -> " + rustTPRef + "Real",
			},
		},
		{
			// VARIES THE ANCHOR: a tuple struct's positional fields are an
			// ordered_field_declaration_list, which this package has never
			// emitted field records for. So the over-fire is UNREACHABLE at
			// this anchor — recorded, not silently assumed: the row asserts
			// the whole fixture is empty, including `Real`.
			name: "tuple_struct_field",
			src:  "pub struct G12<Order>(pub Order, pub Real);",
			want: nil,
		},
		{
			// VARIES THE ANCHOR: `union_item` is not a case in walk(), so a
			// union emits no component and no fields at all.
			name: "union_field",
			src:  "pub union G13<Order: Copy> { pub item: Order }",
			want: nil,
		},
		{
			// VARIES THE OWNER'S NESTING. Per rustc, a nested item sees no
			// enclosing generics, so its OWN list is the whole scope.
			name: "nested_in_mod",
			src:  "pub mod m14 { use super::Real; pub struct G14<Order> { pub item: Order, pub ctl: Real } }",
			want: []string{"G14.ctl -> " + rustTPRef + "Real"},
		},
		{
			// VARIES THE OWNER'S NESTING, and finds a second UNREACHABLE
			// anchor: walk()'s `function_item` case returns without descending
			// into the body, so a struct declared inside a function is never
			// extracted at all — no component, no fields, no edges, not even
			// for `Real`. Recorded rather than assumed, and asserted as empty
			// so that a future pass which does descend turns this row RED and
			// has to re-derive the rule (a nested item still sees only its own
			// parameters, per E0401 above).
			name: "nested_in_fn",
			src:  "pub fn f15() { struct G15<Order> { item: Order, ctl: Real } let _x: Option<G15<u8>> = None; }",
			want: nil,
		},
		{
			// VARIES THE COLLIDER'S DECLARING FORM: trait / enum / type_alias.
			// The refusal must not depend on what the shadowed declaration is.
			name: "collider_trait",
			src:  "pub struct G16<Bnd> { pub item: Bnd, pub ctl: Real }",
			want: []string{"G16.ctl -> " + rustTPRef + "Real"},
		},
		{
			name: "collider_enum",
			src: "pub enum Ce { A }\n" +
				"pub struct G17<Ce> { pub item: Ce, pub ctl: Real }",
			want: []string{"G17.ctl -> " + rustTPRef + "Real"},
		},
		{
			name: "collider_type_alias",
			src:  "pub struct G18<Alias> { pub item: Alias, pub ctl: Real }",
			want: []string{"G18.ctl -> " + rustTPRef + "Real"},
		},
		{
			// VARIES THE COLLIDER'S POSITION: declared AFTER the generic. The
			// pass decides candidates during the walk and targets after it, so
			// the two orders travel different paths.
			name: "collider_declared_after",
			src: "pub struct G19<Late> { pub item: Late, pub ctl: Real }\n" +
				"pub struct Late { pub n: u8 }",
			want: []string{"G19.ctl -> " + rustTPRef + "Real"},
		},
		{
			// VARIES THE DECLARATION THE PARAMETER BELONGS TO. `G20`'s
			// parameter must not leak to `Sib20`, whose `Order` is the struct.
			// This is the ONLY row that grades "per declaration, not per file".
			name: "sibling_declaration_keeps_edge",
			src: "pub struct G20<Order> { pub item: Order }\n" +
				"pub struct Sib20 { pub buyer: Order }",
			want: []string{"Sib20.buyer -> " + rustTPRef + "Order"},
		},
		{
			// GRADES PRODUCER LIVENESS ONLY. `T` is not a same-file type, so
			// this row would pass unchanged with the refusal deleted. It is
			// listed so it is not mistaken for a scoping control.
			name: "non_colliding_param",
			src:  "pub struct G21<T> { pub item: T, pub ctl: Real }",
			want: []string{"G21.ctl -> " + rustTPRef + "Real"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recs := extractRustFiles(t, map[string]string{
				rustTPPath: rustTPPreamble + tc.src + "\n",
			})
			got := rustFieldTypeRefEdges(t, recs)
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Fatalf("field_target_type edges\n got: %v\nwant: %v", got, tc.want)
			}
		})
	}
}
