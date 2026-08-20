package treesitter

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
)

// #6360 — the 10% syntax-error gate is a whole-file average, so a file with one
// localised typo sails under it and is accepted IN FULL, including the broken
// part. Direction 1 of the issue: make the ERROR subtrees invisible to
// extractors so the output is a true subset of reality rather than a wrong
// description of it.
//
// These tests assert the shared-parser contract in BOTH directions:
//
//   - a file with a localised ERROR must expose only the clean part, and
//   - a file with no ERROR at all must expose exactly what it did before
//     (so a change that simply drops nodes everywhere fails here).

// visitAll walks every node reachable through the ts.Node accessors an
// extractor uses (Child) and returns one descriptor per node.
func visitAll(t *testing.T, n ts.Node, src []byte) []string {
	t.Helper()
	if n == nil {
		return nil
	}
	out := []string{fmt.Sprintf("%s@%d-%d", n.Type(), n.StartByte(), n.EndByte())}
	for i := range int(n.ChildCount()) {
		c := n.Child(i)
		if c == nil {
			t.Fatalf("Child(%d) returned nil below ChildCount()=%d on %s", i, n.ChildCount(), n.Type())
		}
		out = append(out, visitAll(t, c, src)...)
	}
	return out
}

// visitNamed does the same through the named-child accessors.
func visitNamed(t *testing.T, n ts.Node) []string {
	t.Helper()
	if n == nil {
		return nil
	}
	out := []string{n.Type()}
	for i := range int(n.NamedChildCount()) {
		c := n.NamedChild(i)
		if c == nil {
			t.Fatalf("NamedChild(%d) returned nil below NamedChildCount()=%d on %s", i, n.NamedChildCount(), n.Type())
		}
		out = append(out, visitNamed(t, c)...)
	}
	return out
}

func parseOK(t *testing.T, lang, src string) *ParseResult {
	t.Helper()
	res, err := NewParserFactory(nil).Parse(context.Background(), []byte(src), lang)
	if err != nil {
		t.Fatalf("parse %s: %v", lang, err)
	}
	if res.TSTree == nil {
		t.Fatalf("parse %s: nil tree", lang)
	}
	t.Cleanup(res.TSTree.Close)
	return res
}

// protoSrc builds the issue's shape: a proto3 file with several clean messages
// and exactly one malformed one, whose broken statement is `broken`.
func protoSrc(broken string) string {
	var b strings.Builder
	b.WriteString("syntax = \"proto3\";\npackage p;\n")
	b.WriteString("message Clean {\n  string a = 1;\n  string b = 2;\n}\n")
	fmt.Fprintf(&b, "message M0 {\n  string a = 1;\n  %s\n}\n", broken)
	b.WriteString("message After {\n  string z = 1;\n}\n")
	return b.String()
}

