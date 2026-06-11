package mcp

// effective_contract_express_4710_test.go — value-asserting coverage for the
// Express/Fastify effective-contract resolver (#4710).
//
// The fixture is an Express handler with:
//   - a VALIDATES edge to a zod schema dto:createUserSchema whose field members
//     are on the graph as SCOPE.Schema subtype=field entities (#3073/#4635),
//   - a branching handler returning res.status(409).json(...) and
//     res.status(404).json(...) numeric statuses,
//   - a requireAuth middleware (auth_middleware prop).
//
// It asserts effective_contract returns the request fields (validation-schema
// body), the per-branch numeric response statuses (from the JSTS analyzer), and
// the authenticated auth posture — the SAME effectiveContract structure the
// DRF/NestJS resolvers emit. Express is the loosest stack; this documents what is
// recoverable when a validation schema IS linked.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

const expressHandlerSource = `
async function createUser(req, res) {
  const dto = createUserSchema.parse(req.body);
  if (await exists(dto.email)) {
    return res.status(409).json({ error: 'exists' });
  }
  if (!dto.name) {
    return res.status(404).json({ error: 'missing' });
  }
  return res.status(201).json(await repo.create(dto));
}
`

func buildExpressContractServer(t *testing.T) *Server {
	t.Helper()
	repoDir := t.TempDir()
	srcRel := "src/routes/users.js"
	abs := filepath.Join(repoDir, srcRel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(expressHandlerSource), 0o644); err != nil {
		t.Fatal(err)
	}

	doc := &graph.Document{
		Repo: "web",
		Entities: []graph.Entity{
			{
				ID: "ep:post:/users", Name: "http:POST:/users",
				Kind: "http_endpoint_definition", Language: "javascript",
				SourceFile: srcRel,
				Properties: map[string]string{
					"framework":       "express",
					"verb":            "POST",
					"path":            "/users",
					"auth_middleware": "requireAuth",
				},
			},
			{
				ID: "op:createUser", Name: "createUser",
				QualifiedName: "createUser",
				Kind:          "SCOPE.Operation", Subtype: "function",
				Language: "javascript", SourceFile: srcRel,
				StartLine: 2, EndLine: 11,
			},
			{ID: "dto:createUserSchema", Name: "createUserSchema", Kind: "SCOPE.Component", Subtype: "schema", Language: "javascript", SourceFile: "src/schemas/user.js"},
			{ID: "f:email", Name: "createUserSchema.email", Kind: "SCOPE.Schema", Subtype: "field", Language: "javascript",
				Properties: map[string]string{"field_name": "email", "field_type": "string", "parent_class": "createUserSchema"}},
			{ID: "f:name", Name: "createUserSchema.name", Kind: "SCOPE.Schema", Subtype: "field", Language: "javascript",
				Properties: map[string]string{"field_name": "name", "field_type": "string", "parent_class": "createUserSchema"}},
			{ID: "f:age", Name: "createUserSchema.age?", Kind: "SCOPE.Schema", Subtype: "field", Language: "javascript",
				Properties: map[string]string{"field_name": "age", "field_type": "number", "parent_class": "createUserSchema", "optional": "true"}},
		},
		Relationships: []graph.Relationship{
			{FromID: "op:createUser", ToID: "ep:post:/users", Kind: "IMPLEMENTS"},
			{FromID: "op:createUser", ToID: "dto:createUserSchema", Kind: "VALIDATES",
				Properties: map[string]string{"via": "dto_extraction", "method": "req.body", "dto": "createUserSchema"}},
		},
	}

	reg := &Registry{Groups: map[string]RegistryGroup{
		"test": {Repos: map[string]RegistryRepo{"web": {Path: repoDir}}},
	}}
	st := NewState(reg)
	st.mu.Lock()
	lg := &LoadedGroup{Name: "test", Repos: map[string]*LoadedRepo{
		"web": {Repo: "web", Path: repoDir, Doc: doc, LabelIndex: BuildLabelIndex(doc), BM25: BuildBM25(doc)},
	}}
	st.groups["test"] = lg
	st.mu.Unlock()
	return &Server{State: st, Tel: NewTelemetry(0)}
}

func TestEffectiveContract_Express_FullContract(t *testing.T) {
	srv := buildExpressContractServer(t)

	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "test", "entity_id": "createUser"}
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
	if g.Framework != "express" {
		t.Errorf("framework = %q, want express", g.Framework)
	}
	if len(g.Handlers) != 1 {
		t.Fatalf("want 1 handler, got %d: %+v", len(g.Handlers), g.Handlers)
	}
	h := g.Handlers[0]
	if h.Verb != "POST" || h.Path != "/users" {
		t.Errorf("verb/path = %q %q, want POST /users", h.Verb, h.Path)
	}

	// --- request fields: validation-schema (zod) body members. ---
	gotFields := map[string]contractField{}
	for _, f := range h.RequestFields {
		gotFields[f.Name] = f
	}
	for _, want := range []string{"email", "name", "age"} {
		if _, ok := gotFields[want]; !ok {
			t.Errorf("missing request field %q; got %+v", want, h.RequestFields)
		}
	}
	if f := gotFields["email"]; f.In != "body" || f.DTO != "createUserSchema" || f.Type != "string" {
		t.Errorf("email field = %+v; want in=body dto=createUserSchema type=string", f)
	}
	if f := gotFields["age"]; f.Required {
		t.Errorf("age should be optional, got required=%v", f.Required)
	}

	// --- per-branch numeric response statuses: from the JSTS analyzer. ---
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

	// --- auth posture: requireAuth middleware → authenticated. ---
	if h.AuthKind != "authenticated" {
		t.Errorf("auth_kind = %q, want authenticated", h.AuthKind)
	}
	if !h.AuthRequired {
		t.Errorf("auth_required = false, want true")
	}
}
