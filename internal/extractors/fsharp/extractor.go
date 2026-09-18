// Package fsharp implements a regex-based extractor for F# source files.
//
// Extracted entities:
//   - module/namespace declarations → Kind="SCOPE.Component", Subtype="module"|"namespace"
//   - let/let rec/let mutable bindings (functions) → Kind="SCOPE.Operation", Subtype="let"
//   - member function definitions → Kind="SCOPE.Operation", Subtype="member"
//   - type declarations (record, discriminated union, class, interface, struct, alias)
//     → Kind="SCOPE.Component"
//   - `inherit Base()` → EXTENDS edge, `interface IFoo with` → IMPLEMENTS edge,
//     both embedded on the owning TYPE record (#6326)
//   - open statements → one SCOPE.Component placeholder per `open`, marked
//     Subtype="import" (#6369) and carrying the IMPORTS edge
//   - function applications → CALLS edges. Captured call forms: paren `name(`,
//     pipe `|> name`, compose `>> name`, and space-applied `head arg`
//     (F#'s dominant curried-application idiom). Each CALLS edge is stamped with
//     a 1-based FILE-ABSOLUTE `line` Property (#5034; promoted from the prior
//     body-relative convention of #4939).
//   - Module CONTAINS members
//
// No tree-sitter grammar for F# is available in smacker/go-tree-sitter, so
// this extractor parses F# with regular expressions. F# is
// whitespace/indent-sensitive; for entity discovery purposes we rely on
// indentation heuristics similar to the Nim extractor.
//
// Registers itself via init() and is imported by registry_gen.go.
package fsharp

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("fsharp", &Extractor{})
}

// Extractor implements extractor.Extractor for F#.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "fsharp" }

