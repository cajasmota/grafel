package javascript

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
)

// Issue #4858 — per-field validation constraints for DTO fields.
//
// NestJS DTOs use class-validator decorators on their properties, e.g.
//
//	class CreateUserDto {
//	  @IsString()
//	  @MaxLength(120)
//	  @IsOptional()
//	  name?: string;
//	}
//
// Each such field already becomes a SCOPE.Schema/field entity (#679/#4845).
// This pass collects the class-validator decorators attached to a
// `public_field_definition` and returns them as a compact, terse list such as
//
//	["IsString", "MaxLength:120", "IsOptional"]
//
// The list is stored on the field entity under Properties["validations"]
// (comma-joined, since Properties is map[string]string) so the dashboard
// ShapeTree can surface them as small constraint chips next to the type.
//
// Only recognised class-validator decorators are collected; unrelated
// decorators (`@Type`, `@Transform`, `@ApiProperty`, …) are ignored so the
// chips stay focused on validation semantics. The order of source decorators
// is preserved.

// classValidatorDecorators is the set of recognised class-validator property
// decorators. Sourced from the class-validator public decorator surface
// (common-validation, type-checks, string/number/date specific, nested).
// Membership-only: argument values (e.g. the `120` in `@MaxLength(120)`) are
// folded into the chip text separately for the small number of decorators that
// carry a single scalar bound.
var classValidatorDecorators = map[string]bool{
	// Common
	"IsDefined": true, "IsOptional": true, "Equals": true, "NotEquals": true,
	"IsEmpty": true, "IsNotEmpty": true, "IsIn": true, "IsNotIn": true,
	// Type checks
	"IsBoolean": true, "IsDate": true, "IsString": true, "IsNumber": true,
	"IsInt": true, "IsArray": true, "IsEnum": true, "IsObject": true,
	// Number
	"IsDivisibleBy": true, "IsPositive": true, "IsNegative": true,
	"Min": true, "Max": true,
	// Date
	"MinDate": true, "MaxDate": true,
	// String-type
	"IsBooleanString": true, "IsDateString": true, "IsNumberString": true,
	// String
	"Contains": true, "NotContains": true, "IsAlpha": true, "IsAlphanumeric": true,
	"IsAscii": true, "IsBase64": true, "IsByteLength": true, "IsCreditCard": true,
	"IsCurrency": true, "IsEmail": true, "IsFQDN": true, "IsFullWidth": true,
	"IsHalfWidth": true, "IsVariableWidth": true, "IsHexColor": true,
	"IsHexadecimal": true, "IsMacAddress": true, "IsIP": true, "IsPort": true,
	"IsISBN": true, "IsISIN": true, "IsISO8601": true, "IsJSON": true,
	"IsJWT": true, "IsLowercase": true, "IsLatLong": true, "IsLatitude": true,
	"IsLongitude": true, "IsMobilePhone": true, "IsMongoId": true,
	"IsMultibyte": true, "IsNumberString2": true, "IsSurrogatePair": true,
	"IsUrl": true, "IsURL": true, "IsUUID": true, "IsFirebasePushId": true,
	"IsUppercase": true, "Length": true, "MaxLength": true, "MinLength": true,
	"Matches": true, "IsPhoneNumber": true, "IsMilitaryTime": true,
	"IsHash": true, "IsSemVer": true,
	// Array
	"ArrayContains": true, "ArrayNotContains": true, "ArrayNotEmpty": true,
	"ArrayMinSize": true, "ArrayMaxSize": true, "ArrayUnique": true,
	// Object / nested
	"IsInstance": true, "ValidateNested": true, "ValidateIf": true,
	"IsNotEmptyObject": true, "Allow": true,
}

// scalarArgDecorators are the class-validator decorators whose first argument is
// a single scalar bound worth folding into the chip text (e.g.
// `@MaxLength(120)` → "MaxLength:120"). For everything else we keep the bare
// decorator name to stay terse and avoid rendering regexes / option objects.
var scalarArgDecorators = map[string]bool{
	"MaxLength": true, "MinLength": true, "Min": true, "Max": true,
	"Length": true, "ArrayMinSize": true, "ArrayMaxSize": true,
	"IsByteLength": true, "MinDate": true, "MaxDate": true,
}

// fieldValidations walks the decorator children of a `public_field_definition`
// (or `field_definition`) node and returns the recognised class-validator
// constraints as a compact, source-ordered list. Returns nil when the field
// carries no class-validator decorators.
func (x *extractor) fieldValidations(field ts.Node) []string {
	if field == nil {
		return nil
	}
	var out []string
	for i := 0; i < int(field.ChildCount()); i++ {
		c := field.Child(i)
		if c == nil || c.Type() != "decorator" {
			continue
		}
		name, call := x.decoratorIdent(c)
		// Strip a namespace prefix (`validator.IsString` → `IsString`).
		if idx := strings.LastIndex(name, "."); idx >= 0 {
			name = name[idx+1:]
		}
		if name == "" || !classValidatorDecorators[name] {
			continue
		}
		chip := name
		if scalarArgDecorators[name] && call != nil {
			if v := firstScalarArg(x, call); v != "" {
				chip = name + ":" + v
			}
		}
		out = append(out, chip)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// firstScalarArg returns the text of the first argument of a decorator
// call_expression when it is a simple numeric / string / identifier literal.
// Compound arguments (objects, arrays, regexes, expressions) yield "" so the
// caller falls back to the bare decorator name.
func firstScalarArg(x *extractor, call ts.Node) string {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return ""
	}
	for i := 0; i < int(args.ChildCount()); i++ {
		a := args.Child(i)
		if a == nil {
			continue
		}
		switch a.Type() {
		case "number", "true", "false", "identifier":
			return x.nodeText(a)
		case "string":
			// Drop the surrounding quotes for a tighter chip.
			return strings.Trim(x.nodeText(a), "'\"`")
		case "(", ")", ",", "comment":
			continue
		default:
			// First non-trivial argument is non-scalar — bail.
			return ""
		}
	}
	return ""
}

// applyFieldValidations stamps the validations list onto a field entity's
// Properties map (creating it if nil). Stored comma-joined since Properties is
// map[string]string; the dashboard splits on "," to rebuild the list.
func applyFieldValidations(props map[string]string, validations []string) map[string]string {
	if len(validations) == 0 {
		return props
	}
	if props == nil {
		props = map[string]string{}
	}
	props["validations"] = strings.Join(validations, ",")
	return props
}
