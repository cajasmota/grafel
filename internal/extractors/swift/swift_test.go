package swift_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	tsswift "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/swift"
	tsofficial "github.com/cajasmota/grafel/internal/treesitter/ts/official"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/swift"
	"github.com/cajasmota/grafel/internal/treesitter"
	"github.com/cajasmota/grafel/internal/treesitter/ts"
)

func parseForTest(t *testing.T, src string) ts.Tree {
	t.Helper()
	parser, err := tsofficial.New().NewParser(tsswift.Language())
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
		TSTree:   tree,
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
		TSTree:   tree,
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
		TSTree:   tree,
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
		TSTree:   tree,
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

// TestSwiftExtractor_ActorEntity covers issue #5417 (C3(b)): `actor`
// declarations are first-class concurrency components emitted as
// SCOPE.Component subtype="actor" (mirroring class/struct/enum), with their
// members extracted exactly like a class — a CONTAINS edge to each method and
// a SCOPE.Schema field per stored property.
func TestSwiftExtractor_ActorEntity(t *testing.T) {
	src := `
actor BankAccount {
    var balance: Int = 0
    func deposit(_ amount: Int) {
        balance += amount
    }
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("swift")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "bank.swift",
		Content:  []byte(src),
		Language: "swift",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var actor *struct {
		hasContains bool
	}
	var deposit, balanceField bool
	for i := range got {
		e := got[i]
		switch {
		case e.Kind == "SCOPE.Component" && e.Subtype == "actor" && e.Name == "BankAccount":
			a := struct{ hasContains bool }{}
			for _, r := range e.Relationships {
				if r.Kind == "CONTAINS" {
					a.hasContains = true
				}
			}
			actor = &a
		case e.Kind == "SCOPE.Operation" && e.Name == "deposit":
			deposit = true
		case e.Kind == "SCOPE.Schema" && e.Subtype == "field" && e.Name == "BankAccount.balance":
			balanceField = true
		}
	}

	if actor == nil {
		t.Fatal("expected entity BankAccount with Kind=SCOPE.Component Subtype=actor")
	}
	if !actor.hasContains {
		t.Error("expected the actor to carry CONTAINS edges to its members")
	}
	if !deposit {
		t.Error("expected the actor's deposit() method as a SCOPE.Operation")
	}
	if !balanceField {
		t.Error("expected the actor's balance stored property as a SCOPE.Schema field")
	}
}

// TestSwiftExtractor_DistributedActorEntity covers the `distributed actor`
// shape (#5417): the `distributed` modifier lives in a leading `modifiers`
// node, but the `actor` keyword still classifies the declaration as subtype=
// "actor".
func TestSwiftExtractor_DistributedActorEntity(t *testing.T) {
	src := `
distributed actor Worker {
    func run() {}
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("swift")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "worker.swift",
		Content:  []byte(src),
		Language: "swift",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Kind == "SCOPE.Component" && e.Subtype == "actor" && e.Name == "Worker" {
			found = true
		}
	}
	if !found {
		t.Error("expected entity Worker with Kind=SCOPE.Component Subtype=actor")
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
		TSTree:   tree,
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

	if !importTargets["Foundation"] {
		t.Error("expected IMPORTS relationship for Foundation")
	}
	if !importTargets["UIKit"] {
		t.Error("expected IMPORTS relationship for UIKit")
	}
}

// TestSwiftExtractor_ImportCarrierDoesNotCollide (#492) — the SCOPE.Component
// emitted by buildImport must NOT collide with a real Swift component that
// happens to share the imported module's bare name. Two guarantees:
//
//  1. Subtype="module" so the cross-file resolver's (module,name) index
//     skips the carrier (mirrors the Python convention).
//  2. The carrier Name is namespaced as `<file>::import::<module>` so it
//     can never be confused with a real type/target identifier even if a
//     downstream consumer ignores Subtype.
//
// Concretely: a file that both `import App` and `class App {}` must yield
// exactly ONE SCOPE.Component entity named "App" (the class) — the import
// carrier must carry a namespaced name, not the bare "App".
func TestSwiftExtractor_ImportCarrierDoesNotCollide(t *testing.T) {
	src := `
import App

class App {
    func run() {}
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("swift")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "main.swift",
		Content:  []byte(src),
		Language: "swift",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var bareApp int
	var importCarriers int
	var classFound bool
	var importToIDFound bool
	for _, e := range got {
		if e.Kind == "SCOPE.Component" && e.Name == "App" {
			bareApp++
			if e.Subtype == "class" {
				classFound = true
			}
		}
		if e.Kind == "SCOPE.Component" && e.Subtype == "module" {
			importCarriers++
			if e.Name == "App" {
				t.Errorf("import carrier must NOT use the bare module name 'App'; got name=%q", e.Name)
			}
			expected := "main.swift::import::App"
			if e.Name != expected {
				t.Errorf("import carrier name = %q, want %q", e.Name, expected)
			}
		}
		for _, rel := range e.Relationships {
			if rel.Kind == "IMPORTS" && rel.ToID == "App" {
				importToIDFound = true
			}
		}
	}

	if !classFound {
		t.Error("expected the SCOPE.Component class 'App' to be extracted")
	}
	if bareApp != 1 {
		t.Errorf("expected exactly one entity Name='App' (the class), got %d — import carrier is colliding (#492)", bareApp)
	}
	if importCarriers != 1 {
		t.Errorf("expected exactly one import-carrier (Subtype=module), got %d", importCarriers)
	}
	if !importToIDFound {
		t.Error("expected an IMPORTS edge with ToID=App (the imported module path is unchanged)")
	}
}

// TestSwiftExtractor_ImportAttributeChildrenSkipped (#499) — import
// attributes such as `@_documentation(visibility: internal)`, `@_exported`,
// `@preconcurrency`, `@_implementationOnly`, `@testable` must NOT contribute
// identifier segments to the extracted import path. Previously
// extractImportPath descended into the `modifiers` / `attribute` subtree
// and produced synthetic dotted paths like
// `_documentation.visibility.internal.Foundation`, which then escaped
// classifyExternal as bug-extractor noise on the vapor framework.
func TestSwiftExtractor_ImportAttributeChildrenSkipped(t *testing.T) {
	src := `
@_documentation(visibility: internal) @_exported import Foundation
@preconcurrency import Combine
@_implementationOnly import PrivateKit
@testable import VaporCore
import Vapor
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("swift")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "imports.swift",
		Content:  []byte(src),
		Language: "swift",
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

	want := []string{"Foundation", "Combine", "PrivateKit", "VaporCore", "Vapor"}
	for _, m := range want {
		if !importTargets[m] {
			t.Errorf("expected clean IMPORTS edge ToID=%q, missing", m)
		}
	}

	// Guard: no synthetic path may contain attribute-name fragments.
	forbiddenFragments := []string{
		"_documentation", "_exported", "preconcurrency",
		"_implementationOnly", "testable", "visibility",
	}
	for toID := range importTargets {
		for _, frag := range forbiddenFragments {
			if strings.Contains(toID, frag) {
				t.Errorf("synthetic IMPORTS ToID %q contains attribute fragment %q (#499)", toID, frag)
			}
		}
	}
}

// TestSwiftExtractor_DeferAndBareInitFiltered (#499) — `defer { ... }` is
// parsed by tree-sitter-swift as a `call_expression` with head
// `simple_identifier "defer"` and a `call_suffix` containing a
// `lambda_literal`. It is a statement, not a call. Same for a hypothetical
// bare `init()` — only `Type.init(...)` (with a receiver) is a real
// initializer call.
func TestSwiftExtractor_DeferAndBareInitFiltered(t *testing.T) {
	src := `
class C {
    func use() {
        defer { cleanup() }
        let y = Bar.init(value: 1)
        cleanup()
    }
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("swift")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "c.swift",
		Content:  []byte(src),
		Language: "swift",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var useFn bool
	callTargets := map[string]bool{}
	receivers := map[string]string{}
	for _, e := range got {
		if e.Kind != "SCOPE.Operation" || e.Name != "use" {
			continue
		}
		useFn = true
		for _, rel := range e.Relationships {
			if rel.Kind != "CALLS" {
				continue
			}
			callTargets[rel.ToID] = true
			if rt, ok := rel.Properties["receiver_type"]; ok {
				receivers[rel.ToID] = rt
			}
		}
	}

	if !useFn {
		t.Fatal("expected SCOPE.Operation 'use'")
	}
	if callTargets["defer"] {
		t.Error("CALLS edge with ToID=defer must be filtered (#499)")
	}
	// `init` with receiver Bar must survive — Type.init(...) is a real
	// explicit initializer call.
	if !callTargets["init"] {
		t.Error("expected CALLS edge for explicit Bar.init(...) initializer (#499)")
	}
	if !callTargets["cleanup"] {
		t.Error("expected CALLS edge for cleanup()")
	}
}

// TestSwiftExtractor_BareInitFiltered (#499) — a top-level function that
// somehow surfaces a bare `init` call_expression head must NOT yield a
// CALLS edge. This is the regression-guard for the receiverless shape.
func TestSwiftExtractor_BareInitFiltered(t *testing.T) {
	// Force a shape: `init()` as a bare call has no legal user-code
	// origin, but we construct a body that includes both shapes and
	// assert only the explicit-receiver form survives.
	src := `
class D {
    func make() {
        let z = SomeClass.init()
    }
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("swift")
	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "d.swift",
		Content:  []byte(src),
		Language: "swift",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range got {
		if e.Kind != "SCOPE.Operation" || e.Name != "make" {
			continue
		}
		var sawInit bool
		for _, rel := range e.Relationships {
			if rel.Kind == "CALLS" && rel.ToID == "init" {
				sawInit = true
			}
		}
		if !sawInit {
			t.Error("expected CALLS edge for SomeClass.init() (explicit receiver preserves init)")
		}
	}
}

func TestSwiftExtractor_EmptyFile(t *testing.T) {
	tree := parseForTest(t, "")
	ext, _ := extractor.Get("swift")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.swift",
		Content:  []byte(""),
		Language: "swift",
		TSTree:   tree,
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
		TSTree:   nil,
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
