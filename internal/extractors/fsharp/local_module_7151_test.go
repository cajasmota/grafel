package fsharp_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// #7151 — THE LOCAL MODULE FORM PRODUCED NO ENTITY AT ALL.
//
// F# spells a module two ways, and they are different constructs with
// different scopes:
//
//	module Foo                 // TOP-LEVEL: scope is the remainder of the FILE
//	module Foo =               // LOCAL:     scope is the offside block below
//	    let helper x = x + 1
//
// `moduleRE` was anchored `\s*$` (extractor.go:132 on a352a6a29):
//
//	(?m)^([ \t]*)module(?:\s+(?:rec|public|private|internal)\b)*\s+([\w.]+)\s*$
//
// so the trailing ` =` of the local form made the anchor fail and the match
// was abandoned. Measured against the extractor at a352a6a29, BEFORE this
// change, with runFSharp over `namespace N\n\nmodule Target =\n    let x = 1\n`:
//
//	module Target =           -> 0 module entities   (want 1)
//	module private Target =   -> 0 module entities   (want 1)
//	module rec Target =       -> 0 module entities   (want 1)
//	    module Target =       -> 0 module entities   (want 1)
//	module Target             -> 1 module entity     (the accommodated form)
//
// This is the SILENT-MISS direction: a whole declaration form absent from the
// graph leaves no odd-looking row behind, only a file that reads as having no
// modules. It is also the direction an ordinary must-have row CAN see, which
// is why every row below asserts the emitted artefact — Name, Subtype,
// StartLine AND Signature — rather than mere presence.
//
// # LEGALITY — DERIVED, NOT EXECUTED
//
// There is no F# toolchain in this environment (`dotnet`, `fsc`, `fsharpc`,
// `fsi`, `mono` all absent; `javac` is the only compiler present), so every
// legality statement here is derived from the sources below and NOT compiled.
//
//  1. F# Language Specification § 10 "Namespaces and Modules"
//     (https://fsharp.github.io/fslang-spec/namespaces-and-modules/):
//     `module-defn := attributes? module access? ident = module-defn-body`.
//     The `= module-defn-body` is part of the production — the local form is
//     the one the grammar spells out, and the body is an offside block.
//     The file-scoped spelling is the separate `named-module` production
//     (`module long-ident module-elems`), which has NO `=` and whose elements
//     run to the end of the file.
//  2. MS Learn "Modules" (learn.microsoft.com/dotnet/fsharp/language-
//     reference/modules) gives the two syntax blocks explicitly:
//     top-level `module [accessibility-modifier] [qualified-namespace.]module-name`
//     with "declarations" following and no `=`, and local
//     `module [accessibility-modifier] module-name = declarations`. The page
//     states the scoping rule this test pins: a top-level module's contents
//     extend to the end of the file, while a local module's contents must be
//     indented under it.
//  3. The same page documents `module rec` (F# 4.1 recursive modules), which
//     is why the modifier group already carries `rec` and is UNTOUCHED here.
//
// FALSIFIER, stated so this can be refuted rather than merely believed: if
// `module Foo =` followed by an indented `let` were NOT a legal F# local
// module — i.e. if an F# compiler rejected the fixture in
// TestLocalModule7151_LocalFormIsExtracted — this whole change would be a
// widening onto an illegal shape and should be reverted, not re-tuned. The
// check that would settle it is `dotnet fsi --use:<fixture>.fsx` on a machine
// that has the toolchain; it was not run here.
//
// # THE SUBTYPE DECISION
//
// Both forms keep Subtype "module". The distinction is carried in the
// SIGNATURE, which echoes the declaration head verbatim:
//
//	module Foo    -> Signature "module Foo"
//	module Foo =  -> Signature "module Foo ="
//
// Justification, from the consumers enumerated on a352a6a29 and RE-DERIVED
// from source one at a time on this branch (an earlier revision of this list
// asserted eight rows and one of them — file_carrier — was false, so the
// whole set was checked again rather than repaired in place). Exactly TWO of
// them carry behaviour off this subtype, and both read it as "this is a
// container scope, not a callable or a resolvable target":
//
//   - internal/resolve/imports.go:285 — BEHAVIOUR-CARRYING.
//     `if e.Kind == "SCOPE.Component" && e.Subtype == "module" { continue }`
//     in pass 2 skips the entity when building the module→entity reverse
//     index, so an import marker does not register as a call target. A local
//     module is equally not a call target. A new subtype would start
//     registering it as one.
//   - internal/mcp/denoise.go:137 — BEHAVIOUR-CARRYING.
//     `bareKind == "component" && (subtype == "file" || subtype == "module")`
//     → `noiseContainer` (and again at :141 off the property fallback). A
//     local module is equally a container. A new subtype would stop it being
//     denoised. NOTE the consequence, stated plainly: the entity this commit
//     adds to the GRAPH is classified as a noise container by MCP, so the
//     payoff is graph-level (resolution, carriers, docgen) and the new record
//     does not surface in ordinary MCP output.
//
// The remaining consumers are INDIFFERENT to the subtype — verified, not
// assumed:
//
//   - internal/extractor/file_carrier.go clause 3 — INDIFFERENT. It keys on
//     `records[i].Name == path` (file_carrier.go:217), never on Subtype; the
//     file has ZERO non-comment occurrences of "module" (its :48/:71/:72/:75/
//     :207 hits are all inside `//` prose). A split subtype would have
//     changed nothing there. What DOES change, and is a Name effect rather
//     than a subtype one, is that a local `module Core.fs =` in a root
//     Core.fs can now reach the same path-named route a top-level one
//     already reached.
//   - internal/docgen/llm_bundle.go:1615-1617 `isModuleKind` — INDIFFERENT.
//     `strings.Contains(strings.ToLower(strings.TrimPrefix(kind,"SCOPE.")),
//     "module")`: it reads the KIND, not the subtype.
//   - cmd/grafel/nestjs_shadow_fold_test.go:37,89,162 — a test census that
//     folds out `n.Subtype == "file" || "import" || "module"`. It reads the
//     subtype, but it is a language-agnostic TEST filter with no product
//     behaviour; a new fsharp subtype would merely start appearing in its
//     census.
//   - internal/dashboard/handlers_iac.go:270,312 — never sees an fsharp
//     record. `iacToolForEntity` gates on
//     `language == "terraform" || language == "hcl"` BEFORE testing
//     `subtype == "resource" || subtype == "module"`; the :703 "module" is a
//     Terraform ref-prefix string, not a subtype.
//   - internal/vbnet/symbols.go:177 — never sees an fsharp record.
//     `s.Kind == KindType && s.TypeName == "module"` reads the VB.NET
//     symbol table's own Symbol type, not an EntityRecord.
//   - internal/mcp/mmapview.go:125,180 (and mmapview_test.go:467) — "module"
//     there is a PROPERTY KEY (the module-rollup label), unrelated to this
//     subtype.
//
// A new subtype (`local_module`) would therefore have silently CHANGED two
// consumers' behaviour — a local module would start registering as a
// resolvable target and stop being denoised — in a direction nobody asked for
// and nothing in this issue requires. The construct-level distinction is real
// and is preserved, in the artefact these rows assert.
//
// # AXES
//
// VARIED: modifier phrase (none, `private`, `internal`, `public`, `rec`,
// `rec private`); indentation of the declaration (0, 4 spaces, a tab);
// name qualification (`Target` vs `Ns.Target`); the whitespace between the
// name and the `=` (one space, three spaces, a tab, none at all); trailing
// whitespace after the `=`; declaration form (local vs top-level vs module
// ABBREVIATION `module M = A.B.C`); and the masking context (bare source,
// `(* … *)` block comment, `"""…"""` triple-quoted string).
//
// HELD CONSTANT: the entity Kind (always SCOPE.Component); the declared name
// (always `Target`, or `Ns.Target` where qualification is the axis, so a
// wrong capture is unambiguous); ONE declaration of interest per source file,
// so no row depends on the `"module:"+name` dedup key — that key carries
// neither indent nor scope and is #7144's arm 3, deliberately not exercised
// here; and the file path within each table.
//
// STATED SCOPE LIMIT — #7193. The local arm's recall is CONDITIONAL on
// stripStringsAndComments, and that function runs away on two legal F#
// constructs: a verbatim string with a trailing backslash (`@"C:\"`) and a
// character literal holding a quote (`'"'`). After either, the rest of the
// file scrubs to blank and a REAL local module below it is dropped — this
// fix does not apply for the remainder of such a file. PRE-EXISTING, not
// caused here (the local form minted 0 unconditionally before #7151; nine
// non-test call sites in this package share the defect), and the attribution
// is clean because a TOP-LEVEL module in the byte-identical file still mints.
// Recorded by TestLocalModule7151_ScrubRunawayHidesLocalModule_7193 below.
//
// ENLARGED SURFACE, stated rather than left to be inferred: the dedup key is
// `"module:"+name` and carries neither indent nor scope, so a file declaring
// BOTH a top-level `module Foo` and a local `module Foo =` now emits one
// entity where before it emitted one for a different reason — the two forms
// now compete for the same key. The key stays #7144's arm 3 and no row here
// depends on it (one declaration of interest per source file), but the
// widening does make the collision reachable and that is a fact about this
// commit, not about #7144.
//
// NOT VARIED, and so not claimed: attributes (`[<AutoOpen>] module Foo =`);
// `module type`; a local module nested inside another local module; and the
// module body's contents beyond a single `let`.
//
// # THE MODIFIER ALLOWLIST IS NOT TOUCHED
//
// `(?:\s+(?:rec|public|private|internal)\b)*` is byte-for-byte unchanged by
// this commit, and so is the mandatory `\s+` separator whose equivalence
// argument the extractor's comment records. #7181 measured that the allowlist
// is graded in the NARROWING direction only (dropping `private` is DEAD at
// 14; ADDING `sealed` is ALIVE at 0). This change neither improves nor
// worsens that: the only edit to the pattern is the new optional `(\s*=)?`
// group after the NAME capture. The rows below do vary the modifier across
// the local form, which ties the two forms together exactly as
// TestModuleModifiers_LocalModuleEqualsGapIsSeparate (#7135) demands.

