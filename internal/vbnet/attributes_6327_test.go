package vbnet

import (
	"reflect"
	"testing"
)

// #6327 S3 — attribute stripping.
//
// '<' is overloaded in VB.NET exactly as '(' is: it opens an attribute list
// and it is the less-than operator. The negatives below are the rules that
// keep `If a < b Then` from being eaten as an attribute.
//
// UNVERIFIED against real VB.NET source; see the package doc.

func TestSplitAttributes(t *testing.T) {
	cases := []struct {
		name  string
		rule  string
		code  string
		attrs []string
		rest  string
	}{
		{
			name: "leading/positive", rule: "an attribute may precede a declaration on the same logical line",
			code:  `<Serializable()> Public Class X`,
			attrs: []string{`Serializable()`}, rest: `Public Class X`,
		},
		{
			name: "leading/positive-repeated", rule: "several attribute groups may precede a declaration",
			code:  `<A> <B> Public Class X`,
			attrs: []string{`A`, `B`}, rest: `Public Class X`,
		},
		{
			name: "leading/positive-nested-parens", rule: "attribute bodies nest parentheses",
			code:  `<Attr(GetType(List(Of Integer)))> Public Class X`,
			attrs: []string{`Attr(GetType(List(Of Integer)))`}, rest: `Public Class X`,
		},
		{
			name: "leading/positive-angle-in-literal", rule: "a '>' inside a literal does not close the attribute",
			code:  `<A("a>b")> Public Class X`,
			attrs: []string{`A("a>b")`}, rest: `Public Class X`,
		},
		{
			name:  "leading/positive-angle-inside-parens",
			rule:  "a '>' inside the attribute's own parentheses does not close it",
			code:  `<MyAttr(2 > 1)> Public Class X`,
			attrs: []string{`MyAttr(2 > 1)`}, rest: `Public Class X`,
		},
		{
			name:  "leading/positive-angle-inside-nested-parens",
			rule:  "paren depth is tracked, not merely non-zero",
			code:  `<MyAttr(F(a > b), 2)> Public Class X`,
			attrs: []string{`MyAttr(F(a > b), 2)`}, rest: `Public Class X`,
		},
		{
			name: "leading/positive-assembly-level", rule: "a file-level attribute leaves nothing behind",
			code:  `<Assembly: AssemblyTitle("Acme")>`,
			attrs: []string{`Assembly: AssemblyTitle("Acme")`}, rest: ``,
		},
		{
			name: "inline/positive-after-paren", rule: "a parameter attribute directly after '(' is stripped",
			code:  `Sub F(<Out> ByRef n As Integer)`,
			attrs: []string{`Out`}, rest: `Sub F( ByRef n As Integer)`,
		},
		{
			name: "inline/positive-after-comma", rule: "a parameter attribute directly after ',' is stripped",
			code:  `Sub F(a As Integer, <Out> ByRef n As Integer)`,
			attrs: []string{`Out`}, rest: `Sub F(a As Integer, ByRef n As Integer)`,
		},

		// --- negatives: '<' as the less-than operator ---
		{
			name: "negative/comparison", rule: "'a < b' is a comparison, not an attribute",
			code:  `If a < b Then Foo(c)`,
			attrs: nil, rest: `If a < b Then Foo(c)`,
		},
		{
			name: "negative/comparison-in-argument", rule: "a comparison inside an argument list is not an attribute",
			code:  `Dim r = f(a, b < c)`,
			attrs: nil, rest: `Dim r = f(a, b < c)`,
		},
		{
			name: "negative/generic-of-clause", rule: "an (Of ...) list is not an attribute",
			code:  `Public Class Box(Of T)`,
			attrs: nil, rest: `Public Class Box(Of T)`,
		},
		{
			name: "negative/unterminated", rule: "an unterminated '<' is left alone rather than eating the line",
			code:  `Dim b = a < `,
			attrs: nil, rest: `Dim b = a <`,
		},
		{
			name: "negative/none", rule: "a plain declaration is returned unchanged",
			code:  `Public Class X`,
			attrs: nil, rest: `Public Class X`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			attrs, rest := SplitAttributes(tc.code)
			if !reflect.DeepEqual(attrs, tc.attrs) {
				t.Errorf("rule %q: attrs = %q, want %q", tc.rule, attrs, tc.attrs)
			}
			if rest != tc.rest {
				t.Errorf("rule %q: rest = %q, want %q", tc.rule, rest, tc.rest)
			}
		})
	}
}
