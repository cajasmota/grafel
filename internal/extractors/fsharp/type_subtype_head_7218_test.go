package fsharp_test

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// #7218 — classifyTypeSubtype decided an F# type's subtype partly from `decl`,
// the declaration HEAD. typeRE's match ENDS AT the `=`, so `decl` is only ever
// "type Foo =" and never the body. The head is the wrong place to look for
// anything F# writes after the `=`, and that one fact produced three defects
// at once, each of which masks the others:
//
//	direction                     shape                              was        want
//	1 unreachable arm             type Point3D = struct … end        "type"     "struct"
//	2 over-fires on identifier    type Mystruct = int                "struct"   "alias"
//	3 over-fires on punctuation   type Shape (* = {a} *) = | A | B   "record"   "discriminated_union"
//
// A fourth row of the same family lives on the BODY side and is fixed here
// because the fix would otherwise widen it: the `interface` / `class` arms
// tested `strings.HasPrefix(bodyTrimmed, kw)`, a PREFIX and not a TOKEN, so a
// lower-case abbreviation target whose name merely begins with the keyword was
// classified by it (`type Alias2 = classic` -> "class", measured). Adding a
// `struct` body arm without a token boundary would have minted the same defect
// a third time (`type Alias1 = structural` -> "struct"), so the boundary is
// part of the fix, not scope creep. It also aligns the classifier with
// isAliasBody, which has always compared strings.Fields(b)[0] for EQUALITY
// against the same keyword set.
//
// FIXTURE LEGALITY. There is no F# toolchain on this machine, so every fixture
// below is derived from the language reference and named here:
//
//   - `type Point3D = struct / val x: float / end` is the explicit structure
//     syntax of the F# reference "Structures" ("type type-name = struct
//     type-definition-elements end"); the three-`val` Point3D example is the
//     reference's own.
//   - `type Mystruct = int` is a type abbreviation (reference "Type
//     Abbreviations"); the identifier's initial upper-case letter is required
//     by typeRE, not by F#.
//   - The block comment `(* … *)` is legal wherever whitespace is legal
//     (reference "Comments"), so it is legal between the type name and the `=`.
//   - The two fixtures the ISSUE cited for direction 3 are NOT legal F# and are
//     deliberately NOT used: `type Foo<'T when 'T = {x:int}> =` is not a
//     constraint (every form in the reference's "Constraints" table is spelled
//     `'T : …`; there is no `=` constraint), and `type Foo(x = {a}) =` is not a
//     primary-constructor parameter (those are patterns; `=` is not a pattern
//     operator). The mechanism they describe is real — the block-comment
//     fixture below reproduces it on legal source — but their exact text would
//     have pinned the extractor against source no F# compiler accepts.

// fsSubtypeOf returns the Subtype of the named type-declaration
// SCOPE.Component, or a distinguishable sentinel when nothing was minted. It
// is deliberately NOT a membership test against fsTypeSubtypes: a set that
// lists "struct" proves nothing about whether "struct" is ever produced, which
// is how direction 1 stayed invisible.
func fsSubtypeOf(ents []types.EntityRecord, name string) string {
	for i := range ents {
		if ents[i].Kind == "SCOPE.Component" && ents[i].Name == name {
			return ents[i].Subtype
		}
	}
	return "<absent>"
}

// fsSubtypeRowFailure is the row predicate itself, extracted so it can be
// graded independently of any extractor run: it returns the failure text for a
// row, or "" when the row holds. TestFSharpTypeSubtype_RowPredicateFires plants
// a violation directly into an entity slice and asserts this returns non-empty
// — an absence assertion passes identically whether it is enforced or simply
// unreachable, and no mutant of the extractor can tell those two apart.
func fsSubtypeRowFailure(ents []types.EntityRecord, name, want string) string {
	got := fsSubtypeOf(ents, name)
	if got == want {
		return ""
	}
	return "type " + name + " subtype=" + got + ", want " + want
}

