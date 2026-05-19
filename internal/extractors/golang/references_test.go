// references_test.go — coverage for the REFERENCES-emission pass
// (analog of #641/#650/#670 for Go).

package golang

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// runGoExtract is a small helper that runs the Go extractor on source
// text and returns the produced EntityRecord slice. Test failures
// bubble up via t.Fatal so callers can assume non-nil non-empty output.
func runGoExtract(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	ex := &GoExtractor{}
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:     "demo.go",
		Language: "go",
		Content:  []byte(src),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

// hasGoRef returns true when any entity named fromName carries a
// REFERENCES edge whose ToID contains substr.
func hasGoRef(ents []types.EntityRecord, fromName, substr string) bool {
	for _, e := range ents {
		if e.Name != fromName {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "REFERENCES" && strings.Contains(r.ToID, substr) {
				return true
			}
		}
	}
	return false
}

// goRelsSummary stringifies every entity's REFERENCES edges for
// failure messages.
func goRelsSummary(ents []types.EntityRecord) string {
	var b strings.Builder
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "REFERENCES" {
				b.WriteString(e.Name)
				b.WriteString(" -> ")
				b.WriteString(r.ToID)
				b.WriteString("; ")
			}
		}
	}
	if b.Len() == 0 {
		return "(no REFERENCES)"
	}
	return b.String()
}

// Same-scope identifier resolution: a function body that uses the name
// of a sibling type declared in the same file emits a REFERENCES edge.
// The identifier is used as a value (not in declaration position and
// not the callee of a call_expression).
func TestGoReferencesSameScope(t *testing.T) {
	src := `package demo

type Helper struct{}

func builder() interface{} {
	var h Helper
	return h
}
`
	ents := runGoExtract(t, src)
	if !hasGoRef(ents, "builder", "Helper") {
		t.Fatalf("expected REFERENCES builder -> Helper, got: %s", goRelsSummary(ents))
	}
}

// Receiver-method `r.<field>` resolution: a method body that uses
// `r.field` emits a REFERENCES edge keyed by the receiver's struct
// type. We bind to the Component (struct) entity (the field lives on it).
func TestGoReferencesReceiverField(t *testing.T) {
	src := `package demo

type Box struct {
	counter int
}

func (b *Box) Bump() int {
	return b.counter + 1
}
`
	ents := runGoExtract(t, src)
	if !hasGoRef(ents, "Box.Bump", "Box") {
		t.Fatalf("expected REFERENCES Box.Bump -> Box (via b.counter), got: %s", goRelsSummary(ents))
	}
}

// Package-level type referenced inside a function: the function uses
// the type in a non-call position (e.g. a cast or composite literal
// type). Emits a REFERENCES edge to the Component entity.
func TestGoReferencesCompositeLiteralType(t *testing.T) {
	src := `package demo

type Config struct {
	Path string
}

func defaults() Config {
	c := Config{Path: "/tmp"}
	return c
}
`
	ents := runGoExtract(t, src)
	if !hasGoRef(ents, "defaults", "Config") {
		t.Fatalf("expected REFERENCES defaults -> Config, got: %s", goRelsSummary(ents))
	}
}

// Imported-name reference: a function body that uses `time.Now` in a
// non-call position emits a REFERENCES edge to the `time` import
// placeholder (the symbol-table indexes imports by their local
// package name).
//
// Note: the extractor's bare-name walker fires on the `time` operand
// of the selector_expression. The full import path is stored as the
// entity Name; this test asserts the import placeholder is bound.
func TestGoReferencesImportedName(t *testing.T) {
	src := `package demo

import "time"

func clock() interface{} {
	fn := time.Now
	return fn
}
`
	ents := runGoExtract(t, src)
	// The Go REFERENCES emitter rewrites the import ToID via Track B
	// to "ext:time", but the REFERENCES edge from clock points to the
	// import entity's Name (the path "time"). We assert the substring
	// "time" appears in some REFERENCES edge sourced from clock.
	if !hasGoRef(ents, "clock", "time") {
		t.Fatalf("expected REFERENCES clock -> time, got: %s", goRelsSummary(ents))
	}
}

// Go builtins (`len`, `cap`, `make`, `new`, `panic`, `int`, `string`,
// `error`, `nil`, `iota`, `_`) and reserved words are filtered. A
// function that only uses builtins must emit no REFERENCES edges.
func TestGoReferencesSkipsBuiltins(t *testing.T) {
	src := `package demo

func takesSlice(xs []int) int {
	if xs == nil {
		return 0
	}
	return len(xs) + cap(xs)
}
`
	ents := runGoExtract(t, src)
	for _, e := range ents {
		if e.Name != "takesSlice" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "REFERENCES" {
				t.Fatalf("did not expect REFERENCES from takesSlice (got %q)", r.ToID)
			}
		}
	}
}

// Self-reference filter: a function body that uses its own name (e.g.
// in a default-value expression, as a function-value assignment, or
// reflexively as a callback) does NOT emit a REFERENCES self-edge.
func TestGoReferencesSelfFiltered(t *testing.T) {
	src := `package demo

func recur() interface{} {
	// Use recur as a value (NOT a call): assigning the function to a
	// variable. The CALLS path doesn't fire, so the REFERENCES path
	// must explicitly filter the self-reference.
	fn := recur
	return fn
}
`
	ents := runGoExtract(t, src)
	for _, e := range ents {
		if e.Name != "recur" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "REFERENCES" && strings.Contains(r.ToID, ":recur") {
				t.Fatalf("did not expect REFERENCES recur -> recur (got %q)", r.ToID)
			}
		}
	}
}

// Blank identifier `_` and the underscore in interface-satisfaction
// checks (`var _ MyInterface = &Impl{}`) must never produce a
// REFERENCES edge sourced from the blank.
//
// In Go, `var _ I = &S{}` at package level is not enclosed by a
// function, so emitReferences is a no-op for the bare `_`. We DO want
// REFERENCES edges from any USAGE inside a function body to skip the
// blank — verified via a function-local usage.
func TestGoReferencesBlankIdentifierSkipped(t *testing.T) {
	src := `package demo

type Sayer interface {
	Say() string
}

type impl struct{}

func (impl) Say() string { return "hi" }

func factory() interface{} {
	var s Sayer = impl{}
	_ = s
	return s
}
`
	ents := runGoExtract(t, src)
	// factory should reference Sayer + impl but never the blank "_".
	for _, e := range ents {
		if e.Name != "factory" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "REFERENCES" && strings.HasSuffix(r.ToID, ":_") {
				t.Fatalf("did not expect REFERENCES factory -> _ (got %q)", r.ToID)
			}
		}
	}
	if !hasGoRef(ents, "factory", "Sayer") {
		t.Fatalf("expected REFERENCES factory -> Sayer, got: %s", goRelsSummary(ents))
	}
}

// Method expression `T.M` reference: `Foo.M` used as a value (a
// function reference) emits REFERENCES edges to both `Foo` (the type)
// and `Foo.M` (the method).
func TestGoReferencesMethodExpression(t *testing.T) {
	src := `package demo

type Foo struct{}

func (Foo) M() string { return "x" }

func register() interface{} {
	fn := Foo.M
	return fn
}
`
	ents := runGoExtract(t, src)
	if !hasGoRef(ents, "register", "Foo") {
		t.Fatalf("expected REFERENCES register -> Foo, got: %s", goRelsSummary(ents))
	}
	if !hasGoRef(ents, "register", "Foo.M") {
		t.Fatalf("expected REFERENCES register -> Foo.M, got: %s", goRelsSummary(ents))
	}
}
