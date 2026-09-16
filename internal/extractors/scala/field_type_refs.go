package scala

import (
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	"github.com/cajasmota/grafel/internal/types"
)

// field_type_refs.go — the field→declared-type edge for Scala (issue #6912).
// Arm A is internal/extractors/csharp (#6984), B proto (#6991), C rust (#7000),
// D golang (#7036), E fsharp (#7040), F java (#7039), G swift (#7043),
// H php (#7042), I cpp (#7057), J kotlin (#7063). Scala is the LAST arm; ruby
// is scoped out because `attr_accessor :buyer` names no type at all.
//
// THE GAP, AND WHY SCALA IS TWO STEPS RATHER THAN ONE. Most shipped arms read a
// declared type their extractor already recorded — a `field_type` property, a
// `Signature`, or a live local at the emit site. Scala recorded NOTHING:
// buildScalaField and emitScalaCaseClassFields built their SCOPE.Schema/field
// records with no Properties map, no Signature and no type datum of any kind.
// That absence was RE-ESTABLISHED BY PROBE before anything was built, not
// inherited from the issue: #6912 asserted exactly this about JAVA and it was
// false (arm F found the type already a local at java.go:425). For Scala the
// probe printed every record of a 10-shape file and every field row came back
// `sig="" props=map[]`.
//
// So this arm CAPTURES the declared type at the emit site and BINDS it in one
// change. The capture writes to Metadata SCRATCH that attachScalaFieldTypeRefs
// deletes, not to an entity property — the graph gains edges and nothing else.
// A capture that shipped alone would be one more unhashed property that nothing
// reads, which is the state #6912 exists to fix.
//
// WHERE THE TYPE LIVES IN THE CST, probed against the real grammar rather than
// read from a doc. Scala's two field anchors put it in the same place, and both
// have a NEIGHBOUR in that node that must not be read:
//
//	val_definition   [modifiers] val identifier ":" TYPE ["=" INITIALIZER]
//	var_definition   [modifiers] var identifier ":" TYPE ["=" INITIALIZER]
//	class_parameter  [modifiers] [val|var] identifier ":" TYPE ["=" DEFAULT]
//
// The INITIALIZER is the Scala instance of the not-a-declaration shape that cost
// arm G half its edges (#7047) and arm I 16 of 39 (#7057), and here it is not
// merely adjacent — it is a plausible type name in the same node:
//
//	val c = new Repo()
//	  val_definition [val, identifier c, "=", instance_expression[new,
//	                  type_identifier "Repo", arguments]]
//
// A capture that walked the whole val_definition would emit `c -> Repo` from an
// INFERRED type, with the right name, the right file, an admissible kind and an
// admissible subtype — and no instrument we own can see that it is wrong
// (#7056). Scoping the capture to the window that FOLLOWS ":" and PRECEDES "="
// excludes it structurally rather than by a blocklist, and the same window
// excludes a class parameter's default value (`opt: Option[Line] = None`) and a
// leading modifier list (`@Inject private val r: Repo`).
//
// THE TYPE-EXPRESSION SHAPE SPACE, enumerated against the grammar rather than
// sampled (arm I's template-parameter refusal was wrong in BOTH directions and
// only enumeration found it; three rounds of hand-picked attacks on earlier arms
// each missed something). Every row below was printed from a real parse:
//
//	Repo                    type_identifier                          → Repo
//	Option[Line]            generic_type[tid, type_arguments]        → Option, Line
//	List[List[Repo]]        nested generic_type                      → List, List, Repo
//	List[_]                 type_arguments holds `wildcard`          → List
//	Repo with Named         compound_type                            → Repo, Named
//	Repo & Named            infix_type[tid, operator_identifier,tid] → Repo, Named
//	(Repo, Named)           tuple_type                               → Repo, Named
//	Repo => Named           function_type[parameter_types, tid]      → Repo, Named
//	Repo @unchecked         annotated_type[tid, annotation[tid]]     → Repo ONLY
//	com.acme.Repo           stable_type_identifier (DOTTED)          → (none)
//	_root_.com.acme.Repo    stable_type_identifier                   → (none)
//	Registry.Inner          stable_type_identifier                   → (none)
//	Registry.type           singleton_type[identifier, ".", type]    → (none)
//	T (a type parameter)    type_identifier                          → (none)
//
// THE ANNOTATION TRAP is Scala's own, and it is the over-collection direction:
// an `annotated_type` holds the annotation's name as a `type_identifier` in the
// very subtree the declared type occupies, so `r: Repo @Inject` would otherwise
// yield BOTH `Repo` and `Inject`. A Scala annotation IS a class, so a same-file
// `class Inject` makes that a fully-binding, fully-wrong edge. The annotation
// subtree is skipped whole.
//
// WHY DOTTED IS SKIPPED WHOLE RATHER THAN REDUCED TO ITS LAST SEGMENT. Arm D
// found Go's resolveTypeReferences stripping `other.Order` to `Order` and
// binding it to a same-file `Order` — a silently wrong binding, and a bound edge
// never reaches bug-extractor. Scala makes it worse than Go: `Registry.Inner` is
// a TYPE PROJECTION whose qualifier is itself a same-file declaration, so BOTH
// segments name something in this file and any reduction picks one of two wrong
// answers. `singleton_type` (`Registry.type`) is refused for the same reason and
// is a stated recall floor, not a claim that it names nothing.
//
// TYPE PARAMETERS ARE EXCLUDED BY NAME. `class Holder[T](val item: T)` yields
// candidate `T`, and a file that also declares `class T` would get a wrong edge
// — the over-fire arm F had to pin as KNOWN-WRONG because Java's grammar gives a
// type parameter and a type reference the same node. Scala's does too, but the
// declaration's `type_parameters` list is right there at the emit site, so the
// shadow is refused rather than documented.
//
// That sentence was FALSE FOR `[+A]` AND `[-A]` when this arm first shipped, and
// the false half is the dominant form in the language: the collector switched on
// two node types the grammar does not have, so every variance-annotated
// parameter fell through and `class A; class Box[+A](val a: A)` emitted
// `Box.a -> A` — a wrong edge that BOUND. A mutant making both of those arms
// unmatchable killed zero tests: they were dead code, and the claim was prose
// asserting what no test observed. The declaration-form space is now enumerated
// from the grammar at scalaTypeParameterNames and graded row by row IN BOTH
// DIRECTIONS (TestScalaFieldTypeRefs_TypeParameterFormSpace), because
// enumerating the parameter's NAME and the ANCHOR thoroughly is exactly what
// made the parameter's FORM look covered.
//
// SCOPE IS LEXICAL AND ACCUMULATES, WHICH IS THE OPPOSITE OF ARM J'S RULE.
// Kotlin nests statically by default, so arm J must not inherit; Scala has no
// `inner` keyword and no static nesting, so a nested class, trait or object sees
// every enclosing declaration's parameters and this pass carries them down (see
// scalaScopedTypeParameterNames). `class T; class Outer[T] { class Inner(val x:
// T) }` emitted `Inner.x -> T`, bound, and was filed as a "recall floor" — it
// was not one: a recall floor loses a correct edge and is safe, this gained a
// wrong one. The over-refusal direction (nesting alone must never refuse) is
// graded in the same table.
//
// THE AMBIGUITY RULE — derived from the tier that resolves THIS address, not
// copied. Every arm has answered "how many same-file declarations sharing this
// name make the binding ambiguous?" differently: arm C counts RECORDS (wrong,
// #7038), arm D counts every KIND, arm F counts within componentKindFamily,
// arm G and arm J needed BOTH tiers, and arm I found arm D's rule ACTIVELY
// WRONG for C++ because a constructor is named after its class.
//
// Scala needs exactly ONE tier, and the reason is a fact about this extractor
// rather than about the resolver: it has exactly ONE admissible target kind.
// `type Alias = Order` parses as `type_definition`, walkNode has no case for it,
// and it mints NO ENTITY AT ALL — so there is no SCOPE.Schema/type_alias target
// here, which is the only thing that put arms G and J on the second tier. Every
// edge this pass emits therefore addresses a SCOPE.Component and is answered by:
//
//	Index.lookupStructural → lookupLocationKind(file, name,
//	  structuralKindFamilies("component") == componentKindFamily) (refs.go:2982)
//
// which returns FIRST; ambigLocation (refs.go:3004) is never consulted. Only a
// rival kind INSIDE that family can blank the match, so the count is over family
// kinds. Driven through resolve.BuildIndex + resolve.ReferencesEmbedded, both
// directions, rather than read.
//
// SCALA'S SAME-FILE, SAME-NAME RIVALS, and why counting anything else is wrong:
//
//	sealed trait Shape + case object Circle
//	                    → SCOPE.Component/trait Shape AND a SCOPE.Enum value-set
//	                      ALSO NAMED Shape (constantset.go's sealed-trait arm).
//	object Status {…}   → SCOPE.Component/object AND a SCOPE.Enum const-group
//	                      value-set NAMED AFTER THE OBJECT.
//	enum Color {…}      → SCOPE.Enum only; walkNode has no enum_definition case,
//	                      so there is no Component rival and no Component target.
//
// SCOPE.Enum is outside componentKindFamily, so none of those can blank a
// component-space ref — measured, not assumed, and arm D's all-kinds rule would
// refuse every one of them.
//
//	case class Order(…) + object Order  (the COMPANION, idiomatic Scala)
//	                    → TWO SCOPE.Component records, same Name, same file.
//
// That pair is the Scala mirror of arm I's constructor: graph.EntityID hashes
// (repo, Kind, Name, SourceFile) with SUBTYPE EXCLUDED, so the two records are
// ONE graph node and ONE ToID — not ambiguous at all. Counting RECORDS (arm C's
// defect, #7038) would delete a correct edge on the single most common shape in
// the language. The unit of the count is the KIND.
//
// TWO GUARDS HERE ARE NOT INDEPENDENTLY GRADED, and that is recorded rather
// than left for the next reader to discover:
//
//   - The COLON requirement in scalaDeclaredTypeCandidates and the ANNOTATION
//     skip in scalaFieldTypeCandidates are MUTUALLY MASKING. Removing either
//     alone changes no output, on the fixture suite and on a 5-file probe alike;
//     removing BOTH is caught by two tests. The reason is a grammar fact: the
//     only pre-colon node a val_definition / class_parameter carries that holds
//     a `type_identifier` is an `annotation` (the child space was enumerated by
//     probe — modifiers → access_modifier → access_qualifier → identifier; the
//     name is an `identifier` / `identifiers`; val/var and the punctuation are
//     anonymous). An independent review re-enumerated that child space by CST
//     dump and added the STRONGER fact: the one shape that puts a ":" below the
//     top level of these nodes — a repeated parameter, `val b: Repo*` — mints no
//     field record at all, so it distinguishes nothing either and the mutant is
//     not production-reachable. No fixture was manufactured to kill it.
//     So the annotation skip carries every refusal the suite can
//     observe, and the colon requirement is DEFENSIVE — it keeps the capture
//     scoped structurally if the grammar ever puts a type there. Neither is
//     claimed to be independently load-bearing.
//   - The `singleton_type` entry in the skip list is REDUNDANT today: probed on
//     `X.type` and `Outer.Inner.type`, the node's children are
//     `identifier` / `stable_identifier` plus the `type` keyword and never a
//     `type_identifier`, so the scanner would collect nothing from it anyway.
//     Kept because it states the intent at the node the reader looks at, and
//     because the dotted-refusal reasoning above applies to it identically.
//
// A NON-BINDING EDGE IS WORSE THAN NO EDGE, so this pass emits only for a type
// DECLARED IN THIS FILE and refuses everything else outright. Cross-file targets
// need the resolver, not the extractor (#6976/#6369), and are a separate arm.