// fsSubtypeCase is one row. `want` is asserted positively; `forbidden`, when
// non-empty, is the value shipped BEFORE #7218 and is asserted separately, so
// a future regression to it fails with the row's own name rather than only as
// a diff against `want`.
//
// The `forbidden` assertion is deliberately recorded as an EQUIVALENT mutant:
// deleting it is ALIVE at 0 `--- FAIL`, and unkillable by construction, because
// every row has forbidden != want and `got == want` already implies
// `got != forbidden`. It is kept for its failure text, which names the
// pre-#7218 verdict, and not as independent coverage — no reviewer should
// score it as such.
type fsSubtypeCase struct {
	name      string
	src       string
	typeName  string
	want      string
	forbidden string
}

// fsSubtypeCaseDefect reports why a row cannot grade anything, or "" when the
// row is well formed. It exists because the `forbidden` assertion is only
// redundant (see the EQUIVALENT note above) so long as every row keeps
// forbidden != want and a non-empty want. Nothing enforced that; a future row
// with forbidden == want would look like coverage and be permanently ungraded.
// It is a pure function so the guard itself has a positive control
// (TestFSharpTypeSubtype_RowGuardFires) — a trip-wire that no row trips is
// otherwise indistinguishable from one that is never consulted. Both halves
// of the guard are scored there, and the control row for the empty-want half
// carries a NON-EMPTY forbidden on purpose: with both empty the two halves
// mask each other and the first is ungraded.
//
// Its CALL SITE in runFSSubtypeCases below is a different matter and is
// PRICED AND DECLINED, not unkillable: short-circuiting it is ALIVE at 0
// `--- FAIL`, and killing it would take a fake testing.TB seam — roughly
// 30-40 lines plus a fake type — to observe that the helper fatals on a
// defective row. For a trip-wire on the test table that is not worth it; the
// cost is recorded here so the judgement can be revisited rather than
// re-derived.
func fsSubtypeCaseDefect(tc fsSubtypeCase) string {
	if tc.want == "" {
		return "row " + tc.name + " has an empty want: it asserts nothing"
	}
	if tc.forbidden == tc.want {
		return "row " + tc.name + " forbids " + tc.forbidden + ", the value it also wants"
	}
	return ""
}

func runFSSubtypeCases(t *testing.T, cases []fsSubtypeCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if d := fsSubtypeCaseDefect(tc); d != "" {
				t.Fatal(d)
			}
			ents := runFSharp(t, tc.src, "Types.fs")
			if msg := fsSubtypeRowFailure(ents, tc.typeName, tc.want); msg != "" {
				t.Errorf("%s\nsource:\n%s", msg, tc.src)
			}
			if tc.forbidden != "" {
				if got := fsSubtypeOf(ents, tc.typeName); got == tc.forbidden {
					t.Errorf("type %s classified %q — the pre-#7218 verdict, decided by the "+
						"declaration HEAD rather than the body\nsource:\n%s",
						tc.typeName, tc.forbidden, tc.src)
				}
			}
		})
	}
}

// TestFSharpTypeSubtype_StructIsReachable — DIRECTION 1, positive.
// The `struct` arm had no body companion (`interface` and `class` both had
// one), so it could only ever be reached through the head, where the keyword
// can never appear. This asserts the VALUE is produced.
func TestFSharpTypeSubtype_StructIsReachable(t *testing.T) {
	runFSSubtypeCases(t, []fsSubtypeCase{
		{
			// Explicit structure syntax, body on its own lines.
			name: "struct body on following lines",
			src: "module M\n\n" +
				"type Point3D =\n" +
				"    struct\n" +
				"        val x: float\n" +
				"        val y: float\n" +
				"        val z: float\n" +
				"    end\n",
			typeName: "Point3D", want: "struct", forbidden: "type",
		},
		{
			// Same construct with `struct` on the declaration line. This varies
			// ONLY the line break after `=`; the members, the indentation of the
			// members and the type name are held constant against the row above.
			name: "struct keyword on the declaration line",
			src: "module M\n\n" +
				"type Point2D = struct\n" +
				"        val x: float\n" +
				"        val y: float\n" +
				"    end\n",
			typeName: "Point2D", want: "struct", forbidden: "type",
		},
		{
			// [<Struct>] on its OWN line, plus the explicit struct…end body. The
			// attribute is not what is being relied on (see the not-claimed note
			// in TestFSharpTypeSubtype_NotClaimed); this row varies the presence
			// of a preceding attribute line and holds the body constant.
			name: "preceding attribute line does not disturb the body arm",
			src: "module M\n\n" +
				"[<Struct>]\n" +
				"type Vec2 =\n" +
				"    struct\n" +
				"        val x: float\n" +
				"        val y: float\n" +
				"    end\n",
			typeName: "Vec2", want: "struct", forbidden: "type",
		},
	})
}

