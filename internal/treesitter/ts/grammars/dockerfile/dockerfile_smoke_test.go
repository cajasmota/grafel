package dockerfile

import (
	"testing"

	"github.com/cajasmota/grafel/internal/treesitter/ts/official"
)

// TestDockerfileSmokeParse is the ABI guard for the vendored dockerfile grammar.
// A grammar whose LANGUAGE_VERSION outruns the runtime compiles but SIGSEGVs at
// RootNode (ADR 0023 §6); the vendored parser.c is ABI 14, inside the v0.24.0
// window. This parses trivial Dockerfile source (exercising the external
// scanner) through the official adapter and asserts a sane, non-error root.
func TestDockerfileSmokeParse(t *testing.T) {
	adapter := official.New()
	parser, err := adapter.NewParser(Language())
	if err != nil {
		t.Fatalf("NewParser failed (ABI mismatch?): %v", err)
	}
	defer parser.Close()

	src := []byte("FROM alpine:3\nRUN echo hi\n")
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
}
