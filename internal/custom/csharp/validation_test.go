package csharp_test

// ---------------------------------------------------------------------------
// Validation — request_validation + dto_extraction
// ---------------------------------------------------------------------------

import "testing"

// FluentValidation: AbstractValidator<T> subclass
func TestValidationFluentAbstractValidator(t *testing.T) {
	src := `
using FluentValidation;

public class CreateOrderValidator : AbstractValidator<CreateOrderDto>
{
    public CreateOrderValidator()
    {
        RuleFor(x => x.CustomerId).NotEmpty();
        RuleFor(x => x.Amount).GreaterThan(0);
    }
}
`
	ents := extract(t, "custom_csharp_validation", fi("CreateOrderValidator.cs", "csharp", src))

	validationFound := false
	dtoFound := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "request_validation" && e.Name == "validation:fluent:CreateOrderValidator" {
			validationFound = true
		}
		if e.Kind == "SCOPE.Schema" && e.Subtype == "dto" && e.Name == "CreateOrderDto" {
			dtoFound = true
		}
	}
	if !validationFound {
		t.Error("expected validation:fluent:CreateOrderValidator SCOPE.Pattern entity")
	}
	if !dtoFound {
		t.Error("expected CreateOrderDto SCOPE.Schema dto entity from AbstractValidator")
	}
}

// FluentValidation: qualified namespace (FluentValidation.AbstractValidator<T>)
func TestValidationFluentQualifiedNamespace(t *testing.T) {
	src := `
public class UserValidator : FluentValidation.AbstractValidator<UserRequest>
{
    public UserValidator()
    {
        RuleFor(u => u.Email).NotEmpty().EmailAddress();
    }
}
`
	ents := extract(t, "custom_csharp_validation", fi("UserValidator.cs", "csharp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "request_validation" && e.Name == "validation:fluent:UserValidator" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected validation:fluent:UserValidator from qualified FluentValidation.AbstractValidator<T>")
	}
}

// DataAnnotations: [Required] on model properties
func TestValidationDataAnnotationsRequired(t *testing.T) {
	src := `
using System.ComponentModel.DataAnnotations;

public class RegisterRequest
{
    [Required]
    public string Username { get; set; }

    [Required]
    [StringLength(100, MinimumLength = 6)]
    public string Password { get; set; }

    [EmailAddress]
    public string Email { get; set; }
}
`
	ents := extract(t, "custom_csharp_validation", fi("RegisterRequest.cs", "csharp", src))

	validationCount := 0
	dtoFound := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "request_validation" {
			validationCount++
		}
		if e.Kind == "SCOPE.Schema" && e.Subtype == "dto" && e.Name == "RegisterRequest" {
			dtoFound = true
		}
	}
	if validationCount == 0 {
		t.Error("expected at least one request_validation SCOPE.Pattern from DataAnnotations")
	}
	if !dtoFound {
		t.Error("expected RegisterRequest SCOPE.Schema dto from DataAnnotation model")
	}
}

// DataAnnotations: [Range] and [RegularExpression]
func TestValidationDataAnnotationsRangeRegex(t *testing.T) {
	src := `
public class ProductDto
{
    [Range(0.01, 9999.99)]
    public decimal Price { get; set; }

    [RegularExpression(@"^[A-Z]{2,4}$")]
    public string Code { get; set; }
}
`
	ents := extract(t, "custom_csharp_validation", fi("ProductDto.cs", "csharp", src))
	rangeFound := false
	regexFound := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "request_validation" {
			if containsEntity([]entitySummary{{Kind: e.Kind, Subtype: e.Subtype, Name: e.Name}}, "SCOPE.Pattern", e.Name) {
				// Just check they exist
				if e.Name != "" {
					rangeFound = rangeFound || (len(e.Name) > 0)
					regexFound = regexFound || (len(e.Name) > 0)
				}
			}
		}
	}
	count := 0
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "request_validation" {
			count++
		}
	}
	if count < 2 {
		t.Errorf("expected at least 2 DataAnnotation validation patterns, got %d", count)
	}
	_ = rangeFound
	_ = regexFound
}

// [ApiController] auto-validation
func TestValidationApiControllerAutoValidation(t *testing.T) {
	src := `
[ApiController]
[Route("api/[controller]")]
public class OrdersController : ControllerBase
{
    [HttpPost]
    public IActionResult Create([FromBody] CreateOrderDto dto) => Ok();
}
`
	ents := extract(t, "custom_csharp_validation", fi("OrdersController.cs", "csharp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "request_validation" && e.Name == "validation:ApiController:auto:OrdersController.cs" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected validation:ApiController:auto entity from [ApiController]")
	}
}

// Wrong language should produce no entities.
func TestValidationWrongLanguageSkipped(t *testing.T) {
	src := `[Required] public string Name { get; set; }`
	ents := extract(t, "custom_csharp_validation", fi("file.java", "java", src))
	if len(ents) != 0 {
		t.Errorf("expected 0 entities for non-csharp language, got %d", len(ents))
	}
}

// No match — plain file with no validation patterns.
func TestValidationNoMatch(t *testing.T) {
	src := `namespace App { class Helper { public void DoWork() {} } }`
	ents := extract(t, "custom_csharp_validation", fi("Helper.cs", "csharp", src))
	if len(ents) != 0 {
		t.Errorf("expected 0 validation entities, got %d", len(ents))
	}
}
