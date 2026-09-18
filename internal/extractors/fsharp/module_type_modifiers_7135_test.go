package fsharp_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// #7135 (`moduleRE` + `typeRE` arm) — both scanners accommodated a FIXED
// modifier sequence, so an access modifier made the whole declaration vanish:
//
//	moduleRE = ^([ \t]*)module(?:\s+rec)?\s+([\w.]+)\s*$
//	typeRE   = ^([ \t]*)type\s+([A-Z][a-zA-Z0-9_']*)...=
//
// Measured against the extractor at e96eb099f, BEFORE this change:
//
//	module private Foo          -> 0 entities   (want 1)
//	module internal Foo         -> 0 entities   (want 1)
//	module rec private Foo      -> 0 entities   (want 1)
//	type private Foo = { A: 1 } -> 0 type entities (want 1)
//	type internal Foo() = ...   -> 0 type entities (want 1)
//	module Foo / module rec Foo -> 1 entity     (the two accommodated shapes)
//
// This is the SILENT-MISS mode, and it is the harder one to notice: a
// mis-named entity at least appears in a listing looking odd, while a missed
// one leaves no trace and reads as "this file has no modules" — a plausible
// answer that no bind-rate, orphan-rate or dangle instrument can flag. So
// every assertion below is on the entity COUNT, not only on presence: an
// absence is exactly what a presence-only assertion cannot see.
//
// Why a miss and not a mis-name (the two scanners differ, and for different
// reasons):
//
//   - moduleRE anchors the line end (`\s*$`). `module private Foo` leaves
//     ` Foo` after `[\w.]+` has taken `private`, the anchor fails, and the
//     match is abandoned altogether. (A bare `module private` with no name
//     DOES still capture `private`; that is unchanged by this commit and is
//     recorded in TestModuleModifiers_BareKeywordUnchanged.)
//   - typeRE requires the name to start `[A-Z]`. `private` is lower-case, so
//     there is nothing for the name capture to take and the match fails.
//
// WHICH SOURCES WERE UNIONED, per construct. The `memberRE` arm (c1e2c3008)
// paid for this lesson: § 8.13's `member-defn` production genuinely omits
// `inline`, and deriving the set from that one production shipped a fix that
// left the defect alive for `member inline this.Method`. A single grammar
// production is not an exhaustive modifier set. Four sources per construct:
//
// MODULE — set is {rec, public, private, internal}:
//
//  1. F# Language Specification § 10 "Namespaces and Modules"
//     (https://fsharp.github.io/fslang-spec/namespaces-and-modules/):
//     `module-defn := attributes? module access? ident = module-defn-body`,
//     `module-abbrev := module ident = long-ident`.
//  2. § 10 / § 10.5 accessibility: `access := private | internal | public`.
//  3. MS Learn "Modules" (learn.microsoft.com/dotnet/fsharp/language-
//     reference/modules) — the syntax block is
//     `module [accessibility-modifier] [qualified-namespace.]module-name`
//     for the top-level form, and "The accessibility-modifier can be one of
//     the following: public, private, internal". The SAME page documents
//     `module rec` under "Recursive modules" ("F# 4.1 introduces ... modules
//     which allow for all contained code to be mutually recursive. This is
//     done via `module rec`") — and the § 10 production above does NOT carry
//     `rec`. That is the second independent instance of the lesson: the
//     grammar production alone would have dropped `rec`, which the CURRENT
//     pattern already accommodates, so deriving from it would have been a
//     REGRESSION.
//  4. The sibling scanner in this file: letRE (#7131) allowlists
//     `rec|mutable|inline|private|internal|public`.
//
// TYPE — set is {public, private, internal}, and nothing else:
//
//  1. F# Language Specification § 8 "Type Definitions"
//     (https://fsharp.github.io/fslang-spec/type-definitions/):
//     `type-name := attributes? access? ident typar-defns?`, and every
//     type-defn variant (abbrev / record / union / class / struct /
//     interface / enum / delegate) is built on `type-name`. No production
//     admits any keyword other than `access` between `type` and the ident.
//  2. § 10.5 accessibility annotations, same `access` production.
//  3. MS Learn "Access Control" (learn.microsoft.com/dotnet/fsharp/
//     language-reference/access-control): "the access control specifiers
//     public, internal, and private can be applied to modules, types,
//     methods, value definitions, functions, properties, and explicit
//     fields", "The access specifier is put in front of the name of the
//     entity", with the worked examples `type private MyPrivateType()` and
//     `type internal MyInternalType()`. The same page states that
//     `protected` "is not used in F#" — so it is deliberately NOT in either
//     allowlist. A fabricated modifier is a widening with no real-world
//     case, and an all-DEAD mutant score cannot detect one.
//  4. The sibling scanners in this file: memberRE (this issue's first arm)
//     and letRE, whose access sets are the same three words. There is no
//     `type rec` — F# expresses recursive types with `type A = ... and B`,
//     so `rec` is in the module set and NOT in the type set.
//
// There is NO F# toolchain in this environment (`dotnet`, `fsc`, `fsharpc`,
// `fsi` and `mono` are all absent; `javac` is the only compiler present), so
// every legality statement here is DERIVED FROM THE SOURCES ABOVE AND NOT
// EXECUTED. No fixture below was compiled, and no header sentence claims that
// every row is legal F#: the labels are per row.
//
// Axes VARIED: construct (`module` / `type`); modifier identity
// (`rec`, `public`, `private`, `internal`, and the EXCLUDED `protected`);
// modifier count (0, 1, and 2); modifier ORDER (`rec private` vs
// `private rec`); repetition of an ACCESS modifier (`private private`); the
// module name's qualification (`Target` vs `Ns.Sub.Target`); the type's body
// shape (record, DU, primary-constructor args, interface, alias, generic);
// presence of a generic parameter list; indentation (0, 2, 4 and a tab); and
// same-name vs distinct-name siblings.
//
// Axes deliberately NOT varied, so the labels do not claim more than the
// fixtures contain: there is no `rec rec` row (only the access modifiers are
// repeated) and no three-modifier row. The module table is 14 modifier
// phrases x 2 names = 28 rows; the type table is 7 phrases x 8 body shapes =
// 56 rows.
//
// Axes HELD CONSTANT: the declared name (always `Target`, or `Ns.Target`
// where qualification is the axis, so a wrong capture is unambiguous); the
// entity kind (SCOPE.Component); one declaration of interest per source file;
// and the file path.
//
// Two PRE-EXISTING defects were measured while enumerating this space. Both
// are independent of the modifier sequence — they reproduce with NO modifier
// at all — so neither is fixed here, and neither is asserted as correct:
//
//   - LOCAL MODULES WERE MISSED ENTIRELY. moduleRE's `\s*$` meant the local
//     form `module Target =` never matched, with or without a modifier. See
//     TestModuleModifiers_LocalModuleEqualsGapIsSeparate, which asserts the
//     RELATION (the modifier changes nothing) rather than the gap. FIXED by
//     #7151; the relation above still holds and now grades the fix.
//   - BLOCK COMMENTS AND TRIPLE-QUOTED STRINGS PRODUCE PHANTOMS. A line
//     `module Ghost` inside `(* ... *)` or `"""..."""` is extracted today.
//     Line comments (`//`, `///`) are correctly ignored, and that direction
//     IS graded here — see TestModuleTypeModifiers_OnlyRealDeclarations.
//
// WHAT THE COUNT ASSERTIONS DID AND DID NOT BUY, measured rather than
// asserted. Deleting every count assertion from both tables and reverting the
// extractor to its pre-fix patterns still produces 88 `--- FAIL` — the SAME
// number as the real red run — and deleting the count from
// TestModuleTypeModifiers_OnlyRealDeclarations while dropping both line
// anchors still produces 1, the same as the count-keeping control. So for THIS
// defect family the counts killed nothing a per-name check did not also kill:
// a TOTAL miss is equally visible to both directions, and the phantom names in
// the fixture are authored, so a per-name check can enumerate them. The counts
// are kept anyway because they are the direction that does not require a test
// author to predict a phantom's name — but they are NOT claimed as graded
// here, and that claim would have been wrong.
//
// What DID need its own grading is the opposite direction, and no count could
// reach it: the allowlist's UPPER boundary. See
// TestModuleTypeModifiers_AllowlistUpperBoundary — before it existed, adding
// `protected` to either group or `rec` to the type group survived the entire
// package suite at 0 `--- FAIL`.
//
// A related hole that NEITHER direction closes, and it is not closed by this
// commit: the module scan keys `"module:" + name` and typeSeen keys the bare
// name, so a phantom sharing a REAL declaration's name is silently merged into
// it and is invisible to a count and to a presence check alike. That is the
// same family as the open #7144 (a dedup key with no enclosing scope).

