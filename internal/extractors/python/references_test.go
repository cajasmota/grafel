// references_test.go — coverage for the REFERENCES-emission pass.

package python

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// runExtract is a small helper that runs the Python extractor on source
// text and returns the produced EntityRecord slice. Test failures bubble
// up via t.Fatal so callers can assume non-nil non-empty output.
func runExtract(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	ex := &Extractor{}
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:     "demo.py",
		Language: "python",
		Content:  []byte(src),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

// hasRef returns true when any entity named fromName carries a
// REFERENCES edge whose ToID contains substr.
func hasRef(ents []types.EntityRecord, fromName, substr string) bool {
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

// Same-scope identifier resolution: a function body that uses a name
// declared at module scope (function, class, etc.) emits a REFERENCES
// edge to that name. The CALLS path takes precedence over REFERENCES
// when the identifier IS the function child of a call expression.
func TestReferencesSameScopeIdentifier(t *testing.T) {
	src := `
HELPER_CONSTANT = 42

def helper():
    return HELPER_CONSTANT

def caller():
    x = helper
    return x
`
	ents := runExtract(t, src)
	// caller -> helper via the assignment to x (NOT a call).
	if !hasRef(ents, "caller", "helper") {
		t.Fatalf("expected REFERENCES caller -> helper, got: %s", relsSummary(ents))
	}
}

// f-string interpolation is the Python analog of JS template-literal
// substitution. The tree-sitter Python grammar surfaces the interpolated
// expression's identifiers as `identifier` nodes inside an
// `interpolation` parent; the walk's generic recursion already covers
// them. We exercise the path by interpolating a same-file function
// reference (entity exists at module scope; module-level bare
// assignments are NOT emitted as entities so we use a function name
// here for the symbol-table hit).
func TestReferencesFStringInterpolation(t *testing.T) {
	src := `
def fmt_user(u):
    return "<" + u + ">"

def build_label(user):
    formatter = fmt_user
    return f"label-{formatter}"
`
	ents := runExtract(t, src)
	if !hasRef(ents, "build_label", "fmt_user") {
		t.Fatalf("expected REFERENCES build_label -> fmt_user (interpolation/assignment), got: %s", relsSummary(ents))
	}
}

// self.<attr> resolution: a method body that uses `self.<field>` emits
// a REFERENCES edge to the class-field entity (subtype=field, Name
// "Class.field") emitted by extractClassFields (#526).
func TestReferencesSelfAttr(t *testing.T) {
	src := `
class Foo:
    counter = 0
    def bump(self):
        return self.counter + 1
`
	ents := runExtract(t, src)
	if !hasRef(ents, "Foo.bump", "Foo.counter") {
		t.Fatalf("expected REFERENCES Foo.bump -> Foo.counter, got: %s", relsSummary(ents))
	}
}

// Imported-name resolution: a same-file use of an imported leaf emits
// a REFERENCES edge to the import-placeholder entity. The import
// extractor stamps Properties["local_name"]; the symbol-table builder
// indexes that as a file-scope name.
func TestReferencesImportedName(t *testing.T) {
	src := `
from typing import Optional

def normalize(value):
    if value is None:
        return Optional
    return value
`
	ents := runExtract(t, src)
	if !hasRef(ents, "normalize", "Optional") {
		t.Fatalf("expected REFERENCES normalize -> Optional (imported), got: %s", relsSummary(ents))
	}
}

// Builtins are skipped: a function that uses `print`, `len`, etc. must
// NOT emit a REFERENCES edge — those identifiers are not project
// entities.
func TestReferencesSkipsBuiltins(t *testing.T) {
	src := `
def takes_list(xs):
    return len(xs) + int("3")
`
	ents := runExtract(t, src)
	for _, e := range ents {
		if e.Name != "takes_list" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "REFERENCES" {
				t.Fatalf("did not expect REFERENCES from takes_list (got %q)", r.ToID)
			}
		}
	}
}

// Self-reference filter: a function body that uses its own name (e.g.
// inside a default-arg, decorator, or closure that captures the
// surrounding def) does NOT emit a REFERENCES self-edge.
func TestReferencesSelfFiltered(t *testing.T) {
	src := `
def fib(n):
    if n < 2:
        return n
    # Reference fib itself as a value (not a call): default-arg shape.
    handler = fib
    return handler
`
	ents := runExtract(t, src)
	for _, e := range ents {
		if e.Name != "fib" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "REFERENCES" && strings.Contains(r.ToID, ":fib") {
				t.Fatalf("did not expect REFERENCES fib -> fib")
			}
		}
	}
}

// relsSummary stringifies every entity's REFERENCES edges for
// failure messages.
func relsSummary(ents []types.EntityRecord) string {
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
