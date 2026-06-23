package typescript

import (
	"testing"

	"github.com/cajasmota/grafel/internal/treesitter/ts/official"
)

// TestTypeScriptSmokeParse is the ABI guard for the official TypeScript grammar
// (ADR 0023 §6): an ABI-incompatible bump compiles but SIGSEGVs at RootNode, so
// this catches it at CI instead of in production.
func TestTypeScriptSmokeParse(t *testing.T) {
	adapter := official.New()
	parser, err := adapter.NewParser(Language())
	if err != nil {
		t.Fatalf("NewParser failed (ABI mismatch?): %v", err)
	}
	defer parser.Close()

	src := []byte("function f(x: number): number { return x; }\n")
	tree, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if tree == nil {
		t.Fatal("Parse returned nil tree")
	}
	defer tree.Close()

	root := tree.RootNode()
	if root == nil {
		t.Fatal("RootNode is nil (ABI mismatch crashes here in the bad pairing)")
	}
	if got := root.Type(); got != "program" {
		t.Fatalf("root kind = %q, want program", got)
	}
	if root.IsError() {
		t.Fatal("root is an ERROR node")
	}
	if root.ChildCount() == 0 {
		t.Fatal("root has no children (expected a function_declaration)")
	}
}

// TestTSXSmokeParse is the ABI guard for the official TSX grammar — the same
// module's JSX-enabled superset that handles .tsx/.jsx. It must load and parse a
// JSX expression without an ERROR root.
func TestTSXSmokeParse(t *testing.T) {
	adapter := official.New()
	parser, err := adapter.NewParser(LanguageTSX())
	if err != nil {
		t.Fatalf("NewParser failed (ABI mismatch?): %v", err)
	}
	defer parser.Close()

	src := []byte("const e = <div className=\"x\">hi</div>;\n")
	tree, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if tree == nil {
		t.Fatal("Parse returned nil tree")
	}
	defer tree.Close()

	root := tree.RootNode()
	if root == nil {
		t.Fatal("RootNode is nil (ABI mismatch crashes here in the bad pairing)")
	}
	if got := root.Type(); got != "program" {
		t.Fatalf("root kind = %q, want program", got)
	}
	if root.IsError() {
		t.Fatal("root is an ERROR node")
	}
	if root.ChildCount() == 0 {
		t.Fatal("root has no children (expected a lexical_declaration with JSX)")
	}
}