// Regex patterns for F# syntax.
var (
	// module declaration: "module [rec] [access] Foo" or "module Foo.Bar"
	//
	// #7135 — the modifier group is a REPEATED ALLOWLIST, not a fixed
	// sequence. It used to read `(?:\s+rec)?`, one optional literal, and this
	// pattern anchors the END of the line (`\s*$`) — so an access modifier
	// did not mis-name the module, it made the declaration VANISH:
	// `module private Foo` left ` Foo` behind after `[\w.]+` had taken
	// `private`, the anchor failed, and the match was abandoned. Nothing was
	// extracted at all.
	//
	// That is the SILENT-MISS mode, which is harder to notice than a
	// mis-name: a mis-named entity at least appears in a listing looking odd,
	// while a missed one leaves no trace and reads as "this file has no
	// modules" — an answer no bind-rate, orphan-rate or dangle instrument can
	// flag. The tests therefore assert entity COUNT, not just presence.
	//
	// The accepted set is the UNION of four sources, because no single one is
	// exhaustive — which is the lesson the memberRE arm of this issue paid
	// for, where § 8.13's `member-defn` production genuinely omits `inline`:
	//
	//  1. F# Language Specification § 10 "Namespaces and Modules"
	//     (https://fsharp.github.io/fslang-spec/namespaces-and-modules/):
	//
	//	module-defn  := attributes? module access? ident = module-defn-body
	//	module-abbrev := module ident = long-ident
	//	access       := private | internal | public      (§ 10.5)
	//
	//  2. § 10.5 "Accessibility Annotations" for `access`.
	//  3. MS Learn "Modules" (learn.microsoft.com/dotnet/fsharp/
	//     language-reference/modules), whose syntax block is
	//     `module [accessibility-modifier] [qualified-namespace.]module-name`
	//     and which states "The accessibility-modifier can be one of the
	//     following: public, private, internal". The SAME page documents
	//     `module rec` under "Recursive modules" — and the § 10 production
	//     above carries no `rec` at all. So deriving the set from that one
	//     production would have DROPPED a form this pattern already handled,
	//     i.e. caused a regression: a second independent instance of "one
	//     grammar production is not an exhaustive modifier set".
	//  4. The sibling scanner in this file: letRE (#7131) allowlists
	//     `rec|mutable|inline|private|internal|public`. `mutable`/`inline`
	//     qualify a VALUE, not a module, so only `rec` + `access` carry over.
	//
	// `protected` is deliberately absent: MS Learn "Access Control" states it
	// "is not used in F#". A fabricated modifier is a widening with no
	// real-world case, and an all-DEAD mutant score cannot detect one.
	//
	// Order is accepted in any direction, as in letRE and memberRE — this is
	// a lenient scanner, not a compiler, and ranking the orders could only
	// create a way to LOSE a real module. No source attests `rec` together
	// with an access modifier in EITHER order, so those rows are labelled
	// lenience-only in the tests rather than claimed as legal F#.
	//
	// `\b` states that a modifier is a whole word. It is NOT load-bearing
	// here: every repetition of the group is gated by a mandatory `\s+`, and
	// a modifier-prefixed name supplies no whitespace, so `module recompute`
	// yields `recompute` with OR without the boundary (the `rec` alternative
	// dies for want of the separator and the engine backtracks to zero
	// repetitions). The two variants were brute-forced against each other
	// over 1,124,864 enumerated declaration lines (indent x three modifier/
	// name tokens from a 13-word alphabet incl. modifier-prefixed names and
	// dotted long-idents x four separators incl. the empty one x eight
	// tails): 0 distinguishing captures. The equivalence is MASKED BY THAT
	// `\s+`, not structural — relax the separator to `\s*` and the same
	// enumeration diverges on 27,648 inputs (`module recrecrec` captures
	// `recrecrec` with the boundary and `rec` without it). Kept as
	// documentation of intent, in the same masking relation as letRE's and
	// memberRE's.
	//
	// THE DISPOSITION IS CONDITIONAL ON THAT MANDATORY `\s+`, AND NOTHING
	// GRADES THE SEPARATOR: relaxing it to `\s*` diverges on 93,100 inputs of
	// an independent reviewer's enumeration, so an edit that relaxes it turns
	// both `\b` mutants live while the suite stays green through both
	// changes. The separator's own hole is pre-existing and filed as #7158
	// (`\s+` -> `\s*` here is ALIVE at 0 `--- FAIL`: `moduleLoader` would
	// mint a module named `Loader`). Re-read this equivalence before touching
	// the separator.
	//
	// #7151 — group 3, `([ \t]*=)?`, is the LOCAL module form. F# spells a
	// module two ways and they are different constructs with different
	// scopes: `module Foo` is TOP-LEVEL and its scope is the remainder of the
	// FILE, while `module Foo =` is LOCAL and its scope is the offside block
	// beneath it (F# spec § 10 `module-defn := ... module access? ident =
	// module-defn-body` vs the separate `named-module` production; MS Learn
	// "Modules" gives both syntax blocks). Until #7151 the `\s*$` anchor
	// meant the trailing ` =` killed the match outright, with or without a
	// modifier, so the local form produced NO ENTITY AT ALL.
	//
	// Three properties of the new group, each deliberate:
	//
	//   - It is `[ \t]*=`, NOT `\s*=`. `\s` matches `\n` in Go, so a `\s*=`
	//     could reach across a blank line to an `=` on a LATER line and
	//     mis-attribute it to a top-level declaration. MEASURED, and the
	//     measurement that matters is NOT a match count: on "module Foo\n=\n"
	//     BOTH patterns match exactly ONCE. They differ only in whether
	//     group 3 CAPTURES — `\s*` captures "\n=" and the declaration is
	//     classified LOCAL with Signature "module Foo =", `[ \t]*` leaves it
	//     unset and it stays TOP with Signature "module Foo". A match-count
	//     comparison is blind to precisely the error this bullet claims to
	//     prevent, so the row that grades it asserts the emitted SIGNATURE:
	//     TestLocalModule7151_NewlineBeforeEqualsStaysTopLevel.
	//     The final `\s*$` is left alone for the OPPOSITE reason: `\s`
	//     includes `\r`, and in Go's `(?m)` mode `$` matches before the
	//     `\n` only — so `\s*$` is what absorbs the `\r` of a CRLF source.
	//     Narrowing it to `[ \t]*$` cannot consume the `\r` and would drop
	//     EVERY module, of BOTH forms, in EVERY CRLF file. MEASURED, not
	//     asserted, and note the blast radius is far larger than the
	//     newline-crossing hazard above: that is one shape this file itself
	//     calls not-legal-F#, this is every module in a whole class of real
	//     files. Graded by TestLocalModule7151_CRLFSourceMintsBothForms —
	//     before it existed the package had ZERO CRLF fixtures and this
	//     half of the bullet was ungraded prose.
	//   - It is anchored at end-of-line, so the MODULE ABBREVIATION
	//     `module M = A.B.C` (§ 10 `module-abbrev`) still does not match —
	//     unchanged from before, and not decided by accident here. Whether
	//     an alias SHOULD mint is #7194, filed, not decided here.
	//   - The modifier group is untouched. #7181 measured that the allowlist
	//     is graded in the NARROWING direction only; this commit neither
	//     improves nor worsens that, because it edits nothing inside it and
	//     nothing in the mandatory `\s+` separator whose equivalence
	//     disposition is recorded above.
	//
	// Both forms keep Subtype "module". Exactly TWO consumers branch on that
	// subtype in a way that carries behaviour, and both read it as "container
	// scope, not a callable" — cited by SYMBOL rather than by line, because a
	// line citation inside a comment is invalidated by editing the comment:
	// resolve.BuildImportTable's pass-2 module reverse index skips it when
	// indexing call targets, and mcp's classifyNoise labels it noiseContainer.
	// A local module is a container scope by the same reading.
	// (extractor's file-carrier clause 3 does NOT key on this subtype — it
	// keys on `records[i].Name == path`, and that file has zero non-comment
	// occurrences of "module" at all. An earlier revision of THIS comment
	// claimed otherwise and was wrong.) The distinction between the two forms
	// is carried in the SIGNATURE instead, which echoes the declaration head:
	// "module Foo" vs "module Foo =". The full enumeration, with the grep or
	// the read behind every row and line numbers anchored to the sha they
	// were taken on, is in local_module_7151_test.go.
	moduleRE = regexp.MustCompile(
		`(?m)^([ \t]*)module(?:\s+(?:rec|public|private|internal)\b)*\s+([\w.]+)([ \t]*=)?\s*$`,
	)

	// namespace declaration: "namespace Foo" or "namespace Foo.Bar"
	namespaceRE = regexp.MustCompile(
		`(?m)^([ \t]*)namespace\s+([\w.]+)\s*$`,
	)

	// let binding: "let <modifiers> name [<params>] =" or "let name ="
	// Captures indentation and name. Handles generic type params like <'T>.
	//
	// #7131 — the modifier group is a REPEATED ALLOWLIST, not a fixed
	// sequence. It used to read `(?:\s+rec)?(?:\s+mutable)?`, two optional
	// literals in a fixed order, which could not absorb a third token in any
	// position: every other modifier landed in the NAME capture, so
	// `let inline distance p q = ...` was indexed as an operation called
	// `inline`. That also collapsed entities, because letSeen is keyed
	// indent+":let:"+name — two same-indent `let inline` bindings both keyed
	// on "inline" and the second was dropped.
	//
	// The set is the one the F# specification admits: `inline`/`mutable`,
	// `access := public | private | internal` (§ 14.6 function/value
	// definitions, § 10.5 accessibility annotations; MS Learn "Access Control
	// in F#"), and `rec` from the enclosing `let rec` form. It is closed and
	// short, so an allowlist is preferable to
	// "any word that is not the last one": the latter would name the curried
	// binding `let add x y = x + y` after its final parameter.
	//
	// `\b` states the intent that a modifier is a whole word, but it is not a
	// load-bearing guard: this pattern with and without it are STRUCTURALLY
	// equivalent, not merely equivalent under the current suite. Consuming
	// `rec` out of `recompute` leaves `ompute` with no preceding `\s+`, so
	// that alternative dies and the engine takes the zero-modifier one —
	// every modifier-prefixed name resolves identically either way. The
	// boundary-less variant was brute-forced against this one over ~592k
	// enumerated inputs (modifier words, modifier-prefixed names, whitespace
	// incl. newline, `'`, `(`, `<'T>`, `=`; depths 1–6, two alphabets):
	// 0 distinguishing cases. Kept as documentation of intent.
	//
	// Order is accepted in any direction — this is a lenient
	// scanner, not a compiler, and ranking orders could only lose a binding.
	letRE = regexp.MustCompile(
		`(?m)^([ \t]*)let(?:\s+(?:rec|mutable|inline|private|internal|public)\b)*` +
			`\s+([a-zA-Z_][a-zA-Z0-9_']*)\s*(?:<[^>]*>)?\s*(?:[^=\n]*)=`,
	)

	// member: "[static] member|override|abstract member|default
	//          [inline] [access] [val] [this.]Name ... ="
	//
	// #7135 — this pattern accommodated NO member modifier at all, and that
	// produced two distinct failure modes:
	//
	//   - MIS-NAME. The optional instance-qualifier group needs a trailing
	//     `.`; `private ` has none, so the qualifier matched empty and the
	//     NAME capture took `private` — `member private this.Go = ...` was
	//     indexed as an operation called `private`, and `member val Go = 1` as
	//     `val`. The qualifier group is NOT the bug: it handled
	//     `this.`/`self.`/`_.` correctly before this change and still does.
	//     The single cause is the absent modifier group, which left the name
	//     capture as the first thing after the keyword. As with #7131 that
	//     also COLLAPSED entities, since memberSeen is keyed
	//     indent+":member:"+name: two same-indent `member private` siblings
	//     both keyed on "private" and the second was dropped.
	//
	//   - SILENT TOTAL MISS. `static` precedes the keyword the pattern
	//     anchored on, so `static member …` was not extracted at all. That is
	//     the worse mode: `static member` is how F# expresses factory and
	//     operator members, so a type could lose its whole public surface and
	//     read as having none — an absence no bind-rate, orphan-rate or
	//     dangle instrument can flag.
	//
	// The accepted set is the UNION of three sources, because no one of them
	// is exhaustive — the § 8.13 member-defn production below genuinely omits
	// `inline`, so deriving from that grammar alone produced an incomplete
	// set on the first pass:
	//
	//   1. F# Language Specification § 8.13 "Members"
	//      (https://fsharp.github.io/fslang-spec/type-definitions/) for the
	//      productions and the ORDER, and § 10.5 "Accessibility Annotations"
	//      for `access`;
	//   2. the official language reference, MS Learn "Inline Functions"
	//      (learn.microsoft.com/dotnet/fsharp/language-reference/functions/
	//      inline-functions), which states `inline` may be applied "at the
	//      method level in a class" and shows `member inline this.f` and
	//      `static member inline F` — a form § 8.13 does not mention;
	//   3. the sibling scanner in this file: letRE (#7131) already allowlists
	//      `inline`, and a modifier legal on a module-level `let` being legal
	//      on a member is exactly the cross-check the grammar missed.
	//
	// § 8.13 productions:
	//
	//	member-defn := attributes? static? member access? method-or-prop-defn
	//	             | attributes? abstract member? access? member-sig
	//	             | attributes? override access? method-or-prop-defn
	//	             | attributes? default access? method-or-prop-defn
	//	             | attributes? static? member auto-prop-defn
	//	             | attributes? override auto-prop-defn
	//	             | attributes? default auto-prop-defn
	//	             | attributes? static? val mutable? access? ident ':' type
	//	             | additional-constr-defn
	//	auto-prop-defn := val access? ident ':' type? = expr (with get/set…)
	//	method-or-prop-defn := (ident '.')? ident pat1 … patn = expr | …
	//	access := public | private | internal
	//
	// Four consequences shape the pattern:
	//
	//  1. `static` is a PREFIX to `member`, never a suffix, so it is spelled
	//     as its own alternative rather than added to the modifier group.
	//  2. `inline` is admitted on members by the language reference even
	//     though the § 8.13 production omits it, so it is in the group.
	//     Without it mode A survives this fix intact: `member inline
	//     this.incrementByOne(x) = x + 1` captures the name `inline`, and two
	//     such members collapse on the memberSeen key.
	//  3. `val` is NOT a modifier. `member val` selects the auto-property
	//     production, whose NAME is the ident after `val` — and after an
	//     optional access modifier, because auto-prop-defn is
	//     `val access? ident`. It belongs in the group precisely so the group
	//     consumes it and the name capture lands on the property name.
	//  4. `abstract`/`abstract member` introduce a member-sig with NO
	//     `= expr`, so an abstract signature never reaches this pattern at
	//     all; the keyword stays in the alternation for the concrete
	//     `abstract member Foo() = …` shape only.
	//
	// Like letRE the group is a REPEATED ALLOWLIST, so order is accepted in
	// any direction after the keyword. The spec order is access-then-name
	// (and `val` then access then name); a reversed order is deliberate
	// scanner lenience, not a claim about the language — this is a lenient
	// scanner, not a compiler, and ranking the orders could only create a way
	// to LOSE a real member.
	//
	// `\b` states that a modifier is a whole word, but it is NOT load-bearing
	// under this pattern and is not graded: every repetition of the group is
	// gated by a mandatory `\s+`, and a modifier-prefixed name supplies no
	// whitespace, so `member this.Validate` yields `Validate` with OR without
	// the boundary. The two variants were compared over ~1.2M enumerated
	// inputs with 0 distinguishing cases. The equivalence is MASKED BY THAT
	// `\s+`, not structural: relax the separator to `\s*` and the pair
	// diverges on ~114k inputs (`member privateinternal Target = 0` captures
	// `privateinternal` with the boundary and `Target` without it). Kept as
	// documentation of intent, in the same masking relation as letRE's.
	memberRE = regexp.MustCompile(
		`(?m)^([ \t]*)(?:static\s+member|member|override|abstract member|default)` +
			`(?:\s+(?:inline|val|public|private|internal)\b)*` +
			`\s+(?:[a-zA-Z_][a-zA-Z0-9_']*\.)?([a-zA-Z_][a-zA-Z0-9_']*)\s*(?:<[^>]*>)?\s*(?:[^=\n]*)=`,
	)

	// type declaration: "type [access] Foo =" or "type Foo<'T> ="
	// Matches record, DU, class, interface, struct, alias, exception types.
	//
	// #7135 — same defect as moduleRE above, in its other spelling. The
	// pattern accommodated NO modifier, and the name capture requires
	// `[A-Z]`, so a lower-case access modifier left nothing for it to take:
	// `type private Foo = { A: int }` did not match AT ALL and produced no
	// entity — a silent total miss, not a mis-name. It cost more than one
	// node: an unseen type has no subtype classification, no type→member
	// CONTAINS edges, no DU-case/record-field sub-entities, no hierarchy
	// edges, and never enters collectRecordTypeNames, so a nested-record
	// VALIDATES edge to it could not resolve either.
	//
	// The accepted set is `access` and NOTHING else, unioned from four
	// sources (one production is not enough — see moduleRE above):
	//
	//  1. F# Language Specification § 8 "Type Definitions"
	//     (https://fsharp.github.io/fslang-spec/type-definitions/):
	//
	//	type-name := attributes? access? ident typar-defns?
	//
	//     and every type-defn variant (abbrev / record / union / anon /
	//     class / struct / interface / enum / delegate / type-extension) is
	//     built on `type-name`, so the modifier slot is shared by all of
	//     them — which is why the tests cross the modifier space with the
	//     body shapes. No production admits any keyword other than `access`
	//     between `type` and the ident.
	//  2. § 10.5 "Accessibility Annotations": `access := public | private |
	//     internal`.
	//  3. MS Learn "Access Control" (learn.microsoft.com/dotnet/fsharp/
	//     language-reference/access-control): the specifiers "can be applied
	//     to modules, types, methods, value definitions, functions,
	//     properties, and explicit fields", "The access specifier is put in
	//     front of the name of the entity", with the worked examples
	//     `type private MyPrivateType()` and `type internal MyInternalType()`.
	//     The same page states `protected` "is not used in F#", so it is
	//     deliberately NOT allowlisted.
	//  4. The sibling scanners in this file — memberRE (this issue's first
	//     arm) and letRE — whose access sets are the same three words.
	//
	// There is no `type rec`: F# expresses recursive types with
	// `type A = ... and B = ...`, so `rec` belongs to moduleRE's set and not
	// to this one. (The `and` continuation form is a separate gap in this
	// scanner and is not this change.)
	//
	// `\b` is not load-bearing here either, and is DOUBLY masked: by the
	// mandatory `\s+` before the name, and by the name's `[A-Z]` requirement,
	// which no lower-case allowlist word can satisfy. Brute-forced over
	// 512,000 enumerated declaration lines: 0 distinguishing captures. Again
	// masked rather than structural — under a `\s*` separator the same
	// enumeration diverges on 22,128 inputs (`type privateprivatePrivate =`
	// matches nothing with the boundary and captures `Private` without it).
	// As with moduleRE, the disposition is CONDITIONAL on the mandatory `\s+`
	// and nothing grades that separator — see the note there, the
	// 93,100-divergence measurement under `\s*`, and #7158, under which
	// `\s+` -> `\s*` here is ALIVE at 0 `--- FAIL` (`typeState() =` would
	// mint a type named `State`).
	// #7153 — HAZARD for any FUTURE pattern keyed on the same declaration
	// head. typeRE had a twin, typeKindRE, which captured the kind token after
	// `=` (`{`, `interface`, `class`, `|`). It was deleted under #7153 because
	// nothing ever referenced it and its doc comment ("helps classify
	// subtype") asserted a production benefit no production code delivered.
	// Subtype classification is done by classifyTypeSubtype on the matched
	// declaration and body text, which is substring-based and therefore
	// modifier-agnostic by construction. If anyone reintroduces a REGEXP that
	// classifies the kind, it must carry the
	// `(?:\s+(?:public|private|internal)\b)*` group below, or it will
	// silently miss `type private Foo = {` while typeRE matches it — a
	// twinned surface diverging on exactly one axis.
	typeRE = regexp.MustCompile(
		`(?m)^([ \t]*)type(?:\s+(?:public|private|internal)\b)*` +
			`\s+([A-Z][a-zA-Z0-9_']*)\s*(?:<[^>]*>)?\s*(?:\([^)]*\))?\s*=`,
	)

	// open statement: "open Foo" or "open Foo.Bar"
	// open statement: "open Foo.Bar", and F# 5's "open type Foo.Bar".
	// The optional `type` keyword must be consumed, not captured: without it
	// `[\w.]+` matches the literal word "type" and the extractor mints an
	// import placeholder named "type" — junk that #6369's marker now makes a
	// *recognised* placeholder, so it is worth eating here.
	openRE = regexp.MustCompile(
		`(?m)^[ \t]*open\s+(?:type\s+)?([\w.]+)`,
	)

	// #6326 — a single-quoted CHAR literal holding a brace: `'{'` / `'}'`.
	// stripStringsAndComments has no case for char literals and cannot naively
	// grow one, because F# generic parameters (`'T`, `<'K,'V>`) open with the
	// same byte and never close. Matching only the two brace-bearing literals
	// sidesteps that entirely: `'{'` and `'}'` are unambiguous three-byte
	// sequences, and a generic parameter can never look like one. Braces are
	// the only characters insideBraces counts, so nothing else needs blanking.
	charBraceRE = regexp.MustCompile(`'[{}]'`)

	// #6326 — inheritance: `inherit Base()`, `inherit Base(args)`, `inherit Base`,
	// `inherit Ns.Generic<'T>()`. The base name is captured WITHOUT its generic
	// arguments or constructor call so the ToID is the bare type name the
	// resolver indexes. `inherit` is a keyword-led, line-anchored clause, so an
	// anchored scan over the owning type's body is exact.
	inheritRE = regexp.MustCompile(
		`(?m)^[ \t]*inherit[ \t]+([A-Za-z_][A-Za-z0-9_'.]*)`,
	)

	// #6326 — interface implementation: `interface IFoo with`. Three guards keep
	// a type DECLARATION from emitting a bogus self-IMPLEMENTS. NONE of them is
	// independently bound by a test; the accounting is stated here rather than
	// implied, because the redundancy is easy to mistake for coverage:
	//
	//   (a) the name must sit on the SAME line as the keyword ([ \t]+, never
	//       \s+). The F# grammar puts it there, and \s+ would let a bare
	//       `interface` line reach down and capture the next line's token.
	//   (b) the clause must end in `with`, which is what marks an
	//       implementation rather than a mention.
	//   (c) fsharpKeywords in the emitter below. Every valid F# continuation of
	//       a bare `interface` line (`abstract`, `inherit`, `member`, `end`) is
	//       a keyword.
	//
	// Each masks the others on every fixture that could tell them apart. Mutation
	// measured, not assumed: dropping any ONE survives the suite, dropping any
	// TWO survives it as well, and only dropping all THREE is caught — by
	// TestFSharp_InterfaceDeclarationIsNotImplements, which then reports
	// `IGreeter IMPLEMENTS = [abstract]`.
	//
	// (b) was briefly bound by a `.fsi` fixture — signature files declare
	// `interface IDisposable` with no `with` and no members — but `.fsi` no
	// longer reaches this scanner at all (see collectHierarchyEdges), so that
	// binding is gone and the signature form is unreachable. The guards are
	// kept because they encode the grammar, not because anything proves they
	// earn their place. Said plainly so nobody reads the belt-and-braces as
	// verified.
	interfaceImplRE = regexp.MustCompile(
		`(?m)^[ \t]*interface[ \t]+([A-Za-z_][A-Za-z0-9_'.]*)[ \t]*(?:<[^>]*>)?[ \t]+with\b`,
	)

	// function application call: identifier( or Module.function(
	// Also detects pipe targets: |> identifier or |> Module.name
	callRE = regexp.MustCompile(
		`(?:^|[^\w.'"])([A-Za-z_][A-Za-z0-9_.]*)(?:\s*<[^>]*>)?\s*\(`,
	)

	// pipe operator call: |> Module.name or |> name
	pipeCallRE = regexp.MustCompile(
		`\|>\s*([A-Za-z_][A-Za-z0-9_.]*)`,
	)

	// compose operator: >> Module.name
	composeCallRE = regexp.MustCompile(
		`>>\s*([A-Za-z_][A-Za-z0-9_.]*)`,
	)

	// space-application call: F#'s dominant call idiom is curried application
	// written `head arg1 arg2` (e.g. `createUser "ada"`, `json user next ctx`).
	// These produce no paren/pipe match, so the head symbol is captured here.
	//
	// To stay conservative (F# is whitespace-sensitive and a bare identifier
	// followed by another identifier is ambiguous with type annotations,
	// record fields, etc.) the head must sit at a *call position* — the start
	// of a clause — and be followed by at least one whitespace-separated
	// argument starter. Recognised clause starters:
	//   - line start (after indentation)        ^[ \t]*
	//   - `=` (binding / continuation)           `let x = head arg`
	//   - `(` `[`                                grouping / list element
	//   - `|>` `<|` `->` `;` `,`                 pipe / lambda body / sequencing
	//   - `return` `return!` `yield` `yield!` `do` `do!` `then` `else`
	// The argument starter is a string/char/number literal, an opening paren or
	// bracket, or a lower-case identifier (an upper-case follower is more likely
	// a type/DU-case, so it is excluded to avoid false positives).
	spaceAppRE = regexp.MustCompile(
		`(?m)(?:^[ \t]*|[=([;,]\s*|\|>\s*|<\|\s*|->\s*|\breturn!?\s+|\byield!?\s+|\bdo!?\s+|\bthen\s+|\belse\s+)` +
			`([a-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)` +
			`[ \t]+(?:"|'|@"|\$"|[0-9]|\(|\[|[a-z_])`,
	)
)