// fsModuleNames lists the names of every module SCOPE.Component, in order.
func fsModuleNames(ents []types.EntityRecord) []string {
	var out []string
	for i := range ents {
		if ents[i].Kind == "SCOPE.Component" && ents[i].Subtype == "module" {
			out = append(out, ents[i].Name)
		}
	}
	return out
}

// fsTypeSubtypes is the set of classifyTypeSubtype results, i.e. every
// SCOPE.Component a `type` declaration can produce. `module` and `namespace`
// are Components too, so a type census must name the subtypes rather than
// count Components.
var fsTypeSubtypes = map[string]bool{
	"record": true, "discriminated_union": true, "interface": true,
	"class": true, "struct": true, "alias": true, "type": true,
	"computation_builder": true,
}

// fsTypeNames lists the names of every type-declaration SCOPE.Component.
func fsTypeNames(ents []types.EntityRecord) []string {
	var out []string
	for i := range ents {
		if ents[i].Kind == "SCOPE.Component" && fsTypeSubtypes[ents[i].Subtype] {
			out = append(out, ents[i].Name)
		}
	}
	return out
}

// fsModTypeModifierWords are the words that must never become an entity name.
// The complementary direction to recall: a count-and-name check on `Target`
// cannot see an entity minted under a modifier's name.
var fsModTypeModifierWords = []string{
	"rec", "public", "private", "internal", "module", "type", "protected",
}

