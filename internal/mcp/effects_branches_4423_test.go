package mcp

// effects_branches_4423_test.go — in-pipeline test for the #4423 `branches`
// facet on REAL upvate Django oracle functions.
//
// Per the standing LIVE-VALIDATE rule (fixtures lie at merge): this exercises
// the REAL effects MCP handler (handleEffects) end-to-end — resolver, on-disk
// source read, per-language branch analyzer — over byte-copies of actual
// branchy oracle functions copied verbatim into testdata/branches_4423/ (NOT
// edited): ContractViewSet.create_contact (400/409/200/201/500 branches +
// except-swallow) and two ecb_pdf_pipeline helpers (an env-gate + a
// swallow-only except).
//
// It asserts:
//   - RED-before / GREEN-after: the DEFAULT call (no include) carries NO
//     `branches` key (default output unchanged — no regression), while
//     include="branches" enumerates every except/early_return/env_gate/guard
//     with the correct outcome + env_var;
//   - the env-gate's env_var is surfaced (ECB_PDF_PIPELINE_ENABLED);
//   - a logging-only except is classified `swallow`, a re-raising one `raise`;
//   - HTTP statuses (409/200/201) are derived into returns.status.

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// branchesTestServer builds a server whose single repo's Path points at the
// branches_4423 testdata dir, with one Operation entity per fixture spanning
// the whole file. Returns the server.
func branchesTestServer(t *testing.T) *Server {
	t.Helper()
	doc := &graph.Document{
		Repo: "upvate-core",
		Entities: []graph.Entity{
			{
				ID: "op_create_contact", Name: "ContractViewSet.create_contact",
				Kind: "SCOPE.Operation", QualifiedName: "core.views.contract_viewset.ContractViewSet.create_contact",
				SourceFile: "contract_viewset_create_contact.py", StartLine: 1, EndLine: 69,
			},
			{
				ID: "op_process_ecb", Name: "process_ecb_pdf_job",
				Kind: "SCOPE.Function", QualifiedName: "core.tasks.ecb_pdf_pipeline.process_ecb_pdf_job",
				SourceFile: "ecb_pdf_pipeline_env_gate.py", StartLine: 1, EndLine: 38,
			},
			{
				ID: "op_write_debug", Name: "_write_debug_payload",
				Kind: "SCOPE.Function", QualifiedName: "core.tasks.ecb_pdf_pipeline._write_debug_payload",
				SourceFile: "ecb_write_debug_payload_swallow.py", StartLine: 1, EndLine: 34,
			},
		},
	}
	srv := newTestServer(t, doc)

	// Point the loaded repo at the real testdata dir so the handler reads the
	// verbatim oracle bytes off disk (the actual in-pipeline path).
	abs, err := filepath.Abs(filepath.Join("testdata", "branches_4423"))
	if err != nil {
		t.Fatalf("abs testdata: %v", err)
	}
	srv.State.mu.Lock()
	srv.State.groups["test"].Repos["upvate-core"].Path = abs
	srv.State.mu.Unlock()
	return srv
}

func callEffects(t *testing.T, srv *Server, entity string, include string) map[string]any {
	t.Helper()
	args := map[string]any{"entity_id": entity, "group": "test"}
	if include != "" {
		args["include"] = include
	}
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	res, err := srv.handleEffects(nil, req)
	if err != nil {
		t.Fatalf("handleEffects(%s, include=%q): %v", entity, include, err)
	}
	if res.IsError {
		t.Fatalf("handleEffects(%s) returned tool error: %+v", entity, res.Content)
	}
	var txt string
	for _, c := range res.Content {
		if tc, ok := c.(mcpapi.TextContent); ok {
			txt = tc.Text
		}
	}
	if txt == "" {
		t.Fatalf("handleEffects(%s) empty content", entity)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(txt), &out); err != nil {
		t.Fatalf("unmarshal effects result: %v\n%s", err, txt)
	}
	return out
}

func branchList(t *testing.T, out map[string]any) []map[string]any {
	t.Helper()
	raw, ok := out["branches"]
	if !ok {
		t.Fatalf("no `branches` key in payload: %v", out)
	}
	arr, ok := raw.([]any)
	if !ok {
		t.Fatalf("branches not an array: %T", raw)
	}
	res := make([]map[string]any, 0, len(arr))
	for _, e := range arr {
		res = append(res, e.(map[string]any))
	}
	return res
}

// findBranch returns the first branch whose condition contains substr.
func findBranch(branches []map[string]any, substr string) map[string]any {
	for _, b := range branches {
		if cond, _ := b["condition"].(string); strings.Contains(cond, substr) {
			return b
		}
	}
	return nil
}

// TestEffectsBranches_DefaultUnchanged is the RED-before assertion: a default
// effects call (no include) must NOT carry a `branches` key. Guards the
// no-regression contract.
func TestEffectsBranches_DefaultUnchanged(t *testing.T) {
	srv := branchesTestServer(t)
	out := callEffects(t, srv, "ContractViewSet.create_contact", "")
	if _, present := out["branches"]; present {
		t.Fatalf("default effects output must NOT contain `branches`; got: %v", out["branches"])
	}
	if _, present := out["branches_supported"]; present {
		t.Fatalf("default output must not advertise branches_supported")
	}
	// Sanity: the resolved entity is still reported.
	if out["entity_id"] == nil {
		t.Fatalf("default output lost entity_id: %v", out)
	}
}

