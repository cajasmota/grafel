package java_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	tsjava "github.com/smacker/go-tree-sitter/java"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/java"
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	"github.com/cajasmota/grafel/internal/types"
)

// parseForTest parses Java source into a ts.Tree via the smacker adapter. Tests
// stamp the result on FileInput.TSTree (which the migrated Java extractor
// consumes); the smacker adapter keeps these tests linkable in the default build
// while exercising the ts façade (B2 #5418).
func parseForTest(t *testing.T, src string) ts.Tree {
	t.Helper()
	parser, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tsjava.GetLanguage()))
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	return tree
}

func TestJavaExtractor_BasicExtraction(t *testing.T) {
	src := `
package com.example;

import java.util.List;
import java.util.ArrayList;

public interface UserRepository {
    User findById(int id);
}

public class UserService implements UserRepository {
    private List<User> users = new ArrayList<>();

    public UserService() {
        users.add(new User(1, "Alice"));
    }

    public User findById(int id) {
        return null;
    }

    public void create(String name) {
    }
}
`
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("java")
	if !ok {
		t.Fatal("java extractor not registered")
	}

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "UserService.java",
		Content:  []byte(src),
		Language: "java",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var classes, interfaces, methods, imports int
	for _, e := range got {
		switch {
		case e.Kind == "SCOPE.Component" && e.Subtype == "class":
			classes++
		case e.Kind == "SCOPE.Component" && e.Subtype == "interface":
			interfaces++
		case e.Kind == "SCOPE.Operation" && e.Subtype == "method":
			methods++
		case e.Kind == "SCOPE.Component" && len(e.Relationships) > 0:
			imports++
		}
	}

	if classes == 0 {
		t.Error("expected at least one class entity")
	}
	if interfaces == 0 {
		t.Error("expected at least one interface entity")
	}
	if methods == 0 {
		t.Error("expected at least one method entity")
	}
	if imports == 0 {
		t.Error("expected at least one import entity")
	}
}

func TestJavaExtractor_ClassEntity(t *testing.T) {
	src := `
public class Foo {
    public void bar() {}
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("java")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Foo.java",
		Content:  []byte(src),
		Language: "java",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Name == "Foo" && e.Kind == "SCOPE.Component" && e.Subtype == "class" {
			found = true
			if e.SourceFile != "Foo.java" {
				t.Errorf("expected source_file Foo.java, got %s", e.SourceFile)
			}
			if e.Language != "java" {
				t.Errorf("expected language java, got %s", e.Language)
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

func TestJavaExtractor_InterfaceEntity(t *testing.T) {
	src := `
public interface IRepository {
    void save();
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("java")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "repo.java",
		Content:  []byte(src),
		Language: "java",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Name == "IRepository" && e.Kind == "SCOPE.Component" && e.Subtype == "interface" {
			found = true
		}
	}
	if !found {
		t.Error("expected entity IRepository with Kind=SCOPE.Component Subtype=interface")
	}
}

func TestJavaExtractor_MethodEntity(t *testing.T) {
	src := `
public class Svc {
    public String getName(int id) { return ""; }
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("java")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "svc.java",
		Content:  []byte(src),
		Language: "java",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Name == "Svc.getName" && e.Kind == "SCOPE.Operation" && e.Subtype == "method" {
			found = true
		}
	}
	if !found {
		t.Error("expected entity getName with Kind=SCOPE.Operation Subtype=method")
	}
}

func TestJavaExtractor_ConstructorEntity(t *testing.T) {
	src := `
public class Bar {
    public Bar() {}
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("java")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "bar.java",
		Content:  []byte(src),
		Language: "java",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Name == "Bar.Bar" && e.Kind == "SCOPE.Operation" && e.Subtype == "constructor" {
			found = true
		}
	}
	if !found {
		t.Error("expected constructor entity Bar with Kind=SCOPE.Operation Subtype=constructor")
	}
}

