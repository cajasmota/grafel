// imports_test.go — coverage for the IMPORTS ToID resolveImportToIDs
// pass (analog of #642/#650 for Java).

package java

import (
	"context"
	"strings"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tsjava "github.com/smacker/go-tree-sitter/java"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// runJavaExtract is a small helper that parses src, runs the Java
// extractor, and returns the produced EntityRecord slice. Test failures
// bubble up via t.Fatal so callers can assume non-nil non-empty output.
func runJavaExtract(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	parser := sitter.NewParser()
	parser.SetLanguage(tsjava.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ex := &Extractor{}
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:     "Demo.java",
		Language: "java",
		Content:  []byte(src),
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

// findJavaImportEdge returns the IMPORTS edge whose source_module
// matches the supplied dotted module path, or nil when no such edge
// exists.
func findJavaImportEdge(ents []types.EntityRecord, sourceModule string) *types.RelationshipRecord {
	for i := range ents {
		e := &ents[i]
		if e.Kind != "SCOPE.Component" {
			continue
		}
		for j := range e.Relationships {
			r := &e.Relationships[j]
			if r.Kind != "IMPORTS" {
				continue
			}
			if r.Properties != nil && r.Properties["source_module"] == sourceModule {
				return r
			}
		}
	}
	return nil
}

// Known external root: `import org.springframework.boot.SpringApplication;`
// → ToID="ext:org.springframework:SpringApplication". The resolver's
// IsKnownExternalPackage allowlist will then classify this as
// ExternalKnown directly.
func TestJavaImportsRewriteKnownExternal(t *testing.T) {
	src := `package com.demo;

import org.springframework.boot.SpringApplication;
import io.quarkus.runtime.Quarkus;
import java.util.List;

public class Demo {}
`
	ents := runJavaExtract(t, src)
	r := findJavaImportEdge(ents, "org.springframework.boot")
	if r == nil {
		t.Fatalf("missing IMPORTS edge for org.springframework.boot")
	}
	if !strings.HasPrefix(r.ToID, "ext:org.springframework") {
		t.Fatalf("spring import ToID = %q, want prefix ext:org.springframework", r.ToID)
	}
	r2 := findJavaImportEdge(ents, "io.quarkus.runtime")
	if r2 == nil {
		t.Fatalf("missing IMPORTS edge for io.quarkus.runtime")
	}
	if !strings.HasPrefix(r2.ToID, "ext:io.quarkus") {
		t.Fatalf("quarkus import ToID = %q, want prefix ext:io.quarkus", r2.ToID)
	}
	r3 := findJavaImportEdge(ents, "java.util")
	if r3 == nil {
		t.Fatalf("missing IMPORTS edge for java.util")
	}
	if !strings.HasPrefix(r3.ToID, "ext:java") {
		t.Fatalf("java.util import ToID = %q, want prefix ext:java", r3.ToID)
	}
}

// Unknown external / in-tree imports are left untouched: the resolver's
// downstream ResolveDottedImportTarget path needs the original dotted
// shape to bind in-tree modules.
func TestJavaImportsLeavesUnknownAlone(t *testing.T) {
	src := `package com.demo;

import com.acmecorp.users.UserService;

public class Demo {}
`
	ents := runJavaExtract(t, src)
	r := findJavaImportEdge(ents, "com.acmecorp.users")
	if r == nil {
		t.Fatalf("missing IMPORTS edge for com.acmecorp.users")
	}
	if strings.HasPrefix(r.ToID, "ext:") {
		t.Fatalf("com.acmecorp.users import ToID = %q, must not be ext: form", r.ToID)
	}
}

// Same-package / unqualified imports — Java has no leading-dot relative
// imports, but defensively the rewrite must not produce an ext: ToID
// for a leading-dot module string.
func TestJavaImportsSkipsRelative(t *testing.T) {
	// Construct an entity with a synthetic relative source_module and
	// verify resolveImportToIDs leaves it alone.
	ents := []types.EntityRecord{
		{
			Name:       "users",
			Kind:       "SCOPE.Component",
			SourceFile: "Demo.java",
			Language:   "java",
			Relationships: []types.RelationshipRecord{{
				ToID: ".users.UserService",
				Kind: "IMPORTS",
				Properties: map[string]string{
					"source_module": ".users",
					"local_name":    "UserService",
					"imported_name": "UserService",
				},
			}},
		},
	}
	resolveImportToIDs(ents)
	if strings.HasPrefix(ents[0].Relationships[0].ToID, "ext:") {
		t.Fatalf("relative-style import got ext: ToID = %q", ents[0].Relationships[0].ToID)
	}
}

// Wildcard imports (`import org.springframework.boot.*;`) — should
// rewrite to `ext:org.springframework` with no member suffix.
func TestJavaImportsWildcard(t *testing.T) {
	src := `package com.demo;

import org.springframework.boot.*;

public class Demo {}
`
	ents := runJavaExtract(t, src)
	// Wildcard source_module is "org.springframework.boot" (the .*
	// suffix is stripped by buildImport).
	r := findJavaImportEdge(ents, "org.springframework.boot")
	if r == nil {
		t.Fatalf("missing IMPORTS edge for org.springframework.boot wildcard")
	}
	if r.ToID != "ext:org.springframework" {
		t.Fatalf("wildcard import ToID = %q, want ext:org.springframework", r.ToID)
	}
}
