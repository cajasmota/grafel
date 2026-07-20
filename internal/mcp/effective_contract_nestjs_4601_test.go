package mcp

// effective_contract_nestjs_4601_test.go — value-asserting coverage for the
// NestJS effective-contract resolver (#4601).
//
// The fixture is a NestJS controller endpoint with:
//   - a @Body() CreateXDto whose class-validator fields are on the graph as
//     SCOPE.Schema subtype=field entities,
//   - a branching handler returning 201 on success and throwing
//     ConflictException (409) and NotFoundException (404),
//   - a @RequirePage("client_admin") guard.
//
// It asserts effective_contract returns the request fields (composed from the
// VALIDATES → dto:CreateXDto edge + the DTO field members), the per-branch
// response shapes+statuses (composed from the effects-branches facet over the
// handler body), and the auth posture (composed from the effective guard) —
// the SAME effectiveContract structure the DRF resolver emits.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// nestControllerSource is the on-disk handler body the branch analyzer reads.
// Line 1 is blank so line numbers below are 1-indexed against this string.
const nestControllerSource = `
@Controller('users')
export class UsersController {
  @Post()
  @RequirePage("client_admin")
  async create(@Body() dto: CreateXDto): Promise<UserDto> {
    const existing = await this.repo.findByEmail(dto.email);
    if (existing) {
      throw new ConflictException('email taken');
    }
    if (!dto.name) {
      throw new NotFoundException('missing');
    }
    return this.repo.create(dto);
  }
}
`

// buildNestContractServer writes the controller source to a temp repo dir and
// builds a Server whose graph models the endpoint, its handler, the DTO, and
// the DTO's class-validator fields — the signals the resolver composes.
func buildNestContractServer(t *testing.T) *Server {
	t.Helper()
	repoDir := t.TempDir()
	srcRel := "src/users/users.controller.ts"
	abs := filepath.Join(repoDir, srcRel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(nestControllerSource), 0o644); err != nil {
		t.Fatal(err)
	}

	doc := &graph.Document{
		Repo: "v3",
		Entities: []graph.Entity{
			// The endpoint definition.
			graph.Entity{
				ID: "ep:post:/users", Name: "http:POST:/users",
				Kind: "http_endpoint_definition", Language: "typescript",
				SourceFile: srcRel,
			}.WithProperties(map[string]string{
				"framework":         "nestjs",
				"verb":              "POST",
				"path":              "/users",
				"request_body_type": "CreateXDto",
				"require_page":      "client_admin",
				"auth_required":     "true",
			},
			),
			// The handler method (controller leaf = UsersController).
			graph.Entity{
				ID: "op:UsersController.create", Name: "create",
				QualifiedName: "UsersController.create",
				Kind:          "SCOPE.Operation", Subtype: "method",
				Language: "typescript", SourceFile: srcRel,
				StartLine: 6, EndLine: 15,
			}.WithProperties(map[string]string{"require_page": "client_admin"}),
			// The DTO class + its class-validator fields (SCOPE.Schema/field).
			{ID: "dto:CreateXDto", Name: "CreateXDto", Kind: "SCOPE.Component", Subtype: "class", Language: "typescript", SourceFile: "src/users/dto/create-x.dto.ts"},
			{ID: "f:email", Name: "CreateXDto.email", Kind: "SCOPE.Schema", Subtype: "field", Signature: "email: string", Language: "typescript"},
			{ID: "f:name", Name: "CreateXDto.name", Kind: "SCOPE.Schema", Subtype: "field", Signature: "name: string", Language: "typescript"},
			{ID: "f:age", Name: "CreateXDto.age?", Kind: "SCOPE.Schema", Subtype: "field", Signature: "age?: number", Language: "typescript"},
		},
		Relationships: []graph.Relationship{
			// handler IMPLEMENTS endpoint (the resolution-pass bridge).
			{FromID: "op:UsersController.create", ToID: "ep:post:/users", Kind: "IMPLEMENTS"},
			// handler VALIDATES dto:CreateXDto (the @Body() DTO extraction edge).
			graph.Relationship{FromID: "op:UsersController.create", ToID: "dto:CreateXDto", Kind: "VALIDATES"}.WithProperties(map[string]string{"via": "dto_extraction", "method": "@Body()", "dto": "CreateXDto"}),
		},
	}

	reg := &Registry{Groups: map[string]RegistryGroup{
		"test": {Repos: map[string]RegistryRepo{"v3": {Path: repoDir}}},
	}}
	st := NewState(reg)
	st.mu.Lock()
	lg := &LoadedGroup{Name: "test", Repos: map[string]*LoadedRepo{
		"v3": {Repo: "v3", Path: repoDir, Doc: doc, LabelIndex: BuildLabelIndex(doc), BM25: BuildBM25(doc)},
	}}
	st.groups["test"] = lg
	st.mu.Unlock()
	return &Server{State: st, Tel: NewTelemetry(0)}
}