// fs7151Modules returns every module SCOPE.Component, in emission order.
// Deliberately separate from fsModuleNames: these rows assert the record, not
// just the name.
func fs7151Modules(ents []types.EntityRecord) []types.EntityRecord {
	var out []types.EntityRecord
	for i := range ents {
		if ents[i].Kind == "SCOPE.Component" && ents[i].Subtype == "module" {
			out = append(out, ents[i])
		}
	}
	return out
}

func fs7151Names(ents []types.EntityRecord) []string {
	var out []string
	for _, e := range fs7151Modules(ents) {
		out = append(out, e.Name)
	}
	return out
}

// TestLocalModule7151_LocalFormIsExtracted is the must-have table. Every row
// asserts Name, Subtype, StartLine and Signature — the artefact, not presence.
func TestLocalModule7151_LocalFormIsExtracted(t *testing.T) {
	cases := []struct {
		label   string
		decl    string // the declaration line, verbatim, incl. indentation
		name    string // expected entity Name
		sigTail string // expected Signature, minus the leading "module "
	}{
		{"no modifier", "module Target =", "Target", "Target ="},
		{"private", "module private Target =", "Target", "Target ="},
		{"internal", "module internal Target =", "Target", "Target ="},
		{"public", "module public Target =", "Target", "Target ="},
		{"rec", "module rec Target =", "Target", "Target ="},
		{"rec private", "module rec private Target =", "Target", "Target ="},
		{"indent 4", "    module Target =", "Target", "Target ="},
		{"indent tab", "\tmodule Target =", "Target", "Target ="},
		{"qualified name", "module Ns.Target =", "Ns.Target", "Ns.Target ="},
		{"three spaces before =", "module Target   =", "Target", "Target ="},
		{"tab before =", "module Target\t=", "Target", "Target ="},
		{"no space before =", "module Target=", "Target", "Target ="},
		{"trailing space after =", "module Target =  ", "Target", "Target ="},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			// The declaration always lands on line 3, so StartLine is a
			// value the test knows independently of the extractor.
			src := "namespace Outer\n\n" + tc.decl + "\n    let x = 1\n"
			ents := runFSharp(t, src, "Local.fs")
			mods := fs7151Modules(ents)
			if len(mods) != 1 {
				t.Fatalf("local module %q produced %d module entities, want 1: %v",
					tc.decl, len(mods), fs7151Names(ents))
			}
			e := mods[0]
			if e.Name != tc.name {
				t.Errorf("local module %q got Name %q, want %q", tc.decl, e.Name, tc.name)
			}
			if e.Subtype != "module" {
				t.Errorf("local module %q got Subtype %q, want \"module\" — both F# module forms keep the container subtype", tc.decl, e.Subtype)
			}
			if e.StartLine != 3 {
				t.Errorf("local module %q got StartLine %d, want 3 — the declaration's own line, not the file's", tc.decl, e.StartLine)
			}
			if want := "module " + tc.sigTail; e.Signature != want {
				t.Errorf("local module %q got Signature %q, want %q — the local form's signature must carry the `=` that distinguishes it from a top-level module", tc.decl, e.Signature, want)
			}
		})
	}
}

