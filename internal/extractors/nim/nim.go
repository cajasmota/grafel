// Package nim implements a regex-based extractor for Nim source files.
//
// Extracted entities:
//   - proc/func/method/converter/template/macro declarations → Kind="SCOPE.Operation", Subtype="proc"
//   - type declarations (object, ref object, enum, tuple, distinct) → Kind="SCOPE.Component"
//   - IMPORTS edges for `import` and `include` statements
//   - CALLS edges for proc invocations inside bodies
//   - CONTAINS edges from type→method (method/proc attached to a type)
//
// No tree-sitter grammar for Nim is bundled in smacker/go-tree-sitter, so
// this extractor parses Nim with regular expressions. Nim is
// whitespace/indent-sensitive but for entity discovery purposes we only
// need to detect top-level declarations and their bodies via indentation
// heuristics (similar to how the fish extractor handles function bodies).
//
// Registers itself via init() and is imported by registry_gen.go.
package nim

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("nim", &Extractor{})
}

// Extractor implements extractor.Extractor for Nim.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "nim" }

// Patterns for Nim syntax.
var (
	// proc/func/method/converter/template/macro declarations.
	// Nim proc signatures: proc name*(params): ReturnType =
	// or just: proc name(params) =
	// Also handles template, macro, converter, iterator, func keywords.
	procRE = regexp.MustCompile(
		`(?m)^([ \t]*)(?:proc|func|method|converter|template|macro|iterator)\s+` +
			`([a-zA-Z_\x{0080}-\x{FFFF}][a-zA-Z0-9_\x{0080}-\x{FFFF}]*\*?)\s*` +
			`(?:\[[^\]]*\])?\s*` + // optional generic params
			`(\([^)]*\))?\s*` + // optional params
			`(?::\s*[^\n]+?)?\s*` + // optional return type annotation
			`(?:\{[^}]*\})?\s*=`, // optional pragma block e.g. {.async.}
	)

	// type block declarations — two forms:
	//   1. type block:  "  Name = object"  (indented under 'type')
	//   2. inline type: "type Name = object" (same line)
	// Handles optional export marker (*) and generic params ([T]).
	//
	// #7190: the `type` keyword prefix is `type[ \t]+`, NOT `type\s+`. `\s`
	// matches a NEWLINE, so for form 1 the match used to START on the block
	// keyword's line — which made the first member's StartLine the keyword's
	// line and left group 1 (the indent) holding the KEYWORD's indentation
	// instead of the declaration's. Both of #7190's symptoms came from that one
	// wrong anchor. Group 1 is the DECLARATION's own indent and is what the
	// call site passes to extractIndentBody as baseIndentLen; it was hard-coded
	// 0 there, which is right only for a declaration at column 0.
	//
	// #7213: a member carrying a PRAGMA produced no entity at all. Only a generic
	// parameter list was admitted between the name and the `=`, and a pragma sits
	// in exactly that position, so `Alpha* {.packed.} = object` never matched.
	// Measured over 4431 .nim files (nim-lang/Nim, nimbus-eth2, pixie, nitter,
	// jester): 846 of 6987 type members — 12.1% — were dropped for this reason.
	// (Every figure in this file and its test counts the FINAL pattern, the one
	// with `\.?\}`; the pre-plain-close numbers were 839 of 6980.)
	//
	// The pragma group follows the generic group and never precedes it:
	// doc/grammar.txt gives `typeDef = identVisDot genericParamList? pragma?
	// ('=' optInd typeDefValue)?`, and nim-lang/Nim's own
	// tests/types/told_pragma_syntax2.nim asserts the reverse order is a compile
	// error. Its body is `[^}\n]*`, NOT `[^}]*`: `[^}]` matches a NEWLINE, so an
	// unterminated `{.` would run through every following declaration to the next
	// `.}` and absorb them. The cost of that choice is the multi-line pragma
	// (361 further sites, 4.9%), left unmatched deliberately.
	//
	// THE CLOSING DELIMITER IS `\.?\}`, NOT `\.\}` — the dot is optional. Nim's
	// compiler/parser.nim, parsePragma, accepts either token:
	//
	//	while p.tok.tokType notin {tkCurlyDotRi, tkCurlyRi, tkEof}: ...
	//	if p.tok.tokType in {tkCurlyDotRi, tkCurlyRi}: getTok(p)
	//
	// and doc/grammar.txt states it as `pragma = '{.' optInd (exprColonEqExpr
	// comma?)* optPar ('.}' | '}')`. 7 sites in the population close with a
	// plain `}` (`MyObject {.exportc: "ExtObject"} = object`). The OPENING
	// delimiter has no such latitude: the lexer has one token, tkCurlyDotLe, so
	// `{` alone never opens a pragma and neither does `{ .` — the two
	// characters must be adjacent.
	typeRE = regexp.MustCompile(
		`(?m)^([ \t]*)(?:type[ \t]+)?([A-Z][a-zA-Z0-9_]*\*?)\s*(?:\[[^\]]*\])?[ \t]*(?:\{\.[^}\n]*\.?\})?\s*=\s*(object|ref\s+object|enum|tuple|distinct\s+\w+)`,
	)

	// typeBlockStartRE marks the start of a "type" keyword block (unused but kept for documentation)
	typeBlockRE = regexp.MustCompile(`(?m)^[ \t]*type\s*$|(?m)^[ \t]*type\s+`)

	// import statement: import module1, module2; import module1/sub
	importRE = regexp.MustCompile(
		`(?m)^[ \t]*import\s+([^\n#]+)`,
	)

	// include statement: include module
	includeRE = regexp.MustCompile(
		`(?m)^[ \t]*include\s+([^\n#]+)`,
	)

	// from X import Y
	fromImportRE = regexp.MustCompile(
		`(?m)^[ \t]*from\s+(\S+)\s+import\s`,
	)

	// call site: identifier( or identifier.method(
	callRE = regexp.MustCompile(
		`(?:^|[^\w.])([a-zA-Z_][a-zA-Z0-9_]*)(?:\.[a-zA-Z_][a-zA-Z0-9_]*)?\s*\(`,
	)
)

