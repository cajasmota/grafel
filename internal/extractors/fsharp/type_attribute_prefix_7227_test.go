package fsharp_test

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// #7227 — typeRE anchored on `(?m)^[ \t]*type`, so a type declaration preceded
// ON THE SAME LINE by an attribute section never matched and NO entity was
// minted. Not a wrong name, not a wrong subtype, not a wrong span: the type is
// absent from the graph, silently.
//
//	source                                        was          want
//	[<Struct>] type SPoint = { SX: int }          <absent>     "record"
//	[<CLIMutable; NoComparison>] type Src = {…}   <absent>     "record"
//	    [<Struct>] type SColor = | SRed | …       <absent>     "discriminated_union"
//	type SPoint = { SX: int }   (no attribute)    "record"     "record"   [control]
//	[<Struct>] on its OWN line, decl below        "record"     "record"   [control]
//
// The last two rows are the CONTROLS: the declaration is otherwise identical,
// so the loss is attributable to the same-line attribute and to nothing else.
//
// RECALL DEFECT — what can and cannot grade this. A recall row that never sees
// the entity cannot distinguish "not extracted" from "not present in the
// fixture", and widening a regex is exactly the direction this repo's suites
// are structurally blind to. So the grading here is in three parts:
//
//  1. recall rows (TestFSharpTypeAttrPrefix_SameLineAttribute) — the entity
//     must be minted, with its body-derived subtype;
//  2. FORBIDDEN rows (TestFSharpTypeAttrPrefix_Forbidden) — sources on which
//     the widened pattern must mint NOTHING, each accompanied by a POSITIVE
//     CONTROL (TestFSharpTypeAttrPrefix_ForbiddenPredicateFires) that plants a
//     violation and shows the predicate fires: an absence assertion passes
//     identically whether it is enforced or simply unreachable, and no mutant
//     of the extractor can tell those two apart;
//  3. a COUNT FLOOR (TestFSharpTypeAttrPrefix_CountFloor) over a file carrying
//     every accepted shape at once, so a partial re-narrowing that still leaves
//     one row green fails.
//
// CORPUS MEASUREMENT (the widening is not justified by hand-written fixtures).
// `/Users/jorgecajas/Projects/archigraph-corpora` holds ZERO `.fs`/`.fsi`/`.fsx`
// files, so a population was built from 7 shallow clones — dotnet/fsharp,
// fsprojects/FSharp.Data, giraffe-fsharp/Giraffe, fable-compiler/Fable,
// fsprojects/Paket, SuaveIO/suave, fsprojects/FSharpPlus — 8,556 F# files:
//
//	26,437  lines matching the OLD anchor `^[ \t]*type[ \t]`
//	   393  lines matching `^[ \t]*\[<…>\][ \t]*type[ \t]`   (1.5%)
//	    91  of those 393 that the WIDENED pattern newly mints
//	         (the other 302 are `[<Measure>] type kg`-style: no `=`, or a
//	          lower-case name, which typeRE's `[A-Z]` rejects either way)
//	     1  line carrying TWO attribute sections   `[<Measure>] [<Measure>] type m`
//	     1  line carrying a NESTED `>`             `[<A<int>>] type C = class end`
//
// FIXTURE LEGALITY. There is no F# compiler on this machine. Every shape below
// is (a) derived from the language reference and (b) attested VERBATIM in
// dotnet/fsharp's own compiler test suite, which by construction compiles:
//
//   - same-line attribute at all: F# 4.1 spec §13.1 "Custom Attributes" and
//     §8 "Type Definitions" (`type-defn := attributes? type-name …`) — the
//     grammar separates `attributes` from `type-name` by whitespace, and F#
//     whitespace is not required to be a newline. 393 corpus lines.
//   - `[<Struct>]` on a RECORD body: MS Learn "Structures" — "you can also use
//     the Struct attribute on a record or discriminated union". Attested:
//     tests/FSharp.Compiler.ComponentTests/CompilerOptions/Fsc/reflectionfree.fs:216
//     `[<Struct>] type StructRecord = { SX: int; SY: int }`.
//     The contested shape `[<Struct>] type V = struct … end` (the one an agent
//     declined to build on under #7218, because the reference presents the
//     attribute and the explicit `struct … end` form as ALTERNATIVES) is NOT
//     used here — the question is sidestepped, not settled.
//   - `[<Struct>]` on a DU body: attested at
//     tests/FSharp.Compiler.ComponentTests/EmittedIL/ReflectionFreeToString.fs:188
//     `[<Struct>] type SColor = | SRed | SCustom of item: int`.
//   - `[<CLIMutable; NoComparison>]` — one attribute SECTION holding two
//     attributes separated by `;` (spec §13.1). Attested at
//     tests/FSharp.Compiler.ComponentTests/Conformance/Spreads/RecordSpreads.fsx:49
//     `[<CLIMutable; NoComparison>] type Src = { A : int; B : int }`.
//   - two adjacent attribute SECTIONS: attested at
//     tests/FSharp.Compiler.ComponentTests/Conformance/BasicGrammarElements/
//     CustomAttributes/Basic/E_AttributeApplication02.fs:6
//     `[<Measure>] [<Measure>] type m`.
//   - a nested `>` from a generic attribute argument: attested at
//     tests/FSharp.Compiler.ComponentTests/Attributes/GenericAttributeAbbreviations.fs:94
//     `[<A<int>>] type C = class end`.
//   - an attribute whose ARGUMENT is a string containing `]`: attribute
//     arguments are expressions (spec §13.1); the shape is attested on members
//     in Fable, e.g. tests/Python/TestArithmetic.fs:1522
//     `[<Emit("[x for x in [0, $0]][-1]")>]`.
//
// WHAT THE WIDENED PATTERN DOES NOT MATCH — see the forbidden table below:
// an attribute section followed by anything other than `type`; an unterminated
// `[<`; an attribute section on the PRECEDING line with a non-type line
// between; `[<A>]` spanning a newline before `type`. The prefix is
// `(?:\[<[^\n]*?>\][ \t]*)*`: NON-greedy, so a trailing `// [<X>]` comment on
// the same line cannot be swallowed into the prefix, and LINE-BOUND, so no
// attribute section may span a newline.

