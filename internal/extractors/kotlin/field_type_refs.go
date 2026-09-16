package kotlin

import (
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	"github.com/cajasmota/grafel/internal/types"
)

// field_type_refs.go — the field→declared-type edge for Kotlin (issue #6912).
// Arm A is internal/extractors/csharp (#6984), B proto (#6991), C rust (#7000),
// D golang (#7036), E fsharp (#7040), F java (#7039), G swift (#7043),
// H php (#7042).
//
// THE GAP, AND WHY KOTLIN IS TWO STEPS RATHER THAN ONE. Every shipped arm reads
// a declared type its extractor already recorded — a `field_type` property, a
// `Signature`, or a live local at the emit site. Kotlin recorded NOTHING:
// buildProperty (kotlin.go) and emitPrimaryConstructorFields build their
// SCOPE.Schema/field records with no Properties map, no Signature and no type
// datum of any kind. Verified by probe, not inherited: the issue asserted the
// same shape about JAVA and it was false (arm F found the type already a local
// there), so the absence was re-established here before anything was built.
//
// So this arm CAPTURES the declared type at the emit site and BINDS it in one
// change. The capture writes to Metadata SCRATCH that attachKotlinFieldTypeRefs
// deletes, not to an entity property — the graph gains edges and nothing else.
// A capture that shipped alone would be one more unhashed property that nothing
// reads, which is the state #6912 exists to fix.
//
// WHERE THE TYPE LIVES IN THE CST, probed against the real grammar. The two
// Kotlin field anchors put it in two different places, and one of them has a
// sibling that must NOT be read:
//
//	class_parameter        [binding_pattern_kind] [simple_identifier] [:] TYPE [= default]
//	property_declaration   [binding_pattern_kind] [user_type RECEIVER] [.]
//	                       [variable_declaration [simple_identifier] [:] TYPE] [getter]
//
// The RECEIVER of an extension property (`val Recv.p: Order`) is a sibling of
// variable_declaration, OUTSIDE it. A capture that walked the whole
// property_declaration would emit `p -> Recv`: right name, right file,
// admissible kind and subtype, and wrong — the not-a-declaration shape that
// cost arm G half its edges (#7047) and arm cpp 16 of 39 (#7057). Scoping the
// capture to the variable_declaration's post-colon children excludes it
// structurally rather than by a blocklist. Likewise the class_parameter scan
// STOPS at `=`, so a default value's own type arguments (`= build<Line>()`)
// never masquerade as the declared type.
//
// NOT-A-DECLARATION AUDIT, done explicitly because a 100% bind rate is not
// evidence — the target only has to be a real DECLARATION, and every instrument
// on this issue is blind to that dimension:
//
//   - companion object — mints NO entity at all (walk has no companion_object
//     case, and buildComponent needs a type_identifier the node does not give
//     it), so `Companion` and a named companion are unreachable as targets.
//     Pinned by TestKotlinFieldTypeRefs_CompanionObjectIsNeverATarget, which
//     asserts the no-entity premise so it cannot pass vacuously.
//   - an `import` carrier — a SCOPE.Component named after the import path.
//     The ordinary shape keeps the FULL dotted path, which a bare
//     type_identifier cannot equal; but a ROOT-PACKAGE import (`import Order`,
//     legal Kotlin) is a BARE-named Component and is exactly Go's import
//     placeholder. Refused by the subtype allow-list, graded from source.
//   - the #577 file carrier — refused by subtype; Name is the file path.
//   - `expect` / `actual` / `external` class — these ARE declarations (of a
//     type in the common source set, of an actual, of an externally implemented
//     type), the extractor routes them through the ordinary class_declaration
//     arm, and a field typed by one names that declaration. ADMITTED, and said
//     out loud rather than left implicit.
//   - `object X` vs `class X` — an object declaration DOES declare a type
//     (`val r: Registry` is legal and names it). ADMITTED.
//   - an extension property's receiver — excluded structurally, above.
//   - a supertype list (`class Sub : Base()`) — mints no entity; Kotlin has no
//     extension-carrier construct at all, which is the single reason this arm
//     does not need arm G's Metadata marker.
//
// TWO FORMS THAT DECLARE A TYPE AND ARE UNREACHABLE AS TARGETS ANYWAY, named
// here so this audit is not presented as exhaustive while omitting them. Both
// are pre-existing extractor ceilings, neither is introduced or widened here,
// and both are RECALL floors — a missing edge, never a wrong one:
//
//   - `fun interface Cb` — mints NO entity for the interface itself. The walk
//     has no case for the node, so only its MEMBERS surface, and they surface
//     BARE (`go`, not `Cb.go`) because no parentType is in scope for them.
//     `val c: Cb` therefore produces no edge. PROBED, not taken on report: the
//     review described this form as a SCOPE.Operation named `Cb` and hence a
//     collider for a same-named typealias, and neither half is what the
//     extractor does — nothing named `Cb` is emitted at all, so it cannot
//     collide with anything.
//   - a LOCAL class (`fun f() { class Local(val o: Order) }`) — mints no
//     entity, so it is neither a target nor a source: its `val o` is not a
//     field record at all, and the type parameters of the enclosing generic
//     function are therefore never a shadow question.
//
// Both are graded by TestKotlinFieldTypeRefs_UnmintedDeclarationForms, which
// asserts the no-entity / wrong-kind premise rather than only the absent edge.
//
// THE SOURCE ENDPOINT, audited too (arm cpp found 68 of 254 "fields" were
// member FUNCTIONS with the return type captured as field_type). Kotlin's field
// records come from exactly two producers, both genuine property declarations:
// a class/object/interface body `property_declaration` and a primary-constructor
// `class_parameter` carrying a binding_pattern_kind. A function is a
// function_declaration and becomes SCOPE.Operation — never a field. The three
// Kotlin shapes worth naming are all genuine properties with a genuine declared type: a
// getter-only property (`val x: Order get() = …`), a delegated property
// (`val x: Order by lazy { … }`) and a function-typed property
// (`val cb: (Order) -> Unit`). All three are graded in
// TestKotlinFieldTypeRefs_AnchorShapeSpace. A non-`val` constructor parameter
// is not a property and the extractor mints no field for it; asserted there.
//
// THE AMBIGUITY RULE — derived from the tier that resolves THIS address, not
// copied. Arm C counts records (wrong, #7038), arm D counts every kind, arm F
// counts within the component family, arm G needed BOTH because it had two
// target kinds. Kotlin has two target kinds too, so it needs both — but for
// Kotlin's own reasons, and the split is measured rather than assumed:
//
//	SCOPE.Component target → Index.lookupStructural → lookupLocationKind
//	  filtered to componentKindFamily (refs.go) runs FIRST and returns; only
//	  a rival kind INSIDE that family can blank the match. Count family kinds.
//	SCOPE.Schema/type_alias target → invisible to that filter, so the ref falls
//	  through to ambigLocation, which is kind-AGNOSTIC. Count every kind.
//
// Kotlin has THREE same-file, same-name rivals OUTSIDE the family, which is
// what makes arm D's rule expensive here and is peculiar to this language:
//
//	enum class Status   → SCOPE.Component/enum AND a SCOPE.Enum value-set
//	object Pages {…}    → SCOPE.Component/object AND a SCOPE.Enum const-group
//	                      value-set NAMED AFTER THE OBJECT (const_valueset.go)
//	@Service class X    → SCOPE.Component/class AND a SCOPE.Service named X
//	                      (SCOPE.Service is deliberately outside the family)
//
// The unit of the count is the KIND, not the RECORD: graph.EntityID hashes
// (repo, Kind, Name, SourceFile) with Subtype EXCLUDED, so two same-file records
// sharing a Kind are ONE graph node and one ToID. Counting records would delete
// an edge that binds perfectly well — arm C's defect.
//
// A NON-BINDING EDGE IS WORSE THAN NO EDGE, so this pass emits only for a type
// DECLARED IN THIS FILE and refuses everything else outright. Cross-file targets
// need the resolver, not the extractor (#6976/#6369), and are a separate arm.