// TestLocalModule7151_TopLevelFormUnchanged is the control half. This commit
// is a WIDENING, so the existing form's behaviour is what a forbidden row
// cannot see. Every value here was measured against a352a6a29 before the fix.
func TestLocalModule7151_TopLevelFormUnchanged(t *testing.T) {
	cases := []struct {
		label string
		decl  string
		name  string
	}{
		{"no modifier", "module Target", "Target"},
		{"private", "module private Target", "Target"},
		{"internal", "module internal Target", "Target"},
		{"public", "module public Target", "Target"},
		{"rec", "module rec Target", "Target"},
		{"rec private", "module rec private Target", "Target"},
		{"indent 4", "    module Target", "Target"},
		{"indent tab", "\tmodule Target", "Target"},
		{"qualified name", "module Ns.Target", "Ns.Target"},
		{"trailing space", "module Target  ", "Target"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			src := "namespace Outer\n\n" + tc.decl + "\n\nlet x = 1\n"
			ents := runFSharp(t, src, "TopLevel.fs")
			mods := fs7151Modules(ents)
			if len(mods) != 1 {
				t.Fatalf("top-level module %q produced %d module entities, want 1: %v",
					tc.decl, len(mods), fs7151Names(ents))
			}
			e := mods[0]
			if e.Name != tc.name {
				t.Errorf("top-level module %q got Name %q, want %q", tc.decl, e.Name, tc.name)
			}
			if e.Subtype != "module" {
				t.Errorf("top-level module %q got Subtype %q, want \"module\"", tc.decl, e.Subtype)
			}
			if e.StartLine != 3 {
				t.Errorf("top-level module %q got StartLine %d, want 3", tc.decl, e.StartLine)
			}
			if want := "module " + tc.name; e.Signature != want {
				t.Errorf("top-level module %q got Signature %q, want %q — a top-level module's signature must NOT carry an `=`", tc.decl, e.Signature, want)
			}
		})
	}
}

