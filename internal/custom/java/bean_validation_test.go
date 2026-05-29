package java

// bean_validation_test.go — tests for the Bean Validation extractor (#3100).
//
// Coverage cells proven:
//   lang.java.validation.bean-validation → custom_validator_extraction  (missing → partial)
//   lang.java.validation.bean-validation → constraint_extraction        (partial → full)
//   lang.java.validation.bean-validation → nested_model_extraction      (partial → full)
//   lang.java.validation.bean-validation → schema_extraction            (partial → full)

import (
	"os"
	"strings"
	"testing"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// bvCtx builds a PatternContext for the bean-validation framework.
func bvCtx(source, filePath string) PatternContext {
	return PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "bean_validation",
		FilePath:  filePath,
	}
}

// entityWithKind returns the first entity whose Kind equals kind and whose Name
// equals name, or nil when not found.
func entityWithKind(entities []SecondaryEntity, kind, name string) *SecondaryEntity {
	for i := range entities {
		if entities[i].Kind == kind && entities[i].Name == name {
			return &entities[i]
		}
	}
	return nil
}

// entityByProvenance returns all entities whose Provenance equals p.
func entityByProvenance(entities []SecondaryEntity, p string) []SecondaryEntity {
	var out []SecondaryEntity
	for _, e := range entities {
		if e.Provenance == p {
			out = append(out, e)
		}
	}
	return out
}

// ── 1. custom_validator_extraction ────────────────────────────────────────────

// TestBeanValidation_ConstraintValidatorDetection_Issue3100 proves that a class
// implementing ConstraintValidator<A,T> is emitted as SCOPE.CustomValidator.
// Registry target: lang.java.validation.bean-validation → custom_validator_extraction → partial.
func TestBeanValidation_ConstraintValidatorDetection_Issue3100(t *testing.T) {
	source := `
package com.example;

import jakarta.validation.ConstraintValidator;
import jakarta.validation.ConstraintValidatorContext;

public class PhoneNumberValidator implements ConstraintValidator<ValidPhoneNumber, String> {
    @Override
    public void initialize(ValidPhoneNumber annotation) {}

    @Override
    public boolean isValid(String value, ConstraintValidatorContext context) {
        if (value == null) return true;
        return value.matches("\\+?[0-9]{7,15}");
    }
}
`
	r := ExtractBeanValidation(bvCtx(source, "PhoneNumberValidator.java"))

	e := entityWithKind(r.Entities, "SCOPE.CustomValidator", "PhoneNumberValidator")
	if e == nil {
		t.Fatalf("[#3100 custom-validator] expected SCOPE.CustomValidator entity for PhoneNumberValidator; got %v", entityNames(r.Entities))
	}
	if e.Provenance != "INFERRED_FROM_CONSTRAINT_VALIDATOR_IMPL" {
		t.Errorf("[#3100 custom-validator] provenance = %q, want INFERRED_FROM_CONSTRAINT_VALIDATOR_IMPL", e.Provenance)
	}
	if e.Properties["annotation_type"] != "ValidPhoneNumber" {
		t.Errorf("[#3100 custom-validator] annotation_type = %v, want ValidPhoneNumber", e.Properties["annotation_type"])
	}
	if e.Properties["validated_type"] != "String" {
		t.Errorf("[#3100 custom-validator] validated_type = %v, want String", e.Properties["validated_type"])
	}
}

