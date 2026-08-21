package resolve

import "testing"

// #6337 round 2 — VBNetExternalHierarchyTarget must return a NORMALISED type
// spelling, and must refuse to classify a malformed one.
//
// Both properties are about the node id the caller mints from the return value,
// so both are asserted on the returned string, never on ok alone. Asserting ok
// would be vacuous for the generics cases: they already answered ok=true before
// the fix, while handing back `List(Of Machine)` for the caller to turn into a
// per-instantiation node.
func TestVBNetExternalHierarchyTargetNormalises_6337(t *testing.T) {
	cases := []struct {
		in       string
		wantType string
		wantMemb string
		wantOK   bool
	}{
		// Generic instantiations collapse onto the open type, so every
		// instantiation shares one node. This is the grouping the arm exists
		// for; returning the raw spelling defeats it.
		{"List(Of Machine)", "List", "", true},
		{"List(Of Profile)", "List", "", true},
		{"IComparable(Of Profile)", "IComparable", "", true},
		{"System.Collections.Generic.List(Of Machine)", "System.Collections.Generic.List", "", true},
		{" List(Of Machine) ", "List", "", true},

		// Unchanged shapes, restated so a mutant that normalises too hard is
		// still caught.
		{"Form", "Form", "", true},
		{"System.Windows.Forms.Form", "System.Windows.Forms.Form", "", true},
		{"IDisposable.Dispose", "IDisposable", "Dispose", true},
		// The member split normalises its head too, so a generic interface
		// implemented member-wise still shares the open type's node.
		{"IComparable(Of Profile).CompareTo", "IComparable", "CompareTo", true},
		{"IFrameServer.GetFrame", "", "", false},
		{"Fomr", "", "", false},

		// Malformed spellings. The dotted rule keys on the ROOT segment only,
		// so every one of these classified before the well-formedness check —
		// turning a misparse into a tidy resolved node and taking it out of
		// the bug-edge count.
		{"System.", "", "", false},
		{"System..Forms.Form", "", "", false},
		{".System.Form", "", "", false},
		{"System.Forms.Form>", "", "", false},
		{"System.9Forms.Form", "", "", false},
		{"System.Forms Form", "", "", false},
		{"", "", "", false},
		{"   ", "", "", false},
		{"(Of T)", "", "", false},
		{"IDisposable.Dis pose", "", "", false},

		// Both directions: a well-formed name with digits and underscores,
		// which VB identifiers permit, must still classify.
		{"Microsoft.Win32.SafeHandles.SafeHandleZeroOrMinusOneIsInvalid", "Microsoft.Win32.SafeHandles.SafeHandleZeroOrMinusOneIsInvalid", "", true},
		{"System.Data.SqlClient.Sql_Command2", "System.Data.SqlClient.Sql_Command2", "", true},
	}
	for _, tc := range cases {
		gotType, gotMemb, gotOK := VBNetExternalHierarchyTarget(tc.in)
		if gotType != tc.wantType || gotMemb != tc.wantMemb || gotOK != tc.wantOK {
			t.Errorf("VBNetExternalHierarchyTarget(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.in, gotType, gotMemb, gotOK, tc.wantType, tc.wantMemb, tc.wantOK)
		}
	}
}

// TestIsWellFormedVBTypeName_6337 pins the predicate directly, in both
// directions, so the rejection cases above cannot be satisfied by an
// always-false mutant.
func TestIsWellFormedVBTypeName_6337(t *testing.T) {
	for _, s := range []string{"Form", "_Form", "F1", "System.Windows.Forms.Form", "a.b.c.d.e"} {
		if !isWellFormedVBTypeName(s) {
			t.Errorf("isWellFormedVBTypeName(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", ".", "System.", ".System", "System..Forms", "1Form", "Form-", "Form Name", "System.Forms."} {
		if isWellFormedVBTypeName(s) {
			t.Errorf("isWellFormedVBTypeName(%q) = true, want false", s)
		}
	}
}
