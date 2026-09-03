// Package python_test — issue #2989 Type System extraction tests.
//
// Verifies that the Python extractor classifies type-system constructs:
//   - class X(Protocol)            → SCOPE.Component/class pattern_type="protocol"
//   - class X(Enum/IntEnum/StrEnum)→ pattern_type="enum" + enum_members
//   - class X(TypedDict)           → pattern_type="typed_dict" + typed_fields
//   - @dataclass / NamedTuple      → pattern_type="dataclass"/"named_tuple"
//   - X = Union[...] / X: TypeAlias = ... / type X = ...
//     → SCOPE.Schema/type_alias entity
package python_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// findClass returns the SCOPE.Component/class entity whose trailing name leaf
// matches name, or nil.
func findClass(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		e := &ents[i]
		if e.Kind == "SCOPE.Component" && e.Subtype == "class" && e.Name == name {
			return e
		}
	}
	return nil
}

// findTypeAlias returns the SCOPE.Schema/type_alias entity named name, or nil.
func findTypeAlias(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		e := &ents[i]
		if e.Kind == "SCOPE.Schema" && e.Subtype == "type_alias" && e.Name == name {
			return e
		}
	}
	return nil
}

// findEnum returns the SCOPE.Enum value-set entity named name, or nil.
func findEnum(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		e := &ents[i]
		if e.Kind == "SCOPE.Enum" && e.Name == name {
			return e
		}
	}
	return nil
}

// TestEnumValueSet_PythonValues asserts that a Python Enum emits a value-
// carrying SCOPE.Enum node whose `values` property records each member's
// literal value (RED=1) — not merely the member names.
func TestEnumValueSet_PythonValues(t *testing.T) {
	src := `
import enum

class Color(enum.Enum):
    RED = 1
    GREEN = 2
    BLUE = 3
`
	ents := extractPy(t, src, "app/colors.py")
	en := findEnum(ents, "Color")
	if en == nil {
		t.Fatal("SCOPE.Enum:Color value-set node not found")
	}
	if got := en.QualifiedName; got != "scope:enum:app/colors.py:Color" {
		t.Fatalf("QualifiedName = %q, want scope:enum:app/colors.py:Color", got)
	}
	if got := en.Properties["values"]; got != "RED=1, GREEN=2, BLUE=3" {
		t.Fatalf("values = %q, want %q", got, "RED=1, GREEN=2, BLUE=3")
	}
	if got := en.Properties["member_count"]; got != "3" {
		t.Fatalf("member_count = %q, want 3", got)
	}
	if got := en.Properties["kind_hint"]; got != "python_enum" {
		t.Fatalf("kind_hint = %q, want python_enum", got)
	}
}

// TestEnumValueSet_PythonStrEnumStripsQuotes asserts string-literal enum
// values are recorded with surrounding quotes stripped (OPEN=open, not
// OPEN="open").
func TestEnumValueSet_PythonStrEnumStripsQuotes(t *testing.T) {
	src := `
from enum import StrEnum

class Status(StrEnum):
    OPEN = "open"
    CLOSED = "closed"
`
	ents := extractPy(t, src, "app/status.py")
	en := findEnum(ents, "Status")
	if en == nil {
		t.Fatal("SCOPE.Enum:Status value-set node not found")
	}
	if got := en.Properties["values"]; got != "OPEN=open, CLOSED=closed" {
		t.Fatalf("values = %q, want %q", got, "OPEN=open, CLOSED=closed")
	}
}

// TestEnumValueSet_PythonParenthesizedValue asserts a member value wrapped in
// parentheses across lines (`NAME = (\n    "value"\n)`) still resolves to its
// literal, while auto() stays empty-valued and a Django-style tuple still
// resolves to its first element.
func TestEnumValueSet_PythonParenthesizedValue(t *testing.T) {
	src := `
import enum

class TransactionType(enum.StrEnum):
    SHORT = "short"
    A_VERY_LONG_MEMBER_NAME = (
        "some-value"
    )
    AUTOD = enum.auto()
    DJANGO_LIKE = ("db", "Label")
`
	ents := extractPy(t, src, "app/transaction.py")
	en := findEnum(ents, "TransactionType")
	if en == nil {
		t.Fatal("SCOPE.Enum:TransactionType value-set node not found")
	}
	want := "SHORT=short, A_VERY_LONG_MEMBER_NAME=some-value, DJANGO_LIKE=db"
	if got := en.Properties["values"]; got != want {
		t.Fatalf("values = %q, want %q", got, want)
	}
	if got := en.Properties["member_count"]; got != "4" {
		t.Fatalf("member_count = %q, want 4", got)
	}
}