// TestFSharpTypeSubtype_IdentifierIsNotAKeyword — DIRECTION 2, forbidden.
// `strings.Contains(decl, kw)` put the type's own IDENTIFIER in scope, so every
// abbreviation whose name contained a kind keyword was classified by it. Recall
// assertions cannot see this: each of these types WAS minted, with a subtype
// that is a member of fsTypeSubtypes. Only a forbidden row catches it.
//
// Positive control: TestFSharpTypeSubtype_StructIsReachable above produces
// "struct" legitimately, and the class/interface controls in
// TestFSharpTypeSubtype_ControlsUnchanged produce "class" and "interface", so
// each forbidden value here is demonstrably observable by this census.
func TestFSharpTypeSubtype_IdentifierIsNotAKeyword(t *testing.T) {
	runFSSubtypeCases(t, []fsSubtypeCase{
		{
			name:     "name containing struct",
			src:      "module M\n\ntype Mystruct = int\n",
			typeName: "Mystruct", want: "alias", forbidden: "struct",
		},
		{
			name:     "name containing class",
			src:      "module M\n\ntype Myclassy = int\n",
			typeName: "Myclassy", want: "alias", forbidden: "class",
		},
		{
			name:     "name containing interface",
			src:      "module M\n\ntype Ainterfaced = int\n",
			typeName: "Ainterfaced", want: "alias", forbidden: "interface",
		},
		{
			// The keyword in the name must not win over a REAL body either —
			// this holds the name shape constant against the row above and
			// varies the body from an abbreviation to a record.
			name:     "name containing struct, record body",
			src:      "module M\n\ntype Mystruct2 = {\n    Name: string\n}\n",
			typeName: "Mystruct2", want: "record", forbidden: "struct",
		},
	})
}

// TestFSharpTypeSubtype_HeadPunctuationIsNotTheBody — DIRECTION 3, forbidden.
// `strings.Contains(decl, "= {")` fired on any `= {` sitting before the final
// `=`. A block comment is the shape that puts one there in LEGAL F#; the arm
// then decided the subtype from commented-out text, overruling the real body.
//
// Positive control: the record rows in TestFSharpTypeSubtype_ControlsUnchanged
// produce "record" from a real record body, so "record" is observable here.
func TestFSharpTypeSubtype_HeadPunctuationIsNotTheBody(t *testing.T) {
	runFSSubtypeCases(t, []fsSubtypeCase{
		{
			name: "commented-out record in the head, DU body",
			src: "module M\n\n" +
				"type Shape (* was = { Kind: string } *) =\n" +
				"    | Circle of float\n" +
				"    | Square of float\n",
			typeName: "Shape", want: "discriminated_union", forbidden: "record",
		},
		{
			// Same head, different body: holds the comment constant and varies
			// the body, so the row grades the body arm rather than the comment.
			name: "commented-out record in the head, class body",
			src: "module M\n\n" +
				"type Widget (* was = { Size: int } *) =\n" +
				"    class\n" +
				"        member this.X = 1\n" +
				"    end\n",
			typeName: "Widget", want: "class", forbidden: "record",
		},
	})
}

