package vbnet

import (
	"strings"
	"testing"
)

// #6327 S3 — comment and string handling.
//
// Every rule below is stated as a positive case (the rule fires) and a
// negative case (a shape that looks like the rule but must not fire),
// following the pattern #6345 established. The negatives are the whole point:
// a comment stripper that is merely eager destroys code, and the damage is
// silent because the pre-pass emits nothing a reader can compare against.
//
// UNVERIFIED against real VB.NET source. No .vb file exists on this machine
// (checked in #6327 S2); every fixture is written from the VB.NET language
// reference and from the shapes reported in #6321.

func TestSplitComment(t *testing.T) {
	cases := []struct {
		name    string
		rule    string
		line    string
		code    string
		comment string
		kind    CommentKind
	}{
		// --- '' inside a string literal is an escaped quote, not a comment ---
		{
			name: "escaped-quote/positive", rule: "'' inside a literal is data",
			line: `Dim s As String = "a '' b"`,
			code: `Dim s As String = "a '' b"`, comment: "", kind: CommentNone,
		},
		{
			name: "escaped-quote/negative", rule: "a ' outside a literal still starts a comment",
			line: `Dim s As String = "a" ' b`,
			code: `Dim s As String = "a" `, comment: `' b`, kind: CommentTick,
		},
		{
			name: "apostrophe-in-literal", rule: "an apostrophe inside a literal is data",
			line: `Console.WriteLine("It's fine")`,
			code: `Console.WriteLine("It's fine")`, comment: "", kind: CommentNone,
		},
		{
			name: "all-escaped-quotes", rule: `"""" is a literal holding one quote`,
			line: `Dim q = """" ' quote`,
			code: `Dim q = """" `, comment: `' quote`, kind: CommentTick,
		},

		// --- tick comments ---
		{
			name: "tick/positive", rule: "' starts a comment",
			line: `Dim x = 1 ' set x`,
			code: `Dim x = 1 `, comment: `' set x`, kind: CommentTick,
		},
		{
			name: "tick/negative", rule: "no ' means no comment",
			line: `Dim x = 1`,
			code: `Dim x = 1`, comment: "", kind: CommentNone,
		},

		// --- XML doc comments are classified, not discarded ---
		{
			name: "xmldoc/positive", rule: "''' is a documentation comment",
			line: `    ''' <summary>Saves.</summary>`,
			code: `    `, comment: `''' <summary>Saves.</summary>`, kind: CommentXMLDoc,
		},
		{
			name: "xmldoc/negative", rule: "'' is an ordinary empty comment, not a doc comment",
			line: `    '' not a doc comment`,
			code: `    `, comment: `'' not a doc comment`, kind: CommentTick,
		},

		// --- REM ---
		{
			name: "rem/positive", rule: "REM at a statement boundary starts a comment",
			line: `    REM legacy note`,
			code: `    `, comment: `REM legacy note`, kind: CommentREM,
		},
		{
			name: "rem/positive-after-colon", rule: "REM after a ':' separator starts a comment",
			line: `x = 1 : REM legacy`,
			code: `x = 1 : `, comment: `REM legacy`, kind: CommentREM,
		},
		{
			name: "rem/positive-bare", rule: "REM alone on a line is an empty comment",
			line: `REM`,
			code: ``, comment: `REM`, kind: CommentREM,
		},
		{
			name: "rem/negative-prefix", rule: "REMaining is an identifier, not REM",
			line: `Dim remaining = 1`,
			code: `Dim remaining = 1`, comment: "", kind: CommentNone,
		},
		{
			name: "rem/negative-prefix-at-statement-boundary",
			rule: "REMOTE_HOST starts a statement with REM but is an identifier",
			line: `REMOTE_HOST = "acme"`,
			code: `REMOTE_HOST = "acme"`, comment: "", kind: CommentNone,
		},
		{
			name: "rem/negative-prefix-declaration",
			rule: "a declaration whose name begins with REM is not a comment",
			line: `Dim x = 1 : Remainder = 2`,
			code: `Dim x = 1 : Remainder = 2`, comment: "", kind: CommentNone,
		},
		{
			name: "rem/positive-tab-separated",
			rule: "REM followed by a tab is still a comment",
			line: "REM\tlegacy",
			code: ``, comment: "REM\tlegacy", kind: CommentREM,
		},
		{
			name: "rem/negative-member", rule: "obj.Rem is a member access, not a comment",
			line: `Dim x = obj.Rem`,
			code: `Dim x = obj.Rem`, comment: "", kind: CommentNone,
		},
		{
			name: "rem/negative-mid-expression", rule: "REM is only a comment at a statement boundary",
			line: `Dim x = a REM b`,
			code: `Dim x = a REM b`, comment: "", kind: CommentNone,
		},
		{
			name: "rem/negative-named-argument", rule: "':=' is a named argument, not a statement separator",
			line: `Foo(bar:=REMx)`,
			code: `Foo(bar:=REMx)`, comment: "", kind: CommentNone,
		},

		// --- degenerate input ---
		{
			name: "unterminated-literal", rule: "an unterminated literal swallows the rest of the line",
			line: `Dim s = "abc ' not a comment`,
			code: `Dim s = "abc ' not a comment`, comment: "", kind: CommentNone,
		},
		{
			name: "empty", rule: "an empty line has no comment",
			line: ``, code: ``, comment: "", kind: CommentNone,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, comment, kind := SplitComment(tc.line)
			if code != tc.code {
				t.Errorf("rule %q: code = %q, want %q", tc.rule, code, tc.code)
			}
			if comment != tc.comment {
				t.Errorf("rule %q: comment = %q, want %q", tc.rule, comment, tc.comment)
			}
			if kind != tc.kind {
				t.Errorf("rule %q: kind = %v, want %v", tc.rule, kind, tc.kind)
			}
			if code+comment != tc.line {
				t.Errorf("rule %q: split is lossy: %q + %q != %q", tc.rule, code, comment, tc.line)
			}
		})
	}
}

