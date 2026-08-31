package treesitter

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tskotlin "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/kotlin"
	tsofficial "github.com/cajasmota/grafel/internal/treesitter/ts/official"
)

// Tests for the Kotlin multi-annotation misparse repair (#6736).
// See kotlin_annot_repair.go for the measured mechanism.

const kotlinCtrl6736 = `@RestController
@RequestMapping("/api")
class RealController {

    @GetMapping("/kept")
    fun kept(): String = "ok"
}
`

// rawParseKotlin6736 parses WITHOUT the factory, so without the repair and
// without the #6360 error-skipping wrapper. Every claim about "what the grammar
// actually does" in these tests is measured through here.
func rawParseKotlin6736(t *testing.T, src string) (ts.Tree, func()) {
	t.Helper()
	p, err := tsofficial.New().NewParser(tskotlin.Language())
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	tree, err := p.Parse([]byte(src))
	if err != nil {
		t.Fatalf("raw parse: %v", err)
	}
	return tree, func() { tree.Close(); p.Close() }
}

func topTypes6736(root ts.Node) []string {
	var out []string
	for i := 0; i < int(root.ChildCount()); i++ {
		out = append(out, root.Child(i).Type())
	}
	return out
}

// findNode6736 returns the first node of the given type in a pre-order walk.
func findNode6736(n ts.Node, typ string) ts.Node {
	if n == nil {
		return nil
	}
	if n.Type() == typ {
		return n
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		if f := findNode6736(n.Child(i), typ); f != nil {
			return f
		}
	}
	return nil
}

// TestKotlinAnnotRepair_PremiseTheGrammarMisparses pins the DEFECT, on the raw
// grammar, with no grafel layer in the way. Everything else in this file is
// only meaningful while this holds: if a future grammar bump fixes the
// ambiguity, this test fails first and says so, rather than leaving the repair
// silently guarding nothing.
func TestKotlinAnnotRepair_PremiseTheGrammarMisparses(t *testing.T) {
	src := kotlinCtrl6736 + "\nfun loose(): String = \"x\"\n"
	tree, done := rawParseKotlin6736(t, src)
	defer done()
	root := tree.RootNode()

	if _, errNodes := countNodesTS(root); errNodes != 0 {
		t.Fatalf("premise failed: the raw parse has %d ERROR node(s); #6736 is a CLEAN-parse "+
			"misparse, so the #6360 error-skipping wrapper cannot be the mechanism", errNodes)
	}
	got := topTypes6736(root)
	if got[0] != "prefix_expression" {
		t.Fatalf("premise failed: the annotated controller now parses as %q, not the misparsed "+
			"prefix_expression — the grammar may have been fixed upstream; top children: %v",
			got[0], got)
	}
	// Same source with the controller LAST parses correctly — proving the
	// trigger is position, not the controller text.
	lastTree, doneLast := rawParseKotlin6736(t, "fun loose(): String = \"x\"\n\n"+kotlinCtrl6736)
	defer doneLast()
	lastTypes := topTypes6736(lastTree.RootNode())
	if lastTypes[len(lastTypes)-1] != "class_declaration" {
		t.Fatalf("premise failed: the controller does not parse as a class_declaration even when "+
			"it is last; the fixture itself is malformed. top children: %v", lastTypes)
	}
}