func TestEffectiveContract_NestJS_FullContract(t *testing.T) {
	srv := buildNestContractServer(t)

	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "test", "entity_id": "UsersController"}
	res, err := srv.handleEffectiveContract(context.Background(), req)
	if err != nil {
		t.Fatalf("handleEffectiveContract error: %v", err)
	}
	if res.IsError {
		t.Fatalf("handleEffectiveContract IsError: %s", resultText(res))
	}
	var out effectiveContractResult
	if err := json.Unmarshal([]byte(resultText(res)), &out); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, resultText(res))
	}

	if len(out.Groups) != 1 {
		t.Fatalf("want 1 group, got %d: %+v", len(out.Groups), out.Groups)
	}
	g := out.Groups[0]
	if g.Class != "UsersController" {
		t.Errorf("class = %q, want UsersController", g.Class)
	}
	if g.Framework != "nestjs" {
		t.Errorf("framework = %q, want nestjs", g.Framework)
	}
	if len(g.Handlers) != 1 {
		t.Fatalf("want 1 handler, got %d: %+v", len(g.Handlers), g.Handlers)
	}
	h := g.Handlers[0]

	if h.Verb != "POST" || h.Path != "/users" {
		t.Errorf("verb/path = %q %q, want POST /users", h.Verb, h.Path)
	}

	// --- request fields: composed from VALIDATES → DTO field members. ---
	gotFields := map[string]contractField{}
	for _, f := range h.RequestFields {
		gotFields[f.Name] = f
	}
	for _, want := range []string{"email", "name", "age"} {
		if _, ok := gotFields[want]; !ok {
			t.Errorf("missing request field %q; got %+v", want, h.RequestFields)
		}
	}
	if f := gotFields["email"]; f.In != "body" || f.DTO != "CreateXDto" || f.Type != "string" {
		t.Errorf("email field = %+v; want in=body dto=CreateXDto type=string", f)
	}
	if f := gotFields["age"]; f.Required {
		t.Errorf("age should be optional (age?), got required=%v", f.Required)
	}
	if f := gotFields["email"]; !f.Required {
		t.Errorf("email should be required, got %+v", f)
	}

	// --- per-branch response shapes + statuses: from the effects-branches facet. ---
	gotStatuses := map[int]bool{}
	for _, b := range h.ResponseBranches {
		gotStatuses[b.Status] = true
	}
	for _, want := range []int{404, 409} {
		if !gotStatuses[want] {
			t.Errorf("missing response branch status %d; got %+v", want, h.ResponseBranches)
		}
	}
	// Error statuses mirrored into the DRF-style flat field.
	errSet := map[int]bool{}
	for _, s := range h.ErrorStatuses {
		errSet[s] = true
	}
	if !errSet[409] || !errSet[404] {
		t.Errorf("error_statuses = %v; want to include 404 and 409", h.ErrorStatuses)
	}

	// --- auth posture: from the effective guard (@RequirePage). ---
	if h.AuthKind != "page" {
		t.Errorf("auth_kind = %q, want page", h.AuthKind)
	}
	if h.AuthLiteral != "client_admin" {
		t.Errorf("auth_literal = %q, want client_admin", h.AuthLiteral)
	}
	if !h.AuthRequired {
		t.Errorf("auth_required = false, want true")
	}
}