// nimKeywords are tokens that the call regex picks up but are not real calls.
var nimKeywords = map[string]bool{
	"if": true, "elif": true, "else": true, "when": true, "while": true,
	"for": true, "case": true, "of": true, "try": true, "except": true,
	"finally": true, "raise": true, "return": true, "yield": true,
	"break": true, "continue": true, "block": true, "defer": true,
	"proc": true, "func": true, "method": true, "iterator": true,
	"converter": true, "template": true, "macro": true,
	"type": true, "var": true, "let": true, "const": true,
	"import": true, "include": true, "from": true, "export": true,
	"discard": true, "echo": true, "and": true, "or": true, "not": true,
	"in": true, "notin": true, "is": true, "isnot": true,
	"addr": true, "cast": true, "nil": true, "true": true, "false": true,
	"object": true, "enum": true, "tuple": true, "ref": true, "ptr": true,
	"concept": true, "mixin": true, "bind": true, "using": true,
	"static": true, "asm": true, "emit": true,
	// built-in procs that are effectively keywords
	"new": true, "newSeq": true, "newString": true,
}

// Extract processes the Nim source and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	out := extractNim(string(file.Content), file.Path)
	// #6815: buildImportEntities anchors every IMPORTS edge on file.Path with
	// nothing carrying that string as its Name. Emit the #577 file carrier when
	// — and only when — such an edge exists. See extractor.FileCarrierFor.
	out = extractor.PrependFileCarrier(file.Path, "nim", out)
	extractor.TagRelationshipsLanguage(out, "nim")
	extractor.TagEntitiesLanguage(out, "nim")
	return out, nil
}

