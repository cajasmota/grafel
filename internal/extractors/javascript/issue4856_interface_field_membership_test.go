// Package javascript — issue #4856 TS interface + type-alias field-membership.
//
// Follow-up to #4845 (which fixed only `class`). LIVE root cause (acme-v3):
// a NestJS response DTO declared as `export interface AlternateAddressResponse`
// resolved to a field-less node — the dashboard shape endpoint returned
// rows:[] / has_children=None — because interface_declaration and
// type_alias_declaration go through a different AST path (property_signature,
// not public_field_definition) and emitted NO SCOPE.Schema/field child
// entities nor owner→field CONTAINS edges. Class-based responses already
// expanded after #4845.
//
// After #4856:
//   - interface_declaration members (property/method signatures) each get a
//     SCOPE.Schema/field entity + interface→field CONTAINS edge.
//   - type_alias_declaration with an object-type RHS (`type X = {a; b}`) gets
//     the same field children + alias→field CONTAINS edges.
//   - interface `extends` edges carry Properties["to"]/["from"] so the
//     dashboard shape walker resolves the base and projects inherited members.
package javascript_test

import (
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// hasSchemaContainsField reports whether the SCOPE.Schema owner named `owner`
// carries a CONTAINS edge to the field structural-ref for `<owner>.<field>`.
// Mirrors hasContainsField (#4845) but matches the SCOPE.Schema owner kind
// used for interfaces and type aliases.
func hasSchemaContainsField(ents []types.EntityRecord, path, owner, field string) bool {
	want := extreg.BuildSchemaFieldStructuralRef("typescript", path, owner+"."+field)
	for _, e := range ents {
		if e.Name != owner || e.Kind != "SCOPE.Schema" {
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

// TestInterfaceDTO_FieldsAreContained proves a NestJS response interface emits
// one SCOPE.Schema/field entity per property AND an interface→field CONTAINS
// edge for each — the exact gap that left shape rows empty for interface DTOs.
func TestInterfaceDTO_FieldsAreContained(t *testing.T) {
	path := "src/address/dto/alternate-address-response.dto.ts"
	src := []byte(`
export interface AlternateAddressResponse {
  readonly line1: string;
  street?: string;
  count: number;
  greet(): void;
  [key: string]: any;
}
`)
	ents := extractHeritageTS(t, path, src)

	for _, f := range []string{"line1", "street", "count", "greet"} {
		if !fieldEntityExists(ents, "AlternateAddressResponse", f) {
			t.Errorf("expected SCOPE.Schema/field entity AlternateAddressResponse.%s", f)
		}
		if !hasSchemaContainsField(ents, path, "AlternateAddressResponse", f) {
			t.Errorf("expected CONTAINS edge from interface to field %q", f)
		}
	}
	// Index signature has no member name → must be skipped gracefully (no crash,
	// no bogus field entity).
	if fieldEntityExists(ents, "AlternateAddressResponse", "key") {
		t.Error("index signature must not produce a field entity")
	}
}

// TestInterfaceExtends_CarriesResolvableBase proves interface `extends` emits an
// EXTENDS edge whose Properties["to"] carries the base name, so the dashboard
// shape walker can resolve the base and project inherited members. An interface
// can extend multiple bases.
func TestInterfaceExtends_CarriesResolvableBase(t *testing.T) {
	path := "src/address/dto/extended-response.dto.ts"
	src := []byte(`
export interface BaseResponse {
  id: string;
}
export interface AuditResponse {
  createdAt: string;
}
export interface AlternateAddressResponse extends BaseResponse, AuditResponse {
  line1: string;
}
`)
	ents := extractHeritageTS(t, path, src)

	// Own field is contained.
	if !hasSchemaContainsField(ents, path, "AlternateAddressResponse", "line1") {
		t.Error("AlternateAddressResponse should CONTAINS its own field line1")
	}
	// Base fields are contained by their own interfaces.
	if !hasSchemaContainsField(ents, path, "BaseResponse", "id") {
		t.Error("BaseResponse should CONTAINS field id")
	}
	if !hasSchemaContainsField(ents, path, "AuditResponse", "createdAt") {
		t.Error("AuditResponse should CONTAINS field createdAt")
	}
	// EXTENDS edges to BOTH bases, with the resolvable name in Properties["to"]
	// (the SCOPE.Schema-owner shape the dashboard's relTargetName reads).
	for _, base := range []string{"BaseResponse", "AuditResponse"} {
		if !hasResolvableExtends(ents, "AlternateAddressResponse", base) {
			t.Errorf("AlternateAddressResponse should EXTENDS %s with Properties[\"to\"]=%q", base, base)
		}
	}
}

// hasResolvableExtends reports whether the entity `from` has an EXTENDS edge
// carrying Properties["to"]==to (the shape the dashboard's relTargetName reads).
func hasResolvableExtends(ents []types.EntityRecord, from, to string) bool {
	for _, e := range ents {
		if e.Name != from {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "EXTENDS" && r.ToID == to &&
				r.Properties != nil && r.Properties.Get("to") == to {
				return true
			}
		}
	}
	return false
}

// TestTypeAliasObjectDTO_FieldsAreContained proves an object-shaped type alias
// (`type X = { a: string; b?: number }`) emits field children + CONTAINS edges
// exactly like a class/interface DTO.
func TestTypeAliasObjectDTO_FieldsAreContained(t *testing.T) {
	path := "src/address/dto/pet.dto.ts"
	src := []byte(`
export type Pet = {
  name: string;
  age?: number;
};
`)
	ents := extractHeritageTS(t, path, src)

	for _, f := range []string{"name", "age"} {
		if !fieldEntityExists(ents, "Pet", f) {
			t.Errorf("expected SCOPE.Schema/field entity Pet.%s", f)
		}
		if !hasSchemaContainsField(ents, path, "Pet", f) {
			t.Errorf("expected CONTAINS edge from type alias to field %q", f)
		}
	}
}

// TestTypeAliasNonObject_NoFields guards the negative case: a union / primitive
// alias has no object members and must emit NO field children.
func TestTypeAliasNonObject_NoFields(t *testing.T) {
	path := "src/address/dto/role.dto.ts"
	src := []byte(`
export type Role = 'admin' | 'user';
export type Id = string;
`)
	ents := extractHeritageTS(t, path, src)

	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "field" {
			t.Errorf("non-object type alias must not emit field entity %q", e.Name)
		}
	}
}
