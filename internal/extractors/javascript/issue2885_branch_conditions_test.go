// Package javascript_test — issue #2885 NativeScript branch_conditions.
//
// The discriminator pass (#2654) only stamps bare-identifier ===/==/!== literal
// comparisons. Real NativeScript-Core view-models branch on MEMBER comparisons
// with the full set of relational operators (this._x !== value, this._counter
// <= 0), plus ternary/switch — none of which are discriminator shaped. The
// #2885 general branch-condition pass captures them as
// Properties["branch_conditions"] and BRANCHES_ON edges, which is the proving
// artifact for flipping Data Flow/branch_conditions back to full.
package javascript_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// branchConditions returns the stamped branch_conditions property for entity
// name, or "" if absent.
func branchConditions(ents []types.EntityRecord, name string) string {
	for i := range ents {
		if ents[i].Name == name {
			return ents[i].Properties["branch_conditions"]
		}
	}
	return ""
}

// branchesOnEdges returns the BRANCHES_ON relationship records for entity name.
func branchesOnEdges(ents []types.EntityRecord, name string) []types.RelationshipRecord {
	for i := range ents {
		if ents[i].Name == name {
			var out []types.RelationshipRecord
			for _, r := range ents[i].Relationships {
				if r.Kind == string(types.RelationshipKindBranchesOn) {
					out = append(out, r)
				}
			}
			return out
		}
	}
	return nil
}

func TestMobile_NativeScript_MemberBranchConditions(t *testing.T) {
	ents := extractFixture(t, "mobile_nativescript/counter-view-model.ts")

	// The `set counter` accessor guards with a member !== comparison that the
	// discriminator pass cannot see (LHS is a member_expression, not a bare id).
	if got := branchConditions(ents, "counter"); !contains(got, "this._counter!==value") {
		t.Errorf("counter setter: branch_conditions=%q, want to contain %q", got, "this._counter!==value")
	}

	// `decrement` branches on a relational `<=` member comparison — `<=` is
	// outside the discriminator operator set entirely.
	if got := branchConditions(ents, "decrement"); !contains(got, "this._counter<=0") {
		t.Errorf("decrement: branch_conditions=%q, want to contain %q", got, "this._counter<=0")
	}

	// `classify` mixes a ternary on a member, a switch on a member, and a `>`
	// member comparison inside the default case.
	cls := branchConditions(ents, "classify")
	for _, want := range []string{"this._busy", "this._status", "this._counter>10"} {
		if !contains(cls, want) {
			t.Errorf("classify: branch_conditions=%q, want to contain %q", cls, want)
		}
	}

	// BRANCHES_ON edges carry operator/kind/line for the captured branches.
	edges := branchesOnEdges(ents, "decrement")
	if len(edges) == 0 {
		t.Fatal("decrement: no BRANCHES_ON edges emitted")
	}
	found := false
	for _, e := range edges {
		if e.ToID == "branch:this._counter<=0" {
			found = true
			if e.Properties.Get("operator") != "<=" {
				t.Errorf("decrement branch: operator=%q, want %q", e.Properties.Get("operator"), "<=")
			}
			if e.Properties.Get("kind") != "if" {
				t.Errorf("decrement branch: kind=%q, want %q", e.Properties.Get("kind"), "if")
			}
			if e.Properties.Get("line") == "" {
				t.Errorf("decrement branch: missing line property")
			}
		}
	}
	if !found {
		t.Errorf("decrement: missing BRANCHES_ON edge to branch:this._counter<=0; edges=%v", edges)
	}

	// Regression: the discriminator pass must NOT have caught these member
	// comparisons (proves the gap the #2885 pass closes is real).
	if d := discriminatorsProp(ents, "decrement"); d != "" {
		t.Errorf("decrement: discriminator pass unexpectedly stamped %q (member compare should be invisible to it)", d)
	}
}

// discriminatorsProp returns the discriminators property for entity name.
func discriminatorsProp(ents []types.EntityRecord, name string) string {
	for i := range ents {
		if ents[i].Name == name {
			return ents[i].Properties["discriminators"]
		}
	}
	return ""
}