func extractNim(src, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord

	imports := collectImports(src)
	importEntities := buildImportEntities(filePath, imports)
	if len(importEntities) > 0 {
		entities = append(entities, importEntities...)
	}

	// 1. Proc/func/method/template/macro/iterator declarations.
	seen := make(map[string]bool)
	// procRE has exactly 3 capture groups (indent, name, the optional
	// `(\([^)]*\))?` params); the generic-params, return-type and pragma groups
	// are all non-capturing. FindAllStringSubmatchIndex returns 2*(1+n) = 8 ints
	// per match invariantly — the optional params group contributes a `-1,-1`
	// pair when absent rather than being omitted — so reading m[2]..m[7] needs
	// no arity guard. #7197 removed the unreachable `if len(m) < 7`. Adding a
	// fourth group changes this invariant, not the guard.
	//
	// The `m[6] >= 0 && m[7] >= 0` test below is NOT the same kind of thing and
	// must not be removed with it: the params group is OPTIONAL, so on a
	// parenless routine (`proc main =`) it is exactly the `-1,-1` pair, and an
	// unconditional src[m[6]:m[7]] panics with a slice-bounds error. #7197 found
	// that guard ungraded — no fixture in this package contained a parenless
	// routine, so deleting it left the suite green — and added
	// params_group_7197_test.go, which makes its removal RED. The copy of this
	// guard in the type loop's proc re-scan is graded there separately.
	for _, m := range procRE.FindAllStringSubmatchIndex(src, -1) {
		indent := src[m[2]:m[3]]
		name := strings.TrimSuffix(src[m[4]:m[5]], "*") // strip export marker
		params := ""
		if m[6] >= 0 && m[7] >= 0 {
			params = src[m[6]:m[7]]
		}
		// #7231, NOTED NOT FIXED — filed as #7256. The trim above is
		// load-bearing HERE TOO, and nothing grades it: mutant CM-33 (key on
		// the UNTRIMMED `indent + ":" + src[m[4]:m[5]]`) is ALIVE against this
		// whole package — vet clean, suite green. Under it `proc foo*` and
		// `proc foo` at one indent become two records, both still NAMED `foo`
		// since Name is built from the trimmed variable, which fold to one
		// graph node whose relationship loop runs OUTSIDE the fold, so the
		// second record's CALLS are unioned onto the survivor.
		//
		// THAT IS A WEAKER CASE THAN THE EXTENDS DEFECT #7231 FIXED, and the
		// difference matters to whoever picks up #7256. In legal Nim two
		// routines sharing a name at one indent must differ in signature, so
		// they are OVERLOADS, and the folded `foo` node legitimately stands for
		// all of them — identity carries no signature. A unioned CALLS edge is
		// therefore a true statement about *a* `foo`, not a fabrication. The
		// harm is INCONSISTENCY: which overloads survive depends on whether
		// their export markers happen to differ. The EXTENDS case was
		// different in kind — there the survivor asserted a base that no
		// surviving declaration stated.
		key := indent + ":" + name
		if seen[key] {
			continue
		}
		seen[key] = true

		startLine := strings.Count(src[:m[0]], "\n") + 1
		body := extractIndentBody(src, m[1], len(indent))
		// #7212: measured from m[1], the same offset `body` starts at — NOT
		// from startLine, which is m[0]'s line. A wrapped parameter list makes
		// the match span lines and the two offsets diverge.
		endLine := spanEndLine(src, m[1], body)
		calls := collectCalls(body, name)

		sig := buildSig(src[m[0]:m[1]], name, params)

		// Determine if this is a top-level proc or a method on a type.
		subtype := "proc"
		// Check if the keyword is "method" — Nim methods are dispatched on types
		kw := extractKeyword(src[m[0]:m[1]])
		if kw == "method" || kw == "template" || kw == "macro" || kw == "iterator" {
			subtype = kw
		}

		entities = append(entities, types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Operation",
			Subtype:            subtype,
			SourceFile:         filePath,
			Language:           "nim",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          sig,
			EnrichmentRequired: false,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
			Relationships: calls,
		})
	}

	// 2. Type declarations — objects, enums, tuples.
	//
	// #7231: THERE IS NO NAME-KEYED DEDUP HERE ANY MORE. It used to be
	// `typeSeen[name]`, first-match-wins, which discarded one record per extra
	// declaration of a name in a file.
	//
	// READ THE NEXT PARAGRAPH BEFORE QUOTING ANY NUMBER BELOW. Every figure in
	// this comment is an EXTRACTOR-LEVEL count — records in the slice
	// extractNim returns. It is NOT graph recall, and #7231 originally claimed
	// it as such. `graph.EntityID(repo, kind, name, sourceFile)`
	// (internal/graph/graph.go:259) does not hash StartLine, so two records
	// that share Kind+Name+SourceFile derive the SAME id, and BOTH assembly
	// seams fold them: cmd/grafel/index.go:6303 keeps the first and drops the
	// rest under `if !seenEntity[id]`, and internal/extractors/incremental.go's
	// convertExtractedRecords does the same via entityRecordToGraphEntity
	// (:2046, the identical derivation) — its own doc calls the collision
	// "EXPECTED, not erroneous". The subprocess path
	// (internal/daemon/extract/subproc.go:358) is a transport: its envelopes
	// decode into extract.Coordinate's Result.Entities, which index.go:1824
	// assigns to pass1Records and feeds to the SAME assembly loop.
	//
	// THAT LIST IS NOT EXHAUSTIVE, and an earlier revision of this comment said
	// it was. `extractors.MergeWithCustom` (internal/extractors/custom_dispatch.go)
	// consumes base language-extractor records BEFORE the assembly fold — from
	// cmd/grafel/index.go's custom-extractor dispatch and from the incremental
	// twin in incremental.go — and it does NOT fold: it iterates baseEntities
	// and appends every one, keyed on `identityKey{SourceFile, Kind, Name}`.
	// The lane is live for nim specifically (internal/custom/nim/ ships several
	// extractors).
	//
	// The conclusion survives it, for a reason worth writing down rather than
	// assuming: `EntityRecord.ComputeID` (internal/types/entity.go) omits
	// StartLine too, so both duplicates share an effectiveID, the enriched
	// record is the lowest-line one and survives the later assembly fold, and
	// the #4406 gap-fill only fills EMPTY fields. So the extra entities below
	// still DO NOT REACH THE GRAPH — but they do pass through one seam that
	// does not fold, and anyone reasoning about a second such seam should start
	// from that, not from a claim of completeness.
	//
	// Whether entity identity should carry a line component is a separate
	// question with a much larger blast radius and is filed on its own; do not
	// answer it here.
	//
	// What the change is worth, then, stated honestly: the records the
	// extractor returns now describe the file, the two halves of the fold rule
	// are written down and graded, and an unreachable map is gone. Recall is
	// NOT among it.
	//
	// UNPINNED MEASUREMENT, 2026-09-18, over a 4431-file Nim population
	// (nim-lang/Nim, nimbus-eth2, pixie, nitter, jester) — `archigraph-corpora`
	// contains zero `.nim` files, so nothing in this tree asserts these and
	// they will drift: type RECORDS 6572 -> 6987 (+415); CONTAINS records 8597,
	// unchanged byte for byte; EXTENDS records 885, unchanged byte for byte.
	// 221 of the 255 duplicated (file,name) groups — 86.7% — are declarations
	// inside DIFFERENT routine bodies (proc/template/macro/block/static), i.e.
	// genuinely distinct coexisting types in disjoint scopes rather than
	// competing descriptions of one type; only 17 are `when`-branch conditional
	// compilation, and only 1 of those 17 differs in Subtype.
	//
	// #7231 was directed as "key the dedup on name + StartLine". That key is
	// INJECTIVE OVER typeRE's MATCHES and therefore can never suppress one:
	// typeRE is `(?m)^…`-anchored, so every match starts at a line start, and
	// FindAllStringSubmatchIndex returns successive NON-OVERLAPPING matches
	// left to right, so match k+1 begins at a line strictly after match k's.
	// No two matches can share a start line, so no two can share the key —
	// note `startLine` is itself computed from `m[0]`, so the invariant and the
	// key are the same quantity. The compound-key form was built and measured
	// against this one over the same population: byte-identical component,
	// CONTAINS and EXTENDS dumps, and a probe printing every suppression
	// printed none. Keeping the map would be a branch no input can reach — the
	// same thing #7197 removed from this file twice. It is stated here instead.
	//
	// The proc loop above keeps ITS dedup (the `key := indent + ":" + name` in
	// the procRE loop — named, not cited by line: this comment said ":181" and
	// the 13-line CM-33 note inserted just above that key in the same commit
	// series moved it, which is the whole reason line citations inside comments
	// are not used here). That is an OBSERVATION about what this file does,
	// not an endorsement: the key
	// is not injective, and the consequence is that three same-indent `show`
	// overloads yield one record, so overloads 2 and 3 are deleted exactly the
	// way type declarations used to be. Whether that is right is not decided
	// here and is not this change's to decide.
	//
	// DELIBERATELY NOT DONE HERE, AND A HAZARD THE MOMENT IDENTITY CHANGES:
	// `local_scope=true` is NOT stamped on the routine-body-nested
	// declarations. #7231 designed it as part of this change and it is absent.
	// internal/mcp/denoise.go maps that property to `noiseLocalScope`, which
	// ranks strictly below every other noise bucket (#6716/#6751), so the
	// stamp is what would keep `grafel_find`'s default surface clean. Omitting
	// it is inert TODAY only because the fold above deletes those records
	// before any surface sees them. If entity identity ever gains a line
	// component, the ~360 routine-local declarations (UNPINNED MEASUREMENT,
	// 2026-09-18, same corpus as the block above — nothing in this tree asserts
	// it) arrive at FULL RANK on the default surface, all at once. Whoever changes
	// identity owns this stamp; it is not a follow-up that can be scheduled
	// independently of that change.
	//
	// firstDeclSeen is the one thing that still needs per-name state — see the
	// gate below.
	firstDeclSeen := make(map[string]bool)
	// typeRE has exactly 3 capture groups (indent, name, kind alternation); the
	// `(?:type[ \t]+)?` prefix, the generics and the pragma block are all
	// non-capturing. FindAllStringSubmatchIndex returns 2*(1+n) = 8 ints per
	// match invariantly — a non-participating group contributes a `-1,-1` pair
	// rather than being omitted — so reading m[2]..m[7] below needs no arity
	// guard. #7197 removed the unreachable `if len(m) < 8`. Adding a fourth
	// group changes this invariant, not the guard. All three groups here are
	// mandatory, so none of them can be the `-1,-1` pair — unlike procRE's
	// optional params group, whose participation test is load-bearing.
	for _, m := range typeRE.FindAllStringSubmatchIndex(src, -1) {
		indent := src[m[2]:m[3]] // #7190: the DECLARATION's own indent
		name := strings.TrimSuffix(src[m[4]:m[5]], "*")
		kind := src[m[6]:m[7]]

		startLine := strings.Count(src[:m[0]], "\n") + 1

		// Determine subtype from the kind clause.
		subtype := "object"
		if strings.HasPrefix(kind, "ref") {
			subtype = "ref object"
		} else if kind == "enum" {
			subtype = "enum"
		} else if kind == "tuple" {
			subtype = "tuple"
		} else if strings.HasPrefix(kind, "distinct") {
			subtype = "distinct"
		}

		// #7190: the base is the declaration's OWN column. It was hard-coded 0,
		// which is right only when the declaration sits at column 0 — the shape
		// every pre-existing fixture happened to use. In an idiomatic `type`
		// SECTION the members are indented, so at 0 every following sibling was
		// "more indented than the declaration" and got absorbed into the first
		// member's body.
		body := extractIndentBody(src, m[1], len(indent))
		// #7212: same origin as `body`. A `= object` clause broken after the
		// `=` or after `ref` puts m[1] lines below m[0], and those lines used
		// to fall into neither term of the sum.
		endLine := spanEndLine(src, m[1], body)

		var rels []types.RelationshipRecord

		// #7231: EVERY EDGE THIS LOOP OWNS — EXTENDS AND CONTAINS ALIKE — IS
		// EMITTED ONLY FROM THE FIRST DECLARATION OF A NAME IN THIS FILE. Not
		// from the first declaration overall, and not from one chosen per
		// (name, subtype): per NAME, because the name is what identity folds on
		// downstream and what the CONTAINS scan keys on.
		//
		// WHY THE GATE COVERS EXTENDS TOO, WHICH IS THE WHOLE REASON THIS IS A
		// FIX AND NOT A TIDY-UP. Dropping the name dedup above does not add
		// nodes to the graph — both assembly seams fold the records by an id
		// that omits StartLine (see the note at the top of this loop). But
		// NEITHER SEAM PUTS ITS RELATIONSHIP LOOP INSIDE THE FOLD.
		// cmd/grafel/index.go:6446 walks `r.Relationships` outside the
		// `!seenEntity[id]` block, and incremental.go's equivalent carries an
		// explicit comment saying it is "NOT inside the else", both defaulting
		// a blank FromID to the derived id. So a DROPPED duplicate's edges are
		// unioned onto the SURVIVOR. Ungated, two declarations of `Widget` with
		// different bases make the single surviving Widget node assert
		// `EXTENDS BaseA` AND `EXTENDS BaseB` — an edge no declaration in the
		// file states. CONTAINS escapes this only by accident: a duplicate's
		// CONTAINS shares FromID, ToID and Kind with the survivor's, so
		// index.go:6453's `seenRel[relID]` folds it. EXTENDS does not share
		// ToID, so nothing downstream collapses it. That asymmetry is the
		// defect; gating both here removes it at the source rather than relying
		// on a downstream dedup that only one of the two edge kinds triggers.
		//
		// With the gate, both edge sets are reproduced byte for byte against
		// the pre-#7231 extractor (see the unpinned measurement above: CONTAINS
		// 8597, EXTENDS 885, both unchanged).
		//
		// THE PRICE OF THE GATE, MEASURED RATHER THAN WAVED THROUGH. An earlier
		// revision claimed locally-scoped duplicates "carry no edges rather
		// than false ones". That is FALSE for one ordinary shape: when the
		// FIRST declaration of a name has no `of` clause and a LATER one does
		// — `type Dual = object` in one proc body, `type Dual = ref object of
		// RealBase` in another — the gate suppresses a TRUE edge, and the name
		// ends up with no base at all.
		//
		// Quantified over the same population (UNPINNED MEASUREMENT, 2026-09-19;
		// same corpus as the block above), by diffing the ungated part-1-only
		// EXTENDS dump against the gated one: of the 31 edges the gate
		// suppresses, 22 are duplicates or competitors on a name whose
		// surviving first declaration already states a base, and 9 — spread
		// over 8 distinct (file,name) pairs, all in nim-lang/Nim's own test
		// tree — are TRUE edges whose only source was a non-first declaration.
		// So #7231's claim that "the +31 were the manufactured edges, not new
		// recall" holds for 22 of 31 and NOT for the other 9.
		//
		// This is not a regression: before #7231 those later declarations were
		// discarded whole, so those 9 edges never existed, and the gated output
		// is byte-identical to the pre-#7231 extractor. It is a bound on what
		// the gate can ever deliver. A rule that preferred the first
		// declaration THAT HAS A BASE would recover the 9 while keeping the 22
		// suppressed, at the cost of the gate no longer being one predicate
		// shared by both edge kinds. That trade is deliberately not taken here
		// and is left stated for whoever revisits it.
		//
		// WHY THE CONTAINS SCAN IS NOT SCOPED TO THIS DECLARATION, AND CANNOT
		// BE. In Nim a "method" is a free-standing proc taking the type as its
		// FIRST PARAMETER, declared OUTSIDE the type body, so the scan is the
		// inner whole-file `procRE.FindAllStringSubmatchIndex(src, -1)` below
		// and the only thing it matches on is `name`. UNPINNED MEASUREMENT,
		// 2026-09-18, same corpus as the block at the top of this loop and
		// asserted by nothing in this tree: over all 9370 (type-declaration,
		// matching-proc) pairs in the population, pairs with the proc INSIDE
		// the declaration's span = 0, outside = 9370. Scoping
		// the scan to the span — the fix #7231 originally prescribed — would
		// take CONTAINS from 8597 to 0. (Attribution by ENCLOSING ROUTINE scope
		// is a real and different question; it needs routine-body spans this
		// loop does not compute, and is its own issue.)
		//
		// "First" means LOWEST BYTE OFFSET, and is read off the iteration order
		// of typeRE's FindAllStringSubmatchIndex — successive non-overlapping
		// matches, left to right — never off map iteration. firstDeclSeen is
		// read and written only along that ordered walk, once, here.
		//
		// THE KEY IS THE `*`-TRIMMED NAME, AND THE TRIM IS PART OF THE GATE.
		// `Widget*` and `Widget` are one Nim name — the export marker is
		// visibility on the declaration, not part of the identifier
		// (doc/grammar.txt: `identVis = symbol OPR?`) — and it is the trimmed
		// form that graph.EntityID hashes, so the gate key must agree with it.
		// Keying on the raw `src[m[4]:m[5]]` gives a file that declares
		// `Widget*` in one routine body and `Widget` in another TWO gate slots,
		// and every manufactured edge above comes back for that shape. That was
		// mutant CM-31 and it was ALIVE until nimExportMarkerDupFixture existed:
		// every duplicated name in the earlier fixtures was spelled
		// identically. The opposite error — normalising past the marker and
		// merging two real names — is scored on the same fixture (CM-32).
		//
		// The gate is keyed on `name`, so it fires ONLY for a name this file
		// declares more than once. A name declared once is untouched and keeps
		// every edge it had; that direction is the one an over-broad gate
		// breaks while every duplicate count stays at zero, and it is pinned
		// for BOTH edge kinds in first_decl_contains_7231_test.go.
		firstDecl := !firstDeclSeen[name]
		firstDeclSeen[name] = true

		// Inheritance: `= ref object of Base` (#6370). m[1] is the byte just
		// past the `object` keyword, which is the ONLY position an `of` can
		// mean inheritance in Nim — see hierarchy.go.
		if firstDecl {
			if ext := baseOfEdge(src, m[1], kind, name, startLine); ext != nil {
				rels = append(rels, *ext)
			}
		}
		// Find methods declared for this type (methods take first param of this type).
		if firstDecl {
			// PER DECLARATION, deliberately — NOT hoisted above the type loop.
			// Its job is to stop ONE type claiming one proc twice, not to stop
			// two types claiming the same proc: `proc combine*(x: Alpha, y:
			// Beta)` legitimately belongs to BOTH, which is ordinary in Nim
			// because a "method" is a free-standing proc. Hoisting it was ALIVE
			// against this package until #7231 added
			// TestFirstDecl7231_OneProcCanBelongToTwoTypes; the construction is
			// inside this block now, so it is #7231's to grade.
			methodSeen := make(map[string]bool)
			// procRE has 3 capture groups, so len(pm) is invariantly 8 — the same
			// derivation as the proc loop above, measured with its own panic probe
			// under #7197 rather than transferred. The `pm[6] >= 0` test below is a
			// different question and is NOT dead: see the proc loop's note.
			for _, pm := range procRE.FindAllStringSubmatchIndex(src, -1) {
				procName := strings.TrimSuffix(src[pm[4]:pm[5]], "*")
				if methodSeen[procName] {
					continue
				}
				// Check if any parameter references this type name.
				params := ""
				if pm[6] >= 0 && pm[7] >= 0 {
					params = src[pm[6]:pm[7]]
				}
				if containsTypeName(params, name) {
					methodSeen[procName] = true
					ref := extractor.BuildOperationStructuralRef("nim", filePath, procName)
					rels = append(rels, types.RelationshipRecord{
						ToID: ref,
						Kind: "CONTAINS",
					})
				}
			}
		}

		entities = append(entities, types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Component",
			Subtype:            subtype,
			SourceFile:         filePath,
			Language:           "nim",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          name + " = " + kind,
			EnrichmentRequired: false,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
			Relationships: rels,
		})
	}

	return entities
}

