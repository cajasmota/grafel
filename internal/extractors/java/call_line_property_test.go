// call_line_property_test.go — regression tests for #2638.
//
// Verifies that every CALLS RelationshipRecord emitted by the Java extractor
// carries a non-zero Properties["line"] value.
package java_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/java"
	"github.com/cajasmota/grafel/internal/types"
)

func extractJavaForLine(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("java")
	if !ok {
		t.Fatal("java extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Test.java",
		Content:  []byte(src),
		Language: "java",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

// TestExtractor_CallEdge_HasLineProperty asserts that a method-to-method
// call emits a CALLS edge with a non-zero Properties["line"] value.
// Java entity names are "ClassName.methodName" per extractor convention.
func TestExtractor_CallEdge_HasLineProperty(t *testing.T) {
	src := `public class Foo {
    void helper() {}

    void caller() {
        helper();
    }
}
`
	ents := extractJavaForLine(t, src)

	var found bool
	for _, ent := range ents {
		// Java extractor names methods as "ClassName.methodName".
		if ent.Name != "Foo.caller" {
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
		t.Fatal("no CALLS edges found on 'Foo.caller'")
	}
}

// TestExtractor_CallEdge_CorrectLineNumber validates the exact line number.
// The call to bar() appears on line 5.
func TestExtractor_CallEdge_CorrectLineNumber(t *testing.T) {
	// line 1: public class Bar {
	// line 2:     void bar() {}
	// line 3: (blank)
	// line 4:     void foo() {
	// line 5:         bar();
	// line 6:     }
	// line 7: }
	src := "public class Bar {\n    void bar() {}\n\n    void foo() {\n        bar();\n    }\n}\n"

	ents := extractJavaForLine(t, src)

	for _, ent := range ents {
		if ent.Name != "Bar.foo" {
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
			if lineStr != "5" {
				t.Errorf("Properties[\"line\"] = %q, want \"5\"", lineStr)
			}
			return
		}
	}
	t.Fatal("CALLS edge not found on 'Bar.foo'")
}

// TestExtractor_AllCallEdges_HaveLineProperty asserts that every emitted CALLS
// edge carries Properties["line"] with a valid positive integer.
func TestExtractor_AllCallEdges_HaveLineProperty(t *testing.T) {
	src := `public class Multi {
    void a() {}
    void b() { a(); }
    void c() { a(); b(); }
}
`
	ents := extractJavaForLine(t, src)

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
