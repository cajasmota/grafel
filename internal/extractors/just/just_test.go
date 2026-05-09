package just_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/just"
	"github.com/cajasmota/archigraph/internal/types"
)

func TestJustExtractor_Registered(t *testing.T) {
	_, ok := extractor.Get("just")
	if !ok {
		t.Fatal("just extractor not registered")
	}
}

func TestJustExtractor_Recipes(t *testing.T) {
	src := `build:
    go build ./...

test: build
    go test ./...

deploy env="prod": test
    ./deploy.sh {{env}}
`
	ext, _ := extractor.Get("just")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "justfile",
		Content:  []byte(src),
		Language: "just",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	recipes := map[string]string{}
	for _, e := range entities {
		if e.Subtype != "recipe" {
			continue
		}
		recipes[e.Name] = e.Properties["dependencies"]
		if e.Kind != "SCOPE.Operation" {
			t.Errorf("recipe %q: expected Kind=SCOPE.Operation, got %q", e.Name, e.Kind)
		}
		if e.Language != "just" {
			t.Errorf("recipe %q: expected Language=just, got %q", e.Name, e.Language)
		}
		if e.StartLine < 1 {
			t.Errorf("recipe %q: StartLine must be >=1, got %d", e.Name, e.StartLine)
		}
	}
	for _, want := range []string{"build", "test", "deploy"} {
		if _, ok := recipes[want]; !ok {
			t.Errorf("expected recipe %q to be extracted", want)
		}
	}
	if recipes["test"] != "build" {
		t.Errorf("expected test.dependencies=build, got %q", recipes["test"])
	}
	if recipes["deploy"] != "test" {
		t.Errorf("expected deploy.dependencies=test, got %q", recipes["deploy"])
	}
	if recipes["build"] != "" {
		t.Errorf("expected build.dependencies empty, got %q", recipes["build"])
	}
}

func TestJustExtractor_Variables(t *testing.T) {
	src := `IMAGE := "myapp"
TAG := "latest"
export GO_VERSION := "1.24"
`
	ext, _ := extractor.Get("just")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "justfile",
		Content:  []byte(src),
		Language: "just",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	vars := map[string]bool{}
	for _, e := range entities {
		if e.Subtype == "variable" {
			vars[e.Name] = true
			if e.Kind != "SCOPE.Schema" {
				t.Errorf("variable %q: expected Kind=SCOPE.Schema, got %q", e.Name, e.Kind)
			}
			if !strings.Contains(e.Signature, ":=") {
				t.Errorf("variable %q: signature should contain :=, got %q", e.Name, e.Signature)
			}
		}
	}
	for _, want := range []string{"IMAGE", "TAG", "GO_VERSION"} {
		if !vars[want] {
			t.Errorf("expected variable %q to be extracted", want)
		}
	}
}

func TestJustExtractor_DependenciesWithArguments(t *testing.T) {
	// Dep with parenthesised arguments: deps should strip the parens.
	src := `release: test (lint "strict") (vet)
    echo "ok"
`
	ext, _ := extractor.Get("just")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "justfile",
		Content:  []byte(src),
		Language: "just",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Filter to recipe entities — the extractor also emits a file-level
	// SCOPE.Component container that carries CONTAINS edges (issue #374).
	var recipes []types.EntityRecord
	for _, e := range entities {
		if e.Subtype == "recipe" {
			recipes = append(recipes, e)
		}
	}
	if len(recipes) != 1 {
		t.Fatalf("expected 1 recipe entity, got %d", len(recipes))
	}
	entities = recipes
	deps := entities[0].Properties["dependencies"]
	if deps != "test" {
		t.Errorf("expected deps='test' (args stripped), got %q", deps)
	}
}

func TestJustExtractor_MultipleDependencies(t *testing.T) {
	src := `ci: build test lint vet
    echo "done"
`
	ext, _ := extractor.Get("just")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "justfile",
		Content:  []byte(src),
		Language: "just",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Filter to recipe entities — the extractor also emits a file-level
	// SCOPE.Component container that carries CONTAINS edges (issue #374).
	var recipes []types.EntityRecord
	for _, e := range entities {
		if e.Subtype == "recipe" {
			recipes = append(recipes, e)
		}
	}
	if len(recipes) != 1 {
		t.Fatalf("expected 1 recipe entity, got %d", len(recipes))
	}
	entities = recipes
	deps := entities[0].Properties["dependencies"]
	if deps != "build,test,lint,vet" {
		t.Errorf("expected deps=build,test,lint,vet got %q", deps)
	}
}