// TestKotlinAnnotRepair_RecoversDeclarationShapes is the repair's own matrix. It
// deliberately varies BOTH axes the corpus held constant: the carrier kind
// (class / object / interface / fun), and what FOLLOWS the annotated
// declaration (fun / val / class / comment / nothing).
func TestKotlinAnnotRepair_RecoversDeclarationShapes(t *testing.T) {
	annots := "@RestController\n@RequestMapping(\"/api\")\n"
	tail := "\nfun loose(): String = \"x\"\n"

	cases := []struct {
		name string
		src  string
		want string // a top-level child type that must be present after repair
	}{
		{"class then fun", annots + "class A {\n  fun k() {}\n}\n" + tail, "class_declaration"},
		{"class then val", annots + "class A {\n  fun k() {}\n}\n\nval v = 1\n", "class_declaration"},
		{"class then class", annots + "class A {\n  fun k() {}\n}\n\nclass B {}\n", "class_declaration"},
		{"class then comment", annots + "class A {\n  fun k() {}\n}\n// t\n", "class_declaration"},
		{"object then fun", annots + "object A {\n  fun k() {}\n}\n" + tail, "object_declaration"},
		{"open class then fun", annots + "open class A {\n  fun k() {}\n}\n" + tail, "class_declaration"},
		{"ctor class then fun", annots + "class A(val x: Int) {\n  fun k() {}\n}\n" + tail, "class_declaration"},
		{"fun then fun", "@Bean\n@Qualifier(\"q\")\nfun a(): String = \"x\"\n" + tail, "function_declaration"},
		{"three annots then fun", "@A\n@B\n@C\nclass A {\n  fun k() {}\n}\n" + tail, "class_declaration"},
		{"class LAST (already fine)", annots + "class A {\n  fun k() {}\n}\n", "class_declaration"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pr, err := NewParserFactory(nil).Parse(context.Background(), []byte(tc.src), "kotlin")
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			defer pr.TSTree.Close()
			got := topTypes6736(pr.TSTree.RootNode())
			found := false
			for _, g := range got {
				if g == tc.want {
					found = true
				}
			}
			if !found {
				t.Fatalf("no top-level %s after repair; top children: %v", tc.want, got)
			}
			// The annotations must be back ON the declaration, not stranded in a
			// leftover prefix_expression: a recovered declaration with no
			// modifiers is useless to every annotation-driven pass.
			for _, g := range got {
				if g == "prefix_expression" {
					t.Fatalf("a top-level prefix_expression survived the repair; top children: %v", got)
				}
			}
			decl := findNode6736(pr.TSTree.RootNode(), tc.want)
			if findNode6736(decl, "modifiers") == nil {
				t.Fatalf("the recovered %s carries no modifiers — its annotations were lost", tc.want)
			}
		})
	}
}

// TestKotlinAnnotRepair_OffsetsAddressTheOriginalSource is the load-bearing
// correctness property of the repair: every consumer slices ITS OWN unmodified
// buffer with the offsets this tree reports, so an off-by-one anywhere after an
// inserted terminator would silently corrupt every name in the file.
func TestKotlinAnnotRepair_OffsetsAddressTheOriginalSource(t *testing.T) {
	src := "package io.demo\n\n" + kotlinCtrl6736 +
		"\nfun looseHelper(): String = \"helper\"\n\nval looseConst: String = \"const\"\n\n" +
		"class SecondClass {\n    fun unrelated() {}\n}\n"

	pr, err := NewParserFactory(nil).Parse(context.Background(), []byte(src), "kotlin")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	defer pr.TSTree.Close()

	// Every identifier in the tree must slice out of the ORIGINAL source to a
	// plausible identifier — one byte of drift turns `RealController` into
	// `RealControlle` or `ealController`.
	want := map[string]bool{
		"RealController": false, "kept": false,
		"looseHelper": false, "looseConst": false,
		"SecondClass": false, "unrelated": false,
	}
	var walk func(n ts.Node)
	walk = func(n ts.Node) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "simple_identifier", "type_identifier":
			s, e := int(n.StartByte()), int(n.EndByte())
			if s < 0 || e > len(src) || s > e {
				t.Fatalf("node %s has out-of-range span [%d,%d) for a %d-byte source",
					n.Type(), s, e, len(src))
			}
			txt := src[s:e]
			if strings.ContainsAny(txt, " \t\n;@(){}\"") {
				t.Fatalf("identifier at [%d,%d) slices the original source to %q — the repair's "+
					"offset mapping is wrong", s, e, txt)
			}
			if _, ok := want[txt]; ok {
				want[txt] = true
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i))
		}
	}
	walk(pr.TSTree.RootNode())
	for k, seen := range want {
		if !seen {
			t.Errorf("identifier %q was never sliced out of the original source at a correct offset", k)
		}
	}

	// The last node in the file must not run past the buffer, which is where a
	// cumulative shift error would surface.
	root := pr.TSTree.RootNode()
	if int(root.EndByte()) > len(src) {
		t.Errorf("root EndByte=%d exceeds the original source length %d", root.EndByte(), len(src))
	}
}

