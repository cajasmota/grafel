package patterns

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// TestOpenAPI_AllOfComposition verifies that a schema using allOf with $refs
// emits REFERENCES edges to each base schema.
func TestOpenAPI_AllOfComposition(t *testing.T) {
	d := &openAPIExtractor{}
	src := loadOpenAPIFixture(t, "openapi_composition.yml")
	results := d.Detect("api/openapi.yml", "yaml", src)

	cat := findByName(results, "openapi_schema_Cat")
	if cat == nil {
		t.Fatalf("Cat schema not found, names=%v", entityNames(results))
	}
	wantBases := []string{"Animal", "Pet"}
	for _, base := range wantBases {
		found := false
		for _, r := range cat.Relationships {
			if r.Kind == "REFERENCES" && r.ToID == "openapi_schema_"+base {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected Cat → %s REFERENCES (allOf), rels=%+v", base, cat.Relationships)
		}
	}
}

// TestOpenAPI_AnyOfOneOfComposition verifies anyOf and oneOf compositions
// produce REFERENCES edges to their member schemas.
func TestOpenAPI_AnyOfOneOfComposition(t *testing.T) {
	d := &openAPIExtractor{}
	src := loadOpenAPIFixture(t, "openapi_composition.yml")
	results := d.Detect("api/openapi.yml", "yaml", src)

	wild := findByName(results, "openapi_schema_Wild")
	if wild == nil {
		t.Fatalf("Wild schema not found")
	}
	hasAnimalRef := false
	for _, r := range wild.Relationships {
		if r.Kind == "REFERENCES" && r.ToID == "openapi_schema_Animal" {
			hasAnimalRef = true
		}
	}
	if !hasAnimalRef {
		t.Errorf("expected Wild → Animal REFERENCES (anyOf)")
	}

	list := findByName(results, "openapi_schema_AnimalList")
	if list == nil {
		t.Fatal("AnimalList schema not found")
	}
	wantMembers := map[string]bool{"Cat": false, "Dog": false, "Wild": false}
	for _, r := range list.Relationships {
		if r.Kind == "REFERENCES" {
			for k := range wantMembers {
				if r.ToID == "openapi_schema_"+k {
					wantMembers[k] = true
				}
			}
		}
	}
	for k, v := range wantMembers {
		if !v {
			t.Errorf("expected AnimalList → %s REFERENCES (oneOf)", k)
		}
	}
}

// TestOpenAPI_ParameterRefResolves verifies parameter-level $ref produces
// Operation → Parameter REFERENCES with reference_kind=parameter_ref.
func TestOpenAPI_ParameterRefResolves(t *testing.T) {
	d := &openAPIExtractor{}
	src := loadOpenAPIFixture(t, "openapi_composition.yml")
	results := d.Detect("api/openapi.yml", "yaml", src)

	listOp := findByName(results, "openapi_op_get__animals")
	if listOp == nil {
		t.Fatalf("get /animals operation not found, names=%v", entityNames(results))
	}
	wantParams := map[string]bool{"PageSize": false, "PageToken": false}
	for _, r := range listOp.Relationships {
		if r.Kind == "REFERENCES" && r.Properties["reference_kind"] == "parameter_ref" {
			for k := range wantParams {
				if r.ToID == "openapi_parameter_"+k {
					wantParams[k] = true
				}
			}
		}
	}
	for k, v := range wantParams {
		if !v {
			t.Errorf("expected get /animals → parameter %s, rels=%+v", k, listOp.Relationships)
		}
	}

	// Parameter entities must exist.
	for _, name := range []string{"PageSize", "PageToken", "AnimalId"} {
		if findByName(results, "openapi_parameter_"+name) == nil {
			t.Errorf("expected parameter entity for %q", name)
		}
	}
}

// TestOpenAPI_SpecContainsParameters verifies the spec emits CONTAINS edges
// to parameter component entities.
func TestOpenAPI_SpecContainsParameters(t *testing.T) {
	d := &openAPIExtractor{}
	src := loadOpenAPIFixture(t, "openapi_composition.yml")
	results := d.Detect("api/openapi.yml", "yaml", src)

	spec := findByName(results, "openapi_spec_Composition")
	if spec == nil {
		t.Fatalf("spec entity not found, names=%v", entityNames(results))
	}
	containsParams := 0
	for _, r := range spec.Relationships {
		if r.Kind == "CONTAINS" && r.Properties["contained_kind"] == "parameter" {
			containsParams++
		}
	}
	if containsParams < 3 {
		t.Errorf("expected ≥3 parameter CONTAINS edges, got %d", containsParams)
	}
}

// TestSwagger2_DefinitionsEmitsSameShape verifies Swagger 2.0 fixtures produce
// schema entities with the same shape as OpenAPI 3.x.
func TestSwagger2_DefinitionsEmitsSameShape(t *testing.T) {
	d := &openAPIExtractor{}
	src := loadOpenAPIFixture(t, "swagger2_petstore.yml")
	results := d.Detect("api/swagger.yml", "yaml", src)

	// Schema entities exist (definitions form).
	wantSchemas := []string{"Pet", "Owner", "PetList"}
	for _, name := range wantSchemas {
		if findByName(results, "openapi_schema_"+name) == nil {
			t.Errorf("expected schema entity for %q (Swagger 2 definitions form), names=%v", name, entityNames(results))
		}
	}

	// Inter-schema $ref preserved (Pet -> Owner via #/definitions/Owner).
	pet := findByName(results, "openapi_schema_Pet")
	if pet == nil {
		t.Fatal("Pet schema not found")
	}
	hasOwnerRef := false
	for _, r := range pet.Relationships {
		if r.Kind == "REFERENCES" && r.ToID == "openapi_schema_Owner" {
			hasOwnerRef = true
		}
	}
	if !hasOwnerRef {
		t.Error("expected Swagger-2 Pet → Owner REFERENCES via #/definitions/Owner")
	}

	// Spec entity uses the Swagger title.
	if findByName(results, "openapi_spec_Swagger2 Petstore") == nil {
		t.Error("expected spec entity 'openapi_spec_Swagger2 Petstore'")
	}

	// Operation -> Schema REFERENCES still wires up.
	post := findByName(results, "openapi_op_post__pets")
	if post == nil {
		t.Fatal("post /pets operation not found")
	}
	hasPetRef := false
	for _, r := range post.Relationships {
		if r.Kind == "REFERENCES" && r.ToID == "openapi_schema_Pet" {
			hasPetRef = true
		}
	}
	if !hasPetRef {
		t.Error("expected Swagger-2 post /pets → Pet REFERENCES")
	}
}

// TestSwagger2_ParameterRefResolves verifies Swagger-2 #/parameters/Foo
// produces operation → parameter REFERENCES.
func TestSwagger2_ParameterRefResolves(t *testing.T) {
	d := &openAPIExtractor{}
	src := loadOpenAPIFixture(t, "swagger2_petstore.yml")
	results := d.Detect("api/swagger.yml", "yaml", src)

	listOp := findByName(results, "openapi_op_get__pets")
	if listOp == nil {
		t.Fatal("get /pets operation not found")
	}
	wantParams := map[string]bool{"Limit": false, "Offset": false}
	for _, r := range listOp.Relationships {
		if r.Kind == "REFERENCES" && r.Properties["reference_kind"] == "parameter_ref" {
			for k := range wantParams {
				if r.ToID == "openapi_parameter_"+k {
					wantParams[k] = true
				}
			}
		}
	}
	for k, v := range wantParams {
		if !v {
			t.Errorf("expected Swagger-2 get /pets → parameter %s", k)
		}
	}

	for _, name := range []string{"Limit", "Offset"} {
		ent := findByName(results, "openapi_parameter_"+name)
		if ent == nil {
			t.Errorf("expected Swagger-2 parameter entity %q", name)
			continue
		}
		if ent.Kind != "SCOPE.Schema" {
			t.Errorf("parameter %q kind=%q, want SCOPE.Schema", name, ent.Kind)
		}
	}
}

// TestOpenAPI_CompositionFixture_AllSchemasContained verifies all composed
// schemas are emitted and contained by the spec.
func TestOpenAPI_CompositionFixture_AllSchemasContained(t *testing.T) {
	d := &openAPIExtractor{}
	src := loadOpenAPIFixture(t, "openapi_composition.yml")
	results := d.Detect("api/openapi.yml", "yaml", src)

	want := []string{"Animal", "Pet", "Cat", "Dog", "Wild", "AnimalList"}
	for _, name := range want {
		ent := findByName(results, "openapi_schema_"+name)
		if ent == nil {
			t.Errorf("expected schema %q", name)
			continue
		}
		if ent.Kind != "SCOPE.Schema" {
			t.Errorf("schema %q kind=%q, want SCOPE.Schema", name, ent.Kind)
		}
	}

	// Sanity: total entities returned for this fixture is healthy.
	if len(results) == 0 {
		t.Fatal("expected entities, got none")
	}
	// Ensure the typed list is not just SCOPE.Schema.
	kinds := map[string]bool{}
	for _, e := range results {
		kinds[e.Kind] = true
	}
	for _, k := range []string{"SCOPE.Config", "SCOPE.Operation", "SCOPE.Schema"} {
		if !kinds[k] {
			t.Errorf("expected at least one entity of kind %q", k)
		}
	}
	_ = types.EntityRecord{} // keep the import live if removed later
}
