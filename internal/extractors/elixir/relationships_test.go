package elixir_test

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/elixir"
	"github.com/cajasmota/archigraph/internal/types"
)

// runElixir parses src with the real elixir grammar and returns extracted entities.
func runElixir(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("elixir")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "test.ex",
		Content:  []byte(src),
		Language: "elixir",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func exFind(ents []types.EntityRecord, name, kind string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return &ents[i]
		}
	}
	return nil
}

func exHasRel(ents []types.EntityRecord, name, kind, edgeKind, toID string) bool {
	e := exFind(ents, name, kind)
	if e == nil {
		return false
	}
	for _, r := range e.Relationships {
		if r.Kind == edgeKind && r.ToID == toID {
			return true
		}
	}
	return false
}

// TestElixir_ContainsModuleFunctions (#370): defmodule attaches one
// CONTAINS edge per def/defp declared inside the body, with the canonical
// structural-ref shape `scope:operation:method:elixir:<file>:<name>`.
func TestElixir_ContainsModuleFunctions(t *testing.T) {
	src := `defmodule Foo do
  def a, do: :ok
  def b(x), do: x
  defp c, do: :ok
end
`
	ents := runElixir(t, src)
	foo := exFind(ents, "Foo", "SCOPE.Component")
	if foo == nil {
		t.Fatal("expected Foo component")
	}
	contains := 0
	for _, r := range foo.Relationships {
		if r.Kind == "CONTAINS" {
			contains++
		}
	}
	if contains != 3 {
		t.Errorf("expected 3 CONTAINS edges, got %d (rels=%+v)", contains, foo.Relationships)
	}
	for _, m := range []string{"a", "b", "c"} {
		want := "scope:operation:method:elixir:test.ex:" + m
		if !exHasRel(ents, "Foo", "SCOPE.Component", "CONTAINS", want) {
			t.Errorf("expected CONTAINS Foo→%s", want)
		}
	}
}

// TestElixir_ContainsProtocolFunctions: defprotocol bodies contain function
// declarations too — they get CONTAINS edges with the same structural-ref shape.
func TestElixir_ContainsProtocolFunctions(t *testing.T) {
	src := `defprotocol Printable do
  def print(data)
end
`
	ents := runElixir(t, src)
	p := exFind(ents, "Printable", "SCOPE.Component")
	if p == nil || p.Subtype != "protocol" {
		t.Fatalf("expected Printable protocol, got %+v", p)
	}
	want := "scope:operation:method:elixir:test.ex:print"
	if !exHasRel(ents, "Printable", "SCOPE.Component", "CONTAINS", want) {
		t.Errorf("expected CONTAINS Printable→%s", want)
	}
}

// TestElixir_CallsBareName: bare function calls inside a function body
// produce CALLS edges with the simple identifier as ToID, deduped.
func TestElixir_CallsBareName(t *testing.T) {
	src := `defmodule M do
  def caller do
    helper()
    helper()
    other_thing()
  end
  def helper, do: :ok
  def other_thing, do: :ok
end
`
	ents := runElixir(t, src)
	if !exHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "helper") {
		t.Errorf("expected CALLS caller→helper")
	}
	if !exHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "other_thing") {
		t.Errorf("expected CALLS caller→other_thing")
	}
	caller := exFind(ents, "caller", "SCOPE.Operation")
	if caller == nil {
		t.Fatal("expected caller op")
	}
	n := 0
	for _, r := range caller.Relationships {
		if r.Kind == "CALLS" && r.ToID == "helper" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("expected dedup CALLS caller→helper to 1, got %d", n)
	}
}

// TestElixir_CallsDottedTrailingIdentifier: dotted calls like `Repo.all(User)`
// must emit CALLS to the trailing identifier ("all"), NOT the receiver alias.
func TestElixir_CallsDottedTrailingIdentifier(t *testing.T) {
	src := `defmodule M do
  def caller do
    Repo.all(User)
    String.downcase(name)
    Map.get(map, key)
  end
end
`
	ents := runElixir(t, src)
	caller := exFind(ents, "caller", "SCOPE.Operation")
	if caller == nil {
		t.Fatal("expected caller op")
	}
	want := map[string]bool{"all": false, "downcase": false, "get": false}
	forbidden := map[string]bool{"Repo": true, "String": true, "Map": true}
	for _, r := range caller.Relationships {
		if r.Kind != "CALLS" {
			continue
		}
		if forbidden[r.ToID] {
			t.Errorf("alias receiver %q must not be emitted as CALLS target", r.ToID)
		}
		if _, ok := want[r.ToID]; ok {
			want[r.ToID] = true
		}
	}
	for k, v := range want {
		if !v {
			t.Errorf("expected CALLS caller→%s", k)
		}
	}
}

