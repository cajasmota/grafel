package fsharp_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// #7131 — `letRE` accommodated exactly two modifiers as a fixed SEQUENCE
// (`(?:\s+rec)?(?:\s+mutable)?`), so any modifier outside that sequence landed
// in the NAME capture: `let inline distance p q = ...` was indexed as an
// operation called `inline`.
//
// The tests below enumerate the modifier space rather than sampling it. A
// single `let inline` fixture would have left `private inline` open — which is
// precisely how a two-slot sequence came to be written in the first place.
//
// Axes VARIED by the table: modifier identity (rec / mutable / inline /
// private / internal / public), modifier COUNT (0, 1, 2), modifier ORDER
// (both orders of two different pairs), presence of value parameters,
// presence of a generic parameter list, and indentation (top level vs nested).
//
// Axes HELD CONSTANT: the bound name (always `target`, so a wrong capture is
// unambiguous), the entity kind (SCOPE.Operation) and subtype (`let`), one
// binding of interest per source, the enclosing `module M`, and the file path.
// Indentation is held constant inside the table (top level) and varied by
// TestLetModifiers_Indented; duplicate collapse is varied by
// TestLetModifiers_SameModifierNoCollapse.
//
// GRAMMAR, and what the table does NOT claim. The modifier order is fixed by
// the F# Language Specification, § 14.6 "Function and Value Definitions"
// (grammar reproduced under `let-binding` / `function-defn` in § 6.6
// "Definition Expressions"; access modifiers in § 10.5 "Accessibility
// Annotations", corroborated by MS Learn "Access Control in F#",
// https://learn.microsoft.com/dotnet/fsharp/language-reference/access-control):
//
//	function-defn := inline? access? ident-or-op typar-defns? argument-pats return-type? = expr
//	value-defn    := mutable? access? pat …
//	access        := public | private | internal
//
// — and `rec` sits OUTSIDE the definition (`let rec function-or-value-defns`).
// So `public` on a `let` IS grammatical, and the legal order is
// rec → inline/mutable → access → name. Rows that reverse it (`private
// inline`, `inline rec`, …) are therefore NOT legal F#, and each one is
// labelled `lenienceOnly` at the row level. Those rows pin the SCANNER's
// deliberate tolerance of a reversed order — this is a lenient scanner, not a
// compiler, and ranking orders could only create a way to LOSE a real binding.
// They are not, and must not be read as, a claim about the language.
//
// No F# toolchain exists in this environment (`dotnet`, `fsc`, `fsharpc`,
// `mono` are all absent), so the classification above is read off the
// specification grammar and was not executed against a compiler.

// fsFindLet returns the SCOPE.Operation named name with subtype "let".
func fsFindLet(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == "SCOPE.Operation" && ents[i].Subtype == "let" {
			return &ents[i]
		}
	}
	return nil
}

// fsLetNames lists the names of every `let` SCOPE.Operation, in order.
func fsLetNames(ents []types.EntityRecord) []string {
	var out []string
	for i := range ents {
		if ents[i].Kind == "SCOPE.Operation" && ents[i].Subtype == "let" {
			out = append(out, ents[i].Name)
		}
	}
	return out
}

// fsGrammar labels a table row against the specification grammar quoted in
// this file's header. It exists so that reading ONE row in isolation cannot
// mislead: a row is either grammatical F#, or it is a deliberate lenience
// probe and says so on its own line.
type fsGrammar string

const (
	// specLegal — grammatical F# under § 14.6 / § 10.5.
	specLegal fsGrammar = "legal F#"
	// lenienceOnly — NOT legal F#: the row reverses the spec's modifier
	// order (access must FOLLOW inline/mutable, and `rec` precedes the
	// definition). It pins the scanner's tolerance, not the language.
	lenienceOnly fsGrammar = "NOT legal F# — pins scanner lenience to reversed modifier order"
)

func TestLetModifiers_NameIsNeverTheModifier(t *testing.T) {
	cases := []struct {
		name    string    // modifier phrase under test ("none" = no modifier)
		grammar fsGrammar // legal F#, or a deliberate lenience probe
		decl    string    // the binding line
	}{
		{"none", specLegal, `let target (a: int) = a`},
		{"rec", specLegal, `let rec target (a: int) = if a > 0 then target (a - 1) else 0`},
		{"mutable", specLegal, `let mutable target = 0`},
		{"inline", specLegal, `let inline target (a: int) = a`},
		{"private", specLegal, `let private target (a: int) = a`},
		{"internal", specLegal, `let internal target (a: int) = a`},
		{"public", specLegal, `let public target (a: int) = a`},
		{"inline private", specLegal, `let inline private target (a: int) = a`},
		{"rec inline", specLegal, `let rec inline target (a: int) = if a > 0 then target (a - 1) else 0`},
		{"rec private", specLegal, `let rec private target (a: int) = if a > 0 then target (a - 1) else 0`},
		{"mutable private", specLegal, `let mutable private target = 0`},
		{"inline generic params", specLegal, `let inline target<'T> (a: 'T) = a`},

		// Reversed order: access before inline/mutable, or `rec` after a
		// modifier. Ungrammatical F# — scanner-lenience probes only.
		{"private inline", lenienceOnly, `let private inline target (a: int) = a`},
		{"internal inline", lenienceOnly, `let internal inline target (a: int) = a`},
		{"inline rec", lenienceOnly, `let inline rec target (a: int) = if a > 0 then target (a - 1) else 0`},
		{"private rec", lenienceOnly, `let private rec target (a: int) = if a > 0 then target (a - 1) else 0`},
		{"private mutable", lenienceOnly, `let private mutable target = 0`},
		{"private inline generic params", lenienceOnly, `let private inline target<'T> (a: 'T) = a`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "module M\n\n" + tc.decl + "\n"
			ents := runFSharp(t, src, "Mods.fs")

			if e := fsFindLet(ents, "target"); e == nil {
				t.Errorf("no `let` SCOPE.Operation named \"target\" for %q [%s]; let names = %v",
					tc.decl, tc.grammar, fsLetNames(ents))
			}
			// The complementary direction: no entity may be named after a
			// modifier. Recall alone cannot detect over-firing.
			for _, bad := range []string{"rec", "mutable", "inline", "private", "internal", "public"} {
				if e := fsFindLet(ents, bad); e != nil {
					t.Errorf("modifier %q recorded as the entity name for %q [%s] (line %d)",
						bad, tc.decl, tc.grammar, e.StartLine)
				}
			}
		})
	}
}

