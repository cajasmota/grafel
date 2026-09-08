package rust

import (
	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// field_type_refs.go — the field→declared-type edge for Rust (issue #6912,
// arm C; arm A is internal/extractors/csharp/field_type_refs.go (#6984), arm B
// internal/extractors/proto/field_type_refs.go (#6991)).
//
// WHAT RUST ALREADY HAD, ASKED THE WAY ARM B TAUGHT US TO ASK IT. #6912's body
// says no core extractor emits this edge; that sentence has now been corrected
// twice, so the question this arm answered FIRST is arm B's: is the FIELD
// record the anchor of some existing type edge, or is something else? For Rust
// the answer is NEITHER. Every relationship this package produces was
// enumerated (struct_fields.go:224 and rust.go:156/208/294/552/918):
//
//	struct/enum → field         CONTAINS   (attachRustFieldContains, #4854)
//	trait/impl  → method        CONTAINS   (#144)
//	operation   → callee        CALLS      (#616)
//	file        → module        IMPORTS
//	trait       → supertrait    EXTENDS
//
// None of them carries a field's declared type at any anchor — unlike protobuf,
// where buildMessage already wired the MESSAGE and left the field a leaf. So
// this arm ADDS the relation rather than duplicating an anchor: there is no
// existing topology to preserve, and nothing to double-count against. The type
// was retained only as the `field_type` PROPERTY and inside the Signature
// ("<type> <wire>"), which is exactly the shape #6912 describes: `Order.buyer`
// knows it is a `Customer` and the graph cannot answer "which fields point at
// Customer?".
//
// VOCABULARY AND ADDRESS ARE INHERITED, NOT RE-DECIDED. Kind REFERENCES with
// the property `ref_kind: "field_target_type"` — the custom lane's spelling in
// five languages (`referencesClassEdge`), adopted by arms A and B. `TYPED_AS`
// and `HAS_TYPE` were DELETED in #6992 precisely so a third spelling could not
// be minted. The address is the structural, file-scoped
// extractor.BuildComponentStructuralRef("rust", <file>, <Name>) —
// `scope:component:class:rust:<file>:<Name>` — and NOT the custom lane's
// `Class:<Name>`, which #6986 measured at 91.6% dangling because the bare-name
// resolver tier goes AMBIGUOUS as soon as a second file declares the same name
// (`Config`, `Error`, `Client` in any Rust workspace). Post-resolution the ToID
// is the target entity's ID, so no query sees the dialect. There is ONE address
// dialect on this arm, not arm A's two: a Rust `enum Foo` is a SCOPE.Component
// (subtype "enum"), so it takes the same component address as a struct. The
// separate SCOPE.Enum value-set nodes emitted by emitRustConstValueSets (#4431)
// are a parallel population and are deliberately NOT targeted — targeting both
// would put two edges on one field for one declaration.
//
// A NON-BINDING EDGE IS WORSE THAN NO EDGE. An unresolved stub is kept verbatim
// and dangling and classified `bug-extractor`; there is no masking branch and
// no drop pass, so every miss is a full hit and each emitted edge adds one
// endpoint to the disposition denominator (#6906). The edge is therefore
// emitted ONLY where the named type is DECLARED IN THE SAME FILE — the single
// condition a pass-1 per-file extractor can verify. That one check is what
// drops `String`, `HashMap`, `serde_json::Value` and every unmodelled name.
//
// WHAT THE CHECK IS NOT.
//
//   - IT IS NOT A PRIMITIVE BLOCKLIST, AND IT DOES NOT NEED ONE — this is the
//     one guard arm B had to keep, and the reason Rust does not is a property
//     of the grammar rather than of our corpus. In tree-sitter-rust, a
//     primitive in TYPE POSITION is the node kind `primitive_type`, never
//     `type_identifier`, at every depth: `i32`, `Vec<u8>`, `Box<str>`,
//     `(Order, i32)`, `fn(Order) -> u8`. rustTypeRefCandidates collects
//     `type_identifier` only, so no primitive is ever a candidate and there is
//     nothing for a blocklist to catch. arm B's counter-example was
//     CONSTRUCTED here rather than argued away (`struct i32;` beside a field
//     `x: i32`, for all 16 primitive names): the shadowing struct IS in the
//     in-file target set, and the field still yields no candidate because the
//     grammar tokenised its type as `primitive_type`. If rustc resolves such a
//     shadow to the struct, this pass MISSES an edge — the safe direction, a
//     miss never dangles — where proto's missing blocklist EMITTED one protoc
//     denies. Enumerated per member by
//     TestRustFieldTypeRefs_EveryPrimitiveShadowedByASameFileStructProducesNoEdge.
//
//     The first cut of this arm ALSO carried explicit `primitive_type`,
//     `lifetime` and `scoped_identifier` skip cases in rustTypeRefCandidates,
//     and they were deleted after a mutant showed the primitive one to be
//     INERT rather than redundant-but-live: all three node kinds are either
//     leaf tokens or carry only `identifier` children, so the collector
//     returns nothing for them whether the case is present or not. That is a
//     stronger claim than arm B's deleted blocklist could make — it does not
//     rest on "our corpus has no such file" — and it was checked in both
//     directions anyway: the deletion leaves every fixture green AND leaves
//     the corpus counts below byte-identical over 789 real .rs files. The one
//     case that is LIVE, `scoped_type_identifier`, is kept and is graded by
//     TestRustFieldTypeRefs_QualifiedPathIsNotStrippedToASameFileName.
//
//   - IT IS FILE SCOPE AND NOTHING ELSE. It consults neither Rust's module
//     scope nor type-parameter scope, so it over-fires twice, and both
//     over-fires produce an edge that BINDS — worse than a dangling edge,
//     because `bug-extractor` never sees a bound edge:
//
//     mod a { pub struct Customer; }
//     mod b { pub struct Order { buyer: Customer } }   ← one file: WRONG edge
//     struct Box<Order> { item: Order }                ← beside a same-file
//     struct Order: WRONG
//
//     So an open type parameter `T` is dropped only because nothing in the file
//     happens to be named `T`, NOT because the guard understands type
//     parameters. Both are PINNED as known-wrong by the two
//     TestRustFieldTypeRefs_KnownOverFire_* cases, which a fix is expected to
//     break.
//
//   - IT DOES NOT DESCEND A QUALIFIED PATH. `crate::models::Order`,
//     `super::Order`, `self::Order` and `<Order as Trait>::Assoc` are
//     `scoped_type_identifier` and are skipped WHOLE — the trailing segment is
//     never taken. Writing `crate::other::Order` in a file that also declares
//     its own `Order` names the OTHER type, and stripping to the last segment
//     would bind the edge to the wrong entity, silently. Arm B carries a
//     forbidden row named for exactly that mutation; so does this arm
//     (TestRustFieldTypeRefs_QualifiedPathIsNotStrippedToASameFileName).
//
//   - THE WRAPPER IS NEVER THE TARGET, but not by a wrapper list. A
//     `generic_type` contributes its constructor AND its arguments as
//     candidates; `Vec`, `Option`, `Box`, `Arc`, `Mutex` and `HashMap` are not
//     declared in the file, so only the element type survives the in-file
//     check. A user-defined generic declared here (`struct Holder<T>`, used as
//     `Holder<Order>`) is a legitimate target by the same rule, which is why
//     the constructor is not blanket-dropped.
//
//   - `impl Order { … }` IS NOT A TYPE DECLARATION and is excluded from the
//     target set. buildImpl emits a SCOPE.Component named after the implementing
//     type, so a Rust file that declares `struct Order` and `impl Order` holds
//     TWO components named `Order` — the overwhelmingly common shape. Treating
//     the impl as a declaration would collide every such name and silently
//     delete the edges this pass exists to add.
//
// THE COST OF THE SAME-FILE RULE, stated plainly: a field whose type is declared
// in another module file gets no edge, and Rust's one-type-per-file convention
// is weaker than C#'s but real. Closing that needs a cross-file type view, which
// FileInput has none of in pass 1. Separate arm, not a widening of this one.

// rustFieldTypeRefsMetaKey is the per-field stash written by rustFieldsFromList
// and consumed — and deleted — by attachRustFieldTypeRefs. The candidates
// cannot become edges during the walk: `struct Order { buyer: Customer }` may be
// declared BEFORE `struct Customer` in the same file, so the set of in-file
// declarations is complete only once the walk has finished.
const rustFieldTypeRefsMetaKey = "field_type_refs"

// rustFieldTargetRefKind is the value of the `ref_kind` edge property, matching
// arm A's csFieldTargetRefKind, arm B's protoFieldTargetRefKind and the custom
// lane's referencesClassEdge verbatim. The discriminator is what makes the edge
// queryable as a field→declared-type edge across languages, so it is one string.
const rustFieldTargetRefKind = "field_target_type"

// rustTypeRefCandidates returns every bare type name written in a field's
// declared type expression, in source order and without deduplication.
//
// It descends the type node and collects `type_identifier` leaves, which unwraps
// reference (`&'a Customer`, `&mut Order`), pointer (`*const Order`), array
// (`[Order; 3]`), tuple (`(Order, i32)`), dyn (`Box<dyn Shipper>`), function
// (`fn(Order) -> u8`) and generic syntax for free. A lifetime carries its name as
// an `identifier`, not a `type_identifier`, so `'a` is dropped by the same rule
// that drops nothing else.
//
// ONE node kind is skipped WHOLE, without descending: `scoped_type_identifier`
// — a qualified path. `crate::models::Order`, `super::Order`, `self::Order` and
// `<Order as Trait>::Assoc` therefore yield NOTHING rather than `Order`, which
// is the point (see the header block).
//
// There is deliberately no case for `primitive_type` or `lifetime`. A primitive
// in type position is its own leaf token and a lifetime carries its name as an
// `identifier`, so neither is — nor contains — a `type_identifier`, and a skip
// case for either would be inert rather than protective. `i32`, `Vec<u8>`,
// `Box<str>`, `(Order, i32)`, `fn(Order) -> u8` and `&'a Customer` are all
// handled by the collector's one rule.
func rustTypeRefCandidates(typ ts.Node, src []byte) []string {
	var out []string
	var walkType func(n ts.Node)
	walkType = func(n ts.Node) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "scoped_type_identifier":
			return
		case "type_identifier":
			out = append(out, string(src[n.StartByte():n.EndByte()]))
			return
		}
		for i := 0; i < int(n.NamedChildCount()); i++ {
			walkType(n.NamedChild(i))
		}
	}
	walkType(typ)
	return out
}