// kotlinFieldTypeRefsMetaKey is the per-field capture written at the two emit
// sites and consumed — and deleted — by attachKotlinFieldTypeRefs.
//
// The candidates cannot become edges at the emit site: `class Order { val c:
// Customer }` may precede `class Customer` in the same file, so the in-file
// declaration set is complete only once walk has finished.
const kotlinFieldTypeRefsMetaKey = "field_type_refs"

// kotlinFieldTypeRefsOwnerKey stashes the field's declaring type name alongside
// the candidates, so the `field_name` property can carry the LEAF name. It is
// not re-derived by splitting the entity Name on '.', because a leaf name may
// itself contain no owner (a nested declaration's members are emitted bare).
const kotlinFieldTypeRefsOwnerKey = "field_type_refs_owner"

// kotlinFieldTargetRefKind is the value of the `ref_kind` edge property. It
// matches arms A-H verbatim — the discriminator is what makes the edge queryable
// as a field→declared-type edge across languages, so it must be one string.
// There is deliberately no shared constant: arm D scored that as a mutant (CZ-2)
// and found the independent literals grade the agreement transitively.
const kotlinFieldTargetRefKind = "field_target_type"

// kotlinComponentAddressFamily is the set of entity Kinds that can make a
// `scope:component:…` structural ref ambiguous — every Kind internal/resolve's
// lookupLocationKind weighs when resolving one.
//
// It is componentKindFamily (refs.go: Component/Class/View/Model plus their
// SCOPE.* spellings) CLOSED UNDER THE INDEX'S TRIM ALIAS. BuildIndex writes each
// entity under its raw Kind AND its SCOPE-trimmed alias, so an entity kinded
// `SCOPE.Class` is keyed under "Class" too — and "Class" IS in the family even
// though "SCOPE.Class" is not. Membership is therefore
// `K ∈ family || trim(K) ∈ family`, and the eight entries below are exactly that
// set. Duplicated here rather than imported so the dependency does not run
// extractor → resolver. Invariant: a Kind ABSENT from this set cannot make a
// component-space ref ambiguous, so widening componentKindFamily upstream
// without widening this would let the pass emit a stub that dangles.
//
// EVERY ENTRY IS GRADED, through the call site rather than by reading this map:
// Unit_EveryAddressFamilyEntryMakesARefAmbiguous drives one rival record per
// entry and asserts the target is blanked, with a non-family rival as the
// table's positive control. Three of them — the BARE Component / View / Model
// spellings — were ungraded until the review found dropping all three left the
// suite green (MR-5); no Kotlin producer emits a bare-kinded record, so no
// source fixture can reach them. What is still NOT checked anywhere is that
// this duplicate and resolve's componentKindFamily stay in sync; that is the
// cost of the deliberate non-import above, and it is a claim about upstream,
// not about this file.
var kotlinComponentAddressFamily = map[string]bool{
	"Component": true, "Class": true, "View": true, "Model": true,
	"SCOPE.Component": true, "SCOPE.Class": true,
	"SCOPE.View": true, "SCOPE.Model": true,
}

