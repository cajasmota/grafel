package fsharp

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// #6912 arm E — an INTERNAL test, and it exists for one reason: the
// `r.SourceFile != filePath` conjunct appears THREE times in this arm (both
// loops of fsharpInFileTypeTargets and the field loop in
// attachFSharpFieldTypeRefs) and none of the three can be reached through
// Extract.
//
// extractFSharp is called once per file and every record it builds carries that
// file's path, so from the outside the conjunct never fires and a mutant
// deleting it survives the entire external suite — the "alive but unreachable"
// verdict. Calling the function directly with the input the guard exists for is
// the cheap answer; the whole cost is this file.
//
// The guard is what keeps the pass correct if it is ever handed a multi-file
// slice, which is not hypothetical: attachFSharpFieldTypeRefs is called on the
// post-PrependFileCarrier slice precisely so it sees EVERY record, and a future
// caller that batches files would silently start binding a field in one file to
// a same-named type in another. That is the cross-file hazard #6976/#6369 are
// about, and #6369 was an F# defect.
func TestFSharpFieldTypeRefs_TargetsAreScopedToTheRequestedFile(t *testing.T) {
	records := []types.EntityRecord{
		{Name: "Customer", Kind: "SCOPE.Component", Subtype: "record", SourceFile: "A.fs"},
		{Name: "Money", Kind: "SCOPE.Component", Subtype: "record", SourceFile: "A.fs"},
		// Same shape, different file. Neither may appear in A.fs's target set,
		// and — the direction that matters — `Shipper` must NOT become a target
		// for A.fs merely because some other file declares it.
		{Name: "Shipper", Kind: "SCOPE.Component", Subtype: "interface", SourceFile: "B.fs"},
		{Name: "Meters", Kind: "SCOPE.Component", Subtype: "alias", SourceFile: "B.fs"},
		// A DIFFERENT-KINDED record in B.fs sharing a name with an A.fs type.
		// This row grades the SourceFile conjunct in the FIRST loop — the one
		// that builds nameKinds — which the rows above cannot: they share no
		// name with A.fs, so a cross-file leak there changes nothing. With that
		// conjunct removed, nameKinds["Customer"] gains a second kind from B.fs
		// and the ambiguity rule wrongly suppresses A.fs's unambiguous Customer.
		// Both loops carry the conjunct, so both are graded rather than one
		// being paid for and its twin left open.
		{Name: "Customer", Kind: "SCOPE.Operation", Subtype: "let", SourceFile: "B.fs"},
	}

	got := fsharpInFileTypeTargets(records, "A.fs")
	want := map[string]string{
		"Customer": "scope:component:class:fsharp:A.fs:Customer",
		"Money":    "scope:component:class:fsharp:A.fs:Money",
	}
	if len(got) != len(want) {
		t.Fatalf("target set has %d entries %v, want %d %v", len(got), fsKeysOf(got), len(want), want)
	}
	for name, toID := range want {
		tgt, ok := got[name]
		if !ok {
			t.Fatalf("%q missing from A.fs's target set %v", name, fsKeysOf(got))
		}
		if tgt.toID != toID {
			t.Errorf("%q toID = %q, want %q", name, tgt.toID, toID)
		}
	}
	for _, foreign := range []string{"Shipper", "Meters"} {
		if _, ok := got[foreign]; ok {
			t.Errorf("%q is declared in B.fs and must NOT be a target for A.fs — "+
				"the same-file rule is this arm's central promise", foreign)
		}
	}

	// The mirror direction, so the assertion above is not merely "B.fs's
	// records were ignored entirely": asking for B.fs returns B.fs's
	// declarations and none of A.fs's.
	gotB := fsharpInFileTypeTargets(records, "B.fs")
	if _, ok := gotB["Shipper"]; !ok {
		t.Fatalf("B.fs's own target set %v is missing Shipper — the assertions "+
			"above would pass even if the function returned nothing", fsKeysOf(gotB))
	}
	if _, ok := gotB["Customer"]; ok {
		// TWO independent reasons this name must be absent from B.fs's set, and
		// the row above means they are no longer the same reason: A.fs's
		// Customer is cross-file, and B.fs's OWN Customer is a SCOPE.Operation,
		// which the allow-list refuses. Either alone suffices; the absence
		// covers both.
		t.Errorf("Customer must not be a target for B.fs: A.fs's is cross-file " +
			"and B.fs's own is a SCOPE.Operation, which is not a type declaration")
	}

	// The THIRD site: attachFSharpFieldTypeRefs' own SourceFile conjunct on the
	// field record. A field in B.fs must not pick up A.fs's targets even when
	// the target set was built for A.fs.
	withField := append([]types.EntityRecord{}, records...)
	withField = append(withField, types.EntityRecord{
		Name: "Foreign.Buyer", Kind: "SCOPE.Schema", Subtype: "field", SourceFile: "B.fs",
		Properties: map[string]string{
			"member_name": "Buyer", "member_type": "Money", "parent_class": "Foreign",
		},
	})
	out := attachFSharpFieldTypeRefs(withField, "A.fs")
	for i := range out {
		if out[i].Name == "Foreign.Buyer" && len(out[i].Relationships) != 0 {
			t.Errorf("a B.fs field received %d edge(s) from A.fs's target set — "+
				"the field loop's SourceFile conjunct is not holding",
				len(out[i].Relationships))
		}
	}
}

func fsKeysOf(m map[string]fsharpFieldTypeTarget) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
