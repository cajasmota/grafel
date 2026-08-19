package vbnet

import (
	"reflect"
	"strings"
	"testing"
)

// #6327 S3 — line-continuation joining.
//
// Getting this wrong silently merges or splits declarations, which is why
// every rule carries a negative: a joiner that is merely eager eats the
// statement below the one it was joining, and nothing downstream can tell.
//
// UNVERIFIED against real VB.NET source; see the package doc.

func texts(lines []LogicalLine) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, l.Text)
	}
	return out
}

func TestJoinContinuations(t *testing.T) {
	cases := []struct {
		name string
		rule string
		src  string
		want []string
	}{
		// --- explicit '_' continuation ---
		{
			name: "explicit/positive",
			rule: "a trailing ' _' joins the next physical line",
			src:  "Dim x = 1 _\n+ 2",
			want: []string{"Dim x = 1 + 2"},
		},
		{
			name: "explicit/positive-three-lines",
			rule: "explicit continuation chains",
			src:  "Dim x = 1 _\n+ 2 _\n+ 3",
			want: []string{"Dim x = 1 + 2 + 3"},
		},
		{
			name: "explicit/negative-identifier",
			rule: "a trailing '_' with no space before it is part of an identifier",
			src:  "Dim x = my_var_\nDim y = 2",
			want: []string{"Dim x = my_var_", "Dim y = 2"},
		},
		{
			name: "explicit/positive-with-trailing-comment",
			rule: "comments are removed before continuation is decided",
			src:  "Dim x = 1 _ ' note\n+ 2",
			want: []string{"Dim x = 1 + 2"},
		},
		{
			name: "explicit/negative-underscore-in-comment",
			rule: "a '_' inside a comment is not a continuation",
			src:  "Dim x = 1 ' trailing _\nDim y = 2",
			want: []string{"Dim x = 1", "Dim y = 2"},
		},

		// --- implicit continuation via bracket depth ---
		{
			name: "implicit/positive-open-paren",
			rule: "an unclosed '(' continues onto the next line",
			src:  "Sub F(a As Integer,\nb As Integer)",
			want: []string{"Sub F(a As Integer, b As Integer)"},
		},
		{
			name: "implicit/positive-closing-paren-alone",
			rule: "a ')' on its own line closes the continuation",
			src:  "Sub F(a As Integer,\nb As Integer\n)",
			want: []string{"Sub F(a As Integer, b As Integer )"},
		},
		{
			name: "implicit/negative-paren-in-literal",
			rule: "a '(' inside a string literal must not raise bracket depth",
			src:  "Dim s = \"(\"\nDim y = 2",
			want: []string{"Dim s = \"(\"", "Dim y = 2"},
		},
		{
			name: "implicit/negative-paren-inside-escaped-quotes",
			rule: `a '(' between "" escapes is still inside the literal and must not raise depth`,
			src:  "Dim s = \"a\"\"(\"\"b\"\nDim y = 2",
			want: []string{"Dim s = \"a\"\"(\"\"b\"", "Dim y = 2"},
		},
		{
			name: "implicit/negative-balanced",
			rule: "a balanced call does not continue",
			src:  "Save(total)\nDim y = 2",
			want: []string{"Save(total)", "Dim y = 2"},
		},
		{
			name: "implicit/negative-unbalanced-close",
			rule: "a stray ')' clamps depth at zero instead of joining the rest of the file",
			src:  "Dim x = 1)\nDim y = 2",
			want: []string{"Dim x = 1)", "Dim y = 2"},
		},

		// --- implicit continuation via trailing token ---
		{
			name: "implicit/positive-trailing-operator",
			rule: "a trailing binary operator continues",
			src:  "Dim x = 1 +\n2",
			want: []string{"Dim x = 1 + 2"},
		},
		{
			name: "implicit/positive-trailing-keyword",
			rule: "a trailing AndAlso continues",
			src:  "Dim b = a AndAlso\nc",
			want: []string{"Dim b = a AndAlso c"},
		},
		{
			name: "implicit/negative-member-named-like-query-operator",
			rule: "obj.Where is a member access, not the query operator",
			src:  "Dim n = q.Where\nDim m = 2",
			want: []string{"Dim n = q.Where", "Dim m = 2"},
		},
		{
			name: "implicit/negative-end-select",
			rule: "`End Select` ends a statement, it does not continue one",
			src:  "End Select\nDim y = 1",
			want: []string{"End Select", "Dim y = 1"},
		},
		{
			name: "implicit/negative-end-with",
			rule: "`End With` closes a block, it does not continue a statement",
			src:  "With obj\n.A = 1\nEnd With\nEnd Sub",
			want: []string{"With obj", ".A = 1", "End With", "End Sub"},
		},
		{
			name: "implicit/negative-end-block-generally",
			rule: "no `End <keyword>` continues, whatever keyword follows End",
			src:  "End Group\nDim y = 1",
			want: []string{"End Group", "Dim y = 1"},
		},
		{
			name: "implicit/negative-option-strict-on",
			rule: "`Option Strict On` is a file header, not a LINQ `Join … On`",
			src:  "Option Strict On\nPublic Class C",
			want: []string{"Option Strict On", "Public Class C"},
		},
		{
			name: "implicit/negative-option-explicit-on",
			rule: "every Option header ends the line, whatever word it ends on",
			src:  "Option Explicit On\nImports SB = System.Text.StringBuilder",
			want: []string{"Option Explicit On", "Imports SB = System.Text.StringBuilder"},
		},
		{
			name: "implicit/positive-join-on-still-continues",
			rule: "the LINQ position '&' On was added for still joins",
			src:  "Dim q = From a In b Join c In d On\na.K Equals c.K",
			want: []string{"Dim q = From a In b Join c In d On a.K Equals c.K"},
		},
		{
			name: "implicit/negative-long-type-character",
			rule: "`32&` is a Long literal, not a dangling concatenation operator",
			src:  "Public Const MAX = 32&\nPublic Const MIN = 1",
			want: []string{"Public Const MAX = 32&", "Public Const MIN = 1"},
		},
		{
			name: "implicit/negative-hex-long-type-character",
			rule: "`&HFFFF&` is a hex Long literal, pervasive in Win32 interop",
			src:  "Public Const MASK = &HFFFF&\nPublic Const NEXT_ = 1",
			want: []string{"Public Const MASK = &HFFFF&", "Public Const NEXT_ = 1"},
		},
		{
			name: "implicit/positive-concatenation-after-literal",
			rule: "a '&' that is not glued to an identifier is still the concatenation operator",
			src:  "Dim s = \"a\" &\nb",
			want: []string{"Dim s = \"a\" & b"},
		},
		{
			name: "implicit/negative-plain-statement",
			rule: "an ordinary statement does not continue",
			src:  "Dim x = 1\nDim y = 2",
			want: []string{"Dim x = 1", "Dim y = 2"},
		},

		// --- attributes ---
		{
			name: "attributes/positive-own-line",
			rule: "a line that is nothing but attributes joins the declaration below it",
			src:  "<Serializable()>\nPublic Class C",
			want: []string{"<Serializable()> Public Class C"},
		},
		{
			name: "attributes/negative-comparison",
			rule: "a line ending in '>' is a comparison, not an open attribute",
			src:  "Dim b = a > c\nDim y = 2",
			want: []string{"Dim b = a > c", "Dim y = 2"},
		},

		// --- structural ---
		{
			name: "blank-and-comment-only-lines",
			rule: "blank and comment-only lines produce no logical lines",
			src:  "Dim x = 1\n\n' just a comment\n\nDim y = 2",
			want: []string{"Dim x = 1", "Dim y = 2"},
		},
		{
			name: "crlf",
			rule: "CRLF input joins the same as LF",
			src:  "Dim x = 1 _\r\n+ 2\r\n",
			want: []string{"Dim x = 1 + 2"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := texts(JoinContinuations(tc.src))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("rule %q:\n got %q\nwant %q", tc.rule, got, tc.want)
			}
		})
	}
}

