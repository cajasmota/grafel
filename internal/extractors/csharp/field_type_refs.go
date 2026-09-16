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
// than minting a third name — `TYPED_AS` and `HAS_TYPE` were both declared in
// internal/types/kinds.go with zero producers (#6906, #5828) and were DELETED
// by #6906 once this pass shipped; a third spelling would have split every
// "which fields point at X?" query.
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
// wrappers (`List`) and every unmodelled name: none of them is declared in the
// file, so none of them gets an edge. It is deliberately the ONLY guard — a
// redundant primitive blocklist in front of it would fire only where this check
// already fires, leaving both ungraded.
//
// WHAT THE CHECK IS NOT. It is FILE scope PLUS type-parameter scope, and
// nothing else: it does not consult the C# NAMESPACE, so it still over-fires
// once, and that over-fire produces an edge that BINDS — which makes it worse
// than a dangling edge, because `bug-extractor` never sees a bound edge and no
// disposition figure will surface it.
//
//	namespace A { class Customer … }
//	namespace B { class Order { Customer Buyer … } }   ← one file: WRONG edge
//
// Measured incidence is zero — aspnetcore-mvc has 12 .cs files declaring two or
// more namespaces and emits no field-type edge inside any of them, and
// aspnetcore-realworld and WakeOnLAN have no multi-namespace file at all — which
// is why this arm records the limitation instead of fixing it. It is PINNED as
// known-wrong behaviour by
// TestCsharpFieldTypeRefs_KnownOverFire_NamespaceScopeIsNotConsulted, which a
// fix is expected to break.
//
// IT DOES UNDERSTAND TYPE PARAMETERS AS A SHADOWING SCOPE (#7041). This is the
// second over-fire this header used to record, and it is CLOSED, not pinned:
// `class Box<Customer> { Customer Item … }` beside a same-file `class Customer`
// used to emit an edge asserting `Item`'s declared type was that class. It is
// not — `Customer` there is the type parameter, which shadows the declaration
// inside `Box`. The refusal lives in csVisibleTypeParameterNames; see it for
// C#'s scoping rule, why that rule is NOT kotlin's, java's or rust's, the
// grammar evidence behind it, and — since no C# compiler exists on the machine
// this was written on — which rows are UNVERIFIED and what would settle them.
// There are FOUR such rows — Sn4, G13, G9 and G8 — all named there, with G9
// flagged as SHARING G13's premise rather than independently confirming it, and
// two further rows (G5, G9's previous spelling) recorded there as found ILLEGAL
// and fixed. Two earlier revisions of this sentence were wrong in turn: one
// promised "exactly which rows" while the block below named Sn4 alone, and the
// next claimed "exactly two" when re-derivation found four unverified and two
// illegal. Both corrections are on the record rather than folded in silently.
// The known-wrong pin
// (TestCsharpFieldTypeRefs_KnownOverFire_TypeParameterShadowsSameFileType, a
// hard `t.Fatalf` asserting the wrong edge WAS present) was deleted with the fix
// and replaced by field_type_refs_7041_test.go.
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
//
// `typeParams` is the shadow set csVisibleTypeParameterNames computed for this
// field's position (#7041). A name bound there is DROPPED as a candidate and
// nothing else about the field changes — the field's OTHER candidates survive.
// That distinction is the whole of the over-refusal direction: `Dict<T, Order>`
// inside `class Box<T>` must still reach `Dict` and `Order`, and a refusal that
// abandoned the field would delete two correct edges with no symptom (#7056).
// Graded by the multiplicity rows of
// TestCsharpFieldTypeRefs_7041_ParameterFormSpace (G10, G11, G12) at BOTH the
// property and the record-positional anchor.
func csTypeRefCandidates(typ ts.Node, src []byte, typeParams map[string]bool) []string {
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
			if name := string(src[n.StartByte():n.EndByte()]); !typeParams[name] {
				out = append(out, name)
			}
			return
		}
		for i := 0; i < int(n.NamedChildCount()); i++ {
			walkType(n.NamedChild(i))
		}
	}
	walkType(typ)
	return out
}