// extractKeyword returns the proc/func/method/etc keyword from the declaration line.
func extractKeyword(decl string) string {
	for _, kw := range []string{"method", "template", "macro", "iterator", "converter", "func", "proc"} {
		if strings.Contains(decl, kw+" ") || strings.Contains(decl, kw+"\t") {
			return kw
		}
	}
	return "proc"
}

// buildSig constructs a human-readable signature from the raw declaration prefix.
func buildSig(declPrefix, name, params string) string {
	kw := extractKeyword(declPrefix)
	if params != "" {
		return kw + " " + name + params
	}
	return kw + " " + name
}

// collectImports parses import/include/from statements and returns unique module paths.
func collectImports(src string) []string {
	seen := make(map[string]bool)
	var imports []string

	addModule := func(mod string) {
		mod = strings.TrimSpace(mod)
		// Strip inline comments
		if ci := strings.Index(mod, "#"); ci >= 0 {
			mod = strings.TrimSpace(mod[:ci])
		}
		if mod == "" {
			return
		}
		if !seen[mod] {
			seen[mod] = true
			imports = append(imports, mod)
		}
	}

	// import module1, module2, module3
	for _, m := range importRE.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		parts := strings.Split(m[1], ",")
		for _, p := range parts {
			addModule(strings.TrimSpace(p))
		}
	}

	// include module
	for _, m := range includeRE.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		addModule(strings.TrimSpace(m[1]))
	}

	// from module import ...
	for _, m := range fromImportRE.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		addModule(strings.TrimSpace(m[1]))
	}

	return imports
}

