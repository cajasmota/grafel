package kotlin_test

// ---------------------------------------------------------------------------
// Validation extractor tests
// ---------------------------------------------------------------------------

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
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

func TestValidationPlainDataClassEmitsDTOOnly(t *testing.T) {
	// A plain data class (no validation annotations) is still a DTO: dto_extraction
	// must emit the schema with its properties, but no request_validation rules.
	src := `data class User(val name: String, val age: Int)`
	ents := extract(t, "custom_kotlin_validation", fi("User.kt", "kotlin", src))
	if !containsEntity(ents, "SCOPE.Schema", "User") {
		t.Fatal("expected User DTO schema entity for plain data class")
	}
	for _, e := range ents {
		if e.Subtype == "request_validation" {
			t.Errorf("plain data class should not emit request_validation, got %q", e.Name)
		}
	}
	dto := findEntity(ents, "SCOPE.Schema", "User")
	if dto.Props["prop.name.type"] != "String" {
		t.Errorf("expected name:String, got %q", dto.Props["prop.name.type"])
	}
	if dto.Props["prop.age.type"] != "Int" {
		t.Errorf("expected age:Int, got %q", dto.Props["prop.age.type"])
	}
}

func TestValidationEmptyFile(t *testing.T) {
	ents := extract(t, "custom_kotlin_validation", fi("Empty.kt", "kotlin", ""))
	if len(ents) != 0 {
		t.Errorf("empty file should return no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Deep value-asserting tests (TS/JS bar): specific field + constraint + bound.
// ---------------------------------------------------------------------------

// request_validation: per-field rule with the SPECIFIC field name, constraint
// kind and parsed bound — not mere annotation presence.
func TestValidationBeanRulesFieldConstraintBounds(t *testing.T) {
	src := `
data class CreateUserRequest(
    @field:NotBlank val name: String,
    @field:Size(min = 1, max = 20) val username: String,
    @field:Email val email: String,
    @field:Min(18) val age: Int,
    @field:Pattern(regexp = "\\d{10}") val phone: String
)
`
	ents := extract(t, "custom_kotlin_validation", fi("CreateUserRequest.kt", "kotlin", src))

	// email: @Email on the email field
	if e := findEntity(ents, "SCOPE.Pattern", "validation:rule:CreateUserRequest.email:Email"); e == nil {
		t.Error("expected email -> @Email rule")
	} else if e.Props["field_name"] != "email" || e.Props["constraint"] != "Email" {
		t.Errorf("email rule props wrong: %+v", e.Props)
	}

	// name: Size(min=1,max=20) bound capture on username
	if e := findEntity(ents, "SCOPE.Pattern", "validation:rule:CreateUserRequest.username:Size"); e == nil {
		t.Error("expected username -> Size rule")
	} else if e.Props["min"] != "1" || e.Props["max"] != "20" {
		t.Errorf("expected Size(min=1,max=20), got min=%q max=%q", e.Props["min"], e.Props["max"])
	}

	// Min(18) value bound on age
	if e := findEntity(ents, "SCOPE.Pattern", "validation:rule:CreateUserRequest.age:Min"); e == nil {
		t.Error("expected age -> Min rule")
	} else if e.Props["value"] != "18" {
		t.Errorf("expected Min value=18, got %q", e.Props["value"])
	}

	// Pattern(regexp=...) on phone
	if e := findEntity(ents, "SCOPE.Pattern", "validation:rule:CreateUserRequest.phone:Pattern"); e == nil {
		t.Error("expected phone -> Pattern rule")
	} else if e.Props["regexp"] != `\\d{10}` {
		// Kotlin source contains the escaped form `\\d{10}`; the extractor
		// reports the literal token verbatim.
		t.Errorf("expected Pattern regexp=\\\\d{10}, got %q", e.Props["regexp"])
	}

	// NotBlank on name
	if e := findEntity(ents, "SCOPE.Pattern", "validation:rule:CreateUserRequest.name:NotBlank"); e == nil {
		t.Error("expected name -> NotBlank rule")
	}
}

// dto_extraction: property name + type + nullability + wire-name override.
func TestValidationDTOPropertyShape(t *testing.T) {
	src := `
@Serializable
data class AddressDto(
    @SerialName("street_name") val streetName: String,
    val unit: String? = null,
    @JsonProperty("zip") val zipCode: String,
    val country: String = "US"
)
`
	ents := extract(t, "custom_kotlin_validation", fi("AddressDto.kt", "kotlin", src))
	dto := findEntity(ents, "SCOPE.Schema", "AddressDto")
	if dto == nil {
		t.Fatal("expected AddressDto DTO schema")
	}
	// kotlinx-serialization @SerialName wire override
	if dto.Props["prop.streetName.wire_name"] != "street_name" {
		t.Errorf("expected streetName wire_name=street_name, got %q", dto.Props["prop.streetName.wire_name"])
	}
	// nullability: String?
	if dto.Props["prop.unit.nullable"] != "true" {
		t.Errorf("expected unit nullable=true, got %q", dto.Props["prop.unit.nullable"])
	}
	if dto.Props["prop.streetName.nullable"] != "false" {
		t.Errorf("expected streetName nullable=false, got %q", dto.Props["prop.streetName.nullable"])
	}
	// Jackson @JsonProperty wire override
	if dto.Props["prop.zipCode.wire_name"] != "zip" {
		t.Errorf("expected zipCode wire_name=zip, got %q", dto.Props["prop.zipCode.wire_name"])
	}
	// default capture
	if dto.Props["prop.country.default"] != `"US"` {
		t.Errorf("expected country default=\"US\", got %q", dto.Props["prop.country.default"])
	}
	if dto.Props["prop.country.type"] != "String" {
		t.Errorf("expected country type=String, got %q", dto.Props["prop.country.type"])
	}
}

// konform DSL: Validation<Foo> { Foo::bar { minLength(1) } } — specific field +
// constraint + bound, not len>0.
func TestValidationKonformDSL(t *testing.T) {
	src := `
val userValidation = Validation<UserDto> {
    UserDto::name {
        minLength(1)
        maxLength(20)
    }
    UserDto::email {
        pattern(".+@.+")
    }
}
`
	ents := extract(t, "custom_kotlin_validation", fi("UserValidation.kt", "kotlin", src))

	if !containsEntity(ents, "SCOPE.Schema", "UserDto") {
		t.Error("expected UserDto DTO schema from konform Validation<UserDto>")
	}
	if e := findEntity(ents, "SCOPE.Pattern", "validation:rule:UserDto.name:minLength"); e == nil {
		t.Error("expected name -> minLength rule")
	} else if e.Props["bound"] != "1" || e.Props["field_name"] != "name" {
		t.Errorf("expected minLength bound=1 field=name, got %+v", e.Props)
	}
	if e := findEntity(ents, "SCOPE.Pattern", "validation:rule:UserDto.name:maxLength"); e == nil {
		t.Error("expected name -> maxLength rule")
	} else if e.Props["bound"] != "20" {
		t.Errorf("expected maxLength bound=20, got %q", e.Props["bound"])
	}
	if e := findEntity(ents, "SCOPE.Pattern", "validation:rule:UserDto.email:pattern"); e == nil {
		t.Error("expected email -> pattern rule")
	} else if e.Props["bound"] != ".+@.+" {
		t.Errorf("expected pattern bound=.+@.+, got %q", e.Props["bound"])
	}
}

// Bare (non-field:) bean annotations still attribute to the right field.
func TestValidationBareBeanAnnotationField(t *testing.T) {
	src := `
data class LoginRequest(
    @NotNull val username: String,
    @Size(max = 64) val password: String
)
`
	ents := extract(t, "custom_kotlin_validation", fi("LoginRequest.kt", "kotlin", src))
	if e := findEntity(ents, "SCOPE.Pattern", "validation:rule:LoginRequest.password:Size"); e == nil {
		t.Error("expected password -> Size rule")
	} else if e.Props["max"] != "64" {
		t.Errorf("expected Size max=64, got %q", e.Props["max"])
	}
	if findEntity(ents, "SCOPE.Pattern", "validation:rule:LoginRequest.username:NotNull") == nil {
		t.Error("expected username -> NotNull rule")
	}
}

// ---------------------------------------------------------------------------
// Nested @Valid → VALIDATES edge (#4972, parity with Java #3605).
// A @Valid-annotated DTO field cascades validation into the nested DTO type;
// the owning DTO must carry a VALIDATES edge to that nested type.
// ---------------------------------------------------------------------------

func TestValidationNestedValidVALIDATESEdge(t *testing.T) {
	src := `
data class OrderRequest(
    @field:NotNull val id: String,
    @field:Valid val shippingAddress: AddressDto,
    @get:Valid val billingAddress: AddressDto?,
    @field:Valid val items: List<LineItemDto>,
    val notes: String?
)

data class AddressDto(
    @field:NotBlank val street: String
)

data class LineItemDto(
    @field:Min(1) val quantity: Int
)
`
	ents := extractRels(t, "custom_kotlin_validation", fi("OrderRequest.kt", "kotlin", src))

	// Owner DTO → nested DTO via @field:Valid.
	if !hasEdge(ents, "OrderRequest", "VALIDATES", "AddressDto") {
		t.Errorf("expected OrderRequest VALIDATES AddressDto; edges=%v", dumpValidatesEdges(ents))
	}
	// Collection element form: List<LineItemDto> unwraps to LineItemDto.
	if !hasEdge(ents, "OrderRequest", "VALIDATES", "LineItemDto") {
		t.Errorf("expected OrderRequest VALIDATES LineItemDto (List element); edges=%v", dumpValidatesEdges(ents))
	}
	// Edge carries the field + via metadata.
	for _, e := range ents {
		if e.Name != "OrderRequest" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "VALIDATES" && r.ToID == "AddressDto" {
				if r.Properties["via"] != "valid_annotation" {
					t.Errorf("expected via=valid_annotation, got %q", r.Properties["via"])
				}
				if r.Properties["field"] == "" {
					t.Error("expected non-empty field on VALIDATES edge")
				}
			}
		}
	}
	// A non-@Valid field must NOT produce a VALIDATES edge.
	if hasEdge(ents, "OrderRequest", "VALIDATES", "String") {
		t.Error("plain (non-@Valid) field should not emit a VALIDATES edge")
	}
}

// Generic-element annotation form: List<@Valid AddressDto> — the @Valid sits on
// the element type inside the generic, not on the property annotation block.
func TestValidationNestedValidElementForm(t *testing.T) {
	src := `
data class BatchRequest(
    val addresses: List<@Valid AddressDto>
)

data class AddressDto(
    @field:NotBlank val street: String
)
`
	ents := extractRels(t, "custom_kotlin_validation", fi("BatchRequest.kt", "kotlin", src))
	if !hasEdge(ents, "BatchRequest", "VALIDATES", "AddressDto") {
		t.Errorf("expected BatchRequest VALIDATES AddressDto (List<@Valid X>); edges=%v", dumpValidatesEdges(ents))
	}
}

func dumpValidatesEdges(ents []types.EntityRecord) string {
	out := ""
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "VALIDATES" {
				out += "\n  " + e.Name + " -VALIDATES-> " + r.ToID
			}
		}
	}
	if out == "" {
		return "(none)"
	}
	return out
}
