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
// Justification, from the consumers enumerated on a352a6a29. Every consumer
// that branches on this subtype reads it as "this is a container scope, not a
// callable or a resolvable target":
//
//   - internal/resolve/imports.go:285 SKIPS `SCOPE.Component`+`module` when
//     building the module→entity reverse index, so an import marker does not
//     register as a call target. A local module is equally not a call target.
//   - internal/mcp/denoise.go:137,141 classify `component`+`module` as
//     `noiseContainer`. A local module is equally a container.
//   - internal/docgen/llm_bundle.go:1617 `isModuleKind` matches any kind
//     CONTAINING "module" — it reads the KIND, not the subtype, so it is
//     indifferent either way.
//   - internal/extractor/file_carrier.go clause 3 rejects a second file
//     carrier when some record is already named after the path; the fsharp
//     route it documents is a module whose dotted name equals the path. A
//     local module can reach that route by the same dotted-name capture, and
//     keeping the subtype keeps that rejection uniform.
//   - cmd/grafel/nestjs_shadow_fold_test.go:37,89,162 fold `module` out of
//     its census; a language-agnostic filter that must not start seeing a new
//     fsharp subtype.
//   - internal/dashboard/handlers_iac.go:270,312 and internal/vbnet/symbols.go
//     :177 both key on "module" but are gated on Terraform / VB.NET
//     respectively and never see an fsharp record.
//
// A new subtype (`local_module`) would therefore have silently CHANGED three
// of those consumers' behaviour — a local module would start registering as a
// resolvable target, stop being denoised, and split the carrier rule — in a
// direction nobody asked for and nothing in this issue requires. The
// construct-level distinction is real and is preserved, in the artefact these
// rows assert.
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
// decide it by accident.
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
