package golang_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/golang"
)

// Issue #4715 — Go struct-tag DTO FIELD-as-member indexing. A request-bound DTO
// struct's fields must be emitted as SCOPE.Schema/field members with name (from
// the json tag), normalized type, optional, validators, AND a CONTAINS edge back
// to the owning struct — the SAME shape as the JS/Python/Java DTO field members.

func TestGoDTO_FieldMembers(t *testing.T) {
	src := "package api\n\n" +
		"import \"github.com/gin-gonic/gin\"\n\n" +
		"type CreateUserReq struct {\n" +
		"\tName  string `json:\"name\" validate:\"required\"`\n" +
		"\tEmail string `json:\"email\" binding:\"required,email\"`\n" +
		"\tAge   int    `json:\"age,omitempty\"`\n" +
		"\tBio   *string `json:\"bio\"`\n" +
		"}\n\n" +
		"func handler(c *gin.Context) {\n" +
		"\tvar req CreateUserReq\n" +
		"\tc.ShouldBindJSON(&req)\n" +
		"}\n"

	e, ok := extreg.Get("custom_go_dto")
	if !ok {
		t.Fatal("custom_go_dto not registered")
	}
	ents, err := e.Extract(context.Background(),
		extreg.FileInput{Path: "user.go", Language: "go", Content: []byte(src)})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	field := func(name string) *types.EntityRecord {
		for i := range ents {
			if ents[i].Name == name && ents[i].Kind == "SCOPE.Schema" && ents[i].Subtype == "field" {
				return &ents[i]
			}
		}
		return nil
	}
	containsTo := func(id string) bool {
		for _, e := range ents {
			for _, r := range e.Relationships {
				if r.Kind == string(types.RelationshipKindContains) && r.ToID == id {
					return true
				}
			}
		}
		return false
	}

	name := field("CreateUserReq.name")
	if name == nil {
		t.Fatal("expected CreateUserReq.name field member")
	}
	if name.Properties["field_type"] != "string" {
		t.Errorf("name type = %q, want string", name.Properties["field_type"])
	}
	if name.Properties["optional"] == "true" {
		t.Errorf("name (validate:required) should not be optional, props=%v", name.Properties)
	}
	if name.Properties["validators"] == "" {
		t.Errorf("name should carry @required validator, props=%v", name.Properties)
	}
	if name.Signature == "" {
		t.Error("name field must carry a Signature for the shape resolver")
	}
	if !containsTo(name.ID) {
		t.Error("expected CONTAINS edge to CreateUserReq.name")
	}

	email := field("CreateUserReq.email")
	if email == nil || email.Properties["field_type"] != "string" {
		t.Fatalf("expected CreateUserReq.email:string, got %+v", email)
	}
	if email.Properties["optional"] == "true" {
		t.Errorf("email (binding:required) should not be optional, props=%v", email.Properties)
	}

	age := field("CreateUserReq.age")
	if age == nil || age.Properties["field_type"] != "integer" {
		t.Fatalf("expected CreateUserReq.age:integer, got %+v", age)
	}
	if age.Properties["optional"] != "true" {
		t.Errorf("age (omitempty, no required) should be optional, props=%v", age.Properties)
	}

	bio := field("CreateUserReq.bio")
	if bio == nil || bio.Properties["optional"] != "true" || bio.Properties["field_type"] != "string" {
		t.Fatalf("bio (*string) should be optional string, got %+v", bio)
	}
}