// fsAttrTypeNames lists every type-declaration SCOPE.Component name, in order.
//
// It is deliberately NOT the sibling helper fsTypeNames (module_type_modifiers_
// 7135_test.go), which keeps only entities whose Subtype is in a fixed allow
// set: an entity minted with an UNEXPECTED subtype would be invisible to it,
// and the count floor below must see everything typeRE mints. Modules and
// namespaces share the SCOPE.Component kind, so they are excluded by name of
// their subtype rather than by an allow set.
func fsAttrTypeNames(ents []types.EntityRecord) []string {
	var out []string
	for i := range ents {
		if ents[i].Kind != "SCOPE.Component" {
			continue
		}
		if ents[i].Subtype == "module" || ents[i].Subtype == "namespace" {
			continue
		}
		out = append(out, ents[i].Name)
	}
	return out
}

// fsAttrSubtypeOf returns the Subtype of the named type-declaration
// SCOPE.Component, or "<absent>" when nothing was minted. The sentinel is the
// point: `<absent>` is what #7227 produced, and a membership test against a
// set of legal subtypes could not tell it from a wrong-but-present value.
func fsAttrSubtypeOf(ents []types.EntityRecord, name string) string {
	for i := range ents {
		if ents[i].Kind == "SCOPE.Component" && ents[i].Name == name {
			return ents[i].Subtype
		}
	}
	return "<absent>"
}

// fsAttrForbiddenFailure is the FORBIDDEN-row predicate, extracted so it can be
// graded WITHOUT an extractor run: it returns failure text when `name` was
// minted, and "" when it was correctly absent.
// TestFSharpTypeAttrPrefix_ForbiddenPredicateFires plants a violation straight
// into an entity slice and asserts this returns non-empty.
func fsAttrForbiddenFailure(ents []types.EntityRecord, name string) string {
	if got := fsAttrSubtypeOf(ents, name); got != "<absent>" {
		return "type " + name + " was minted (subtype=" + got + "); it must not be"
	}
	return ""
}