// fsharpKeywords are tokens the call regex picks up but are not real calls.
//
// #6326: `inherit` and `interface` stay in this set. It gates addCall only —
// i.e. it suppresses the KEYWORD ITSELF from becoming a CALLS target. The
// hierarchy scanner below is a separate, line-anchored pass that reads the
// keyword's OPERAND, so suppression as a call target and recognition as an
// edge keyword do not compete.
var fsharpKeywords = map[string]bool{
	"if": true, "elif": true, "else": true, "then": true,
	"while": true, "for": true, "do": true, "done": true,
	"match": true, "with": true, "when": true,
	"try": true, "finally": true,
	"raise": true, "failwith": true, "failwithf": true,
	"return": true, "yield": true, "and": true, "or": true, "not": true,
	"let": true, "in": true, "fun": true, "function": true,
	"type": true, "open": true, "module": true, "namespace": true,
	"begin": true, "end": true, "inherit": true, "interface": true,
	"member": true, "override": true, "default": true, "abstract": true,
	"static": true, "mutable": true, "rec": true, "new": true,
	"null": true, "true": true, "false": true,
	"async": true, "seq": true, "query": true,
	"upcast": true, "downcast": true, "typeof": true, "typedefof": true,
	"sizeof": true, "nameof": true, "use": true, "using": true,
	// common computation expression keywords
	"async.Return": true, "async.Bind": true, "async.Zero": true,
}

// Extract processes F# source and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	out := extractFSharp(string(file.Content), file.Path)
	extractor.TagRelationshipsLanguage(out, "fsharp")
	extractor.TagEntitiesLanguage(out, "fsharp")
	return out, nil
}

// letSeenKey builds the composite key under which a `let` binding is recorded
// in letSeen. It exists so that the two sites which must agree on that key —
// the let-binding scanner that WRITES it, and the member loop that READS it to
// skip a member already emitted as a `let` — cannot drift. #7136: they were two
// independent `":let:"` literals, and a one-sided change to either left the
// package green at 0 `--- FAIL` while silently emitting the same binding twice.
// The indent is part of the key on purpose: two same-named bindings at
// different nesting levels are different operations.
func letSeenKey(indent, name string) string {
	return indent + ":let:" + name
}

