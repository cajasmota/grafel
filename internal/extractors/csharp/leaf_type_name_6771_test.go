// Tests for issue #6771 — leafTypeName returned the NAMESPACE ROOT for every
// qualified C# type, and its last-resort branch minted punctuation as a type.
//
// DEFECT 1. The `qualified_name` branch flattened the node with findAllNodes
// and took `ids[len(ids)-1]`, under a comment claiming that is "the rightmost
// identifier". findAllNodes is a LIFO stack walk that pushes children
// left→right and pops from the end, so it visits the RIGHTMOST child FIRST:
// on `A.B.C` — nested by the grammar as qualified_name(qualified_name(A,B),C)
// — the accumulator is [C, B, A] and the last element is `A`. Every
// fully-qualified type reference therefore resolved to its namespace root:
// `System.Text.StringBuilder` → `System`, `Microsoft.Extensions.Logging.
// ILogger` → `Microsoft`. It was wrong in a plausible-looking way — `System`
// and `Microsoft` are real identifiers — so nothing downstream rejected it.
//
// DEFECT 2. The last resort admitted any text free of `" <>[]?,"`. That is a
// BLOCKLIST, so it admitted every punctuation token nobody enumerated; `:`
// was merely the one that was found. The replacement is an ALLOW-list — the
// text must actually be C#-identifier-shaped — and the token space below is
// enumerated exhaustively over printable ASCII rather than hand-picked,
// because hand-picked attacks have twice missed holes in this repo.
//
// The unit cases here drive leafTypeName directly; the extraction cases in
// qualified_type_6771_test.go prove the fixed branch is reached by a real
// extraction and not only by a test calling the helper.
package csharp

import (
	"regexp"
	"testing"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tscsharp "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/csharp"
	tsofficial "github.com/cajasmota/grafel/internal/treesitter/ts/official"
)

// csParse6771 parses C# source for the in-package unit cases.
func csParse6771(t *testing.T, src string) ts.Tree {
	t.Helper()
	parser, err := tsofficial.New().NewParser(tscsharp.Language())
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	return tree
}

// firstNodeInOrder returns the first descendant of root with the given type in
// a deterministic PRE-ORDER, LEFT-TO-RIGHT walk. It deliberately does not use
// findAllNodes: findAllNodes' traversal order is the defect under test, so a
// test that located its own inputs with it would inherit the bug.
func firstNodeInOrder(root ts.Node, kind string) ts.Node {
	if root == nil {
		return nil
	}
	if root.Type() == kind {
		return root
	}
	for i := 0; i < int(root.ChildCount()); i++ {
		if n := firstNodeInOrder(root.Child(i), kind); n != nil {
			return n
		}
	}
	return nil
}

// fieldTypeNode returns the declared-type node of the first field declaration.
func fieldTypeNode(t *testing.T, src string) (ts.Node, []byte) {
	t.Helper()
	tree := csParse6771(t, src)
	vd := firstNodeInOrder(tree.RootNode(), "variable_declaration")
	if vd == nil {
		t.Fatalf("no variable_declaration parsed from %q", src)
	}
	typ := vd.ChildByFieldName("type")
	if typ == nil {
		t.Fatalf("field declaration has no type node in %q", src)
	}
	return typ, []byte(src)
}

// TestLeafTypeName_QualifiedTypeResolvesToItsRightmostSegment is the headline
// #6771 case. Each declaration below is a field whose declared type is
// qualified; the leaf is the LAST dotted segment, never the namespace root.
func TestLeafTypeName_QualifiedTypeResolvesToItsRightmostSegment(t *testing.T) {
	cases := []struct {
		decl string // the C# type as written
		want string
	}{
		{"System.String", "String"},
		{"System.Text.StringBuilder", "StringBuilder"},
		{"Microsoft.Extensions.Logging.ILogger", "ILogger"},
		{"A.B.C.D.E.Deepest", "Deepest"},
		// The namespace root and the leaf are the same identifier: this case
		// passes even with the defect present, so it is here to pin that the
		// fix does not overshoot, not as evidence of the fix.
		{"Same.Same", "Same"},
		// Generic, array and nullable wrappers around a qualified name: the
		// leaf is still the rightmost SEGMENT, with the wrapper stripped.
		{"A.B.Handler<int>", "Handler"},
		{"System.Text.StringBuilder[]", "StringBuilder"},
		{"System.Text.StringBuilder?", "StringBuilder"},
		// Unqualified shapes must be unaffected by the qualified-name fix.
		{"StringBuilder", "StringBuilder"},
		{"int", "int"},
		{"List<string>", "List"},
	}
	for _, tc := range cases {
		src := "class C { private " + tc.decl + " f; }"
		typ, b := fieldTypeNode(t, src)
		if got := leafTypeName(typ, b); got != tc.want {
			t.Errorf("leafTypeName(%q) = %q, want %q", tc.decl, got, tc.want)
		}
	}
}