// TestLocalModule7151_BothFormsInOneFileStayDistinct is the row that pins the
// DISTINCTION rather than mere presence. A fix that admits the local form by
// treating it as a top-level one passes every must-have row above and fails
// here.
func TestLocalModule7151_BothFormsInOneFileStayDistinct(t *testing.T) {
	src := "namespace Outer\n" + // 1
		"\n" + // 2
		"module Top\n" + // 3  top-level: scope is the rest of the file
		"\n" + // 4
		"module Inner =\n" + // 5  local: scope is the offside block
		"    let x = 1\n" // 6
	ents := runFSharp(t, src, "Both.fs")
	mods := fs7151Modules(ents)
	if len(mods) != 2 {
		t.Fatalf("one top-level and one local module produced %d module entities, want 2: %v",
			len(mods), fs7151Names(ents))
	}
	byName := map[string]types.EntityRecord{}
	for _, e := range mods {
		byName[e.Name] = e
	}
	top, ok := byName["Top"]
	if !ok {
		t.Fatalf("the top-level module `Top` is missing; got %v", fs7151Names(ents))
	}
	inner, ok := byName["Inner"]
	if !ok {
		t.Fatalf("the local module `Inner` is missing; got %v", fs7151Names(ents))
	}
	if top.Signature != "module Top" {
		t.Errorf("top-level `module Top` got Signature %q, want \"module Top\"", top.Signature)
	}
	if inner.Signature != "module Inner =" {
		t.Errorf("local `module Inner =` got Signature %q, want \"module Inner =\" — the two forms must not be conflated", inner.Signature)
	}
	if top.Signature == inner.Signature {
		t.Errorf("the top-level and local forms emitted the SAME signature %q; they are different constructs with different scopes", top.Signature)
	}
	if top.StartLine != 3 {
		t.Errorf("top-level `module Top` got StartLine %d, want 3", top.StartLine)
	}
	if inner.StartLine != 5 {
		t.Errorf("local `module Inner =` got StartLine %d, want 5 — its OWN line, not the enclosing top-level module's", inner.StartLine)
	}
}

