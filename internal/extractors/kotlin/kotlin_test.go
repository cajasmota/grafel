package kotlin_test

import (
	"context"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tskotlin "github.com/smacker/go-tree-sitter/kotlin"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/kotlin"
)

// parseForTest parses Kotlin source using the real grammar.
func parseForTest(t *testing.T, src string) *sitter.Tree {
	t.Helper()
	parser := sitter.NewParser()
	parser.SetLanguage(tskotlin.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, []byte(src))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	return tree
}

func TestKotlinExtractor_BasicExtraction(t *testing.T) {
	src := `
package com.example

import org.springframework.web.bind.annotation.*
import org.springframework.http.ResponseEntity

data class User(val id: Int, val name: String)

class UserService {
    fun findById(id: Int): User? = null
    fun create(name: String): User = User(1, name)
}
`
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("kotlin")
	if !ok {
		t.Fatal("kotlin extractor not registered")
	}

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "UserService.kt",
		Content:  []byte(src),
		Language: "kotlin",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var classes, functions int
	for _, e := range got {
		switch {
		case e.Kind == "SCOPE.Component" && (e.Subtype == "class"):
			classes++
		case e.Kind == "SCOPE.Operation":
			functions++
		}
	}

	if classes == 0 {
		t.Error("expected at least one class entity")
	}
	if functions == 0 {
		t.Error("expected at least one function entity")
	}
}

func TestKotlinExtractor_ClassEntity(t *testing.T) {
	src := `
class Foo {
    fun bar(): String = "hello"
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("kotlin")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Foo.kt",
		Content:  []byte(src),
		Language: "kotlin",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Name == "Foo" && e.Kind == "SCOPE.Component" && e.Subtype == "class" {
			found = true
			if e.SourceFile != "Foo.kt" {
				t.Errorf("expected source_file Foo.kt, got %s", e.SourceFile)
			}
			if e.Language != "kotlin" {
				t.Errorf("expected language kotlin, got %s", e.Language)
			}
			if e.StartLine == 0 {
				t.Error("expected non-zero start_line")
			}
		}
	}
	if !found {
		t.Error("expected entity Foo with Kind=SCOPE.Component Subtype=class")
	}
}

func TestKotlinExtractor_FunctionEntity(t *testing.T) {
	src := `
class Svc {
    fun getName(id: Int): String = "name"
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("kotlin")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "svc.kt",
		Content:  []byte(src),
		Language: "kotlin",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Name == "getName" && e.Kind == "SCOPE.Operation" && e.Subtype == "function" {
			found = true
		}
	}
	if !found {
		t.Error("expected entity getName with Kind=SCOPE.Operation Subtype=function")
	}
}

func TestKotlinExtractor_ObjectDeclaration(t *testing.T) {
	src := `
object MySingleton {
    fun doSomething(): Unit {}
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("kotlin")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "singleton.kt",
		Content:  []byte(src),
		Language: "kotlin",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Name == "MySingleton" && e.Kind == "SCOPE.Component" && e.Subtype == "object" {
			found = true
		}
	}
	if !found {
		t.Error("expected entity MySingleton with Kind=SCOPE.Component Subtype=object")
	}
}

// TestKotlinExtractor_NoImportGhostComponents is the regression guard for
// MX-1081 AC #2. The Go kotlin extractor previously emitted a SCOPE.Component
// entity named after the top-level segment of every import path (e.g. "org",
// "com", "java") plus an IMPORTS relationship. The Python indexer emits
// neither, so those entities were Go-only ghosts polluting parity output.
//
// This test locks the current behaviour: given a file with org./com./java.
// imports, the extractor must not emit any entity whose name is one of those
// segments, and must not emit any IMPORTS relationship at all.
func TestKotlinExtractor_NoImportGhostComponents(t *testing.T) {
	src := `
package com.example.demo

import org.springframework.web.bind.annotation.RestController
import org.springframework.http.ResponseEntity
import com.fasterxml.jackson.databind.ObjectMapper
import java.util.UUID

class Foo {}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("kotlin")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "imports.kt",
		Content:  []byte(src),
		Language: "kotlin",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ghostNames := map[string]bool{"org": true, "com": true, "java": true}
	for _, e := range got {
		if ghostNames[e.Name] {
			t.Errorf("ghost entity %q (kind=%s) emitted from package/import parsing — MX-1081 regression",
				e.Name, e.Kind)
		}
		for _, rel := range e.Relationships {
			if rel.Kind == "IMPORTS" {
				t.Errorf("unexpected IMPORTS relationship %s → %s — kotlin extractor must not emit imports (MX-1081)",
					rel.FromID, rel.ToID)
			}
		}
	}
}

