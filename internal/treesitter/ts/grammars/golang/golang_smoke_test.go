package golang

import (
	"testing"

	"github.com/cajasmota/grafel/internal/treesitter/ts/official"
)

// TestGoSmokeParse is the ABI guard for the official Go grammar. A grammar whose
// LANGUAGE_VERSION outruns the runtime compiles but SIGSEGVs at RootNode (ADR
// 0023 §6: runtime v0.24.0 + tree-sitter-go v0.25.0). This test parses trivial
// Go source through the official adapter and asserts a sane, non-error root —
// so an ABI-incompatible bump fails CI here instead of crashing production.
func TestGoSmokeParse(t *testing.T) {
	adapter := official.New()
	parser, err := adapter.NewParser(Language())
	if err != nil {
		t.Fatalf("NewParser failed (ABI mismatch?): %v", err)
	}
	defer parser.Close()

	src := []byte("package p\nfunc F() int { return 1 }\n")
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
	if got := root.Type(); got != "source_file" {
		t.Fatalf("root kind = %q, want source_file", got)
	}
	if root.IsError() {
		t.Fatal("root is an ERROR node")
	}
	if root.ChildCount() != 2 {
		t.Fatalf("root child count = %d, want 2 (package_clause + function_declaration)", root.ChildCount())
	}
	// Spot-check positions and field access through the façade.
	fn := root.Child(1)
	if fn == nil || fn.Type() != "function_declaration" {
		t.Fatalf("child(1) = %v, want function_declaration", fn)
	}
	name := fn.ChildByFieldName("name")
	if name == nil {
		t.Fatal("function_declaration has no name field")
	}
	if got := string(src[name.StartByte():name.EndByte()]); got != "F" {
		t.Fatalf("function name = %q, want F", got)
	}
	if fn.StartPoint().Row != 1 {
		t.Fatalf("function start row = %d, want 1", fn.StartPoint().Row)
	}
}
