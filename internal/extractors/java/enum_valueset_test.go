package java_test

// enum_valueset_test.go — value-asserting tests for the Java SCOPE.Enum
// value-set node (data-model, epic #3628 / completes #3806).

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func extractJavaForEnum(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("java")
	if !ok {
		t.Fatal("java extractor not registered")
	}
	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path: path, Content: []byte(src), Language: "java", TSTree: tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return got
}

func findJavaEnum(recs []types.EntityRecord, name string) *types.EntityRecord {
	for i := range recs {
		if recs[i].Kind == "SCOPE.Enum" && recs[i].Name == name {
			return &recs[i]
		}
	}
	return nil
}

func TestJavaEnumValueSet_BareMembers(t *testing.T) {
	recs := extractJavaForEnum(t, "Status.java", `
public enum Status { ACTIVE, INACTIVE, PENDING }
`)
	en := findJavaEnum(recs, "Status")
	if en == nil {
		t.Fatal("SCOPE.Enum:Status value-set node not found")
	}
	if got := en.Properties["kind_hint"]; got != "java_enum" {
		t.Fatalf("kind_hint = %q, want java_enum", got)
	}
	if got := en.Properties["members"]; got != "ACTIVE, INACTIVE, PENDING" {
		t.Fatalf("members = %q, want %q", got, "ACTIVE, INACTIVE, PENDING")
	}
	// Bare constants have no constructor args → no fabricated values.
	if got, ok := en.Properties["values"]; ok {
		t.Fatalf("values should be absent for bare members, got %q", got)
	}
}

func TestJavaEnumValueSet_ConstructorValues(t *testing.T) {
	recs := extractJavaForEnum(t, "Color.java", `
public enum Color {
    RED("#f00"),
    GREEN("#0f0"),
    BLUE("#00f");

    private final String hex;
    Color(String hex) { this.hex = hex; }
}
`)
	en := findJavaEnum(recs, "Color")
	if en == nil {
		t.Fatal("SCOPE.Enum:Color value-set node not found")
	}
	if got := en.Properties["values"]; got != "RED=#f00, GREEN=#0f0, BLUE=#00f" {
		t.Fatalf("values = %q, want %q", got, "RED=#f00, GREEN=#0f0, BLUE=#00f")
	}
	wantQN := "scope:enum:Color.java:Color"
	if en.QualifiedName != wantQN {
		t.Fatalf("QualifiedName = %q, want %q", en.QualifiedName, wantQN)
	}
}

func TestJavaEnumValueSet_NumericConstructorValue(t *testing.T) {
	recs := extractJavaForEnum(t, "Planet.java", `
public enum Planet {
    MERCURY(1),
    VENUS(2);
    Planet(int order) {}
}
`)
	en := findJavaEnum(recs, "Planet")
	if en == nil {
		t.Fatal("SCOPE.Enum:Planet value-set node not found")
	}
	if got := en.Properties["values"]; got != "MERCURY=1, VENUS=2" {
		t.Fatalf("values = %q, want %q", got, "MERCURY=1, VENUS=2")
	}
}

// Negative: a plain class is not an enum value-set.
func TestJavaEnumValueSet_NonEnumClass(t *testing.T) {
	recs := extractJavaForEnum(t, "Foo.java", `
public class Foo { private int x; }
`)
	if en := findJavaEnum(recs, "Foo"); en != nil {
		t.Fatal("non-enum class Foo should NOT produce a SCOPE.Enum node")
	}
}
