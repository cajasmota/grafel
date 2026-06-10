package mcp

// effects_branches_4449_test.go — in-pipeline test for the #4449 `branches`
// facet on Scala (extends the Python #4423/#4435 flagship and the #4434
// JS/TS/Java/Go brace-language analyzers).
//
// Per the standing LIVE-VALIDATE rule: this exercises the REAL effects MCP
// handler (handleEffects) end-to-end — resolver, on-disk source read, the Scala
// branch analyzer — over a representative branchy Scala method copied into
// testdata/branches_4449/UserService.scala:
//
//   - an env-gate (sys.env.get("SIGNUP_ENABLED")) returning a Left carrying a
//     ServiceUnavailable (503) named status;
//   - an early-return guard yielding Left(BadRequest(...)) → 400;
//   - a 409 Conflict guard (Either Left) inside the try;
//   - a try/catch that logs then re-throws a ServiceException → raise.
//
// It asserts:
//   - RED-before / GREEN-after: the DEFAULT call (no include) carries NO
//     `branches` key (default output unchanged — no regression), while
//     include="branches" enumerates each branch with the right kind/outcome;
//   - the env-gate's env_var is surfaced (SIGNUP_ENABLED);
//   - named Scala statuses (503/400/409) are derived into returns.status;
//   - the re-throwing catch classifies as except/raise.

import (
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

const scalaEnt = "scala-svc::op_create_user_scala"

// branches4449Server builds a server whose single repo's Path points at the
// branches_4449 testdata dir, with one Operation entity spanning the Scala
// createUser method.
func branches4449Server(t *testing.T) *Server {
	t.Helper()
	doc := &graph.Document{
		Repo: "scala-svc",
		Entities: []graph.Entity{
			{
				ID: "op_create_user_scala", Name: "UserService.createUser",
				Kind: "SCOPE.Operation", QualifiedName: "com.example.api.UserService.createUser",
				SourceFile: "UserService.scala", StartLine: 14, EndLine: 36,
			},
		},
	}
	srv := newTestServer(t, doc)
	abs, err := filepath.Abs(filepath.Join("testdata", "branches_4449"))
	if err != nil {
		t.Fatalf("abs testdata: %v", err)
	}
	srv.State.mu.Lock()
	srv.State.groups["test"].Repos["scala-svc"].Path = abs
	srv.State.mu.Unlock()
	return srv
}

// TestEffectsBranches4449_DefaultUnchanged is the RED-before assertion: a
// default effects call (no include) must NOT carry a `branches` key. Guards the
// no-regression contract for Scala.
func TestEffectsBranches4449_DefaultUnchanged(t *testing.T) {
	srv := branches4449Server(t)
	out := callEffects(t, srv, scalaEnt, "")
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

// TestEffectsBranches4449_Scala is the GREEN-after assertion for Scala.
func TestEffectsBranches4449_Scala(t *testing.T) {
	srv := branches4449Server(t)
	out := callEffects(t, srv, scalaEnt, "branches")
	if sup, _ := out["branches_supported"].(bool); !sup {
		t.Fatalf("branches_supported should be true for scala; got %v", out["branches_supported"])
	}
	branches := branchList(t, out)

	// env-gate: sys.env.get("SIGNUP_ENABLED") → Left(ServiceUnavailable) 503
	env := findBranch(branches, "SIGNUP_ENABLED")
	if env == nil {
		t.Fatalf("missing the sys.env.get env-gate: %v", branches)
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

	// 400 email-required guard (Left(BadRequest(...)))
	g400 := findBranch(branches, "req.email == null")
	if g400 == nil {
		t.Fatalf("missing the email-required 400 guard: %v", branches)
	}
	if g400["outcome"] != "return_value" {
		t.Errorf("400 guard outcome=%v; want return_value", g400["outcome"])
	}
	assertStatus(t, g400, "400")

	// 409 existing-email guard inside the try (Left(Conflict(...)))
	g409 := findBranch(branches, "existing.isDefined")
	if g409 == nil {
		t.Fatalf("missing the 409 existing-email guard: %v", branches)
	}
	assertStatus(t, g409, "409")

	// catch re-throws ServiceException → raise
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
}