// csVisibleTypeParameterNames returns every type-parameter name that shadows a
// same-file type declaration at `node`'s position — the union of the
// `type_parameter_list`s carried by every LEXICALLY ENCLOSING node.
//
// ── C#'s RULE, DERIVED, AND WHY IT IS NOT A PORT ──────────────────────────────
//
// ASCEND UNCONDITIONALLY. ECMA-334 §7.7 ("Scopes") gives the scope of a type
// parameter declared by a type_parameter_list on a class_declaration as "the
// class_base, type_parameter_constraints_clauses, and class_body of that
// class_declaration", with the same sentence repeated for struct_declaration,
// interface_declaration and delegate_declaration, and §7.7.1 makes the inner
// declaration SHADOW the outer name. A nested type declaration is part of the
// enclosing class_body, so a nested type's members are inside the outer
// parameters' scope. Microsoft's COMPILER-MESSAGE page for CS0693 ("type
// parameter 'T' has the same name as the type parameter from outer type") says
// it directly — it describes "a generic member (such as a method or NESTED
// TYPE) … inside a generic class" — and the diagnostic exists at all precisely
// BECAUSE the outer parameter is visible in the nested declaration. At the CLR
// level `Outer<T>.Inner` is `Outer`1+Inner`, generic over the same T.
// (An earlier revision of this comment cited the "Generic Classes" guidance
// page for the nested-type claim. It was checked on review and DOES NOT MENTION
// NESTED TYPES; the citation was wrong and is corrected here rather than
// quietly dropped.)
//
// That is the same IMPLEMENTATION as java's arm and a DIFFERENT justification,
// and the difference matters because three sibling arms have three incompatible
// rules and none of them is portable:
//
//	kotlin  ascends only through `inner` — a plain nested class does NOT capture,
//	        so `T` there really names the same-file type.
//	java    ascends unconditionally, but because the static-context reference is
//	        ILLEGAL (javac: "non-static type variable P cannot be referenced from
//	        a static context") — javac does not fall back to a top-level `P`.
//	rust    does not ascend at all (E0401), and a top-level `struct T` does not
//	        make rustc fall back either.
//	C#      ascends unconditionally because the nested reference is LEGAL and
//	        MEANS the outer parameter.
//
// "Which names are in scope" and "which names this pass may bind to" are
// different questions. For C# they coincide, and the reason they coincide is
// stated above rather than assumed from java's arm matching.
//
// ── WHAT COULD NOT BE VERIFIED ───────────────────────────────────────────────
//
// THERE IS NO C# COMPILER ON THE MACHINE THIS WAS WRITTEN ON — `csc`, `dotnet`,
// `mono` and `mcs` are all absent, checked. Two arms of #7041 caught a wrong
// language claim only because someone could run `javac` / `rustc`; nobody can
// here, so the unverified rows are named instead of asserted:
//
//   - `static class` nested inside a generic class (fixture N4/`Sn4`). §7.7
//     scopes the outer parameters over the whole class_body and C# nested types
//     are always static in the Java sense, so it should compile — NOT
//     DEMONSTRATED. The behaviour is safe either way: if legal, `Order` there is
//     the parameter and refusing is right; if illegal, no program exercises the
//     row. What would settle it: `csc` on
//     `class Order{} class O<Order>{ static class S { public static Order A; } }`.
//   - `[Mark]` on a type parameter where `class Mark : System.Attribute` is
//     declared in the SAME FILE (fixture G13). Legal by the default
//     AttributeUsage (all targets) and the optional-`Attribute`-suffix lookup
//     rule — NOT DEMONSTRATED. Safe either way for the same reason as Sn4, and
//     the KEEP half (`Mark` must still bind in field position, which is what
//     kills a descendant-walk collector) only gets stronger if the form is
//     legal. What would settle it: `csc` on that snippet.
//   - `[App.Mark]` on a type parameter (fixture G9) — the same premise as G13
//     with the attribute named by its QUALIFIED spelling. Recorded as a FOURTH
//     unverified row but NOT as a second confirmation of the third: two rows
//     sharing one premise are one premise.
//   - `Order?` where `Order` is an UNCONSTRAINED type parameter (fixture G8).
//     `T?` on an unconstrained parameter requires C# 9 or later — before that
//     it is CS8627 — and the sibling `Real?` warns CS8632 outside a
//     `#nullable enable` context. The row grades the `nullable_type` CST shape,
//     which the parse produces at any LangVersion. NOT DEMONSTRATED.
//
// TWO ROWS WERE FOUND ILLEGAL AND FIXED rather than marked, because an illegal
// program grades nothing at all. This list was RE-DERIVED under review of
// #7075 after its own "exactly two rows" claim proved false in the dangerous
// direction — a row asserted legal that was not:
//
//   - `interface G5<in Order, out Real> { Order A { get; set; } Real B { get;
//     set; } }` was CS1961 twice — a contravariant `in` parameter cannot appear
//     in a getter, a covariant `out` parameter cannot appear in a setter. G5 is
//     the sole occupant of the "variance-annotated" and "interface" axes, so
//     both were ungraded. Now `{ set; }` / `{ get; }`.
//   - `class G9<[System.Obsolete] Order>` was CS0592: ObsoleteAttribute's
//     AttributeUsage does not include GenericParameter. Now `[App.Mark]`.
//
// NOT WRITTEN AT ALL, and named rather than guessed: using a type parameter in
// generic-CONSTRUCTOR position (`class B<D> { D<int,int> f; }`) — believed
// illegal, so no fixture asserts anything about it; and a shadowing parameter on
// a `delegate_declaration`, `method_declaration` or `local_function_statement`,
// which are UNREACHABLE for a different and verified reason given below.
//
// ── NO DECLARATION-KIND LIST, AND WHY THAT IS SOUND ──────────────────────────
//
// The ascent matches on the node type `type_parameter_list` alone. THE SPELLING
// WAS TAKEN FROM THE GRAMMAR, NOT FROM A SIBLING ARM: java's node is
// `type_parameters` and rust's is `type_parameters`; C#'s is
// `type_parameter_list`, so porting either spelling would have been dead code —
// the scala failure mode (#7065), where two matched node types did not exist in
// the grammar at all.
//
// Parsed from the pinned grammar's node-types.json (tree-sitter-c-sharp
// v0.23.1), EXACTLY SEVEN node kinds can carry a `type_parameter_list`:
// class_declaration, struct_declaration, interface_declaration,
// record_declaration, delegate_declaration, method_declaration and
// local_function_statement. Each scopes those parameters over precisely its own
// subtree, so the lexical parent chain IS the scope chain and matching the list
// node alone can neither miss a binder nor invent one. (enum_declaration is
// absent — a C# enum cannot be generic.)
//
// THE LAST THREE ARE MATCHED AND CAN NEVER FIRE. csharp.go's walk RETURNS at
// `method_declaration` and `constructor_declaration` without descending into the
// body, so a type declared inside a method body is never walked and emits no
// SCOPE.Schema/field record; `local_function_statement` only ever appears inside
// such a body; and `delegate_declaration` has no body to declare a field in.
// That is a property of the walk, not a claim about C#, and it is why no fixture
// tries to produce a field anchored under one.
func csVisibleTypeParameterNames(node ts.Node, src []byte) map[string]bool {
	out := map[string]bool{}
	if node == nil {
		return out
	}
	for p := node.Parent(); p != nil; p = p.Parent() {
		csCollectTypeParameterNames(p, src, out)
	}
	return out
}