type fsAttrCase struct {
	name     string
	src      string
	typeName string
	want     string
}

// TestFSharpTypeAttrPrefix_SameLineAttribute is the recall half.
//
// AXES VARIED: presence of the attribute (rows vs the two controls); number of
// attribute SECTIONS (one / two adjacent); number of attributes WITHIN a
// section (`[<CLIMutable; NoComparison>]`); attribute argument shape (none /
// generic `<int>` / string containing `]`); indentation (column 0 / indented);
// body form (record / DU / abbreviation / class); access modifier (absent /
// `private`, which is #7135's allowlist composing with this prefix).
//
// AXES HELD CONSTANT: file name and extension (`Types.fs`); the enclosing
// module header; the `=` terminator (typeRE requires it); an upper-case initial
// letter in the type name (typeRE's `[A-Z]`, not an F# rule).
func TestFSharpTypeAttrPrefix_SameLineAttribute(t *testing.T) {
	cases := []fsAttrCase{
		{
			name:     "control_no_attribute",
			src:      "module M\n\ntype SPoint = { SX: int; SY: int }\n",
			typeName: "SPoint",
			want:     "record",
		},
		{
			name:     "control_attribute_on_its_own_line",
			src:      "module M\n\n[<Struct>]\ntype SPoint = { SX: int; SY: int }\n",
			typeName: "SPoint",
			want:     "record",
		},
		{
			name:     "same_line_struct_on_record",
			src:      "module M\n\n[<Struct>] type StructRecord = { SX: int; SY: int }\n",
			typeName: "StructRecord",
			want:     "record",
		},
		{
			name:     "same_line_two_attributes_in_one_section",
			src:      "module M\n\n[<CLIMutable; NoComparison>] type Src = { A : int; B : int }\n",
			typeName: "Src",
			want:     "record",
		},
		{
			name:     "same_line_indented_struct_on_du",
			src:      "module M\n\nmodule Inner =\n    [<Struct>] type SColor = | SRed | SCustom of item: int\n",
			typeName: "SColor",
			want:     "discriminated_union",
		},
		{
			name:     "same_line_two_adjacent_sections",
			src:      "module M\n\n[<Measure>] [<Measure>] type Metre = float\n",
			typeName: "Metre",
			want:     "alias",
		},
		{
			name:     "same_line_generic_attribute_argument_nested_gt",
			src:      "module M\n\n[<A<int>>] type C = class end\n",
			typeName: "C",
			want:     "class",
		},
		{
			name:     "same_line_attribute_argument_string_holding_a_bracket",
			src:      "module M\n\n[<Emit(\"[x for x in [0, $0]][-1]\")>] type Emitted = { E: int }\n",
			typeName: "Emitted",
			want:     "record",
		},
		{
			name:     "same_line_attribute_composes_with_7135_access_modifier",
			src:      "module M\n\n[<Struct>] type private Hidden = { H: int }\n",
			typeName: "Hidden",
			want:     "record",
		},
		{
			// The GREEDY-PREFIX killer. A trailing line comment carrying a
			// SECOND attribute section and a SECOND `type … =` is the only
			// shape on which greedy and non-greedy disagree: greedy swallows
			// the prefix to the comment's `>]` and captures `Bogus`,
			// non-greedy captures `Sec`. A comment repeating the SAME
			// declaration does not discriminate — greedy's long match then
			// fails for want of a trailing `=` and the engine falls back to
			// the right one. Measured: the weaker shape leaves the greedy
			// mutant ALIVE.
			name:     "same_line_attribute_with_trailing_comment_holding_another_declaration",
			src:      "module M\n\n[<Measure>] type Sec = float // [<Foo>] type Bogus = int\n",
			typeName: "Sec",
			want:     "alias",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ents := runFSharp(t, tc.src, "Types.fs")
			if got := fsAttrSubtypeOf(ents, tc.typeName); got != tc.want {
				t.Errorf("type %s subtype=%s, want %s\nsource:\n%s\nminted: %v",
					tc.typeName, got, tc.want, tc.src, fsAttrTypeNames(ents))
			}
		})
	}
}