// kotlinFieldTypeCandidates returns every bare type name written in a declared
// type expression, in source order and without deduplication.
//
// Candidates are taken ONLY from a `user_type` node, and only when that node is
// not DOTTED. That single rule covers the whole shape space probed against the
// grammar:
//
//	Order                → user_type[type_identifier]                   → Order
//	Order?               → nullable_type[user_type]                     → Order
//	List<Order>          → user_type[tid List, type_arguments[…Order…]] → List, Order
//	Array<out Order>     → …type_projection[variance, user_type]        → Array, Order
//	(Order) -> Unit      → function_type[function_type_parameters[…]]   → Order, Unit
//	List<*>              → type_projection holds no user_type           → List
//	com.acme.Order       → ONE user_type with THREE type_identifier
//	                       children separated by "." — DOTTED, so no
//	                       segment is taken                             → (none)
//	Outer.Inner<Order>   → dotted user_type, but its type_arguments are
//	                       a SIBLING of the identifiers and still
//	                       descended                                    → Order
//	Order.() -> Unit     → the receiver is a bare type_identifier child
//	                       of function_type, not a user_type            → Unit
//
// WHY DOTTED IS SKIPPED WHOLE RATHER THAN REDUCED TO ITS LAST SEGMENT. Arm D
// found Go's resolveTypeReferences stripping `other.Order` to `Order` and
// binding it to a same-file `Order` — a silently wrong binding, and a bound edge
// never reaches bug-extractor. `Outer.Inner` is worse still: both segments are
// plausible same-file names, so any reduction guesses between two wrong answers.
//
// WHY THE FUNCTION-TYPE RECEIVER IS NOT TAKEN. `Order.() -> Unit` puts the
// receiver as a bare `type_identifier`, in the same position a dotted qualifier
// occupies, and `Outer.Inner.() -> Unit` makes the two indistinguishable without
// a second rule. It is a recall floor, stated as one, not a claim that the
// receiver names nothing.
//
// TYPE PARAMETERS ARE EXCLUDED BY NAME. `class Holder<T>(val item: T)` yields
// candidate `T`, and a file that also declares `class T` would get a wrong edge
// — the over-fire arm F had to pin as KNOWN-WRONG because Java's grammar gives a
// type parameter and a type reference the same node. Kotlin's does too, but the
// `type_parameters` lists are reachable from the emit site, so the shadow is
// refused instead of documented.
//
// The set refused is the one VISIBLE at the declaration, not the one it
// declares: an `inner class` captures its enclosing class's parameters and
// declares none of its own, so reading the immediate list alone emitted a wrong
// edge that BOUND (review finding F1). kotlinVisibleTypeParameterNames walks
// the enclosing-declaration chain and states exactly where it stops, because
// stopping too late deletes a correct edge instead.
func kotlinFieldTypeCandidates(typeNode ts.Node, src []byte, typeParams map[string]bool) []string {
	var out []string
	var walkType func(n ts.Node)
	walkType = func(n ts.Node) {
		if n == nil {
			return
		}
		if n.Type() == "user_type" {
			dotted := false
			for i := 0; i < int(n.ChildCount()); i++ {
				if n.Child(i).Type() == "." {
					dotted = true
					break
				}
			}
			for i := 0; i < int(n.ChildCount()); i++ {
				ch := n.Child(i)
				switch ch.Type() {
				case "type_identifier":
					if dotted {
						continue
					}
					name := string(src[ch.StartByte():ch.EndByte()])
					if name != "" && !typeParams[name] {
						out = append(out, name)
					}
				case ".":
				default:
					walkType(ch)
				}
			}
			return
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walkType(n.Child(i))
		}
	}
	walkType(typeNode)
	return out
}