// TestImplicitRuleCoverage drives the recorded coverage table through the
// joiner in both directions.
//
// It exists so that the cost of the narrow implicit-continuation choice is a
// fact the suite enforces rather than a claim in a comment: every Honoured
// rule must join, and every unhonoured rule must NOT — so implementing one
// forces the table to be updated, and the gap can never quietly change size.
func TestImplicitRuleCoverage(t *testing.T) {
	honoured := 0
	for _, rule := range ImplicitRuleCoverage {
		rule := rule
		t.Run(rule.Name, func(t *testing.T) {
			got := JoinContinuations(rule.Sample)
			joined := len(got) == 1
			if joined != rule.Honoured {
				t.Errorf("sample %q joined=%v, table says Honoured=%v (why: %s); got %q",
					rule.Sample, joined, rule.Honoured, rule.Why, texts(got))
			}
			if !rule.Honoured && rule.Why == "" {
				t.Errorf("rule %q is not honoured but records no reason", rule.Name)
			}
		})
		if rule.Honoured {
			honoured++
		}
	}
	// Pin the measured figure quoted in the package documentation so the two
	// cannot drift apart.
	if honoured != 16 || len(ImplicitRuleCoverage) != 24 {
		t.Errorf("coverage is %d/%d; the doc comment on ImplicitRuleCoverage says 16/24",
			honoured, len(ImplicitRuleCoverage))
	}
}

func TestLogicalLineMetadata(t *testing.T) {
	src := strings.Join([]string{
		"''' <summary>Saves the form.</summary>",
		"''' <param name=\"force\">Force it.</param>",
		"<Obsolete(\"use SaveAsync\")>",
		"Public Sub Save(force As Boolean) ' entry point",
		"End Sub",
	}, "\n")

	got := JoinContinuations(src)
	if len(got) != 2 {
		t.Fatalf("want 2 logical lines, got %d: %q", len(got), texts(got))
	}
	ll := got[0]
	if want := `<Obsolete("use SaveAsync")> Public Sub Save(force As Boolean)`; ll.Text != want {
		t.Errorf("Text = %q, want %q", ll.Text, want)
	}
	if want := "Public Sub Save(force As Boolean)"; ll.Code != want {
		t.Errorf("Code = %q, want %q", ll.Code, want)
	}
	if want := []string{`Obsolete("use SaveAsync")`}; !reflect.DeepEqual(ll.Attributes, want) {
		t.Errorf("Attributes = %q, want %q", ll.Attributes, want)
	}
	wantDoc := []string{"<summary>Saves the form.</summary>", `<param name="force">Force it.</param>`}
	if !reflect.DeepEqual(ll.Doc, wantDoc) {
		t.Errorf("Doc = %q, want %q", ll.Doc, wantDoc)
	}
	if !reflect.DeepEqual(ll.Comments, []string{"entry point"}) {
		t.Errorf("Comments = %q, want [entry point]", ll.Comments)
	}
	if ll.Line != 3 || ll.EndLine != 4 {
		t.Errorf("Line..EndLine = %d..%d, want 3..4", ll.Line, ll.EndLine)
	}
	// A ''' run must not leak onto a later, unrelated declaration.
	if len(got[1].Doc) != 0 {
		t.Errorf("End Sub inherited a docstring: %q", got[1].Doc)
	}
}
