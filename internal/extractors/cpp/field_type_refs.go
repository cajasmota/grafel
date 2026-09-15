package cpp

import (
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// field_type_refs.go — the field→declared-type edge for C and C++ (issue #6912,
// arm I).
//
// Shipped predecessors: arm A csharp (#6984), arm B proto (#6991), arm C rust
// (#7000), arm D golang (#7036), arm F java (#7039), arm E fsharp (#7040), arm H
// php (#7042), arm G swift (#7043).
//
// THE GAP. emitClassFieldMembers (struct_fields.go:46) mints one
// SCOPE.Schema/field entity per data member of a class/struct/union and records
// the declared type ONLY as Properties["field_type"] and inside Signature. The
// one edge attached to it is the owner→member CONTAINS, which the orphan
// definition excludes by design, so every C/C++ field is a LEAF on its outbound
// side. Contributor #6726 measures SCOPE.Schema at 99.3% orphan of 35,353
// entities; cpp fields are minted as SCOPE.Schema, so they are part of that
// population.
//
// ============================================================================
// 1. THE PROPERTY IS LOSSLESS, AND THE DECLARATOR AXIS IS NOT THE TYPE AXIS
// ============================================================================
//
// struct_fields.go:61 reads ch.ChildByFieldName("type") and stores its VERBATIM
// text, so `std::vector<Order>`, `std::map<Item, Order>`, `Ns::Order`,
// `unsigned int` and even an inline `struct { int inner; }` all survive
// character for character. Arm G's stash-from-the-AST is therefore unnecessary
// (swift needed it because its property is the FIRST pre-order type_identifier
// and yields `Dictionary` for `Dictionary<String, Order>`); this is arm E/H's
// shape — tokenise the type expression and judge each bare identifier
// independently. Pinned by TestCppFieldTypeRefs_FieldTypeIsVerbatim.
//
// THE TRAP THAT MAKES A CPP FIXTURE LOOK MORE THOROUGH THAN IT IS: in the
// tree-sitter C++ grammar the pointer / reference / array decoration lives in
// the DECLARATOR, not in the `type` field. `Order o;`, `Order* p;`, `Order& r;`
// and `Order arr[4];` therefore arrive at this pass as the IDENTICAL string
// "Order" — a fixture sweeping those four rows has varied the declarator axis
// while holding the type-expression axis constant. The two axes are swept
// separately in field_type_refs_6912_test.go, and the declarator rows are
// labelled there as what they actually grade: that each decorated member gets
// its own field entity and its own edge.
//
// ============================================================================
// 2. THE RESOLVER TIER THAT RESOLVES THIS ADDRESS — DRIVEN, NOT INFERRED
// ============================================================================
//
// The ToID is extractor.BuildComponentStructuralRef, i.e.
// `scope:component:class:<lang>:<file>:<Name>`, where <lang> is the FILE's
// language — "cpp" or "c" — because one extractor serves both (extractor.go:63,
// :72). resolveStructuralRef (internal/resolve/refs.go:2982) then tries, in
// order:
//
//  1. lookupLocationKind(file, name, structuralKindFamilies("component")), i.e.
//     componentKindFamily {Component, Class, View, Model, SCOPE.Component,
//     SCOPE.View, SCOPE.Model} (refs.go:2341), plus each entity's SCOPE-trimmed
//     alias (refs.go:1290-1296).
//  2. ambigLocation[file][name] (refs.go:3004) — set when TWO DISTINCT ENTITY
//     IDS share (file, name), REGARDLESS of kind.
//  3. byLocation[file][name] — kind-agnostic.
//
// EVERY target this pass admits is a SCOPE.Component, so every edge it emits is
// answered at TIER 1 and never reaches tiers 2/3. That is the whole basis of the
// ambiguity rule in section 4, and it was established by driving the real
// resolver (BuildIndex → ReferencesEmbedded) over records produced by the real
// extractor, not by reading refs.go: TestCppFieldTypeRefs_ResolvesToEntityIDs.
//
// ============================================================================
// 3. NOT-A-DECLARATION CARRIERS — THE #7047 / #7056 AUDIT
// ============================================================================
//
// Arm G shipped 19 of 38 edges bound to an `extension Foo` CARRIER rather than
// to the declaration of Foo, and EVERY instrument reported a perfect result
// (total=38 bound=38 dangling=0 wrongName=0 wrongFile=0) because the carrier had
// the right name, right file, admissible kind and admissible subtype. It was
// wrong only in not being a declaration. C++ is the language most able to repeat
// that, so every same-file record that can carry a TYPE'S BARE NAME without
// declaring it was enumerated from this package's own emit sites and probed:
//
//	FORWARD DECLARATION `class Order;`   extractClassLike (extractor.go:825) does
//	  NOT require a body, so this mints SCOPE.Component/class named Order in this
//	  file — right name, right file, admissible kind AND admissible subtype. It is
//	  the exact #7047 shape and it is REFUSED here, by Metadata["definition"],
//	  which extractClassLike sets only when findClassBody returns a body.
//	  Graded by TestCppFieldTypeRefs_ForwardDeclarationIsNeverATarget, and its
//	  opposite direction (a forward declaration must not SUPPRESS the definition
//	  beside it) by ..._ForwardDeclarationBesideItsDefinitionStillBinds.
//
//	ELABORATED TYPE SPECIFIER `struct Item i;` mints a SECOND, bodyless Component
//	  named Item from the field's own type. Refused by the same marker, which is
//	  why the marker is a body test rather than a forward-declaration test.
//	  (`extern struct Foo x;` is the same shape.)
//
//	#include / using placeholders   extractInclude (:1269) and extractUsing
//	  (:1327) mint SCOPE.Component/import named `vector` (from `#include
//	  <vector>`), `std` (from `using namespace std;`) and `std::string` (from
//	  `using std::string;`). The first two are BARE names in type position under
//	  the extremely common `using namespace std;`. Refused by the subtype
//	  allow-list; graded by ..._IncludePlaceholderIsNeverATarget.
//
//	NAMESPACE (including a RE-OPENED one)   SCOPE.Component/namespace. Refused by
//	  the subtype allow-list. Re-opening mints two records with one (Kind, Name,
//	  SourceFile) and so ONE node, which is why it cannot make anything ambiguous.
//
//	EXPLICIT / PARTIAL TEMPLATE SPECIALISATION `template<> struct Box<int>` is
//	  SCOPE.Schema/template named "Box<int>" — refused by kind, and additionally
//	  unreachable because a scanner token can never contain '<'.
//
//	`friend class Buddy;`   mints NO record (probed, not assumed:
//	  TestCppFieldTypeRefs_FriendDeclarationMintsNoRecord).
//
//	`using Alias = Order;` / `typedef Order OrderT;`   neither alias_declaration
//	  nor type_definition is a case in walkStructural, so they mint NO record at
//	  all. They are therefore not a wrong-binding hazard; they ARE a recall
//	  ceiling, pinned by ..._AliasAndTypedefMintNoRecord so closing the capture
//	  fails loudly rather than silently changing this pass's answers.
//
//	FILE CARRIER   SCOPE.Component/file (extractor.FileEntity), Name = the file
//	  path. Refused by the subtype allow-list; unreachable by name shape too,
//	  which is claimed as belt and not as the reason.
//
// A 100% BIND RATE IS NOT EVIDENCE OF ANY OF THIS. Every carrier above binds
// perfectly; what the corpus harness therefore measures is not the bind rate but
// the TARGET SET, diffed row for row with the refusal toggled off (PR body).
//
// ============================================================================
// 3b. THE SOURCE ENDPOINT — THE SAME AUDIT, ON THE OTHER END OF THE EDGE
// ============================================================================
//
// The audit above is exhaustive about what an edge may point AT and said nothing
// about what it may hang OFF. That asymmetry is #7056's blindness mirrored, and
// on the first revision of this arm it was live. The SCOPE.Schema/field record a
// cpp field edge hangs off is minted for two constructs that are not plain data
// members:
//
//	virtual const VideoInfo& GetVideoInfo() = 0;   a pure virtual MEMBER FUNCTION
//	AVSMap& (VideoFrame::* getProperties)();       a POINTER-TO-MEMBER-FUNCTION
//	                                               data member
//
// In both, the record's `field_type` is the RETURN type, so the edge asserted
// `field_name=GetVideoInfo, ref_kind=field_target_type` about a member whose
// declared type is not that type at all — a confidently wrong edge that BINDS,
// which is the direction `bug-extractor` cannot flag.
//
// MEASURED, on the corpus's C++-parsed headers: 68 of 254 field entities (26.8%)
// are function-shaped, and refusing them as sources takes this arm from 23 edges
// to 6. The per-shape split of those 17 is NOT re-derived here; an independent
// review counted 8 member functions and 8 pointer-to-member-function members,
// which accounts for 16 of the 17, and the aggregate is what this pass measured.
//
// The CAPTURE defect is pre-existing (#4854) and is NOT fixed here: correcting
// it changes entity counts, the #6118 digest and the #4854 contract. It is filed
// as #7061. What this arm does is stop propagating it — emitClassFieldMembers
// now marks such records Metadata["function_shaped"], and this pass refuses them
// as edge SOURCES (cppIsFunctionShaped). The marker is also why the false claim
// in cppFieldNames' doc comment — "Returns nil for a member function
// declaration", true only for a BARE `void f();` — has been corrected rather
// than left to mislead the next reader.
//
// ============================================================================
// 4. THE AMBIGUITY RULE — DERIVED FROM THE TIER, NOT COPIED FROM AN ARM
// ============================================================================
//
// The standing rule on #6912 is: COUNT THE KINDS THAT THE RESOLVER TIER WHICH
// RESOLVES YOUR ADDRESS ACTUALLY WEIGHS. Per section 2 that tier is
// lookupLocationKind filtered to componentKindFamily, so this pass counts kinds
// WITHIN THAT FAMILY — arm F/H's scope, reached from cpp's own tier rather than
// inherited. The UNIT is the KIND, not the RECORD, mirroring refs.go:1308:
// within one file two records sharing a Kind are ONE node, so counting records
// would refuse edges that bind perfectly well — and in C++ that is not a corner
// case, because a forward declaration beside its definition, a re-opened
// namespace and an elaborated type specifier all mint a second record for one
// node. Arm C counts records and is wrong under the rule (#7038); neither its
// rule nor its stated reason is copied.
//
// NOTE ON THE REFUSAL'S REASON. Arms C, D and E justify rule 2 as "emitting
// would guarantee a dangling stub". That reason is FALSE IN GENERAL — it is a
// conservative approximation that is load-bearing only for rivals INSIDE the
// family the resolving tier weighs — and it is not repeated here.
//
// ============================================================================
// 5. SAME-FILE TARGETS ONLY
// ============================================================================
//
// A field whose type is declared in another translation unit or header gets
// NOTHING. Binding a bare type name across files is exactly the #6976/#6369
// hazard, and C++ sharpens it: `#include "order.h"` means the bare `Order` in
// this file names a type in a DIFFERENT file, and the same bare name recurs
// across a tree. Closing it needs an include-graph view this pass has none of.
//
// There are TWO independent same-file conjuncts and they fail in OPPOSITE
// directions, so each is graded separately rather than one paying for the
// other — grading one is exactly what makes its sibling look covered:
//
//	(a) the RIVAL scan in cppInFileTypeTargets' pass 1 (`r.SourceFile !=
//	    filePath` there): a cross-file record wrongly SUPPRESSING an edge.
//	(b) the TARGET scan in pass 2 (`r.SourceFile != filePath` there): a
//	    cross-file record wrongly BECOMING a target.
//
// A third conjunct guards the FIELD side in attachCppFieldTypeRefs — a field
// record from another file must not be walked at all. All three are graded by
// TestCppFieldTypeRefs_Unit_* in field_type_refs_units_6912_test.go.
//
// ============================================================================
// 6. WHAT THIS PASS DOES NOT DO — RECALL CEILINGS, STATED WHERE THEY ARE MADE
// ============================================================================
//
//   - ENUMS are not targets. See cppTypeDeclSubtypes.
//   - TEMPLATES are not targets (SCOPE.Schema/template).
//
//   - TEMPLATE PARAMETERS are refused by name, and the first revision of this
//     comment was WRONG to call the over-fire unreachable. Two shapes, and only
//     the first is genuinely out of reach:
//
//     A templated class's DIRECT members emit no field entity at all
//     (walkStructural's template arm walks the body for Operations only), so
//     `template<typename T> struct Box { T v; };` has nothing to over-fire —
//     asserted by ..._TemplatedClassMembersEmitNoFieldEntity.
//
//     A class NESTED inside a template does get field entities, because that arm
//     recurses into the inner class body. `template<typename T> struct Outer {
//     struct Inner { T val; }; };` beside a real `struct T` is four lines away
//     and WOULD have bound `Inner.val` to `struct T`. Go ships exactly that as a
//     known-wrong over-fire (#7041); swift fixed it; this arm fixes it, by
//     recording the parameter names in scope on each field
//     (stampCppTemplateParams) and refusing a candidate that matches one. The
//     refusal is scoped to the fields actually inside the template, so a real
//     `struct T` member of an ordinary class elsewhere in the file still binds —
//     both directions graded by ..._TemplateParameterIsNeverATarget.
//   - ALIASES and TYPEDEFS name nothing in the graph (section 3).
//   - QUALIFIED names are refused whole (see cppFieldTypeCandidates).
//   - Two types with the SAME bare name in one file — `Ns::Order` beside a global
//     `Order` — collapse to one graph node, because cpp Component names are
//     unqualified and graph.EntityID hashes (repo, Kind, Name, SourceFile). An
//     edge to the bare name binds to that conflated node. That is a PRE-EXISTING
//     identity ceiling, not something this pass introduces, and it is the reason
//     a qualified spelling is refused rather than resolved.

// cppFieldTargetRefKind is the value of the `ref_kind` edge property. It matches
// arm A's csFieldTargetRefKind, arm B's protoFieldTargetRefKind, arm C's
// rustFieldTargetRefKind, arm D's goFieldTargetRefKind, arm F's
// javaFieldTargetRefKind, arm E's fsharpFieldTargetRefKind, arm H's
// phpFieldTargetRefKind and arm G's swiftFieldTargetRefKind verbatim — the
// discriminator is what makes the edge queryable as a field→declared-type edge
// across languages, so it must be one string. Arm D established that the
// independent literals are deliberate: N graders enforcing agreement
// transitively beat one shared constant.
const cppFieldTargetRefKind = "field_target_type"

// cppFieldTypeCandidates returns every UNQUALIFIED identifier written in a C/C++
// type expression, in source order and without deduplication.
//
// It is a scanner, not a parser. It models neither cv-qualifiers nor template
// syntax nor the C elaborated-type form, and that is the point: every named type
// in a C++ type expression appears as a bare identifier whatever composes them,
// so there is no type-syntax table to keep in sync. `std::pair<Order, Item>`
// names TWO types and each gets its own edge, because each genuinely is a
// declared type the field's type is built from.
//
// TWO token shapes are handled deliberately:
//
//   - A QUALIFIED name (`std::vector`, `Ns::Order`, `::Order`) is consumed WHOLE
//     and discarded. Consuming it whole is what makes it safe: a scanner that
//     merely SKIPPED the `::` would re-enter mid-token and produce exactly the
//     bare segment the skip exists to prevent. Reducing `Ns::Order` to `Order` is
//     precisely the failure arm D found in Go's resolveTypeReferences, where
//     `other.Order` bound to a same-file `Order` — a wrong binding, which never
//     reaches `bug-extractor` because it binds. A LEADING `::` is refused for a
//     second and independent reason: inside `namespace N`, this package names the
//     Component for `N::Order` just `Order`, so a same-file bare `Order` may BE
//     `N::Order` while `::Order` is the global one — different types. In the
//     namespace-less case the two coincide and the refusal is merely
//     conservative, which is the direction to be conservative in.
//     Graded by TestCppFieldTypeRefs_QualifiedTypeIsNotStrippedToASameFileName
//     and the qualified rows of the shape space.
//
//   - A type expression containing a BRACE is refused ENTIRELY, because an inline
//     type definition (`struct { int Order; } anon;`) is not a reference to a
//     declared type and its MEMBER names are identifiers this scanner would
//     otherwise offer as type candidates — `int Order;` inside one would emit an
//     edge to a same-file `class Order` from a member name. Graded by
//     TestCppFieldTypeRefs_AnonymousInlineTypeIsRefusedWhole, whose fixture
//     contains exactly that trap.
//
// NO KEYWORD BLOCKLIST EXISTS AND NONE IS NEEDED. `int`, `unsigned`, `const`,
// `struct`, `class`, `void` and friends are collected as candidates like any
// other identifier and refused by the DECLARATION GATE: the file declares no type
// of that name, and C++ reserves every one of those words so no legal
// declaration ever can. That is a different argument from PHP's and from F#'s —
// F# needed no blocklist because `list`/`option` are ordinary shadowable
// identifiers the file does not declare, whereas here they are unusable as
// names — and it is why the absence of a blocklist is recorded rather than
// inherited. Graded by the primitive rows of the shape space.
//
// A field type is at most a few dozen characters, so the scan is linear in the
// property length with no allocation beyond the result slice.
func cppFieldTypeCandidates(typ string) []string {
	if strings.ContainsRune(typ, '{') {
		return nil
	}
	var out []string
	r := []rune(typ)
	i := 0
	for i < len(r) {
		switch {
		case isCppQualSep(r, i):
			// Leading `::` — consume the separator AND every segment after it,
			// so no segment can re-enter the scanner on its own.
			i += 2
			for i < len(r) && (isCppIdentRune(r[i]) || isCppQualSep(r, i)) {
				if isCppQualSep(r, i) {
					i += 2
					continue
				}
				i++
			}
		case isCppIdentStartRune(r[i]):
			j := i
			qualified := false
			for j < len(r) {
				if isCppIdentRune(r[j]) {
					j++
					continue
				}
				if isCppQualSep(r, j) {
					qualified = true
					j += 2
					continue
				}
				break
			}
			if !qualified {
				out = append(out, string(r[i:j]))
			}
			i = j
		default:
			i++
		}
	}
	return out
}

// isCppQualSep reports whether r[i:] begins with the `::` scope-resolution
// operator.
func isCppQualSep(r []rune, i int) bool {
	return i+1 < len(r) && r[i] == ':' && r[i+1] == ':'
}

// isCppIdentStartRune / isCppIdentRune implement the C/C++ identifier grammar,
// `[A-Za-z_][A-Za-z0-9_]*`. `$` is deliberately excluded: it is a widespread
// compiler EXTENSION rather than standard C++, and this package's own entity
// Names come from the same source text either way, so a `$`-bearing name that
// really is declared here would simply never be a candidate rather than be
// mis-split.
func isCppIdentStartRune(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isCppIdentRune(r rune) bool {
	return isCppIdentStartRune(r) || (r >= '0' && r <= '9')
}

// cppFieldTypeTarget is one in-file type DEFINITION a field-type edge may
// address: the structural ToID that binds to it, and the declared spelling of
// the name for the `target_type` property and the self-reference check.
type cppFieldTypeTarget struct {
	toID string
	name string
}

// cppTypeDeclSubtypes is the target ALLOW-LIST over SCOPE.Component subtypes.
//
// It is an allow-list rather than a denylist because this package emits several
// BARE-NAMED SCOPE.Components that are not type declarations (section 3), and a
// future producer adding another one must be refused BY DEFAULT. Enumerated from
// this package's own emit sites rather than inherited from an arm:
//
//	ADMITTED — the three subtypes walkStructural (extractor.go:185) passes to
//	extractClassLike, i.e. every kind of C/C++ record type there is:
//	  class   struct   union
//
//	REFUSED:
//	  "namespace"  extractNamespace (:1003) — not a type.
//	  "import"     extractInclude (:1269) / extractUsing (:1327). THE LIVE KILL:
//	               `#include <vector>` is named `vector`, `using namespace std;`
//	               is named `std`, and both are bare names a field type can spell
//	               under `using namespace std;`. See section 3.
//	  "file"       extractor.FileEntity, the #577 file carrier.
//
// ENUMS ARE NOT HERE, AND THE REASON IS NOT THE ONE ITS PREDECESSORS GIVE.
// extractEnum (:1080) mints `enum class Color` as SCOPE.Schema/enum — a REAL
// same-file declaration, and `Color c;` is ordinary C++. Arm H refused PHP enums
// because a component-space ref to one hits an AMBIGUOUS tier 2 and DANGLES; that
// reason does NOT transfer, because a lone cpp enum has no SCOPE.Enum value-set
// beside it (this package never calls extractor.EnumEntity) and a component-space
// ref to it would fall through to the kind-agnostic byLocation tier and BIND.
// This arm refuses them anyway, for two reasons it can defend:
//
//  1. An address whose scope segment says `component` must not be answered by a
//     non-component. Admitting enums would put this arm's targets on TWO tiers
//     with two different ambiguity rules — arm G's shape, which arm G took only
//     because swift's type_alias left it no choice.
//  2. The honest alternative is a SCHEMA-space dialect (schemaKindFamily
//     {Schema, Field, Property, SCOPE.Schema} at refs.go:2385 resolves a cpp enum
//     at tier 1), i.e. two address dialects behind one ref_kind — #6451's defect
//     shape. It should be decided across arms, not invented by this one.
//
// IT IS A NAMED RECALL LOSS, measured on the corpus and reported in the PR body
// rather than described. Graded in both directions by
// TestCppFieldTypeRefs_EnumIsNotATarget.
//
// SCOPE.Schema/template and SCOPE.Schema/concept are refused by Kind for the same
// reason as enums, and a specialisation's Name additionally carries '<'.
var cppTypeDeclSubtypes = map[string]bool{
	"class":  true,
	"struct": true,
	"union":  true,
}

// cppComponentAddressFamily is the set of entity Kinds that can make a
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
var cppComponentAddressFamily = map[string]bool{
	"Component": true, "Class": true, "View": true, "Model": true,
	"SCOPE.Component": true, "SCOPE.Class": true,
	"SCOPE.View": true, "SCOPE.Model": true,
}

// cppInFileTypeTargets indexes every TYPE DEFINED in this file that a field-type
// edge may address, keyed by the bare name.
//
// THE KEY IS NOT CASE-FOLDED. C and C++ identifiers are case-sensitive, so
// `class money` and `class Money` are two different types and a fold would
// conflate them. (Arm H folds because PHP class names are case-insensitive; the
// difference is a property of the language, not a style choice.)
//
// Three rules, all argued in the header block:
//
//  1. The record must be a SCOPE.Component this package emits for a real type
//     declaration (cppTypeDeclSubtypes). This is a check on the EMITTED RECORD,
//     not on the AST, so a declaration the extractor chose not to emit can never
//     be addressed.
//
//  2. The record must be a DEFINITION, i.e. carry Metadata["definition"], which
//     extractClassLike sets only when the specifier has a body. This is the
//     #7047 refusal: a forward declaration, an elaborated type specifier and an
//     `extern` declaration all mint a Component with the right name and an
//     admissible subtype while declaring nothing here.
//
//  3. A name carried by MORE THAN ONE DISTINCT KIND IN THE COMPONENT ADDRESS
//     FAMILY is dropped. Restricted to that family — not to every same-file
//     kind — because a non-family kind never enters the tier that resolves this
//     address (section 4).
func cppInFileTypeTargets(records []types.EntityRecord, filePath, lang string) map[string]cppFieldTypeTarget {
	// Pass 1 — how many DISTINCT COMPONENT-FAMILY KINDS each same-file name
	// carries, target-eligible or not.
	nameKinds := make(map[string]map[string]bool)
	for i := range records {
		r := &records[i]
		if r.SourceFile != filePath || r.Name == "" || !cppComponentAddressFamily[r.Kind] {
			continue
		}
		if nameKinds[r.Name] == nil {
			nameKinds[r.Name] = make(map[string]bool)
		}
		nameKinds[r.Name][r.Kind] = true
	}

	// Pass 2 — admit the type definitions whose name is unambiguous.
	targets := make(map[string]cppFieldTypeTarget)
	for i := range records {
		r := &records[i]
		if r.SourceFile != filePath || r.Name == "" || r.Kind != "SCOPE.Component" {
			continue
		}
		if !cppTypeDeclSubtypes[r.Subtype] {
			continue
		}
		if !cppIsTypeDefinition(r) {
			continue
		}
		if len(nameKinds[r.Name]) > 1 {
			continue
		}
		targets[r.Name] = cppFieldTypeTarget{
			toID: extractor.BuildComponentStructuralRef(lang, filePath, r.Name),
			name: r.Name,
		}
	}
	return targets
}

// cppIsTypeDefinition reports whether a class/struct/union Component record came
// from a specifier WITH A BODY — i.e. whether this file defines the type rather
// than merely naming it. extractClassLike stamps Metadata["definition"] exactly
// when findClassBody returns a body.
//
// The marker is read rather than re-derived from line spans because a one-line
// definition (`struct Item { int n; };`) and a forward declaration (`class
// Order;`) have the identical single-line span, and an EMPTY definition
// (`struct Empty {};`) carries no "fields" metadata either — so every proxy
// available on the record alone is wrong in one of those three directions.
func cppIsTypeDefinition(r *types.EntityRecord) bool {
	if r.Metadata == nil {
		return false
	}
	def, _ := r.Metadata["definition"].(bool)
	return def
}

// cppIsFunctionShaped reports whether a SCOPE.Schema/field record was minted
// from a construct carrying a function declarator — a member function
// declaration wrapped in pointer/reference decoration, or a
// pointer-to-member-function data member. emitClassFieldMembers stamps
// Metadata["function_shaped"] on exactly those; see cppFunctionShapedNames for
// why both shapes are marked and why the capture itself is not fixed here.
func cppIsFunctionShaped(r *types.EntityRecord) bool {
	if r.Metadata == nil {
		return false
	}
	fs, _ := r.Metadata["function_shaped"].(bool)
	return fs
}

// cppTemplateParamsOf returns the template parameter names in scope where a
// field was declared, as a set. stampCppTemplateParams (extractor.go) records
// them on every field emitted inside a template_declaration's nested classes.
func cppTemplateParamsOf(r *types.EntityRecord) map[string]bool {
	if r.Metadata == nil {
		return nil
	}
	names, _ := r.Metadata["template_params"].([]string)
	if len(names) == 0 {
		return nil
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return set
}

// attachCppFieldTypeRefs appends one REFERENCES edge per (field, in-file defined
// type) pair, reading the declared type from the field record's own `field_type`
// property.
//
// FromID is left empty so graph assembly anchors the edge on the FIELD record
// that carries it — the Django precedent at
// internal/extractors/python/django_relational.go:246-252, and arms B–H. Arm B's
// operative correction applies here too: an edge anchored on the PARENT leaves
// the field a leaf and leaves #6726's orphan rate exactly where it was.
//
// Relationships is APPENDED to, never assigned: the field record carries none
// today, but a later producer's edge must not be clobbered. (The owner→member
// CONTAINS lives on the OWNER's record, not here.)
//
// CALL PLACEMENT. It runs in Extract after attachCppFieldMembership, on the
// assembled record slice. Placement relative to emitExceptionFlowEdges is NOT
// load-bearing and is not claimed to be: that pass emits SCOPE.ExceptionType
// records on a synthetic `<exception>` path, which is neither a component-family
// kind nor this file, so it can neither become a target nor change a kind count.
// What the placement DOES buy is that the pass sees the complete record set,
// which is the invariant to preserve if a future pass starts minting bare-named
// Components.
func attachCppFieldTypeRefs(records []types.EntityRecord, filePath, lang string) []types.EntityRecord {
	targets := cppInFileTypeTargets(records, filePath, lang)
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
		// §3b — the SOURCE endpoint must be a plain data member. A field record
		// marked function_shaped came from a member function declaration or a
		// pointer-to-member-function member, and its `field_type` is the RETURN
		// type, so an edge from it would assert that the member's declared type
		// is something it is not.
		if cppIsFunctionShaped(r) {
			continue
		}
		owner := r.Properties["parent_class"]
		fieldName := r.Properties["field_name"]
		params := cppTemplateParamsOf(r)
		emitted := make(map[string]bool)
		for _, cand := range cppFieldTypeCandidates(declared) {
			// §6 — a TEMPLATE PARAMETER in scope is not a reference to the
			// same-named type declared in this file.
			if params[cand] {
				continue
			}
			t, ok := targets[cand]
			if !ok || emitted[t.toID] {
				continue
			}
			// A field never targets its own declaring type: `struct Node {
			// Node* next; }` is a self-reference the CONTAINS edge already
			// relates, and the recursive-type edge asserts nothing new.
			if owner != "" && t.name == owner {
				continue
			}
			emitted[t.toID] = true
			r.Relationships = append(r.Relationships, types.RelationshipRecord{
				ToID: t.toID,
				Kind: string(types.RelationshipKindReferences),
				Properties: types.Props{
					{K: "field_name", V: fieldName},
					{K: "ref_kind", V: cppFieldTargetRefKind},
					{K: "target_type", V: t.name},
				},
			})
		}
	}
	return records
}