// TestLeafTypeName_ColonTokenIsNotAType drives the exact node the last-resort
// branch used to mint `:` from: the anonymous ":" separator of a base list.
func TestLeafTypeName_ColonTokenIsNotAType(t *testing.T) {
	src := `class A : B { }`
	tree := csParse6771(t, src)
	bl := firstNodeInOrder(tree.RootNode(), "base_list")
	if bl == nil {
		t.Fatal("no base_list parsed")
	}
	var colon ts.Node
	for i := 0; i < int(bl.ChildCount()); i++ {
		if c := bl.Child(i); c != nil && c.Type() == ":" {
			colon = c
			break
		}
	}
	if colon == nil {
		t.Fatal("base_list has no \":\" child — the premise of this test is gone")
	}
	if got := leafTypeName(colon, []byte(src)); got != "" {
		t.Fatalf("the base-list \":\" token is not a type name; leafTypeName returned %q", got)
	}
}

// TestCsIdentifierText_PrintableASCIISpaceIsEnumerated brute-forces the whole
// single-character and two-character printable-ASCII token space against an
// INDEPENDENT oracle regexp for a C# identifier, rather than hand-picking
// attack tokens. Every string the guard admits must be identifier-shaped, and
// every identifier-shaped string must be admitted — both directions, so a
// guard made either too permissive or too strict fails here.
func TestCsIdentifierText_PrintableASCIISpaceIsEnumerated(t *testing.T) {
	// Independent spec: optional `@` verbatim prefix, then a letter or
	// underscore, then letters/digits/underscores.
	oracle := regexp.MustCompile(`^@?[A-Za-z_][A-Za-z0-9_]*$`)
	check := func(s string) {
		want := oracle.MatchString(s)
		if got := isCsIdentifierText(s); got != want {
			t.Errorf("isCsIdentifierText(%q) = %v, want %v", s, got, want)
		}
	}
	check("")
	for a := byte(0x20); a <= 0x7e; a++ {
		check(string([]byte{a}))
		for b := byte(0x20); b <= 0x7e; b++ {
			check(string([]byte{a, b}))
		}
	}
}

// TestCsIdentifierText_RejectsNonIdentifierShapes states the permissive
// direction in longhand: shapes a too-loose guard would admit. Our suites are
// structurally blind to over-firing, so these negatives are the only thing
// that grades a guard for being too generous.
func TestCsIdentifierText_RejectsNonIdentifierShapes(t *testing.T) {
	for _, bad := range []string{
		"", "@", "@@x", "@1a", // verbatim prefix without a valid identifier after it
		"1abc", "0", "9lives", // leading digit
		"a b", " ab", "ab ", "a\tb", "a\nb", // embedded or surrounding whitespace
		"A.B", "System.Text.StringBuilder", // dotted: a qualified name is not a leaf
		"a-b", "a+b", "a*b", "a/b", // operators
		":", "::", "=>", "(", ")", "{", "}", ";", ",", // punctuation
		"List<T>", "int[]", "int?", // type syntax, not a bare identifier
		"a$b", "a#b", "a!b", "a'b", "a\"b",
	} {
		if isCsIdentifierText(bad) {
			t.Errorf("isCsIdentifierText(%q) = true; %q is not a C# identifier", bad, bad)
		}
	}
}

// TestCsIdentifierText_AcceptsRealIdentifiers is the recall direction: the
// guard must not have been tightened into rejecting the names extraction
// depends on.
func TestCsIdentifierText_AcceptsRealIdentifiers(t *testing.T) {
	for _, ok := range []string{
		"String", "StringBuilder", "ILogger", "i", "_", "_x", "x1",
		"int", "var", "T", "A0_b", "@class", "@int",
	} {
		if !isCsIdentifierText(ok) {
			t.Errorf("isCsIdentifierText(%q) = false; %q is a valid C# identifier", ok, ok)
		}
	}
}
