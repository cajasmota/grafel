package python

import (
	"testing"

	"github.com/cajasmota/grafel/internal/treesitter/ts/official"
)

// TestPythonSmokeParse is the ABI guard for the official Python grammar. A
// grammar whose LANGUAGE_VERSION outruns the runtime compiles but SIGSEGVs at
// RootNode (ADR 0023 §6). This parses trivial Python through the official
// adapter and asserts a sane, non-error root, so an ABI-incompatible bump fails
// CI here instead of crashing production.
func TestPythonSmokeParse(t *testing.T) {
	adapter := official.New()
	parser, err := adapter.NewParser(Language())
	if err != nil {
		t.Fatalf("NewParser failed (ABI mismatch?): %v", err)
	}
	defer parser.Close()

	src := []byte("def f():\n    return 1\n")
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
	if got := root.Type(); got != "module" {
		t.Fatalf("root kind = %q, want module", got)
	}
	if root.IsError() {
		t.Fatal("root is an ERROR node")
	}
	if root.ChildCount() == 0 {
		t.Fatal("root has no children (expected a function_definition)")
	}
}
