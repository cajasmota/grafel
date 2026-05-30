package cpp_test

// validation_test.go — fixture tests for validation.go

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
)

// extractFull returns full EntityRecords (with Properties) so tests can assert
// on field_type, description, struct_type and validation properties.
func extractFull(t *testing.T, name string, file extreg.FileInput) []propEntity {
	t.Helper()
	e, ok := extreg.Get(name)
	if !ok {
		t.Fatalf("extractor %q not registered", name)
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	var out []propEntity
	for _, ent := range ents {
		out = append(out, propEntity{Name: ent.Name, Props: ent.Properties})
	}
	return out
}

type propEntity struct {
	Name  string
	Props map[string]string
}

// fieldProp returns the first non-empty value of prop across all entities
// named field. A field name can be emitted under multiple framework lenses
// (e.g. cpprestsdk j["x"] and generic j["x"]); the property we assert on may
// live on either record.
func fieldProp(ents []propEntity, field, prop string) string {
	for _, e := range ents {
		if e.Name == field {
			if v := e.Props[prop]; v != "" {
				return v
			}
		}
	}
	return ""
}

func TestValidationDTOField(t *testing.T) {
	src := `
class UserDto : public oatpp::DTO {
  DTO_FIELD(String, username);
  DTO_FIELD(Int32, age);
}
`
	ents := extract(t, "custom_cpp_validation", fi("dto.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Schema", "username") {
		t.Errorf("expected username DTO field, got %v", ents)
	}
	if !containsEntity(ents, "SCOPE.Schema", "age") {
		t.Errorf("expected age DTO field, got %v", ents)
	}
}

func TestValidationDrogonGetParameter(t *testing.T) {
	src := `
void handler(const HttpRequestPtr& req, ResponseCallback&& cb) {
    auto id = req->getParameter("user_id");
    auto token = req->getParameter("token");
}
`
	ents := extract(t, "custom_cpp_validation", fi("handler.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Schema", "user_id") {
		t.Errorf("expected user_id param, got %v", ents)
	}
	if !containsEntity(ents, "SCOPE.Schema", "token") {
		t.Errorf("expected token param, got %v", ents)
	}
}

func TestValidationGenericGetParam(t *testing.T) {
	src := `auto name = req.getParam("name");`
	ents := extract(t, "custom_cpp_validation", fi("handler.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Schema", "name") {
		t.Errorf("expected name param from getParam, got %v", ents)
	}
}

func TestValidationCppRestJSONField(t *testing.T) {
	src := `
auto body = request.extract_json().get();
auto username = body["username"].as_string();
`
	ents := extract(t, "custom_cpp_validation", fi("handler.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Schema", "username") {
		t.Errorf("expected username from JSON field access, got %v", ents)
	}
}

func TestValidationNlohmannJSON(t *testing.T) {
	src := `
nlohmann::json j = nlohmann::json::parse(req.body);
auto email = j["email"];
auto pass = j.at("password");
`
	ents := extract(t, "custom_cpp_validation", fi("handler.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Schema", "email") {
		t.Errorf("expected email from nlohmann JSON, got %v", ents)
	}
	if !containsEntity(ents, "SCOPE.Schema", "password") {
		t.Errorf("expected password from nlohmann JSON at(), got %v", ents)
	}
}

func TestValidationNoMatch(t *testing.T) {
	src := `#include <crow.h>
void handler() { std::cout << "hello"; }`
	ents := extract(t, "custom_cpp_validation", fi("handler.cpp", "cpp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

func TestValidationWrongLanguage(t *testing.T) {
	src := `DTO_FIELD(String, username);`
	ents := extract(t, "custom_cpp_validation", fi("dto.c", "c", src))
	if len(ents) != 0 {
		t.Errorf("wrong language should return no entities, got %d", len(ents))
	}
}

// --- Deep VALUE-ASSERTING tests (field type / description / struct mapping) ---

func TestValidationDTOFieldType(t *testing.T) {
	src := `
class UserDto : public oatpp::DTO {
  DTO_INIT(UserDto, DTO)
  DTO_FIELD(String, email);
  DTO_FIELD(Int32, age);
  DTO_FIELD(Vector<String>, roles);
  DTO_FIELD(oatpp::Boolean, active);
}
`
	ents := extractFull(t, "custom_cpp_validation", fi("dto.cpp", "cpp", src))
	if got := fieldProp(ents, "email", "field_type"); got != "String" {
		t.Errorf("email field_type = %q, want String", got)
	}
	if got := fieldProp(ents, "age", "field_type"); got != "Int32" {
		t.Errorf("age field_type = %q, want Int32", got)
	}
	if got := fieldProp(ents, "roles", "field_type"); got != "Vector<String>" {
		t.Errorf("roles field_type = %q, want Vector<String>", got)
	}
	if got := fieldProp(ents, "active", "field_type"); got != "oatpp::Boolean" {
		t.Errorf("active field_type = %q, want oatpp::Boolean", got)
	}
}

func TestValidationDTOFieldInfoDescription(t *testing.T) {
	src := `
class UserDto : public oatpp::DTO {
  DTO_INIT(UserDto, DTO)
  DTO_FIELD(String, email);
  DTO_FIELD_INFO(email) {
    info->description = "User email address";
  }
}
`
	ents := extractFull(t, "custom_cpp_validation", fi("dto.cpp", "cpp", src))
	if got := fieldProp(ents, "email", "description"); got != "User email address" {
		t.Errorf("email description = %q, want %q", got, "User email address")
	}
	// type must still be captured alongside the description
	if got := fieldProp(ents, "email", "field_type"); got != "String" {
		t.Errorf("email field_type = %q, want String", got)
	}
}

func TestValidationNlohmannDefineType(t *testing.T) {
	src := `
struct User {
  std::string name;
  int age;
};
NLOHMANN_DEFINE_TYPE_INTRUSIVE(User, name, age)
`
	ents := extractFull(t, "custom_cpp_validation", fi("user.hpp", "cpp", src))
	if !containsName(ents, "name") || !containsName(ents, "age") {
		t.Fatalf("expected name+age fields from NLOHMANN_DEFINE_TYPE, got %v", ents)
	}
	if got := fieldProp(ents, "name", "struct_type"); got != "User" {
		t.Errorf("name struct_type = %q, want User", got)
	}
	if got := fieldProp(ents, "age", "struct_type"); got != "User" {
		t.Errorf("age struct_type = %q, want User", got)
	}
}

func TestValidationNlohmannDefineTypeNonIntrusive(t *testing.T) {
	src := `NLOHMANN_DEFINE_TYPE_NON_INTRUSIVE(Address, street, city, zip)`
	ents := extractFull(t, "custom_cpp_validation", fi("addr.hpp", "cpp", src))
	for _, f := range []string{"street", "city", "zip"} {
		if got := fieldProp(ents, f, "struct_type"); got != "Address" {
			t.Errorf("%s struct_type = %q, want Address", f, got)
		}
	}
}

func TestValidationNlohmannRequiredContains(t *testing.T) {
	src := `
nlohmann::json j = nlohmann::json::parse(req.body);
if (!j.contains("email")) { throw std::runtime_error("missing email"); }
auto email = j["email"];
`
	ents := extractFull(t, "custom_cpp_validation", fi("handler.cpp", "cpp", src))
	if got := fieldProp(ents, "email", "validation"); got != "required" {
		t.Errorf("email validation = %q, want required", got)
	}
}

func containsName(ents []propEntity, name string) bool {
	for _, e := range ents {
		if e.Name == name {
			return true
		}
	}
	return false
}
