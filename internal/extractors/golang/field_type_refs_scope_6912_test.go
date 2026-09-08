package golang

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// #6912 arm D — an INTERNAL test, and it exists for one reason: the
// `r.SourceFile != filePath` conjunct in goInFileTypeTargets cannot be reached
// through Extract.
//
// Extract is called once per file and every record it builds carries that file's
// path, so from the outside the conjunct never fires and a mutant that deletes it
// survives the entire external suite. That is the "alive but unreachable" verdict
// — and the cheap answer to it is to call the function directly with the input
// the guard exists for, rather than to leave a conjunct ungraded and say it was
// too expensive. The whole cost is this file.
//
// The guard is what keeps the pass correct if goInFileTypeTargets is ever handed
// a multi-file slice — which is not hypothetical: attachClassContains and
// attachImplementsRelationships next door take the same (records, filePath)
// shape, and a future caller that batches files would silently start binding a
// field in one file to a same-named type in another. That is exactly the
// cross-file hazard #6976/#6369 are about, and the same-file rule is this arm's
// central promise.
func TestGoFieldTypeRefs_TargetsAreScopedToTheRequestedFile(t *testing.T) {
	records := []types.EntityRecord{
		{Name: "Customer", Kind: "SCOPE.Component", Subtype: "struct", SourceFile: "a.go"},
		{Name: "Order", Kind: "SCOPE.Component", Subtype: "struct", SourceFile: "a.go"},
		// Same shape, different file. Neither may appear in a.go's target set,
		// and — the direction that matters — `Shipper` must NOT become a target
		// for a.go merely because some other file declares it.
		{Name: "Shipper", Kind: "SCOPE.Component", Subtype: "interface", SourceFile: "b.go"},
		{Name: "Meters", Kind: "SCOPE.Schema", Subtype: "type_alias", SourceFile: "b.go"},
	}

	got := goInFileTypeTargets(records, "a.go")

	want := map[string]string{
		"Customer": "scope:component:class:go:a.go:Customer",
		"Order":    "scope:component:class:go:a.go:Order",
	}
	if len(got) != len(want) {
		t.Fatalf("target set has %d entries %v, want %d %v", len(got), keysOf(got), len(want), want)
	}
	for name, toID := range want {
		tgt, ok := got[name]
		if !ok {
			t.Fatalf("%q missing from a.go's target set %v", name, keysOf(got))
		}
		if tgt.toID != toID {
			t.Errorf("%q toID = %q, want %q", name, tgt.toID, toID)
		}
	}
	for _, foreign := range []string{"Shipper", "Meters"} {
		if _, ok := got[foreign]; ok {
			t.Errorf("%q is declared in b.go and must NOT be a target for a.go — "+
				"the same-file rule is this arm's central promise", foreign)
		}
	}

	// The mirror direction, so the assertion above is not just "b.go's records
	// were ignored entirely": asking for b.go returns b.go's declarations and
	// none of a.go's.
	gotB := goInFileTypeTargets(records, "b.go")
	if _, ok := gotB["Shipper"]; !ok {
		t.Fatalf("b.go's own target set %v is missing Shipper — the test above "+
			"would pass even if the function returned nothing", keysOf(gotB))
	}
	if _, ok := gotB["Customer"]; ok {
		t.Errorf("Customer is declared in a.go and must not be a target for b.go")
	}
}

func keysOf(m map[string]goFieldTypeTarget) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
