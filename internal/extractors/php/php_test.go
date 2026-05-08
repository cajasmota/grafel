package php_test

import (
	"context"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tsphp "github.com/smacker/go-tree-sitter/php"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/php"
)

// parseForTest parses PHP source using the real grammar.
func parseForTest(t *testing.T, src string) *sitter.Tree {
	t.Helper()
	parser := sitter.NewParser()
	parser.SetLanguage(tsphp.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, []byte(src))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	return tree
}

func TestPHPExtractor_BasicExtraction(t *testing.T) {
	src := `<?php

namespace App\Controllers;

interface UserRepositoryInterface {
    public function find(int $id): ?array;
}

class UserController {
    public function index(): array {
        return [];
    }

    public function show(int $id): array {
        return [];
    }
}

function handleRequest(string $method): void {}
`
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("php")
	if !ok {
		t.Fatal("php extractor not registered")
	}

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "controller.php",
		Content:  []byte(src),
		Language: "php",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var classes, interfaces, methods, functions int
	for _, e := range got {
		switch {
		case e.Kind == "SCOPE.Component" && e.Subtype == "class":
			classes++
		case e.Kind == "SCOPE.Component" && e.Subtype == "interface":
			interfaces++
		case e.Kind == "SCOPE.Operation" && e.Subtype == "method":
			methods++
		case e.Kind == "SCOPE.Operation" && e.Subtype == "function":
			functions++
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
	if functions == 0 {
		t.Error("expected at least one function entity")
	}
}

func TestPHPExtractor_ClassEntity(t *testing.T) {
	src := `<?php
class Foo {
    public function bar(): void {}
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("php")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "foo.php",
		Content:  []byte(src),
		Language: "php",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Name == "Foo" && e.Kind == "SCOPE.Component" && e.Subtype == "class" {
			found = true
			if e.SourceFile != "foo.php" {
				t.Errorf("expected source_file foo.php, got %s", e.SourceFile)
			}
			if e.Language != "php" {
				t.Errorf("expected language php, got %s", e.Language)
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

func TestPHPExtractor_InterfaceEntity(t *testing.T) {
	src := `<?php
interface IRepository {
    public function save(): void;
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("php")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "repo.php",
		Content:  []byte(src),
		Language: "php",
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

func TestPHPExtractor_MethodEntity(t *testing.T) {
	src := `<?php
class Svc {
    public function getName(int $id): string { return ""; }
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("php")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "svc.php",
		Content:  []byte(src),
		Language: "php",
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

func TestPHPExtractor_FunctionEntity(t *testing.T) {
	src := `<?php
function handleRequest(string $method): void {
    echo "ok";
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("php")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "func.php",
		Content:  []byte(src),
		Language: "php",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Name == "handleRequest" && e.Kind == "SCOPE.Operation" && e.Subtype == "function" {
			found = true
		}
	}
	if !found {
		t.Error("expected entity handleRequest with Kind=SCOPE.Operation Subtype=function")
	}
}

func TestPHPExtractor_EmptyFile(t *testing.T) {
	tree := parseForTest(t, "")
	ext, _ := extractor.Get("php")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.php",
		Content:  []byte(""),
		Language: "php",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected zero entities for empty file, got %d", len(got))
	}
}

func TestPHPExtractor_NilTree(t *testing.T) {
	ext, _ := extractor.Get("php")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "nil.php",
		Content:  []byte("<?php class Foo {}"),
		Language: "php",
		Tree:     nil,
	})
	if err != nil {
		t.Fatalf("unexpected error on nil tree: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected zero entities for nil tree, got %d", len(got))
	}
}

func TestPHPExtractor_MalformedFile(t *testing.T) {
	src := `<?php
class GoodClass {
    public function goodMethod(): void {}
}

class BadClass {
    public function badMethod(int $x
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("php")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "malformed.php",
		Content:  []byte(src),
		Language: "php",
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

func TestPHPExtractor_UnregisteredLanguage(t *testing.T) {
	_, ok := extractor.Get("cobol")
	if ok {
		t.Error("expected false for unregistered language cobol")
	}
}

func TestPHPExtractor_LineNumbers(t *testing.T) {
	src := `<?php
class Alpha {
    public function method1(): void {}
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("php")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "lines.php",
		Content:  []byte(src),
		Language: "php",
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
