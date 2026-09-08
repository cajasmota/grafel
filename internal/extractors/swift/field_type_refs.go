package swift

import (
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	"github.com/cajasmota/grafel/internal/types"
)

// field_type_refs.go — the field→declared-type edge for Swift (issue #6912,
// arm G). Arm A is internal/extractors/csharp/field_type_refs.go (#6984),
// arm B internal/extractors/proto (#6991), arm C internal/extractors/rust
// (#7000), arm D internal/extractors/golang (#7036), arm F
// internal/extractors/java (#7039), arm E internal/extractors/fsharp (#7040).
//
// THE GAP. emitSwiftFieldMembers (#4854) mints one SCOPE.Schema/field record
// per stored property and records the declared type ONLY as the unhashed
// `field_type` property. Nothing links the field to the ENTITY of that type,
// so every Swift field is a leaf on its outbound side — the population
// contributor #6726 measures at 99.3% orphan for SCOPE.Schema. Swift fields are
// minted as SCOPE.Schema (field_members.go), so they are IN that population,
// not adjacent to it.
//
// THE TRAP THIS ARM EXISTS TO CLOSE, AND WHY THE PROPERTY IS NOT THE INPUT.
// `field_type` is firstDescendantText(type_annotation, "type_identifier") —
// the FIRST PRE-ORDER type_identifier under the annotation. Probed against the
// real grammar rather than read:
//
//	var d: Dictionary<String, Order>   user_type[ type_identifier "Dictionary",
//	                                              type_arguments[…"Order"…] ]
//	                                   → field_type == "Dictionary"
//	var j: Set<Order>                  → field_type == "Set"
//	var k: Result<Order, MyErr>        → field_type == "Result"
//	var c: [Order]                     array_type[ user_type "Order" ]
//	                                   → field_type == "Order"  (by luck of order)
//	var b: Order?                      → field_type == "Order"  (by luck of order)
//	var h: Foundation.Data             user_type[ tid "Foundation", ".", tid "Data" ]
//	                                   → field_type == "Foundation"
//
// So for every generic wrapper the argument is ALREADY GONE by the time the
// property is written, and for a module-qualified type the property holds the
// MODULE. Reading `field_type` and emitting an edge would ship exactly the
// `List<Customer>` → `List` binding arm A went out of its way to forbid, and a
// `Foundation.Data` field would bind to a same-file `struct Foundation`.
//
// This arm therefore takes arm C's technique — stash the candidates FROM THE
// AST at the emit site — and not arm E's (tokenise the type string), because
// F#'s `member_type` is the declared type verbatim and Swift's `field_type` is
// not. `field_type` is left exactly as it is: fixing it would change entity
// Properties and Signature for every Swift field in the graph, which is a
// separate change with its own blast radius. Its lossiness is pinned by
// TestSwiftFieldTypeRefs_FieldTypePropertyIsLossyForGenerics so the next reader
// meets the trap as a failing expectation rather than as prose.
//
// ==========================================================================
// THE AMBIGUITY RULE — SWIFT NEEDS BOTH SHIPPED RULES, SPLIT BY TARGET KIND
// ==========================================================================
//
// The standing instruction on #6912 is: COUNT THE KINDS THAT THE RESOLVER TIER
// WHICH RESOLVES YOUR ADDRESS ACTUALLY WEIGHS. Arm D (go) counts all kinds,
// arm F (java) counts within the component address family, arm C (rust) counts
// records and is wrong (#7038). Swift is the first arm where the answer is not
// one of those three, because SWIFT EMITS TARGETS IN TWO DIFFERENT KIND SPACES
// and the same address resolves them through two DIFFERENT tiers.
//
// The address is BuildComponentStructuralRef → `scope:component:class:swift:
// <file>:<Name>`, so lookupStructural (internal/resolve/refs.go:2982) calls
// lookupLocationKind(file, name, structuralKindFamilies("component")) =
// componentKindFamily. Driven through the real resolver, not inferred:
//
//   - TARGET IS SCOPE.Component (class / struct / enum / actor / protocol).
//     byLocationKind indexes an entity under BOTH its Kind and its SCOPE-
//     trimmed Kind (refs.go:1207-1210), so a SCOPE.Component lands under
//     "SCOPE.Component" AND "Component", and "Component" is in
//     componentKindFamily. lookupLocationKind's tier-2 pass therefore MATCHES
//     and returns before ambigLocation is ever consulted. Only a second kind
//     THAT IS ALSO IN THAT FAMILY can make uniqueMatchInFamily see two distinct
//     ids and fail. This is arm F's rule, and Swift needs it for the same
//     reason java did: an `enum Status` emits a SCOPE.Component AND a
//     SCOPE.Enum value-set of the SAME NAME IN THE SAME FILE (swift.go:116-120,
//     types.go buildEnumValueSet). SCOPE.Enum trims to "Enum", which is in NO
//     family, so the value-set cannot shadow the Component — and arm D's
//     all-kinds rule would refuse EVERY Swift enum target.
//
//   - TARGET IS SCOPE.Schema/type_alias (`typealias Money = Double`,
//     types.go:154-165). "SCOPE.Schema" trims to "Schema", which is NOT in
//     componentKindFamily, so lookupLocationKind MISSES ENTIRELY and the ref
//     falls through to the kind-agnostic pair at refs.go:3004-3012:
//     ambigLocation first, then byLocation. ambigLocation is set whenever a
//     (file, name) meets a SECOND DISTINCT ENTITY ID (refs.go:1301-1316) —
//     ANY kind at all. So for an alias target the count must span every kind.
//     This is arm D's rule, and arm F's rule applied here would emit an edge
//     that DANGLES: `typealias Tier` beside a top-level `func Tier()` is
//     Schema + Operation, invisible to a component-family count, and
//     statusAmbiguous at the resolver. Graded by
//     TestSwiftFieldTypeRefs_AliasShadowedByAnOperationWouldDangleUnderArmFsRule,
//     which drives the real resolver rather than asserting the emit decision.
//
// The unit is the KIND rather than the RECORD in both halves, mirroring the
// resolver: graph.EntityID hashes (repo, Kind, Name, SourceFile) with Subtype
// EXCLUDED, and buildSymbolIndex marks a collision only on `existing != e.ID`
// (refs.go:1308). Two same-file records sharing a Kind are ONE node and must
// not be counted twice. Arm C counts records and is wrong for exactly this
// reason (#7038) — its rule is not copied here.
//
// WHY NOT SIMPLY DROP type_alias TARGETS AND USE ARM F'S RULE UNCHANGED. That
// is the arm-F counterfactual and it is a pure recall loss: arm D measured 19%
// of Go's edges on exactly this shape after nearly inheriting arm C's
// Component-only guard. The measured Swift figure is in the PR body. A Swift
// typealias is a first-class nameable type and a field declared with one is
// not a different proposition from a field declared with a struct.
//
// ==========================================================================
// WHAT THIS PASS IS NOT
// ==========================================================================
//
//   - SAME FILE ONLY. A field whose type is declared in another file gets
//     nothing. Binding a bare type name across files is the #6976/#6369
//     hazard and every arm on this issue rests on refusing it. For Swift this
//     is the dominant miss: a Swift module is a directory of files and
//     one-type-per-file IS the convention, so the recall ceiling here is lower
//     than C#'s or Java's. Both directions of the same-file conjuncts are
//     graded — a foreign-file record must not SUPPRESS an edge (pass 1) and
//     must not BECOME one (pass 2) — because grading one is precisely what
//     makes the other look covered.
//
//   - A DOTTED TYPE GETS NO EDGE AT ALL, head or tail. tree-sitter-swift gives
//     `Foundation.Data` and `Order.Inner` and the metatype `Order.Type` one
//     `user_type` node carrying TWO type_identifier children around an
//     anonymous ".". Taking the tail would bind `Order.Inner` to a same-file
//     `Inner`; taking the head would bind `Order.Type` to `Order` but also
//     `Foundation.Data` to a same-file `Foundation`. Both are wrong bindings,
//     and a wrong binding never reaches bug-extractor precisely because it
//     binds. So the head-and-tail of a dotted user_type are both refused —
//     which costs the metatype and the module-qualified same-module type, the
//     honest price of not guessing. Type ARGUMENTS are still descended into,
//     so `Swift.Array<Order>` yields `Order`. Pinned by
//     TestSwiftFieldTypeRefs_DottedTypeBindsNeitherHeadNorTail.
//
//   - AN INFERRED TYPE GETS NOTHING. `var p = Order()` has no type_annotation;
//     the initializer expression is not read. A computed property is not a
//     stored member and has no field entity at all (#4854).
//
//   - PROPERTY-WRAPPER AND ATTRIBUTE TYPES ARE NOT CANDIDATES. `@Published var
//     s: Order` shapes `Published` as a `user_type` under a `modifiers >
//     attribute` sibling of the type_annotation. Candidates are collected from
//     the type_annotation subtree ONLY, so the wrapper is out of reach by
//     construction rather than by a name filter. Graded by
//     TestSwiftFieldTypeRefs_PropertyWrapperTypeIsNotACandidate, which declares
//     a same-file `struct Published` so the assertion can fail.
//
//   - RECORDS THIS PASS CANNOT SEE. The collision scan spans the records this
//     extractor has built, which is every record `Extract` returns. It does NOT
//     span the custom lane: internal/custom/swift/swiftui.go:288 mints a
//     BARE-NAMED SCOPE.Component from an @ObservedObject/@StateObject property
//     NAME in the same file. That kind IS in the component address family, so a
//     property named after a same-file type would make the target ambiguous at
//     the resolver and dangle an edge this pass believed safe. Measured
//     incidence on the corpus is in the PR body; the invariant to preserve is
//     that the custom lane does not mint a bare Component sharing a declared
//     type's name, not that today's corpus happens not to.
//
// A NON-BINDING EDGE IS WORSE THAN NO EDGE (#6912's hard requirement): an
// unresolved stub is kept verbatim, dangles, and is classified bug-extractor,
// so every miss is a full hit on the disposition denominator. That is why the
// rules above refuse rather than guess, and why
// TestSwiftFieldTypeRefs_ResolvesToEntityIDs drives every emitted edge through
// resolve.BuildIndex → resolve.ReferencesEmbedded and names the entity each one
// lands on.

