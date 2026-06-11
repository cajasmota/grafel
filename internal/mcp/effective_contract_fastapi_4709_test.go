package mcp

// effective_contract_fastapi_4709_test.go — value-asserting coverage for the
// FastAPI effective-contract resolver (#4709).
//
// The fixture is a FastAPI path-operation with:
//   - a Pydantic body model CreateItem whose #4613 field members are on the graph
//     as SCOPE.Schema subtype=field entities,
//   - a branching handler raising HTTPException(status_code=409) and
//     HTTPException(status_code=404),
//   - a decorator status_code=201 success default + response_model=ItemOut,
//   - a Depends(get_current_user) security dependency.
//
// It asserts effective_contract returns the request fields (Pydantic model), the
// per-branch response statuses (raised HTTPExceptions from the Python analyzer +
// the 201 decorator success), and the authenticated auth posture — the SAME
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

const fastapiRouterSource = `
@router.post("/items", status_code=201, response_model=ItemOut)
async def create_item(payload: CreateItem, user=Depends(get_current_user)):
    if exists(payload.sku):
        raise HTTPException(status_code=409, detail="exists")
    if not payload.name:
        raise HTTPException(status_code=404, detail="missing")
    return repo.create(payload)
`

func buildFastAPIContractServer(t *testing.T) *Server {
	t.Helper()
	repoDir := t.TempDir()
	srcRel := "app/routers/items.py"
	abs := filepath.Join(repoDir, srcRel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(fastapiRouterSource), 0o644); err != nil {
		t.Fatal(err)
	}

	doc := &graph.Document{
		Repo: "api",
		Entities: []graph.Entity{
			{
				ID: "ep:post:/items", Name: "http:POST:/items",
				Kind: "http_endpoint_definition", Language: "python",
				SourceFile: srcRel,
				Properties: map[string]string{
					"framework":         "fastapi",
					"verb":              "POST",
					"path":              "/items",
					"request_body_type": "CreateItem",
					"response_model":    "ItemOut",
					"status_code":       "201",
					"auth_required":     "true",
				},
			},
			{
				ID: "op:create_item", Name: "create_item",
				QualifiedName: "create_item",
				Kind:          "SCOPE.Operation", Subtype: "function",
				Language: "python", SourceFile: srcRel,
				StartLine: 3, EndLine: 9,
			},
			{ID: "dto:CreateItem", Name: "CreateItem", Kind: "SCOPE.Component", Subtype: "class", Language: "python", SourceFile: "app/schemas.py"},
			{ID: "f:sku", Name: "CreateItem.sku", Kind: "SCOPE.Schema", Subtype: "field", Language: "python",
				Properties: map[string]string{"field_name": "sku", "field_type": "string", "parent_class": "CreateItem"}},
			{ID: "f:name", Name: "CreateItem.name", Kind: "SCOPE.Schema", Subtype: "field", Language: "python",
				Properties: map[string]string{"field_name": "name", "field_type": "string", "parent_class": "CreateItem"}},
			{ID: "f:qty", Name: "CreateItem.qty", Kind: "SCOPE.Schema", Subtype: "field", Language: "python",
				Properties: map[string]string{"field_name": "qty", "field_type": "integer", "parent_class": "CreateItem", "optional": "true"}},
		},
		Relationships: []graph.Relationship{
			{FromID: "op:create_item", ToID: "ep:post:/items", Kind: "IMPLEMENTS"},
		},
	}

	reg := &Registry{Groups: map[string]RegistryGroup{
		"test": {Repos: map[string]RegistryRepo{"api": {Path: repoDir}}},
	}}
	st := NewState(reg)
	st.mu.Lock()
	lg := &LoadedGroup{Name: "test", Repos: map[string]*LoadedRepo{
		"api": {Repo: "api", Path: repoDir, Doc: doc, LabelIndex: BuildLabelIndex(doc), BM25: BuildBM25(doc)},
	}}
	st.groups["test"] = lg
	st.mu.Unlock()
	return &Server{State: st, Tel: NewTelemetry(0)}
}

func TestEffectiveContract_FastAPI_FullContract(t *testing.T) {
	srv := buildFastAPIContractServer(t)

	req := mcpapi.CallToolRequest{}
	// Group by the path-op function name.
	req.Params.Arguments = map[string]any{"group": "test", "entity_id": "create_item"}
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
	if g.Framework != "fastapi" {
		t.Errorf("framework = %q, want fastapi", g.Framework)
	}
	if len(g.Handlers) != 1 {
		t.Fatalf("want 1 handler, got %d: %+v", len(g.Handlers), g.Handlers)
	}
	h := g.Handlers[0]
	if h.Verb != "POST" || h.Path != "/items" {
		t.Errorf("verb/path = %q %q, want POST /items", h.Verb, h.Path)
	}

	// --- request fields: Pydantic body model members. ---
	gotFields := map[string]contractField{}
	for _, f := range h.RequestFields {
		gotFields[f.Name] = f
	}
	for _, want := range []string{"sku", "name", "qty"} {
		if _, ok := gotFields[want]; !ok {
			t.Errorf("missing request field %q; got %+v", want, h.RequestFields)
		}
	}
	if f := gotFields["sku"]; f.In != "body" || f.DTO != "CreateItem" || f.Type != "string" {
		t.Errorf("sku field = %+v; want in=body dto=CreateItem type=string", f)
	}
	if f := gotFields["qty"]; f.Required {
		t.Errorf("qty should be optional, got required=%v", f.Required)
	}

	// --- per-branch response statuses: raised HTTPExceptions + 201 default. ---
	gotStatuses := map[int]bool{}
	for _, b := range h.ResponseBranches {
		gotStatuses[b.Status] = true
	}
	for _, want := range []int{201, 404, 409} {
		if !gotStatuses[want] {
			t.Errorf("missing response branch status %d; got %+v", want, h.ResponseBranches)
		}
	}
	if h.DefaultStatus != 201 {
		t.Errorf("default_status = %d, want 201 (decorator status_code)", h.DefaultStatus)
	}
	if h.Serializer != "ItemOut" {
		t.Errorf("serializer = %q, want ItemOut (response_model)", h.Serializer)
	}
	errSet := map[int]bool{}
	for _, s := range h.ErrorStatuses {
		errSet[s] = true
	}
	if !errSet[409] || !errSet[404] {
		t.Errorf("error_statuses = %v; want 404 and 409", h.ErrorStatuses)
	}

	// --- auth posture: Depends(get_current_user) → authenticated. ---
	if h.AuthKind != "authenticated" {
		t.Errorf("auth_kind = %q, want authenticated", h.AuthKind)
	}
	if !h.AuthRequired {
		t.Errorf("auth_required = false, want true")
	}
}
