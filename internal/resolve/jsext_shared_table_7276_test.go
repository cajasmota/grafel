package resolve

import (
	"reflect"
	"testing"

	"github.com/cajasmota/grafel/internal/jsext"
)

// TestJSExtensionReplacementsIsTheSharedTable_7276 pins the SHARING, which no
// other test in either package can observe.
//
// internal/jsext says "add extensions HERE and nowhere else" and this package
// says "this is an alias, not a copy". Both are prose. The table's CONTENT is
// genuinely graded — mutating a family kills tests in internal/resolve and in
// internal/extractors/javascript simultaneously — but replacing the alias with
// an inline copy of the identical literal is invisible to every one of them.
// The copy would then drift on the next edit, and a drifted family is exactly
// the #7272 defect: the extractor stamps a resolvedFile the resolver keys
// nothing under.
//
// Map identity is the only observable that distinguishes an alias from an
// equal copy, so that is what this asserts.
func TestJSExtensionReplacementsIsTheSharedTable_7276(t *testing.T) {
	if len(jsExtensionReplacements) == 0 {
		t.Fatal("jsExtensionReplacements is empty — this guard would be vacuous")
	}
	got := reflect.ValueOf(jsExtensionReplacements).Pointer()
	want := reflect.ValueOf(jsext.Replacements).Pointer()
	if got != want {
		t.Errorf("jsExtensionReplacements is not the same map as jsext.Replacements "+
			"(%#x vs %#x) — it has been turned back into a copy. The two tables are "+
			"read by different packages on opposite sides of the extract→resolve "+
			"pipeline and MUST answer identically; a copy is free to drift and "+
			"nothing else in either suite would notice", got, want)
	}
}

// TestJSExtensionReplacementsSharingIsObservableThroughJsext_7276 is the
// positive control for the test above: it confirms the identity assertion is
// reachable by showing a deliberately-built equal-but-distinct table does NOT
// share the pointer. Without this, a reflect.Pointer comparison that happened
// to be trivially true (both nil, say) would grade nothing.
func TestJSExtensionReplacementsSharingIsObservableThroughJsext_7276(t *testing.T) {
	copyOfTable := make(map[string][]string, len(jsext.Replacements))
	for k, v := range jsext.Replacements {
		copyOfTable[k] = v
	}
	if !reflect.DeepEqual(copyOfTable, jsext.Replacements) {
		t.Fatal("the control copy is not equal to the shared table — the setup is wrong")
	}
	if reflect.ValueOf(copyOfTable).Pointer() == reflect.ValueOf(jsext.Replacements).Pointer() {
		t.Fatal("an equal-but-distinct map compared identical — the identity " +
			"assertion in the sibling test cannot distinguish an alias from a copy")
	}
}
