package fsharp_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// # THE ARTEFACT-LEVEL ROWS FOR #7199
//
// The helper-level rows live in verbatim_string_7199_internal_test.go. These
// rows assert what a CONSUMER sees: the entities and edges the runaway removed.
// stripStringsAndComments has nine non-test call sites in this package, and a
// DEAD verdict at one says nothing about another, so the two scored here are
// named:
//
//   - extractFSharp's LOCAL-module gate (extractor.go:643) — the site where a
//     runaway costs an ENTITY, not merely an edge (#7192 made the local
//     `module Foo =` declaration depend on the scrub).
//   - collectHierarchyEdges (extractor.go:~1330) — the heaviest consumer; a
//     runaway costs the EXTENDS/IMPLEMENTS edges below the trigger.
//
// LEFT UNSCORED, stated rather than implied: collectCalls (:1512),
// scrubKeepingQuote (:1589), validators.go:81 and :357, and
// compexpr_active_patterns.go:568, :601 and :687. All seven take the same
// return value from the same helper and none of them re-derives the scan, so
// the fix reaches them by construction — but that is an argument, not a
// measurement, and no row below observes them.
//
// Member entities are NOT affected at the hierarchy site: the member scan does
// not consult the scrub, so `Dispose` below a `@"C:\"` was minted even before
// this fix. The entity-level loss is the local-module one, and that is why both
// sites are needed to make the "entity AND its edge" claim.