// TestEnumValueSet_NonEnumNoNode asserts an ordinary (non-enum) class emits NO
// SCOPE.Enum node — the negative case.
func TestEnumValueSet_NonEnumNoNode(t *testing.T) {
	src := `
class Plain:
    RED = 1
    GREEN = 2
`
	ents := extractPy(t, src, "app/plain.py")
	if en := findEnum(ents, "Plain"); en != nil {
		t.Fatalf("non-enum class Plain produced a SCOPE.Enum node: %+v", en.Properties)
	}
}

func TestTypeSystem_Protocol(t *testing.T) {
	src := `
from typing import Protocol

class Serializer(Protocol):
    def to_dict(self) -> dict: ...
    def from_dict(self, d: dict) -> "Serializer": ...
`
	ents := extractPy(t, src, "app/contracts.py")
	c := findClass(ents, "Serializer")
	if c == nil {
		t.Fatal("Serializer class entity not found")
	}
	if got := c.Properties["pattern_type"]; got != "protocol" {
		t.Fatalf("pattern_type = %q, want protocol", got)
	}
	if got := c.Properties["protocol_methods"]; got != "to_dict, from_dict" {
		t.Fatalf("protocol_methods = %q, want %q", got, "to_dict, from_dict")
	}
}

func TestTypeSystem_Enum(t *testing.T) {
	src := `
import enum

class Color(enum.Enum):
    RED = 1
    GREEN = 2
    BLUE = 3
`
	ents := extractPy(t, src, "app/colors.py")
	c := findClass(ents, "Color")
	if c == nil {
		t.Fatal("Color class entity not found")
	}
	if got := c.Properties["pattern_type"]; got != "enum" {
		t.Fatalf("pattern_type = %q, want enum", got)
	}
	if got := c.Properties["enum_members"]; got != "RED, GREEN, BLUE" {
		t.Fatalf("enum_members = %q, want %q", got, "RED, GREEN, BLUE")
	}
}

func TestTypeSystem_IntEnumAndStrEnum(t *testing.T) {
	src := `
from enum import IntEnum, StrEnum

class Priority(IntEnum):
    LOW = 1
    HIGH = 9

class Status(StrEnum):
    OPEN = "open"
    CLOSED = "closed"
`
	ents := extractPy(t, src, "app/enums.py")
	for _, name := range []string{"Priority", "Status"} {
		c := findClass(ents, name)
		if c == nil {
			t.Fatalf("%s class entity not found", name)
		}
		if got := c.Properties["pattern_type"]; got != "enum" {
			t.Fatalf("%s pattern_type = %q, want enum", name, got)
		}
	}
}

func TestTypeSystem_TypedDict(t *testing.T) {
	src := `
from typing import TypedDict

class Movie(TypedDict):
    title: str
    year: int
`
	ents := extractPy(t, src, "app/schemas.py")
	c := findClass(ents, "Movie")
	if c == nil {
		t.Fatal("Movie class entity not found")
	}
	if got := c.Properties["pattern_type"]; got != "typed_dict" {
		t.Fatalf("pattern_type = %q, want typed_dict", got)
	}
	if got := c.Properties["typed_fields"]; got != "title, year" {
		t.Fatalf("typed_fields = %q, want %q", got, "title, year")
	}
}

func TestTypeSystem_Dataclass(t *testing.T) {
	src := `
from dataclasses import dataclass

@dataclass
class Point:
    x: int
    y: int = 0
`
	ents := extractPy(t, src, "app/geometry.py")
	c := findClass(ents, "Point")
	if c == nil {
		t.Fatal("Point class entity not found")
	}
	if got := c.Properties["pattern_type"]; got != "dataclass" {
		t.Fatalf("pattern_type = %q, want dataclass", got)
	}
	if got := c.Properties["typed_fields"]; got != "x, y" {
		t.Fatalf("typed_fields = %q, want %q", got, "x, y")
	}
}

