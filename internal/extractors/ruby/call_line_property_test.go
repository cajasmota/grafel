// call_line_property_test.go — regression tests for #2638.
//
// Verifies that every CALLS RelationshipRecord emitted by the Ruby extractor
// carries a non-zero Properties["line"] value.
package ruby_test

import (
	"strconv"
	"testing"
)

// TestExtractor_CallEdge_HasLineProperty asserts that a method-to-method
// call emits a CALLS edge with a non-zero Properties["line"] value.
func TestExtractor_CallEdge_HasLineProperty(t *testing.T) {
	src := `class Foo
  def helper
  end

  def caller
    helper
  end
end
`
	ents := runRuby(t, src)

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

// TestExtractor_AllCallEdges_HaveLineProperty asserts that every emitted CALLS
// edge carries Properties["line"] with a valid positive integer.
func TestExtractor_AllCallEdges_HaveLineProperty(t *testing.T) {
	src := `class Bar
  def a
  end

  def b
    a
  end

  def c
    a
    b
  end
end
`
	ents := runRuby(t, src)

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