// kotlinVisibleTypeParameterNames returns every type-parameter name in scope
// for the fields of `declNode` — its OWN list, plus the lists it CAPTURES.
//
// The immediate list is not the whole scope. Kotlin has exactly one nested form
// that captures an enclosing declaration's type parameters, and it is spelled:
//
//	class Cache<Key> { inner class Entry { val k: Key } }  // Key is Cache's
//
// `Entry` declares nothing, so reading only the immediate `type_parameters`
// gives an EMPTY shadow set and `k` binds to a same-file `class Key` — a wrong
// edge that BINDS, which is invisible to every instrument on this issue (a bound
// wrong edge never reaches bug-extractor; #7056). That was review finding F1.
//
// THE CHAIN STOPS EXACTLY WHERE KOTLIN'S SCOPING DOES, because over-refusing
// deletes a CORRECT edge and a missing edge has no symptom at all — arm cpp's
// equivalent refusal was wrong in both directions and only enumeration found it
// (#7057). So the walk ascends only while the current declaration is `inner`,
// and only to the nearest enclosing DECLARATION of any sort:
//
//	inner class in a generic class      → captures            (ascend)
//	inner class in an inner class       → captures both levels (ascend twice)
//	plain nested class                  → captures NOTHING; `T` there really
//	                                      does name a same-file `class T`
//	inner class whose enclosing decl is
//	  an `object` / `companion object`  → captures NOTHING; an object holds no
//	                                      type parameters and captures none.
//	                                      Ascending to the nearest enclosing
//	                                      CLASS rather than the nearest
//	                                      enclosing DECLARATION would jump over
//	                                      it and over-refuse.
//	local class in a generic function   → mints no entity at all, so no field
//	                                      and no edge in either direction; a
//	                                      stated limit, not a rule.
//
// Every one of those forms is graded in both directions, with a real-type
// positive control in each fixture, by TestKotlinFieldTypeRefs_NestingFormSpace.
func kotlinVisibleTypeParameterNames(declNode ts.Node, src []byte) map[string]bool {
	out := kotlinTypeParameterNames(declNode, src)
	for cur := declNode; kotlinDeclarationIsInner(cur); {
		encl := kotlinEnclosingDeclaration(cur)
		// Only a CLASS carries type parameters to capture. Anything else —
		// an object, a companion object, a function — ends the chain.
		if encl == nil || encl.Type() != "class_declaration" {
			return out
		}
		for name := range kotlinTypeParameterNames(encl, src) {
			out[name] = true
		}
		cur = encl
	}
	return out
}

