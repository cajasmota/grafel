package php

import (
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// field_type_refs.go — the field→declared-type edge for PHP (issue #6912, arm H).
//
// Shipped predecessors: arm A csharp/field_type_refs.go (#6984), arm B
// proto/field_type_refs.go (#6991), arm C rust/field_type_refs.go (#7000), arm D
// golang/field_type_refs.go (#7036), arm F java/field_type_refs.go (#7039), arm E
// fsharp/field_type_refs.go (#7040).
//
// THE GAP. emitPhpFieldMembers (field_members.go:60) mints one SCOPE.Schema/field
// entity per typed class property and per promoted constructor parameter, and
// records the declared type ONLY as Properties["field_type"] and inside Signature.
// The single edge attached to it is the owner→member CONTAINS, which the orphan
// definition excludes by design, so every PHP field is a LEAF on its outbound
// side. Contributor #6726 measures SCOPE.Schema at 99.3% orphan of 35,353
// entities; PHP fields are minted as SCOPE.Schema, so they are part of that
// population.
//
// ============================================================================
// 1. THE PROPERTY IS LOSSLESS — VERIFIED BY PROBE, NOT BY READING
// ============================================================================
//
// phpDeclaredType (field_members.go:135) returns the VERBATIM source span of the
// first primitive_type | named_type | optional_type | union_type |
// intersection_type | type_list child. Probed through the real extractor on 18
// property shapes, every one arrives character for character as written:
//
//	?Customer   Customer|Money   Customer&Shipper   \Other\Order   ?\Money
//	string|int  self             array              iterable       int
//
// So unlike swift — whose field_type is the FIRST pre-order type_identifier and
// yields `Dictionary` for `Dictionary<String, Order>` — PHP's needs no stash from
// the AST. Arm C's technique is declined because it is unnecessary here, not
// because it is wrong. Pinned by TestPhpFieldTypeRefs_FieldTypeIsVerbatim.
//
// PROMOTED CONSTRUCTOR PARAMETERS ARE COVERED FOR FREE and are asserted rather
// than assumed: emitPhpFieldMembers emits them through the SAME `add` closure
// with the same Properties, so `public function __construct(public readonly
// Customer $owner)` is one more SCOPE.Schema/field with field_type="Customer".
// Graded by the promoted rows of TestPhpFieldTypeRefs_ShapeSpace.
//
// ============================================================================
// 2. THE RESOLVER TIER THAT RESOLVES THIS ADDRESS — DRIVEN, NOT INFERRED
// ============================================================================
//
// The ToID is extractor.BuildComponentStructuralRef, i.e.
// `scope:component:class:php:<file>:<Name>`. resolveStructuralRef
// (internal/resolve/refs.go:2982) tries, in order:
//
//  1. lookupLocationKind(file, name, structuralKindFamilies("component"))
//     — i.e. componentKindFamily {Component, Class, View, Model, SCOPE.Component,
//     SCOPE.View, SCOPE.Model} (refs.go:2341), plus each entity's SCOPE-trimmed
//     alias (refs.go:1290-1296), which is what makes a bare `Class`-kinded
//     entity a family member too.
//  2. ambigLocation[file][name] (refs.go:3004) — set when TWO DISTINCT ENTITY
//     IDS share (file, name), REGARDLESS OF KIND (refs.go:1302-1315).
//  3. byLocation[file][name] — kind-agnostic.
//
// Every target this pass admits is a SCOPE.Component, so every edge it emits is
// answered at TIER 1 and never reaches tiers 2/3. That is the whole basis of the
// ambiguity rule below, and it was established by driving the real resolver
// (BuildIndex → ReferencesEmbedded) over records produced by the real extractor,
// not by reading refs.go. TestPhpFieldTypeRefs_ResolvesToEntityIDs is that drive.
//
// ============================================================================
// 3. WHY PHP ENUMS ARE NOT TARGETS — A MEASUREMENT, NOT A PREFERENCE
// ============================================================================
//
// buildEnum (php.go:384) mints `enum Status: string { case A = 'a'; }` as
// SCOPE.Schema/enum named "Status", and emitConstValueSets → buildEnumValueSet
// (const_valueset.go:181) ADDITIONALLY mints a SCOPE.Enum value-set with the
// SAME Name in the SAME file. Two kinds, two graph nodes (graph.EntityID hashes
// (repo, Kind, Name, SourceFile)), and NEITHER is in componentKindFamily.
//
// A component-space ref to such a name therefore misses tier 1, and hits tier 2
// as AMBIGUOUS. Driven through the real resolver:
//
//	enum Status: string { case A = 'a'; }   ->  DANGLE (statusAmbiguous)
//	enum Empty2 {}   (no cases, no value-set) ->  binds
//
// So admitting enums would emit a dangling stub for every enum that has at least
// one case — i.e. for every enum anybody writes. A non-binding edge is WORSE than
// no edge: it is kept verbatim and classified `bug-extractor`, a full hit on the
// disposition denominator. Enums are refused, and the refusal is graded in both
// directions by TestPhpFieldTypeRefs_EnumIsNotATarget (the enum field gets no
// edge) with a same-fixture positive control (a class field in the same class
// does get one), so the test cannot pass by the pass being broken.
//
// THIS IS A RECALL LOSS AND IT IS NAMED AS ONE. `public Status $status` is
// ordinary PHP and stays orphan. Closing it needs a SECOND ADDRESS DIALECT — a
// schema-space ref, which would hit lookupLocationKind with schemaKindFamily
// {Schema, Field, Property, SCOPE.Schema}, where the SCOPE.Enum value-set is NOT
// a member and the SCOPE.Schema/enum resolves uniquely. That is a real option
// with a measured basis, and it is deliberately NOT taken here: shipping two
// address dialects behind one ref_kind is #6451's defect shape, and it should be
// decided across arms rather than invented by this one.
//
// ============================================================================
// 4. THE AMBIGUITY RULE — DERIVED FROM THE TIER, NOT COPIED FROM AN ARM
// ============================================================================
//
// The standing rule on #6912 is: COUNT THE KINDS THAT THE RESOLVER TIER WHICH
// RESOLVES YOUR ADDRESS ACTUALLY WEIGHS. Per section 2 that tier is
// lookupLocationKind filtered to componentKindFamily, so this pass counts kinds
// WITHIN THAT FAMILY — arm F's scope, reached from php's own tier rather than
// borrowed. The UNIT is the KIND and not the RECORD, mirroring refs.go:1308:
// within one file two records sharing a Kind are ONE node, so counting records
// would refuse edges that bind perfectly well. Arm C counts records and is wrong
// under the rule (#7038); its rule is not copied and neither is its stated
// reason.
//
// AND THE HONEST PART: SCOPE.Component IS THE ONLY componentKindFamily KIND ANY
// PHP PRODUCER EMITS. Core (internal/extractors/php) and custom
// (internal/custom/php) were both enumerated: neither emits SCOPE.Model,
// SCOPE.View, SCOPE.Class, nor a bare Component/Class/View/Model. So
// len(nameKinds[name]) can never exceed 1 from PHP source today and rule 2 is
// VACUOUS AS SHIPPED. It is kept as a mirror of the resolver for the producer
// that adds an in-family kind later — exactly the shape arm F's SCOPE.Class hole
// was — and because it is unreachable from Extract it is graded by a direct-call
// unit test (TestPhpFieldTypeRefs_Unit_InFamilyRivalSuppresses...) rather than
// claimed to be covered by the fixture suite.
//
// The counterfactual of ARM D's rule (count ALL same-file kinds) is not vacuous
// here, and it is the reason this arm does not inherit it: PHP has separate
// symbol tables for classes and functions, so `class Money {}` beside
// `function Money() {}` in one file is LEGAL PHP and puts a SCOPE.Operation
// beside the SCOPE.Component. Arm D's rule refuses that target; tier 1 binds it
// without hesitation, because SCOPE.Operation is not in the family it weighs.
// Graded by TestPhpFieldTypeRefs_SameNamedFunctionDoesNotSuppressTheClass, whose
// edge is additionally driven through the real resolver.
//
// ============================================================================
// 5. THE TARGET ALLOW-LIST — THE LIVE WRONG-BINDING KILL
// ============================================================================
//
// buildUseImports/useImportRecord (php.go:622-660) mints ONE SCOPE.Component per
// `use` statement whose Name is the TOP namespace segment, and buildNamespace
// (php.go:483) does the same for the file's own `namespace`. Both carry an EMPTY
// Subtype. So `use Ship\Thing;` puts a Component literally named `Ship` in this
// file — and a component-space ref to `Ship` BINDS TO IT (driven, section 2's
// harness). Without the subtype allow-list a field typed `Ship` in that file
// binds to an IMPORT PLACEHOLDER: a confident wrong answer rather than a dangle,
// and a wrong binding is the direction `bug-extractor` cannot flag. Graded by
// TestPhpFieldTypeRefs_ImportPlaceholderIsNeverATarget, whose file declares NO
// class of that name so rule 2 cannot mask the allow-list.
//
// SCOPE.Component/file (extractor.FileEntity) is refused by the same list. It is
// unreachable by name shape — its Name is the file path and a candidate token
// can never contain `/` or `.` — and that is claimed as belt, not as the reason.
//
// ============================================================================
// 6. NO PRIMITIVE BLOCKLIST
// ============================================================================
//
// `int`, `string`, `array`, `callable`, `iterable`, `object`, `mixed`, `never`,
// `void`, `false`, `null`, `true`, `float`, `bool`, `self`, `static`, `parent`
// are collected as candidates like any other identifier and refused by the
// DECLARATION GATE: the file does not declare a type of that name. No blocklist
// exists, and none is needed — PHP reserves every one of those words as a class
// name (`class int {}` is a fatal error), so unlike F#'s shadowable type
// abbreviations there is no legal same-file declaration a blocklist would have to
// out-rank. Graded by the primitive rows of TestPhpFieldTypeRefs_ShapeSpace.
//
// `self`/`static`/`parent` additionally name the OWNER, which is refused a second
// time by the self-reference rule in attachPhpFieldTypeRefs.
//
// ============================================================================
// 7. SAME-FILE TARGETS ONLY
// ============================================================================
//
// A field whose type is declared in another .php file gets NOTHING. Binding a
// bare type name across files is exactly the #6976/#6369 hazard, and PHP makes it
// sharper than most: `use App\Models\Order;` means the bare `Order` in this file
// denotes a class in a DIFFERENT file, and the same bare name is declared in
// dozens of files across a Symfony/Laravel tree. Closing it needs the cross-file
// namespace view pass 1 has none of. Separate arm, not a widening of this one.
//
// There are TWO independent same-file conjuncts and they fail in OPPOSITE
// directions, so they are graded separately rather than one paying for the other:
// the rival scan in phpInFileTypeTargets (a cross-file record wrongly SUPPRESSING
// an edge) and the target scan (a cross-file record wrongly BECOMING a target).
// TestPhpFieldTypeRefs_Unit_TargetsAreScopedToTheRequestedFile covers both.

// phpFieldTargetRefKind is the value of the `ref_kind` edge property. It matches
// arm A's csFieldTargetRefKind, arm B's protoFieldTargetRefKind, arm C's
// rustFieldTargetRefKind, arm D's goFieldTargetRefKind, arm F's
// javaFieldTargetRefKind and arm E's fsharpFieldTargetRefKind verbatim — the
// discriminator is what makes the edge queryable as a field→declared-type edge
// across languages, so it must be one string. Arm D established that the six
// independent literals are deliberate: six graders enforcing agreement
// transitively beat one shared constant.
const phpFieldTargetRefKind = "field_target_type"

// phpFieldTypeCandidates returns every UNQUALIFIED identifier written in a PHP
// type expression, in source order and without deduplication.
//
// It is a scanner, not a parser. It models neither `?` nor `|` nor `&` nor
// `(A&B)|null` DNF grouping, and that is the point: every named type in a PHP
// type expression appears as a bare identifier whatever composes them, so there
// is no type-syntax table to keep in sync. A UNION names several types and EACH
// gets its own edge (`Customer|Money` → two), because each alternative genuinely
// is a declared type the field may hold; an INTERSECTION likewise (`A&B` — the
// value is both). Nullability contributes nothing: `?Customer` names exactly the
// one type `Customer`.
//
// ONE token shape is consumed WHOLE and discarded, and consuming it WHOLE is what
// makes it safe — a scanner that merely SKIPPED the backslash would re-enter
// mid-token and produce exactly the bare name the skip exists to prevent:
//
//   - A NAMESPACE-QUALIFIED name, in either spelling: relative (`Other\Order`) or
//     fully-qualified with a leading separator (`\Other\Order`, `\Money`). `\` is
//     scanned as an identifier continuation character, AND a token may START with
//     it, so the whole qualified name arrives as one token and is rejected as one.
//
// Reducing `Other\Order` to `Order` is precisely the failure arm D found in Go's
// resolveTypeReferences, where `other.Order` bound to a same-file `Order` — a
// wrong binding, which never reaches `bug-extractor` because it binds. A LEADING
// separator is refused for a second and independent reason: in a file that
// declares `namespace App;`, the bare `Money` means `App\Money` while `\Money`
// means the GLOBAL `Money`, so they are different types and binding one to the
// other would be wrong in the namespaced case. It is conservative in the
// namespace-less case (where the two coincide) and that is the direction to be
// conservative in. Both spellings are graded by the qualified rows of
// TestPhpFieldTypeRefs_ShapeSpace and by
// TestPhpFieldTypeRefs_QualifiedTypeIsNotStrippedToASameFileName.
//
// A field type is at most a few dozen characters, so the scan is linear in the
// property length with no allocation beyond the result slice.
func phpFieldTypeCandidates(typ string) []string {
	var out []string
	r := []rune(typ)
	i := 0
	for i < len(r) {
		switch {
		case r[i] == '\\':
			// Fully-qualified name — consume the separator AND every segment
			// after it, so no segment can re-enter the scanner on its own.
			i++
			for i < len(r) && (isPhpIdentRune(r[i]) || r[i] == '\\') {
				i++
			}
		case isPhpIdentStartRune(r[i]):
			j := i
			for j < len(r) && (isPhpIdentRune(r[j]) || r[j] == '\\') {
				j++
			}
			if tok := string(r[i:j]); !strings.ContainsRune(tok, '\\') {
				out = append(out, tok)
			}
			i = j
		default:
			i++
		}
	}
	return out
}

// isPhpIdentStartRune / isPhpIdentRune implement PHP's identifier grammar,
// `[a-zA-Z_\x80-\xff][a-zA-Z0-9_\x80-\xff]*`. The high-byte range is why
// non-ASCII letters are admitted rather than treated as separators: `class Ünité`
// is a legal PHP class and a field typed `Ünité` must produce the same edge as an
// ASCII one.
func isPhpIdentStartRune(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r >= 0x80
}

func isPhpIdentRune(r rune) bool {
	return isPhpIdentStartRune(r) || (r >= '0' && r <= '9')
}

// phpFieldTypeTarget is one in-file type declaration a field-type edge may
// address: the structural ToID that binds to it, and the DECLARED spelling of the
// name for the `target_type` property and the self-reference check.
type phpFieldTypeTarget struct {
	toID string
	name string
}

// phpTypeDeclSubtypes is the target ALLOW-LIST over SCOPE.Component subtypes.
//
// It is an allow-list rather than a denylist because PHP emits several BARE-NAMED
// SCOPE.Components that are not type declarations (section 5), and a future
// producer adding another one must be refused BY DEFAULT. Enumerated from this
// package's own emit sites rather than inherited from an arm:
//
//	ADMITTED — every subtype buildComponent (php.go:304) is ever called with:
//	  class      php.go:127
//	  interface  php.go:181
//	  trait      php.go:214
//
//	REFUSED:
//	  ""     — buildNamespace (php.go:483) and useImportRecord (php.go:650).
//	           THE LIVE KILL; see section 5.
//	  "file" — extractor.FileEntity, the #577 file carrier.
//
// A PHP `trait` is admitted deliberately. It cannot be used as a type in a
// declaration (`public SomeTrait $x` is not meaningful PHP), so the row buys no
// edge on its own — but a trait IS a nameable SCOPE.Component this package emits
// for a real declaration, and leaving it out would make the allow-list a list of
// "things that happen to appear in type position" rather than a list of type
// declarations. It is named here so its emptiness is a recorded decision rather
// than an oversight.
var phpTypeDeclSubtypes = map[string]bool{
	"class":     true,
	"interface": true,
	"trait":     true,
}

// phpComponentAddressFamily is the set of entity Kinds that can make a
// `scope:component:…` structural ref ambiguous — every Kind that
// internal/resolve's lookupLocationKind weighs when resolving one.
//
// It is componentKindFamily (internal/resolve/refs.go:2341) CLOSED UNDER THE
// INDEX'S TRIM ALIAS, and the closure is load-bearing rather than pedantic:
// BuildIndex writes each entity under its raw Kind AND its SCOPE-trimmed alias
// (refs.go:1290-1296), so an entity kinded `SCOPE.Class` is keyed under both
// "SCOPE.Class" and "Class" — and "Class" IS in the family even though
// "SCOPE.Class" itself is not. The membership test is therefore
// `K ∈ family || TrimPrefix(K, "SCOPE.") ∈ family`, and these eight entries are
// exactly that set. Arm F shipped this table with "SCOPE.Class" missing in a
// first revision, its own comment claiming to carry both spellings; that hole is
// not re-introduced here.
//
// It is duplicated rather than exported from internal/resolve because the
// dependency must not run extractor → resolver, and because an independently
// written literal is a stronger grader than a shared constant. The invariant to
// preserve: a Kind ABSENT from this set cannot make a component-space ref
// ambiguous, so adding a Kind to componentKindFamily upstream — or adding one
// whose trimmed alias lands in it — without adding it here would let this pass
// emit a stub the resolver refuses.
var phpComponentAddressFamily = map[string]bool{
	"Component": true, "Class": true, "View": true, "Model": true,
	"SCOPE.Component": true, "SCOPE.Class": true,
	"SCOPE.View": true, "SCOPE.Model": true,
}

// phpInFileTypeTargets indexes every TYPE DECLARED in this file that a field-type
// edge may address, keyed by the LOWER-CASED bare name.
//
// THE KEY IS FOLDED BECAUSE PHP CLASS NAMES ARE CASE-INSENSITIVE. `public money
// $m` and `public Money $m` denote the same `class Money`, and both are legal
// PHP. The stored `name` keeps the DECLARED spelling, so the emitted ToID always
// carries the spelling the entity was minted under and binds — folding the lookup
// without folding the address would emit `…:money` and dangle. Graded by
// TestPhpFieldTypeRefs_TypeNameMatchIsCaseInsensitive, which asserts the BOUND
// endpoint and not merely the edge count.
//
// Two DIFFERENT declared spellings that fold together (`class Money` +
// `interface MONEY`) would be a fatal redeclaration error in PHP, so the shape is
// not reachable from a file PHP will load — but this pass is handed records, not
// a running interpreter, so the collision is refused explicitly rather than
// assumed away.
//
// Two rules, both argued in the header block:
//
//  1. The record must be a SCOPE.Component this package emits for a real type
//     declaration (phpTypeDeclSubtypes). This is a check on the EMITTED RECORD,
//     not on the AST, so a declaration the extractor chose not to emit can never
//     be addressed.
//
//  2. A name carried by MORE THAN ONE DISTINCT KIND IN THE COMPONENT ADDRESS
//     FAMILY is dropped. Restricted to that family — not to every same-file kind
//     — because a non-family kind never enters the tier that resolves this
//     address (section 4). VACUOUS AS SHIPPED, kept as a resolver mirror, graded
//     by direct call.
func phpInFileTypeTargets(records []types.EntityRecord, filePath string) map[string]phpFieldTypeTarget {
	// Pass 1 — how many DISTINCT COMPONENT-FAMILY KINDS each same-file name
	// carries, target-eligible or not.
	nameKinds := make(map[string]map[string]bool)
	for i := range records {
		r := &records[i]
		if r.SourceFile != filePath || r.Name == "" || !phpComponentAddressFamily[r.Kind] {
			continue
		}
		if nameKinds[r.Name] == nil {
			nameKinds[r.Name] = make(map[string]bool)
		}
		nameKinds[r.Name][r.Kind] = true
	}

	// Pass 2 — admit the type declarations whose name is unambiguous.
	targets := make(map[string]phpFieldTypeTarget)
	folded := make(map[string]string) // fold key -> declared spelling
	for i := range records {
		r := &records[i]
		if r.SourceFile != filePath || r.Name == "" || r.Kind != "SCOPE.Component" {
			continue
		}
		if !phpTypeDeclSubtypes[r.Subtype] {
			continue
		}
		if len(nameKinds[r.Name]) > 1 {
			continue
		}
		key := strings.ToLower(r.Name)
		if prev, ok := folded[key]; ok && prev != r.Name {
			// Two different spellings folding together — refuse both rather
			// than pick one.
			delete(targets, key)
			continue
		}
		folded[key] = r.Name
		targets[key] = phpFieldTypeTarget{
			toID: extractor.BuildComponentStructuralRef("php", filePath, r.Name),
			name: r.Name,
		}
	}
	return targets
}

// attachPhpFieldTypeRefs appends one REFERENCES edge per (field, in-file declared
// type) pair, reading the declared type from the field record's own `field_type`
// property.
//
// It covers class properties AND promoted constructor parameters because
// emitPhpFieldMembers emits both as the same SCOPE.Schema/field shape.
//
// FromID is left empty so graph assembly anchors the edge on the field record
// that carries it — the Django precedent at
// internal/extractors/python/django_relational.go:246-252, and arms B–F.
//
// Relationships is APPENDED to, never assigned: the field record carries none
// today, but a later producer's edge must not be clobbered. (The owner→member
// CONTAINS lives on the OWNER's record, not here.)
//
// CALL PLACEMENT. It runs at the end of Extract, on the fully assembled record
// slice. Placement is NOT load-bearing today and is not claimed to be: every pass
// that runs after walk() emits either an out-of-family kind (SCOPE.Enum,
// SCOPE.Operation, SCOPE.Config, SCOPE.Template, SCOPE.TranslationKey) or a
// PREFIXED / different-file name (`exception:X` on the synthetic `<exception>`
// path), so none can become a target or change a family kind count. What the
// placement DOES buy is that the pass sees the complete record set, which is the
// invariant to preserve if a future pass starts minting bare-named Components.
func attachPhpFieldTypeRefs(records []types.EntityRecord, filePath string) []types.EntityRecord {
	targets := phpInFileTypeTargets(records, filePath)
	if len(targets) == 0 {
		return records
	}
	for i := range records {
		r := &records[i]
		if r.Kind != "SCOPE.Schema" || r.Subtype != "field" || r.SourceFile != filePath {
			continue
		}
		declared := r.Properties["field_type"]
		if declared == "" {
			continue
		}
		owner := r.Properties["parent_class"]
		fieldName := r.Properties["field_name"]
		emitted := make(map[string]bool)
		for _, cand := range phpFieldTypeCandidates(declared) {
			t, ok := targets[strings.ToLower(cand)]
			if !ok || emitted[t.toID] {
				continue
			}
			// A field never targets its own declaring class: `class Node {
			// public ?Node $next; }` is a self-reference the CONTAINS edge
			// already relates, and the recursive-type edge asserts nothing new.
			// Also what makes `self`/`static` a no-op even in a file whose owner
			// somehow carried one of those names.
			if owner != "" && strings.EqualFold(t.name, owner) {
				continue
			}
			emitted[t.toID] = true
			r.Relationships = append(r.Relationships, types.RelationshipRecord{
				ToID: t.toID,
				Kind: string(types.RelationshipKindReferences),
				Properties: types.Props{
					{K: "field_name", V: fieldName},
					{K: "ref_kind", V: phpFieldTargetRefKind},
					{K: "target_type", V: t.name},
				},
			})
		}
	}
	return records
}