// rustFieldTypeTarget is one in-file type declaration a field can point at: the
// structural ToID that binds to it, and the bare name for the `target_type`
// property.
type rustFieldTypeTarget struct {
	toID string
	name string
}

// rustInFileTypeTargets indexes every TYPE DECLARED in this file that a
// field-type edge may address, keyed by bare name.
//
// The four subtypes are exactly the Rust declaration forms that introduce a name
// into the type namespace and that this package emits as entities: struct, enum,
// trait and type_alias (`type Meters = f64;`). Subtype "impl" is excluded — it
// is a declaration ABOUT a type, carries the implementing type's name, and
// including it would make `struct Order` + `impl Order` a duplicate name (see
// the header block). Subtype "file" (#577) is excluded for the same reason:
// it is not a type.
//
// A name declared twice in one file — two `mod`s each declaring `Config`, or a
// struct and a trait sharing a name — is REMOVED rather than resolved to either,
// so the pass never guesses which declaration a field meant.
func rustInFileTypeTargets(records []types.EntityRecord, filePath string) map[string]rustFieldTypeTarget {
	targets := make(map[string]rustFieldTypeTarget)
	collide := make(map[string]bool)
	for i := range records {
		r := &records[i]
		if r.SourceFile != filePath || r.Kind != "SCOPE.Component" || r.Name == "" {
			continue
		}
		switch r.Subtype {
		case "struct", "enum", "trait", "type_alias":
		default:
			continue
		}
		if _, seen := targets[r.Name]; seen {
			collide[r.Name] = true
			continue
		}
		targets[r.Name] = rustFieldTypeTarget{
			toID: extractor.BuildComponentStructuralRef("rust", filePath, r.Name),
			name: r.Name,
		}
	}
	for name := range collide {
		delete(targets, name)
	}
	return targets
}

