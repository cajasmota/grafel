package mcp

// effects_branches_4445_test.go — in-pipeline test for the #4445 `branches`
// facet on C# (extends the #4423/#4435 Python flagship and the #4434 JS/TS,
// Java, Go analyzers).
//
// Per the standing LIVE-VALIDATE rule: this exercises the REAL effects MCP
// handler (handleEffects) end-to-end — resolver, on-disk source read, the
// per-language (csharp) branch analyzer registered in
// internal/substrate/branches_csharp.go — over a representative branchy C#
// controller action copied into testdata/branches_4445/:
//
//   - UserController.cs — an ASP.NET Core controller action with an env-gate
//     (Environment.GetEnvironmentVariable("SIGNUP_ENABLED") → StatusCode(503)),
//     a 400 BadRequest() validation guard, a 409 Conflict() guard inside the
//     try, and a try/catch returning StatusCode(500, ...).
//
// It asserts:
//   - RED-before / GREEN-after: the DEFAULT call (no include) carries NO
//     `branches` key (default output unchanged — no regression), while
//     include="branches" enumerates each branch with the right kind/outcome;
//   - the env-gate's env_var is surfaced (SIGNUP_ENABLED) → 503;
//   - the helper-result statuses (400 BadRequest / 409 Conflict) and the
//     explicit StatusCode(500) catch are derived into returns.status;
//   - the catch returns a value (return_value), not a re-throw.

import (
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

const csEnt = "csharp-svc::op_create_user_cs"

// branches4445Server builds a server whose single repo's Path points at the
// branches_4445 testdata dir, with one Operation entity spanning the C#
// controller action.
func branches4445Server(t *testing.T) *Server {
	t.Helper()
	doc := &graph.Document{
		Repo: "csharp-svc",
		Entities: []graph.Entity{
			{
				ID: "op_create_user_cs", Name: "UserController.CreateUser",
				Kind: "SCOPE.Operation", QualifiedName: "Example.Api.UserController.CreateUser",
				SourceFile: "UserController.cs", StartLine: 18, EndLine: 50,
			},
		},
	}
	srv := newTestServer(t, doc)
	abs, err := filepath.Abs(filepath.Join("testdata", "branches_4445"))
	if err != nil {
		t.Fatalf("abs testdata: %v", err)
	}
	srv.State.mu.Lock()
	srv.State.groups["test"].Repos["csharp-svc"].Path = abs
	srv.State.mu.Unlock()
	return srv
}

// TestEffectsBranches4445_DefaultUnchanged is the RED-before assertion: a
// default effects call (no include) must NOT carry a `branches` key. Guards the
// no-regression contract.
func TestEffectsBranches4445_DefaultUnchanged(t *testing.T) {
	srv := branches4445Server(t)
	out := callEffects(t, srv, csEnt, "")
	if _, present := out["branches"]; present {
		t.Fatalf("%s: default effects output must NOT contain `branches`", csEnt)
	}
	if _, present := out["branches_supported"]; present {
		t.Fatalf("%s: default output must not advertise branches_supported", csEnt)
	}
	if out["entity_id"] == nil {
		t.Fatalf("%s: default output lost entity_id", csEnt)
	}
}

// TestEffectsBranches4445_CSharp is the GREEN-after assertion for C#.
func TestEffectsBranches4445_CSharp(t *testing.T) {
	srv := branches4445Server(t)
	out := callEffects(t, srv, csEnt, "branches")
	if sup, _ := out["branches_supported"].(bool); !sup {
		t.Fatalf("branches_supported should be true for csharp; got %v", out["branches_supported"])
	}
	branches := branchList(t, out)

	// env-gate: Environment.GetEnvironmentVariable("SIGNUP_ENABLED") → 503
	env := findBranch(branches, "SIGNUP_ENABLED")
	if env == nil {
		t.Fatalf("missing the Environment.GetEnvironmentVariable env-gate: %v", branches)
	}
	if env["kind"] != "env_gate" {
		t.Errorf("env-gate kind=%v; want env_gate", env["kind"])
	}
	if env["env_var"] != "SIGNUP_ENABLED" {
		t.Errorf("env_var=%v; want SIGNUP_ENABLED", env["env_var"])
	}
	if env["outcome"] != "return_value" {
		t.Errorf("env-gate outcome=%v; want return_value", env["outcome"])
	}
	assertStatus(t, env, "503")

	// 400 BadRequest() validation guard
	g400 := findBranch(branches, "dto.Email == null")
	if g400 == nil {
		t.Fatalf("missing the email-required 400 guard: %v", branches)
	}
	if g400["outcome"] != "return_value" {
		t.Errorf("400 guard outcome=%v; want return_value", g400["outcome"])
	}
	assertStatus(t, g400, "400")

	// 409 Conflict() guard inside the try
	g409 := findBranch(branches, "ExistsByEmail")
	if g409 == nil {
		t.Fatalf("missing the 409 existing-email guard: %v", branches)
	}
	assertStatus(t, g409, "409")

	// catch returns StatusCode(500, ...) → return_value, status 500
	cat := findBranch(branches, "catch")
	if cat == nil {
		t.Fatalf("missing the catch handler: %v", branches)
	}
	if cat["kind"] != "except" {
		t.Errorf("catch kind=%v; want except", cat["kind"])
	}
	if cat["outcome"] != "return_value" {
		t.Errorf("catch returns StatusCode(500) → outcome should be return_value; got %v", cat["outcome"])
	}
	assertStatus(t, cat, "500")
}