// swiftFieldTypeRefsMetaKey is the per-field stash written by
// emitSwiftFieldMembers and consumed — and deleted — by
// attachSwiftFieldTypeRefs.
//
// The candidates cannot become edges during the walk: `struct Order { var b:
// Customer }` may be declared BEFORE `struct Customer` in the same file, so the
// in-file declaration set is complete only once the walk has finished, and the
// enum value-sets the collision scan must see are appended as the walk goes.
const swiftFieldTypeRefsMetaKey = "field_type_refs"

// swiftFieldTargetRefKind is the value of the `ref_kind` edge property. It
// matches arm A's csFieldTargetRefKind, arm B's protoFieldTargetRefKind, arm
// C's rustFieldTargetRefKind, arm D's goFieldTargetRefKind, arm F's
// javaFieldTargetRefKind and the custom lane's referencesClassEdge verbatim —
// the discriminator is what makes the edge queryable as a field→declared-type
// edge across languages, so it must be one string.
const swiftFieldTargetRefKind = "field_target_type"

// swiftComponentFamilyKeys mirrors internal/resolve's componentKindFamily
// (refs.go:2341-2348) exactly — including the deliberate ABSENCE of
// SCOPE.Service (#6459/#6492) and of any Schema/Enum spelling.
var swiftComponentFamilyKeys = map[string]bool{
	"Component": true,
	"Class":     true,
	"View":      true,
	"Model":     true,
	// The SCOPE.* spellings the family lists literally.
	"SCOPE.Component": true,
	"SCOPE.View":      true,
	"SCOPE.Model":     true,
}

