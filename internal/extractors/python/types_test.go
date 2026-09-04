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
	"strings"
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

// TestEnumValueSet_PythonParenthesizedValue pins BOTH directions of the
// parenthesised-RHS case. Recall: a literal wrapped in parentheses
// (`NAME = (\n    "value"\n)`) still resolves, for a string, for a non-string
// literal, and through a parenthesised Django tuple. Over-firing: a computed
// expression wrapped in parentheses stays value-less — unwrapping must recurse
// into the inner node, never fall back to the parenthesised node's raw text.
//
// Axis varied per member, everything else held constant (same class, same enum
// base, same single-assignment form):
//
//	SHORT                   — unparenthesised string (control for the whole set)
//	A_VERY_LONG_MEMBER_NAME — parens + newlines around a string
//	NUM                     — parens around a NON-string literal (integer), so
//	                          the new case is not pinned only via the string arm
//	AUTOD                   — bare `enum.auto()`: computed, no parens
//	AUTOD_PAREN             — computed WRAPPED IN PARENS: the negative control.
//	                          This is the only member that enters the
//	                          parenthesised case with a computed payload; it is
//	                          what a raw-text fallback would fabricate a value
//	                          for.
//	COMPUTED                — second computed-in-parens control (arithmetic)
//	DJANGO_LIKE             — bare tuple, no extra parens
//	DJANGO_PAREN            — parens around a tuple: the only member where the
//	                          parenthesised case and the tuple case compose
//
// Not covered here, deliberately: an f-string value (#6799) and a comment
// inside the parentheses (#6800). Both are open bugs; pinning today's output
// would pin the defect.
func TestEnumValueSet_PythonParenthesizedValue(t *testing.T) {
	src := `
import enum

class TransactionType(enum.StrEnum):
    SHORT = "short"
    A_VERY_LONG_MEMBER_NAME = (
        "some-value"
    )
    NUM = (
        7
    )
    AUTOD = enum.auto()
    AUTOD_PAREN = (enum.auto())
    COMPUTED = (1 + 2)
    DJANGO_LIKE = ("db", "Label")
    DJANGO_PAREN = (("db2", "Label"))
`
	ents := extractPy(t, src, "app/transaction.py")
	en := findEnum(ents, "TransactionType")
	if en == nil {
		t.Fatal("SCOPE.Enum:TransactionType value-set node not found")
	}
	want := "SHORT=short, A_VERY_LONG_MEMBER_NAME=some-value, NUM=7, DJANGO_LIKE=db, DJANGO_PAREN=db2"
	got := en.Properties["values"]
	if got != want {
		t.Fatalf("values = %q, want %q", got, want)
	}
	// Negative control, stated separately from the equality above so a failure
	// names the defect: unwrapping parentheses must not hand a computed member
	// a value. A raw-text fallback would emit `AUTOD_PAREN=enum.auto()` and
	// `COMPUTED=1 + 2` here.
	for _, computed := range []string{"AUTOD_PAREN", "COMPUTED"} {
		if strings.Contains(got, computed+"=") {
			t.Errorf("computed member %s in parentheses was given a value: values = %q", computed, got)
		}
	}
	// All 8 members are recorded even though 3 carry no value.
	if got := en.Properties["member_count"]; got != "8" {
		t.Fatalf("member_count = %q, want 8", got)
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
