package mcp

// effects_branches_4693_ruby_test.go — method-boundary scoping regression for
// the RUBY (`end`-delimited) branch analyzer (#4693, follow-up to #4666, epic
// #4419).
//
// #4666 added substrate.ClampToFunctionBody for python (dedent) and the brace
// languages (matching `}`). Ruby methods are `def`…`end`-delimited, not brace
// or dedent, so they fell through the clamp unchanged: a ruby controller method
// whose entity EndLine is missing/zero gets a padded StartLine+N window that
// over-captures branches into the NEXT `def`. bodyEndRuby now clamps that
// window to the `end` closing the method's own `def`.
//
// This test pins the fix on a verbatim two-method Rails-style controller
// (create_contact immediately followed by update_contact): with create_contact
// given NO EndLine, effects(create_contact, include=branches) must return ONLY
// create_contact's own branches (400/409 guards + 201/500) and NONE of
// update_contact's distinct branches (its 422 email conflict, the 503).
//
// The negative case asserts an endless/one-liner `def` does NOT over-clamp:
// the branchy sibling below it is still seen (over-inclusion beats truncation).

import (
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

func rubyBranchScopeTestServer(t *testing.T) *Server {
	t.Helper()
	doc := &graph.Document{
		Repo: "upvate-core",
		Entities: []graph.Entity{
			{
				ID: "op_create_contact_rb", Name: "ContractsController.create_contact",
				Kind:          "SCOPE.Operation",
				QualifiedName: "contracts_controller.ContractsController.create_contact",
				// StartLine is the `def create_contact` header (fixture line 2).
				// EndLine omitted (0) → degenerate span → padded window → would
				// overshoot into update_contact (fixture line 24+) pre-fix.
				SourceFile: "contracts_controller_two_methods.rb", StartLine: 2, EndLine: 0,
			},
			{
				ID: "op_ping_rb", Name: "StatusController.ping",
				Kind:          "SCOPE.Operation",
				QualifiedName: "status_controller.StatusController.ping",
				// Endless one-liner `def ping = head(:ok)` (fixture line 5).
				// EndLine omitted → padded window runs to EOF. bodyEndRuby must
				// NOT clamp at the header (no separate `end` body) → the branchy
				// sibling health_check below stays visible (over-inclusion).
				SourceFile: "oneliner_methods.rb", StartLine: 5, EndLine: 0,
			},
		},
	}
	srv := newTestServer(t, doc)
	abs, err := filepath.Abs(filepath.Join("testdata", "branches_4693_ruby"))
	if err != nil {
		t.Fatalf("abs testdata: %v", err)
	}
	srv.State.mu.Lock()
	srv.State.groups["test"].Repos["upvate-core"].Path = abs
	srv.State.mu.Unlock()
	return srv
}

func TestEffectsBranches_RubyMethodScope(t *testing.T) {
	srv := rubyBranchScopeTestServer(t)
	out := callEffects(t, srv, "ContractsController.create_contact", "branches")

	if sup, _ := out["branches_supported"].(bool); !sup {
		t.Fatalf("branches_supported should be true for ruby; got %v", out["branches_supported"])
	}
	branches := branchList(t, out)
	if len(branches) == 0 {
		t.Fatalf("expected create_contact's own branches, got none")
	}

	// --- create_contact's OWN branches are present ---
	if b := findBranch(branches, "contract.client.nil?"); b == nil {
		t.Fatalf("missing create_contact's 400 guard: %v", branches)
	} else {
		assertStatus(t, b, "400")
	}
	if b := findBranch(branches, "existing.present?"); b == nil {
		t.Fatalf("missing create_contact's 409 guard: %v", branches)
	}

	// --- update_contact's DISTINCT branches must NOT appear (the over-capture) ---
	for _, b := range branches {
		cond, _ := b["condition"].(string)
		if cond == "if conflict.present? && conflict.id != contact.id" {
			t.Errorf("over-capture: create_contact branch list bled into update_contact's 422 conflict guard (%q)", cond)
		}
		if st := branchStatus(b); st == "422" || st == "503" {
			t.Errorf("over-capture: create_contact branch carries update_contact-only status %s (cond=%v)", st, cond)
		}
	}

	// No branch may be reported at/after update_contact's region (fixture line 24).
	for _, b := range branches {
		if line := toInt(b["line"]); line >= 24 {
			t.Errorf("over-capture: branch at line %d is inside/after the sibling update_contact method (cond=%v)", line, b["condition"])
		}
	}
}

// TestEffectsBranches_RubyOneLinerNoOverClamp is the negative case: an
// endless/one-liner `def ping = head(:ok)` must NOT cause bodyEndRuby to clamp
// at the header line. health_check (the branchy sibling below it) keeps its own
// 503 guard — over-inclusion is the conservative direction, never truncation.
func TestEffectsBranches_RubyOneLinerNoOverClamp(t *testing.T) {
	srv := rubyBranchScopeTestServer(t)
	out := callEffects(t, srv, "StatusController.ping", "branches")

	branches := branchList(t, out)
	if b := findBranch(branches, "Maintenance.active?"); b == nil {
		t.Fatalf("over-clamp regression: endless `def ping` clamped at its header and dropped the branchy sibling health_check: %v", branches)
	}
}
