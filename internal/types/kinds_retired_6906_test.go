package types

import (
	"os"
	"strings"
	"testing"
)

// Issue #6906 — TYPED_AS and HAS_TYPE are RETIRED. This guard fails if either
// name comes back.
//
// Both were declared in kinds.go and registered in AllRelationshipKinds() with
// zero producers and zero consumers, and both named one concept: a field or
// parameter → its declared type. #6984 shipped that edge for C# as
// RelationshipKindReferences with the property ref_kind="field_target_type",
// adopting the spelling the custom lane had already shipped in five languages.
// Re-declaring either constant would put a third name on one concept.
//
// WHY THE LITERALS ARE WRITTEN OUT HERE. Every assertion below compares against
// an INDEPENDENT string literal, never against AllRelationshipKinds() or any
// other production value (#6975): a guard that ranges over the production slice
// and asks whether it contains what the production slice contains cannot
// observe a re-addition. "TYPED_AS" and "HAS_TYPE" appear in this file as
// hard-coded strings for that reason, and must not be replaced by a reference
// to a constant.
var retired6906Kinds = []string{"TYPED_AS", "HAS_TYPE"}

// TestRetiredRelationshipKindsStayRetired asserts the retirement on all three
// surfaces a re-addition could use: the const declarations in kinds.go (read
// from source, so a constant declared under ANY name is caught by its value),
// the registered vocabulary, and the IsValidRelationshipKind predicate.
func TestRetiredRelationshipKindsStayRetired(t *testing.T) {
	declared := declaredRelationshipKindsFromSource(t)

	// Non-vacuity, ahead of every absence check. A parse that read the wrong
	// file, or read the right file and matched nothing, would let all three
	// absence assertions below pass by examining nothing. Two independent
	// literals must be PRESENT before any absence is believed.
	if len(declared) == 0 {
		t.Fatalf("no RelationshipKind constants parsed from %s; every absence check below "+
			"would be vacuous", kindsSourceFile)
	}
	byValue := map[string]string{}
	for _, d := range declared {
		byValue[d.Value] = d.Name
	}
	for _, present := range []string{"CALLS", "REFERENCES"} {
		if _, ok := byValue[present]; !ok {
			t.Fatalf("control %q is not among the %d constants parsed from %s; the extraction is "+
				"not reading the declarations and the absence checks are vacuous",
				present, len(declared), kindsSourceFile)
		}
		if !IsValidRelationshipKind(present) {
			t.Fatalf("control IsValidRelationshipKind(%q) = false; the predicate under test is "+
				"not answering, so a false on a retired kind proves nothing", present)
		}
	}

	for _, retired := range retired6906Kinds {
		if name, ok := byValue[retired]; ok {
			t.Errorf("%s declares %s = %q, but that kind was retired by #6906/#5828. The "+
				"field→declared-type edge is REFERENCES with ref_kind=\"field_target_type\" "+
				"(internal/extractors/csharp/field_type_refs.go, #6984); see the RETIRED KINDS "+
				"note above AllRelationshipKinds.", kindsSourceFile, name, retired)
		}
		for _, k := range AllRelationshipKinds() {
			if string(k) == retired {
				t.Errorf("AllRelationshipKinds() registers the retired kind %q (#6906/#5828)", retired)
			}
		}
		if IsValidRelationshipKind(retired) {
			t.Errorf("IsValidRelationshipKind(%q) = true; the retired kind is back in the "+
				"vocabulary (#6906/#5828)", retired)
		}
	}
}

// coverageJSPath is the public coverage page's hand-maintained kind roster.
// Relative to this package directory, which is where `go test` runs.
const coverageJSPath = "../../site/src/scripts/coverage.js"

// TestRetiredRelationshipKindsAreGoneFromTheCoveragePage extends the retirement
// to the one surface a Go-only enumeration cannot see.
//
// The first version of this PR grepped the UPPERCASE literals and the Go
// constant names, and concluded "no dashboard or webui reference". It was
// wrong: site/src/scripts/coverage.js listed "has_type" and "typed_as" in its
// LOWERCASE display form, so the public page would have advertised two kinds
// that no longer exist. A name has more than one spelling — constant,
// string literal, lowercase display form — and an enumeration that covers one
// spelling has not covered the name.
//
// This is deliberately a plain substring scan rather than a JS parse: the
// spelling to catch IS the raw token, and a parser would add a way to be
// silently wrong about a file that has no Go meaning.
func TestRetiredRelationshipKindsAreGoneFromTheCoveragePage(t *testing.T) {
	raw, err := os.ReadFile(coverageJSPath)
	if err != nil {
		t.Fatalf("read %s: %v (the coverage page is checked in; if it moved, "+
			"repoint this guard rather than deleting it)", coverageJSPath, err)
	}
	page := string(raw)

	// Non-vacuity: the scan must be looking at a file that really carries the
	// lowercase roster, in the spelling the retired names would have used.
	// Without this a moved, emptied or restructured file reads as "absent".
	for _, control := range []string{`"references"`, `"inherits"`, `"contains"`} {
		if !strings.Contains(page, control) {
			t.Fatalf("control %s is absent from %s (%d bytes read); this scan is not looking at "+
				"the relationship-kind roster and its absence checks are vacuous",
				control, coverageJSPath, len(page))
		}
	}

	for _, retired := range retired6906Kinds {
		// The display form: lowercased, quoted as a roster item. The prose
		// comment in that file names the retired kinds in UPPERCASE to explain
		// why they are missing, which is why this matches the quoted lowercase
		// token and not the bare name.
		item := `"` + strings.ToLower(retired) + `"`
		if strings.Contains(page, item) {
			t.Errorf("%s still lists %s; %s was retired by #6906/#5828 and the public coverage "+
				"page would advertise a kind grafel cannot emit",
				coverageJSPath, item, retired)
		}
	}
}