// attachRustFieldTypeRefs appends one REFERENCES edge per (field, in-file
// declared type) pair and clears the stash it consumed.
//
// FromID is left empty so graph assembly anchors the edge on the field record
// that carries it — the Django precedent at
// internal/extractors/python/django_relational.go:246-252. (Hibernate sets an
// explicit structural FromID because it hangs its edge off its own parallel
// SCOPE.Component node; that is a different shape and not the one to copy —
// #6912's own body was corrected on this point.)
//
// Relationships is APPENDED to, never assigned: a Rust field record carries no
// outbound edge today, but assigning would silently clobber one added later.
func attachRustFieldTypeRefs(records []types.EntityRecord, filePath string) []types.EntityRecord {
	targets := rustInFileTypeTargets(records, filePath)
	for i := range records {
		r := &records[i]
		if r.Metadata == nil {
			continue
		}
		cands, _ := r.Metadata[rustFieldTypeRefsMetaKey].([]string)
		delete(r.Metadata, rustFieldTypeRefsMetaKey)
		if len(cands) == 0 || r.Kind != "SCOPE.Schema" || r.Subtype != "field" {
			continue
		}
		emitted := make(map[string]bool)
		for _, cand := range cands {
			t, ok := targets[cand]
			if !ok || emitted[t.toID] {
				continue
			}
			emitted[t.toID] = true
			r.Relationships = append(r.Relationships, types.RelationshipRecord{
				ToID: t.toID,
				Kind: string(types.RelationshipKindReferences),
				Properties: types.Props{
					{K: "field_name", V: r.Properties["field_name"]},
					{K: "ref_kind", V: rustFieldTargetRefKind},
					{K: "target_type", V: t.name},
				},
			})
		}
	}
	return records
}