func extractFSharp(src, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord

	// #6326: a `.fsi` signature file emits no hierarchy edges — see
	// collectHierarchyEdges. Passed as a bool rather than the path so the
	// emitter cannot reach a file path at all, which is what keeps a filePath
	// from ever finding its way into FromID (#6295).
	signatureFile := strings.HasSuffix(strings.ToLower(filePath), ".fsi")

	imports := collectOpenStatements(src)
	importEntities := buildImportEntities(filePath, imports)
	entities = append(entities, importEntities...)

	// #5048: active-pattern definitions (`let (|Even|Odd|) n = ...`). The base
	// letRE only matches a plain identifier head, so the banana-clip name is
	// invisible to it; scan separately and emit SCOPE.Pattern entities (+case
	// sub-entities). Track the clip name so the plain let-scanner does not also
	// try to (and fail to) bind it.
	apEntities := extractActivePatterns(src, filePath, imports)
	entities = append(entities, apEntities...)

	// #5077: resolution maps reused across the operation/type passes.
	//   - apCases:        bare case name → dotted case entity Name, so a match
	//     site `| Even ->` emits a USES edge to the active-pattern case.
	//   - ceBuilderTypes: type names that declare the CE builder protocol.
	//   - ceMemberNames:  CE-protocol member names (Bind/Return/...) declared by
	//     any builder, so the matching member operations are re-typed ce_member.
	//   - builderBindings: `let optional = OptionBuilder()` → builder TYPE, so a
	//     CE USES edge to `optional` resolves to the OptionBuilder type entity.
	apCases := collectActivePatternCases(apEntities)
	ceBuilderTypes, ceMemberNames := collectCEBuilderTypes(src)
	builderBindings := collectBuilderBindings(src, ceBuilderTypes)

	// 1. Module/namespace declarations → SCOPE.Component
	seen := make(map[string]bool)
	// #7151: the LOCAL arm (`module Foo =`) is gated on the comment/string
	// scrub, computed lazily because most files have no local module at all.
	// These declaration scanners otherwise read the RAW `src` (#7152), so
	// widening the pattern would have enlarged what a `(* ... *)` block or a
	// `"""..."""` literal can mint. The gate is applied to the LOCAL arm ONLY:
	// the top-level arm's pre-existing phantom is #7152's to fix, and leaving
	// it alone is what keeps this commit's before/after on the existing form
	// empty. Pinned in both directions by
	// TestLocalModule7151_MaskedLocalFormMintsNothing and
	// TestLocalModule7151_MaskedTopLevelFormIsUNCHANGED.
	//
	// stripStringsAndComments is byte-offset preserving (it writes one output
	// byte per input byte), so the NAME capture's offsets index the scrubbed
	// copy directly; a name that survives the scrub unchanged was real source.
	//
	// STATED SCOPE LIMIT — #7193. stripStringsAndComments RUNS AWAY on a
	// character literal holding a quote (`'"'`): there is no general
	// char-literal state. The OTHER runaway this comment used to name, a
	// verbatim string with a trailing backslash (`@"C:\"`), is fixed for the
	// three openers the F# lexer admits — `@"`, `$@"` and `@$"` — by #7199's
	// verbatim mode, in which `\` is an ordinary character and a doubled quote
	// is the escape, so the `case '"'` arm's "Check for verbatim string"
	// comment now describes a check that exists.
	//
	// NOT fixed for an `@` before a TRIPLE quote: that input reaches the
	// triple-quote branch, which runs first, and the scrubber's reading of it
	// disagrees with the lexer's. Recorded, not fixed, by
	// TestScrub7199_AtTripleQuoteIsReadAsTripleQuote_DISAGREES_WITH_FSC. So
	// "the verbatim half is fixed" is a claim about those three openers, not
	// about every construct that begins with an `@`.
	//
	// READ THIS BEFORE FIXING #7193 — the package ALREADY has a recorded
	// decision that a GENERAL char-literal scrub is WRONG, and it is easy to
	// walk straight into it. charBraceRE (below, #6326) deliberately matches
	// ONLY `'{'` and `'}'` because F# identifiers may end in an apostrophe,
	// so a general `'.'` scrub misreads `c' '}'` — a primed identifier next
	// to a char literal, ordinary F# — as the span `' '`. That reasoning and
	// its counter-example are pinned by
	// TestFSharp_PrimedIdentifierBeforeCharLiteralBrace in hierarchy_test.go.
	// A naive `'.'` state added to stripStringsAndComments turns that test
	// RED at the same time as it turns the remaining recording subtest below
	// GREEN. Any #7193 fix must satisfy both. #7199's verbatim mode needed no
	// such trade: `@"` is unambiguous, so that half is fixed and only the
	// char-literal half is still open.
	// Everything after the char-literal construct scrubs to blank, so a REAL
	// local module below one is dropped and this fix does not apply for the
	// rest of that file. This is PRE-EXISTING and NOT caused here — the local
	// form minted 0 unconditionally before #7151, and nine non-test call sites
	// in this package share the defect. The attribution is clean because a
	// TOP-LEVEL module in the byte-identical file still mints (that arm is
	// ungated). Recorded, not fixed, by
	// TestLocalModule7151_ScrubRunawayHidesLocalModule_7193.
	var moduleScrubbed string
	for _, m := range moduleRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 8 {
			continue
		}
		name := src[m[4]:m[5]]
		isLocal := m[6] >= 0
		if isLocal {
			if moduleScrubbed == "" {
				moduleScrubbed = stripStringsAndComments(src)
			}
			if moduleScrubbed[m[4]:m[5]] != name {
				continue
			}
		}
		key := "module:" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		signature := "module " + name
		if isLocal {
			signature += " ="
		}
		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "module",
			SourceFile: filePath,
			Language:   "fsharp",
			StartLine:  startLine,
			EndLine:    startLine,
			Signature:  signature,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
		})
	}

	for _, m := range namespaceRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		name := src[m[4]:m[5]]
		key := "namespace:" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "namespace",
			SourceFile: filePath,
			Language:   "fsharp",
			StartLine:  startLine,
			EndLine:    startLine,
			Signature:  "namespace " + name,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
		})
	}

	// 2. let bindings (functions) → SCOPE.Operation
	//
	// #7163: a `let` head already CLAIMED by the active-pattern scanner is
	// skipped. letRE matches a modifier-carrying active-pattern head and
	// mis-names it after the last modifier in the phrase — see
	// activePatternLetOffsets for the backtracking mechanism and for why the
	// rejection is derived from the sibling scanner's offsets rather than
	// re-stating its modifier allowlist here.
	apClaimed := activePatternLetOffsets(src)
	letSeen := make(map[string]bool)
	for _, m := range letRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		if apClaimed[m[0]] {
			continue
		}
		indent := src[m[2]:m[3]]
		name := src[m[4]:m[5]]
		key := letSeenKey(indent, name)
		if letSeen[key] {
			continue
		}
		letSeen[key] = true

		startLine := strings.Count(src[:m[0]], "\n") + 1
		body := extractIndentBody(src, m[1], len(indent))
		endLine := startLine + strings.Count(body, "\n")
		calls := collectCalls(body, name, startLine)
		// #5048: computation-expression usage (`async { }` / custom builders)
		// inside the body → USES edges to the builder symbol. #5077: builder
		// symbol resolves to its bound TYPE; computed `( ... ) {` heads captured.
		calls = append(calls, collectCEUsage(body, builderBindings)...)
		// #5077: active-pattern match-SITE edges — `| Even ->` → case sub-entity.
		calls = append(calls, collectMatchSiteEdges(body, filePath, apCases)...)
		// #5130: Validus / FsToolkit.ErrorHandling validator-pipeline VALIDATES
		// edges (`validate { }` / `validation { }` / Check.*/Validation.*).
		calls = append(calls, collectValidatorPipelineEdges(body, startLine)...)

		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Operation",
			Subtype:    "let",
			SourceFile: filePath,
			Language:   "fsharp",
			StartLine:  startLine,
			EndLine:    endLine,
			Signature:  buildLetSig(src[m[0]:m[1]], name),
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
			Relationships: calls,
		})
	}

	// 3. member definitions → SCOPE.Operation
	memberSeen := make(map[string]bool)
	for _, m := range memberRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		indent := src[m[2]:m[3]]
		name := src[m[4]:m[5]]
		// Skip if same name already from let bindings (avoid double-counting)
		if letSeen[letSeenKey(indent, name)] {
			continue
		}
		key := indent + ":member:" + name
		if memberSeen[key] {
			continue
		}
		memberSeen[key] = true

		startLine := strings.Count(src[:m[0]], "\n") + 1
		body := extractIndentBody(src, m[1], len(indent))
		endLine := startLine + strings.Count(body, "\n")
		calls := collectCalls(body, name, startLine)
		calls = append(calls, collectCEUsage(body, builderBindings)...)
		calls = append(calls, collectMatchSiteEdges(body, filePath, apCases)...)
		// #5130: validator-pipeline VALIDATES edges in member bodies too.
		calls = append(calls, collectValidatorPipelineEdges(body, startLine)...)

		memberProps := map[string]string{
			"imports": strings.Join(imports, ","),
		}
		memberSubtype := "member"
		// #5077: a member that implements the CE builder protocol (Bind/Return/
		// Zero/Combine/...) is re-typed a CE-protocol operation so the builder
		// protocol is queryable. ceMemberNames holds the CE-member names declared
		// by any builder type in this file; ceBuilderMembers gates it to the
		// canonical protocol so an unrelated method named the same is not flipped.
		if ceMemberNames[name] && ceBuilderMembers[name] {
			memberSubtype = "ce_member"
			memberProps["ce_member"] = "true"
			memberProps["ce_protocol_method"] = name
		}

		entities = append(entities, types.EntityRecord{
			Name:          name,
			Kind:          "SCOPE.Operation",
			Subtype:       memberSubtype,
			SourceFile:    filePath,
			Language:      "fsharp",
			StartLine:     startLine,
			EndLine:       endLine,
			Signature:     "member " + name,
			Properties:    memberProps,
			Relationships: calls,
		})
	}

	// #5130: pre-scan the file for the set of RECORD type names so a record
	// field whose type is another in-file record can materialise an owner→nested
	// VALIDATES edge (nested_model_extraction).
	recordTypes := collectRecordTypeNames(src)

	// 4. type declarations → SCOPE.Component
	typeSeen := make(map[string]bool)
	for _, m := range typeRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		name := src[m[4]:m[5]]
		if typeSeen[name] {
			continue
		}
		typeSeen[name] = true

		startLine := strings.Count(src[:m[0]], "\n") + 1
		body := extractIndentBody(src, m[1], len(src[m[2]:m[3]]))
		endLine := startLine + strings.Count(body, "\n")

		// Determine subtype
		subtype := classifyTypeSubtype(src[m[0]:m[1]], body)

		// Find members/functions that belong to this type (CONTAINS edges)
		var rels []types.RelationshipRecord
		memberRef := make(map[string]bool)
		// Check members declared at higher indentation after this type
		typeIndentLen := len(src[m[2]:m[3]])
		for _, pm := range memberRE.FindAllStringSubmatchIndex(src, -1) {
			if len(pm) < 6 {
				continue
			}
			pmIndentLen := len(src[pm[2]:pm[3]])
			if pmIndentLen <= typeIndentLen {
				continue
			}
			// Member must appear after the type declaration start
			if pm[0] < m[0] {
				continue
			}
			mName := src[pm[4]:pm[5]]
			if memberRef[mName] {
				continue
			}
			memberRef[mName] = true
			ref := extractor.BuildOperationStructuralRef("fsharp", filePath, mName)
			rels = append(rels, types.RelationshipRecord{
				ToID: ref,
				Kind: "CONTAINS",
			})
		}

		// #6326: inheritance topology — `inherit Base()` → EXTENDS,
		// `interface IFoo with` → IMPLEMENTS. Emitted EMBEDDED on this type's
		// record so the edge anchors on the TYPE, never on the file.
		rels = append(rels, collectHierarchyEdges(body, startLine, signatureFile)...)

		// #4942: emit DU cases / record fields as SCOPE.Schema sub-entities,
		// with a type→member CONTAINS edge each.
		memberEnts, memberRels := extractTypeMembers(name, subtype, body, filePath, startLine)
		rels = append(rels, memberRels...)

		// #5130: type-level validation edges — nested-record VALIDATES
		// (nested_model), [<CustomValidation(...)>] field validators, and
		// IValidatableObject custom validators. Only records carry nested/custom
		// field validators; IValidatableObject can sit on any type.
		var fieldRefs []fsFieldRef
		if subtype == "record" {
			for _, mi := range parseRecordFields(body) {
				fieldRefs = append(fieldRefs, fsFieldRef{
					name:       mi.name,
					typ:        mi.typ,
					attrLines:  mi.attrLines,
					lineOffset: mi.lineOffset,
				})
			}
		}
		rels = append(rels, collectTypeValidatorEdges(
			name, filePath, fieldRefs, recordTypes,
			fsTypeImplementsIValidatable(body), startLine,
		)...)

		typeProps := map[string]string{
			"imports": strings.Join(imports, ","),
		}
		// #5048: a type that declares the computation-expression builder member
		// protocol (Bind/Return/Zero/Combine/...) is a CE BUILDER — stamp it so
		// `myBuilder { ... }` USES edges have a recognisable target type.
		if members, ok := detectCEBuilder(body); ok {
			typeProps["ce_builder"] = "true"
			typeProps["ce_builder_members"] = strings.Join(members, ",")
			subtype = "computation_builder"
		}

		entities = append(entities, types.EntityRecord{
			Name:          name,
			Kind:          "SCOPE.Component",
			Subtype:       subtype,
			SourceFile:    filePath,
			Language:      "fsharp",
			StartLine:     startLine,
			EndLine:       endLine,
			Signature:     "type " + name,
			Properties:    typeProps,
			Relationships: rels,
		})
		entities = append(entities, memberEnts...)
	}

	// #5129: Fable + Elmish/Feliz frontend decoration. Import-gated — a no-op
	// for any file that does not `open` Elmish/Feliz/Fable. Mutates the entities
	// in place (re-kinds Model/Msg, tags the MVU triad, re-kinds Feliz components
	// + emits RENDERS, stamps Cmd dispatch USES edges).
	applyElmishFeliz(src, filePath, imports, entities)

	// #6852: buildImportEntities anchors each `open` placeholder's IMPORTS on
	// filePath, and nothing above is NAMED after the file — every record is
	// named after a module, namespace, type, operation, DU case or active
	// pattern. resolve/refs.go has no path→entity index, so that FROM end
	// resolved to nothing and the raw path reached the graph. The carrier is
	// CONDITIONAL (#6815, #6518): an .fs file with no `open` anchors nothing,
	// and an unconditional carrier would mint one bare orphan node per source
	// file across a whole repo — a change no recall-shaped assertion can see.
	//
	// Placement is load-bearing: FileCarrierFor clause 2 asks whether some
	// record already anchors on the path, so the call must come AFTER the
	// import placeholders exist. Clause 3 matters here too — a root file
	// `Core.fs` declaring `module Core.fs` already emits a record named
	// exactly the path, and graph.EntityID does not hash Subtype, so a second
	// SCOPE.Component would land under the module record's id (#6369/#6480).
	// #6912 arm E: the field→declared-type REFERENCES edge. Placement is
	// load-bearing in BOTH directions and the call therefore wraps the carrier
	// rather than preceding it:
	//
	//   - It runs AFTER applyElmishFeliz, which RE-KINDS the `Model` record to
	//     SCOPE.Model and the `Msg` DU to SCOPE.Event — and this ordering is
	//     LOAD-BEARING, via the AMBIGUITY RULE rather than the allow-list.
	//     `open Foo.Model` beside an Elmish `type Model` is the distinguishing
	//     input: read after the re-kind the name denotes two kinds and the pass
	//     declines (0 edges); read before, both records are SCOPE.Component,
	//     one kind, and the pass emits a stub that DANGLES because Component
	//     and Model share a kind family. It is not, as an earlier draft of this
	//     comment claimed, that the allow-list would lose the re-kinded rows —
	//     before the re-kind those records are already admitted. See the
	//     fsharpTypeDeclKinds block for the full experiment, and
	//     TestFSharpFieldTypeRefs_PlacementAfterTheElmishRekindIsLoadBearing
	//     for the grader.
	//   - It must run after EVERY producer that can add a same-file record,
	//     because its ambiguity rule counts the graph nodes a name denotes in
	//     this file and a collision it cannot see reaches the resolver as a
	//     dangling edge. Passing the post-PrependFileCarrier slice makes that
	//     unconditional rather than resting on the carrier's Name always being
	//     a path. That half is defence-in-depth and is scored as such: moving
	//     the call to BEFORE PrependFileCarrier is an EQUIVALENT mutant (ALIVE,
	//     and unkillable) because FileEntity names the carrier file.Path
	//     (extractor/extractor.go:344) and every F# path contains a `.`, which
	//     no bare candidate token ever does.
	return attachFSharpFieldTypeRefs(
		extractor.PrependFileCarrier(filePath, "fsharp", entities), filePath)
}