// TestKotlinAnnotRepair_CleanFileIsUntouched pins the no-op guarantee: a Kotlin
// file without the misparse signature is never re-parsed and never wrapped, so
// it is byte-for-byte the same tree the grammar produced.
func TestKotlinAnnotRepair_CleanFileIsUntouched(t *testing.T) {
	for _, src := range []string{
		// No annotations at all.
		"package p\n\nclass A {\n  fun k() {}\n}\n\nfun loose() {}\n",
		// ONE annotation — below the two-annotation trigger.
		"package p\n\n@RestController\nclass A {\n  fun k() {}\n}\n\nfun loose() {}\n",
		// Two annotations, declaration already last.
		"package p\n\n" + kotlinCtrl6736,
	} {
		raw, done := rawParseKotlin6736(t, src)
		wantSexp := raw.RootNode().String()
		done()

		pr, err := NewParserFactory(nil).Parse(context.Background(), []byte(src), "kotlin")
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		gotSexp := pr.TSTree.RootNode().String()
		gotEnd := pr.TSTree.RootNode().EndByte()
		pr.TSTree.Close()

		if gotSexp != wantSexp {
			t.Errorf("clean file was altered by the repair\n got: %s\nwant: %s", gotSexp, wantSexp)
		}
		if int(gotEnd) != len(src) {
			t.Errorf("clean file root EndByte=%d, want %d", gotEnd, len(src))
		}
	}
}

// TestKotlinAnnotRepair_ErrorSubtreesAreStillSkipped pins that the #6360
// error-skipping wrapper still hides ERROR subtrees, including on a Kotlin file
// that ALSO trips the repair — the repair runs first and must compose with it,
// not replace it.
func TestKotlinAnnotRepair_ErrorSubtreesAreStillSkipped(t *testing.T) {
	// A genuinely malformed tail, after a misparsing annotated controller.
	src := "package p\n\n" + kotlinCtrl6736 + "\nfun broken( { ) = = =\n\nval ok = 1\n"

	raw, done := rawParseKotlin6736(t, src)
	_, rawErrs := countNodesTS(raw.RootNode())
	done()
	if rawErrs == 0 {
		t.Fatalf("control failed: this fixture is supposed to contain a genuine syntax error, " +
			"but the raw parse has zero ERROR nodes — the test would prove nothing")
	}

	pr, err := NewParserFactory(nil).Parse(context.Background(), []byte(src), "kotlin")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	defer pr.TSTree.Close()

	if findNode6736(pr.TSTree.RootNode(), "ERROR") != nil {
		t.Errorf("an ERROR node is reachable through the returned tree — the #6360 wrapper is " +
			"no longer applied on the Kotlin repair path")
	}
	var reachableErr func(n ts.Node) bool
	reachableErr = func(n ts.Node) bool {
		if n == nil {
			return false
		}
		if n.IsError() {
			return true
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			if reachableErr(n.Child(i)) {
				return true
			}
		}
		return false
	}
	if reachableErr(pr.TSTree.RootNode()) {
		t.Errorf("IsError() is true somewhere in the returned tree; ERROR subtrees are no longer skipped")
	}
}

// TestKotlinAnnotRepair_OtherLanguagesAreNotTouched pins the blast radius: the
// repair is reachable only for Kotlin. A Java file with the same annotation
// shape must be handed back exactly as the grammar parsed it.
func TestKotlinAnnotRepair_OtherLanguagesAreNotTouched(t *testing.T) {
	src := `package p;

@RestController
@RequestMapping("/api")
public class RealController {
    @GetMapping("/kept")
    public String kept() { return "ok"; }
}

class Second {}
`
	pr, err := NewParserFactory(nil).Parse(context.Background(), []byte(src), "java")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	defer pr.TSTree.Close()
	root := pr.TSTree.RootNode()
	if int(root.EndByte()) != len(src) {
		t.Errorf("java root EndByte=%d, want %d — a non-Kotlin tree was rewritten",
			root.EndByte(), len(src))
	}
	if findNode6736(root, "class_declaration") == nil {
		t.Errorf("java class_declaration missing; top children: %v", topTypes6736(root))
	}
}