// fill renders n StringFill bytes, so masked expectations stay readable.
func fill(n int) string { return strings.Repeat(string(rune(StringFill)), n) }

func TestMaskStringLiterals(t *testing.T) {
	cases := []struct {
		name string
		rule string
		line string
		want string
	}{
		{
			name: "call-shape-in-literal/positive",
			rule: "a call shape inside a literal must not reach the later passes",
			line: `Dim s = "Foo(1)"`,
			want: `Dim s = "` + fill(6) + `"`,
		},
		{
			name: "call-shape-outside-literal/negative",
			rule: "code outside a literal is untouched",
			line: `Dim s = Foo(1)`,
			want: `Dim s = Foo(1)`,
		},
		{
			name: "escaped-quote/positive",
			rule: `"a '' b" is one literal, so the whole interior is masked`,
			line: `Dim s = "a '' b"`,
			want: `Dim s = "` + fill(6) + `"`,
		},
		{
			// The "" escape rule, which the SplitComment cases above do NOT
			// reach: `"a '' b"` exercises "an apostrophe is not a comment
			// introducer", a different rule entirely. A mutation removing the
			// "" escape survived the whole suite until this case was added.
			name: "escaped-quote-pair/positive",
			rule: `"" inside a literal is one escaped quote, so the literal is one span`,
			line: `Dim s = "a""b"`,
			want: `Dim s = "` + fill(4) + `"`,
		},
		{
			name: "escaped-quote-pair/negative",
			rule: "two adjacent literals are two spans, and the code between them survives",
			line: `Dim s = "a" & "b"`,
			want: `Dim s = "` + fill(1) + `" & "` + fill(1) + `"`,
		},
		{
			name: "two-literals",
			rule: "each literal is masked independently and the code between survives",
			line: `Dim s = "ab" & "cd"`,
			want: `Dim s = "` + fill(2) + `" & "` + fill(2) + `"`,
		},
		{
			name: "length-preserved",
			rule: "masking preserves byte length so offsets stay valid",
			line: `Dim s = "hello world"`,
			want: `Dim s = "` + fill(11) + `"`,
		},
		{
			name: "no-literal/negative",
			rule: "a line with no literal is returned unchanged",
			line: `Dim x = 1 + 2`,
			want: `Dim x = 1 + 2`,
		},
		{
			name: "colon-in-literal",
			rule: "a ':' inside a literal must not read as a statement separator",
			line: `Dim s = "a:b"`,
			want: `Dim s = "` + fill(3) + `"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MaskStringLiterals(tc.line)
			if got != tc.want {
				t.Errorf("rule %q: got %q, want %q", tc.rule, got, tc.want)
			}
			if len(got) != len(tc.line) {
				t.Errorf("rule %q: length changed: %d -> %d", tc.rule, len(tc.line), len(got))
			}
		})
	}
}

func TestFoldName(t *testing.T) {
	cases := []struct {
		name  string
		rule  string
		a, b  string
		equal bool
	}{
		{name: "identifier-case/positive", rule: "VB.NET identifiers are case-insensitive", a: "Foo", b: "FOO", equal: true},
		{name: "keyword-case/positive", rule: "VB.NET keywords are case-insensitive", a: "Dim", b: "dim", equal: true},
		{name: "mixed/positive", rule: "any casing folds to the same name", a: "btnSave", b: "BTNSAVE", equal: true},
		{name: "distinct/negative", rule: "different names still differ", a: "Foo", b: "Bar", equal: false},
		{name: "underscore/negative", rule: "folding does not strip underscores", a: "my_var", b: "myvar", equal: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FoldName(tc.a) == FoldName(tc.b); got != tc.equal {
				t.Errorf("rule %q: FoldName(%q)==FoldName(%q) = %v, want %v", tc.rule, tc.a, tc.b, got, tc.equal)
			}
		})
	}
}

func TestCommentBody(t *testing.T) {
	cases := []struct {
		name    string
		comment string
		kind    CommentKind
		want    string
	}{
		{name: "xmldoc", comment: "''' <summary>x</summary>", kind: CommentXMLDoc, want: "<summary>x</summary>"},
		{name: "tick", comment: "' note", kind: CommentTick, want: "note"},
		{name: "rem", comment: "REM note", kind: CommentREM, want: "note"},
		{name: "none/negative", comment: "", kind: CommentNone, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CommentBody(tc.comment, tc.kind); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
