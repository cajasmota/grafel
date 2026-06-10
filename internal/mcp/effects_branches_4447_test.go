package mcp

// effects_branches_4447_test.go — in-pipeline test for the #4447 `branches`
// facet on Rust (extends the Python #4423/#4435 flagship and the JS/TS+Java+Go
// #4434 brace-language analyzers).
//
// Per the standing LIVE-VALIDATE rule: this exercises the REAL effects MCP
// handler (handleEffects) end-to-end — resolver, on-disk source read, the Rust
// branch analyzer — over a representative branchy Rust function copied into
// testdata/branches_4447/user_handler.rs: an axum-style handler with a
// std::env::var env-gate, early-return guards returning Err(StatusCode::...),
// the `?` try operator, a panic! guard, and a `match` Err(e) => arm.
//
// It asserts:
//   - RED-before / GREEN-after: the DEFAULT call (no include) carries NO
//     `branches` key (default output unchanged — no regression), while
//     include="branches" enumerates each branch with the right kind/outcome;
//   - the env-gate's env_var is surfaced (SIGNUP_ENABLED), kind=env_gate;
//   - return Err(StatusCode::...) → raise with the mapped status (503/400/409);
//   - the `?` operator → raise (error propagation);
//   - panic! → raise;
//   - the `match` Err arm → except/raise, status 500.

import (
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

const rustEnt = "rust-svc::op_create_user_rs"

// branches4447Server builds a server whose single repo's Path points at the
// branches_4447 testdata dir, with one Operation entity spanning the Rust
// handler.
func branches4447Server(t *testing.T) *Server {
	t.Helper()
	doc := &graph.Document{
		Repo: "rust-svc",
		Entities: []graph.Entity{
			{
				ID: "op_create_user_rs", Name: "create_user",
				Kind: "SCOPE.Function", QualifiedName: "api::create_user",
				SourceFile: "user_handler.rs", StartLine: 11, EndLine: 35,
			},
		},
	}
	srv := newTestServer(t, doc)
	abs, err := filepath.Abs(filepath.Join("testdata", "branches_4447"))
	if err != nil {
		t.Fatalf("abs testdata: %v", err)
	}
	srv.State.mu.Lock()
	srv.State.groups["test"].Repos["rust-svc"].Path = abs
	srv.State.mu.Unlock()
	return srv
}

// TestEffectsBranches4447_DefaultUnchanged is the RED-before / no-regression
// assertion: a default effects call (no include) must NOT carry a `branches`
// key nor advertise branches_supported.
func TestEffectsBranches4447_DefaultUnchanged(t *testing.T) {
	srv := branches4447Server(t)
	out := callEffects(t, srv, rustEnt, "")
	if _, present := out["branches"]; present {
		t.Fatalf("default effects output must NOT contain `branches`")
	}
	if _, present := out["branches_supported"]; present {
		t.Fatalf("default output must not advertise branches_supported")
	}
	if out["entity_id"] == nil {
		t.Fatalf("default output lost entity_id")
	}
}

// TestEffectsBranches4447_Rust is the GREEN-after assertion for Rust.
func TestEffectsBranches4447_Rust(t *testing.T) {
	srv := branches4447Server(t)
	out := callEffects(t, srv, rustEnt, "branches")
	if sup, _ := out["branches_supported"].(bool); !sup {
		t.Fatalf("branches_supported should be true for rust; got %v", out["branches_supported"])
	}
	branches := branchList(t, out)

	// env-gate: std::env::var("SIGNUP_ENABLED") → return Err(503)
	env := findBranch(branches, "SIGNUP_ENABLED")
	if env == nil {
		t.Fatalf("missing the std::env::var SIGNUP_ENABLED env-gate: %v", branches)
	}
	if env["kind"] != "env_gate" {
		t.Errorf("env-gate kind=%v; want env_gate", env["kind"])
	}
	if env["env_var"] != "SIGNUP_ENABLED" {
		t.Errorf("env_var=%v; want SIGNUP_ENABLED", env["env_var"])
	}
	if env["outcome"] != "raise" {
		t.Errorf("env-gate outcome=%v; want raise (return Err)", env["outcome"])
	}
	assertStatus(t, env, "503")

	// 400 email-required guard → return Err(StatusCode::BAD_REQUEST)
	g400 := findBranch(branches, "email.is_empty()")
	if g400 == nil {
		t.Fatalf("missing the email-required 400 guard: %v", branches)
	}
	if g400["outcome"] != "raise" {
		t.Errorf("400 guard outcome=%v; want raise", g400["outcome"])
	}
	assertStatus(t, g400, "400")

	// `?` operator on the find_by_email lookup → raise (propagation)
	q := findBranch(branches, "find_by_email")
	if q == nil {
		t.Fatalf("missing the `?` propagation branch: %v", branches)
	}
	if q["outcome"] != "raise" {
		t.Errorf("`?` branch outcome=%v; want raise", q["outcome"])
	}

	// 409 existing-email guard
	g409 := findBranch(branches, "existing.is_some()")
	if g409 == nil {
		t.Fatalf("missing the 409 existing-email guard: %v", branches)
	}
	assertStatus(t, g409, "409")

	// panic! guard → raise
	pan := findBranch(branches, "is_poisoned()")
	if pan == nil {
		t.Fatalf("missing the panic! guard: %v", branches)
	}
	if pan["outcome"] != "raise" {
		t.Errorf("panic guard outcome=%v; want raise", pan["outcome"])
	}

	// match Err(e) => arm returning Err(500) → except/raise, status 500
	cat := findBranch(branches, "Err(e) =>")
	if cat == nil {
		t.Fatalf("missing the match Err arm: %v", branches)
	}
	if cat["kind"] != "except" {
		t.Errorf("match Err arm kind=%v; want except", cat["kind"])
	}
	if cat["outcome"] != "raise" {
		t.Errorf("match Err arm outcome=%v; want raise (return Err 500)", cat["outcome"])
	}
	assertStatus(t, cat, "500")
}