// TestKotlinMisparsePoints_DetachedVsSwallowed pins the structural discriminator
// directly, because getting it wrong is silent: an annotations-only
// prefix_expression means the carrier declaration is the NEXT sibling, whereas a
// prefix_expression that swallowed its declaration is its own carrier. Deciding
// this by byte proximity instead makes the repair terminate the wrong
// declaration, which is exactly the bug the first draft of this fix had.
func TestKotlinMisparsePoints_DetachedVsSwallowed(t *testing.T) {
	// SWALLOWED: `class` is inside the prefix_expression.
	swallowed := "@A\n@B\nclass C {\n  fun k() {}\n}\n\nfun loose() {}\n"
	tree, done := rawParseKotlin6736(t, swallowed)
	root := tree.RootNode()
	pts := kotlinMisparsePoints(root)
	if len(pts) != 1 {
		done()
		t.Fatalf("swallowed shape: got %d insertion points, want 1", len(pts))
	}
	// The terminator must land on the CLASS's closing brace, not past the
	// unrelated `fun loose()` that follows it.
	if got := swallowed[pts[0]-1 : pts[0]]; got != "}" {
		done()
		t.Fatalf("swallowed shape: terminator would go after %q (offset %d), want after the "+
			"class's closing brace; source is %q", got, pts[0], swallowed)
	}
	if pts[0] >= strings.Index(swallowed, "fun loose") {
		done()
		t.Fatalf("swallowed shape: terminator at %d is past `fun loose` — the repair would "+
			"terminate the wrong declaration", pts[0])
	}
	done()

	// DETACHED: the object survives, the annotations are stranded. Note the
	// argument-carrying annotation — it is what produces this shape rather than
	// the swallowed one, and it is also why the stranded expression is not made
	// of annotation nodes end to end (the args split off as a sibling
	// parenthesized_expression).
	detached := "@A\n@B(\"/api\")\nobject C {\n  fun k() {}\n}\n\nfun loose() {}\n"
	tree2, done2 := rawParseKotlin6736(t, detached)
	defer done2()
	root2 := tree2.RootNode()
	if top := topTypes6736(root2); top[0] != "prefix_expression" || top[1] != "object_declaration" {
		t.Fatalf("detached-shape premise failed: top children are %v, want a stranded "+
			"prefix_expression followed by the surviving object_declaration", top)
	}
	pts2 := kotlinMisparsePoints(root2)
	if len(pts2) != 1 {
		t.Fatalf("detached shape: got %d insertion points, want 1", len(pts2))
	}
	// Here the terminator MUST reach past the annotations to the end of the
	// object, otherwise the annotations stay detached and nothing is fixed.
	if got := detached[pts2[0]-1 : pts2[0]]; got != "}" {
		t.Fatalf("detached shape: terminator would go after %q (offset %d), want after the "+
			"object's closing brace", got, pts2[0])
	}
	if pts2[0] <= strings.Index(detached, "object C") {
		t.Fatalf("detached shape: terminator at %d lands before the object declaration it must "+
			"terminate", pts2[0])
	}
}

// TestKotlinMisparsePoints_OnlyAnnotationLedExpressions pins the detection
// guard itself: a top-level `prefix_expression` that is NOT led by an
// `annotation` is not the #6736 misparse and must not be terminated.
//
// Dropping the annotation check would leave every test in this file green while
// making the repair rewrite arbitrary Kotlin, which is precisely the permissive
// direction the rest of the suite cannot see: the repair's other tests all feed
// it sources that DO carry the signature, so they cannot distinguish "fires on
// the misparse" from "fires on everything".
func TestKotlinMisparsePoints_OnlyAnnotationLedExpressions(t *testing.T) {
	// A Kotlin script-style top-level statement: a genuine prefix_expression at
	// the top level, with nothing wrong with it.
	src := "val flag = true\n\n!flag\n\nfun loose() {}\n"
	tree, done := rawParseKotlin6736(t, src)
	defer done()
	root := tree.RootNode()

	// Premise: this really does produce a top-level prefix_expression, otherwise
	// the assertion below is vacuous.
	sawPrefix := false
	for i := 0; i < int(root.ChildCount()); i++ {
		if root.Child(i).Type() == "prefix_expression" {
			sawPrefix = true
		}
	}
	if !sawPrefix {
		t.Fatalf("premise failed: no top-level prefix_expression in %q; top children: %v",
			src, topTypes6736(root))
	}

	if pts := kotlinMisparsePoints(root); len(pts) != 0 {
		t.Fatalf("a non-annotation-led top-level prefix_expression was treated as the #6736 "+
			"misparse (insertion points %v) — the repair would rewrite well-formed Kotlin", pts)
	}

	// And end to end: the tree the factory returns for this source is the tree
	// the grammar produced, untouched.
	want := root.String()
	pr, err := NewParserFactory(nil).Parse(context.Background(), []byte(src), "kotlin")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	defer pr.TSTree.Close()
	if got := pr.TSTree.RootNode().String(); got != want {
		t.Errorf("well-formed Kotlin with a top-level prefix_expression was rewritten\n got: %s\nwant: %s",
			got, want)
	}
}
