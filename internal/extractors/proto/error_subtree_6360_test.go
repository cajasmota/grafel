package proto_test

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/proto"
	"github.com/cajasmota/grafel/internal/treesitter"
)

// #6360 — the consumer side of the shared-parser change.
//
// The other proto tests parse through the raw official adapter
// (parseForTest), which bypasses the 10% gate entirely. These go through
// treesitter.ParserFactory, the shared path production uses, so they see the
// ERROR-subtree skipping the factory now applies.
//
// The contract asserted here is the one the issue asks for: what the extractor
// emits for a file with one localised typo must be exactly the CLEAN part —
// no entity built out of the unreadable region — while a file with no typo at
// all must emit exactly what it emitted before.

// extractViaFactory runs the registered protobuf extractor over a tree obtained
// from the shared parser factory, and returns "kind/subtype:name" for each
// entity, sorted.
func extractViaFactory(t *testing.T, path, src string) []string {
	t.Helper()
	res, err := treesitter.NewParserFactory(nil).Parse(context.Background(), []byte(src), "proto")
	if err != nil {
		t.Fatalf("factory parse: %v", err)
	}
	t.Cleanup(res.TSTree.Close)

	ext, ok := extractor.Get("protobuf")
	if !ok {
		t.Fatal("protobuf extractor not registered")
	}
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "protobuf",
		TSTree:   res.TSTree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	got := make([]string, 0, len(entities))
	for _, e := range entities {
		got = append(got, fmt.Sprintf("%s/%s:%s", e.Kind, e.Subtype, e.Name))
	}
	sort.Strings(got)
	return got
}

// src6360 is the issue's shape: clean messages either side of exactly one
// malformed statement inside M0.
func src6360(broken string) string {
	var b strings.Builder
	b.WriteString("syntax = \"proto3\";\npackage p;\n")
	b.WriteString("message Clean {\n  string a = 1;\n  string b = 2;\n}\n")
	fmt.Fprintf(&b, "message M0 {\n  string a = 1;\n  %s\n}\n", broken)
	b.WriteString("message After {\n  string z = 1;\n}\n")
	return b.String()
}

// TestErrorSubtree6360_OutputIsSubsetOfCleanFile pins the extractor output for
// each of the issue's three fixture variants against the output for the same
// file with the typo repaired. Every emitted entity must also be emitted by the
// clean file — i.e. nothing is invented out of the unreadable region.
func TestErrorSubtree6360_OutputIsSubsetOfCleanFile(t *testing.T) {
	clean := extractViaFactory(t, "m.proto", src6360("string b = 2;"))
	cleanSet := map[string]bool{}
	for _, e := range clean {
		cleanSet[e] = true
	}

	for name, broken := range map[string]string{
		"missing_equals":    "string b 2;",
		"missing_fieldname": "string = 2;",
		"missing_semicolon": "string b = 2",
	} {
		t.Run(name, func(t *testing.T) {
			got := extractViaFactory(t, "m.proto", src6360(broken))
			for _, e := range got {
				if !cleanSet[e] {
					t.Errorf("entity %q is emitted for the BROKEN file but not for the repaired one: "+
						"the output describes tree-sitter's error recovery, not the source", e)
				}
			}
			// The clean messages either side of the typo must survive.
			for _, want := range []string{"Clean", "M0", "After"} {
				found := false
				for _, e := range got {
					if strings.HasSuffix(e, ":"+want) {
						found = true
					}
				}
				if !found {
					t.Errorf("message %s was dropped; only the ERROR subtree should be skipped", want)
				}
			}
		})
	}
}

// TestErrorSubtree6360_CleanFileUnchanged is the other direction: a file with no
// ERROR node must emit the exact, complete entity set. A change that just drops
// entities everywhere fails here.
func TestErrorSubtree6360_CleanFileUnchanged(t *testing.T) {
	got := extractViaFactory(t, "m.proto", src6360("string b = 2;"))
	want := []string{
		// The per-file SCOPE.Component/file entity (#6518) owns the file-level
		// CONTAINS edges that used to be anchored on the file PATH.
		"SCOPE.Component/file:m.proto",
		"SCOPE.Schema/field:After.z",
		"SCOPE.Schema/field:Clean.a",
		"SCOPE.Schema/field:Clean.b",
		"SCOPE.Schema/field:M0.a",
		"SCOPE.Schema/field:M0.b",
		"SCOPE.Schema/message:After",
		"SCOPE.Schema/message:Clean",
		"SCOPE.Schema/message:M0",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("clean-file entities changed.\n got: %v\nwant: %v", got, want)
	}
}