// TestBeanValidation_MultipleCustomValidators_Issue3100 proves that two
// ConstraintValidator implementations in the same file are both detected.
func TestBeanValidation_MultipleCustomValidators_Issue3100(t *testing.T) {
	source := `
package com.example;

import jakarta.validation.ConstraintValidator;
import jakarta.validation.ConstraintValidatorContext;

public class PhoneNumberValidator implements ConstraintValidator<ValidPhoneNumber, String> {
    public boolean isValid(String v, ConstraintValidatorContext ctx) { return true; }
}

public class PositiveAmountValidator implements ConstraintValidator<PositiveAmount, java.math.BigDecimal> {
    public boolean isValid(java.math.BigDecimal v, ConstraintValidatorContext ctx) { return true; }
}
`
	r := ExtractBeanValidation(bvCtx(source, "Validators.java"))

	customs := entityByProvenance(r.Entities, "INFERRED_FROM_CONSTRAINT_VALIDATOR_IMPL")
	if len(customs) < 2 {
		t.Errorf("[#3100 multi-validator] expected 2 SCOPE.CustomValidator entities; got %d: %v",
			len(customs), entityNames(r.Entities))
	}
	names := entityNames(customs)
	if !contains(names, "PhoneNumberValidator") {
		t.Errorf("[#3100 multi-validator] PhoneNumberValidator missing from %v", names)
	}
	if !contains(names, "PositiveAmountValidator") {
		t.Errorf("[#3100 multi-validator] PositiveAmountValidator missing from %v", names)
	}
}

// TestBeanValidation_ConstraintValidatorWithSpringFramework_Issue3100 proves
// that the extractor fires for framework=spring_boot (not just bean_validation).
func TestBeanValidation_ConstraintValidatorWithSpringFramework_Issue3100(t *testing.T) {
	source := `
public class AgeValidator implements ConstraintValidator<ValidAge, Integer> {
    public boolean isValid(Integer age, ConstraintValidatorContext ctx) {
        return age != null && age >= 0 && age <= 150;
    }
}
`
	r := ExtractBeanValidation(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "spring_boot",
		FilePath:  "AgeValidator.java",
	})

	if entityWithKind(r.Entities, "SCOPE.CustomValidator", "AgeValidator") == nil {
		t.Errorf("[#3100 spring-validator] expected SCOPE.CustomValidator for AgeValidator in spring_boot; got %v", entityNames(r.Entities))
	}
}

// ── 2. constraint_extraction (bounds parsing) ─────────────────────────────────

// TestBeanValidation_ConstraintBoundsSize_Issue3100 proves that @Size(min, max)
// bounds are parsed and stored as properties.
// Registry target: lang.java.validation.bean-validation → constraint_extraction → full.
func TestBeanValidation_ConstraintBoundsSize_Issue3100(t *testing.T) {
	source := `
package com.example;

public class UserDto {
    @NotNull
    @Size(min = 2, max = 50)
    private String username;
}
`
	r := ExtractBeanValidation(bvCtx(source, "UserDto.java"))

	// Find the field entity
	var fieldEntity *SecondaryEntity
	for i := range r.Entities {
		if r.Entities[i].Name == "UserDto.username" {
			fieldEntity = &r.Entities[i]
		}
	}
	if fieldEntity == nil {
		t.Fatalf("[#3100 constraint-bounds-size] expected schema entity for UserDto.username; got %v", entityNames(r.Entities))
	}
	if fieldEntity.Properties["size_min"] != "2" {
		t.Errorf("[#3100 constraint-bounds-size] size_min = %v, want 2", fieldEntity.Properties["size_min"])
	}
	if fieldEntity.Properties["size_max"] != "50" {
		t.Errorf("[#3100 constraint-bounds-size] size_max = %v, want 50", fieldEntity.Properties["size_max"])
	}
}

// TestBeanValidation_ConstraintBoundsMinMax_Issue3100 proves @Min/@Max bounds.
func TestBeanValidation_ConstraintBoundsMinMax_Issue3100(t *testing.T) {
	source := `
public class OrderDto {
    @Min(1)
    @Max(1000)
    private int quantity;
}
`
	r := ExtractBeanValidation(bvCtx(source, "OrderDto.java"))

	var fieldEntity *SecondaryEntity
	for i := range r.Entities {
		if r.Entities[i].Name == "OrderDto.quantity" {
			fieldEntity = &r.Entities[i]
		}
	}
	if fieldEntity == nil {
		t.Fatalf("[#3100 constraint-bounds-minmax] expected schema entity for OrderDto.quantity; got %v", entityNames(r.Entities))
	}
	if fieldEntity.Properties["min_value"] != "1" {
		t.Errorf("[#3100 constraint-bounds-minmax] min_value = %v, want 1", fieldEntity.Properties["min_value"])
	}
	if fieldEntity.Properties["max_value"] != "1000" {
		t.Errorf("[#3100 constraint-bounds-minmax] max_value = %v, want 1000", fieldEntity.Properties["max_value"])
	}
}