// TestFSharpTypeSubtype_BodyKeywordIsATokenNotAPrefix — the fourth row.
// The body arms were prefix tests, so an abbreviation target that merely BEGINS
// with a kind keyword was classified by it. `type Alias2 = classic` -> "class"
// was measured on the pre-fix binary; `type Alias1 = structural` -> "struct"
// is what a prefix-only `struct` body arm would have newly minted.
//
// Positive control: the `struct` / `class` / `interface` rows elsewhere in this
// file produce those three values from real bodies.
func TestFSharpTypeSubtype_BodyKeywordIsATokenNotAPrefix(t *testing.T) {
	runFSSubtypeCases(t, []fsSubtypeCase{
		{
			name:     "abbreviation target beginning with struct",
			src:      "module M\n\ntype Alias1 = structural\n",
			typeName: "Alias1", want: "alias", forbidden: "struct",
		},
		{
			name:     "abbreviation target beginning with class",
			src:      "module M\n\ntype Alias2 = classic\n",
			typeName: "Alias2", want: "alias", forbidden: "class",
		},
		{
			name:     "abbreviation target beginning with interface",
			src:      "module M\n\ntype Alias3 = interfaces\n",
			typeName: "Alias3", want: "alias", forbidden: "interface",
		},
		// The F# identifier CONTINUATION alphabet is enumerated here rather
		// than sampled, by the CATEGORIES of F# 4.1 spec §3.4 — ident-char =
		// letter-char (\Lu \Ll \Lt \Lm \Lo \Nl) | digit-char (\Nd) |
		// connecting-char (\Pc, which is where `_` itself lives) |
		// combining-char (\Mn \Mc) | formatting-char (\Cf) | `'` | `_`.
		// Each category is separately deletable from the boundary check, and
		// the ASCII rows above reach only two of them. The first revision of
		// this fix read rest[0] as a BYTE and ran it through an ASCII-only
		// isLetter, so every multi-byte continuation looked like a token
		// boundary and `type AliasB = structא` classified as "struct" — a
		// wrong answer this fix introduced on its own new path, caught in
		// review of the first revision and pinned by the non-ASCII rows below.
		{
			name:     "abbreviation target: keyword then a digit",
			src:      "module M\n\ntype Alias4 = struct2\n",
			typeName: "Alias4", want: "alias", forbidden: "struct",
		},
		{
			name:     "abbreviation target: keyword then an underscore",
			src:      "module M\n\ntype Alias5 = class_t\n",
			typeName: "Alias5", want: "alias", forbidden: "class",
		},
		{
			name:     "abbreviation target: keyword then a prime",
			src:      "module M\n\ntype Alias6 = interface'\n",
			typeName: "Alias6", want: "alias", forbidden: "interface",
		},
		{
			// CASE. `strings.HasPrefix` is case-sensitive and so is F#, but
			// nothing in this package said so: making the prefix match
			// case-insensitive (`strings.ToLower(bodyTrimmed)`) survived the
			// whole package suite at 0 `--- FAIL`, and it is REACHABLE, not
			// equivalent — under it `type AliasJ = Struct` classifies
			// "struct". F# types are PascalCase by convention, so a
			// user-defined type named `Struct` is idiomatic rather than
			// exotic, and a later "robustness" tidy-up that normalises case
			// would misclassify it while every other row here stayed green.
			//
			// Note the boundary check does NOT cover this: `Classy` is
			// rejected by the boundary whatever the case folding, so only an
			// exactly-case-differing keyword misclassifies. That is why the
			// row is the bare keyword and not a longer identifier.
			name:     "capitalised keyword as an abbreviation target",
			src:      "module M\n\ntype AliasJ = Struct\n",
			typeName: "AliasJ", want: "alias", forbidden: "struct",
		},
		{
			name:     "capitalised keyword as an abbreviation target (interface)",
			src:      "module M\n\ntype AliasK = Interface\n",
			typeName: "AliasK", want: "alias", forbidden: "interface",
		},
		{
			name:     "capitalised keyword as an abbreviation target (class)",
			src:      "module M\n\ntype AliasL = Class\n",
			typeName: "AliasL", want: "alias", forbidden: "class",
		},
		{
			// letter-char, non-ASCII: HEBREW LETTER ALEF U+05D0 is \Lo.
			name:     "keyword then a non-ASCII letter (Lo)",
			src:      "module M\n\ntype AliasB = struct\u05d0\n",
			typeName: "AliasB", want: "alias", forbidden: "struct",
		},
		{
			// letter-char, non-ASCII: GREEK SMALL LETTER ALPHA U+03B1 is \Ll.
			name:     "keyword then a non-ASCII letter (Ll)",
			src:      "module M\n\ntype AliasC = class\u03b1\n",
			typeName: "AliasC", want: "alias", forbidden: "class",
		},
		{
			// letter-char: ROMAN NUMERAL TWO U+2161 is \Nl, which is part of
			// letter-char in §3.4 and is NOT covered by unicode.IsLetter.
			name:     "keyword then a letter-number (Nl)",
			src:      "module M\n\ntype AliasD = interface\u2161\n",
			typeName: "AliasD", want: "alias", forbidden: "interface",
		},
		{
			// digit-char, non-ASCII: ARABIC-INDIC DIGIT THREE U+0663 is \Nd.
			name:     "keyword then a non-ASCII digit (Nd)",
			src:      "module M\n\ntype AliasE = struct\u0663\n",
			typeName: "AliasE", want: "alias", forbidden: "struct",
		},
		{
			// connecting-char other than `_`: UNDERTIE U+203F is \Pc.
			name:     "keyword then a connecting char (Pc)",
			src:      "module M\n\ntype AliasF = class\u203ft\n",
			typeName: "AliasF", want: "alias", forbidden: "class",
		},
		{
			// combining-char: COMBINING GRAVE ACCENT U+0300 is \Mn.
			name:     "keyword then a non-spacing mark (Mn)",
			src:      "module M\n\ntype AliasG = interface\u0300\n",
			typeName: "AliasG", want: "alias", forbidden: "interface",
		},
		{
			// combining-char: DEVANAGARI SIGN VISARGA U+0903 is \Mc.
			name:     "keyword then a spacing mark (Mc)",
			src:      "module M\n\ntype AliasH = struct\u0903\n",
			typeName: "AliasH", want: "alias", forbidden: "struct",
		},
		{
			// formatting-char: ZERO WIDTH JOINER U+200D is \Cf.
			name:     "keyword then a formatting char (Cf)",
			src:      "module M\n\ntype AliasI = class\u200dt\n",
			typeName: "AliasI", want: "alias", forbidden: "class",
		},
	})
}