// scalaFieldTypeRefsMetaKey is the per-field capture written at the two emit
// sites and consumed — and deleted — by attachScalaFieldTypeRefs.
//
// The candidates cannot become edges at the emit site: `class Svc { val r: Repo
// }` may precede `class Repo` in the same file, so the in-file declaration set
// is complete only once the walk has finished.
const scalaFieldTypeRefsMetaKey = "field_type_refs"

// scalaFieldTypeRefsOwnerKey stashes the field's declaring type name alongside
// the candidates, so the `field_name` property can carry the LEAF name. It is
// not re-derived by splitting the entity Name on '.', because a Scala field name
// may itself be dotted-looking and a top-level val carries no owner at all.
const scalaFieldTypeRefsOwnerKey = "field_type_refs_owner"

// scalaFieldTargetRefKind is the value of the `ref_kind` edge property. It
// matches arms A-J verbatim — the discriminator is what makes the edge queryable
// as a field→declared-type edge across languages, so it must be one string.
// There is deliberately no shared constant: arm D scored that as a mutant (CZ-2)
// and found the independent literals grade the agreement transitively.
const scalaFieldTargetRefKind = "field_target_type"

// scalaComponentAddressFamily is the set of entity Kinds that can make a
// `scope:component:…` structural ref ambiguous — every Kind internal/resolve's
// lookupLocationKind weighs when resolving one.
//
// It is componentKindFamily (refs.go:2341 — Component/Class/View/Model plus the
// SCOPE.* spellings of three of them) CLOSED UNDER THE INDEX'S TRIM ALIAS.
// BuildIndex writes each entity under its raw Kind AND its SCOPE-trimmed alias,
// so an entity kinded `SCOPE.Class` is keyed under "Class" too — and "Class" IS
// in the family even though "SCOPE.Class" is not. Membership is therefore
// `K ∈ family || trim(K) ∈ family`, and the eight entries below are exactly that
// set. Duplicated here rather than imported so the dependency does not run
// extractor → resolver. Invariant: a Kind ABSENT from this set cannot make a
// component-space ref ambiguous, so widening componentKindFamily upstream
// without widening this would let the pass emit a stub that dangles.
var scalaComponentAddressFamily = map[string]bool{
	"Component": true, "Class": true, "View": true, "Model": true,
	"SCOPE.Component": true, "SCOPE.Class": true,
	"SCOPE.View": true, "SCOPE.Model": true,
}

