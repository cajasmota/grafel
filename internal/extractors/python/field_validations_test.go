package python_test

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// fieldValidations4871 returns the comma-split validations chips stamped on the
// SCOPE.Schema/field entity named "<class>.<attr>", or nil when absent.
func fieldValidations4871(ents []types.EntityRecord, name string) []string {
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "field" && e.Name == name {
			raw := strings.TrimSpace(e.Properties["validations"])
			if raw == "" {
				return nil
			}
			return strings.Split(raw, ",")
		}
	}
	return nil
}

// TestPythonFieldValidations_Pydantic covers #4871 — Pydantic v1/v2 Field(),
// Annotated[T, Field(...)], constrained-type constructors, Optional markers and
// @field_validator presence are stamped as terse constraint chips on the field
// entity under Properties["validations"].
func TestPythonFieldValidations_Pydantic(t *testing.T) {
	src := `from pydantic import BaseModel, Field, field_validator, conint
from typing import Optional, Annotated

class User(BaseModel):
    name: str = Field(..., max_length=120, min_length=2)
    age: int = Field(0, gt=0, le=150)
    nickname: Optional[str] = None
    tags: str | None = None
    score: Annotated[int, Field(ge=0, le=100)] = 0
    count: conint(gt=0) = 1
    email: str = Field(..., pattern=r"^.+@.+$")
    plain: str

    @field_validator("name", "email")
    def chk(cls, v):
        return v
`
	ents := stripFileEntity(extractPy(t, src, "users.py"))

	cases := []struct {
		field string
		want  []string
	}{
		{"User.name", []string{"Required", "MaxLength:120", "MinLength:2", "Validated"}},
		{"User.age", []string{"Gt:0", "Le:150"}},
		{"User.nickname", []string{"Optional"}},
		{"User.tags", []string{"Optional"}},
		{"User.score", []string{"Ge:0", "Le:100"}},
		{"User.count", []string{"Gt:0"}},
		{"User.email", []string{"Required", "Pattern", "Validated"}},
	}
	for _, c := range cases {
		got := fieldValidations4871(ents, c.field)
		if !equalStrs(got, c.want) {
			t.Errorf("%s validations=%v want %v", c.field, got, c.want)
		}
	}

	// A field with no constraints carries no validations property.
	if got := fieldValidations4871(ents, "User.plain"); got != nil {
		t.Errorf("User.plain expected no validations, got %v", got)
	}
}

// TestPythonFieldValidations_DRF covers #4871 — DRF serializer field kwargs
// (max_length / min_length / min_value / max_value / required=False /
// allow_null=True / read_only=True) → constraint chips.
func TestPythonFieldValidations_DRF(t *testing.T) {
	src := `from rest_framework import serializers

class UserSerializer(serializers.ModelSerializer):
    username = serializers.CharField(max_length=30, required=False, allow_null=True)
    amount = serializers.IntegerField(min_value=0, max_value=999)
    token = serializers.CharField(read_only=True)

    class Meta:
        model = User
        fields = "__all__"
`
	ents := stripFileEntity(extractPy(t, src, "serializers.py"))

	cases := []struct {
		field string
		want  []string
	}{
		{"UserSerializer.username", []string{"MaxLength:30", "Optional", "AllowNull"}},
		{"UserSerializer.amount", []string{"Min:0", "Max:999"}},
		{"UserSerializer.token", []string{"ReadOnly"}},
	}
	for _, c := range cases {
		got := fieldValidations4871(ents, c.field)
		if !equalStrs(got, c.want) {
			t.Errorf("%s validations=%v want %v", c.field, got, c.want)
		}
	}
}

func equalStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
