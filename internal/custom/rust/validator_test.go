package rust_test

// validator_test.go — value-asserting tests for the custom_rust_validator
// extractor (record lang.rust.validation.validator, issue #3545).
//
// These tests assert the SPECIFIC constraint kind + captured bound/regex for
// each field, not merely that some entity exists. findEntity is shared with
// auth_policy_test.go.

import (
	"testing"
)

// The canonical fixture from the issue brief: a Validate-derived struct with one
// constraint per kind, plus a nested field.
const validatorFixture = `
use validator::Validate;

#[derive(Debug, Validate)]
struct CreateUser {
    #[validate(email)]
    email: String,
    #[validate(length(min = 1, max = 20))]
    name: String,
    #[validate(range(min = 18))]
    age: u8,
    #[validate(regex = "RE_X")]
    code: String,
    #[validate(nested)]
    address: Address,
}

fn handle(input: CreateUser) -> Result<(), validator::ValidationErrors> {
    input.validate()?;
    Ok(())
}
`

func TestValidator_SchemaExtraction(t *testing.T) {
	ents := extract(t, "custom_rust_validator", fi("user.rs", "rust", validatorFixture))
	s, ok := findEntity(ents, "SCOPE.Schema", "validator_schema:CreateUser")
	if !ok {
		t.Fatal("expected validator_schema:CreateUser")
	}
	if s.Subtype != "validator_schema" {
		t.Errorf("subtype = %q, want validator_schema", s.Subtype)
	}
	if got := s.Props["field_count"]; got != "5" {
		t.Errorf("field_count = %q, want 5", got)
	}
}

func TestValidator_EmailConstraint(t *testing.T) {
	ents := extract(t, "custom_rust_validator", fi("user.rs", "rust", validatorFixture))
	c, ok := findEntity(ents, "SCOPE.Constraint", "validator_constraint:CreateUser.email:email")
	if !ok {
		t.Fatal("expected email constraint on CreateUser.email")
	}
	if got := c.Props["constraint_kind"]; got != "email" {
		t.Errorf("constraint_kind = %q, want email", got)
	}
	if got := c.Props["field_name"]; got != "email" {
		t.Errorf("field_name = %q, want email", got)
	}
}

func TestValidator_LengthConstraintBounds(t *testing.T) {
	ents := extract(t, "custom_rust_validator", fi("user.rs", "rust", validatorFixture))
	c, ok := findEntity(ents, "SCOPE.Constraint", "validator_constraint:CreateUser.name:length")
	if !ok {
		t.Fatal("expected length constraint on CreateUser.name")
	}
	if got := c.Props["min"]; got != "1" {
		t.Errorf("length min = %q, want 1", got)
	}
	if got := c.Props["max"]; got != "20" {
		t.Errorf("length max = %q, want 20", got)
	}
}

func TestValidator_RangeConstraintBound(t *testing.T) {
	ents := extract(t, "custom_rust_validator", fi("user.rs", "rust", validatorFixture))
	c, ok := findEntity(ents, "SCOPE.Constraint", "validator_constraint:CreateUser.age:range")
	if !ok {
		t.Fatal("expected range constraint on CreateUser.age")
	}
	if got := c.Props["min"]; got != "18" {
		t.Errorf("range min = %q, want 18", got)
	}
}

func TestValidator_RegexConstraintValue(t *testing.T) {
	ents := extract(t, "custom_rust_validator", fi("user.rs", "rust", validatorFixture))
	c, ok := findEntity(ents, "SCOPE.Constraint", "validator_constraint:CreateUser.code:regex")
	if !ok {
		t.Fatal("expected regex constraint on CreateUser.code")
	}
	if got := c.Props["value"]; got != "RE_X" {
		t.Errorf("regex value = %q, want RE_X", got)
	}
}

func TestValidator_NestedExtraction(t *testing.T) {
	ents := extract(t, "custom_rust_validator", fi("user.rs", "rust", validatorFixture))
	n, ok := findEntity(ents, "SCOPE.Schema", "validator_nested:CreateUser.address")
	if !ok {
		t.Fatal("expected nested_validation on CreateUser.address")
	}
	if n.Subtype != "nested_validation" {
		t.Errorf("subtype = %q, want nested_validation", n.Subtype)
	}
	if got := n.Props["field_type"]; got != "Address" {
		t.Errorf("nested field_type = %q, want Address", got)
	}
	// A nested marker must NOT also be emitted as a generic constraint.
	if _, dup := findEntity(ents, "SCOPE.Constraint", "validator_constraint:CreateUser.address:nested"); dup {
		t.Error("nested should not be double-counted as a constraint")
	}
}

func TestValidator_ValidateInvocation(t *testing.T) {
	ents := extract(t, "custom_rust_validator", fi("user.rs", "rust", validatorFixture))
	if _, ok := findEntity(ents, "SCOPE.Pattern", "validator_validate_call"); !ok {
		t.Error("expected validation_invocation from input.validate()? call site")
	}
}

func TestValidator_FieldLevelCustom(t *testing.T) {
	src := `
use validator::Validate;

#[derive(Validate)]
struct Payment {
    #[validate(custom = "validate_currency")]
    currency: String,
}
`
	ents := extract(t, "custom_rust_validator", fi("pay.rs", "rust", src))
	c, ok := findEntity(ents, "SCOPE.Pattern", "validator_custom:Payment.currency")
	if !ok {
		t.Fatal("expected field-level custom validator on Payment.currency")
	}
	if got := c.Props["function"]; got != "validate_currency" {
		t.Errorf("custom function = %q, want validate_currency", got)
	}
	if got := c.Props["scope"]; got != "field" {
		t.Errorf("scope = %q, want field", got)
	}
}

func TestValidator_StructLevelSchemaFn(t *testing.T) {
	src := `
use validator::Validate;

#[derive(Validate)]
#[validate(schema(function = "validate_passwords_match"))]
struct SignupForm {
    #[validate(length(min = 8))]
    password: String,
    password_confirm: String,
}
`
	ents := extract(t, "custom_rust_validator", fi("signup.rs", "rust", src))
	c, ok := findEntity(ents, "SCOPE.Pattern", "validator_custom:SignupForm:schema")
	if !ok {
		t.Fatal("expected struct-level schema custom validator on SignupForm")
	}
	if got := c.Props["function"]; got != "validate_passwords_match" {
		t.Errorf("schema function = %q, want validate_passwords_match", got)
	}
	if got := c.Props["scope"]; got != "struct" {
		t.Errorf("scope = %q, want struct", got)
	}
}

// A plain serde DTO with no Validate derive yields no validator entities.
func TestValidator_NoMatchPlainSerde(t *testing.T) {
	src := `
use serde::Deserialize;

#[derive(Deserialize)]
struct Plain {
    name: String,
}
`
	ents := extract(t, "custom_rust_validator", fi("plain.rs", "rust", src))
	for _, e := range ents {
		if e.Subtype == "validator_schema" || e.Kind == "SCOPE.Constraint" {
			t.Errorf("unexpected validator entity for plain serde DTO: %s/%s", e.Kind, e.Name)
		}
	}
}