// scalaFieldTypeCandidates returns every bare type name written in a declared
// type expression, in source order and without deduplication.
//
// The rule is a three-way split over node types, and the full shape space it
// covers is enumerated in the file header:
//
//	stable_type_identifier / singleton_type → skipped WHOLE (dotted or
//	  path-dependent; neither segment may be guessed at)
//	annotation                              → skipped WHOLE (the annotation's
//	  own name is a type_identifier living inside the declared type's subtree)
//	type_identifier                         → a candidate, unless it is one of
//	  the enclosing declaration's type parameters
//	anything else                           → descended
func scalaFieldTypeCandidates(typeNode ts.Node, src []byte, typeParams map[string]bool) []string {
	var out []string
	var walkType func(n ts.Node)
	walkType = func(n ts.Node) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "stable_type_identifier", "singleton_type", "annotation":
			return
		case "type_identifier":
			name := string(src[n.StartByte():n.EndByte()])
			if name != "" && !typeParams[name] {
				out = append(out, name)
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

// scalaTypeParameterNames returns the type-parameter names a class/trait/object
// declaration introduces ON ITS OWN, e.g. {"T", "R"} for `class Holder[T, R]`.
// Use scalaScopedTypeParameterNames to get the set actually in scope for the
// declaration's members, which also carries every enclosing declaration's.
//
// THE DIRECT-CHILD SPACE OF `type_parameters`, ENUMERATED BY CST DUMP rather
// than recalled. The first cut of this function switched on
// `variant_type_parameter` / `type_parameter`, and NEITHER NODE TYPE EXISTS in
// this grammar, so `[+A]` and `[-A]` — the form the collection-shaped generics
// Scala is actually written in are spelled (`List[+A]`,
// `Function1[-T, +R]`) — contributed NOTHING and `class A; class Box[+A](val a:
// A)` emitted `Box.a -> A`: a wrong edge that BINDS rather than dangles, the
// #7056 signature. A mutant making both of those arms unmatchable killed zero
// tests, because they were dead code. The real space is:
//
//	identifier                     [A]                 THE NAME (invariant)
//	covariant_type_parameter       [+A]                `+`, identifier, bounds
//	contravariant_type_parameter   [-A]                `-`, identifier, bounds
//	upper_bound                    [A <: Repo]         a REAL type
//	lower_bound                    [A >: Repo]         a REAL type
//	view_bound                     [A <% Repo]         a REAL type
//	context_bound                  [A : Ord]           a REAL type
//	type_parameters   (nested)     [F[_]] / [F[X]]     F's OWN parameters
//	annotation                     [@specialized A]    a REAL type
//	wildcard                       [_ <: Repo]         no name at all
//	[ ] , :                        anonymous
//
// ONLY THE NAME IS REFUSED, and that is the direction this guard must not get
// wrong. A bound's or context bound's right-hand side, an annotation's class and
// a higher-kinded parameter's own parameters are all REAL types that a field may
// legitimately be declared as (`class Box[A <: Repo](val r: Repo)`), and arm I
// shipped a template-parameter refusal that was wrong in BOTH directions at once
// (#7057) — a deleted correct edge has no symptom. So this walks DIRECT CHILDREN
// only and never descends into a wrapper that is not a variance wrapper, and it
// matches `identifier` alone: a `type_identifier` never occurs as a direct child
// (every one of them sits inside a bound, an annotation or a nested parameter
// list), and if the grammar ever minted one it would be a bound's RHS, i.e. a
// type that must stay bindable.
//
// TWO MUTANTS HERE ARE EQUIVALENT UNDER AN OBSERVED GRAMMAR FACT, not under an
// argument — TestScalaFieldTypeRefs_PremiseTypeParametersChildShapes walks the
// whole enumerated form space and fails if either fact moves:
//
//   - Re-adding `type_identifier` to the `identifier` arm changes nothing,
//     because no direct child of `type_parameters` is ever a `type_identifier`.
//     The day one is, that mutant becomes an OVER-REFUSAL and the premise test
//     fails first.
//   - Deleting the `break` in the variance arm changes nothing, because a
//     variance wrapper has exactly ONE direct `identifier` child.
//
// The reachable direction on both — a bound's RHS joining the shadow set — is
// graded, in the variance wrapper and at the top level alike.
func scalaTypeParameterNames(declNode ts.Node, src []byte) map[string]bool {
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
			switch p.Type() {
			case "identifier":
				out[string(src[p.StartByte():p.EndByte()])] = true
			case "covariant_type_parameter", "contravariant_type_parameter":
				// `+A` / `-A`. The variance sigil is an anonymous child and the
				// name is the first `identifier`; any bound that follows stays
				// wrapped in its own node and is deliberately not read.
				for k := 0; k < int(p.ChildCount()); k++ {
					id := p.Child(k)
					if id.Type() == "identifier" {
						out[string(src[id.StartByte():id.EndByte()])] = true
						break
					}
				}
			}
		}
	}
	return out
}

