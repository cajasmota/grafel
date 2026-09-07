package csharp

import (
	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// field_type_refs.go — the field→declared-type edge for C# (issue #6912).
//
// THE GAP. emitFieldMembers records a field's declared type as the `field_type`
// PROPERTY and never as a relationship, so every SCOPE.Schema/field entity is a
// leaf on its outbound side: `Order.Buyer` knows it is a `Customer` and the
// graph cannot answer "which fields point at Customer?". #6912 measured
// SCOPE.Schema/field at 86 of the golden corpus's 109 SCOPE.Schema orphans.
//
// THE VOCABULARY IS NOT NEW. The custom lane already emits exactly this edge in
// five languages through one shared helper (internal/custom/{python,golang,
// java,javascript,ruby}, `referencesClassEdge`): Kind REFERENCES with the
// property `ref_kind: "field_target_type"`. Core adopts that spelling rather
// than minting a third name — `TYPED_AS` and `HAS_TYPE` are both declared in
// internal/types/kinds.go with zero producers (#6906, #5828), and a third
// spelling would split every "which fields point at X?" query.
//
// THE ADDRESS IS NOT the custom lane's `Class:<Target>`, and that divergence is
// measured, not stylistic. Both `Customer` and `Class:<Customer>` reach the
// resolver's bare-name tier, which returns AMBIGUOUS the moment a second file
// declares a type of the same name — `Result`, `Options` and `Settings` are
// routine in C#. This pass therefore addresses the target by its exact
// location:
//
//	class / interface / struct / record → extractor.BuildComponentStructuralRef
//	                                      "scope:component:class:csharp:<file>:<Name>"
//	enum                                → extractor.EnumQualifiedName
//	                                      "scope:enum:<file>:<Name>"
//
// Both forms are file-scoped and bind exactly — proven against the production
// resolver (resolve.BuildIndex → LookupStatusHint / ReferencesEmbedded) in
// field_type_refs_6912_test.go, including the two-files-one-name case where the
// bare-name forms go ambiguous. Once resolved the ToID is rewritten to the
// target entity's ID, so the stub dialect is a binding mechanism only and no
// downstream query sees it.
//
// A NON-BINDING EDGE IS WORSE THAN NO EDGE. An unresolved stub is kept verbatim
// and dangling and classified `bug-extractor`; there is no masking branch and no
// drop pass, and every emitted edge adds exactly one endpoint to the disposition
// denominator (#6906). So the pass emits ONLY when the target type is DECLARED
// IN THE SAME FILE — the single condition under which a pass-1 per-file
// extractor can know an entity exists at all. That one check is what drops
// primitives (`int`, `string`), framework and BCL types (`HttpClient`), generic
// wrappers (`List`), open type parameters (`T`) and every unmodelled name: none
// of them is declared in the file, so none of them gets an edge. It is
// deliberately the ONLY guard — a redundant primitive blocklist in front of it
// would fire only where this check already fires, leaving both ungraded.
//
// THE COST OF THAT RULE, stated plainly: a field whose type is declared in
// ANOTHER file gets no edge, which on a one-type-per-file C# codebase is most
// of them. Closing that needs a cross-file type view, and FileInput has none in
// pass 1 (CrossFileFields is attached for pass-2.5 detectors only). That is a
// separate arm, not a widening of this one.

// csFieldTypeRefsMetaKey is the per-field stash written by emitFieldMembers and
// consumed — and deleted — by attachCsharpFieldTypeRefs. The candidates cannot
// be turned into edges during the walk: `class Order { Customer Buyer; }` may be
// declared BEFORE `class Customer` in the same file, so the set of in-file
// declarations is only complete once the walk has finished.
const csFieldTypeRefsMetaKey = "field_type_refs"

// csFieldTargetRefKind is the value of the `ref_kind` edge property, matching
// the custom lane's shared referencesClassEdge helper verbatim.
const csFieldTargetRefKind = "field_target_type"

// csTypeRefCandidates returns every bare type name written in a field's declared
// type expression, in source order and without deduplication.
//
// It descends the type node and collects `identifier` leaves, which unwraps
// nullable (`Order?`), array (`Order[]`), pointer (`Order*`) and tuple
// (`(int, Customer)`) syntax for free, and for a generic returns the CONSTRUCTOR
// as well as its arguments — `Dictionary<string, Order>` yields
// [Dictionary, Order] and `List<Order>` yields [List, Order]. Emitting to `List`
// would be wrong and emitting to `Order` is right; the caller's in-file check is
// what separates them, since `Dictionary` and `List` are never declared in the
// file and `Order` may be. A user-defined generic (`Box<T>` declared here) is
// picked up by the same mechanism.
//
// A qualified name (`App.Other.Customer`, `System.Net.Http.HttpClient`) is NOT
// descended into, and that is the one case where the rule is stricter than the
// in-file check alone: writing `App.Other.Customer` in a file that also declares
// its own `Customer` names the OTHER type, and taking the rightmost segment
// would bind the edge to the wrong entity. A same-file type is written bare in
// practice, so the recall this costs is small and the false edge it prevents is
// silent. Pinned by TestCsharpFieldTypeRefs_QualifiedTypeIsNotResolved.
func csTypeRefCandidates(typ ts.Node, src []byte) []string {
	var out []string
	var walkType func(n ts.Node)
	walkType = func(n ts.Node) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "qualified_name", "alias_qualified_name":
			return
		case "identifier":
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

// csFieldTypeTarget is one in-file type declaration a field can point at: the
// structural ToID that binds to it, and the bare type name for the edge's
// `target_type` property.
type csFieldTypeTarget struct {
	toID string
	name string
}

// csInFileTypeTargets indexes every type DECLARED in this file that a field-type
// edge may address, keyed by bare name.
//
// The SCOPE.Enum value-set node is the enum's target, not the SCOPE.Schema/enum
// declaration twin: the twin carries QualifiedName "" and is unaddressable, and
// the two share a Name so the bare-name tier is ambiguous between them (probed
// in field_type_refs_6912_test.go). Targeting the value-set node is also what
// gives SCOPE.Enum its first inbound edge — #6906 measures that population at
// 100% terminal orphan.
//
// A name declared twice in one file (a partial class, or a class and an enum
// sharing a name) is REMOVED rather than resolved to either, so the pass never
// guesses which of two same-file declarations a field meant.
func csInFileTypeTargets(records []types.EntityRecord, filePath string) map[string]csFieldTypeTarget {
	targets := make(map[string]csFieldTypeTarget)
	collide := make(map[string]bool)
	put := func(name, toID string) {
		if name == "" || toID == "" {
			return
		}
		if _, seen := targets[name]; seen {
			collide[name] = true
			return
		}
		targets[name] = csFieldTypeTarget{toID: toID, name: name}
	}
	for i := range records {
		r := &records[i]
		if r.SourceFile != filePath {
			continue
		}
		switch {
		case r.Kind == "SCOPE.Enum":
			put(r.Name, r.QualifiedName)
		case r.Kind == "SCOPE.Component":
			switch r.Subtype {
			case "class", "interface", "struct", "type":
				put(r.Name, extractor.BuildComponentStructuralRef("csharp", filePath, r.Name))
			}
		}
	}
	for name := range collide {
		delete(targets, name)
	}
	return targets
}

// attachCsharpFieldTypeRefs appends one REFERENCES edge per (field, in-file
// declared type) pair and clears the stash it consumed.
//
// FromID is left empty so graph assembly anchors the edge on the field record
// that carries it — the Django precedent at
// internal/extractors/python/django_relational.go:246-252. (Hibernate sets an
// explicit structural FromID because it hangs its edge off its own parallel
// SCOPE.Component node; that is a different shape and not the one to copy.)
func attachCsharpFieldTypeRefs(records []types.EntityRecord, filePath string) []types.EntityRecord {
	targets := csInFileTypeTargets(records, filePath)
	for i := range records {
		r := &records[i]
		if r.Metadata == nil {
			continue
		}
		cands, _ := r.Metadata[csFieldTypeRefsMetaKey].([]string)
		delete(r.Metadata, csFieldTypeRefsMetaKey)
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
					{K: "ref_kind", V: csFieldTargetRefKind},
					{K: "target_type", V: t.name},
				},
			})
		}
	}
	return records
}