// classifyTypeSubtype determines the F# type subtype from the declaration context.
func classifyTypeSubtype(decl, body string) string {
	// Check for "= {" → record
	if strings.Contains(decl, "= {") || strings.TrimSpace(body) != "" && strings.HasPrefix(strings.TrimSpace(body), "{") {
		return "record"
	}
	// Check for "= |" or body starting with "|" → discriminated union
	if strings.Contains(decl, "= |") {
		return "discriminated_union"
	}
	bodyTrimmed := strings.TrimSpace(body)
	if strings.HasPrefix(bodyTrimmed, "|") {
		return "discriminated_union"
	}
	// Check for interface/class keywords
	if strings.Contains(decl, "interface") || strings.HasPrefix(bodyTrimmed, "interface") {
		return "interface"
	}
	if strings.Contains(decl, "class") || strings.HasPrefix(bodyTrimmed, "class") {
		return "class"
	}
	if strings.Contains(decl, "struct") {
		return "struct"
	}
	// #4942: a pure alias (`type Foo = Bar`, `type Id = int`) is a distinct
	// subtype, not the catch-all "type".
	if isAliasBody(body) {
		return "alias"
	}
	return "type"
}

// buildLetSig builds a signature string for a let binding from the raw declaration.
func buildLetSig(decl, name string) string {
	// Trim whitespace and return a reasonable signature
	sig := strings.TrimSpace(decl)
	if idx := strings.Index(sig, "="); idx >= 0 {
		sig = strings.TrimSpace(sig[:idx])
	}
	if sig == "" {
		return "let " + name
	}
	return sig
}

// collectOpenStatements parses "open" statements and returns unique module paths.
func collectOpenStatements(src string) []string {
	seen := make(map[string]bool)
	var imports []string

	for _, m := range openRE.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		mod := strings.TrimSpace(m[1])
		// Strip inline comments
		if ci := strings.IndexAny(mod, "//"); ci >= 0 {
			mod = strings.TrimSpace(mod[:ci])
		}
		if mod == "" || seen[mod] {
			continue
		}
		seen[mod] = true
		imports = append(imports, mod)
	}
	return imports
}

// buildImportEntities creates SCOPE.Component stubs carrying IMPORTS edges.
//
// #6369 — THE `Subtype:"import"` MARKER IS LOAD-BEARING, not decoration. It is
// the single property resolve.isImportPlaceholderKind (internal/resolve/refs.go)
// tests to tell a per-import placeholder from a real declaration of the same
// name. Without it BuildIndex indexed this stub in `byName` exactly like a
// type declaration, so one `open Acme.Animal` whose LAST SEGMENT collided with
// a real type flipped that name AMBIGUOUS globally and dropped every bare-name
// edge to it repo-wide — including in files that imported nothing at all
// (measured on #6369: two cross-file EXTENDS to `Animal` went unresolved after
// an unrelated file was added, and reported ambiguous=0, i.e. silently).
// #6427 fixed the resolver for every extractor that stamps the marker; F# was
// not one of them, so the defect stayed live here.
//
// THE FULL MODULE PATH TRAVELS ON Properties["import_module"]. Name is only
// the last segment (importDisplayName), so a specifier channel is required —
// without one the #6156 external-module restore records the bare segment
// `Animal` as the imported module. resolve.placeholderModuleSpecifier reads
// `import_module` FIRST, ahead of the three legacy channels.
//
// THE TWO LEGACY CHANNELS ARE BOTH CONTAMINATED, and this extractor used each
// of them in turn before landing here. Neither is a channel; both are fields
// that already mean something else:
//
//	QualifiedName        also feeds resolve.BuildIndex's `byQualifiedName`,
//	                     which Lookup/lookupWithStatus probe BEFORE every other
//	                     tier and which never got #6427's placeholder
//	                     precedence — first-writer-wins with a blank ambiguity
//	                     sentinel. Measured on `module Acme.Animal` +
//	                     `open Acme.Animal`, the IMPORTS ToID became this
//	                     file's OWN placeholder: a fabricated intra-file
//	                     dependency in place of the real module entity.
//	                     (razor and vue do this and carry the same hazard.)
//
//	Properties["module"] is the MODULE-ROLLUP LABEL — internal/module.Derive's
//	                     depth-capped path prefix of the source file. Both
//	                     stampers treat a present value as authoritative:
//	                     module.EnsureModule returns props unchanged, and
//	                     stampModuleOnEntities skips the entity
//	                     ("extractor-supplied label preserved"). Measured:
//	                     placeholder "Generic" in src/Domain/Core.fs came out
//	                     labelled module="System.Collections.Generic" where the
//	                     path-derived label is "src/Domain".
//
//	                     The full rebuild hides this by ordering alone —
//	                     cmd/grafel/index.go prunes placeholders BEFORE
//	                     EnsureModule. The daemon's INCREMENTAL reindex, which
//	                     is the default path (#5231), never prunes at all, so
//	                     every distinct `open` would mint a fabricated Module
//	                     node with CONTAINS/DEPENDS_ON edges, and would break
//	                     stampModuleOnEntities' plain-repo label recovery
//	                     (two distinct labels => `multiple` => fall back to
//	                     doc.Repo) for every newly extracted entity.
//
// So: NOTHING an F# record carries may be a name index or a derived label.
// The placeholder sets Name (bare segment), Subtype (the marker) and
// import_module (the specifier) — and no QualifiedName and no "module".
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
			Name:       importDisplayName(mod),
			Kind:       "SCOPE.Component",
			Subtype:    "import",
			SourceFile: filePath,
			Language:   "fsharp",
			// The full module path travels on the private
			// Properties["import_module"] key — NOT QualifiedName and
			// NOT Properties["module"]; see the block comment above.
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
// e.g. "Microsoft.FSharp.Collections" → "Collections"
func importDisplayName(mod string) string {
	mod = strings.TrimSpace(mod)
	if dot := strings.LastIndexByte(mod, '.'); dot >= 0 {
		return mod[dot+1:]
	}
	return mod
}

// extractIndentBody returns the body text following a declaration line.
// Collects lines that are more indented than baseIndent.
func extractIndentBody(src string, afterPos int, baseIndentLen int) string {
	rest := src[afterPos:]
	lines := strings.Split(rest, "\n")
	if len(lines) == 0 {
		return ""
	}

	var bodyLines []string
	// #7176: the body continues at any column STRICTLY GREATER than the
	// declaration's own column, so the threshold is baseIndentLen+1 — not +2.
	//
	// It was +2, which left a DEAD BAND at exactly baseIndentLen+1: such a line
	// satisfied neither `indent >= minBodyIndent` nor `indent <= baseIndentLen`,
	// so the loop silently skipped it and kept scanning. The line vanished from
	// the body while everything after it stayed, which moved EndLine, dropped
	// calls, and lost `inherit` clauses and DU cases.
	//
	// +1 is F#'s offside rule, not a style guess. F# 4.1 Language Specification
	// §15.1.4 "Offside Lines" gives a worked example indented by exactly one
	// column — `let x = 1` / ` let y = 2` — in which the one-column-deeper `let`
	// is "unmatched", i.e. absorbed as a continuation rather than read as a
	// sibling; it is the RETURN to column 0 that goes offside (FS0058). §15.1.8
	// states the same boundary from the closing side: "When a token occurs on or
	// before the offside limit for the current offside stack ... enclosing
	// contexts are closed." On-or-before closes; strictly greater continues —
	// which is exactly the `indent <= baseIndentLen` terminator below.
	//
	// §15.1.8 also names where a SIBLING sits, which is the sharpest form of the
	// rule for this loop: "When a token other than `and` appears directly ON THE
	// OFFSIDE LINE of Let context, and the next surrounding context is a
	// SeqBlock, the $in token is inserted." Directly on the offside line means at
	// exactly `base`, not `base+1` — and `and` is the single token the spec
	// exempts. So `base+1` cannot be a sibling; it is body.
	//
	// DERIVED-NOT-EXECUTED: no F# toolchain exists on the build machine, so this
	// is read off the specification rather than compiled.
	minBodyIndent := baseIndentLen + 1

	for i, line := range lines {
		if i == 0 && strings.TrimSpace(line) != "" {
			// Same-line body
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
			break
		}
	}
	return strings.Join(bodyLines, "\n")
}

// countIndent counts leading spaces/tabs.
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

// insideBraces reports whether off sits inside an unclosed `{ ... }` region of
// scrubbed.
//
// An F# OBJECT EXPRESSION carries its own inheritance clauses:
//
//	member _.Enumerate () =
//	    { new IEnumerator<int> with
//	          member _.Current = 0
//	      interface IEnumerator with     ← the anonymous object's, not the type's
//	      interface IDisposable with     ← likewise
//	          member _.Dispose () = () }
//
// Those `interface X with` lines are line-anchored exactly like a real one, so
// a flat scan of the type body attributes them to the enclosing type and
// fabricates edges it does not have. That is the custom-sequence /
// IDisposable-wrapper idiom, not a contrived shape, and when it is mixed with a
// genuine clause the true and the fabricated edge are indistinguishable in the
// output. #6326's premise is that a false-positive edge is worse than a missing
// one, so the whole braced region is skipped.
//
// Depth, not a `{ new` sniff: a type's own clauses always sit at brace depth 0
// of its body (F# class members are indentation-delimited, not braced), while
// everything a `{ ... }` encloses — object expression or record literal — is at
// depth ≥ 1 and is never the type's own. Counting is balanced rather than
// sticky, so a record literal in one member does not swallow a real clause in
// the next.
//
// The count needs every brace that is not real code to be gone, which is a
// STRICTER requirement than the one stripStringsAndComments was built for. That
// scrubber suppresses call TOKENS, where a surviving brace was harmless; here a
// single stray brace flips the depth for the rest of the body. Two gaps
// therefore have to be closed rather than assumed away:
//
//   - Char literals. The scrubber has no case for `'x'` and cannot naively grow
//     one (`'T` generic parameters open the same way and never close), so an
//     unmatched `'}'` used to cancel an object expression's `{` and re-admit
//     its clauses at apparent depth 0 — re-opening the exact false positive
//     this gate exists to close — while a lone `'{'` suppressed every real
//     clause below it. charBraceRE blanks the two brace-bearing literals here.
//     A balanced `match c with | '{' -> ... | '}' -> ...` was always fine, which
//     is why this is narrow, but `c = '}'` on its own is ordinary F#.
//   - Nested block comments, handled at the source in stripStringsAndComments.
//
// The caller passes the SCRUBBED body; this function closes the char-literal
// gap on top of it.
func insideBraces(scrubbed string, off int) bool {
	if off > len(scrubbed) {
		off = len(scrubbed)
	}
	prefix := charBraceRE.ReplaceAllString(scrubbed[:off], "   ")
	return strings.Count(prefix, "{") > strings.Count(prefix, "}")
}

