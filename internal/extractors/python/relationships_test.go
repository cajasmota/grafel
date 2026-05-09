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
	// Issue #144 — CONTAINS targets are structural-ref stubs (Format A:
	// scope:operation:method:python:<file>:<name>) so the resolver can
	// disambiguate same-named methods across files. Methods carry the
	// class-qualified Name "Foo.<method>" (issue #45), which appears as
	// the trailing :<name> segment of the structural-ref.
	for _, m := range []string{"Foo.a", "Foo.b", "Foo.c"} {
		want := "scope:operation:method:python:test.py:" + m
		if !hasRel(ents, "Foo", "CONTAINS", want) {
			t.Errorf("expected CONTAINS Foo→%s", want)
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

// relProperty returns the value of a property on the first relationship
// matching (kind, toID) on the named entity, or "" when not found.
func relProperty(ents []types.EntityRecord, fromName, kind, toID, key string) string {
	src := findEntity(ents, fromName)
	if src == nil {
		return ""
	}
	for _, r := range src.Relationships {
		if r.Kind == kind && r.ToID == toID {
			return r.Properties[key]
		}
	}
	return ""
}

// TestExtract_CallsReceiverTypeBinding covers issue #69. Two classes declare
// a same-named method `process`; a third class's method calls both via
// constructor expressions `A().process()` and `B().process()`. Both calls
// must bind to distinct dotted method targets ("A.process", "B.process")
// rather than collapsing to the bare leaf "process" — which would otherwise
// either drop as self-recursion (when the caller is also named `process`)
// or resolve ambiguously across the two same-named entities.
func TestExtract_CallsReceiverTypeBinding(t *testing.T) {
	src := `class A:
    def process(self, x):
        return x

class B:
    def process(self, x):
        return x + 1

class Driver:
    def process(self, x):
        a = A().process(x)
        b = B().process(x)
        return a + b

    def run(self, x):
        return self.process(x)
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")
	ents, err := ext.Extract(context.Background(), makeFile(src, tree))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// Driver.process → A.process and Driver.process → B.process must both
	// be present as distinct CALLS edges. Critically, even though the caller
	// is itself named `process`, the bare-leaf self-recursion guard must NOT
	// drop the receiver-typed targets.
	if !hasRel(ents, "Driver.process", "CALLS", "A.process") {
		t.Errorf("expected CALLS Driver.process→A.process, got %+v",
			findEntity(ents, "Driver.process").Relationships)
	}
	if !hasRel(ents, "Driver.process", "CALLS", "B.process") {
		t.Errorf("expected CALLS Driver.process→B.process, got %+v",
			findEntity(ents, "Driver.process").Relationships)
	}
	// Bare "process" must NOT be emitted — both calls have inferable
	// receiver types and should be qualified.
	if hasRel(ents, "Driver.process", "CALLS", "process") {
		t.Errorf("did not expect bare CALLS Driver.process→process; "+
			"receiver-typed calls should bind dotted: %+v",
			findEntity(ents, "Driver.process").Relationships)
	}
	// Receiver-typed targets must not carry the ambiguous hint.
	if h := relProperty(ents, "Driver.process", "CALLS", "A.process",
		"disposition_hint"); h != "" {
		t.Errorf("A.process edge should not be marked ambiguous, got %q", h)
	}

	// Driver.run calls self.process — qualifies to Driver.process. Since
	// the caller is `run` (not `process`), the edge is a real intra-class
	// call and must be emitted.
	if !hasRel(ents, "Driver.run", "CALLS", "Driver.process") {
		t.Errorf("expected CALLS Driver.run→Driver.process (self.process), "+
			"got %+v", findEntity(ents, "Driver.run").Relationships)
	}
}

// TestExtract_CallsAmbiguousReceiver covers the fallback path: a method call
// on an unknown-typed receiver (parameter `obj`) must still produce a
// CALLS edge to the bare leaf, but tagged with disposition_hint=ambiguous
// so the resolver can classify the edge correctly when it cannot bind.
func TestExtract_CallsAmbiguousReceiver(t *testing.T) {
	src := `class C:
    def run(self, obj):
        obj.foo()
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")
	ents, _ := ext.Extract(context.Background(), makeFile(src, tree))

	if !hasRel(ents, "C.run", "CALLS", "foo") {
		t.Errorf("expected fallback CALLS C.run→foo (bare leaf), got %+v",
			findEntity(ents, "C.run").Relationships)
	}
	if h := relProperty(ents, "C.run", "CALLS", "foo",
		"disposition_hint"); h != "ambiguous" {
		t.Errorf("expected disposition_hint=ambiguous on bare-leaf edge, got %q", h)
	}
}

// TestExtract_SelfRecursionStillDropped guards the original self-recursion
// drop after the issue #69 receiver-type fix. `self.process()` inside
// `class C.process` resolves to C.process — i.e. true self-recursion — and
// must continue to drop, matching the Go extractor and the legacy Python
// indexer dedup semantics.
func TestExtract_SelfRecursionStillDropped(t *testing.T) {
	src := `class C:
    def process(self, n):
        if n <= 0:
            return 0
        return self.process(n - 1)
`
	tree := parse(t, []byte(src))
	ext, _ := extractor.Get("python")
	ents, _ := ext.Extract(context.Background(), makeFile(src, tree))

	for _, r := range findEntity(ents, "C.process").Relationships {
		if r.Kind == "CALLS" && (r.ToID == "process" || r.ToID == "C.process") {
			t.Errorf("self-recursion should not produce a CALLS edge: %+v", r)
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
