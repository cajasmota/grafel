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

// TestFSharp7199_StringBodyIsNotExtracted is the FORBIDDEN row, and it is the
// one that catches the PERMISSIVE failure: "stop treating `\"` as a string
// opener at all" makes every must-have row above pass while destroying the
// helper's purpose. It is NOT dominated by a must-have sibling — no row above
// puts an inheritance clause or a module declaration INSIDE a literal, so this
// row fails alone under that mutant (measured; see the PR body).
//
// Both literal forms are covered: the ordinary `"…"` and the verbatim `@"…"`
// the new mode handles.
func TestFSharp7199_StringBodyIsNotExtracted(t *testing.T) {
	// Two details of this fixture are load-bearing, and both were established
	// by measurement rather than assumed.
	//
	// (1) The literal bodies START A LINE (legal F#: a `=`'s right-hand side
	// may sit indented on the next line). moduleRE and the inheritance clauses
	// are `^\s*`-anchored, so a literal written mid-line — `let a = "module
	// Ghost ="` — cannot be extracted even when the scrub stops suppressing,
	// and a row built that way is vacuous in both directions.
	//
	// (2) The MODULE literals close on the FOLLOWING line. moduleRE will not
	// match `module Ghost ="` — a trailing quote on the declaration line blocks
	// it — so with the closer on the same line that assertion is vacuous too.
	// Measured: `let a =\n     module Ghost ="` mints nothing, the same source
	// without the trailing quote mints [Ghost].
	src := `namespace App

let a =
    "module Ghost =
    "
let b =
    @"module Phantom =
    "

type Doc() =
    member _.Note =
        "inherit GhostBase()"
    member _.Path =
        @"inherit PhantomBase()"
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
