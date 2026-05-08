package elixir_test

import (
	"context"
	"errors"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tselixir "github.com/smacker/go-tree-sitter/elixir"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/elixir"
	"github.com/cajasmota/archigraph/internal/treesitter"
)

func parseForTest(t *testing.T, src string) *sitter.Tree {
	t.Helper()
	parser := sitter.NewParser()
	parser.SetLanguage(tselixir.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, []byte(src))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	return tree
}

func TestElixirExtractor_BasicExtraction(t *testing.T) {
	src := `
defmodule SampleApi.UserController do
  use SampleApi.Web, :controller
  alias SampleApi.User

  def index(conn, _params) do
    users = Repo.all(User)
    render(conn, "index.json", users: users)
  end

  defp private_helper(x), do: x + 1
end

defprotocol MyProtocol do
  def process(data)
end
`
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("elixir")
	if !ok {
		t.Fatal("elixir extractor not registered")
	}

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "user_controller.ex",
		Content:  []byte(src),
		Language: "elixir",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var modules, protocols, funcs, privFuncs, imports int
	for _, e := range got {
		switch {
		case e.Kind == "SCOPE.Component" && e.Subtype == "module":
			modules++
		case e.Kind == "SCOPE.Component" && e.Subtype == "protocol":
			protocols++
		case e.Kind == "SCOPE.Operation" && e.Subtype == "function":
			funcs++
		case e.Kind == "SCOPE.Operation" && e.Subtype == "private_function":
			privFuncs++
		case e.Kind == "SCOPE.Component" && len(e.Relationships) > 0:
			imports++
		}
	}

	if modules == 0 {
		t.Error("expected at least one module entity")
	}
	if protocols == 0 {
		t.Error("expected at least one protocol entity")
	}
	if funcs == 0 {
		t.Error("expected at least one function entity")
	}
	if privFuncs == 0 {
		t.Error("expected at least one private_function entity")
	}
	if imports == 0 {
		t.Error("expected at least one import entity")
	}
}

func TestElixirExtractor_ModuleEntity(t *testing.T) {
	src := `
defmodule Foo.Bar do
end
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("elixir")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "foo.ex",
		Content:  []byte(src),
		Language: "elixir",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Kind == "SCOPE.Component" && e.Subtype == "module" {
			found = true
			if e.SourceFile != "foo.ex" {
				t.Errorf("expected source_file foo.ex, got %s", e.SourceFile)
			}
			if e.Language != "elixir" {
				t.Errorf("expected language elixir, got %s", e.Language)
			}
			if e.StartLine == 0 {
				t.Error("expected non-zero start_line")
			}
		}
	}
	if !found {
		t.Error("expected a module entity")
	}
}

func TestElixirExtractor_FunctionEntity(t *testing.T) {
	src := `
defmodule MyMod do
  def hello(name) do
    "Hello, " <> name
  end
end
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("elixir")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "mod.ex",
		Content:  []byte(src),
		Language: "elixir",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Kind == "SCOPE.Operation" && e.Name == "hello" {
			found = true
			if e.Subtype != "function" {
				t.Errorf("expected subtype function, got %s", e.Subtype)
			}
		}
	}
	if !found {
		t.Error("expected entity hello with Kind=SCOPE.Operation Subtype=function")
	}
}

func TestElixirExtractor_PrivateFunctionEntity(t *testing.T) {
	src := `
defmodule MyMod do
  defp secret(x), do: x * 2
end
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("elixir")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "mod.ex",
		Content:  []byte(src),
		Language: "elixir",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Kind == "SCOPE.Operation" && e.Name == "secret" {
			found = true
			if e.Subtype != "private_function" {
				t.Errorf("expected subtype private_function, got %s", e.Subtype)
			}
		}
	}
	if !found {
		t.Error("expected entity secret with Kind=SCOPE.Operation Subtype=private_function")
	}
}

func TestElixirExtractor_ImportRelationship(t *testing.T) {
	src := `
defmodule Foo do
  alias SampleApi.User
  import Ecto.Query
  use Phoenix.Controller
end
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("elixir")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "foo.ex",
		Content:  []byte(src),
		Language: "elixir",
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
		t.Error("expected at least one IMPORTS relationship")
	}
}

func TestElixirExtractor_EmptyFile(t *testing.T) {
	tree := parseForTest(t, "")
	ext, _ := extractor.Get("elixir")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.ex",
		Content:  []byte(""),
		Language: "elixir",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected zero entities for empty file, got %d", len(got))
	}
}

func TestElixirExtractor_NilTree(t *testing.T) {
	ext, _ := extractor.Get("elixir")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "nil.ex",
		Content:  []byte("defmodule Foo do\nend"),
		Language: "elixir",
		Tree:     nil,
	})
	if err != nil {
		t.Fatalf("unexpected error on nil tree: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected zero entities for nil tree, got %d", len(got))
	}
}

func TestElixirExtractor_MissingGrammarReturnsErrUnsupportedLanguage(t *testing.T) {
	factory := treesitter.NewParserFactory(nil)
	_, err := factory.Parse(context.Background(), []byte("defmodule Foo do\nend"), "dart")
	if err == nil {
		t.Fatal("expected ErrUnsupportedLanguage for dart, got nil")
	}
	if !errors.Is(err, treesitter.ErrUnsupportedLanguage) {
		t.Errorf("expected ErrUnsupportedLanguage, got: %v", err)
	}
}

func TestElixirExtractor_ProtocolEntity(t *testing.T) {
	src := `
defprotocol Printable do
  def print(data)
end
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("elixir")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "printable.ex",
		Content:  []byte(src),
		Language: "elixir",
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
		t.Error("expected a protocol entity")
	}
}