// TestErrorSubtree6360_LocalisedErrorHidesOnlyItsSubtree is the failing
// direction: today every node under the ERROR node is reachable by an extractor
// traversal, so the extractor sees text it could not parse.
func TestErrorSubtree6360_LocalisedErrorHidesOnlyItsSubtree(t *testing.T) {
	variants := map[string]string{
		"missing_equals":    "string b 2;",
		"missing_fieldname": "string = 2;",
		"missing_semicolon": "string b = 2",
	}
	for name, broken := range variants {
		t.Run(name, func(t *testing.T) {
			src := protoSrc(broken)
			res := parseOK(t, "proto", src)

			if res.ErrorRatio > maxErrorRatio {
				t.Fatalf("fixture is meant to sail UNDER the gate; ratio=%v", res.ErrorRatio)
			}

			nodes := visitAll(t, res.TSTree.RootNode(), []byte(src))
			for _, d := range nodes {
				if strings.HasPrefix(d, "ERROR@") {
					t.Errorf("ERROR node still reachable by extractor traversal: %s", d)
				}
			}
			// The named-child accessors must agree with the child accessors.
			for _, ty := range visitNamed(t, res.TSTree.RootNode()) {
				if ty == "ERROR" {
					t.Errorf("ERROR node still reachable via NamedChild traversal")
				}
			}

			// The clean part around the typo must survive untouched.
			for _, want := range []string{"message@", "field@", "message_name@"} {
				found := false
				for _, d := range nodes {
					if strings.HasPrefix(d, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("clean %s nodes were dropped too", want)
				}
			}
			// Three messages must still be visible: Clean, M0, After.
			msgs := 0
			for _, d := range nodes {
				if strings.HasPrefix(d, "message_name@") {
					msgs++
				}
			}
			if msgs != 3 {
				t.Errorf("message_name count = %d, want 3 (the clean part must survive)", msgs)
			}
		})
	}
}

// TestErrorSubtree6360_CleanFileUnchanged is the opposite direction: a file with
// no ERROR node at all must expose exactly the same node inventory as the raw
// tree. A blanket "drop nodes" change fails here.
func TestErrorSubtree6360_CleanFileUnchanged(t *testing.T) {
	src := protoSrc("string b = 2;")
	res := parseOK(t, "proto", src)
	if res.ErrorRatio != 0 {
		t.Fatalf("control fixture must be clean; ratio=%v", res.ErrorRatio)
	}
	nodes := visitAll(t, res.TSTree.RootNode(), []byte(src))
	if len(nodes) != res.NodeCount {
		t.Errorf("reachable nodes = %d, want %d (every node of a clean tree must stay reachable)",
			len(nodes), res.NodeCount)
	}
}

// TestErrorSubtree6360_CrossLanguage bounds the blast radius: the gate is shared
// by every tree-sitter grammar, so the same contract is asserted for a
// non-protobuf language with a localised syntax slip.
func TestErrorSubtree6360_CrossLanguage(t *testing.T) {
	cases := []struct {
		lang  string
		src   string
		clean string // a token from the clean part that must still be reachable
	}{
		{
			lang: "go",
			src: "package m\n\nfunc Good() int { return 1 }\n\nfunc Bad() int { return ) }\n\n" +
				"func AlsoGood() int { return 2 }\n",
			clean: "function_declaration",
		},
		{
			lang:  "python",
			src:   "def good():\n    return 1\n\ndef bad(:\n    return 2\n\ndef also_good():\n    return 3\n",
			clean: "function_definition",
		},
	}
	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			res, err := NewParserFactory(nil).Parse(context.Background(), []byte(tc.src), tc.lang)
			if err != nil {
				t.Skipf("%s fixture tripped the whole-file gate (%v); not the case under test", tc.lang, err)
			}
			t.Cleanup(res.TSTree.Close)
			if res.ErrorRatio == 0 {
				t.Skipf("%s fixture produced no ERROR node; not the case under test", tc.lang)
			}
			nodes := visitAll(t, res.TSTree.RootNode(), []byte(tc.src))
			for _, d := range nodes {
				if strings.HasPrefix(d, "ERROR@") {
					t.Errorf("%s: ERROR node still reachable: %s", tc.lang, d)
				}
			}
			found := false
			for _, d := range nodes {
				if strings.HasPrefix(d, tc.clean+"@") {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s: clean %s nodes were dropped", tc.lang, tc.clean)
			}
		})
	}
}

// TestErrorSubtree6360_AccessorsStayConsistent sweeps a real tree and pins,
// for every reachable node: Child never returns an ERROR, a non-empty
// FieldNameForChild always resolves through ChildByFieldName to a non-ERROR
// node, and Parent/PrevSibling never step into the hidden region.
//
// What it does NOT pin: the visible->underlying index remap in
// FieldNameForChild. This sweep only ever reaches nodes that kept all their
// children, so on every node it visits the two indices coincide and dropping
// the remap changes nothing. That case is pinned separately, and deliberately,
// by TestErrorSubtree6360_FieldNameRemapsToVisibleIndex below.
func TestErrorSubtree6360_AccessorsStayConsistent(t *testing.T) {
	src := "package m\n\nfunc Good() int { return 1 }\n\nfunc Bad() int { return ) }\n"
	res, err := NewParserFactory(nil).Parse(context.Background(), []byte(src), "go")
	if err != nil {
		t.Skipf("fixture tripped the whole-file gate: %v", err)
	}
	t.Cleanup(res.TSTree.Close)

	var check func(n ts.Node)
	check = func(n ts.Node) {
		for i := range int(n.ChildCount()) {
			c := n.Child(i)
			if c.IsError() {
				t.Fatalf("Child(%d) of %s is an ERROR node", i, n.Type())
			}
			if fname := n.FieldNameForChild(i); fname != "" {
				byField := n.ChildByFieldName(fname)
				if byField == nil {
					t.Errorf("%s: FieldNameForChild(%d)=%q but ChildByFieldName(%q) is nil",
						n.Type(), i, fname, fname)
				} else if byField.IsError() {
					t.Errorf("%s: ChildByFieldName(%q) handed back an ERROR node", n.Type(), fname)
				}
			}
			if p := c.Parent(); p != nil && p.IsError() {
				t.Errorf("%s: Parent() of a visible node is an ERROR node", c.Type())
			}
			if s := c.PrevSibling(); s != nil && s.IsError() {
				t.Errorf("%s: PrevSibling() is an ERROR node", c.Type())
			}
			check(c)
		}
	}
	check(res.TSTree.RootNode())
}