func fsNoModifierNamedEntity(t *testing.T, ents []types.EntityRecord, decl string, g fsGrammar) {
	t.Helper()
	for _, bad := range fsModTypeModifierWords {
		for i := range ents {
			if ents[i].Name == bad {
				t.Errorf("modifier %q recorded as an entity name (%s/%s, line %d) for %q [%s]",
					bad, ents[i].Kind, ents[i].Subtype, ents[i].StartLine, decl, g)
			}
		}
	}
}

// lenienceRepetition is a fourth grammar label. `lenienceOnly` is defined in
// let_modifiers_7131_test.go as pinning lenience toward a REVERSED modifier
// ORDER, which is not what a repeated access modifier is: no source admits two
// access modifiers on one declaration in ANY order. The repeated-allowlist
// construction tolerates it on purpose, and mislabelling it would make a claim
// about ordering that the row does not test.
const lenienceRepetition fsGrammar = "NOT legal F# — pins scanner lenience to a REPEATED modifier"

// modifierPhrases enumerates the modifier space for a construct as the CROSS
// PRODUCT of its allowlist rather than a hand-picked sample: the empty case,
// each modifier alone, both orders of every cross-kind pair, and each ACCESS
// modifier repeated. Hand-picking is how a two-slot fixed sequence came to be
// written in the first place.
//
// What this does NOT generate, so the description claims no more than the rows
// contain: only the `access` words are repeated — there is no `rec rec` row
// (nothing repeats a word from `extra`) and no three-modifier row, since every
// phrase is one or two words. This is a SECOND copy of the header's axis list,
// and it was stale once already; keep the two in step.
//
// The label is per row. A pair of `rec` with an access modifier is attested by
// neither order (MS Learn shows `module rec X` and `module private X`
// separately, and the § 10 production carries no `rec` at all), so both orders
// are `lenienceOnly`; a repeated access modifier is `lenienceRepetition`.
func modifierPhrases(access []string, extra []string) []struct {
	phrase  string
	grammar fsGrammar
} {
	var out []struct {
		phrase  string
		grammar fsGrammar
	}
	add := func(p string, g fsGrammar) {
		out = append(out, struct {
			phrase  string
			grammar fsGrammar
		}{p, g})
	}
	add("", specLegal)
	for _, a := range access {
		add(a, specLegal)
	}
	for _, e := range extra {
		add(e, specLegal)
		for _, a := range access {
			add(e+" "+a, lenienceOnly)
			add(a+" "+e, lenienceOnly)
		}
	}
	for _, a := range access {
		add(a+" "+a, lenienceRepetition)
	}
	return out
}

var fsAccess = []string{"public", "private", "internal"}