// swiftInComponentAddressFamily reports whether an entity of this Kind is
// WEIGHED by lookupLocationKind under componentKindFamily.
//
// The trim step is not defensive padding: buildSymbolIndex indexes every entity
// under BOTH its Kind and its SCOPE-trimmed Kind (refs.go:1207-1210), so a
// hypothetical "SCOPE.Class" entity lands under the key "Class", which the
// family lists. Deriving membership instead of hand-listing the spellings is
// what keeps this in step with the resolver rather than with a snapshot of it.
func swiftInComponentAddressFamily(kind string) bool {
	if swiftComponentFamilyKeys[kind] {
		return true
	}
	if t := strings.TrimPrefix(kind, "SCOPE."); t != kind && swiftComponentFamilyKeys[t] {
		return true
	}
	return false
}

// swiftTypeParameterNames returns the names bound by a declaration's
// `type_parameters` child — `struct Box<T>` → {"T"}.
//
// Arms A and D both ship a KNOWN-WRONG over-fire here: a type parameter named
// after a same-file type binds the field to that type. Swift can refuse it for
// free because emitSwiftFieldMembers already holds the owning declaration node,
// so the shadowing scope is in hand at the collection site rather than needing
// a scope tracker. Doing it is a deliberate departure from those arms and it is
// in the direction this board keeps getting caught by — too broad, and binding,
// so invisible. Graded by
// TestSwiftFieldTypeRefs_TypeParameterShadowedByASameFileTypeGetsNoEdge.
func swiftTypeParameterNames(node ts.Node, src []byte) map[string]bool {
	if node == nil {
		return nil
	}
	var out map[string]bool
	for i := 0; i < int(node.ChildCount()); i++ {
		tp := node.Child(i)
		if tp == nil || tp.Type() != "type_parameters" {
			continue
		}
		for j := 0; j < int(tp.ChildCount()); j++ {
			p := tp.Child(j)
			if p == nil || p.Type() != "type_parameter" {
				continue
			}
			if n := firstDescendantText(p, src, "type_identifier"); n != "" {
				if out == nil {
					out = make(map[string]bool)
				}
				out[n] = true
			}
		}
	}
	return out
}