// buildImportEntities creates SCOPE.Component stubs carrying IMPORTS edges.
func buildImportEntities(filePath string, imports []string) []types.EntityRecord {
	if len(imports) == 0 {
		return nil
	}
	out := make([]types.EntityRecord, 0, len(imports))
	seen := make(map[string]bool, len(imports))
	for _, mod := range imports {
		if seen[mod] {
			continue
		}
		seen[mod] = true
		out = append(out, types.EntityRecord{
			Name: importDisplayName(mod),
			Kind: "SCOPE.Component",
			// #6481: resolve/refs.go keys the import-placeholder marker on
			// kind=="SCOPE.Component" && subtype=="import". Without it this stub
			// stays in the by-name index as a real declaration of its LAST PATH
			// SEGMENT and flips every colliding name AMBIGUOUS.
			Subtype:    "import",
			SourceFile: filePath,
			Language:   "nim",
			// The FULL module path, not the display name:
			// resolve.placeholderModuleSpecifier reads import_module first, and
			// the #6156 restore would otherwise record the bare last segment.
			Properties: map[string]string{"import_module": mod},
			Relationships: []types.RelationshipRecord{
				{
					FromID: filePath,
					ToID:   mod,
					Kind:   "IMPORTS",
				},
			},
		})
	}
	return out
}

