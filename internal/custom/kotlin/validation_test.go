package kotlin_test

// ---------------------------------------------------------------------------
// Validation extractor tests
// ---------------------------------------------------------------------------

import (
	"testing"
)

func TestValidationAtValid(t *testing.T) {
	src := `
@RestController
class UserController {
    @PostMapping("/users")
    fun createUser(@Valid @RequestBody req: CreateUserRequest): ResponseEntity<User> {
        return ResponseEntity.ok(userService.create(req))
    }
}
`
	ents := extract(t, "custom_kotlin_validation", fi("UserController.kt", "kotlin", src))
	found := false
	for _, e := range ents {
		if e.Subtype == "request_validation" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected request_validation entity from @Valid annotation")
	}
}

func TestValidationAtValidated(t *testing.T) {
	src := `
@Controller
class AccountController {
    @PostMapping("/accounts")
    fun createAccount(@Validated body: AccountRequest): String {
        return "ok"
    }
}
`
	ents := extract(t, "custom_kotlin_validation", fi("AccountController.kt", "kotlin", src))
	found := false
	for _, e := range ents {
		if e.Subtype == "request_validation" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected request_validation entity from @Validated annotation")
	}
}

func TestValidationFieldAnnotations(t *testing.T) {
	src := `
data class CreateUserRequest(
    @NotNull val name: String,
    @Size(min = 1, max = 100) val username: String,
    @Email val email: String,
    @Pattern(regexp = "\\d+") val phone: String,
    @NotBlank val password: String
)
`
	ents := extract(t, "custom_kotlin_validation", fi("CreateUserRequest.kt", "kotlin", src))
	hasValidation := false
	hasDTO := false
	for _, e := range ents {
		if e.Subtype == "request_validation" {
			hasValidation = true
		}
		if e.Kind == "SCOPE.Schema" && e.Subtype == "dto" && e.Name == "CreateUserRequest" {
			hasDTO = true
		}
	}
	if !hasValidation {
		t.Error("expected request_validation entities from field annotations")
	}
	if !hasDTO {
		t.Error("expected DTO schema entity for CreateUserRequest")
	}
}

func TestValidationNotNullEmitsDTO(t *testing.T) {
	src := `
data class LoginRequest(
    @NotNull val username: String,
    @NotNull val password: String
)
`
	ents := extract(t, "custom_kotlin_validation", fi("LoginRequest.kt", "kotlin", src))
	if !containsEntity(ents, "SCOPE.Schema", "LoginRequest") {
		t.Error("expected LoginRequest DTO schema entity")
	}
}

func TestValidationContractBlock(t *testing.T) {
	src := `
class UserRequestValidator {
    fun validate(request: UserRequest) {
        validate<UserRequest>(request) {
            validate(UserRequest::name).hasSize(min = 1, max = 100)
            validate(UserRequest::email).isEmail()
        }
    }
}
`
	ents := extract(t, "custom_kotlin_validation", fi("UserValidator.kt", "kotlin", src))
	found := false
	for _, e := range ents {
		if e.Subtype == "request_validation" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected request_validation entity from validate() contract block")
	}
}

func TestValidationContractWithTypeEmitsDTO(t *testing.T) {
	src := `
fun validateOrder(order: OrderRequest) = validate<OrderRequest>(order) {
    validate(OrderRequest::amount).isPositive()
}
`
	ents := extract(t, "custom_kotlin_validation", fi("OrderValidator.kt", "kotlin", src))
	if !containsEntity(ents, "SCOPE.Schema", "OrderRequest") {
		t.Error("expected OrderRequest DTO schema entity from contract block")
	}
}

func TestValidationWrongLanguage(t *testing.T) {
	src := `@Valid @NotNull String name;`
	ents := extract(t, "custom_kotlin_validation", fi("Test.java", "java", src))
	if len(ents) != 0 {
		t.Errorf("wrong language should return no entities, got %d", len(ents))
	}
}

func TestValidationNoMatch(t *testing.T) {
	src := `data class User(val name: String, val age: Int)`
	ents := extract(t, "custom_kotlin_validation", fi("User.kt", "kotlin", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for plain data class, got %d", len(ents))
	}
}

func TestValidationEmptyFile(t *testing.T) {
	ents := extract(t, "custom_kotlin_validation", fi("Empty.kt", "kotlin", ""))
	if len(ents) != 0 {
		t.Errorf("empty file should return no entities, got %d", len(ents))
	}
}