// TestEffectsBranches_CreateContact is the GREEN-after assertion on the
// flagship multi-Response ViewSet action.
func TestEffectsBranches_CreateContact(t *testing.T) {
	srv := branchesTestServer(t)
	out := callEffects(t, srv, "ContractViewSet.create_contact", "branches")

	if sup, _ := out["branches_supported"].(bool); !sup {
		t.Fatalf("branches_supported should be true for python; got %v", out["branches_supported"])
	}
	branches := branchList(t, out)

	// The function has: 400 guard (client is None), 409 guard, 200 guard
	// (user is not None), 201 created (fall-through return — no guard), and a
	// 500 except-swallow handler. The branch facet must enumerate the guards
	// and the except.
	if len(branches) < 4 {
		t.Fatalf("expected >=4 branches, got %d: %v", len(branches), branches)
	}

	// 400 — the leading guard is classified early_return, return_value, 400.
	g400 := findBranch(branches, "client is None")
	if g400 == nil {
		t.Fatalf("missing the `client is None` 400 guard: %v", branches)
	}
	if g400["kind"] != "early_return" {
		t.Errorf("client-is-None kind=%v; want early_return", g400["kind"])
	}
	if g400["outcome"] != "return_value" {
		t.Errorf("client-is-None outcome=%v; want return_value", g400["outcome"])
	}
	assertStatus(t, g400, "400")

	// 409 conflict guard.
	g409 := findBranch(branches, "available")
	if g409 == nil {
		t.Fatalf("missing the email-availability 409 guard: %v", branches)
	}
	if g409["outcome"] != "return_value" {
		t.Errorf("409 guard outcome=%v; want return_value", g409["outcome"])
	}
	assertStatus(t, g409, "409")

	// 500 except — logger? no: it returns a Response, so return_value (NOT
	// swallow) — the handler returns a 500 Response.
	gExcept := findBranch(branches, "except Exception")
	if gExcept == nil {
		t.Fatalf("missing the except Exception handler: %v", branches)
	}
	if gExcept["kind"] != "except" {
		t.Errorf("except kind=%v; want except", gExcept["kind"])
	}
	if gExcept["outcome"] != "return_value" {
		t.Errorf("create_contact except returns a 500 Response → outcome should be return_value; got %v", gExcept["outcome"])
	}
	assertStatus(t, gExcept, "500")
}

// TestEffectsBranches_EnvGate asserts the env-gate var is surfaced and the
// swallow/early-return classification on the integration task helper.
func TestEffectsBranches_EnvGate(t *testing.T) {
	srv := branchesTestServer(t)
	out := callEffects(t, srv, "process_ecb_pdf_job", "branches")
	branches := branchList(t, out)

	// `if not settings.ECB_PDF_PIPELINE_ENABLED: ... return` — env_gate with
	// env_var surfaced.
	envGate := findBranch(branches, "ECB_PDF_PIPELINE_ENABLED")
	if envGate == nil {
		t.Fatalf("missing the settings.ECB_PDF_PIPELINE_ENABLED env-gate: %v", branches)
	}
	if envGate["kind"] != "env_gate" {
		t.Errorf("env-gate kind=%v; want env_gate", envGate["kind"])
	}
	if envGate["env_var"] != "ECB_PDF_PIPELINE_ENABLED" {
		t.Errorf("env_var=%v; want ECB_PDF_PIPELINE_ENABLED", envGate["env_var"])
	}
	if envGate["outcome"] != "return_value" {
		t.Errorf("env-gate outcome=%v; want return_value (bare return)", envGate["outcome"])
	}

	// `if missing: ... return` — a second guard (early_return — first non-env
	// guard).
	missingGuard := findBranch(branches, "if missing")
	if missingGuard == nil {
		t.Fatalf("missing the `if missing` early-return guard: %v", branches)
	}
	if missingGuard["outcome"] != "return_value" {
		t.Errorf("`if missing` outcome=%v; want return_value", missingGuard["outcome"])
	}
}

// TestEffectsBranches_SwallowExcept asserts a logging-only except (never
// re-raises, never returns) is classified `swallow`.
func TestEffectsBranches_SwallowExcept(t *testing.T) {
	srv := branchesTestServer(t)
	out := callEffects(t, srv, "_write_debug_payload", "branches")
	branches := branchList(t, out)

	ex := findBranch(branches, "except Exception")
	if ex == nil {
		t.Fatalf("missing the except Exception handler: %v", branches)
	}
	if ex["kind"] != "except" {
		t.Errorf("kind=%v; want except", ex["kind"])
	}
	if ex["outcome"] != "swallow" {
		t.Errorf("logging-only except outcome=%v; want swallow (catch-and-continue)", ex["outcome"])
	}
}

func assertStatus(t *testing.T, b map[string]any, want string) {
	t.Helper()
	r, ok := b["returns"].(map[string]any)
	if !ok {
		t.Errorf("branch %q has no returns block; want status %s", b["condition"], want)
		return
	}
	if r["status"] != want {
		t.Errorf("branch %q returns.status=%v; want %s", b["condition"], r["status"], want)
	}
}
