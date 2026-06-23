// imports_test.go — coverage for the IMPORTS ToID resolveImportToIDs
// pass (analog of #642/#650 for Java).

package java

import (
	"context"
	"strings"
	"testing"

	tsjava "github.com/smacker/go-tree-sitter/java"

	"github.com/cajasmota/grafel/internal/extractor"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	"github.com/cajasmota/grafel/internal/types"
)

// runJavaExtract is a small helper that parses src, runs the Java
// extractor, and returns the produced EntityRecord slice. Test failures
// bubble up via t.Fatal so callers can assume non-nil non-empty output.
func runJavaExtract(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	parser, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tsjava.GetLanguage()))
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ex := &Extractor{}
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:     "Demo.java",
		Language: "java",
		Content:  []byte(src),
		TSTree:   tree,
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

// ---- Issue #666 + #681: IMPORTS edges carry local_name in Properties ----
//
// Issue #666 fixed: the local bound name is now in Properties["local_name"]
// on the IMPORTS edge (e.g. "List" for `import java.util.List;`).
//
// Issue #681 changed: IMPORTS edges are now attached to the file-level
// entity (Name=file path, Subtype="file") instead of a separate
// SCOPE.Component placeholder per import. The resolver's BuildImportTable
// reads rel.Properties["local_name"] — it does not need the entity Name to
// equal the local name. The old import-placeholder entities with
// Name="List", Name="UserService" etc. no longer exist; their absence is
// the whole point of #681.

// TestJavaImportLocalNameInProperties verifies that IMPORTS edges carry
// the correct local_name in Properties (issue #666 contract), and that
// no separate import-placeholder SCOPE.Component entity is emitted
// (issue #681 contract — no dangling orphan entities).
//
//	import com.example.UserService;  → Properties["local_name"] = "UserService"
//	import java.util.List;           → Properties["local_name"] = "List"
func TestJavaImportLocalNameInProperties(t *testing.T) {
	src := `package com.demo;

import com.example.UserService;
import java.util.List;

public class Demo {}
`
	ents := runJavaExtract(t, src)

	// Helper: find IMPORTS edge by source_module and return its local_name property.
	findLocalName := func(sourceModule string) string {
		for i := range ents {
			e := &ents[i]
			for j := range e.Relationships {
				r := &e.Relationships[j]
				if r.Kind == "IMPORTS" && r.Properties != nil &&
					r.Properties["source_module"] == sourceModule {
					return r.Properties["local_name"]
				}
			}
		}
		return ""
	}

	if got := findLocalName("com.example"); got != "UserService" {
		t.Errorf("import com.example.UserService: local_name = %q, want UserService", got)
	}
	if got := findLocalName("java.util"); got != "List" {
		t.Errorf("import java.util.List: local_name = %q, want List", got)
	}

	// Issue #681 — no import-placeholder SCOPE.Component entity should exist.
	// The only SCOPE.Component entities must be: file entity + class entities.
	for i := range ents {
		e := &ents[i]
		if e.Kind != "SCOPE.Component" {
			continue
		}
		// Allowed: subtype="file" (file entity) or subtype="class"/"interface"/"enum".
		if e.Subtype == "" {
			t.Errorf("import-placeholder entity still emitted: Name=%q SourceFile=%q", e.Name, e.SourceFile)
		}
	}
}

