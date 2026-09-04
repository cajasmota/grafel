package graph

import "testing"

// TestDropHierarchySelfLoops_DropsOnlyHierarchySelfLoops grades FOUR axes of
// DropHierarchySelfLoops. Two of them the golden fixture also grades; two of
// them this test is the SOLE grader of. Both facts were measured, not assumed
// — each mutant below was applied and the full 27-fixture gate re-run.
//
//	axis                                        graded by fixture?  sole grader here?
//	kind set is narrow (CALLS survives)          no  (M6 gate-green)      YES
//	self-loop predicate required                 yes (M5 → gate red)      no
//	kind comparison is case-insensitive          no  (M4/M4b gate-green)  YES
//	empty endpoints are not a self-loop          no  (M9 gate-green)      YES
//
// The empty-endpoint axis is here because the original rationale for that
// guard — "every hierarchy extractor emits FromID: \"\", so the guard protects
// them" — was checked and is FALSE: erlang emits ToID: d.name and graphql
// emits ToID: target, neither of which can produce "" == "". Removing the
// guard leaves all 27 fixtures green and erlang-otp-mini / graphql-schema-mini
// byte-identical. The guard is kept because it is correct — two unresolved
// stubs are two things we failed to bind, not one entity — but nothing except
// this test observes it.
//
// The DEPENDS_ON and ROUTES_TO rows pin a DELIBERATE non-behaviour: #6809's
// rules emit literal self-loops under those kinds, and this filter must not
// grow to cover them (see the kind-set comment in the file under test).
// Without those rows, widening the kind set is invisible — measured: adding
// DEPENDS_ON left all 27 fixtures AND this test green.
func TestDropHierarchySelfLoops_DropsOnlyHierarchySelfLoops(t *testing.T) {
	cases := []struct {
		name string
		rel  Relationship
		drop bool
	}{
		{"implements self-loop", Relationship{FromID: "aaaa", ToID: "aaaa", Kind: "IMPLEMENTS"}, true},
		{"extends self-loop", Relationship{FromID: "aaaa", ToID: "aaaa", Kind: "EXTENDS"}, true},
		{"inherits self-loop", Relationship{FromID: "aaaa", ToID: "aaaa", Kind: "INHERITS"}, true},
		// Case axis — sole grader. A producer spelling the kind in lower case
		// states the same impossible fact.
		{"lower-case implements self-loop", Relationship{FromID: "aaaa", ToID: "aaaa", Kind: "implements"}, true},
		{"mixed-case extends self-loop", Relationship{FromID: "aaaa", ToID: "aaaa", Kind: "Extends"}, true},
		// Kind axis — a self-loop CALLS edge is direct recursion, a real fact.
		{"calls self-loop is recursion", Relationship{FromID: "aaaa", ToID: "aaaa", Kind: "CALLS"}, false},
		{"contains self-loop", Relationship{FromID: "aaaa", ToID: "aaaa", Kind: "CONTAINS"}, false},
		// #6809 kinds — these self-loops are real and this filter must NOT
		// grow to swallow them. Widening the kind set fails here and nowhere
		// else.
		{"depends_on self-loop is #6809's, not ours", Relationship{FromID: "aaaa", ToID: "aaaa", Kind: "DEPENDS_ON"}, false},
		{"routes_to self-loop is #6809's, not ours", Relationship{FromID: "aaaa", ToID: "aaaa", Kind: "ROUTES_TO"}, false},
		// Self-loop axis — the expect/actual pairing this must not touch.
		{"implements between distinct ids", Relationship{FromID: "aaaa", ToID: "bbbb", Kind: "IMPLEMENTS"}, false},
		// An unresolved endpoint pair is not evidence of a self-loop; two
		// empty stubs are two things we failed to bind, not one entity.
		{"both endpoints empty", Relationship{FromID: "", ToID: "", Kind: "IMPLEMENTS"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := []Relationship{tc.rel}
			out, n := DropHierarchySelfLoops(in)
			wantDropped := 0
			if tc.drop {
				wantDropped = 1
			}
			if n != wantDropped {
				t.Fatalf("dropped count = %d, want %d (kind=%q from=%q to=%q)",
					n, wantDropped, tc.rel.Kind, tc.rel.FromID, tc.rel.ToID)
			}
			if len(out) != 1-wantDropped {
				t.Fatalf("survivors = %d, want %d", len(out), 1-wantDropped)
			}
		})
	}
}

// TestDropHierarchySelfLoops_PreservesOrderAndCountsEveryDrop pins that the
// filter is not a "drop the first one and stop" shape and that the returned
// count is the number of rows actually removed, not a boolean-ish 1.
func TestDropHierarchySelfLoops_PreservesOrderAndCountsEveryDrop(t *testing.T) {
	in := []Relationship{
		{FromID: "a", ToID: "b", Kind: "IMPLEMENTS"},
		{FromID: "c", ToID: "c", Kind: "IMPLEMENTS"},
		{FromID: "d", ToID: "e", Kind: "EXTENDS"},
		{FromID: "f", ToID: "f", Kind: "EXTENDS"},
		{FromID: "g", ToID: "h", Kind: "CALLS"},
	}
	out, n := DropHierarchySelfLoops(in)
	if n != 2 {
		t.Fatalf("dropped = %d, want 2", n)
	}
	want := []string{"a", "d", "g"}
	if len(out) != len(want) {
		t.Fatalf("survivors = %d, want %d", len(out), len(want))
	}
	for i, w := range want {
		if out[i].FromID != w {
			t.Fatalf("survivor[%d].FromID = %q, want %q (order not preserved)", i, out[i].FromID, w)
		}
	}
}
