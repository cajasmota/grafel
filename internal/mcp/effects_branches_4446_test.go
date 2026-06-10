package mcp

// effects_branches_4446_test.go — in-pipeline test for the #4446 `branches`
// facet on Kotlin (extends the Python #4423/#4435 flagship and the JS/TS+Java+Go
// #4434 brace-language analyzers, epic #4419 capability 4).
//
// Per the standing LIVE-VALIDATE rule: this exercises the REAL effects MCP
// handler (handleEffects) end-to-end — resolver, on-disk source read, the
// per-language Kotlin branch analyzer — over a representative branchy Kotlin
// function copied into testdata/branches_4446/:
//
//   - UserController.kt — Spring controller (Kotlin) with an env-gate
//     (System.getenv), an early-return guard returning
//     ResponseEntity.status(HttpStatus.BAD_REQUEST), a 409 guard inside a try,
//     and a try/catch that logs then returns a 500 ResponseEntity.
//
// It asserts:
//   - RED-before / GREEN-after: the DEFAULT call (no include) carries NO
//     `branches` key (default output unchanged — no regression), while
//     include="branches" enumerates each branch with the right kind/outcome;
//   - the env-gate's env_var is surfaced (SIGNUP_ENABLED) with status 503;
//   - HTTP statuses (503/400/409/500) are derived into returns.status, including
//     HttpStatus.NAME enum → code mapping (BAD_REQUEST → 400, CONFLICT → 409);
//   - catch returns a 500 ResponseEntity → return_value.

import (
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

const kotlinEnt = "polyglot-kt::op_create_user_kt"

// branches4446Server builds a server whose single repo's Path points at the
// branches_4446 testdata dir, with one Operation entity for the Kotlin fixture
// spanning the create() method.
func branches4446Server(t *testing.T) *Server {
	t.Helper()
	doc := &graph.Document{
		Repo: "polyglot-kt",
		Entities: []graph.Entity{
			{
				ID: "op_create_user_kt", Name: "UserController.create",
				Kind: "SCOPE.Operation", QualifiedName: "com.example.api.UserController.create",
				SourceFile: "UserController.kt", StartLine: 24, EndLine: 45,
			},
		},
	}
	srv := newTestServer(t, doc)
	abs, err := filepath.Abs(filepath.Join("testdata", "branches_4446"))
	if err != nil {
		t.Fatalf("abs testdata: %v", err)
	}
	srv.State.mu.Lock()
	srv.State.groups["test"].Repos["polyglot-kt"].Path = abs
	srv.State.mu.Unlock()
	return srv
}

// TestEffectsBranches4446_DefaultUnchanged is the RED-before assertion: a
// default effects call (no include) must NOT carry a `branches` key. Guards the
// no-regression contract.
func TestEffectsBranches4446_DefaultUnchanged(t *testing.T) {
	srv := branches4446Server(t)
	out := callEffects(t, srv, kotlinEnt, "")
	if _, present := out["branches"]; present {
		t.Fatalf("%s: default effects output must NOT contain `branches`", kotlinEnt)
	}
	if _, present := out["branches_supported"]; present {
		t.Fatalf("%s: default output must not advertise branches_supported", kotlinEnt)
	}
	if out["entity_id"] == nil {
		t.Fatalf("%s: default output lost entity_id", kotlinEnt)
	}
}

// TestEffectsBranches4446_Kotlin is the GREEN-after assertion for Kotlin.
func TestEffectsBranches4446_Kotlin(t *testing.T) {
	srv := branches4446Server(t)
	out := callEffects(t, srv, kotlinEnt, "branches")
	if sup, _ := out["branches_supported"].(bool); !sup {
		t.Fatalf("branches_supported should be true for kotlin; got %v", out["branches_supported"])
	}
	branches := branchList(t, out)

	// env-gate: System.getenv("SIGNUP_ENABLED") → 503
	env := findBranch(branches, "SIGNUP_ENABLED")
	if env == nil {
		t.Fatalf("missing the System.getenv env-gate: %v", branches)
	}
	if env["kind"] != "env_gate" || env["env_var"] != "SIGNUP_ENABLED" {
		t.Errorf("env-gate=%v; want env_gate/SIGNUP_ENABLED", env)
	}
	assertStatus(t, env, "503")

	// BAD_REQUEST early-return guard → 400 via HttpStatus enum mapping
	g400 := findBranch(branches, "dto.email == null")
	if g400 == nil {
		t.Fatalf("missing the BAD_REQUEST guard: %v", branches)
	}
	if g400["outcome"] != "return_value" {
		t.Errorf("400 guard outcome=%v; want return_value", g400["outcome"])
	}
	assertStatus(t, g400, "400")

	// CONFLICT guard inside try → 409
	g409 := findBranch(branches, "existsByEmail")
	if g409 == nil {
		t.Fatalf("missing the CONFLICT guard: %v", branches)
	}
	assertStatus(t, g409, "409")

	// catch logs then returns a 500 ResponseEntity → return_value, status 500
	cat := findBranch(branches, "catch")
	if cat == nil {
		t.Fatalf("missing the catch handler: %v", branches)
	}
	if cat["kind"] != "except" {
		t.Errorf("catch kind=%v; want except", cat["kind"])
	}
	if cat["outcome"] != "return_value" {
		t.Errorf("catch returns 500 ResponseEntity → outcome should be return_value; got %v", cat["outcome"])
	}
	assertStatus(t, cat, "500")
}