// csCollectTypeParameterNames adds the names bound by `decl`'s own
// `type_parameter_list` child, if it has one, to `into`.
//
// THE NAME IS READ FROM THE `name` FIELD OF `type_parameter`, never by scanning
// for identifiers. The CST — dumped against tree-sitter-c-sharp v0.23.1, not
// recalled — puts an attribute and a variance annotation as SIBLINGS of the
// name inside the same `type_parameter`:
//
//	<T>                         type_parameter[name: identifier T]
//	<in T, out R>               type_parameter[`in`, name: identifier T]
//	<[System.Obsolete] T>       type_parameter[attribute_list[attribute[
//	                              qualified_name[identifier System, identifier
//	                              Obsolete]]], name: identifier T]
//
// A descendant identifier walk would therefore harvest an attribute's own name
// as a shadowed type and DELETE the correct edges that type earns in field
// position — the silent direction, and exactly what cost the scala arm its
// guard. Probed directly: `ChildByFieldName("name")` returns `identifier=T` for
// `[System.Obsolete] in T`, so the field accessor is immune to both.
//
// A CONSTRAINT CANNOT BE HARVESTED AT ALL, structurally. `where T : Order` is a
// `type_parameter_constraints_clause`, a SIBLING of `type_parameter_list` under
// the declaration, never a child of it — so `Order` is out of this function's
// reach no matter how it walks. That is what makes cpp's shipped bug (#7057,
// which collected `template<typename T = Order>`'s default argument and silently
// deleted a correct edge) unreproducible here. Graded anyway, not assumed:
// row G3.B of TestCsharpFieldTypeRefs_7041_ParameterFormSpace is the constraint
// type in field position, and it must bind.
func csCollectTypeParameterNames(decl ts.Node, src []byte, into map[string]bool) {
	for i := 0; i < int(decl.ChildCount()); i++ {
		tpl := decl.Child(i)
		if tpl == nil || tpl.Type() != "type_parameter_list" {
			continue
		}
		for j := 0; j < int(tpl.ChildCount()); j++ {
			tp := tpl.Child(j)
			if tp == nil || tp.Type() != "type_parameter" {
				continue
			}
			nm := tp.ChildByFieldName("name")
			if nm == nil {
				continue
			}
			if name := string(src[nm.StartByte():nm.EndByte()]); name != "" {
				into[name] = true
			}
		}
	}
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