// TestModuleModifiers_ModuleIsNeverMissed enumerates the module modifier
// space. Each row must yield EXACTLY ONE module entity under the declared
// name. The count is the load-bearing assertion: `module private Foo`
// produced zero entities before this change, and zero entities is invisible
// to a presence-only check.
func TestModuleModifiers_ModuleIsNeverMissed(t *testing.T) {
	for _, name := range []string{"Target", "Ns.Sub.Target"} {
		for _, mp := range modifierPhrases(fsAccess, []string{"rec"}) {
			g := mp.grammar
			decl := strings.TrimRight("module "+mp.phrase, " ") + " " + name
			t.Run(fmt.Sprintf("%s/%s", name, mp.phrase), func(t *testing.T) {
				src := decl + "\n\nlet x = 1\n"
				ents := runFSharp(t, src, "Mod.fs")
				got := fsModuleNames(ents)
				if len(got) != 1 {
					t.Errorf("%q [%s] produced %d module entities, want 1: %v",
						decl, g, len(got), got)
				}
				if len(got) != 1 || got[0] != name {
					t.Errorf("%q [%s] named module %v, want [%s]", decl, g, got, name)
				}
				fsNoModifierNamedEntity(t, ents, decl, g)
			})
		}
	}
}

// TestTypeModifiers_TypeIsNeverMissed enumerates the type modifier space
// CROSSED with the type-defn body shapes, because `type-name` is shared by
// every variant (§ 8) and a fix that only reached records would leave the DU,
// class and alias productions missed. Count-asserted for the same reason as
// the module table.
func TestTypeModifiers_TypeIsNeverMissed(t *testing.T) {
	shapes := []struct {
		name    string
		tail    string // everything from the type name onwards
		subtype string
	}{
		{"record", "Target = { A: int }", "record"},
		{"record, indented body", "Target =\n    { A: int }", "record"},
		{"discriminated_union", "Target =\n    | A\n    | B", "discriminated_union"},
		// MEASURED, not assumed: a primary-constructor type with no `class`
		// keyword classifies as the catch-all "type" (classifyTypeSubtype
		// looks for the literal keyword), so that is what the row asserts —
		// the same value the unmodified declaration produces today.
		{"ctor args", "Target() =\n    member this.X = 1", "type"},
		{"generic ctor args", "Target<'T>(x: 'T) =\n    member this.X = x", "type"},
		{"generic record", "Target<'T> = { A: 'T }", "record"},
		{"alias", "Target = string", "alias"},
		{"interface", "Target =\n    interface\n        abstract Go: unit -> unit\n    end", "interface"},
	}
	for _, sh := range shapes {
		for _, mp := range modifierPhrases(fsAccess, nil) {
			g := mp.grammar
			decl := strings.TrimRight("type "+mp.phrase, " ") + " " + sh.tail
			t.Run(fmt.Sprintf("%s/%s", sh.name, mp.phrase), func(t *testing.T) {
				src := "module M\n\n" + decl + "\n"
				ents := runFSharp(t, src, "Types.fs")
				got := fsTypeNames(ents)
				if len(got) != 1 {
					t.Errorf("%q [%s] produced %d type entities, want 1: %v",
						decl, g, len(got), got)
				}
				if e := fsFind(ents, "Target", "SCOPE.Component"); e == nil {
					t.Errorf("no type SCOPE.Component named \"Target\" for %q [%s]; got %v",
						decl, g, got)
				} else if e.Subtype != sh.subtype {
					t.Errorf("%q [%s] classified as subtype %q, want %q",
						decl, g, e.Subtype, sh.subtype)
				}
				fsNoModifierNamedEntity(t, ents, decl, g)
			})
		}
	}
}

