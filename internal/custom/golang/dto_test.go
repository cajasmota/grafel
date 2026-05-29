package golang_test

import (
	"strings"
	"testing"
)

// Tests for the framework-agnostic request/response DTO-struct scanner
// (issue #3255, part of #3210): request-bind + response-serialise call sites
// resolved to file-local struct models, emitted as SCOPE.Schema (subtype=dto).

// dtoEntities returns all DTO schema entities (subtype carried in the name and
// pattern_kind=dto prop).
func dtoEntities(ents []fullEntity) []fullEntity {
	var out []fullEntity
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Props["pattern_kind"] == "dto" {
			out = append(out, e)
		}
	}
	return out
}

// findDto returns the DTO entity for the given direction whose resolved struct
// name (or, when unresolved, bound expr) matches, or nil.
func findDto(ents []fullEntity, direction, name string) *fullEntity {
	for i := range ents {
		e := &ents[i]
		if e.Kind != "SCOPE.Schema" || e.Props["pattern_kind"] != "dto" {
			continue
		}
		if e.Props["dto_direction"] != direction {
			continue
		}
		if e.Props["struct_name"] == name || e.Props["bound_expr"] == name {
			return e
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Framework attribution: no recognised framework marker => no emit.
// ---------------------------------------------------------------------------

func TestDtoNoFrameworkNoEmit(t *testing.T) {
	src := "package x\n" +
		"type Req struct {\n\tName string `json:\"name\"`\n}\n" +
		"func h() { json.NewDecoder(r).Decode(&req) }\n"
	ents := extractFull(t, "custom_go_dto", fi("main.go", "go", src))
	if got := dtoEntities(ents); len(got) != 0 {
		t.Fatalf("expected no DTO entities without a framework marker, got %d: %+v", len(got), got)
	}
}

func TestDtoNonGoNoEmit(t *testing.T) {
	src := "r := gin.Default(); c.ShouldBindJSON(&req)"
	ents := extractFull(t, "custom_go_dto", fi("main.py", "python", src))
	if len(ents) != 0 {
		t.Fatalf("non-go file should yield nothing, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Request binding resolves the bound variable to its struct type + fields.
// ---------------------------------------------------------------------------

func TestDtoRequestResolvesStruct(t *testing.T) {
	src := "r := gin.Default()\n" +
		"type CreateReq struct {\n" +
		"\tEmail string `json:\"email\"`\n" +
		"\tAge   int    `json:\"age\"`\n" +
		"}\n" +
		"func h(c *gin.Context) {\n" +
		"\tvar req CreateReq\n" +
		"\tc.ShouldBindJSON(&req)\n" +
		"}\n"
	ents := extractFull(t, "custom_go_dto", fi("main.go", "go", src))
	d := findDto(ents, "request", "CreateReq")
	if d == nil {
		t.Fatalf("missing request DTO CreateReq; got %+v", dtoEntities(ents))
	}
	if d.Props["resolved"] != "true" {
		t.Errorf("resolved=%q want true", d.Props["resolved"])
	}
	if d.Props["field_count"] != "2" {
		t.Errorf("field_count=%q want 2", d.Props["field_count"])
	}
	if !strings.Contains(d.Props["fields"], "Email:string") || !strings.Contains(d.Props["fields"], "Age:int") {
		t.Errorf("fields=%q missing Email/Age", d.Props["fields"])
	}
	if d.Props["framework"] != "gin" {
		t.Errorf("framework=%q want gin", d.Props["framework"])
	}
	if d.Props["binding_subtype"] != "should_bind" {
		t.Errorf("binding_subtype=%q want should_bind", d.Props["binding_subtype"])
	}
}

// ':=' composite-literal declaration also resolves the type.
func TestDtoShortDeclResolves(t *testing.T) {
	src := "app := fiber.New()\n" +
		"type Body struct {\n\tX string `json:\"x\"`\n}\n" +
		"func h(c *fiber.Ctx) error {\n" +
		"\tbody := Body{}\n" +
		"\treturn c.BodyParser(&body)\n" +
		"}\n"
	ents := extractFull(t, "custom_go_dto", fi("main.go", "go", src))
	d := findDto(ents, "request", "Body")
	if d == nil {
		t.Fatalf("missing request DTO Body; got %+v", dtoEntities(ents))
	}
	if d.Props["resolved"] != "true" || d.Props["binding_subtype"] != "body_parser" {
		t.Errorf("unexpected props %+v", d.Props)
	}
}

// An unresolvable bound var still emits a request DTO with resolved=false.
func TestDtoUnresolvedRequestStillEmits(t *testing.T) {
	src := "r := gin.Default()\n" +
		"func h(c *gin.Context) { c.ShouldBindJSON(&mystery) }\n"
	ents := extractFull(t, "custom_go_dto", fi("main.go", "go", src))
	d := findDto(ents, "request", "mystery")
	if d == nil {
		t.Fatalf("missing unresolved request DTO; got %+v", dtoEntities(ents))
	}
	if d.Props["resolved"] != "false" {
		t.Errorf("resolved=%q want false", d.Props["resolved"])
	}
}

// ---------------------------------------------------------------------------
// Response serialiser only emits when it resolves to a known struct (avoids
// flooding the graph with generic locals like err/code).
// ---------------------------------------------------------------------------

func TestDtoResponseRequiresKnownStruct(t *testing.T) {
	src := "r := gin.Default()\n" +
		"type Resp struct {\n\tID int `json:\"id\"`\n}\n" +
		"func h(c *gin.Context) {\n" +
		"\tresp := Resp{}\n" +
		"\tc.JSON(200, resp)\n" +
		"\tc.JSON(400, err)\n" +
		"}\n"
	ents := extractFull(t, "custom_go_dto", fi("main.go", "go", src))
	if findDto(ents, "response", "Resp") == nil {
		t.Fatalf("missing response DTO Resp; got %+v", dtoEntities(ents))
	}
	// `err` does not resolve to a struct -> no response DTO for it.
	if findDto(ents, "response", "err") != nil {
		t.Errorf("generic local err should not emit a response DTO")
	}
}

func TestDtoProvenance(t *testing.T) {
	src := "r := gin.Default()\n" +
		"type Req struct {\n\tA string `json:\"a\"`\n}\n" +
		"func h(c *gin.Context) { var req Req; c.ShouldBindJSON(&req) }\n"
	ents := extractFull(t, "custom_go_dto", fi("main.go", "go", src))
	d := findDto(ents, "request", "Req")
	if d == nil {
		t.Fatal("missing request DTO Req")
	}
	if d.Props["provenance"] != "INFERRED_FROM_GIN_DTO_REQUEST" {
		t.Errorf("provenance=%q want INFERRED_FROM_GIN_DTO_REQUEST", d.Props["provenance"])
	}
}

// ---------------------------------------------------------------------------
// Fixture-driven end-to-end checks. Each fixture carries a request DTO struct
// bound in a handler + a response DTO struct serialised back. These prove the
// general request-bind + response-serialise + struct-field case across the
// flagship gin/echo/fiber/chi frameworks plus the stdlib-decode family
// (net-http / gorilla-mux).
// ---------------------------------------------------------------------------

func TestDtoFixtures(t *testing.T) {
	cases := []struct {
		fixture, framework, reqStruct, respStruct, reqField string
	}{
		{"gin_dto.go", "gin", "CreateUserReq", "UserResp", "Email"},
		{"echo_dto.go", "echo", "LoginReq", "TokenResp", "Username"},
		{"fiber_dto.go", "fiber", "SignupReq", "SignupResp", "Email"},
		{"chi_dto.go", "chi", "OrderReq", "OrderResp", "SKU"},
		{"nethttp_dto.go", "net-http", "NetHTTPSignupReq", "NetHTTPSignupResp", "Username"},
		{"gorilla_mux_dto.go", "gorilla-mux", "MuxCreateUserReq", "MuxUserResp", "Email"},
	}
	for _, c := range cases {
		t.Run(c.framework, func(t *testing.T) {
			ents := extractFull(t, "custom_go_dto", fixtureFile(t, c.fixture))

			req := findDto(ents, "request", c.reqStruct)
			if req == nil {
				t.Fatalf("%s: missing request DTO %s; got %+v", c.framework, c.reqStruct, dtoEntities(ents))
			}
			if req.Props["resolved"] != "true" {
				t.Errorf("%s: request DTO not resolved: %+v", c.framework, req.Props)
			}
			if !strings.Contains(req.Props["fields"], c.reqField+":") {
				t.Errorf("%s: request DTO fields %q missing %s", c.framework, req.Props["fields"], c.reqField)
			}

			if resp := findDto(ents, "response", c.respStruct); resp == nil {
				t.Errorf("%s: missing response DTO %s; got %+v", c.framework, c.respStruct, dtoEntities(ents))
			}

			// every emitted DTO must be attributed to this framework.
			for _, e := range dtoEntities(ents) {
				if e.Props["framework"] != c.framework {
					t.Errorf("%s: entity %q framework=%q", c.framework, e.Name, e.Props["framework"])
				}
			}
		})
	}
}