// TestLetModifiers_Indented varies the indentation axis the table holds
// constant: a modifier-bearing binding nested inside another binding.
func TestLetModifiers_Indented(t *testing.T) {
	src := `module M

let outer () =
    let inline helper (a: int) = a + 1
    helper 1
`
	ents := runFSharp(t, src, "Nested.fs")
	if fsFindLet(ents, "helper") == nil {
		t.Errorf("indented `let inline helper` not named \"helper\"; let names = %v", fsLetNames(ents))
	}
	if e := fsFindLet(ents, "inline"); e != nil {
		t.Errorf("indented binding recorded with name \"inline\" (line %d)", e.StartLine)
	}
}

// TestLetModifiers_SameModifierNoCollapse pins the second consequence of the
// defect, the one a name-only reading of the bug leaves behind: `letSeen` is
// keyed `indent + ":let:" + name`, so when both bindings are misnamed
// `inline` the second one is DROPPED — one entity where there should be two.
func TestLetModifiers_SameModifierNoCollapse(t *testing.T) {
	src := `module M

let inline alpha (a: int) = a + 1

let inline beta (b: int) = b + 2
`
	ents := runFSharp(t, src, "Collapse.fs")
	names := fsLetNames(ents)
	if len(names) != 2 {
		t.Errorf("two same-indent `let inline` bindings produced %d let entities, want 2: %v",
			len(names), names)
	}
	if fsFindLet(ents, "alpha") == nil {
		t.Errorf("missing let entity \"alpha\"; let names = %v", names)
	}
	if fsFindLet(ents, "beta") == nil {
		t.Errorf("missing let entity \"beta\"; let names = %v", names)
	}
}

// TestLetModifiers_ModifierPrefixedNames is a negative control against the
// obvious over-widening: an allowlist matched without a word boundary would
// eat the leading `rec` of `recompute`, the `mutable` of `mutableState`, and
// so on. Every name below is a legal F# identifier that merely BEGINS with a
// modifier word, so the source really contains the shape the mutant misreads.
func TestLetModifiers_ModifierPrefixedNames(t *testing.T) {
	src := `module M

let recompute (a: int) = a
let mutableState (a: int) = a
let inlineCache (a: int) = a
let privateKey (a: int) = a
let internalId (a: int) = a
let publicFacing (a: int) = a
`
	ents := runFSharp(t, src, "Prefix.fs")
	for _, want := range []string{
		"recompute", "mutableState", "inlineCache", "privateKey", "internalId", "publicFacing",
	} {
		if fsFindLet(ents, want) == nil {
			t.Errorf("name %q lost to a modifier-prefix match; let names = %v",
				want, fsLetNames(ents))
		}
	}
	if got := len(fsLetNames(ents)); got != 6 {
		t.Errorf("got %d let entities, want 6: %v", got, fsLetNames(ents))
	}
}

// TestLetModifiers_CurriedParamsAreNotTheName is a negative control against
// the other over-widening: capturing "the last identifier before the
// parameters", or treating ANY word before the name as a modifier. F#'s
// dominant function form is curried, space-applied parameters with no
// parentheses — `let add x y = x + y` — so that shape is genuinely present
// here, and either over-wide reading would name this binding `y`.
func TestLetModifiers_CurriedParamsAreNotTheName(t *testing.T) {
	src := `module M

let add x y = x + y

let inline scale factor value = factor * value
`
	ents := runFSharp(t, src, "Curried.fs")
	if fsFindLet(ents, "add") == nil {
		t.Errorf("curried binding not named \"add\"; let names = %v", fsLetNames(ents))
	}
	if fsFindLet(ents, "scale") == nil {
		t.Errorf("curried binding with a modifier not named \"scale\"; let names = %v", fsLetNames(ents))
	}
	for _, bad := range []string{"x", "y", "factor", "value", "inline"} {
		if e := fsFindLet(ents, bad); e != nil {
			t.Errorf("parameter/modifier %q captured as the binding name (line %d)", bad, e.StartLine)
		}
	}
}
