package scala_test

import (
	"context"
	"errors"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tsscala "github.com/smacker/go-tree-sitter/scala"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/scala"
	"github.com/cajasmota/archigraph/internal/treesitter"
)

func parseForTest(t *testing.T, src string) *sitter.Tree {
	t.Helper()
	parser := sitter.NewParser()
	parser.SetLanguage(tsscala.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, []byte(src))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	return tree
}

func TestScalaExtractor_BasicExtraction(t *testing.T) {
	src := `
package com.example

import scala.collection.mutable
import scala.util.{Try, Success, Failure}

case class User(id: Int, name: String)

class UserService {
  def findAll(): List[User] = List.empty
  def findById(id: Int): Option[User] = None
}

object UserService {
  def apply(): UserService = new UserService()
}

trait Repository[T] {
  def findById(id: Int): Option[T]
  def findAll(): List[T]
}
`
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("scala")
	if !ok {
		t.Fatal("scala extractor not registered")
	}

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "service.scala",
		Content:  []byte(src),
		Language: "scala",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var classes, traits, objects, funcs, imports int
	for _, e := range got {
		switch {
		case e.Kind == "SCOPE.Component" && (e.Subtype == "class" || e.Subtype == "case_class"):
			classes++
		case e.Kind == "SCOPE.Component" && e.Subtype == "trait":
			traits++
		case e.Kind == "SCOPE.Component" && e.Subtype == "object":
			objects++
		case e.Kind == "SCOPE.Operation":
			funcs++
		case e.Kind == "SCOPE.Component" && len(e.Relationships) > 0:
			imports++
		}
	}

	if classes == 0 {
		t.Error("expected at least one class entity")
	}
	if traits == 0 {
		t.Error("expected at least one trait entity")
	}
	if objects == 0 {
		t.Error("expected at least one object entity")
	}
	if funcs == 0 {
		t.Error("expected at least one function entity")
	}
	if imports == 0 {
		t.Error("expected at least one import entity")
	}
}

func TestScalaExtractor_ClassEntity(t *testing.T) {
	src := `
class Foo {
  def bar(): String = "hello"
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("scala")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "foo.scala",
		Content:  []byte(src),
		Language: "scala",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Name == "Foo" && e.Kind == "SCOPE.Component" && e.Subtype == "class" {
			found = true
			if e.SourceFile != "foo.scala" {
				t.Errorf("expected source_file foo.scala, got %s", e.SourceFile)
			}
			if e.Language != "scala" {
				t.Errorf("expected language scala, got %s", e.Language)
			}
		}
	}
	if !found {
		t.Error("expected entity Foo with Kind=SCOPE.Component Subtype=class")
	}
}

func TestScalaExtractor_TraitEntity(t *testing.T) {
	src := `
trait Serializable {
  def serialize(): String
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("scala")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "serializable.scala",
		Content:  []byte(src),
		Language: "scala",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Kind == "SCOPE.Component" && e.Subtype == "trait" {
			found = true
		}
	}
	if !found {
		t.Error("expected a trait SCOPE.Component entity")
	}
}

func TestScalaExtractor_ObjectEntity(t *testing.T) {
	src := `
object Config {
  val host = "localhost"
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("scala")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "config.scala",
		Content:  []byte(src),
		Language: "scala",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Kind == "SCOPE.Component" && e.Subtype == "object" {
			found = true
		}
	}
	if !found {
		t.Error("expected an object SCOPE.Component entity")
	}
}

func TestScalaExtractor_FunctionEntity(t *testing.T) {
	src := `
class MathHelper {
  def add(a: Int, b: Int): Int = a + b
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("scala")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "math.scala",
		Content:  []byte(src),
		Language: "scala",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Kind == "SCOPE.Operation" && e.Name == "add" {
			found = true
			if e.Subtype != "function" {
				t.Errorf("expected subtype function, got %s", e.Subtype)
			}
		}
	}
	if !found {
		t.Error("expected entity add with Kind=SCOPE.Operation Subtype=function")
	}
}

func TestScalaExtractor_ImportRelationship(t *testing.T) {
	src := `
import scala.collection.mutable
import scala.util.{Try, Success, Failure}

class Foo {}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("scala")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "imports.scala",
		Content:  []byte(src),
		Language: "scala",
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

	if !importTargets["scala.collection.mutable"] {
		t.Error("expected IMPORTS relationship for scala.collection.mutable")
	}
}

func TestScalaExtractor_EmptyFile(t *testing.T) {
	tree := parseForTest(t, "")
	ext, _ := extractor.Get("scala")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.scala",
		Content:  []byte(""),
		Language: "scala",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected zero entities for empty file, got %d", len(got))
	}
}

func TestScalaExtractor_NilTree(t *testing.T) {
	ext, _ := extractor.Get("scala")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "nil.scala",
		Content:  []byte("class Foo {}"),
		Language: "scala",
		Tree:     nil,
	})
	if err != nil {
		t.Fatalf("unexpected error on nil tree: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected zero entities for nil tree, got %d", len(got))
	}
}

func TestScalaExtractor_MissingGrammarReturnsErrUnsupportedLanguage(t *testing.T) {
	factory := treesitter.NewParserFactory(nil)
	_, err := factory.Parse(context.Background(), []byte("class Foo {}"), "dart")
	if err == nil {
		t.Fatal("expected ErrUnsupportedLanguage for dart, got nil")
	}
	if !errors.Is(err, treesitter.ErrUnsupportedLanguage) {
		t.Errorf("expected ErrUnsupportedLanguage, got: %v", err)
	}
}

func TestScalaExtractor_CaseClassEntity(t *testing.T) {
	src := `
case class Point(x: Double, y: Double)
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("scala")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "point.scala",
		Content:  []byte(src),
		Language: "scala",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// case class may be represented as case_class_definition or class_definition
	// depending on grammar version — accept either subtype.
	var found bool
	for _, e := range got {
		if e.Kind == "SCOPE.Component" && (e.Subtype == "case_class" || e.Subtype == "class") {
			found = true
		}
	}
	if !found {
		t.Error("expected a case class entity")
	}
}

func TestScalaExtractor_MultiImport(t *testing.T) {
	src := `
import scala.util.{Try, Success, Failure}

object App {}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("scala")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "multi.scala",
		Content:  []byte(src),
		Language: "scala",
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

	if len(importTargets) == 0 {
		t.Error("expected at least one IMPORTS relationship for multi-import")
	}
}