// collectHierarchyEdges scans a TYPE body for F#'s two inheritance clauses and
// returns the corresponding edges (#6326):
//
//	type Derived() =
//	    inherit Base()                  → EXTENDS  Base
//	    interface IDisposable with      → IMPLEMENTS IDisposable
//	        member _.Dispose () = ()
//
// The returned records are meant to be EMBEDDED on the owning type's
// EntityRecord, not appended to a standalone relationship slice: only
// resolve.ReferencesEmbedded supplies the parent's file and package dir, which
// is what the six locality tiers rank on. resolve.References, which the
// standalone slice goes through, has no caller context at all.
//
// FromID is deliberately left EMPTY. The assembly loop stamps the owning
// record's own entity id, which is the only value that anchors the edge on the
// TYPE. Passing filePath here would be non-empty and non-hex, so
// ReferencesEmbedded would rewrite it — and the file entity carries that same
// path, so every type in a multi-type file would merge its bases onto the one
// file component. That is the defect fixed in #6295 (Solidity) and #6298
// (Verilog, Astro); this is the same contract, stated once.
//
// The bare type name is used as the ToID (the Solidity/Crystal convention) so
// the resolver can bind the base across files by name; a file-pinned structural
// ref would be wrong, since a base type is usually declared elsewhere.
//
// A `.fsi` SIGNATURE file emits nothing. `.fsi` routes to this extractor
// (classifier.go:454, substrate.go:193), and F# requires every signature file
// to be paired with the `.fs` it describes, which carries the same clauses. But
// graph.EntityID hashes SourceFile, so the `.fsi` and `.fs` components get
// different ids and the (from, to, kind) dedup triple differs — the edge lands
// twice, inheritance queries return the type twice, and centrality and
// impact-radius double-weight it. That is the same "one logical thing, two
// nodes" shape as #6295/#6298, so the `.fs` is the single source of truth. The
// gate keys on `.fsi` specifically, not on "not `.fs`": a `.fsx` script is
// standalone and keeps its edges.
//
// #7187: the scan IS nesting-aware, via maskNestedTypeBodies below. A clause
// that sits inside a NESTED type declaration's own block belongs to that nested
// type, never to the outer one. The nested type is USUALLY matched by the
// file-level typeRE pass in its own right and collects the clause there, so
// masking it out of the outer body moves the edge rather than deleting it. The
// one measured exception is a nested type whose NAME collides with an earlier
// type: `typeSeen` drops it, and the clause then lands nowhere at all. See
// maskNestedTypeBodies for the full accounting of where the two predicates
// diverge — they share a regex, not a composed predicate.
//
// One known vector still limits this scan, noted for the record and
// deliberately NOT fixed here:
//
//   - typeRE does not admit the self-identifier form `type X() as this =`, so
//     those types produce no entity at all — and hence no hierarchy edge. That
//     form is common precisely on the inheriting classes this scan targets. It
//     is also the one shape where the #7187 masking cannot help: typeRE not
//     matching the nested header means the nested block is neither masked out
//     of the outer body nor given an owner of its own, so such a nested type's
//     clause is still attributed to the enclosing type.
func collectHierarchyEdges(body string, typeStartLine int, signatureFile bool) []types.RelationshipRecord {
	if body == "" || signatureFile {
		return nil
	}
	// Comments and string literals must not look like inheritance clauses.
	// stripStringsAndComments preserves byte offsets, so line stamping is exact.
	scrubbed := stripStringsAndComments(body)

	// #7187: a nested type's clauses are its own. Matching runs over the masked
	// text; the BRACE guard below keeps consulting the unmasked `scrubbed`.
	//
	// That is a real choice, not a formality, and it is GRADED —
	// TestFSharp_NestedType7187_BraceGuardReadsTheUnmaskedBody is the witness,
	// added after PR #7188's EC-1 mutant (both guard sites swapped to `masked`)
	// came back ALIVE at 0 --- FAIL and showed the claim was unobserved. The two
	// inputs differ exactly when a masked region holds MORE `}` than `{`, i.e. a
	// brace region that opens OUTSIDE a nested block and closes INSIDE it:
	// masking eats the closer, insideBraces reads the prefix as permanently
	// open, and every later clause is silently suppressed. The opposite
	// imbalance cannot flip the verdict, since `count("{") > count("}")` reads a
	// negative depth the same as zero.
	//
	// So `scrubbed` is the conservative side, on a shape that is almost
	// certainly not legal F# (DERIVED-NOT-EXECUTED; no toolchain here) and whose
	// corpus incidence is uncounted. Stated as what is known, not as "masking
	// cannot change any depth count" — that earlier wording asserted a causal
	// claim nothing in the suite observed.
	masked := maskNestedTypeBodies(scrubbed)

	var out []types.RelationshipRecord
	seen := make(map[string]bool)

	add := func(kind, target string, off int) {
		target = strings.TrimSuffix(target, ".")
		if target == "" || fsharpKeywords[target] {
			return
		}
		key := kind + ":" + target
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, types.RelationshipRecord{
			// FromID intentionally empty — see the doc comment above.
			ToID: target,
			Kind: kind,
			Properties: types.Props{
				// Count newlines in the ORIGINAL body. Since #6336 the scrub
				// preserves newlines too, so a scrub-based count would now
				// agree; counting the source keeps this stamp independent of
				// that guarantee. Byte offsets are preserved by the scrub, so
				// `body[:off]` is the exact prefix.
				{K: "line", V: strconv.Itoa(typeStartLine + strings.Count(body[:off], "\n"))},
			},
		})
	}

	for _, m := range inheritRE.FindAllStringSubmatchIndex(masked, -1) {
		if len(m) >= 4 && m[2] >= 0 && !insideBraces(scrubbed, m[0]) {
			add("EXTENDS", masked[m[2]:m[3]], m[2])
		}
	}
	for _, m := range interfaceImplRE.FindAllStringSubmatchIndex(masked, -1) {
		if len(m) >= 4 && m[2] >= 0 && !insideBraces(scrubbed, m[0]) {
			add("IMPLEMENTS", masked[m[2]:m[3]], m[2])
		}
	}
	return out
}

// maskNestedTypeBodies blanks out every NESTED type declaration inside a type
// body — the `type ...` header line and the offside block it owns — replacing
// those bytes with spaces while leaving every newline in place. Byte offsets
// and line numbering are therefore unchanged, which is what keeps
// collectHierarchyEdges' `line` Property exact (#7187).
//
// WHY A HEADER SCAN AND NOT THE RECORDED SPANS. Both owners' spans are already
// emitted (on the #7187 reproducer: outer 6–9, nested 7–9), so subtracting the
// nested span from the outer one is a real alternative. It is not the one taken,
// for three reasons, the third decisive:
//
//   - The spans live on OTHER EntityRecords that the assembly loop has not built
//     yet when this runs; consuming them means a second pass and cross-entity
//     coupling in a function whose contract is "given one body, return its
//     edges".
//
//   - `typeSeen` in that loop dedups by NAME, so a nested type whose name was
//     already taken has no record and no span at all. A span-subtraction fix
//     would silently leave that clause on the outer type.
//
//   - The excision predicate and the re-attribution predicate should be as close
//     to the SAME predicate as possible, because wherever they diverge the edge
//     is dropped rather than moved. Sharing typeRE — the very regex the
//     file-level pass uses to decide what becomes a type — is the closest
//     available coupling; a span-based fix couples them through a third
//     artefact instead and can diverge from it independently.
//
//     The REGEX is shared. The COMPOSED predicates are NOT identical, and
//     saying so plainly matters more than the tidiness of the claim:
//
//     (a) `typeSeen` in the file-level pass dedups by NAME and sits between
//     excision and re-attribution. A nested type whose name collides with
//     an earlier one is masked out of the outer body and then produces no
//     entity, so its clause lands nowhere. Pre-change it landed on the
//     OUTER type; this is a real, new recall loss, accepted deliberately
//     and pinned by
//     TestFSharp_NestedType7187_NameCollisionDropsTheEdge.
//     (b) Excision reads the SCRUBBED body while re-attribution reads raw
//     `src`, so a header the scrub alters is in principle another
//     divergence. Probed on PR #7188 (fixture P10,
//     `    (* c *)type P10Nested() =`): neither predicate matched, so
//     nothing was excised without an owner. That half is UNEXERCISED, not
//     refuted.
//
// The block extent is not a re-statement of extractIndentBody's rule; it is
// that function's own answer, called with the same arguments the file-level
// pass will use (#7176's `baseIndentLen+1` partition, F# 4.1 spec §15.1.4 /
// §15.1.8). So the masked region equals the nested type's own body EXACTLY:
// nothing that would land in the nested owner's body is left in the outer's,
// and nothing outside it is removed — a clause back at the outer's member
// column AFTER the nested block stays with the outer type.
//
// The input is the SCRUBBED body, so a `type X =` inside a comment or a string
// literal is already blank and cannot mask anything.
func maskNestedTypeBodies(scrubbed string) string {
	locs := typeRE.FindAllStringSubmatchIndex(scrubbed, -1)
	if len(locs) == 0 {
		return scrubbed
	}

	out := []byte(scrubbed)
	blank := func(lo, hi int) {
		for i := lo; i < hi; i++ {
			if out[i] != '\n' {
				out[i] = ' '
			}
		}
	}

	maskedTo := 0
	for _, m := range locs {
		if len(m) < 4 {
			continue
		}
		// A match at offset 0 would be the tail of the OWNING type's own
		// declaration line (extractIndentBody keeps same-line body text), not a
		// nested declaration on a line of its own. `(?m)^` matches there, so it
		// is excluded explicitly.
		if m[0] == 0 || m[0] < maskedTo {
			continue
		}
		headerIndent := len(scrubbed[m[2]:m[3]])

		// The nested block is extractIndentBody's own answer, not a re-statement
		// of its rule: call it, from the END of the matched header (m[1]) and
		// with the header's own indent, exactly as the file-level type pass
		// does. Its output is a CONTIGUOUS prefix of scrubbed[m[1]:] — every
		// non-blank line is either >= headerIndent+1 (appended) or <=
		// headerIndent (terminates), with no third case since #7176 closed the
		// dead band, and blanks are always appended — so its LENGTH is the
		// block's extent in bytes.
		//
		// Starting at m[1] rather than at the newline after m[0] is what makes
		// a MULTI-LINE header work: typeRE's `\s+`, `(?:<[^>]*>)?` and
		// `(?:\([^)]*\))?` all cross newlines, so a wrapped parameter list puts
		// the header's CONTINUATION on the next line. Walking from m[0]'s
		// newline treated that continuation as the first body line and
		// terminated on it whenever it sat at or shallower than the `type`
		// column, leaving the nested clause in the outer body — #7187 surviving
		// verbatim. Reported on PR #7188 (fixture P4a) and pinned by
		// TestFSharp_NestedType7187_MultiLineNestedHeader.
		end := m[1] + len(extractIndentBody(scrubbed, m[1], headerIndent))
		blank(m[0], end)
		maskedTo = end
	}
	return string(out)
}

