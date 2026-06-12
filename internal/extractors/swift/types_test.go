package swift_test

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// extractSwift parses src and runs the registered swift extractor.
func extractSwift(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("swift")
	if !ok {
		t.Fatal("swift extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Types.swift",
		Content:  []byte(src),
		Language: "swift",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return ents
}

func findEnumValueSet(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == string(types.EntityKindEnum) && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

func findTypeAlias(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == "SCOPE.Schema" && ents[i].Subtype == "type_alias" && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

func TestSwiftTypes_PlainEnumValueSet(t *testing.T) {
	src := `enum Direction {
    case north
    case south, east
    case west
}`
	ents := extractSwift(t, src)
	vs := findEnumValueSet(ents, "Direction")
	if vs == nil {
		t.Fatal("no SCOPE.Enum value-set for Direction")
	}
	got := vs.Properties["members"]
	want := "north, south, east, west"
	if got != want {
		t.Fatalf("members = %q, want %q", got, want)
	}
	if vs.Properties["member_count"] != "4" {
		t.Fatalf("member_count = %q, want 4", vs.Properties["member_count"])
	}
	// The nominal SCOPE.Component(enum) must still be present (no replacement).
	var comp bool
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" && e.Subtype == "enum" && e.Name == "Direction" {
			comp = true
		}
	}
	if !comp {
		t.Fatal("nominal SCOPE.Component(enum) Direction was lost")
	}
}

func TestSwiftTypes_RawValueEnum(t *testing.T) {
	src := `enum HTTPStatus: Int {
    case ok = 200
    case notFound = 404
}`
	ents := extractSwift(t, src)
	vs := findEnumValueSet(ents, "HTTPStatus")
	if vs == nil {
		t.Fatal("no value-set for HTTPStatus")
	}
	if vs.Properties["values"] != "ok=200, notFound=404" {
		t.Fatalf("values = %q, want ok=200, notFound=404", vs.Properties["values"])
	}
}

func TestSwiftTypes_StringRawValueEnum(t *testing.T) {
	src := `enum Suit: String {
    case hearts = "♥"
    case spades = "♠"
}`
	ents := extractSwift(t, src)
	vs := findEnumValueSet(ents, "Suit")
	if vs == nil {
		t.Fatal("no value-set for Suit")
	}
	// Quotes stripped by StripLiteralQuotes.
	if vs.Properties["values"] != "hearts=♥, spades=♠" {
		t.Fatalf("values = %q", vs.Properties["values"])
	}
}

func TestSwiftTypes_TypeAlias(t *testing.T) {
	src := `typealias UserID = Int
typealias Handler = (Int) -> Void`
	ents := extractSwift(t, src)
	uid := findTypeAlias(ents, "UserID")
	if uid == nil {
		t.Fatal("no type_alias UserID")
	}
	if uid.Properties["type_body"] != "Int" {
		t.Fatalf("UserID type_body = %q, want Int", uid.Properties["type_body"])
	}
	h := findTypeAlias(ents, "Handler")
	if h == nil {
		t.Fatal("no type_alias Handler")
	}
	if h.Properties["type_body"] != "(Int) -> Void" {
		t.Fatalf("Handler type_body = %q, want (Int) -> Void", h.Properties["type_body"])
	}
}

func TestSwiftTypes_NoTypeAliasNoEmit(t *testing.T) {
	src := `struct User { var id: Int }`
	ents := extractSwift(t, src)
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "type_alias" {
			t.Fatalf("unexpected type_alias emitted: %s", e.Name)
		}
		if e.Kind == string(types.EntityKindEnum) {
			t.Fatalf("unexpected enum value-set emitted: %s", e.Name)
		}
	}
}