// TestJavaImportNameStaticAndWildcard verifies that static and wildcard
// imports carry the correct local_name / wildcard properties (issue #666
// edge cases) and do not produce orphan placeholder entities (#681).
//
//	import static com.example.UserService.create; → local_name = "create"
//	import org.springframework.boot.*;             → wildcard = "1"
func TestJavaImportNameStaticAndWildcard(t *testing.T) {
	src := `package com.demo;

import static com.example.UserService.create;
import org.springframework.boot.*;
import com.example.UserService.Inner;

public class Demo {}
`
	ents := runJavaExtract(t, src)

	// findImportsEdge returns the first IMPORTS edge whose source_module AND
	// local_name both match. For wildcard imports, pass localName="".
	findImportsEdge := func(sourceModule, localName string) *types.RelationshipRecord {
		for i := range ents {
			e := &ents[i]
			for j := range e.Relationships {
				r := &e.Relationships[j]
				if r.Kind != "IMPORTS" || r.Properties == nil {
					continue
				}
				if r.Properties["source_module"] != sourceModule {
					continue
				}
				if localName == "" {
					// wildcard: no local_name key expected
					if r.Properties["wildcard"] == "1" {
						return r
					}
					continue
				}
				if r.Properties["local_name"] == localName {
					return r
				}
			}
		}
		return nil
	}

	// Static import: `import static com.example.UserService.create;`
	// local_name = "create"
	if r := findImportsEdge("com.example.UserService", "create"); r == nil {
		t.Errorf("import static ...UserService.create: no IMPORTS edge with local_name=create for source_module=com.example.UserService")
	}

	// Wildcard import: wildcard = "1", no local_name.
	if r := findImportsEdge("org.springframework.boot", ""); r == nil {
		t.Errorf("import org.springframework.boot.*: no wildcard IMPORTS edge found")
	}

	// Inner class import: `import com.example.UserService.Inner;`
	// Shares source_module "com.example.UserService" with the static import.
	if r := findImportsEdge("com.example.UserService", "Inner"); r == nil {
		t.Errorf("import com.example.UserService.Inner: no IMPORTS edge with local_name=Inner found")
	}

	// Issue #681 — no import-placeholder SCOPE.Component entity should exist.
	for i := range ents {
		e := &ents[i]
		if e.Kind == "SCOPE.Component" && e.Subtype == "" {
			t.Errorf("import-placeholder entity still emitted: Name=%q SourceFile=%q", e.Name, e.SourceFile)
		}
	}
}

// TestJavaImportsRewritePOI verifies that org.apache.poi.* and
// org.apache.pdfbox.* imports are rewritten by resolveImportToIDs to use
// the ext: prefix, routing the IMPORTS edge to ExternalKnown in the
// resolver (issue #787c).
func TestJavaImportsRewritePOI(t *testing.T) {
	src := `package com.demo.inventory.reports;

import org.apache.poi.xssf.usermodel.XSSFWorkbook;
import org.apache.poi.xssf.streaming.SXSSFWorkbook;
import org.apache.poi.ss.util.CellRangeAddress;
import org.apache.poi.ss.usermodel.*;
import org.apache.pdfbox.pdmodel.PDDocument;
import org.apache.pdfbox.pdmodel.PDPage;
import org.apache.commons.io.FileUtils;

public class Report {}
`
	ents := runJavaExtract(t, src)

	cases := []struct {
		sourceMod string
		wantPfx   string
	}{
		{"org.apache.poi.xssf.usermodel", "ext:org.apache.poi"},
		{"org.apache.poi.xssf.streaming", "ext:org.apache.poi"},
		{"org.apache.poi.ss.util", "ext:org.apache.poi"},
		{"org.apache.poi.ss.usermodel", "ext:org.apache.poi"},
		{"org.apache.pdfbox.pdmodel", "ext:org.apache.pdfbox"},
		{"org.apache.commons.io", "ext:org.apache.commons.io"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.sourceMod, func(t *testing.T) {
			r := findJavaImportEdge(ents, c.sourceMod)
			if r == nil {
				t.Fatalf("missing IMPORTS edge for source_module=%q", c.sourceMod)
			}
			if len(r.ToID) < len(c.wantPfx) || r.ToID[:len(c.wantPfx)] != c.wantPfx {
				t.Fatalf("source_module=%q: ToID=%q, want prefix %q", c.sourceMod, r.ToID, c.wantPfx)
			}
		})
	}
}