func TestTypeSystem_DataclassWithArgs(t *testing.T) {
	src := `
import dataclasses

@dataclasses.dataclass(frozen=True)
class Config:
    name: str
`
	ents := extractPy(t, src, "app/config.py")
	c := findClass(ents, "Config")
	if c == nil {
		t.Fatal("Config class entity not found")
	}
	if got := c.Properties["pattern_type"]; got != "dataclass" {
		t.Fatalf("pattern_type = %q, want dataclass", got)
	}
}

func TestTypeSystem_NamedTuple(t *testing.T) {
	src := `
from typing import NamedTuple

class Coordinate(NamedTuple):
    lat: float
    lon: float
`
	ents := extractPy(t, src, "app/geo.py")
	c := findClass(ents, "Coordinate")
	if c == nil {
		t.Fatal("Coordinate class entity not found")
	}
	if got := c.Properties["pattern_type"]; got != "named_tuple" {
		t.Fatalf("pattern_type = %q, want named_tuple", got)
	}
}

func TestTypeSystem_TypeAlias_TypingSubscript(t *testing.T) {
	src := `
from typing import Union, Dict

Vector = Union[int, float]
Registry = Dict[str, int]
`
	ents := extractPy(t, src, "app/types.py")
	v := findTypeAlias(ents, "Vector")
	if v == nil {
		t.Fatal("Vector type alias not found")
	}
	if got := v.Properties["pattern_type"]; got != "type_alias" {
		t.Fatalf("Vector pattern_type = %q, want type_alias", got)
	}
	if got := v.Properties["type_body"]; got != "Union[int, float]" {
		t.Fatalf("Vector type_body = %q", got)
	}
	if findTypeAlias(ents, "Registry") == nil {
		t.Fatal("Registry type alias not found")
	}
}

func TestTypeSystem_TypeAlias_ExplicitAnnotation(t *testing.T) {
	src := `
from typing import TypeAlias

Coord: TypeAlias = tuple[int, int]
`
	ents := extractPy(t, src, "app/types.py")
	c := findTypeAlias(ents, "Coord")
	if c == nil {
		t.Fatal("Coord type alias not found")
	}
	if got := c.Properties["explicit"]; got != "true" {
		t.Fatalf("Coord explicit = %q, want true", got)
	}
}

func TestTypeSystem_TypeAlias_PEP695(t *testing.T) {
	src := `
type Vector = list[float]
type StringOrInt = str | int
`
	ents := extractPy(t, src, "app/types.py")
	v := findTypeAlias(ents, "Vector")
	if v == nil {
		t.Fatal("Vector PEP 695 type alias not found")
	}
	if got := v.Properties["explicit"]; got != "true" {
		t.Fatalf("Vector explicit = %q, want true", got)
	}
	if findTypeAlias(ents, "StringOrInt") == nil {
		t.Fatal("StringOrInt PEP 695 type alias not found")
	}
}

func TestTypeSystem_TypeAlias_PEP604Union(t *testing.T) {
	src := `
Identifier = UserId | OrderId | None
`
	ents := extractPy(t, src, "app/types.py")
	if findTypeAlias(ents, "Identifier") == nil {
		t.Fatal("Identifier PEP 604 union alias not found")
	}
}

// TestTypeSystem_NoFalsePositiveOnRuntimeAssignment guards against
// misclassifying ordinary runtime-value assignments as type aliases.
func TestTypeSystem_NoFalsePositiveOnRuntimeAssignment(t *testing.T) {
	src := `
DEBUG = True
count = compute() + 1
flags = a | b
router = Router()
`
	ents := extractPy(t, src, "app/settings.py")
	for _, name := range []string{"DEBUG", "count", "flags", "router"} {
		if findTypeAlias(ents, name) != nil {
			t.Fatalf("%s misclassified as a type alias", name)
		}
	}
}

// TestTypeSystem_PlainClassNotAnnotated ensures ordinary classes are untouched.
func TestTypeSystem_PlainClassNotAnnotated(t *testing.T) {
	src := `
class OrderService:
    def place(self): ...
`
	ents := extractPy(t, src, "app/services.py")
	c := findClass(ents, "OrderService")
	if c == nil {
		t.Fatal("OrderService class entity not found")
	}
	if got := c.Properties["pattern_type"]; got != "" {
		t.Fatalf("OrderService unexpectedly stamped pattern_type = %q", got)
	}
}
