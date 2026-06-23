package groovy

import (
	"context"
	"testing"

	tsgroovy "github.com/smacker/go-tree-sitter/groovy"

	"github.com/cajasmota/grafel/internal/extractor"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	"github.com/cajasmota/grafel/internal/types"
)

// extractGroovy parses src and runs the Groovy extractor, returning all records.
func extractGroovy(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	parser, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tsgroovy.GetLanguage()))
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	recs, err := (&Extractor{}).Extract(context.Background(), extractor.FileInput{
		Path:    "Sample.groovy",
		Content: []byte(src),
		TSTree:  tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return recs
}

// findEnum returns the SCOPE.Enum record with the given enum_name, or nil.
func findEnum(recs []types.EntityRecord, name string) *types.EntityRecord {
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindEnum) && recs[i].Properties["enum_name"] == name {
			return &recs[i]
		}
	}
	return nil
}

func TestGroovyTypes_PlainEnumValueSet(t *testing.T) {
	recs := extractGroovy(t, "enum Color {\n  RED, GREEN, BLUE\n}\n")
	e := findEnum(recs, "Color")
	if e == nil {
		t.Fatalf("no SCOPE.Enum for Color; got %d records", len(recs))
	}
	if got := e.Properties["members"]; got != "RED, GREEN, BLUE" {
		t.Fatalf("members = %q, want RED, GREEN, BLUE", got)
	}
	if got := e.Properties["member_count"]; got != "3" {
		t.Fatalf("member_count = %q, want 3", got)
	}
	if got := e.Properties["kind_hint"]; got != "groovy_enum" {
		t.Fatalf("kind_hint = %q, want groovy_enum", got)
	}
}

func TestGroovyTypes_ValuedEnum(t *testing.T) {
	recs := extractGroovy(t, "enum Status {\n  ACTIVE(1), INACTIVE(0)\n}\n")
	e := findEnum(recs, "Status")
	if e == nil {
		t.Fatalf("no SCOPE.Enum for Status")
	}
	if got := e.Properties["values"]; got != "ACTIVE=1, INACTIVE=0" {
		t.Fatalf("values = %q, want ACTIVE=1, INACTIVE=0", got)
	}
}

func TestGroovyTypes_StringValuedEnum(t *testing.T) {
	recs := extractGroovy(t, "enum Suit {\n  HEARTS('red'), SPADES('black')\n}\n")
	e := findEnum(recs, "Suit")
	if e == nil {
		t.Fatalf("no SCOPE.Enum for Suit")
	}
	// String literals are quote-stripped to the inner content.
	if got := e.Properties["values"]; got != "HEARTS=red, SPADES=black" {
		t.Fatalf("values = %q, want HEARTS=red, SPADES=black", got)
	}
}

func TestGroovyTypes_EnumWithBodyExcludesFieldsAndCtor(t *testing.T) {
	// Planet has valued constants, then a field + constructor; only the two
	// constants should be members — NOT `mass` or the `Planet(...)` ctor.
	src := "enum Planet {\n  MERCURY(3.3e23), VENUS(4.8e24)\n\n  double mass\n  Planet(double m) { mass = m }\n}\n"
	recs := extractGroovy(t, src)
	e := findEnum(recs, "Planet")
	if e == nil {
		t.Fatalf("no SCOPE.Enum for Planet")
	}
	if got := e.Properties["members"]; got != "MERCURY, VENUS" {
		t.Fatalf("members = %q, want MERCURY, VENUS", got)
	}
	if got := e.Properties["member_count"]; got != "2" {
		t.Fatalf("member_count = %q, want 2", got)
	}
}

func TestGroovyTypes_NoEnumNoEmit(t *testing.T) {
	// A plain class must not produce any SCOPE.Enum record.
	recs := extractGroovy(t, "class Book {\n  def read() {}\n}\n")
	if e := findEnum(recs, "Book"); e != nil {
		t.Fatalf("unexpected SCOPE.Enum emitted for a plain class")
	}
}
