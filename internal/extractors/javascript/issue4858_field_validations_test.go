// Package javascript — issue #4858 per-field validation-constraint extraction.
//
// A NestJS DTO field decorated with class-validator decorators
// (`@IsString() @MaxLength(120) @IsOptional() name?: string`) should carry
// those constraints as structured metadata on its SCOPE.Schema/field entity
// (Properties["validations"], comma-joined) so the dashboard ShapeTree can
// render them as small constraint chips next to the field type.
package javascript_test

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// fieldValidationsProp returns the value of Properties["validations"] for the
// SCOPE.Schema/field entity named "<class>.<field>", or "" when the field or
// property is absent.
func fieldValidationsProp(ents []types.EntityRecord, className, field string) string {
	want := className + "." + field
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "field" && e.Name == want {
			if e.Properties == nil {
				return ""
			}
			return e.Properties["validations"]
		}
	}
	return ""
}

func TestDTO_ClassValidatorConstraintsStampedOnField(t *testing.T) {
	path := "src/user/dto/create-user.dto.ts"
	src := []byte(`
import { IsString, IsOptional, MaxLength, MinLength, IsEmail, IsInt, Min, Max } from 'class-validator';

export class CreateUserDto {
  @IsString()
  @MinLength(2)
  @MaxLength(120)
  @IsOptional()
  name?: string;

  @IsEmail()
  email: string;

  @IsInt()
  @Min(0)
  @Max(150)
  age: number;

  // No validation decorators — must carry no validations property.
  nickname: string;
}
`)
	ents := extractHeritageTS(t, path, src)

	cases := []struct {
		field string
		want  []string
	}{
		{"name", []string{"IsString", "MinLength:2", "MaxLength:120", "IsOptional"}},
		{"email", []string{"IsEmail"}},
		{"age", []string{"IsInt", "Min:0", "Max:150"}},
	}
	for _, c := range cases {
		got := fieldValidationsProp(ents, "CreateUserDto", c.field)
		want := strings.Join(c.want, ",")
		if got != want {
			t.Errorf("field %q validations = %q, want %q", c.field, got, want)
		}
	}

	// A field with no class-validator decorators must NOT carry a validations
	// property (avoid empty chips in the dashboard).
	if v := fieldValidationsProp(ents, "CreateUserDto", "nickname"); v != "" {
		t.Errorf("nickname should have no validations, got %q", v)
	}
}