// importDisplayName returns a short display name for an import path.
// e.g. "std/strutils" → "strutils", "asyncdispatch" → "asyncdispatch"
func importDisplayName(mod string) string {
	mod = strings.TrimSpace(mod)
	// Nim uses / as path separator in imports
	if slash := strings.LastIndexByte(mod, '/'); slash >= 0 {
		mod = mod[slash+1:]
	}
	return mod
}

// #7212: the line on which byte offset `pos` sits, 1-based. Both call sites cut
// the declaration's body at the regex match END (m[1]) and then measure its
// length in newlines, so the line that length is added TO must be the line of
// that same offset. It used to be the line of the match START (m[0]): sound
// only while the match is confined to one line, and short by the match's own
// line count whenever it is not — a `= object` clause broken after the `=` or
// after `ref`, or a proc parameter list wrapped across lines. Not an
// off-by-one; the deficit is the clause's line count, so it is 2 at three
// lines. See span_origin_7212_test.go.
func lineOf(src string, pos int) int {
	return strings.Count(src[:pos], "\n") + 1
}

// spanEndLine assembles the end of a declaration's span from ONE origin: the
// line of `afterPos` — the offset `body` was cut from — plus the body's own
// line count. Callers keep deriving StartLine from the match start, which is
// the declaration's own line and is correct there.
func spanEndLine(src string, afterPos int, body string) int {
	return lineOf(src, afterPos) + strings.Count(body, "\n")
}