// scalaScopedTypeParameterNames returns the type parameters in scope for the
// MEMBERS of declNode: its own, UNION every lexically enclosing declaration's.
//
// SCALA'S SCOPING IS NOT KOTLIN'S, AND THIS RULE IS THE OPPOSITE OF ARM J'S.
// Kotlin nests statically by default — a nested class does not see the enclosing
// class's type parameters unless it is declared `inner` — so arm J's guard must
// NOT inherit. Scala has no `inner` keyword and no static nesting at all: every
// member class, trait and object of a template is an inner member of it, and the
// template is evaluated in a scope that already contains the declaration's type
// parameters (SLS §5.1). `object` is not an exception the way Kotlin's
// `companion object` is: an object nested inside a class is a PER-INSTANCE
// member — only a TOP-LEVEL object is static-like — so it sees `T` too.
//
// Before this, `class T; class Outer[T] { class Inner(val x: T) }` emitted
// `Inner.x -> T` and it BOUND, to the top-level `class T` that `Outer[T]`
// shadows. That was filed as a "recall floor" and it was not one: a recall floor
// LOSES a correct edge and is safe; this GAINED a wrong one, invisibly.
//
// The parent's map is never mutated — siblings of declNode share it, and the
// scope must not leak sideways or forward out of a nested body.
func scalaScopedTypeParameterNames(enclosing map[string]bool, declNode ts.Node, src []byte) map[string]bool {
	own := scalaTypeParameterNames(declNode, src)
	if len(enclosing) == 0 {
		return own
	}
	out := make(map[string]bool, len(enclosing)+len(own))
	for k := range enclosing {
		out[k] = true
	}
	for k := range own {
		out[k] = true
	}
	return out
}