// TestFSharpTypeSubtype_BodyIsExactlyTheKeyword grades the boundary check's
// `rest == ""` branch, which nothing else reaches: every other fixture has
// something after the keyword, so flipping that branch to `return false` was
// ALIVE at 0 `--- FAIL` in the first revision's table (found in review, N1).
//
// FIXTURE HONESTY: `type Point = struct` with nothing after it is NOT a
// complete F# declaration — the explicit form requires members and `end`. It is
// an INCOMPLETE BUFFER, which is a real input for an extractor that indexes
// files as they are edited, and it is labelled as such rather than dressed up
// as legal source. What is pinned is that a half-written struct is recognised
// as a struct and not as an alias to a type named `struct`.
func TestFSharpTypeSubtype_BodyIsExactlyTheKeyword(t *testing.T) {
	runFSSubtypeCases(t, []fsSubtypeCase{
		{
			name:     "incomplete buffer: body is exactly the keyword",
			src:      "module M\n\ntype Point = struct\n",
			typeName: "Point", want: "struct", forbidden: "type",
		},
	})
}

// TestFSharpTypeSubtype_RowGuardFires is the positive control for
// fsSubtypeCaseDefect. A guard that no row trips passes identically whether it
// is enforced or never consulted — the same hole the forbidden rows have — so a
// defective row is planted here and the guard must name it.
func TestFSharpTypeSubtype_RowGuardFires(t *testing.T) {
	if d := fsSubtypeCaseDefect(fsSubtypeCase{
		name: "planted", want: "struct", forbidden: "struct",
	}); d == "" {
		t.Error("guard accepted a row that forbids the value it wants: every " +
			"`forbidden` assertion in this file is then unconstrained")
	}
	// This row carries a NON-EMPTY forbidden on purpose: with forbidden also
	// empty the two guards mask each other, and the empty-want branch is then
	// ungraded no matter how it is mutated (measured: ALIVE at 0 --- FAIL).
	if d := fsSubtypeCaseDefect(fsSubtypeCase{
		name: "planted", want: "", forbidden: "struct",
	}); d == "" {
		t.Error("guard accepted a row with an empty want")
	}
	// Inverted control: a well-formed row must pass, so the guard is a
	// discrimination and not a constant failure.
	if d := fsSubtypeCaseDefect(fsSubtypeCase{
		name: "ok", want: "alias", forbidden: "struct",
	}); d != "" {
		t.Errorf("guard rejected a well-formed row: %s", d)
	}
	// A row may legitimately omit `forbidden` — most controls do.
	if d := fsSubtypeCaseDefect(fsSubtypeCase{name: "ok", want: "record"}); d != "" {
		t.Errorf("guard rejected a control row with no forbidden value: %s", d)
	}
}

