package java_test

import (
	"context"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tsjava "github.com/smacker/go-tree-sitter/java"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/java"
)

// parseForTest parses Java source using the real grammar.
func parseForTest(t *testing.T, src string) *sitter.Tree {
	t.Helper()
	parser := sitter.NewParser()
	parser.SetLanguage(tsjava.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, []byte(src))
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
		Tree:     tree,
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
		Tree:     tree,
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
		Tree:     tree,
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
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Name == "getName" && e.Kind == "SCOPE.Operation" && e.Subtype == "method" {
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
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Name == "Bar" && e.Kind == "SCOPE.Operation" && e.Subtype == "constructor" {
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
		Tree:     tree,
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

	if !importTargets["java.util.List"] {
		t.Error("expected IMPORTS relationship for java.util.List")
	}
	if !importTargets["java.util.ArrayList"] {
		t.Error("expected IMPORTS relationship for java.util.ArrayList")
	}
}

func TestJavaExtractor_EmptyFile(t *testing.T) {
	tree := parseForTest(t, "")
	ext, _ := extractor.Get("java")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.java",
		Content:  []byte(""),
		Language: "java",
		Tree:     tree,
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
		Tree:     nil,
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
		Tree:     tree,
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
	_, ok := extractor.Get("cobol")
	if ok {
		t.Error("expected false for unregistered language cobol")
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
		Tree:     tree,
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
	if !names["innerMethod"] {
		t.Error("expected innerMethod")
	}
	if !names["outerMethod"] {
		t.Error("expected outerMethod")
	}
}