// TestLocalModule7151_MaskedLocalFormMintsNothing is the forbidden half, and
// it is the direction this widening creates. The declaration scanners read
// the RAW `src`, not the comment/string-scrubbed copy (#7152), so a looser
// `moduleRE` enlarges what commented-out and string-embedded text can mint.
// The new local arm is gated on the scrub for exactly that reason.
func TestLocalModule7151_MaskedLocalFormMintsNothing(t *testing.T) {
	cases := []struct {
		label string
		src   string
	}{
		{
			"block comment",
			"namespace Outer\n\n(*\nmodule Ghost =\n    let y = 2\n*)\n\nlet real = 1\n",
		},
		{
			"block comment with modifier",
			"namespace Outer\n\n(*\nmodule private Ghost =\n    let y = 2\n*)\n\nlet real = 1\n",
		},
		{
			"nested block comment",
			"namespace Outer\n\n(*\n(*\nmodule Ghost =\n*)\n*)\n\nlet real = 1\n",
		},
		{
			"triple-quoted string",
			"namespace Outer\n\nlet doc = \"\"\"\nmodule Ghost =\n    let y = 2\n\"\"\"\n",
		},
		{
			"triple-quoted string with modifier",
			"namespace Outer\n\nlet doc = \"\"\"\nmodule internal Ghost =\n    let y = 2\n\"\"\"\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			ents := runFSharp(t, tc.src, "Masked.fs")
			for _, e := range fs7151Modules(ents) {
				if e.Name == "Ghost" {
					t.Errorf("a masked local module (%s) minted the entity %q with signature %q; "+
						"the local arm must mint nothing from a block comment or a triple-quoted string",
						tc.label, e.Name, e.Signature)
				}
			}
			if names := fs7151Names(ents); len(names) != 0 {
				t.Errorf("a masked local module (%s) produced %d module entities, want 0: %v",
					tc.label, len(names), names)
			}
		})
	}
}