// TestFSharpTypeSubtype_ControlsUnchanged pins what the fix must NOT move. The
// private-record row is the modifier-tolerance control: typeRE's
// `(?:\s+(?:public|private|internal)\b)*` allowlist is the only reason a
// modified declaration reaches the classifier at all (#7135), and reading the
// body instead of the head must not quietly depend on it differently.
func TestFSharpTypeSubtype_ControlsUnchanged(t *testing.T) {
	runFSSubtypeCases(t, []fsSubtypeCase{
		{"record", "module M\n\ntype Person = {\n    Name: string\n    Age: int\n}\n", "Person", "record", ""},
		{"record with private modifier", "module M\n\ntype private Secret = {\n    Token: string\n}\n", "Secret", "record", ""},
		{"record on the declaration line", "module M\n\ntype Rc = { A: int }\n", "Rc", "record", ""},
		{"discriminated union", "module M\n\ntype Colour =\n    | Red\n    | Green\n", "Colour", "discriminated_union", ""},
		{"discriminated union on the declaration line", "module M\n\ntype Col = | R | G\n", "Col", "discriminated_union", ""},
		{"interface", "module M\n\ntype IService =\n    interface\n        abstract member Process: string -> string\n    end\n", "IService", "interface", ""},
		{"class", "module M\n\ntype Runner =\n    class\n        member this.Go () = 1\n    end\n", "Runner", "class", ""},
		{"alias", "module M\n\ntype Id = int\n", "Id", "alias", ""},
		// The record and DU arms are PREFIX tests, and nothing else in this
		// package distinguishes a prefix from a substring for them: relaxing
		// either to strings.Contains survived the whole package suite until
		// these two rows existed. A record literal and a `match` are the
		// ordinary ways a `{` and a `|` appear inside a body that is not a
		// record or a DU.
		{"class body containing a record literal", "module M\n\ntype Defaults =\n    class\n        member this.Config = { Retries = 3 }\n    end\n", "Defaults", "class", "record"},
		{"class body containing a match expression", "module M\n\ntype Guard =\n    class\n        member this.Check x =\n            match x with\n            | 0 -> true\n            | _ -> false\n    end\n", "Guard", "class", "discriminated_union"},
		{"catch-all type", "module M\n\ntype Holder =\n    member this.A = 1\n    member this.B = 2\n", "Holder", "type", ""},
	})
}

