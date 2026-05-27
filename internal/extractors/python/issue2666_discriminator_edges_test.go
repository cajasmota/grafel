// Package python_test — issue #2666: verifies that DISCRIMINATES_ON edges
// emitted by stampPythonDiscriminators carry line + literal properties.
//
// Issue #2659 added the property-only stamp; #2666 promotes the data into
// DISCRIMINATES_ON edges so inspect/find can surface line-precise hits.
package python_test

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// findEntPy returns the EntityRecord with the given Name, or nil.
func findEntPy(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

// TestDiscriminator_PythonEmitsEdge verifies that `if status == 'paid':`
// produces a DISCRIMINATES_ON edge to var:status with literal "paid" and a
// non-empty line number.
func TestDiscriminator_PythonEmitsEdge(t *testing.T) {
	src := `
def process_payment(status):
    if status == 'paid':
        return True
    return False
`
	ents := extractPy(t, src, "payments/views.py")
	e := findEntPy(ents, "process_payment")
	if e == nil {
		t.Fatal("entity 'process_payment' not found")
	}

	var found bool
	for _, r := range e.Relationships {
		if r.Kind != "DISCRIMINATES_ON" {
			continue
		}
		if r.ToID != "var:status" {
			continue
		}
		found = true
		if r.Properties == nil {
			t.Fatalf("edge has nil Properties; want line+literal")
		}
		if r.Properties["literal"] != "paid" {
			t.Errorf("Properties[literal]=%q, want %q", r.Properties["literal"], "paid")
		}
		if r.Properties["line"] == "" {
			t.Errorf("Properties[line] is empty")
		}
		// Comparison is on line 3 of the snippet (line 1 blank, line 2 def,
		// line 3 `if status == 'paid':`).
		if r.Properties["line"] != "3" {
			t.Errorf("Properties[line]=%q, want %q", r.Properties["line"], "3")
		}
	}
	if !found {
		t.Logf("relationships on process_payment:")
		for _, r := range e.Relationships {
			t.Logf("  %s → %s (props=%v)", r.Kind, r.ToID, r.Properties)
		}
		t.Errorf("DISCRIMINATES_ON edge to var:status not found")
	}
}

// TestDiscriminator_PythonNumericLiteral_EmitsEdge verifies the numeric form.
func TestDiscriminator_PythonNumericLiteral_EmitsEdge(t *testing.T) {
	src := `
def handle_response(code):
    if code == 404:
        return 'not_found'
    return 'ok'
`
	ents := extractPy(t, src, "api/views.py")
	e := findEntPy(ents, "handle_response")
	if e == nil {
		t.Fatal("entity 'handle_response' not found")
	}
	var found bool
	for _, r := range e.Relationships {
		if r.Kind == "DISCRIMINATES_ON" && r.ToID == "var:code" {
			found = true
			if r.Properties["literal"] != "404" {
				t.Errorf("Properties[literal]=%q, want %q", r.Properties["literal"], "404")
			}
			if r.Properties["line"] == "" {
				t.Errorf("Properties[line] is empty")
			}
		}
	}
	if !found {
		t.Errorf("DISCRIMINATES_ON edge to var:code not found")
	}
}

// TestDiscriminator_PythonNoEdgeWhenAbsent verifies that a function with no
// discriminator comparisons emits no DISCRIMINATES_ON edges.
func TestDiscriminator_PythonNoEdgeWhenAbsent(t *testing.T) {
	src := `
def add(a, b):
    return a + b
`
	ents := extractPy(t, src, "math/util.py")
	e := findEntPy(ents, "add")
	if e == nil {
		t.Fatal("entity 'add' not found")
	}
	for _, r := range e.Relationships {
		if strings.EqualFold(r.Kind, "DISCRIMINATES_ON") {
			t.Errorf("unexpected DISCRIMINATES_ON edge: %+v", r)
		}
	}
}