// swiftFieldTypeCandidates returns every bare type name written in a field's
// type_annotation, in source order and without deduplication, minus the owner's
// type parameters.
//
// Node handling, each shape confirmed against the real grammar by CST probe:
//
//   - `user_type` — the nominal type. When it is DOTTED (an anonymous "." child
//     stands between two type_identifier children) neither segment is taken;
//     see the header block. Otherwise its single direct type_identifier child is
//     the candidate. Either way any `type_arguments` child IS descended into, so
//     `Dictionary<String, Order>` yields String and Order and NOT Dictionary,
//     and `Swift.Array<Order>` yields Order.
//   - everything else recurses. That covers `optional_type` (`Order?`),
//     `array_type` (`[Order]`), `dictionary_type` (`[String: Order]`),
//     `tuple_type` (`(Int, Order)`), `function_type` (`(Order) -> Void`),
//     `existential_type` (`any Shape`) and `opaque_type` (`some Shape`) with no
//     wrapper list to keep in sync.
//
// A SWIFT PRIMITIVE NEEDS NO BLOCKLIST, and — as in arm D and unlike arm B — a
// blocklist would be actively wrong. `Int`, `String` and `Bool` are ordinary
// stdlib nominal types, not grammar keywords: the parser gives them the same
// `user_type > type_identifier` shape as a user type. They are refused by the
// in-file declaration check instead, which is the correct order of operations,
// because a file that declares `struct Int` genuinely does mean THAT type in
// field position. Both directions graded:
// TestSwiftFieldTypeRefs_PrimitiveFieldsProduceNoEdge and
// TestSwiftFieldTypeRefs_ShadowedStdlibNameIsATarget.
func swiftFieldTypeCandidates(ta ts.Node, src []byte, typeParams map[string]bool) []string {
	var out []string
	var walkType func(n ts.Node)
	walkType = func(n ts.Node) {
		if n == nil {
			return
		}
		if n.Type() == "user_type" {
			dotted := false
			for i := 0; i < int(n.ChildCount()); i++ {
				if c := n.Child(i); c != nil && c.Type() == "." {
					dotted = true
					break
				}
			}
			for i := 0; i < int(n.ChildCount()); i++ {
				c := n.Child(i)
				if c == nil {
					continue
				}
				switch c.Type() {
				case "type_identifier":
					if dotted {
						continue
					}
					name := nodeTextSwift(c, src)
					if name != "" && !typeParams[name] {
						out = append(out, name)
					}
				case "type_arguments":
					walkType(c)
				}
			}
			return
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walkType(n.Child(i))
		}
	}
	walkType(ta)
	return out
}

