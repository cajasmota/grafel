package fsharp

import (
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// field_type_refs.go — the field→declared-type edge for F# (issue #6912, arm E).
// Arm A is internal/extractors/csharp/field_type_refs.go (#6984), arm B is
// internal/extractors/proto/field_type_refs.go (#6991), arm C is
// internal/extractors/rust/field_type_refs.go (#7000), arm D is
// internal/extractors/golang/field_type_refs.go (#7036).
//
// THE GAP. du_record_members.go emits one SCOPE.Schema/field entity per record
// field, with the declared type recorded only as Properties["member_type"] and
// Signature. The single edge it attaches is the owner→member CONTAINS, which
// the orphan definition excludes by design, so every record field is a LEAF on
// its outbound side. F# is one of the four languages in contributor #6726's
// corpus — csharp / fsharp / java / protobuf — whose headline number is
// SCOPE.Schema at 99.3% orphan of 35,353 entities, and F# record members are
// minted as SCOPE.Schema by du_record_members.go:77. They are part of the
// exact population reported.
//
// WHY THIS ARM READS A PROPERTY WHEN NO PREVIOUS ARM DID
//
// Arm C stashed AST candidates during the walk rather than reading rust's
// `field_type` property, because a flat type STRING cannot generally be
// unwrapped safely; swift's equivalent property is actively wrong (it is the
// first pre-order type_identifier, so `Dictionary<String, Order>` yields
// `Dictionary`). Neither objection applies here, for two reasons that were
// established by probe rather than assumed:
//
//  1. THERE IS NO AST. The F# extractor is regex- and text-driven end to end
//     (extractor.go's moduleRE/typeRE/memberRE + extractIndentBody). There is
//     no tree-sitter grammar for F# in this tree, so "stash from the AST" is
//     not an available option, not a choice declined.
//  2. `member_type` IS THE VERBATIM SEGMENT TEXT, NOT A LOSSY PROJECTION.
//     parseRecordFields takes everything after the first `:` in the field
//     segment and only TrimSpace's it (du_record_members.go:140). Probed
//     against `Order`, `Customer option`, `Customer list`,
//     `Map<string, Customer>`, `Customer[]`, `int * Customer`,
//     `int -> Customer`, `Domain.Customer` and `'T`, it is character for
//     character what was written. It is the one shipped field-type property on
//     this issue that loses nothing — swift's is the first pre-order
//     type_identifier and yields `Dictionary` for `Dictionary<String, Order>` —
//     which is why this arm needs no stash.
//
//     THE PRECISE CLAIM IS "THE SEGMENT AFTER THE COLON", NOT "THE TYPE".
//     Nothing strips a trailing comment, so `Anno: Customer // why` arrives as
//     `"Customer // why"` and scans to candidates [Customer why]. It is
//     harmless — `why` is refused by the declaration gate and the right edge
//     still emits — but it is a real edge of the verbatim claim rather than a
//     rounding of it, and a file that happened to declare `type why` would get
//     a spurious edge. Pinned by the trailing-comment row in
//     TestFSharpFieldTypeRefs_MemberTypeIsVerbatimSourceText.
//
// F#'s POSTFIX GENERICS ARE NOT A SPECIAL CASE — THEY ARE WHY TOKENISING WORKS
//
// `Customer list` and `Customer option` are a shape no shipped arm has faced:
// the type constructor trails its argument. A wrapper-list unwrapper ("strip a
// known `option`/`list` suffix") would have to enumerate FSharp.Core and would
// still be wrong for a user-defined postfix constructor. So this pass does not
// unwrap at all. It TOKENISES the type expression into bare identifiers and
// judges each one independently against the same-file declaration set — arm
// D's rule, reached by a different route.
//
// That works because every named type in an F# type expression appears as a
// bare identifier, whatever the syntax composing them: prefix generics
// (`Map<string, Customer>`), postfix generics (`Customer list`), arrays
// (`Customer[]`), tuples (`int * Customer`), functions (`int -> Customer`),
// units of measure (`float<kg>`). Each token is then admitted only if the file
// DECLARES a type of that name, so `option`, `list`, `seq` and `Map` — all of
// them FSharp.Core, none of them declared here — fall out with no blocklist to
// maintain.
//
// The order of operations is load-bearing and is arm D's argument transplanted:
// a PRIMITIVE is collected as a candidate and refused by the declaration gate,
// never by a blocklist. `int`, `string` and `float` are type ABBREVIATIONS in
// F#, not keywords, and are legally shadowable by a same-file
// `type string = ...`. A blocklist would refuse the one edge that should exist
// there. Both directions are graded:
// TestFSharpFieldTypeRefs_PrimitiveFieldsProduceNoEdge and
// TestFSharpFieldTypeRefs_ShadowedPrimitiveAbbreviationIsATarget.
//
// A QUALIFIED NAME IS SKIPPED WHOLE, NEVER REDUCED TO ITS LAST SEGMENT
//
// `Domain.Customer` is tokenised as ONE token containing a dot and dropped.
// Taking the trailing segment is the failure arm D found in go's
// resolveTypeReferences: `other.Order` reduced to `Order` binds to a same-file
// `Order`, a WRONG binding, and a wrong binding never reaches `bug-extractor`
// because it binds. F# has the identical hazard — `Domain.Customer` where the
// file also declares its own `Customer` — and declines it the same way. Pinned
// by TestFSharpFieldTypeRefs_QualifiedTypeIsNotStrippedToASameFileName.
//
// A GENERIC TYPE PARAMETER IS SKIPPED WHOLE for the same reason: `'T` is
// consumed leading-quote-first so it can never re-enter the scanner as the
// bare token `T` and bind to a same-file `type T`. Arm D shipped that exact
// over-fire as a known-wrong case because Go's `T` is indistinguishable from a
// type name; F#'s leading apostrophe makes it distinguishable, so this arm
// closes it instead of pinning it.
//
// RECORDS ONLY — THE DU CASE IS A DIFFERENT PROPOSITION, DELIBERATELY DECLINED
//
// du_record_members.go emits DU cases through the same function, as
// SCOPE.Schema/du_case, and `| Placed of Order` does put a type name in
// `member_type`. This pass still refuses them, on Subtype, for two separate
// reasons either of which is sufficient:
//
//   - IT IS NOT THE SAME RELATION. A record field IS a member with a declared
//     type: `Order.Buyer : Customer` means every Order has a Customer. A DU
//     case is a CONSTRUCTOR — `Status.Placed of Customer` means a Status may be
//     built FROM a Customer, and only in that one alternative. Emitting it
//     under `ref_kind=field_target_type` would make one query return two
//     different facts, which is #6902's defect family. If the DU-case relation
//     is worth having it wants its own ref_kind and its own arm.
//   - THE DATUM IS LOSSIER. splitDUCase takes everything after ` of `
//     verbatim, so a named-field case `| Named of who: Customer * qty: int`
//     arrives with the FIELD LABELS still in it (probed). Tokenising that
//     yields `who` and `qty` as candidates alongside the real types — harmless
//     under the declaration gate today, but it is inference on a parse that was
//     never built to carry types.
//
// Graded rather than asserted: TestFSharpFieldTypeRefs_DUCasePayloadGetsNoEdge
// uses a payload type that IS declared in the file and IS admitted by the
// allow-list, so the case fails if the Subtype guard is removed, rather than
// passing because nothing would have bound anyway.
//
// A NON-BINDING EDGE IS WORSE THAN NO EDGE. An unresolved stub is kept verbatim
// and dangling and classified `bug-extractor`, so the two rules below are both
// about declining to emit rather than about recall.

// fsharpFieldTargetRefKind is the value of the `ref_kind` edge property. It
// matches arm A's csFieldTargetRefKind, arm B's protoFieldTargetRefKind, arm
// C's rustFieldTargetRefKind, arm D's goFieldTargetRefKind and the custom
// lane's referencesClassEdge verbatim — the discriminator is what makes the
// edge queryable as a field→declared-type edge across languages, so it must be
// one string. Arm D established there is no shared constant and no cross-arm
// agreement test, and that this is deliberate: five independent literals
// asserted in five packages enforce agreement transitively, where a shared
// constant would be one grader instead of five.
const fsharpFieldTargetRefKind = "field_target_type"

// fsharpFieldTypeCandidates returns every bare identifier written in an F# type
// expression, in source order and without deduplication.
//
// It is a scanner, not a parser, and that is the point: it does not model
// `option`, `list`, `<>`, `*`, `->` or `[]` at all, so there is no wrapper list
// to keep in sync with F#'s type syntax and no postfix/prefix asymmetry to get
// wrong. Every named type in the expression is a bare identifier; every bare
// identifier becomes a candidate; the declaration gate in
// fsharpInFileTypeTargets decides which are real.
//
// TWO token shapes are consumed WHOLE and discarded, and consuming them WHOLE
// is what makes them safe — a scanner that merely skipped the leading
// character would re-enter mid-token and produce exactly the bare name the
// skip exists to prevent:
//
//   - A DOTTED token (`Domain.Customer`, `System.Collections.Generic.List`).
//     `.` is scanned as an identifier continuation character precisely so the
//     whole qualified name arrives as one token and can be rejected as one.
//   - A token opening with `'` (`'T`, `'TKey`) — an F# generic type parameter.
//
// A field type is at most a few dozen characters, so the scan is linear in the
// property length with no allocation beyond the result slice.
func fsharpFieldTypeCandidates(typ string) []string {
	var out []string
	r := []rune(typ)
	i := 0
	for i < len(r) {
		switch {
		case r[i] == '\'':
			// Generic type parameter — consume the apostrophe AND the name.
			i++
			for i < len(r) && isFSharpIdentRune(r[i]) {
				i++
			}
		case isLetter(r[i]) || r[i] == '_':
			j := i
			for j < len(r) && (isFSharpIdentRune(r[j]) || r[j] == '.') {
				j++
			}
			if tok := string(r[i:j]); !strings.ContainsRune(tok, '.') {
				out = append(out, tok)
			}
			i = j
		default:
			i++
		}
	}
	return out
}

// isFSharpIdentRune reports whether r may appear in the interior of an F#
// identifier. The apostrophe is included (`x'` is a legal F# name), which is
// also why a LEADING apostrophe must be consumed by the generic-parameter
// branch above rather than merely skipped.
func isFSharpIdentRune(r rune) bool {
	return r == '_' || r == '\'' || isLetter(r) || (r >= '0' && r <= '9')
}

// fsharpFieldTypeTarget is one in-file type declaration a field-type edge may
// address: the structural ToID that binds to it, and the bare name for the
// `target_type` property.
type fsharpFieldTypeTarget struct {
	toID string
	name string
}

// fsharpTypeDeclKinds is the target ALLOW-LIST, keyed "<Kind>/<Subtype>".
//
// It is an allow-list and not a denylist because F# emits several BARE-NAMED
// records that are not type declarations, and a future producer adding another
// one must be refused by default. Enumerated from this package's own emit
// sites rather than inherited:
//
//	ADMITTED — every subtype classifyTypeSubtype can return for a `type`
//	declaration (extractor.go:503, :540-570), plus detectCEBuilder's
//	re-subtyping and applyElmishFeliz's two re-KINDINGS:
//
//	  SCOPE.Component/record               type T = { ... }
//	  SCOPE.Component/discriminated_union  type T = A | B
//	  SCOPE.Component/interface            type T = interface ... / IFoo
//	  SCOPE.Component/class                type T() = class ...
//	  SCOPE.Component/struct               [<Struct>] type T = ...
//	  SCOPE.Component/alias                type Id = int          (#4942)
//	  SCOPE.Component/type                 classifyTypeSubtype catch-all
//	  SCOPE.Component/computation_builder  detectCEBuilder (#5048)
//	  SCOPE.Model/elmish_model             the `Model` RECORD, re-kinded
//	  SCOPE.Event/elmish_msg               the `Msg` DU, re-kinded
//
// The last two are why this map is keyed on Kind AND Subtype rather than
// testing `Kind == "SCOPE.Component"`. applyElmishFeliz (elmish_feliz.go:104,
// :109) re-kinds the Model record and the Msg DU in any file that opens
// Elmish/Feliz/Fable, and `State: Model` is then the single most common
// declared-type relation in an Elmish app. Arm C's `Kind != "SCOPE.Component"`
// guard would drop both — the same shape as the 19% of Go edges it would have
// dropped there. Graded by TestFSharpFieldTypeRefs_ElmishModelAndMsgAreTargets,
// which fails when those two rows alone are deleted.
//
// THE CALL PLACEMENT IS LOAD-BEARING TOO — BUT THROUGH RULE 2, NOT THIS
// ALLOW-LIST. This paragraph has been wrong twice in opposite directions and
// the sequence is worth keeping, because the second error is the rarer one.
//
// The first draft said the after-applyElmishFeliz placement was load-bearing
// BECAUSE the allow-list needs the re-kinded rows. A three-row experiment
// refuted that justification:
//
//	drop the two rows, keep the placement          -> DEAD (1 test)
//	move the call before applyElmishFeliz,
//	  keep the two rows                            -> ALIVE  <- fixture-bound
//	move the call AND drop the two rows            -> ALIVE  <- fixture-bound
//
// Before the re-kind, `Model` is SCOPE.Component/record and `Msg` is
// SCOPE.Component/discriminated_union — both already admitted — so on THAT
// input the two arrangements agree, and the allow-list explanation is indeed
// wrong. The second draft then RETRACTED THE CLAIM ALONG WITH ITS REASON. That
// over-corrected: the claim survives on a different mechanism, and withdrawing
// it left a reachable dangling-edge mutant ungraded.
//
// The distinguishing input is the commonest module name in an Elmish app:
//
//	module App
//	open Elmish
//	open Foo.Model      // import placeholder, SCOPE.Component, named "Model"
//	type Model = { Count: int }
//	type Env = { State: Model }
//
//	as shipped (read AFTER the re-kind)  -> 0 edges
//	call moved BEFORE the re-kind        -> 1 edge, DANGLING through
//	                                        BuildIndex -> ReferencesEmbedded
//
// The separator is RULE 2. Read after the re-kind, `Model` denotes two kinds
// (SCOPE.Model/elmish_model + SCOPE.Component/import), so rule 2 refuses. Read
// before, both records are SCOPE.Component — one kind — so rule 2 is silent,
// the allow-list admits, and the emitted stub cannot resolve: Component and
// Model are BOTH in componentKindFamily, so lookupLocationKind finds no unique
// answer and ambigLocation blanks it. Graded by
// TestFSharpFieldTypeRefs_PlacementAfterTheElmishRekindIsLoadBearing.
//
// So: the two allow-list rows are graded (each individually — deleting either
// one alone kills), AND the placement is graded, and they are graded by
// DIFFERENT rules. Recorded at length because "the justification failed, so the
// claim falls" is the wrong inference — re-derive whether the claim still holds
// before retracting it.
//
//	REFUSED, and each of the three refusals is a distinct hazard:
//
//	  SCOPE.Component/import    — THE LIVE KILL. buildImportEntities
//	    (extractor.go:678) mints one Component per `open`, named
//	    importDisplayName(mod), which is the LAST SEGMENT of the module path.
//	    So `open Acme.Customer` puts a Component literally named `Customer` in
//	    this file. resolve/refs.go filters import placeholders out of
//	    indexByName (refs.go:1611) but NOT out of byLocation/byLocationKind
//	    (refs.go:1271-1317, no such test), and byLocation is precisely the
//	    fallback a component-space structural ref lands in. So without this
//	    refusal a field typed `Customer` in a file that opens `Acme.Customer`
//	    and declares no `Customer` BINDS TO THE IMPORT PLACEHOLDER. It is a
//	    wrong binding, not a dangle, so nothing downstream would report it.
//	    F# is more exposed than Go here, where the import record's Name is the
//	    whole path: truncation to a segment is what makes the collision
//	    ordinary rather than contrived. Graded by
//	    TestFSharpFieldTypeRefs_ImportPlaceholderIsNeverATarget, whose file
//	    declares NO type of that name so rule 2 cannot mask the allow-list.
//	  SCOPE.Component/module and /namespace — also bare-named
//	    (extractor.go:267, :293), and `module Order` beside `type Order` is the
//	    F# companion-module idiom, so the collision is the norm rather than an
//	    edge case. It costs nothing today because the two records share Kind
//	    AND Name AND file, hence ONE graph node under graph.EntityID, so the
//	    admitted record supplies the same ToID either way — but a module with
//	    NO companion type is a bare name with nothing behind it, and that is
//	    the case the refusal is for.
//	  SCOPE.Operation, SCOPE.Pattern, SCOPE.UIComponent, SCOPE.Component/file,
//	    SCOPE.Schema/{field,du_case,active_pattern_case} — a value, a pattern,
//	    a UI function, the file carrier, and the sub-entity populations. None
//	    is a type. The three SCOPE.Schema subtypes are additionally unreachable
//	    by name shape (their Names are dotted and a candidate token never
//	    contains a dot), and that is claimed as nothing more than belt.
var fsharpTypeDeclKinds = map[string]bool{
	"SCOPE.Component/record":              true,
	"SCOPE.Component/discriminated_union": true,
	"SCOPE.Component/interface":           true,
	"SCOPE.Component/class":               true,
	"SCOPE.Component/struct":              true,
	"SCOPE.Component/alias":               true,
	"SCOPE.Component/type":                true,
	"SCOPE.Component/computation_builder": true,
	"SCOPE.Model/elmish_model":            true,
	"SCOPE.Event/elmish_msg":              true,
}

// fsharpInFileTypeTargets indexes every TYPE DECLARED in this file that a
// field-type edge may address, keyed by bare name.
//
// Two rules:
//
//  1. The record must be an admitted type declaration per fsharpTypeDeclKinds.
//
//  2. A name that denotes MORE THAN ONE DISTINCT GRAPH NODE in this file is
//     dropped, and the count spans EVERY record in the file rather than only
//     the admitted ones. Arm C's rule (count only within the admitted set)
//     would not see such a collision; #7038 is filed against it for that.
//
//     IT IS A CONSERVATIVE APPROXIMATION OF THE RESOLVER'S RULE, NOT A MIRROR
//     OF IT, and the earlier draft of this comment claimed otherwise: it said
//     that a second kind means statusAmbiguous so "emitting would GUARANTEE a
//     dangling stub". That premise is false in the direction the rule is
//     graded. Measured by hand-emitting the refused edge on this arm's own
//     shadow fixture and driving it through BuildIndex -> ReferencesEmbedded:
//
//     rival SCOPE.Operation/let beside SCOPE.Component/record  -> BINDS, correctly
//     rival SCOPE.Component/import beside SCOPE.Model          -> DANGLES
//
//     The separator is the KIND FAMILY. refs.go:2982 tries
//     lookupLocationKind(file, name, structuralKindFamilies("component"))
//     BEFORE the kind-agnostic byLocation tier that ambigLocation guards, so a
//     rival OUTSIDE componentKindFamily is simply not weighed and the edge
//     resolves. The rule is therefore genuinely load-bearing only when the
//     rival kind is INSIDE that family — Component/View/Model, the case rule 2
//     shares with the call-placement experiment above — and it silently costs
//     recall for every out-of-family rival (SCOPE.Operation, SCOPE.Pattern, the
//     SCOPE.Schema sub-populations).
//
//     The over-strictness is KEPT here rather than narrowed: mirroring the
//     family table at the emit site is real coupling to the resolver's internal
//     tiering and deserves its own decision across arms C/D/E, not a rider on
//     this one. What is fixed is the stated reason. The general form — count
//     the kinds the resolver TIER THAT RESOLVES YOUR ADDRESS actually weighs —
//     is recorded on #6912.
//
//     THE UNIT IS THE KIND, NOT THE RECORD, and that too is the resolver's own
//     rule rather than a convenience: graph.EntityID hashes
//     (repo, Kind, Name, SourceFile) with SUBTYPE EXCLUDED, so within one file
//     two records collide into ONE node exactly when they share a Kind.
//     Counting RECORDS would refuse edges that bind perfectly well — the F#
//     companion-module idiom `type Order = {...}` beside `module Order` is two
//     records, both SCOPE.Component/Order in this file, and ONE node. Counting
//     KINDS keeps that edge and still drops the genuinely-two-node cases (a
//     type sharing a name with a SCOPE.Pattern or a SCOPE.Operation). BOTH
//     directions are graded:
//     TestFSharpFieldTypeRefs_CompanionModuleSharingATypeNameBinds is the
//     record-count mutant's kill, and
//     TestFSharpFieldTypeRefs_TypeShadowedByAnotherKindGetsNoEdge is the
//     no-rule mutant's, with a positive control in the SAME fixture so it
//     cannot pass by the target never working at all.
func fsharpInFileTypeTargets(records []types.EntityRecord, filePath string) map[string]fsharpFieldTypeTarget {
	// Pass 1 — how many DISTINCT GRAPH NODES each same-file name denotes,
	// target or not.
	nameKinds := make(map[string]map[string]bool)
	for i := range records {
		r := &records[i]
		if r.SourceFile != filePath || r.Name == "" {
			continue
		}
		if nameKinds[r.Name] == nil {
			nameKinds[r.Name] = make(map[string]bool)
		}
		nameKinds[r.Name][r.Kind] = true
	}

	// Pass 2 — admit the type declarations whose name is unambiguous.
	targets := make(map[string]fsharpFieldTypeTarget)
	for i := range records {
		r := &records[i]
		if r.SourceFile != filePath || r.Name == "" {
			continue
		}
		if !fsharpTypeDeclKinds[r.Kind+"/"+r.Subtype] {
			continue
		}
		if len(nameKinds[r.Name]) > 1 {
			continue
		}
		targets[r.Name] = fsharpFieldTypeTarget{
			toID: extractor.BuildComponentStructuralRef("fsharp", filePath, r.Name),
			name: r.Name,
		}
	}
	return targets
}

// attachFSharpFieldTypeRefs appends one REFERENCES edge per (record field,
// in-file declared type) pair.
//
// SAME FILE ONLY. A field whose type is declared in another .fs file gets
// nothing. Closing that needs the cross-file view pass 1 has none of, and
// binding a bare type name across files is the exact hazard #6976/#6369 are
// about — #6369 was an F# defect, so this package has already paid for that
// lesson once. Separate arm, not a widening of this one.
//
// FromID is left empty so graph assembly anchors the edge on the field record
// that carries it — the Django precedent at
// internal/extractors/python/django_relational.go:246-252, and arms B, C and D.
//
// Relationships is APPENDED to, never assigned: the field record already
// carries none, but a later producer's edge must not be clobbered. (The
// owner→member CONTAINS lives on the OWNER's record, not here.)
//
// It must run after every producer that can create or re-kind a same-file
// record, which is why extractFSharp calls it on the fully assembled slice
// INCLUDING the file carrier — see the placement comment at the call site.
func attachFSharpFieldTypeRefs(records []types.EntityRecord, filePath string) []types.EntityRecord {
	targets := fsharpInFileTypeTargets(records, filePath)
	if len(targets) == 0 {
		return records
	}
	for i := range records {
		r := &records[i]
		// Subtype "field" ONLY — never du_case, never active_pattern_case; see
		// the "RECORDS ONLY" section of the header block.
		if r.Kind != "SCOPE.Schema" || r.Subtype != "field" || r.SourceFile != filePath {
			continue
		}
		declared := r.Properties["member_type"]
		if declared == "" {
			continue
		}
		owner := r.Properties["parent_class"]
		fieldName := r.Properties["member_name"]
		emitted := make(map[string]bool)
		for _, cand := range fsharpFieldTypeCandidates(declared) {
			t, ok := targets[cand]
			if !ok || emitted[t.toID] {
				continue
			}
			// A field never targets its own owning type: `type Node = { Next:
			// Node option }` is a self-reference the CONTAINS edge already
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
					{K: "ref_kind", V: fsharpFieldTargetRefKind},
					{K: "target_type", V: t.name},
				},
			})
		}
	}
	return records
}