// TestBeanValidation_ConstraintBoundsPattern_Issue3100 proves @Pattern regexp extraction.
func TestBeanValidation_ConstraintBoundsPattern_Issue3100(t *testing.T) {
	source := `
public class AddressDto {
    @Pattern(regexp = "^[A-Z]{2}$", message = "Must be 2-letter country code")
    private String countryCode;
}
`
	r := ExtractBeanValidation(bvCtx(source, "AddressDto.java"))

	var fieldEntity *SecondaryEntity
	for i := range r.Entities {
		if r.Entities[i].Name == "AddressDto.countryCode" {
			fieldEntity = &r.Entities[i]
		}
	}
	if fieldEntity == nil {
		t.Fatalf("[#3100 constraint-bounds-pattern] expected schema entity for AddressDto.countryCode; got %v", entityNames(r.Entities))
	}
	if fieldEntity.Properties["pattern_regexp"] != "^[A-Z]{2}$" {
		t.Errorf("[#3100 constraint-bounds-pattern] pattern_regexp = %v, want ^[A-Z]{2}$", fieldEntity.Properties["pattern_regexp"])
	}
}

// ── 3. nested_model_extraction (@Valid recursion) ─────────────────────────────

// TestBeanValidation_ValidFieldRecursion_Issue3100 proves that a @Valid-annotated
// field emits a VALIDATES edge from the owner class to the nested type.
// Registry target: lang.java.validation.bean-validation → nested_model_extraction → full.
func TestBeanValidation_ValidFieldRecursion_Issue3100(t *testing.T) {
	source := `
package com.example;

public class CreateOrderRequest {
    @NotNull
    @Valid
    private ShippingAddress shippingAddress;
}

public class ShippingAddress {
    @NotBlank
    private String street;
}
`
	r := ExtractBeanValidation(bvCtx(source, "CreateOrderRequest.java"))

	// Expect at least one VALIDATES relationship
	var found bool
	for _, rel := range r.Relationships {
		if rel.RelationshipType == "VALIDATES" && rel.Properties["via"] == "valid_annotation" {
			found = true
		}
	}
	if !found {
		t.Errorf("[#3100 nested-model] expected VALIDATES relationship for @Valid field; got %v", r.Relationships)
	}
}

// TestBeanValidation_ValidFieldFieldName_Issue3100 proves the VALIDATES edge
// carries the field name as a property.
func TestBeanValidation_ValidFieldFieldName_Issue3100(t *testing.T) {
	source := `
public class OrderRequest {
    @Valid
    private ShippingAddress shippingAddress;
}
`
	r := ExtractBeanValidation(bvCtx(source, "OrderRequest.java"))

	var validatesRel *Relationship
	for i := range r.Relationships {
		if r.Relationships[i].RelationshipType == "VALIDATES" {
			validatesRel = &r.Relationships[i]
		}
	}
	if validatesRel == nil {
		t.Fatalf("[#3100 nested-model-field] expected VALIDATES rel; got %v", r.Relationships)
	}
	if validatesRel.Properties["field"] != "shippingAddress" {
		t.Errorf("[#3100 nested-model-field] field = %q, want shippingAddress", validatesRel.Properties["field"])
	}
}

// ── 4. schema_extraction (field-level on DTO classes) ─────────────────────────