// scalaDeclaredTypeCandidates collects the candidates from the children of
// `holder` that FOLLOW a ":" and PRECEDE any "=".
//
// Both Scala field anchors put the declared type in exactly that window, and the
// window is what excludes the INITIALIZER of `val c = new Repo()` (an inferred
// type, whose `new Repo()` holds a type_identifier that would otherwise become a
// confident wrong edge), a class parameter's DEFAULT VALUE, and a leading
// modifier/annotation list. All three exclusions are structural, and each is
// graded in its own test.
func scalaDeclaredTypeCandidates(holder ts.Node, src []byte, typeParams map[string]bool) []string {
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
			out = append(out, scalaFieldTypeCandidates(ch, src, typeParams)...)
		}
	}
	return out
}

// stashScalaFieldTypeRefs records a field entity's declared-type candidates and
// owning type, for attachScalaFieldTypeRefs to turn into edges once the file's
// full record set exists. A no-op when the declaration names nothing addressable
// (an inferred type, a fully-qualified type, a bare wildcard), so a field that
// can never produce an edge carries no metadata at all.
func stashScalaFieldTypeRefs(rec *types.EntityRecord, cands []string, owner string) {
	if len(cands) == 0 {
		return
	}
	if rec.Metadata == nil {
		rec.Metadata = make(map[string]interface{})
	}
	rec.Metadata[scalaFieldTypeRefsMetaKey] = cands
	rec.Metadata[scalaFieldTypeRefsOwnerKey] = owner
}

