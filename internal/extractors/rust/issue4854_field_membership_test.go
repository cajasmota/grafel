// Package rust — issue #4854 general struct/enum field-membership tests.
//
// Root cause: Rust struct fields were only emitted as field entities by the
// serde/utoipa/ORM-bound custom emitters (internal/custom/rust, #4635). A plain
// data struct resolved to a SCOPE.Component with ZERO field children, so the
// dashboard shape endpoint returned rows:[] — the same gap #4850/#4855 closed
// for Go and #4845/#4851 for JS/TS.
//
// After #4854 every named struct field gets a SCOPE.Schema/field entity AND a
// struct→field CONTAINS edge, honouring serde rename (wire name) and skip;
// named fields of struct-style enum variants become "<Enum>.<Variant>.<field>".
// Rust has no inheritance, so there is no EXTENDS.
package rust_test

import (
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func rsFieldEntityExists(ents []types.EntityRecord, owner, field string) bool {
	want := owner + "." + field
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "field" && e.Name == want {
			return true
		}
	}
	return false
}

func rsHasContainsField(ents []types.EntityRecord, owner, dottedField string) bool {
	want := extreg.BuildSchemaFieldStructuralRef("rust", "test.rs", dottedField)
	for _, e := range ents {
		if e.Name != owner || e.Kind != "SCOPE.Component" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "CONTAINS" && r.ToID == want {
				return true
			}
		}
	}
	return false
}

// TestRustStructFieldsAreContained proves a plain data struct with serde
// rename/skip emits one SCOPE.Schema/field entity per named field (wire name
// honoured, skip excluded) AND a struct→field CONTAINS edge for each.
func TestRustStructFieldsAreContained(t *testing.T) {
	src := `
struct User {
    id: u64,
    #[serde(rename = "userName")]
    name: String,
    #[serde(skip)]
    secret: String,
}
`
	ents := runRust(t, src)
	// id is untagged; name uses the serde wire name "userName".
	for _, f := range []string{"id", "userName"} {
		if !rsFieldEntityExists(ents, "User", f) {
			t.Errorf("expected SCOPE.Schema/field entity User.%s", f)
		}
		if !rsHasContainsField(ents, "User", "User."+f) {
			t.Errorf("expected CONTAINS edge from User to field %q", f)
		}
	}
	if rsFieldEntityExists(ents, "User", "secret") {
		t.Errorf("#[serde(skip)] field secret must be excluded")
	}
}

// TestRustEnumVariantFieldsAreContained proves named fields of struct-style
// enum variants become "<Enum>.<Variant>.<field>" members with CONTAINS edges.
func TestRustEnumVariantFieldsAreContained(t *testing.T) {
	src := `
enum Event {
    Click { x: i32, y: i32 },
    Key(char),
}
`
	ents := runRust(t, src)
	for _, f := range []string{"Click.x", "Click.y"} {
		if !rsFieldEntityExists(ents, "Event", f) {
			t.Errorf("expected SCOPE.Schema/field entity Event.%s", f)
		}
		if !rsHasContainsField(ents, "Event", "Event."+f) {
			t.Errorf("expected CONTAINS edge from Event to field %q", f)
		}
	}
}