// TestBeanValidation_FieldLevelSchemaExtraction_Issue3100 proves that constraint
// annotations on DTO fields emit SCOPE.Schema entities.
// Registry target: lang.java.validation.bean-validation → schema_extraction → full.
func TestBeanValidation_FieldLevelSchemaExtraction_Issue3100(t *testing.T) {
	source := `
package com.example;

public class UserRegistrationDto {
    @NotNull
    @Size(min = 3, max = 30)
    private String username;

    @Email
    @NotBlank
    private String email;
}
`
	r := ExtractBeanValidation(bvCtx(source, "UserRegistrationDto.java"))

	schemas := entityByProvenance(r.Entities, "INFERRED_FROM_BEAN_VALIDATION_FIELD")
	if len(schemas) < 2 {
		t.Errorf("[#3100 schema-extraction] expected at least 2 SCOPE.Schema field entities; got %d: %v",
			len(schemas), entityNames(schemas))
	}
	names := entityNames(schemas)
	if !contains(names, "UserRegistrationDto.username") {
		t.Errorf("[#3100 schema-extraction] missing UserRegistrationDto.username in %v", names)
	}
	if !contains(names, "UserRegistrationDto.email") {
		t.Errorf("[#3100 schema-extraction] missing UserRegistrationDto.email in %v", names)
	}
}

// TestBeanValidation_SchemaConstraintsList_Issue3100 proves that the constraints
// property lists the annotation heads for the field.
func TestBeanValidation_SchemaConstraintsList_Issue3100(t *testing.T) {
	source := `
public class ProductDto {
    @NotNull
    @Size(min = 1, max = 255)
    private String name;
}
`
	r := ExtractBeanValidation(bvCtx(source, "ProductDto.java"))

	var fieldEnt *SecondaryEntity
	for i := range r.Entities {
		if r.Entities[i].Name == "ProductDto.name" {
			fieldEnt = &r.Entities[i]
		}
	}
	if fieldEnt == nil {
		t.Fatalf("[#3100 schema-constraints] expected ProductDto.name entity; got %v", entityNames(r.Entities))
	}
	constraints, ok := fieldEnt.Properties["constraints"].(string)
	if !ok || constraints == "" {
		t.Errorf("[#3100 schema-constraints] constraints property empty or missing; got %v", fieldEnt.Properties["constraints"])
	}
	// Should include @NotNull and @Size
	if !strings.Contains(constraints, "@NotNull") {
		t.Errorf("[#3100 schema-constraints] @NotNull missing from constraints %q", constraints)
	}
	if !strings.Contains(constraints, "@Size") {
		t.Errorf("[#3100 schema-constraints] @Size missing from constraints %q", constraints)
	}
}

// ── 5. @Validated class-level detection ──────────────────────────────────────

// TestBeanValidation_ValidatedClassDetection_Issue3100 proves that @Validated on
// a class emits a SCOPE.Component entity.
func TestBeanValidation_ValidatedClassDetection_Issue3100(t *testing.T) {
	source := `
import jakarta.validation.Validated;

@Validated
public class OrderService {
    public void placeOrder(CreateOrderRequest request) {}
}
`
	r := ExtractBeanValidation(bvCtx(source, "OrderService.java"))

	e := entityWithKind(r.Entities, "SCOPE.Component", "OrderService")
	if e == nil {
		t.Fatalf("[#3100 validated-class] expected SCOPE.Component for OrderService; got %v", entityNames(r.Entities))
	}
	if e.Provenance != "INFERRED_FROM_VALIDATED_ANNOTATION" {
		t.Errorf("[#3100 validated-class] provenance = %q, want INFERRED_FROM_VALIDATED_ANNOTATION", e.Provenance)
	}
}

// ── 6. Gating ─────────────────────────────────────────────────────────────────

// TestBeanValidation_Gating_Issue3100 confirms the extractor is gated on Java
// and does NOT fire for non-Java languages.
func TestBeanValidation_Gating_Issue3100(t *testing.T) {
	source := `public class Foo implements ConstraintValidator<Bar, String> {}`

	for _, lang := range []string{"kotlin", "python", "typescript", "ruby"} {
		r := ExtractBeanValidation(PatternContext{
			Source: source, Language: lang,
			Framework: "spring_boot", FilePath: "Foo.java",
		})
		if len(r.Entities) != 0 {
			t.Errorf("[#3100 gating] language %q should no-op, got %d entities", lang, len(r.Entities))
		}
	}

	// Should fire for java + any valid framework
	r := ExtractBeanValidation(PatternContext{
		Source: source, Language: "java",
		Framework: "spring_boot", FilePath: "Foo.java",
	})
	if len(r.Entities) == 0 {
		t.Error("[#3100 gating] expected entities for language=java, got none")
	}
}