// fs7199Module returns the module SCOPE.Component named name, or nil.
func fs7199Module(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == "SCOPE.Component" && ents[i].Subtype == "module" && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

// fs7199Src builds one source file whose LOCAL MODULE and whose type's
// INTERFACE clause both sit BELOW a verbatim string literal spelled `lit`.
// One template across the rows keeps the literal the only thing that varies.
func fs7199Src(lit string) string {
	return `namespace App

open System

let root = ` + lit + `

module Paths =
    let sep = 1

type Doc() =
    inherit Base()
    member _.Path = ` + lit + `
    interface IDisposable with
        member _.Dispose () = ()
`
}

// TestFSharp7199_ArtefactBelowVerbatimString is the must-have artefact table.
// Before the fix the `@"C:\"` row emitted NO `Paths` entity and NO IMPLEMENTS
// edge; the `@"say ""hi"""` row passed already (the doubled quotes happen to
// balance under the old C-style-escape reading) and is here so a fix that
// closes the string at the first interior quote cannot pass the table.
func TestFSharp7199_ArtefactBelowVerbatimString(t *testing.T) {
	cases := []struct {
		label string
		lit   string
	}{
		{"trailing backslash — the #7199 defect", `@"C:\"`},
		{"doubled-quote escape", `@"say ""hi"""`},
		{"plain verbatim", `@"abc"`},
	}
	for _, tc := range cases {
		ents := runFSharp(t, fs7199Src(tc.lit), "src/App.fs")

		// THE ENTITY below the trigger, asserted as a record rather than by
		// presence (extractor.go:643).
		mod := fs7199Module(ents, "Paths")
		if mod == nil {
			t.Errorf("%s: no local module entity `Paths` below the literal — the scrub ran away and the declaration was never seen", tc.label)
		} else {
			if mod.StartLine != 7 {
				t.Errorf("%s: Paths StartLine = %d, want 7", tc.label, mod.StartLine)
			}
			if mod.Signature != "module Paths =" {
				t.Errorf("%s: Paths Signature = %q, want %q", tc.label, mod.Signature, "module Paths =")
			}
		}

		// THE EDGE below the trigger (collectHierarchyEdges). EXTENDS sits
		// ABOVE the literal in the body and IMPLEMENTS BELOW it, so the pair
		// localises the loss to what follows the literal.
		if got := fsToIDs(fsRelsOfKind(t, ents, "Doc", "EXTENDS")); len(got) != 1 || got[0] != "Base" {
			t.Errorf("%s: Doc EXTENDS = %v, want [Base] (this clause is ABOVE the literal and must be unaffected)", tc.label, got)
		}
		if got := fsToIDs(fsRelsOfKind(t, ents, "Doc", "IMPLEMENTS")); len(got) != 1 || got[0] != "IDisposable" {
			t.Errorf("%s: Doc IMPLEMENTS = %v, want [IDisposable] — the clause BELOW the literal was swallowed", tc.label, got)
		}
	}
}

// TestFSharp7199_UnterminatedVerbatimStringStillSuppresses is the other
// direction of the terminated/unterminated axis at the artefact level: a
// verbatim string with no closing quote MUST still blank to EOF. fsc rejects
// such a file, so matching it beats out-guessing it, and the row exists so a
// later "be lenient at EOF" change is a deliberate decision rather than a
// silent one.
func TestFSharp7199_UnterminatedVerbatimStringStillSuppresses(t *testing.T) {
	src := `namespace App

let root = @"C:\

module Paths =
    let sep = 1
`
	ents := runFSharp(t, src, "src/App.fs")
	if mod := fs7199Module(ents, "Paths"); mod != nil {
		t.Errorf("an UNTERMINATED verbatim string must suppress to EOF; got a `Paths` entity at line %d", mod.StartLine)
	}
}

// TestFSharp7199_StringBodyIsNotExtracted is the FORBIDDEN row: it catches the
// PERMISSIVE failure "stop suppressing what a string literal contains", which
// every must-have row above passes while the helper's whole purpose is
// destroyed. No must-have row IN THIS FILE puts an inheritance clause or a
// module declaration inside a literal, and this row does fail on its own when
// run in isolation under such a mutant.
//
// WHAT IS NOT CLAIMED, because it was measured and is false: that this row is
// the only thing in the PACKAGE that would catch such a mutant. It is not.
// indent_band_7176_test.go:338 already carries
// `member _.Show () = "inherit GhostB()"` and asserts EXTENDS == [RealBase],
// and TestFSharp_BraceInStringDoesNotSuppressClause covers a neighbouring
// shape; both fail alongside this row under the ordinary-arm mutant. The value
// this row adds is the VERBATIM arm, which those rows do not exercise at all:
// leaking the verbatim body leaks `Phantom`/`PhantomBase` here, and no
// PRE-EXISTING row in the package notices that mutant — only rows added by
// #7199 do.
//
// Both literal forms are covered, and each is graded by its own mutant — the
// ordinary `"…"` by the ordinary-arm mutant, the verbatim `@"…"` by the
// verbatim-body mutant.
func TestFSharp7199_StringBodyIsNotExtracted(t *testing.T) {
	// THREE details of this fixture are load-bearing, and all three were
	// established by measurement — after an earlier version of this row turned
	// out to be VACUOUS in two of its four assertions.
	//
	// (1) The keyword must start a line IN THE SOURCE, not merely in the scrub.
	// moduleRE and the inheritance clauses are `^\s*`-anchored AND they run
	// over `src`; the scrub is only a GATE that removes matches
	// (extractor.go:643 checks `moduleScrubbed[name] != name`). So
	// `    "module Ghost ="` — a quote before the keyword — never matches in
	// the first place, and an assertion built on it can never fail however
	// permissive the scrub becomes. That is what the first version of this row
	// did, and no mutant could have told me: a vacuous forbidden row is
	// indistinguishable from a satisfied one.
	//
	// (2) Hence the literals are MULTI-LINE (legal F#: an ordinary or verbatim
	// string may span lines), putting the keyword at a line start in `src`
	// where the anchored patterns reach it. The `module` keywords sit at
	// column 0; the `inherit` clauses are INDENTED, because a column-0 line
	// ends the enclosing type's indent band and would put the clause outside
	// `Doc`'s body — which is how the second version of this fixture made the
	// `inherit` half vacuous while fixing the `module` half. Both indentations
	// are deliberate.
	//
	// (3) The two halves are graded by DIFFERENT mutants, so neither is
	// redundant: suppressing the ordinary arm leaks `Ghost`/`GhostBase` and
	// leaves the verbatim arm intact; leaking the verbatim body leaks
	// `Phantom`/`PhantomBase` and leaves the ordinary arm intact. Both
	// verdicts are measured in the PR body.
	src := `namespace App

let a = "
module Ghost =
    let x = 1
"
let b = @"
module Phantom =
    let x = 1
"

type Doc() =
    member _.Note = "
    inherit GhostBase()
    "
    member _.Path = @"
    inherit PhantomBase()
    "
    member _.Noop () = ()
`
	ents := runFSharp(t, src, "src/App.fs")

	for _, name := range []string{"Ghost", "Phantom"} {
		if mod := fs7199Module(ents, name); mod != nil {
			t.Errorf("a `module %s =` INSIDE a string literal was extracted as an entity (line %d) — the scrub stopped suppressing", name, mod.StartLine)
		}
	}
	if got := fsRelsOfKind(t, ents, "Doc", "EXTENDS"); len(got) != 0 {
		t.Errorf("Doc EXTENDS = %v, want none — an `inherit` inside a string literal must not become an edge", fsToIDs(got))
	}
}

// TestFSharp7199_IdentifierAbuttingVerbatimString is the ARTEFACT-level row for
// the contested adjacency shape, and it asserts both halves of the decision
// taken at the opener in extractor.go.
//
// `helper@"C:\"` is a verbatim string per the F# lexer (longest match gives
// `@"`, since `"` is not an op_char and so the list-append rule matches only
// the single `@` — see verbatimOpenerStart for the rule citations). So:
//
//   - THE RUNAWAY IS FIXED here like anywhere else: the `module` below it is
//     extracted. This is what a token-boundary condition on the opener would
//     have cost — that form would read `helper@"C:\"` as an ordinary string,
//     eat the closing quote and blank the rest of the file.
//   - NO CALLS EDGE to `helper` is minted. Blanking the `@` is what would make
//     spaceAppRE read this as a space application; the lexer says that edge is
//     correct, but the reading is DERIVED FROM SOURCE AND NOT EXECUTED, and a
//     wrong edge reads as valid to every consumer while a missing one is
//     detectable. So the edge is declined on this one shape and the fix does
//     not depend on it.
//
// If the lexing is ever confirmed by execution, the second assertion is the one
// to flip — deliberately, with the confirmation named.
func TestFSharp7199_IdentifierAbuttingVerbatimString(t *testing.T) {
	src := `namespace App

module Caller =
    let go () =
        helper@"C:\" tail
        readOther "plain"

module Paths =
    let sep = 1
`
	ents := runFSharp(t, src, "src/App.fs")

	// The runaway is fixed: a declaration below the literal is seen.
	if mod := fs7199Module(ents, "Paths"); mod == nil {
		t.Errorf("no `Paths` entity below `helper@\"C:\\\"` — the scrub ran away on an identifier-abutting " +
			"verbatim opener, which is the cost a token-boundary condition would carry")
	}

	// And no edge is fabricated for the contested shape.
	var callsFromGo []string
	for i := range ents {
		if ents[i].Kind == "SCOPE.Operation" && ents[i].Name == "go" {
			callsFromGo = fsToIDs(ents[i].Relationships)
		}
	}
	if len(callsFromGo) == 0 {
		t.Fatalf("no CALLS edges from `go` at all — the fixture no longer reaches the call scanner, " +
			"so the assertion below would be vacuous")
	}
	for _, to := range callsFromGo {
		if to == "helper" {
			t.Errorf("a CALLS edge to `helper` was minted from `helper@\"C:\\\" tail` (edges = %v). Per the "+
				"lexer that edge is correct, but it rests on an UNEXECUTED reading, so this shape "+
				"deliberately declines it — see the opener comment in extractor.go", callsFromGo)
		}
	}
}

// TestFSharp7199_OperatorSuffixOpenerDoesNotRunAway is the ARTEFACT-level row
// for the regression this PR's review caught, and it is here because the
// helper-level rows alone under-state the damage: a runaway costs an ENTITY at
// the local-module gate, not merely a scrub that looks wrong.
//
// `$$@"`, `.@"`, `?@"`, `$@$"`, `@@"` and `x=@"` are all an OPERATOR followed by
// an ORDINARY string per lex.fsl — the trailing `op_char*` of every symbolic
// operator rule swallows the `@`, so the lexer never starts rule 655 there and
// rule 586 opens an ordinary string, where `\` escapes. Treating them as
// verbatim ate the closing quote and blanked every remaining byte of the file,
// which is #7199's own defect in the permissive direction, on shapes the
// pre-#7199 code read CORRECTLY.
//
// Each subtest puts a real `module` below such a literal and asserts the
// declaration is still minted. The literal bodies all contain `\"` so that a
// verbatim misreading really does run away rather than merely mis-suppress.
func TestFSharp7199_OperatorSuffixOpenerDoesNotRunAway(t *testing.T) {
	for _, lit := range []string{
		// round-3 set: an operator run ending in `@`
		`$$@"a\"b"`,
		`x .@"a\"b"`,
		`x ?@"a\"b"`,
		`x $@$"a\"b"`,
		`@@"a\"b"`,
		`x=@"a\"b"`,
		`x<>@"a\"b"`,
		`x+@"a\"b"`,
		// round-4 set: a `$@`/`@$` opener whose `=` does NOT start the run, so
		// rule 976 cannot fire. These ran away while the exception was keyed on
		// the `=` byte instead of on the `=` starting a token.
		`x==$@"a\"b"`,
		`x<=$@"a\"b"`,
		`x>=$@"a\"b"`,
		`x+=$@"a\"b"`,
		`x-=$@"a\"b"`,
		`x*=$@"a\"b"`,
		`x|=$@"a\"b"`,
		`x&=$@"a\"b"`,
		`x!=$@"a\"b"`,
		`x%=$@"a\"b"`,
		`x/=$@"a\"b"`,
		`x~=$@"a\"b"`,
		`.=$@"a\"b"`,
		`$=$@"a\"b"`,
		`?=$@"a\"b"`,
		`x<>=$@"a\"b"`,
		`x==@$"a\"b"`,
		`x<=@$"a\"b"`,
	} {
		src := "namespace App\n\nlet p = " + lit + "\n\nmodule Paths =\n    let sep = 1\n"
		ents := runFSharp(t, src, "src/App.fs")
		if mod := fs7199Module(ents, "Paths"); mod == nil {
			t.Errorf("no `Paths` entity below `%s` — the scrub ran away on an operator-suffix opener. "+
				"Per lex.fsl that is an operator plus an ORDINARY string, where `\\` escapes and the "+
				"literal closes; reading it as verbatim reintroduces #7199 on a shape the pre-#7199 "+
				"code handled correctly", lit)
		}
	}
}
