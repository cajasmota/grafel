package proto

import (
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

// field_type_refs.go — the field→declared-type edge for protobuf (issue #6912,
// arm B; arm A is internal/extractors/csharp/field_type_refs.go, #6984).
//
// THE GAP, AND HOW IT DIFFERS FROM C#. buildField records a proto field's
// declared type as the `type` PROPERTY and in the Signature, and never as a
// relationship, so every SCOPE.Schema/field entity this package emits is a leaf
// on its outbound side. That is the #6912 population: #6726 measures
// SCOPE.Schema at 99.3% orphan over their corpus, and protobuf is one of their
// four languages.
//
// What protobuf already had, and what it did NOT have, is the whole design
// question here. buildMessage already emits a REFERENCES edge per named field
// type — but it hangs it off the MESSAGE (`message User` → `message Order`,
// properties `type` / `via_field`). The message is therefore already wired; the
// FIELD is not. "Which fields point at Order?" is answerable for C# after arm A
// and was not answerable for protobuf, because the only edge that knew about
// `Order` was anchored one level up and named its field in a property instead
// of in its endpoints.
//
// So this pass adds a SECOND edge, from the field, rather than moving the
// existing one. Moving it would break the message→message topology the .proto
// dependency view is built on (#6419, #6422); duplicating the ANCHOR is the
// point, not duplicating the information.
//
// VOCABULARY AND ADDRESS ARE ARM A'S, NOT NEW. Kind REFERENCES with the
// property `ref_kind: "field_target_type"` — the spelling the custom lane's
// shared `referencesClassEdge` helper has used in five languages, and the one
// arm A adopted for core. `TYPED_AS` (#6906) and `HAS_TYPE` (#5828) are both
// declared in internal/types/kinds.go with zero producers; a third spelling
// would split every "which fields point at X?" query (#6902).
//
// The address is messageTypeRef — `scope:schema:message:proto:<file>:<Name>` —
// which is this package's existing structural type address, already used by the
// message→type edge, by the file→message CONTAINS edge (#6422) and by rpc
// request/response types (#6359). It is FILE-SCOPED, so it does not go
// AMBIGUOUS when a second .proto declares a same-named message — the failure
// that #6986 measured at 91.6% dangling for the custom lane's `Class:<Name>`
// form. Post-resolution the ToID is the target entity's ID, so no query sees
// the dialect. Note the SCOPE.Enum arm of arm A has no analogue here: protobuf
// enums are SCOPE.Schema/enum entities and take the SAME messageTypeRef
// address, so there is one address dialect on this arm rather than two.
//
// A NON-BINDING EDGE IS WORSE THAN NO EDGE. An unresolved stub is kept verbatim
// and dangling and classified `bug-extractor`, with no masking branch and no
// drop pass, so every miss is a full hit and each emitted edge adds one
// endpoint to the disposition denominator (#6906). The edge is therefore
// emitted ONLY where the target is declared IN THE SAME FILE.
//
// THAT GATE IS NOT NEW EITHER, and reusing it is deliberate. #6357 already
// installed dropUnresolvableTypeRefs, which runs in Extract after the walk and
// removes every REFERENCES edge — on ANY entity — whose ToID is not a
// message/enum declared in this file. Emitting the field edge into that same
// address space means the same-file rule has ONE implementation and ONE
// spelling for both anchors. A second private copy of the check inside this
// file would fire only where that pass already fires, leaving both ungraded
// (the redundant-guard trap arm A called out for its primitive blocklist).
// The consequence is asserted end-to-end rather than described: see
// TestProtoFieldTypeRefs_CrossFileTypeGetsNoEdge.
//
// WHAT THIS PASS IS NOT.
//
//   - It is FILE scope and nothing else. It does not consult the proto
//     `package` declaration, and it cannot: one .proto file declares exactly one
//     package, so there is no in-file namespace shadowing to get wrong. That is
//     why arm A's namespace over-fire has no counterpart here.
//   - It is DELIBERATELY STRICTER THAN THE MESSAGE-LEVEL EDGE on qualified
//     names, and this is the one place the two anchors disagree. namedTypeRefs
//     strips a qualifier to its trailing segment (`.foo.bar.Order` → `Order`),
//     so if THIS file also happens to declare an unrelated `Order`, the
//     message-level edge binds to the wrong entity — silently, because a bound
//     edge never reaches `bug-extractor`. protoFieldTypeCandidates does not
//     strip: any name carrying a `.` is skipped outright. A same-file type is
//     written bare in practice, so the recall this costs is small and the false
//     edge it prevents is invisible. Pinned in both directions by
//     TestProtoFieldTypeRefs_QualifiedTypeIsNotStrippedToASameFileName, which
//     also pins that the message-level edge's behaviour is UNCHANGED by this
//     arm. Widening the field edge is not the fix; narrowing namedTypeRefs is,
//     and it belongs with the cross-file package index #6357 defers.
//   - A NESTED message referenced as `Outer.Inner` therefore gets nothing from
//     this pass, by the same rule. That is the honest floor, asserted by name
//     rather than described.
//   - It cannot reach an IMPORTED type at all, which for protobuf is the
//     majority case: `import "common.proto"; ... Address address = 3;` gets no
//     edge, because pass 1 sees one file and the import directives carry file
//     paths, not package names (#6357). Closing that needs the cross-file proto
//     package index, which is a separate arm.

// protoFieldTargetRefKind is the value of the `ref_kind` edge property. It
// matches arm A's csFieldTargetRefKind and the custom lane's referencesClassEdge
// verbatim — the discriminator is what makes the edge queryable as a
// field→declared-type edge across languages, so it must be one string.
//
// It is NOT the spelling the pre-existing message→type edge uses (`type` /
// `via_field`). That edge is a different relation with a different anchor and is
// left exactly as it was; re-spelling it would be a graph-wide change with no
// bearing on #6912.
const protoFieldTargetRefKind = "field_target_type"

// protoFieldTypeCandidates returns the bare type names a field's declared type
// may address, in source order and without deduplication.
//
// The input is the rendered type string from fieldTypeAndLabel /
// mapFieldTypeAndLabel — the TYPE only. The proto label (`repeated`,
// `optional`, `required`) is returned by those helpers as a SEPARATE value and
// prepended only to the Signature, so `repeated Order orders = 3;` arrives here
// as "Order" and never as "repeated Order". Pinned by
// TestProtoFieldTypeRefs_LabelIsNeverTheTarget.
//
// map<K, V> IS UNWRAPPED, and the rule is: the wrapper is never a target and
// each of K and V is considered independently. proto3 constrains a map key to an
// integral or string scalar, so in practice only V ever yields a candidate — but
// both are walked rather than assuming the spec, exactly as namedTypeRefs does.
// `map<string, Order>` therefore yields [Order]: one candidate, not two, and not
// a `map` target — the `string` key is dropped by the scalar rule below, which
// is load-bearing here and not decorative (see it). All branches are graded by
// TestProtoFieldTypeRefs_MapValueIsTheTarget and, for the shadowed-scalar key,
// by TestProtoFieldTypeRefs_ScalarShadowedByASameFileMessage.
//
// Two rejections:
//
//   - A SCALAR (the full protoScalars set: double float int32 int64 uint32
//     uint64 sint32 sint64 fixed32 fixed64 sfixed32 sfixed64 bool string bytes)
//     yields nothing. This is the largest field population in any .proto file
//     and the one that must produce zero edges.
//
//     THIS CHECK IS NOT REDUNDANT WITH THE IN-FILE GATE, and the first cut of
//     this arm deleted it on the argument that it was. The argument was that no
//     .proto declares `message string`, so the gate drops every scalar anyway
//     and a blocklist in front of it would fire only where the gate already
//     fires. **That is a corpus-relative zero, not a property of the pass**, and
//     it is FALSE IN FACT — review of #6991 constructed the case:
//
//     message string { int32 v = 1; }
//     enum bool { B_ZERO = 0; }
//     message Holder { string name = 1; bool flag = 2; }
//
//     `message string` is in the file, so it IS in the gate's local set: without
//     this check `Holder.name` gets an edge to it and the edge BINDS, so it
//     never reaches `bug-extractor` and nothing surfaces it — the exact failure
//     the qualified-name rule below exists to prevent. The file compiles under
//     protoc at RC 0, and protoc's own descriptor binds `Holder.name` to
//     TYPE_STRING with an EMPTY type_name: the grammar matches `string` in
//     field-type position as a scalar before it ever considers a message type,
//     so the shadowing message can never be the referent and the edge asserts
//     something protoc says does not exist. `map<string, Order>` under that
//     declaration gains a bogus second key edge on top of the right one.
//
//     Graded end to end, not at the unit, by
//     TestProtoFieldTypeRefs_ScalarShadowedByASameFileMessage — because "an
//     edge case that compiles under protoc" is what separates a live guard from
//     a redundant one, and only a fixture containing the case can tell them
//     apart. TestProtoFieldTypeRefs_EveryScalarProducesNoEdge enumerates the
//     whole production scalar set separately, against an independent literal
//     (#6975).
//
//     THE GENERAL LESSON, recorded here because it cost a review round: a
//     mutant killed ONLY by its own unit test does not establish that a guard is
//     redundant. It is equally consistent with the end-to-end population simply
//     lacking the case — and telling those two apart requires CONSTRUCTING the
//     case, which is the step that was skipped. (`namedTypeRefs`, the older
//     message-anchored path, keeps its own protoScalars check; had this one
//     stayed deleted the two anchors would have disagreed in the wrong
//     direction, the field edge over-firing exactly where the message edge
//     correctly stays silent.)
//
//   - A QUALIFIED name — anything containing a `.`, including the
//     fully-qualified leading-dot form `.foo.bar.Order` — yields nothing, and is
//     not stripped to its trailing segment. See the header block: this is where
//     the field edge is deliberately narrower than the message edge.
func protoFieldTypeCandidates(ftype string) []string {
	ftype = strings.TrimSpace(ftype)
	if ftype == "" {
		return nil
	}
	var parts []string
	if strings.HasPrefix(ftype, "map<") && strings.HasSuffix(ftype, ">") {
		inner := ftype[len("map<") : len(ftype)-1]
		for _, p := range strings.Split(inner, ",") {
			parts = append(parts, strings.TrimSpace(p))
		}
	} else {
		parts = append(parts, ftype)
	}
	var out []string
	for _, p := range parts {
		if p == "" || protoScalars[p] || strings.Contains(p, ".") {
			continue
		}
		out = append(out, p)
	}
	return out
}

// protoFieldTypeRefs builds the field→declared-type REFERENCES edges carried by
// ONE field entity, deduplicated by target.
//
// FromID is left empty so graph assembly anchors the edge on the field record
// that carries it — the Django precedent at
// internal/extractors/python/django_relational.go:246-252. (Hibernate sets an
// explicit structural FromID because it hangs its edge off its own parallel
// SCOPE.Component node; that is a different shape and not the one to copy —
// #6912's own body was corrected on this point.)
//
// Every edge returned here is still provisional: dropUnresolvableTypeRefs
// removes the ones whose target is not declared in this file. This function
// deliberately does not pre-filter, so the same-file rule has one owner.
func protoFieldTypeRefs(filePath, fname, ftype string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	emitted := make(map[string]bool)
	for _, cand := range protoFieldTypeCandidates(ftype) {
		toID := messageTypeRef(filePath, cand)
		if emitted[toID] {
			continue
		}
		emitted[toID] = true
		out = append(out, types.RelationshipRecord{
			ToID: toID,
			Kind: string(types.RelationshipKindReferences),
			Properties: types.Props{
				{K: "field_name", V: fname},
				{K: "ref_kind", V: protoFieldTargetRefKind},
				{K: "target_type", V: cand},
			},
		})
	}
	return out
}