// scalaFieldTypeTarget is one in-file type declaration a field can point at:
// the structural ToID that binds to it, and the bare name for `target_type`.
type scalaFieldTypeTarget struct {
	toID string
	name string
}

// scalaInFileTypeTargets indexes every type DECLARED IN THIS FILE that a
// field-type edge may address, keyed by bare name, applying the ambiguity rule
// derived in the header block.
//
// Pass 1 counts the distinct Kinds in the COMPONENT ADDRESS FAMILY per name.
// That is the whole count: every target this pass admits is a SCOPE.Component,
// so every edge is answered by lookupLocationKind under componentKindFamily and
// never reaches ambigLocation. Two records sharing a Kind are ONE node (EntityID
// excludes Subtype), which is why `case class Order` beside its companion
// `object Order` is not ambiguous.
//
// THE `len(famKinds[name]) > 1` GUARD CANNOT FIRE THROUGH Extract AT ALL, and
// that is stronger than "its incidence is unmeasured". `records` is the slice
// THIS extractor alone built, and this extractor mints exactly one family Kind
// (`SCOPE.Component`) — internal/patterns' `SCOPE.Model` records are added to a
// different slice, later, and never reach here. So `famKinds[name]` has at most
// one entry on every input Extract can produce, and every test that scores this
// guard scores it against a HAND-BUILT record set. It is kept deliberately, as
// the defence against a SECOND family-Kind producer being added to this package
// — the day a `SCOPE.Class` or `SCOPE.View` arm lands here, the count starts
// mattering and the unit of the count (the KIND, not the record) is already
// right. What is graded is therefore the guard's RULE, counterfactually; its
// production incidence today is ZERO, by construction rather than by
// measurement.
//
// Pass 2's allow-list is written on Scala's own emit sites:
//
//	SCOPE.Component + class | case_class | trait → admitted
//
// What that excludes, at the strength each claim has:
//
//   - SCOPE.Component + "" (an IMPORT placeholder) — REACHABLE and
//     load-bearing, and more reachable in Scala than in any prior arm: buildImports
//     names the placeholder after the FIRST dotted segment of the path, so
//     `import Order.Status` or `import Config._` mints a bare-named Component
//     called `Order` / `Config`. A field typed `Order` in a file that only
//     IMPORTS Order would otherwise bind to the placeholder — Go's import
//     placeholder defect (arm D) and F#'s wrong binding (arm E) in one. Graded
//     from source, with the placeholder's existence asserted first.
//   - SCOPE.Component + "object" — an `object` declaration introduces a TERM,
//     not a type: `val r: Registry` does not compile for `object Registry`,
//     only `val r: Registry.type` does, and singleton_type is refused by the
//     candidate scanner. When an object IS a companion (`case class Order` +
//     `object Order`) the class record already supplies the identical ToID, so
//     the refusal costs nothing there. Measured on the corpus at zero delta.
//   - SCOPE.Component + "file" (#577) and + "twirl" (#501) — refused by
//     subtype; Name is the file path.
//   - SCOPE.Enum (the value-set beside a sealed trait, and the const-group
//     value-set named after an object) — refused as a TARGET while remaining
//     invisible to the family COUNT. Those are the two halves of the rule and
//     they are graded separately.
//   - SCOPE.Schema/field — refused by kind; also unreachable by name shape,
//     since a field Name is dotted. Claimed as nothing more.
//   - SCOPE.Operation — a Scala `def` sharing a type's name is legal and
//     common (`def Order(…)` as a factory), and it is refused as a target. It is
//     NOT a collider either, because it is outside the family — which is exactly
//     what arm D's rule would get wrong here.
//   - SCOPE.ExceptionType — refused by kind, and unreachable anyway: those
//     records carry a SENTINEL SourceFile, not this file's path.
func scalaInFileTypeTargets(records []types.EntityRecord, filePath string) map[string]scalaFieldTypeTarget {
	famKinds := make(map[string]map[string]bool)
	for i := range records {
		r := &records[i]
		if r.SourceFile != filePath || r.Name == "" {
			continue
		}
		if !scalaComponentAddressFamily[r.Kind] {
			continue
		}
		if famKinds[r.Name] == nil {
			famKinds[r.Name] = make(map[string]bool)
		}
		famKinds[r.Name][r.Kind] = true
	}

	targets := make(map[string]scalaFieldTypeTarget)
	for i := range records {
		r := &records[i]
		if r.SourceFile != filePath || r.Name == "" {
			continue
		}
		if r.Kind != "SCOPE.Component" {
			continue
		}
		switch r.Subtype {
		case "class", "case_class", "trait":
		default:
			continue
		}
		if len(famKinds[r.Name]) > 1 {
			continue
		}
		targets[r.Name] = scalaFieldTypeTarget{
			toID: extractor.BuildComponentStructuralRef("scala", filePath, r.Name),
			name: r.Name,
		}
	}
	return targets
}