// TestFSharpTypeAttrPrefix_Forbidden is the over-firing half: the widened
// prefix must NOT turn these into type declarations. Each row names the
// identifier that must stay absent.
//
// Every row here is paired with TestFSharpTypeAttrPrefix_ForbiddenPredicateFires,
// which proves fsAttrForbiddenFailure actually reports a minted entity. Without
// that control these rows would pass identically if the predicate were inert.
func TestFSharpTypeAttrPrefix_Forbidden(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		absent  string
		because string
	}{
		{
			name:    "attribute_then_a_let_binding_is_not_a_type",
			src:     "module M\n\n[<Literal>] let Answer = 42\n",
			absent:  "Answer",
			because: "the prefix must be followed by the `type` keyword",
		},
		{
			name:    "attribute_then_an_identifier_that_merely_starts_with_type",
			src:     "module M\n\n[<Struct>] typeAlias = 1\n",
			absent:  "TypeAlias",
			because: "`type` must be a keyword, not a prefix of an identifier",
		},
		{
			name:    "attribute_section_spanning_a_newline_before_type",
			src:     "module M\n\n[<Struct\n>] type Spanning = { S: int }\n",
			absent:  "Spanning",
			because: "the attribute prefix is line-bound: `[^\\n]*?` may not cross a newline",
		},
		{
			name:    "unterminated_attribute_section",
			src:     "module M\n\n[<Struct type Unterminated = { U: int }\n",
			absent:  "Unterminated",
			because: "the section must close with `>]` before `type`",
		},
		{
			name:    "attribute_then_a_non_type_line_then_a_type_on_the_next_line",
			src:     "module M\n\n[<Struct>] let x = 1\n  Later = { L: int }\n",
			absent:  "Later",
			because: "the prefix is same-line only; it cannot reach a later line",
		},
		{
			name:    "attribute_inside_a_line_comment",
			src:     "module M\n\n// [<Struct>] type Commented = { C: int }\n",
			absent:  "Commented",
			because: "`^[ \\t]*` admits no `//`, so a commented-out declaration stays unmatched",
		},
		{
			name:    "declaration_inside_a_trailing_line_comment",
			src:     "module M\n\n[<Measure>] type Sec = float // [<Foo>] type Bogus = int\n",
			absent:  "Bogus",
			because: "the attribute prefix is NON-greedy; it may not reach past the real `type` into a trailing comment",
		},
		{
			name:    "attribute_after_other_code_on_the_line",
			src:     "module M\n\nlet y = 1 in [<Struct>] type Trailing = { T: int }\n",
			absent:  "Trailing",
			because: "the prefix is anchored at the start of the line, after indent only",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ents := runFSharp(t, tc.src, "Types.fs")
			if msg := fsAttrForbiddenFailure(ents, tc.absent); msg != "" {
				t.Errorf("%s (%s)\nsource:\n%s\nminted: %v",
					msg, tc.because, tc.src, fsAttrTypeNames(ents))
			}
		})
	}
}

// TestFSharpTypeAttrPrefix_ForbiddenPredicateFires is the POSITIVE CONTROL for
// every forbidden row above. It does not run the extractor: it plants the
// violation directly and asserts the predicate reports it, and asserts the
// predicate is silent on the clean slice. An absence assertion is
// indistinguishable from an unreachable one without this.
func TestFSharpTypeAttrPrefix_ForbiddenPredicateFires(t *testing.T) {
	planted := []types.EntityRecord{
		{Kind: "SCOPE.Component", Name: "Trailing", Subtype: "record"},
	}
	msg := fsAttrForbiddenFailure(planted, "Trailing")
	if msg == "" {
		t.Fatal("forbidden predicate did not fire on a PLANTED violation: " +
			"every forbidden row above is therefore unenforced")
	}
	if !strings.Contains(msg, "Trailing") || !strings.Contains(msg, "record") {
		t.Errorf("failure text %q names neither the entity nor its subtype", msg)
	}
	if got := fsAttrForbiddenFailure(nil, "Trailing"); got != "" {
		t.Errorf("forbidden predicate fired on an EMPTY slice: %q "+
			"(it would then pass no row and grade nothing)", got)
	}
	// The predicate must key on the NAME, not merely on non-emptiness.
	if got := fsAttrForbiddenFailure(planted, "SomethingElse"); got != "" {
		t.Errorf("forbidden predicate fired for an unrelated name: %q", got)
	}
}

