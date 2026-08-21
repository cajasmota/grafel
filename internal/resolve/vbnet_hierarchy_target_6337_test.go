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

// #6337 round 4 — the dotted half's root lookup was case-SENSITIVE against a
// case-INSENSITIVE language, and declining there is not neutral: the spelling
// falls through to the generic dotted-root fallback and comes back as
// `ext:system`, subtype "package", untagged. So the case bug on this half was a
// FABRICATION path, not a recall loss, and it survived the round-2
// malformed-spelling guard because `system.Windows.Forms.Form` is perfectly
// well-formed.
//
// The test pins the returned SPELLING, not ok alone: the root is rewritten to
// the table's casing so every case variant shares one node, and an
// ok-only assertion would let a mutant hand back the raw root and mint a node
// per casing.
func TestVBNetDottedRootFoldsCase_6337(t *testing.T) {
	cases := []struct {
		in       string
		wantType string
		wantOK   bool
	}{
		{"system.Windows.Forms.Form", "System.Windows.Forms.Form", true},
		{"SYSTEM.Windows.Forms.Form", "System.Windows.Forms.Form", true},
		{"SyStEm.Windows.Forms.Form", "System.Windows.Forms.Form", true},
		{"microsoft.Win32.Registry", "Microsoft.Win32.Registry", true},
		{"windows.Forms.TextBox", "Windows.Forms.TextBox", true},
		// Unchanged: the canonical casing must still classify unchanged, so a
		// mutant that lowercases the whole spelling is caught too.
		{"System.Windows.Forms.Form", "System.Windows.Forms.Form", true},
		// Still not framework roots in any casing.
		{"my.MyProject.MySettings", "", false},
		{"forms.TextBox", "", false},
	}
	for _, tc := range cases {
		gotType, gotMemb, gotOK := VBNetExternalHierarchyTarget(tc.in)
		if gotType != tc.wantType || gotMemb != "" || gotOK != tc.wantOK {
			t.Errorf("VBNetExternalHierarchyTarget(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.in, gotType, gotMemb, gotOK, tc.wantType, "", tc.wantOK)
		}
	}

	// A truncated clause is a truncated clause in any casing, and the malformed
	// predicate has to agree with the classifier about which roots are the
	// platform's — otherwise `system.` is declined here and resolved as
	// `ext:system` by the fallback.
	for _, s := range []string{"system.", "SYSTEM.", "microsoft..Win32", "windows.Forms.Form>"} {
		if !VBNetHierarchyTargetIsMalformed(s) {
			t.Errorf("VBNetHierarchyTargetIsMalformed(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"system.Windows.Forms.Form", "SYSTEM.Windows.Forms.Form", "Fomr>", "my."} {
		if VBNetHierarchyTargetIsMalformed(s) {
			t.Errorf("VBNetHierarchyTargetIsMalformed(%q) = true, want false", s)
		}
	}
}

// TestVBNetBareLeafStaysCaseSensitive_6337 is the OTHER half of the asymmetry
// introduced above, and it is deliberate rather than an oversight: see
// vbFrameworkRootCanonical in refs.go.
//
// vbExternalBaseTypes holds 58 UNQUALIFIED identifiers and this arm sits above
// the language-agnostic stdlibBareNames stop-list, so folding case here makes
// `list`, `form` and `label` — everyday spellings in the other languages this
// pipeline serves — classify as .NET base types. Declining a lowercase bare
// leaf costs nothing but a visible bug edge, because a bare name has no dotted
// root and no fallback picks it up.
//
// Without this test the asymmetry is unpinned in one direction: a mutant that
// "fixes the other half too" would be caught only indirectly, by the stdlib
// theft test in internal/external.
func TestVBNetBareLeafStaysCaseSensitive_6337(t *testing.T) {
	for _, s := range []string{"panel", "PANEL", "form", "list", "exception", "iDisposable"} {
		if gotType, _, gotOK := VBNetExternalHierarchyTarget(s); gotOK {
			t.Errorf("VBNetExternalHierarchyTarget(%q) = (%q, _, true); the bare-leaf "+
				"half must stay case-sensitive — it is matched against unqualified "+
				"identifiers shared with every other language in the graph", s, gotType)
		}
	}
	for _, s := range []string{"Panel", "Form", "List", "Exception", "IDisposable"} {
		if _, _, gotOK := VBNetExternalHierarchyTarget(s); !gotOK {
			t.Errorf("VBNetExternalHierarchyTarget(%q) = ok false; the canonical "+
				"spelling must still classify", s)
		}
	}
}

// #6337 round 4 — the generic strip validated nothing it stripped. It removed
// everything from the first `(` to a trailing `)` and then checked only the
// stump, so any misparse with a parenthesised tail — a trailing comment, a
// truncated clause, an attribute list — normalised onto an allowlisted name and
// was synthesised as a tidy resolved node, leaving the bug-edge count.
func TestVBNetGenericArgListIsValidated_6337(t *testing.T) {
	cases := []struct {
		in       string
		wantType string
		wantMemb string
		wantOK   bool
	}{
		// The fabrications, all four executed through Synthesize before the fix.
		{"Form (deprecated)", "", "", false},
		{"Form(!!! @@@)", "", "", false},
		{"Form()", "", "", false},
		{"Form(Of )", "", "", false},
		{"Form(Of ,)", "", "", false},
		{"System.Windows.Forms.Form(???)", "", "", false},
		{"System.Windows.Forms.Form(REM stale)", "", "", false},
		// `Off` is not `Of` + an argument named `f`.
		{"List(Off)", "", "", false},

		// The grouping round 2 added must survive untouched, including the two
		// shapes measured in the corpus: multiple arguments and nesting.
		{"List(Of Machine)", "List", "", true},
		{"List(of Machine)", "List", "", true},
		{"List(OF Machine)", "List", "", true},
		{"List(Of KeyValuePair(Of String, Action))", "List", "", true},
		{"System.Collections.Generic.List(Of Machine)", "System.Collections.Generic.List", "", true},
		{"IComparable(Of Profile).CompareTo", "IComparable", "CompareTo", true},
	}
	for _, tc := range cases {
		gotType, gotMemb, gotOK := VBNetExternalHierarchyTarget(tc.in)
		if gotType != tc.wantType || gotMemb != tc.wantMemb || gotOK != tc.wantOK {
			t.Errorf("VBNetExternalHierarchyTarget(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.in, gotType, gotMemb, gotOK, tc.wantType, tc.wantMemb, tc.wantOK)
		}
	}

	// The predicate itself, in both directions, so the rejections above cannot
	// be satisfied by an always-false mutant and the acceptances above cannot
	// be satisfied by an always-true one.
	for _, s := range []string{"Of T", "of T", "Of  T", "Of\tT", "Of A, B", "Of KeyValuePair(Of String, Action)", "Of A , B"} {
		if !isVBGenericArgList(s) {
			t.Errorf("isVBGenericArgList(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "Of", "Of ", "OfT", "Off", "T", "deprecated", "!!! @@@", "Of ,", "Of A,", "Of A)(", "Of A(", "Of 9T", "Of A.B C"} {
		if isVBGenericArgList(s) {
			t.Errorf("isVBGenericArgList(%q) = true, want false", s)
		}
	}
}
