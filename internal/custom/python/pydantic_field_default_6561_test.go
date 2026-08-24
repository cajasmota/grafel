package python_test

import "testing"

// A Pydantic field whose default is a single literal publishes that literal as
// `default_value`; a default that cannot be read as one literal publishes
// nothing (#6561).
func TestPydantic_FieldDefaultValue_6561(t *testing.T) {
	src := `import os
import pydantic
from pydantic import BaseSettings, Field

class Settings(BaseSettings):
    API_PREFIX: str = "/v1"
    PORT: int = 8000
    RATIO: float = 0.5
    DEBUG: bool = False
    NOTE: str = 'single'
    SUFFIX: str = "/x"  # trailing comment
    HASHED: str = "a # b"
    LABEL: str = Field(default="svc", min_length=1)
    QUALIFIED: str = pydantic.Field(default="p")
    HOST: str = os.environ["HOST"]
    TAGS: list = []
    EXPIRES: int = 60 * 24  # eight days
    GREETING: str = f"hi {who}"
    ITEMS: list = Field(default_factory=list)  # default=5, historical
    PROXY: str = os.environ["X"]  # default="/inj"
    CUSTOM: str = MyCustomField(default="cf")
    NESTED: str = wrap(Field(default="w"))
    PROSE: str = Field(default="svc", description="the default=1 legacy")
    DESC: str = Field(description="default=5, historical")
    DESC2: str = Field(description="set to default=None) when unset")
    ALIASD: str = Field(alias="default=9,", min_length=1)
    DFAC: list = Field(default_factory=list, description="default=7,")
    COND: str = Field(default="x") if c else Field(default="y")
    UNDERSCORED: int = Field(default=1_000)
    EXPONENT: float = Field(default=1.5e3)
    REQUIRED: str
`
	rs := extract(t, "python_pydantic", src)

	for _, tc := range []struct{ field, want string }{
		{"API_PREFIX", `"/v1"`},
		{"PORT", "8000"},
		{"RATIO", "0.5"},
		{"DEBUG", "False"},
		{"NOTE", "'single'"},
		{"SUFFIX", `"/x"`},
		{"HASHED", `"a # b"`},
		{"LABEL", `"svc"`},
		{"QUALIFIED", `"p"`},
		// A real `default=` still reads through, and reads verbatim, when the same
		// call also carries prose that names a default. Blanking the string
		// contents must not reach the `default=` argument's own literal.
		{"PROSE", `"svc"`},
	} {
		f := findFieldChild(rs, "Settings."+tc.field)
		if f == nil {
			t.Fatalf("expected field sub-entity Settings.%s", tc.field)
		}
		if got := f.Props["default_value"]; got != tc.want {
			t.Errorf("Settings.%s default_value = %q, want %q", tc.field, got, tc.want)
		}
	}

	// Controls: a default that is not one literal must publish no value at all,
	// rather than a truncated or a synthesized one. `ITEMS` and `PROXY` carry a
	// comment that reads like a default, so a value can only come from the
	// comment; `CUSTOM` and `NESTED` name a call that is not this field's
	// `Field()`. `DESC`, `DESC2`, `ALIASD` and `DFAC` have no `default=` argument
	// at all — the text is prose inside a `description=`/`alias=` string, which is
	// ordinary in real models, so a value can only come from reading a string
	// literal as code. `COND` names two `Field()` calls and neither one is the
	// field's default. `UNDERSCORED` and `EXPONENT` are numeric literals outside
	// the recognized pattern, so publishing `1` or `1.5` would be a truncation.
	for _, field := range []string{
		"HOST", "TAGS", "EXPIRES", "GREETING", "ITEMS", "PROXY", "CUSTOM", "NESTED",
		"DESC", "DESC2", "ALIASD", "DFAC", "COND", "UNDERSCORED", "EXPONENT", "REQUIRED",
	} {
		f := findFieldChild(rs, "Settings."+field)
		if f == nil {
			t.Fatalf("expected field sub-entity Settings.%s", field)
		}
		if got, ok := f.Props["default_value"]; ok {
			t.Errorf("Settings.%s must not carry default_value, got %q", field, got)
		}
	}

	// Optionality is decided exactly as before.
	if f := findFieldChild(rs, "Settings.REQUIRED"); f.Props["optional"] == "true" {
		t.Error("Settings.REQUIRED (no default) must stay required")
	}
	if f := findFieldChild(rs, "Settings.HOST"); f.Props["optional"] != "true" {
		t.Error("Settings.HOST (has a default) must stay optional")
	}
}

// A DRF serializer field goes through the same emitter and must not grow the
// property, because its declaration carries no annotated default.
func TestDRF_FieldMembersCarryNoDefaultValue_6561(t *testing.T) {
	src := `from rest_framework import serializers

class UserSerializer(serializers.Serializer):
    name = serializers.CharField(max_length=100, default="anon")
`
	rs := extract(t, "python_django", src)
	f := findFieldChild(rs, "UserSerializer.name")
	if f == nil {
		t.Fatal("expected field sub-entity UserSerializer.name")
	}
	if got, ok := f.Props["default_value"]; ok {
		t.Errorf("UserSerializer.name must not carry default_value, got %q", got)
	}
}