// ── 7. Fixture integration test ───────────────────────────────────────────────

// TestBeanValidation_FixtureFile_Issue3100 loads the updated bean_validation
// fixture and verifies all expected entities and relationships are extracted.
func TestBeanValidation_FixtureFile_Issue3100(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/fixtures/sources/java/bean_validation/ValidatedDtoFixture.java")
	if err != nil {
		t.Fatalf("[#3100 fixture] cannot read fixture: %v", err)
	}
	source := string(data)
	r := ExtractBeanValidation(bvCtx(source, "ValidatedDtoFixture.java"))

	// 1. Custom validators
	customValidators := entityByProvenance(r.Entities, "INFERRED_FROM_CONSTRAINT_VALIDATOR_IMPL")
	wantValidators := []string{"PhoneNumberValidator", "PositiveAmountValidator"}
	cvNames := entityNames(customValidators)
	for _, want := range wantValidators {
		if !contains(cvNames, want) {
			t.Errorf("[#3100 fixture] missing custom validator %q in %v", want, cvNames)
		}
	}

	// 2. Custom constraint annotations (@Constraint meta-annotation)
	constraintAnnots := entityByProvenance(r.Entities, "INFERRED_FROM_BEAN_VALIDATION_CONSTRAINT")
	if len(constraintAnnots) < 2 {
		t.Errorf("[#3100 fixture] expected ≥2 custom constraint annotations; got %d: %v",
			len(constraintAnnots), entityNames(constraintAnnots))
	}

	// 3. Field-level schema entities (schema_extraction)
	fieldSchemas := entityByProvenance(r.Entities, "INFERRED_FROM_BEAN_VALIDATION_FIELD")
	if len(fieldSchemas) == 0 {
		t.Errorf("[#3100 fixture] expected ≥1 field schema entities; got none")
	}

	// 4. VALIDATES relationships (nested_model_extraction)
	var validatesRels int
	for _, rel := range r.Relationships {
		if rel.RelationshipType == "VALIDATES" && rel.Properties["via"] == "valid_annotation" {
			validatesRels++
		}
	}
	if validatesRels == 0 {
		t.Errorf("[#3100 fixture] expected ≥1 VALIDATES relationship for @Valid fields; got 0")
	}

	// 5. @Validated class detection
	e := entityWithKind(r.Entities, "SCOPE.Component", "OrderService")
	if e == nil {
		t.Errorf("[#3100 fixture] expected SCOPE.Component for OrderService; got %v", entityNames(r.Entities))
	}
}

// TestBeanValidation_ConstraintBounds_FixtureFile_Issue3100 verifies constraint
// bounds are parsed from the fixture (proving constraint_extraction → full).
func TestBeanValidation_ConstraintBounds_FixtureFile_Issue3100(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/fixtures/sources/java/bean_validation/ValidatedDtoFixture.java")
	if err != nil {
		t.Fatalf("[#3100 fixture-bounds] cannot read fixture: %v", err)
	}
	source := string(data)
	r := ExtractBeanValidation(bvCtx(source, "ValidatedDtoFixture.java"))

	// Look for a field entity with min_value or max_value set
	var hasBounds bool
	for _, e := range r.Entities {
		if e.Properties["min_value"] != nil || e.Properties["max_value"] != nil ||
			e.Properties["size_min"] != nil || e.Properties["pattern_regexp"] != nil {
			hasBounds = true
			break
		}
	}
	if !hasBounds {
		t.Errorf("[#3100 fixture-bounds] expected at least one field entity with parsed constraint bounds; got none")
	}
}