// TestFSharpTypeSubtype_RowPredicateFires is the positive control for the row
// predicate itself. Every forbidden row above is an absence assertion, and an
// absence assertion passes identically whether it is enforced or simply
// unreachable — if fsSubtypeOf silently returned "" for every input, or if
// runFSSubtypeCases dropped its comparison, every row would still be green. So
// a violation is planted directly into an entity slice and the predicate must
// report it.
func TestFSharpTypeSubtype_RowPredicateFires(t *testing.T) {
	planted := []types.EntityRecord{
		{Kind: "SCOPE.Component", Name: "Mystruct", Subtype: "struct"},
	}
	msg := fsSubtypeRowFailure(planted, "Mystruct", "alias")
	if msg == "" {
		t.Fatal("planted violation (Mystruct classified \"struct\") was not reported: " +
			"the forbidden rows in this file assert nothing")
	}
	if !strings.Contains(msg, "struct") || !strings.Contains(msg, "alias") {
		t.Errorf("failure text %q names neither the observed nor the wanted subtype", msg)
	}
	// Inverted control: the same predicate must stay silent on a conforming
	// slice, so the row above is a discrimination and not a constant failure.
	if msg := fsSubtypeRowFailure([]types.EntityRecord{
		{Kind: "SCOPE.Component", Name: "Mystruct", Subtype: "alias"},
	}, "Mystruct", "alias"); msg != "" {
		t.Errorf("predicate reported %q on a conforming slice", msg)
	}
	// And it must not be satisfied by an entity of another Kind wearing the
	// name, which is how a census can go vacuously green.
	if msg := fsSubtypeRowFailure([]types.EntityRecord{
		{Kind: "SCOPE.Module", Name: "Mystruct", Subtype: "alias"},
	}, "Mystruct", "alias"); msg == "" {
		t.Error("predicate accepted a non-Component entity as the subject")
	}
}

// TestFSharpTypeSubtype_NotClaimed records what this change does NOT fix, so a
// later reader does not mistake the rows above for coverage of the whole space.
// Each is asserted at its CURRENT behaviour, without claiming that behaviour is
// correct; the test fails if any of them silently changes.
func TestFSharpTypeSubtype_NotClaimed(t *testing.T) {
	// (a) Attributes are never read. `[<Struct>] type Vec = { X: float }` — the
	// attribute-only struct form of the F# reference, with the struct/end
	// omitted — is indistinguishable from a record to this classifier.
	ents := runFSharp(t, "module M\n\n[<Struct>]\ntype Vec =\n    { X: float }\n", "A.fs")
	if got := fsSubtypeOf(ents, "Vec"); got != "record" {
		t.Errorf("attribute-only struct form: subtype=%q, want the unchanged %q "+
			"(attributes are not read; #7218 did not claim this)", got, "record")
	}
	// (b) An attribute on the SAME LINE as the declaration defeats typeRE
	// outright — the pattern is `(?m)^[ \t]*type`, so nothing is minted at all
	// and the classifier is never reached. A separate gap from #7218.
	ents = runFSharp(t, "module M\n\n[<Struct>] type Vec3 =\n    struct\n        val x: float\n    end\n", "B.fs")
	if got := fsSubtypeOf(ents, "Vec3"); got != "<absent>" {
		t.Errorf("same-line attribute: subtype=%q, want no entity at all — if this "+
			"now mints, typeRE changed and this note is stale", got)
	}
	// (c) The `and` continuation form (`type A = … and B = …`) is not scanned by
	// typeRE at all, so B has no subtype to get wrong.
	ents = runFSharp(t, "module M\n\ntype A = { X: int }\nand B = { Y: int }\n", "C.fs")
	if got := fsSubtypeOf(ents, "B"); got != "<absent>" {
		t.Errorf("`and` continuation: subtype=%q, want no entity at all", got)
	}
}
