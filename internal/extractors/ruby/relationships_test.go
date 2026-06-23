package ruby_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/ruby"
	"github.com/cajasmota/grafel/internal/types"
)

func runRuby(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("ruby")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "test.rb",
		Content:  []byte(src),
		Language: "ruby",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func rbFind(ents []types.EntityRecord, name, kind string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return &ents[i]
		}
	}
	return nil
}

func rbHasRel(ents []types.EntityRecord, name, kind, edgeKind, toID string) bool {
	e := rbFind(ents, name, kind)
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

// TestRuby_ContainsClassMethods (#41): class with N methods → N CONTAINS edges.
func TestRuby_ContainsClassMethods(t *testing.T) {
	src := `class Foo
  def a; end
  def b; end
  def c; end
end
`
	ents := runRuby(t, src)
	foo := rbFind(ents, "Foo", "SCOPE.Component")
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
	// Issue #140 — CONTAINS edges target structural-ref stubs so the
	// resolver can disambiguate same-name methods across different
	// Rails controllers (Format A: scope:operation:method:ruby:<file>:<name>).
	for _, m := range []string{"a", "b", "c"} {
		want := "scope:operation:method:ruby:test.rb:" + m
		if !rbHasRel(ents, "Foo", "SCOPE.Component", "CONTAINS", want) {
			t.Errorf("expected CONTAINS Foo→%s", want)
		}
	}
}

// TestRuby_ContainsClassMethods_OSNativePath (#140): structural-ref ToID
// must be normalized via filepath.ToSlash so OS-native separators (e.g.
// Windows backslashes) produce forward-slash refs that match the
// resolver's convention (internal/resolve/refs.go:93,785). On POSIX the
// separator is already "/" so this test exercises the no-op branch;
// constructing the input via filepath.FromSlash makes the assertion
// meaningful on every host (backslashes on Windows, slashes elsewhere).
func TestRuby_ContainsClassMethods_OSNativePath(t *testing.T) {
	src := `class Foo
  def a; end
end
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("ruby")
	nativePath := filepath.FromSlash("app/controllers/foo.rb")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     nativePath,
		Content:  []byte(src),
		Language: "ruby",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	want := "scope:operation:method:ruby:app/controllers/foo.rb:a"
	if !rbHasRel(ents, "Foo", "SCOPE.Component", "CONTAINS", want) {
		foo := rbFind(ents, "Foo", "SCOPE.Component")
		t.Errorf("expected CONTAINS Foo→%s; got rels=%+v", want, foo.Relationships)
	}
}

// TestRuby_CallsBareName (#41): method calling another method → CALLS edge.
func TestRuby_CallsBareName(t *testing.T) {
	src := `class A
  def caller
    helper
    helper
    puts "x"
  end

  def helper; end
end
`
	ents := runRuby(t, src)
	if !rbHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "helper") {
		t.Errorf("expected CALLS caller→helper")
	}
	caller := rbFind(ents, "caller", "SCOPE.Operation")
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

// TestRuby_Imports (#41): require statements emit IMPORTS module entities.
func TestRuby_Imports(t *testing.T) {
	src := `require 'json'
require_relative 'foo/bar'
class A; end
`
	ents := runRuby(t, src)
	want := map[string]bool{"json": false, "foo/bar": false}
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				if _, ok := want[r.ToID]; ok {
					want[r.ToID] = true
				}
			}
		}
	}
	for k, ok := range want {
		if !ok {
			t.Errorf("expected IMPORTS edge for %q", k)
		}
	}
}