// nodeTextSwift returns a node's source text.
func nodeTextSwift(n ts.Node, src []byte) string {
	if n == nil {
		return ""
	}
	return string(src[n.StartByte():n.EndByte()])
}

// swiftFieldTypeTarget is one in-file type declaration a field can point at:
// the structural ToID that binds to it and the bare name for `target_type`.
type swiftFieldTypeTarget struct {
	toID string
	name string
}

// swiftInFileTypeTargets indexes every type DECLARED IN THIS FILE that a
// field-type edge may address, keyed by bare name, applying the split
// ambiguity rule derived in the header block.
//
// Pass 1 builds two independent collision counts because the two admitted
// target kinds resolve through two different resolver tiers:
//
//	famKinds[name]  — distinct Kinds in the COMPONENT ADDRESS FAMILY. Governs a
//	                  SCOPE.Component target, which lookupLocationKind resolves
//	                  before ambigLocation is consulted (arm F's rule).
//	allKinds[name]  — distinct Kinds, full stop. Governs a SCOPE.Schema/
//	                  type_alias target, which lookupLocationKind cannot see at
//	                  all, so it falls through to ambigLocation (arm D's rule).
//
// Pass 2's allow-list is written on Swift's own emit sites rather than
// inherited:
//
//	SCOPE.Component + class|struct|enum|actor|protocol → admitted
//	SCOPE.Schema    + type_alias                       → admitted
//
// Everything else is refused. What that actually excludes, and how reachable
// each is, stated as measured rather than as reassurance:
//
//   - SCOPE.Enum (the value-set beside every `enum`) — REACHABLE AND LOAD-
//     BEARING. Admitting it would address the value-set with a component-space
//     ref that the component family cannot resolve to it, so the edge would
//     have to fall through to byLocation, where the same-named Component makes
//     it ambiguous. It is refused as a TARGET while still being invisible to
//     the component-family COUNT — the two halves of arm F's rule.
//   - SCOPE.Schema/field — refused so a field never targets a field. This one
//     is ALSO unreachable by name shape: a field record's Name is dotted
//     (`Order.buyer`) and a candidate is a single type_identifier, which cannot
//     contain a dot. Refused anyway, claimed as nothing more.
//   - SCOPE.Component/module (the import carrier) — refused by subtype, and
//     ALSO unreachable by name shape, because buildImport namespaces the
//     carrier as `<file>::import::<module>` precisely so it cannot match a bare
//     Swift type identifier (#492). Go had no such protection and its import
//     placeholder was the one record its allow-list genuinely had to stop; the
//     Swift equivalent is already stopped upstream. Pinned by
//     TestSwiftFieldTypeRefs_ImportCarrierNameCannotCollide so a rename of the
//     carrier surfaces here.
//   - SCOPE.Component/file (#577) — refused by subtype; its Name is the file
//     path.
//   - SCOPE.Operation — refused by kind. Not reachable as a wrong TARGET (its
//     kind is not admitted) but very much reachable as a COLLIDER: a top-level
//     `func Tier()` is what makes a same-named typealias dangle, which is the
//     whole reason the alias arm counts all kinds.
func swiftInFileTypeTargets(records []types.EntityRecord, filePath string) map[string]swiftFieldTypeTarget {
	famKinds := make(map[string]map[string]bool)
	allKinds := make(map[string]map[string]bool)
	for i := range records {
		r := &records[i]
		if r.SourceFile != filePath || r.Name == "" {
			continue
		}
		if allKinds[r.Name] == nil {
			allKinds[r.Name] = make(map[string]bool)
		}
		allKinds[r.Name][r.Kind] = true
		if swiftInComponentAddressFamily(r.Kind) {
			if famKinds[r.Name] == nil {
				famKinds[r.Name] = make(map[string]bool)
			}
			famKinds[r.Name][r.Kind] = true
		}
	}

	targets := make(map[string]swiftFieldTypeTarget)
	for i := range records {
		r := &records[i]
		if r.SourceFile != filePath || r.Name == "" {
			continue
		}
		switch {
		case r.Kind == "SCOPE.Component":
			switch r.Subtype {
			case "class", "struct", "enum", "actor", "protocol":
			default:
				continue
			}
			// Component tier: only a rival IN THE SAME ADDRESS FAMILY can
			// break uniqueMatchInFamily. A SCOPE.Enum value-set cannot.
			if len(famKinds[r.Name]) > 1 {
				continue
			}
		case r.Kind == "SCOPE.Schema" && r.Subtype == "type_alias":
			// Alias tier: the component family cannot see this entity at all,
			// so the ref falls through to ambigLocation, which counts EVERY
			// kind. A same-named Component is included in that count, which is
			// also what keeps a Component target from being clobbered here.
			if len(allKinds[r.Name]) > 1 {
				continue
			}
		default:
			continue
		}
		targets[r.Name] = swiftFieldTypeTarget{
			toID: extractor.BuildComponentStructuralRef("swift", filePath, r.Name),
			name: r.Name,
		}
	}
	return targets
}