// TestElixir_CallsKeywordsFiltered: Elixir control-flow keywords and
// def-defining macros must NOT appear as CALLS targets.
func TestElixir_CallsKeywordsFiltered(t *testing.T) {
	src := `defmodule M do
  def caller(x) do
    if x > 0 do
      helper()
    else
      other()
    end
    case x do
      :ok -> done()
      _ -> nope()
    end
  end
  def helper, do: :ok
  def other, do: :ok
  def done, do: :ok
  def nope, do: :ok
end
`
	ents := runElixir(t, src)
	caller := exFind(ents, "caller", "SCOPE.Operation")
	if caller == nil {
		t.Fatal("expected caller op")
	}
	for _, r := range caller.Relationships {
		if r.Kind != "CALLS" {
			continue
		}
		switch r.ToID {
		case "if", "unless", "case", "cond", "with", "for", "do", "fn",
			"def", "defp", "defmodule", "alias", "import", "use", "require":
			t.Errorf("elixir keyword %q must not be emitted as CALLS target", r.ToID)
		}
	}
	// Real calls inside the conditional branches are still emitted.
	for _, want := range []string{"helper", "other", "done", "nope"} {
		if !exHasRel(ents, "caller", "SCOPE.Operation", "CALLS", want) {
			t.Errorf("expected CALLS caller→%s", want)
		}
	}
}

// TestElixir_CallsDropSelfRecursion: a function calling itself does NOT
// emit a CALLS edge to its own name.
func TestElixir_CallsDropSelfRecursion(t *testing.T) {
	src := `defmodule M do
  def loop(x) do
    loop(x - 1)
  end
end
`
	ents := runElixir(t, src)
	loop := exFind(ents, "loop", "SCOPE.Operation")
	if loop == nil {
		t.Fatal("expected loop op")
	}
	for _, r := range loop.Relationships {
		if r.Kind == "CALLS" && r.ToID == "loop" {
			t.Errorf("self-recursion must not produce CALLS edge")
		}
	}
}

// TestElixir_ImportProperties: IMPORTS edges carry the property contract
// (local_name, source_module, imported_name, import_kind) matching the
// Java/Python schema, for all four import forms.
func TestElixir_ImportProperties(t *testing.T) {
	src := `defmodule Foo do
  alias SampleApi.User
  import Ecto.Query
  use Phoenix.Controller
  require Logger
end
`
	ents := runElixir(t, src)
	type want struct {
		local, mod, kind string
	}
	expect := map[string]want{
		"SampleApi.User":      {"User", "SampleApi", "alias"},
		"Ecto.Query":          {"Query", "Ecto", "import"},
		"Phoenix.Controller":  {"Controller", "Phoenix", "use"},
		"Logger":              {"Logger", "Logger", "require"},
	}
	seen := map[string]bool{}
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind != "IMPORTS" {
				continue
			}
			w, ok := expect[r.ToID]
			if !ok {
				continue
			}
			seen[r.ToID] = true
			if r.FromID != "test.ex" {
				t.Errorf("IMPORTS %s: expected FromID=test.ex, got %q", r.ToID, r.FromID)
			}
			if r.Properties["local_name"] != w.local {
				t.Errorf("IMPORTS %s: local_name=%q want %q", r.ToID, r.Properties["local_name"], w.local)
			}
			if r.Properties["source_module"] != w.mod {
				t.Errorf("IMPORTS %s: source_module=%q want %q", r.ToID, r.Properties["source_module"], w.mod)
			}
			if r.Properties["imported_name"] != w.local {
				t.Errorf("IMPORTS %s: imported_name=%q want %q", r.ToID, r.Properties["imported_name"], w.local)
			}
			if r.Properties["import_kind"] != w.kind {
				t.Errorf("IMPORTS %s: import_kind=%q want %q", r.ToID, r.Properties["import_kind"], w.kind)
			}
		}
	}
	for k := range expect {
		if !seen[k] {
			t.Errorf("expected IMPORTS edge for %s", k)
		}
	}
}

// TestElixir_ImportSingleSegment: bare `alias Foo` (no dot) — local_name
// and source_module both equal the full identifier.
func TestElixir_ImportSingleSegment(t *testing.T) {
	src := `defmodule M do
  alias Foo
end
`
	ents := runElixir(t, src)
	var found bool
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind != "IMPORTS" || r.ToID != "Foo" {
				continue
			}
			found = true
			if r.Properties["local_name"] != "Foo" {
				t.Errorf("local_name=%q want Foo", r.Properties["local_name"])
			}
			if r.Properties["source_module"] != "Foo" {
				t.Errorf("source_module=%q want Foo", r.Properties["source_module"])
			}
		}
	}
	if !found {
		t.Error("expected IMPORTS edge for Foo")
	}
}

// TestElixir_LanguageTagging: every emitted relationship carries
// Properties["language"]="elixir" so the resolver picks the right
// dynamic-pattern catalog (issue #90).
func TestElixir_LanguageTagging(t *testing.T) {
	src := `defmodule M do
  alias Foo.Bar
  def caller do
    helper()
  end
  def helper, do: :ok
end
`
	ents := runElixir(t, src)
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Properties["language"] != "elixir" {
				t.Errorf("entity %q rel %s→%s missing language tag (props=%+v)",
					e.Name, r.Kind, r.ToID, r.Properties)
			}
		}
	}
}
