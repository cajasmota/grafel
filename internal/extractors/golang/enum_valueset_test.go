package golang_test

// enum_valueset_test.go — value-asserting tests for the Go SCOPE.Enum
// value-set node (data-model, epic #3628 / completes #3806).

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func goEnumRecords(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	out, err := extractFromPath(src, "enums.go")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	recs := make([]types.EntityRecord, 0, len(out))
	for _, r := range out {
		recs = append(recs, r.(types.EntityRecord))
	}
	return recs
}

func findGoEnum(recs []types.EntityRecord, name string) *types.EntityRecord {
	for i := range recs {
		if recs[i].Kind == "SCOPE.Enum" && recs[i].Name == name {
			return &recs[i]
		}
	}
	return nil
}

func TestGoEnumValueSet_IotaMembersInOrder(t *testing.T) {
	recs := goEnumRecords(t, `package p
type Status int
const (
	Active Status = iota
	Inactive
	Pending
)
`)
	en := findGoEnum(recs, "Status")
	if en == nil {
		t.Fatal("SCOPE.Enum:Status value-set node not found")
	}
	if got := en.Properties["kind_hint"]; got != "go_iota" {
		t.Fatalf("kind_hint = %q, want go_iota", got)
	}
	if got := en.Properties["members"]; got != "Active, Inactive, Pending" {
		t.Fatalf("members = %q, want %q (order matters)", got, "Active, Inactive, Pending")
	}
	// iota members are recorded value-less (no fabricated ordinals).
	if got, ok := en.Properties["values"]; ok {
		t.Fatalf("values should be absent for iota members, got %q", got)
	}
}

func TestGoEnumValueSet_ExplicitStringValues(t *testing.T) {
	recs := goEnumRecords(t, `package p
type Color string
const (
	Red   Color = "red"
	Green Color = "green"
	Blue  Color = "blue"
)
`)
	en := findGoEnum(recs, "Color")
	if en == nil {
		t.Fatal("SCOPE.Enum:Color value-set node not found")
	}
	if got := en.Properties["values"]; got != "Red=red, Green=green, Blue=blue" {
		t.Fatalf("values = %q, want %q", got, "Red=red, Green=green, Blue=blue")
	}
	wantQN := "scope:enum:enums.go:Color"
	if en.QualifiedName != wantQN {
		t.Fatalf("QualifiedName = %q, want %q", en.QualifiedName, wantQN)
	}
}

func TestGoEnumValueSet_ExplicitIntValues(t *testing.T) {
	recs := goEnumRecords(t, `package p
type Level int
const (
	Low  Level = 1
	High Level = 2
)
`)
	en := findGoEnum(recs, "Level")
	if en == nil {
		t.Fatal("SCOPE.Enum:Level value-set node not found")
	}
	if got := en.Properties["values"]; got != "Low=1, High=2" {
		t.Fatalf("values = %q, want %q", got, "Low=1, High=2")
	}
}

// Negative: an untyped const block (no named type) is not an enum value-set.
func TestGoEnumValueSet_UntypedConst_NoEnum(t *testing.T) {
	recs := goEnumRecords(t, `package p
const (
	A = 1
	B = 2
)
`)
	for i := range recs {
		if recs[i].Kind == "SCOPE.Enum" {
			t.Fatalf("untyped const block should NOT produce a SCOPE.Enum node, got %q", recs[i].Name)
		}
	}
}

// Negative: a const block typed by a builtin-only (no same-file named type)
// is not an enum value-set.
func TestGoEnumValueSet_BuiltinTyped_NoEnum(t *testing.T) {
	recs := goEnumRecords(t, `package p
const (
	Pi float64 = 3.14
)
`)
	if en := findGoEnum(recs, "float64"); en != nil {
		t.Fatal("builtin-typed const should NOT produce a SCOPE.Enum node")
	}
}