// kotlinDeclarationIsInner reports whether a declaration carries the `inner`
// modifier, read from the CST (`modifiers > class_modifier > inner`) rather
// than by scanning the declaration's source text, which would also match the
// word inside a nested declaration's own header.
func kotlinDeclarationIsInner(declNode ts.Node) bool {
	if declNode == nil {
		return false
	}
	for i := 0; i < int(declNode.ChildCount()); i++ {
		mods := declNode.Child(i)
		if mods.Type() != "modifiers" {
			continue
		}
		for j := 0; j < int(mods.ChildCount()); j++ {
			cm := mods.Child(j)
			if cm.Type() != "class_modifier" {
				continue
			}
			for k := 0; k < int(cm.ChildCount()); k++ {
				if cm.Child(k).Type() == "inner" {
					return true
				}
			}
		}
	}
	return false
}

// kotlinDeclarationBoundaries is the set of CST node types that END a type
// parameter scope. The ascent in kotlinVisibleTypeParameterNames stops at the
// NEAREST of these, and captures only when that nearest one is a class — so an
// `object` or a `companion object` sitting between an inner class and a generic
// class blocks the capture, as Kotlin does.
var kotlinDeclarationBoundaries = map[string]bool{
	"class_declaration":     true,
	"object_declaration":    true,
	"companion_object":      true,
	"function_declaration":  true,
	"anonymous_function":    true,
	"object_literal":        true,
	"secondary_constructor": true,
}

// kotlinEnclosingDeclaration returns the nearest ancestor declaration of
// `node`, or nil at the top level.
func kotlinEnclosingDeclaration(node ts.Node) ts.Node {
	if node == nil {
		return nil
	}
	for p := node.Parent(); p != nil; p = p.Parent() {
		if kotlinDeclarationBoundaries[p.Type()] {
			return p
		}
	}
	return nil
}

// kotlinTypeParameterNames returns the type-parameter names a class/object
// declaration introduces ITSELF, e.g. {"T", "R"} for `class Holder<T, R>`. It
// is the immediate list only; kotlinVisibleTypeParameterNames adds the captured
// ones and is what the emit sites use.
func kotlinTypeParameterNames(declNode ts.Node, src []byte) map[string]bool {
	out := map[string]bool{}
	if declNode == nil {
		return out
	}
	for i := 0; i < int(declNode.ChildCount()); i++ {
		tp := declNode.Child(i)
		if tp.Type() != "type_parameters" {
			continue
		}
		for j := 0; j < int(tp.ChildCount()); j++ {
			p := tp.Child(j)
			if p.Type() != "type_parameter" {
				continue
			}
			for k := 0; k < int(p.ChildCount()); k++ {
				id := p.Child(k)
				if id.Type() == "type_identifier" {
					out[string(src[id.StartByte():id.EndByte()])] = true
				}
			}
		}
	}
	return out
}

