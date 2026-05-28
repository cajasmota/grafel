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
		// Issue #145: methods declared inside a class body are emitted
		// with Name="<Class>.<method>" so two classes with same-named
		// methods produce distinct entity IDs.
		if e.Name == "Svc.getName" && e.Kind == "SCOPE.Operation" && e.Subtype == "method" {
			found = true
		}
	}
	if !found {
		t.Error("expected entity Svc.getName with Kind=SCOPE.Operation Subtype=method")
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
	_, ok := extractor.Get("fortran")
	if ok {
		t.Error("expected false for unregistered language fortran")
	}
}

// TestPHPExtractor_UseStatementImports covers issue #102: every PHP
// `use` statement should emit an IMPORTS edge whose ToID is the FQN
// of the imported symbol. Without this the synth allowlist never sees
// the Symfony / Doctrine roots and they all land in bug-extractor.
func TestPHPExtractor_UseStatementImports(t *testing.T) {
	src := `<?php

namespace App\Form;

use App\Entity\Post;
use Symfony\Component\Form\AbstractType;
use Symfony\Component\Form\FormBuilderInterface as FBI;
use function Symfony\Component\String\u;
use const Symfony\Component\HttpFoundation\Cookie\SAMESITE_LAX;
use Symfony\Component\HttpFoundation\{Request, Response};

class PostType extends AbstractType {}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("php")
	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "src/Form/PostType.php",
		Content:  []byte(src),
		Language: "php",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantImports := map[string]bool{
		"App\\Entity\\Post":                                        false,
		"Symfony\\Component\\Form\\AbstractType":                   false,
		"Symfony\\Component\\Form\\FormBuilderInterface":           false,
		"Symfony\\Component\\String\\u":                            false,
		"Symfony\\Component\\HttpFoundation\\Cookie\\SAMESITE_LAX": false,
		"Symfony\\Component\\HttpFoundation\\Request":              false,
		"Symfony\\Component\\HttpFoundation\\Response":             false,
	}
	for _, e := range got {
		for _, r := range e.Relationships {
			if r.Kind != "IMPORTS" {
				continue
			}
			if _, ok := wantImports[r.ToID]; ok {
				wantImports[r.ToID] = true
			}
		}
	}
	for fqn, seen := range wantImports {
		if !seen {
			t.Errorf("expected IMPORTS edge to %q, not found", fqn)
		}
	}
}

// TestPHPExtractor_UseImportsCarryProperties (#113): PHP IMPORTS edges
// must carry the same Properties contract Python (#93) and Java (#120)
// emit so the cross-file resolver's per-file binding table can be
// built. For `use App\Entity\Post;` local_name="Post",
// source_module="App.Entity" (slashes normalized to dots),
// imported_name="Post". Aliased forms drop the alias at FQN extraction;
// the leaf identifier of the canonical FQN is still what local_name
// records.
func TestPHPExtractor_UseImportsCarryProperties(t *testing.T) {
	src := `<?php

namespace App\Form;

use App\Entity\Post;
use Symfony\Component\Form\AbstractType;
use Symfony\Component\HttpFoundation\{Request, Response};

class PostType extends AbstractType {}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("php")
	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "src/Form/PostType.php",
		Content:  []byte(src),
		Language: "php",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]map[string]string{
		"App\\Entity\\Post": {
			"local_name":    "Post",
			"source_module": "App.Entity",
			"imported_name": "Post",
		},
		"Symfony\\Component\\Form\\AbstractType": {
			"local_name":    "AbstractType",
			"source_module": "Symfony.Component.Form",
			"imported_name": "AbstractType",
		},
		"Symfony\\Component\\HttpFoundation\\Request": {
			"local_name":    "Request",
			"source_module": "Symfony.Component.HttpFoundation",
			"imported_name": "Request",
		},
		"Symfony\\Component\\HttpFoundation\\Response": {
			"local_name":    "Response",
			"source_module": "Symfony.Component.HttpFoundation",
			"imported_name": "Response",
		},
	}
	gotProps := map[string]map[string]string{}
	for _, e := range got {
		for _, r := range e.Relationships {
			if r.Kind != "IMPORTS" {
				continue
			}
			gotProps[r.ToID] = r.Properties
		}
	}
	for to, wantP := range want {
		gp, ok := gotProps[to]
		if !ok {
			t.Errorf("expected IMPORTS edge to=%q, got=%v", to, gotProps)
			continue
		}
		for k, v := range wantP {
			if gp[k] != v {
				t.Errorf("IMPORTS to=%q prop %q: got=%q want=%q (all=%v)",
					to, k, gp[k], v, gp)
			}
		}
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

// TestPHPExtractor_ClassContains_TwoClassesSameMethod verifies issue
// #145: two PHP classes in the same file each declaring a method with
// the same bare name produce distinct method entities AND each class
// carries a CONTAINS edge whose ToID is a Format-A structural-ref
// keyed on the source file + the dotted Class.method Name.
func TestPHPExtractor_ClassContains_TwoClassesSameMethod(t *testing.T) {
	src := `<?php
class UserRepo {
    public function find(int $id): string { return ""; }
}
class OrderRepo {
    public function find(int $id): string { return ""; }
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("php")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "repos.php",
		Content:  []byte(src),
		Language: "php",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Distinct method entity IDs (dotted Name + SourceFile/Kind/Name
	// → ComputeID hash).
	methodIDs := map[string]string{}
	for _, e := range got {
		if e.Kind != "SCOPE.Operation" || e.Subtype != "method" {
			continue
		}
		id := e.ComputeID()
		if existing, dup := methodIDs[id]; dup {
			t.Errorf("method ID collision: %q and %q both compute to %s",
				existing, e.Name, id)
		}
		methodIDs[id] = e.Name
	}
	if len(methodIDs) != 2 {
		t.Fatalf("expected 2 distinct method entities, got %d (%v)",
			len(methodIDs), methodIDs)
	}

	// Each class must own a CONTAINS edge with the canonical
	// structural-ref ToID for its method.
	wantContains := map[string]string{
		"UserRepo":  extractor.BuildOperationStructuralRef("php", "repos.php", "UserRepo.find"),
		"OrderRepo": extractor.BuildOperationStructuralRef("php", "repos.php", "OrderRepo.find"),
	}
	for _, e := range got {
		if e.Kind != "SCOPE.Component" || e.Subtype != "class" {
			continue
		}
		want, expected := wantContains[e.Name]
		if !expected {
			continue
		}
		var gotEdges []string
		for _, rel := range e.Relationships {
			if rel.Kind == "CONTAINS" {
				gotEdges = append(gotEdges, rel.ToID)
			}
		}
		if len(gotEdges) != 1 {
			t.Errorf("class %s: expected 1 CONTAINS edge, got %d (%v)",
				e.Name, len(gotEdges), gotEdges)
			continue
		}
		if gotEdges[0] != want {
			t.Errorf("class %s: CONTAINS ToID = %q, want %q",
				e.Name, gotEdges[0], want)
		}
		delete(wantContains, e.Name)
	}
	for name := range wantContains {
		t.Errorf("class %s: not found in extracted entities", name)
	}
}