// TestLocalModule7151_MaskedTopLevelFormIsUNCHANGED records what this commit
// deliberately does NOT change. The TOP-LEVEL arm still reads raw source, so
// `module Ghost` inside a block comment is still extracted — a pre-existing
// phantom filed as #7152 and already recorded by #7135's
// TestModuleTypeModifiers_OnlyRealDeclarations. It is asserted here, as a
// BUG whose before/after this commit leaves empty, so that:
//
//   - the guard added for the local arm is demonstrably NARROW (had it been
//     applied to both arms, this row goes red and the change would have been
//     silently doing #7152's job), and
//   - nobody reads the local arm's new guard as evidence that the top-level
//     arm has one.
//
// This row is NOT an endorsement. When #7152 lands it should go red and be
// rewritten to the forbidden direction.
func TestLocalModule7151_MaskedTopLevelFormIsUNCHANGED(t *testing.T) {
	src := "namespace Outer\n\n(*\nmodule Ghost\n*)\n\nlet real = 1\n"
	ents := runFSharp(t, src, "MaskedTop.fs")
	names := fs7151Names(ents)
	found := false
	for _, n := range names {
		if n == "Ghost" {
			found = true
		}
	}
	if !found {
		t.Errorf("a commented-out TOP-LEVEL `module Ghost` minted %v; this commit must leave the "+
			"pre-existing #7152 phantom exactly as it was — if it is now gone, the local arm's "+
			"scrub guard was applied too widely and this change is silently doing #7152's work", names)
	}
}

// TestLocalModule7151_ModuleAbbreviationStillMintsNothing pins the OTHER edge
// of the widening. F# § 10 also has `module-abbrev := module ident =
// long-ident` — `module M = A.B.C` on one line, an ALIAS, not a declaration
// of a new module. The new `(\s*=)?` group is anchored at end-of-line, so an
// abbreviation still does not match, which is exactly its behaviour at
// a352a6a29 (measured: 0 module entities). Whether an alias SHOULD produce an
// entity is not decided here; what is pinned is that this commit did not
// decide it by accident. That decision is #7194.
func TestLocalModule7151_ModuleAbbreviationStillMintsNothing(t *testing.T) {
	for _, decl := range []string{
		"module M = A.B.C",
		"module private M = A.B.C",
		"    module M = Microsoft.FSharp.Core",
	} {
		t.Run(strings.TrimSpace(decl), func(t *testing.T) {
			src := "namespace Outer\n\n" + decl + "\n\nlet x = 1\n"
			ents := runFSharp(t, src, "Abbrev.fs")
			if names := fs7151Names(ents); len(names) != 0 {
				t.Errorf("module abbreviation %q produced %d module entities, want 0 "+
					"(unchanged from a352a6a29): %v", decl, len(names), names)
			}
		})
	}
}

// TestLocalModule7151_ModifierRelationHoldsForLocalForm keeps #7151's own
// grading requirement: the two forms stay TIED, so a one-sided future fix
// fails. It is the relation #7135's TestModuleModifiers_LocalModuleEqualsGap-
// IsSeparate asserted while the count on both sides was 0; now that the count
// is 1 the same relation grades the fix instead of the gap.
func TestLocalModule7151_ModifierRelationHoldsForLocalForm(t *testing.T) {
	base := runFSharp(t, "namespace N\n\nmodule Target =\n    let x = 1\n", "Rel.fs")
	baseMods := fs7151Modules(base)
	if len(baseMods) != 1 {
		t.Fatalf("`module Target =` produced %d module entities, want 1", len(baseMods))
	}
	for _, mod := range []string{"rec", "public", "private", "internal", "rec private"} {
		t.Run(mod, func(t *testing.T) {
			src := fmt.Sprintf("namespace N\n\nmodule %s Target =\n    let x = 1\n", mod)
			got := fs7151Modules(runFSharp(t, src, "Rel.fs"))
			if len(got) != len(baseMods) {
				t.Fatalf("`module Target =` produced %d module entities and `module %s Target =` produced %d; "+
					"the modifier must make no difference to the local form", len(baseMods), mod, len(got))
			}
			if got[0].Name != baseMods[0].Name || got[0].Signature != baseMods[0].Signature ||
				got[0].StartLine != baseMods[0].StartLine || got[0].Subtype != baseMods[0].Subtype {
				t.Errorf("`module %s Target =` emitted (%q,%q,%q,%d) but `module Target =` emitted (%q,%q,%q,%d); "+
					"the modifier must make no difference to the local form",
					mod, got[0].Name, got[0].Subtype, got[0].Signature, got[0].StartLine,
					baseMods[0].Name, baseMods[0].Subtype, baseMods[0].Signature, baseMods[0].StartLine)
			}
		})
	}
}