// kotlinDeclaredTypeCandidates collects the candidates from the children of
// `holder` that follow a ":" and precede any "=".
//
// Both Kotlin field anchors put the declared type in exactly that window —
// class_parameter directly, property_declaration inside its variable_declaration
// — and the window is what excludes an extension property's RECEIVER (before the
// colon) and a constructor parameter's DEFAULT VALUE (after the equals). Both
// exclusions are structural, and both are graded in their own test.
func kotlinDeclaredTypeCandidates(holder ts.Node, src []byte, typeParams map[string]bool) []string {
	if holder == nil {
		return nil
	}
	var out []string
	sawColon := false
	for i := 0; i < int(holder.ChildCount()); i++ {
		ch := holder.Child(i)
		switch ch.Type() {
		case ":":
			sawColon = true
			continue
		case "=":
			return out
		}
		if sawColon {
			out = append(out, kotlinFieldTypeCandidates(ch, src, typeParams)...)
		}
	}
	return out
}

// stashKotlinFieldTypeRefs records a field entity's declared-type candidates and
// owning type, for attachKotlinFieldTypeRefs to turn into edges once the file's
// full record set exists. A no-op when the declaration names nothing addressable
// (an inferred type, a bare star projection), so a field that can never produce
// an edge carries no metadata at all.
func stashKotlinFieldTypeRefs(rec *types.EntityRecord, cands []string, owner string) {
	if len(cands) == 0 {
		return
	}
	if rec.Metadata == nil {
		rec.Metadata = make(map[string]interface{})
	}
	rec.Metadata[kotlinFieldTypeRefsMetaKey] = cands
	rec.Metadata[kotlinFieldTypeRefsOwnerKey] = owner
}

// kotlinFieldTypeTarget is one in-file type declaration a field can point at:
// the structural ToID that binds to it, and the bare name for `target_type`.
type kotlinFieldTypeTarget struct {
	toID string
	name string
}

