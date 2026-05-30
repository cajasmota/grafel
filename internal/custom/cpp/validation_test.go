package cpp_test

// validation_test.go — fixture tests for validation.go

import "testing"

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