// TestLocalModule7151_NewlineBeforeEqualsStaysTopLevel grades the ONE thing
// the extractor comment claims about `[ \t]*=` vs the naive `\s*=`.
//
// A MATCH COUNT IS BLIND HERE, which is why this row asserts the emitted
// artefact instead. On "module Foo\n=\n" (and on "module Foo\n\n   =\n")
// BOTH patterns match exactly ONCE. They differ only in whether group 3
// CAPTURES: `\s` matches `\n` in Go, so `\s*=` reaches across the line break,
// captures "\n=" and classifies a TOP-LEVEL declaration as LOCAL with
// Signature "module Foo =". `[ \t]*=` cannot cross the line break and the
// declaration stays TOP with Signature "module Foo". A row that counted
// matches, or merely asserted that one module entity exists, would pass under
// both patterns and grade nothing.
//
// Both halves below can fail alone: the must-have fails if the declaration
// stops being extracted or loses its top-level signature; the forbidden fails
// if ANY module record in the file carries a local `=` signature, which is
// what a `\s*` pattern produces even while the count stays 1.
//
// The shape guarded against — a `module Foo` line followed by a line whose
// first non-whitespace byte is `=` — is not itself legal F#, so the hazard is
// narrow; the mechanism claim is nonetheless the reason the pattern is
// written the way it is, and this is what makes it falsifiable.
func TestLocalModule7151_NewlineBeforeEqualsStaysTopLevel(t *testing.T) {
	for _, tc := range []struct {
		label string
		src   string
	}{
		{"= on the next line", "namespace Outer\n\nmodule Foo\n=\n"},
		{"= after a blank line, indented", "namespace Outer\n\nmodule Foo\n\n   =\n"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			mods := fs7151Modules(runFSharp(t, tc.src, "Newline.fs"))

			// MUST-HAVE: the declaration is extracted, and as TOP-LEVEL.
			var foo *types.EntityRecord
			for i := range mods {
				if mods[i].Name == "Foo" {
					foo = &mods[i]
				}
			}
			if foo == nil {
				t.Fatalf("`module Foo` with an `=` on a later line produced no module entity named Foo: %v",
					fs7151ModNames(mods))
			}
			if foo.Signature != "module Foo" {
				t.Errorf("got Signature %q, want \"module Foo\" — the `=` is on a LATER line, so this is a "+
					"TOP-LEVEL declaration; a `\\s*=` group would cross the newline and mis-sign it \"module Foo =\"",
					foo.Signature)
			}
			if foo.Subtype != "module" {
				t.Errorf("got Subtype %q, want \"module\"", foo.Subtype)
			}

			// FORBIDDEN, and it can fail alone: no record in this file may
			// carry the LOCAL signature, however many records there are.
			for _, e := range mods {
				if strings.HasSuffix(e.Signature, " =") {
					t.Errorf("module %q got the LOCAL signature %q from a source whose `=` is on a "+
						"different line; `[ \\t]*=` must not cross a newline", e.Name, e.Signature)
				}
			}
		})
	}
}

