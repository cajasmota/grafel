// imports_test.go — coverage for the IMPORTS ToID resolveImportToIDs
// pass (analog of #642/#650/#670 for Kotlin).

package kotlin

import (
	"context"
	"strings"
	"testing"

	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	tskotlin "github.com/smacker/go-tree-sitter/kotlin"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// runKotlinExtract is a small helper that parses src, runs the Kotlin
// extractor, and returns the produced EntityRecord slice. Test failures
// bubble up via t.Fatal so callers can assume non-nil non-empty output.
func runKotlinExtract(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	parser, perr := tssmacker.New().NewParser(tssmacker.WrapLanguage(tskotlin.GetLanguage()))
	if perr != nil {
		t.Fatalf("parser init: %v", perr)
	}
	defer parser.Close()
	tree, err := parser.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ex := &Extractor{}
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:     "Demo.kt",
		Language: "kotlin",
		Content:  []byte(src),
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

// findKotlinImportEdge returns the IMPORTS edge whose entity Name
// matches the supplied dotted module path, or nil when no such edge
// exists.
func findKotlinImportEdge(ents []types.EntityRecord, entityName string) *types.RelationshipRecord {
	for i := range ents {
		e := &ents[i]
		if e.Kind != "SCOPE.Component" || e.Subtype != "import" {
			continue
		}
		if e.Name != entityName {
			continue
		}
		for j := range e.Relationships {
			r := &e.Relationships[j]
			if r.Kind == "IMPORTS" {
				return r
			}
		}
	}
	return nil
}

// Known external root: `import org.springframework.boot.SpringApplication`
// → ToID="ext:org.springframework:SpringApplication". The resolver's
// IsKnownExternalPackage allowlist will then classify this as
// ExternalKnown directly. Also covers io.ktor (multi-segment dotted
// root) and kotlin (single-segment stdlib root).
func TestKotlinImportsRewriteKnownExternal(t *testing.T) {
	src := `package com.demo

import org.springframework.boot.SpringApplication
import io.ktor.server.routing.get
import kotlin.io.println

class Demo
`
	ents := runKotlinExtract(t, src)
	r := findKotlinImportEdge(ents, "org.springframework.boot.SpringApplication")
	if r == nil {
		t.Fatalf("missing IMPORTS edge for org.springframework.boot.SpringApplication")
	}
	if r.ToID != "ext:org.springframework:SpringApplication" {
		t.Fatalf("spring import ToID = %q, want ext:org.springframework:SpringApplication", r.ToID)
	}
	r2 := findKotlinImportEdge(ents, "io.ktor.server.routing.get")
	if r2 == nil {
		t.Fatalf("missing IMPORTS edge for io.ktor.server.routing.get")
	}
	if r2.ToID != "ext:io.ktor:get" {
		t.Fatalf("ktor import ToID = %q, want ext:io.ktor:get", r2.ToID)
	}
	r3 := findKotlinImportEdge(ents, "kotlin.io.println")
	if r3 == nil {
		t.Fatalf("missing IMPORTS edge for kotlin.io.println")
	}
	if r3.ToID != "ext:kotlin:println" {
		t.Fatalf("kotlin.io import ToID = %q, want ext:kotlin:println", r3.ToID)
	}
}

// Unknown external / in-tree imports are left untouched: the resolver's
// downstream ResolveDottedImportTarget path needs the original dotted
// shape to bind in-tree modules.
func TestKotlinImportsLeavesUnknownAlone(t *testing.T) {
	src := `package com.demo

import com.acmecorp.users.UserService

class Demo
`
	ents := runKotlinExtract(t, src)
	r := findKotlinImportEdge(ents, "com.acmecorp.users.UserService")
	if r == nil {
		t.Fatalf("missing IMPORTS edge for com.acmecorp.users.UserService")
	}
	if strings.HasPrefix(r.ToID, "ext:") {
		t.Fatalf("com.acmecorp.users.UserService import ToID = %q, must not be ext: form", r.ToID)
	}
}

// Wildcard imports (`import io.ktor.server.routing.*`) — should
// rewrite to `ext:io.ktor` with no member suffix.
func TestKotlinImportsWildcard(t *testing.T) {
	src := `package com.demo

import io.ktor.server.routing.*
import kotlinx.coroutines.*

class Demo
`
	ents := runKotlinExtract(t, src)
	// The trailing .* is stripped by buildImport, so the entity Name
	// is "io.ktor.server.routing".
	r := findKotlinImportEdge(ents, "io.ktor.server.routing")
	if r == nil {
		t.Fatalf("missing IMPORTS edge for io.ktor.server.routing wildcard")
	}
	if r.ToID != "ext:io.ktor" {
		t.Fatalf("wildcard ktor import ToID = %q, want ext:io.ktor", r.ToID)
	}
	r2 := findKotlinImportEdge(ents, "kotlinx.coroutines")
	if r2 == nil {
		t.Fatalf("missing IMPORTS edge for kotlinx.coroutines wildcard")
	}
	if r2.ToID != "ext:kotlinx" {
		t.Fatalf("wildcard kotlinx import ToID = %q, want ext:kotlinx", r2.ToID)
	}
}

// Defensive: a relative-style ToID (no Kotlin source produces this,
// but the rewrite must not corrupt it).
func TestKotlinImportsSkipsRelative(t *testing.T) {
	ents := []types.EntityRecord{
		{
			Name:       "users.UserService",
			Kind:       "SCOPE.Component",
			Subtype:    "import",
			SourceFile: "Demo.kt",
			Language:   "kotlin",
			Relationships: []types.RelationshipRecord{{
				ToID: ".users.UserService",
				Kind: "IMPORTS",
				Properties: map[string]string{
					"source_module": ".users.UserService",
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
