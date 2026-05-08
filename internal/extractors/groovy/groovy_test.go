package groovy_test

import (
	"context"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tsgroovy "github.com/smacker/go-tree-sitter/groovy"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/groovy"
)

func parseForTest(t *testing.T, src string) *sitter.Tree {
	t.Helper()
	parser := sitter.NewParser()
	parser.SetLanguage(tsgroovy.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, []byte(src))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	return tree
}

func TestGroovyExtractor_Registered(t *testing.T) {
	_, ok := extractor.Get("groovy")
	if !ok {
		t.Fatal("groovy extractor not registered")
	}
}

func TestGroovyExtractor_ClassAndMethods(t *testing.T) {
	src := `class UserController {
    def index() {
        return [users: []]
    }

    def show(int id) {
        return null
    }

    private boolean validate(String email) {
        return email.contains('@')
    }
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("groovy")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "UserController.groovy",
		Content:  []byte(src),
		Language: "groovy",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) < 2 {
		t.Fatalf("expected at least 2 entities, got %d", len(entities))
	}

	classes := make(map[string]bool)
	methods := make(map[string]bool)
	for _, e := range entities {
		switch e.Subtype {
		case "class":
			classes[e.Name] = true
			if e.Kind != "SCOPE.Component" {
				t.Errorf("class %q: expected Kind=SCOPE.Component, got %q", e.Name, e.Kind)
			}
		case "method":
			methods[e.Name] = true
			if e.Kind != "SCOPE.Operation" {
				t.Errorf("method %q: expected Kind=SCOPE.Operation, got %q", e.Name, e.Kind)
			}
		}
	}

	if !classes["UserController"] {
		t.Error("expected class 'UserController' to be extracted")
	}
}

func TestGroovyExtractor_TopLevelFunction(t *testing.T) {
	src := `def handleRequest(String method, String path) {
    return [status: 'ok']
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("groovy")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "handler.groovy",
		Content:  []byte(src),
		Language: "groovy",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, e := range entities {
		if e.Name == "handleRequest" && e.Subtype == "function" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected top-level function 'handleRequest' to be extracted")
	}
}

func TestGroovyExtractor_EmptyInput(t *testing.T) {
	ext, _ := extractor.Get("groovy")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.groovy",
		Content:  []byte{},
		Language: "groovy",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) != 0 {
		t.Errorf("expected 0 entities, got %d", len(entities))
	}
}

func TestGroovyExtractor_Language(t *testing.T) {
	src := `class Foo {
    def bar() { return 1 }
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("groovy")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Foo.groovy",
		Content:  []byte(src),
		Language: "groovy",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range entities {
		if e.Language != "groovy" {
			t.Errorf("entity %q: expected Language=groovy, got %q", e.Name, e.Language)
		}
	}
}
