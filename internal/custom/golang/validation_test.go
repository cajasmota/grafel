package golang_test

import (
	"testing"
)

// Tests for the framework-agnostic request-validation scanner (issue #3213,
// cluster 2 validation capability): struct-tag rules, custom validators, and
// binding call sites within gin/echo/fiber/chi context.

// findVal returns the first SCOPE.Pattern validation entity matching the given
// validation_kind (and, when non-empty, validation_subtype), or nil.
func findVal(ents []fullEntity, kind, subtype string) *fullEntity {
	for i := range ents {
		if ents[i].Kind != "SCOPE.Pattern" {
			continue
		}
		p := ents[i].Props
		if p["pattern_kind"] != "validation" || p["validation_kind"] != kind {
			continue
		}
		if subtype != "" && p["validation_subtype"] != subtype {
			continue
		}
		return &ents[i]
	}
	return nil
}

func valEntities(ents []fullEntity) []fullEntity {
	var out []fullEntity
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Props["pattern_kind"] == "validation" {
			out = append(out, e)
		}
	}
	return out
}

func findValRule(ents []fullEntity, structName, field string) *fullEntity {
	for i := range ents {
		p := ents[i].Props
		if p["pattern_kind"] == "validation" && p["validation_kind"] == "rule" &&
			p["struct_name"] == structName && p["field_name"] == field {
			return &ents[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Framework attribution: no recognised framework marker => no emit.
// ---------------------------------------------------------------------------

func TestValidationNoFrameworkNoEmit(t *testing.T) {
	src := `package x
type Req struct {
	Name string ` + "`" + `validate:"required"` + "`" + `
}`
	ents := extractFull(t, "custom_go_validation", fi("main.go", "go", src))
	if got := valEntities(ents); len(got) != 0 {
		t.Fatalf("expected no validation entities without a framework marker, got %d: %+v", len(got), got)
	}
}

func TestValidationNonGoNoEmit(t *testing.T) {
	src := `r := gin.Default()`
	ents := extractFull(t, "custom_go_validation", fi("main.py", "python", src))
	if len(ents) != 0 {
		t.Fatalf("non-go file should yield nothing, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Struct-tag rule detection: binding: (gin) and validate: (others).
// ---------------------------------------------------------------------------

func TestValidationBindingTagRule(t *testing.T) {
	src := "r := gin.Default()\n" +
		"type Req struct {\n" +
		"\tName string `json:\"name\" binding:\"required,min=2\"`\n" +
		"\tSkip string `json:\"-\" binding:\"-\"`\n" +
		"}\n"
	ents := extractFull(t, "custom_go_validation", fi("main.go", "go", src))
	r := findValRule(ents, "Req", "Name")
	if r == nil {
		t.Fatalf("missing rule for Req.Name; got %+v", valEntities(ents))
	}
	if r.Props["rules"] != "required,min=2" {
		t.Errorf("rules=%q want required,min=2", r.Props["rules"])
	}
	if r.Props["rule_source"] != "binding" {
		t.Errorf("rule_source=%q want binding", r.Props["rule_source"])
	}
	// the "-" skip tag must not produce a rule entity.
	if findValRule(ents, "Req", "Skip") != nil {
		t.Errorf("explicit skip tag (binding:\"-\") should not emit a rule")
	}
}

func TestValidationValidateTagRule(t *testing.T) {
	src := "e := echo.New()\n" +
		"type Login struct {\n" +
		"\tUsername string `validate:\"required,alphanum\"`\n" +
		"}\n"
	ents := extractFull(t, "custom_go_validation", fi("main.go", "go", src))
	r := findValRule(ents, "Login", "Username")
	if r == nil {
		t.Fatalf("missing rule for Login.Username; got %+v", valEntities(ents))
	}
	if r.Props["rule_source"] != "validate" {
		t.Errorf("rule_source=%q want validate", r.Props["rule_source"])
	}
	if r.Props["framework"] != "echo" {
		t.Errorf("framework=%q want echo", r.Props["framework"])
	}
}

// ---------------------------------------------------------------------------
// Custom validators.
// ---------------------------------------------------------------------------

func TestValidationCustomValidator(t *testing.T) {
	src := "r := gin.Default()\n" +
		"v := validator.New()\n" +
		"v.RegisterValidation(\"is_even\", fn)\n"
	ents := extractFull(t, "custom_go_validation", fi("main.go", "go", src))
	if findVal(ents, "validator", "validator_new") == nil {
		t.Errorf("missing validator_new; got %+v", valEntities(ents))
	}
	reg := findVal(ents, "validator", "register_validation")
	if reg == nil {
		t.Fatalf("missing register_validation; got %+v", valEntities(ents))
	}
	if reg.Props["tag"] != "is_even" {
		t.Errorf("tag=%q want is_even", reg.Props["tag"])
	}
}

// ---------------------------------------------------------------------------
// Binding call sites, per framework dialect.
// ---------------------------------------------------------------------------

func TestValidationBindingCallSites(t *testing.T) {
	cases := []struct {
		name, src, subtype string
	}{
		{"gin_shouldbind", "r := gin.Default()\nc.ShouldBindJSON(&req)", "bind_call"},
		{"echo_validate", "e := echo.New()\nc.Validate(&req)", "validate_call"},
		{"fiber_bodyparser", "app := fiber.New()\nc.BodyParser(&req)", "parse_call"},
		{"chi_render", "r := chi.NewRouter()\nrender.DecodeJSON(req.Body, &b)", "bind_call"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ents := extractFull(t, "custom_go_validation", fi("main.go", "go", c.src))
			if findVal(ents, "binding", c.subtype) == nil {
				t.Fatalf("missing binding/%s; got %+v", c.subtype, valEntities(ents))
			}
		})
	}
}

func TestValidationProvenance(t *testing.T) {
	src := "r := gin.Default()\nv := validator.New()"
	ents := extractFull(t, "custom_go_validation", fi("main.go", "go", src))
	v := findVal(ents, "validator", "validator_new")
	if v == nil {
		t.Fatal("missing validator_new")
	}
	if v.Props["provenance"] != "INFERRED_FROM_GIN_VALIDATION" {
		t.Errorf("provenance=%q want INFERRED_FROM_GIN_VALIDATION", v.Props["provenance"])
	}
}

// ---------------------------------------------------------------------------
// Fixture-driven end-to-end checks: each framework fixture carries struct-tag
// rules + a binding call site (+ gin a custom validator). These prove the
// general gin/echo/fiber/chi validation surface.
// ---------------------------------------------------------------------------

func TestValidationFixtures(t *testing.T) {
	cases := []struct {
		fixture, framework, ruleStruct, ruleField, bindSubtype string
	}{
		{"gin_validation.go", "gin", "CreateUserReq", "Email", "bind_call"},
		{"echo_validation.go", "echo", "LoginReq", "Password", "validate_call"},
		{"fiber_validation.go", "fiber", "SignupReq", "Email", "parse_call"},
		{"chi_validation.go", "chi", "OrderReq", "SKU", "bind_call"},
		// extended frameworks (issue #3213); fasthttp + revel are
		// honesty-NA (no struct-tag binding) and intentionally absent.
		{"beego_validation.go", "beego", "BeegoCreateUserReq", "Email", "parse_call"},
		{"iris_validation.go", "iris", "IrisLoginReq", "Password", "parse_call"},
		{"hertz_validation.go", "hertz", "HertzSignupReq", "Email", "validate_call"},
		{"buffalo_validation.go", "buffalo", "BuffaloOrderReq", "SKU", "bind_call"},
		{"gorilla_mux_validation.go", "gorilla-mux", "MuxCreateUserReq", "Email", "decode_call"},
		{"nethttp_validation.go", "net-http", "NetHTTPSignupReq", "Username", "decode_call"},
	}
	for _, c := range cases {
		t.Run(c.framework, func(t *testing.T) {
			ents := extractFull(t, "custom_go_validation", fixtureFile(t, c.fixture))

			// a struct-tag rule on the expected field.
			if findValRule(ents, c.ruleStruct, c.ruleField) == nil {
				t.Errorf("%s: missing rule %s.%s; got %+v",
					c.framework, c.ruleStruct, c.ruleField, valEntities(ents))
			}

			// a binding call site (framework's dialect).
			if findVal(ents, "binding", c.bindSubtype) == nil {
				t.Errorf("%s: missing binding/%s", c.framework, c.bindSubtype)
			}

			// every emitted entity must be attributed to this framework.
			for _, e := range valEntities(ents) {
				if e.Props["framework"] != c.framework {
					t.Errorf("%s: entity %q framework=%q", c.framework, e.Name, e.Props["framework"])
				}
			}
		})
	}
}
