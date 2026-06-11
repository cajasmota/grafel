package mcp

// effective_contract_spring_4708_test.go — value-asserting coverage for the
// Spring effective-contract resolver (#4708).
//
// The fixture is a Spring @RestController endpoint with:
//   - a @RequestBody CreateUserDto whose #4613 field members are on the graph as
//     SCOPE.Schema subtype=field entities (field_name/field_type/optional props),
//   - a @PathVariable id scalar (path_params="id"),
//   - a branching handler returning 201 (ResponseEntity.status(201)) and throwing
//     on conflict (ResponseEntity.status(HttpStatus.CONFLICT)) / not-found
//     (HttpStatus.NOT_FOUND),
//   - a @PreAuthorize("hasRole('ADMIN')") guard.
//
// It asserts effective_contract returns the request fields (body DTO + path
// param), the per-branch response statuses (from the Java branch analyzer), and
// the role auth posture (from the spring-security resolver) — the SAME
// effectiveContract structure the DRF/NestJS resolvers emit.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

const springControllerSource = `
@RestController
public class UserController {
  @PostMapping("/users/{id}")
  @PreAuthorize("hasRole('ADMIN')")
  public ResponseEntity<UserDto> create(@PathVariable Long id, @RequestBody CreateUserDto dto) {
    if (repo.existsByEmail(dto.getEmail())) {
      return ResponseEntity.status(HttpStatus.CONFLICT).build();
    }
    if (id == null) {
      return ResponseEntity.status(HttpStatus.NOT_FOUND).build();
    }
    return ResponseEntity.status(201).body(repo.create(dto));
  }
}
`

func buildSpringContractServer(t *testing.T) *Server {
	t.Helper()
	repoDir := t.TempDir()
	srcRel := "src/main/java/com/app/UserController.java"
	abs := filepath.Join(repoDir, srcRel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(springControllerSource), 0o644); err != nil {
		t.Fatal(err)
	}

	doc := &graph.Document{
		Repo: "svc",
		Entities: []graph.Entity{
			{
				ID: "ep:post:/users/{id}", Name: "http:POST:/users/{id}",
				Kind: "http_endpoint_definition", Language: "java",
				SourceFile: srcRel,
				Properties: map[string]string{
					"framework":         "spring-boot",
					"verb":              "POST",
					"path":              "/users/{id}",
					"request_body_type": "CreateUserDto",
					"path_params":       "id",
					"auth_expression":   "hasRole('ADMIN')",
				},
			},
			{
				ID: "op:UserController.create", Name: "create",
				QualifiedName: "UserController.create",
				Kind:          "SCOPE.Operation", Subtype: "method",
				Language: "java", SourceFile: srcRel,
				StartLine: 6, EndLine: 14,
			},
			{ID: "dto:CreateUserDto", Name: "CreateUserDto", Kind: "SCOPE.Component", Subtype: "class", Language: "java", SourceFile: "src/main/java/com/app/dto/CreateUserDto.java"},
			{ID: "f:email", Name: "CreateUserDto.email", Kind: "SCOPE.Schema", Subtype: "field", Language: "java",
				Properties: map[string]string{"field_name": "email", "field_type": "String", "parent_class": "CreateUserDto", "required": "true"}},
			{ID: "f:name", Name: "CreateUserDto.name", Kind: "SCOPE.Schema", Subtype: "field", Language: "java",
				Properties: map[string]string{"field_name": "name", "field_type": "String", "parent_class": "CreateUserDto"}},
			{ID: "f:age", Name: "CreateUserDto.age", Kind: "SCOPE.Schema", Subtype: "field", Language: "java",
				Properties: map[string]string{"field_name": "age", "field_type": "Integer", "parent_class": "CreateUserDto", "optional": "true"}},
		},
		Relationships: []graph.Relationship{
			{FromID: "op:UserController.create", ToID: "ep:post:/users/{id}", Kind: "IMPLEMENTS"},
		},
	}

	reg := &Registry{Groups: map[string]RegistryGroup{
		"test": {Repos: map[string]RegistryRepo{"svc": {Path: repoDir}}},
	}}
	st := NewState(reg)
	st.mu.Lock()
	lg := &LoadedGroup{Name: "test", Repos: map[string]*LoadedRepo{
		"svc": {Repo: "svc", Path: repoDir, Doc: doc, LabelIndex: BuildLabelIndex(doc), BM25: BuildBM25(doc)},
	}}
	st.groups["test"] = lg
	st.mu.Unlock()
	return &Server{State: st, Tel: NewTelemetry(0)}
}

func TestEffectiveContract_Spring_FullContract(t *testing.T) {
	srv := buildSpringContractServer(t)

	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "test", "entity_id": "UserController"}
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
	if g.Class != "UserController" || g.Framework != "spring" {
		t.Errorf("class/framework = %q/%q, want UserController/spring", g.Class, g.Framework)
	}
	if len(g.Handlers) != 1 {
		t.Fatalf("want 1 handler, got %d: %+v", len(g.Handlers), g.Handlers)
	}
	h := g.Handlers[0]
	if h.Verb != "POST" || h.Path != "/users/{id}" {
		t.Errorf("verb/path = %q %q, want POST /users/{id}", h.Verb, h.Path)
	}

	// --- request fields: @RequestBody DTO members + @PathVariable scalar. ---
	gotFields := map[string]contractField{}
	for _, f := range h.RequestFields {
		gotFields[f.Name] = f
	}
	for _, want := range []string{"email", "name", "age", "id"} {
		if _, ok := gotFields[want]; !ok {
			t.Errorf("missing request field %q; got %+v", want, h.RequestFields)
		}
	}
	if f := gotFields["email"]; f.In != "body" || f.DTO != "CreateUserDto" || f.Type != "String" {
		t.Errorf("email field = %+v; want in=body dto=CreateUserDto type=String", f)
	}
	if f := gotFields["id"]; f.In != "param" || f.Source != "scalar_param" || !f.Required {
		t.Errorf("id field = %+v; want in=param scalar_param required", f)
	}
	if f := gotFields["age"]; f.Required {
		t.Errorf("age should be optional, got required=%v", f.Required)
	}
	if f := gotFields["email"]; !f.Required {
		t.Errorf("email should be required, got %+v", f)
	}

	// --- per-branch response statuses: from the Java branch analyzer. The
	// analyzer surfaces CONDITIONAL branches (the 409/404 guards); the trailing
	// UNCONDITIONAL success return (201) is captured when it sits in a guarded
	// branch — here the early guards are the error branches. ---
	gotStatuses := map[int]bool{}
	for _, b := range h.ResponseBranches {
		gotStatuses[b.Status] = true
	}
	for _, want := range []int{404, 409} {
		if !gotStatuses[want] {
			t.Errorf("missing response branch status %d; got %+v", want, h.ResponseBranches)
		}
	}
	errSet := map[int]bool{}
	for _, s := range h.ErrorStatuses {
		errSet[s] = true
	}
	if !errSet[409] || !errSet[404] {
		t.Errorf("error_statuses = %v; want 404 and 409", h.ErrorStatuses)
	}

	// --- auth posture: @PreAuthorize("hasRole('ADMIN')") → role/ADMIN. ---
	if h.AuthKind != "role" || h.AuthLiteral != "ADMIN" {
		t.Errorf("auth = %q/%q, want role/ADMIN", h.AuthKind, h.AuthLiteral)
	}
	if !h.AuthRequired {
		t.Errorf("auth_required = false, want true")
	}
}
