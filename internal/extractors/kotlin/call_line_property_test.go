// call_line_property_test.go — regression tests for #2638.
//
// Verifies that every CALLS RelationshipRecord emitted by the Kotlin extractor
// carries a non-zero Properties["line"] value.
package kotlin_test

import (
	"strconv"
	"testing"
)

// TestExtractor_CallEdge_HasLineProperty asserts that a function-to-function
// call emits a CALLS edge with a non-zero Properties["line"] value.
func TestExtractor_CallEdge_HasLineProperty(t *testing.T) {
	src := `fun helper() {}

fun caller() {
    helper()
}
`
	ents := runKotlin(t, src)

	var found bool
	for _, ent := range ents {
		if ent.Name != "caller" {
			continue
		}
		for _, r := range ent.Relationships {
			if r.Kind != "CALLS" {
				continue
			}
			found = true
			lineStr, ok := r.Properties["line"]
			if !ok {
				t.Fatalf("CALLS edge to %q missing Properties[\"line\"]", r.ToID)
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
		t.Fatal("no CALLS edges found on 'caller'")
	}
}

// TestExtractor_CallEdge_CorrectLineNumber validates the exact line number.
// The call to bar() appears on line 4.
func TestExtractor_CallEdge_CorrectLineNumber(t *testing.T) {
	// line 1: fun bar() {}
	// line 2: (blank)
	// line 3: fun foo() {
	// line 4:     bar()
	// line 5: }
	src := "fun bar() {}\n\nfun foo() {\n    bar()\n}\n"

	ents := runKotlin(t, src)

	for _, ent := range ents {
		if ent.Name != "foo" {
			continue
		}
		for _, r := range ent.Relationships {
			if r.Kind != "CALLS" {
				continue
			}
			lineStr, ok := r.Properties["line"]
			if !ok {
				t.Fatal("CALLS edge missing Properties[\"line\"]")
			}
			if lineStr != "4" {
				t.Errorf("Properties[\"line\"] = %q, want \"4\"", lineStr)
			}
			return
		}
	}
	t.Fatal("CALLS edge not found on 'foo'")
}

// TestExtractor_AllCallEdges_HaveLineProperty asserts that every emitted CALLS
// edge carries Properties["line"] with a valid positive integer.
func TestExtractor_AllCallEdges_HaveLineProperty(t *testing.T) {
	src := `fun a() {}

fun b() {
    a()
}

fun c() {
    a()
    b()
}
`
	ents := runKotlin(t, src)

	for _, ent := range ents {
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