// TestModuleTypeModifiers_NoNameCollapse is the dedup-key check the letRE and
// memberRE arms both needed, asked of THESE two scanners specifically.
//
// FINDING, and it is not the same shape as letSeen/memberSeen: the module
// scan's key is `"module:" + name` with NO indent, and typeSeen's key is the
// BARE name — both are file-wide by name, weaker than the indent+name keys
// that #7144 is open about. This commit does not touch either key.
//
// A collapse was NOT reachable through the old defect: both scanners MISSED
// the modified declaration rather than mis-naming it, so there was never a
// modifier-shaped key for siblings to share. This test therefore pins the
// direction the widening could break — distinct-named modified siblings must
// stay distinct — by COUNT, since a collapse shows up as a missing entity.
func TestModuleTypeModifiers_NoNameCollapse(t *testing.T) {
	t.Run("modules", func(t *testing.T) {
		src := "module private Alpha\n\nmodule private Beta\n\nmodule internal Gamma\n"
		ents := runFSharp(t, src, "Mods.fs")
		got := fsModuleNames(ents)
		if len(got) != 3 {
			t.Errorf("3 distinct private/internal modules produced %d entities, want 3: %v",
				len(got), got)
		}
		for _, want := range []string{"Alpha", "Beta", "Gamma"} {
			if fsFind(ents, want, "SCOPE.Component") == nil {
				t.Errorf("module %q missing; got %v", want, got)
			}
		}
	})
	t.Run("types", func(t *testing.T) {
		src := "module M\n\ntype private Alpha = { A: int }\ntype private Beta = { B: int }\ntype internal Gamma = { C: int }\n"
		ents := runFSharp(t, src, "Types.fs")
		got := fsTypeNames(ents)
		if len(got) != 3 {
			t.Errorf("3 distinct private/internal types produced %d entities, want 3: %v",
				len(got), got)
		}
		for _, want := range []string{"Alpha", "Beta", "Gamma"} {
			if fsFind(ents, want, "SCOPE.Component") == nil {
				t.Errorf("type %q missing; got %v", want, got)
			}
		}
	})
}

// TestTypeModifiers_IndentedType varies the indentation axis on legal F#: a
// type inside a local module is indented, and typeRE captures the indent it
// then uses to delimit the body. The type is what is asserted here; the
// enclosing `module Inner =` is missed for an unrelated reason (see
// TestModuleModifiers_LocalModuleEqualsGapIsSeparate).
func TestTypeModifiers_IndentedType(t *testing.T) {
	src := `namespace N

module Inner =

    type private Target =
        { A: int
          B: string }

    let use (t: Target) = t.A
`
	ents := runFSharp(t, src, "Indented.fs")
	got := fsTypeNames(ents)
	if len(got) != 1 {
		t.Fatalf("indented `type private` produced %d type entities, want 1: %v", len(got), got)
	}
	e := fsFind(ents, "Target", "SCOPE.Component")
	if e == nil {
		t.Fatalf("no type named Target; got %v", got)
	}
	if e.Subtype != "record" {
		t.Errorf("subtype = %q, want record", e.Subtype)
	}
	// The indent capture delimits the body: both fields must be inside it.
	for _, f := range []string{"Target.A", "Target.B"} {
		if fsFind(ents, f, "SCOPE.Schema") == nil {
			t.Errorf("field entity %q missing — the indented body was not delimited", f)
		}
	}
}

// TestModuleModifiers_IndentIsNotLoadBearing pins that the widened modifier
// group does not consume the indent capture. An indented module with no `=`
// is scanner lenience, not legal F# (a local module requires the `=`), and is
// labelled as such; what it grades is that the indent group and the modifier
// group stay separate.
func TestModuleModifiers_IndentIsNotLoadBearing(t *testing.T) {
	for _, indent := range []string{"", "  ", "\t", "    "} {
		g := specLegal
		if indent != "" {
			g = lenienceOnly
		}
		src := indent + "module private Target\n"
		ents := runFSharp(t, src, "Mod.fs")
		got := fsModuleNames(ents)
		if len(got) != 1 || got[0] != "Target" {
			t.Errorf("indent %q [%s]: got %v, want [Target]", indent, g, got)
		}
	}
}

// TestModuleTypeModifiers_ModifierPrefixedNames is the over-firing direction
// of the widening: a declared name that merely STARTS with a modifier word
// must be captured whole. This is what the `\b` in each modifier group states,
// and the behaviour must hold whether or not the boundary is load-bearing.
func TestModuleTypeModifiers_ModifierPrefixedNames(t *testing.T) {
	cases := []struct {
		src      string
		wantMods []string
		wantType []string
	}{
		{"module recompute\n", []string{"recompute"}, nil},
		{"module publicity\n", []string{"publicity"}, nil},
		{"module Privateer\n", []string{"Privateer"}, nil},
		{"module internals\n", []string{"internals"}, nil},
		{"module M\n\ntype Privateer = { A: int }\n", []string{"M"}, []string{"Privateer"}},
		{"module M\n\ntype Internally = { A: int }\n", []string{"M"}, []string{"Internally"}},
		{"module M\n\ntype Recompute = { A: int }\n", []string{"M"}, []string{"Recompute"}},
	}
	for _, tc := range cases {
		ents := runFSharp(t, tc.src, "Prefixed.fs")
		if got := fsModuleNames(ents); !eqStrs(got, tc.wantMods) {
			t.Errorf("%q: module names = %v, want %v", tc.src, got, tc.wantMods)
		}
		if got := fsTypeNames(ents); !eqStrs(got, tc.wantType) {
			t.Errorf("%q: type names = %v, want %v", tc.src, got, tc.wantType)
		}
	}
}

func eqStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestModuleTypeModifiers_OnlyRealDeclarations grades the `^` line anchor in
// the OVER-FIRING direction for the widened patterns: recall alone cannot
// detect a pattern that fires too often. A modified declaration inside a line
// comment or a doc comment must mint nothing, and the COUNT is asserted with a
// real declaration of each construct as a positive control — three absences
// are invisible to a per-name check, and a phantom minted under an unexpected
// name would slip through a name-only assertion.
//
// NOT asserted here: `(* ... *)` block comments and `"""..."""` strings DO
// produce phantoms today, with or without a modifier (measured at e96eb099f:
// `module Ghost` inside a block comment yields a module entity). That is a
// pre-existing over-fire of the `^`-anchored scan, independent of this change,
// and is reported rather than pinned.
func TestModuleTypeModifiers_OnlyRealDeclarations(t *testing.T) {
	src := `module Real

// module private CommentGhost
/// type private DocGhost = { A: int }
//  module internal IndentedCommentGhost

type private RealType = { A: int }
`
	ents := runFSharp(t, src, "Real.fs")
	mods := fsModuleNames(ents)
	typs := fsTypeNames(ents)
	if len(mods) != 1 || mods[0] != "Real" {
		t.Errorf("module names = %v, want exactly [Real]", mods)
	}
	if len(typs) != 1 || typs[0] != "RealType" {
		t.Errorf("type names = %v, want exactly [RealType]", typs)
	}
	for _, ghost := range []string{"CommentGhost", "DocGhost", "IndentedCommentGhost"} {
		if e := fsFind(ents, ghost, "SCOPE.Component"); e != nil {
			t.Errorf("phantom entity %q from a comment line (%s, line %d)",
				ghost, e.Subtype, e.StartLine)
		}
	}
}