// TestErrorSubtree6360_FieldNameRemapsToVisibleIndex pins the subtlest line in
// the decorator: FieldNameForChild takes an index into the FILTERED child list
// and must answer it against the UNDERLYING one.
//
// The sweep above cannot fail if that remap is dropped, because it never
// reaches a node that has BOTH lost a child to filtering AND carries grammar
// field names. This test picks exactly that shape out of two grammars:
//
//	python class_definition:        [class, identifier/name, :, ERROR, block/body]
//	javascript variable_declarator: [identifier/name, =, ERROR, unary_expression/value]
//
// In both, the ERROR sits BEFORE a field-bearing child, so that child's visible
// index is one less than its underlying index. Answering the visible index
// against the underlying tree lands on the hidden ERROR — which has no field
// name — and the field silently becomes "". That is the failure mode pinned
// here: a mis-pairing that returns a wrong answer rather than raising one.
func TestErrorSubtree6360_FieldNameRemapsToVisibleIndex(t *testing.T) {
	cases := []struct {
		name      string
		lang      string
		src       string
		nodeType  string // the node that lost a child to filtering
		visIdx    int    // index into the FILTERED child list
		wantField string // grammar field of that visible child
		wantType  string // node type of that visible child
	}{
		{
			name:      "python_class_body",
			lang:      "python",
			src:       "class C:\n    )\n    def m(self):\n        return 1\n",
			nodeType:  "class_definition",
			visIdx:    3,
			wantField: "body",
			wantType:  "block",
		},
		{
			name:      "javascript_declarator_value",
			lang:      "javascript",
			src:       "function g(){return 1}\nconst x = ) + 1;\nfunction h(){return 2}\n",
			nodeType:  "variable_declarator",
			visIdx:    2,
			wantField: "value",
			wantType:  "unary_expression",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := NewParserFactory(nil).Parse(context.Background(), []byte(tc.src), tc.lang)
			if err != nil {
				t.Fatalf("fixture must sail UNDER the whole-file gate: %v", err)
			}
			t.Cleanup(res.TSTree.Close)

			target := findNodeOfType(res.TSTree.RootNode(), tc.nodeType)
			if target == nil {
				t.Fatalf("fixture no longer produces a %s node", tc.nodeType)
			}

			// Guard against the test going vacuous: the node must actually have
			// LOST a child, otherwise visible and underlying indices coincide
			// and the remap is not exercised at all.
			if int(target.ChildCount()) != tc.visIdx+1 {
				t.Fatalf("%s has %d visible children, want %d: the fixture no longer puts "+
					"an ERROR before the field-bearing child, so the remap is untested",
					tc.nodeType, target.ChildCount(), tc.visIdx+1)
			}

			child := target.Child(tc.visIdx)
			if child == nil {
				t.Fatalf("%s: Child(%d) is nil", tc.nodeType, tc.visIdx)
			}
			if child.Type() != tc.wantType {
				t.Fatalf("%s: visible Child(%d) type = %q, want %q",
					tc.nodeType, tc.visIdx, child.Type(), tc.wantType)
			}

			// The killing assertion. Without the visible->underlying remap this
			// index lands on the hidden ERROR and returns "".
			if got := target.FieldNameForChild(tc.visIdx); got != tc.wantField {
				t.Errorf("%s: FieldNameForChild(%d) = %q, want %q — field names are "+
					"mis-paired with the visible child list",
					tc.nodeType, tc.visIdx, got, tc.wantField)
			}

			// And the two accessors must agree on the same node.
			byField := target.ChildByFieldName(tc.wantField)
			if byField == nil {
				t.Fatalf("%s: ChildByFieldName(%q) is nil", tc.nodeType, tc.wantField)
			}
			if !ts.SameNode(byField, child) {
				t.Errorf("%s: ChildByFieldName(%q) and Child(%d) disagree: %s@%d-%d vs %s@%d-%d",
					tc.nodeType, tc.wantField, tc.visIdx,
					byField.Type(), byField.StartByte(), byField.EndByte(),
					child.Type(), child.StartByte(), child.EndByte())
			}
		})
	}
}

// findNodeOfType returns the first node of the given type in a pre-order walk.
func findNodeOfType(n ts.Node, want string) ts.Node {
	if n == nil {
		return nil
	}
	if n.Type() == want {
		return n
	}
	for i := range int(n.ChildCount()) {
		if hit := findNodeOfType(n.Child(i), want); hit != nil {
			return hit
		}
	}
	return nil
}