func TestJavaExtractor_ImportRelationship(t *testing.T) {
	src := `
import java.util.List;
import java.util.ArrayList;
import static java.util.Collections.sort;

public class Foo {}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("java")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "imports.java",
		Content:  []byte(src),
		Language: "java",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	importTargets := map[string]bool{}
	for _, e := range got {
		for _, rel := range e.Relationships {
			if rel.Kind == "IMPORTS" {
				importTargets[rel.ToID] = true
			}
		}
	}

	// IMPORTS ToIDs for known external JVM packages are rewritten to the
	// `ext:<root>:<leaf>` form by resolveImportToIDs (analog of #642/#650)
	// so the resolver's external-disposition gate classifies them as
	// ExternalKnown directly.
	if !importTargets["ext:java:List"] {
		t.Errorf("expected IMPORTS ext:java:List, got: %v", importTargets)
	}
	if !importTargets["ext:java:ArrayList"] {
		t.Errorf("expected IMPORTS ext:java:ArrayList, got: %v", importTargets)
	}
}

func TestJavaExtractor_EmptyFile(t *testing.T) {
	tree := parseForTest(t, "")
	ext, _ := extractor.Get("java")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.java",
		Content:  []byte(""),
		Language: "java",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected zero entities for empty file, got %d", len(got))
	}
}

func TestJavaExtractor_NilTree(t *testing.T) {
	ext, _ := extractor.Get("java")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "nil.java",
		Content:  []byte("public class Foo {}"),
		Language: "java",
		TSTree:   nil,
	})
	if err != nil {
		t.Fatalf("unexpected error on nil tree: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected zero entities for nil tree, got %d", len(got))
	}
}

func TestJavaExtractor_MalformedFile(t *testing.T) {
	// Malformed: unclosed brace — tree-sitter produces partial tree
	src := `
public class GoodClass {
    public void goodMethod() {}
}
public class BadClass {
    public void badMethod(int x
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("java")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "malformed.java",
		Content:  []byte(src),
		Language: "java",
		TSTree:   tree,
	})
	// Must not panic; may return partial results.
	if err != nil {
		t.Fatalf("unexpected error on malformed file: %v", err)
	}
	// At least the valid class should be extracted.
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

func TestJavaExtractor_UnregisteredLanguage(t *testing.T) {
	// Verify that requesting an unregistered language returns false.
	_, ok := extractor.Get("fortran")
	if ok {
		t.Error("expected false for unregistered language fortran")
	}
}

func TestJavaExtractor_LineNumbers(t *testing.T) {
	src := `public class Alpha {
    public void method1() {}
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("java")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "lines.java",
		Content:  []byte(src),
		Language: "java",
		TSTree:   tree,
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

func TestJavaExtractor_NestedClass(t *testing.T) {
	src := `
public class Outer {
    public static class Inner {
        public void innerMethod() {}
    }
    public void outerMethod() {}
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("java")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "nested.java",
		Content:  []byte(src),
		Language: "java",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := map[string]bool{}
	for _, e := range got {
		names[e.Name] = true
	}

	if !names["Outer"] {
		t.Error("expected Outer class")
	}
	if !names["Inner"] {
		t.Error("expected Inner nested class")
	}
	if !names["Inner.innerMethod"] {
		t.Error("expected Inner.innerMethod")
	}
	if !names["Outer.outerMethod"] {
		t.Error("expected Outer.outerMethod")
	}
}

// TestJavaExtractor_DuplicateMethodNamesAcrossClasses is the regression
// test for issue #65. Two classes in the same file each declare a
// `validate` and a `save` method. The extractor must emit four DISTINCT
// method entities with class-qualified Names so
// ComputeID(SourceFile+Kind+Name) produces four distinct IDs rather
// than collapsing the same-named methods into two.
func TestJavaExtractor_DuplicateMethodNamesAcrossClasses(t *testing.T) {
	src := `
public class UserSerializer {
    public Object validate(Object value) { return value; }
    public Object save(Object value) { return value; }
}

public class OrderSerializer {
    public Object validate(Object value) { return value; }
    public Object save(Object value) { return value; }
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("java")

	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Serializers.java",
		Content:  []byte(src),
		Language: "java",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	wantMethods := map[string]bool{
		"UserSerializer.validate":  false,
		"UserSerializer.save":      false,
		"OrderSerializer.validate": false,
		"OrderSerializer.save":     false,
	}
	methodCount := 0
	var allNames []string
	for _, e := range entities {
		allNames = append(allNames, e.Name)
		if e.Kind == "SCOPE.Operation" && e.Subtype == "method" {
			methodCount++
			if _, ok := wantMethods[e.Name]; ok {
				wantMethods[e.Name] = true
			}
		}
	}
	if methodCount != 4 {
		t.Errorf("expected 4 distinct method entities, got %d (names=%v)",
			methodCount, allNames)
	}
	for name, seen := range wantMethods {
		if !seen {
			t.Errorf("expected method entity %q not found in %v", name, allNames)
		}
	}

	// IDs must be distinct under ComputeID(SourceFile+Kind+Name).
	ids := map[string]string{}
	for _, e := range entities {
		if e.Kind != "SCOPE.Operation" || e.Subtype != "method" {
			continue
		}
		id := e.ComputeID()
		if existing, ok := ids[id]; ok {
			t.Errorf("method ID collision: %q and %q both compute to %s",
				existing, e.Name, id)
		}
		ids[id] = e.Name
	}

	// Each class must own a CONTAINS edge per method (4 total: 2 per class).
	for _, cls := range []string{"UserSerializer", "OrderSerializer"} {
		count := 0
		for _, e := range entities {
			if e.Kind == "SCOPE.Component" && e.Name == cls {
				for _, r := range e.Relationships {
					if r.Kind == "CONTAINS" {
						count++
					}
				}
			}
		}
		if count != 2 {
			t.Errorf("class %s: expected 2 CONTAINS edges, got %d", cls, count)
		}
	}
}

// TestJavaExtractor_DuplicateMethodsFromFixture mirrors the inline test
// against the committed testdata fixture so the on-disk artifact stays
// in sync with the regression contract.
func TestJavaExtractor_DuplicateMethodsFromFixture(t *testing.T) {
	path := filepath.Join("testdata", "duplicate_methods.java.fixture")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	tree := parseForTest(t, string(src))
	ext, _ := extractor.Get("java")

	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  src,
		Language: "java",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	methodCount := 0
	var allNames []string
	byName := map[string]types.EntityRecord{}
	for _, e := range entities {
		allNames = append(allNames, e.Name)
		if e.Kind == "SCOPE.Operation" && e.Subtype == "method" {
			methodCount++
		}
		// Last-write-wins is fine — names are unique by construction
		// in this fixture (that's the whole point of the test).
		byName[e.Name] = e
	}
	// Expanded for #76:
	//   UserSerializer.{validate,save}      = 2
	//   OrderSerializer.{validate,save}     = 2
	//   Greeter.greet                       = 1 (interface method)
	//   LoggingGreeter.{greet,process}      = 2
	//   Outer.validate                      = 1
	//   Inner.validate                      = 1
	//   Status.describe                     = 1
	// Total                                 = 10
	if methodCount != 10 {
		t.Errorf("fixture: expected 10 method entities, got %d (names=%v)",
			methodCount, allNames)
	}

	// #76: interface + class collision — Greeter.greet and
	// LoggingGreeter.greet must be distinct entities with distinct IDs.
	ifaceGreet, okIface := byName["Greeter.greet"]
	classGreet, okClass := byName["LoggingGreeter.greet"]
	if !okIface {
		t.Errorf("expected interface method Greeter.greet (have=%v)", allNames)
	}
	if !okClass {
		t.Errorf("expected class method LoggingGreeter.greet (have=%v)", allNames)
	}
	if okIface && okClass && ifaceGreet.ComputeID() == classGreet.ComputeID() {
		t.Errorf("Greeter.greet and LoggingGreeter.greet share ComputeID %s",
			ifaceGreet.ComputeID())
	}

	// #76: outer/inner collision — single-level qualification per #65
	// means Inner.validate uses Inner (immediate parent), not
	// Outer.Inner.
	outerValidate, okOuter := byName["Outer.validate"]
	innerValidate, okInner := byName["Inner.validate"]
	if !okOuter {
		t.Errorf("expected Outer.validate (have=%v)", allNames)
	}
	if !okInner {
		t.Errorf("expected Inner.validate (immediate-parent qualification, have=%v)", allNames)
	}
	if okOuter && okInner && outerValidate.ComputeID() == innerValidate.ComputeID() {
		t.Errorf("Outer.validate and Inner.validate share ComputeID %s",
			outerValidate.ComputeID())
	}

	// #76: enum-with-method — enclosing Status must be emitted with
	// subtype="enum", and Status.describe must be emitted as a method.
	status, okStatus := byName["Status"]
	if !okStatus {
		t.Errorf("expected enum entity Status (have=%v)", allNames)
	} else {
		if status.Kind != "SCOPE.Component" {
			t.Errorf("Status: expected Kind=SCOPE.Component, got %s", status.Kind)
		}
		if status.Subtype != "enum" {
			t.Errorf("Status: expected Subtype=enum, got %s", status.Subtype)
		}
	}
	describe, okDescribe := byName["Status.describe"]
	if !okDescribe {
		t.Errorf("expected enum method Status.describe (have=%v)", allNames)
	} else if describe.Kind != "SCOPE.Operation" || describe.Subtype != "method" {
		t.Errorf("Status.describe: expected SCOPE.Operation/method, got %s/%s",
			describe.Kind, describe.Subtype)
	}
}

// TestJavaExtractor_QualifiedName verifies that entities extracted from a file
// with a package declaration carry a non-empty QualifiedName of the form
// "<package>.<ClassName>" for classes and "<package>.<Class>.<method>" for
// methods (#1917).
func TestJavaExtractor_QualifiedName(t *testing.T) {
	src := `package com.example.orders.controllers;

public class OrderController {
    public void createOrder() {}
}
`
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("java")
	if !ok {
		t.Fatal("java extractor not registered")
	}
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:    "src/controllers/OrderController.java",
		Content: []byte(src),
		TSTree:  tree,
	})
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	byName := make(map[string]types.EntityRecord)
	for _, e := range entities {
		byName[e.Name] = e
	}

	cls, ok := byName["OrderController"]
	if !ok {
		t.Fatal("expected class entity OrderController")
	}
	wantClassQN := "com.example.orders.controllers.OrderController"
	if cls.QualifiedName != wantClassQN {
		t.Errorf("OrderController QualifiedName = %q, want %q", cls.QualifiedName, wantClassQN)
	}

	method, ok := byName["OrderController.createOrder"]
	if !ok {
		t.Fatal("expected method entity OrderController.createOrder")
	}
	wantMethodQN := "com.example.orders.controllers.OrderController.createOrder"
	if method.QualifiedName != wantMethodQN {
		t.Errorf("OrderController.createOrder QualifiedName = %q, want %q", method.QualifiedName, wantMethodQN)
	}
}

// TestJavaExtractor_QualifiedName_LombokSynth verifies that Lombok-
// synthesised accessor Operations (which have no real AST node in the
// .java file) still carry a non-empty QualifiedName of the form
// "<package>.<Class>.<accessor>" so cross-package symbol navigation
// works for @Data / @Getter / @Setter generated methods (#1887).
func TestJavaExtractor_QualifiedName_LombokSynth(t *testing.T) {
	src := `package com.example.dto;

import lombok.Data;

@Data
public class ListRequest {
    private Integer pageSize;
}
`
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("java")
	if !ok {
		t.Fatal("java extractor not registered")
	}
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:    "src/main/java/com/example/dto/ListRequest.java",
		Content: []byte(src),
		TSTree:  tree,
	})
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	byName := make(map[string]types.EntityRecord)
	for _, e := range entities {
		byName[e.Name] = e
	}

	// Lombok @Data synthesises getPageSize / setPageSize / equals /
	// hashCode / toString on the class. We only assert the getter
	// since that is the canonical W5R3 evidence entity in #1887.
	getter, ok := byName["ListRequest.getPageSize"]
	if !ok {
		t.Fatalf("expected lombok-synth getter ListRequest.getPageSize, entities: %v", keys(byName))
	}
	if getter.Properties["synthesized_from"] == "" {
		t.Errorf("getter missing synthesized_from property: %#v", getter.Properties)
	}
	wantQN := "com.example.dto.ListRequest.getPageSize"
	if getter.QualifiedName != wantQN {
		t.Errorf("lombok-synth getter QualifiedName = %q, want %q (#1887)", getter.QualifiedName, wantQN)
	}
}

// keys returns the keys of a byName map for diagnostic output.
func keys(m map[string]types.EntityRecord) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestJavaExtractor_QualifiedName_NoPackage verifies that entities in a file
// without a package declaration still get a non-empty QualifiedName equal to
// the class/method name (default package, #1917).
func TestJavaExtractor_QualifiedName_NoPackage(t *testing.T) {
	src := `public class Foo {
    public void bar() {}
}
`
	tree := parseForTest(t, src)
	ext, ok2 := extractor.Get("java")
	if !ok2 {
		t.Fatal("java extractor not registered")
	}
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:    "Foo.java",
		Content: []byte(src),
		TSTree:  tree,
	})
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	byName := make(map[string]types.EntityRecord)
	for _, e := range entities {
		byName[e.Name] = e
	}

	cls, ok := byName["Foo"]
	if !ok {
		t.Fatal("expected class entity Foo")
	}
	// No package → QualifiedName falls back to bare class name.
	if cls.QualifiedName != "Foo" {
		t.Errorf("Foo QualifiedName = %q, want \"Foo\"", cls.QualifiedName)
	}

	method, ok := byName["Foo.bar"]
	if !ok {
		t.Fatal("expected method entity Foo.bar")
	}
	if method.QualifiedName != "Foo.bar" {
		t.Errorf("Foo.bar QualifiedName = %q, want \"Foo.bar\"", method.QualifiedName)
	}
}
