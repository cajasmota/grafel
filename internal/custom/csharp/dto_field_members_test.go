package csharp_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/csharp"
)

// Issue #4715 — C#/.NET DataAnnotations DTO FIELD-as-member indexing. A DTO
// class's properties + DataAnnotation attributes must be emitted as
// SCOPE.Schema/field members with name, normalized type, optional, validators,
// AND a CONTAINS edge back to the class — the SAME shape as the JS/Python/Java/Go
// DTO field members.

func TestCsharpDTO_FieldMembers(t *testing.T) {
	src := `
using System.ComponentModel.DataAnnotations;

public class CreateUserRequest
{
    [Required]
    [StringLength(100)]
    public string Name { get; set; }

    [EmailAddress]
    public string? Email { get; set; }

    [Range(0, 130)]
    public int Age { get; set; }

    public string? Bio { get; set; }
}
`
	e, ok := extreg.Get("custom_csharp_validation")
	if !ok {
		t.Fatal("custom_csharp_validation not registered")
	}
	ents, err := e.Extract(context.Background(),
		extreg.FileInput{Path: "CreateUserRequest.cs", Language: "csharp", Content: []byte(src)})
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

	name := field("CreateUserRequest.Name")
	if name == nil {
		t.Fatal("expected CreateUserRequest.Name field member")
	}
	if name.Properties["field_type"] != "string" {
		t.Errorf("Name type = %q, want string", name.Properties["field_type"])
	}
	if name.Properties["optional"] == "true" {
		t.Errorf("Name ([Required], non-nullable) should not be optional, props=%v", name.Properties)
	}
	if name.Properties["validators"] == "" {
		t.Errorf("Name should carry @Required/@StringLength validators, props=%v", name.Properties)
	}
	if name.Signature == "" {
		t.Error("Name field must carry a Signature for the shape resolver")
	}
	if !containsTo(name.ID) {
		t.Error("expected CONTAINS edge to CreateUserRequest.Name")
	}

	email := field("CreateUserRequest.Email")
	if email == nil || email.Properties["field_type"] != "string" {
		t.Fatalf("expected CreateUserRequest.Email:string, got %+v", email)
	}
	if email.Properties["optional"] != "true" {
		t.Errorf("Email (string?, no [Required]) should be optional, props=%v", email.Properties)
	}

	age := field("CreateUserRequest.Age")
	if age == nil || age.Properties["field_type"] != "integer" {
		t.Fatalf("expected CreateUserRequest.Age:integer, got %+v", age)
	}
	if age.Properties["optional"] == "true" {
		t.Errorf("Age (non-nullable int) should not be optional, props=%v", age.Properties)
	}

	bio := field("CreateUserRequest.Bio")
	if bio == nil || bio.Properties["optional"] != "true" {
		t.Fatalf("Bio (string?) should be optional, got %+v", bio)
	}
}
