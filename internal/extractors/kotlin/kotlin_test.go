package kotlin_test

import (
	"context"
	"strings"
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

// TestKotlinExtractor_NoImportGhostComponents is the ghost-entity
// regression guard for AC #2. The Go kotlin extractor previously
// emitted a SCOPE.Component entity named after the top-level segment
// of every import path (e.g. "org", "com", "java"). That ghost shape
// broke parity verdict classification.
//
// IMPORTS edges ARE now emitted (Ktor-verb fix — the synth classifier
// needs the per-file import set to gate Ktor server DSL HTTP verbs),
// but every import entity carries Name=FULL dotted path. This test
// locks the current behaviour: given a file with org./com./java.
// imports, the extractor must not emit any entity whose name is the
// short leading segment, and every IMPORTS edge must carry the full
// dotted path as its ToID.
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

	ghostNames := map[string]bool{"org": true, "com": true, "java": true, "io": true, "kotlin": true}
	for _, e := range got {
		if ghostNames[e.Name] {
			t.Errorf("ghost entity %q (kind=%s) emitted from package/import parsing",
				e.Name, e.Kind)
		}
		for _, rel := range e.Relationships {
			if rel.Kind != "IMPORTS" {
				continue
			}
			// Every IMPORTS edge must carry a fully-qualified
			// dotted path OR the `ext:<root>[:<leaf>]` form produced
			// by resolveImportToIDs (mirror of #642/#650/#670 for
			// Kotlin) — never a short leading segment.
			if ghostNames[rel.ToID] {
				t.Errorf("IMPORTS ToID=%q is a ghost segment; want full dotted path",
					rel.ToID)
			}
			if !strings.Contains(rel.ToID, ".") && !strings.HasPrefix(rel.ToID, "ext:") {
				t.Errorf("IMPORTS ToID=%q has no '.' and no ext: prefix — expected fully-qualified import path or ext-tagged form",
					rel.ToID)
			}
		}
	}
}

// TestKotlinExtractor_NoOrgGhostFromPackageDeclaration specifically locks the
// AC #2 scenario: a Kotlin file with `package com.example.demo` must
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
			t.Errorf("package-declaration path segment %q emitted as entity (kind=%s)",
				e.Name, e.Kind)
		}
	}
}

// TestKotlinExtractor_SpringRestControllerEmitsService is the regression guard
// AC #3. A class annotated with @RestController must produce a
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
				t.Error("kotlin extractor emitted legacy 'spring_service' ghost")
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
	_, ok := extractor.Get("fortran")
	if ok {
		t.Error("expected false for unregistered language fortran")
	}
}

// ---------------------------------------------------------------------------
// Issue #3275 — type-system CST extraction: interface, enum, typealias
// ---------------------------------------------------------------------------

// TestKotlinExtractor_InterfaceDeclaration verifies that an interface
// declaration emits a SCOPE.Component/interface entity (not a plain class).
func TestKotlinExtractor_InterfaceDeclaration(t *testing.T) {
	src := `
interface Greeter {
    fun greet(name: String): String
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("kotlin")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Greeter.kt",
		Content:  []byte(src),
		Language: "kotlin",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Name == "Greeter" && e.Kind == "SCOPE.Component" && e.Subtype == "interface" {
			found = true
			if e.SourceFile != "Greeter.kt" {
				t.Errorf("expected SourceFile=Greeter.kt, got %s", e.SourceFile)
			}
			if e.Language != "kotlin" {
				t.Errorf("expected Language=kotlin, got %s", e.Language)
			}
		}
	}
	if !found {
		t.Error("expected entity Greeter with Kind=SCOPE.Component Subtype=interface")
	}
}

// TestKotlinExtractor_InterfaceNotClass ensures an interface declaration is NOT
// emitted with subtype "class".
func TestKotlinExtractor_InterfaceNotClass(t *testing.T) {
	src := `interface Repository { fun find(id: Int): Any? }`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("kotlin")
	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path: "Repo.kt", Content: []byte(src), Language: "kotlin", Tree: tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range got {
		if e.Name == "Repository" && e.Subtype == "class" {
			t.Error("interface Repository must not be emitted with subtype=class")
		}
	}
}

// TestKotlinExtractor_EnumDeclaration verifies that an enum class emits a
// SCOPE.Component/enum entity.
func TestKotlinExtractor_EnumDeclaration(t *testing.T) {
	src := `
enum class Direction {
    NORTH, SOUTH, EAST, WEST
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("kotlin")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Direction.kt",
		Content:  []byte(src),
		Language: "kotlin",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Name == "Direction" && e.Kind == "SCOPE.Component" && e.Subtype == "enum" {
			found = true
		}
	}
	if !found {
		t.Error("expected entity Direction with Kind=SCOPE.Component Subtype=enum")
	}
}

// TestKotlinExtractor_EnumNotClass ensures an enum class is NOT emitted with
// subtype "class".
func TestKotlinExtractor_EnumNotClass(t *testing.T) {
	src := `enum class Color { RED, GREEN, BLUE }`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("kotlin")
	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path: "Color.kt", Content: []byte(src), Language: "kotlin", Tree: tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range got {
		if e.Name == "Color" && e.Subtype == "class" {
			t.Error("enum class Color must not be emitted with subtype=class")
		}
	}
}

// TestKotlinExtractor_TypeAlias verifies that a typealias declaration emits a
// SCOPE.Schema/type_alias entity.
func TestKotlinExtractor_TypeAlias(t *testing.T) {
	src := `
typealias Handler = (String) -> Unit
typealias UserId = Int
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("kotlin")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "aliases.kt",
		Content:  []byte(src),
		Language: "kotlin",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := make(map[string]bool)
	for _, e := range got {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "type_alias" {
			names[e.Name] = true
			if e.Language != "kotlin" {
				t.Errorf("expected Language=kotlin on type_alias entity, got %s", e.Language)
			}
		}
	}
	if !names["Handler"] {
		t.Error("expected SCOPE.Schema/type_alias entity named Handler")
	}
	if !names["UserId"] {
		t.Error("expected SCOPE.Schema/type_alias entity named UserId")
	}
}

// TestKotlinExtractor_TypeSystemMixed verifies that a file mixing class,
// interface, enum, and typealias produces correct subtype discriminations.
func TestKotlinExtractor_TypeSystemMixed(t *testing.T) {
	src := `
interface Shape {
    fun area(): Double
}

enum class Color { RED, GREEN, BLUE }

typealias Callback = () -> Unit

data class Circle(val radius: Double) : Shape {
    override fun area(): Double = Math.PI * radius * radius
}

class Box(val width: Double, val height: Double) : Shape {
    override fun area(): Double = width * height
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("kotlin")
	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path: "shapes.kt", Content: []byte(src), Language: "kotlin", Tree: tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	type check struct {
		name, kind, subtype string
		found               bool
	}
	checks := []*check{
		{name: "Shape", kind: "SCOPE.Component", subtype: "interface"},
		{name: "Color", kind: "SCOPE.Component", subtype: "enum"},
		{name: "Callback", kind: "SCOPE.Schema", subtype: "type_alias"},
		{name: "Circle", kind: "SCOPE.Component", subtype: "data_class"},
		{name: "Box", kind: "SCOPE.Component", subtype: "class"},
	}
	for _, e := range got {
		for _, c := range checks {
			if e.Name == c.name && e.Kind == c.kind && e.Subtype == c.subtype {
				c.found = true
			}
		}
	}
	for _, c := range checks {
		if !c.found {
			t.Errorf("expected entity %s Kind=%s Subtype=%s", c.name, c.kind, c.subtype)
		}
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