// collectCalls extracts CALLS edges from a function body.
//
// bodyStartLine is the 1-based FILE line at which the body's first line sits
// (i.e. the enclosing operation's StartLine). The stamped `line` Property is
// file-absolute: bodyStartLine + (body-relative line - 1), so a clickable
// jump-to-call-site is possible without a separate body-offset lookup (#5034).
func collectCalls(body, callerName string, bodyStartLine int) []types.RelationshipRecord {
	if body == "" {
		return nil
	}
	scrubbed := stripStringsAndComments(body)

	seen := make(map[string]bool)
	var out []types.RelationshipRecord

	// addCall records a CALLS edge to target, stamping the FILE-ABSOLUTE 1-based
	// line at which the call site begins (#5034 — promoted from the prior
	// body-relative convention). Duplicate targets keep the FIRST line seen.
	addCall := func(target string, off int) {
		if target == "" || callerName == target {
			return
		}
		if fsharpKeywords[target] {
			return
		}
		// Skip single-letter identifiers (usually type params)
		if len(target) == 1 {
			return
		}
		if seen[target] {
			return
		}
		seen[target] = true
		line := bodyStartLine + strings.Count(scrubbed[:off], "\n")
		out = append(out, types.RelationshipRecord{
			ToID: target,
			Kind: "CALLS",
			Properties: types.Props{
				{K: "line", V: strconv.Itoa(line)},
			},
		})
	}

	// Regular function calls: name( — the capture-group start is the call-site
	// offset used for line stamping.
	for _, m := range callRE.FindAllStringSubmatchIndex(scrubbed, -1) {
		if len(m) >= 4 && m[2] >= 0 {
			addCall(scrubbed[m[2]:m[3]], m[2])
		}
	}

	// Pipe operator: |> name or |> Module.name
	for _, m := range pipeCallRE.FindAllStringSubmatchIndex(scrubbed, -1) {
		if len(m) >= 4 && m[2] >= 0 {
			addCall(scrubbed[m[2]:m[3]], m[2])
		}
	}

	// Compose operator: >> name
	for _, m := range composeCallRE.FindAllStringSubmatchIndex(scrubbed, -1) {
		if len(m) >= 4 && m[2] >= 0 {
			addCall(scrubbed[m[2]:m[3]], m[2])
		}
	}

	// Space-applied calls: head arg1 arg2 (F#'s dominant idiom). Gated to call
	// positions so it does not fire on prose, type annotations, or record fields.
	// Use a literal-preserving scrub: string/char literal bodies are blanked but
	// the OPENING quote survives as a visible `"`, so a string argument
	// (`createUser "ada"`) still presents an argument-starter to the scanner.
	// Byte offsets are preserved, so head positions line up with `scrubbed`.
	spaceScrubbed := scrubKeepingQuote(body)
	for _, m := range spaceAppRE.FindAllStringSubmatchIndex(spaceScrubbed, -1) {
		if len(m) >= 4 && m[2] >= 0 {
			addCall(spaceScrubbed[m[2]:m[3]], m[2])
		}
	}

	return out
}

// scrubKeepingQuote is like stripStringsAndComments but preserves the OPENING
// quote of each string/char literal as a visible `"`. This lets the
// space-application scanner recognise a string argument (`createUser "ada"`)
// whose body would otherwise be blanked to whitespace. Byte offsets are
// preserved exactly, so head-symbol offsets stay aligned with the standard scrub.
func scrubKeepingQuote(src string) string {
	scrubbed := []byte(stripStringsAndComments(src))
	i := 0
	for i < len(scrubbed) {
		ch := src[i]
		// A literal opens at a quote/char/verbatim/interpolated start where the
		// standard scrub blanked it. Restore a single `"` marker, then let the
		// inner bytes stay blank.
		if (ch == '"' || ch == '\'') && scrubbed[i] == ' ' {
			scrubbed[i] = '"'
		}
		i++
	}
	return string(scrubbed)
}

// stripStringsAndComments replaces string literals and //-line comments
// with spaces so the call scanner doesn't pick up tokens inside them.
//
// NEWLINES SURVIVE (#6336). Every other byte of a suppressed region becomes a
// space, but a `'\n'` stays a `'\n'`, so the scrub has exactly the same line
// structure as `src`. Without that, a `(* ... *)` block or a multi-line string
// glued the lines around it together, and `strings.Count(scrubbed[:off], "\n")`
// — how collectCalls, collectCEUsages and firstSignalLine stamp their `line`
// Property — under-reported by one per swallowed newline. The stamp is what a
// consumer jumps to, so every call site below such a region pointed too high.
//
// MATCHING CHANGES TOO, in both directions, wherever a delimiter closes
// MID-LINE. This is a consequence of the change, not a verdict about it, and
// both halves are measured by tests rather than asserted here:
//
//   - GAINED, for `(?m)^`-anchored patterns. Restoring an interior newline
//     creates a line start whose remainder is blanked-delimiter spaces followed
//     by live code, which `^[ \t]*` now reaches. `let x = 1 (* c\n c *)
//     interface IValidatableObject with` yields no edge before and an
//     IMPLEMENTS (and a VALIDATES) after — see
//     TestFSharp_DeclarationAfterMidLineCommentClose.
//   - LOST, for patterns that span `[ \t]+` between two tokens — spaceAppRE
//     here, felizChildRE in elmish_feliz.go. That gap used to cross a blanked
//     region and cannot cross a newline, so the space application
//     `helper (*\n gap \n*) n` emitted CALLS before and emits none after — see
//     TestFSharp_KnownLimitation_SpaceApplicationSplitByBlockComment.
//
// The trade is taken deliberately: the gain is ordinary F#, the loss needs an
// argument separated from its head by a multi-line comment.
//
// The byte LENGTH is unchanged either way — `out` is allocated at len(src) and
// every write is an in-place assignment to an existing index — so all the byte
// offsets the callers carry across the scrub boundary stay valid.
func stripStringsAndComments(src string) string {
	out := make([]byte, len(src))
	i := 0
	inStr := byte(0) // 0=none, '"'=double-quote
	inTriple := false
	inVerbatim := false
	for i < len(src) {
		ch := src[i]
		if inVerbatim {
			// Inside @"..." a `\` is an ORDINARY character, not an escape
			// (#7199). The only escape is a DOUBLED quote `""`, which denotes
			// one literal quote and does NOT close the string — so both bytes
			// are consumed together. A single `"` closes.
			out[i] = ' '
			if ch == '"' {
				if i+1 < len(src) && src[i+1] == '"' {
					out[i+1] = ' '
					i += 2
					continue
				}
				inVerbatim = false
			}
			i++
			continue
		}
		if inTriple {
			out[i] = ' '
			if i+2 < len(src) && ch == '"' && src[i+1] == '"' && src[i+2] == '"' {
				out[i+1] = ' '
				out[i+2] = ' '
				i += 3
				inTriple = false
				continue
			}
			i++
			continue
		}
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
			// Check for triple-quoted string
			if i+2 < len(src) && src[i+1] == '"' && src[i+2] == '"' {
				out[i] = ' '
				out[i+1] = ' '
				out[i+2] = ' '
				i += 3
				inTriple = true
				continue
			}
			// Check for verbatim string @"..." — see verbatimOpenerStart for which
			// prefixes open one, and for why a preceding operator byte disqualifies
			// the opener entirely.
			//
			// TWO DECISIONS, DELIBERATELY SEPARATE (#7199). Whether a verbatim
			// string is OPENED is decided by the lexer's rules alone; whether the
			// opener's `@`/`$` prefix BYTES are blanked is decided by adjacency.
			// Keeping them apart is what lets the runaway fix follow the lexer
			// without betting a FABRICATED EDGE on an unexecuted reading of it:
			//
			//   - OPENING: `@"`, `$@"` and `@$"` open a verbatim string where
			//     lexerOpensTokenAt computes that the lexer starts a token at the
			//     opener — NOT "unconditional on adjacency", and NOT "in any
			//     position where the lexer starts a token" either. BOTH of those
			//     framings shipped on this PR and both were over-stated: the first
			//     reintroduced this issue's own defect on `$$@"`/`x=@"`, and the
			//     second over-claimed by two constructs that are still NOT fixed —
			//     an `@` before a TRIPLE quote (the triple-quote check runs first)
			//     and a run whose start no operator rule can begin at other than
			//     the COLON family (`.:@"`), which falls to the conservative
			//     default. verbatimOpenerStart and lexerOpensTokenAt carry the
			//     rule citations, the exhaustiveness argument and the
			//     measurements.
			//   - BLANKING: done only where the opener does not abut an identifier
			//     or a closing bracket. Where it does — `xs@"abc"`, the shape a
			//     reader is most likely to read as the list-append operator — the
			//     `@` is left VISIBLE, as it was before this mode existed.
			//
			// Why the prefix is blanked at all: a leaked body behind a surviving `@`
			// cannot match the `^\s*`-anchored patterns (moduleRE, the inheritance
			// clauses), so a scrub that stopped suppressing a verbatim body was
			// INVISIBLE to every anchored consumer and the forbidden row watching
			// for it was vacuous. Blanking defends the verbatim path for the same
			// reason the ordinary path is defended, instead of by a delimiter left
			// lying in the output.
			//
			// Why NOT where it abuts an identifier: blanking the `@` is what makes
			// scrubKeepingQuote present ` "` instead of `@"`, which makes spaceAppRE
			// read `helper@"C:\tmp"` as a space application and mint a CALLS edge to
			// `helper`. Per the lexer that edge is CORRECT, but the reading is
			// unexecuted and a wrong edge reads as valid to every consumer while a
			// missing one is detectable, so this one shape declines to mint rather
			// than betting. SCOPED CLAIM, not a general one: the test is spelled as
			// adjacency, so it covers the ZERO-SPACE spelling only. `xs @$"abc"`
			// does mint an `xs` edge — correctly, since rule 670's three-byte match
			// beats the two-byte operator munch there, so it is a GAINED TRUE edge
			// rather than a fabricated one.
			//
			// Length is preserved throughout: every write is an in-place assignment
			// to an index that already exists.
			if start := verbatimOpenerStart(src, i); start >= 0 {
				out[i] = ' '
				if start == 0 || !abutsIdentifier(src[start-1]) {
					for j := start; j < i; j++ {
						out[j] = ' '
					}
				}
				i++
				inVerbatim = true
				continue
			}
			inStr = '"'
			out[i] = ' '
			i++
		case '/':
			// F# line comment: //
			if i+1 < len(src) && src[i+1] == '/' {
				for i < len(src) && src[i] != '\n' {
					out[i] = ' '
					i++
				}
				continue
			}
			out[i] = ch
			i++
		case '(':
			// F# block comment: (* ... *). F# block comments NEST, so this
			// tracks depth instead of stopping at the first `*)` — otherwise
			// the tail of an outer comment stays visible and its tokens (and,
			// for insideBraces, its braces) are read as code. `(*)` is the
			// multiplication operator passed as a function value, not a comment
			// opener, so it is excluded — without that, `List.fold (*) 1 xs`
			// opens a runaway comment that eats the clauses below it.
			//
			// The exclusion is deliberately at the OPENING check only, not
			// inside the nesting loop, so `(* fold with the (*) operator *)`
			// DOES still run away. That is not a defect to fix: F# nests block
			// comments, so the inner `(*` opens a nested comment that the lone
			// `*)` closes and leaves the outer one unbalanced, and fsc rejects
			// such a file outright. Matching the compiler beats out-guessing it.
			// Both halves are pinned by tests — see
			// TestFSharp_MultiplicationOperatorIsNotACommentOpener and
			// TestFSharp_OperatorInsideBlockCommentRunsAway_MatchesCompiler.
			if i+1 < len(src) && src[i+1] == '*' && !(i+2 < len(src) && src[i+2] == ')') {
				depth := 1
				out[i] = ' '
				out[i+1] = ' '
				i += 2
				for i < len(src) && depth > 0 {
					if i+1 < len(src) && src[i] == '(' && src[i+1] == '*' {
						depth++
						out[i] = ' '
						out[i+1] = ' '
						i += 2
						continue
					}
					if i+1 < len(src) && src[i] == '*' && src[i+1] == ')' {
						depth--
						out[i] = ' '
						out[i+1] = ' '
						i += 2
						continue
					}
					out[i] = ' '
					i++
				}
				continue
			}
			out[i] = ch
			i++
		default:
			out[i] = ch
			i++
		}
	}
	// Restore every newline the suppression loops blanked. Done as a single
	// pass rather than at each `out[i] = ' '` site so no future branch can
	// forget it, and length-preserving by construction: it only rewrites bytes
	// that already exist at the same index.
	for i := range src {
		if src[i] == '\n' {
			out[i] = '\n'
		}
	}
	return string(out)
}

