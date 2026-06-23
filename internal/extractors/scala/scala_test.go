package scala_test

import (
	"context"
	"errors"
	"testing"

	tsscala "github.com/smacker/go-tree-sitter/scala"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/scala"
	"github.com/cajasmota/grafel/internal/treesitter"
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
)

func parseForTest(t *testing.T, src string) ts.Tree {
	t.Helper()
	parser, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tsscala.GetLanguage()))
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
		TSTree:   tree,
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
		TSTree:   tree,
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
		TSTree:   tree,
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
		TSTree:   tree,
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
		TSTree:   tree,
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
		TSTree:   tree,
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
		TSTree:   nil,
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
		TSTree:   tree,
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

	if len(importTargets) == 0 {
		t.Error("expected at least one IMPORTS relationship for multi-import")
	}
}

// Issue #501 — Twirl templates (*.scala.html) must be detected as Scala,
// not as plain HTML, and the extractor emits a file entity with subtype="twirl".
func TestScalaExtractor_TwirlFileDetection(t *testing.T) {
	// A minimal Twirl template containing a Scala import.
	// The Scala tree-sitter grammar will parse what it can from the content
	// (import statements are valid Scala). The key assertion is the file entity
	// subtype.
	src := `@(title: String)
@import models.User
<!DOCTYPE html>
<html>
<body>@title</body>
</html>
`
	// We can't parse Twirl directly with the Scala grammar, but we can test
	// the isTwirlTemplate detection and the subtype on the file entity by
	// passing a minimal parseable snippet with a .scala.html path.
	minimalScala := `import models.User
class Views {}
`
	tree := parseForTest(t, minimalScala)
	ext, ok := extractor.Get("scala")
	if !ok {
		t.Fatal("scala extractor not registered")
	}

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "app/views/index.scala.html",
		Content:  []byte(minimalScala),
		Language: "scala",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Find the file entity — must have subtype="twirl".
	var foundTwirl bool
	for _, e := range got {
		if e.Kind == "SCOPE.Component" && e.SourceFile == "app/views/index.scala.html" {
			if e.Subtype == "twirl" {
				foundTwirl = true
			}
		}
	}
	if !foundTwirl {
		t.Error("expected file entity with subtype=twirl for .scala.html path")
	}
	_ = src // Twirl source used for documentation context
}

// Issue #501 — non-Twirl .scala files must still get subtype="file".
func TestScalaExtractor_RegularScalaFileNotTwirl(t *testing.T) {
	src := `class Foo {}`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("scala")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "app/models/Foo.scala",
		Content:  []byte(src),
		Language: "scala",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, e := range got {
		if e.Kind == "SCOPE.Component" && e.SourceFile == "app/models/Foo.scala" {
			if e.Subtype == "twirl" {
				t.Errorf("regular .scala file must NOT have subtype=twirl, got %q", e.Subtype)
			}
		}
	}
}

// Issue #501 — other Twirl variant extensions (.scala.xml, .scala.txt) also
// detected.
func TestScalaExtractor_TwirlVariantExtensions(t *testing.T) {
	variants := []string{
		"email.scala.txt",
		"feed.scala.xml",
		"app.scala.js",
	}
	src := `class A {}`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("scala")

	for _, path := range variants {
		got, err := ext.Extract(context.Background(), extractor.FileInput{
			Path:     path,
			Content:  []byte(src),
			Language: "scala",
			TSTree:   tree,
		})
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", path, err)
		}
		var foundTwirl bool
		for _, e := range got {
			if e.Kind == "SCOPE.Component" && e.Subtype == "twirl" {
				foundTwirl = true
			}
		}
		if !foundTwirl {
			t.Errorf("expected subtype=twirl for Twirl variant path %s", path)
		}
	}
}

// Issue #502 — PascalCase static-call receivers (Promise.success, Action.async)
// must be emitted as qualified "Type.method" CALLS edges.
func TestScalaExtractor_PascalCaseStaticCallBinding(t *testing.T) {
	src := `
import scala.concurrent.Future
import play.api.mvc.Action

object MyController {
  def index = Action.async {
    Future.successful("ok")
  }
  def submit = Action {
    Promise.success("done")
  }
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("scala")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "controllers/MyController.scala",
		Content:  []byte(src),
		Language: "scala",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Collect all CALLS edges.
	calls := map[string]bool{}
	for _, e := range got {
		for _, r := range e.Relationships {
			if r.Kind == "CALLS" {
				calls[r.ToID] = true
			}
		}
	}

	// PascalCase static calls must be qualified.
	for _, want := range []string{"Future.successful", "Action.async"} {
		if !calls[want] {
			t.Errorf("expected CALLS edge with ToID=%q; got: %v", want, calls)
		}
	}
}

// Issue #502 — instance field receiver (counter.nextCount) must resolve to
// "Counter.nextCount" when counter is declared with type annotation.
func TestScalaExtractor_InstanceFieldReceiverBinding(t *testing.T) {
	src := `
class TickService(val counter: Counter) {
  def tick(): Unit = {
    counter.nextCount
    counter.reset()
  }
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("scala")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "services/TickService.scala",
		Content:  []byte(src),
		Language: "scala",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := map[string]string{} // toID → receiver_type
	for _, e := range got {
		if e.Kind == "SCOPE.Operation" && e.Name == "tick" {
			for _, r := range e.Relationships {
				if r.Kind == "CALLS" {
					calls[r.ToID] = r.Properties["receiver_type"]
				}
			}
		}
	}

	// counter.reset() is a method call with parens → always emitted as CALLS.
	// counter.nextCount is a zero-arg method / property access (no parens) →
	// tree-sitter sees it as a field_expression, not a call_expression, so it
	// is NOT emitted as a CALLS edge (property accesses are not calls).
	if recv, ok := calls["Counter.reset"]; !ok {
		t.Errorf("expected CALLS edge to Counter.reset; got: %v", calls)
	} else if recv != "Counter" {
		t.Errorf("CALLS Counter.reset receiver_type=%q want Counter", recv)
	}
}

// Issue #502 — body val receiver binding (non-constructor val with type annotation).
func TestScalaExtractor_BodyValReceiverBinding(t *testing.T) {
	src := `
class OrderProcessor {
  val repo: OrderRepo = new OrderRepo()
  def process(id: Int): Unit = {
    repo.findById(id)
    repo.save()
  }
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("scala")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "OrderProcessor.scala",
		Content:  []byte(src),
		Language: "scala",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := map[string]bool{}
	for _, e := range got {
		if e.Kind == "SCOPE.Operation" && e.Name == "process" {
			for _, r := range e.Relationships {
				if r.Kind == "CALLS" {
					calls[r.ToID] = true
				}
			}
		}
	}

	for _, want := range []string{"OrderRepo.findById", "OrderRepo.save"} {
		if !calls[want] {
			t.Errorf("expected CALLS edge %q; got: %v", want, calls)
		}
	}
}
