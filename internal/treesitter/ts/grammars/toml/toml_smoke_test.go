package toml

import (
	"testing"

	"github.com/cajasmota/grafel/internal/treesitter/ts/official"
)

// TestTomlSmokeParse is the ABI guard for the official TOML grammar (ADR 0023
// §6): an ABI-incompatible bump compiles but SIGSEGVs at RootNode, so this
// catches it at CI instead of in production.
func TestTomlSmokeParse(t *testing.T) {
	adapter := official.New()
	parser, err := adapter.NewParser(Language())
	if err != nil {
		t.Fatalf("NewParser failed (ABI mismatch?): %v", err)
	}
	defer parser.Close()

	src := []byte("[table]\nkey = \"value\"\n")
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
	if got := root.Type(); got != "document" {
		t.Fatalf("root kind = %q, want document", got)
	}
	if root.IsError() {
		t.Fatal("root is an ERROR node")
	}
	if root.ChildCount() == 0 {
		t.Fatal("root has no children (expected a table)")
	}
}