// attachSwiftFieldTypeRefs appends one REFERENCES edge per (field, in-file
// declared type) pair and clears the stash it consumed.
//
// FromID is left empty so graph assembly anchors the edge on the field record
// that carries it — the Django precedent at
// internal/extractors/python/django_relational.go:246-252, and arms B, C, D
// and F. (Hibernate sets an explicit structural FromID because it hangs its
// edge off its own parallel SCOPE.Component node; that is a different shape and
// #6912's body was corrected on the point.)
//
// A SELF-REFERENCE IS EMITTED. `class Node { var next: Node? }` yields
// `Node.next → Node`. Arm D suppresses the equivalent, but its reason does not
// transfer: Go already ships a struct-anchored DEPENDS_ON that declines the
// self case, and arm D matched it. Swift has no adjacent declared-type edge to
// stay consistent with, and arms A and C — which likewise have none — do not
// suppress it. The CONTAINS edge relates owner→field, not field→owner, so the
// self edge is not a restatement of one that exists. Graded by
// TestSwiftFieldTypeRefs_SelfReferentialFieldGetsAnEdge.
//
// Relationships is APPENDED to, never assigned: a Swift field record carries no
// outbound edge today, but assigning would silently clobber one added later.
func attachSwiftFieldTypeRefs(records []types.EntityRecord, filePath string) []types.EntityRecord {
	targets := swiftInFileTypeTargets(records, filePath)
	for i := range records {
		r := &records[i]
		if r.Metadata == nil {
			continue
		}
		cands, _ := r.Metadata[swiftFieldTypeRefsMetaKey].([]string)
		delete(r.Metadata, swiftFieldTypeRefsMetaKey)
		if len(cands) == 0 || r.Kind != "SCOPE.Schema" || r.Subtype != "field" {
			continue
		}
		fieldName := r.Properties["field_name"]
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
					{K: "field_name", V: fieldName},
					{K: "ref_kind", V: swiftFieldTargetRefKind},
					{K: "target_type", V: t.name},
				},
			})
		}
	}
	return records
}
