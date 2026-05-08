package swift_test

import (
	"context"
	"errors"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tsswift "github.com/smacker/go-tree-sitter/swift"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/swift"
	"github.com/cajasmota/archigraph/internal/treesitter"
)

func parseForTest(t *testing.T, src string) *sitter.Tree {
	t.Helper()
	parser := sitter.NewParser()
	parser.SetLanguage(tsswift.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, []byte(src))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	return tree
}

func TestSwiftExtractor_BasicExtraction(t *testing.T) {
	src := `
import Foundation

struct User {
    var id: Int
    var name: String
}

class UserService {
    func findAll() -> [User] {
        return []
    }
    func findById(_ id: Int) -> User? {
        return nil
    }
}

protocol Repository {
    func findById(_ id: Int) -> User?
}
`
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("swift")
	if !ok {
		t.Fatal("swift extractor not registered")
	}

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "service.swift",
		Content:  []byte(src),
		Language: "swift",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var classes, structs, protocols, funcs, imports int
	for _, e := range got {
		switch {
		case e.Kind == "SCOPE.Component" && e.Subtype == "class":
			classes++
		case e.Kind == "SCOPE.Component" && e.Subtype == "struct":
			structs++
		case e.Kind == "SCOPE.Component" && e.Subtype == "protocol":
			protocols++
		case e.Kind == "SCOPE.Operation":
			funcs++
		case e.Kind == "SCOPE.Component" && len(e.Relationships) > 0:
			imports++
		}
	}

	if classes == 0 {
		t.Error("expected at least one class entity")
	}
	if structs == 0 {
		t.Error("expected at least one struct entity")
	}
	if protocols == 0 {
		t.Error("expected at least one protocol entity")
	}
	if funcs == 0 {
		t.Error("expected at least one function entity")
	}
	if imports == 0 {
		t.Error("expected at least one import entity")
	}
}

func TestSwiftExtractor_ClassEntity(t *testing.T) {
	src := `
class Foo {
    func bar() {}
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("swift")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "foo.swift",
		Content:  []byte(src),
		Language: "swift",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Name == "Foo" && e.Kind == "SCOPE.Component" && e.Subtype == "class" {
			found = true
			if e.SourceFile != "foo.swift" {
				t.Errorf("expected source_file foo.swift, got %s", e.SourceFile)
			}
			if e.Language != "swift" {
				t.Errorf("expected language swift, got %s", e.Language)
			}
		}
	}
	if !found {
		t.Error("expected entity Foo with Kind=SCOPE.Component Subtype=class")
	}
}

func TestSwiftExtractor_StructEntity(t *testing.T) {
	src := `
struct Point {
    var x: Double
    var y: Double
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("swift")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "point.swift",
		Content:  []byte(src),
		Language: "swift",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Kind == "SCOPE.Component" && e.Subtype == "struct" {
			found = true
		}
	}
	if !found {
		t.Error("expected a struct SCOPE.Component entity")
	}
}

func TestSwiftExtractor_ProtocolEntity(t *testing.T) {
	src := `
protocol Drawable {
    func draw()
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("swift")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "drawable.swift",
		Content:  []byte(src),
		Language: "swift",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Kind == "SCOPE.Component" && e.Subtype == "protocol" {
			found = true
		}
	}
	if !found {
		t.Error("expected a protocol SCOPE.Component entity")
	}
}

func TestSwiftExtractor_FunctionEntity(t *testing.T) {
	src := `
class Svc {
    func greet(_ name: String) -> String {
        return "Hello " + name
    }
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("swift")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "svc.swift",
		Content:  []byte(src),
		Language: "swift",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "function" {
			found = true
		}
	}
	if !found {
		t.Error("expected at least one SCOPE.Operation function entity")
	}
}

func TestSwiftExtractor_ImportRelationship(t *testing.T) {
	src := `
import Foundation
import UIKit

class App {}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("swift")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "app.swift",
		Content:  []byte(src),
		Language: "swift",
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

	if !importTargets["Foundation"] {
		t.Error("expected IMPORTS relationship for Foundation")
	}
	if !importTargets["UIKit"] {
		t.Error("expected IMPORTS relationship for UIKit")
	}
}

func TestSwiftExtractor_EmptyFile(t *testing.T) {
	tree := parseForTest(t, "")
	ext, _ := extractor.Get("swift")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.swift",
		Content:  []byte(""),
		Language: "swift",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected zero entities for empty file, got %d", len(got))
	}
}

func TestSwiftExtractor_NilTree(t *testing.T) {
	ext, _ := extractor.Get("swift")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "nil.swift",
		Content:  []byte("class Foo {}"),
		Language: "swift",
		Tree:     nil,
	})
	if err != nil {
		t.Fatalf("unexpected error on nil tree: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected zero entities for nil tree, got %d", len(got))
	}
}

func TestSwiftExtractor_MissingGrammarReturnsErrUnsupportedLanguage(t *testing.T) {
	factory := treesitter.NewParserFactory(nil)
	_, err := factory.Parse(context.Background(), []byte("class Foo {}"), "dart")
	if err == nil {
		t.Fatal("expected ErrUnsupportedLanguage for dart, got nil")
	}
	if !errors.Is(err, treesitter.ErrUnsupportedLanguage) {
		t.Errorf("expected ErrUnsupportedLanguage, got: %v", err)
	}
}