// TestLocalModule7151_ScrubRunawayHidesLocalModule_7193 RECORDS TODAY'S
// BEHAVIOUR. IT IS NOT AN ENDORSEMENT, AND ITS FAILURE IS NOT A REGRESSION.
//
// The local arm is gated on stripStringsAndComments (see the comment in
// extractFSharp). That function runs away on two legal F# constructs:
//
//	let p = @"C:\"     // verbatim string, trailing backslash
//	let q = '"'        // character literal holding a quote
//
// Its `case '"'` arm carries the comment "Check for verbatim string @\"...\""
// and performs NO such check, so `\` is treated as a C-style escape and eats
// the closing quote; and there is no char-literal state at all. Everything
// after either construct scrubs to blank, so a REAL `module Target =` below
// one is silently dropped and #7151's fix does not apply for the rest of that
// file.
//
// PRE-EXISTING, NOT CAUSED BY #7151: before this commit the local form minted
// 0 unconditionally, so the count here was 0 then too. Nine non-test call
// sites in this package share the defect. Filed as #7193.
//
// The ATTRIBUTION is what makes this clean, and it is the last subtest: a
// TOP-LEVEL `module Target` in the BYTE-IDENTICAL file still mints, because
// the top-level arm is ungated. So the loss is the scrubber's, and the
// control also fails alone if anything else in the extractor breaks that file.
//
// # HOW TO UPDATE THIS ROW
//
// A fix for #7193 SHOULD TURN THE FIRST TWO SUBTESTS RED. That is the signal
// that it worked. When that happens, flip those rows to want-1 with a note
// saying #7193 landed — UPDATE them deliberately, do not delete them, and do
// not read the failure as a regression in #7151.
func TestLocalModule7151_ScrubRunawayHidesLocalModule_7193(t *testing.T) {
	const decl = "module Target =\n    let x = 1\n"

	t.Run("verbatim string with trailing backslash (want 1, #7193 makes it 0)", func(t *testing.T) {
		src := "namespace Outer\n\nlet p = @\"C:\\\"\n\n" + decl
		got := fs7151ModNames(fs7151Modules(runFSharp(t, src, "Runaway.fs")))
		if len(got) != 0 {
			t.Errorf("RECORDED BEHAVIOUR CHANGED: a local module below `@\"C:\\\"` now emits %v. "+
				"If #7193 has been fixed this is the SIGNAL THAT IT WORKED, not a regression — "+
				"update this row to want exactly [Target] with a note naming the fix.", got)
		}
	})

	t.Run("char literal holding a quote (want 1, #7193 makes it 0)", func(t *testing.T) {
		src := "namespace Outer\n\nlet q = '\"'\n\n" + decl
		got := fs7151ModNames(fs7151Modules(runFSharp(t, src, "Runaway.fs")))
		if len(got) != 0 {
			t.Errorf("RECORDED BEHAVIOUR CHANGED: a local module below `'\\\"'` now emits %v. "+
				"If #7193 has been fixed this is the SIGNAL THAT IT WORKED, not a regression — "+
				"update this row to want exactly [Target] with a note naming the fix.", got)
		}
	})

	t.Run("well-formed verbatim string is NOT affected", func(t *testing.T) {
		src := "namespace Outer\n\nlet p = @\"say \"\"hi\"\"\"\n\n" + decl
		got := fs7151ModNames(fs7151Modules(runFSharp(t, src, "Runaway.fs")))
		if len(got) != 1 || got[0] != "Target" {
			t.Errorf("a local module below a WELL-FORMED verbatim string emitted %v, want [Target]; "+
				"#7193 is about the trailing-backslash and char-literal shapes only, and this row is "+
				"what stops the two above being read as 'the gate drops everything'", got)
		}
	})

	t.Run("POSITIVE CONTROL: top-level form in the identical file still mints", func(t *testing.T) {
		src := "namespace Outer\n\nlet p = @\"C:\\\"\n\nmodule Target\n\nlet x = 1\n"
		got := fs7151ModNames(fs7151Modules(runFSharp(t, src, "Runaway.fs")))
		if len(got) != 1 || got[0] != "Target" {
			t.Errorf("the TOP-LEVEL arm is ungated and must still mint from the byte-identical file; "+
				"got %v, want [Target]. Without this row the loss above is not attributable to the "+
				"scrubber rather than to something else in the extractor", got)
		}
	})
}

// fs7151ModNames is fs7151Names over an already-filtered slice.
func fs7151ModNames(mods []types.EntityRecord) []string {
	out := []string{}
	for _, e := range mods {
		out = append(out, e.Name)
	}
	return out
}