// TestFSharpTypeAttrPrefix_CountFloor is the third grading leg: one file
// carrying every accepted shape at once, asserted by COUNT as well as by
// membership. A re-narrowing that still satisfies one recall row — say a
// prefix admitting a single section only — leaves this red.
func TestFSharpTypeAttrPrefix_CountFloor(t *testing.T) {
	src := strings.Join([]string{
		"module M",
		"",
		"type Plain = { P: int }",
		"[<Struct>] type StructRecord = { SX: int; SY: int }",
		"[<CLIMutable; NoComparison>] type Src = { A : int; B : int }",
		"[<Measure>] [<Measure>] type Metre = float",
		"[<A<int>>] type C = class end",
		"[<Struct>] type private Hidden = { H: int }",
		"",
		"module Inner =",
		"    [<Struct>] type SColor = | SRed | SCustom of item: int",
		"",
	}, "\n")
	want := []string{
		"Plain", "StructRecord", "Src", "Metre", "C", "Hidden", "SColor",
	}
	ents := runFSharp(t, src, "Types.fs")
	got := fsAttrTypeNames(ents)
	if len(got) != len(want) {
		t.Errorf("minted %d type declarations %v, want exactly %d %v",
			len(got), got, len(want), want)
	}
	have := map[string]bool{}
	for _, n := range got {
		have[n] = true
	}
	for _, n := range want {
		if !have[n] {
			t.Errorf("type %s absent; minted %v", n, got)
		}
	}
}

// TestFSharpTypeAttrPrefix_SpanStartsAtTheTypeLine pins the separator between
// an attribute section and `type` as `[ \t]*` and NOT `\s*`.
//
// The two differ only in where the MATCH BEGINS when the attribute sits on its
// own line: under `\s*` the leftmost match starts at the attribute line, so the
// entity's StartLine (and the indent captured for extractIndentBody) move up to
// it. That is a real behaviour change and nothing else in the package observes
// it — the `\s*` mutant is ALIVE against every recall, forbidden and count row
// here. It is pinned at the SHIPPED behaviour: the span starts at the `type`
// line. No claim is made that including the attribute in the span would be
// wrong, only that this change does not make it.
func TestFSharpTypeAttrPrefix_SpanStartsAtTheTypeLine(t *testing.T) {
	cases := []struct {
		name      string
		src       string
		typeName  string
		wantStart int
	}{
		// line 1 "module M", 2 blank, 3 "[<Struct>]", 4 "type Foo = …"
		{"attribute_on_its_own_line", "module M\n\n[<Struct>]\ntype Foo = { F: int }\n", "Foo", 4},
		// line 3 carries both; there is nowhere else the span could start.
		{"attribute_on_the_same_line", "module M\n\n[<Struct>] type Foo = { F: int }\n", "Foo", 3},
		{"no_attribute", "module M\n\ntype Foo = { F: int }\n", "Foo", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ents := runFSharp(t, tc.src, "Types.fs")
			found := false
			for i := range ents {
				if ents[i].Kind != "SCOPE.Component" || ents[i].Name != tc.typeName {
					continue
				}
				found = true
				if ents[i].StartLine != tc.wantStart {
					t.Errorf("type %s StartLine=%d, want %d\nsource:\n%s",
						tc.typeName, ents[i].StartLine, tc.wantStart, tc.src)
				}
			}
			if !found {
				t.Fatalf("type %s absent; minted %v", tc.typeName, fsAttrTypeNames(ents))
			}
		})
	}
}