// TestTypeModifiers_PrivateTypeKeepsItsSurface grades the DOWNSTREAM
// consumers of the widened typeRE rather than merely disclosing them. A type
// the scanner never saw had no members, no CONTAINS edges, no record fields
// and no record-type registration, so the miss cost more than one node.
func TestTypeModifiers_PrivateTypeKeepsItsSurface(t *testing.T) {
	src := `module M

type private Money(amount: decimal) =
    member this.Amount = amount
    static member Zero = Money(0m)

type internal Order =
    { Id: int
      Total: decimal }
`
	ents := runFSharp(t, src, "Surface.fs")

	money := fsFind(ents, "Money", "SCOPE.Component")
	if money == nil {
		t.Fatalf("no Component for `type private Money`; types = %v", fsTypeNames(ents))
	}
	// type -> member CONTAINS edges.
	var contains []string
	for _, r := range money.Relationships {
		if r.Kind == "CONTAINS" {
			contains = append(contains, r.ToID)
		}
	}
	if len(contains) != 2 {
		t.Errorf("`type private Money` has %d CONTAINS edges, want 2 (Amount, Zero): %v",
			len(contains), contains)
	}
	for _, want := range []string{"Amount", "Zero"} {
		found := false
		for _, id := range contains {
			if strings.Contains(id, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("no CONTAINS edge to member %q from `type private Money`: %v", want, contains)
		}
	}

	// record fields of an `internal` record become SCOPE.Schema sub-entities.
	order := fsFind(ents, "Order", "SCOPE.Component")
	if order == nil {
		t.Fatalf("no Component for `type internal Order`; types = %v", fsTypeNames(ents))
	}
	if order.Subtype != "record" {
		t.Errorf("`type internal Order` subtype = %q, want record", order.Subtype)
	}
	for _, f := range []string{"Order.Id", "Order.Total"} {
		if fsFind(ents, f, "SCOPE.Schema") == nil {
			t.Errorf("field entity %q missing from `type internal Order`", f)
		}
	}
}

// fsRelTargets lists the ToIDs of an entity's relationships of one kind.
func fsRelTargets(e *types.EntityRecord, kind string) []string {
	var out []string
	for _, r := range e.Relationships {
		if r.Kind == kind {
			out = append(out, r.ToID)
		}
	}
	return out
}

// TestTypeModifiers_ModifiedTypeKeepsItsTopology observes the two downstream
// consequences this change claims that nothing else in the suite watches: the
// HIERARCHY edges (#6326) and collectRecordTypeNames.
//
// A type the scanner never saw is not one missing node. It is also an
// `inherit` with no EXTENDS, an `interface ... with` with no IMPLEMENTS, and —
// because collectRecordTypeNames runs typeRE a second time to build the set of
// in-file RECORD names — a nested-model VALIDATES edge from a PLAIN record
// onto a modified one that cannot resolve. That last one is the direction
// worth watching: the entity that loses an edge is not the modified type, so
// an access modifier on one declaration degrades a different, unmodified one.
func TestTypeModifiers_ModifiedTypeKeepsItsTopology(t *testing.T) {
	t.Run("hierarchy edges", func(t *testing.T) {
		src := `module M

type Base() =
    member this.Go () = 1

type IThing =
    abstract Go: unit -> int

type private Derived() =
    inherit Base()
    interface IThing with
        member this.Go () = 2
`
		ents := runFSharp(t, src, "Hier.fs")
		d := fsFind(ents, "Derived", "SCOPE.Component")
		if d == nil {
			t.Fatalf("no Component for `type private Derived()`; types = %v", fsTypeNames(ents))
		}
		if got := fsRelTargets(d, "EXTENDS"); !eqStrs(got, []string{"Base"}) {
			t.Errorf("`type private Derived()` EXTENDS = %v, want [Base]", got)
		}
		if got := fsRelTargets(d, "IMPLEMENTS"); !eqStrs(got, []string{"IThing"}) {
			t.Errorf("`type private Derived()` IMPLEMENTS = %v, want [IThing]", got)
		}
	})

	t.Run("nested-model VALIDATES onto a modified record", func(t *testing.T) {
		src := `module M

type internal Address = { City: string }

type Person =
    { Name: string
      Home: Address }
`
		ents := runFSharp(t, src, "Nested.fs")
		// The edge is on the UNMODIFIED record, and it resolves only because
		// collectRecordTypeNames saw `type internal Address` as a record.
		p := fsFind(ents, "Person", "SCOPE.Component")
		if p == nil {
			t.Fatalf("no Component for `type Person`; types = %v", fsTypeNames(ents))
		}
		if got := fsRelTargets(p, "VALIDATES"); !eqStrs(got, []string{"Address"}) {
			t.Errorf("`type Person` VALIDATES = %v, want [Address] — the nested record "+
				"`type internal Address` did not reach collectRecordTypeNames", got)
		}
	})
}

// TestModuleTypeModifiers_AllowlistUpperBoundary grades the allowlist's UPPER
// boundary — the direction every must-have row above is structurally blind to.
//
// Recall rows can only show that an accepted modifier is not LOST; nothing in
// them can fail if the allowlist accepts a word F# does not. Measured on the
// shipped patterns, three widening mutants were ALIVE at 0 `--- FAIL` each:
// adding `protected` to moduleRE's group, adding `protected` to typeRE's, and
// adding `rec` to typeRE's. Both exclusions this change reasons about
// therefore existed only in PROSE, which is this repo's dominant defect class.
// `fsModTypeModifierWords` does not help: it forbids an entity NAMED
// `protected`, not an entity minted BECAUSE `protected` was accepted.
//
// The two exclusions, and their sources:
//
//   - `protected` is not an F# access specifier. MS Learn "Access Control":
//     "The access specifier `protected` is not used in F#, although it is
//     acceptable if you are using types authored in languages that do support
//     `protected` access" — a statement about OVERRIDING an inherited member,
//     not about a declaration-site modifier. It belongs in neither allowlist.
//   - There is no `type rec`. § 8's `type-name := attributes? access? ident
//     typar-defns?` admits only `access`, and F# expresses recursive types
//     with `type A = ... and B = ...`. `rec` is a MODULE modifier (MS Learn
//     "Recursive modules") and it is in moduleRE's set only.
//
// Each row is paired with a POSITIVE CONTROL that differs from it in exactly
// the modifier word, so the row cannot pass because the harness sees nothing:
// the control must mint 1 while the forbidden form mints 0. That is also the
// relation a one-sided future widening breaks — accept `protected` and the
// control still passes while the forbidden row fails.
func TestModuleTypeModifiers_AllowlistUpperBoundary(t *testing.T) {
	cases := []struct {
		name string
		// forbidden and control differ ONLY in the modifier word.
		forbidden, control string
		// census picks the construct the row is about.
		census func([]types.EntityRecord) []string
	}{
		{
			"module: `protected` is not an F# access specifier",
			"module protected Target\n\nlet x = 1\n",
			"module private Target\n\nlet x = 1\n",
			fsModuleNames,
		},
		{
			"type: `protected` is not an F# access specifier",
			"module M\n\ntype protected Target = { A: int }\n",
			"module M\n\ntype private Target = { A: int }\n",
			fsTypeNames,
		},
		{
			"type: there is no `type rec` (rec is a MODULE modifier)",
			"module M\n\ntype rec Target = { A: int }\n",
			"module M\n\ntype internal Target = { A: int }\n",
			fsTypeNames,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.census(runFSharp(t, tc.forbidden, "Forbidden.fs")); len(got) != 0 {
				t.Errorf("%q minted %d entities %v — the allowlist accepts a word no "+
					"source admits for this construct", tc.forbidden, len(got), got)
			}
			// Positive control: the same shape with an attested modifier must
			// still mint exactly one, so the row above is a real exclusion and
			// not a silent harness failure.
			if got := tc.census(runFSharp(t, tc.control, "Control.fs")); len(got) != 1 {
				t.Errorf("positive control %q minted %d entities %v, want 1 — the "+
					"forbidden row above proves nothing without it", tc.control, len(got), got)
			}
		})
	}
}