// verbatimOpenerStart reports the index at which the VERBATIM-string opener
// ending at the quote src[q] begins, or -1 if src[q] does not open a verbatim
// string.
//
// THE OPENERS ARE THE LEXER'S. dotnet/fsharp `src/Compiler/lex.fsl`, all inside
// `rule token` (line 336), carries exactly these double-quote-opening rules --
// there are no others:
//
//	586  ordinary            -- `\` escapes
//	599  interpolated triple
//	611  extended interpolated triple
//	626  interpolated, NOT verbatim  -- `\` escapes
//	640  triple-quoted
//	655  VERBATIM                     `@` then a quote
//	670  interpolated VERBATIM        `$@` or `@$` then a quote
//
// So `@"`, `$@"` and `@$"` admit a verbatim body and a plain `$"` does not.
//
// BUT AN OPENER ONLY OPENS ANYTHING WHERE THE LEXER STARTS A TOKEN, which is
// what lexerOpensTokenAt decides and what three earlier revisions of this
// function got wrong in the same way -- see that function's comment. Each
// revision approximated "does a token start here" with a LOCAL test on the
// byte(s) next to the opener, and each leaked permissively at a cost of a
// whole-file runaway:
//
//	round 2  no test at all        -> `$$@"`, `.@"`, `x=@"`, `@@"`, ... ran away
//	round 3  reject if prev is an  -> fixed those, but the `=` exception it
//	         op_char, except `=`      added was itself a flat byte test, so
//	         before `$@`/`@$`         `x<=$@"`, `x==$@"`, `.=$@"`, ... ran away
//
// The lesson is in lexerOpensTokenAt: the question is not "what is the byte
// before the opener" but "where does the token containing that byte BEGIN".
//
// THE `xs@"abc"` CASE is admitted, which is longest match read the other way:
// the append rule `967 | ignored_op_char* ('@'|'^') op_char*` cannot include
// the `"` (line 238's `op_char` has no `"`), so at the `@` it matches ONE byte
// while 655 matches TWO. That fslex resolves by longest match is proven from
// this same file rather than assumed: `586` (one quote) PRECEDES `640` (three
// quotes), so under first-match-wins the triple-quote rule would be unreachable
// dead code and F# would have no triple-quoted strings.
//
// NOT EXECUTED -- there is no F# toolchain on this machine, so all of this is
// read off the reference implementation's lexer rather than observed from a
// compile. Because it is unexecuted, the CALLS-edge consequence is declined at
// the call site for the one shape a reader would most likely misread.
//
// Still NOT handled, now stated precisely rather than as "any position where
// the lexer starts a token", which was over-stated: an `@` before a TRIPLE
// quote never reaches here at all, because the triple-quote check runs first --
// a disagreement with the lexer recorded by
// TestScrub7199_AtTripleQuoteIsReadAsTripleQuote_DISAGREES_WITH_FSC.
func verbatimOpenerStart(src string, q int) int {
	start := -1
	if q >= 1 {
		switch src[q-1] {
		case '@':
			if q >= 2 && src[q-2] == '$' {
				start = q - 2 // $@"
			} else {
				start = q - 1 // @"
			}
		case '$':
			if q >= 2 && src[q-2] == '@' {
				start = q - 2 // @$"
			}
		}
	}
	if start < 0 || !lexerOpensTokenAt(src, start, q) {
		return -1
	}
	return start
}

// lexerOpensTokenAt reports whether the F# lexer would START a token at index
// `start`, where src[start:q] is a candidate verbatim opener and src[q] is its
// quote. If a token beginning further left swallows the opener, rule 655/670
// never fires and rule 586 opens an ORDINARY string at the quote -- where `\`
// escapes, which is the difference between suppressing one literal and blanking
// the rest of the file.
//
// WHY THIS WALKS LEFT INSTEAD OF TESTING A BYTE. Every symbolic-operator rule
// in lex.fsl has the shape
//
//	| ignored_op_char* <core> op_char*
//
// with `ignored_op_char = '.' | '$' | '?'` (line 240) and `op_char` (line 238)
// INCLUDING `@`. The trailing `op_char*` is unbounded, so such a token swallows
// an arbitrarily long run -- which is why no fixed-width lookbehind can decide
// this, and why the two previous revisions each leaked on a slightly longer
// prefix than the one before. A non-op_char is a hard boundary for these rules,
// so the run of op_chars ending at the quote is the whole neighbourhood that
// can matter, and the decision is made from ITS START.
//
// THE CASE ANALYSIS IS EXHAUSTIVE, which is the property the byte tests lacked.
// Of the 18 `op_char`s, every one is an `ignored_op_char`, or a `<core>` of some
// rule, or `:` -- and `:` is the ONLY one that is neither (verified by
// enumeration in TestScrub7199_OpCharsAreEitherIgnoredOrCoreOrColon). So from
// the run start exactly three things can happen:
//
//  1. `:` -- starts no operator rule at all, only the fixed COLON family
//     (`:` `::` `:>` `:?` `:=`, lines 846-862). The run continues after it, so
//     `r:=@"C:\"` and `x::@"C:\"` ARE verbatim.
//  2. `=` immediately followed by `$@`/`@$` and then the quote -- rule 976
//     (`| '=' ("$@" | "@$") '"'`) matches four bytes, beating the three-byte
//     operator munch, and consumes ONLY the `=` before rewinding (the file's
//     one and only `LexemeLength <-`), so the opener is re-lexed. This is why
//     `x=$@"` is verbatim while `x=@"` is not: there is no `'=' '@' '"'` rule
//     (verified: zero occurrences). The `p+3 == q` test is what keeps the
//     exception scoped to a `=` that STARTS the run -- without it `x<=$@"` and
//     `x==$@"` run away, which is exactly the round-3 leak.
//  3. anything else -- an operator rule starts at or before this position and
//     its `op_char*` tail swallows the rest of the run, including our opener.
//
// Case 3 also absorbs the `<@` / `<@@` quotation rules (802/804) without
// needing to break their length tie with the operator munch: both readings
// consume the `@`, so both leave rule 586 at the quote and the verdict is the
// same either way.
//
// CONSERVATIVE WHERE IT IS UNSURE: a run whose start is an ignored_op_char
// followed by something that is neither a core nor handled above (`.:@"`, not
// real F#) falls into case 3 and is read as ORDINARY. That is the pre-#7199
// reading, so it can only ever under-fix -- never blank a file that used to
// survive.
func lexerOpensTokenAt(src string, start, q int) bool {
	runStart := start
	for runStart > 0 && isOpChar(src[runStart-1]) {
		runStart--
	}
	for p := runStart; p < start; {
		switch {
		case src[p] == ':':
			if p+1 < q && (src[p+1] == ':' || src[p+1] == '>' || src[p+1] == '?' || src[p+1] == '=') {
				p += 2
			} else {
				p++
			}
		case src[p] == '=' && p+3 == q &&
			(src[p+1] == '$' && src[p+2] == '@' || src[p+1] == '@' && src[p+2] == '$'):
			p++ // rule 976: consumes the `=` only, then rewinds
		default:
			return false // an operator token swallows the opener
		}
	}
	// No overshoot check is needed, and one that was here has been DELETED as
	// dead code after it scored ALIVE at 0. `p` advances by two only in the
	// COLON branch, and only when src[p+1] is one of `:` `>` `?` `=`; the
	// opener's first byte is always `@` or `$`, neither of which is in that
	// set, so `p+1 == start` always takes the one-byte branch and `p` can never
	// step OVER `start`. The loop therefore exits with `p == start` exactly,
	// and the `p < start` condition is the only bound required. The three
	// COLON cells in TestScrub7199_OpenerFormsAndAdjacency exercise the
	// two-byte step.
	return true
}

// isOpChar reports whether b is one of lex.fsl's `op_char` (line 238). Kept as
// the literal set from that line, in that order, so it can be diffed against
// the source it came from.
func isOpChar(b byte) bool {
	switch b {
	case '!', '$', '%', '&', '*', '+', '-', '.', '/', '<', '=', '>', '?', '@', '^', '|', '~', ':':
		return true
	}
	return false
}

// abutsIdentifier reports whether b is a byte an F# identifier or a closing
// bracket can end with -- i.e. whether a `@"` immediately after it is the shape
// where a reader could instead see the list-append operator applied to a string
// literal. Used ONLY to decide whether the opener's prefix bytes are blanked,
// never whether a verbatim string is opened. Non-ASCII is included because F#
// identifiers admit Unicode letters.
//
// NO `'.'` ARM, deliberately, and this is a correction: one was here and was
// UNREACHABLE. `.` is an `op_char`, so a `.`-preceded opener is rejected by
// lexerOpensTokenAt before this function is consulted, and the arm could never
// fire. It is deleted rather than kept as documentation of coverage that
// cannot be exercised. The bytes that DO reach here are `)`, `]`, `}`, `_`,
// a backtick, an apostrophe, alphanumerics, non-ASCII -- none of which is an
// op_char -- plus `=`, which arrives via rule 976 and is deliberately not in
// the set, so `x=$@"..."` has its prefix blanked.
func abutsIdentifier(b byte) bool {
	switch {
	case b >= '0' && b <= '9',
		b >= 'a' && b <= 'z',
		b >= 'A' && b <= 'Z',
		b >= 0x80:
		return true
	}
	switch b {
	case '_', '\'', '`', ')', ']', '}':
		return true
	}
	return false
}