// kotlinInFileTypeTargets indexes every type DECLARED IN THIS FILE that a
// field-type edge may address, keyed by bare name, applying the split ambiguity
// rule derived in the header block.
//
// Pass 1 builds two independent collision counts because the two admitted target
// kinds resolve through two different resolver tiers:
//
//	famKinds[name] — distinct Kinds in the COMPONENT ADDRESS FAMILY. Governs a
//	                 SCOPE.Component target, which lookupLocationKind resolves
//	                 before ambigLocation is consulted (arm F's rule).
//	allKinds[name] — distinct Kinds, full stop. Governs a SCOPE.Schema/type_alias
//	                 target, which lookupLocationKind cannot see at all, so it
//	                 falls through to ambigLocation (arm D's rule).
//
// Pass 2's allow-list is written on Kotlin's own emit sites:
//
//	SCOPE.Component + class|data_class|interface|enum|object → admitted
//	SCOPE.Schema    + type_alias                             → admitted
//
// What that excludes, at the strength each claim has:
//
//   - SCOPE.Component/import — REACHABLE and load-bearing. A root-package
//     `import Order` is a bare-named Component and would otherwise be the target
//     a field typed `Order` binds to, which is Go's import-placeholder defect
//     exactly. The ordinary dotted import cannot collide by name shape; both are
//     pinned.
//   - SCOPE.Component/file (#577) — refused by subtype; Name is the file path.
//   - SCOPE.Enum (the value-set beside an enum class, and the const-group
//     value-set named after an object) — refused as a TARGET while remaining
//     invisible to the component-family COUNT. Those are the two halves of the
//     rule and they are graded separately.
//   - SCOPE.Service (the Spring stereotype twin) — same shape, refused as a
//     target, not counted against a Component.
//   - SCOPE.Schema/field — refused by subtype; also unreachable by name shape,
//     since a field Name is dotted. Claimed as nothing more.
//   - SCOPE.Operation — not admissible as a target, but very much reachable as
//     a COLLIDER: a top-level `fun Handler()` beside `typealias Handler` is what
//     makes the alias ambiguous, which is why the alias arm counts all kinds.
func kotlinInFileTypeTargets(records []types.EntityRecord, filePath string) map[string]kotlinFieldTypeTarget {
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
		if kotlinComponentAddressFamily[r.Kind] {
			if famKinds[r.Name] == nil {
				famKinds[r.Name] = make(map[string]bool)
			}
			famKinds[r.Name][r.Kind] = true
		}
	}

	targets := make(map[string]kotlinFieldTypeTarget)
	for i := range records {
		r := &records[i]
		if r.SourceFile != filePath || r.Name == "" {
			continue
		}
		switch {
		case r.Kind == "SCOPE.Component":
			switch r.Subtype {
			case "class", "data_class", "interface", "enum", "object":
			default:
				continue
			}
			// Component tier: only a rival IN THE SAME ADDRESS FAMILY can
			// break uniqueMatchInFamily. A SCOPE.Enum value-set or a
			// SCOPE.Service twin cannot.
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
		targets[r.Name] = kotlinFieldTypeTarget{
			toID: extractor.BuildComponentStructuralRef("kotlin", filePath, r.Name),
			name: r.Name,
		}
	}
	return targets
}

// attachKotlinFieldTypeRefs appends one REFERENCES edge per (field, in-file
// declared type) pair and clears the capture it consumed.
//
// FromID is left empty so graph assembly anchors the edge on the field record
// that carries it — the Django precedent at
// internal/extractors/python/django_relational.go:246-252, and arms B-H.
//
// A SELF-REFERENCE IS EMITTED. `class Node { val next: Node? }` yields
// `Node.next → Node`. Arms D and F suppress the equivalent to stay consistent
// with a struct-anchored declared-type edge they already ship; Kotlin ships
// none, so there is nothing to be consistent with, and CONTAINS runs owner→field
// rather than field→owner, so the self edge restates nothing. Arms A, C and G
// emit it.
//
// Relationships is APPENDED to, never assigned: a Kotlin field record carries no
// outbound edge today, but assigning would silently clobber one added later.
func attachKotlinFieldTypeRefs(records []types.EntityRecord, filePath string) {
	targets := kotlinInFileTypeTargets(records, filePath)
	for i := range records {
		r := &records[i]
		if r.Metadata == nil {
			continue
		}
		cands, _ := r.Metadata[kotlinFieldTypeRefsMetaKey].([]string)
		owner, _ := r.Metadata[kotlinFieldTypeRefsOwnerKey].(string)
		delete(r.Metadata, kotlinFieldTypeRefsMetaKey)
		delete(r.Metadata, kotlinFieldTypeRefsOwnerKey)
		// The capture is the only thing that ever creates a Metadata map on a
		// Kotlin field record, so leaving an EMPTY one behind would put a
		// `metadata: {}` on records that carried no key before this pass
		// existed. Drop it, so a field whose type named nothing addressable is
		// byte-identical to what it was.
		if len(r.Metadata) == 0 {
			r.Metadata = nil
		}
		if len(cands) == 0 || r.Kind != "SCOPE.Schema" || r.Subtype != "field" {
			continue
		}
		fieldName := r.Name
		if owner != "" {
			fieldName = strings.TrimPrefix(r.Name, owner+".")
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
					{K: "field_name", V: fieldName},
					{K: "ref_kind", V: kotlinFieldTargetRefKind},
					{K: "target_type", V: t.name},
				},
			})
		}
	}
}