// TestKotlinExtractor_NoOrgGhostFromPackageDeclaration specifically locks the
// MX-1081 AC #2 scenario: a Kotlin file with `package com.example.demo` must
// not produce entities named "com", "com.example", or "com.example.demo".
func TestKotlinExtractor_NoOrgGhostFromPackageDeclaration(t *testing.T) {
	src := `
package com.example.demo

class Bar {
    fun baz(): Int = 1
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("kotlin")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Bar.kt",
		Content:  []byte(src),
		Language: "kotlin",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	forbidden := map[string]bool{
		"com":              true,
		"com.example":      true,
		"com.example.demo": true,
	}
	for _, e := range got {
		if forbidden[e.Name] {
			t.Errorf("package-declaration path segment %q emitted as entity (kind=%s) — MX-1081 regression",
				e.Name, e.Kind)
		}
	}
}

// TestKotlinExtractor_SpringRestControllerEmitsService is the regression guard
// for MX-1081 AC #3. A class annotated with @RestController must produce a
// SCOPE.Service entity whose name equals the class name (NOT the previous
// hardcoded "spring_service" ghost).
func TestKotlinExtractor_SpringRestControllerEmitsService(t *testing.T) {
	src := `
@RestController
@RequestMapping("/api/users")
class UserController {
    fun list(): List<String> = emptyList()
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("kotlin")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "UserController.kt",
		Content:  []byte(src),
		Language: "kotlin",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var svc *struct {
		Name          string
		QualifiedName string
		Provenance    string
		SourceType    string
	}
	for _, e := range got {
		if e.Kind == "SCOPE.Service" {
			if e.Name == "spring_service" {
				t.Error("kotlin extractor emitted legacy 'spring_service' ghost — MX-1081 regression")
			}
			if svc == nil {
				svc = &struct {
					Name          string
					QualifiedName string
					Provenance    string
					SourceType    string
				}{
					Name:          e.Name,
					QualifiedName: e.QualifiedName,
					Provenance:    e.Properties["provenance"],
					SourceType:    e.Properties["source_type"],
				}
			}
		}
	}
	if svc == nil {
		t.Fatal("expected a SCOPE.Service entity for @RestController-annotated class")
	}
	if svc.Name != "UserController" {
		t.Errorf("SCOPE.Service name = %q, want %q", svc.Name, "UserController")
	}
	if svc.QualifiedName != "UserController.kt::UserController" {
		t.Errorf("SCOPE.Service qualified_name = %q, want %q", svc.QualifiedName, "UserController.kt::UserController")
	}
	if svc.Provenance != "@RestController" {
		t.Errorf("SCOPE.Service provenance = %q, want %q", svc.Provenance, "@RestController")
	}
	if svc.SourceType != "class" {
		t.Errorf("SCOPE.Service source_type = %q, want %q", svc.SourceType, "class")
	}
}

// TestKotlinExtractor_AllSpringStereotypesEmitService covers every Spring
// stereotype annotation recognised by buildSpringService.
func TestKotlinExtractor_AllSpringStereotypesEmitService(t *testing.T) {
	cases := []struct {
		stereotype string
		className  string
	}{
		{"RestController", "ApiController"},
		{"Controller", "MvcController"},
		{"Service", "UserService"},
		{"Component", "JobScheduler"},
		{"Repository", "UserRepository"},
	}
	for _, tc := range cases {
		t.Run(tc.stereotype, func(t *testing.T) {
			src := "@" + tc.stereotype + "\nclass " + tc.className + " {\n    fun ping(): String = \"ok\"\n}\n"
			tree := parseForTest(t, src)
			ext, _ := extractor.Get("kotlin")
			got, err := ext.Extract(context.Background(), extractor.FileInput{
				Path:     tc.className + ".kt",
				Content:  []byte(src),
				Language: "kotlin",
				Tree:     tree,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var found bool
			for _, e := range got {
				if e.Kind == "SCOPE.Service" && e.Name == tc.className {
					found = true
					if e.Properties["provenance"] != "@"+tc.stereotype {
						t.Errorf("provenance = %q, want %q", e.Properties["provenance"], "@"+tc.stereotype)
					}
				}
			}
			if !found {
				t.Errorf("expected SCOPE.Service %q for @%s class", tc.className, tc.stereotype)
			}
		})
	}
}

// TestKotlinExtractor_PlainClassNoService asserts that a class without any
// Spring stereotype annotation does NOT emit a SCOPE.Service entity.
func TestKotlinExtractor_PlainClassNoService(t *testing.T) {
	src := `
class PlainOldKotlinClass {
    fun hello(): String = "hi"
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("kotlin")
	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "plain.kt",
		Content:  []byte(src),
		Language: "kotlin",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range got {
		if e.Kind == "SCOPE.Service" {
			t.Errorf("unexpected SCOPE.Service entity %q — plain class must not produce a service", e.Name)
		}
	}
}

func TestKotlinExtractor_EmptyFile(t *testing.T) {
	tree := parseForTest(t, "")
	ext, _ := extractor.Get("kotlin")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.kt",
		Content:  []byte(""),
		Language: "kotlin",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected zero entities for empty file, got %d", len(got))
	}
}

func TestKotlinExtractor_NilTree(t *testing.T) {
	ext, _ := extractor.Get("kotlin")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "nil.kt",
		Content:  []byte("class Foo {}"),
		Language: "kotlin",
		Tree:     nil,
	})
	if err != nil {
		t.Fatalf("unexpected error on nil tree: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected zero entities for nil tree, got %d", len(got))
	}
}

func TestKotlinExtractor_MalformedFile(t *testing.T) {
	src := `
class GoodClass {
    fun goodMethod(): String = "ok"
}

class BadClass {
    fun badMethod(x: Int
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("kotlin")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "malformed.kt",
		Content:  []byte(src),
		Language: "kotlin",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error on malformed file: %v", err)
	}

	var foundGood bool
	for _, e := range got {
		if e.Name == "GoodClass" {
			foundGood = true
		}
	}
	if !foundGood {
		t.Error("expected GoodClass to be extracted from malformed file")
	}
}

func TestKotlinExtractor_UnregisteredLanguage(t *testing.T) {
	_, ok := extractor.Get("cobol")
	if ok {
		t.Error("expected false for unregistered language cobol")
	}
}

func TestKotlinExtractor_LineNumbers(t *testing.T) {
	src := `class Alpha {
    fun method1(): Unit {}
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("kotlin")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "lines.kt",
		Content:  []byte(src),
		Language: "kotlin",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, e := range got {
		if e.Kind == "SCOPE.Component" && e.Name == "Alpha" {
			if e.StartLine < 1 {
				t.Errorf("expected StartLine >= 1, got %d", e.StartLine)
			}
			if e.EndLine < e.StartLine {
				t.Errorf("expected EndLine >= StartLine, got start=%d end=%d", e.StartLine, e.EndLine)
			}
		}
	}
}
