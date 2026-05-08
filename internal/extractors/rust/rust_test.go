package rust_test

import (
	"context"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tsrust "github.com/smacker/go-tree-sitter/rust"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/rust"
)

// parseForTest parses Rust source using the real grammar.
func parseForTest(t *testing.T, src string) *sitter.Tree {
	t.Helper()
	parser := sitter.NewParser()
	parser.SetLanguage(tsrust.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, []byte(src))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	return tree
}

func TestRustExtractor_BasicExtraction(t *testing.T) {
	src := `
use std::collections::HashMap;
use serde::{Deserialize, Serialize};

#[derive(Serialize, Deserialize)]
struct User {
    id: u32,
    name: String,
}

trait Repository {
    fn find(&self, id: u32) -> Option<User>;
}

impl Repository for Vec<User> {
    fn find(&self, id: u32) -> Option<User> {
        self.iter().find(|u| u.id == id).cloned()
    }
}

fn create_user(name: String) -> User {
    User { id: 1, name }
}
`
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("rust")
	if !ok {
		t.Fatal("rust extractor not registered")
	}

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "main.rs",
		Content:  []byte(src),
		Language: "rust",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var structs, traits, impls, functions, imports int
	for _, e := range got {
		switch {
		case e.Kind == "SCOPE.Component" && e.Subtype == "struct":
			structs++
		case e.Kind == "SCOPE.Component" && e.Subtype == "trait":
			traits++
		case e.Kind == "SCOPE.Component" && e.Subtype == "impl":
			impls++
		case e.Kind == "SCOPE.Operation":
			functions++
		case e.Kind == "SCOPE.Component" && len(e.Relationships) > 0:
			imports++
		}
	}

	if structs == 0 {
		t.Error("expected at least one struct entity")
	}
	if traits == 0 {
		t.Error("expected at least one trait entity")
	}
	if impls == 0 {
		t.Error("expected at least one impl entity")
	}
	if functions == 0 {
		t.Error("expected at least one function entity")
	}
	if imports == 0 {
		t.Error("expected at least one import entity")
	}
}

func TestRustExtractor_StructEntity(t *testing.T) {
	src := `
struct Foo {
    id: u32,
    name: String,
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("rust")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "foo.rs",
		Content:  []byte(src),
		Language: "rust",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Name == "Foo" && e.Kind == "SCOPE.Component" && e.Subtype == "struct" {
			found = true
			if e.SourceFile != "foo.rs" {
				t.Errorf("expected source_file foo.rs, got %s", e.SourceFile)
			}
			if e.Language != "rust" {
				t.Errorf("expected language rust, got %s", e.Language)
			}
			if e.StartLine == 0 {
				t.Error("expected non-zero start_line")
			}
		}
	}
	if !found {
		t.Error("expected entity Foo with Kind=SCOPE.Component Subtype=struct")
	}
}

func TestRustExtractor_EnumEntity(t *testing.T) {
	src := `
enum Color {
    Red,
    Green,
    Blue,
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("rust")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "color.rs",
		Content:  []byte(src),
		Language: "rust",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Name == "Color" && e.Kind == "SCOPE.Component" && e.Subtype == "enum" {
			found = true
		}
	}
	if !found {
		t.Error("expected entity Color with Kind=SCOPE.Component Subtype=enum")
	}
}

func TestRustExtractor_TraitEntity(t *testing.T) {
	src := `
trait Animal {
    fn speak(&self) -> String;
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("rust")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "animal.rs",
		Content:  []byte(src),
		Language: "rust",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Name == "Animal" && e.Kind == "SCOPE.Component" && e.Subtype == "trait" {
			found = true
		}
	}
	if !found {
		t.Error("expected entity Animal with Kind=SCOPE.Component Subtype=trait")
	}
}

func TestRustExtractor_FunctionEntity(t *testing.T) {
	src := `
fn create_user(name: String) -> User {
    User { id: 1, name }
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("rust")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "func.rs",
		Content:  []byte(src),
		Language: "rust",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Name == "create_user" && e.Kind == "SCOPE.Operation" && e.Subtype == "function" {
			found = true
		}
	}
	if !found {
		t.Error("expected entity create_user with Kind=SCOPE.Operation Subtype=function")
	}
}

func TestRustExtractor_ImportRelationship(t *testing.T) {
	src := `
use std::collections::HashMap;
use serde::{Deserialize, Serialize};
use actix_web::web;

fn main() {}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("rust")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "imports.rs",
		Content:  []byte(src),
		Language: "rust",
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

	if !importTargets["std::collections::HashMap"] {
		t.Error("expected IMPORTS for std::collections::HashMap")
	}
	if !importTargets["actix_web::web"] {
		t.Error("expected IMPORTS for actix_web::web")
	}
}

func TestRustExtractor_EmptyFile(t *testing.T) {
	tree := parseForTest(t, "")
	ext, _ := extractor.Get("rust")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.rs",
		Content:  []byte(""),
		Language: "rust",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected zero entities for empty file, got %d", len(got))
	}
}

func TestRustExtractor_NilTree(t *testing.T) {
	ext, _ := extractor.Get("rust")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "nil.rs",
		Content:  []byte("fn main() {}"),
		Language: "rust",
		Tree:     nil,
	})
	if err != nil {
		t.Fatalf("unexpected error on nil tree: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected zero entities for nil tree, got %d", len(got))
	}
}

func TestRustExtractor_MalformedFile(t *testing.T) {
	src := `
struct GoodStruct {
    id: u32,
}

fn good_function() -> u32 { 1 }

fn bad_function(x: u32
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("rust")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "malformed.rs",
		Content:  []byte(src),
		Language: "rust",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error on malformed file: %v", err)
	}

	var foundGood bool
	for _, e := range got {
		if e.Name == "GoodStruct" {
			foundGood = true
		}
	}
	if !foundGood {
		t.Error("expected GoodStruct to be extracted from malformed file")
	}
}

func TestRustExtractor_UnregisteredLanguage(t *testing.T) {
	_, ok := extractor.Get("cobol")
	if ok {
		t.Error("expected false for unregistered language cobol")
	}
}

func TestRustExtractor_LineNumbers(t *testing.T) {
	src := `struct Alpha {
    id: u32,
}

fn method1() -> u32 { 1 }
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("rust")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "lines.rs",
		Content:  []byte(src),
		Language: "rust",
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
