// call_line_property_test.go — regression tests for #2636.
//
// Verifies that every CALLS RelationshipRecord emitted by the Python extractor
// carries a non-zero Properties["line"] value.
package python

import (
	"strconv"
	"testing"
)

// TestExtractor_CallEdge_HasLineProperty asserts that a simple function-to-function
// call emits a CALLS edge with a non-zero line number.
func TestExtractor_CallEdge_HasLineProperty(t *testing.T) {
	src := `def helper():
    pass

def caller():
    helper()
`
	ents := extractEntities(t, "test.py", src)

	calls := findCallsFrom(ents, "caller")
	if len(calls) == 0 {
		t.Fatal("no CALLS edges on 'caller'")
	}

	var found bool
	for _, r := range calls {
		if r.ToID == "helper" {
			found = true
			lineStr, ok := r.Properties.Lookup("line")
			if !ok {
				t.Fatal("CALLS edge missing Properties[\"line\"]")
			}
			n, err := strconv.Atoi(lineStr)
			if err != nil {
				t.Fatalf("Properties[\"line\"] = %q is not a valid integer: %v", lineStr, err)
			}
			if n <= 0 {
				t.Errorf("Properties[\"line\"] = %d, want > 0", n)
			}
		}
	}
	if !found {
		t.Fatal("CALLS edge to 'helper' not found on 'caller'")
	}
}

// TestExtractor_CallEdge_CorrectLineNumber validates the exact line number.
// The call to bar() appears on line 5.
func TestExtractor_CallEdge_CorrectLineNumber(t *testing.T) {
	// line 1: def bar():
	// line 2:     pass
	// line 3: (blank)
	// line 4: def foo():
	// line 5:     bar()
	src := "def bar():\n    pass\n\ndef foo():\n    bar()\n"

	ents := extractEntities(t, "test.py", src)

	calls := findCallsFrom(ents, "foo")
	for _, r := range calls {
		if r.ToID == "bar" {
			lineStr, ok := r.Properties.Lookup("line")
			if !ok {
				t.Fatal("CALLS edge missing Properties[\"line\"]")
			}
			if lineStr != "5" {
				t.Errorf("Properties[\"line\"] = %q, want \"5\"", lineStr)
			}
			return
		}
	}
	t.Fatal("CALLS edge to 'bar' not found on 'foo'")
}

// TestExtractor_AllCallEdges_HaveLineProperty asserts that every emitted CALLS
// edge carries Properties["line"] with a valid positive integer.
func TestExtractor_AllCallEdges_HaveLineProperty(t *testing.T) {
	src := `def a():
    pass

def b():
    a()

def c():
    a()
    b()
`
	ents := extractEntities(t, "test.py", src)

	for _, ent := range ents {
		for _, r := range ent.Relationships {
			if r.Kind != "CALLS" {
				continue
			}
			lineStr, ok := r.Properties.Lookup("line")
			if !ok {
				t.Errorf("entity %q: CALLS edge to %q missing Properties[\"line\"]", ent.Name, r.ToID)
				continue
			}
			n, err := strconv.Atoi(lineStr)
			if err != nil || n <= 0 {
				t.Errorf("entity %q: CALLS edge to %q has invalid line %q", ent.Name, r.ToID, lineStr)
			}
		}
	}
}