// extractIndentBody returns the body text following a declaration line.
// It collects lines that are more indented than baseIndent (the declaration's own indent level).
// For top-level procs (indent=0), collects all lines that start with at least one space/tab.
func extractIndentBody(src string, afterPos int, baseIndentLen int) string {
	rest := src[afterPos:]
	// #7200 (CANONICAL NOTE for this invariant; the other sites point here).
	// There is no `len(lines) == 0` guard and none should be added back: with a
	// NON-EMPTY separator strings.Split always returns at least one element.
	// Walking genSplit in full, since the one step that can SHRINK the result is
	// easy to skip when checking this against the Go source:
	//
	//   1. Split calls genSplit(s, sep, 0, -1), so n == -1 on entry and the
	//      `n == 0 -> return nil` escape is unreachable;
	//   2. sep is the literal "\n", so the `sep == "" -> explode(s, n)` escape
	//      is unreachable too;
	//   3. n < 0, so n = Count(s, sep) + 1, which is >= 1;
	//   4. `if n > len(s)+1 { n = len(s)+1 }` CLAMPS n downward — but only to
	//      len(s)+1, which is itself >= 1, so the floor of 1 survives;
	//   5. the return is a[:i+1] with i >= 0, hence length >= 1.
	//
	// So lines[0] is always safe and `len(lines) == 0` is never true. The same
	// holds for every non-empty separator, not just "\n".
	lines := strings.Split(rest, "\n")
	// #7195: a source that ends in a newline makes strings.Split yield a FINAL
	// EMPTY ELEMENT — the empty remainder after the last '\n'. It is not a line
	// of the file, but it satisfies the `TrimSpace(line) == ""` arm below and was
	// appended as a blank body line. `endLine := startLine + Count(body, "\n")`
	// at both call sites then counted it, so the LAST declaration in every
	// newline-terminated file reported EndLine = lineCount + 1: a span past EOF.
	// Earlier declarations broke on their following sibling and never reached
	// this element, which is why only the last one was ever wrong.
	//
	// Drop EXACTLY that one element and nothing else — and test `== ""`, never
	// `strings.TrimSpace(...) == ""`. Every widening of this guard is the
	// PERMISSIVE direction and deletes a real line from a span:
	//
	//   - trimming trailing blank lines from the COLLECTED BODY shortens an
	//     earlier declaration whose body ends in blanks before a sibling —
	//     forbidden by TestEOF7195ForbiddenEarlierDeclUnchanged and its type
	//     twin (these do NOT grade the two routes below; a blank before a
	//     sibling is mid-split and unreachable from the end);
	//   - looping the drop over every trailing empty element shortens a body
	//     whose blanks run to EOF — forbidden by
	//     TestEOF7195ForbiddenTrailingBlankLinesAtEOFKept and its type twin;
	//   - keying on TrimSpace deletes the last line of any file whose final
	//     line is whitespace-only and which does NOT end in a newline, where
	//     that element IS the line and no phantom exists — forbidden by
	//     TestEOF7195ForbiddenWhitespaceLineAtEOFNoTrailingNewline and its type
	//     twin. This crossed cell was unforbidden in the first round and the
	//     TrimSpace mutant passed the whole package.
	if n := len(lines); n > 1 && lines[n-1] == "" {
		lines = lines[:n-1]
	}

	var bodyLines []string
	// The first line after '=' may be on the same line or the next.
	// We want lines that are more indented than the declaration.
	// #7185: the body continues at any column STRICTLY GREATER than the
	// declaration's own column, so the threshold is baseIndentLen+1 — not +2.
	//
	// It was +2, which left a DEAD BAND at exactly baseIndentLen+1: such a line
	// satisfied neither `indent >= minBodyIndent` nor `indent <= baseIndentLen`,
	// so the loop silently skipped it and kept scanning. The emitted body then had
	// a HOLE — the base+1 line gone while deeper lines below it were still
	// collected — which moved EndLine and dropped CALLS edges.
	//
	// +1 IS NIM'S RULE, NOT A STYLE GUESS, AND NOT F#'s. PR #7184 chose +1 for the
	// fsharp copy of this helper from the F# 4.1 offside rule; that argument is
	// about F# and does not transfer. Nim manual, Lexical Analysis -> Indentation:
	//
	//	"Nim's standard grammar describes an indentation sensitive language. This
	//	 means that all the control structures are recognized by indentation.
	//	 Indentation consists only of spaces; tabulators are not allowed."
	//
	// and the grammar pseudo-terminals that same section defines:
	//
	//	IND{>}	"denotes an indentation that consists of MORE SPACES than the
	//		 entry at the top of the stack"
	//	IND{=}	"an indentation that has the SAME number of spaces"
	//
	// An indented statement list is introduced by IND{>} — strictly more spaces
	// than the enclosing entry — while a SIBLING is IND{=}, i.e. exactly the
	// enclosing column. The manual names no minimum step, so "more spaces" is
	// satisfied by one. base+1 is therefore body, and only base-or-less can be a
	// sibling — which is exactly the `indent <= baseIndentLen` terminator below.
	// With +1 the two conditions are complementary and no band can exist.
	//
	// DERIVED-NOT-EXECUTED: no Nim toolchain exists on the build machine, so this
	// is read off the manual rather than compiled. FALSIFIER: a Nim program in
	// which a statement indented exactly one space deeper than its enclosing
	// declaration is rejected, or parses as that declaration's SIBLING.
	minBodyIndent := baseIndentLen + 1

	for i, line := range lines {
		if i == 0 && strings.TrimSpace(line) != "" {
			// Same-line body: "proc foo() = result"
			bodyLines = append(bodyLines, line)
			continue
		}
		if strings.TrimSpace(line) == "" {
			bodyLines = append(bodyLines, line)
			continue
		}
		indent := countIndent(line)
		if indent >= minBodyIndent {
			bodyLines = append(bodyLines, line)
		} else if indent <= baseIndentLen && strings.TrimSpace(line) != "" {
			// Back to same or lesser indent — body ends
			break
		}
	}
	return strings.Join(bodyLines, "\n")
}

