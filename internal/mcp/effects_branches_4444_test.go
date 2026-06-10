package mcp

// effects_branches_4444_test.go — in-pipeline test for the #4444 `branches`
// facet on Ruby (generalizes the Python #4423/#4435 flagship and the #4434
// JS/TS/Java/Go brace-language analyzers to Ruby's end-keyword block scope).
//
// Per the standing LIVE-VALIDATE rule: this exercises the REAL effects MCP
// handler (handleEffects) end-to-end — resolver, on-disk source read,
// per-language branch analyzer — over a representative branchy Rails action
// copied into testdata/branches_4444/users_controller.rb:
//
//   - an env-gate (`head :service_unavailable unless ENV['SIGNUP_ENABLED']`)
//     → env_gate / return_value / 503, env_var SIGNUP_ENABLED;
//   - a block validation guard (`if params[:email].blank? ... render
//     status: :bad_request`) → return_value / 400;
//   - a trailing-modifier guard (`return render(status: :conflict) if
//     User.exists?`) → return_value / 409;
//   - a begin/rescue that re-raises after logging → except / raise.
//
// It asserts RED-before / GREEN-after: the DEFAULT call (no include) carries NO
// `branches` key (default output unchanged — no regression), while
// include="branches" enumerates each branch with the right kind/outcome/
// env_var/status.

import (
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

const rubyEnt = "ruby-svc::op_create_user_rb"

// branches4444Server builds a server whose single repo's Path points at the
// branches_4444 testdata dir, with one Operation entity spanning the Ruby
// `create` action (lines 6..25 of the fixture).
func branches4444Server(t *testing.T) *Server {
	t.Helper()
	doc := &graph.Document{
		Repo: "ruby-svc",
		Entities: []graph.Entity{
			{
				ID: "op_create_user_rb", Name: "UsersController#create",
				Kind: "SCOPE.Operation", QualifiedName: "app.controllers.users_controller.UsersController.create",
				SourceFile: "users_controller.rb", StartLine: 6, EndLine: 25,
			},
		},
	}
	srv := newTestServer(t, doc)
	abs, err := filepath.Abs(filepath.Join("testdata", "branches_4444"))
	if err != nil {
		t.Fatalf("abs testdata: %v", err)
	}
	srv.State.mu.Lock()
	srv.State.groups["test"].Repos["ruby-svc"].Path = abs
	srv.State.mu.Unlock()
	return srv
}

// TestEffectsBranches4444_DefaultUnchanged is the RED-before assertion: a
// default effects call (no include) must NOT carry a `branches` key. Guards the
// no-regression contract for Ruby.
func TestEffectsBranches4444_DefaultUnchanged(t *testing.T) {
	srv := branches4444Server(t)
	out := callEffects(t, srv, rubyEnt, "")
	if _, present := out["branches"]; present {
		t.Fatalf("default effects output must NOT contain `branches`; got: %v", out["branches"])
	}
	if _, present := out["branches_supported"]; present {
		t.Fatalf("default output must not advertise branches_supported")
	}
	if out["entity_id"] == nil {
		t.Fatalf("default output lost entity_id: %v", out)
	}
}

// TestEffectsBranches4444_Ruby is the GREEN-after assertion for Ruby.
func TestEffectsBranches4444_Ruby(t *testing.T) {
	srv := branches4444Server(t)
	out := callEffects(t, srv, rubyEnt, "branches")
	if sup, _ := out["branches_supported"].(bool); !sup {
		t.Fatalf("branches_supported should be true for ruby; got %v", out["branches_supported"])
	}
	branches := branchList(t, out)

	// env-gate: ENV['SIGNUP_ENABLED'] → head :service_unavailable (503).
	env := findBranch(branches, "SIGNUP_ENABLED")
	if env == nil {
		t.Fatalf("missing the ENV['SIGNUP_ENABLED'] env-gate: %v", branches)
	}
	if env["kind"] != "env_gate" {
		t.Errorf("env-gate kind=%v; want env_gate", env["kind"])
	}
	if env["env_var"] != "SIGNUP_ENABLED" {
		t.Errorf("env_var=%v; want SIGNUP_ENABLED", env["env_var"])
	}
	if env["outcome"] != "return_value" {
		t.Errorf("env-gate outcome=%v; want return_value (head :service_unavailable)", env["outcome"])
	}
	assertStatus(t, env, "503")

	// block guard: `if params[:email].blank?` renders 400.
	g400 := findBranch(branches, "params[:email].blank?")
	if g400 == nil {
		t.Fatalf("missing the email-required 400 guard: %v", branches)
	}
	if g400["outcome"] != "return_value" {
		t.Errorf("400 guard outcome=%v; want return_value", g400["outcome"])
	}
	assertStatus(t, g400, "400")

	// trailing-modifier guard: `return render(status: :conflict) if User.exists?`.
	g409 := findBranch(branches, "User.exists?")
	if g409 == nil {
		t.Fatalf("missing the 409 existing-email guard: %v", branches)
	}
	if g409["outcome"] != "return_value" {
		t.Errorf("409 guard outcome=%v; want return_value", g409["outcome"])
	}
	assertStatus(t, g409, "409")

	// begin/rescue re-raises after logging → except / raise.
	resc := findBranch(branches, "rescue")
	if resc == nil {
		t.Fatalf("missing the rescue handler: %v", branches)
	}
	if resc["kind"] != "except" {
		t.Errorf("rescue kind=%v; want except", resc["kind"])
	}
	if resc["outcome"] != "raise" {
		t.Errorf("rescue re-raises → outcome should be raise; got %v", resc["outcome"])
	}
}
