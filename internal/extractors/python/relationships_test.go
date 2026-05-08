package python_test

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
	// Blank import to trigger init() registration.
	_ "github.com/cajasmota/archigraph/internal/extractors/python"
)

// findEntity returns the first entity with the given name, or nil.
func findEntity(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

// hasRel reports whether ents[*].Relationships contains a (kind, toID) match
// on any entity in the slice.
func hasRel(ents []types.EntityRecord, fromName, kind, toID string) bool {
	src := findEntity(ents, fromName)
	if src == nil {
		return false
	}
	for _, r := range src.Relationships {
		if r.Kind == kind && r.ToID == toID {
			return true
		}
	}
	return false
}

func countRel(ents []types.EntityRecord, fromName, kind string) int {
	src := findEntity(ents, fromName)
	if src == nil {
		return 0
	}
	n := 0
	for _, r := range src.Relationships {
		if r.Kind == kind {
			n++
		}
	}
	return n
}

// TestExtract_ContainsClassMethods covers the regression for issue #25:
// every method declared inside a class body must produce exactly one
// CONTAINS edge from the class entity to the method entity.
func TestExtract_ContainsClassMethods(t *testing.T) {
	src := `class Foo:
    def a(self):
        pass

    def b(self, x):
        return x

    def c(self):
        pass
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")
	ents, err := ext.Extract(context.Background(), makeFile(src, tree))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if c := countRel(ents, "Foo", "CONTAINS"); c != 3 {
		t.Errorf("expected 3 CONTAINS edges from Foo, got %d (rels=%+v)",
			c, findEntity(ents, "Foo").Relationships)
	}
	for _, m := range []string{"a", "b", "c"} {
		if !hasRel(ents, "Foo", "CONTAINS", m) {
			t.Errorf("expected CONTAINS Foo→%s", m)
		}
	}
}

// TestExtract_CallsBareName covers the CALLS regression for issue #25.
// A function calling another function must produce exactly one CALLS edge
// whose ToID is the bare callee name (a stub the resolver can rewrite to a
// deterministic ID across files).
func TestExtract_CallsBareName(t *testing.T) {
	src := `def helper():
    return 1

def caller():
    helper()
    helper()  # duplicate calls collapse to one edge
    print("x")
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")
	ents, err := ext.Extract(context.Background(), makeFile(src, tree))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if !hasRel(ents, "caller", "CALLS", "helper") {
		t.Errorf("expected CALLS caller→helper (stub to_id 'helper'), got %+v",
			findEntity(ents, "caller").Relationships)
	}
	if !hasRel(ents, "caller", "CALLS", "print") {
		t.Errorf("expected CALLS caller→print")
	}
	// Dedup: only one edge to helper despite two call sites.
	n := 0
	for _, r := range findEntity(ents, "caller").Relationships {
		if r.Kind == "CALLS" && r.ToID == "helper" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("expected 1 CALLS caller→helper after dedup, got %d", n)
	}
}

// TestExtract_CallsAttributeTrailing covers selector-style calls. The Python
// extractor uses the trailing identifier of an attribute expression, mirroring
// the Go extractor's "fmt.Println" → "Println" rule.
func TestExtract_CallsAttributeTrailing(t *testing.T) {
	src := `def f():
    obj.method(1, 2)
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")
	ents, _ := ext.Extract(context.Background(), makeFile(src, tree))

	if !hasRel(ents, "f", "CALLS", "method") {
		t.Errorf("expected CALLS f→method (trailing attribute), got %+v",
			findEntity(ents, "f").Relationships)
	}
}

// TestExtract_ImportsSimple covers `import x` and `import x.y`.
func TestExtract_ImportsSimple(t *testing.T) {
	src := `import os
import os.path
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")
	ents, _ := ext.Extract(context.Background(), makeFile(src, tree))

	want := map[string]bool{"os": false, "os.path": false}
	for _, e := range ents {
		if e.Subtype == "module" {
			if _, ok := want[e.Name]; ok {
				want[e.Name] = true
			}
			if len(e.Relationships) != 1 || e.Relationships[0].Kind != "IMPORTS" {
				t.Errorf("import entity %q missing IMPORTS edge: %+v", e.Name, e.Relationships)
			}
		}
	}
	for k, ok := range want {
		if !ok {
			t.Errorf("expected import entity for %q", k)
		}
	}
}

// TestExtract_ImportsFromSymbol covers `from x.y import a, b`. Each imported
// symbol is emitted as its own module entity with name "x.y.a" / "x.y.b".
func TestExtract_ImportsFromSymbol(t *testing.T) {
	src := `from collections import OrderedDict, defaultdict
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")
	ents, _ := ext.Extract(context.Background(), makeFile(src, tree))

	want := []string{"collections.OrderedDict", "collections.defaultdict"}
	for _, w := range want {
		found := false
		for _, e := range ents {
			if e.Subtype == "module" && e.Name == w {
				found = true
				if len(e.Relationships) != 1 || e.Relationships[0].Kind != "IMPORTS" {
					t.Errorf("entity %q missing IMPORTS edge: %+v", w, e.Relationships)
				}
				break
			}
		}
		if !found {
			t.Errorf("expected import entity for %q", w)
		}
	}
}

// TestExtract_NoSelfCall confirms self-recursion is not emitted as a CALLS edge.
func TestExtract_NoSelfCall(t *testing.T) {
	src := `def fact(n):
    if n <= 1:
        return 1
    return n * fact(n - 1)
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")
	ents, _ := ext.Extract(context.Background(), makeFile(src, tree))

	for _, r := range findEntity(ents, "fact").Relationships {
		if r.Kind == "CALLS" && r.ToID == "fact" {
			t.Errorf("self-recursion should not produce a CALLS edge: %+v", r)
		}
	}
}
