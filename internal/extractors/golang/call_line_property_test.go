// call_line_property_test.go — regression tests for #2638.
//
// Verifies that every CALLS RelationshipRecord emitted by the Go extractor
// carries a non-zero Properties["line"] value.
package golang_test

import (
	"strconv"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// findCalls returns all CALLS relationships on the named entity.
func findCalls(ents []interface{}, name string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, raw := range ents {
		ent, ok := raw.(types.EntityRecord)
		if !ok {
			continue
		}
		if ent.Name != name {
			continue
		}
		for _, r := range ent.Relationships {
			if r.Kind == "CALLS" {
				out = append(out, r)
			}
		}
	}
	return out
}

// TestExtractor_CallEdge_HasLineProperty asserts that a simple function-to-function
// call emits a CALLS edge with a non-zero Properties["line"] value.
func TestExtractor_CallEdge_HasLineProperty(t *testing.T) {
	src := `package main

func helper() {}

func caller() {
	helper()
}
`
	ents, err := extractFrom(src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	calls := findCalls(ents, "caller")
	if len(calls) == 0 {
		t.Fatal("no CALLS edges on 'caller'")
	}

	var found bool
	for _, r := range calls {
		if r.ToID == "helper" || (r.Properties != nil && r.Properties["line"] != "") {
			found = true
			lineStr, ok := r.Properties["line"]
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
		t.Fatal("CALLS edge to 'helper' not found (or missing line) on 'caller'")
	}
}

// TestExtractor_CallEdge_CorrectLineNumber validates the exact line number.
// The call to bar() appears on line 6.
func TestExtractor_CallEdge_CorrectLineNumber(t *testing.T) {
	// line 1: package main
	// line 2: (blank)
	// line 3: func bar() {}
	// line 4: (blank)
	// line 5: func foo() {
	// line 6: 	bar()
	// line 7: }
	src := "package main\n\nfunc bar() {}\n\nfunc foo() {\n\tbar()\n}\n"

	ents, err := extractFrom(src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	calls := findCalls(ents, "foo")
	for _, r := range calls {
		if r.ToID == "bar" || (r.Properties != nil && r.Properties["line"] != "") {
			lineStr, ok := r.Properties["line"]
			if !ok {
				t.Fatal("CALLS edge missing Properties[\"line\"]")
			}
			if lineStr != "6" {
				t.Errorf("Properties[\"line\"] = %q, want \"6\"", lineStr)
			}
			return
		}
	}
	t.Fatal("CALLS edge to 'bar' not found on 'foo'")
}

// TestExtractor_AllCallEdges_HaveLineProperty asserts that every emitted CALLS
// edge carries Properties["line"] with a valid positive integer.
func TestExtractor_AllCallEdges_HaveLineProperty(t *testing.T) {
	src := `package main

func a() {}

func b() {
	a()
}

func c() {
	a()
	b()
}
`
	ents, err := extractFrom(src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	for _, raw := range ents {
		ent, ok := raw.(types.EntityRecord)
		if !ok {
			continue
		}
		for _, r := range ent.Relationships {
			if r.Kind != "CALLS" {
				continue
			}
			lineStr, ok := r.Properties["line"]
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
