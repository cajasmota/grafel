package java_test

import (
	"strings"
	"testing"
)

// Issue #4872 — Java Bean Validation annotations (javax.* + jakarta.*) on DTO
// fields must surface in the unified Properties["validations"] property so the
// dashboard ShapeTree renders them as constraint chips (parity with TS #4858
// and Python #4871).

// chips splits the comma-joined validations property into a set for assertions.
func valSet(props map[string]string) map[string]bool {
	out := map[string]bool{}
	if props == nil {
		return out
	}
	for _, c := range strings.Split(props["validations"], ",") {
		c = strings.TrimSpace(c)
		if c != "" {
			out[c] = true
		}
	}
	return out
}

func TestJava_BeanValidation_ClassDTO_jakarta(t *testing.T) {
	src := `
package com.example;

import jakarta.validation.constraints.*;

public class CreateUserDto {
    @NotNull
    @Size(max = 120)
    @Email
    private String email;

    @NotBlank
    @Size(min = 2, max = 50)
    private String name;

    @Min(0)
    @Max(150)
    private int age;

    @Positive
    private long credits;

    @Pattern(regexp = "[A-Z]{3}")
    private String code;

    private String untouched;
}
`
	ents := runJava(t, src)

	email := javaFind(ents, "CreateUserDto.email", "SCOPE.Schema")
	if email == nil {
		t.Fatal("expected field CreateUserDto.email")
	}
	got := valSet(email.Properties)
	for _, want := range []string{"Required", "MaxLength:120", "Email"} {
		if !got[want] {
			t.Errorf("email validations missing %q; got %v", want, got)
		}
	}

	name := javaFind(ents, "CreateUserDto.name", "SCOPE.Schema")
	if name == nil {
		t.Fatal("expected field CreateUserDto.name")
	}
	gotName := valSet(name.Properties)
	for _, want := range []string{"NotBlank", "Size:2..50"} {
		if !gotName[want] {
			t.Errorf("name validations missing %q; got %v", want, gotName)
		}
	}

	age := javaFind(ents, "CreateUserDto.age", "SCOPE.Schema")
	gotAge := valSet(age.Properties)
	for _, want := range []string{"Min:0", "Max:150"} {
		if !gotAge[want] {
			t.Errorf("age validations missing %q; got %v", want, gotAge)
		}
	}

	credits := javaFind(ents, "CreateUserDto.credits", "SCOPE.Schema")
	if !valSet(credits.Properties)["Positive"] {
		t.Errorf("credits should carry Positive; got %v", valSet(credits.Properties))
	}

	code := javaFind(ents, "CreateUserDto.code", "SCOPE.Schema")
	if !valSet(code.Properties)["Pattern"] {
		t.Errorf("code should carry Pattern; got %v", valSet(code.Properties))
	}

	// A field with no Bean Validation annotations must not get a validations
	// property at all.
	untouched := javaFind(ents, "CreateUserDto.untouched", "SCOPE.Schema")
	if untouched == nil {
		t.Fatal("expected field CreateUserDto.untouched")
	}
	if _, ok := untouched.Properties["validations"]; ok {
		t.Errorf("untouched field should have no validations; got %v", untouched.Properties)
	}
}

func TestJava_BeanValidation_javaxAndDecimal(t *testing.T) {
	src := `
package com.example;

import javax.validation.constraints.DecimalMin;
import javax.validation.constraints.DecimalMax;
import javax.validation.constraints.NotEmpty;

public class PriceDto {
    @NotEmpty
    private String label;

    @DecimalMin("0.0")
    @DecimalMax("9999.99")
    private java.math.BigDecimal amount;
}
`
	ents := runJava(t, src)

	label := javaFind(ents, "PriceDto.label", "SCOPE.Schema")
	if !valSet(label.Properties)["NotEmpty"] {
		t.Errorf("label should carry NotEmpty; got %v", valSet(label.Properties))
	}

	amount := javaFind(ents, "PriceDto.amount", "SCOPE.Schema")
	got := valSet(amount.Properties)
	for _, want := range []string{"Min:0.0", "Max:9999.99"} {
		if !got[want] {
			t.Errorf("amount validations missing %q; got %v", want, got)
		}
	}
}

func TestJava_BeanValidation_RecordComponents(t *testing.T) {
	src := `
package com.example;

import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Size;

public record SignupRequest(
    @NotNull @Size(max = 64) String username,
    @NotNull String password
) {}
`
	ents := runJava(t, src)

	user := javaFind(ents, "SignupRequest.username", "SCOPE.Schema")
	if user == nil {
		t.Fatal("expected field SignupRequest.username")
	}
	got := valSet(user.Properties)
	for _, want := range []string{"Required", "MaxLength:64"} {
		if !got[want] {
			t.Errorf("username validations missing %q; got %v", want, got)
		}
	}

	pw := javaFind(ents, "SignupRequest.password", "SCOPE.Schema")
	if !valSet(pw.Properties)["Required"] {
		t.Errorf("password should carry Required; got %v", valSet(pw.Properties))
	}
}