// countIndent counts leading spaces/tabs in a line (tabs count as 1).
func countIndent(line string) int {
	n := 0
	for _, ch := range line {
		if ch == ' ' || ch == '\t' {
			n++
		} else {
			break
		}
	}
	return n
}

// collectCalls extracts CALLS edges from a proc body.
func collectCalls(body, callerName string) []types.RelationshipRecord {
	if body == "" {
		return nil
	}
	scrubbed := stripStringsAndComments(body)
	matches := callRE.FindAllStringSubmatchIndex(scrubbed, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	out := make([]types.RelationshipRecord, 0, len(matches))
	// callRE has 1 capture group, so len(m) is invariantly 4 — #7197 removed the
	// unreachable `if len(m) < 4` here by the same derivation as the loops above.
	for _, m := range matches {
		// m[2] and m[3] are the start and end indices of the first capturing group
		// (the identifier).
		//
		// #7197 measured this guard and DELIBERATELY LEFT IT. Unlike the arity
		// guards, it is a participation test, and participation tests can be
		// load-bearing — nim.go has two that are. This one is not: callRE's only
		// group is on the pattern's required path, inside no `?`, `*` or
		// alternation branch, so it always participates and m[2] is never -1
		// (measured: every probed input returns m[2] >= 0). It is therefore
		// unreachable rather than load-bearing, but removing it is a claim about
		// a MANDATORY group staying mandatory, which is a different and more
		// fragile claim than 2*(1+n), so it stays.
		if m[2] < 0 || m[3] < 0 {
			continue
		}
		target := scrubbed[m[2]:m[3]]
		if target == "" {
			continue
		}
		if nimKeywords[target] {
			continue
		}
		if target == callerName {
			continue // skip self-recursion
		}
		if seen[target] {
			continue
		}
		seen[target] = true
		// Compute line number by counting newlines up to match position
		lineNum := 1 + strings.Count(scrubbed[:m[0]], "\n")
		out = append(out, types.RelationshipRecord{
			ToID: target,
			Kind: "CALLS",
			Properties: types.Props{
				{K: "line", V: strconv.Itoa(lineNum)},
			},
		})
	}
	return out
}

// containsTypeName checks whether a parameter list string contains a reference
// to typeName (e.g. "self: MyType" or "x: var MyType").
func containsTypeName(params, typeName string) bool {
	if params == "" || typeName == "" {
		return false
	}
	// Simple check: type name appears after a colon in params
	return strings.Contains(params, typeName)
}

// stripStringsAndComments replaces string literals and #-line-comments
// with spaces so the call scanner doesn't pick up tokens inside them.
func stripStringsAndComments(src string) string {
	out := make([]byte, len(src))
	i := 0
	inStr := byte(0) // 0=none, '"'=double-quote, '\''=single-quote
	for i < len(src) {
		ch := src[i]
		if inStr != 0 {
			out[i] = ' '
			if ch == '\\' && i+1 < len(src) {
				out[i+1] = ' '
				i += 2
				continue
			}
			if ch == inStr {
				inStr = 0
			}
			i++
			continue
		}
		switch ch {
		case '"':
			// Check for triple-quoted string """..."""
			if i+2 < len(src) && src[i+1] == '"' && src[i+2] == '"' {
				// Find closing """
				end := strings.Index(src[i+3:], `"""`)
				if end >= 0 {
					for j := i; j < i+3+end+3; j++ {
						if j < len(out) {
							out[j] = ' '
						}
					}
					i = i + 3 + end + 3
					continue
				}
			}
			inStr = '"'
			out[i] = ' '
			i++
		case '\'':
			inStr = '\''
			out[i] = ' '
			i++
		case '#':
			// Nim comment: # to end of line
			for i < len(src) && src[i] != '\n' {
				out[i] = ' '
				i++
			}
		default:
			out[i] = ch
			i++
		}
	}
	return string(out)
}
