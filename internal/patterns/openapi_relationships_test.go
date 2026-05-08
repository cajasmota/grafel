package patterns

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func loadOpenAPIFixture(t *testing.T, name string) string {
	t.Helper()
	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filename)
	path := filepath.Join(dir, "testdata", name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

func findByName(entities []types.EntityRecord, name string) *types.EntityRecord {
	for i := range entities {
		if entities[i].Name == name {
			return &entities[i]
		}
	}
	return nil
}

// TestOpenAPI_SchemaEntitiesEmitted verifies schema entities exist.
func TestOpenAPI_SchemaEntitiesEmitted(t *testing.T) {
	d := &openAPIExtractor{}
	src := loadOpenAPIFixture(t, "openapi_petstore.yml")
	results := d.Detect("api/openapi.yml", "yaml", src)

	wantSchemas := []string{"Pet", "Owner", "PetList"}
	for _, name := range wantSchemas {
		if findByName(results, "openapi_schema_"+name) == nil {
			t.Errorf("expected schema entity for %q", name)
		}
	}
}

// TestOpenAPI_OperationReferencesSchema verifies Operation → Schema REFERENCES.
func TestOpenAPI_OperationReferencesSchema(t *testing.T) {
	d := &openAPIExtractor{}
	src := loadOpenAPIFixture(t, "openapi_petstore.yml")
	results := d.Detect("api/openapi.yml", "yaml", src)

	op := findByName(results, "openapi_op_post__pets")
	if op == nil {
		t.Fatal("post /pets operation not found")
	}
	hasPetRef := false
	for _, r := range op.Relationships {
		if r.Kind == "REFERENCES" && r.ToID == "openapi_schema_Pet" {
			hasPetRef = true
		}
	}
	if !hasPetRef {
		t.Errorf("expected post /pets to REFERENCE openapi_schema_Pet, got rels=%+v", op.Relationships)
	}
}

// TestOpenAPI_SchemaReferencesSchema verifies nested $ref between schemas.
func TestOpenAPI_SchemaReferencesSchema(t *testing.T) {
	d := &openAPIExtractor{}
	src := loadOpenAPIFixture(t, "openapi_petstore.yml")
	results := d.Detect("api/openapi.yml", "yaml", src)

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
		t.Error("expected Pet → Owner REFERENCES")
	}

	petList := findByName(results, "openapi_schema_PetList")
	if petList == nil {
		t.Fatal("PetList schema not found")
	}
	hasItemRef := false
	for _, r := range petList.Relationships {
		if r.Kind == "REFERENCES" && r.ToID == "openapi_schema_Pet" {
			hasItemRef = true
		}
	}
	if !hasItemRef {
		t.Error("expected PetList → Pet REFERENCES")
	}
}

// TestOpenAPI_SpecContainsOperationsAndSchemas verifies CONTAINS edges from spec.
func TestOpenAPI_SpecContainsOperationsAndSchemas(t *testing.T) {
	d := &openAPIExtractor{}
	src := loadOpenAPIFixture(t, "openapi_petstore.yml")
	results := d.Detect("api/openapi.yml", "yaml", src)

	// info.title is parsed via the YAML-aware helper, so the spec name uses
	// the real title from the fixture ("Petstore") rather than the "api"
	// fallback. (Refs #85)
	spec := findByName(results, "openapi_spec_Petstore")
	if spec == nil {
		t.Fatal("spec entity not found")
	}
	containsOps := 0
	containsSchemas := 0
	for _, r := range spec.Relationships {
		if r.Kind != "CONTAINS" {
			continue
		}
		switch r.Properties["contained_kind"] {
		case "operation":
			containsOps++
		case "schema":
			containsSchemas++
		}
	}
	if containsOps < 3 {
		t.Errorf("expected ≥3 operation CONTAINS edges, got %d", containsOps)
	}
	if containsSchemas < 3 {
		t.Errorf("expected ≥3 schema CONTAINS edges, got %d", containsSchemas)
	}
}

// TestOpenAPI_OperationTaggedAs verifies tag relationships (block + inline).
func TestOpenAPI_OperationTaggedAs(t *testing.T) {
	d := &openAPIExtractor{}
	src := loadOpenAPIFixture(t, "openapi_petstore.yml")
	results := d.Detect("api/openapi.yml", "yaml", src)

	// post /pets has inline `tags: [pets, write]`
	op := findByName(results, "openapi_op_post__pets")
	if op == nil {
		t.Fatal("post /pets operation not found")
	}
	gotTags := map[string]bool{}
	for _, r := range op.Relationships {
		if r.Kind == "TAGGED_AS" {
			gotTags[r.Properties["tag"]] = true
		}
	}
	if !gotTags["pets"] {
		t.Error("expected post /pets TAGGED_AS pets")
	}
	if !gotTags["write"] {
		t.Error("expected post /pets TAGGED_AS write (inline tag list)")
	}

	// get /pets has block-form tags
	getOp := findByName(results, "openapi_op_get__pets")
	if getOp == nil {
		t.Fatal("get /pets operation not found")
	}
	hasPetsTag := false
	for _, r := range getOp.Relationships {
		if r.Kind == "TAGGED_AS" && r.Properties["tag"] == "pets" {
			hasPetsTag = true
		}
	}
	if !hasPetsTag {
		t.Error("expected get /pets TAGGED_AS pets (block form)")
	}
}

// TestOpenAPI_NestedInfoTitleExtracted verifies info.title is parsed from the
// canonical nested YAML form rather than falling back to the literal "api".
// (Refs #85)
func TestOpenAPI_NestedInfoTitleExtracted(t *testing.T) {
	d := &openAPIExtractor{}
	src := `openapi: "3.0.0"
info:
  title: My API
  version: "1.0"
paths:
  /users:
    get:
      summary: list
`
	results := d.Detect("openapi.yaml", "yaml", src)
	if findByName(results, "openapi_spec_My API") == nil {
		t.Fatalf("expected spec entity with title 'My API', got names=%v", entityNames(results))
	}
	if findByName(results, "openapi_spec_api") != nil {
		t.Error("did not expect fallback 'api' title when info.title is present")
	}
}

// TestOpenAPI_MissingInfoTitleFallsBack verifies the "api" fallback still
// fires when no title is declared anywhere.
func TestOpenAPI_MissingInfoTitleFallsBack(t *testing.T) {
	d := &openAPIExtractor{}
	src := `openapi: "3.0.0"
paths:
  /users:
    get:
      summary: list
`
	results := d.Detect("openapi.yaml", "yaml", src)
	if findByName(results, "openapi_spec_api") == nil {
		t.Errorf("expected fallback spec name 'openapi_spec_api', got names=%v", entityNames(results))
	}
}

func entityNames(entities []types.EntityRecord) []string {
	out := make([]string, 0, len(entities))
	for _, e := range entities {
		out = append(out, e.Name)
	}
	return out
}
