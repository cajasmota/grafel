package javascript_test

// enum_valueset_test.go — value-asserting tests for the TypeScript SCOPE.Enum
// value-set node (data-model, epic #3628 / completes #3806). Asserts the enum
// node id + specific member values, not merely a non-empty member list.

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func findTSEnum(recs []types.EntityRecord, name string) *types.EntityRecord {
	for i := range recs {
		if recs[i].Kind == "SCOPE.Enum" && recs[i].Name == name {
			return &recs[i]
		}
	}
	return nil
}

func TestTSEnumValueSet_StringValues(t *testing.T) {
	src := []byte(`enum Status { Active = 'active', Inactive = 'inactive' }`)
	tree := parseTS(t, src)
	recs := extract(t, src, "typescript", tree)

	en := findTSEnum(recs, "Status")
	if en == nil {
		t.Fatalf("SCOPE.Enum:Status value-set node not found; names: %v", entityNames(recs))
	}
	if got := en.Properties["kind_hint"]; got != "ts_enum" {
		t.Fatalf("kind_hint = %q, want ts_enum", got)
	}
	if got := en.Properties["values"]; got != "Active=active, Inactive=inactive" {
		t.Fatalf("values = %q, want %q", got, "Active=active, Inactive=inactive")
	}
	wantQN := "scope:enum:" + en.SourceFile + ":Status"
	if en.QualifiedName != wantQN {
		t.Fatalf("QualifiedName = %q, want %q", en.QualifiedName, wantQN)
	}
}

func TestTSEnumValueSet_ImplicitMembers(t *testing.T) {
	src := []byte(`enum Dir { Up, Down, Left, Right }`)
	tree := parseTS(t, src)
	recs := extract(t, src, "typescript", tree)

	en := findTSEnum(recs, "Dir")
	if en == nil {
		t.Fatalf("SCOPE.Enum:Dir value-set node not found")
	}
	if got := en.Properties["members"]; got != "Up, Down, Left, Right" {
		t.Fatalf("members = %q, want %q", got, "Up, Down, Left, Right")
	}
	// Implicit numeric enum → no fabricated ordinals.
	if got, ok := en.Properties["values"]; ok {
		t.Fatalf("values should be absent for implicit members, got %q", got)
	}
}

func TestTSEnumValueSet_NumericExplicit(t *testing.T) {
	src := []byte(`enum Level { Low = 1, High = 2 }`)
	tree := parseTS(t, src)
	recs := extract(t, src, "typescript", tree)

	en := findTSEnum(recs, "Level")
	if en == nil {
		t.Fatalf("SCOPE.Enum:Level value-set node not found")
	}
	if got := en.Properties["values"]; got != "Low=1, High=2" {
		t.Fatalf("values = %q, want %q", got, "Low=1, High=2")
	}
}

func TestTSLiteralUnionValueSet(t *testing.T) {
	src := []byte(`type Role = 'admin' | 'user' | 'guest';`)
	tree := parseTS(t, src)
	recs := extract(t, src, "typescript", tree)

	en := findTSEnum(recs, "Role")
	if en == nil {
		t.Fatalf("SCOPE.Enum:Role value-set node not found; names: %v", entityNames(recs))
	}
	if got := en.Properties["kind_hint"]; got != "ts_literal_union" {
		t.Fatalf("kind_hint = %q, want ts_literal_union", got)
	}
	if got := en.Properties["members"]; got != "admin, user, guest" {
		t.Fatalf("members = %q, want %q", got, "admin, user, guest")
	}
	if got := en.Properties["values"]; got != "admin=admin, user=user, guest=guest" {
		t.Fatalf("values = %q, want %q", got, "admin=admin, user=user, guest=guest")
	}
}

// Negative: a union with any type reference is not an enumerated value-set.
func TestTSMixedUnion_NoEnum(t *testing.T) {
	src := []byte(`type Mixed = string | Foo;`)
	tree := parseTS(t, src)
	recs := extract(t, src, "typescript", tree)

	if en := findTSEnum(recs, "Mixed"); en != nil {
		t.Fatalf("Mixed should NOT produce a SCOPE.Enum value-set node")
	}
}

// Negative: a plain non-enum type alias produces no value-set node.
func TestTSPlainAlias_NoEnum(t *testing.T) {
	src := []byte(`type ID = string;`)
	tree := parseTS(t, src)
	recs := extract(t, src, "typescript", tree)

	if en := findTSEnum(recs, "ID"); en != nil {
		t.Fatalf("plain alias ID should NOT produce a SCOPE.Enum value-set node")
	}
}
