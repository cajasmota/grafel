// issue2081_test.go — probe-policy exclusion tests for #2081.
//
// Verifies that isInherentlyLeafField correctly excludes:
//   - SCOPE.Schema/field entities whose Name contains ".Meta." (Category A:
//     DRF/Django Meta inner-class configuration keys)
//   - SCOPE.Schema/field entities with Properties["field_type"] set (Category B:
//     Django model scalar fields stamped by enrichDjangoModelFieldsAndManagers)
//
// Also verifies that non-leaf field kinds (DRF serializer fields with
// outbound REFERENCES) are NOT excluded.
package main

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

func entity(kind, subtype, name string, props map[string]string) *graph.Entity {
	return graph. // deterministic for test purposes

			EntityPtr(graph.Entity{
			ID:      name,
			Kind:    kind,
			Subtype: subtype,
			Name:    name,
		}.WithProperties(props))
}

// TestIsInherentlyLeafField_MetaInnerClassFields verifies that SCOPE.Schema/field
// entities whose Name contains ".Meta." are excluded (Category A).
func TestIsInherentlyLeafField_MetaInnerClassFields(t *testing.T) {
	cases := []struct {
		name string
		ent  *graph.Entity
		want bool
	}{
		{
			name: "Meta.model field excluded",
			ent:  entity("SCOPE.Schema", "field", "ContractSerializer.Meta.model", nil),
			want: true,
		},
		{
			name: "Meta.fields field excluded",
			ent:  entity("SCOPE.Schema", "field", "DeviceSerializer.Meta.fields", nil),
			want: true,
		},
		{
			name: "Meta.db_table field excluded",
			ent:  entity("SCOPE.Schema", "field", "Contract.Meta.db_table", nil),
			want: true,
		},
		{
			name: "nested Meta field excluded",
			ent:  entity("SCOPE.Schema", "field", "OuterSerializer.Meta.read_only_fields", nil),
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isInherentlyLeafField(tc.ent)
			if got != tc.want {
				t.Errorf("isInherentlyLeafField(%q) = %v, want %v", tc.ent.Name, got, tc.want)
			}
		})
	}
}

// TestIsInherentlyLeafField_DjangoModelScalarFields verifies that SCOPE.Schema/field
// entities with Properties["field_type"] set are excluded (Category B).
func TestIsInherentlyLeafField_DjangoModelScalarFields(t *testing.T) {
	cases := []struct {
		name     string
		fieldTyp string
	}{
		{"CharField", "CharField"},
		{"DateField", "DateField"},
		{"IntegerField", "IntegerField"},
		{"DecimalField", "DecimalField"},
		{"BooleanField", "BooleanField"},
		{"TextField", "TextField"},
		{"UUIDField", "UUIDField"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := entity("SCOPE.Schema", "field", "Contract."+tc.name, map[string]string{
				"field_type": tc.fieldTyp,
			})
			if !isInherentlyLeafField(e) {
				t.Errorf("isInherentlyLeafField(%q with field_type=%q) = false, want true", e.Name, tc.fieldTyp)
			}
		})
	}
}

// TestIsInherentlyLeafField_NonLeafEntitiesNotExcluded verifies that non-leaf
// entities are NOT excluded.
func TestIsInherentlyLeafField_NonLeafEntitiesNotExcluded(t *testing.T) {
	cases := []struct {
		name string
		ent  *graph.Entity
	}{
		{
			name: "plain serializer field without Meta or field_type not excluded",
			ent:  entity("SCOPE.Schema", "field", "InspectionSerializer.status", nil),
		},
		{
			name: "function entity not excluded",
			ent: &graph.Entity{
				ID:   "fn1",
				Kind: "function",
				Name: "my_func",
			},
		},
		{
			name: "class entity not excluded",
			ent: &graph.Entity{
				ID:   "cls1",
				Kind: "class",
				Name: "ContractSerializer",
			},
		},
		{
			name: "SCOPE.Schema/field with dotted name but no .Meta. not excluded",
			ent:  entity("SCOPE.Schema", "field", "ContractSerializer.sales_agent", nil),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if isInherentlyLeafField(tc.ent) {
				t.Errorf("isInherentlyLeafField(%q) = true, want false (should not be excluded)", tc.ent.Name)
			}
		})
	}
}

// TestIsInherentlyLeafField_WrongKindNotExcluded verifies that non-SCOPE.Schema
// entities with Meta in their name or field_type property are not excluded.
func TestIsInherentlyLeafField_WrongKindNotExcluded(t *testing.T) {
	// A class whose name contains ".Meta." — should not be excluded
	e1 := &graph.Entity{
		ID:      "cls1",
		Kind:    "class",
		Subtype: "",
		Name:    "Foo.Meta",
	}
	if isInherentlyLeafField(e1) {
		t.Errorf("class entity with Meta name should not be excluded")
	}

	// A function with field_type property — should not be excluded
	e2 :=

		graph.EntityPtr(graph.Entity{
			ID:   "fn1",
			Kind: "function",
			Name: "get_field",
		}.WithProperties(map[string]string{"field_type": "CharField"}))
	if isInherentlyLeafField(e2) {
		t.Errorf("function entity with field_type should not be excluded")
	}
}