// TestModuleModifiers_LocalModuleEqualsGapIsSeparate records a defect found
// while enumerating this space, WITHOUT asserting that it is correct.
// moduleRE ended `\s*$`, so the LOCAL module form `module Target =`
// (MS Learn "Modules": `module [accessibility-modifier] module-name =`) never
// matched — with or without a modifier. It was therefore not the
// fixed-modifier-sequence defect and was not fixed here.
//
// What is asserted is the RELATION: the modifier makes no difference to this
// form. The test stays honest if the gap is later closed — it fails only if
// one of the two is fixed and the other is not, which is exactly the pairing
// a future fix must not get wrong.
//
// THE GAP IS NOW CLOSED, by #7151 (`([ \t]*=)?` on moduleRE). Both sides of
// this relation are 1 rather than 0, so the relation now grades the fix
// instead of the gap — and it is exactly the property #7151's one-sided-fix
// guard needed. The must-have rows live in local_module_7151_test.go; this
// one is kept because it is the pairing assertion, not a recall row.
func TestModuleModifiers_LocalModuleEqualsGapIsSeparate(t *testing.T) {
	plain := runFSharp(t, "namespace N\n\nmodule Target =\n    let x = 1\n", "Local.fs")
	modified := runFSharp(t, "namespace N\n\nmodule private Target =\n    let x = 1\n", "Local.fs")
	if a, b := len(fsModuleNames(plain)), len(fsModuleNames(modified)); a != b {
		t.Errorf("`module Target =` produced %d module entities and `module private Target =` produced %d; "+
			"the access modifier must make no difference to the local-module form", a, b)
	}
}

// TestModuleModifiers_BareKeywordUnchanged pins the one shape where moduleRE
// does NOT miss a modifier: a bare `module private` with no name captures
// `private` AS the name, both before and after this change, because the
// repeated group backtracks to zero repetitions when no name follows. It is
// not legal F# and the widening neither introduces nor removes it; the row
// exists so the behaviour is recorded rather than discovered later and read as
// a regression of this commit.
func TestModuleModifiers_BareKeywordUnchanged(t *testing.T) {
	ents := runFSharp(t, "module private\n", "Bare.fs")
	got := fsModuleNames(ents)
	if !eqStrs(got, []string{"private"}) {
		t.Errorf("`module private` (no name) produced %v, want [private] — "+
			"measured at e96eb099f before this change", got)
	}
}