func TestJustExtractor_RecipeParameters(t *testing.T) {
	src := `deploy env="staging" region="us-east-1":
    echo {{env}} {{region}}
`
	ext, _ := extractor.Get("just")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "justfile",
		Content:  []byte(src),
		Language: "just",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Filter to recipe entities — the extractor also emits a file-level
	// SCOPE.Component container that carries CONTAINS edges (issue #374).
	var recipes []types.EntityRecord
	for _, e := range entities {
		if e.Subtype == "recipe" {
			recipes = append(recipes, e)
		}
	}
	if len(recipes) != 1 {
		t.Fatalf("expected 1 recipe entity, got %d", len(recipes))
	}
	entities = recipes
	e := entities[0]
	if e.Name != "deploy" {
		t.Errorf("expected name=deploy, got %q", e.Name)
	}
	// Parameters are recorded as params (not deps).
	if !strings.Contains(e.Properties["parameters"], "env") {
		t.Errorf("expected parameters property to contain 'env', got %q", e.Properties["parameters"])
	}
}

func TestJustExtractor_UnderscoreRecipe(t *testing.T) {
	src := `_internal:
    echo "hidden"
`
	ext, _ := extractor.Get("just")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "justfile",
		Content:  []byte(src),
		Language: "just",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var recipes []types.EntityRecord
	for _, e := range entities {
		if e.Subtype == "recipe" {
			recipes = append(recipes, e)
		}
	}
	if len(recipes) != 1 || recipes[0].Name != "_internal" {
		t.Errorf("expected _internal recipe, got %+v", recipes)
	}
}

func TestJustExtractor_ReservedKeywords_NotRecipes(t *testing.T) {
	src := `set shell := ["bash", "-c"]
set dotenv-load := true

build:
    echo "real recipe"
`
	ext, _ := extractor.Get("just")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "justfile",
		Content:  []byte(src),
		Language: "just",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range entities {
		if e.Name == "set" {
			t.Errorf("'set' should not be extracted as recipe or variable")
		}
	}
	var gotBuild bool
	for _, e := range entities {
		if e.Name == "build" && e.Subtype == "recipe" {
			gotBuild = true
		}
	}
	if !gotBuild {
		t.Error("expected 'build' recipe to be extracted alongside set directives")
	}
}

func TestJustExtractor_EmptyInput(t *testing.T) {
	ext, _ := extractor.Get("just")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty",
		Content:  []byte{},
		Language: "just",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) != 0 {
		t.Errorf("expected 0 entities, got %d", len(entities))
	}
}

func TestJustExtractor_MalformedInput_NoPanic(t *testing.T) {
	// Half-written recipe line — extractor must never raise.
	src := "nocolon_here\nbroken := \n"
	ext, _ := extractor.Get("just")
	if _, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "bad",
		Content:  []byte(src),
		Language: "just",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestJustExtractor_RecipeEndLine(t *testing.T) {
	src := `build:
    echo "line1"
    echo "line2"
    echo "line3"

test:
    echo "next"
`
	ext, _ := extractor.Get("just")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "justfile",
		Content:  []byte(src),
		Language: "just",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var build *struct {
		Start int
		End   int
	}
	for _, e := range entities {
		if e.Name == "build" {
			build = &struct {
				Start int
				End   int
			}{e.StartLine, e.EndLine}
		}
	}
	if build == nil {
		t.Fatal("build recipe not extracted")
	}
	if build.Start != 1 {
		t.Errorf("expected build.StartLine=1, got %d", build.Start)
	}
	if build.End != 4 {
		t.Errorf("expected build.EndLine=4 (last indented body line), got %d", build.End)
	}
}

func TestJustExtractor_RealWorldFixture(t *testing.T) {
	path := filepath.Join("..", "..", "..", "fixtures", "real-world", "just", "Justfile")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ext, _ := extractor.Get("just")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  data,
		Language: "just",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var recipeCount, varCount int
	for _, e := range entities {
		switch e.Subtype {
		case "recipe":
			recipeCount++
		case "variable":
			varCount++
		}
	}
	if recipeCount < 8 {
		t.Errorf("expected >=8 recipes in fixture Justfile, got %d", recipeCount)
	}
	if varCount < 3 {
		t.Errorf("expected >=3 variables in fixture Justfile, got %d", varCount)
	}
	t.Logf("fixture Justfile: %d recipes, %d variables, %d total entities", recipeCount, varCount, len(entities))
}

func TestJustExtractor_LanguageMethod(t *testing.T) {
	ext, _ := extractor.Get("just")
	if got := ext.Language(); got != "just" {
		t.Errorf("Language() = %q, want %q", got, "just")
	}
}
