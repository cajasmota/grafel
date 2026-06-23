package ruby_test

import (
	"context"
	"testing"

	tsruby "github.com/smacker/go-tree-sitter/ruby"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/ruby"
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
)

// parseForTest parses Ruby source into a ts.Tree via the smacker adapter. Tests
// stamp the result on FileInput.TSTree (which the migrated Ruby extractor
// consumes); the smacker adapter keeps these tests linkable in the default build
// while exercising the ts façade (B2 #5418).
func parseForTest(t *testing.T, src string) ts.Tree {
	t.Helper()
	parser, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tsruby.GetLanguage()))
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

func TestRubyExtractor_BasicExtraction(t *testing.T) {
	src := `
module Concerns
  class User
    def initialize(name)
      @name = name
    end

    def full_name
      @name
    end
  end
end
`
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("ruby")
	if !ok {
		t.Fatal("ruby extractor not registered")
	}

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "user.rb",
		Content:  []byte(src),
		Language: "ruby",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var classes, modules, methods int
	for _, e := range got {
		switch {
		case e.Kind == "SCOPE.Component" && e.Subtype == "class":
			classes++
		case e.Kind == "SCOPE.Component" && e.Subtype == "module":
			modules++
		case e.Kind == "SCOPE.Operation":
			methods++
		}
	}

	if classes == 0 {
		t.Error("expected at least one class entity")
	}
	if modules == 0 {
		t.Error("expected at least one module entity")
	}
	if methods == 0 {
		t.Error("expected at least one method entity")
	}
}

func TestRubyExtractor_ClassEntity(t *testing.T) {
	src := `
class Foo
  def bar
    "hello"
  end
end
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("ruby")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "foo.rb",
		Content:  []byte(src),
		Language: "ruby",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Name == "Foo" && e.Kind == "SCOPE.Component" && e.Subtype == "class" {
			found = true
			if e.SourceFile != "foo.rb" {
				t.Errorf("expected source_file foo.rb, got %s", e.SourceFile)
			}
			if e.Language != "ruby" {
				t.Errorf("expected language ruby, got %s", e.Language)
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

func TestRubyExtractor_ModuleEntity(t *testing.T) {
	src := `
module MyModule
  def helper
    nil
  end
end
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("ruby")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "module.rb",
		Content:  []byte(src),
		Language: "ruby",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Name == "MyModule" && e.Kind == "SCOPE.Component" && e.Subtype == "module" {
			found = true
		}
	}
	if !found {
		t.Error("expected entity MyModule with Kind=SCOPE.Component Subtype=module")
	}
}

func TestRubyExtractor_MethodEntity(t *testing.T) {
	src := `
class Svc
  def get_name(id)
    "name"
  end
end
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("ruby")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "svc.rb",
		Content:  []byte(src),
		Language: "ruby",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Ruby methods emit Subtype="function" to match Python parity golden
	// (fixtures/ruby/ruby__sample_rails.json). See
	// internal/extractors/ruby/ruby.go:buildMethod.
	var found bool
	for _, e := range got {
		if e.Name == "get_name" && e.Kind == "SCOPE.Operation" && e.Subtype == "function" {
			found = true
		}
	}
	if !found {
		t.Error("expected entity get_name with Kind=SCOPE.Operation Subtype=function")
	}
}

func TestRubyExtractor_EmptyFile(t *testing.T) {
	tree := parseForTest(t, "")
	ext, _ := extractor.Get("ruby")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.rb",
		Content:  []byte(""),
		Language: "ruby",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected zero entities for empty file, got %d", len(got))
	}
}

func TestRubyExtractor_NilTree(t *testing.T) {
	ext, _ := extractor.Get("ruby")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "nil.rb",
		Content:  []byte("class Foo; end"),
		Language: "ruby",
		TSTree:   nil,
	})
	if err != nil {
		t.Fatalf("unexpected error on nil tree: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected zero entities for nil tree, got %d", len(got))
	}
}

func TestRubyExtractor_MalformedFile(t *testing.T) {
	src := `
class GoodClass
  def good_method
    "ok"
  end
end

def orphan_method(x
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("ruby")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "malformed.rb",
		Content:  []byte(src),
		Language: "ruby",
		TSTree:   tree,
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

func TestRubyExtractor_UnregisteredLanguage(t *testing.T) {
	_, ok := extractor.Get("fortran")
	if ok {
		t.Error("expected false for unregistered language fortran")
	}
}

func TestRubyExtractor_LineNumbers(t *testing.T) {
	src := `class Alpha
  def method1
    nil
  end
end
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("ruby")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "lines.rb",
		Content:  []byte(src),
		Language: "ruby",
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
