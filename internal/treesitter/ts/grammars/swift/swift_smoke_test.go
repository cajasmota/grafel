package swift

import (
	"testing"

	"github.com/cajasmota/grafel/internal/treesitter/ts/official"
)

// TestSwiftSmokeParse is the ABI guard for the official Swift grammar (ADR 0023
// §6): an ABI-incompatible bump compiles but SIGSEGVs at RootNode, so this
// catches it at CI instead of in production.
func TestSwiftSmokeParse(t *testing.T) {
	adapter := official.New()
	parser, err := adapter.NewParser(Language())
	if err != nil {
		t.Fatalf("NewParser failed (ABI mismatch?): %v", err)
	}
	defer parser.Close()

	src := []byte("func f() -> Int { return 1 }\n")
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
	if root.ChildCount() == 0 {
		t.Fatal("root has no children (expected a function)")
	}
}
