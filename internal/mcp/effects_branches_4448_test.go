package mcp

// effects_branches_4448_test.go — in-pipeline test for the #4448 `branches`
// facet on PHP (extends the Python #4423/#4435 flagship and the JS/TS+Java+Go
// #4434 generalization).
//
// Per the standing LIVE-VALIDATE rule: this exercises the REAL effects MCP
// handler (handleEffects) end-to-end — resolver, on-disk source read,
// per-language PHP branch analyzer — over a representative branchy PHP
// controller action copied into testdata/branches_4448/:
//
//   - UserController.php — a Laravel/Symfony controller with an env-gate
//     (env('SIGNUP_ENABLED') → 503), an early-return 400 guard, a 409 conflict
//     guard inside the try, and a try/catch that re-throws HttpException(500).
//
// It asserts:
//   - RED-before / GREEN-after: the DEFAULT call (no include) carries NO
//     `branches` key (default output unchanged — no regression), while
//     include="branches" enumerates each branch with the right kind/outcome;
//   - the env-gate's env_var (SIGNUP_ENABLED) is surfaced as kind env_gate;
//   - HTTP statuses (503/400/409/500) are derived into returns.status;
//   - the catch re-throw is classified except/raise.

import (
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

const phpEnt = "php-svc::op_store_user_php"

// branches4448Server builds a server whose single repo's Path points at the
// branches_4448 testdata dir, with one Operation entity spanning the PHP method.
func branches4448Server(t *testing.T) *Server {
	t.Helper()
	doc := &graph.Document{
		Repo: "php-svc",
		Entities: []graph.Entity{
			{
				ID: "op_store_user_php", Name: "UserController.store",
				Kind: "SCOPE.Operation", QualifiedName: "App.Http.Controllers.UserController.store",
				SourceFile: "UserController.php", StartLine: 16, EndLine: 38,
			},
		},
	}
	srv := newTestServer(t, doc)
	abs, err := filepath.Abs(filepath.Join("testdata", "branches_4448"))
	if err != nil {
		t.Fatalf("abs testdata: %v", err)
	}
	srv.State.mu.Lock()
	srv.State.groups["test"].Repos["php-svc"].Path = abs
	srv.State.mu.Unlock()
	return srv
}

// TestEffectsBranches4448_DefaultUnchanged is the RED-before assertion: a
// default effects call (no include) must NOT carry a `branches` key.
func TestEffectsBranches4448_DefaultUnchanged(t *testing.T) {
	srv := branches4448Server(t)
	out := callEffects(t, srv, phpEnt, "")
	if _, present := out["branches"]; present {
		t.Fatalf("%s: default effects output must NOT contain `branches`", phpEnt)
	}
	if _, present := out["branches_supported"]; present {
		t.Fatalf("%s: default output must not advertise branches_supported", phpEnt)
	}
	if out["entity_id"] == nil {
		t.Fatalf("%s: default output lost entity_id", phpEnt)
	}
}

// TestEffectsBranches4448_PHP is the GREEN-after assertion for PHP.
func TestEffectsBranches4448_PHP(t *testing.T) {
	srv := branches4448Server(t)
	out := callEffects(t, srv, phpEnt, "branches")
	if sup, _ := out["branches_supported"].(bool); !sup {
		t.Fatalf("branches_supported should be true for php; got %v", out["branches_supported"])
	}
	branches := branchList(t, out)

	// env-gate: env('SIGNUP_ENABLED') → 503
	env := findBranch(branches, "SIGNUP_ENABLED")
	if env == nil {
		t.Fatalf("missing the env('SIGNUP_ENABLED') env-gate: %v", branches)
	}
	if env["kind"] != "env_gate" || env["env_var"] != "SIGNUP_ENABLED" {
		t.Errorf("env-gate=%v; want env_gate/SIGNUP_ENABLED", env)
	}
	assertStatus(t, env, "503")

	// 400 email-required guard
	g400 := findBranch(branches, "email') === null")
	if g400 == nil {
		t.Fatalf("missing the email-required 400 guard: %v", branches)
	}
	if g400["outcome"] != "return_value" {
		t.Errorf("400 guard outcome=%v; want return_value", g400["outcome"])
	}
	assertStatus(t, g400, "400")

	// 409 conflict guard inside the try
	g409 := findBranch(branches, "exists()")
	if g409 == nil {
		t.Fatalf("missing the 409 conflict guard: %v", branches)
	}
	assertStatus(t, g409, "409")

	// catch re-throws HttpException(500) → raise, status 500
	cat := findBranch(branches, "catch")
	if cat == nil {
		t.Fatalf("missing the catch handler: %v", branches)
	}
	if cat["kind"] != "except" {
		t.Errorf("catch kind=%v; want except", cat["kind"])
	}
	if cat["outcome"] != "raise" {
		t.Errorf("catch re-throws → outcome should be raise; got %v", cat["outcome"])
	}
	assertStatus(t, cat, "500")
}