// attachScalaFieldTypeRefs appends one REFERENCES edge per (field, in-file
// declared type) pair and clears the capture it consumed.
//
// FromID is left empty so graph assembly anchors the edge on the field record
// that carries it — the Django precedent at
// internal/extractors/python/django_relational.go:246-252, and arms B-J.
//
// A SELF-REFERENCE IS EMITTED. `class Node { val next: Option[Node] }` yields
// `Node.next → Node`. Arms D and F suppress the equivalent to stay consistent
// with a struct-anchored declared-type edge they already ship; Scala ships none,
// so there is nothing to be consistent with, and CONTAINS runs owner→field
// rather than field→owner, so the self edge restates nothing. Arms A, C, G and J
// emit it.
//
// Relationships is APPENDED to, never assigned: a Scala field record carries no
// outbound edge today, but assigning would silently clobber one added later.
func attachScalaFieldTypeRefs(records []types.EntityRecord, filePath string) {
	targets := scalaInFileTypeTargets(records, filePath)
	for i := range records {
		r := &records[i]
		if r.Metadata == nil {
			continue
		}
		cands, _ := r.Metadata[scalaFieldTypeRefsMetaKey].([]string)
		owner, _ := r.Metadata[scalaFieldTypeRefsOwnerKey].(string)
		delete(r.Metadata, scalaFieldTypeRefsMetaKey)
		delete(r.Metadata, scalaFieldTypeRefsOwnerKey)
		// The capture is the only thing that ever creates a Metadata map on a
		// Scala field record, so leaving an EMPTY one behind would put a
		// `metadata: {}` on records that carried no key before this pass
		// existed. Drop it, so a field whose type named nothing addressable is
		// byte-identical to what it was.
		if len(r.Metadata) == 0 {
			r.Metadata = nil
		}
		if len(cands) == 0 || r.Kind != "SCOPE.Schema" || r.Subtype != "field" ||
			r.SourceFile != filePath {
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
					{K: "ref_kind", V: scalaFieldTargetRefKind},
					{K: "target_type", V: t.name},
				},
			})
		}
	}
}
