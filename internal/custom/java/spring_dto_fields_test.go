package java

import (
	"strings"
	"testing"
)

// spring_dto_fields_test.go — tests for Spring DTO FIELD-as-member indexing
// (#4613). Mirrors the JS/TS (#4635) and Python (#4613) field-membership model:
// each POJO field / record component of a Spring DTO becomes a
// SCOPE.Schema/field member carrying name/type/optional/validators + a CONTAINS
// edge to the owning class.

func sdfCtx(source, filePath string) PatternContext {
	return PatternContext{
		Source: source, Language: "java", Framework: "spring_boot", FilePath: filePath,
	}
}

func fieldMember(res PatternResult, name string) *SecondaryEntity {
	for i := range res.Entities {
		if res.Entities[i].Name == name &&
			res.Entities[i].Kind == "SCOPE.Schema" &&
			res.Entities[i].Subtype == "field" {
			return &res.Entities[i]
		}
	}
	return nil
}

func hasContainsRel(res PatternResult, ownerStub, targetRefSuffix string) bool {
	for _, r := range res.Relationships {
		if r.RelationshipType == "CONTAINS" &&
			r.FromName == ownerStub &&
			strings.HasSuffix(r.TargetRef, targetRefSuffix) {
			return true
		}
	}
	return false
}

func TestSpringDTO_PojoFieldMembers(t *testing.T) {
	src := `package com.example;

public class CreateUserRequest {
    @NotBlank
    private String name;
    private int age;
    private Optional<String> nickname;
}`
	res := ExtractSpringDTOFields(sdfCtx(src, "CreateUserRequest.java"))

	name := fieldMember(res, "CreateUserRequest.name")
	if name == nil {
		t.Fatal("expected field member CreateUserRequest.name")
	}
	if name.Properties["field_type"] != "string" {
		t.Errorf("name type = %v, want string", name.Properties["field_type"])
	}
	if name.Properties["required"] != "true" {
		t.Errorf("name (@NotBlank) should be required, props=%v", name.Properties)
	}
	if v, _ := name.Properties["validators"].(string); !strings.Contains(v, "@NotBlank") {
		t.Errorf("name should carry @NotBlank validator, props=%v", name.Properties)
	}

	age := fieldMember(res, "CreateUserRequest.age")
	if age == nil || age.Properties["field_type"] != "integer" {
		t.Fatalf("expected CreateUserRequest.age:integer, got %+v", age)
	}
	if age.Properties["required"] != "true" {
		t.Errorf("primitive int age should be required, props=%v", age.Properties)
	}

	nick := fieldMember(res, "CreateUserRequest.nickname")
	if nick == nil {
		t.Fatal("expected CreateUserRequest.nickname")
	}
	if nick.Properties["optional"] != "true" {
		t.Errorf("Optional<String> nickname should be optional, props=%v", nick.Properties)
	}

	// CONTAINS membership edges (FromName = Class:owner).
	if !hasContainsRel(res, "Class:CreateUserRequest", "CreateUserRequest.name") {
		t.Error("expected CONTAINS edge Class:CreateUserRequest -> CreateUserRequest.name")
	}
}

func TestSpringDTO_RecordComponents(t *testing.T) {
	src := `package com.example;

public record CreateOrder(@NotNull Long productId, int quantity, String note) {}`
	res := ExtractSpringDTOFields(sdfCtx(src, "CreateOrder.java"))

	pid := fieldMember(res, "CreateOrder.productId")
	if pid == nil || pid.Properties["field_type"] != "integer" {
		t.Fatalf("expected CreateOrder.productId:integer, got %+v", pid)
	}
	if pid.Properties["required"] != "true" {
		t.Errorf("productId (@NotNull) should be required, props=%v", pid.Properties)
	}
	qty := fieldMember(res, "CreateOrder.quantity")
	if qty == nil || qty.Properties["field_type"] != "integer" {
		t.Fatalf("expected CreateOrder.quantity:integer, got %+v", qty)
	}
	note := fieldMember(res, "CreateOrder.note")
	if note == nil || note.Properties["field_type"] != "string" {
		t.Fatalf("expected CreateOrder.note:string, got %+v", note)
	}
	if !hasContainsRel(res, "Class:CreateOrder", "CreateOrder.productId") {
		t.Error("expected CONTAINS edge to CreateOrder.productId")
	}
}

// A class referenced by @RequestBody but without a DTO-suffix name still gets
// field members.
func TestSpringDTO_RequestBodyTypeWithoutSuffix(t *testing.T) {
	src := `package com.example;

@RestController
class C {
    @PostMapping("/p")
    public void create(@RequestBody Payload p) {}
}

class Payload {
    private String title;
}`
	res := ExtractSpringDTOFields(sdfCtx(src, "C.java"))
	if fieldMember(res, "Payload.title") == nil {
		t.Fatal("expected @RequestBody Payload.title field member")
	}
}

// Stereotype-annotated classes (controllers/services) must NOT get DTO members.
func TestSpringDTO_SkipsStereotypes(t *testing.T) {
	src := `package com.example;

@Service
public class UserService {
    private final UserRepo repo;
}`
	res := ExtractSpringDTOFields(sdfCtx(src, "UserService.java"))
	for _, e := range res.Entities {
		if strings.HasPrefix(e.Name, "UserService.") {
			t.Errorf("@Service class must not emit DTO field members, got %s", e.Name)
		}
	}
}

// Non-spring framework → no-op.
func TestSpringDTO_FrameworkGate(t *testing.T) {
	src := `public class FooRequest { private String x; }`
	res := ExtractSpringDTOFields(PatternContext{
		Source: src, Language: "java", Framework: "quarkus", FilePath: "F.java",
	})
	if len(res.Entities) != 0 {
		t.Errorf("non-spring framework should emit nothing, got %d", len(res.Entities))
	}
}
